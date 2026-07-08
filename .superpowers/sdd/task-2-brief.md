# Task 2 Brief: ERC-6551 TBA 智能体收款执行账户部署与防御

## 目标
实现标准的 ERC-6551 智能体 TBA 账户合约（`AgentTokenBoundAccount.sol`），并在托管合约（`PaymentEscrow.sol`）的结算中绑定专属 TBA 验证防护，拦截非 TBA 的越权接收地址。

## 涉及文件
- 新增: `contracts/src/interfaces/IERC6551Account.sol`
- 新增: `contracts/src/payment/AgentTokenBoundAccount.sol`
- 修改: `contracts/src/payment/PaymentEscrow.sol`
- 修改: `contracts/test/PaymentEscrowTest.t.sol`

## 全局约束
- Solidity 统一为 `0.8.20`。
- 不得使用 TODO 或占位符。
- TBA 划出资金必须只有其对应的 Agent NFT 的 owner 拥有执行权限。

## 需求与步骤

### 1. 编写 ERC-6551 账户合约
- 创建 `contracts/src/interfaces/IERC6551Account.sol`：定义标准账户的 `isValidSigner`，`token`，`state` 和 `receive()` 接口。
- 创建 `contracts/src/payment/AgentTokenBoundAccount.sol`：
  - 挂接 `IERC6551Account`。
  - 实现 `token()`：返还持有该 TBA 账户的 NFT 元组 `(uint256 chainId, address tokenContract, uint256 tokenId)`。为简化解析，可在创建时通过 `immutable` 构造参数存入这三个变量。
  - 实现 `execute(address to, uint256 value, bytes calldata data) external payable returns (bytes memory)`：
    - **安全校验**：仅允许此 TBA 绑定的 Agent NFT 拥有者（`ownerOf(tokenId)`）调用。可通过反查 `tokenContract.ownerOf(tokenId)` 检验 `msg.sender` 权限。非 owner 触发则 Revert。
    - 执行调用并返回执行结果。

### 2. 托管结算绑定 TBA 防御
- 修改 `PaymentEscrow.sol`：
  - 增加状态变量：
    ```solidity
    address public erc6551Registry;
    address public tbaImplementation;
    address public agentIdentityRegistry;
    ```
  - 在 `initialize` / 构造函数中支持对这三个变量的配置。
  - 在 `batchSettle` 与原有的 `releasePayment` 结算转账前，增加安全防线：
    - 调用 `IERC6551Registry(erc6551Registry).account(tbaImplementation, bytes32(0), block.chainid, agentIdentityRegistry, lock.agentId)` 计算专属 TBA 地址。
    - 校验接收方 `agentOwner` 必须等于该计算出来的专属 TBA 地址，如果不匹配，抛出 `InvalidAddress()` 异常拦截结算。

### 3. 单元测试覆盖
- 在 `PaymentEscrowTest.t.sol` 中：
  - 编写一个 Mock ERC-6551 Registry 合约（用于 `account` 反查和 `createAccount` 自动部署模拟）。
  - 在 `setUp` 阶段部署 `AgentTokenBoundAccount` 实现模板和 Mock Registry。
  - 编写 `test_BatchSettleToTBARecipientSuccess()`：校验批量结算时，结算金额成功转入对应的 TBA 账户，剩余资金返回 payer。
  - 编写 `test_BatchSettleToNonTBARecipientReverts()`：校验当 `agentOwner` 传入普通 EOA 地址时，被托管合约成功 Revert 拦截。
  - 编写 `test_TBAExecuteOnlyOwner()`：校验由 EOA 尝试调用 TBA 账户的 `execute` 转移资金被 Revert 拦截，而 NFT owner 调用则成功通过。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test -v
```
要求：所有测试编译无误并 100% 通过。
