# Task 3 Brief: Modify ModifyResponse to Exclude On-chain Settlement under Stripe mode

**Goal**: Exclude EIP-712 settlement receipt generation and SQLite enqueuing in `gateway/internal/proxy/reverse.go` if the request is paid via Stripe.

**Files**:
- Modify: `gateway/internal/proxy/reverse.go`
- Modify: `gateway/internal/proxy/proxy_hold_test.go`

**Instructions**:
1. Open `gateway/internal/proxy/reverse.go`.
2. Inside `ModifyResponse` function:
   - Retrieve `paymentMethod` from request context:
     `paymentMethod := middleware.GetPaymentMethod(ctx)`
   - If `paymentMethod == "stripe"`:
     - Get the `stripeSessionID` from context:
       `stripeSessionID := middleware.GetStripeSessionID(ctx)`
     - Skip the database enqueuing (`wrapper.QueueManager.Enqueue`) and Settle Receipt generation logic.
     - Set the header `X-402-Payment-Method: stripe` and `X-402-Stripe-Session: <stripeSessionID>` on the response `res.Header`.
     - Log: `[Proxy] Request paid via Stripe session <stripeSessionID>. Skipping chain settlement receipt signing.`.
     - Proceed normally (allowing downstream response output to pass through).
3. If `paymentMethod` is not `"stripe"` (either empty, legacy, or `"channel"`):
   - Keep the existing logic (calculate `actualCost`, enqueue task, append `X-402-Settle-Receipt`).
4. Align Go tests in `gateway/internal/proxy/proxy_hold_test.go`:
   - Add unit test to verify that if request carries `stripe:` prefix Bearer token (meaning `paymentMethod` is `"stripe"`):
     - No `X-402-Settle-Receipt` header is present.
     - `X-402-Payment-Method: stripe` and `X-402-Stripe-Session` are set.
     - SQLite queue task is NOT created.
5. Ensure all proxy tests build and pass successfully.
6. Commit changes.
