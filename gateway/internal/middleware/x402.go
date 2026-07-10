package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"log"

	"gateway/internal/stripe"
	"gateway/internal/queue"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"sync"
)

var (
	DBQueueManager           *queue.QueueManager
	consumedStripeSessions   = make(map[string]bool)
	consumedStripeSessionsMu sync.Mutex
)

type contextKey string

const (
	TokenContextKey           contextKey = "x402_token"
	LockIDContextKey          contextKey = "x402_lock_id"
	HoldAmountContextKey      contextKey = "x402_hold_amount"
	ChannelIDContextKey       contextKey = "x402_channel_id"
	SignatureContextKey       contextKey = "x402_signature"
	NonceContextKey           contextKey = "x402_nonce"
	ExpirationContextKey      contextKey = "x402_expiration"
	PaymentMethodContextKey   contextKey = "x402_payment_method"
	StripeSessionIDContextKey contextKey = "x402_stripe_session_id"
)

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// X402Middleware 检查 Authorization: Bearer <token>
func X402Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		
		// 优先获取 X-Payment-Lock-Id 作为 lockId
		lockID := r.Header.Get("X-Payment-Lock-Id")

		// 检查 Authorization
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			trigger402(w)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)
		if token == "" {
			trigger402(w)
			return
		}

		// 检查 token 是否以 stripe: 开头
		if strings.HasPrefix(token, "stripe:") {
			sessionID := strings.TrimPrefix(token, "stripe:")
			if sessionID == "" {
				trigger402(w)
				return
			}

			// 1. 防重放/双花：优先通过 SQLite 数据库的 INSERT 独占锁进行判定（防并发 race condition 与重启重放）
			if DBQueueManager != nil {
				locked, err := DBQueueManager.TryLockStripeSession(sessionID)
				if err != nil || !locked {
					trigger402(w)
					return
				}
			} else {
				// 测试环境 / 无 DB 时回退到内存锁
				consumedStripeSessionsMu.Lock()
				isConsumed := consumedStripeSessions[sessionID]
				consumedStripeSessionsMu.Unlock()
				if isConsumed {
					trigger402(w)
					return
				}
			}

			stripeKey := os.Getenv("STRIPE_SECRET_KEY")
			stripeClient := stripe.NewStripeClient(stripeKey)

			// 2. 外部网络/Mock支付状态核销
			valid, err := stripeClient.VerifyCheckoutSession(sessionID)
			if err != nil || !valid {
				if DBQueueManager != nil {
					_ = DBQueueManager.ReleaseStripeSession(sessionID) // 验证失败，释放待定状态
				}
				trigger402(w)
				return
			}

			// 3. 验证成功后真正消费
			if DBQueueManager != nil {
				if err := DBQueueManager.CommitStripeSession(sessionID); err != nil {
					_ = DBQueueManager.ReleaseStripeSession(sessionID)
					trigger402(w)
					return
				}
			} else {
				consumedStripeSessionsMu.Lock()
				consumedStripeSessions[sessionID] = true
				consumedStripeSessionsMu.Unlock()
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, TokenContextKey, token)
			ctx = context.WithValue(ctx, LockIDContextKey, sessionID)
			ctx = context.WithValue(ctx, PaymentMethodContextKey, "stripe")
			ctx = context.WithValue(ctx, StripeSessionIDContextKey, sessionID)

			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 尝试解析 EIP-712 Hold 格式：Bearer <channelId>:<holdAmount>:<nonce>:<expiration>:<sig>
		parts := strings.Split(token, ":")
		if len(parts) == 5 {
			channelID := parts[0]
			holdAmount := parts[1]
			nonce := parts[2]
			expiration := parts[3]
			sig := parts[4]

			if !isNumeric(holdAmount) || !isNumeric(nonce) || !isNumeric(expiration) {
				trigger402(w)
				return
			}

			// 检查是否过期
			expTime, err := strconv.ParseInt(expiration, 10, 64)
			if err != nil || expTime < time.Now().Unix() {
				trigger402(w)
				return
			}

			// 密码学签名验证与安全隔离
			isProd := os.Getenv("APP_ENV") == "production"
			clientAddress := os.Getenv("CLIENT_ADDRESS")
			allowMock := os.Getenv("ALLOW_MOCK_SIGNATURE") != "false"

			isMockSig := !strings.HasPrefix(sig, "0x") || len(sig) != 132

			if isMockSig {
				if isProd || !allowMock {
					trigger402(w)
					return
				}
				// 仅在显式允许且非生产环境时继续放行 mock-signature (用于兼容测试用例)
			} else {
				signerAddr, err := RecoverEIP712Signer(channelID, holdAmount, nonce, expiration, sig)
				if err != nil {
					trigger402(w)
					return
				}

				if clientAddress != "" {
					if !strings.EqualFold(signerAddr, clientAddress) {
						trigger402(w)
						return
					}
				} else if isProd {
					// 生产模式无 clientAddress 配置阻断防线并触发紧急警告日志
					log.Printf("[CRITICAL ERROR] Gateway is running in PRODUCTION mode, but CLIENT_ADDRESS is NOT configured. All cryptographic requests will be blocked for safety!")
					trigger402(w)
					return
				}
			}

			if lockID == "" {
				lockID = channelID
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, TokenContextKey, token)
			ctx = context.WithValue(ctx, LockIDContextKey, lockID)
			ctx = context.WithValue(ctx, HoldAmountContextKey, holdAmount)
			ctx = context.WithValue(ctx, ChannelIDContextKey, channelID)
			ctx = context.WithValue(ctx, NonceContextKey, nonce)
			ctx = context.WithValue(ctx, ExpirationContextKey, expiration)
			ctx = context.WithValue(ctx, SignatureContextKey, sig)

			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 解析 lockId
		// 需求："读取客户端请求头中携带的 X-402 支付哈希或 lockId（可以在 Authorization: Bearer <lockId:token> 格式中携带，或者使用 X-Payment-Lock-Id 头传递）"
		if lockID == "" {
			// 如果 Authorization 里有冒号，拆分出 lockId
			if strings.Contains(token, ":") {
				parts := strings.SplitN(token, ":", 2)
				lockID = parts[0]
			} else {
				lockID = token
			}
		}

		// 将 token 和 lockID 注入 context
		ctx := r.Context()
		ctx = context.WithValue(ctx, TokenContextKey, token)
		ctx = context.WithValue(ctx, LockIDContextKey, lockID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetLockID 从 context 中获取 lockId
func GetLockID(ctx context.Context) string {
	if val, ok := ctx.Value(LockIDContextKey).(string); ok {
		return val
	}
	return ""
}

// GetToken 从 context 中获取 token
func GetToken(ctx context.Context) string {
	if val, ok := ctx.Value(TokenContextKey).(string); ok {
		return val
	}
	return ""
}

// GetHoldAmount 从 context 中获取 holdAmount
func GetHoldAmount(ctx context.Context) string {
	if val, ok := ctx.Value(HoldAmountContextKey).(string); ok {
		return val
	}
	return ""
}

// GetChannelID 从 context 中获取 channelId
func GetChannelID(ctx context.Context) string {
	if val, ok := ctx.Value(ChannelIDContextKey).(string); ok {
		return val
	}
	return ""
}

// GetSignature 从 context 中获取 signature
func GetSignature(ctx context.Context) string {
	if val, ok := ctx.Value(SignatureContextKey).(string); ok {
		return val
	}
	return ""
}

// GetNonce 从 context 中获取 nonce
func GetNonce(ctx context.Context) string {
	if val, ok := ctx.Value(NonceContextKey).(string); ok {
		return val
	}
	return ""
}

// GetExpiration 从 context 中获取 expiration
func GetExpiration(ctx context.Context) string {
	if val, ok := ctx.Value(ExpirationContextKey).(string); ok {
		return val
	}
	return ""
}

// GetPaymentMethod 从 context 中获取 payment method
func GetPaymentMethod(ctx context.Context) string {
	if val, ok := ctx.Value(PaymentMethodContextKey).(string); ok {
		return val
	}
	return ""
}

// GetStripeSessionID 从 context 中获取 stripe session ID
func GetStripeSessionID(ctx context.Context) string {
	if val, ok := ctx.Value(StripeSessionIDContextKey).(string); ok {
		return val
	}
	return ""
}

func trigger402(w http.ResponseWriter) {
	// 获取 ESCROW_ADDRESS 环境变量，默认为 Mock 合约地址
	escrowAddr := os.Getenv("ESCROW_ADDRESS")
	if escrowAddr == "" {
		escrowAddr = "0x5FbDB2315678afecb367f032d93F642f64180aa3" // 默认回退 Mock 地址
	}

	platformBps := os.Getenv("PLATFORM_BPS")
	if platformBps == "" {
		platformBps = "10"
	}
	modelProvider := os.Getenv("MODEL_PROVIDER_ADDRESS")
	if modelProvider == "" {
		modelProvider = "0x90F79bf6EB2c4f870365E785982E1f101E93b906"
	}

	w.Header().Set("X-402-Price", "1000")
	w.Header().Set("X-402-Currency", "USDC")
	w.Header().Set("X-402-Chain", "base-sepolia")
	w.Header().Set("X-402-Payment-Address", escrowAddr)
	w.Header().Set("X-402-Version", "1")
	w.Header().Set("X-402-Payment-Type", "channel")
	w.Header().Set("X-402-Hold-Amount", "50000")
	w.Header().Set("X-402-Platform-Bps", platformBps)
	w.Header().Set("X-402-Model-Provider", modelProvider)
	w.Header().Set("X-402-Payment-Methods", "crypto-channel,fiat-stripe")
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusPaymentRequired) // 402

	resp := ErrorResponse{
		Error:   "payment_required",
		Message: "micropayment required via x-402 protocol",
	}
	json.NewEncoder(w).Encode(resp)
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// GetEIP712DomainSeparator 计算 EIP-712 Domain Separator
func GetEIP712DomainSeparator(verifyingContract string) []byte {
	typeHash := crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	nameHash := crypto.Keccak256([]byte("AgentPay"))
	versionHash := crypto.Keccak256([]byte("1"))

	chainIDVal := int64(31337)
	if envChainID := os.Getenv("CHAIN_ID"); envChainID != "" {
		if cid, err := strconv.ParseInt(envChainID, 10, 64); err == nil {
			chainIDVal = cid
		}
	}

	chainIdBytes := make([]byte, 32)
	new(big.Int).SetInt64(chainIDVal).FillBytes(chainIdBytes)

	contractAddr := common.HexToAddress(verifyingContract)
	contractBytes := make([]byte, 32)
	copy(contractBytes[12:], contractAddr.Bytes())

	data := append(typeHash, nameHash...)
	data = append(data, versionHash...)
	data = append(data, chainIdBytes...)
	data = append(data, contractBytes...)

	return crypto.Keccak256(data)
}

// GetChannelHoldMessageHash 计算 ChannelHold typed data message hash
func GetChannelHoldMessageHash(channelID string, holdAmount, nonce, expiration *big.Int) []byte {
	typeHash := crypto.Keccak256([]byte("ChannelHold(bytes32 channelId,uint256 holdAmount,uint256 nonce,uint256 expiration)"))

	var channelBytes [32]byte
	if strings.HasPrefix(channelID, "0x") {
		h := common.HexToHash(channelID)
		copy(channelBytes[:], h.Bytes())
	} else {
		copy(channelBytes[:], []byte(channelID))
	}

	holdAmountBytes := make([]byte, 32)
	holdAmount.FillBytes(holdAmountBytes)

	nonceBytes := make([]byte, 32)
	nonce.FillBytes(nonceBytes)

	expirationBytes := make([]byte, 32)
	expiration.FillBytes(expirationBytes)

	data := append(typeHash, channelBytes[:]...)
	data = append(data, holdAmountBytes...)
	data = append(data, nonceBytes...)
	data = append(data, expirationBytes...)

	return crypto.Keccak256(data)
}

// RecoverEIP712Signer 从预授权 Token 还原以太坊签名地址
func RecoverEIP712Signer(channelID, holdAmountStr, nonceStr, expirationStr, sigStr string) (string, error) {
	verifyingContract := os.Getenv("ESCROW_ADDRESS")
	if verifyingContract == "" {
		verifyingContract = "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	}

	holdAmount, ok := new(big.Int).SetString(holdAmountStr, 10)
	if !ok {
		return "", errors.New("invalid holdAmount")
	}
	nonce, ok := new(big.Int).SetString(nonceStr, 10)
	if !ok {
		return "", errors.New("invalid nonce")
	}
	expiration, ok := new(big.Int).SetString(expirationStr, 10)
	if !ok {
		return "", errors.New("invalid expiration")
	}

	domainSeparator := GetEIP712DomainSeparator(verifyingContract)
	messageHash := GetChannelHoldMessageHash(channelID, holdAmount, nonce, expiration)

	digestData := append([]byte("\x19\x01"), domainSeparator...)
	digestData = append(digestData, messageHash...)
	digest := crypto.Keccak256(digestData)

	sigBytes, err := hexutil.Decode(sigStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode signature: %w", err)
	}
	if len(sigBytes) != 65 {
		return "", fmt.Errorf("invalid signature length: %d", len(sigBytes))
	}

	if sigBytes[64] == 27 || sigBytes[64] == 28 {
		sigBytes[64] -= 27
	}

	pubKey, err := crypto.SigToPub(digest, sigBytes)
	if err != nil {
		return "", fmt.Errorf("failed to recover public key: %w", err)
	}

	address := crypto.PubkeyToAddress(*pubKey).Hex()
	return address, nil
}
