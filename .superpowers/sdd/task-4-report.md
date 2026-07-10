# Task 4: Extend Gateway SQLite Queue & Task Fields - 完成报告

## 1. 任务概述与要求
在本次任务中，我们对 Go Gateway 的 SQLite 结算任务队列进行了关键扩展，以支持三方拆分结算（Split Settlement）流程。具体修改包括：
- **数据结构与模式升级**：在 `SettleTask` 结构体中新增了三方分账所需的所有字段，并在 `initDB` 中实现了防御性的 `ALTER TABLE` 检查逻辑，可动态在已有数据库的 `settle_tasks` 表中添加缺失的列。
- **队列接口扩展**：更新了 `Enqueue` 接口，支持传入 `taskDetails`（通道结算时携带完整分账参数，旧版锁任务时传入 `nil`）。
- **待处理任务查询**：更新 `getPendingTasks`，安全采用 `sql.NullString` 和 `sql.NullInt64` 扫描以兼容旧的待处理任务，实现数据结构向后兼容。
- **拆分路由转发**：在队列 worker 执行任务时（`processSingleTask`），如果判定是通道任务（`ChannelID != ""`），自动将 API 请求 URL 的 `/aa/settle` 替换为 `/aa/split-settle`，并将完整的 EIP-712 签名及拆分账单数据以 JSON 格式 POST 发送给 AA Bridge；否则，回退到原有的旧版 `/aa/settle` 格式。
- **反向代理对齐**：更新了 `reverse.go` 的 `ModifyResponse` 拦截响应逻辑。在检测到 `X-Agent-Proof` 后，如果是通道请求，读取 Context 中的通道签名参数，解析下游 Agent 上报的模型费 `modelCost` 并计算总花费 `actualCost = modelCost + 2000`（受限于 `holdAmount`），将所有数据封装并 Enqueue。同时，Settle Receipt 的签名金额计算也同样调整为此 `actualCost`。
- **单元测试通过**：创建了 `sqlite_queue_test.go` 以提供完整的入出队与转发行为测试；更新并修复了 `proxy_hold_test.go` 和 `x402_test.go` 的编译与断言对齐，所有测试全绿通过。

---

## 2. 修改的文件及实现细节

### 2.1 `gateway/internal/queue/sqlite_queue.go`
- **SettleTask 结构体**：增加了 `ChannelID`, `HoldAmount`, `Nonce`, `Expiration`, `Signature`, `AccumulatedAmount`, `ModelCost`, `ServiceFee`, `ModelProvider`, `Treasury`, `PlatformBps`, `AgentID` 字段。
- **initDB**：添加了防重复加列逻辑：使用 `PRAGMA table_info` 读取现有表列，若缺失任何一个分账列，则自动执行 `ALTER TABLE ADD COLUMN`。
- **Enqueue**：修改了签名，支持可选 `taskDetails`。对 `INSERT` 语句进行扩展，完美处理 NULL 值。
- **getPendingTasks**：使用 `sql.Null*` 类型逐行 Scan 还原 `SettleTask` 对象。
- **processSingleTask**：针对 `task.ChannelID != ""` 场景计算新的转发目标 URL（`/aa/split-settle`），并发送更全面的 JSON 属性；对于旧任务则维持原流程。

### 2.2 `gateway/internal/proxy/reverse.go`
- 在 `ModifyResponse` 中，首先统一计算当前通道请求的 `modelCost`（来自下游 `X-Agent-Cost` 头，默认 `1000`）与 `actualCost`（`modelCost + 2000`，最大不超过 `holdAmount`）。
- 提取 Preauth EIP-712 签名，从环境变量或 fallback 中读取 `platformBps`、`modelProvider`、`treasury`，并解析 `agentID`（若缺失，取 `AGENT_ID` 环境变量或默认回退 `888`）。
- 将该 `actualCost` 和 `modelCost` 统一应用于：
  1. `QueueManager.Enqueue` 的拆分结算任务入队。
  2. 用于网关签名的 Settle Receipt（`X-402-Settle-Receipt`）返回头中。

### 2.3 单元测试及对齐
- **新测试文件**：`gateway/internal/queue/sqlite_queue_test.go`
  - `TestQueue_EnqueueAndGetPendingTasks`：测试分账字段的数据表初始与读写，并测试 `nil` 参数的向后兼容。
  - `TestQueue_ProcessSingleTask`：模拟 Bridge Server，测试当是通道任务时，分发 POST 请求至 `/aa/split-settle` 且 payload 完整正确；当是锁任务时，分发至 `/aa/settle`。
- **修改测试文件**：`gateway/internal/proxy/proxy_hold_test.go`
  - 调整断言：通道清算时用户的实际总花费（`actualCost`）现在由于加入了固定服务费 2000，断言从原有的 modelCost（如 `12000`）变更为 `14000`，同时修改了期待签名比对的 raw message 数据串。
- **修改测试文件**：`gateway/internal/middleware/x402_test.go`
  - 修正了 `TestDebugTasks` 测试中对 `queueMgr.Enqueue` 的调用签名（传入 `nil`）。

---

## 3. 测试验证结果
我们在 `/Users/oraclez/code/AgentPay/gateway` 目录下执行 `go test -v ./...`：
- **`gateway/internal/middleware`**：PASS
- **`gateway/internal/proxy`**：PASS
- **`gateway/internal/queue`**：PASS
所有 Go 网关测试均成功通过。

---

## 4. Git 提交信息
- **Commit ID**: `d0a7cfeb`
- **提交日志**:
  ```
  feat: extend SQLite queue schema and update proxy for splitSettle parameter passing
  ```
