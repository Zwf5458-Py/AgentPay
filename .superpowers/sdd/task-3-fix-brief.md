# Task 3 修复 Brief: AA Bridge 状态容灾与生产安全加固

## 问题描述与修复要求

### 1. 禁用生产环境下的静默降级 (/aa/settle)
- 在 `src/index.ts` 的 `/aa/settle` 处理函数中：
  - 检查环境变量 `DEV_MODE`。若 `DEV_MODE !== "true"`，当调用 `PaymentEscrow.releasePayment` 合约交易发生错误（如 RPC 离线、余额不足、Proof 验证失败、Gas 估算失败等）时，**禁止捕捉异常后返回 Mock txHash**。
  - 必须直接返回 HTTP 500 状态码，并附带错误详情：`{ success: false, error: error.message }`。

### 2. 去除内存 DB 依赖，实现链上身份逆向反查智能钱包 (/aa/account/:agentId)
- 废除原有的内存 Map（如 `accountsDb`）作为唯一持久化手段。
- 在 `src/index.ts` 的 `GET /aa/account/:agentId` 或 `createAccount` 逻辑中：
  - 如果缓存未命中，在非 Mock 模式下，必须使用 `viem` 调用链上部署的 `AgentIdentityRegistry` 的 `ownerOf(agentId)` (或者 `ownerOf` 对应的接口) 动态查询该 Agent 的 EOA 所有权人地址。
  - 获取到所有权人地址后，使用 `getSmartAccountAddress` 离线 counterfactual 计算智能账户地址并返回给请求方。
  - 若在链上未查到该 `agentId` 的 NFT，返回 HTTP 404 `{ error: "Agent identity not registered" }`。

### 3. 全局异常捕捉与入参校验
- 凡是接收外部以太坊地址作为输入参数（如 `ownerAddress`, `sessionKeyAddress` 等）的接口，引入 `viem` 的 `isAddress` 方法校验地址格式。如果不合法，返回 HTTP 400 状态码。
- 为路由处理函数补充外层的 try-catch 拦截，对于一切未捕捉的内部错误返回 HTTP 500。

## 验证与测试命令
在 `aa-bridge` 目录下执行：
```bash
npm run test
```
要求：所有测试正常通过。
