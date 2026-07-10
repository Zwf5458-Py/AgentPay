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

	"ledger/rail"
	"ledger/service"
	pricingmodel "pricing/model"
	pricingservice "pricing/service"

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
	LedgerService  *service.LedgerService
	PricingService *pricingservice.PricingService
}

func (w *ReverseProxyWrapper) SetLedgerService(svc *service.LedgerService) {
	w.LedgerService = svc
}

func (w *ReverseProxyWrapper) SetPricingService(svc *pricingservice.PricingService) {
	w.PricingService = svc
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
		paymentMethod := middleware.GetPaymentMethod(ctx)
		channelID := middleware.GetChannelID(ctx)
		holdAmountStr := middleware.GetHoldAmount(ctx)
		nonceStr := middleware.GetNonce(ctx)
		expirationStr := middleware.GetExpiration(ctx)
		signature := middleware.GetSignature(ctx)

	// 1. 调用通用计费引擎 pricing.Quote 计算 modelCost / serviceFee / platformBps / actualCost
	var holdVal int64 = 0
	if holdAmountStr != "" {
		if val, err := strconv.ParseInt(holdAmountStr, 10, 64); err == nil {
			holdVal = val
		}
	}
	
	// 读取下游返回的 token 用量（X-Agent-Tokens），缺失时由 X-Agent-Cost 反推
	tokens := uint64(0)
	if tStr := res.Header.Get("X-Agent-Tokens"); tStr != "" {
		if tv, err := strconv.ParseUint(tStr, 10, 64); err == nil {
			tokens = tv
		}
	}
	if tokens == 0 && wrapper.PricingService != nil {
		if cv, err := strconv.ParseUint(res.Header.Get("X-Agent-Cost"), 10, 64); err == nil && cv > 0 {
			tokens = (cv / 1500) * 1000
			if cv%1500 != 0 {
				tokens += 1000
			}
		}
	}
	
	// 提前提取 agentID 供 pricing.Quote 使用
	agentIDStrTmp := res.Request.Header.Get("X-Agent-Id")
	if agentIDStrTmp == "" {
		agentIDStrTmp = os.Getenv("AGENT_ID")
	}
	agentIDTmp := int64(888)
	if agentIDStrTmp != "" {
		if val, err := strconv.ParseInt(agentIDStrTmp, 10, 64); err == nil {
			agentIDTmp = val
		}
	}
	var modelCost int64
	var serviceFeeVal int64
	var platformBps uint16
	var actualCost int64
	if wrapper.PricingService != nil && agentIDTmp != 0 {
		quote, qerr := wrapper.PricingService.Quote(ctx, strconv.FormatInt(agentIDTmp, 10), tokens, "")
		if qerr != nil {
			log.Printf("[Proxy] pricing.Quote failed: %v, falling back to env defaults", qerr)
		} else {
			modelCost = int64(quote.ModelCost)
			serviceFeeVal = int64(quote.ServiceFee)
			platformBps = quote.PlatformBps
			actualCost = int64(quote.MicroAmount)
		}
	}
	if actualCost == 0 {
		// 回退：保持 legacy env 行为
		modelCost = int64(1000)
		if cv, err := strconv.ParseInt(res.Header.Get("X-Agent-Cost"), 10, 64); err == nil && cv > 0 {
			modelCost = cv
		}
		if holdVal > 0 && modelCost > holdVal {
			modelCost = holdVal
		}
		platformBpsVal, _ := strconv.ParseUint(os.Getenv("PLATFORM_BPS"), 10, 16)
		if platformBpsVal == 0 {
			platformBpsVal = 10
		}
		platformBps = uint16(platformBpsVal)
		serviceFeeVal = int64(2000)
		if feeEnv := os.Getenv("AGENT_SERVICE_FEE"); feeEnv != "" {
			if val, err := strconv.ParseInt(feeEnv, 10, 64); err == nil {
				serviceFeeVal = val
			}
		}
		if channelID != "" {
			actualCost = (modelCost + serviceFeeVal) * 10000 / (10000 - int64(platformBps))
		} else {
			actualCost = modelCost
		}
	}

		// 安全防线：实际总扣款不超过预授权冻结额
		if holdVal > 0 && actualCost > holdVal {
			actualCost = holdVal
		}

		// 提取 agentID
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

		// ----------------------------------------------------
		// A. Stripe (法币) 流程记账：直接在代理层同步记账
		// ----------------------------------------------------
		if paymentMethod == "stripe" {
			stripeSessionID := middleware.GetStripeSessionID(ctx)
			res.Header.Set("X-402-Payment-Method", "stripe")
			res.Header.Set("X-402-Stripe-Session", stripeSessionID)
			log.Printf("[Proxy] Request paid via Stripe session %s.", stripeSessionID)

			if wrapper.LedgerService != nil && stripeSessionID != "" {
				inv, err := wrapper.LedgerService.CreateInvoice(ctx, stripeSessionID, strconv.FormatInt(agentID, 10), uint64(actualCost), "stripe")
				if err == nil {
					payouts := []rail.Payout{
						{Target: wrapper.agentOwner, Amount: uint64(actualCost)},
					}
					errSettle := wrapper.LedgerService.SettleInvoice(ctx, inv.ID, uint64(actualCost), payouts, stripeSessionID, "")
					if errSettle != nil {
						log.Printf("[Proxy] Ledger settle failed for Stripe session %s: %v", stripeSessionID, errSettle)
					} else {
						log.Printf("[Proxy] Ledger double-entry record success for Stripe session %s", stripeSessionID)

						if wrapper.PricingService != nil {
							q := &pricingmodel.Quote{
								AgentID:     strconv.FormatInt(agentID, 10),
								ModelCost:   uint64(modelCost),
								ServiceFee:  uint64(serviceFeeVal),
								PlatformBps: platformBps,
								MicroAmount: uint64(actualCost),
							}
							errRecord := wrapper.PricingService.RecordUsage(ctx, q.AgentID, inv.ID, tokens, q)
							if errRecord != nil {
								log.Printf("[Proxy] Pricing record usage failed for Stripe session %s: %v", stripeSessionID, errRecord)
							} else {
								log.Printf("[Proxy] Pricing successfully recorded usage for Stripe session %s, tokens %d", stripeSessionID, tokens)
							}
						}
					}
				} else {
					log.Printf("[Proxy] Ledger CreateInvoice failed for Stripe session %s: %v", stripeSessionID, err)
				}
			}
			return nil
		}

		// ----------------------------------------------------
		// B. Crypto (通道) 流程：创建 Invoice，入队异步清算
		// ----------------------------------------------------
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

					var serviceFee uint64 = uint64(serviceFeeVal)

					// 创建账本 Invoice
					var invoiceID string
					if wrapper.LedgerService != nil {
						inv, err := wrapper.LedgerService.CreateInvoice(ctx, channelID, strconv.FormatInt(agentID, 10), uint64(actualCost), "crypto")
						if err == nil {
							invoiceID = inv.ID
							log.Printf("[Proxy] Ledger created pending invoice %s for channel %s", invoiceID, channelID)
						} else {
							log.Printf("[Proxy] Ledger CreateInvoice failed for channel %s: %v", channelID, err)
						}
					}

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
						InvoiceID:         invoiceID,
						Tokens:            tokens,
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
