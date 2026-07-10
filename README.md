# AgentPay 通用 AI 智能体支付与弹性复式记账结算协议

AgentPay 是一个专为 AI 智能体（AI Agents，如 Claude Code, Eliza）设计的高并发、低延迟微支付与可编程清结算底座。

它有机地融合了 **X-402 接口支付协商标准**、**EIP-712 预授权通道锁定**、**独立复式平衡记账账本（Double-Entry Ledger）**、**多模型计费计量服务（Pricing Engine）**、**Redis 分布式限流与并发锁**、**下游 RPC 故障熔断隔离** 与标准的 **Model Context Protocol (MCP) 插件协议**。通过链下累计签名授权和链上批量清算，极大降低了 Agent 间交易的 Gas 损耗与延迟。

---

## 1. 模块化系统架构

整个项目基于 Go Workspace 与 TypeScript 服务联合构建，实现了“清结算隔离”和“计费记账分层”的高稳定架构：

```
                           ┌─────────────────────────────┐
                           │      MCP Clients / SDK      │
                           │  - Claude Desktop, Cursor   │
                           │  - client.html / admin.html │
                           └──────────────┬──────────────┘
                                          │ 1. API 访问 / X-402 协商 / MCP JSON-RPC
                                          ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                               Go Gateway (微支付协商网关)                                │
│   - 限流防刷 (Rate Limiter)      - X-402 Challenge Interceptor     - MCP Tools Endpoint │
│   - Redis 分布式限制 (令牌桶)    - Agent 并发锁 (Lua SET NX)       - 锁回收 Worker (GC) │
└───────────────────┬────────────────────────────────────────────┬───────────────────────┘
                    │ 2. 锁定资金并创建 Invoice                   │ 3. 异步对账 / 记账结算
                    ▼                                            ▼
┌──────────────────────────────────────┐     ┌───────────────────────────────────────────┐
│     Go Pricing (可编程计费引擎)       │     │       Go Ledger (物理复式记账账本)         │
│   - Quote 纯报价 (Per-call/订阅/阶梯) │     │   - LedgerStore (SQLite WAL 高并发物理库)  │
│   - RecordUsage (消费用量累计记账)    │     │   - Stripe 真实支付轨 (Lock/Split/Refund)  │
└──────────────────────────────────────┘     └─────────────────────┬─────────────────────┘
                                                                   │ 4. 发起链上智能合约结算
                                                                   ▼
                                             ┌───────────────────────────────────────────┐
                                             │      aa-bridge (TBA 合约交互微服务)         │
                                             │  - circuit-breaker 熔断保护 (故障隔离)     │
                                             └─────────────────────┬─────────────────────┘
                                                                   │ 5. 状态通道三方分成入账
                                                                   ▼
                                             ┌───────────────────────────────────────────┐
                                             │      Solidity Contracts (以太坊合约)       │
                                             │  - PaymentEscrow (通道三方拆分与超时清退)    │
                                             │  - ERC-6551 Token Bound Account (TBA收款) │
                                             └───────────────────────────────────────────┘
```

---

## 2. 核心重构模块与弹性生产特性

### 2.1 物理复式记账账本 (`ledger` 模块)
*   **双式记账明细（Double-Entry）**：每一笔 Invoice 的结算均原子级生成 `debit`（借记，付款人）与 `credit`（贷记，收款人/平台）两条平衡条目，确保账本资金轧差永远归零，具备金融级对账合规性。
*   **以太坊 Nonce 幂等防重放**：基于以太坊结算 Nonce 构成账本条目唯一幂等键，在网络抖动或重试时，自动识别并拦截二次扣款（防双花）。
*   **真实 Stripe 支付轨（Stripe Checkout & Refunds）**：
    *   `CryptoRail`：底层封包清算数据，通过 `extData` 投递驱动合约桥（`aa-bridge`）进行区块链分账结算。
    *   `StripeRail`：彻底摒弃 Mock 桩，原生对接真实 Stripe API（`v1/checkout/sessions` 与 `v1/refunds`），支持 `Lock` / `Split` / `Verify` / `Refund` 等全栈接口，并内置 `mock_` 密钥自动退避安全开发机制。

### 2.2 Redis 分布式限流与 Agent 锁 (`gateway` 限流加固)
*   **分布式令牌桶**：使用 go-redis/v9 通过 Redis 集中管理令牌桶（`ratelimit:ip:{ip}`），支持多网关实例的横向水平扩展。
*   **Agent 并发锁**：利用 Lua `SET NX EX` 进行 30 秒的并发原子加锁，并具备心跳续期（`RenewLock`）与校验释放（`ReleaseLock`）防误删逻辑，防止并发竞态。
*   **优雅降级**：当 Redis 发生网络故障或未配置时，网关自动且平滑地降级退避回本地内存限流器（`IPRateLimiter`），无感保障服务可用性。

### 2.3 锁 GC / 自动回收 Worker (`reclaim.go`)
*   为了防止网关进程异常崩溃导致已锁定的 pending 任务永久挂起，系统运行锁自动回收协程（每分钟执行）。
*   自动扫描 `settle_tasks` 中 `status='pending' AND next_retry_at < now-5min` 的挂起任务，将其强制重置 `retry_count=0` 并恢复到当前时间，重新激活 Worker 重试。

### 2.4 下游结算熔断隔离器 (`circuit-breaker.ts`)
*   在 `aa-bridge` 下自研了轻量级、零外部依赖的熔断器状态机。
*   提供 5 秒超时强断路机制；在连续 5 次结算失败后进入 `open` 开路状态，10 秒超时后转入 `half-open` 探测，完美防范区块链 RPC 异常等 downstream 服务导致的网关雪崩。

### 2.5 多模型计费计量服务 (`pricing` 模块)
提供由服务端集中统一定价的通用计费核心：
*   **按次计费 (`per_call`)**：按实际消耗 tokens 乘以单价线性计算。
*   **阶梯计费 (`tiered`)**：根据该智能体在当前账户下的累计消费额度，自动落入对应优惠区间，乘以折扣单价。
*   **订阅优先 (`subscription`)**：配额未用尽时优先扣减配额，配额用尽后自动回退至线性计费。
*   **物理用量入账**：网关或 Worker 完成物理结算后触发 `RecordUsage` 真实计量落库。

### 2.6 标准 Model Context Protocol (MCP) 支持
在 `/v1/plugin/mcp` 下挂载了标准的 JSON-RPC 2.0 协议端点：
*   `pay`：锁定资金。
    *   *输入参数*：`agentId` (int, 必填), `tokens` (int, 必填，预期消费用量)。
    *   *业务*：调 `pricing.Quote` 算得需冻结资金，并在账本中创建 `pending` Invoice。
*   `checkout`：核销对账。
    *   *输入参数*：`invoiceId` (string, 必填), `actualCostUsdc` (float, 必填，实际美分消费), `tokens` (int, 选填，实际消耗用量)。
    *   *业务*：驱动底层支付轨真实清算，并在账本执行复式记账及用量记账。

---

## 3. 目录与工作区配置

项目采用 **Go 1.25.6** 工作区进行管理，根目录下使用 `go.work` 绑定以下三模块：
*   **`gateway/`**：核心网关服务。包含反向代理、限流阀门、SQLite 任务队列、Redis 锁、MCP 插件网桥。
*   **`ledger/`**：物理记账引擎及 Stripe/Crypto 支付轨适配器。
*   **`pricing/`**：计费策略配置与用量记账引擎。
*   **`aa-bridge/`**：TypeScript 编写的智能账户清算中继微服务，内含结算调用熔断器。
*   **`contracts/`**：以太坊通道三方拆分与 ERC-6551 智能账户合约。
*   **`sdk/`**：TS/JS 客户端 SDK。

---

## 4. 快速开始与测试

### 4.1 单元测试（全量保绿大满贯）
可在各模块下分别运行测试：

*   **计费引擎测试**：
    ```bash
    cd pricing && go test -v ./...
    ```
*   **账本与 Stripe 支付轨测试**：
    ```bash
    cd ledger && go test -v ./...
    ```
*   **网关、Redis限流锁与回收 Worker 测试**：
    ```bash
    cd gateway && go test -v ./...
    ```
*   **aa-bridge 与熔断器测试**：
    ```bash
    cd aa-bridge && npm run test
    ```
*   **智能合约测试**：
    ```bash
    cd contracts && forge test -v
    ```

### 4.2 Docker Compose 工作区编译与运行
网关构建 Context 已上提到项目根目录。

**构建与拉起服务**：
1. 开启您的 VPN 代理并切换为 **“全局路由 (Global)”** 模式（国内由于镜像限制，需在 Docker 设置中移除失效的 `docker.nju.edu.cn` 加速源）。
2. 运行一键构建与启动（容器组会自动拉起并健康检测 Redis 服务）：
   ```bash
   docker compose up --build
   ```

### 4.3 前端与调试面板
*   **审计沙盒客户端**：[client.html](file:///Users/oraclez/code/AgentPay/client.html)（可体验私钥直签或 Stripe 同步 X-402 自愈结算）。
*   **特权监控大屏**：[admin.html](file:///Users/oraclez/code/AgentPay/admin.html)（霓虹玻璃风实时交易统计与对账异常重试监控）。
