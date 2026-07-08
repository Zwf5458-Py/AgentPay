# Task 5 修复 Brief: SDK 并发排队锁加固与 Bridge Schema 路由校验

## 问题描述与修复要求

### 1. 实现 SDK 状态通道并发排队锁 (`sdk/src/client.ts`)
- 在 `AgentPayClient` 内部新增排队锁：
  `private channelLocks = new Map<number, Promise<any>>();`
- 将现有的 `execute(agentId: number, input: string): Promise<any>` 方法重命名为私有逻辑方法：
  `private async executeInternal(agentId: number, input: string): Promise<any>`。
- 重构公有的 `execute` 方法，基于 Promise 链实施针对同个 `agentId` 通道的串行保护：
  ```typescript
  public async execute(agentId: number, input: string): Promise<any> {
      const currentLock = this.channelLocks.get(agentId) || Promise.resolve();
      const nextLock = currentLock.then(() => this.executeInternal(agentId, input));
      // 捕获异常防止后续排队发生死锁阻塞
      this.channelLocks.set(agentId, nextLock.catch(() => {}));
      return nextLock;
  }
  ```

### 2. 移除测试延时，模拟真实并发 (`sdk/test/e2e.test.ts`)
- 修改测试用例 `should support state channel adaptive spend accumulation over multiple calls`。
- 移除测试代码中的 `await new Promise(resolve => setTimeout(resolve, 100));` 人为等待逻辑。
- 直接采用并发触发：
  `const [res1, res2] = await Promise.all([client.execute(888, 'A'), client.execute(888, 'B')]);`
- 断言 `res1` 与 `res2` 均正常返回 200 结果，且第二次请求没有触发 402，且累计消费成功累增至 2000n。

### 3. Bridge 路由引入 Fastify 结构化 Schema 验证 (`aa-bridge/src/index.ts`)
- 为 `/aa/settle` 路由配置挂载 `schema` 类型校验对象，校验 request body 参数：
  ```typescript
  schema: {
    body: {
      type: 'object',
      properties: {
        lockId: { type: 'string' },
        proof: { type: 'string' },
        channelId: { type: 'string' },
        accumulatedAmount: { type: 'string' },
        signature: { type: 'string' },
        agentId: { type: 'integer' }
      }
    }
  }
  ```
- 路由 Handler 内部继续保留条件业务逻辑验证，对参数缺失返回 HTTP 400。

## 验证与测试命令
- 在 `sdk` 目录下运行：`npm run test`
- 在 `aa-bridge` 目录下运行：`npm run test`
要求：所有测试正常通过。
