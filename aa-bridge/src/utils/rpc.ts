import { http, fallback } from 'viem';

/**
 * Parses a comma-separated RPC URL string and returns a fallback-enabled transport 
 * if multiple URLs are present, otherwise a standard http transport.
 */
export function getRpcTransport(rpcUrl: string) {
  if (rpcUrl.includes(',')) {
    const urls = rpcUrl.split(',').map(url => url.trim()).filter(Boolean);
    return fallback(urls.map(url => http(url)));
  }
  return http(rpcUrl);
}
