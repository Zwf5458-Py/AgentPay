# AgentPay Client SDK and E2E Integration Test Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Node.js + TypeScript environment inside the `sdk/` directory, implement the `AgentPayClient` SDK supporting HTTP 402 auto-healing with mock EIP-3009 tokens, write Dockerfiles for `gateway`, `aa-bridge`, and `agent` alongside a root `docker-compose.yml`, and verify everything via `sdk/test/e2e.test.ts`.

**Architecture:** Use Vitest to run a lightweight, self-contained mock HTTP server inside E2E tests to simulate Gateway, AA Bridge, and Downstream Agent services. Build standard Dockerfiles for each service and orchestrate them with Anvil.

**Tech Stack:** Node.js, TypeScript, Vitest, Viem, Go (for Gateway Dockerfile)

## Global Constraints

- Runtime Environment: Node.js 18+, TypeScript, Vitest
- Service Bindings: Gateway on 8080, AA Bridge on 3001, Eliza Agent on 3002
- Max price limit default: 5000

---

### Task 1: Initialize SDK Environment and Scaffolding

**Files:**
- Create: `sdk/package.json`
- Create: `sdk/tsconfig.json`

**Interfaces:**
- Produces: Initial structure for Node.js/TypeScript and testing

- [ ] **Step 1: Create package.json**

Create `sdk/package.json` with dependency list (viem, dotenv, vitest, typescript).
```json
{
  "name": "agentpay-sdk",
  "version": "1.0.0",
  "description": "AgentPay Client SDK",
  "main": "dist/src/client.js",
  "types": "dist/src/client.d.ts",
  "type": "module",
  "scripts": {
    "build": "tsc",
    "test": "vitest run"
  },
  "dependencies": {
    "dotenv": "^16.4.5",
    "viem": "^2.21.0"
  },
  "devDependencies": {
    "typescript": "^5.2.2",
    "vitest": "^1.6.0"
  }
}
```

- [ ] **Step 2: Create tsconfig.json**

Create `sdk/tsconfig.json` to compile TypeScript to ES Modules.
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "lib": ["ES2022"],
    "declaration": true,
    "sourceMap": true,
    "outDir": "./dist",
    "rootDir": "./",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["src/**/*", "test/**/*"]
}
```

- [ ] **Step 3: Run npm install**

Run: `npm install` inside `sdk` directory.
Expected: Node modules are successfully installed.

- [ ] **Step 4: Commit**

Run:
```bash
git add sdk/package.json sdk/tsconfig.json sdk/package-lock.json
git commit -m "chore: initialize sdk package.json and tsconfig.json"
```

---

### Task 2: Implement Client SDK with Auto-Healing

**Files:**
- Create: `sdk/src/client.ts`

**Interfaces:**
- Produces: `AgentPayClient` class with `execute(agentId: number, input: string): Promise<any>`

- [ ] **Step 1: Write client implementation**

Create `sdk/src/client.ts`.
```typescript
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
    this.maxPriceLimit = config.maxPriceLimit || 5000n;
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

    // First attempt
    let response = await this.sendRequest(agentId, input);

    // Capture challenge (HTTP 402)
    if (response.status === 402) {
      const priceStr = response.headers.get('X-402-Price');
      const paymentAddress = response.headers.get('X-402-Payment-Address');

      if (!priceStr || !paymentAddress) {
        throw new Error('Invalid HTTP 402 response from Gateway: missing headers');
      }

      if (!isAddress(paymentAddress)) {
        throw new Error('Invalid payment address in HTTP 402 headers');
      }

      const price = BigInt(priceStr);
      if (price > this.maxPriceLimit) {
        throw new Error(`Price limit exceeded: Price is ${price}, max limit is ${this.maxPriceLimit}`);
      }

      // Generate signature token
      let authorizationHeader = '';
      if (this.env === 'development') {
        const lockId = 'lock-999';
        const token = 'mock-token-signed';
        authorizationHeader = `Bearer ${lockId}:${token}`;
      } else {
        // Production signature (EIP-3009 transfer authorization signature)
        // Here we just raise an error or placeholder for production signing as E2E test runs in development
        if (!this.privateKey) {
          throw new Error('Private key is required for production signing');
        }
        // Simplified EIP-3009 for demo production environment
        const mockLockId = `lock-${Date.now()}`;
        authorizationHeader = `Bearer ${mockLockId}:production-eip3009-signed-token`;
      }

      // Retry attempt with Authorization header
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
```

- [ ] **Step 2: Run build to verify TypeScript syntax**

Run: `npm run build` inside `sdk` directory.
Expected: Compilation passes with zero errors.

- [ ] **Step 3: Commit**

Run:
```bash
git add sdk/src/client.ts
git commit -m "feat: implement AgentPayClient with HTTP 402 challenge capture and retry"
```

---

### Task 3: Create Service Dockerfiles

**Files:**
- Create: `gateway/Dockerfile`
- Create: `aa-bridge/Dockerfile`
- Create: `agent/Dockerfile`

- [ ] **Step 1: Write gateway/Dockerfile**

Create `gateway/Dockerfile` with a multi-stage Go build.
```dockerfile
# Build Stage
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o gateway ./cmd/gateway/main.go

# Production Stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/gateway .
EXPOSE 8080
CMD ["./gateway"]
```

- [ ] **Step 2: Write aa-bridge/Dockerfile**

Create `aa-bridge/Dockerfile`.
```dockerfile
FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build
EXPOSE 3001
CMD ["npm", "start"]
```

- [ ] **Step 3: Write agent/Dockerfile**

Create `agent/Dockerfile`.
```dockerfile
FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN npm run build
EXPOSE 3002
CMD ["npm", "start"]
```

- [ ] **Step 4: Commit**

Run:
```bash
git add gateway/Dockerfile aa-bridge/Dockerfile agent/Dockerfile
git commit -m "feat: add Dockerfiles for gateway, aa-bridge, and agent"
```

---

### Task 4: Write Root docker-compose.yml

**Files:**
- Create: `docker-compose.yml`

- [ ] **Step 1: Create docker-compose.yml**

Create `docker-compose.yml` in the root directory.
```yaml
version: '3.8'

services:
  anvil:
    image: ghcr.io/foundry-rs/foundry:latest
    ports:
      - "8545:8545"
    entrypoint: ["anvil", "--host", "0.0.0.0"]

  aa-bridge:
    build:
      context: ./aa-bridge
      dockerfile: Dockerfile
    ports:
      - "3001:3001"
    environment:
      - PORT=3001
      - RPC_URL=http://anvil:8545
      - DEV_MODE=true
      - PRIVATE_KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
    depends_on:
      - anvil

  agent:
    build:
      context: ./agent
      dockerfile: Dockerfile
    ports:
      - "3002:3002"
    environment:
      - NODE_ENV=development
      - PORT=3002

  gateway:
    build:
      context: ./gateway
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    environment:
      - PORT=8080
      - ELIZA_AGENT_URL=http://agent:3002
      - AA_BRIDGE_URL=http://aa-bridge:3001/aa/settle
      - ESCROW_ADDRESS=0x5FbDB2315678afecb367f032d93F642f64180aa3
    depends_on:
      - agent
      - aa-bridge
```

- [ ] **Step 2: Commit**

Run:
```bash
git add docker-compose.yml
git commit -m "feat: add root docker-compose.yml for microservice orchestration"
```

---

### Task 5: Implement E2E Test via Mock Servers

**Files:**
- Create: `sdk/test/e2e.test.ts`

- [ ] **Step 1: Write E2E integration test**

Create `sdk/test/e2e.test.ts` using Node.js `http` module to start three mock servers representing Gateway, Agent, and AA Bridge.
```typescript
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import http from 'http';
import { AgentPayClient } from '../src/client.js';

let mockGateway: http.Server;
let mockAgent: http.Server;
let mockBridge: http.Server;

let gatewayRequestCount = 0;
let agentRequestCount = 0;
let bridgeRequestCount = 0;
let bridgeLastBody: any = null;

beforeAll(async () => {
  // 1. Mock AA Bridge (:3001)
  mockBridge = http.createServer((req, res) => {
    if (req.method === 'POST' && req.url === '/aa/settle') {
      let body = '';
      req.on('data', chunk => { body += chunk; });
      req.on('end', () => {
        bridgeRequestCount++;
        bridgeLastBody = JSON.parse(body);
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          success: true,
          txHash: '0x7777777777777777777777777777777777777777777777777777777777777777',
          mocked: true
        }));
      });
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  // 2. Mock Eliza Agent (:3002)
  mockAgent = http.createServer((req, res) => {
    if (req.method === 'POST' && req.url === '/agent/execute') {
      let body = '';
      req.on('data', chunk => { body += chunk; });
      req.on('end', () => {
        agentRequestCount++;
        const parsed = JSON.parse(body);
        res.writeHead(200, {
          'Content-Type': 'application/json',
          'X-Agent-Proof': 'mock-proof-base64'
        });
        res.end(JSON.stringify({
          output: `Processed by AgentPay AI: ${parsed.input}`,
          proof: { agentId: parsed.agentId, output: `Processed by AgentPay AI: ${parsed.input}` }
        }));
      });
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  // 3. Mock Gateway (:8080)
  mockGateway = http.createServer((req, res) => {
    if (req.method === 'POST' && req.url === '/agent/execute') {
      gatewayRequestCount++;
      const auth = req.headers['authorization'];

      if (!auth || auth !== 'Bearer lock-999:mock-token-signed') {
        // Return 402
        res.writeHead(402, {
          'Content-Type': 'application/json',
          'X-402-Price': '1000',
          'X-402-Currency': 'USDC',
          'X-402-Chain': 'base-sepolia',
          'X-402-Payment-Address': '0x5FbDB2315678afecb367f032d93F642f64180aa3',
          'X-402-Version': '1'
        });
        res.end(JSON.stringify({ error: 'payment_required' }));
      } else {
        // Forward to downstream Agent (Proxy simulator)
        const agentReq = http.request({
          host: '127.0.0.1',
          port: 3002,
          path: '/agent/execute',
          method: 'POST',
          headers: { 'Content-Type': 'application/json' }
        }, (agentRes) => {
          let agentBody = '';
          agentRes.on('data', chunk => { agentBody += chunk; });
          agentRes.on('end', () => {
            const proof = agentRes.headers['x-agent-proof'];
            if (proof) {
              // Asynchronous settle call to AA Bridge
              const bridgeReq = http.request({
                host: '127.0.0.1',
                port: 3001,
                path: '/aa/settle',
                method: 'POST',
                headers: { 'Content-Type': 'application/json' }
              });
              bridgeReq.write(JSON.stringify({
                lockId: 'lock-999',
                proof: proof,
                agentOwner: '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266',
                escrowAddress: '0x5FbDB2315678afecb367f032d93F642f64180aa3'
              }));
              bridgeReq.end();
            }

            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(agentBody);
          });
        });

        // Pipe request payload forward to Agent
        let reqBody = '';
        req.on('data', chunk => { reqBody += chunk; });
        req.on('end', () => {
          agentReq.write(reqBody);
          agentReq.end();
        });
      }
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  // Listen
  await Promise.all([
    new Promise<void>(resolve => mockBridge.listen(3001, '127.0.0.1', resolve)),
    new Promise<void>(resolve => mockAgent.listen(3002, '127.0.0.1', resolve)),
    new Promise<void>(resolve => mockGateway.listen(8080, '127.0.0.1', resolve)),
  ]);
});

afterAll(async () => {
  await Promise.all([
    new Promise<void>(resolve => mockBridge.close(() => resolve())),
    new Promise<void>(resolve => mockAgent.close(() => resolve())),
    new Promise<void>(resolve => mockGateway.close(() => resolve())),
  ]);
});

describe('AgentPay SDK E2E Integration Test', () => {
  it('should auto-heal from 402, retry with token and trigger bridge settle', async () => {
    const client = new AgentPayClient({
      gatewayUrl: 'http://127.0.0.1:8080',
      env: 'development'
    });

    const result = await client.execute(1, 'E2E Integration Test String');

    expect(result.output).toBe('Processed by AgentPay AI: E2E Integration Test String');

    // Wait for the async settlement post requests to fully finish processing
    await new Promise(resolve => setTimeout(resolve, 50));

    expect(gatewayRequestCount).toBe(2); // First failed, second success
    expect(agentRequestCount).toBe(1);   // Agent only got hit on second request
    expect(bridgeRequestCount).toBe(1);  // Bridge settle got triggered once
    expect(bridgeLastBody.lockId).toBe('lock-999');
    expect(bridgeLastBody.proof).toBe('mock-proof-base64');
  });

  it('should throw an error if the price exceeds max price limit', async () => {
    const client = new AgentPayClient({
      gatewayUrl: 'http://127.0.0.1:8080',
      maxPriceLimit: 500n, // Less than the mock 1000 price
      env: 'development'
    });

    await expect(client.execute(1, 'Exp')).rejects.toThrow('Price limit exceeded');
  });
});
```

- [ ] **Step 2: Run test using vitest**

Run: `npm run test` inside `sdk` directory.
Expected: Both test suites successfully PASS.

- [ ] **Step 3: Commit**

Run:
```bash
git add sdk/test/e2e.test.ts
git commit -m "test: add e2e integration test with self-contained mock services"
```
