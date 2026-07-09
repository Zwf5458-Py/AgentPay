package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type contextKey string

const (
	TokenContextKey      contextKey = "x402_token"
	LockIDContextKey     contextKey = "x402_lock_id"
	HoldAmountContextKey contextKey = "x402_hold_amount"
	ChannelIDContextKey  contextKey = "x402_channel_id"
	SignatureContextKey  contextKey = "x402_signature"
	NonceContextKey      contextKey = "x402_nonce"
	ExpirationContextKey contextKey = "x402_expiration"
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

func trigger402(w http.ResponseWriter) {
	// 获取 ESCROW_ADDRESS 环境变量，默认为 Mock 合约地址
	escrowAddr := os.Getenv("ESCROW_ADDRESS")
	if escrowAddr == "" {
		escrowAddr = "0x5FbDB2315678afecb367f032d93F642f64180aa3" // 默认回退 Mock 地址
	}

	w.Header().Set("X-402-Price", "1000")
	w.Header().Set("X-402-Currency", "USDC")
	w.Header().Set("X-402-Chain", "base-sepolia")
	w.Header().Set("X-402-Payment-Address", escrowAddr)
	w.Header().Set("X-402-Version", "1")
	w.Header().Set("X-402-Payment-Type", "channel")
	w.Header().Set("X-402-Hold-Amount", "50000")
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
