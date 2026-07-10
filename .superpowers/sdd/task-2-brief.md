# Task 2 Brief: Upgrade X402Middleware to support Bearer stripe:<session_id> Credentials

**Goal**: Support Stripe-paid requests by parsing and verifying `Bearer stripe:<session_id>` authorization header in the X-402 middleware.

**Files**:
- Modify: `gateway/internal/middleware/x402.go`
- Modify: `gateway/internal/middleware/x402_test.go`

**Instructions**:
1. Open `gateway/internal/middleware/x402.go`.
2. Introduce context keys or values to denote payment method (e.g. `PaymentMethodContextKey` value `"x402_payment_method"`).
3. In `X402Middleware`:
   - Inspect the incoming token (the parsed string after `Bearer ` prefix).
   - Check if token starts with `"stripe:"` (e.g. `strings.HasPrefix(token, "stripe:")`).
   - If it matches:
     - Extract `sessionID` by removing the `"stripe:"` prefix (e.g. `strings.TrimPrefix(token, "stripe:")`).
     - Retrieve the Stripe client instance initialized in main (or dynamically initialize a `stripe.NewStripeClient` reading environment variables).
     - Call `VerifyCheckoutSession(sessionID)` to confirm if it has been fully paid.
     - If the session is NOT valid or unpaid, call `trigger402(w)` and return.
     - If valid/paid, inject the following details into the request context:
       - `TokenContextKey` = token (`stripe:<session_id>`)
       - `LockIDContextKey` = `sessionID`
       - PaymentMethodContextKey = `"stripe"`
       - StripeSessionIDContextKey = `sessionID`
     - Call `next.ServeHTTP(w, r.WithContext(ctx))` and return.
4. Export context accessors:
   - `GetPaymentMethod(ctx context.Context) string`
   - `GetStripeSessionID(ctx context.Context) string`
5. Align unit tests in `gateway/internal/middleware/x402_test.go` to test this route:
   - Add a test `TestX402Middleware_StripeValid` and `TestX402Middleware_StripeInvalid`.
   - Mock Stripe verification (since Stripe client in test environment defaults to Mock Mode if environment variables are not set, it will easily return verified for `cs_mock_` sessions).
6. Commit changes.
