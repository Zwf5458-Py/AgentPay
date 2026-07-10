# Task 3 Brief: Compile/Boot docker images & run Gateway health checks

**Goal**: Implement docker image building, orchestration of the full stack (Gateway, AA Bridge, Agent helper), wait loop for Gateway responsiveness, and automated cURL health checks.

**Files**:
- Modify: `deploy.sh`

**Instructions**:
1. Open `deploy.sh`.
2. Right after updating `.env` with the escrow address:
   - Print: "Building and starting services via Docker Compose..."
   - Run: `docker-compose up -d --build aa-bridge agent gateway` (Make sure to support both `docker-compose` and `docker compose` depending on the detected command from Task 1).
   - Wait/sleep 5 seconds (to allow Go server to bind and start listening).
3. Implement Gateway health checks:
   - Print: "Verifying Go Gateway health & API access security..."
   - Target URL: `http://localhost:8080/admin/stats`
   - Test 1 (Unauthorized block): Send a cURL query without the `X-Internal-Secret` header. Confirm it returns status code `401`. If it returns something else, print failure and exit 1.
   - Test 2 (Authorized stats check): Send a cURL query with header `X-Internal-Secret: $INTERNAL_SECRET` (loaded from `.env`). Confirm it returns status code `200` and contains the expected JSON structure (like `"success_tasks"` or similar keys). If it fails or returns error, print failure and exit 1.
4. Render successful deployment guidelines:
   - Print a nice ASCII banner "AgentPay Deployed successfully!"
   - Output links:
     - Client panel: `file:///Users/oraclez/code/AgentPay/client.html`
     - Admin dashboard: `file:///Users/oraclez/code/AgentPay/admin.html`
     - Escrow address: `$ESCROW_ADDR`
     - Gateway URL: `http://localhost:8080`
     - Admin Secret: `$INTERNAL_SECRET`
5. Commit changes.
