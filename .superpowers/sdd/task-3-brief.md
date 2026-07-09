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

