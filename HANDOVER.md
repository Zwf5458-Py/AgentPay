# AgentPay 通用 AI 支付与计费底层重构：全案交接总结报告 (Handover Report)

本交接文档汇总了通用支付底座重构（复式记账本 `ledger` 模块、计费引擎 `pricing` 模块、标准 MCP 插件端点、以及清结算物理隔离）的最新交付状况。新会话启动后，可直接读取本文件恢复完整的开发与设计上下文。

---

## 1. 架构升级状态

项目已由“ Solidity 审计微支付网关 Demo”升级重构为**支持通用 AI 智能体调用的支付基础设施底座**，各服务和模块采用 Go Workspace（Go 1.25.6）管理，包含：
1. **`ledger/` 模块**：物理记账层。定义 `PaymentRail` 通用支付轨，内置 `LedgerService` 财务引擎，执行双式记账平衡以及基准以太坊/法币 Nonce 的幂等拦截防重。
2. **`pricing/` 模块**：通用计费计量层。支持按次/阶梯阈值单价/订阅周期配额报价，并在清结算最终点触发 `RecordUsage` 进账计量。
3. **`gateway/` 模块**：反向代理与 MCP 服务。拦截 downstream 响应自动锁存，提供 `/v1/plugin/mcp` 的 MCP Tool 标准 JSON-RPC 接口（`pay` / `checkout`）供外部 Agent 调用。

---

## 2. 核心重构与改动触及 (Phase 1 ~ 4)

### 2.1 物理 PaymentRail 与清结算解耦 (Decoupling)
*   为了防止网关队列污染，我们将物理通道调用与记账状态彻底解耦。
*   `rail.PaymentRail` 声明 `Split(..., extData string)` 签名，利用 `extData` 隧道将交易凭证 JSON 透传。
*   `CryptoRail.Split` 真正负责向 `aa-bridge/split-settle` 派发链上分账请求。
*   `sqlite_queue.go` 队列 Worker 仅需调用 `LedgerService.SettleInvoice(...)`，实现“清算驱动记账”的原子闭环。

### 2.2 Go 1.25.6 Workspace 与编译限制破除
*   创建根目录 `go.work` 绑定 `gateway`、`ledger` 与 `pricing` 模块，取消 `go.mod` 对本地相对路径的繁琐 replace。
*   Go 编译器限制同级目录导入同级 module 的 `internal/` 子包（提示 `use of internal package ... not allowed`）。我已将 `ledger` 核心库物理移出 `internal` 重命名为公开模块，恢复了 Workspace 的正常引用。
*   适配了 `docker-compose.yml` 的 gateway 构建 Context 至项目根目录，在 `gateway/Dockerfile` 内使用多阶段编译并进行 `go work sync`，彻底铲除了 Docker 部署环境的编译 Bug。

### 2.3 计费报价计量与 RecordUsage 进账
*   引入通用计费引擎 `pricing` 并与网关深度绑定。
*   反代拦截响应时读取 `X-Agent-Tokens` 用量头并调 `pricingSvc.Quote` 计算报价；在异步 Worker 分账清算或 Stripe 同步结算成功后，驱动调用 `RecordUsage` 推进 tokens 阶梯折抵。
*   MCP Tool 中 `pay` 接口入参重构为 `tokens`，由网关调 Quote 确定冻结金额。在 `checkout` 执行成功后亦调用 `RecordUsage` 进行用量计量。

---

## 3. 全链路测试验证状况

目前系统测试处于 **100% 成功** 状态：

1. **计费模块测试**:
   `cd pricing && go test -v ./...` (4 个计费策略测试 PASS)
2. **复式记账测试**:
   `cd ledger && go test -v ./...` (双式记账及去重幂等测试 PASS)
3. **网关与 MCP 测试**:
   `cd gateway && go test -v ./...` (包含新增的 MCP JSON-RPC 通道锁定与结算核销测试全部 PASS)
4. **智能合约测试**:
   `cd contracts && forge test -v` (合约 SplitSettle 资金分配与抽佣测试全部 PASS **41 tests**)
5. **aa-bridge vitest 测试**:
   `cd aa-bridge && npm test` (22 tests PASS，包含新增的熔断器测试)

---

## 4. P4 生产级弹性完成状况 (2026-07-10)

### 4.1 Redis 分布式限流 + 并发锁
*   新建 `gateway/internal/middleware/ratelimit_redis.go`：基于 go-redis/v9 的分布式令牌桶 + Lua 原子锁。
*   每 IP 限流：Redis 集中管理令牌桶（`ratelimit:ip:{ip}`），多网关实例共享。
*   每 agent 并发锁：Lua `SET lock:agent:{agentID} callID NX EX 30` 原子获取，防止并发竞态。
*   降级模式：Redis 不可用时自动回退内存限流（`IPRateLimiter`）。
*   新增测试 `ratelimit_redis_test.go`：连接降级、并发锁竞争、TTL 过期、限流阈值等。

### 4.2 真实 Stripe 法币轨
*   改造 `ledger/rail/stripe.go`：从 mock stub 变为真实 Stripe Checkout API 集成。
*   `Lock`：创建 Stripe Checkout 会话，返回 sessionID 作为 lockID。
*   `Split`：验证支付状态后记录分账（真实分账由 Stripe Connect 处理）。
*   `Refund`：创建 Stripe 退款（查询 payment_intent → POST /v1/refunds）。
*   `Verify`：查询 Stripe 会话支付状态（payment_status == 'paid'）。
*   `gateway/cmd/gateway/main.go` 改造：传递真实 `STRIPE_SECRET_KEY`（非空且非 mock_ 前缀时使用真实 API）。
*   新增测试 `ledger/rail/stripe_test.go`：mock 模式 + 真实模式密钥识别。

### 4.3 结算熔断器
*   新建 `aa-bridge/src/circuit-breaker.ts`：轻量自实现熔断器（无外部依赖）。
*   阈值 5 次失败触发开路，10s 后半开探测，5s 调用超时保护。
*   包装 `aa-bridge/src/index.ts:549` `/aa/split-settle` 端点，防止下游 RPC 故障雪崩。
*   新增测试 `aa-bridge/src/circuit-breaker.test.ts`：开路/半开/超时/重置等 7 个测试。

### 4.4 锁 GC / 回收
*   新建 `gateway/internal/queue/reclaim.go`：定期扫描超时锁并重置。
*   `StartReclaim`：每分钟扫描 `settle_tasks` 中 `status='pending' AND next_retry_at < now-5min` 的任务。
*   强制重置 `retry_count=0` + `next_retry_at=now`，触发 Worker 重试，防止进程崩溃导致锁永久持有。
*   集成 `gateway/cmd/gateway/main.go:135`：启动 `queueMgr.StartReclaim(ctx, 1*time.Minute)`。
*   新增测试 `gateway/internal/queue/reclaim_test.go`：卡住任务回收 + 正常任务保护。

### 4.5 部署配置
*   `docker-compose.yml` 新增 `redis` service（redis:7-alpine，包含健康检测）。
*   Gateway 环境变量新增：`REDIS_URL`、`RATE_LIMIT`、`RATE_BURST`、`AGENT_LOCK_TTL`。
*   Gateway 依赖 `docker-compose` 的 `redis` 服务（condition: service_healthy）。
*   新建 `.env.example` 样例配置文件。

---

## 5. 后续推荐迭代建议 (Next Steps)

1. **多模块 Docker 化依赖集成调试**：
   - 使用 `docker compose up --build` 测试全套微服务在 Docker 下的跨网络 RPC 调用。本地编译需更换失效的国内加速源，代理软件切换全局（Global）模式。
   - 验证 Redis 限流在多网关实例下的集中管理效果。
2. **高频计量在缓存中的延迟落库**：
   - 考虑在 `pricing/service` 层加入 Redis 对 `RecordUsage` 进行预处理，并在高频 tokens 刷新时定时 Bulk 批量更新至 SQLite 库，提升网关的并发吞吐能力。
2. **高频计量在缓存中的延迟落库**：
   - 考虑在 `pricing/service` 层加入 Redis 对 `RecordUsage` 进行预处理，并在高频 tokens 刷新时定时 Bulk 批量更新至 SQLite 库，提升网关的并发吞吐能力。
