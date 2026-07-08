import { isAddress } from 'viem';

export interface AgentPayClientConfig {
  gatewayUrl?: string;
  maxPriceLimit?: bigint;
  privateKey?: `0x${string}`;
  env?: 'development' | 'production';
}

export class AgentPayClient {
  private gatewayUrl: string;
  private maxPriceLimit: bigint;
  private privateKey?: `0x${string}`;
  private env: 'development' | 'production';

  constructor(config: AgentPayClientConfig = {}) {
    this.gatewayUrl = config.gatewayUrl || 'http://127.0.0.1:8080';
    this.maxPriceLimit = config.maxPriceLimit ?? 5000n;
    this.privateKey = config.privateKey;
    this.env = config.env || 'production';
  }

  public async execute(agentId: number, input: string): Promise<any> {
    if (typeof input !== 'string' || input.trim() === '') {
      throw new Error('Invalid input. Must be a non-empty string.');
    }
    if (typeof agentId !== 'number' || !Number.isSafeInteger(agentId) || agentId < 0) {
      throw new Error('Invalid agentId. Must be a non-negative safe integer.');
    }

    // 发起第一次请求
    let response = await this.sendRequest(agentId, input);

    // 挑战捕获与自愈 (HTTP 402)
    if (response.status === 402) {
      const priceStr = response.headers.get('X-402-Price');
      const paymentAddress = response.headers.get('X-402-Payment-Address');

      if (!priceStr || !paymentAddress) {
        throw new Error('Invalid HTTP 402 response from Gateway: missing headers');
      }

      if (!isAddress(paymentAddress)) {
        throw new Error('Invalid payment address in HTTP 402 headers');
      }

      let price: bigint;
      try {
        price = BigInt(priceStr);
      } catch (err) {
        throw new Error(`Invalid HTTP 402 price format from Gateway: ${priceStr}`);
      }

      if (price > this.maxPriceLimit) {
        throw new Error(`Price limit exceeded: Price is ${price}, max limit is ${this.maxPriceLimit}`);
      }

      // 生成授权锁与 Token
      let authorizationHeader = '';
      if (this.env === 'development') {
        const lockId = 'lock-999';
        const token = 'mock-token-signed';
        authorizationHeader = `Bearer ${lockId}:${token}`;
      } else {
        if (!this.privateKey) {
          throw new Error('Private key is required for production signing');
        }
        // TODO: Implement EIP-3009 EIP-712 typing signature verification using viem.signTypedData
        // 模拟/简化的 production 签名，可以带上生成的 mock lockId
        const mockLockId = `lock-${Date.now()}`;
        authorizationHeader = `Bearer ${mockLockId}:production-eip3009-signed-token`;
      }

      // 二次请求
      response = await this.sendRequest(agentId, input, authorizationHeader);
    }

    if (response.status !== 200) {
      const errorText = await response.text();
      throw new Error(`Request failed with status ${response.status}: ${errorText}`);
    }

    return response.json();
  }

  private async sendRequest(agentId: number, input: string, authHeader?: string): Promise<Response> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    };
    if (authHeader) {
      headers['Authorization'] = authHeader;
    }

    return fetch(`${this.gatewayUrl}/agent/execute`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ agentId, input }),
    });
  }
}
