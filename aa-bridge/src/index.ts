import fastify from 'fastify';
import cors from '@fastify/cors';
import dotenv from 'dotenv';
import { getSmartAccountAddress, getAccountBalance } from './kernel/account.js';
import { grantPermission } from './kernel/permissions.js';
import { createPublicClient, createWalletClient, http, isAddress, pad, stringToHex } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { baseSepolia } from 'viem/chains';
import { getRpcTransport } from './utils/rpc.js';

dotenv.config();

const server = fastify({ logger: true });

await server.register(cors, {
  origin: '*',
  methods: ['GET', 'POST', 'OPTIONS'],
  allowedHeaders: ['Content-Type', 'Authorization', 'x-internal-secret'],
  optionsSuccessStatus: 200,
});

// In-memory registry mapping agentId -> account details
const accountsDb = new Map<
  number,
  { ownerAddress: string; salt: string; smartAccountAddress: string }
>();

const escrowConfigCache = new Map<
  string,
  {
    tbaImplementation: string;
    erc6551Registry: string;
    agentIdentityRegistry: string;
  }
>();

function parseChannelId(channelId: string): `0x${string}` {
  const clean = channelId.startsWith('0x') ? channelId.slice(2) : channelId;
  const is64Hex = clean.length === 64 && /^[0-9a-fA-F]{64}$/.test(clean);
  if (is64Hex) {
    return `0x${clean}`;
  }
  return pad(stringToHex(channelId), { size: 32 });
}

// Global Error Handler for Promise Rejections & Uncaught Errors
server.setErrorHandler((error, request, reply) => {
  server.log.error(error);
  reply.status(500).send({
    success: false,
    error: error.message || 'Internal Server Error',
  });
});

// onRequest hook to enforce security credentials validation via x-internal-secret
server.addHook('onRequest', async (request, reply) => {
  if (request.method === 'OPTIONS' || request.url === '/health' || request.url === '/') {
    return;
  }
  const secret = process.env.INTERNAL_SECRET;
  if (!secret) {
    server.log.error('INTERNAL_SECRET is not configured in the environment');
    return reply.status(500).send({
      success: false,
      error: 'INTERNAL_SECRET is not configured on the server',
    });
  }

  const clientSecret = request.headers['x-internal-secret'];
  if (!clientSecret || clientSecret !== secret) {
    return reply.status(401).send({
      success: false,
      error: 'Unauthorized: Missing or invalid x-internal-secret header',
    });
  }
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
      transport: getRpcTransport(rpcUrl),
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
server.post('/aa/settle', {
  schema: {
    body: {
      type: 'object',
      properties: {
        lockId: { type: 'string' },
        proof: { type: 'string' },
        channelId: { type: 'string' },
        accumulatedAmount: { type: 'string' },
        holdAmount: { type: 'string' },
        nonce: { type: 'string' },
        expiration: { type: 'string' },
        signature: { type: 'string' },
        agentId: { type: 'integer' }
      }
    }
  }
}, async (request, reply) => {
  const body = request.body as any;

  if (body && body.channelId !== undefined) {
    const { channelId, accumulatedAmount, holdAmount, nonce, expiration, signature, agentId, escrowAddress } = body as {
      channelId: string;
      accumulatedAmount: string | bigint;
      holdAmount: string | bigint;
      nonce: string | bigint;
      expiration: string | bigint;
      signature: string;
      agentId: number;
      escrowAddress?: string;
    };

    if (!channelId || accumulatedAmount === undefined || holdAmount === undefined || nonce === undefined || expiration === undefined || !signature || agentId === undefined) {
      return reply.status(400).send({ error: 'Missing channelId, accumulatedAmount, holdAmount, nonce, expiration, signature, or agentId' });
    }

    const resolvedEscrowAddress = escrowAddress || process.env.ESCROW_ADDRESS;
    if (!resolvedEscrowAddress || !isAddress(resolvedEscrowAddress)) {
      return reply.status(400).send({ error: 'Invalid or missing escrowAddress' });
    }

    const escrowAbi = [
      {
        name: 'tbaImplementation',
        type: 'function',
        stateMutability: 'view',
        inputs: [],
        outputs: [{ name: '', type: 'address' }],
      },
      {
        name: 'erc6551Registry',
        type: 'function',
        stateMutability: 'view',
        inputs: [],
        outputs: [{ name: '', type: 'address' }],
      },
      {
        name: 'agentIdentityRegistry',
        type: 'function',
        stateMutability: 'view',
        inputs: [],
        outputs: [{ name: '', type: 'address' }],
      },
      {
        name: 'batchSettle',
        type: 'function',
        stateMutability: 'nonpayable',
        inputs: [
          { name: 'channelId', type: 'bytes32' },
          { name: 'accumulatedAmount', type: 'uint256' },
          { name: 'holdAmount', type: 'uint256' },
          { name: 'nonce', type: 'uint256' },
          { name: 'expiration', type: 'uint256' },
          { name: 'signature', type: 'bytes' },
          { name: 'agentOwner', type: 'address' },
        ],
        outputs: [],
      },
    ];

    const registryAbi = [
      {
        name: 'account',
        type: 'function',
        stateMutability: 'view',
        inputs: [
          { name: 'implementation', type: 'address' },
          { name: 'salt', type: 'bytes32' },
          { name: 'chainId', type: 'uint256' },
          { name: 'tokenContract', type: 'address' },
          { name: 'tokenId', type: 'uint256' },
        ],
        outputs: [{ name: '', type: 'address' }],
      },
      {
        name: 'createAccount',
        type: 'function',
        stateMutability: 'nonpayable',
        inputs: [
          { name: 'implementation', type: 'address' },
          { name: 'salt', type: 'bytes32' },
          { name: 'chainId', type: 'uint256' },
          { name: 'tokenContract', type: 'address' },
          { name: 'tokenId', type: 'uint256' },
        ],
        outputs: [{ name: '', type: 'address' }],
      },
    ];

    const devMode = process.env.DEV_MODE === 'true' || !process.env.ZERODEV_PROJECT_ID;

    try {
      const privateKey = process.env.PRIVATE_KEY as `0x${string}`;
      const rpcUrl = process.env.RPC_URL || 'http://127.0.0.1:8545';

      if (!privateKey) {
        throw new Error('PRIVATE_KEY is not configured in environment variables');
      }

      const account = privateKeyToAccount(privateKey);
      const publicClient = createPublicClient({
        chain: baseSepolia,
        transport: getRpcTransport(rpcUrl),
      });

      const walletClient = createWalletClient({
        account,
        chain: baseSepolia,
        transport: getRpcTransport(rpcUrl),
      });

      // 1. 获取 TBA 相关的合约地址
      let tbaImplementationAddress: string;
      let erc6551RegistryAddress: string;
      let agentIdentityRegistryAddress: string;

      const cached = escrowConfigCache.get(resolvedEscrowAddress);
      if (cached) {
        tbaImplementationAddress = cached.tbaImplementation;
        erc6551RegistryAddress = cached.erc6551Registry;
        agentIdentityRegistryAddress = cached.agentIdentityRegistry;
      } else {
        try {
          tbaImplementationAddress = await publicClient.readContract({
            address: resolvedEscrowAddress as `0x${string}`,
            abi: escrowAbi,
            functionName: 'tbaImplementation',
          }) as string;
          erc6551RegistryAddress = await publicClient.readContract({
            address: resolvedEscrowAddress as `0x${string}`,
            abi: escrowAbi,
            functionName: 'erc6551Registry',
          }) as string;
          agentIdentityRegistryAddress = await publicClient.readContract({
            address: resolvedEscrowAddress as `0x${string}`,
            abi: escrowAbi,
            functionName: 'agentIdentityRegistry',
          }) as string;

          if (devMode) {
            if (!tbaImplementationAddress) tbaImplementationAddress = '0x2222222222222222222222222222222222222222';
            if (!erc6551RegistryAddress) erc6551RegistryAddress = '0x1111111111111111111111111111111111111111';
            if (!agentIdentityRegistryAddress) agentIdentityRegistryAddress = '0x3333333333333333333333333333333333333333';
          }
        } catch (err: any) {
          if (!devMode) throw err;
          // Mock 环境下 fallback 地址
          tbaImplementationAddress = '0x2222222222222222222222222222222222222222';
          erc6551RegistryAddress = '0x1111111111111111111111111111111111111111';
          agentIdentityRegistryAddress = '0x3333333333333333333333333333333333333333';
        }
        escrowConfigCache.set(resolvedEscrowAddress, {
          tbaImplementation: tbaImplementationAddress,
          erc6551Registry: erc6551RegistryAddress,
          agentIdentityRegistry: agentIdentityRegistryAddress,
        });
      }

      // 2. 计算专属 TBA 账户地址
      const salt = '0x0000000000000000000000000000000000000000000000000000000000000000' as `0x${string}`;
      const chainId = BigInt(baseSepolia.id);

      let computedTBA: string;
      try {
        computedTBA = await publicClient.readContract({
          address: erc6551RegistryAddress as `0x${string}`,
          abi: registryAbi,
          functionName: 'account',
          args: [
            tbaImplementationAddress as `0x${string}`,
            salt,
            chainId,
            agentIdentityRegistryAddress as `0x${string}`,
            BigInt(agentId),
          ],
        }) as string;

        if (!computedTBA && devMode) {
          computedTBA = '0x4444444444444444444444444444444444444444';
        }
      } catch (err: any) {
        if (!devMode) throw err;
        // Mock 模式下本地生成
        computedTBA = '0x4444444444444444444444444444444444444444';
      }

      // 3. 检测该 TBA 账户是否部署
      let isDeployed = false;
      if (!devMode) {
        try {
          const bytecode = await publicClient.getBytecode({ address: computedTBA as `0x${string}` });
          isDeployed = bytecode !== undefined && bytecode !== '0x';
        } catch {
          isDeployed = false;
        }
      }

      // 4. 若未部署，调用 createAccount 自动部署
      if (!isDeployed && !devMode) {
        const { request: deployRequest } = await publicClient.simulateContract({
          account,
          address: erc6551RegistryAddress as `0x${string}`,
          abi: registryAbi,
          functionName: 'createAccount',
          args: [
            tbaImplementationAddress as `0x${string}`,
            salt,
            chainId,
            agentIdentityRegistryAddress as `0x${string}`,
            BigInt(agentId),
          ],
        });
        const deployHash = await walletClient.writeContract(deployRequest);
        await publicClient.waitForTransactionReceipt({ hash: deployHash });
      } else if (!isDeployed && devMode) {
        server.log.info(`Mock environment: Simulated deployment of TBA for agent ${agentId} at address ${computedTBA}`);
      }

      // 5. 调用 batchSettle
      const bytes32ChannelId = parseChannelId(channelId);

      if (devMode) {
        return {
          success: true,
          txHash: '0x7777777777777777777777777777777777777777777777777777777777777777',
          mocked: true,
          computedTBA,
        };
      }

      const { request: settleRequest } = await publicClient.simulateContract({
        account,
        address: resolvedEscrowAddress as `0x${string}`,
        abi: escrowAbi,
        functionName: 'batchSettle',
        args: [
          bytes32ChannelId,
          BigInt(accumulatedAmount),
          BigInt(holdAmount),
          BigInt(nonce),
          BigInt(expiration),
          signature as `0x${string}`,
          computedTBA as `0x${string}`,
        ],
      });

      const hash = await walletClient.writeContract(settleRequest);

      return {
        success: true,
        txHash: hash,
        computedTBA,
      };
    } catch (error: any) {
      server.log.warn(`Channel Settle transaction failed/skipped: ${error.message}. DevMode: ${devMode}`);

      if (!devMode) {
        return reply.status(500).send({
          success: false,
          error: error.message || 'Channel settlement transaction execution failed',
        });
      }

      return {
        success: true,
        txHash: '0x7777777777777777777777777777777777777777777777777777777777777777',
        mocked: true,
        computedTBA: '0x4444444444444444444444444444444444444444',
      };
    }
  } else {
    // 走原有的 lockId 结算逻辑 (即原来的 releasePayment 逻辑)
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
        transport: getRpcTransport(rpcUrl),
      });

      const walletClient = createWalletClient({
        account,
        chain: baseSepolia,
        transport: getRpcTransport(rpcUrl),
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
  }
});

// 3.5. Split Settle Payment
server.post('/aa/split-settle', {
  schema: {
    body: {
      type: 'object',
      properties: {
        channelId: { type: 'string' },
        accumulatedAmount: { type: 'string' },
        modelCost: { type: 'string' },
        serviceFee: { type: 'string' },
        modelProvider: { type: 'string' },
        treasury: { type: 'string' },
        platformBps: { type: 'integer' },
        holdAmount: { type: 'string' },
        nonce: { type: 'string' },
        expiration: { type: 'string' },
        signature: { type: 'string' },
        agentId: { type: 'integer' },
        escrowAddress: { type: 'string' }
      }
    }
  }
}, async (request, reply) => {
  const body = request.body as any;

  if (!body) {
    return reply.status(400).send({ error: 'Missing request body' });
  }

  const {
    channelId,
    accumulatedAmount,
    modelCost,
    serviceFee,
    modelProvider,
    treasury,
    platformBps,
    holdAmount,
    nonce,
    expiration,
    signature,
    agentId,
    escrowAddress
  } = body;

  if (
    !channelId ||
    accumulatedAmount === undefined ||
    modelCost === undefined ||
    serviceFee === undefined ||
    !modelProvider ||
    !treasury ||
    platformBps === undefined ||
    holdAmount === undefined ||
    nonce === undefined ||
    expiration === undefined ||
    !signature ||
    agentId === undefined
  ) {
    return reply.status(400).send({
      error: 'Missing required parameters: channelId, accumulatedAmount, modelCost, serviceFee, modelProvider, treasury, platformBps, holdAmount, nonce, expiration, signature, or agentId'
    });
  }

  if (!isAddress(modelProvider) || modelProvider === '0x0000000000000000000000000000000000000000') {
    return reply.status(400).send({ error: 'Invalid modelProvider address' });
  }

  if (!isAddress(treasury) || treasury === '0x0000000000000000000000000000000000000000') {
    return reply.status(400).send({ error: 'Invalid treasury address' });
  }

  const resolvedEscrowAddress = escrowAddress || process.env.ESCROW_ADDRESS;
  if (!resolvedEscrowAddress || !isAddress(resolvedEscrowAddress)) {
    return reply.status(400).send({ error: 'Invalid or missing escrowAddress' });
  }

  const biAccumulated = BigInt(accumulatedAmount);
  const biModelCost = BigInt(modelCost);
  const biServiceFee = BigInt(serviceFee);
  const biHoldAmount = BigInt(holdAmount);
  const biNonce = BigInt(nonce);
  const biExpiration = BigInt(expiration);

  const platformFee = (biAccumulated * BigInt(platformBps)) / 10000n;
  if (biModelCost + biServiceFee + platformFee > biAccumulated) {
    return reply.status(400).send({ error: 'Invalid amounts: modelCost + serviceFee + platformFee exceeds accumulatedAmount' });
  }

  const escrowAbi = [
    {
      name: 'tbaImplementation',
      type: 'function',
      stateMutability: 'view',
      inputs: [],
      outputs: [{ name: '', type: 'address' }],
    },
    {
      name: 'erc6551Registry',
      type: 'function',
      stateMutability: 'view',
      inputs: [],
      outputs: [{ name: '', type: 'address' }],
    },
    {
      name: 'agentIdentityRegistry',
      type: 'function',
      stateMutability: 'view',
      inputs: [],
      outputs: [{ name: '', type: 'address' }],
    },
    {
      name: 'splitSettle',
      type: 'function',
      stateMutability: 'nonpayable',
      inputs: [
        { name: 'channelId', type: 'bytes32' },
        { name: 'accumulatedAmount', type: 'uint256' },
        { name: 'modelCost', type: 'uint256' },
        { name: 'serviceFee', type: 'uint256' },
        { name: 'modelProvider', type: 'address' },
        { name: 'treasury', type: 'address' },
        { name: 'platformBps', type: 'uint16' },
        { name: 'holdAmount', type: 'uint256' },
        { name: 'nonce', type: 'uint256' },
        { name: 'expiration', type: 'uint256' },
        { name: 'signature', type: 'bytes' },
      ],
      outputs: [],
    },
  ];

  const registryAbi = [
    {
      name: 'account',
      type: 'function',
      stateMutability: 'view',
      inputs: [
        { name: 'implementation', type: 'address' },
        { name: 'salt', type: 'bytes32' },
        { name: 'chainId', type: 'uint256' },
        { name: 'tokenContract', type: 'address' },
        { name: 'tokenId', type: 'uint256' },
      ],
      outputs: [{ name: '', type: 'address' }],
    },
    {
      name: 'createAccount',
      type: 'function',
      stateMutability: 'nonpayable',
      inputs: [
        { name: 'implementation', type: 'address' },
        { name: 'salt', type: 'bytes32' },
        { name: 'chainId', type: 'uint256' },
        { name: 'tokenContract', type: 'address' },
        { name: 'tokenId', type: 'uint256' },
      ],
      outputs: [{ name: '', type: 'address' }],
    },
  ];

  const devMode = process.env.DEV_MODE === 'true' || !process.env.ZERODEV_PROJECT_ID;

  try {
    const privateKey = process.env.PRIVATE_KEY as `0x${string}`;
    const rpcUrl = process.env.RPC_URL || 'http://127.0.0.1:8545';

    if (!privateKey) {
      throw new Error('PRIVATE_KEY is not configured in environment variables');
    }

    const account = privateKeyToAccount(privateKey);
    const publicClient = createPublicClient({
      chain: baseSepolia,
      transport: getRpcTransport(rpcUrl),
    });

    const walletClient = createWalletClient({
      account,
      chain: baseSepolia,
      transport: getRpcTransport(rpcUrl),
    });

    // 1. 获取 TBA 相关的合约地址
    let tbaImplementationAddress: string;
    let erc6551RegistryAddress: string;
    let agentIdentityRegistryAddress: string;

    const cached = escrowConfigCache.get(resolvedEscrowAddress);
    if (cached) {
      tbaImplementationAddress = cached.tbaImplementation;
      erc6551RegistryAddress = cached.erc6551Registry;
      agentIdentityRegistryAddress = cached.agentIdentityRegistry;
    } else {
      try {
        tbaImplementationAddress = await publicClient.readContract({
          address: resolvedEscrowAddress as `0x${string}`,
          abi: escrowAbi,
          functionName: 'tbaImplementation',
        }) as string;
        erc6551RegistryAddress = await publicClient.readContract({
          address: resolvedEscrowAddress as `0x${string}`,
          abi: escrowAbi,
          functionName: 'erc6551Registry',
        }) as string;
        agentIdentityRegistryAddress = await publicClient.readContract({
          address: resolvedEscrowAddress as `0x${string}`,
          abi: escrowAbi,
          functionName: 'agentIdentityRegistry',
        }) as string;

        if (devMode) {
          if (!tbaImplementationAddress) tbaImplementationAddress = '0x2222222222222222222222222222222222222222';
          if (!erc6551RegistryAddress) erc6551RegistryAddress = '0x1111111111111111111111111111111111111111';
          if (!agentIdentityRegistryAddress) agentIdentityRegistryAddress = '0x3333333333333333333333333333333333333333';
        }
      } catch (err: any) {
        if (!devMode) throw err;
        tbaImplementationAddress = '0x2222222222222222222222222222222222222222';
        erc6551RegistryAddress = '0x1111111111111111111111111111111111111111';
        agentIdentityRegistryAddress = '0x3333333333333333333333333333333333333333';
      }
      escrowConfigCache.set(resolvedEscrowAddress, {
        tbaImplementation: tbaImplementationAddress,
        erc6551Registry: erc6551RegistryAddress,
        agentIdentityRegistry: agentIdentityRegistryAddress,
      });
    }

    // 2. 计算专属 TBA 账户地址
    const salt = '0x0000000000000000000000000000000000000000000000000000000000000000' as `0x${string}`;
    const chainId = BigInt(baseSepolia.id);

    let computedTBA: string;
    try {
      computedTBA = await publicClient.readContract({
        address: erc6551RegistryAddress as `0x${string}`,
        abi: registryAbi,
        functionName: 'account',
        args: [
          tbaImplementationAddress as `0x${string}`,
          salt,
          chainId,
          agentIdentityRegistryAddress as `0x${string}`,
          BigInt(agentId),
        ],
      }) as string;

      if (!computedTBA && devMode) {
        computedTBA = '0x4444444444444444444444444444444444444444';
      }
    } catch (err: any) {
      if (!devMode) throw err;
      computedTBA = '0x4444444444444444444444444444444444444444';
    }

    // 3. 检测该 TBA 账户是否部署
    let isDeployed = false;
    if (!devMode) {
      try {
        const bytecode = await publicClient.getBytecode({ address: computedTBA as `0x${string}` });
        isDeployed = bytecode !== undefined && bytecode !== '0x';
      } catch {
        isDeployed = false;
      }
    }

    // 4. 若未部署，调用 createAccount 自动部署
    if (!isDeployed && !devMode) {
      const { request: deployRequest } = await publicClient.simulateContract({
        account,
        address: erc6551RegistryAddress as `0x${string}`,
        abi: registryAbi,
        functionName: 'createAccount',
        args: [
          tbaImplementationAddress as `0x${string}`,
          salt,
          chainId,
          agentIdentityRegistryAddress as `0x${string}`,
          BigInt(agentId),
        ],
      });
      const deployHash = await walletClient.writeContract(deployRequest);
      await publicClient.waitForTransactionReceipt({ hash: deployHash });
    } else if (!isDeployed && devMode) {
      server.log.info(`Mock environment: Simulated deployment of TBA for agent ${agentId} at address ${computedTBA}`);
    }

    // 5. 调用 splitSettle
    const bytes32ChannelId = parseChannelId(channelId);
    const agentPayout = biAccumulated - biModelCost - platformFee;

    const payouts = {
      modelProvider,
      modelProviderPayout: biModelCost.toString(),
      platformFee: platformFee.toString(),
      agentPayout: agentPayout.toString(),
      recipient: computedTBA,
    };

    if (devMode) {
      return {
        success: true,
        txHash: '0x7777777777777777777777777777777777777777777777777777777777777777',
        mocked: true,
        computedTBA,
        payouts,
      };
    }

    const { request: splitSettleRequest } = await publicClient.simulateContract({
      account,
      address: resolvedEscrowAddress as `0x${string}`,
      abi: escrowAbi,
      functionName: 'splitSettle',
      args: [
        bytes32ChannelId,
        biAccumulated,
        biModelCost,
        biServiceFee,
        modelProvider as `0x${string}`,
        treasury as `0x${string}`,
        platformBps,
        biHoldAmount,
        biNonce,
        biExpiration,
        signature as `0x${string}`,
      ],
    });

    const hash = await walletClient.writeContract(splitSettleRequest);

    return {
      success: true,
      txHash: hash,
      computedTBA,
      payouts,
    };
  } catch (error: any) {
    server.log.warn(`Channel splitSettle transaction failed/skipped: ${error.message}. DevMode: ${devMode}`);

    if (!devMode) {
      return reply.status(500).send({
        success: false,
        error: error.message || 'Channel split settlement transaction execution failed',
      });
    }

    const agentPayout = biAccumulated - biModelCost - platformFee;

    return {
      success: true,
      txHash: '0x7777777777777777777777777777777777777777777777777777777777777777',
      mocked: true,
      computedTBA: '0x4444444444444444444444444444444444444444',
      payouts: {
        modelProvider,
        modelProviderPayout: biModelCost.toString(),
        platformFee: platformFee.toString(),
        agentPayout: agentPayout.toString(),
        recipient: '0x4444444444444444444444444444444444444444',
      },
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
          transport: getRpcTransport(rpcUrl),
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

// Health check endpoints
server.get('/health', async () => ({ status: 'OK' }));
server.get('/', async () => ({ status: 'OK' }));

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
