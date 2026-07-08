package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type limiterItem struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter 包含针对不同 IP 的 rate.Limiter，支持并发安全与内存过期清理
type IPRateLimiter struct {
	ips map[string]*limiterItem
	mu  sync.RWMutex
	r   rate.Limit
	b   int
}

// NewIPRateLimiter 构造器
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*limiterItem),
		r:   r,
		b:   b,
	}
}

// GetLimiter 获取或动态创建指定 IP 的限流器，并刷新活跃时间戳
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	item, exists := i.ips[ip]
	if exists {
		item.lastSeen = time.Now()
		return item.limiter
	}

	limiter := rate.NewLimiter(i.r, i.b)
	i.ips[ip] = &limiterItem{
		limiter:  limiter,
		lastSeen: time.Now(),
	}
	return limiter
}

// StartCleanup 启动后台清理 Worker 协程，定期清理过期 IP 限流器
func (i *IPRateLimiter) StartCleanup(ctx context.Context, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.Cleanup(ttl)
		}
	}
}

// Cleanup 执行一次过期 IP 限流器的清理
func (i *IPRateLimiter) Cleanup(ttl time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := time.Now()
	for ip, item := range i.ips {
		if now.Sub(item.lastSeen) > ttl {
			delete(i.ips, ip)
		}
	}
}

// GetIPsCount 返回缓存的 IP 数量，便于单测断言
func (i *IPRateLimiter) GetIPsCount() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.ips)
}

// SetLimiterLastSeen 手动设置某个 IP 的 lastSeen，便于单测模拟超时
func (i *IPRateLimiter) SetLimiterLastSeen(ip string, t time.Time) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if item, exists := i.ips[ip]; exists {
		item.lastSeen = t
	}
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

// getIP 提取客户端 IP 地址，过滤端口号。仅当 TRUST_PROXY=true 时才信赖 X-Forwarded-For
func getIP(r *http.Request) string {
	var ip string
	if os.Getenv("TRUST_PROXY") == "true" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				ip = strings.TrimSpace(parts[0])
			}
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
