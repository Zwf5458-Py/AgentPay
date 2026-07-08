# Task 5 Brief: Go 支付网关开发 (X-402 拦截与代理代理)

## 目标
在 `gateway/` 目录下使用 Go 语言开发一个高性能 X-402 支付网关。网关负责对未付款调用拦截并返回 `HTTP 402` 支付配置挑战头；对授权调用则反向代理转发给内部的 Eliza Agent，并在收到推理输出与 `X-Agent-Proof` 后，异步通知 AA Bridge 发起链上结算，以保证推理结果的高速原子释放与延迟静默结算。

## 涉及文件
- 新增: `gateway/go.mod`
- 新增: `gateway/cmd/gateway/main.go`
- 新增: `gateway/internal/middleware/x402.go`
- 新增: `gateway/internal/proxy/reverse.go`
- 新增: `gateway/internal/middleware/x402_test.go`

## 全局约束
- 端口分配：Go Gateway 监听在 `0.0.0.0:8080`
- 下游配置：Eliza Agent 监听在 `127.0.0.1:3002`，AA Bridge 监听在 `127.0.0.1:3001`
- 接口规范：X-402 支付 challenge 响应挑战，HTTP 反向代理

## 需求与步骤

### 1. 初始化 Go Module 与路由配置
- 在 `gateway/` 初始化项目并拉取 `github.com/go-chi/chi/v5` 等路由与 HTTP 处理包。

### 2. X-402 中间件拦截 (`internal/middleware/x402.go`)
- 拦截请求：
  - 检查 HTTP 请求头是否带有 `Authorization: Bearer <token>`。
  - **未带 Token 拦截**：如果无 Authorization 头，立即返回 `HTTP 402 Payment Required`。
    - Header 必须注入：
      - `X-402-Price: 1000` (单次微支付金额，等值 0.001 USDC)
      - `X-402-Currency: USDC`
      - `X-402-Chain: base-sepolia`
      - `X-402-Payment-Address: 0xPaymentEscrowContractAddress` (此处通过环境变量 `ESCROW_ADDRESS` 注入，若未配置，默认回退到一个测试 Mock 合约地址)
      - `X-402-Version: 1`
    - Response Body 返回 JSON: `{"error": "payment_required", "message": "micropayment required via x-402 protocol"}`。
  - **携带 Token 放行**：开发阶段校验 Authorization 是否以 `"Bearer "` 开头，如果格式合规，则解析并放行，将 Token 内容和状态通过 `context` 注入传递给下游 Handler。

### 3. 反向代理与异步结算挂钩 (`internal/proxy/reverse.go`)
- 实现一个 HTTP 反向代理处理器，负责将客户端发往 `/agent/execute` 的请求透传给下游 Eliza Agent (`http://127.0.0.1:3002/agent/execute`)。
- **劫持与拦截 Response**：
  - 代理拦截下游 Agent 的响应，提取 Response Header 中的 `X-Agent-Proof`（该 Base64 内容含有 TEE 签名证明）。
  - 如果检测到 `X-Agent-Proof` 头：
    - 读取客户端请求头中携带的 X-402 支付哈希或 `lockId`（可以在 `Authorization: Bearer <lockId:token>` 格式中携带，或者使用 `X-Payment-Lock-Id` 头传递，此处设计为网关能自动解析这两处之一）。
    - 网关在将响应 Body 正常写给客户端之后，**异步（使用 goroutine 协程）** 向 AA Bridge (`http://127.0.0.1:3001/aa/settle`) 发起结算请求：
      - Body: `{ "lockId": "<lockId>", "proof": "<Proof Base64>", "agentOwner": "<Agent的所有权人EOA>" }`
      - 结算结果需记录日志，即便 AA Bridge 异步结算失败，也绝不能阻塞当前客户端已经接收到的响应。

### 4. 单元与集成测试 (`internal/middleware/x402_test.go`)
- 使用 `net/http/httptest` 进行网关中间件的单元测试。
- 用例 1：不带 Token 时，验证网关能返回 HTTP 402 并正确注入 X-402-Price、X-402-Payment-Address 等所有首部字段。
- 用例 2：携带 `Bearer mock-token` 时，验证网关能够正常放行，并转发到 mock 的 Agent 服务器上。
- 用例 3：模拟反向代理流程，拦截 mock Agent 响应的 `X-Agent-Proof`，验证能否触发异步协程发送 Settle 请求。

## 验证与测试命令
在 `gateway` 目录下执行：
```bash
go test ./...
```
要求：测试通过。
