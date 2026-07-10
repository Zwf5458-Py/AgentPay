package middleware_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gateway/internal/middleware"
	"gateway/internal/proxy"
	"gateway/internal/queue"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
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
		{"X-402-Platform-Bps", "10"},
		{"X-402-Model-Provider", "0x90F79bf6EB2c4f870365E785982E1f101E93b906"},
		{"X-402-Payment-Methods", "crypto-channel,fiat-stripe"},
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

func TestX402Middleware_NoToken_EnvVars(t *testing.T) {
	// 设置环境变量以便测试
	escrowAddr := "0xTestEscrowAddress123"
	os.Setenv("ESCROW_ADDRESS", escrowAddr)
	os.Setenv("PLATFORM_BPS", "25")
	os.Setenv("MODEL_PROVIDER_ADDRESS", "0xProviderAddressABC")
	defer func() {
		os.Unsetenv("ESCROW_ADDRESS")
		os.Unsetenv("PLATFORM_BPS")
		os.Unsetenv("MODEL_PROVIDER_ADDRESS")
	}()

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
		{"X-402-Platform-Bps", "25"},
		{"X-402-Model-Provider", "0xProviderAddressABC"},
		{"X-402-Payment-Methods", "crypto-channel,fiat-stripe"},
		{"Content-Type", "application/json"},
	}

	for _, h := range headers {
		got := rr.Header().Get(h.key)
		if got != h.value {
			t.Errorf("Header %s: expected %q, got %q", h.key, h.value, got)
		}
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

func TestCORS_OPTIONS(t *testing.T) {
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Internal-Secret")
			w.Header().Set("Access-Control-Expose-Headers", "X-402-Payment-Address, X-402-Price, X-402-Payment-Type, X-Agent-Proof, X-402-Platform-Bps, X-402-Model-Provider, X-402-Payment-Methods, X-402-Hold-Amount, X-402-Settle-Receipt, X-402-Currency, X-402-Chain, X-402-Version")
			
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/agent/execute", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	expectedHeaders := map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "POST, GET, OPTIONS, PUT, DELETE",
		"Access-Control-Allow-Headers": "Content-Type, Authorization, X-Internal-Secret",
		"Access-Control-Expose-Headers": "X-402-Payment-Address, X-402-Price, X-402-Payment-Type, X-Agent-Proof, X-402-Platform-Bps, X-402-Model-Provider, X-402-Payment-Methods, X-402-Hold-Amount, X-402-Settle-Receipt, X-402-Currency, X-402-Chain, X-402-Version",
	}

	for key, expectedValue := range expectedHeaders {
		gotValue := rr.Header().Get(key)
		if gotValue != expectedValue {
			t.Errorf("Header %s: expected %q, got %q", key, expectedValue, gotValue)
		}
	}
}

func TestDebugTasks(t *testing.T) {
	dbPath := t.TempDir() + "/test_debug_tasks.db"
	queueMgr, err := queue.NewQueueManager(dbPath, "http://mock-bridge/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer queueMgr.Close()

	err = queueMgr.Enqueue("lock-123", "proof-abc", "owner-xyz", "escrow-123", nil)
	if err != nil {
		t.Fatalf("Failed to enqueue task: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tasks, err := queueMgr.GetLatestTasks(10)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tasks)
	})

	req := httptest.NewRequest("GET", "/debug/tasks", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	if contentType := rr.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
	}

	var tasks []map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &tasks)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	task := tasks[0]
	if task["lock_id"] != "lock-123" {
		t.Errorf("Expected lock_id 'lock-123', got %v", task["lock_id"])
	}
	if task["proof"] != "proof-abc" {
		t.Errorf("Expected proof 'proof-abc', got %v", task["proof"])
	}
	if task["agent_owner"] != "owner-xyz" {
		t.Errorf("Expected agent_owner 'owner-xyz', got %v", task["agent_owner"])
	}
	if task["escrow_address"] != "escrow-123" {
		t.Errorf("Expected escrow_address 'escrow-123', got %v", task["escrow_address"])
	}
	if task["status"] != "pending" {
		t.Errorf("Expected status 'pending', got %v", task["status"])
	}
	if task["retry_count"] == nil {
		t.Errorf("Expected retry_count to not be nil")
	}
	if task["created_at"] == nil {
		t.Errorf("Expected created_at to not be nil")
	}
}

func TestX402Middleware_HoldAmount(t *testing.T) {
	// 1. 测试不带 Auth 头时，必须返回 402 并携带 X-402-Payment-Type: channel 和 X-402-Hold-Amount: 50000
	handler := middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/agent/execute", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// 验证 402 状态码
	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status code %d, got %d", http.StatusPaymentRequired, rr.Code)
	}

	// 验证新增的 Headers
	paymentType := rr.Header().Get("X-402-Payment-Type")
	if paymentType != "channel" {
		t.Errorf("Expected X-402-Payment-Type to be 'channel', got %q", paymentType)
	}

	holdAmount := rr.Header().Get("X-402-Hold-Amount")
	if holdAmount != "50000" {
		t.Errorf("Expected X-402-Hold-Amount to be '50000', got %q", holdAmount)
	}

	// 2. 测试带上 EIP-712 Hold 格式 Auth 头时放行并正确注入 Context
	var capturedHoldAmount string
	var capturedChannelID string
	var capturedSignature string
	var capturedNonce string
	var capturedExpiration string

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHoldAmount = middleware.GetHoldAmount(r.Context())
		capturedChannelID = middleware.GetChannelID(r.Context())
		capturedSignature = middleware.GetSignature(r.Context())
		capturedNonce = middleware.GetNonce(r.Context())
		capturedExpiration = middleware.GetExpiration(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler = middleware.X402Middleware(nextHandler)

	futureExp := time.Now().Unix() + 3600
	futureExpStr := strconv.FormatInt(futureExp, 10)

	req = httptest.NewRequest("GET", "/agent/execute", nil)
	// Bearer <channelId>:<holdAmount>:<nonce>:<expiration>:<sig>
	req.Header.Set("Authorization", "Bearer 0xChannel123:50000:456:"+futureExpStr+":0xSigabc")
	rr = httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	if capturedHoldAmount != "50000" {
		t.Errorf("Expected captured hold amount '50000', got %q", capturedHoldAmount)
	}

	if capturedChannelID != "0xChannel123" {
		t.Errorf("Expected captured channel ID '0xChannel123', got %q", capturedChannelID)
	}

	if capturedSignature != "0xSigabc" {
		t.Errorf("Expected captured signature '0xSigabc', got %q", capturedSignature)
	}

	if capturedNonce != "456" {
		t.Errorf("Expected captured nonce '456', got %q", capturedNonce)
	}

	if capturedExpiration != futureExpStr {
		t.Errorf("Expected captured expiration %q, got %q", futureExpStr, capturedExpiration)
	}
}

func TestX402Middleware_InvalidFormat(t *testing.T) {
	handler := middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	invalidTokens := []string{
		"Bearer 0xChannel123:50000a:123456:1718999999:0xSig", // holdAmount 含字母
		"Bearer 0xChannel123:50000:123456b:1718999999:0xSig", // nonce 含字母
		"Bearer 0xChannel123:50000:123456:1718999999c:0xSig", // expiration 含字母
		"Bearer 0xChannel123::123456:1718999999:0xSig",       // holdAmount 为空
		"Bearer 0xChannel123:50000::1718999999:0xSig",       // nonce 为空
		"Bearer 0xChannel123:50000:123456::0xSig",           // expiration 为空
	}

	for _, token := range invalidTokens {
		req := httptest.NewRequest("GET", "/agent/execute", nil)
		req.Header.Set("Authorization", token)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusPaymentRequired {
			t.Errorf("For token %q, expected status %d, got %d", token, http.StatusPaymentRequired, rr.Code)
		}
	}
}

func TestX402Middleware_Expiration(t *testing.T) {
	handler := middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	now := time.Now().Unix()

	// 1. 过期 token (10秒前过期)
	expiredTime := now - 10
	expiredToken := "Bearer 0xChannel123:50000:123456:" + strconv.FormatInt(expiredTime, 10) + ":0xSig"
	req := httptest.NewRequest("GET", "/agent/execute", nil)
	req.Header.Set("Authorization", expiredToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status %d for expired token, got %d", http.StatusPaymentRequired, rr.Code)
	}

	// 2. 有效 token (1小时后过期)
	activeTime := now + 3600
	activeToken := "Bearer 0xChannel123:50000:123456:" + strconv.FormatInt(activeTime, 10) + ":0xSig"
	req = httptest.NewRequest("GET", "/agent/execute", nil)
	req.Header.Set("Authorization", activeToken)
	rr = httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d for active token, got %d", http.StatusOK, rr.Code)
	}
}

func TestX402Middleware_EIP712ValidSignature(t *testing.T) {
	// 准备测试私钥
	privKeyHex := "2222222222222222222222222222222222222222222222222222222222222222"
	privKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	// 导出地址为 0x1563915e194D8CfBA1943570603F7606A3115508
	clientAddr := crypto.PubkeyToAddress(privKey.PublicKey).Hex()
	os.Setenv("CLIENT_ADDRESS", clientAddr)
	defer os.Unsetenv("CLIENT_ADDRESS")

	escrowAddr := "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	os.Setenv("ESCROW_ADDRESS", escrowAddr)
	defer os.Unsetenv("ESCROW_ADDRESS")

	// 构造 typed data 字段
	channelID := "0x0000000000000000000000000000000000000000000000000000000000000888"
	holdAmountStr := "50000"
	nonceStr := "1"
	expirationStr := strconv.FormatInt(time.Now().Unix()+3600, 10)

	holdAmount, _ := new(big.Int).SetString(holdAmountStr, 10)
	nonce, _ := new(big.Int).SetString(nonceStr, 10)
	expiration, _ := new(big.Int).SetString(expirationStr, 10)

	// 计算 EIP-712 Hash
	domainSeparator := middleware.GetEIP712DomainSeparator(escrowAddr)
	messageHash := middleware.GetChannelHoldMessageHash(channelID, holdAmount, nonce, expiration)

	digestData := append([]byte("\x19\x01"), domainSeparator...)
	digestData = append(digestData, messageHash...)
	digest := crypto.Keccak256(digestData)

	// 签名
	sigBytes, err := crypto.Sign(digest, privKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}
	sigBytes[64] += 27 // 转为以太坊 V 格式
	sig := hexutil.Encode(sigBytes)

	// 构造 Token
	token := fmt.Sprintf("Bearer %s:%s:%s:%s:%s", channelID, holdAmountStr, nonceStr, expirationStr, sig)

	handler := middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/agent/execute", nil)
	req.Header.Set("Authorization", token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestX402Middleware_EIP712InvalidSignature(t *testing.T) {
	// 准备测试私钥
	privKeyHex := "2222222222222222222222222222222222222222222222222222222222222222"
	privKey, err := crypto.HexToECDSA(privKeyHex)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	// 故意把预期客户端地址设置为另一个地址
	os.Setenv("CLIENT_ADDRESS", "0x3c7CAd7D0fA28a1c86D2e48AFa87df2c21966C5a")
	defer os.Unsetenv("CLIENT_ADDRESS")

	escrowAddr := "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	os.Setenv("ESCROW_ADDRESS", escrowAddr)
	defer os.Unsetenv("ESCROW_ADDRESS")

	channelID := "0x0000000000000000000000000000000000000000000000000000000000000888"
	holdAmountStr := "50000"
	nonceStr := "1"
	expirationStr := strconv.FormatInt(time.Now().Unix()+3600, 10)

	holdAmount, _ := new(big.Int).SetString(holdAmountStr, 10)
	nonce, _ := new(big.Int).SetString(nonceStr, 10)
	expiration, _ := new(big.Int).SetString(expirationStr, 10)

	// 计算 EIP-712 Hash
	domainSeparator := middleware.GetEIP712DomainSeparator(escrowAddr)
	messageHash := middleware.GetChannelHoldMessageHash(channelID, holdAmount, nonce, expiration)

	digestData := append([]byte("\x19\x01"), domainSeparator...)
	digestData = append(digestData, messageHash...)
	digest := crypto.Keccak256(digestData)

	// 签名
	sigBytes, err := crypto.Sign(digest, privKey)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}
	sigBytes[64] += 27
	sig := hexutil.Encode(sigBytes)

	// 构造带非法签名的 Token (由于 CLIENT_ADDRESS 为 0x3c7CAd...，而我们使用 0x156391... 签的，因此应该被拒绝)
	token := fmt.Sprintf("Bearer %s:%s:%s:%s:%s", channelID, holdAmountStr, nonceStr, expirationStr, sig)

	handler := middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/agent/execute", nil)
	req.Header.Set("Authorization", token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status code %d for invalid signature, got %d", http.StatusPaymentRequired, rr.Code)
	}

	// 测试篡改字段 (holdAmount 被篡改为 60000，但签名依然是针对 50000 签的)
	tamperedToken := fmt.Sprintf("Bearer %s:60000:%s:%s:%s", channelID, nonceStr, expirationStr, sig)
	req2 := httptest.NewRequest("GET", "/agent/execute", nil)
	req2.Header.Set("Authorization", tamperedToken)
	rr2 := httptest.NewRecorder()

	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status code %d for tampered parameter, got %d", http.StatusPaymentRequired, rr2.Code)
	}
}

func TestX402Middleware_StripeValid(t *testing.T) {
	var capturedToken string
	var capturedLockID string
	var capturedPaymentMethod string
	var capturedStripeSessionID string

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedToken = middleware.GetToken(r.Context())
		capturedLockID = middleware.GetLockID(r.Context())
		capturedPaymentMethod = middleware.GetPaymentMethod(r.Context())
		capturedStripeSessionID = middleware.GetStripeSessionID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.X402Middleware(nextHandler)

	req := httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer stripe:cs_mock_test123")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if capturedToken != "stripe:cs_mock_test123" {
		t.Errorf("Expected token 'stripe:cs_mock_test123', got %q", capturedToken)
	}
	if capturedLockID != "cs_mock_test123" {
		t.Errorf("Expected lockID 'cs_mock_test123', got %q", capturedLockID)
	}
	if capturedPaymentMethod != "stripe" {
		t.Errorf("Expected paymentMethod 'stripe', got %q", capturedPaymentMethod)
	}
	if capturedStripeSessionID != "cs_mock_test123" {
		t.Errorf("Expected stripeSessionID 'cs_mock_test123', got %q", capturedStripeSessionID)
	}
}

func TestX402Middleware_StripeInvalid(t *testing.T) {
	handler := middleware.X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer stripe:cs_invalid_test123")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected status %d, got %d", http.StatusPaymentRequired, rr.Code)
	}
}




