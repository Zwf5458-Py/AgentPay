# 智能合约层状态通道扩展与 EIP-712 批量结算设计方案

## 1. 业务目标
修改 `PaymentEscrow.sol` 智能合约，支持基于累积签名的状态通道支付（ChannelLock）。用户可通过 `lockChannel` 存入代币并开启通道。通道期内，线下进行微支付累积并由 payer 对每次累积的总金额进行 EIP-712 签名。结算人（Settler）通过调用 `batchSettle` 将最新的累积签名提交至链上，将已结算的金额转账给接收方，并将通道内的余款退回给 payer，同时关闭通道。若通道到期未结算，payer 可通过 `refundChannel` 申请退回全额资金。

## 2. 详细接口设计

### 2.1 新增接口 `IERC6551Registry.sol`
在 `contracts/src/interfaces/IERC6551Registry.sol` 中定义标准 ERC-6551 注册表接口：
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IERC6551Registry {
    event ERC6551AccountCreated(
        address account,
        address indexed implementation,
        bytes32 salt,
        uint256 chainId,
        address indexed tokenContract,
        uint256 indexed tokenId
    );

    function createAccount(
        address implementation,
        bytes32 salt,
        uint256 chainId,
        address tokenContract,
        uint256 tokenId
    ) external returns (address);

    function account(
        address implementation,
        bytes32 salt,
        uint256 chainId,
        address tokenContract,
        uint256 tokenId
    ) external view returns (address);
}
```

### 2.2 重构 `PaymentEscrow.sol`

#### 2.2.1 新增数据结构与映射
- 引入通道状态结构体 `ChannelLock`：
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
- 新增映射表：
```solidity
mapping(bytes32 => ChannelLock) public channels;
```

#### 2.2.2 EIP-712 域定义与常量
- 哈希常量定义：
```solidity
bytes32 public constant CHANNEL_SETTLE_TYPEHASH = keccak256("ChannelSettle(bytes32 channelId,uint256 accumulatedAmount)");
bytes32 public hashDomainSeparator; // 或者是 DOMAIN_SEPARATOR
```
- 在构造函数中初始化：
```solidity
// 我们可以在构造函数里计算 DOMAIN_SEPARATOR
```

#### 2.2.3 状态通道核心函数
1. **`lockChannel`**:
   ```solidity
   function lockChannel(
       uint256 agentId,
       uint256 amount,
       uint256 duration
   ) external returns (bytes32 channelId);
   ```
   - 校验：`amount > 0`，`duration > 0`。
   - 划转代币：从 `msg.sender` 转账 `amount` USDC 到 `PaymentEscrow` 合约中。
   - 生成 `channelId`：使用哈希算法保证唯一性：
     ```solidity
     channelId = keccak256(abi.encodePacked(
         msg.sender,
         agentId,
         amount,
         block.timestamp,
         _nonce++
     ));
     ```
   - 存储：在 `channels` 映射中保存 `ChannelLock` 实例，状态设为 `PaymentStatus.Locked`。
   - 触发事件：`ChannelLocked`。

2. **`batchSettle`**:
   ```solidity
   function batchSettle(
       bytes32 channelId,
       uint256 accumulatedAmount,
       bytes calldata signature,
       address agentOwner
   ) external onlySettler;
   ```
   - 校验：
     - 通道必须存在且状态为 `PaymentStatus.Locked`。
     - 结算时，`accumulatedAmount` 必须在合理区间：`0 < accumulatedAmount <= lock.maxAmount`。
     - 校验通道未到期：`block.timestamp <= lock.expiresAt`。
     - `agentOwner` 不能是零地址。
   - EIP-712 签名恢复：
     - 构建 hashStruct：
       ```solidity
       bytes32 hashStruct = keccak256(abi.encode(
           CHANNEL_SETTLE_TYPEHASH,
           channelId,
           accumulatedAmount
       ));
       ```
     - 生成 digest，并使用 `ECDSA.recover(digest, signature)` 恢复签名者地址。
     - 断言签名者等于 `lock.payer`。
   - 资金划转（遵循 CEI 规范）：
     - 将通道状态置为 `PaymentStatus.Released`。
     - 设置 `lock.settledAmount = accumulatedAmount`。
     - 向 `agentOwner` 转账 `accumulatedAmount` USDC。
     - 若有余额（`lock.maxAmount > accumulatedAmount`），将差额 `lock.maxAmount - accumulatedAmount` 退还给 `lock.payer`。
     - 触发事件：`ChannelSettled`。

3. **`refundChannel`**:
   ```solidity
   function refundChannel(bytes32 channelId) external;
   ```
   - 校验：通道状态必须为 `PaymentStatus.Locked` 且当前时间已过到期时间（`block.timestamp > lock.expiresAt`）。
   - 资金划转（遵循 CEI 规范）：
     - 将通道状态置为 `PaymentStatus.Refunded`。
     - 退还 `lock.maxAmount` USDC 给 `lock.payer`。
     - 触发事件：`ChannelRefunded`。

## 3. 单元测试设计
在 `PaymentEscrowTest.t.sol` 中：
- 构建测试私钥：`uint256 payerPrivateKey = 0xA11CE;`
- 生成 payer 的地址，为其 mint 代币并授权。
- 调用 `lockChannel` 创建通道。
- 构造 EIP-712 签名：
  - 在 Solidity/Foundry 中，使用 `vm.sign(payerPrivateKey, digest)` 获得签名 `(v, r, s)`，并组合成 `bytes signature`。
- 模拟 `settler` 调用 `batchSettle`，断言代币转移到 `agentOwner`，差额部分退回给 `payer`。
- 新增逆向测试和边界用例：签名不匹配时 revert、超时后结算 revert、未超时退款 revert 等。
