# Task 1 Brief: Fix EIP-712 Domain Name & Align Tests

**Goal**: Align EIP-712 domain name in contract to `"AgentPay"`.

**Files**:
- Modify: `contracts/src/payment/PaymentEscrow.sol:131`
- Test: `contracts/test/PaymentEscrowTest.t.sol`

**Instructions**:
1. Run current contract tests: `cd contracts && forge test` to verify they pass.
2. Change `"PaymentEscrow"` to `"AgentPay"` in `contracts/src/payment/PaymentEscrow.sol` constructor.
3. If any test fails, update the test code's domain name to `"AgentPay"` (in `PaymentEscrowTest.t.sol`).
4. Make sure all tests pass.
5. Commit the changes.
