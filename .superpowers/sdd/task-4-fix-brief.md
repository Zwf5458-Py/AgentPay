# Task 4 修复 Brief: Agent API 入参防崩加固

## 问题描述与修复要求

### 1. API 接口防御性重构 (`agent/src/index.ts`)
- 限制 `input` 必须是有效的字符串：
  ```typescript
  if (typeof input !== "string" || input.trim() === "") {
      return reply.code(400).send({ error: "Invalid input. Must be a non-empty string." });
  }
  ```
- 限制 `agentId` 必须是安全的、合规的正整数：
  ```typescript
  if (typeof agentId !== "number" || !Number.isSafeInteger(agentId) || agentId < 0) {
      return reply.code(400).send({ error: "Invalid agentId. Must be a non-negative safe integer." });
  }
  ```
- 确保将校验失败返回的状态码统一为 **HTTP 400**。

### 2. 单元测试覆盖与测试追加 (`agent/test/agent.test.ts`)
- 新增逆向输入漏洞测试场景：
  - 测试 `input: null`，`input: undefined` 等非字符串。
  - 测试 `agentId: 1.5`（浮点数），`agentId: NaN`（非法数值）。
  - 断言以上场景均会拦截并以 **HTTP 400** 状态码拒绝。

## 验证与测试命令
在 `agent` 目录下执行：
```bash
npm run test
```
要求：所有测试通过。
