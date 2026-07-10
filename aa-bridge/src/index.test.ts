import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

// Declare Vitest mocks before imports so they are hoisted correctly
const mockReadContract = vi.fn();
const mockGetBalance = vi.fn().mockResolvedValue(0n);

vi.mock('viem', async (importOriginal) => {
  const original = await importOriginal<typeof import('viem')>();
  return {
    ...original,
    createPublicClient: (config: any) => {
      const client = original.createPublicClient(config) as any;
      
      client.request = async (args: any) => {
        if (args.method === 'eth_chainId') return 84532;
        if (args.method === 'eth_getBalance') return '0x0';
        if (args.method === 'eth_getCode') return '0x';
        return null;
      };

      client.readContract = mockReadContract;
      client.getBalance = mockGetBalance;
      client.getBytecode = vi.fn().mockResolvedValue('0x');
      client.simulateContract = vi.fn().mockRejectedValue(new Error('Mock simulation error'));

      return client as any;
    },
  };
});

// Mock internal account module to prevent ZeroDev SDK from triggering real on-chain queries.
vi.mock('./kernel/account.js', async (importOriginal) => {
  const original = await importOriginal<typeof import('./kernel/account.js')>();
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

import { server } from './index.js';

describe('AA Bridge API Integration Tests - split-settle', () => {
  beforeAll(async () => {
    process.env.INTERNAL_SECRET = 'test-secret';
    process.env.DEV_MODE = 'true';
    await server.ready();
  });

  afterAll(async () => {
    await server.close();
    vi.restoreAllMocks();
  });

  it('should successfully split settle in DEV_MODE with correct payouts', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/split-settle',
      headers: {
        'x-internal-secret': 'test-secret',
      },
      payload: {
        channelId: 'channel-test-1',
        accumulatedAmount: '1000',
        modelCost: '200',
        serviceFee: '100',
        modelProvider: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
        treasury: '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
        platformBps: 500, // 5% platform fee = 50
        holdAmount: '2000',
        nonce: '1',
        expiration: '9999999999',
        signature: '0xsignature',
        proof: '0xproof',
        agentId: 100,
        escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3',
      },
    });

    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body.success).toBe(true);
    expect(body.txHash).toBe('0x7777777777777777777777777777777777777777777777777777777777777777');
    expect(body.mocked).toBe(true);
    expect(body.computedTBA).toBe('0x4444444444444444444444444444444444444444');
    expect(body.payouts).toEqual({
      modelProvider: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
      modelProviderPayout: '200',
      platformFee: '50', // 1000 * 500 / 10000 = 50
      agentPayout: '650', // 1000 - 200 - 50 - 100 = 650
      recipient: '0x4444444444444444444444444444444444444444',
    });
  });

  it('should return 400 if required parameters are missing', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/split-settle',
      headers: {
        'x-internal-secret': 'test-secret',
      },
      payload: {
        channelId: 'channel-test-1',
        accumulatedAmount: '1000',
        // missing modelCost
        serviceFee: '100',
        modelProvider: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
        treasury: '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
        platformBps: 500,
        holdAmount: '2000',
        nonce: '1',
        expiration: '9999999999',
        signature: '0xsignature',
        agentId: 100,
      },
    });

    expect(response.statusCode).toBe(400);
    const body = JSON.parse(response.body);
    expect(body.error).toContain('Missing required parameters');
  });

  it('should return 400 for invalid modelProvider or treasury address', async () => {
    const res1 = await server.inject({
      method: 'POST',
      url: '/aa/split-settle',
      headers: {
        'x-internal-secret': 'test-secret',
      },
      payload: {
        channelId: 'channel-test-1',
        accumulatedAmount: '1000',
        modelCost: '200',
        serviceFee: '100',
        modelProvider: 'invalid-address',
        treasury: '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
        platformBps: 500,
        holdAmount: '2000',
        nonce: '1',
        expiration: '9999999999',
        signature: '0xsignature',
        proof: '0xproof',
        agentId: 100,
      },
    });

    expect(res1.statusCode).toBe(400);
    expect(JSON.parse(res1.body).error).toContain('Invalid modelProvider address');

    const res2 = await server.inject({
      method: 'POST',
      url: '/aa/split-settle',
      headers: {
        'x-internal-secret': 'test-secret',
      },
      payload: {
        channelId: 'channel-test-1',
        accumulatedAmount: '1000',
        modelCost: '200',
        serviceFee: '100',
        modelProvider: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
        treasury: 'invalid-address',
        platformBps: 500,
        holdAmount: '2000',
        nonce: '1',
        expiration: '9999999999',
        signature: '0xsignature',
        proof: '0xproof',
        agentId: 100,
      },
    });

    expect(res2.statusCode).toBe(400);
    expect(JSON.parse(res2.body).error).toContain('Invalid treasury address');
  });

  it('should return 400 if sum of modelCost, serviceFee, and platformFee exceeds accumulatedAmount', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/split-settle',
      headers: {
        'x-internal-secret': 'test-secret',
      },
      payload: {
        channelId: 'channel-test-1',
        accumulatedAmount: '1000',
        modelCost: '800',
        serviceFee: '200', // 800 + 200 + 50 (platform fee) = 1050 > 1000
        modelProvider: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
        treasury: '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
        platformBps: 500,
        holdAmount: '2000',
        nonce: '1',
        expiration: '9999999999',
        signature: '0xsignature',
        proof: '0xproof',
        agentId: 100,
        escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3',
      },
    });

    expect(response.statusCode).toBe(400);
    const body = JSON.parse(response.body);
    expect(body.error).toContain('modelCost + serviceFee + platformFee exceeds accumulatedAmount');
  });

  describe('Non-DEV_MODE scenarios', () => {
    const originalDevMode = process.env.DEV_MODE;
    const originalProjectId = process.env.ZERODEV_PROJECT_ID;

    beforeAll(() => {
      process.env.DEV_MODE = 'false';
      process.env.ZERODEV_PROJECT_ID = 'prod-project-id';
    });

    afterAll(() => {
      process.env.DEV_MODE = originalDevMode;
      process.env.ZERODEV_PROJECT_ID = originalProjectId;
    });

    it('should fail with HTTP 500 upon on-chain execution errors when not in DEV_MODE', async () => {
      const response = await server.inject({
        method: 'POST',
        url: '/aa/split-settle',
        headers: {
          'x-internal-secret': 'test-secret',
        },
        payload: {
          channelId: 'channel-test-1',
          accumulatedAmount: '1000',
          modelCost: '200',
          serviceFee: '100',
          modelProvider: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
          treasury: '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
          platformBps: 500,
          holdAmount: '2000',
          nonce: '1',
          expiration: '9999999999',
          signature: '0xsignature',
          proof: '0xproof',
          agentId: 100,
          escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3',
        },
      });

      expect(response.statusCode).toBe(500);
      const body = JSON.parse(response.body);
      expect(body.success).toBe(false);
      expect(body.error).toBeDefined();
    });
  });
});
