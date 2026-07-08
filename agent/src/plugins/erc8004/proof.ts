import { encodePacked, keccak256, toBytes } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';

export interface InferenceProof {
  agentId: bigint;
  inputHash: `0x${string}`;
  outputHash: `0x${string}`;
  modelId: string;
  timestamp: number;
  signature: `0x${string}`;
}

/**
 * 根据 ERC-8004 规范生成 AI 推理证明与签名
 * 
 * @param agentId 智能体 ID
 * @param input 推理输入文本
 * @param output 推理输出文本
 * @param modelId 模型 ID
 * @param privateKey 用于签名的私钥
 */
export async function generateProof(
  agentId: bigint,
  input: string,
  output: string,
  modelId: string,
  privateKey: `0x${string}`
): Promise<InferenceProof> {
  // 1. 计算 inputHash = keccak256(toBytes(input))
  const inputHash = keccak256(toBytes(input));

  // 2. 计算 outputHash = keccak256(toBytes(output))
  const outputHash = keccak256(toBytes(output));

  // 3. 获取当前时间戳
  const timestamp = Math.floor(Date.now() / 1000);

  // 4. 构造需要被签名的结构体消息，使用 Solidity ABI 兼容的打包编码
  // 打包顺序与类型：[agentId (uint256), inputHash (bytes32), outputHash (bytes32), modelId (string), timestamp (uint256)]
  const encoded = encodePacked(
    ['uint256', 'bytes32', 'bytes32', 'string', 'uint256'],
    [agentId, inputHash, outputHash, modelId, BigInt(timestamp)]
  );
  const messageHash = keccak256(encoded);

  // 5. 使用私钥进行 EIP-191 签名
  const account = privateKeyToAccount(privateKey);
  const signature = await account.signMessage({
    message: { raw: messageHash }
  });

  return {
    agentId,
    inputHash,
    outputHash,
    modelId,
    timestamp,
    signature
  };
}

/**
 * 将 InferenceProof 序列化为 JSON 字符串，安全处理 bigint 类型
 */
export function serializeProof(proof: InferenceProof): string {
  return JSON.stringify(proof, (_, value) => 
    typeof value === 'bigint' ? value.toString() : value
  );
}

/**
 * 将 JSON 字符串反序列化为 InferenceProof 对象，恢复 bigint 类型
 */
export function deserializeProof(jsonStr: string): InferenceProof {
  const parsed = JSON.parse(jsonStr);
  return {
    ...parsed,
    agentId: BigInt(parsed.agentId)
  };
}
