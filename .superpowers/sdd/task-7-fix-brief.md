# Task 7 Brief: 终期评审 Blocker 问题修复与系统安全加固

## 目标
根据总审计师的 `system_audit_report.md` 报告，修复系统存在的 3 个 Blockers：
1. 为 `aa-bridge/package.json` 添加 `"type": "module"`，消除 ESM 运行时模块解析致命崩溃。
2. 在 `aa-bridge` 引入 `INTERNAL_SECRET` 安全拦截中间件，要求请求必须携带匹配的 `X-Internal-Secret` 请求头。
3. 在 `gateway` 的异步结算 `goroutine` 中添加 `X-Internal-Secret` 头，并在 `docker-compose.yml` 中安全隐藏 `3001` 端口只允许 127.0.0.1 访问。

## 涉及文件
- 修改: `aa-bridge/package.json`
- 修改: `aa-bridge/src/index.ts`
- 修改: `gateway/internal/proxy/reverse.go`
- 修改: `gateway/cmd/gateway/main.go` (从环境变量加载 INTERNAL_SECRET 并传给 reverse-proxy)
- 修改: `docker-compose.yml`
- 修改: `sdk/test/e2e.test.ts` (测试适配，带上校验头)

## 全局约束
- 共享密钥变量名称：`INTERNAL_SECRET` (用于 Go 网关与 Node Bridge 通信验证)

## 需求与步骤

### 1. 消除 ESM 运行时崩溃 (Blocker 3)
- 打开 `aa-bridge/package.json`，在顶级字段中增加 `"type": "module"`。
- 检查 `agent/package.json` 同样补充 `"type": "module"`。

### 2. AA Bridge 引入安全令牌拦截 (Blocker 1)
- 在 `aa-bridge/src/index.ts` 中，使用 Fastify 的 `onRequest` Hook 拦截请求：
  - 检查环境变量 `INTERNAL_SECRET` 是否存在，若没有，在启动或请求时打印 Critical 错误并停止服务（或直接返回 500 表示配置错误）。
  - 获取 HTTP 请求头 `x-internal-secret`。如果缺失或不等于 `INTERNAL_SECRET`，直接拦截并返回 `HTTP 401 Unauthorized`，禁止外部直接访问结算或 Session Key 生成 API。
  - 在 `aa-bridge/test/aa-bridge.test.ts` 中，更新测试用例，默认向 Fastify 的 `.inject()` 传递合法的 `x-internal-secret` 头部，并增加一个不带密钥时被返回 401 拦截的负面测试。

### 3. Go 网关添加 X-Internal-Secret 头 & 隐藏端口
- 在 `gateway/cmd/gateway/main.go` 中：
  - 从环境变量 `INTERNAL_SECRET` 提取安全密钥。
  - 初始化 `ReverseProxyWrapper` 时将其传入并保存到结构体变量中。
- 在 `gateway/internal/proxy/reverse.go` 的 `settle` 协程网络请求方法中：
  - 在构建 POST 结算请求时，在 HTTP Request 头部中注入 `X-Internal-Secret: <INTERNAL_SECRET>` 标头，保证能正常通过 AA Bridge 的安全校验。
  - 在 `gateway/internal/middleware/x402_test.go` 的 Mock Bridge 部分，对接收端请求做校验断言：测试是否确实带有预期的 `X-Internal-Secret` 头，没有则返回 401。
- 在 `docker-compose.yml` 中：
  - 将 `aa-bridge` 的端口映射 `3001:3001` 修改为 `127.0.0.1:3001:3001`，以限制其无法在公网网卡直接暴露，仅允许本地环回接口访问。

## 验证与测试命令
1. 运行 `gateway/internal/middleware` 的测试：`go test -v ./...`
2. 运行 `aa-bridge` 的测试：`npm run test`
3. 运行 `sdk` 的测试：`npm run test`
要求：全部测试正常跑通。
