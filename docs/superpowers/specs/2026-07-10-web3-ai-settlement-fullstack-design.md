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

## P2 详细设计：场景与客户端

### 1. AI 代码安全审计智能体改造

为了提供真实的代码审计服务，需要将 `agent/src/index.ts` 中的推理提示词与输入输出改造为代码安全审计助手。

#### 1.1 Prompt 提示词模板

当 `agent` 接收到 `/agent/execute` 请求时，如果检测到输入类似于 Solidity 合约代码（或默认强制开启审计提示），其调用本地 Hermes 模型的 messages 结构将构造为：

```typescript
const systemPrompt = `你是一个顶级的 Web3 智能合约安全专家。请对用户提交的 Solidity 代码进行安全审计。
要求必须返回以下格式的结构化 Markdown 审计报告：

# 智能合约安全审计报告

## 1. 漏洞概览
- 🔴 高风险漏洞：[数量]
- 🟡 中风险漏洞：[数量]
- 🟢 低风险漏洞：[数量]

## 2. 安全综合评分
[分值，例如：85/100] 🛡️ [安全性评语]

## 3. 漏洞详情与防范建议
### [漏洞名称] ([风险级别])
- **行号**: [大概行号或相关代码片段]
- **原理说明**: [漏洞产生原因简述]
- **防范建议**: [修复建议与安全代码示例]
`;
```

#### 1.2 Agent 计费参数微调

- 机器人收取的固定服务费 `serviceFee` 为 `2000` micro-units（由网关从环境变量中读取并作为 splitSettle 拆分入参）。
- 模型推理费 `modelCost` 依旧基于实际产生的 Prompt & Completion Tokens 按公式计算。

---

### 2. 独立客户端 `client.html` 设计

`client.html` 放置在项目根目录下，提供一个完全独立于开发者沙盒的用户审计服务界面。

#### 2.1 UI 界面设计

采用暗黑未来科技风（Neon Dark Theme）配以毛玻璃磨砂（Glassmorphism）质感。
- **左侧面板**：
  - **钱包连接组件**：
    - 私钥直连模式：提供测试私钥输入框（隐藏/显示眼睛图标），默认填充 Anvil 账户 0。
    - MetaMask / 浏览器钱包直连模式：点击 "Connect Browser Wallet" 按钮，通过 `window.ethereum` 自动获取账户地址，并使用 `ethers.BrowserProvider` 或 `viem` 在签名时拉起浏览器插件进行签名。
    - 账户信息显示：动态更新当前连接的账户地址、USDC 测试代币余额。
  - **代码编辑器**：
    - 一个带有代码行号样式的 `<textarea>` 输入框，提供一键加载内置漏洞模板的功能（如“重入漏洞模板”、“整数溢出漏洞模板”）。
  - **参数设置**：
    - Agent ID 输入框（默认 `889`）。
    - Max Price 限制输入框（默认 `15000` micro-units）。
  - **执行按钮**：
    - `[开始安全审计 (Execute Audit)]` 大按钮，点击后禁用并显示 loading spinners。
- **右侧面板**：
  - **实时状态追踪时间轴**：
    - Step 1: 发起请求 (无凭证拦截)
    - Step 2: 捕获 402 预扣款挑战
    - Step 3: 本地 EIP-712 / MetaMask 签名 ChannelHold 授权
    - Step 4: 携带签名重试并开始审计 (正在审计呼吸灯闪烁)
    - Step 5: 审计完成 & 凭证清算 (自愈成功，解冻差额)
  - **审计报告展示区**：
    - 使用前端轻量级 Markdown 渲染器（如 `marked.js`，通过 CDN 引入），将大模型返回的审计 Markdown 渲染为排版精美的 HTML 页面。
  - **账单费用拆分看板**：
    - 清楚展示该笔交易的清算账单：
      - 冻结预授权金额 (Hold Amount)：`0.05 USDC`
      - 实际总扣款 (Actual Cost)：`actualCost`
      - 模型费 (Model Cost)：`modelCost`
      - 机器人服务费 (Service Fee)：`serviceFee`
      - 平台佣金 (Platform Fee)：`platformFee` (0.1%)
      - 解冻退还金额 (Refunded Payer)：`holdAmount - actualCost`

#### 2.2 双模钱包签名实现细节

- **测试私钥模式**：
  - 直接在内存中利用传入的私钥使用 `ethers.Wallet` 或 `viem` 的 `privateKeyToAccount` 完成 `signTypedData`。
- **浏览器插件钱包模式**：
  - 检测 `window.ethereum`。
  - 使用 Web3 提供者拉起钱包：
    ```javascript
    const provider = new ethers.BrowserProvider(window.ethereum);
    const signer = await provider.getSigner();
    const sig = await signer.signTypedData(domain, types, message);
    ```

---

### 3. P2 验证计划

#### 3.1 场景功能验证
- 提交含有重入漏洞的 Solidity 合约，检查 Agent 控制台日志，验证 Hermes 模型成功被 Prompt 引导并输出了指定 Markdown 格式的报告。
- 检查返回的 `X-Agent-Cost`，确保根据生成 Token 数量计算的模型费在 header 中正确返回。

#### 3.2 客户端端到端验证
- 用 `file://` 或本地静态服务器打开 `client.html`。
- 在“私钥直连”模式下，点击安全审计，观察时间轴在 402 触发后自动完成签名与自愈，最终成功在右侧展示 Markdown 报告与费用分账明细。
- 在“浏览器钱包连接”模式下，切换 MetaMask 到本地 Anvil 网络，点击审计，确认 MetaMask 成功弹出 EIP-712 结构化数据签名窗口，用户签名通过后，审计流程自愈完成并展示结果。
- 检查 SQLite 队列，验证结算任务成功入队并最终清算完成。

---

### 4. 后续待开发阶段

- 法币支付通道 (Stripe) → P3
- 管理后台 (`admin.html`) → P4
- 生产环境部署 → 所有阶段完成后
