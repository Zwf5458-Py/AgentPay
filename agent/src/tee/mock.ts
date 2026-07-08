import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';

let cachedPrivateKey: `0x${string}` | null = null;

/**
 * 获取 Agent 的私钥。优先从环境变量 AGENT_PRIVATE_KEY 获取。
 * 如果没有配置环境变量，则动态生成一个临时的私钥并缓存，确保不因私钥缺失而中断。
 */
export function getAgentPrivateKey(): `0x${string}` {
  const envKey = process.env.AGENT_PRIVATE_KEY;
  if (envKey) {
    if (envKey.startsWith('0x')) {
      return envKey as `0x${string}`;
    }
    return `0x${envKey}` as `0x${string}`;
  }

  if (!cachedPrivateKey) {
    cachedPrivateKey = generatePrivateKey();
  }
  return cachedPrivateKey;
}

/**
 * 获取 Agent 的公钥地址（由私钥派生）
 */
export function getAgentAddress(): `0x${string}` {
  const privateKey = getAgentPrivateKey();
  const account = privateKeyToAccount(privateKey);
  return account.address;
}
