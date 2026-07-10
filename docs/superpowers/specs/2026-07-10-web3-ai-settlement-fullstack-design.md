# Web3 AI 全景结算系统设计规格

## 概述

将 AgentPay 从单一的「用户→Agent」链上结算协议，升级为一个完整的 **Web3 AI 三方分账结算平台**。系统支持加密货币 + 法币双通道支付，具备独立的用户客户端和平台管理后台。

### 业务场景

**AI 代码审计助手**：用户向 AI 机器人提交 Solidity 智能合约代码，机器人调用本地 Hermes 模型进行安全审计分析（检测重入攻击、整数溢出等漏洞），返回审计报告。

### 三方角色

| 角色 | 说明 | 收入来源 |
|------|------|---------|
| 模型服务商 (Model Provider) | 提供 AI 推理算力的 API 中转服务 | 按 Token 消耗收取模型调用费 |
| Agent 运营者 (Agent Operator) | 运营 AI 代码审计机器人 | 收取固定审计服务费 (2000 micro-units) |
| 平台 (AgentPay Platform) | 提供结算基础设施 | 每笔交易总额 × 0.1% 佣金 |

### 资金流转

```
┌─────────────┐                    ┌──────────────────┐
│  用户钱包    │── 总费用锁定 ──────▶│  PaymentEscrow   │
│  (Payer)    │                    │  托管合约         │
└─────────────┘                    └────────┬─────────┘
                                            │ splitSettle()
                              ┌─────────────┼─────────────┐
                              ▼             ▼             ▼
                     ┌──────────────┐ ┌──────────┐ ┌──────────────┐
                     │ 模型服务商    │ │ Agent    │ │ 平台金库      │
                     │ (API Relay)  │ │ TBA 账户 │ │ (Treasury)   │
                     │ 收: 模型调用费│ │ 收: 服务费│ │ 收: 0.1% 佣金 │
                     └──────────────┘ └──────────┘ └──────────────┘
```

**分配公式**：
- `platformFee = totalAmount * 10 / 10000`（0.1%）
- `modelPayout = Agent 上报的实际 Token 消耗费用`
- `agentPayout = totalAmount - platformFee - modelPayout`

---

## 交付计划（4 阶段）

| 阶段 | 内容 | 交付物 |
|------|------|--------|
| **P1 · 核心结算** | 修复 EIP-712 域名 bug；升级合约增加 `splitSettle` 三方分账；修复 Queue Worker Channel 参数传递；补全 CORS 头 | 链上三方分账可用 |
| **P2 · 场景 + 客户端** | 改造 Agent 为代码审计服务；创建 `client.html` 用户端 | 用户端到端可用 |
| **P3 · 法币通道** | 网关 `PaymentStrategy` 抽象；Stripe Checkout 集成（测试模式）；支付宝接口预留 | 加密 + 法币双通道 |
| **P4 · 管理后台** | 创建 `admin.html` 平台运营仪表盘 | 收入看板、流水明细、Agent 管理、费率配置、趋势图表 |

> [!IMPORTANT]
> 本 spec 聚焦 **P1 阶段（核心结算）** 的详细设计。P2-P4 将在 P1 完成后各自独立编写 spec。

---

## P1 详细设计：核心结算

### 1. 智能合约变更

#### 1.1 修复 EIP-712 Domain Name 不一致（严重 Bug）

**问题**：Go Gateway 和 SDK 使用 `name: "AgentPay"` 做 EIP-712 domain separator，但合约 `PaymentEscrow.sol` 构造器使用 `name: "PaymentEscrow"`。导致链上 `batchSettle` 的 `ECDSA.recover` 恢复出错误的签名者地址，**链上结算始终失败**。

**修复**：将 `contracts/src/payment/PaymentEscrow.sol` 构造器中的 EIP-712 domain name 从 `"PaymentEscrow"` 改为 `"AgentPay"`。

#### 1.2 新增 `splitSettle` 函数

在 `PaymentEscrow.sol` 中新增三方分账结算函数：

```solidity
function splitSettle(
    bytes32 channelId,
    uint256 accumulatedAmount,   // 用户累计授权额度
    uint256 modelCost,           // 模型调用费（Agent 上报）
    uint256 serviceFee,          // Agent 服务费
    address modelProvider,       // 模型服务商收款地址
    address treasury,            // 平台金库地址
    uint16  platformBps,         // 平台佣金基点（10 = 0.1%）
    uint256 holdAmount,
    uint256 nonce,
    uint256 expiration,
    bytes   calldata signature
) external onlySettler
```

**内部逻辑**：
1. 验证 EIP-712 ChannelHold 签名（复用现有 `batchSettle` 的验签逻辑）
2. 计算三方分配：
   - `platformFee = accumulatedAmount * platformBps / 10000`
   - `modelPayout = modelCost`
   - `agentPayout = accumulatedAmount - platformFee - modelCost`
   - 安全检查：`require(agentPayout + modelPayout + platformFee <= accumulatedAmount)`
   - 安全检查：`require(modelCost + serviceFee + platformFee <= accumulatedAmount)`
3. 三笔 ERC20 `transfer`：`modelProvider`、Agent TBA、`treasury`
4. 退还未消费额度给用户：`remainder = holdAmount - accumulatedAmount`
5. 触发事件 `ChannelSplitSettled(channelId, agentPayout, modelPayout, platformFee)`

#### 1.3 新增事件

```solidity
event ChannelSplitSettled(
    bytes32 indexed channelId,
    uint256 agentPayout,
    uint256 modelPayout,
    uint256 platformFee,
    address modelProvider,
    address treasury
);
```

---

### 2. Go 网关改造

#### 2.1 X-402 中间件扩展

在 402 挑战响应头中新增字段：

| 头字段 | 说明 | 示例值 |
|--------|------|--------|
| `X-402-Platform-Bps` | 平台佣金基点 | `10` |
| `X-402-Model-Provider` | 模型服务商收款地址 | `0x...` |
| `X-402-Payment-Methods` | 支持的支付方式（预留法币扩展） | `crypto-channel` |

新增环境变量：
- `MODEL_PROVIDER_ADDRESS`：模型服务商收款地址
- `TREASURY_ADDRESS`：平台金库收款地址
- `PLATFORM_BPS`：平台佣金基点（默认 `10`）

#### 2.2 Reverse Proxy `ModifyResponse` 改造

入队数据结构扩展：

```go
type SettleTask struct {
    // 原有字段
    ChannelID   string
    Proof       string
    AgentID     string
    
    // 新增：分账参数
    HoldAmount        uint64
    Nonce             uint64
    Expiration        uint64
    Signature         string
    AccumulatedAmount uint64
    ModelCost         uint64   // 从 X-Agent-Cost 解析
    ServiceFee        uint64   // 固定服务费 (2000)
    ModelProvider     string   // 从环境变量读取
    Treasury          string   // 从环境变量读取
    PlatformBps       uint16   // 从环境变量读取
}
```

#### 2.3 Queue Worker 改造

`processSingleTask` 向 AA Bridge 发送的 POST body 扩展为上述完整结构，调用新路由 `/aa/split-settle`。

#### 2.4 CORS 头补全

在 `Access-Control-Expose-Headers` 中补充以下缺失的响应头：
- `X-402-Hold-Amount`
- `X-402-Settle-Receipt`
- `X-402-Platform-Bps`
- `X-402-Model-Provider`
- `X-402-Payment-Methods`
- `X-402-Currency`
- `X-402-Chain`
- `X-402-Version`

---

### 3. AA Bridge 升级

#### 3.1 新路由 `POST /aa/split-settle`

```typescript
interface SplitSettleRequest {
  channelId: string;
  accumulatedAmount: string;  // bigint string
  modelCost: string;
  serviceFee: string;
  modelProvider: string;
  treasury: string;
  platformBps: number;
  holdAmount: string;
  nonce: string;
  expiration: string;
  signature: string;
  agentId: number;
}
```

内部逻辑：
1. 根据 `agentId` 派生 TBA 地址（复用现有 `computeTBAAddress` 逻辑）
2. 检测 TBA 是否已部署，未部署则自动 `createAccount`
3. 调用合约 `splitSettle()`，一次交易完成三方分账
4. 返回 `{ txHash, agentPayout, modelPayout, platformFee }`

#### 3.2 向后兼容

原有 `/aa/settle` 路由**保留不删除**，确保旧版 Queue Worker 仍可工作。

#### 3.3 新增环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MODEL_PROVIDER_ADDRESS` | 模型服务商收款地址 | Anvil Account 2 (`0x3C44...`) |
| `TREASURY_ADDRESS` | 平台金库收款地址 | Anvil Account 3 (`0x90F7...`) |
| `PLATFORM_BPS` | 平台佣金基点 | `10` |

---

### 4. 验证计划

#### 4.1 自动化测试

| 测试项 | 文件 | 方法 |
|--------|------|------|
| `splitSettle` 分账正确性 | `contracts/test/PaymentEscrow.t.sol` | 新增测试用例：验证三方余额变动精确匹配 |
| `splitSettle` 边界条件 | 同上 | 测试 modelCost > accumulatedAmount、platformBps = 0 等边界 |
| EIP-712 域名统一 | 同上 | 现有 32 个测试全部通过（修复 domain name 后） |
| Queue Worker 参数传递 | `gateway/internal/queue/sqlite_queue_test.go` | 验证入队/出队数据完整性 |
| AA Bridge split-settle | `aa-bridge/src/index.test.ts` | 新增接口测试 |

#### 4.2 手动端到端验证

1. 启动 Anvil 本地链
2. 部署更新后的合约
3. 启动 Gateway → Agent → AA Bridge
4. 在 Playground 中执行请求
5. 检查 Anvil 日志，验证三方地址各收到正确金额
6. 检查 SQLite 队列任务状态为 `success`

---

### 5. 不在 P1 范围内的工作

以下内容不在 P1 范围内，将在后续阶段实现：

- 客户端 UI (`client.html`) → P2
- 代码审计 Agent 改造 → P2
- 法币支付通道 (Stripe) → P3
- 管理后台 (`admin.html`) → P4
- 支付宝接口预留 → P3
- 生产环境部署 → 所有阶段完成后
