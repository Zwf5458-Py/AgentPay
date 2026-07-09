package proxy

import (
	"crypto/ecdsa"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gateway/internal/middleware"
	"gateway/internal/queue"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// ReverseProxyWrapper 封装了反向代理的逻辑
type ReverseProxyWrapper struct {
	proxy          *httputil.ReverseProxy
	aaBridgeURL    string
	agentOwner     string
	escrowAddress  string
	internalSecret string
	QueueManager   *queue.QueueManager
	privateKey     *ecdsa.PrivateKey
}

// NewReverseProxy 构造反向代理实例
func NewReverseProxy(targetURL string, aaBridgeURL string, internalSecret string, queueMgr *queue.QueueManager) (*ReverseProxyWrapper, error) {
	url, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	// 默认值
	agentOwner := os.Getenv("AGENT_OWNER")
	if agentOwner == "" {
		agentOwner = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" // Anvil 默认第一个账户
	}

	escrowAddress := os.Getenv("ESCROW_ADDRESS")
	if escrowAddress == "" {
		escrowAddress = "0x5FbDB2315678afecb367f032d93F642f64180aa3" // 默认合约 Mock 地址
	}

	// 加载私钥
	var privKey *ecdsa.PrivateKey
	privKeyHex := os.Getenv("GATEWAY_PRIVATE_KEY")
	if privKeyHex != "" {
		privKeyHex = strings.TrimPrefix(privKeyHex, "0x")
		k, err := crypto.HexToECDSA(privKeyHex)
		if err != nil {
			return nil, fmt.Errorf("failed to parse GATEWAY_PRIVATE_KEY: %w", err)
		}
		privKey = k
		log.Println("[Proxy] Successfully loaded GATEWAY_PRIVATE_KEY.")
	}

	if privKey == nil {
		log.Println("[Proxy] GATEWAY_PRIVATE_KEY not set. Generating a temporary ECDSA key in memory.")
		k, err := crypto.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate temporary private key: %w", err)
		}
		privKey = k
	}

	wrapper := &ReverseProxyWrapper{
		aaBridgeURL:    aaBridgeURL,
		agentOwner:     agentOwner,
		escrowAddress:  escrowAddress,
		internalSecret: internalSecret,
		QueueManager:   queueMgr,
		privateKey:     privKey,
	}

	proxy := httputil.NewSingleHostReverseProxy(url)

	// 劫持响应
	proxy.ModifyResponse = func(res *http.Response) error {
		// 1. 异步入队结算原逻辑
		proof := res.Header.Get("X-Agent-Proof")
		if proof != "" {
			// 提取 lockId
			ctx := res.Request.Context()
			lockID := middleware.GetLockID(ctx)

			if lockID != "" {
				log.Printf("[Proxy] Intercepted X-Agent-Proof. Enqueueing settle task for lockId: %s", lockID)
				if err := wrapper.QueueManager.Enqueue(lockID, proof, wrapper.agentOwner, wrapper.escrowAddress); err != nil {
					log.Printf("[Proxy] Enqueue failed for lockId %s: %v", lockID, err)
				}
			} else {
				log.Printf("[Proxy] Intercepted X-Agent-Proof but lockId is missing in request context.")
			}
		}

		// 2. 生成并追加状态通道清算凭证 Settle Receipt
		ctx := res.Request.Context()
		channelID := middleware.GetChannelID(ctx)
		if channelID != "" {
			holdAmountStr := middleware.GetHoldAmount(ctx)
			nonceStr := middleware.GetNonce(ctx)

			// 计算实际开销 actualCost
			actualCost := int64(1000) // 默认微支付单次价格
			costStr := res.Header.Get("X-Agent-Cost")
			if costStr == "" {
				log.Println("[Proxy] No X-Agent-Cost header found in downstream response, defaulting to cost 1000")
			} else {
				if costVal, err := strconv.ParseInt(costStr, 10, 64); err == nil {
					if costVal < 0 {
						log.Println("[Proxy] Invalid negative X-Agent-Cost value found in downstream response, defaulting to cost 1000")
						log.Printf("[Proxy] Failed to parse X-Agent-Cost header: '%s' (or it is negative), defaulting to cost 1000", costStr)
						actualCost = 1000
					} else {
						actualCost = costVal
					}
				} else {
					log.Printf("[Proxy] Failed to parse X-Agent-Cost header: '%s' (or it is negative), defaulting to cost 1000", costStr)
					actualCost = 1000
				}
			}

			// 安全防线：实际扣款不超过预授权冻结额
			if holdAmountStr != "" {
				if holdVal, err := strconv.ParseInt(holdAmountStr, 10, 64); err == nil {
					if actualCost > holdVal {
						actualCost = holdVal
					}
				}
			}

			// 生成签名
			expectedMsg := fmt.Sprintf("%s:%s:%d:%s", channelID, holdAmountStr, actualCost, nonceStr)
			expectedMsgHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(expectedMsg), expectedMsg)))

			sigBytes, err := crypto.Sign(expectedMsgHash.Bytes(), wrapper.privateKey)
			if err != nil {
				log.Printf("[Proxy] Failed to sign settle receipt: %v", err)
			} else {
				sigBytes[64] += 27 // 格式化为以太坊 V 格式
				sigStr := hexutil.Encode(sigBytes)

				receiptStr := fmt.Sprintf("%s:%s:%d:%s:%s", channelID, holdAmountStr, actualCost, nonceStr, sigStr)
				res.Header.Set("X-402-Settle-Receipt", receiptStr)
				log.Printf("[Proxy] Appended Settle Receipt header: %s", receiptStr)
			}
		}

		return nil
	}

	wrapper.proxy = proxy
	return wrapper, nil
}

// ServeHTTP 转发请求
func (w *ReverseProxyWrapper) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	w.proxy.ServeHTTP(rw, req)
}
