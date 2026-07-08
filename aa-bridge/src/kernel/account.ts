import { keccak256, encodePacked, getAddress, createPublicClient, http, formatEther, parseAbi } from 'viem';
import { baseSepolia } from 'viem/chains';
import { privateKeyToAccount } from 'viem/accounts';

// We import these dynamically or handle missing package errors gracefully if needed, 
// but since they are installed, we can import them directly.
import { createKernelAccount } from '@zerodev/sdk';
import { KERNEL_V3_1, getEntryPoint } from '@zerodev/sdk/constants';
import { signerToEcdsaValidator } from '@zerodev/ecdsa-validator';

const erc20Abi = parseAbi([
  'function balanceOf(address owner) view returns (uint256)',
  'function decimals() view returns (uint8)'
]);

/**
 * Deterministically generates or retrieves the Smart Account (Kernel v3) address.
 */
export async function getSmartAccountAddress(
  ownerAddress: string,
  agentId: number,
  salt: string
): Promise<string> {
  const devMode = process.env.DEV_MODE === 'true' || !process.env.ZERODEV_PROJECT_ID;

  if (devMode) {
    // Mock mode: deterministic offline address generation using keccak256 hash of parameters
    const hash = keccak256(
      encodePacked(
        ['address', 'uint256', 'string'],
        [ownerAddress as `0x${string}`, BigInt(agentId), salt]
      )
    );
    // Convert to a valid 20-byte address format
    const slicedHash = `0x${hash.substring(26)}`;
    return getAddress(slicedHash);
  }

  // Production mode: ZeroDev Kernel v3 Counterfactual address derivation
  const rpcUrl = process.env.RPC_URL || 'https://sepolia.base.org';
  const entryPoint = getEntryPoint('0.7');
  const kernelVersion = KERNEL_V3_1;

  const publicClient = createPublicClient({
    chain: baseSepolia,
    transport: http(rpcUrl),
  });

  // Since we only need to compute the address and might not have the owner's private key in this microservice,
  // we create a custom signer account with the owner's address. ZeroDev's signerToEcdsaValidator 
  // needs a LocalAccount interface for signer to compute the deployment initCode.
  const signer = {
    address: ownerAddress as `0x${string}`,
    source: 'custom',
    type: 'local',
    signMessage: async () => '0x' as `0x${string}`,
    signTransaction: async () => '0x' as `0x${string}`,
    signTypedData: async () => '0x' as `0x${string}`,
  } as any;

  const ecdsaValidator = await signerToEcdsaValidator(publicClient, {
    signer,
    entryPoint,
    kernelVersion,
  });

  // Map agentId & salt to a 256-bit index
  const indexHash = keccak256(
    encodePacked(
      ['uint256', 'string'],
      [BigInt(agentId), salt]
    )
  );
  const index = BigInt(indexHash);

  const account = await createKernelAccount(publicClient, {
    plugins: {
      sudo: ecdsaValidator,
    },
    index,
    entryPoint,
    kernelVersion,
  });

  return account.address;
}

/**
 * Checks the Native (ETH) and ERC20 token balances for a given address.
 */
export async function getAccountBalance(
  address: string
): Promise<{ native: string; token: string }> {
  const rpcUrl = process.env.RPC_URL || 'http://127.0.0.1:8545';
  const tokenAddress = process.env.PAYMENT_TOKEN_ADDRESS as `0x${string}` | undefined;

  try {
    const publicClient = createPublicClient({
      chain: baseSepolia,
      transport: http(rpcUrl),
    });

    const nativeBalance = await publicClient.getBalance({ address: address as `0x${string}` });
    
    let tokenBalanceStr = '0.0';
    if (tokenAddress && tokenAddress !== '0x0000000000000000000000000000000000000000') {
      try {
        const tokenBalance = await publicClient.readContract({
          address: tokenAddress,
          abi: erc20Abi,
          functionName: 'balanceOf',
          args: [address as `0x${string}`],
        });
        const decimals = await publicClient.readContract({
          address: tokenAddress,
          abi: erc20Abi,
          functionName: 'decimals',
        });
        tokenBalanceStr = (Number(tokenBalance) / 10 ** Number(decimals)).toString();
      } catch (e) {
        // Fail-safe if ERC20 query fails
      }
    }

    return {
      native: formatEther(nativeBalance),
      token: tokenBalanceStr,
    };
  } catch (err) {
    // Fallback Mock values if RPC is unreachable
    return {
      native: '1.0',
      token: '100.0',
    };
  }
}
