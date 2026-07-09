import { isAddress, recoverMessageAddress } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';

export interface AgentPayClientConfig {
  gatewayUrl?: string;
  maxPriceLimit?: bigint;
  privateKey?: `0x${string}`;
  env?: 'development' | 'production';
  chainId?: number;
  verifyingContract?: `0x${string}`;
  gatewayAddress?: `0x${string}`;
}

export class AgentPayClient {
  private gatewayUrl: string;
  private maxPriceLimit: bigint;
  private privateKey?: `0x${string}`;
  private env: 'development' | 'production';
  private chainId: number;
  private verifyingContract: `0x${string}`;
  private gatewayAddress?: `0x${string}`;
  private channels = new Map<number, { id: string; confirmedSpend: bigint; accumulatedSpend: bigint; lastPrice: bigint; nonce: bigint }>();
  private channelLocks = new Map<number, Promise<any>>();
  private activeRequests = new Map<number, number>();

  constructor(config: AgentPayClientConfig = {}) {
    this.gatewayUrl = config.gatewayUrl || 'http://127.0.0.1:8080';
    this.maxPriceLimit = config.maxPriceLimit ?? 5000n;
    this.privateKey = config.privateKey;
    this.env = config.env || 'production';
    this.chainId = config.chainId || 11155111;
    this.verifyingContract = config.verifyingContract || '0x5FbDB2315678afecb367f032d93F642f64180aa3';
    this.gatewayAddress = config.gatewayAddress;
  }

  public async execute(agentId: number, input: string): Promise<any> {
    const count = (this.activeRequests.get(agentId) || 0) + 1;
    this.activeRequests.set(agentId, count);

    const currentLock = this.channelLocks.get(agentId) || Promise.resolve();
    const nextLock = currentLock.then(() => this.executeInternal(agentId, input));
    
    // 捕获异常防止后续排队发生死锁阻塞，并在最终执行清理以释放 Promise 节点
    const cleanPromise = nextLock.catch(() => {}).finally(() => {
      const currentCount = this.activeRequests.get(agentId) || 0;
      if (currentCount <= 1) {
        this.activeRequests.delete(agentId);
        this.channelLocks.delete(agentId);
      } else {
        this.activeRequests.set(agentId, currentCount - 1);
      }
    });

    this.channelLocks.set(agentId, cleanPromise);
    return nextLock;
  }

  private async executeInternal(agentId: number, input: string): Promise<any> {
    if (typeof input !== 'string' || input.trim() === '') {
      throw new Error('Invalid input. Must be a non-empty string.');
    }
    if (typeof agentId !== 'number' || !Number.isSafeInteger(agentId) || agentId < 0) {
      throw new Error('Invalid agentId. Must be a non-negative safe integer.');
    }

    // 记录发起本轮请求前的 confirmedSpend
    const channelBefore = this.channels.get(agentId);
    const lastConfirmedSpend = channelBefore ? channelBefore.confirmedSpend : 0n;

    // 检查本地通道缓存，决定是否预先注入 Authorization
    let authHeader: string | undefined;
    const channel = this.channels.get(agentId);
    if (channel && channel.lastPrice > 0n) {
      if (this.privateKey) {
        const holdAmount = channel.lastPrice;
        const currentNonce = channel.nonce;
        channel.nonce += 1n;
        const expiration = BigInt(Math.floor(Date.now() / 1000) + 3600);

        const account = privateKeyToAccount(this.privateKey);
        const sig = await account.signTypedData({
          domain: {
            name: 'AgentPay',
            version: '1',
            chainId: this.chainId,
            verifyingContract: this.verifyingContract,
          },
          types: {
            ChannelHold: [
              { name: 'channelId', type: 'bytes32' },
              { name: 'holdAmount', type: 'uint256' },
              { name: 'nonce', type: 'uint256' },
              { name: 'expiration', type: 'uint256' },
            ]
          },
          primaryType: 'ChannelHold',
          message: {
            channelId: channel.id as `0x${string}`,
            holdAmount,
            nonce: currentNonce,
            expiration,
          }
        });

        authHeader = `Bearer ${channel.id}:${holdAmount}:${currentNonce}:${expiration}:${sig}`;
      } else {
        channel.accumulatedSpend = channel.confirmedSpend + channel.lastPrice;
        authHeader = `Bearer ${channel.id}:${channel.accumulatedSpend}:mock-channel-sig`;
      }
    }

    // 发起第一次请求
    let response = await this.sendRequest(agentId, input, authHeader);

    // 挑战捕获与自愈 (HTTP 402)
    if (response.status === 402) {
      const paymentType = response.headers.get('X-402-Payment-Type');

      if (paymentType === 'channel') {
        const priceStr = response.headers.get('X-402-Price');
        if (!priceStr) {
          throw new Error('Invalid HTTP 402 response from Gateway: missing price header');
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

        const holdAmountStr = response.headers.get('X-402-Hold-Amount') || '50000';
        const holdAmount = BigInt(holdAmountStr);

        let channelObj = this.channels.get(agentId);
        if (!channelObj) {
          const defaultId = this.privateKey 
            ? '0x0000000000000000000000000000000000000000000000000000000000000888' 
            : 'channel-888';
          channelObj = {
            id: defaultId,
            confirmedSpend: 0n,
            accumulatedSpend: 0n,
            lastPrice: 0n,
            nonce: 1n,
          };
          this.channels.set(agentId, channelObj);
        }

        channelObj.lastPrice = price;
        channelObj.accumulatedSpend = channelObj.confirmedSpend + price;

        let authorizationHeader = '';
        if (this.privateKey) {
          const currentNonce = channelObj.nonce;
          channelObj.nonce += 1n;
          const expiration = BigInt(Math.floor(Date.now() / 1000) + 3600);

          const account = privateKeyToAccount(this.privateKey);
          const sig = await account.signTypedData({
            domain: {
              name: 'AgentPay',
              version: '1',
              chainId: this.chainId,
              verifyingContract: this.verifyingContract,
            },
            types: {
              ChannelHold: [
                { name: 'channelId', type: 'bytes32' },
                { name: 'holdAmount', type: 'uint256' },
                { name: 'nonce', type: 'uint256' },
                { name: 'expiration', type: 'uint256' },
              ]
            },
            primaryType: 'ChannelHold',
            message: {
              channelId: channelObj.id as `0x${string}`,
              holdAmount,
              nonce: currentNonce,
              expiration,
            }
          });

          authorizationHeader = `Bearer ${channelObj.id}:${holdAmount}:${currentNonce}:${expiration}:${sig}`;
        } else {
          authorizationHeader = `Bearer ${channelObj.id}:${channelObj.accumulatedSpend}:mock-channel-sig`;
        }

        response = await this.sendRequest(agentId, input, authorizationHeader);
      } else {
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
    }

    if (response.status !== 200) {
      const errorText = await response.text();
      throw new Error(`Request failed with status ${response.status}: ${errorText}`);
    }

    const receipt = response.headers.get('X-402-Settle-Receipt');
    let hasUpdatedViaReceipt = false;

    if (receipt) {
      const parts = receipt.split(':');
      if (parts.length === 5) {
        const [channelId, holdAmountStr, actualCostStr, nonceStr, receiptSig] = parts;
        const actualCost = BigInt(actualCostStr);

        if (this.gatewayAddress) {
          const message = `${channelId}:${holdAmountStr}:${actualCostStr}:${nonceStr}`;
          try {
            const recovered = await recoverMessageAddress({
              message,
              signature: receiptSig as `0x${string}`,
            });
            if (recovered.toLowerCase() !== this.gatewayAddress.toLowerCase()) {
              throw new Error(`Invalid gateway signature. Recovered: ${recovered}, Expected: ${this.gatewayAddress}`);
            }
          } catch (err: any) {
            throw new Error(`Gateway signature verification failed: ${err.message}`);
          }
        }

        const updatedChannel = this.channels.get(agentId);
        if (updatedChannel) {
          updatedChannel.confirmedSpend = lastConfirmedSpend + actualCost;
          updatedChannel.accumulatedSpend = updatedChannel.confirmedSpend;
          updatedChannel.lastPrice = actualCost;
          hasUpdatedViaReceipt = true;
        }
      }
    }

    if (!hasUpdatedViaReceipt) {
      const updatedChannel = this.channels.get(agentId);
      if (updatedChannel && updatedChannel.accumulatedSpend > updatedChannel.confirmedSpend) {
        updatedChannel.confirmedSpend = updatedChannel.accumulatedSpend;
      }
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
