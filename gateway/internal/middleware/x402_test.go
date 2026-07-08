package middleware_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gateway/internal/middleware"
	"gateway/internal/proxy"
	"gateway/internal/queue"

	"golang.org/x/time/rate"
)

func TestX402Middleware_NoToken(t *testing.T) {
	// 设置环境变量以便测试
	escrowAddr := "0xTestEscrowAddress123"
	os.Setenv("ESCROW_ADDRESS", escrowAddr)
	defer os.Unsetenv("ESCROW_ADDRESS")

	handler := middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/agent/execute", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// 验证 402 状态码
	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status code %d, got %d", http.StatusPaymentRequired, rr.Code)
	}

	// 验证 Header 注入
	headers := []struct {
		key   string
		value string
	}{
		{"X-402-Price", "1000"},
		{"X-402-Currency", "USDC"},
		{"X-402-Chain", "base-sepolia"},
		{"X-402-Payment-Address", escrowAddr},
		{"X-402-Version", "1"},
		{"Content-Type", "application/json"},
	}

	for _, h := range headers {
		got := rr.Header().Get(h.key)
		if got != h.value {
			t.Errorf("Header %s: expected %q, got %q", h.key, h.value, got)
		}
	}

	// 验证 Response Body
	var resp middleware.ErrorResponse
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	if err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}

	if resp.Error != "payment_required" {
		t.Errorf("Expected error field 'payment_required', got %q", resp.Error)
	}
	if !strings.Contains(resp.Message, "micropayment required") {
		t.Errorf("Expected message to contain 'micropayment required', got %q", resp.Message)
	}
}

func TestX402Middleware_WithToken(t *testing.T) {
	var capturedToken string
	var capturedLockID string

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedToken = middleware.GetToken(r.Context())
		capturedLockID = middleware.GetLockID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.X402Middleware(nextHandler)

	// 用例 2a: 携带普通 Bearer mock-token
	req := httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer mock-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if capturedToken != "mock-token" {
		t.Errorf("Expected token 'mock-token', got %q", capturedToken)
	}
	if capturedLockID != "mock-token" {
		t.Errorf("Expected lockID 'mock-token' (fallback to token), got %q", capturedLockID)
	}

	// 用例 2b: 携带 lockId:token 格式的 Bearer token
	req = httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer myLockID:myActualToken")
	rr = httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if capturedToken != "myLockID:myActualToken" {
		t.Errorf("Expected token 'myLockID:myActualToken', got %q", capturedToken)
	}
	if capturedLockID != "myLockID" {
		t.Errorf("Expected lockID 'myLockID', got %q", capturedLockID)
	}

	// 用例 2c: 携带 X-Payment-Lock-Id 请求头
	req = httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer someToken")
	req.Header.Set("X-Payment-Lock-Id", "customLockID")
	rr = httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if capturedToken != "someToken" {
		t.Errorf("Expected token 'someToken', got %q", capturedToken)
	}
	if capturedLockID != "customLockID" {
		t.Errorf("Expected lockID 'customLockID', got %q", capturedLockID)
	}
}

func TestProxyReverse_AsyncSettle(t *testing.T) {
	// 1. 模拟下游 Agent 服务
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/execute" {
			t.Errorf("Agent received path %q, expected /agent/execute", r.URL.Path)
		}
		w.Header().Set("X-Agent-Proof", "MockProofBase64String")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":"mocked output"}`))
	}))
	defer agentServer.Close()

	// 2. 模拟 AA Bridge 服务，捕获异步结算请求
	var receivedSettleBody map[string]string
	var receivedSecret string
	var mu sync.Mutex
	var settleCount int
	var wg sync.WaitGroup
	wg.Add(1)

	bridgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		settleCount++

		if r.URL.Path != "/aa/settle" {
			t.Errorf("Bridge received path %q, expected /aa/settle", r.URL.Path)
		}

		receivedSecret = r.Header.Get("X-Internal-Secret")
		if receivedSecret != "test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read bridge request body: %v", err)
		}

		err = json.Unmarshal(bodyBytes, &receivedSettleBody)
		if err != nil {
			t.Errorf("Failed to parse bridge request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"txHash":"0x7777"}`))

		if settleCount == 1 {
			wg.Done()
		}
	}))
	defer bridgeServer.Close()

	// 创建临时的 QueueManager
	dbPath := t.TempDir() + "/test_async_settle.db"
	queueMgr, err := queue.NewQueueManager(dbPath, bridgeServer.URL+"/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer queueMgr.Close()

	// 启动 Worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queueMgr.StartWorker(ctx)

	// 3. 初始化 Gateway 反向代理
	gatewayProxy, err := proxy.NewReverseProxy(agentServer.URL, bridgeServer.URL+"/aa/settle", "test-secret", queueMgr)
	if err != nil {
		t.Fatalf("Failed to create reverse proxy: %v", err)
	}

	// 4. 将中间件和代理组合成 Router 模拟网关
	handler := middleware.X402Middleware(gatewayProxy)

	// 5. 客户端发起正常支付放行的请求，携带 X-Payment-Lock-Id
	req := httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer mock-session-token")
	req.Header.Set("X-Payment-Lock-Id", "lock-999")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// 6. 验证客户端能够即时收到响应，并且不被阻塞
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// 7. 等待异步 worker 执行结算请求 (带超时保护)
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		// 正常接收
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for async settle worker to trigger AA Bridge")
	}

	// 8. 验证结算请求体中的字段
	mu.Lock()
	defer mu.Unlock()
	if receivedSettleBody == nil {
		t.Fatal("Bridge did not receive settle request")
	}

	if receivedSettleBody["lockId"] != "lock-999" {
		t.Errorf("Expected lockId 'lock-999', got %q", receivedSettleBody["lockId"])
	}
	if receivedSettleBody["proof"] != "MockProofBase64String" {
		t.Errorf("Expected proof 'MockProofBase64String', got %q", receivedSettleBody["proof"])
	}
	if receivedSecret != "test-secret" {
		t.Errorf("Expected secret 'test-secret', got %q", receivedSecret)
	}
}

func TestProxyReverse_BridgeOutageSelfHealing(t *testing.T) {
	// 1. 模拟下游 Agent 服务
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Agent-Proof", "MockProofSelfHealing")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":"mocked output"}`))
	}))
	defer agentServer.Close()

	// 2. 模拟可动态调整行为的 AA Bridge 服务
	var mu sync.Mutex
	shouldFail := true
	settleSuccessReceived := false
	var receivedSecret string

	bridgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		receivedSecret = r.Header.Get("X-Internal-Secret")

		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"bridge temp offline"}`))
			return
		}

		settleSuccessReceived = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"txHash":"0x8888"}`))
	}))
	defer bridgeServer.Close()

	// 3. 创建临时的 QueueManager
	dbPath := t.TempDir() + "/test_self_healing.db"
	queueMgr, err := queue.NewQueueManager(dbPath, bridgeServer.URL+"/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer queueMgr.Close()

	// 启动 Worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queueMgr.StartWorker(ctx)

	// 4. 初始化 Gateway 反向代理
	gatewayProxy, err := proxy.NewReverseProxy(agentServer.URL, bridgeServer.URL+"/aa/settle", "test-secret", queueMgr)
	if err != nil {
		t.Fatalf("Failed to create reverse proxy: %v", err)
	}

	handler := middleware.X402Middleware(gatewayProxy)

	// 5. 客户端发起推理请求
	req := httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer mock-session-token")
	req.Header.Set("X-Payment-Lock-Id", "lock-heal-123")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// 验证网关能立刻正常向客户端写回推理数据（HTTP 200）
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// 6. 稍微等一等以确保 Worker 跑了一轮（第一次请求），然后我们去 SQLite 查任务状态，断言为 'pending'
	time.Sleep(2500 * time.Millisecond)

	// 打开 SQLite 检查状态
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	defer db.Close()

	var status string
	var retryCount int
	err = db.QueryRow("SELECT status, retry_count FROM settle_tasks WHERE lock_id = ?", "lock-heal-123").Scan(&status, &retryCount)
	if err != nil {
		t.Fatalf("Failed to query task status: %v", err)
	}

	if status != "pending" {
		t.Errorf("Expected task status 'pending', got %q", status)
	}
	if retryCount < 1 {
		t.Errorf("Expected retry_count to be at least 1, got %d", retryCount)
	}

	// 7. 将 Mock Bridge 的行为恢复为正常，等待 SQLite Worker 轮询重试自愈
	mu.Lock()
	shouldFail = false
	mu.Unlock()

	// 刚才 retryCount 为 1，退避延迟时间是 2^1 = 2s。
	// 这里我们直接等待退避延迟过去。为稳妥起见，我们直接等待 4.5 秒。
	time.Sleep(4500 * time.Millisecond)

	// 再次查询 SQLite，断言任务状态更新为 'success'
	err = db.QueryRow("SELECT status FROM settle_tasks WHERE lock_id = ?", "lock-heal-123").Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query task status again: %v", err)
	}

	if status != "success" {
		t.Errorf("Expected task status to heal to 'success', got %q", status)
	}

	mu.Lock()
	successReceived := settleSuccessReceived
	mu.Unlock()

	if !successReceived {
		t.Error("Mock Bridge did not receive a successful settle request")
	}

	if receivedSecret != "test-secret" {
		t.Errorf("Expected secret 'test-secret', got %q", receivedSecret)
	}
}

func TestRateLimitMiddleware_LimitExceeded(t *testing.T) {
	// 配置 IPRateLimiter 限制速率为 2/s，桶大小为 3
	limiter := middleware.NewIPRateLimiter(rate.Limit(2), 3)

	// 创建一个路由器处理器：限流中间件包裹 X402 中间件
	// 前 3 次请求由于没有携带 Authorization 头，但能通过限流器，所以会被 X402 拦截并返回 402
	// 第 4 次和第 5 次请求由于超出速率限制，会被限流中间件拦截并返回 429
	handler := middleware.RateLimitMiddleware(limiter)(middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest("POST", "/agent/execute", nil)
		req.RemoteAddr = "192.168.1.100:12345" // 统一客户端 IP
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if i <= 3 {
			// 前 3 次请求返回 HTTP 402
			if rr.Code != http.StatusPaymentRequired {
				t.Errorf("Request %d: expected status code %d, got %d", i, http.StatusPaymentRequired, rr.Code)
			}
		} else {
			// 第 4 次和第 5 次请求返回 HTTP 429
			if rr.Code != http.StatusTooManyRequests {
				t.Errorf("Request %d: expected status code %d, got %d", i, http.StatusTooManyRequests, rr.Code)
			}

			// 验证 Response Body 匹配 rate_limit_exceeded
			var resp middleware.ErrorResponse
			err := json.Unmarshal(rr.Body.Bytes(), &resp)
			if err != nil {
				t.Fatalf("Request %d: failed to unmarshal response body: %v", i, err)
			}

			if resp.Error != "rate_limit_exceeded" {
				t.Errorf("Request %d: expected error field 'rate_limit_exceeded', got %q", i, resp.Error)
			}
			if resp.Message != "too many requests" {
				t.Errorf("Request %d: expected message 'too many requests', got %q", i, resp.Message)
			}
		}
	}
}

func TestRateLimitLimiter_CleanupTTL(t *testing.T) {
	// 配置限流速率为 2/s，桶大小为 3
	limiter := middleware.NewIPRateLimiter(rate.Limit(2), 3)

	ip := "192.168.1.50"

	// 写入一个 IP 记录
	limiter.GetLimiter(ip)

	if limiter.GetIPsCount() != 1 {
		t.Errorf("Expected IP count to be 1, got %d", limiter.GetIPsCount())
	}

	// 模拟写入一个 IP 记录，设置其 lastSeen 为 10 分钟前
	limiter.SetLimiterLastSeen(ip, time.Now().Add(-10*time.Minute))

	// 执行清理，清除过期时间大于 5 分钟的 IP
	limiter.Cleanup(5 * time.Minute)

	// 断言 map 确实删除了该超期 IP
	if limiter.GetIPsCount() != 0 {
		t.Errorf("Expected IP count to be 0 after cleanup, got %d", limiter.GetIPsCount())
	}
}


