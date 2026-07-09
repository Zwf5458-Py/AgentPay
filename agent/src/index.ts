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

  // 1. 校验参数完整性与类型限制
  if (!body) {
    return reply.status(400).send({ error: 'Missing request body' });
  }

  const { input, agentId } = body;

  // 限制 input 必须是有效的非空字符串
  if (typeof input !== 'string' || input.trim() === '') {
    return reply.status(400).send({ error: 'Invalid input. Must be a non-empty string.' });
  }

  // 限制 agentId 必须是安全的、合规的非负整数
  if (typeof agentId !== 'number' || !Number.isSafeInteger(agentId) || agentId < 0) {
    return reply.status(400).send({ error: 'Invalid agentId. Must be a non-negative safe integer.' });
  }

  try {
    // 3. 优先尝试调用本地 LLM (如 oMLX 上运行的 Hermes 模型) 进行真实推理
    let output = `Processed by AgentPay AI: ${input}`;
    let modelId = 'deepseek-r1';
    let actualCost = 1000; // 默认基础单次推理价格 (1000 micro-units = 0.001 USDC)

    const llmUrl = process.env.LLM_API_URL || 'http://127.0.0.1:8007/v1/chat/completions';
    const llmModel = process.env.LLM_MODEL || 'Qwythos-9B-Claude-Mythos-5-1M-optiq-5bpw-mlx';
    const llmApiKey = process.env.LLM_API_KEY || 'sk-225458@';

    try {
      const llmResponse = await fetch(llmUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${llmApiKey}`
        },
        body: JSON.stringify({
          model: llmModel,
          messages: [{ role: 'user', content: input }],
          temperature: 0.7
        })
      });
      if (llmResponse.ok) {
        const data: any = await llmResponse.json();
        if (data.choices && data.choices[0] && data.choices[0].message) {
          output = data.choices[0].message.content;
          modelId = llmModel;
          console.log(`[Agent] Successfully generated inference via local model: ${output}`);

          // 根据真实 Token 消耗进行动态微支付计费
          if (data.usage) {
            const promptTokens = data.usage.prompt_tokens || 0;
            const completionTokens = data.usage.completion_tokens || 0;
            // 计费规则：输入每千 Token 1.5 micro-unit，输出每千 Token 6.0 micro-unit
            const calculatedCost = Math.round(promptTokens * 1.5 + completionTokens * 6.0);
            actualCost = Math.max(1000, calculatedCost); // 设置底价为 1000 micro-units
            console.log(`[Agent] Token usage: Prompt: ${promptTokens}, Completion: ${completionTokens}. Dynamic cost calculated: ${actualCost} micro-units`);
          }
        }
      }
    } catch (e: any) {
      console.log(`[Agent] Local LLM connection failed, falling back to mock output. Error: ${e.message}`);
    }

    // 获取私钥
    const privateKey = getAgentPrivateKey();

    // 4. 调用 generateProof，生成包含签名的 InferenceProof
    const proof = await generateProof(
      BigInt(agentId),
      input,
      output,
      modelId,
      privateKey
    );

    // 将 InferenceProof 序列化并 Base64 编码，附加至返回的 Response Header `X-Agent-Proof` 中
    const proofJson = serializeProof(proof);
    const proofBase64 = Buffer.from(proofJson).toString('base64');
    reply.header('X-Agent-Proof', proofBase64);
    
    // 注入动态计算出的推理成本到 Header 中，供 Gateway 提取并最终完成签名账本扣除
    reply.header('X-Agent-Cost', actualCost.toString());

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
