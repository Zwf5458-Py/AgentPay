# Task 2 Brief: Automate Foundry contract deployment & address extraction

**Goal**: Implement contract compilation/broadcast deployment, parse the deployed `PaymentEscrow` address, and dynamically inject it into `.env`.

**Files**:
- Modify: `deploy.sh`

**Instructions**:
1. Open `deploy.sh`.
2. Right after the Anvil connection polling loop finishes successfully:
   - Print: "Deploying PaymentEscrow contracts to local Anvil..."
   - Change directory to `contracts/`.
   - Compile and deploy contracts:
     `forge script script/Deploy.s.sol:DeployScript --rpc-url http://localhost:8545 --broadcast`
   - Check if the script succeeded. If it fails, report error and exit 1.
3. Extract the deployed address from Foundry broadcast:
   - Check if `contracts/broadcast/Deploy.s.sol/31337/run-latest.json` exists.
   - Run a Python inline snippet (or awk/grep) to parse this JSON file and print the `contractAddress` of `PaymentEscrow`. Specifically:
     ```bash
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
     ```
   - Assert that `ESCROW_ADDR` is not empty and matches a valid 42-character Ethereum address format (starts with `0x` and has length 42). If invalid, print error and exit 1.
4. Inject the address into `.env`:
   - Replace the `ESCROW_ADDRESS=...` line in the root `.env` file with `ESCROW_ADDRESS=$ESCROW_ADDR`.
   - Print: "Extracted PaymentEscrow address: $ESCROW_ADDR and updated .env".
5. Commit changes.
