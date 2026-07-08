import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

// Declare Vitest mocks before imports so they are hoisted correctly
const mockReadContract = vi.fn();
const mockGetBalance = vi.fn().mockResolvedValue(0n);

vi.mock('viem', async (importOriginal) => {
  const original = await importOriginal<typeof import('viem')>();
  return {
    ...original,
    createPublicClient: (config: any) => {
      const client = original.createPublicClient(config);
      
      client.request = async (args: any) => {
        if (args.method === 'eth_chainId') return 84532;
        if (args.method === 'eth_getBalance') return '0x0';
        if (args.method === 'eth_getCode') return '0x';
        return null;
      };

      client.readContract = mockReadContract;
      client.getBalance = mockGetBalance;

      return client as any;
    },
  };
});

// Mock internal account module to return offline deterministic addresses for specific mock EOA owners,
// preventing ZeroDev SDK from triggering real on-chain EntryPoint.getSenderAddress eth_call RPC queries.
vi.mock('../src/kernel/account.js', async (importOriginal) => {
  const original = await importOriginal<typeof import('../src/kernel/account.js')>();
  return {
    ...original,
    getSmartAccountAddress: async (ownerAddress: string, agentId: number, salt: string) => {
      if (ownerAddress === '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266' && agentId === 100) {
        return '0xa513E1E4b9121f3538743021903c8d2d2c87848c';
      }
      return original.getSmartAccountAddress(ownerAddress, agentId, salt);
    },
  };
});

import { server } from '../src/index.js';

describe('AA Bridge API Integration Tests (Mock Mode & Production Vulnerability Fixes)', () => {
  beforeAll(async () => {
    process.env.INTERNAL_SECRET = 'test-secret';
    await server.ready();
  });

  afterAll(async () => {
    await server.close();
    vi.restoreAllMocks();
  });

  it('should create smart account deterministically in DEV_MODE', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/account/create',
      headers: {
        'x-internal-secret': 'test-secret'
      },
      payload: {
        ownerAddress: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
        agentId: 42,
        salt: 'test-salt-1'
      }
    });

    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body).toHaveProperty('smartAccountAddress');
    expect(body.smartAccountAddress).toMatch(/^0x[a-fA-F0-9]{40}$/);
    expect(body.isDeployed).toBe(false);
  });

  it('should reject invalid address formats with HTTP 400', async () => {
    // 1. Create account with invalid ownerAddress
    const createRes = await server.inject({
      method: 'POST',
      url: '/aa/account/create',
      headers: {
        'x-internal-secret': 'test-secret'
      },
      payload: {
        ownerAddress: '0xinvalidEthereumAddress',
        agentId: 42,
        salt: 'salt'
      }
    });
    expect(createRes.statusCode).toBe(400);
    expect(JSON.parse(createRes.body).error).toContain('Invalid ownerAddress format');

    // 2. Grant permission with invalid sessionKeyAddress
    const grantRes = await server.inject({
      method: 'POST',
      url: '/aa/permission/grant',
      headers: {
        'x-internal-secret': 'test-secret'
      },
      payload: {
        agentId: 42,
        sessionKeyAddress: '0xnotAnAddress',
        scopes: [],
        spendLimit: '100'
      }
    });
    expect(grantRes.statusCode).toBe(400);
    expect(JSON.parse(grantRes.body).error).toContain('Invalid sessionKeyAddress format');

    // 3. Settle with invalid agentOwner
    const settleRes = await server.inject({
      method: 'POST',
      url: '/aa/settle',
      headers: {
        'x-internal-secret': 'test-secret'
      },
      payload: {
        lockId: '0x1111111111111111111111111111111111111111111111111111111111111111',
        proof: '0xabcdef',
        agentOwner: '0xbadAddress',
        escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3'
      }
    });
    expect(settleRes.statusCode).toBe(400);
    expect(JSON.parse(settleRes.body).error).toContain('Invalid agentOwner');
  });

  it('should query account balance successfully when cached', async () => {
    const response = await server.inject({
      method: 'GET',
      url: '/aa/account/42',
      headers: {
        'x-internal-secret': 'test-secret'
      }
    });

    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body).toHaveProperty('smartAccountAddress');
    expect(body).toHaveProperty('balance');
  });

  it('should settle state channel successfully in DEV_MODE', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/settle',
      headers: {
        'x-internal-secret': 'test-secret'
      },
      payload: {
        channelId: 'channel-888',
        accumulatedAmount: '2000',
        signature: 'mock-channel-sig',
        agentId: 888,
        escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3'
      }
    });

    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body.success).toBe(true);
    expect(body.txHash).toBe('0x7777777777777777777777777777777777777777777777777777777777777777');
    expect(body.mocked).toBe(true);
    expect(body.computedTBA).toBe('0x4444444444444444444444444444444444444444');
  });

  it('should return 400 for state channel settle with missing params', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/settle',
      headers: {
        'x-internal-secret': 'test-secret'
      },
      payload: {
        channelId: 'channel-888',
        signature: 'mock-channel-sig',
        agentId: 888
        // missing accumulatedAmount
      }
    });

    expect(response.statusCode).toBe(400);
    const body = JSON.parse(response.body);
    expect(body.error).toContain('Missing channelId, accumulatedAmount, signature, or agentId');
  });

  describe('Non-DEV_MODE / Production scenarios', () => {
    const originalDevMode = process.env.DEV_MODE;
    const originalProjectId = process.env.ZERODEV_PROJECT_ID;

    beforeAll(() => {
      // Simulate production environment
      process.env.DEV_MODE = 'false';
      process.env.ZERODEV_PROJECT_ID = 'prod-project-id';
    });

    afterAll(() => {
      // Restore dev configurations
      process.env.DEV_MODE = originalDevMode;
      process.env.ZERODEV_PROJECT_ID = originalProjectId;
    });

    it('should query ownerOf from chain when cache misses and resolve address', async () => {
      // Mock AgentIdentityRegistry ownerOf and default queries
      mockReadContract.mockImplementation(async (args: any) => {
        if (args.functionName === 'ownerOf') {
          return '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266';
        }
        return '0x0000000000000000000000000000000000000000';
      });

      const response = await server.inject({
        method: 'GET',
        url: '/aa/account/100', // cache miss
        headers: {
          'x-internal-secret': 'test-secret'
        }
      });

      expect(response.statusCode).toBe(200);
      const body = JSON.parse(response.body);
      expect(body).toHaveProperty('smartAccountAddress');
      expect(body.smartAccountAddress).toMatch(/^0x[a-fA-F0-9]{40}$/);
      expect(mockReadContract).toHaveBeenCalled();
    });

    it('should return 404 when agent NFT does not exist on chain', async () => {
      // Mock ownerOf query to throw revert error indicating token doesn't exist
      mockReadContract.mockImplementation(async (args: any) => {
        if (args.functionName === 'ownerOf') {
          throw new Error('ERC721: owner query for nonexistent token (revert)');
        }
        return '0x0000000000000000000000000000000000000000';
      });

      const response = await server.inject({
        method: 'GET',
        url: '/aa/account/999', // cache miss and unregistered
        headers: {
          'x-internal-secret': 'test-secret'
        }
      });

      expect(response.statusCode).toBe(404);
      const body = JSON.parse(response.body);
      expect(body.error).toContain('Agent identity not registered');
    });

    it('should not silently fallback on /aa/settle and should return HTTP 500 upon execution errors', async () => {
      // This will fail because simulateContract will throw an error since there is no actual RPC connection
      const response = await server.inject({
        method: 'POST',
        url: '/aa/settle',
        headers: {
          'x-internal-secret': 'test-secret'
        },
        payload: {
          lockId: '0x1111111111111111111111111111111111111111111111111111111111111111',
          proof: '0xabcdef',
          agentOwner: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
          escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3'
        }
      });

      expect(response.statusCode).toBe(500);
      const body = JSON.parse(response.body);
      expect(body.success).toBe(false);
      expect(body).toHaveProperty('error');
    });

    it('should return 401 Unauthorized when x-internal-secret header is missing or incorrect', async () => {
      const res1 = await server.inject({
        method: 'GET',
        url: '/aa/account/42'
      });
      expect(res1.statusCode).toBe(401);
      expect(JSON.parse(res1.body).error).toContain('Unauthorized');

      const res2 = await server.inject({
        method: 'GET',
        url: '/aa/account/42',
        headers: {
          'x-internal-secret': 'wrong-secret'
        }
      });
      expect(res2.statusCode).toBe(401);
      expect(JSON.parse(res2.body).error).toContain('Unauthorized');
    });
  });
});
