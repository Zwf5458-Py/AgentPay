# Task 1 Brief: Extend SQLite Queue & Register Gateway Admin Routes

**Goal**: Expose `/admin/*` API endpoints in Go Gateway to fetch statistics, retrieve the complete task queue, trigger manual task retry, and clear Stripe sessions. Secure these endpoints using `X-Internal-Secret` middleware validation.

**Files**:
- Modify: `gateway/internal/queue/sqlite_queue.go`
- Modify: `gateway/cmd/gateway/main.go`
- Modify: `gateway/internal/queue/sqlite_queue_test.go`

**Instructions**:
1. Open `gateway/internal/queue/sqlite_queue.go`.
2. Add `GetAdminStats() (map[string]interface{}, error)` on `QueueManager`:
   - Query `SELECT TOTAL(accumulated_amount) FROM settle_tasks WHERE status = 'success'`.
   - Query `SELECT TOTAL(accumulated_amount * platform_bps / 10000) FROM settle_tasks WHERE status = 'success'`.
   - Query `SELECT COUNT(*) FROM consumed_stripe_sessions`.
   - Query task status counts for `'success'`, `'pending'`, and `'failed'`.
   - Return map with keys: `total_settled_usdc`, `total_platform_fees_usdc`, `total_stripe_sessions`, `success_tasks`, `pending_tasks`, `failed_tasks`.
3. Add `ManualRetryTask(lockID string) error` on `QueueManager`:
   - Execute: `UPDATE settle_tasks SET status = 'pending', retry_count = 0, next_retry_at = ? WHERE lock_id = ?`.
4. Add `ClearStripeSessions() error` on `QueueManager`:
   - Execute: `DELETE FROM consumed_stripe_sessions`.
5. Open `gateway/cmd/gateway/main.go`.
6. Add `AdminAuthMiddleware(next http.Handler) http.Handler`:
   - Check if `INTERNAL_SECRET` env var is configured. If set, check if header `X-Internal-Secret` matches it. If not, return HTTP 401 Unauthorized.
7. Register routes under `/admin` routing group (with `CORSMiddleware` and `AdminAuthMiddleware` applied):
   - `GET /admin/stats`: calls `queueMgr.GetAdminStats()`.
   - `GET /admin/tasks`: returns all tasks (e.g. `SELECT lock_id, proof, agent_owner, escrow_address, status, retry_count, created_at FROM settle_tasks ORDER BY id DESC`). Define `queueMgr.GetAllTasks()` returning task objects.
   - `POST /admin/tasks/retry`: parses `{ "lock_id": "..." }`, calls `queueMgr.ManualRetryTask(...)`.
   - `POST /admin/stripe-sessions/clear`: calls `queueMgr.ClearStripeSessions()`.
8. Update `gateway/internal/queue/sqlite_queue_test.go` to cover these new helper methods and test that manual retry correctly updates tasks.
9. Commit changes.
