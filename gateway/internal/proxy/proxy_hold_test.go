package proxy_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"gateway/internal/middleware"
	"gateway/internal/proxy"
	"gateway/internal/queue"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestProxy_SettleReceipt(t *testing.T) {
	// 1. 设置环境变量 GATEWAY_PRIVATE_KEY
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}
	privKeyHex := hexutil.Encode(crypto.FromECDSA(privKey))
	os.Setenv("GATEWAY_PRIVATE_KEY", privKeyHex)
	defer os.Unsetenv("GATEWAY_PRIVATE_KEY")

	// 2. 模拟下游 Agent (Eliza) 服务
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Agent-Proof", "MockProofForSettleReceipt")
		w.Header().Set("X-Agent-Cost", "12000") // 传递实际花费
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":"agent replied"}`))
	}))
	defer agentServer.Close()

	// 3. 模拟 AA Bridge 服务
	bridgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	}))
	defer bridgeServer.Close()

	// 4. 创建临时的 QueueManager
	dbPath := t.TempDir() + "/test_proxy_settle.db"
	queueMgr, err := queue.NewQueueManager(dbPath, bridgeServer.URL+"/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer queueMgr.Close()

	// 5. 初始化 Gateway 反向代理
	gatewayProxy, err := proxy.NewReverseProxy(agentServer.URL, bridgeServer.URL+"/aa/settle", "test-secret", queueMgr)
	if err != nil {
		t.Fatalf("Failed to create reverse proxy: %v", err)
	}

	// 6. 将中间件和代理组合成 Router
	handler := middleware.X402Middleware(gatewayProxy)

	// 7. 发送预授权请求
	req := httptest.NewRequest("POST", "/agent/execute", nil)
	futureExp := time.Now().Unix() + 3600
	futureExpStr := strconv.FormatInt(futureExp, 10)

	// Authorization: Bearer <channelId>:<holdAmount>:<nonce>:<expiration>:<sig>
	req.Header.Set("Authorization", "Bearer 0xChannel999:50000:789:"+futureExpStr+":0xSigabc")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// 8. 验证响应状态
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	// 9. 验证 Header 中的 X-402-Settle-Receipt
	receipt := rr.Header().Get("X-402-Settle-Receipt")
	if receipt == "" {
		t.Fatal("Expected X-402-Settle-Receipt header, but got none")
	}

	// 格式应为：<channelId>:<holdAmount>:<actualCost>:<nonce>:<sig>
	parts := strings.Split(receipt, ":")
	if len(parts) != 5 {
		t.Fatalf("Expected 5 parts in SettleReceipt header, got %d: %q", len(parts), receipt)
	}

	channelID := parts[0]
	holdAmount := parts[1]
	actualCost := parts[2]
	nonce := parts[3]
	sig := parts[4]

	if channelID != "0xChannel999" {
		t.Errorf("Expected channelId '0xChannel999', got %q", channelID)
	}
	if holdAmount != "50000" {
		t.Errorf("Expected holdAmount '50000', got %q", holdAmount)
	}
	if actualCost != "12000" {
		t.Errorf("Expected actualCost '12000' (from X-Agent-Cost), got %q", actualCost)
	}
	if nonce != "789" {
		t.Errorf("Expected nonce '789', got %q", nonce)
	}

	// 10. 验证签名有效性
	expectedMsg := "0xChannel999:50000:12000:789"
	expectedMsgHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(expectedMsg), expectedMsg)))

	sigBytes, err := hexutil.Decode(sig)
	if err != nil {
		t.Fatalf("Failed to decode signature hex %q: %v", sig, err)
	}

	if len(sigBytes) != 65 {
		t.Fatalf("Expected signature length to be 65, got %d", len(sigBytes))
	}
	// 将 V 还原为 0 或 1，以便 crypto.SigToPub 可以校验
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	pubKey, err := crypto.SigToPub(expectedMsgHash.Bytes(), sigBytes)
	if err != nil {
		t.Fatalf("Failed to recover public key from signature: %v", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	expectedAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	if recoveredAddr != expectedAddr {
		t.Errorf("Signature verification failed. Recovered address %s, expected %s", recoveredAddr.Hex(), expectedAddr.Hex())
	}
}

func TestProxy_SettleReceipt_MockKey(t *testing.T) {
	// 1. 确保 GATEWAY_PRIVATE_KEY 环境变量未设置
	os.Unsetenv("GATEWAY_PRIVATE_KEY")

	// 2. 模拟下游 Agent (Eliza) 服务
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Agent-Proof", "MockProofForSettleReceiptMockKey")
		w.Header().Set("X-Agent-Cost", "3500")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output":"agent replied"}`))
	}))
	defer agentServer.Close()

	// 3. 模拟 AA Bridge 服务
	bridgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	}))
	defer bridgeServer.Close()

	// 4. 创建临时的 QueueManager
	dbPath := t.TempDir() + "/test_proxy_settle_mockkey.db"
	queueMgr, err := queue.NewQueueManager(dbPath, bridgeServer.URL+"/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer queueMgr.Close()

	// 5. 初始化 Gateway 反向代理
	gatewayProxy, err := proxy.NewReverseProxy(agentServer.URL, bridgeServer.URL+"/aa/settle", "test-secret", queueMgr)
	if err != nil {
		t.Fatalf("Failed to create reverse proxy: %v", err)
	}

	// 6. 将中间件和代理组合成 Router
	handler := middleware.X402Middleware(gatewayProxy)

	// 7. 发送预授权请求
	req := httptest.NewRequest("POST", "/agent/execute", nil)
	futureExp := time.Now().Unix() + 3600
	futureExpStr := strconv.FormatInt(futureExp, 10)

	// Authorization: Bearer <channelId>:<holdAmount>:<nonce>:<expiration>:<sig>
	req.Header.Set("Authorization", "Bearer 0xChannelMock:20000:999:"+futureExpStr+":0xSigabc")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// 8. 验证响应状态
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	// 9. 验证 Header 中的 X-402-Settle-Receipt
	receipt := rr.Header().Get("X-402-Settle-Receipt")
	if receipt == "" {
		t.Fatal("Expected X-402-Settle-Receipt header, but got none")
	}

	parts := strings.Split(receipt, ":")
	if len(parts) != 5 {
		t.Fatalf("Expected 5 parts in SettleReceipt header, got %d: %q", len(parts), receipt)
	}

	channelID := parts[0]
	holdAmount := parts[1]
	actualCost := parts[2]
	nonce := parts[3]
	sig := parts[4]

	if channelID != "0xChannelMock" {
		t.Errorf("Expected channelId '0xChannelMock', got %q", channelID)
	}
	if holdAmount != "20000" {
		t.Errorf("Expected holdAmount '20000', got %q", holdAmount)
	}
	if actualCost != "3500" {
		t.Errorf("Expected actualCost '3500', got %q", actualCost)
	}
	if nonce != "999" {
		t.Errorf("Expected nonce '999', got %q", nonce)
	}

	// 10. 验证签名是否可由以太坊校验算法正确 recover 出任何地址，从而断言格式合法
	expectedMsg := "0xChannelMock:20000:3500:999"
	expectedMsgHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(expectedMsg), expectedMsg)))

	sigBytes, err := hexutil.Decode(sig)
	if err != nil {
		t.Fatalf("Failed to decode signature hex %q: %v", sig, err)
	}

	if len(sigBytes) != 65 {
		t.Fatalf("Expected signature length to be 65, got %d", len(sigBytes))
	}
	if sigBytes[64] >= 27 {
		sigBytes[64] -= 27
	}

	pubKey, err := crypto.SigToPub(expectedMsgHash.Bytes(), sigBytes)
	if err != nil {
		t.Errorf("Failed to recover public key from mock-generated signature: %v", err)
	}
	if pubKey == nil {
		t.Error("Recovered public key is nil")
	}
}

