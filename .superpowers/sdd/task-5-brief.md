# Task 5 Brief: Upgrade AA Bridge split-settle Router

**Goal**: Implement the `POST /aa/split-settle` endpoint in the AA Bridge Fastify server to parse splitSettle parameters, compute and deploy the TBA if needed, and invoke the contract's `splitSettle` method.

**Files**:
- Modify: `aa-bridge/src/index.ts`
- Test: `aa-bridge/src/index.test.ts`

**Instructions**:
1. Open `aa-bridge/src/index.ts`. Register a new route:
   `POST /aa/split-settle`
2. Parse the request body:
   - `channelId`: bytes32 hex string
   - `accumulatedAmount`: decimal/hex string (convert to BigInt)
   - `modelCost`: decimal/hex string (convert to BigInt)
   - `serviceFee`: decimal/hex string (convert to BigInt)
   - `modelProvider`: address string
   - `treasury`: address string
   - `platformBps`: number (uint16)
   - `holdAmount`: decimal/hex string (convert to BigInt)
   - `nonce`: decimal/hex string (convert to BigInt)
   - `expiration`: decimal/hex string (convert to BigInt)
   - `signature`: hex string
   - `agentId`: number/string (convert to BigInt)
3. Compute the expected TBA address for the given `agentId` using `computeTBAAddress(agentId)` (reuse existing lookup logic in `index.ts`).
4. Read the contract bytecode of the TBA address. If the TBA has not been deployed yet (bytecode length is 0 or `"0x"`), call `createAccount` on the registry to deploy the TBA first.
5. Invoke `splitSettle` on `PaymentEscrow` contract:
   ```typescript
   await escrowContract.write.splitSettle([
     channelId,
     accumulatedAmount,
     modelCost,
     serviceFee,
     modelProvider,
     treasury,
     platformBps,
     holdAmount,
     nonce,
     expiration,
     signature
   ]);
   ```
6. Return the `txHash` and payout splits.
7. Support DEV_MODE fallback (return a mock TxHash if `DEV_MODE` is enabled or in test environment, matching the existing mock transaction design in `index.ts`).
8. Add tests inside `aa-bridge/src/index.test.ts` to cover split-settle success and validation paths.
9. Verify all tests pass: `cd aa-bridge && npm run test`
10. Commit changes.
