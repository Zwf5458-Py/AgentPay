import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import http from 'http';
import { AgentPayClient } from '../src/client.js';

let mockGateway: http.Server;
let mockAgent: http.Server;
let mockBridge: http.Server;

let gatewayRequestCount = 0;
let agentRequestCount = 0;
let bridgeRequestCount = 0;
let bridgeLastBody: any = null;

const BRIDGE_PORT = 13001;
const AGENT_PORT = 13002;
const GATEWAY_PORT = 18080;

beforeAll(async () => {
  // 1. Mock AA Bridge (:13001)
  mockBridge = http.createServer((req, res) => {
    if (req.method === 'POST' && req.url === '/aa/settle') {
      let body = '';
      req.on('data', chunk => { body += chunk; });
      req.on('end', () => {
        bridgeRequestCount++;
        try {
          bridgeLastBody = JSON.parse(body);
        } catch (e) {
          bridgeLastBody = null;
        }
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          success: true,
          txHash: '0x7777777777777777777777777777777777777777777777777777777777777777',
          mocked: true
        }));
      });
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  // 2. Mock Eliza Agent (:13002)
  mockAgent = http.createServer((req, res) => {
    if (req.method === 'POST' && req.url === '/agent/execute') {
      let body = '';
      req.on('data', chunk => { body += chunk; });
      req.on('end', () => {
        agentRequestCount++;
        try {
          const parsed = JSON.parse(body);
          res.writeHead(200, {
            'Content-Type': 'application/json',
            'X-Agent-Proof': 'mock-proof-base64'
          });
          res.end(JSON.stringify({
            output: `Processed by AgentPay AI: ${parsed.input}`,
            proof: { agentId: parsed.agentId, output: `Processed by AgentPay AI: ${parsed.input}` }
          }));
        } catch (e) {
          res.writeHead(400);
          res.end(JSON.stringify({ error: 'invalid_json' }));
        }
      });
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  // 3. Mock Gateway (:18080)
  mockGateway = http.createServer((req, res) => {
    if (req.method === 'POST' && req.url === '/agent/execute') {
      gatewayRequestCount++;
      const auth = req.headers['authorization'];

      if (!auth || auth !== 'Bearer lock-999:mock-token-signed') {
        // Return 402
        res.writeHead(402, {
          'Content-Type': 'application/json',
          'X-402-Price': '1000',
          'X-402-Currency': 'USDC',
          'X-402-Chain': 'base-sepolia',
          'X-402-Payment-Address': '0x5FbDB2315678afecb367f032d93F642f64180aa3',
          'X-402-Version': '1'
        });
        res.end(JSON.stringify({ error: 'payment_required' }));
      } else {
        // Forward to downstream Agent (Proxy simulator)
        let reqBody = '';
        req.on('data', chunk => { reqBody += chunk; });
        req.on('end', () => {
          const agentReq = http.request({
            host: '127.0.0.1',
            port: AGENT_PORT,
            path: '/agent/execute',
            method: 'POST',
            headers: { 'Content-Type': 'application/json' }
          }, (agentRes) => {
            let agentBody = '';
            agentRes.on('data', chunk => { agentBody += chunk; });
            agentRes.on('end', () => {
              const proof = agentRes.headers['x-agent-proof'];
              if (proof) {
                // Asynchronous settle call to AA Bridge
                const bridgeReq = http.request({
                  host: '127.0.0.1',
                  port: BRIDGE_PORT,
                  path: '/aa/settle',
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' }
                });
                bridgeReq.write(JSON.stringify({
                  lockId: 'lock-999',
                  proof: proof,
                  agentOwner: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
                  escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3'
                }));
                bridgeReq.end();
              }

              res.writeHead(200, { 'Content-Type': 'application/json' });
              res.end(agentBody);
            });
          });

          agentReq.write(reqBody);
          agentReq.end();
        });
      }
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  // Listen
  await Promise.all([
    new Promise<void>(resolve => mockBridge.listen(BRIDGE_PORT, '127.0.0.1', resolve)),
    new Promise<void>(resolve => mockAgent.listen(AGENT_PORT, '127.0.0.1', resolve)),
    new Promise<void>(resolve => mockGateway.listen(GATEWAY_PORT, '127.0.0.1', resolve)),
  ]);
});

afterAll(async () => {
  await Promise.all([
    new Promise<void>(resolve => mockBridge.close(() => resolve())),
    new Promise<void>(resolve => mockAgent.close(() => resolve())),
    new Promise<void>(resolve => mockGateway.close(() => resolve())),
  ]);
});

describe('AgentPay SDK E2E Integration Test', () => {
  it('should auto-heal from 402, retry with token and trigger bridge settle', async () => {
    const client = new AgentPayClient({
      gatewayUrl: `http://127.0.0.1:${GATEWAY_PORT}`,
      env: 'development'
    });

    const result = await client.execute(1, 'E2E Integration Test String');

    expect(result.output).toBe('Processed by AgentPay AI: E2E Integration Test String');

    // Wait for the async settlement post requests to fully finish processing
    await new Promise(resolve => setTimeout(resolve, 100));

    expect(gatewayRequestCount).toBe(2); // First failed, second success
    expect(agentRequestCount).toBe(1);   // Agent only got hit on second request
    expect(bridgeRequestCount).toBe(1);  // Bridge settle got triggered once
    expect(bridgeLastBody.lockId).toBe('lock-999');
    expect(bridgeLastBody.proof).toBe('mock-proof-base64');
  });

  it('should throw an error if the price exceeds max price limit', async () => {
    const client = new AgentPayClient({
      gatewayUrl: `http://127.0.0.1:${GATEWAY_PORT}`,
      maxPriceLimit: 500n, // Less than the mock 1000 price
      env: 'development'
    });

    await expect(client.execute(1, 'Exp')).rejects.toThrow('Price limit exceeded');
  });

  it('should throw error if maxPriceLimit is set to 0n', async () => {
    const client = new AgentPayClient({
      gatewayUrl: `http://127.0.0.1:${GATEWAY_PORT}`,
      maxPriceLimit: 0n,
      env: 'development'
    });

    await expect(client.execute(1, 'Test 0n Limit')).rejects.toThrow('Price limit exceeded');
  });
});
