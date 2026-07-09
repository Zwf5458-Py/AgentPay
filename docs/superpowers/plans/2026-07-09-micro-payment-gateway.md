# AgentPay 智能体微支付网关 (Micro-payment Gateway) 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AI 智能体之间的免 Gas 费高频微支付网关，支持 Host-Local EIP-712 预授权签名与全链下信贷锁定及乐观惩罚清算。

**Architecture:** 客户端向网关发出请求，网关返回 402 预授权额挑战；客户端宿主本地使用私钥进行大额 EIP-712 预授权签名并发送，网关验证后锁定信贷额度并转发下游；执行成功后，网关计算实际开销并生成网关私钥签署的清算凭证发回；客户端校验后将本地确认额度修正回落，释放多余冻结。

**Tech Stack:** Go (Chi Router, modernc.org/sqlite, crypto/ecdsa), TypeScript (Viem, Jest).

## Global Constraints
- 所有文件路径必须为绝对或明确相对路径。
- 所有代码片段和测试命令必须能开箱即用。
- 不使用任何 TODO 或 TBD 占位符。

---

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

### Task 2: Go 反向代理中签署并分发清算凭证 (Settle Receipt)

**Files:**
- Modify: `gateway/internal/proxy/proxy.go`
- Test: 创建 `gateway/internal/proxy/proxy_hold_test.go`

**Interfaces:**
- Consumes: `gateway/internal/middleware.GetLockID`, `gateway/internal/middleware.GetToken`
- Produces: 响应头中的 `X-402-Settle-Receipt` 以及由网关私钥签署的凭证

- [ ] **Step 1: 编写反向代理的清算凭证测试**
  创建 `gateway/internal/proxy/proxy_hold_test.go`，Mock 下游 Eliza 返回 200，并断言代理在向客户端写回数据时：
  1. 包含了 `X-402-Settle-Receipt` 头。
  2. 该 Receipt 包含合法格式：`<channelId>:<holdAmount>:<actualCost>:<nonce>:<sig>`。
- [ ] **Step 2: 运行测试确保失败**
  运行：`cd gateway && go test -v ./internal/proxy -run TestProxy_SettleReceipt`
  预期：FAIL
- [ ] **Step 3: 修改代理层实现凭证签名与响应拦截**
  在 `gateway/internal/proxy/proxy.go` 中，拦截下游响应后，获取 Context 中的 Hold 额度。计算实际费用 `actualCost`。读取环境变量 `GATEWAY_PRIVATE_KEY` 对应的私钥，生成 ECDSA 签名：
  ```go
  // 生成 SettleReceipt 签名，并将其追加到 w.Header().Set("X-402-Settle-Receipt", receiptStr)
  ```
- [ ] **Step 4: 运行测试确认通过**
  运行：`cd gateway && go test -v ./internal/proxy -run TestProxy_SettleReceipt`
  预期：PASS
- [ ] **Step 5: 提交**
  ```bash
  git add gateway/internal/proxy/proxy.go gateway/internal/proxy/proxy_hold_test.go
  git commit -m "feat: generate and append settle receipt signature in proxy"
  ```

---

### Task 3: 升级 TS 客户端 SDK 状态通道预授权签名与清算自愈 (TS SDK Credit Hold & Self-heal)

**Files:**
- Modify: `sdk/src/client.ts`
- Test: 创建 `sdk/test/client_hold.test.ts`

**Interfaces:**
- Consumes: 网关返回的 `X-402-Hold-Amount` 挑战与 `X-402-Settle-Receipt` 响应头
- Produces: `AgentPayClient.execute` 具备真正的 EIP-712 签名与余额清算修正

- [ ] **Step 1: 编写 SDK 预授权与清算自愈的单元测试**
  在 `sdk/test/client_hold.test.ts` 中编写测试：
  ```typescript
  test('should generate EIP-712 signatures for hold and update confirmedSpend on settle receipt', async () => { ... })
  ```
- [ ] **Step 2: 运行测试确保失败**
  运行：`cd sdk && npm run test` (只测试新增的 `client_hold.test.ts`)
  预期：FAIL
- [ ] **Step 3: 在 SDK 中实现 EIP-712 signTypedData 与 Receipt 校验**
  修改 `sdk/src/client.ts`：
  1. 使用 `viem` 中的 `signTypedData`，使用 `privateKey` 签署 `ChannelHold` 数据结构。
  2. 在响应成功后，解析 `X-402-Settle-Receipt` 响应头。
  3. 校验网关清算凭证的 ECDSA 签名。
  4. 修正本地的 `confirmedSpend` 为 `lastConfirmedSpend + actualCost`。
- [ ] **Step 4: 运行测试验证通过**
  运行：`cd sdk && npm run test`
  预期：All tests PASS
- [ ] **Step 5: 提交**
  ```bash
  git add sdk/src/client.ts sdk/test/client_hold.test.ts
  git commit -m "feat: support EIP-712 client hold signing and receipt settlement"
  ```

---

### Task 4: 扩展 Playground 前端面板显示预授权状态

**Files:**
- Modify: `playground.html`

- [ ] **Step 1: 增加 Hold / Settle 的 UI 可视化组件**
  在 `playground.html` 的状态通道状态展示区域，增加“冻结中额度 (Hold Amount)”与“本轮实际开销 (Actual Cost)”的字段显示，并用不同颜色的光圈及动画来表达锁定和清算解冻的瞬间状态变化。
- [ ] **Step 2: 手动验证网关与前端全链路测试**
  启动网关与前端测试页面，进行交互测试，观察控制台和 UI 展示是否能流利完成“402 Hold ➡️ SDK 签名 ➡️ 执行成功 ➡️ 凭证清算回落”。
- [ ] **Step 3: 提交**
  ```bash
  git add playground.html
  git commit -m "fe: visualize credit hold and settle receipt on playground"
  ```
