# Task 3 Report: Go Gateway 本地 SQLite 任务持久化队列实现报告

本报告概述了为 Go Gateway 引入本地 SQLite 持久化队列和后台重试 Worker 的设计方案、具体实现步骤及验证结果。

## 1. 任务背景与目标
为了降低网关/Bridge 掉线导致的资金漏单风险，我们需要：
- 将原本反向代理中的同步 HTTP 结算请求改为本地 SQLite 数据库持久化队列（只入库即返回，避免阻塞客户端）。
- 使用后台重试协程（Worker）对已入库的结算任务进行轮询，并采用指数级退避机制重试。
- 保证无外部 C 依赖（使用纯 Go 实现的 `modernc.org/sqlite`）。
- 保证网关优雅退出时的并发安全性与 SQLite 写操作的互斥锁保护。

---

## 2. 具体实现细节

### 2.1 依赖安装与声明
- 配置了 `GOPROXY` 以保证在多网络环境下依赖包能够顺利下载。
- 在 `gateway/go.mod` 中显式声明并拉取了纯 Go 的 `modernc.org/sqlite`。
- 执行 `go mod tidy` 整理了所有直接与间接依赖，消除了编译环境的缺失。

### 2.2 SQLite 本地持久化队列与后台 Worker
- 文件路径：[sqlite_queue.go](file:///Users/oraclez/code/AgentPay/gateway/internal/queue/sqlite_queue.go)
- **表结构设计**：
  在初始化数据库时，自动创建 `settle_tasks` 表：
  ```sql
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
  ```
- **写入幂等性**：
  在 `Enqueue` 方法中使用 `INSERT OR IGNORE` 逻辑，结合 `lock_id` 的 `UNIQUE` 索引，完美实现了去重与操作幂等。
- **后台 Worker 重试及退避公式**：
  - 启动后台 Go 协程，每 2 秒轮询一次待结算任务。
  - 查询条件：`status = 'pending' AND next_retry_at <= ?` (传入当前 Unix 时间戳)。
  - 网络请求失败或返回非 200：
    - 增加 `retry_count`。
    - 如果重试达到 5 次，将状态更新为 `failed` 并输出 Critical 级别报警日志。
    - 如果不足 5 次，按照指数级退避公式计算下一次重试时间：`delay := time.Duration(1 << newRetryCount) * time.Second`，即 $2^{retry\_count}$ 秒。
  - 优雅退出保护：在请求 API 或写入数据库的前后全面接入 `context.Context` 校验，一旦检测到 context 已被取消，立刻中止数据库更新，避免在单元测试或进程关闭时访问已关闭的 db 连接，从而消除报错与脏数据。

### 2.3 反代钩子拦截改造
- 文件路径：[reverse.go](file:///Users/oraclez/code/AgentPay/gateway/internal/proxy/reverse.go)
- 剔除了 `ReverseProxyWrapper` 中原本的 `http.Client`，引入 `QueueManager` 引用。
- 在代理截获到 `X-Agent-Proof` 头后，从上下文获取 `lockID`，接着调用 `QueueManager.Enqueue` 异步持久化该结算任务。
- 存盘后立刻返回推理结果给下游客户端，不在这里等待网桥结算，消除延迟并确保极高可用性。

### 2.4 主程序启动逻辑挂接
- 文件路径：[main.go](file:///Users/oraclez/code/AgentPay/gateway/cmd/gateway/main.go)
- 启动时从环境变量中读取 `INTERNAL_SECRET` 并实例化 `QueueManager` (存盘到 `gateway.db`)。
- 在 `main` 结束时延迟关闭 `QueueManager`。
- 在独立 Context 中启动后台重试 Worker，并将其传入 `NewReverseProxy` 实例中。

### 2.5 掉线自愈与并发单元测试
- 文件路径：[x402_test.go](file:///Users/oraclez/code/AgentPay/gateway/internal/middleware/x402_test.go)
- 编写了 `TestProxyReverse_BridgeOutageSelfHealing`：
  1. 客户端发送推理请求，Mock Bridge 被配置为故意返回 500。
  2. 验证网关可以瞬间返回 200 推理结果，不被 500 阻塞。
  3. 查询 SQLite 数据库，断言此时对应的 `settle_tasks` 记录为 `pending`。
  4. 将 Mock Bridge 状态调整为正常返回 200。
  5. 等待 SQLite Worker 轮询重试并断言状态正常演变为 `success`，同时验证 `X-Internal-Secret` 凭证头的匹配以及指数级退避算法的触发。

---

## 3. 测试验证结果
在 `gateway` 目录下执行单元测试：
```bash
go test -v ./...
```
测试输出：
```
=== RUN   TestX402Middleware_NoToken
--- PASS: TestX402Middleware_NoToken (0.00s)
=== RUN   TestX402Middleware_WithToken
--- PASS: TestX402Middleware_WithToken (0.00s)
=== RUN   TestProxyReverse_AsyncSettle
2026/07/08 18:20:48 [Proxy] Intercepted X-Agent-Proof. Enqueueing settle task for lockId: lock-999
2026/07/08 18:20:48 [Queue] Successfully enqueued lockId lock-999
2026/07/08 18:20:50 [Queue Worker] Stopping worker...
--- PASS: TestProxyReverse_AsyncSettle (2.01s)
=== RUN   TestProxyReverse_BridgeOutageSelfHealing
2026/07/08 18:20:50 [Proxy] Intercepted X-Agent-Proof. Enqueueing settle task for lockId: lock-heal-123
2026/07/08 18:20:50 [Queue] Successfully enqueued lockId lock-heal-123
2026/07/08 18:20:52 [Queue Worker] Bridge returned non-200 for lockId lock-heal-123: HTTP status 500 Internal Server Error: {"error":"bridge temp offline"}
2026/07/08 18:20:52 [Queue Worker] Settle failed for lockId lock-heal-123, scheduling retry 1 in 2s
2026/07/08 18:20:54 [Queue Worker] Successfully settled payment for lockId lock-heal-123
2026/07/08 18:20:57 [Queue Worker] Stopping worker...
--- PASS: TestProxyReverse_BridgeOutageSelfHealing (7.01s)
PASS
ok  	gateway/internal/middleware	9.538s
```
**结果**：测试 100% 成功通过，Bridge 掉线自愈行为完全符合预期，且在优雅停止 Worker 期间没有任何冗余数据库报错，确保了生产级健壮性。
