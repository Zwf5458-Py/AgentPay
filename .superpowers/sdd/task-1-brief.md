# Task 1 Brief: Integrate Stripe API / Mock Payment Services in Gateway

**Goal**: Implement Stripe payment session creation and verification, supporting both production Stripe SDK and a mock fallback if `STRIPE_SECRET_KEY` is absent.

**Files**:
- Create: `gateway/internal/stripe/stripe.go`
- Modify: `gateway/cmd/gateway/main.go`

**Instructions**:
1. Create a new Go package directory if needed: `gateway/internal/stripe`.
2. Inside `gateway/internal/stripe/stripe.go`:
   - Define a `StripeClient` struct or interface.
   - Implement `NewStripeClient(secretKey string) *StripeClient`.
   - If `secretKey` starts with `mock_` or is empty:
     - Operate in **Mock Mode**.
     - `CreateCheckoutSession(amountMicro uint64, successURL, cancelURL string) (string, string, error)`:
       - Generate a mock session ID: `cs_mock_` followed by random characters.
       - The checkout URL should point to a mock success redirect page or a local URL (e.g. `successURL?session_id=cs_mock_xxx`).
       - Print log: `[Stripe Mock] Created mock checkout session cs_mock_xxx`.
     - `VerifyCheckoutSession(sessionID string) (bool, error)`:
       - Return `true, nil` for any session starting with `cs_mock_`.
   - If `secretKey` is a valid Stripe secret:
     - Use the official Stripe Go SDK (`github.com/stripe/stripe-go/v72` or similar) to call `checkoutsession.New(...)`.
     - (Note: if installing a new go module, first run `go get github.com/stripe/stripe-go/v72` or appropriate package. However, to keep it simple and compile immediately, we can use standard net/http call or official package. Official package is preferred. Let's make sure it is imported correctly).
     - Let's check if the standard `stripe-go` library is in dependencies or if we can implement a clean net/http Stripe integration, OR use official SDK.
     - Official SDK is preferred: `github.com/stripe/stripe-go/v72`.
     - Let's check dependencies of go.mod first or let's specify a robust net/http Stripe API client to avoid module dependency issues in offline/sandbox environments, OR download the module.
     - A robust HTTP wrapper for Stripe Checkout Session API is simple and 100% reliable:
       ```go
       // POST https://api.stripe.com/v1/checkout/sessions
       // Header: Authorization: Bearer <secretKey>
       ```
       This requires no external library, makes it extremely lightweight, highly reliable, and compiles instantly! Let's recommend implementing a clean net/http client for Stripe Checkout Session creation and session retrieval.
3. In `gateway/cmd/gateway/main.go`:
   - Initialize the Stripe client.
   - Register route `POST /stripe/create-session`:
     - Parse request body: `{ "amount": 50000 }` (Micro-units) or use fixed fee 50000.
     - Call `CreateCheckoutSession`.
     - Return JSON: `{ "sessionId": "...", "url": "..." }`.
   - Update CORS middleware to expose new headers `X-402-Payment-Method` and `X-402-Stripe-Session`.
4. Commit changes.
