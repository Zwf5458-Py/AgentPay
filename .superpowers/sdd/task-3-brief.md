# Task 3 Brief: Connect API calls and Implement Manual Retry in admin.html

**Goal**: Bind client-side JavaScript in `admin.html` to real API endpoints of Go Gateway, implementing authorization, real-time fetching, formatting stats, manual retry triggers, and database flush actions.

**Files**:
- Modify: `admin.html`

**Instructions**:
1. Open `admin.html`.
2. Extract interactive UI elements:
   - Config inputs: Gateway Origin (id `gatewayUrlInput`), Admin Secret Key (id `adminSecretInput`).
   - Refresh button: id `refreshBtn`.
   - Clear Queue button: id `clearQueueBtn`.
   - Clear Stripe Sessions button: id `clearStripeBtn`.
   - KPI metrics text elements:
     - `total_settled_usdc` (id `totalSettledText`)
     - `total_platform_fees_usdc` (id `platformFeesText`)
     - `total_stripe_sessions` (id `stripeSessionsText`)
     - `success_rate` (calculated as `success_tasks / (success_tasks + failed_tasks) * 100` or from gateway, id `successRateText`).
   - Table bodies: Tasks Queue table body (id `tasksTableBody`), Stripe Sessions list container (id `stripeSessionsList`).
3. Implement `fetchStats()`, `fetchTasks()`, and `fetchStripeSessions()`:
   - Make HTTP requests to Gateway endpoint URL (e.g. `${gatewayOrigin}/admin/stats`).
   - Attach `X-Internal-Secret` header containing the value of `adminSecretInput`.
   - Display a visual loading indicator or toast notification when fetching.
   - Format micro-units to USDC string (dividing by `1e6` to output 6 decimal places).
   - Render tasks in list. Match statuses with correct CSS tags:
     - `success`: green glow
     - `pending`/`retrying`: orange glow
     - `failed`: red glow
   - For non-success tasks, add a `[重试 (Retry)]` button calling `POST /admin/tasks/retry` passing `{ "lock_id": lockId }`. Refresh dashboard after completion.
4. Bind `clearQueueBtn` and `clearStripeBtn` to call Gateway debug clears (e.g. `/debug/tasks/clear` and `/admin/stripe-sessions/clear` respectively). Confirm before executing.
5. In addition to instructions:
   - Adjust `--neon-violet` style to `#ff00ff` to precisely conform with Spec guidelines.
   - Adjust stripe clear button text from "Clear Sessions" to "Clear stripe sessions" to conform with Spec guidelines.
6. Commit changes.
