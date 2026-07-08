# Task 2 修复 Brief: 信誉防刷分加固与合约安全加固

## 问题描述与修复要求

### 1. 修复防刷分漏洞 (Reputation Registry Bypass)
- **移去 `_hasPaid` 机制**：在 `PaymentEscrow.sol` 移除 `_hasPaid` 的存储和更新。
- **绑定 `lockId` 到评分系统**：
  - 修改 `ReputationRegistry.sol` 的 `addFeedback` 为：
    `function addFeedback(bytes32 lockId, uint8 score, bool completed) external`
  - 在 `addFeedback` 中通过 `paymentEscrow.getLock(lockId)` 获取锁定详情 `lock`：
    - 校验 `lock.payer == msg.sender`，不符合则抛出 `NotPayer()`。
    - 校验 `lock.status == PaymentStatus.Released`，不符合则抛出 `PaymentNotReleased()`。
    - 校验该 `lockId` 没有被评价过（定义 `mapping(bytes32 => bool) private _evaluatedLocks`），如果已被评分，抛出 `LockAlreadyEvaluated()`。
  - 提取 `lock.agentId` 进行数据聚合。

### 2. 安全边界防御
- **接收方零地址防空**：在 `PaymentEscrow.sol` 的 `releasePayment` 方法中，增加对 `agentOwner != address(0)` 的校验，否则抛出 `InvalidAddress()` 自定义错误。
- **时序校验**：在 `lockPayment` 限制 `duration > 0`，若为 0 抛出 `InvalidDuration()`。
- **构造函数校验**：所有构造函数在绑定外部合约地址（如 `paymentToken`、`validationRegistry` 等）时，确保传入参数非 `address(0)`。

### 3. 分页机制
- 优化 `ReputationRegistry.sol` 的 `getRecords(uint256 agentId, uint256 offset, uint256 limit)`，不再直接返回全局大数组，避免 Gas 耗尽。

### 4. 逆向测试与通过验证
- 在 `PaymentEscrowTest.t.sol` 中添加：
  - `test_addFeedbackDuplicateReverts`：验证对同一个 `lockId` 两次评价会抛出 `LockAlreadyEvaluated()` 异常。
  - `test_addFeedbackUnauthorizedPayerReverts`：验证非该锁的 payer 尝试对 `lockId` 评分时被拒绝并抛出 `NotPayer()`。
  - `test_addFeedbackNotReleasedReverts`：验证锁定但未释放的锁无法评分。
  - `test_releaseZeroAddressReverts`：验证 settler 传入零地址接收人会被拦截。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test
```
要求：所有原有与新写测试 100% 成功。
