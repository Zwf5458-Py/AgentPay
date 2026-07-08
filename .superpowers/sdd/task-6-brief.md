# Task 6 Brief: 客户端 SDK 与端到端集成测试 (E2E Test)

## 目标
在 `sdk/` 目录下开发 TypeScript 客户端 SDK `AgentPayClient`，封装 X-402 自动协商挑战、授权自签名以及二次重连协议；在根目录下创建 `docker-compose.yml` 编排本地 anvil 分叉网、网关、AA Bridge 与 Agent 服务，并编写 `sdk/test/e2e.test.ts` 进行全系统的端到端功能验证（E2E 测试）。

## 涉及文件
- 新增: `sdk/package.json`
- 新增: `sdk/tsconfig.json`
- 新增: `sdk/src/client.ts`
- 新增: `sdk/test/e2e.test.ts`
- 新增: `docker-compose.yml` (根目录下)

## 全局约束
- 运行环境：Node.js 18+, TypeScript, Vitest
- 服务绑定：Go Gateway 监听在 `8080`，AA Bridge 监听在 `3001`，Eliza Agent 监听在 `3002`。

## 需求与步骤

### 1. SDK 脚手架与依赖安装
- 配置 `sdk/package.json` 及 `tsconfig.json`。
- 安装 `viem` 等 Web3 类库和 `vitest` 测试库。

### 2. 客户端 SDK 自动协商逻辑 (`sdk/src/client.ts`)
- 编写 `AgentPayClient` 封装方法 `execute(agentId: number, input: string): Promise<any>`：
  - 第一次尝试请求：以 POST 形式调用 Gateway `http://127.0.0.1:8080/agent/execute`。
  - **挑战捕获与自愈**：
    - 若收到 `HTTP 402` 状态码：
      - 从首部提取价格 `X-402-Price` 和支付地址 `X-402-Payment-Address`。
      - 校验价格，若大于单次支付上限 `maxPriceLimit`，抛出安全超限异常。
      - 模拟 EIP-3009 授权，生成签名格式的授权 Token（例如在测试/Mock环境下，返回格式 `"lock-999:mock-token-signed"`，其中包含提取的锁 ID 或是请求生成的锁 ID）。
      - 二次携带 `Authorization: Bearer <lockId>:<token>` 请求头重新发送 execute 推理。
    - 若收到 `HTTP 200` 状态码，直接返回响应 Body。

### 3. Docker-Compose 服务编排与 E2E 验证
- 在根目录下创建 `docker-compose.yml`，包含 `gateway`、`aa-bridge`、`agent` 服务的镜像/本地构建映射及端口透传（8080, 3001, 3002）。
- 编写 `sdk/test/e2e.test.ts` 全链路测试用例：
  - 在测试中，由于我们需要起真实的微服务并确保全流程可测试，我们需要使用 `vitest` 分别启动本地 Gateway Go 进程、Fastify TS 进程及 Downstream Agent TS 进程（或使用 Mock 框架劫持其请求确保其在测试执行时处于可用监听状态）。
  - 执行 `client.execute(0, "E2E Integration Test String")`。
  - 断言：
    - 验证第一次请求被 X-402 拦截并成功处理。
    - 验证二次请求携带 Token 后被成功放行，并获得 downstream Agent 返回的推理响应。
    - 验证下游响应头附带的 `X-Agent-Proof` 成功触发异步 settle 结算流程。

## 验证与测试命令
在 `sdk` 目录下执行：
```bash
npm run test
```
要求：所有 E2E 集成测试正常通过。
