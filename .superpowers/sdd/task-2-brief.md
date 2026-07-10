# Task 2 Brief: Implement `splitSettle` in PaymentEscrow.sol

**Goal**: Implement the `splitSettle` function to distribute funds to model provider, platform treasury, and the agent's TBA, and refund the remainder to the user.

**Files**:
- Modify: `contracts/src/payment/PaymentEscrow.sol`
- Test: `contracts/test/PaymentEscrowTest.t.sol`

**Instructions**:
1. Add `ChannelSplitSettled` event to `PaymentEscrow.sol`:
   ```solidity
   event ChannelSplitSettled(
       bytes32 indexed channelId,
       uint256 agentPayout,
       uint256 modelPayout,
       uint256 platformFee,
       address modelProvider,
       address treasury
   );
   ```
2. Implement the `splitSettle` function:
   ```solidity
   function splitSettle(
       bytes32 channelId,
       uint256 accumulatedAmount,
       uint256 modelCost,
       uint256 serviceFee,
       address modelProvider,
       address treasury,
       uint16  platformBps,
       uint256 holdAmount,
       uint256 nonce,
       uint256 expiration,
       bytes   calldata signature
   ) external onlySettler
   ```
   Detailed logic:
   - Validate addresses, status is Locked, channel is not expired, accumulatedAmount > 0 and <= maxAmount, holdAmount == maxAmount.
   - Verify EIP-712 signature against ChannelHold hash struct. Signer must be the payer.
   - Calculate platform fee: `platformFee = accumulatedAmount * platformBps / 10000`.
   - Verify that `modelCost + serviceFee + platformFee <= accumulatedAmount`.
   - Update lock status to `Released`, and set `settledAmount = accumulatedAmount`.
   - Safely transfer `modelCost` to `modelProvider` (if > 0), `platformFee` to `treasury` (if > 0).
   - Resolve agent's TBA address dynamically using `erc6551Registry.account(...)`.
   - Transfer the remaining `agentPayout = accumulatedAmount - modelCost - platformFee` to the TBA (if > 0).
   - Refund the remainder `lock.maxAmount - accumulatedAmount` back to the payer.
   - Emit `ChannelSplitSettled`.
3. Add a Foundry unit test `testSplitSettle()` in `contracts/test/PaymentEscrowTest.t.sol` to verify the splits, signature validation, transfers, and remainder refunds.
4. Verify all tests pass: `forge test`
5. Commit changes.
