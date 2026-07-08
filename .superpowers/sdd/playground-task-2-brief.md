# Task 2 Brief: Node.js AA 网桥跨域 CORS 加固

## 目标
在 Fastify AA Bridge 网桥应用中引入并注册 `@fastify/cors` 插件，放行来自 Web 调试端的跨域请求。

## 涉及文件
- 修改: `aa-bridge/package.json`
- 修改: `aa-bridge/src/index.ts`
- 修改: `aa-bridge/test/aa-bridge.test.ts`

## 详细要求

### 1. 引入 @fastify/cors 依赖
- 在 `aa-bridge/package.json` 中引入 `"@fastify/cors"` 模块依赖。

### 2. 注册并配置 Fastify 跨域插件
- 在 `aa-bridge/src/index.ts` 中导入并注册该插件：
  ```typescript
  import cors from '@fastify/cors';

  await server.register(cors, {
    origin: '*',
    methods: ['GET', 'POST', 'OPTIONS'],
    allowedHeaders: ['Content-Type', 'Authorization', 'x-internal-secret'],
  });
  ```
- **关键细节**：注册顺序必须优先于 `x-internal-secret` 安全校验的 `onRequest` 钩子，否则跨域预检请求（OPTIONS）会因为未带密钥而被阻断。

### 3. 测试验证
- 在 `aa-bridge/test/aa-bridge.test.ts` 中补充 OPTIONS 请求预检测试：
  - 发送 `OPTIONS /aa/settle` 请求。
  - 断言返回 200，且包含 `Access-Control-Allow-Origin: *`。
  - 运行 `vitest run` 保证所有网桥测试 100% 成功。

## 验证与测试命令
在 `aa-bridge` 目录下执行：
```bash
npm run test
```
要求：所有测试编译无误并 100% 成功。
