package middleware

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// TestRedisRateLimiter_Connection 测试 Redis 连接降级
func TestRedisRateLimiter_Connection(t *testing.T) {
	// 测试无效 Redis URL → 返回 nil（降级模式）
	limiter := NewRedisRateLimiter("redis://localhost:6399", rate.Limit(5), 10)
	if limiter != nil {
		t.Error("Expected nil limiter for unreachable Redis (fallback mode)")
	}

	// 测试空 URL → 默认 localhost:6379，如果本地无 Redis 应降级为 nil
	limiter2 := NewRedisRateLimiter("", rate.Limit(5), 10)
	// 本地可能无 Redis，两种情况都接受：nil（降级）或非 nil（已连接）
	if limiter2 != nil {
		defer limiter2.Close()
		// 如果连接成功，验证 Allow 方法正常
		if !limiter2.Allow("test-ip") {
			t.Error("Expected Allow to return true on first request")
		}
	}
}

// TestRedisRateLimiter_TryLockAgent 测试并发锁原子性
func TestRedisRateLimiter_TryLockAgent(t *testing.T) {
	redisURL := os.Getenv("REDIS_TEST_URL")
	if redisURL == "" {
		t.Skip("REDIS_TEST_URL not set, skipping Redis integration test")
	}

	limiter := NewRedisRateLimiter(redisURL, rate.Limit(5), 10)
	if limiter == nil {
		t.Skip("Redis not available, skipping test")
	}
	defer limiter.Close()

	ctx := context.Background()

	// 清理测试键
	client := limiter.GetRedisClient()
	client.Del(ctx, "lock:agent:test123")

	// 第一次获取锁 → 成功
	locked, err := limiter.TryLockAgent(ctx, "test123", "call-1")
	if err != nil {
		t.Fatalf("TryLockAgent failed: %v", err)
	}
	if !locked {
		t.Error("Expected first lock acquisition to succeed")
	}

	// 第二次获取同一 agent 锁（不同 callID）→ 失败
	locked2, err := limiter.TryLockAgent(ctx, "test123", "call-2")
	if err != nil {
		t.Fatalf("TryLockAgent failed: %v", err)
	}
	if locked2 {
		t.Error("Expected second lock acquisition to fail (already locked)")
	}

	// 释放锁
	err = limiter.ReleaseLock(ctx, "test123", "call-1")
	if err != nil {
		t.Fatalf("ReleaseLock failed: %v", err)
	}

	// 释放后重新获取 → 成功
	locked3, err := limiter.TryLockAgent(ctx, "test123", "call-3")
	if err != nil {
		t.Fatalf("TryLockAgent failed: %v", err)
	}
	if !locked3 {
		t.Error("Expected lock re-acquisition to succeed after release")
	}

	// 清理
	client.Del(ctx, "lock:agent:test123")
}

// TestRedisRateLimiter_Allow 测试令牌桶限流
func TestRedisRateLimiter_Allow(t *testing.T) {
	redisURL := os.Getenv("REDIS_TEST_URL")
	if redisURL == "" {
		t.Skip("REDIS_TEST_URL not set, skipping Redis integration test")
	}

	limiter := NewRedisRateLimiter(redisURL, rate.Limit(5), 2)
	if limiter == nil {
		t.Skip("Redis not available, skipping test")
	}
	defer limiter.Close()

	client := limiter.GetRedisClient()
	client.Del(context.Background(), "ratelimit:ip:test-allow")

	// 前 2 个请求应该成功（burst=2）
	for i := 0; i < 2; i++ {
		if !limiter.Allow("test-allow") {
			t.Errorf("Request %d should be allowed (within burst)", i)
		}
	}

	// 第 3 个请求应该被限流（超过 burst）
	if limiter.Allow("test-allow") {
		t.Error("Request 3 should be rate-limited (exceeds burst)")
	}

	// 清理
	client.Del(context.Background(), "ratelimit:ip:test-allow")
}

// TestRedisRateLimiter_Fallback 测试降级模式行为
func TestRedisRateLimiter_Fallback(t *testing.T) {
	// 模拟降级模式（nil limiter）
	var limiter *RedisRateLimiter = nil

	// 降级模式允许所有请求
	if !limiter.Allow("any-ip") {
		t.Error("Fallback mode should allow all requests")
	}

	// 降级模式总是允许锁
	locked, err := limiter.TryLockAgent(context.Background(), "agent1", "call1")
	if err != nil {
		t.Fatalf("Fallback TryLockAgent should not error: %v", err)
	}
	if !locked {
		t.Error("Fallback TryLockAgent should return true")
	}

	// 降级模式释放锁无错误
	if err := limiter.ReleaseLock(context.Background(), "agent1", "call1"); err != nil {
		t.Errorf("Fallback ReleaseLock should not error: %v", err)
	}
}

// TestRedisRateLimiter_ConcurrentLock 测试并发锁竞争
func TestRedisRateLimiter_ConcurrentLock(t *testing.T) {
	redisURL := os.Getenv("REDIS_TEST_URL")
	if redisURL == "" {
		t.Skip("REDIS_TEST_URL not set, skipping Redis integration test")
	}

	limiter := NewRedisRateLimiter(redisURL, rate.Limit(5), 10)
	if limiter == nil {
		t.Skip("Redis not available, skipping test")
	}
	defer limiter.Close()

	ctx := context.Background()
	client := limiter.GetRedisClient()
	client.Del(ctx, "lock:agent:concurrent-test")

	const numGoroutines = 50
	results := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			locked, _ := limiter.TryLockAgent(ctx, "concurrent-test",
				"call-"+string(rune('A'+id%26)))
			results <- locked
		}(i)
	}

	successCount := 0
	for i := 0; i < numGoroutines; i++ {
		if <-results {
			successCount++
		}
	}

	// 仅 1 个 goroutine 应成功获取锁
	if successCount != 1 {
		t.Errorf("Expected exactly 1 lock acquisition, got %d", successCount)
	}

	// 清理
	client.Del(ctx, "lock:agent:concurrent-test")
}

// TestRedisRateLimiter_TTL 测试锁 TTL 自动过期
func TestRedisRateLimiter_TTL(t *testing.T) {
	redisURL := os.Getenv("REDIS_TEST_URL")
	if redisURL == "" {
		t.Skip("REDIS_TEST_URL not set, skipping Redis integration test")
	}

	// 设置短 TTL 测试
	os.Setenv("AGENT_LOCK_TTL", "1")
	defer os.Unsetenv("AGENT_LOCK_TTL")

	limiter := NewRedisRateLimiter(redisURL, rate.Limit(5), 10)
	if limiter == nil {
		t.Skip("Redis not available, skipping test")
	}
	defer limiter.Close()

	ctx := context.Background()
	client := limiter.GetRedisClient()
	client.Del(ctx, "lock:agent:ttl-test")

	// 获取锁
	locked, err := limiter.TryLockAgent(ctx, "ttl-test", "call-1")
	if err != nil || !locked {
		t.Fatal("Failed to acquire lock")
	}

	// 等待 TTL 过期
	time.Sleep(2 * time.Second)

	// 重新获取应该成功（旧锁已过期）
	locked2, err := limiter.TryLockAgent(ctx, "ttl-test", "call-2")
	if err != nil {
		t.Fatalf("TryLockAgent failed: %v", err)
	}
	if !locked2 {
		t.Error("Expected lock re-acquisition after TTL expiration")
	}

	// 清理
	client.Del(ctx, "lock:agent:ttl-test")
}
