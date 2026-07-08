# Task 4 Brief: Eliza Agent 的 ERC-8004 插件开发

## 目标
在 `agent/` 目录下搭建 Eliza 运行时模拟端点，实现符合 ERC-8004 规范的证明插件。Agent 在接收到网关透传的推理请求后，执行推理并基于请求入参和生成响应使用 Agent 自身的私钥（Mock TEE）进行哈希签名，生成可供链上 `ValidationRegistry` 检验的 `InferenceProof` 证明，并通过 HTTP 头部和 JSON Body 返回。

## 涉及文件
- 新增: `agent/package.json`
- 新增: `agent/tsconfig.json`
- 新增: `agent/src/index.ts`
- 新增: `agent/src/plugins/erc8004/proof.ts`
- 新增: `agent/src/tee/mock.ts`
- 新增: `agent/test/agent.test.ts` (对证明生成和签名验证的单元测试)

## 全局约束
- 链基础: Base Sepolia (Chain ID: 84532)
- 开发语言与环境: Node.js 18+, TypeScript, Fastify (或 Express)
- 接口规范: ERC-8004 推理证明格式，支持 EIP-191 密码学自签名

## 需求与步骤

### 1. 结构与依赖配置 (package.json & tsconfig.json)
- 配置开发环境，安装 `fastify`，`viem`，`dotenv` 等基础模块。
- 引入 `vitest` 用作单测测试驱动。

### 2. 推理证明生成 (`src/plugins/erc8004/proof.ts`)
- 定义 `InferenceProof` 接口：
  ```typescript
  interface InferenceProof {
    agentId: bigint;
    inputHash: `0x${string}`;
    outputHash: `0x${string}`;
    modelId: string;
    timestamp: number;
    signature: `0x${string}`;
  }
  ```
- **哈希与签名规则**：
  - 计算 `inputHash = keccak256(toBytes(input))`。
  - 计算 `outputHash = keccak256(toBytes(output))`。
  - 构造需要被签名的结构体消息，消息打包格式使用 Solidity ABI 兼容的编码（`viem` 中的 `encodePacked` 或 `encodeAbiParameters`），打包内容为：`[agentId, inputHash, outputHash, modelId, timestamp]`。
  - 使用 `keccak256` 获得该打包消息的哈希，并使用 Agent 私钥（Mock TEE）进行 EIP-191 签名（`privateKeyToAccount(privateKey).signMessage({ message: { raw: messageHash } })`）。

### 3. API 路由定义 (index.ts)
服务运行在内部端口 `127.0.0.1:3002`：
- `POST /agent/execute`
  - Body: `{ input: string, agentId: number }`
  - 行为：
    - 校验参数完整性，校验 `agentId` 必须大于等于 0。
    - 生成 mock 的推理输出：`"Processed by AgentPay AI: " + input`。
    - 调用 `generateProof` 并使用配置的环境变量 `AGENT_PRIVATE_KEY` 签名。如果未提供 `AGENT_PRIVATE_KEY`，在测试环境下随机派生一个测试 Private Key 确保签名正常进行，绝不能发生异常中断。
    - 将最终生成的 `InferenceProof` 序列化为 JSON，并以 Base64 格式编码，附加到返回的 Response Header `X-Agent-Proof` 中。
    - 返回 Body 格式：`{ output: string, proof: InferenceProof }`。

### 4. 测试与验证 (`test/agent.test.ts`)
- 编写单测：
  - 验证 `/agent/execute` 接收合法入参后，返回的 Body 包含完整的 `InferenceProof`。
  - 验证返回的 Header `X-Agent-Proof` 存在且可通过 Base64 完美还原为 Proof 对象。
  - 验证 Proof 对象的密码学签名合法性：使用 `viem` 的 `recoverMessageAddress` 反解签名地址，验证反解出的地址与 Agent 的 Public Address 完全一致。

## 验证与测试命令
在 `agent` 目录下执行：
```bash
npm run test
```
要求：所有测试正常通过。
