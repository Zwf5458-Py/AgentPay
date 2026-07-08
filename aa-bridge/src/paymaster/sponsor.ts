import { http } from 'viem';
import { baseSepolia } from 'viem/chains';
import { createZeroDevPaymasterClient } from '@zerodev/sdk';
import { getEntryPoint } from '@zerodev/sdk/constants';

/**
 * Creates and returns a ZeroDev Paymaster Client for sponsoring smart account transactions.
 * Returns null if in Mock Mode or if ZERODEV_PROJECT_ID is not configured.
 */
export function getSponsorPaymasterClient() {
  const projectId = process.env.ZERODEV_PROJECT_ID;
  const devMode = process.env.DEV_MODE === 'true' || !projectId;

  if (devMode) {
    return null;
  }

  const entryPoint = getEntryPoint('0.7');
  const paymasterUrl = `https://paymaster.zerodev.app/api/v2/paymaster/${projectId}`;

  return createZeroDevPaymasterClient({
    chain: baseSepolia,
    transport: http(paymasterUrl),
    entryPoint,
  });
}
