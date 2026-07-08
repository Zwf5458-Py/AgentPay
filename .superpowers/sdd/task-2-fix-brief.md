# Task 2 修复 Brief: TBA 状态一致性及 Escrow 静态优化

## 问题描述与修复要求

### 1. 修复 TBA state() 规范一致性缺陷 (`contracts/src/payment/AgentTokenBoundAccount.sol`)
- 添加私有状态变量 `uint256 private _state;`。
- 将 `state()` 视图方法由 `pure` 重构为 `view`，并返回该状态值。
- 在 `execute` 方法成功执行外部 `call` 调用后，递增该状态值：`_state++`，以符合 ERC-6551 标准对状态改变的更新要求。

### 2. 优化错误回滚冒泡 (`contracts/src/payment/AgentTokenBoundAccount.sol`)
- 在 `execute` 外部调用成功与否的判断中，若执行失败，将原有的报错逻辑重构为基于 Assembly 提取并重新抛出（Bubble Up）目标合约返回的原始回滚报错数据：
  ```solidity
  if (!success) {
      assembly {
          revert(add(result, 32), mload(result))
      }
  }
  ```

### 3. 将托管静态地址声明为 immutable (`contracts/src/payment/PaymentEscrow.sol`)
- 将 `erc6551Registry`、`tbaImplementation` 以及 `agentIdentityRegistry` 状态变量均声明为 `immutable` 类型：
  ```solidity
  address public immutable erc6551Registry;
  address public immutable tbaImplementation;
  address public immutable agentIdentityRegistry;
  ```
- 确保在构造函数中执行赋值。

### 4. 冗余变量清理与单测追加 (`contracts/test/PaymentEscrowTest.t.sol`)
- 移除 `MockERC6551Registry` 合约中定义的未使用冗余变量 `_accounts`。
- 新增单元测试 `test_TBAExecuteIncrementsState()`：
  - 模拟 NFT 拥有者通过 TBA 执行一笔普通转账（或向 mock 合约发起调用）。
  - 在调用执行前后分别查询 `state()`。
  - 断言调用后的状态值确实相比调用前递增了 1。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test -v
```
要求：所有测试正常通过。
