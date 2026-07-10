# Task 4 Brief: Update client.html to support Stripe Checkout Popup Flow

**Goal**: Upgrade `client.html` with payment mode tabs (Crypto Channel / Stripe Fiat), handle creation of Stripe sessions, mock checkout page behavior, timeline adjustments, and invoice rendering under Stripe payment mode.

**Files**:
- Modify: `client.html`

**Instructions**:
1. Open `client.html`.
2. Add a payment strategy selector UI section above the Wallet Control card:
   - Elements: A tab bar with two tabs:
     - `🔗 链上支付 (Crypto Channel)` (id `paymentCryptoTab`, active by default)
     - `💳 法币支付 (Stripe)` (id `paymentStripeTab`)
3. Handle Tab switching behavior:
   - Set a global variable `activePaymentStrategy = "crypto"` or `"stripe"`.
   - When `"crypto"` is selected:
     - Show the entire simulated wallet connection card and related controls.
     - The timeline should display the 5-step on-chain lifecycle: Request → Challenge → Sign → Audit → Settle.
     - Clear any Stripe notes.
   - When `"stripe"` is selected:
     - Hide the simulated wallet connection card (or collapse it).
     - Show a simplified Stripe checkout details note: "使用信用卡通过 Stripe 收银台进行按次支付，每次固定 $0.05 USDC 等值法币。".
     - Enable the `Execute Audit` button directly (no wallet connection required for Stripe mode).
     - The timeline should adjust to a 4-step structure: Request → Challenge → Stripe Pay → Audit (removing Sign and Settle). Update step IDs dynamically.
4. Implement `executeAudit` handler modifications to support Stripe payment Strategy:
   - When `activePaymentStrategy === "stripe"`:
     - **Step 1 (Request)**: Call `POST /agent/execute` with no authorization.
     - **Step 2 (Challenge)**: Intercept HTTP 402. Read `X-402-Payment-Methods` and confirm `"fiat-stripe"` is supported.
     - **Step 3 (Stripe Pay)**:
       - Update timeline Step Stripe Pay to `active`.
       - Call Gateway route `POST /stripe/create-session` sending `{ "amount": 50000, "agentId": agentId }` to retrieve `sessionId` and `url`.
       - Open a popup window using `window.open(url, "StripeCheckout", "width=600,height=700")`.
       - In Mock Mode (since `url` returned is a local redirect page `http://.../stripe/success?session_id=...` or mock page):
         - Monitor the popup window or simulate its completion after a short delay (e.g. 2 seconds) by closing it.
         - Update timeline Step Stripe Pay to `completed`.
     - **Step 4 (Audit)**:
       - Update timeline Step Audit to `active`.
       - Send the second request to `/agent/execute` with header `Authorization: Bearer stripe:<sessionId>`.
       - Verify HTTP 200 response, render the Markdown audit report.
       - Verify receipt details: Stripe mode returns headers `X-402-Payment-Method: stripe` and `X-402-Stripe-Session`.
       - Update the Invoice details panel:
         - Display "支付策略: 信用卡支付 (Stripe)"
         - Cost: 0.050000 USDC
         - platformFee, serviceFee, modelCost: dynamically split if `X-Agent-Cost` header is present, or display as card checkout values.
         - Refunded: 0.000000 USDC (fiat payment is exact, no hold refund).
         - Update Step Audit to `completed`.
5. Ensure layout styles remain premium and dark neon glassmorphic.
6. Commit changes.
