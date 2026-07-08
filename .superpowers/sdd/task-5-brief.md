# Task 5 Brief: 客户端 SDK 与 AA Bridge 批量结算适配

## 目标
完成 TypeScript 客户端 SDK 状态通道自愈累加签名，以及 AA Bridge 的动态 TBA 收款反查与自动部署上链结算。在集成测试中跑通「402 通道协商 -> SDK 累加签名 -> Gateway SQLite 异步入队 -> AA Bridge TBA 反查自动部署 -> PaymentEscrow 链上批量结算」的全链路模拟。

## 涉及文件
- 修改: `sdk/src/client.ts`
- 修改: `aa-bridge/src/index.ts`
- 修改: `sdk/test/e2e.test.ts`

## 全局约束
- 累积签名参数包含 `channelId`，`accumulatedSpend`（bigint），及 `signature`。
- SDK 本地状态需能够支持同一通道的连续高频调用且 accumulatedSpend 持续累加。
- 所有单元测试 100% 通过。

## 需求与步骤

### 1. 修改 TypeScript SDK 状态通道签名累加 (`sdk/src/client.ts`)
- 在 `AgentPayClient` 内置 Map：
  `private channels = new Map<number, { id: string; accumulatedSpend: bigint }>();`
- 修改 `execute(agentId, input)`：
  - 拦截 402 响应。如果首部 `X-402-Payment-Type` 等于 `channel`：
    - 提取价格 `price`。
    - 检查 Map 中是否存在 `agentId` 对应的通道。如果不存在：
      - 创建并缓存：`id` 设置为 `channel-888`，`accumulatedSpend = 0n`。
    - 通道累计额递增：`channel.accumulatedSpend += price`。
    - 对通道 `id` 和 `accumulatedSpend` 构造 EIP-712 签名。在开发/Mock 模式下，直接生成 Mock token：
      `authorizationHeader = Bearer ${channel.id}:${channel.accumulatedSpend}:mock-channel-sig`。
    - 携带该 Token 发送二次请求，网关放行并返回 200 推理结果。

### 2. 修改 AA Bridge `/aa/settle` 路由适配 (`aa-bridge/src/index.ts`)
- 修改 `POST /aa/settle` 请求接收参数：
  - 支持接收 `channelId`, `accumulatedAmount` (string/bigint), `signature`, `agentId`。
- 在结算逻辑中：
  - 计算专属 TBA 账户地址：调用 Registry 计算 `IERC6551Registry.account(tbaImplementation, salt, chainId, agentNFT, agentId)`。
  - 检测该 TBA 账户是否部署（若 code.length == 0，调用 `createAccount` 自动部署， Mock 环境下则直接记录/模拟部署）。
  - 调用 `PaymentEscrow.batchSettle(channelId, accumulatedAmount, signature, computedTBA)` 发起链上批量结算。返回 Mock 交易 hash。

### 3. 修改 SDK 端到端集成测试 (`sdk/test/e2e.test.ts`)
- 更新测试中的 Mock Gateway、Mock Bridge 以及测试用例：
  - 在 `Mock Gateway` 中，对传入的 `Authorization: Bearer <channelId>:<accumulatedSpend>:<sig>` 进行拦截解析。如果 `accumulatedSpend` 正确且签名有效，将其 Enqueue 入库并立即给客户端返回 200。
  - 后台 Worker 触发 POST 调用 `Mock Bridge` 的 `/aa/settle` 进行批量结算。
  - 在测试中调用 `client.execute()` 两次。
  - 断言第一次触发 402 并自愈；断言第二次调用直接携带了累加了 2 倍价格的 `accumulatedSpend` 通道头部；断言 Mock Bridge 收到的最终结算累计金额确为 2 倍价格。

## 验证与测试命令
在 `sdk` 目录下执行：
```bash
npm run test
```
在 `aa-bridge` 目录下执行：
```bash
npm run test
```
要求：所有测试编译无误并 100% 通过。
