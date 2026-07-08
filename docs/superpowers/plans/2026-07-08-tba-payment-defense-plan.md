# ERC-6551 TBA 智能体收款执行账户部署与防御实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ERC-6551 TBA 账户（`AgentTokenBoundAccount.sol`），并在 `PaymentEscrow.sol` 托管结算中进行专属 TBA 校验防御，拦截非 TBA 接收地址。

**Architecture:** 
1. 编写 IERC6551Account 接口；
2. 编写 AgentTokenBoundAccount 合约，在 execute 中实现仅绑定 NFT owner 的权限验证；
3. 修改 PaymentEscrow 的 constructor，配置 registry, implementation 和 agentIdentityRegistry，并在 releasePayment 和 batchSettle 中计算出 TBA 地址，校验接收者地址必须与该 TBA 地址一致；
4. 在 PaymentEscrowTest.t.sol 中编写 Mock Registry，并在 setUp 中重构部署过程，增加测试验证结算流入 TBA、非 TBA 被拒绝以及 TBA execute 的 owner 隔离权限。

**Tech Stack:** Solidity 0.8.20, Foundry (Forge)

## Global Constraints
- Solidity 版本统一为 `0.8.20`
- 不得使用 TODO 或任何占位符
- TBA 划出资金必须只有其对应的 Agent NFT 的 owner 拥有执行权限
- 所有测试通过 `forge test` 100% 跑通

---

### Task 1: 编写 IERC6551Account 接口

**Files:**
- Create: `contracts/src/interfaces/IERC6551Account.sol`

**Interfaces:**
- Produces: `IERC6551Account` 接口

- [ ] **Step 1: 编写接口代码**

编写 `contracts/src/interfaces/IERC6551Account.sol`：
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IERC6551Account {
    receive() external payable;

    function token()
        external
        view
        returns (
            uint256 chainId,
            address tokenContract,
            uint256 tokenId
        );

    function state() external view returns (uint256);

    function isValidSigner(address signer, bytes calldata context)
        external
        view
        returns (bytes4 magicValue);
}
```

- [ ] **Step 2: 编译验证**

运行：`forge build`（在 contracts 目录下）
Expected: 编译通过，无关于 IERC6551Account 的错误。

- [ ] **Step 3: 提交改动**

```bash
git add contracts/src/interfaces/IERC6551Account.sol
git commit -m "feat: add IERC6551Account interface"
```

---

### Task 2: 编写 AgentTokenBoundAccount 账户合约

**Files:**
- Create: `contracts/src/payment/AgentTokenBoundAccount.sol`

**Interfaces:**
- Consumes: `contracts/src/interfaces/IERC6551Account.sol`
- Produces: `AgentTokenBoundAccount` 合约，包含 `execute` 方法

- [ ] **Step 1: 编写合约实现**

在 `contracts/src/payment/AgentTokenBoundAccount.sol` 中编写：
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "../interfaces/IERC6551Account.sol";
import "@openzeppelin/contracts/token/ERC721/IERC721.sol";

contract AgentTokenBoundAccount is IERC6551Account {
    uint256 public immutable chainIdVal;
    address public immutable tokenContractVal;
    uint256 public immutable tokenIdVal;

    error NotOwner();
    error CallFailed();

    constructor(uint256 _chainId, address _tokenContract, uint256 _tokenId) {
        chainIdVal = _chainId;
        tokenContractVal = _tokenContract;
        tokenIdVal = _tokenId;
    }

    receive() external payable override {}

    function token()
        external
        view
        override
        returns (
            uint256,
            address,
            uint256
        )
    {
        return (chainIdVal, tokenContractVal, tokenIdVal);
    }

    function state() external view override returns (uint256) {
        return 0;
    }

    function isValidSigner(address signer, bytes calldata)
        external
        view
        override
        returns (bytes4 magicValue)
    {
        if (signer == IERC721(tokenContractVal).ownerOf(tokenIdVal)) {
            return IERC6551Account.isValidSigner.selector;
        }
        return bytes4(0);
    }

    function execute(
        address to,
        uint256 value,
        bytes calldata data
    ) external payable returns (bytes memory) {
        if (msg.sender != IERC721(tokenContractVal).ownerOf(tokenIdVal)) {
            revert NotOwner();
        }

        (bool success, bytes memory result) = to.call{value: value}(data);
        if (!success) {
            revert CallFailed();
        }

        return result;
    }
}
```

- [ ] **Step 2: 编译验证**

运行：`forge build`
Expected: 编译通过。

- [ ] **Step 3: 提交改动**

```bash
git add contracts/src/payment/AgentTokenBoundAccount.sol
git commit -m "feat: add AgentTokenBoundAccount ERC-6551 implementation"
```

---

### Task 3: 修改 PaymentEscrow.sol 以集成 TBA 校验

**Files:**
- Modify: `contracts/src/payment/PaymentEscrow.sol`

**Interfaces:**
- Consumes: `contracts/src/interfaces/IERC6551Registry.sol`

- [ ] **Step 1: 修改 PaymentEscrow.sol 代码**

我们需要将状态变量、构造函数以及 `releasePayment` 与 `batchSettle` 做修改：
1. 导入 `IERC6551Registry.sol`
2. 添加状态变量 `erc6551Registry`, `tbaImplementation`, `agentIdentityRegistry`
3. 修改构造函数以传入并校验这三个新变量
4. 在 `releasePayment` 和 `batchSettle` 转账结算前计算 `expectedTba` 账户，并核对 `agentOwner`。

- [ ] **Step 2: 运行测试查看编译错误**

运行：`forge build`
Expected: 编译报错（由于 PaymentEscrowTest.t.sol 中的 constructor 传参还没修改）。

- [ ] **Step 3: 提交修改**

```bash
git add contracts/src/payment/PaymentEscrow.sol
git commit -m "feat: integrate ERC-6551 TBA validation into PaymentEscrow"
```

---

### Task 4: 在 PaymentEscrowTest.t.sol 中编写 Mock Registry 并更新 setUp

**Files:**
- Modify: `contracts/test/PaymentEscrowTest.t.sol`

**Interfaces:**
- Consumes: `AgentTokenBoundAccount`, `IERC6551Registry`

- [ ] **Step 1: 在测试文件中加入 MockRegistry 和 辅助合约**

在 `contracts/test/PaymentEscrowTest.t.sol` 文件的最上方或外部定义一个 MockRegistry：
```solidity
contract MockERC6551Registry is IERC6551Registry {
    mapping(address => mapping(bytes32 => mapping(uint256 => mapping(address => mapping(uint256 => address))))) private _accounts;

    function createAccount(
        address implementation,
        bytes32 salt,
        uint256 chainId,
        address tokenContract,
        uint256 tokenId
    ) external override returns (address) {
        address computed = account(implementation, salt, chainId, tokenContract, tokenId);
        _accounts[implementation][salt][chainId][tokenContract][tokenId] = computed;
        emit ERC6551AccountCreated(computed, implementation, salt, chainId, tokenContract, tokenId);
        return computed;
    }

    function account(
        address implementation,
        bytes32 salt,
        uint256 chainId,
        address tokenContract,
        uint256 tokenId
    ) public view override returns (address) {
        bytes32 codeHash = keccak256(
            abi.encodePacked(
                type(AgentTokenBoundAccount).creationCode,
                abi.encode(chainId, tokenContract, tokenId)
            )
        );
        
        bytes32 data = keccak256(
            abi.encodePacked(
                bytes1(0xff),
                address(this),
                salt,
                codeHash
            )
        );
        return address(uint160(uint256(data)));
    }
}
```
并且在 `setUp` 中完成：
- 部署 `AgentIdentityRegistry` 作为 `agentIdentityRegistry`
- 部署 Mock Registry 和 `AgentTokenBoundAccount` 的实现模板合约。
- 重构 `PaymentEscrow` 的实例化参数。

- [ ] **Step 2: 重新编译**

运行：`forge build`
Expected: 编译通过。

- [ ] **Step 3: 提交改动**

```bash
git add contracts/test/PaymentEscrowTest.t.sol
git commit -m "test: add MockERC6551Registry and update setUp to support new PaymentEscrow constructor"
```

---

### Task 5: 编写测试用例并使用 forge test 跑通所有测试

**Files:**
- Modify: `contracts/test/PaymentEscrowTest.t.sol`

- [ ] **Step 1: 在 PaymentEscrowTest 中编写具体的测试函数**

编写：
1. `test_BatchSettleToTBARecipientSuccess()`：结算金额成功转入 TBA 账户，剩余退回 payer。
2. `test_BatchSettleToNonTBARecipientReverts()`：结算时如果 `agentOwner` 不是 TBA 账户地址，则被 `PaymentEscrow` Revert 拦截，抛出 `InvalidAddress()`。
3. `test_TBAExecuteOnlyOwner()`：验证非 owner 尝试调用 TBA `execute` 转移资金被 Revert，而 owner 调用则成功。

- [ ] **Step 2: 运行测试并修复所有发现的问题**

运行：`forge test`（在 contracts 目录下）
Expected: 所有测试 100% 成功。

- [ ] **Step 3: 提交改动**

```bash
git add contracts/test/PaymentEscrowTest.t.sol
git commit -m "test: implement TBA batch settle and execution authorization tests"
```

---

### Task 6: 提交与报告

**Files:**
- Create: `/Users/oraclez/code/AgentPay/.superpowers/sdd/task-2-report.md`

- [ ] **Step 1: 编写详细的执行报告**
  将详细执行报告保存到 `task-2-report.md`。

- [ ] **Step 2: 最终确认**
  确认 git 状态是干净的，所有测试通过。

- [ ] **Step 3: 向 parent 返回结果**
  返回最终状态（DONE 等）及最新的 git commit hash。
