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
let mockGatewayChannelSpend = 0n;

const BRIDGE_PORT = 13001;
const AGENT_PORT = 13002;
const GATEWAY_PORT = 18080;

beforeAll(async () => {
  // 1. Mock AA Bridge (:13001)
  mockBridge = http.createServer((req, res) => {
    const secretHeader = req.headers['x-internal-secret'];
    const expectedSecret = process.env.INTERNAL_SECRET || 'test-secret';
    if (!secretHeader || secretHeader !== expectedSecret) {
      res.writeHead(401, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ error: 'unauthorized' }));
      return;
    }
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
      let reqBody = '';
      req.on('data', chunk => { reqBody += chunk; });
      req.on('end', () => {
        gatewayRequestCount++;
        const auth = req.headers['authorization'];
        let parsedBody: any = {};
        try {
          parsedBody = JSON.parse(reqBody);
        } catch (e) {}
        const agentId = parsedBody.agentId;

        if (agentId === 888) {
          // 通道模式的校验与处理
          let isChannelAuthValid = false;
          let currentSpend = 0n;
          let signature = '';
          let channelId = '';

          if (auth && auth.startsWith('Bearer channel-')) {
            const parts = auth.substring(7).split(':');
            if (parts.length === 5) {
              channelId = parts[0];
              signature = parts[4];
              if (signature === 'mock-channel-sig') {
                isChannelAuthValid = true;
                mockGatewayChannelSpend += 1000n;
                currentSpend = mockGatewayChannelSpend;
              }
            }
          }

          if (isChannelAuthValid) {
            // 转发下游 Eliza Agent
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
                // 异步 settle 调用
                const bridgeReq = http.request({
                  host: '127.0.0.1',
                  port: BRIDGE_PORT,
                  path: '/aa/settle',
                  method: 'POST',
                  headers: {
                    'Content-Type': 'application/json',
                    'x-internal-secret': process.env.INTERNAL_SECRET || 'test-secret'
                  }
                });
                const proof = agentRes.headers['x-agent-proof'] || 'mock-proof-base64';
                bridgeReq.write(JSON.stringify({
                  channelId,
                  accumulatedAmount: currentSpend.toString(),
                  signature,
                  agentId,
                  proof,
                  escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3'
                }));
                bridgeReq.end();

                res.writeHead(200, { 'Content-Type': 'application/json' });
                res.end(agentBody);
              });
            });
            agentReq.write(reqBody);
            agentReq.end();
          } else {
            // 返回 402 通道协商首部
            res.writeHead(402, {
              'Content-Type': 'application/json',
              'X-402-Payment-Type': 'channel',
              'X-402-Price': '1000',
              'X-402-Currency': 'USDC',
              'X-402-Chain': 'base-sepolia',
              'X-402-Payment-Address': '0x5FbDB2315678afecb367f032d93F642f64180aa3',
              'X-402-Version': '1'
            });
            res.end(JSON.stringify({ error: 'payment_required' }));
          }
        } else {
          // 原有的 lockId 校验与处理
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
                    headers: {
                      'Content-Type': 'application/json',
                      'x-internal-secret': process.env.INTERNAL_SECRET || 'test-secret'
                    }
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
          }
        }
      });
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

    const result = await client.execute(888, 'E2E Integration Test String');

    expect(result.output).toBe('Processed by AgentPay AI: E2E Integration Test String');

    // Wait for the async settlement post requests to fully finish processing
    await new Promise(resolve => setTimeout(resolve, 100));

    expect(gatewayRequestCount).toBe(2); // First failed, second success
    expect(agentRequestCount).toBe(1);   // Agent only got hit on second request
    expect(bridgeRequestCount).toBe(1);  // Bridge settle got triggered once
    expect(bridgeLastBody.channelId).toBe('channel-888');
    expect(bridgeLastBody.proof).toBe('mock-proof-base64');
  });

  it('should support state channel adaptive spend accumulation over multiple calls', async () => {
    // 重置全局 Mock 计数器和网关通道额度
    gatewayRequestCount = 0;
    agentRequestCount = 0;
    bridgeRequestCount = 0;
    bridgeLastBody = null;
    mockGatewayChannelSpend = 0n;

    const client = new AgentPayClient({
      gatewayUrl: `http://127.0.0.1:${GATEWAY_PORT}`,
      env: 'development'
    });

    // 并发触发两个请求
    const [result1, result2] = await Promise.all([
      client.execute(888, 'Channel Call 1'),
      client.execute(888, 'Channel Call 2')
    ]);

    expect(result1.output).toBe('Processed by AgentPay AI: Channel Call 1');
    expect(result2.output).toBe('Processed by AgentPay AI: Channel Call 2');

    // 等待异步网关转发和桥结算完成
    await new Promise(resolve => setTimeout(resolve, 100));

    expect(gatewayRequestCount).toBe(3); // 第一笔 1 次 402 + 1 次重试，第二笔排队执行直接携带累计额通过，共 3 次请求
    expect(agentRequestCount).toBe(2);
    expect(bridgeRequestCount).toBe(2);
    expect(bridgeLastBody.channelId).toBe('channel-888');
    expect(bridgeLastBody.accumulatedAmount).toBe('2000');
  });

  it('should throw an error if the price exceeds max price limit', async () => {
    const client = new AgentPayClient({
      gatewayUrl: `http://127.0.0.1:${GATEWAY_PORT}`,
      maxPriceLimit: 500n, // Less than the mock 1000 price
      env: 'development'
    });

    await expect(client.execute(888, 'Exp')).rejects.toThrow('Price limit exceeded');
  });

  it('should throw error if maxPriceLimit is set to 0n', async () => {
    const client = new AgentPayClient({
      gatewayUrl: `http://127.0.0.1:${GATEWAY_PORT}`,
      maxPriceLimit: 0n,
      env: 'development'
    });

    await expect(client.execute(888, 'Test 0n Limit')).rejects.toThrow('Price limit exceeded');
  });

  it('should return 401 from mockBridge if x-internal-secret header is missing', async () => {
    await new Promise<void>((resolve, reject) => {
      const req = http.request({
        host: '127.0.0.1',
        port: BRIDGE_PORT,
        path: '/aa/settle',
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      }, (res) => {
        expect(res.statusCode).toBe(401);
        resolve();
      });
      req.on('error', reject);
      req.write(JSON.stringify({}));
      req.end();
    });
  });
});
