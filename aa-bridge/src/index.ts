import fastify from 'fastify';
import dotenv from 'dotenv';
import { getSmartAccountAddress, getAccountBalance } from './kernel/account.js';
import { grantPermission } from './kernel/permissions.js';
import { createPublicClient, createWalletClient, http, isAddress } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { baseSepolia } from 'viem/chains';

dotenv.config();

const server = fastify({ logger: true });

// In-memory registry mapping agentId -> account details
const accountsDb = new Map<
  number,
  { ownerAddress: string; salt: string; smartAccountAddress: string }
>();

// Global Error Handler for Promise Rejections & Uncaught Errors
server.setErrorHandler((error, request, reply) => {
  server.log.error(error);
  reply.status(500).send({
    success: false,
    error: error.message || 'Internal Server Error',
  });
});

// Helper to check bytecode size on-chain to determine deployment status
async function checkIsDeployed(address: string): Promise<boolean> {
  if (process.env.DEV_MODE === 'true' || !process.env.ZERODEV_PROJECT_ID) {
    return false;
  }
  try {
    const rpcUrl = process.env.RPC_URL || 'https://sepolia.base.org';
    const publicClient = createPublicClient({
      chain: baseSepolia,
      transport: http(rpcUrl),
    });
    const bytecode = await publicClient.getBytecode({ address: address as `0x${string}` });
    return bytecode !== undefined && bytecode !== '0x';
  } catch {
    return false;
  }
}

// 1. Create Smart Account
server.post('/aa/account/create', async (request, reply) => {
  const { ownerAddress, agentId, salt } = request.body as {
    ownerAddress: string;
    agentId: number;
    salt: string;
  };

  if (!ownerAddress || agentId === undefined || !salt) {
    return reply.status(400).send({ error: 'Missing ownerAddress, agentId, or salt' });
  }

  // Input address validation
  if (!isAddress(ownerAddress)) {
    return reply.status(400).send({ error: 'Invalid ownerAddress format' });
  }

  try {
    const smartAccountAddress = await getSmartAccountAddress(ownerAddress, agentId, salt);
    const isDeployed = await checkIsDeployed(smartAccountAddress);

    // Save to local mapping
    accountsDb.set(Number(agentId), {
      ownerAddress,
      salt,
      smartAccountAddress,
    });

    return {
      smartAccountAddress,
      isDeployed,
    };
  } catch (error: any) {
    server.log.error(error);
    return reply.status(500).send({ error: error.message || 'Failed to create smart account' });
  }
});

// 2. Grant Session Key Permission
server.post('/aa/permission/grant', async (request, reply) => {
  const { agentId, sessionKeyAddress, scopes, spendLimit } = request.body as {
    agentId: number;
    sessionKeyAddress: string;
    scopes: any[];
    spendLimit: string;
  };

  if (agentId === undefined || !sessionKeyAddress || !scopes || !spendLimit) {
    return reply.status(400).send({ error: 'Missing agentId, sessionKeyAddress, scopes, or spendLimit' });
  }

  // Input address validation
  if (!isAddress(sessionKeyAddress)) {
    return reply.status(400).send({ error: 'Invalid sessionKeyAddress format' });
  }

  try {
    const result = await grantPermission(Number(agentId), sessionKeyAddress, scopes, spendLimit);
    return result;
  } catch (error: any) {
    server.log.error(error);
    return reply.status(500).send({ error: error.message || 'Failed to grant permission' });
  }
});

// 3. Settle Payment
server.post('/aa/settle', async (request, reply) => {
  const { lockId, proof, agentOwner, escrowAddress } = request.body as {
    lockId: string;
    proof: string;
    agentOwner: string;
    escrowAddress: string;
  };

  if (!lockId || !proof || !agentOwner || !escrowAddress) {
    return reply.status(400).send({ error: 'Missing lockId, proof, agentOwner, or escrowAddress' });
  }

  // Input address validation
  if (!isAddress(agentOwner)) {
    return reply.status(400).send({ error: 'Invalid agentOwner address format' });
  }
  if (!isAddress(escrowAddress)) {
    return reply.status(400).send({ error: 'Invalid escrowAddress format' });
  }

  const escrowAbi = [
    {
      name: 'releasePayment',
      type: 'function',
      stateMutability: 'external',
      inputs: [
        { name: 'lockId', type: 'bytes32' },
        { name: 'proof', type: 'bytes' },
        { name: 'agentOwner', type: 'address' },
      ],
      outputs: [],
    },
  ];

  try {
    const privateKey = process.env.PRIVATE_KEY as `0x${string}`;
    const rpcUrl = process.env.RPC_URL || 'http://127.0.0.1:8545';

    if (!privateKey) {
      throw new Error('PRIVATE_KEY is not configured in environment variables');
    }

    const account = privateKeyToAccount(privateKey);
    const publicClient = createPublicClient({
      chain: baseSepolia,
      transport: http(rpcUrl),
    });

    const walletClient = createWalletClient({
      account,
      chain: baseSepolia,
      transport: http(rpcUrl),
    });

    // Simulate on-chain call
    const { request: txRequest } = await publicClient.simulateContract({
      account,
      address: escrowAddress as `0x${string}`,
      abi: escrowAbi,
      functionName: 'releasePayment',
      args: [lockId as `0x${string}`, proof as `0x${string}`, agentOwner as `0x${string}`],
    });

    // Send transaction
    const hash = await walletClient.writeContract(txRequest);

    return {
      success: true,
      txHash: hash,
    };
  } catch (error: any) {
    const devMode = process.env.DEV_MODE === 'true' || !process.env.ZERODEV_PROJECT_ID;
    
    server.log.warn(`Escrow transaction failed/skipped: ${error.message}. DevMode: ${devMode}`);

    // If not in DevMode (Production), DO NOT silently fallback. Propagate the error as HTTP 500.
    if (!devMode) {
      return reply.status(500).send({
        success: false,
        error: error.message || 'Escrow settlement transaction execution failed',
      });
    }

    // Safe mock fallback hash ONLY when in DevMode
    return {
      success: true,
      txHash: '0x7777777777777777777777777777777777777777777777777777777777777777',
      mocked: true,
    };
  }
});

// 4. Get Agent Smart Account and Balance
server.get('/aa/account/:agentId', async (request, reply) => {
  const { agentId } = request.params as { agentId: string };
  const numericAgentId = Number(agentId);

  if (isNaN(numericAgentId)) {
    return reply.status(400).send({ error: 'Invalid agentId parameter' });
  }

  // Look up account in memory DB
  const entry = accountsDb.get(numericAgentId);

  if (!entry) {
    const devMode = process.env.DEV_MODE === 'true' || !process.env.ZERODEV_PROJECT_ID;
    
    if (devMode) {
      // In Mock mode: fallback gracefully by deriving a deterministic Mock address
      const mockOwner = '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266';
      const smartAccountAddress = await getSmartAccountAddress(mockOwner, numericAgentId, 'fallback-salt');
      const balance = await getAccountBalance(smartAccountAddress);

      return {
        smartAccountAddress,
        balance,
      };
    } else {
      // In Production/Non-Mock mode: fetch owner from blockchain AgentIdentityRegistry
      const registryAddress = process.env.IDENTITY_REGISTRY_ADDRESS;
      if (!registryAddress) {
        return reply.status(500).send({ error: 'IDENTITY_REGISTRY_ADDRESS not configured' });
      }

      try {
        const rpcUrl = process.env.RPC_URL || 'https://sepolia.base.org';
        const publicClient = createPublicClient({
          chain: baseSepolia,
          transport: http(rpcUrl),
        });

        // Call ownerOf on AgentIdentityRegistry contract (implements ERC721)
        const owner = await publicClient.readContract({
          address: registryAddress as `0x${string}`,
          abi: [
            {
              name: 'ownerOf',
              type: 'function',
              stateMutability: 'view',
              inputs: [{ name: 'tokenId', type: 'uint256' }],
              outputs: [{ name: '', type: 'address' }],
            },
          ],
          functionName: 'ownerOf',
          args: [BigInt(numericAgentId)],
        }) as string;

        if (!owner || owner === '0x0000000000000000000000000000000000000000') {
          return reply.status(404).send({ error: 'Agent identity not registered' });
        }

        // Deterministically compute smart account address using standard default-salt
        const smartAccountAddress = await getSmartAccountAddress(owner, numericAgentId, 'default-salt');
        const balance = await getAccountBalance(smartAccountAddress);

        // Cache the newly resolved account details
        accountsDb.set(numericAgentId, {
          ownerAddress: owner,
          salt: 'default-salt',
          smartAccountAddress,
        });

        return {
          smartAccountAddress,
          balance,
        };
      } catch (error: any) {
        server.log.error(`Failed to lookup agent owner on-chain: ${error.message}`);
        
        // Return 404 if the contract reverted on ownerOf (token doesn't exist)
        if (
          error.message.includes('ownerOf') ||
          error.message.includes('revert') ||
          error.message.includes('not exist') ||
          error.message.includes('AgentDoesNotExist')
        ) {
          return reply.status(404).send({ error: 'Agent identity not registered' });
        }
        
        // Propagate other system/RPC errors as 500
        return reply.status(500).send({ error: `Failed to query AgentIdentityRegistry: ${error.message}` });
      }
    }
  }

  try {
    const balance = await getAccountBalance(entry.smartAccountAddress);
    return {
      smartAccountAddress: entry.smartAccountAddress,
      balance,
    };
  } catch (error: any) {
    server.log.error(error);
    return reply.status(500).send({ error: error.message || 'Failed to query account details' });
  }
});

// Start fastify server
const start = async () => {
  try {
    const port = Number(process.env.PORT) || 3001;
    await server.listen({ port, host: '127.0.0.1' });
    console.log(`Smart Account Bridge Microservice running on http://127.0.0.1:${port}`);
  } catch (err) {
    server.log.error(err);
    process.exit(1);
  }
};

// Check if running directly to start the server
if (process.env.NODE_ENV !== 'test') {
  start();
}

export { server };
