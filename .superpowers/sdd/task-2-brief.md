# Task 2 Brief: 信誉与支付托管合约开发 (PaymentEscrow & ReputationRegistry)

## 目标
实现 `ReputationRegistry.sol`（信誉评价合约，仅允许付款方对 Agent 评分）和 `PaymentEscrow.sol`（非托管式资金锁定、超时退款、以及由 Settler 验证后释放资金）。

## 涉及文件
- 新增/修改: `contracts/src/identity/ReputationRegistry.sol`
- 新增: `contracts/src/payment/PaymentEscrow.sol`
- 新增: `contracts/test/MockERC20.sol` (如果未包含，编写一个极简 Mock 用作 USDC)
- 新增: `contracts/test/PaymentEscrowTest.t.sol`

## 全局约束
- 链基础: Base Sepolia (Chain ID: 84532)
- 语言和框架: Solidity + Foundry (EntryPoint v0.7 / OpenZeppelin v5)

## 需求与步骤

### 1. ReputationRegistry.sol
- **与托管合约绑定**：引入 `IPaymentEscrow` 接口，在提交反馈时校验 `paymentEscrow.hasPaid(msg.sender, agentId)`，必须返回 `true` 才允许提交（防刷分）。
- **字段和机制**：
  - 评分范围 1-5。包含任务哈希 (taskHash) 以及是否完成 (taskCompleted)。
  - 提供 `getReputation(uint256 agentId)` 查询平均评分和评价总数。
  - 提供 `getRecords` 或类似结构返回评价明细。
  - 自定义错误：如评分超限抛出 `InvalidScore()`，未付款抛出 `NotPaid()`。

### 2. PaymentEscrow.sol
- **非托管支付托管**：
  - 拥有 `IERC20 public paymentToken`（初始化传入，例如 mock USDC）。
  - 拥有 `ValidationRegistry public validationRegistry` 用来做 TEE Proof 校验路由。
  - 拥有 `address public settler`（仅此地址可调用 `releasePayment`）。
- **核心函数**：
  - `lockPayment(uint256 agentId, uint256 amount, bytes32 requestHash, uint256 duration)`:
    - 校验 `amount > 0`。
    - 将 `paymentToken` 从 `msg.sender` 划转至当前合约。
    - 记录锁定结构 `PaymentLock`，状态设为 `Locked`，到期时间为 `block.timestamp + duration`。
    - 返回唯一的 `lockId`（使用 `keccak256(abi.encodePacked(...))` 生成）。
  - `releasePayment(bytes32 lockId, bytes calldata proof, address agentOwner)`:
    - **仅限 `settler` 调用** (使用 `onlySettler` modifier)。
    - 确保 `lock.status == PaymentStatus.Locked` 且未超时。
    - 调用 `validationRegistry.validateProof(lock.agentId, "TEE", proof)` 必须返回 `true`。
    - 标记该 lock 为 `Released`。
    - 标记 payer 对该 agentId 已有付款记录（即 `hasPaid[payer][agentId] = true`）。
    - 将锁定资金划转给传入的 `agentOwner`。
  - `refund(bytes32 lockId)`:
    - 任何人都可以触发，但必须在锁定超时之后（`block.timestamp > lock.expiresAt`）且状态为 `Locked` 时。
    - 标记为 `Refunded`，并将资金退回 `payer`。
- **自定义错误**：
  - 例如：`NotSettler()`, `InvalidStatus()`, `LockExpired()`, `LockNotExpired()`, `ProofValidationFailed()`, `TransferFailed()`。

### 3. 测试与 TDD 验证
- 编写 `contracts/test/PaymentEscrowTest.t.sol`。
- 测试覆盖：
  - 正常的资金锁定、验证 Proof 后释放。
  - 未超时的退款拦截，超时的成功退款。
  - 非 Settler 触发释放报错拦截。
  - 验证付款记录是否正确传递，使得 `ReputationRegistry` 顺利通过付款判断并提交反馈。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test
```
要求：所有测试正常通过。
