import Fastify from 'fastify';
import dotenv from 'dotenv';
import { fileURLToPath } from 'url';
import { generateProof, serializeProof } from './plugins/erc8004/proof.js';
import { getAgentPrivateKey } from './tee/mock.js';

// 加载环境变量
dotenv.config();

const server = Fastify({
  logger: process.env.NODE_ENV !== 'test'
});

interface ExecuteBody {
  input: string;
  agentId: number;
}

// POST /agent/execute
server.post('/agent/execute', async (request, reply) => {
  const body = request.body as ExecuteBody;

  // 1. 校验参数完整性
  if (!body || body.input === undefined || body.agentId === undefined) {
    return reply.status(400).send({ error: 'Missing input or agentId' });
  }

  const { input, agentId } = body;

  // 2. 校验 agentId 必须大于等于 0
  if (typeof agentId !== 'number' || agentId < 0) {
    return reply.status(400).send({ error: 'agentId must be a non-negative number' });
  }

  try {
    // 3. 生成 mock 的推理输出
    const output = `Processed by AgentPay AI: ${input}`;

    // 获取私钥
    const privateKey = getAgentPrivateKey();

    // 4. 调用 generateProof，生成包含签名的 InferenceProof
    const proof = await generateProof(
      BigInt(agentId),
      input,
      output,
      'deepseek-r1', // mock 模型的 modelId
      privateKey
    );

    // 将 InferenceProof 序列化并 Base64 编码，附加至返回的 Response Header `X-Agent-Proof` 中
    const proofJson = serializeProof(proof);
    const proofBase64 = Buffer.from(proofJson).toString('base64');
    reply.header('X-Agent-Proof', proofBase64);

    // 5. 返回 Body 格式：{ output: string, proof: InferenceProof }
    // 使用自定义序列化以支持 bigint 字段的 JSON 传输
    const responseJson = JSON.stringify(
      { output, proof },
      (_, value) => (typeof value === 'bigint' ? value.toString() : value)
    );

    return reply.type('application/json').send(responseJson);
  } catch (error: any) {
    server.log.error(error);
    return reply.status(500).send({ error: error.message || 'Internal Server Error' });
  }
});

const start = async () => {
  try {
    const port = 3002;
    const host = '127.0.0.1';
    await server.listen({ port, host });
    console.log(`AgentPay Agent server running at http://${host}:${port}`);
  } catch (err) {
    server.log.error(err);
    process.exit(1);
  }
};

// 检查是否作为主模块运行
const isMain = process.argv[1] && (
  process.argv[1] === fileURLToPath(import.meta.url) ||
  process.argv[1].endsWith('index.ts') ||
  process.argv[1].endsWith('src/index.ts') ||
  process.argv[1].endsWith('index.js')
);

if (isMain) {
  start();
}

export { server };
