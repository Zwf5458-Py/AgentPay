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
