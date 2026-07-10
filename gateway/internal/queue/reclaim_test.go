package queue

import (
	"os"
	"testing"
	"time"
)

func TestReclaimStuckTasks(t *testing.T) {
	dbFile := "./test_reclaim.db"
	defer os.Remove(dbFile)

	qm, err := NewQueueManager(dbFile, "http://localhost:3001/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer qm.Close()

	// 插入一个卡住的任务（next_retry_at 超时 6 分钟）
	stuckTime := time.Now().Add(-6 * time.Minute).Unix()
	qm.mu.Lock()
	_, err = qm.db.Exec(`
		INSERT INTO settle_tasks (lock_id, proof, agent_owner, escrow_address, status, retry_count, next_retry_at, created_at)
		VALUES (?, ?, ?, ?, 'pending', 3, ?, ?)
	`, "lock-stuck-1", "proof1", "0xAgent", "0xEscrow", stuckTime, time.Now().Unix())
	qm.mu.Unlock()

	if err != nil {
		t.Fatalf("Failed to insert stuck task: %v", err)
	}

	// 验证存在卡住的任务
	stuckCount, err := qm.GetStuckTaskCount()
	if err != nil {
		t.Fatalf("Failed to get stuck count: %v", err)
	}
	if stuckCount != 1 {
		t.Errorf("Expected 1 stuck task, got %d", stuckCount)
	}

	// 执行回收
	qm.reclaimStuckTasks()

	// 验证任务已被重置（next_retry_at 更新为现在，不再算作卡住）
	stuckCount, err = qm.GetStuckTaskCount()
	if err != nil {
		t.Fatalf("Failed to get stuck count after reclaim: %v", err)
	}
	if stuckCount != 0 {
		t.Errorf("Expected 0 stuck tasks after reclaim, got %d", stuckCount)
	}

	// 验证 retry_count 已重置为 0
	qm.mu.Lock()
	var retryCount int
	err = qm.db.QueryRow("SELECT retry_count FROM settle_tasks WHERE lock_id = ?", "lock-stuck-1").Scan(&retryCount)
	qm.mu.Unlock()

	if err != nil {
		t.Fatalf("Failed to query retry_count: %v", err)
	}
	if retryCount != 0 {
		t.Errorf("Expected retry_count reset to 0, got %d", retryCount)
	}
}

func TestReclaimNoStuckTasks(t *testing.T) {
	dbFile := "./test_reclaim_no_stuck.db"
	defer os.Remove(dbFile)

	qm, err := NewQueueManager(dbFile, "http://localhost:3001/aa/settle", "test-secret")
	if err != nil {
		t.Fatalf("Failed to create QueueManager: %v", err)
	}
	defer qm.Close()

	// 插入一个正常的 pending 任务（next_retry_at 在未来）
	qm.mu.Lock()
	_, err = qm.db.Exec(`
		INSERT INTO settle_tasks (lock_id, proof, agent_owner, escrow_address, status, retry_count, next_retry_at, created_at)
		VALUES (?, ?, ?, ?, 'pending', 0, ?, ?)
	`, "lock-fresh-1", "proof1", "0xAgent", "0xEscrow", time.Now().Add(1*time.Minute).Unix(), time.Now().Unix())
	qm.mu.Unlock()

	if err != nil {
		t.Fatalf("Failed to insert fresh task: %v", err)
	}

	// 验证无卡住的任务
	stuckCount, err := qm.GetStuckTaskCount()
	if err != nil {
		t.Fatalf("Failed to get stuck count: %v", err)
	}
	if stuckCount != 0 {
		t.Errorf("Expected 0 stuck tasks, got %d", stuckCount)
	}

	// 执行回收（不应影响正常任务）
	qm.reclaimStuckTasks()

	// 验证仍然无卡住的任务
	stuckCount, err = qm.GetStuckTaskCount()
	if err != nil {
		t.Fatalf("Failed to get stuck count after reclaim: %v", err)
	}
	if stuckCount != 0 {
		t.Errorf("Expected 0 stuck tasks after reclaim, got %d", stuckCount)
	}
}
