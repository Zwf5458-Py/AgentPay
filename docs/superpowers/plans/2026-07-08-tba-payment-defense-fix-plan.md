# ERC-6551 TBA 修复与优化实现计划

**Goal:** 修复 TBA `state()` 一致性问题，实现错误回滚冒泡优化，将 PaymentEscrow 静态地址字段改写为 `immutable`，并清理测试冗余变量及补充单测。

**Tech Stack:** Solidity 0.8.20, Foundry

---

### Task 1: 修复 TBA 状态一致性与错误冒泡 (`AgentTokenBoundAccount.sol`)

**Files:**
- Modify: `contracts/src/payment/AgentTokenBoundAccount.sol`

- [ ] **Step 1: 修改合约属性与方法**
1. 增加 `uint256 private _state;`
2. 重构 `state()` 方法，去掉 `pure`，改写为 `view` 并返回 `_state`
3. 重构 `execute` 方法，在成功执行外部 `call` 后递增 `_state` (`_state++`)。
4. 重构 `execute` 方法外部调用失败的报错，改写为 Solidity inline assembly 冒泡 revert。

修改后代码：
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "../interfaces/IERC6551Account.sol";
import "@openzeppelin/contracts/token/ERC721/IERC721.sol";

contract AgentTokenBoundAccount is IERC6551Account {
    uint256 public immutable chainIdVal;
    address public immutable tokenContractVal;
    uint256 public immutable tokenIdVal;
    uint256 private _state;

    error NotOwner();

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
        return _state;
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
            assembly {
                revert(add(result, 32), mload(result))
            }
        }

        _state++;

        return result;
    }
}
```

- [ ] **Step 2: 编译测试**
运行：`forge build`
Expected: 编译通过。

---

### Task 2: 优化托管合约静态地址属性为 immutable (`PaymentEscrow.sol`)

**Files:**
- Modify: `contracts/src/payment/PaymentEscrow.sol`

- [ ] **Step 1: 修改合约变量定义**
将 `erc6551Registry`, `tbaImplementation`, `agentIdentityRegistry` 从：
```solidity
    address public erc6551Registry;
    address public tbaImplementation;
    address public agentIdentityRegistry;
```
修改为：
```solidity
    address public immutable erc6551Registry;
    address public immutable tbaImplementation;
    address public immutable agentIdentityRegistry;
```
由于它们在构造函数中赋值且不再更改，改用 `immutable` 能节约大量的 Gas。

- [ ] **Step 2: 编译测试**
运行：`forge build`
Expected: 编译通过。

---

### Task 3: 清理 Mock Registry 冗余变量并追加测试用例 (`PaymentEscrowTest.t.sol`)

**Files:**
- Modify: `contracts/test/PaymentEscrowTest.t.sol`

- [ ] **Step 1: 清理冗余变量**
移除 `MockERC6551Registry` 合约中的 `mapping(address => mapping(bytes32 => ...)) private _accounts;` 变量。

- [ ] **Step 2: 追加 test_TBAExecuteIncrementsState**
在 `PaymentEscrowTest.t.sol` 中添加：
```solidity
    // 25. 测试 TBA execute 成功后能够使 state 递增
    function test_TBAExecuteIncrementsState() public {
        uint256 tbaBalance = 100 * 10**6;
        usdc.mint(agentOwner, tbaBalance);

        address nftOwner = address(0x3);
        
        bytes memory callData = abi.encodeWithSelector(
            IERC20.transfer.selector,
            address(0x555),
            50 * 10**6
        );

        uint256 stateBefore = AgentTokenBoundAccount(payable(agentOwner)).state();

        vm.prank(nftOwner);
        AgentTokenBoundAccount(payable(agentOwner)).execute(address(usdc), 0, callData);

        uint256 stateAfter = AgentTokenBoundAccount(payable(agentOwner)).state();

        assertEq(stateAfter, stateBefore + 1);
    }
```

- [ ] **Step 3: 运行 forge test 验证所有测试**
运行：`forge test -v`
Expected: 所有测试 100% 跑通，无错误。

---

### Task 4: 提交代码与追加报告

- [ ] **Step 1: 追加修复结果到报告**
在 `.superpowers/sdd/task-2-report.md` 末尾追加修复详情。

- [ ] **Step 2: 提交所有代码改动到 git**
- [ ] **Step 3: 向 parent 报告最终状态与 commit hash**
