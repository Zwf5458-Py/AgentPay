package queue

import (
	"context"
	"log"
	"time"
)

// StartReclaim 启动锁回收 Worker
// 定期扫描：SQLite 中 status='pending' 且 next_retry_at < now-5min 的任务
// 强制重置为 pending（next_retry_at = now）触发重试，防止锁因进程崩溃永久持有
func (qm *QueueManager) StartReclaim(ctx context.Context, interval time.Duration) {
	qm.wg.Add(1)
	go func() {
		defer qm.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[Reclaim] Stopping reclaim worker...")
				return
			case <-ticker.C:
				qm.reclaimStuckTasks()
			}
		}
	}()
}

// reclaimStuckTasks 扫描并重置超时未处理的 pending 任务
func (qm *QueueManager) reclaimStuckTasks() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// 查找超过 5min 未更新的 pending 任务
	cutoff := time.Now().Add(-5 * time.Minute).Unix()
	query := `
		UPDATE settle_tasks
		SET retry_count = 0, next_retry_at = ?
		WHERE status = 'pending' AND next_retry_at < ?
	`

	result, err := qm.db.Exec(query, time.Now().Unix(), cutoff)
	if err != nil {
		log.Printf("[Reclaim] Failed to reclaim stuck tasks: %v", err)
		return
	}

	if rows, err := result.RowsAffected(); err == nil && rows > 0 {
		log.Printf("[Reclaim] Reclaimed %d stuck tasks older than 5min", rows)
	}
}

// GetStuckTaskCount 统计超时的 pending 任务数量（供测试和监控使用）
func (qm *QueueManager) GetStuckTaskCount() (int64, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute).Unix()
	var count int64
	err := qm.db.QueryRow(`
		SELECT COUNT(*) FROM settle_tasks
		WHERE status = 'pending' AND next_retry_at < ?
	`, cutoff).Scan(&count)

	if err != nil {
		return 0, err
	}
	return count, nil
}
