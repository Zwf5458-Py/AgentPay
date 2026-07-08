# AgentPay 智能体支付协议 — 设计规格文档

> **项目定位**：基于 ERC-8004 + X-402 的标准化"智能体+支付"闭环系统，解决 AI Agent 在 Web3 生态中自主执行任务时的信任与结算痛点。

## 1. 架构方案：分层网关架构

采用 **Go 核心 + TypeScript 胶水层** 的分层网关架构：

- **Go Gateway**：高并发支付网关，负责 X-402 拦截、签名验证、限流、反向代理
- **AA Bridge**（TypeScript）：封装 ZeroDev SDK 的账户抽象操作，仅对 Gateway 暴露内部 API
- **Eliza Agent**（TypeScript）：ERC-8004 规范的 Agent 微服务，阶段四迁移至 TEE
- **Client SDK**（TypeScript）：封装 X-402 自动支付协商，简化客户端集成

**选型依据**：
- Go 的高并发能力匹配支付网关的高频微支付场景
- TypeScript 胶水层避免在 Go 中重写 ZeroDev SDK（工作量大且易出兼容性问题）
- 各组件职责清晰、可独立扩缩容

---

## 2. 技术栈

| 领域 | 选型 |
|------|------|
| 链基础 | Base Sepolia (chainId: 84532) |
| 合约开发 | Solidity + Foundry |
| AA 基础设施 | ZeroDev Kernel v3 + EntryPoint v0.7 |
| 后端网关 | Go + chi/gin + go-ethereum |
| AA 胶水层 | TypeScript + Fastify + @zerodev/sdk + viem |
| Agent 运行时 | Eliza 框架 (阶段1-3) → Phala TEE (阶段4) |
| 客户端 SDK | TypeScript + viem |

---

## 3. Monorepo 目录结构

```
AgentPay/
├── contracts/                  # Solidity + Foundry
│   ├── src/
│   │   ├── identity/
│   │   │   ├── AgentIdentityRegistry.sol
│   │   │   ├── ReputationRegistry.sol
│   │   │   └── ValidationRegistry.sol
│   │   ├── payment/
│   │   │   ├── PaymentEscrow.sol
│   │   │   └── AgentPaymaster.sol
│   │   └── interfaces/
│   ├── test/
│   ├── script/
│   └── foundry.toml
├── gateway/                    # Go — X-402 支付网关
│   ├── cmd/gateway/main.go
│   ├── internal/
│   │   ├── middleware/         # x402.go, ratelimit.go
│   │   ├── payment/           # verifier.go, escrow.go
│   │   ├── proxy/             # reverse.go
│   │   └── config/
│   ├── pkg/x402/              # 协议头解析/构造
│   └── go.mod
├── aa-bridge/                  # TypeScript — AA 胶水层
│   ├── src/
│   │   ├── index.ts
│   │   ├── kernel/            # account.ts, permissions.ts
│   │   ├── paymaster/         # sponsor.ts
│   │   └── routes/
│   └── package.json
├── agent/                      # TypeScript — Eliza Agent
│   ├── src/
│   │   ├── index.ts
│   │   ├── plugins/
│   │   │   ├── erc8004/       # identity.ts, proof.ts
│   │   │   └── payment/
│   │   ├── tee/               # mock.ts, phala.ts
│   │   └── models/
│   └── package.json
├── sdk/                        # TypeScript — 客户端 SDK
│   ├── src/
│   │   ├── client.ts
│   │   ├── x402.ts
│   │   └── types.ts
│   └── package.json
├── docs/superpowers/
├── docker-compose.yml
├── Makefile
└── README.md
```

---

## 4. 组件间通信

```
客户端 (SDK)
    │  HTTP + X-402 Headers
    ▼
┌─────────────┐     HTTP (内部)    ┌─────────────┐
│  Go Gateway │ ◄──────────────► │  AA Bridge  │
│  (X-402)    │                   │  (ZeroDev)  │
└──────┬──────┘                   └──────┬──────┘
       │  反向代理                         │  viem/ZeroDev SDK
       ▼                                  ▼
┌─────────────┐                   ┌──────────────┐
│ Eliza Agent │──── 链上交互 ────►│  Base Chain  │
│  (ERC-8004) │                   │  (Sepolia)   │
└─────────────┘                   └──────────────┘
```

- Go Gateway 是唯一外部入口
- AA Bridge 仅监听 `127.0.0.1:3001`，通过 `X-Internal-Secret` 头验证来源
- Gateway ↔ AA Bridge 通过 HTTP REST 通信

---

## 5. 智能合约设计

### 5.1 AgentIdentityRegistry.sol — Agent 身份 NFT

基于 ERC-721 Soulbound（不可转让）NFT：

```solidity
struct AgentMetadata {
    string  modelId;          // 可信模型标识
    string  serviceEndpoint;  // Agent API 端点 URL
    bytes32 teeAttestation;   // TEE 远程证明哈希 (阶段四填充)
    string  capabilities;     // 能力描述 (JSON)
    uint256 registeredAt;
}
```

**接口**：
- `registerAgent(AgentMetadata)` → 铸造身份 NFT，返回 agentId
- `updateMetadata(uint256 agentId, AgentMetadata)` → 仅 owner 可更新
- `getAgent(uint256 agentId)` → 查询元数据
- `verifyAgent(uint256 agentId)` → 验证身份有效性

Soulbound 通过 override `_update()` 禁止转让。

### 5.2 ReputationRegistry.sol — 信誉记录

```solidity
struct ReputationRecord {
    uint256 agentId;
    address caller;
    uint8   score;            // 1-5
    string  taskHash;         // IPFS CID
    bool    taskCompleted;
    uint256 timestamp;
}
```

**接口**：
- `submitFeedback(uint256 agentId, uint8 score, string taskHash, bool completed)`
- `getReputation(uint256 agentId)` → 聚合信誉分（平均分 + 总次数）
- `getRecords(uint256 agentId, uint256 offset, uint256 limit)`

仅实际支付过的调用方可提交反馈（通过 PaymentEscrow 交易记录校验）。

### 5.3 ValidationRegistry.sol — 验证插件注册

```solidity
interface IValidator {
    function validate(uint256 agentId, bytes calldata proof) external returns (bool);
}
```

**接口**：
- `registerValidator(string validationType, address validatorContract)`
- `validateProof(uint256 agentId, string validationType, bytes proof)`
- `getValidators()`

阶段一部署 `MockTEEValidator`（始终返回 true），阶段四替换为 `PhalaTEEValidator`。

### 5.4 PaymentEscrow.sol — 资金托管

```solidity
struct PaymentLock {
    address payer;
    uint256 agentId;
    uint256 amount;           // USDC, 6 decimals
    bytes32 requestHash;
    PaymentStatus status;     // Locked / Released / Refunded
    uint256 lockedAt;
    uint256 expiresAt;        // 超时自动退款 (5分钟)
}
```

**接口**：
- `lockPayment(uint256 agentId, uint256 amount, bytes32 requestHash)` → 锁定 USDC
- `releasePayment(bytes32 lockId, bytes proof)` → 验证证明后释放给 Agent（`onlySettler` 权限，仅 AA Bridge 签名者可调用）
- `refund(bytes32 lockId)` → 超时/失败退款（任何人可调用，但仅在 `expiresAt` 之后生效，资金退回原支付方）
- `batchSettle(bytes32[] lockIds, bytes[] proofs)` → 批量结算

使用 EIP-3009 `transferWithAuthorization` 实现非托管锁定。

### 5.5 AgentPaymaster.sol — Gas 赞助

```solidity
enum SponsorPolicy {
    USDC_DEDUCT,     // 从 USDC 余额扣除等值 Gas
    PREPAID_POOL,    // 预付资金池赞助
    FREE_TIER        // 每日限额免费
}
```

**接口**：
- `validatePaymasterUserOp(PackedUserOperation, bytes32, uint256)`
- `depositFunds(uint256 agentId)` → 预存 USDC
- `setSponsorPolicy(uint256 agentId, SponsorPolicy)`

---

## 6. Go 支付网关设计

### 6.1 中间件链

```
请求 → RateLimiter → X402Middleware → ReverseProxy → Agent
                          │
                     有Token → 验证签名 → 查链上锁定 → 放行
                     无Token → 返回 402 + 支付要求
```

### 6.2 X-402 中间件

```go
type X402Config struct {
    PricePerCall    *big.Int  // 单次调用价格 (USDC 最小单位)
    PaymentAddress  string    // PaymentEscrow 合约地址
    ChainID         int64     // 84532
    AABridgeURL     string
    EscrowTimeout   int       // 300 秒
}
```

**402 响应头**：
```
HTTP/1.1 402 Payment Required
X-402-Price: 1000
X-402-Currency: USDC
X-402-Chain: base-sepolia
X-402-Payment-Address: 0x...
X-402-Version: 1
```

### 6.3 动态限流

```go
type RateLimitConfig struct {
    GlobalRPS       int     // 10000
    PerAgentRPS     int     // 100
    PerCallerRPS    int     // 50
    BurstMultiplier float64 // 2.0
}
```

三级令牌桶（全局 / 单 Agent / 单调用方），基于 `golang.org/x/time/rate`。

### 6.4 支付验证

```go
type PaymentVerifier struct {
    ethClient    *ethclient.Client
    escrowABI    abi.ABI
    escrowAddr   common.Address
    chainID      *big.Int
}
```

- `VerifyToken(token)` → 解码 base64、验证 EIP-712 签名、查链上 PaymentLock 状态
- `TriggerSettlement(lockId, proof)` → 调用 AA Bridge `/settle` 端点

### 6.5 依赖

| 依赖 | 用途 |
|------|------|
| `net/http` + `chi` 或 `gin` | HTTP 路由 |
| `go-ethereum` | 链上查询、ABI 绑定 |
| `golang.org/x/time/rate` | 限流 |
| `go.uber.org/zap` | 结构化日志 |
| `prometheus/client_golang` | 监控 |

---

## 7. AA Bridge 设计

### 7.1 内部 API

```
POST /aa/account/create      → ZeroDev createKernelAccount()
GET  /aa/account/:agentId    → 查询账户状态/余额
POST /aa/permission/grant    → ERC-7715 grantPermission()
DELETE /aa/permission/:id    → revokePermission()
POST /aa/settle              → 构造 UserOp → releasePayment()
POST /aa/sponsor/check       → Paymaster evaluateSponsorship()
```

### 7.2 智能账户管理

```typescript
interface CreateAccountParams {
  ownerAddress: Address;
  agentId: bigint;
  salt: bigint;
}
```

使用 ZeroDev SDK 创建 Kernel v3 智能账户，CREATE2 确定性地址，首次 UserOp 时链上部署。

### 7.3 Session Key 权限

```typescript
interface PermissionScope {
  target: Address;         // 允许调用的合约
  selector: Hex;           // 允许的函数选择器
  maxValue: bigint;        // 单次最大值
  validAfter: number;
  validUntil: number;
}
```

限制：仅允许调用 PaymentEscrow + USDC approve，单笔 ≤ 0.1 USDC，累计 ≤ 10 USDC。

### 7.4 Gas 赞助

```typescript
interface SponsorDecision {
  willSponsor: boolean;
  policy: 'USDC_DEDUCT' | 'PREPAID_POOL' | 'FREE_TIER';
  estimatedGas: bigint;
  deductAmount?: bigint;
}
```

### 7.5 安全

- 仅监听 `127.0.0.1:3001`
- `X-Internal-Secret` 头验证
- 签名密钥从环境变量注入，不落盘
- 所有 UserOp 记录结构化审计日志

### 7.6 依赖

| 依赖 | 用途 |
|------|------|
| `@zerodev/sdk` | Kernel v3 智能账户 |
| `@zerodev/ecdsa-validator` | ECDSA 验证模块 |
| `viem` | 以太坊交互 |
| `permissionless` | ERC-4337 UserOp |
| `fastify` | HTTP 框架 |

---

## 8. Eliza Agent 设计

### 8.1 服务架构

Agent 通过 Eliza 插件机制扩展 Web3 能力，暴露 HTTP 端口 `127.0.0.1:3002`（仅网关可达）：

```
POST /agent/execute    → 执行推理
GET  /agent/health     → 健康检查
GET  /agent/metadata   → 元数据查询
```

### 8.2 ERC-8004 Plugin

**身份管理** (`plugins/erc8004/identity.ts`)：
- `register(profile)` → 调用 AgentIdentityRegistry 铸造 Soulbound NFT
- `updateProfile(updates)` → 更新元数据
- `publishToENS(agentId, ensName)` → 可选 ENS 绑定

**推理证明** (`plugins/erc8004/proof.ts`)：
```typescript
interface InferenceProof {
  agentId: bigint;
  inputHash: Hex;          // keccak256(输入)
  outputHash: Hex;         // keccak256(输出)
  modelId: string;
  timestamp: number;
  teeSignature?: Hex;      // 阶段四
  mockSignature: Hex;      // 阶段1-3
}
```

- `generateProof(input, output)` → 阶段 1-3 用 EOA 签名，阶段 4 用 TEE 签名
- `serializeProof(proof)` → ABI 编码供链上验证

### 8.3 Payment Plugin

Agent 不直接处理支付。响应头附带 `X-Agent-Proof: <base64 proof>`，由 Gateway 提取后调用 AA Bridge 结算。

```typescript
interface ExecuteResponse {
  output: string;
  proof: InferenceProof;
  usage: { promptTokens: number; completionTokens: number; totalTokens: number; };
}
```

### 8.4 TEE 适配层

```typescript
interface ITEEAdapter {
  isSecure(): boolean;
  getAttestation(): Promise<Hex>;
  sign(data: Hex): Promise<Hex>;
  sealKey(key: Hex): Promise<Hex>;
  unsealKey(sealed: Hex): Promise<Hex>;
}
```

- `MockTEEAdapter`：阶段 1-3，EOA 签名，密钥存内存
- `PhalaTEEAdapter`：阶段 4，SGX enclave 内签名，通过 pRuntime API 交互

通过接口抽象实现零改动切换。

---

## 9. 客户端 SDK 设计

```typescript
class AgentPayClient {
  constructor(config: AgentPayConfig);

  execute(agentId, input, context?): Promise<ExecuteResult>;
  // 自动处理 402 协商: 请求 → 402 → 签名支付 → 重发 → 获取结果

  estimatePrice(agentId): Promise<PriceInfo>;
  getAgentInfo(agentId): Promise<AgentMetadata>;
  getPaymentHistory(agentId?): Promise<PaymentRecord[]>;
}
```

**X-402 协议处理**：
- `parseChallenge(response)` → 解析 402 响应头
- `createPaymentToken(challenge, signer)` → 签名 EIP-3009，构造 base64 token
- `attachToken(request, token)` → 附加到 `Authorization: Bearer` 头

**安全阀**：价格超过 `maxAutoPayPerCall` 时抛出 `PaymentExceedsLimit`，需用户显式确认。

---

## 10. 端到端数据流

```
① SDK 发送请求 (无 token)
② Gateway 返回 402 + 支付要求
③ SDK 自动签名 EIP-3009, 构造 x402-token, 重发
④ Gateway 验证 token → AA Bridge 锁定资金 → 放行到 Agent
⑤ Agent 推理, 生成 InferenceProof, 返回结果
⑥ Gateway 透传结果给客户端 (立即可用)
⑦ Gateway 异步: 提取 proof → AA Bridge 结算 → 链上释放资金给 Agent
```

### 异常处理

| 场景 | 处理方式 |
|------|---------|
| 签名验证失败 | 401 Unauthorized |
| 余额不足 | 402 + InsufficientBalance |
| Agent 推理超时 | 5 分钟后允许退款 |
| 无效 proof | 结算失败，超时退款 |
| AA Bridge 不可用 | 缓存结算请求，重试 3 次 |
| 链上交易 revert | 记录日志，告警，人工介入 |

---

## 11. 测试策略

| 层级 | 工具 | 覆盖内容 |
|------|------|---------|
| 合约单测 | `forge test` | 每个函数、边界、权限、revert |
| 合约 Fuzz | `forge test --fuzz-runs 1000` | 金额边界、时间窗口 |
| Go 单测 | `go test` + `testify` | 中间件、签名验证、限流 |
| Go 集成 | `go test` + anvil fork | Gateway ↔ 链上合约 |
| TS 单测 | `vitest` | AA Bridge 模块、SDK X-402 |
| TS 集成 | `vitest` + anvil fork | ZeroDev 账户、UserOp |
| 端到端 | 自定义脚本 | 完整调用链闭环 |

---

## 12. 分阶段交付

### 阶段一：基础设施部署
- Foundry 项目初始化
- 5 个合约开发 + 测试
- Base Sepolia 部署
- AA Bridge 搭建 + ZeroDev 集成
- 占比约 40%

### 阶段二：Agent 封装
- Eliza 运行时搭建
- ERC-8004 Plugin 开发（身份注册 + 证明生成）
- MockTEE 适配器
- Agent 单元/集成测试
- 占比约 25%

### 阶段三：网关集成
- Go Gateway 开发（X-402 中间件 + 限流 + 反向代理）
- 客户端 SDK 开发
- 端到端集成测试
- 占比约 25%

### 阶段四：TEE 部署
- PhalaTEEAdapter 实现
- 远程证明集成
- ValidationRegistry 验证器切换
- 密钥封装迁移
- 全链路验证
- 占比约 10%

---

## 13. 开发环境

### docker-compose 编排

- `anvil`：Base Sepolia 本地分叉 (端口 8545)
- `gateway`：Go 网关 (端口 8080)
- `aa-bridge`：AA 胶水层 (端口 3001)
- `agent`：Eliza Agent (端口 3002)

### 环境变量

```bash
# 链配置
ETH_RPC_URL, CHAIN_ID
# ZeroDev
ZERODEV_PROJECT_ID, BUNDLER_URL, PAYMASTER_URL
# 密钥 (仅开发)
DEPLOYER_PRIVATE_KEY, AGENT_PRIVATE_KEY, SIGNER_PRIVATE_KEY
# 内部通信
INTERNAL_SECRET
# 合约地址 (部署后填入)
IDENTITY_REGISTRY, REPUTATION_REGISTRY, VALIDATION_REGISTRY
PAYMENT_ESCROW, AGENT_PAYMASTER, USDC_ADDRESS
```
