import { isAddress, recoverMessageAddress, createWalletClient, createPublicClient, http, custom, parseAbi, parseAbiItem, publicActions } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { foundry, baseSepolia } from 'viem/chains';

export interface AgentPayClientConfig {
  gatewayUrl?: string;
  maxPriceLimit?: bigint;
  privateKey?: `0x${string}`;
  provider?: any;
  env?: 'development' | 'production';
  chainId?: number;
  verifyingContract?: `0x${string}`;
  erc20Address?: `0x${string}`;
  gatewayAddress?: `0x${string}`;
}

export class AgentPayClient {
  private gatewayUrl: string;
  private maxPriceLimit: bigint;
  private privateKey?: `0x${string}`;
  private provider?: any;
  private env: 'development' | 'production';
  private chainId: number;
  private verifyingContract: `0x${string}`;
  private erc20Address: `0x${string}`;
  private gatewayAddress?: `0x${string}`;
  private channels = new Map<number, { id: string; confirmedSpend: bigint; accumulatedSpend: bigint; lastPrice: bigint; nonce: bigint; maxAmount: bigint }>();
  private channelLocks = new Map<number, Promise<any>>();
  private activeRequests = new Map<number, number>();

  private walletClient?: any;
  private publicClient?: any;

  constructor(config: AgentPayClientConfig = {}) {
    this.gatewayUrl = config.gatewayUrl || 'http://127.0.0.1:8080';
    this.maxPriceLimit = config.maxPriceLimit ?? 5000n;
    this.privateKey = config.privateKey;
    this.provider = config.provider;
    this.env = config.env || 'production';
    this.chainId = config.chainId || 31337;
    this.verifyingContract = config.verifyingContract || '0x4d5e11be368a8f5ea304197475467b3173c25eea';
    this.erc20Address = config.erc20Address || '0x43b22e11d0444ce69528e5db1a868f7b587b1c3c';
    this.gatewayAddress = config.gatewayAddress;

    const chain = this.chainId === 31337 ? foundry : baseSepolia;
    if (this.provider) {
      this.walletClient = createWalletClient({ chain, transport: custom(this.provider) }).extend(publicActions);
      this.publicClient = createPublicClient({ chain, transport: custom(this.provider) });
    } else if (this.privateKey) {
      const account = privateKeyToAccount(this.privateKey);
      this.walletClient = createWalletClient({ account, chain, transport: http() }).extend(publicActions);
      this.publicClient = createPublicClient({ chain, transport: http() });
    }
  }

  public getChannel(agentId: number) {
    return this.channels.get(agentId);
  }

  public async openChannel(agentId: number, amount: bigint, duration: bigint = 3600n): Promise<string> {
    if (!this.walletClient || !this.publicClient) {
      throw new Error('Wallet/Provider is not configured');
    }

    let userAddress = this.walletClient.account?.address;
    if (!userAddress && this.provider) {
      const accounts = await this.walletClient.requestAddresses();
      userAddress = accounts[0];
    }
    if (!userAddress) throw new Error('No account found');

    const approveAbi = parseAbi(['function approve(address spender, uint256 amount) returns (bool)']);
    const approveHash = await this.walletClient.writeContract({
      address: this.erc20Address,
      abi: approveAbi,
      functionName: 'approve',
      args: [this.verifyingContract, amount],
      account: userAddress
    });
    await this.publicClient.waitForTransactionReceipt({ hash: approveHash });

    const escrowAbi = parseAbi(['function lockChannel(uint256 agentId, uint256 amount, uint256 duration)']);
    const lockHash = await this.walletClient.writeContract({
      address: this.verifyingContract,
      abi: escrowAbi,
      functionName: 'lockChannel',
      args: [BigInt(agentId), amount, duration],
      account: userAddress
    });
    
    const receipt = await this.publicClient.waitForTransactionReceipt({ hash: lockHash });
    
    const eventAbi = parseAbiItem('event ChannelLocked(bytes32 indexed channelId, uint256 indexed agentId, address indexed payer, uint256 amount, uint256 expiration)');
    const logs = await this.publicClient.getLogs({
      address: this.verifyingContract,
      event: eventAbi,
      fromBlock: receipt.blockNumber,
      toBlock: receipt.blockNumber
    });
    
    let channelId = '';
    for (const log of logs) {
      if (log.args && log.args.agentId === BigInt(agentId) && log.args.payer?.toLowerCase() === userAddress.toLowerCase()) {
        channelId = log.args.channelId as string;
        break;
      }
    }
    
    if (!channelId) {
       throw new Error('Failed to parse channelId from ChannelLocked event log');
    }

    this.channels.set(agentId, {
      id: channelId,
      confirmedSpend: 0n,
      accumulatedSpend: 0n,
      lastPrice: 0n,
      nonce: 1n,
      maxAmount: amount
    });
    
    return channelId;
  }

  public async execute(agentId: number, input: string): Promise<any> {
    const count = (this.activeRequests.get(agentId) || 0) + 1;
    this.activeRequests.set(agentId, count);

    const currentLock = this.channelLocks.get(agentId) || Promise.resolve();
    const nextLock = currentLock.then(() => this.executeInternal(agentId, input));
    
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

    const channelBefore = this.channels.get(agentId);
    const lastConfirmedSpend = channelBefore ? channelBefore.confirmedSpend : 0n;

    let authHeader: string | undefined;
    const channel = this.channels.get(agentId);
    
    let signFunc = async (cObj: any, holdAmt: bigint, currentN: bigint, exp: bigint) => {
       if (this.walletClient && this.publicClient) {
          let userAddr = this.walletClient.account;
          if (!userAddr && this.provider) {
             const accs = await this.walletClient.requestAddresses();
             userAddr = accs[0];
          }
          if (!userAddr) throw new Error('No account available for signing');
          return await this.walletClient.signTypedData({
              account: userAddr,
              domain: { name: 'AgentPay', version: '1', chainId: this.chainId, verifyingContract: this.verifyingContract },
              types: {
                ChannelHold: [
                  { name: 'channelId', type: 'bytes32' },
                  { name: 'holdAmount', type: 'uint256' },
                  { name: 'nonce', type: 'uint256' },
                  { name: 'expiration', type: 'uint256' },
                ]
              },
              primaryType: 'ChannelHold',
              message: { channelId: cObj.id as `0x${string}`, holdAmount: holdAmt, nonce: currentN, expiration: exp }
          });
       } else if (this.env === 'development') {
          return 'mock-channel-sig';
       }
       throw new Error('Wallet/Provider is required to sign ChannelHold');
    };

    if (channel && channel.lastPrice > 0n) {
        const holdAmount = channel.maxAmount || channel.lastPrice;
        const currentNonce = channel.nonce;
        channel.nonce += 1n;
        const expiration = BigInt(Math.floor(Date.now() / 1000) + 3600);
        
        const sig = await signFunc(channel, holdAmount, currentNonce, expiration);
        authHeader = `Bearer ${channel.id}:${holdAmount}:${currentNonce}:${expiration}:${sig}`;
        channel.accumulatedSpend = channel.confirmedSpend + channel.lastPrice;
    }

    let response = await this.sendRequest(agentId, input, authHeader);

    if (response.status === 402) {
      const paymentType = response.headers.get('X-402-Payment-Type');

      if (paymentType === 'channel') {
        const priceStr = response.headers.get('X-402-Price');
        if (!priceStr) throw new Error('Missing price header');
        const price = BigInt(priceStr);
        if (price > this.maxPriceLimit) throw new Error('Price limit exceeded');

        const holdAmountStr = response.headers.get('X-402-Hold-Amount') || '50000';
        const holdAmount = BigInt(holdAmountStr);

        let channelObj = this.channels.get(agentId);
        if (!channelObj) {
           if (this.env === 'development' && !this.walletClient) {
              channelObj = { id: 'channel-888', confirmedSpend: 0n, accumulatedSpend: 0n, lastPrice: 0n, nonce: 1n, maxAmount: holdAmount };
              this.channels.set(agentId, channelObj);
           } else {
              throw new Error('On-chain channel must be opened via openChannel() prior to execution');
           }
        }

        channelObj.lastPrice = price;
        channelObj.accumulatedSpend = channelObj.confirmedSpend + price;

        const currentNonce = channelObj.nonce;
        channelObj.nonce += 1n;
        const expiration = BigInt(Math.floor(Date.now() / 1000) + 3600);
        const sig = await signFunc(channelObj, channelObj.maxAmount || holdAmount, currentNonce, expiration);

        const authorizationHeader = `Bearer ${channelObj.id}:${channelObj.maxAmount || holdAmount}:${currentNonce}:${expiration}:${sig}`;
        response = await this.sendRequest(agentId, input, authorizationHeader);
      } else {
         throw new Error('Unsupported alternative payment flow in SDK');
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
        const [receiptChannelId, holdAmountStr, actualCostStr, nonceStr, receiptSig] = parts;
        const actualCost = BigInt(actualCostStr);
        const nonceVal = BigInt(nonceStr);

        const activeChannel = this.channels.get(agentId);
        if (activeChannel && activeChannel.id !== receiptChannelId) {
            throw new Error(`Receipt channelId mismatch. Expected: ${activeChannel.id}, got: ${receiptChannelId}`);
        }
        if (activeChannel && nonceVal > activeChannel.nonce) {
            throw new Error('Receipt nonce is unexpectedly ahead');
        }

        if (this.gatewayAddress && this.env !== 'development') {
          const message = `${receiptChannelId}:${holdAmountStr}:${actualCostStr}:${nonceStr}`;
          try {
            const recovered = await recoverMessageAddress({ message, signature: receiptSig as `0x${string}` });
            if (recovered.toLowerCase() !== this.gatewayAddress.toLowerCase()) {
              throw new Error(`Invalid gateway signature`);
            }
          } catch (err: any) {
            throw new Error(`Signature verification failed: ${err.message}`);
          }
        }

        if (activeChannel) {
          activeChannel.confirmedSpend = lastConfirmedSpend + actualCost;
          activeChannel.accumulatedSpend = activeChannel.confirmedSpend;
          activeChannel.lastPrice = actualCost;
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
    return fetch(`${this.gatewayUrl}/agent/execute`, { method: 'POST', headers, body: JSON.stringify({ agentId, input }) });
  }
}
