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
		isProd := os.Getenv("APP_ENV") == "production"
		if isProd {
			return nil, fmt.Errorf("GATEWAY_PRIVATE_KEY must be provided in production environment")
		}
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
		ctx := res.Request.Context()
		channelID := middleware.GetChannelID(ctx)
		holdAmountStr := middleware.GetHoldAmount(ctx)
		nonceStr := middleware.GetNonce(ctx)
		expirationStr := middleware.GetExpiration(ctx)
		signature := middleware.GetSignature(ctx)

		// 1. 统一提取与计算 modelCost 和 actualCost
		var holdVal int64 = 0
		if holdAmountStr != "" {
			if val, err := strconv.ParseInt(holdAmountStr, 10, 64); err == nil {
				holdVal = val
			}
		}

		// 计算模型费 modelCost (来自 X-Agent-Cost)
		modelCost := int64(1000) // 默认 1000
		costStr := res.Header.Get("X-Agent-Cost")
		if costStr == "" {
			log.Println("[Proxy] No X-Agent-Cost header found in downstream response, defaulting to model cost 1000")
		} else {
			if costVal, err := strconv.ParseInt(costStr, 10, 64); err == nil {
				if costVal < 0 {
					log.Println("[Proxy] Invalid negative X-Agent-Cost value found in downstream response, defaulting to model cost 1000")
					modelCost = 1000
				} else {
					modelCost = costVal
				}
			} else {
				log.Printf("[Proxy] Failed to parse X-Agent-Cost header: '%s', defaulting to model cost 1000", costStr)
				modelCost = 1000
			}
		}

		// 限制 modelCost 不超过 holdAmount
		if holdVal > 0 && modelCost > holdVal {
			modelCost = holdVal
		}

		platformBpsStr := os.Getenv("PLATFORM_BPS")
		if platformBpsStr == "" {
			platformBpsStr = "10"
		}
		platformBpsVal, _ := strconv.ParseUint(platformBpsStr, 10, 16)
		platformBps := uint16(platformBpsVal)

		serviceFeeVal := int64(2000)
		if feeEnv := os.Getenv("AGENT_SERVICE_FEE"); feeEnv != "" {
			if val, err := strconv.ParseInt(feeEnv, 10, 64); err == nil {
				serviceFeeVal = val
			}
		}

		var actualCost int64
		if channelID != "" {
			actualCost = (modelCost + serviceFeeVal) * 10000 / (10000 - int64(platformBps))
		} else {
			actualCost = modelCost
		}

		// 安全防线：实际总扣款不超过预授权冻结额
		if holdVal > 0 && actualCost > holdVal {
			actualCost = holdVal
		}

		// 2. 异步入队结算
		proof := res.Header.Get("X-Agent-Proof")
		if proof != "" {
			lockID := middleware.GetLockID(ctx)
			if lockID != "" {
				enqueueLockID := lockID
				if channelID != "" && nonceStr != "" {
					enqueueLockID = fmt.Sprintf("%s:%s", channelID, nonceStr)
				}
				log.Printf("[Proxy] Intercepted X-Agent-Proof. Enqueueing settle task for lockId: %s", enqueueLockID)

				if channelID != "" {
					// 三方分账参数准备
					var holdAmount uint64
					if holdVal > 0 {
						holdAmount = uint64(holdVal)
					}
					var nonce uint64
					if val, err := strconv.ParseUint(nonceStr, 10, 64); err == nil {
						nonce = val
					}
					var expiration uint64
					if val, err := strconv.ParseUint(expirationStr, 10, 64); err == nil {
						expiration = val
					}

					modelProvider := os.Getenv("MODEL_PROVIDER_ADDRESS")
					if modelProvider == "" {
						modelProvider = "0x90F79bf6EB2c4f870365E785982E1f101E93b906"
					}

					treasury := os.Getenv("TREASURY_ADDRESS")
					if treasury == "" {
						treasury = "0x15d34AAf54a67C68101F309492526a9000025B7b"
					}

					agentIDStr := res.Request.Header.Get("X-Agent-Id")
					if agentIDStr == "" {
						agentIDStr = os.Getenv("AGENT_ID")
					}
					var agentID int64 = 888
					if agentIDStr != "" {
						if val, err := strconv.ParseInt(agentIDStr, 10, 64); err == nil {
							agentID = val
						}
					}

					var serviceFee uint64 = uint64(serviceFeeVal)

					taskDetails := &queue.SettleTask{
						ChannelID:         channelID,
						HoldAmount:        holdAmount,
						Nonce:             nonce,
						Expiration:        expiration,
						Signature:         signature,
						AccumulatedAmount: uint64(actualCost),
						ModelCost:         uint64(modelCost),
						ServiceFee:        serviceFee,
						ModelProvider:     modelProvider,
						Treasury:          treasury,
						PlatformBps:       platformBps,
						AgentID:           agentID,
					}

					if err := wrapper.QueueManager.Enqueue(enqueueLockID, proof, wrapper.agentOwner, wrapper.escrowAddress, taskDetails); err != nil {
						log.Printf("[Proxy] Enqueue split-settle failed for lockId %s: %v", enqueueLockID, err)
					}
				} else {
					// 遗留锁模式
					if err := wrapper.QueueManager.Enqueue(enqueueLockID, proof, wrapper.agentOwner, wrapper.escrowAddress, nil); err != nil {
						log.Printf("[Proxy] Enqueue legacy failed for lockId %s: %v", enqueueLockID, err)
					}
				}
			} else {
				log.Printf("[Proxy] Intercepted X-Agent-Proof but lockId is missing in request context.")
			}
		}

		// 3. 生成并追加状态通道清算凭证 Settle Receipt
		if channelID != "" {
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
