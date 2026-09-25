#!/usr/bin/env node

const readline = require('readline');
const http = require('http');

const GATEWAY_MCP_URL = 'http://localhost:8080/v1/plugin/mcp';

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: false
});

rl.on('line', (line) => {
  if (!line.trim()) return;
  try {
    const rpcRequest = JSON.parse(line);
    
    // 转发请求给 Gateway HTTP MCP Endpoint
    const url = new URL(GATEWAY_MCP_URL);
    const options = {
      hostname: url.hostname,
      port: url.port,
      path: url.pathname,
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      }
    };

    const req = http.request(options, (res) => {
      let data = '';
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => {
        if (res.statusCode === 200) {
          try {
            const rpcResponse = JSON.parse(data);
            process.stdout.write(JSON.stringify(rpcResponse) + '\n');
          } catch (e) {
            sendError(rpcRequest.id, -32603, `Invalid JSON response: ${data}`);
          }
        } else {
          sendError(rpcRequest.id, -32603, `HTTP Error from Gateway: ${res.statusCode} - ${data}`);
        }
      });
    });

    req.on('error', (err) => {
      sendError(rpcRequest.id, -32603, `Connection error to Gateway: ${err.message}`);
    });

    req.write(JSON.stringify(rpcRequest));
    req.end();
  } catch (err) {
    sendError(null, -32700, `Parse error: ${err.message}`);
  }
});

function sendError(id, code, message) {
  const errResp = {
    jsonrpc: '2.0',
    error: { code, message },
    id: id || null
  };
  process.stdout.write(JSON.stringify(errResp) + '\n');
}
