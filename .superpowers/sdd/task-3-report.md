# Task 3: 升级 TS 客户端 SDK 状态通道预授权签名与清算自愈 (TS SDK Credit Hold & Self-heal) - 完成报告

## 1. 任务概述与要求
在本次任务中，我们完成了 TS 客户端 SDK 状态通道预授权签名及清算自愈的升级工作：
- **EIP-712 签名挑战自愈**：在面临 HTTP 402 时，如果网关返回 `X-402-Payment-Type: channel`，SDK 解析 `X-402-Hold-Amount` 预授权冻结额。并在本地使用私钥签署 `ChannelHold` 类型的 EIP-712 Typed Data。其包含 `channelId`, `holdAmount`, `nonce`, `expiration` 四个字段。
- **发送二次授权请求**：将生成的签名与其余字段格式化为 `Authorization: Bearer <channelId>:<holdAmount>:<nonce>:<expiration>:<sig>` 后重试发送请求。
- **清算凭证校验**：当请求成功返回 200 时，从响应头中解析 `X-402-Settle-Receipt` 获得 `<channelId>:<holdAmount>:<actualCost>:<nonce>:<receipt_sig>`。
- **ECDSA 验证与自愈**：使用 `viem` 的 `recoverMessageAddress` 验证网关的以太坊个人签名。如果有效，将通道的 `confirmedSpend` 回落更新为 `lastConfirmedSpend + actualCost`（释放了 `holdAmount - actualCost` 的冻结额度，将 `accumulatedSpend` 恢复为 `confirmedSpend`）。

---

## 2. 代码实现细节

### 2.1 SDK 核心实现修改 ([client.ts](file:///Users/oraclez/code/AgentPay/sdk/src/client.ts))
- **配置接口修改**：在 `AgentPayClientConfig` 中增加了可选参数 `chainId`, `verifyingContract`, `gatewayAddress` 属性，并在构造函数中完成了合理的默认值配置。
- **通道状态增加 Nonce**：在 `channels` map 中为每个通道额外维护了 `nonce` 以便防重放，nonce 随每次 EIP-712 签名生成自增。
- **EIP-712 签名**：
  在 402 `channel` 分支以及在通道有 `lastPrice` 预先注入时，如果配置了 `privateKey`，使用 `viem/accounts` 的 `privateKeyToAccount(this.privateKey).signTypedData` 方法动态生成满足规格的结构化 EIP-712 签名。
- **清算凭证解析校验**：
  在请求返回 200 时，截获 `X-402-Settle-Receipt` 头，如果配置了 `gatewayAddress`，通过 `recoverMessageAddress` 验证以太坊消息签名者。校验通过后，计算修正 `confirmedSpend` 并校准 `accumulatedSpend` 和 `lastPrice`，释放被多余冻结的资金额度，实现了客户端的清算自愈。

### 2.2 单元测试编写 ([client_hold.test.ts](file:///Users/oraclez/code/AgentPay/sdk/test/client_hold.test.ts))
我们设计了完整的测试用例 `should generate EIP-712 signatures for hold and update confirmedSpend on settle receipt`：
1. 启动本地 Mock http.Server。
2. 客户端第一次发起请求，由于未携带 Authorization 头，Mock Server 拦截并返回 `HTTP 402` 挑战以及 `X-402-Hold-Amount: 50000`。
3. 客户端捕获 402 后，在内部利用私钥签署 `ChannelHold` 结构的 EIP-712 Typed Data，并携带 5 部分格式的授权头发起二次请求。
4. Mock Server 接收到二次请求后，解析 5 部分的 Authorization 头，并通过 `viem` 的 `verifyTypedData` 工具方法断言生成的 EIP-712 签名是由客户端私钥签署、符合预期的。
5. 验证成功后，Mock Server 模拟 Eliza 执行开销，生成由网关私钥签署的 `X-402-Settle-Receipt`（包含实际花费 15000）并返回 200。
6. 客户端在获取到 200 成功响应后，通过其配置的网关地址，验证清算凭证的 ECDSA 签名。验证成功，自愈修正 `confirmedSpend = 15000n`，解冻其余 35000n。

---

## 3. 测试运行结果
在 `sdk/` 目录运行 `npm run test` 进行了全部单元测试和 E2E 集成测试的验证：
```bash
> agentpay-sdk@1.0.0 test
> vitest run

 RUN  v1.6.1 /Users/oraclez/code/AgentPay/sdk

 ✓ test/client_hold.test.ts  (1 test) 32ms
 ✓ test/e2e.test.ts  (5 tests) 238ms

 Test Files  2 passed (2)
      Tests  6 passed (6)
   Start at  12:43:37
   Duration  528ms (transform 55ms, setup 0ms, collect 276ms, tests 270ms, environment 0ms, prepare 79ms)
```
测试证明：新实现已完美通过，且 100% 兼容已有的旧逻辑。

---

## 4. 代码提交信息
```bash
commit 31f38c69fc35ecdf0d9dbfa6e89f81beec184d0b
Author: Oracle.Z <oraclez@macMacBook-Pro-M32.local>
Date:   Thu Jul 9 12:43:41 2026 +0800

    feat: support EIP-712 client hold signing and receipt settlement
```
