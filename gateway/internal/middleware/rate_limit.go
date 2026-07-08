package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// IPRateLimiter 包含针对不同 IP 的 rate.Limiter，支持并发安全
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  sync.RWMutex
	r   rate.Limit
	b   int
}

// NewIPRateLimiter 构造器
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		r:   r,
		b:   b,
	}
}

// GetLimiter 获取或动态创建指定 IP 的限流器
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.RLock()
	limiter, exists := i.ips[ip]
	i.mu.RUnlock()

	if exists {
		return limiter
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	// 双重检查以确保并发安全性
	limiter, exists = i.ips[ip]
	if !exists {
		limiter = rate.NewLimiter(i.r, i.b)
		i.ips[ip] = limiter
	}

	return limiter
}

// RateLimitMiddleware 限流中间件处理器
func RateLimitMiddleware(limiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getIP(r)
			l := limiter.GetLimiter(ip)
			if !l.Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests) // 429
				resp := ErrorResponse{
					Error:   "rate_limit_exceeded",
					Message: "too many requests",
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getIP 提取客户端 IP 地址，过滤端口号，支持 X-Forwarded-For 优先级
func getIP(r *http.Request) string {
	var ip string
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip = strings.TrimSpace(parts[0])
		}
	}
	if ip == "" {
		ip = r.RemoteAddr
	}

	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		return host
	}

	return strings.Trim(ip, "[]")
}
