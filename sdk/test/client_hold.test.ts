import { describe, test, expect, beforeAll, afterAll } from 'vitest';
import http from 'http';
import { privateKeyToAccount, privateKeyToAddress } from 'viem/accounts';
import { verifyTypedData } from 'viem';
import { AgentPayClient } from '../src/client.js';

const PORT = 18089;
const gatewayPrivateKey = '0x1111111111111111111111111111111111111111111111111111111111111111';
const gatewayAddress = privateKeyToAddress(gatewayPrivateKey);
const clientPrivateKey = '0x2222222222222222222222222222222222222222222222222222222222222222';
const clientAddress = privateKeyToAddress(clientPrivateKey);

let mockServer: http.Server;
let lastRequestHeaders: http.IncomingHttpHeaders;

beforeAll(async () => {
  mockServer = http.createServer((req, res) => {
    lastRequestHeaders = req.headers;
    const auth = req.headers['authorization'];

    if (!auth) {
      // 首次请求，未携带授权信息，返回 402 Challenge
      res.writeHead(402, {
        'Content-Type': 'application/json',
        'X-402-Payment-Type': 'channel',
        'X-402-Hold-Amount': '50000',
        'X-402-Payment-Address': '0x5FbDB2315678afecb367f032d93F642f64180aa3',
        'X-402-Price': '1000',
        'X-402-Version': '1',
      });
      res.end(JSON.stringify({ error: 'payment_required' }));
    } else {
      // 携带了授权信息，进行校验
      // Authorization: Bearer <channelId>:<holdAmount>:<nonce>:<expiration>:<sig>
      const token = auth.replace('Bearer ', '');
      const parts = token.split(':');
      if (parts.length !== 5) {
        res.writeHead(400);
        res.end(JSON.stringify({ error: 'invalid_auth_format' }));
        return;
      }

      const [channelId, holdAmount, nonce, expiration, sig] = parts;

      // 验证客户端签署的 EIP-712 typed data signature
      verifyTypedData({
        address: clientAddress,
        domain: {
          name: 'AgentPay',
          version: '1',
          chainId: 31337,
          verifyingContract: '0x4d5e11be368a8f5ea304197475467b3173c25eea',
        },
        types: {
          ChannelHold: [
            { name: 'channelId', type: 'bytes32' },
            { name: 'holdAmount', type: 'uint256' },
            { name: 'nonce', type: 'uint256' },
            { name: 'expiration', type: 'uint256' },
          ],
        },
        primaryType: 'ChannelHold',
        message: {
          channelId: channelId as `0x${string}`,
          holdAmount: BigInt(holdAmount),
          nonce: BigInt(nonce),
          expiration: BigInt(expiration),
        },
        signature: sig as `0x${string}`,
      }).then((isValid) => {
        if (!isValid) {
          res.writeHead(401);
          res.end(JSON.stringify({ error: 'invalid_signature' }));
          return;
        }

        // 模拟执行成功，返回 200 并携带 X-402-Settle-Receipt
        // 实际开销假设为 15000 微美分
        const actualCost = '15000';
        const gatewayAccount = privateKeyToAccount(gatewayPrivateKey);
        const receiptMessage = `${channelId}:${holdAmount}:${actualCost}:${nonce}`;

        gatewayAccount.signMessage({
          message: receiptMessage,
        }).then((receiptSig) => {
          res.writeHead(200, {
            'Content-Type': 'application/json',
            'X-402-Settle-Receipt': `${channelId}:${holdAmount}:${actualCost}:${nonce}:${receiptSig}`,
          });
          res.end(JSON.stringify({ output: 'success' }));
        }).catch((err) => {
          res.writeHead(500);
          res.end(JSON.stringify({ error: 'failed_to_sign_receipt', details: err.message }));
        });
      }).catch((err) => {
        res.writeHead(400);
        res.end(JSON.stringify({ error: 'invalid_typed_data_verification', details: err.message }));
      });
    }
  });

  await new Promise<void>((resolve) => mockServer.listen(PORT, '127.0.0.1', resolve));
});

afterAll(async () => {
  await new Promise<void>((resolve) => mockServer.close(() => resolve()));
});

describe('AgentPay SDK Credit Hold & Self-heal Unit Tests', () => {
  test('should generate EIP-712 signatures for hold and update confirmedSpend on settle receipt', async () => {
    const client = new AgentPayClient({
      gatewayUrl: `http://127.0.0.1:${PORT}`,
      privateKey: clientPrivateKey,
      gatewayAddress: gatewayAddress,
    });

    (client as any).channels.set(1, {
      id: '0x0000000000000000000000000000000000000000000000000000000000000888',
      confirmedSpend: 0n,
      accumulatedSpend: 0n,
      lastPrice: 0n,
      nonce: 1n,
      maxAmount: 50000n
    });

    const result = await client.execute(1, 'Test hold signing and receipt settlement');
    expect(result.output).toBe('success');

    const channel = (client as any).channels.get(1);
    expect(channel).toBeDefined();
    // 初始 confirmedSpend 是 0n，收到 Settle-Receipt 后应该被修正为 lastConfirmedSpend + actualCost (0n + 15000n = 15000n)
    expect(channel.confirmedSpend).toBe(15000n);
    expect(channel.accumulatedSpend).toBe(15000n);

    // 检查第二次请求头中 Authorization 的格式
    const auth = lastRequestHeaders['authorization'];
    expect(auth).toBeDefined();
    expect(auth).toMatch(/^Bearer 0x[0-9a-fA-F]{64}:50000:\d+:\d+:0x[0-9a-fA-F]+/);
  });
});
