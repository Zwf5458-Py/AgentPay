# Task 3 Brief: AA Bridge 开发 (TypeScript 账户抽象胶水层)

## 目标
在 `aa-bridge/` 目录下，使用 TypeScript 与 Fastify（或 Express）开发一个内部使用的账户抽象胶水层微服务。该服务需要封装与 ZeroDev Kernel v3 SDK (EntryPoint v0.7) 的交互，为 Go 网关提供智能账户管理、Session Key 授权以及调用 `PaymentEscrow.releasePayment` 结算的 API 接口。

## 涉及文件
- 新增: `aa-bridge/package.json`
- 新增: `aa-bridge/tsconfig.json`
- 新增: `aa-bridge/src/index.ts`
- 新增: `aa-bridge/src/kernel/account.ts`
- 新增: `aa-bridge/src/kernel/permissions.ts`
- 新增: `aa-bridge/src/paymaster/sponsor.ts`
- 新增: `aa-bridge/test/aa-bridge.test.ts` (使用 vitest 或 jest 进行接口单元测试)

## 全局约束
- 链基础: Base Sepolia (Chain ID: 84532) 或 Anvil 本地分叉 (Chain ID: 84532)
- 开发语言与环境: Node.js 18+, TypeScript, Fastify (或 Express)
- AA 基础设施: 兼容 ZeroDev Kernel v3 + EntryPoint v0.7

## 需求与步骤

### 1. 结构与依赖配置 (package.json & tsconfig.json)
- 安装 Fastify，viem，dotenv 以及 `@zerodev/sdk` 等相关依赖（见计划描述）。
- 配置 TypeScript 编译环境，支持 `npm run build` 和 `npm run dev`（使用 ts-node 运行）。

### 2. 核心模块与 Mock 机制
由于本地 Anvil 开发环境可能无法连接真实的 ZeroDev Bundler 和 Paymaster，系统必须具备**双模切换能力**（生产模式 vs Mock 开发模式）：
- **Mock 模式触发条件**：环境变量 `DEV_MODE=true` 或 `ZERODEV_PROJECT_ID` 缺失。
- **Mock 行为**：
  - 智能账户创建：确定性计算一个 Counterfactual 地址（可以使用 `viem` 简单生成），不与链上 Bundler 交互。
  - 结算 `/aa/settle`：直接使用本地的签名者私钥账户（Signer EOA）构建普通的以太坊交易，直接调用 `PaymentEscrow.releasePayment`，返回模拟的交易哈希，以配合测试。

### 3. API 路由定义 (index.ts)
服务仅供 Gateway 内部调用，监听在 `127.0.0.1:3001`：
- `POST /aa/account/create`
  - Body: `{ ownerAddress: string, agentId: number, salt: string }`
  - 返回: `{ smartAccountAddress: string, isDeployed: boolean }`
- `POST /aa/permission/grant`
  - Body: `{ agentId: number, sessionKeyAddress: string, scopes: any[], spendLimit: string }`
  - 返回: 授权结果及模拟的 txHash
- `POST /aa/settle`
  - Body: `{ lockId: string, proof: string, agentOwner: string, escrowAddress: string }`
  - 行为：调用 `PaymentEscrow.releasePayment(lockId, proof, agentOwner)` 合约方法。
  - 返回: `{ success: true, txHash: string }`
- `GET /aa/account/:agentId`
  - 返回该 Agent 对应的智能账户地址及代币余额。

### 4. 接口测试 (Vitest)
- 编写 `aa-bridge/test/aa-bridge.test.ts`。
- 在 Mock 模式下，启动 Fastify 实例，使用 `supertest` 或 Fastify 自带的 `inject` 模拟调用：
  - 调用 `/aa/account/create` 能正常返回格式正确的以太坊地址。
  - 调用 `/aa/settle` 能正常返回 `{ success: true, txHash: ... }`。

## 验证与测试命令
在 `aa-bridge` 目录下执行：
```bash
npm run test
```
要求：所有测试正常通过。
