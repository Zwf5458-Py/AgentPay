# Task 4 执行报告：Eliza Agent 的 ERC-8004 插件开发

本报告详细记录了在 `agent/` 目录下完成 Eliza 运行时模拟端点、实现 ERC-8004 规范证明插件的任务执行细节。

## 1. 任务概述与环境搭建

我们在 `/Users/oraclez/code/AgentPay/agent` 目录下搭建了 Node.js + TypeScript 的开发环境，配置如下：
- **`package.json`**:
  - 引入了 `fastify` (API 框架) 及其 TS 运行时 `tsx`。
  - 引入了 `viem` (Web3 库) 用于以太坊密码学哈希签名及地址恢复。
  - 引入了 `dotenv` 用于环境参数加载。
  - 引入了 `vitest` 用作单元测试框架。
- **`tsconfig.json`**:
  - 配置了现代 ES2022 / NodeNext 的 ESM 模块解析，排除 `test/**/*` 确保 tsc 编译干净的生产代码至 `dist/`。

## 2. 核心模块实现

### 2.1 密码学哈希与签名逻辑
在 `src/plugins/erc8004/proof.ts` 中，我们严格遵循 ERC-8004 规范：
1. **哈希规则**:
   - `inputHash = keccak256(toBytes(input))`
   - `outputHash = keccak256(toBytes(output))`
2. **打包编码与签名**:
   - 使用 Solidity 兼容的 `encodePacked` 将 `[agentId (uint256), inputHash (bytes32), outputHash (bytes32), modelId (string), timestamp (uint256)]` 打包。
   - 对打包消息进行 `keccak256` 得到 `messageHash`。
   - 使用 Agent 的 TEE 模拟私钥，通过 `signMessage` 进行 EIP-191 标准签名。
3. **序列化封装**:
   - 提供 `serializeProof` 和 `deserializeProof`，安全支持 `bigint` 到 `string` 的 JSON 传输转换。

### 2.2 TEE 模拟私钥安全获取
在 `src/tee/mock.ts` 中：
- 优先读取 `process.env.AGENT_PRIVATE_KEY`。
- 如果环境变量未配置，则在内存中动态生成一个测试账户私钥并全局缓存，保证测试和开发环境自洽，不发生中断。

### 2.3 Fastify API 开发
在 `src/index.ts` 中：
- 启动 Fastify 服务监听 `127.0.0.1:3002`。
- 开放 `POST /agent/execute` 接口，接收 `{ input: string, agentId: number }` 并进行参数有效性校验。
- 返回的 Body 中携带 `{ output, proof }`；同时在 Response Header 中附带 Base64 编码的 `X-Agent-Proof`（序列化 Proof 对象）。

## 3. 测试与验证

在 `test/agent.test.ts` 中，我们编写了完整的自动化测试：
1. **API 校验测试**: 验证合法及非法参数（缺失输入、agentId 负数）情况下的返回。
2. **Base64 完美还原测试**: 提取 Response Header 的 `X-Agent-Proof`，反解 Base64，与 Body 内的 Proof 字段一一比对，全部完美契合。
3. **密码学验证测试**:
   - 提取还原后的 `InferenceProof`，用同样规则计算 `messageHash`。
   - 调用 `viem` 的 `recoverMessageAddress` 反解得到签名公钥地址。
   - 验证反解地址与 Agent Public Address 完全一致。

### 运行结果
```bash
> vitest run

 RUN  v1.6.1 /Users/oraclez/code/AgentPay/agent

 ✓ test/agent.test.ts  (3 tests) 39ms

 Test Files  1 passed (1)
      Tests  3 passed (3)
   Start at  17:44:50
   Duration  376ms
```
测试全部正常通过。

## 4. Git 提交记录

- 所有改动文件已添加到 git 分支。
- `.gitignore` 已配置，排除 `node_modules` 与构建文件夹 `dist/`。
