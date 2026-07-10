# Web3 AI Settlement P1 (Core Settlement) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the P1 Core Settlement phase, resolving the EIP-712 domain name bug, adding `splitSettle`三方分账 to the PaymentEscrow contract, and updating the Gateway and AA Bridge to enqueue and execute the multi-party split settlement.

**Architecture:**
- **Smart Contract**: Update `PaymentEscrow.sol` to align the EIP-712 name to `"AgentPay"`. Implement `splitSettle` which validates EIP-712 signatures for `ChannelHold` and distributes the locked funds to the model provider, platform treasury, and the agent's TBA, while returning the remaining funds back to the user.
- **Go Gateway**: Update the X-402 challenge header, extend SQLite queue schema/worker task structure to store EIP-712 pre-authorization parameters and token cost values, and pass them to the AA Bridge when executing the settlement tasks.
- **AA Bridge**: Implement the `/aa/split-settle` endpoint which checks if the TBA is deployed, deploys it if needed, and calls the contract's `splitSettle` method.

**Tech Stack:** Go (Chi, Chi Middleware, modernc.org/sqlite), TypeScript (Fastify, Viem), Solidity (Foundry).

## Global Constraints
- **Platform Name**: `"AgentPay"` (EIP-712 Domain Name)
- **Chain ID**: `84532` (Base Sepolia) / `31337` (Anvil Local)
- **Settler Contract verification**: Only `settler` address is allowed to settle.
- **Fee Rate**: `10` bps (0.1% platform fee)

---

### Task 1: Fix EIP-712 Domain Name & Align Tests

**Files:**
- Modify: `contracts/src/payment/PaymentEscrow.sol:131`
- Test: `contracts/test/PaymentEscrowTest.t.sol`

**Interfaces:**
- Domain name in `PaymentEscrow.sol` must be `"AgentPay"`.

- [ ] **Step 1: Run current contract tests to verify failure/success**
  Run: `cd contracts && forge test`
  Expected: All tests pass (they currently pass because the test setup also used the wrong domain separator or Mock EIP-712 check, or the tests were not checking signatures strictly).

- [ ] **Step 2: Update domain name in PaymentEscrow.sol**
  Modify line 131 of `contracts/src/payment/PaymentEscrow.sol`:
  ```solidity
  keccak256(bytes("AgentPay")),
  ```

- [ ] **Step 3: Verify tests still pass or fail**
  Run: `forge test`
  If any tests fail due to domain mismatch, update the test code's domain name to `"AgentPay"` (usually inside `PaymentEscrowTest.t.sol`'s signature generation logic).

- [ ] **Step 4: Commit changes**
  ```bash
  git add contracts/src/payment/PaymentEscrow.sol
  git commit -m "fix: align EIP-712 domain name in PaymentEscrow constructor"
  ```

---

### Task 2: Implement `splitSettle` in PaymentEscrow.sol

**Files:**
- Modify: `contracts/src/payment/PaymentEscrow.sol`
- Test: `contracts/test/PaymentEscrowTest.t.sol`

**Interfaces:**
- Produces: `splitSettle` function in `PaymentEscrow`
- Produces: `ChannelSplitSettled` event

- [ ] **Step 1: Add interface/event definitions and the function signature**
  Add the event `ChannelSplitSettled` to `contracts/src/payment/PaymentEscrow.sol`:
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
  Add the `splitSettle` function:
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
  ) external onlySettler {
      if (modelProvider == address(0) || treasury == address(0)) revert InvalidAddress();

      ChannelLock storage lock = channels[channelId];
      if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
      if (block.timestamp > lock.expiresAt) revert ChannelExpired();
      if (accumulatedAmount == 0 || accumulatedAmount > lock.maxAmount) revert InvalidAmount();
      if (holdAmount != lock.maxAmount) revert InvalidAmount();

      // Verify EIP-712 ChannelHold signature
      bytes32 hashStruct = keccak256(abi.encode(
          CHANNEL_HOLD_TYPEHASH,
          channelId,
          holdAmount,
          nonce,
          expiration
      ));
      bytes32 digest = keccak256(abi.encodePacked(
          "\x19\x01",
          DOMAIN_SEPARATOR,
          hashStruct
      ));
      address signer = ECDSA.recover(digest, signature);
      if (signer != lock.payer) revert InvalidSignature();

      // Calculate fees and splits
      uint256 platformFee = (accumulatedAmount * platformBps) / 10000;
      require(modelCost + serviceFee + platformFee <= accumulatedAmount, "Total payout exceeds accumulated amount");

      lock.status = PaymentStatus.Released;
      lock.settledAmount = accumulatedAmount;

      // Split transfer payments
      if (modelCost > 0) {
          paymentToken.safeTransfer(modelProvider, modelCost);
      }
      if (platformFee > 0) {
          paymentToken.safeTransfer(treasury, platformFee);
      }
      uint256 agentPayout = accumulatedAmount - modelCost - platformFee;
      if (agentPayout > 0) {
          // agentOwner is expected to be the TBA address of the Agent ID.
          // Get the TBA address from lock.agentId
          address agentOwner = IERC6551Registry(erc6551Registry).account(
              tbaImplementation,
              bytes32(0),
              block.chainid,
              agentIdentityRegistry,
              lock.agentId
          );
          paymentToken.safeTransfer(agentOwner, agentPayout);
      }

      // Refund remainder back to user (payer)
      uint256 remainder = lock.maxAmount - accumulatedAmount;
      if (remainder > 0) {
          paymentToken.safeTransfer(lock.payer, remainder);
      }

      emit ChannelSplitSettled(channelId, agentPayout, modelCost, platformFee, modelProvider, treasury);
  }
  ```

- [ ] **Step 2: Add test case for `splitSettle` in `PaymentEscrowTest.t.sol`**
  Add `testSplitSettle()` to `contracts/test/PaymentEscrowTest.t.sol` to verify signature parsing, fee calculation, transfers to three parties (model provider, platform, TBA), and remainder return.
  
  ```solidity
  function testSplitSettle() public {
      // Mock EIP-712 message signing and test the splitSettle functionality.
      // Assert balance changes for payer, modelProvider, treasury, and agent TBA.
  }
  ```

- [ ] **Step 3: Run the test to verify it passes**
  Run: `forge test --match-test testSplitSettle -v`
  Expected: PASS

- [ ] **Step 4: Commit changes**
  ```bash
  git add contracts/src/payment/PaymentEscrow.sol contracts/test/PaymentEscrowTest.t.sol
  git commit -m "feat: implement splitSettle in PaymentEscrow and add unit tests"
  ```

---

### Task 3: Update Gateway X-402 Middleware Headers & Configs

**Files:**
- Modify: `gateway/internal/middleware/x402.go`
- Test: `gateway/internal/middleware/x402_test.go`

**Interfaces:**
- Extends headers in 402 challenge response:
  - `X-402-Platform-Bps`: `"10"`
  - `X-402-Model-Provider`: value from env `MODEL_PROVIDER_ADDRESS`
  - `X-402-Payment-Methods`: `"crypto-channel,fiat-stripe"`
- Extends CORS allowed headers: `X-402-Platform-Bps`, `X-402-Model-Provider`, `X-402-Payment-Methods` etc.

- [ ] **Step 1: Update X402Middleware trigger402 function**
  Modify `gateway/internal/middleware/x402.go:204-227`:
  Read `MODEL_PROVIDER_ADDRESS` (default Anvil account 2: `0x3C44Cd356a89457491c5443417` or fallback) and `PLATFORM_BPS` (default `"10"`). Add them as Headers:
  ```go
  modelProvider := os.Getenv("MODEL_PROVIDER_ADDRESS")
  if modelProvider == "" {
      modelProvider = "0x90F79bf6EB2c4f870365E785982E1f101E93b906" // Anvil Account 2 fallback
  }
  platformBps := os.Getenv("PLATFORM_BPS")
  if platformBps == "" {
      platformBps = "10"
  }
  w.Header().Set("X-402-Platform-Bps", platformBps)
  w.Header().Set("X-402-Model-Provider", modelProvider)
  w.Header().Set("X-402-Payment-Methods", "crypto-channel,fiat-stripe")
  ```

- [ ] **Step 2: Update CORS middleware configuration**
  Search for `Access-Control-Expose-Headers` in `gateway` codebase. Add `X-402-Platform-Bps`, `X-402-Model-Provider`, `X-402-Payment-Methods` to exposed headers.

- [ ] **Step 3: Run gateway unit tests to verify they build and pass**
  Run: `go test -v ./internal/middleware/...`
  Expected: PASS

- [ ] **Step 4: Commit changes**
  ```bash
  git add gateway/internal/middleware/
  git commit -m "feat: add split-settle configuration headers to 402 challenge response"
  ```

---

### Task 4: Extend Gateway SQLite Queue & Task Fields

**Files:**
- Modify: `gateway/internal/queue/sqlite_queue.go`
- Modify: `gateway/internal/proxy/reverse.go`
- Test: `gateway/internal/queue/sqlite_queue_test.go`
- Test: `gateway/internal/proxy/proxy_hold_test.go`

**Interfaces:**
- Consumes: context parameters (`HoldAmount`, `Nonce`, `Expiration`, `Signature`)
- Consumes: header values (`X-Agent-Cost`)
- Produces: SQLite database schema changes to store split-settle columns
- Produces: POST calls from worker to `/aa/split-settle` (if task is channel-based)

- [ ] **Step 1: Modify SettleTask struct in sqlite_queue.go**
  Add fields to `SettleTask`:
  ```go
  type SettleTask struct {
      ID                int64
      LockID            string
      Proof             string
      AgentOwner        string
      EscrowAddress     string
      RetryCount        int
      ChannelID         string
      HoldAmount        uint64
      Nonce             uint64
      Expiration        uint64
      Signature         string
      AccumulatedAmount uint64
      ModelCost         uint64
      ServiceFee        uint64
      ModelProvider     string
      Treasury          string
      PlatformBps       uint16
      AgentID           int64
  }
  ```

- [ ] **Step 2: Update initDB to create missing columns if they do not exist**
  Update SQLite table definition to add columns:
  `channel_id TEXT`, `hold_amount INTEGER`, `nonce INTEGER`, `expiration INTEGER`, `signature TEXT`, `accumulated_amount INTEGER`, `model_cost INTEGER`, `service_fee INTEGER`, `model_provider TEXT`, `treasury TEXT`, `platform_bps INTEGER`, `agent_id INTEGER`.

- [ ] **Step 3: Update Enqueue and getPendingTasks database queries**
  Update `Enqueue` function parameters and `INSERT` statement.
  Update `getPendingTasks` select fields and row scanner.

- [ ] **Step 4: Update proxy/reverse.go to call extended Enqueue**
  In proxy's `ModifyResponse`, if `channelID` is present, read all preauth EIP-712 parameters, calculate `modelCost` (from `X-Agent-Cost`) and `serviceFee` (total actualCost - platformFee - modelCost). Enqueue all parameters.
  
- [ ] **Step 5: Update processSingleTask in worker**
  If `task.ChannelID` is not empty, set target URL to `/aa/split-settle` relative to base bridge URL, marshal all parameters to JSON, and POST to bridge.

- [ ] **Step 6: Run tests and commit**
  Run: `go test -v ./internal/queue` and `go test -v ./internal/proxy`
  Expected: PASS
  ```bash
  git add gateway/internal/queue/ gateway/internal/proxy/
  git commit -m "feat: extend SQLite queue schema for splitSettle parameter passing"
  ```

---

### Task 5: Upgrade AA Bridge with `/aa/split-settle` Router

**Files:**
- Modify: `aa-bridge/src/index.ts`
- Test: `aa-bridge/src/index.test.ts`

**Interfaces:**
- Produces: `POST /aa/split-settle` endpoint on port `3001`
- Invokes: `splitSettle` in `PaymentEscrow`

- [ ] **Step 1: Register endpoint in aa-bridge server**
  Add `/aa/split-settle` route parsing EIP-712 splitSettle parameters from JSON body.

- [ ] **Step 2: Implement contract invocation in Bridge handler**
  Extract TBA address using existing ERC-6551 registry lookup. If TBA address bytecode length is 0, deploy TBA first using `createAccount`.
  Call `escrowContract.write.splitSettle([channelId, accumulatedAmount, modelCost, serviceFee, modelProvider, treasury, platformBps, holdAmount, nonce, expiration, signature])`.

- [ ] **Step 3: Update mock bridge responses in Dev Mode**
  Ensure that in DEV mode, the bridge returns a mock TxHash instead of real Base Sepolia tx execution.

- [ ] **Step 4: Write test cases in bridge for split-settle route**
  Add tests inside `aa-bridge` test suites to query `/aa/split-settle` with invalid/valid inputs.

- [ ] **Step 5: Run tests and commit**
  Run: `cd aa-bridge && npm run test`
  Expected: PASS
  ```bash
  git add aa-bridge/
  git commit -m "feat: implement /aa/split-settle router in AA Bridge"
  ```
