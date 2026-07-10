# Task 1 Brief: Refine Docker Compose & Add deploy.sh Shell Orchestrator

**Goal**: Prepare `docker-compose.yml` to read variables dynamically from `.env` and write the initial template for `deploy.sh` to check dependencies and spin up Anvil.

**Files**:
- Modify: `docker-compose.yml`
- Create: `deploy.sh`

**Instructions**:
1. Open `docker-compose.yml`.
2. Update environment variables in the YAML services to fallback to environment variables or local `.env` values:
   - `gateway` service environment should read `ESCROW_ADDRESS=${ESCROW_ADDRESS}` and `INTERNAL_SECRET=${INTERNAL_SECRET}` and `STRIPE_SECRET_KEY=${STRIPE_SECRET_KEY}`.
   - `aa-bridge` service environment should read `ESCROW_ADDRESS=${ESCROW_ADDRESS}` and `INTERNAL_SECRET=${INTERNAL_SECRET}` and `PRIVATE_KEY=${PRIVATE_KEY}`.
3. Create `deploy.sh` in the root workspace directory.
4. Inside `deploy.sh`:
   - Add prerequisite checks: check if `docker` is running, if `docker-compose` is available, and if `forge` is installed.
   - If `.env` doesn't exist, create it from a template, generating a random `INTERNAL_SECRET` and writing default test values:
     - `INTERNAL_SECRET=testsecret_xxx` (randomized)
     - `PRIVATE_KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80` (Anvil Account 0)
     - `STRIPE_SECRET_KEY=mock_sk_test`
     - `GATEWAY_PRIVATE_KEY=0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d` (Anvil Account 1 for gateway settler)
   - Add logic to spin up Anvil:
     `docker-compose up -d anvil`
   - Wait for Anvil to be ready by sending cURL JSON-RPC requests to `http://localhost:8545` in a loop (up to 15 seconds) until it returns successful block number or version.
5. Commit changes.
