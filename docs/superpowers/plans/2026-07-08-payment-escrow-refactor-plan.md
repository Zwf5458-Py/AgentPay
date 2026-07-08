# PaymentEscrow Refactor & Status Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `PaymentEscrow.sol` to support `ChannelLock` status channels with EIP-712 batch settlement and refund functions, implement `IERC6551Registry.sol`, and add corresponding unit tests in `PaymentEscrowTest.t.sol` to ensure they all pass.

**Architecture:** 
- Standard ERC-6551 Registry interface `IERC6551Registry`.
- `ChannelLock` data structure mapping channel IDs to locks.
- EIP-712 structured data hashing using `DOMAIN_SEPARATOR` and `CHANNEL_SETTLE_TYPEHASH`.
- `ECDSA.recover` to verify EIP-712 signatures.
- Re-entrancy prevention using Checks-Effects-Interactions (CEI).

**Tech Stack:** Solidity 0.8.20, Foundry, OpenZeppelin Contracts (v5.x).

## Global Constraints
- Solidity version must be exactly `0.8.20`.
- No TODOs or placeholder comments are allowed.
- Follow Checks-Effects-Interactions (CEI) to prevent re-entrancy.

---

### Task 1: Create IERC6551Registry.sol Interface

**Files:**
- Create: `contracts/src/interfaces/IERC6551Registry.sol`

**Interfaces:**
- Produces: `IERC6551Registry` with `createAccount` and `account` signatures.

- [ ] **Step 1: Write IERC6551Registry.sol**

Create the file `/Users/oraclez/code/AgentPay/contracts/src/interfaces/IERC6551Registry.sol`:
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

- [ ] **Step 2: Verify compilation**
Run: `forge build` in `/Users/oraclez/code/AgentPay/contracts`
Expected: Successful compilation without errors.

- [ ] **Step 3: Commit**
```bash
git add src/interfaces/IERC6551Registry.sol
git commit -m "feat: add IERC6551Registry interface"
```

---

### Task 2: Define ChannelLock, Events, and DOMAIN_SEPARATOR in PaymentEscrow.sol

**Files:**
- Modify: `contracts/src/payment/PaymentEscrow.sol`

**Interfaces:**
- Produces: `ChannelLock` struct, `channels` mapping, `CHANNEL_SETTLE_TYPEHASH`, `DOMAIN_SEPARATOR`, and `lockChannel` function.

- [ ] **Step 1: Declare structural components and lockChannel**
Update `/Users/oraclez/code/AgentPay/contracts/src/payment/PaymentEscrow.sol` to define the struct, state variables, domain separator computation in constructor, and the `lockChannel` implementation.

```solidity
    struct ChannelLock {
        address payer;
        uint256 agentId;
        uint256 maxAmount;
        uint256 settledAmount;
        uint256 expiresAt;
        PaymentStatus status;
    }

    mapping(bytes32 => ChannelLock) public channels;

    bytes32 public constant CHANNEL_SETTLE_TYPEHASH = keccak256("ChannelSettle(bytes32 channelId,uint256 accumulatedAmount)");
    bytes32 public immutable DOMAIN_SEPARATOR;

    event ChannelLocked(
        bytes32 indexed channelId,
        address indexed payer,
        uint256 indexed agentId,
        uint256 maxAmount,
        uint256 expiresAt
    );
```

- [ ] **Step 2: Add lockChannel function implementation**
```solidity
    function lockChannel(
        uint256 agentId,
        uint256 amount,
        uint256 duration
    ) external returns (bytes32 channelId) {
        if (amount == 0) revert InvalidAmount();
        if (duration == 0) revert InvalidDuration();

        paymentToken.safeTransferFrom(msg.sender, address(this), amount);

        channelId = keccak256(abi.encodePacked(
            msg.sender,
            agentId,
            amount,
            block.timestamp,
            _nonce++
        ));

        uint256 expiresAt = block.timestamp + duration;

        channels[channelId] = ChannelLock({
            payer: msg.sender,
            agentId: agentId,
            maxAmount: amount,
            settledAmount: 0,
            expiresAt: expiresAt,
            status: PaymentStatus.Locked
        });

        emit ChannelLocked(channelId, msg.sender, agentId, amount, expiresAt);
    }
```

- [ ] **Step 3: Compile to verify syntax**
Run: `forge build`
Expected: Successful compilation.

- [ ] **Step 4: Commit**
```bash
git add src/payment/PaymentEscrow.sol
git commit -m "feat: add ChannelLock struct, state variables and lockChannel function"
```

---

### Task 3: Implement batchSettle with EIP-712 Signature Verification

**Files:**
- Modify: `contracts/src/payment/PaymentEscrow.sol`

**Interfaces:**
- Produces: `batchSettle` function.

- [ ] **Step 1: Update imports to include ECDSA**
Add `import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";` in `/Users/oraclez/code/AgentPay/contracts/src/payment/PaymentEscrow.sol`.

- [ ] **Step 2: Implement batchSettle**
Add the `batchSettle` function:
```solidity
    event ChannelSettled(
        bytes32 indexed channelId,
        address indexed agentOwner,
        uint256 settledAmount
    );

    error InvalidSignature();
    error ChannelExpired();

    function batchSettle(
        bytes32 channelId,
        uint256 accumulatedAmount,
        bytes calldata signature,
        address agentOwner
    ) external onlySettler {
        if (agentOwner == address(0)) revert InvalidAddress();

        ChannelLock storage lock = channels[channelId];
        if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
        if (block.timestamp > lock.expiresAt) revert ChannelExpired();
        if (accumulatedAmount == 0 || accumulatedAmount > lock.maxAmount) revert InvalidAmount();

        // 1. Verify EIP-712 signature
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_SETTLE_TYPEHASH,
            channelId,
            accumulatedAmount
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            DOMAIN_SEPARATOR,
            hashStruct
        ));
        address signer = ECDSA.recover(digest, signature);
        if (signer != lock.payer) revert InvalidSignature();

        // 2. State update (CEI)
        lock.status = PaymentStatus.Released;
        lock.settledAmount = accumulatedAmount;

        // 3. Asset transfer
        paymentToken.safeTransfer(agentOwner, accumulatedAmount);
        
        uint256 remainder = lock.maxAmount - accumulatedAmount;
        if (remainder > 0) {
            paymentToken.safeTransfer(lock.payer, remainder);
        }

        emit ChannelSettled(channelId, agentOwner, accumulatedAmount);
    }
```

- [ ] **Step 3: Verify compilation**
Run: `forge build`
Expected: Successful compilation.

- [ ] **Step 4: Commit**
```bash
git add src/payment/PaymentEscrow.sol
git commit -m "feat: implement batchSettle function with EIP-712 validation"
```

---

### Task 4: Implement refundChannel Function

**Files:**
- Modify: `contracts/src/payment/PaymentEscrow.sol`

**Interfaces:**
- Produces: `refundChannel` function.

- [ ] **Step 1: Add refundChannel implementation**
Add `refundChannel` function:
```solidity
    event ChannelRefunded(
        bytes32 indexed channelId,
        address indexed payer,
        uint256 refundedAmount
    );

    error ChannelNotExpired();

    function refundChannel(bytes32 channelId) external {
        ChannelLock storage lock = channels[channelId];
        if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
        if (block.timestamp <= lock.expiresAt) revert ChannelNotExpired();

        lock.status = PaymentStatus.Refunded;

        paymentToken.safeTransfer(lock.payer, lock.maxAmount);

        emit ChannelRefunded(channelId, lock.payer, lock.maxAmount);
    }
```

- [ ] **Step 2: Verify compilation**
Run: `forge build`
Expected: Successful compilation.

- [ ] **Step 3: Commit**
```bash
git add src/payment/PaymentEscrow.sol
git commit -m "feat: implement refundChannel function"
```

---

### Task 5: Add Unit Tests in PaymentEscrowTest.t.sol

**Files:**
- Modify: `contracts/test/PaymentEscrowTest.t.sol`

- [ ] **Step 1: Write test_ChannelBatchSettle and related tests**
Define state variables for key generation in setup or within the test, construct the digest manually in Solidity, sign it using `vm.sign`, assemble the signature, and invoke the escrow functions.

```solidity
    // Channel Settle Types
    bytes32 public constant CHANNEL_SETTLE_TYPEHASH = keccak256("ChannelSettle(bytes32 channelId,uint256 accumulatedAmount)");

    // Test Channel Batch Settle Success
    function test_ChannelBatchSettle() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        // Mint and approve
        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        // Lock Channel
        uint256 maxAmount = 500 * 10**6;
        uint256 duration = 3600;
        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, maxAmount, duration);

        // Check channel attributes
        (
            address channelPayer,
            uint256 channelAgentId,
            uint256 channelMaxAmount,
            uint256 channelSettledAmount,
            uint256 channelExpiresAt,
            PaymentEscrow.PaymentStatus channelStatus
        ) = escrow.channels(channelId);
        assertEq(channelPayer, customPayer);
        assertEq(channelAgentId, agentId);
        assertEq(channelMaxAmount, maxAmount);
        assertEq(channelSettledAmount, 0);
        assertEq(channelExpiresAt, block.timestamp + duration);
        assertEq(uint256(channelStatus), 1); // Locked

        // Create signature offline (using EIP-712 digest)
        uint256 accumulatedAmount = 300 * 10**6;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_SETTLE_TYPEHASH,
            channelId,
            accumulatedAmount
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // Settle
        uint256 agentOwnerBalanceBefore = usdc.balanceOf(agentOwner);
        uint256 payerBalanceBefore = usdc.balanceOf(customPayer);

        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, signature, agentOwner);

        // Verify status and balances
        (,,,,, channelStatus) = escrow.channels(channelId);
        assertEq(uint256(channelStatus), 2); // Released
        
        assertEq(usdc.balanceOf(agentOwner), agentOwnerBalanceBefore + accumulatedAmount);
        assertEq(usdc.balanceOf(customPayer), payerBalanceBefore + (maxAmount - accumulatedAmount));
    }
```

- [ ] **Step 2: Write negative tests (invalid signature, refund logic, etc.)**
Include:
- `test_ChannelBatchSettleInvalidSignatureReverts`
- `test_ChannelRefundSuccess`
- `test_ChannelRefundNotExpiredReverts`
- `test_ChannelBatchSettleExpiredReverts`

- [ ] **Step 3: Run forge test**
Run: `forge test --match-test test_Channel -v`
Expected: All channel-related tests pass.

- [ ] **Step 4: Commit and finalize**
```bash
git add test/PaymentEscrowTest.t.sol
git commit -m "test: add channel batch settle and refund tests"
```
