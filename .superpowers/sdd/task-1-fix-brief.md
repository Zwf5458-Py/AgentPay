# Task 1 Fix Brief: 状态通道边界防御单测补充

## 目标
在 `contracts/test/PaymentEscrowTest.t.sol` 中追加针对通道结算 `batchSettle` 的各类边界拦截单元测试，达到 100% 覆盖率。

## 追加的测试用例需求

1. `test_ChannelBatchSettleNonSettlerReverts`
   - 模拟使用非 Settler 账户（EOA）调用 `batchSettle`，断言交易必须 Revert（例如 `OnlySettler()`）。
2. `test_ChannelBatchSettleZeroAddressRecipientReverts`
   - 结算传入的 `agentOwner = address(0)`，断言抛出 `InvalidAddress()` Revert。
3. `test_ChannelBatchSettleZeroAmountReverts`
   - 结算传入的累计消费 `accumulatedAmount = 0`，断言抛出 `InvalidAmount()` Revert。
4. `test_ChannelBatchSettleExceedMaxAmountReverts`
   - 结算传入的累计消费 `accumulatedAmount = maxAmount + 1`，断言抛出 `InvalidAmount()` Revert。
5. `test_ChannelBatchSettleDoubleSettleReverts`
   - 对同一个通道成功结算一次后，尝试再次调用进行二次结算，断言抛出 `InvalidStatus()` Revert。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test -v
```
要求：所有原有的及新增的测试均正常跑通。
