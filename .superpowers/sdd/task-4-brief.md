# Task 4 Brief: Extend Gateway SQLite Queue & Task Fields

**Goal**: Extend the SQLite database schema and QueueManager task enqueuing to store all EIP-712 pre-authorization and split settlement parameters, and send them to the AA Bridge when executing the settlement tasks.

**Files**:
- Modify: `gateway/internal/queue/sqlite_queue.go`
- Modify: `gateway/internal/proxy/reverse.go`
- Test: `gateway/internal/queue/sqlite_queue_test.go`
- Test: `gateway/internal/proxy/proxy_hold_test.go`

**Instructions**:
1. In `gateway/internal/queue/sqlite_queue.go`, extend the `SettleTask` struct to include:
   - `ChannelID string`
   - `HoldAmount uint64`
   - `Nonce uint64`
   - `Expiration uint64`
   - `Signature string`
   - `AccumulatedAmount uint64`
   - `ModelCost uint64`
   - `ServiceFee uint64`
   - `ModelProvider string`
   - `Treasury string`
   - `PlatformBps uint16`
   - `AgentID int64`
2. Update the `initDB` function to alter or create the `settle_tasks` table with these new columns:
   `channel_id TEXT`, `hold_amount INTEGER`, `nonce INTEGER`, `expiration INTEGER`, `signature TEXT`, `accumulated_amount INTEGER`, `model_cost INTEGER`, `service_fee INTEGER`, `model_provider TEXT`, `treasury TEXT`, `platform_bps INTEGER`, `agent_id INTEGER`.
   Note: Since we use `CREATE TABLE IF NOT EXISTS`, if the table already exists, the new columns won't be created in existing dbs. Ensure that we dynamically run helper check/alter statements or create the table with these columns initially. (For clean tests/fresh dbs, having them in `CREATE TABLE` query is sufficient; for existing local `gateway.db`, you may drop/alter it or write defensive alter statements).
3. Update `Enqueue` method signature and query in `sqlite_queue.go`:
   `func (qm *QueueManager) Enqueue(lockID, proof, agentOwner, escrowAddress string, taskDetails *SettleTask) error`
   (Or overload it, or change signature directly. Changing the signature and adapting caller is cleaner).
4. Update `getPendingTasks` select query to scan these new columns into `SettleTask`.
5. Update `processSingleTask` to check if it's a channel settlement (`task.ChannelID != ""`). If so, send a POST to the `/aa/split-settle` endpoint (you can compute it by replacing `/aa/settle` with `/aa/split-settle` in `qm.bridgeURL` string) with a JSON body containing all the split-settle properties. If it's a legacy lock-based task, keep the original `/aa/settle` payload structure.
6. In `gateway/internal/proxy/reverse.go`'s `ModifyResponse`:
   - If `X-Agent-Proof` is found, check if it's a channel request (middleware channel context is present).
   - If it is a channel request, calculate `modelCost` from `X-Agent-Cost` (fallbacks as specified in code), platform fee (`actualCost * platformBps / 10000`), and `serviceFee = actualCost - platformFee - modelCost`.
   - Read signature, expiration, holdAmount, nonce, and channelID from middleware context.
   - Read platformBps, modelProvider, treasury configs from environment variables or helper methods.
   - Enqueue all of these details via the updated `QueueManager.Enqueue` method.
7. Update all unit tests in `sqlite_queue_test.go` and `proxy_hold_test.go` to adapt to the new `Enqueue` signature.
8. Verify all tests in `gateway/internal/queue` and `gateway/internal/proxy` pass.
9. Commit changes.
