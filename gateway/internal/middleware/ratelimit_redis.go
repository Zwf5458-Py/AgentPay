package middleware

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

// RedisRateLimiter 分布式限流 + 并发锁，通过 Redis 集中管理
type RedisRateLimiter struct {
	client *redis.Client
	r      rate.Limit
	b      int
	ctx    context.Context
}

// NewRedisRateLimiter 构造器
// redisURL 默认为 redis://localhost:6379
func NewRedisRateLimiter(redisURL string, r rate.Limit, b int) *RedisRateLimiter {
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr:         redisURL,
		Password:     "",
		DB:           0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
	})

	ctx := context.Background()

	// 验证连通性
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("[RedisRateLimiter] Warning: Failed to connect to Redis at %s: %v", redisURL, err)
		log.Printf("[RedisRateLimiter] Falling back to in-memory rate limiter")
		// 连接失败时关闭客户端，降级为内存限流
		client.Close()
		return nil
	}

	log.Printf("[RedisRateLimiter] Successfully connected to Redis at %s", redisURL)
	return &RedisRateLimiter{
		client: client,
		r:      r,
		b:      b,
		ctx:    ctx,
	}
}

// Allow 检查 IP 令牌桶是否允许请求
// 使用 Redis INCR + EXPIRE 实现令牌桶模式
func (l *RedisRateLimiter) Allow(ip string) bool {
	if l == nil || l.client == nil {
		return true // 降级模式：总是允许（由调用方的 fallback 处理）
	}

	key := fmt.Sprintf("ratelimit:ip:%s", ip)
	now := time.Now().UnixNano()

	// 使用 Lua 脚本原子执行令牌桶检查
	script := `
		local key = KEYS[1]
		local rate = tonumber(ARGV[1])
		local burst = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])

		local bucket = redis.call('HMGET', key, 'tokens', 'timestamp')
		local tokens = tonumber(bucket[1])
		local timestamp = tonumber(bucket[2])

		if tokens == nil then
			tokens = burst
			timestamp = now
		end

		local delta = math.min(burst, (now - timestamp) / 1e9 * rate)
		tokens = math.min(burst, tokens + delta)

		if tokens >= 1 then
			tokens = tokens - 1
			redis.call('HMSET', key, 'tokens', tokens, 'timestamp', now)
			redis.call('EXPIRE', key, 60)
			return 1
		else
			redis.call('HMSET', key, 'tokens', tokens, 'timestamp', now)
			redis.call('EXPIRE', key, 60)
			return 0
		end
	`

	result, err := l.client.Eval(l.ctx, script, []string{key},
		strconv.FormatFloat(float64(l.r), 'f', -1, 64),
		strconv.Itoa(l.b),
		strconv.FormatInt(now, 10),
	).Int()

	if err != nil {
		log.Printf("[RedisRateLimiter] Allow check failed for IP %s: %v", ip, err)
		return true // 降级：允许请求
	}

	return result == 1
}

// TryLockAgent 尝试为指定 agent 获取并发锁
// 使用 Lua SET NX EX 原子操作防止竞态
// Key: lock:agent:{agentID} Value: callID TTL: 30s
// 返回 (true, nil) 表示成功获取锁，(false, nil) 表示已被占用，(false, err) 表示Redis错误
func (l *RedisRateLimiter) TryLockAgent(ctx context.Context, agentID, callID string) (bool, error) {
	if l == nil || l.client == nil {
		return true, nil // 降级模式：总是允许（由调用方处理）
	}

	ttlStr := os.Getenv("AGENT_LOCK_TTL")
	ttl := 30 // 默认 30 秒
	if ttlStr != "" {
		if parsed, err := strconv.Atoi(ttlStr); err == nil && parsed > 0 {
			ttl = parsed
		}
	}

	key := fmt.Sprintf("lock:agent:%s", agentID)

	script := `
		if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'EX', ARGV[2]) then
			return 1
		else
			return 0
		end
	`

	result, err := l.client.Eval(ctx, script, []string{key}, callID, strconv.Itoa(ttl)).Int()
	if err != nil {
		log.Printf("[RedisRateLimiter] TryLockAgent failed for agent %s: %v", agentID, err)
		return false, err
	}

	return result == 1, nil
}

// RenewLock 续期 agent 并发锁（心跳）
func (l *RedisRateLimiter) RenewLock(ctx context.Context, agentID, callID string) error {
	if l == nil || l.client == nil {
		return nil
	}

	ttlStr := os.Getenv("AGENT_LOCK_TTL")
	ttl := 30
	if ttlStr != "" {
		if parsed, err := strconv.Atoi(ttlStr); err == nil && parsed > 0 {
			ttl = parsed
		}
	}

	key := fmt.Sprintf("lock:agent:%s", agentID)

	// 仅当值匹配时才续期（防止误删其他 callID 的锁）
	script := `
		if redis.call('GET', KEYS[1]) == ARGV[1] then
			return redis.call('EXPIRE', KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	_, err := l.client.Eval(ctx, script, []string{key}, callID, strconv.Itoa(ttl)).Int()
	return err
}

// ReleaseLock 显式释放 agent 并发锁
func (l *RedisRateLimiter) ReleaseLock(ctx context.Context, agentID, callID string) error {
	if l == nil || l.client == nil {
		return nil
	}

	key := fmt.Sprintf("lock:agent:%s", agentID)

	// 仅当值匹配时才删除（Lua GET + DEL 原子操作）
	script := `
		if redis.call('GET', KEYS[1]) == ARGV[1] then
			return redis.call('DEL', KEYS[1])
		else
			return 0
		end
	`

	_, err := l.client.Eval(ctx, script, []string{key}, callID).Int()
	return err
}

// Close 关闭 Redis 连接
func (l *RedisRateLimiter) Close() error {
	if l == nil || l.client == nil {
		return nil
	}
	return l.client.Close()
}

// GetRedisClient 暴露 Redis 客户端供测试使用
func (l *RedisRateLimiter) GetRedisClient() *redis.Client {
	if l == nil {
		return nil
	}
	return l.client
}
