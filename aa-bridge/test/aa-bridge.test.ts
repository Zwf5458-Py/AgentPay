import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { server } from '../src/index.js';

describe('AA Bridge API Integration Tests (Mock Mode)', () => {
  beforeAll(async () => {
    // Wait for fastify instance to be ready
    await server.ready();
  });

  afterAll(async () => {
    // Close the server cleanly
    await server.close();
  });

  it('should create smart account deterministically', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/account/create',
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

  it('should grant permission to session key', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/permission/grant',
      payload: {
        agentId: 42,
        sessionKeyAddress: '0x70997970C51812dc3A010C7d01b50e0d17dc79C8',
        scopes: [],
        spendLimit: '100'
      }
    });

    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body.success).toBe(true);
    expect(body.txHash).toMatch(/^0x[a-fA-F0-9]{64}$/);
  });

  it('should query account balance successfully', async () => {
    const response = await server.inject({
      method: 'GET',
      url: '/aa/account/42'
    });

    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body).toHaveProperty('smartAccountAddress');
    expect(body).toHaveProperty('balance');
    expect(body.balance).toHaveProperty('native');
    expect(body.balance).toHaveProperty('token');
  });

  it('should execute settle successfully and return txHash', async () => {
    const response = await server.inject({
      method: 'POST',
      url: '/aa/settle',
      payload: {
        lockId: '0x1111111111111111111111111111111111111111111111111111111111111111',
        proof: '0xabcdef',
        agentOwner: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
        escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3'
      }
    });

    expect(response.statusCode).toBe(200);
    const body = JSON.parse(response.body);
    expect(body.success).toBe(true);
    expect(body.txHash).toMatch(/^0x[a-fA-F0-9]{64}$/);
  });
});
