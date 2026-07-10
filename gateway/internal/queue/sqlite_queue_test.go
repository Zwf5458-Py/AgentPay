package queue

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueue_EnqueueAndGetPendingTasks(t *testing.T) {
	dbPath := t.TempDir() + "/test_queue.db"
	qm, err := NewQueueManager(dbPath, "http://localhost:8081/aa/settle", "secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer qm.Close()

	// 1. 测试通道结算任务入队
	taskDetails := &SettleTask{
		ChannelID:         "0xChannel123",
		HoldAmount:        50000,
		Nonce:             12,
		Expiration:        uint64(time.Now().Unix() + 3600),
		Signature:         "0xSigExample",
		AccumulatedAmount: 3000,
		ModelCost:         1000,
		ServiceFee:        1997,
		ModelProvider:     "0xModelProviderAddr",
		Treasury:          "0xTreasuryAddr",
		PlatformBps:       10,
		AgentID:           42,
	}

	err = qm.Enqueue("0xChannel123:12", "mock-proof-1", "0xAgentOwner", "0xEscrow", taskDetails)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// 2. 测试遗留结算任务入队 (taskDetails 为 nil)
	err = qm.Enqueue("lock-456", "mock-proof-2", "0xAgentOwner", "0xEscrow", nil)
	if err != nil {
		t.Fatalf("Enqueue legacy failed: %v", err)
	}

	// 3. 获取待处理任务并验证
	tasks, err := qm.getPendingTasks()
	if err != nil {
		t.Fatalf("getPendingTasks failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("Expected 2 pending tasks, got %d", len(tasks))
	}

	// 验证通道任务
	var chanTask SettleTask
	var legacyTask SettleTask
	if tasks[0].ChannelID != "" {
		chanTask = tasks[0]
		legacyTask = tasks[1]
	} else {
		chanTask = tasks[1]
		legacyTask = tasks[0]
	}

	if chanTask.ChannelID != "0xChannel123" {
		t.Errorf("Expected ChannelID '0xChannel123', got %q", chanTask.ChannelID)
	}
	if chanTask.HoldAmount != 50000 {
		t.Errorf("Expected HoldAmount 50000, got %d", chanTask.HoldAmount)
	}
	if chanTask.Nonce != 12 {
		t.Errorf("Expected Nonce 12, got %d", chanTask.Nonce)
	}
	if chanTask.Signature != "0xSigExample" {
		t.Errorf("Expected Signature '0xSigExample', got %q", chanTask.Signature)
	}
	if chanTask.AccumulatedAmount != 3000 {
		t.Errorf("Expected AccumulatedAmount 3000, got %d", chanTask.AccumulatedAmount)
	}
	if chanTask.ModelCost != 1000 {
		t.Errorf("Expected ModelCost 1000, got %d", chanTask.ModelCost)
	}
	if chanTask.ServiceFee != 1997 {
		t.Errorf("Expected ServiceFee 1997, got %d", chanTask.ServiceFee)
	}
	if chanTask.ModelProvider != "0xModelProviderAddr" {
		t.Errorf("Expected ModelProvider '0xModelProviderAddr', got %q", chanTask.ModelProvider)
	}
	if chanTask.Treasury != "0xTreasuryAddr" {
		t.Errorf("Expected Treasury '0xTreasuryAddr', got %q", chanTask.Treasury)
	}
	if chanTask.PlatformBps != 10 {
		t.Errorf("Expected PlatformBps 10, got %d", chanTask.PlatformBps)
	}
	if chanTask.AgentID != 42 {
		t.Errorf("Expected AgentID 42, got %d", chanTask.AgentID)
	}

	// 验证遗留任务的默认/空值
	if legacyTask.ChannelID != "" {
		t.Errorf("Expected empty ChannelID for legacy task, got %q", legacyTask.ChannelID)
	}
	if legacyTask.HoldAmount != 0 {
		t.Errorf("Expected HoldAmount 0 for legacy task, got %d", legacyTask.HoldAmount)
	}
	if legacyTask.Nonce != 0 {
		t.Errorf("Expected Nonce 0 for legacy task, got %d", legacyTask.Nonce)
	}
	if legacyTask.Signature != "" {
		t.Errorf("Expected empty Signature for legacy task, got %q", legacyTask.Signature)
	}
}

func TestQueue_ProcessSingleTask(t *testing.T) {
	var lastURL string
	var lastBody map[string]interface{}

	bridgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastURL = r.URL.Path
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &lastBody)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"txHash":"0xTxMock"}`))
	}))
	defer bridgeServer.Close()

	dbPath := t.TempDir() + "/test_process.db"
	qm, err := NewQueueManager(dbPath, bridgeServer.URL+"/aa/settle", "secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer qm.Close()

	// 1. 测试通道结算任务（应 POST 至 /aa/split-settle）
	chanTask := SettleTask{
		LockID:            "0xChan789:5",
		Proof:             "proof-split",
		AgentOwner:        "0xAgentOwner",
		EscrowAddress:     "0xEscrow",
		ChannelID:         "0xChan789",
		HoldAmount:        30000,
		Nonce:             5,
		Expiration:        999999,
		Signature:         "0xSig789",
		AccumulatedAmount: 5000,
		ModelCost:         2500,
		ServiceFee:        2495,
		ModelProvider:     "0xModelProv",
		Treasury:          "0xTreas",
		PlatformBps:       10,
		AgentID:           888,
	}

	qm.processSingleTask(context.Background(), chanTask)

	if lastURL != "/aa/split-settle" {
		t.Errorf("Expected POST to /aa/split-settle for channel task, got %q", lastURL)
	}
	if lastBody["channelId"] != "0xChan789" {
		t.Errorf("Expected channelId '0xChan789' in body, got %v", lastBody["channelId"])
	}
	if lastBody["accumulatedAmount"] != "5000" {
		t.Errorf("Expected accumulatedAmount '5000', got %v", lastBody["accumulatedAmount"])
	}
	if lastBody["modelCost"] != "2500" {
		t.Errorf("Expected modelCost '2500', got %v", lastBody["modelCost"])
	}
	if lastBody["serviceFee"] != "2495" {
		t.Errorf("Expected serviceFee '2495', got %v", lastBody["serviceFee"])
	}
	if lastBody["modelProvider"] != "0xModelProv" {
		t.Errorf("Expected modelProvider '0xModelProv', got %v", lastBody["modelProvider"])
	}
	if lastBody["treasury"] != "0xTreas" {
		t.Errorf("Expected treasury '0xTreas', got %v", lastBody["treasury"])
	}
	if lastBody["platformBps"] != float64(10) {
		t.Errorf("Expected platformBps 10, got %v", lastBody["platformBps"])
	}
	if lastBody["agentId"] != float64(888) {
		t.Errorf("Expected agentId 888, got %v", lastBody["agentId"])
	}

	// 2. 测试遗留结算任务（应 POST 至 /aa/settle）
	legacyTask := SettleTask{
		LockID:        "lock-456",
		Proof:         "proof-legacy",
		AgentOwner:    "0xAgentOwner",
		EscrowAddress: "0xEscrow",
	}

	qm.processSingleTask(context.Background(), legacyTask)

	if lastURL != "/aa/settle" {
		t.Errorf("Expected POST to /aa/settle for legacy task, got %q", lastURL)
	}
	if lastBody["lockId"] != "lock-456" {
		t.Errorf("Expected lockId 'lock-456' in body, got %v", lastBody["lockId"])
	}
}
