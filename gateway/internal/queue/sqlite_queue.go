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
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type SettleTask struct {
	ID                int64
	LockID            string
	Proof             string
	AgentOwner        string
	EscrowAddress     string
	RetryCount        int
	ChannelID         string
	HoldAmount        uint64
	Nonce             uint64
	Expiration        uint64
	Signature         string
	AccumulatedAmount uint64
	ModelCost         uint64
	ServiceFee        uint64
	ModelProvider     string
	Treasury          string
	PlatformBps       uint16
	AgentID           int64
}

type QueueManager struct {
	db             *sql.DB
	bridgeURL      string
	internalSecret string
	mu             sync.Mutex // SQLite 互斥锁，确保并发写安全
	client         *http.Client
	wg             sync.WaitGroup // 追踪协程以实现优雅退出
}

// NewQueueManager 构造函数
func NewQueueManager(dbPath, bridgeURL, secret string) (*QueueManager, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// 限制最大打开连接数为 1，彻底杜绝并发文件锁死
	db.SetMaxOpenConns(1)

	// 配置性能优化与忙碌重试
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set journal_mode WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
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
		created_at INTEGER,
		channel_id TEXT,
		hold_amount INTEGER,
		nonce INTEGER,
		expiration INTEGER,
		signature TEXT,
		accumulated_amount INTEGER,
		model_cost INTEGER,
		service_fee INTEGER,
		model_provider TEXT,
		treasury TEXT,
		platform_bps INTEGER,
		agent_id INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_settle_tasks_status_next_retry ON settle_tasks (status, next_retry_at);
	`
	_, err := qm.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to initialize table: %w", err)
	}

	queryStripe := `
	CREATE TABLE IF NOT EXISTS consumed_stripe_sessions (
		session_id TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := qm.db.Exec(queryStripe); err != nil {
		return fmt.Errorf("failed to initialize stripe session table: %w", err)
	}

	// 动态检查缺失的列并添加
	rows, err := qm.db.Query("PRAGMA table_info(settle_tasks);")
	if err != nil {
		return fmt.Errorf("failed to get table info: %w", err)
	}
	existingCols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltVal interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltVal, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan table info: %w", err)
		}
		existingCols[name] = true
	}
	rows.Close()

	columns := []struct {
		name    string
		typeStr string
	}{
		{"channel_id", "TEXT"},
		{"hold_amount", "INTEGER"},
		{"nonce", "INTEGER"},
		{"expiration", "INTEGER"},
		{"signature", "TEXT"},
		{"accumulated_amount", "INTEGER"},
		{"model_cost", "INTEGER"},
		{"service_fee", "INTEGER"},
		{"model_provider", "TEXT"},
		{"treasury", "TEXT"},
		{"platform_bps", "INTEGER"},
		{"agent_id", "INTEGER"},
	}

	for _, col := range columns {
		if !existingCols[col.name] {
			alterQuery := fmt.Sprintf("ALTER TABLE settle_tasks ADD COLUMN %s %s", col.name, col.typeStr)
			if _, err := qm.db.Exec(alterQuery); err != nil {
				return fmt.Errorf("failed to add column %s: %w", col.name, err)
			}
		}
	}

	return nil
}

// Close 关闭数据库，阻塞等待重试协程安全退出
func (qm *QueueManager) Close() error {
	qm.wg.Wait()
	return qm.db.Close()
}

// Enqueue 入队
func (qm *QueueManager) Enqueue(lockID, proof, agentOwner, escrowAddress string, taskDetails *SettleTask) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	now := time.Now().Unix()

	var channelID, signature, modelProvider, treasury interface{}
	var holdAmount, nonce, expiration, accumulatedAmount, modelCost, serviceFee, platformBps, agentID interface{}

	if taskDetails != nil {
		if taskDetails.ChannelID != "" {
			channelID = taskDetails.ChannelID
		}
		if taskDetails.HoldAmount != 0 {
			holdAmount = taskDetails.HoldAmount
		}
		if taskDetails.Nonce != 0 {
			nonce = taskDetails.Nonce
		}
		if taskDetails.Expiration != 0 {
			expiration = taskDetails.Expiration
		}
		if taskDetails.Signature != "" {
			signature = taskDetails.Signature
		}
		if taskDetails.AccumulatedAmount != 0 {
			accumulatedAmount = taskDetails.AccumulatedAmount
		}
		if taskDetails.ModelCost != 0 {
			modelCost = taskDetails.ModelCost
		}
		if taskDetails.ServiceFee != 0 {
			serviceFee = taskDetails.ServiceFee
		}
		if taskDetails.ModelProvider != "" {
			modelProvider = taskDetails.ModelProvider
		}
		if taskDetails.Treasury != "" {
			treasury = taskDetails.Treasury
		}
		if taskDetails.PlatformBps != 0 {
			platformBps = taskDetails.PlatformBps
		}
		if taskDetails.AgentID != 0 {
			agentID = taskDetails.AgentID
		}
	}

	query := `
	INSERT OR IGNORE INTO settle_tasks (
		lock_id, proof, agent_owner, escrow_address, status, retry_count, next_retry_at, created_at,
		channel_id, hold_amount, nonce, expiration, signature, accumulated_amount, model_cost, service_fee,
		model_provider, treasury, platform_bps, agent_id
	)
	VALUES (?, ?, ?, ?, 'pending', 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := qm.db.Exec(query,
		lockID, proof, agentOwner, escrowAddress, now, now,
		channelID, holdAmount, nonce, expiration, signature, accumulatedAmount, modelCost, serviceFee,
		modelProvider, treasury, platformBps, agentID,
	)
	if err != nil {
		log.Printf("[Queue] Failed to enqueue lockId %s: %v", lockID, err)
		return err
	}
	log.Printf("[Queue] Successfully enqueued lockId %s", lockID)
	return nil
}

// StartWorker 启动后台 Worker 协程
func (qm *QueueManager) StartWorker(ctx context.Context) {
	qm.wg.Add(1)
	go func() {
		defer qm.wg.Done()
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
	SELECT id, lock_id, proof, agent_owner, escrow_address, retry_count,
	       channel_id, hold_amount, nonce, expiration, signature, accumulated_amount,
	       model_cost, service_fee, model_provider, treasury, platform_bps, agent_id
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
		var channelID, signature, modelProvider, treasury sql.NullString
		var holdAmount, nonce, expiration, accumulatedAmount, modelCost, serviceFee, platformBps, agentID sql.NullInt64

		err := rows.Scan(
			&t.ID, &t.LockID, &t.Proof, &t.AgentOwner, &t.EscrowAddress, &t.RetryCount,
			&channelID, &holdAmount, &nonce, &expiration, &signature, &accumulatedAmount,
			&modelCost, &serviceFee, &modelProvider, &treasury, &platformBps, &agentID,
		)
		if err != nil {
			return nil, err
		}

		if channelID.Valid {
			t.ChannelID = channelID.String
		}
		if holdAmount.Valid {
			t.HoldAmount = uint64(holdAmount.Int64)
		}
		if nonce.Valid {
			t.Nonce = uint64(nonce.Int64)
		}
		if expiration.Valid {
			t.Expiration = uint64(expiration.Int64)
		}
		if signature.Valid {
			t.Signature = signature.String
		}
		if accumulatedAmount.Valid {
			t.AccumulatedAmount = uint64(accumulatedAmount.Int64)
		}
		if modelCost.Valid {
			t.ModelCost = uint64(modelCost.Int64)
		}
		if serviceFee.Valid {
			t.ServiceFee = uint64(serviceFee.Int64)
		}
		if modelProvider.Valid {
			t.ModelProvider = modelProvider.String
		}
		if treasury.Valid {
			t.Treasury = treasury.String
		}
		if platformBps.Valid {
			t.PlatformBps = uint16(platformBps.Int64)
		}
		if agentID.Valid {
			t.AgentID = agentID.Int64
		}

		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (qm *QueueManager) processSingleTask(ctx context.Context, task SettleTask) {
	var targetURL string
	var bodyBytes []byte
	var err error

	if task.ChannelID != "" {
		targetURL = strings.ReplaceAll(qm.bridgeURL, "/aa/settle", "/aa/split-settle")
		bodyMap := map[string]interface{}{
			"channelId":         task.ChannelID,
			"accumulatedAmount": strconv.FormatUint(task.AccumulatedAmount, 10),
			"modelCost":         strconv.FormatUint(task.ModelCost, 10),
			"serviceFee":        strconv.FormatUint(task.ServiceFee, 10),
			"modelProvider":     task.ModelProvider,
			"treasury":          task.Treasury,
			"platformBps":       task.PlatformBps,
			"holdAmount":        strconv.FormatUint(task.HoldAmount, 10),
			"nonce":             strconv.FormatUint(task.Nonce, 10),
			"expiration":        strconv.FormatUint(task.Expiration, 10),
			"signature":         task.Signature,
			"agentId":           task.AgentID,
			"proof":             task.Proof,
			"agentOwner":        task.AgentOwner,
			"escrowAddress":     task.EscrowAddress,
		}
		bodyBytes, err = json.Marshal(bodyMap)
	} else {
		targetURL = qm.bridgeURL
		bodyMap := map[string]string{
			"lockId":        task.LockID,
			"proof":         task.Proof,
			"agentOwner":    task.AgentOwner,
			"escrowAddress": task.EscrowAddress,
		}
		bodyBytes, err = json.Marshal(bodyMap)
	}

	if err != nil {
		log.Printf("[Queue Worker] JSON marshal failed for lockId %s: %v", task.LockID, err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(bodyBytes))
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

// GetLatestTasks 获取最近的 limit 个结算任务，用互斥锁保护，created_at 读为 int64
func (qm *QueueManager) GetLatestTasks(limit int) ([]map[string]interface{}, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	query := `
	SELECT lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at
	FROM settle_tasks
	ORDER BY id DESC
	LIMIT ?
	`
	rows, err := qm.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest tasks: %w", err)
	}
	defer rows.Close()

	var tasks []map[string]interface{}
	for rows.Next() {
		var lockID, proof, agentOwner, escrowAddress, status string
		var retryCount int
		var createdAt int64
		err := rows.Scan(&lockID, &proof, &agentOwner, &escrowAddress, &status, &retryCount, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		task := map[string]interface{}{
			"lock_id":        lockID,
			"proof":          proof,
			"agent_owner":    agentOwner,
			"escrow_address": escrowAddress,
			"status":         status,
			"retry_count":    retryCount,
			"created_at":     createdAt,
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	return tasks, nil
}

// ClearTasks 清空数据库中所有的结算任务，用互斥锁保护
func (qm *QueueManager) ClearTasks() error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	query := `DELETE FROM settle_tasks`
	_, err := qm.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to clear tasks: %w", err)
	}
	return nil
}

// TryLockStripeSession 尝试向数据库插入待定记录，若由于主键冲突失败说明已锁/消费，返回 false
func (qm *QueueManager) TryLockStripeSession(sessionID string) (bool, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	_, err := qm.db.Exec(`
		INSERT INTO consumed_stripe_sessions (session_id, status)
		VALUES (?, 'pending')
	`, sessionID)
	if err != nil {
		// 主键冲突或其他错误，直接返回已锁定
		return false, nil
	}
	return true, nil
}

// CommitStripeSession 将 Stripe 会话标记为真正已消费状态
func (qm *QueueManager) CommitStripeSession(sessionID string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	_, err := qm.db.Exec(`
		UPDATE consumed_stripe_sessions
		SET status = 'consumed', updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ?
	`, sessionID)
	return err
}

// ReleaseStripeSession 如果支付失败，将待定状态记录删除以允许重新尝试
func (qm *QueueManager) ReleaseStripeSession(sessionID string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	_, err := qm.db.Exec(`
		DELETE FROM consumed_stripe_sessions
		WHERE session_id = ? AND status = 'pending'
	`, sessionID)
	return err
}
