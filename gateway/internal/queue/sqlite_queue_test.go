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

func TestQueue_AdminOperations(t *testing.T) {
	dbPath := t.TempDir() + "/test_admin.db"
	qm, err := NewQueueManager(dbPath, "http://localhost:8081/aa/settle", "secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer qm.Close()

	// 1. 插入测试数据
	// 任务 1: success, amount = 10000, bps = 200 (fee = 200)
	_, err = qm.db.Exec(`
		INSERT INTO settle_tasks (lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at, accumulated_amount, platform_bps)
		VALUES ('lock-1', 'proof-1', 'owner-1', 'escrow-1', 'success', 0, 1000, 10000, 200)
	`)
	if err != nil {
		t.Fatalf("Failed to insert task 1: %v", err)
	}

	// 任务 2: success, amount = 5000, bps = 150 (fee = 75)
	_, err = qm.db.Exec(`
		INSERT INTO settle_tasks (lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at, accumulated_amount, platform_bps)
		VALUES ('lock-2', 'proof-2', 'owner-2', 'escrow-2', 'success', 0, 1001, 5000, 150)
	`)
	if err != nil {
		t.Fatalf("Failed to insert task 2: %v", err)
	}

	// 任务 3: pending
	_, err = qm.db.Exec(`
		INSERT INTO settle_tasks (lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at, accumulated_amount, platform_bps)
		VALUES ('lock-3', 'proof-3', 'owner-3', 'escrow-3', 'pending', 0, 1002, 0, 0)
	`)
	if err != nil {
		t.Fatalf("Failed to insert task 3: %v", err)
	}

	// 任务 4: failed
	_, err = qm.db.Exec(`
		INSERT INTO settle_tasks (lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at, accumulated_amount, platform_bps)
		VALUES ('lock-4', 'proof-4', 'owner-4', 'escrow-4', 'failed', 4, 1003, 0, 0)
	`)
	if err != nil {
		t.Fatalf("Failed to insert task 4: %v", err)
	}

	// Stripe 会话记录
	_, err = qm.db.Exec(`
		INSERT INTO consumed_stripe_sessions (session_id, status)
		VALUES ('session-1', 'consumed'), ('session-2', 'consumed'), ('session-3', 'consumed')
	`)
	if err != nil {
		t.Fatalf("Failed to insert stripe sessions: %v", err)
	}

	// 2. 测试 GetAdminStats
	stats, err := qm.GetAdminStats()
	if err != nil {
		t.Fatalf("GetAdminStats failed: %v", err)
	}

	if val, ok := stats["total_settled_usdc"].(float64); !ok || val != 15000.0 {
		t.Errorf("Expected total_settled_usdc 15000.0, got %v", stats["total_settled_usdc"])
	}
	if val, ok := stats["total_platform_fees_usdc"].(float64); !ok || val != 275.0 {
		t.Errorf("Expected total_platform_fees_usdc 275.0, got %v", stats["total_platform_fees_usdc"])
	}
	if val, ok := stats["total_stripe_sessions"].(int64); !ok || val != 3 {
		t.Errorf("Expected total_stripe_sessions 3, got %v", stats["total_stripe_sessions"])
	}
	if val, ok := stats["success_tasks"].(int64); !ok || val != 2 {
		t.Errorf("Expected success_tasks 2, got %v", stats["success_tasks"])
	}
	if val, ok := stats["pending_tasks"].(int64); !ok || val != 1 {
		t.Errorf("Expected pending_tasks 1, got %v", stats["pending_tasks"])
	}
	if val, ok := stats["failed_tasks"].(int64); !ok || val != 1 {
		t.Errorf("Expected failed_tasks 1, got %v", stats["failed_tasks"])
	}

	// 3. 测试 GetAllTasks
	tasks, err := qm.GetAllTasks()
	if err != nil {
		t.Fatalf("GetAllTasks failed: %v", err)
	}
	if len(tasks) != 4 {
		t.Fatalf("Expected 4 tasks, got %d", len(tasks))
	}
	// 验证按 id DESC 排序，即最新的（排在最后的 lock-4）应该在最前面
	if tasks[0].LockID != "lock-4" {
		t.Errorf("Expected tasks[0].LockID 'lock-4', got %q", tasks[0].LockID)
	}
	if tasks[0].Status != "failed" {
		t.Errorf("Expected tasks[0].Status 'failed', got %q", tasks[0].Status)
	}

	// 4. 测试 ManualRetryTask
	err = qm.ManualRetryTask("lock-4")
	if err != nil {
		t.Fatalf("ManualRetryTask failed: %v", err)
	}

	var status string
	var retryCount int
	var nextRetryAt int64
	err = qm.db.QueryRow("SELECT status, retry_count, next_retry_at FROM settle_tasks WHERE lock_id = 'lock-4'").Scan(&status, &retryCount, &nextRetryAt)
	if err != nil {
		t.Fatalf("Failed to query updated task 4: %v", err)
	}

	if status != "pending" {
		t.Errorf("Expected status 'pending', got %q", status)
	}
	if retryCount != 0 {
		t.Errorf("Expected retryCount 0, got %d", retryCount)
	}
	if time.Now().Unix()-nextRetryAt > 5 {
		t.Errorf("Expected nextRetryAt to be close to now, got %d", nextRetryAt)
	}

	// 5. 测试 ClearStripeSessions
	err = qm.ClearStripeSessions()
	if err != nil {
		t.Fatalf("ClearStripeSessions failed: %v", err)
	}

	var stripeCount int
	err = qm.db.QueryRow("SELECT COUNT(*) FROM consumed_stripe_sessions").Scan(&stripeCount)
	if err != nil {
		t.Fatalf("Failed to query stripe sessions count after clear: %v", err)
	}
	if stripeCount != 0 {
		t.Errorf("Expected stripe sessions count 0, got %d", stripeCount)
	}
}
