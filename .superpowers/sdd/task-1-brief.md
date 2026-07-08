# Task 1 Brief: 智能合约层状态通道扩展与 EIP-712 批量结算

## 目标
修改 `PaymentEscrow.sol` 支持累积签名的 `ChannelLock`，实现 `lockChannel`，`batchSettle`，及 `refundChannel` 合约方法，并编写测试 `test_ChannelBatchSettle` 跑通 EIP-712 签名累计校验批量释放。

## 涉及文件
- 新增: `contracts/src/interfaces/IERC6551Registry.sol`
- 修改: `contracts/src/payment/PaymentEscrow.sol`
- 修改: `contracts/test/PaymentEscrowTest.t.sol`

## 全局约束
- Solidity 统一为 `0.8.20`。
- 不得使用 TODO 或占位符。
- 转账操作遵循 Checks-Effects-Interactions (CEI) 防重入规范。

## 接口变动与定义

1. **`IERC6551Registry.sol`**:
   定义 `createAccount` 与 `account` 方法。
2. **`PaymentEscrow.sol`**:
   - 增加 `ChannelLock` 结构体：
     ```solidity
     struct ChannelLock {
         address payer;
         uint256 agentId;
         uint256 maxAmount;
         uint256 settledAmount;
         uint256 expiresAt;
         PaymentStatus status;
     }
     ```
   - 映射表: `mapping(bytes32 => ChannelLock) public channels;`
   - 哈希常量: `bytes32 public constant CHANNEL_SETTLE_TYPEHASH = keccak256("ChannelSettle(bytes32 channelId,uint256 accumulatedAmount)");`
   - `lockChannel(uint256 agentId, uint256 amount, uint256 duration) external returns (bytes32 channelId)`:
     划入 USDC，在 `channels` 表中初始化通道。
   - `batchSettle(bytes32 channelId, uint256 accumulatedAmount, bytes calldata signature, address agentOwner) external onlySettler`:
     恢复 `accumulatedAmount` 的 EIP-712 签名，验证签名人必须为 `lock.payer`，并将 `accumulatedAmount` 代币划给 `agentOwner`，差额部分退回给 `lock.payer`。

## 单元测试覆盖
在 `PaymentEscrowTest.t.sol` 中添加 `test_ChannelBatchSettle`：
- 创建通道 -> SDK 用私钥生成 EIP-712 签名 -> 模拟 settler 结算 -> 断言账户代币划拨正确，通道剩余资金退回原主。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test --match-test test_ChannelBatchSettle -v
```
要求：所有测试编译无误并 100% 通过。
