### Task 1: 升级 Go 网关中间件 (X402 Middleware)

**Files:**
- Modify: `gateway/internal/middleware/x402.go`
- Test: `gateway/internal/middleware/x402_test.go`

**Interfaces:**
- Consumes: 现有的 `gateway/internal/middleware/x402.go`
- Produces: 升级后的 `X402Middleware` (从 Context 中可读取预授权 HoldAmount、ChannelID 和 Signature)

- [ ] **Step 1: 编写失败的测试**
  在 `gateway/internal/middleware/x402_test.go` 中，编写测试：
  ```go
  func TestX402Middleware_HoldAmount(t *testing.T) {
      // 1. 测试不带 Auth 头时，必须返回 402 并携带 X-402-Payment-Type: channel 和 X-402-Hold-Amount
      // 2. 测试带上 EIP-712 Hold 格式 Auth 头时放行
  }
  ```
- [ ] **Step 2: 运行测试并确保失败**
  运行：`cd gateway && go test -v ./internal/middleware -run TestX402Middleware_HoldAmount`
  预期：测试失败（因为没有返回 Hold 头部）
- [ ] **Step 3: 实现中间件预授权逻辑**
  修改 `gateway/internal/middleware/x402.go` 中的 `trigger402` 函数：
  ```go
  w.Header().Set("X-402-Payment-Type", "channel")
  w.Header().Set("X-402-Hold-Amount", "50000") // 0.05 USDC 的微额度
  ```
  并在 `X402Middleware` 中解析 `Bearer <channelId>:<holdAmount>:<nonce>:<expiration>:<sig>` 格式并注入 Context。
- [ ] **Step 4: 运行测试确认通过**
  运行：`cd gateway && go test -v ./internal/middleware -run TestX402Middleware_HoldAmount`
  预期：PASS
- [ ] **Step 5: 提交**
  ```bash
  git add gateway/internal/middleware/x402.go gateway/internal/middleware/x402_test.go
  git commit -m "feat: implement X-402 channel hold headers and parsing"
  ```

---

