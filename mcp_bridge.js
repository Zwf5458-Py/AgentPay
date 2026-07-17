#!/usr/bin/env node

import { stdin, stdout } from 'process';
import readline from 'readline';

const GATEWAY_MCP_URL = 'http://localhost:8080/v1/plugin/mcp';

const rl = readline.createInterface({
  input: stdin,
  output: stdout,
  terminal: false
});

rl.on('line', async (line) => {
  if (!line.trim()) return;
  try {
    const rpcRequest = JSON.parse(line);
    
    // 转发请求给 Gateway HTTP MCP Endpoint
    const res = await fetch(GATEWAY_MCP_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(rpcRequest)
    });
    
    if (res.ok) {
      const rpcResponse = await res.json();
      stdout.write(JSON.stringify(rpcResponse) + '\n');
    } else {
      const errText = await res.text();
      sendError(rpcRequest.id, -32603, `HTTP Error from Gateway: ${res.status} - ${errText}`);
    }
  } catch (err) {
    sendError(null, -32700, `Parse/Bridge error: ${err.message}`);
  }
});

function sendError(id, code, message) {
  const errResp = {
    jsonrpc: '2.0',
    error: { code, message },
    id: id || null
  };
  stdout.write(JSON.stringify(errResp) + '\n');
}
