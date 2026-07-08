# Task 3 Report: AA Bridge 开发与接口测试报告

## 1. 任务概述
根据 Task 3 Brief，在 `/Users/oraclez/code/AgentPay/aa-bridge` 目录下搭建了 Node.js + TypeScript 的智能账户抽象胶水层（AA Bridge）微服务，集成了 ZeroDev Kernel v3 SDK (EntryPoint v0.7) 与 Fastify。服务具备生产模式与 Mock 本地开发模式的双模切换能力。

## 2. 依赖与环境配置
- **初始化项目**：创建了 `package.json` 及 `tsconfig.json`。
- **依赖安装**：
  - 核心依赖：`fastify` (v4), `viem` (v2), `dotenv`, `@zerodev/sdk` (v5), `@zerodev/ecdsa-validator` (v5), `@zerodev/permissions` (v5)。
  - 开发依赖：`typescript`, `ts-node`, `vitest`。
- **环境缺陷处理**：
  - 在初次测试中，由于 ZeroDev 的 CommonJS 代码引用了 `tslib`，我们手动在项目中安装了 `tslib`，彻底消除了模块找不到的编译期和运行时缺陷。
- **环境变量配置**：创建了 `.env` 和 `.env.example`。默认启用 `DEV_MODE=true` 且预置 Anvil 的默认 Settler 私钥与合约地址。

## 3. 核心模块与实现逻辑

### 3.1 账户管理 (`src/kernel/account.ts`)
- **地址生成**：
  - **Mock 模式**：基于 `ownerAddress`, `agentId` 与 `salt` 进行 `keccak256` 确定性离线哈希计算，截取后 20 字节作为确定性 Smart Account 地址。
  - **生产模式**：使用 `createKernelAccount` 结合 `signerToEcdsaValidator`（构建具有 owner 地址的自定义 LocalAccount Signer），通过 `agentId` 与 `salt` 的哈希派生 256 位 `index` 来确定性计算智能钱包的 counterfactual 链上地址。
- **余额获取**：
  - 支持查询原生 ETH 余额及 ERC20 代币余额。当本地 RPC 连接失败时，会自动且安全地降级回 Mock 数据 `{ native: '1.0', token: '100.0' }`。

### 3.2 权限与 Paymaster (`src/kernel/permissions.ts`, `src/paymaster/sponsor.ts`)
- 实现了 `grantPermission` 模块以支持授权 Session Key，并配置了 ZeroDev Paymaster Client 用于非 Mock 模式下的交易 Sponsor。

### 3.3 Fastify API Server (`src/index.ts`)
服务监听在 `127.0.0.1:3001`，内部使用内存 Map `accountsDb` 维护 `agentId` -> `smartAccount` 映射，并暴露了以下 API 接口：
- `POST /aa/account/create`
- `POST /aa/permission/grant`
- `POST /aa/settle`：该接口用来代替 Go 网关完成资金结算交易。在调用 `PaymentEscrow.releasePayment(lockId, proof, agentOwner)` 时，如果无 RPC 服务或模拟交易失败，系统会触发异常保护，打印控制台警告并安全返回 Mock 的 `txHash` 响应。
- `GET /aa/account/:agentId`

## 4. 接口单元测试与验证 (`test/aa-bridge.test.ts`)
使用 `vitest` 与 Fastify 原生的 `.inject()` 请求注入能力实现了 4 个核心 API 的全链路测试。

## 5. Git 提交
所有开发的新增代码均已被 add 并 commit 到当前 git 本地分支 `feat/payment-escrow-reputation`。

---

## 6. 状态容灾与生产安全加固修复 (V2 补丁)

针对评审中提出的 “Critical 静默降级与生产环境状态丢失漏洞”，我们对 `aa-bridge` 微服务进行了全面重构：

### 6.1 禁用生产环境下的静默降级 (`/aa/settle`)
- 移除了非 `DEV_MODE` 生产环境下的异常吞没机制。
- 当 `DEV_MODE !== "true"` 时，如果发生链上交易执行错误（如 Gas 估算失败、RPC 掉线或 Proof 验证失败），系统**禁止**返回 Mock 的哈希，而是直接向请求方返回 **HTTP 500** 状态码，并附带错误描述 `{ success: false, error: error.message }`。

### 6.2 链上身份逆向反查灾备 (`GET /aa/account/:agentId`)
- 废除了对内存 Map 的强依赖，在缓存未命中时增加了自动向链上反查所有权的灾备逻辑。
- 在生产环境下，若内存缓存未命中，使用 `viem` 通过 `readContract` 调用链上 `AgentIdentityRegistry` 合约的 `ownerOf(agentId)` 动态获取所有权人（EOA 所有者）。
- 成功取得所有权人地址后，调用 `getSmartAccountAddress` 离线计算智能钱包账户并动态回写缓存后返回。
- 若在链上未查到该 `agentId` 对应的 NFT（合约调用 revert），则直接向客户端返回 **HTTP 404** 错误 `{ error: "Agent identity not registered" }`。

### 6.3 安全加固与全局异常防御
- **以太坊地址合法性校验**：所有接收以太坊地址作为输入参数的接口（如 `ownerAddress`, `sessionKeyAddress`, `agentOwner`, `escrowAddress`）引入了 `viem` 的 `isAddress` 方法进行格式安全过滤。对于格式不合法的地址，统一拦截并返回 **HTTP 400** 状态码。
- **全局 Promise Rejection 拦截**：在 Fastify 中配置了全局 `setErrorHandler` 机制，提供最终的 Promise 异常与未捕获的报错拦截防线，始终返回 **HTTP 500**，确保微服务在任何黑天鹅异常下都不会静默退出。

### 6.4 Vitest 测试用例更新与通过验证
- 更新了 `test/aa-bridge.test.ts`，利用 `vi.mock` 劫持 `viem.createPublicClient` 的底层 JSON-RPC 传输通道（如拦截 `eth_chainId`, `eth_getBalance`, `eth_getCode`），并 Mock 本地 `account.ts` 的 `getSmartAccountAddress` 以避免在离线测试时发起真实的 EntryPoint 网络调用。
- 新增了 3 个集成测试用例，覆盖：地址合法性校验 (HTTP 400)、生产环境下缓存未命中且 NFT 存在时的成功反查与缓存重写、生产环境下未注册 NFT 返回 HTTP 404 错误、以及生产环境调用交易失败返回 HTTP 500。
- **测试通过结果**：
  ```bash
  > aa-bridge@1.0.0 test
  > vitest run

   RUN  v1.6.1 /Users/oraclez/code/AgentPay/aa-bridge

   ✓ test/aa-bridge.test.ts  (6 tests) 1111ms

   Test Files  1 passed (1)
        Tests  6 passed (6)
     Start at  17:42:48
     Duration  1.73s
  ```
  6 个测试用例全部正常通过！
