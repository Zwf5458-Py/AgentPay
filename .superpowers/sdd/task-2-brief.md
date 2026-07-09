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

