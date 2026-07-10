#!/bin/bash
# deploy.sh - Orchestrates local AgentPay deployment and spins up Anvil.
# Exit immediately if a command exits with a non-zero status.
set -e

echo "=== AgentPay Deploy Orchestrator ==="

# 1. Check and create .env file from template if missing
ENV_FILE=".env"
if [ ! -f "$ENV_FILE" ]; then
  echo ".env file not found, creating from template..."
  if command -v openssl >/dev/null 2>&1; then
    RAND_SECRET=$(openssl rand -hex 16)
  else
    RAND_SECRET=$((RANDOM))
  fi
  
  cat <<EOF > "$ENV_FILE"
# AgentPay local environment variables
INTERNAL_SECRET=testsecret_${RAND_SECRET}
PRIVATE_KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
STRIPE_SECRET_KEY=mock_sk_test
GATEWAY_PRIVATE_KEY=0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d
ESCROW_ADDRESS=0x5FbDB2315678afecb367f032d93F642f64180aa3
EOF
  echo ".env file created with default values and randomized secret."
else
  echo ".env file already exists. Skipping creation."
fi

# Load environment variables for local shell execution
set -a
. "$ENV_FILE"
set +a

# 2. Prerequisite checks
echo "Checking prerequisites..."

# Check docker CLI
if ! command -v docker >/dev/null 2>&1; then
  echo "Error: 'docker' CLI is not installed." >&2
  exit 1
fi

# Check docker daemon
if ! docker info >/dev/null 2>&1; then
  echo "Error: Docker daemon is not running. Please start Docker." >&2
  exit 1
fi

# Check docker-compose/docker compose
DOCKER_COMPOSE_CMD=""
if docker-compose --version >/dev/null 2>&1; then
  DOCKER_COMPOSE_CMD="docker-compose"
elif docker compose version >/dev/null 2>&1; then
  DOCKER_COMPOSE_CMD="docker compose"
else
  echo "Error: Neither 'docker-compose' nor 'docker compose' is installed." >&2
  exit 1
fi
echo "Using: $DOCKER_COMPOSE_CMD"

# Check forge
if ! command -v forge >/dev/null 2>&1; then
  echo "Error: 'forge' (Foundry) is not installed." >&2
  exit 1
fi
echo "Prerequisites satisfied."

# 3. Boot Anvil container
echo "Booting Anvil container..."
$DOCKER_COMPOSE_CMD up -d anvil

# 4. Polling loops to wait until Anvil RPC is responsive (max 15 seconds)
echo "Waiting for Anvil RPC to respond..."
MAX_RETRIES=15
RETRY_COUNT=0
RPC_READY=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  RESPONSE=$(curl -s -X POST -H "Content-Type: application/json" \
    --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
    http://localhost:8545 || true)
  
  if echo "$RESPONSE" | grep -q "result"; then
    BLOCK_HEX=$(echo "$RESPONSE" | grep -o '"result":"[^"]*"' | cut -d'"' -f4)
    BLOCK_DEC=$(printf "%d" "$BLOCK_HEX" 2>/dev/null || echo "$BLOCK_HEX")
    echo "Anvil RPC is responsive! Current Block: $BLOCK_DEC ($BLOCK_HEX)"
    RPC_READY=true
    break
  fi
  
  echo "Anvil RPC not ready yet (attempt $((RETRY_COUNT+1))/$MAX_RETRIES)..."
  sleep 1
  RETRY_COUNT=$((RETRY_COUNT+1))
done

if [ "$RPC_READY" = false ]; then
  echo "Error: Anvil RPC did not become responsive within 15 seconds." >&2
  exit 1
fi

echo "Deploying PaymentEscrow contracts to local Anvil..."
(
  cd contracts
  export FOUNDRY_PRIVATE_KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
  export GATEWAY_SETTLER_ADDRESS=0x70997970C51812dc3A010C7d01b50e0d17dc79C8
  if ! forge script script/Deploy.s.sol:DeployScript --rpc-url http://localhost:8545 --broadcast; then
    echo "Error: Contract deployment failed." >&2
    exit 1
  fi
)

# Extract the deployed address from Foundry broadcast
if [ ! -f "contracts/broadcast/Deploy.s.sol/31337/run-latest.json" ]; then
  echo "Error: Broadcast run-latest.json not found." >&2
  exit 1
fi

ESCROW_ADDR=$(python3 -c "
import json, sys
try:
    with open('contracts/broadcast/Deploy.s.sol/31337/run-latest.json') as f:
        data = json.load(f)
        for tx in data.get('transactions', []):
            if tx.get('contractName') == 'PaymentEscrow':
                print(tx['contractAddress'])
                sys.exit(0)
except Exception as e:
    print('', end='')
    sys.exit(1)
")

if [ -z "$ESCROW_ADDR" ] || [ ${#ESCROW_ADDR} -ne 42 ] || ! echo "$ESCROW_ADDR" | grep -qE '^0x[0-9a-fA-F]{40}$'; then
  echo "Error: Failed to extract a valid PaymentEscrow address. Got: '$ESCROW_ADDR'" >&2
  exit 1
fi

# Inject the address into .env
python3 -c "
import re, sys
env_path = '.env'
addr = sys.argv[1]
with open(env_path, 'r') as f:
    content = f.read()
new_content, count = re.subn(r'^ESCROW_ADDRESS=.*', f'ESCROW_ADDRESS={addr}', content, flags=re.MULTILINE)
if count == 0:
    new_content = content + f'\nESCROW_ADDRESS={addr}'
with open(env_path, 'w') as f:
    f.write(new_content)
" "$ESCROW_ADDR"

echo "Extracted PaymentEscrow address: $ESCROW_ADDR and updated .env"

echo "=== Deployment Successful ==="
