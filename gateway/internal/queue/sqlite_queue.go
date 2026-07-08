package queue

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type SettleTask struct {
	ID            int64
	LockID        string
	Proof         string
	AgentOwner    string
	EscrowAddress string
	RetryCount    int
}

type QueueManager struct {
	db             *sql.DB
	bridgeURL      string
	internalSecret string
	mu             sync.Mutex // SQLite 互斥锁，确保并发写安全
	client         *http.Client
}

// NewQueueManager 构造函数
func NewQueueManager(dbPath, bridgeURL, secret string) (*QueueManager, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	qm := &QueueManager{
		db:             db,
		bridgeURL:      bridgeURL,
		internalSecret: secret,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	if err := qm.initDB(); err != nil {
		db.Close()
		return nil, err
	}

	return qm, nil
}

func (qm *QueueManager) initDB() error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	query := `
	CREATE TABLE IF NOT EXISTS settle_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		lock_id TEXT UNIQUE NOT NULL,
		proof TEXT NOT NULL,
		agent_owner TEXT NOT NULL,
		escrow_address TEXT NOT NULL,
		status TEXT DEFAULT 'pending',
		retry_count INTEGER DEFAULT 0,
		next_retry_at INTEGER,
		created_at INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_settle_tasks_status_next_retry ON settle_tasks (status, next_retry_at);
	`
	_, err := qm.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to initialize table: %w", err)
	}
	return nil
}

// Close 关闭数据库
func (qm *QueueManager) Close() error {
	return qm.db.Close()
}

// Enqueue 入队
func (qm *QueueManager) Enqueue(lockID, proof, agentOwner, escrowAddress string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	now := time.Now().Unix()
	query := `
	INSERT OR IGNORE INTO settle_tasks (lock_id, proof, agent_owner, escrow_address, status, retry_count, next_retry_at, created_at)
	VALUES (?, ?, ?, ?, 'pending', 0, ?, ?)
	`
	_, err := qm.db.Exec(query, lockID, proof, agentOwner, escrowAddress, now, now)
	if err != nil {
		log.Printf("[Queue] Failed to enqueue lockId %s: %v", lockID, err)
		return err
	}
	log.Printf("[Queue] Successfully enqueued lockId %s", lockID)
	return nil
}

// StartWorker 启动后台 Worker 协程
func (qm *QueueManager) StartWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[Queue Worker] Stopping worker...")
				return
			case <-ticker.C:
				qm.processTasks(ctx)
			}
		}
	}()
}

func (qm *QueueManager) processTasks(ctx context.Context) {
	tasks, err := qm.getPendingTasks()
	if err != nil {
		log.Printf("[Queue Worker] Fetch pending tasks failed: %v", err)
		return
	}

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return
		default:
		}
		qm.processSingleTask(ctx, task)
	}
}

func (qm *QueueManager) getPendingTasks() ([]SettleTask, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	now := time.Now().Unix()
	query := `
	SELECT id, lock_id, proof, agent_owner, escrow_address, retry_count
	FROM settle_tasks
	WHERE status = 'pending' AND next_retry_at <= ?
	`
	rows, err := qm.db.Query(query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []SettleTask
	for rows.Next() {
		var t SettleTask
		if err := rows.Scan(&t.ID, &t.LockID, &t.Proof, &t.AgentOwner, &t.EscrowAddress, &t.RetryCount); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (qm *QueueManager) processSingleTask(ctx context.Context, task SettleTask) {
	bodyMap := map[string]string{
		"lockId":        task.LockID,
		"proof":         task.Proof,
		"agentOwner":    task.AgentOwner,
		"escrowAddress": task.EscrowAddress,
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		log.Printf("[Queue Worker] JSON marshal failed for lockId %s: %v", task.LockID, err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", qm.bridgeURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Printf("[Queue Worker] Create request failed for lockId %s: %v", task.LockID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if qm.internalSecret != "" {
		req.Header.Set("X-Internal-Secret", qm.internalSecret)
	}

	resp, err := qm.client.Do(req)
	if err != nil {
		select {
		case <-ctx.Done():
			// 避免在 context 取消时报错或继续写入已关闭的 DB
			return
		default:
		}
		log.Printf("[Queue Worker] Connection failed for lockId %s: %v", task.LockID, err)
		qm.handleFailure(ctx, task, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		err = fmt.Errorf("HTTP status %s: %s", resp.Status, string(respBody))
		log.Printf("[Queue Worker] Bridge returned non-200 for lockId %s: %v", task.LockID, err)
		qm.handleFailure(ctx, task, err)
		return
	}

	// 成功
	qm.handleSuccess(ctx, task)
}

func (qm *QueueManager) handleSuccess(ctx context.Context, task SettleTask) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	qm.mu.Lock()
	defer qm.mu.Unlock()

	query := `UPDATE settle_tasks SET status = 'success' WHERE id = ?`
	_, err := qm.db.Exec(query, task.ID)
	if err != nil {
		log.Printf("[Queue Worker] Failed to update task %d success status: %v", task.ID, err)
		return
	}
	log.Printf("[Queue Worker] Successfully settled payment for lockId %s", task.LockID)
}

func (qm *QueueManager) handleFailure(ctx context.Context, task SettleTask, lastErr error) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	qm.mu.Lock()
	defer qm.mu.Unlock()

	newRetryCount := task.RetryCount + 1
	if newRetryCount >= 5 {
		query := `UPDATE settle_tasks SET status = 'failed', retry_count = ? WHERE id = ?`
		_, err := qm.db.Exec(query, newRetryCount, task.ID)
		if err != nil {
			log.Printf("[Queue Worker] Failed to update task %d failed status: %v", task.ID, err)
		}
		log.Printf("[Queue Worker] [CRITICAL ERROR] Failed to settle payment for lockId %s after %d attempts. Last error: %v", task.LockID, newRetryCount, lastErr)
	} else {
		// 指数级退避
		delay := time.Duration(1 << newRetryCount) * time.Second
		nextRetryAt := time.Now().Add(delay).Unix()

		query := `UPDATE settle_tasks SET retry_count = ?, next_retry_at = ? WHERE id = ?`
		_, err := qm.db.Exec(query, newRetryCount, nextRetryAt, task.ID)
		if err != nil {
			log.Printf("[Queue Worker] Failed to update task %d retry status: %v", task.ID, err)
		}
		log.Printf("[Queue Worker] Settle failed for lockId %s, scheduling retry %d in %v", task.LockID, newRetryCount, delay)
	}
}
