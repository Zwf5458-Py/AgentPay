# Task 2 Report: Implement `splitSettle` in PaymentEscrow.sol

## Goal
Implement the `splitSettle` function in `PaymentEscrow.sol` to enable settling a status channel with a multi-way fee distribution (to model provider, treasury, and the agent's TBA), refunding any unused lock balance to the payer. Add corresponding unit tests in `PaymentEscrowTest.t.sol` to guarantee logic verification.

## Implementation Details

### Contract Changes (`contracts/src/payment/PaymentEscrow.sol`)
1. **Event Definition**: Added the `ChannelSplitSettled` event.
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
2. **`splitSettle` Function**: Added the function with `onlySettler` modifier:
   - Validated `modelProvider` and `treasury` are non-zero addresses.
   - Performed channel status validation (`Locked` status, not expired, `accumulatedAmount` > 0 and <= `maxAmount`, `holdAmount == maxAmount`).
   - Verified EIP-712 signature of `ChannelHold` struct against the `lock.payer`.
   - Calculated platform fee (`accumulatedAmount * platformBps / 10000`).
   - Verified `modelCost + serviceFee + platformFee <= accumulatedAmount`.
   - Dynamically resolved the agent's TBA using ERC-6551 Registry.
   - Transferred `modelCost` to `modelProvider`, `platformFee` to `treasury`, remaining `agentPayout` (`accumulatedAmount - modelCost - platformFee`) to the TBA, and refunded the remainder (`maxAmount - accumulatedAmount`) to the payer.
   - Emitted `ChannelSplitSettled`.

### Test Coverage (`contracts/test/PaymentEscrowTest.t.sol`)
Added 7 test cases covering the new logic:
1. `test_SplitSettleSuccess`: Verifies correct split amounts distribution, Dynamic TBA resolution, refunds, and `ChannelSplitSettled` event emission.
2. `test_SplitSettleZeroAddressReverts`: Verifies passing `address(0)` for `modelProvider` or `treasury` reverts with `InvalidAddress`.
3. `test_SplitSettleNonSettlerReverts`: Verifies non-settler reverts with `NotSettler`.
4. `test_SplitSettleInvalidSignatureReverts`: Verifies forged signature reverts with `InvalidSignature`.
5. `test_SplitSettleExceedAccumulatedAmountReverts`: Verifies fee allocation sum exceeding `accumulatedAmount` reverts with `InvalidAmount`.
6. `test_SplitSettleInvalidStatusReverts`: Verifies double-settlement reverts with `InvalidStatus`.
7. `test_SplitSettleExpiredReverts`: Verifies settling after channel expiration reverts with `ChannelExpired`.

## Test Results
All 40 unit tests (including the 7 new splitSettle tests) pass successfully.
- Test Run Command: `forge test`
- Summary: 33 tests in `PaymentEscrowTest` and 7 tests in `AgentIdentityTest` passed (40 total).

## Concerns / Notes
- None. The implementation aligns perfectly with the requirements in `task-2-brief.md`.
