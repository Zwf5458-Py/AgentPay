import { describe, it, expect } from 'vitest';
import { server } from '../src/index.js';
import { deserializeProof } from '../src/plugins/erc8004/proof.js';
import { getAgentAddress } from '../src/tee/mock.js';
import { encodePacked, keccak256, recoverMessageAddress } from 'viem';

describe('Agent execute endpoint & cryptography validation', () => {
  it('should successfully execute inference and return a valid signature proof', async () => {
    const payload = {
      input: 'hello world',
      agentId: 1
    };

    // 使用 Fastify 的 inject 发起 HTTP POST 请求，不会实际监听物理端口，便于并行测试
    const response = await server.inject({
      method: 'POST',
      url: '/agent/execute',
      payload
    });

    expect(response.statusCode).toBe(200);

    const body = JSON.parse(response.body);
    expect(body).toHaveProperty('output');
    expect(body.output).toBe('Processed by AgentPay AI: hello world');
    expect(body).toHaveProperty('proof');

    const proof = body.proof;
    expect(proof).toHaveProperty('agentId');
    expect(proof.agentId).toBe('1'); // JSON 序列化中 bigint 被转换为 string 传输
    expect(proof).toHaveProperty('inputHash');
    expect(proof).toHaveProperty('outputHash');
    expect(proof).toHaveProperty('modelId');
    expect(proof).toHaveProperty('timestamp');
    expect(proof).toHaveProperty('signature');

    // 验证 Response Header 中的 X-Agent-Proof
    const headerProofBase64 = response.headers['x-agent-proof'] as string;
    expect(headerProofBase64).toBeDefined();

    // 验证 Base64 完美还原为 Proof 对象
    const headerProofJson = Buffer.from(headerProofBase64, 'base64').toString('utf8');
    const headerProof = deserializeProof(headerProofJson);

    expect(headerProof.agentId).toBe(1n);
    expect(headerProof.inputHash).toBe(proof.inputHash);
    expect(headerProof.outputHash).toBe(proof.outputHash);
    expect(headerProof.modelId).toBe(proof.modelId);
    expect(headerProof.timestamp).toBe(proof.timestamp);
    expect(headerProof.signature).toBe(proof.signature);

    // 验证 Proof 对象的密码学签名合法性：
    // 1. 构造相同的数据包进行 keccak256
    const encoded = encodePacked(
      ['uint256', 'bytes32', 'bytes32', 'string', 'uint256'],
      [
        headerProof.agentId,
        headerProof.inputHash,
        headerProof.outputHash,
        headerProof.modelId,
        BigInt(headerProof.timestamp)
      ]
    );
    const messageHash = keccak256(encoded);

    // 2. 反解签名地址
    const recoveredAddress = await recoverMessageAddress({
      message: { raw: messageHash },
      signature: headerProof.signature
    });

    // 3. 验证反解出的地址与 Agent 的 Public Address 完全一致
    const expectedAddress = getAgentAddress();
    expect(recoveredAddress.toLowerCase()).toBe(expectedAddress.toLowerCase());
  });

  it('should return 400 for various invalid inputs', async () => {
    const invalidInputs = [
      { payload: { agentId: 1 }, errorMsg: 'Invalid input. Must be a non-empty string.' }, // missing input
      { payload: { input: '', agentId: 1 }, errorMsg: 'Invalid input. Must be a non-empty string.' }, // empty string
      { payload: { input: '   ', agentId: 1 }, errorMsg: 'Invalid input. Must be a non-empty string.' }, // blank string
      { payload: { input: null, agentId: 1 }, errorMsg: 'Invalid input. Must be a non-empty string.' }, // null input
      { payload: { input: 123, agentId: 1 }, errorMsg: 'Invalid input. Must be a non-empty string.' }, // non-string input
    ];

    for (const { payload, errorMsg } of invalidInputs) {
      const response = await server.inject({
        method: 'POST',
        url: '/agent/execute',
        payload
      });
      expect(response.statusCode).toBe(400);
      expect(JSON.parse(response.body).error).toBe(errorMsg);
    }
  });

  it('should return 400 for various invalid agentIds', async () => {
    const invalidAgentIds = [
      { payload: { input: 'hello' }, errorMsg: 'Invalid agentId. Must be a non-negative safe integer.' }, // missing agentId
      { payload: { input: 'hello', agentId: -1 }, errorMsg: 'Invalid agentId. Must be a non-negative safe integer.' }, // negative
      { payload: { input: 'hello', agentId: 1.5 }, errorMsg: 'Invalid agentId. Must be a non-negative safe integer.' }, // float
      { payload: { input: 'hello', agentId: NaN }, errorMsg: 'Invalid agentId. Must be a non-negative safe integer.' }, // NaN
      { payload: { input: 'hello', agentId: null }, errorMsg: 'Invalid agentId. Must be a non-negative safe integer.' }, // null agentId
      { payload: { input: 'hello', agentId: Number.MAX_SAFE_INTEGER + 10 }, errorMsg: 'Invalid agentId. Must be a non-negative safe integer.' }, // unsafe integer
    ];

    for (const { payload, errorMsg } of invalidAgentIds) {
      const response = await server.inject({
        method: 'POST',
        url: '/agent/execute',
        payload
      });
      expect(response.statusCode).toBe(400);
      expect(JSON.parse(response.body).error).toBe(errorMsg);
    }
  });
});
