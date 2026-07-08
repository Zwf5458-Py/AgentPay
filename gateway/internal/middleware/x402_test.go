package middleware_test

import (
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
		// 校验请求
		if r.URL.Path != "/agent/execute" {
			t.Errorf("Agent received path %q, expected /agent/execute", r.URL.Path)
		}
		// 返回带有 X-Agent-Proof 的响应
		w.Header().Set("X-Agent-Proof", "MockProofBase64String")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":"mocked output"}`))
	}))
	defer agentServer.Close()

	// 2. 模拟 AA Bridge 服务，捕获异步结算请求
	var receivedSettleBody map[string]string
	var wg sync.WaitGroup
	wg.Add(1)

	bridgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		if r.URL.Path != "/aa/settle" {
			t.Errorf("Bridge received path %q, expected /aa/settle", r.URL.Path)
		}

		secret := r.Header.Get("X-Internal-Secret")
		if secret != "test-secret" {
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

		// 返回成功响应
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"txHash":"0x7777"}`))
	}))
	defer bridgeServer.Close()

	// 3. 初始化 Gateway 反向代理
	gatewayProxy, err := proxy.NewReverseProxy(agentServer.URL, bridgeServer.URL+"/aa/settle", "test-secret")
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

	// 7. 等待异步 goroutine 执行结算请求 (带超时保护)
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		// 正常接收
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for async settle goroutine to trigger AA Bridge")
	}

	// 8. 验证结算请求体中的字段
	if receivedSettleBody == nil {
		t.Fatal("Bridge did not receive settle request")
	}

	if receivedSettleBody["lockId"] != "lock-999" {
		t.Errorf("Expected lockId 'lock-999', got %q", receivedSettleBody["lockId"])
	}
	if receivedSettleBody["proof"] != "MockProofBase64String" {
		t.Errorf("Expected proof 'MockProofBase64String', got %q", receivedSettleBody["proof"])
	}
	// 验证网关有默认/环境变量中读取的 EOA
	if receivedSettleBody["agentOwner"] == "" {
		t.Error("Expected non-empty agentOwner EOA")
	}
	if receivedSettleBody["escrowAddress"] == "" {
		t.Error("Expected non-empty escrowAddress")
	}
}

func TestProxyReverse_SettleRetry(t *testing.T) {
	// 模拟下游 Agent 服务
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Agent-Proof", "MockProofForRetry")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":"mocked output"}`))
	}))
	defer agentServer.Close()

	// 模拟一个总是失败的 AA Bridge 服务 (返回 500)
	var mu sync.Mutex
	attempts := 0
	bridgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()

		secret := r.Header.Get("X-Internal-Secret")
		if secret != "test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"bridge internal error"}`))
	}))
	defer bridgeServer.Close()

	// 初始化 Gateway 反向代理，指向会失败 defects AA Bridge
	gatewayProxy, err := proxy.NewReverseProxy(agentServer.URL, bridgeServer.URL+"/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create reverse proxy: %v", err)
	}

	handler := middleware.X402Middleware(gatewayProxy)

	req := httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer mock-session-token")
	req.Header.Set("X-Payment-Lock-Id", "lock-fail-retry")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// 异步重试最长需要 2 秒（每次重试间隔 1 秒，共 3 次请求）
	// 我们等待 3.5 秒确保重试流程全部走完
	time.Sleep(3500 * time.Millisecond)

	mu.Lock()
	finalAttempts := attempts
	mu.Unlock()

	// 应该在失败后尝试了 3 次
	if finalAttempts != 3 {
		t.Errorf("Expected exactly 3 settle attempts, got %d", finalAttempts)
	}
}
