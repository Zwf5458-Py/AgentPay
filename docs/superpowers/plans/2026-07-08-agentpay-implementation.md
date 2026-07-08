# AgentPay 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立一套标准化的“智能体+支付”闭环系统，利用 ERC-8004 与 X-402 协议及 ZeroDev Kernel v3 实现支付与执行的闭环。

**Architecture:** Go 支付网关拦截 HTTP 请求，处理 X-402 协商；TS 编写的 AA Bridge 与 ZeroDev SDK 交互进行智能账户管理与链上结算；Eliza Agent 提供推理服务并生成 ERC-8004 证明。

**Tech Stack:** Solidity (Foundry), Go (net/http, go-ethereum), TypeScript (Node.js, ZeroDev SDK v3, viem, Eliza)

## Global Constraints
- 链基础: Base Sepolia (Chain ID: 84532)
- AA 基础设施: ZeroDev Kernel v3 + EntryPoint v0.7
- 接口规范: ERC-8004 (Identity/Reputation/Validation), X-402 支付协议
- 目录结构: Monorepo (/contracts, /gateway, /aa-bridge, /agent, /sdk)
- 开发环境: Docker Compose + Anvil 本地分叉 (8545)

---

## 计划任务列表

### Task 1: 合约脚手架与 ERC-8004 身份/验证合约开发

**Files:**
- Create: `contracts/foundry.toml`
- Create: `contracts/src/identity/AgentIdentityRegistry.sol`
- Create: `contracts/src/identity/ValidationRegistry.sol`
- Create: `contracts/test/AgentIdentityTest.t.sol`

**Interfaces:**
- Consumes: None
- Produces: 
  - `AgentIdentityRegistry`: 注册与验证 Agent 身份 NFT
  - `ValidationRegistry`: 注册并路由校验推理证明

- [ ] **Step 1.1: 初始化 Foundry 项目**

在 `/contracts` 下生成基本框架。
运行:
```bash
cd /Users/oraclez/code/AgentPay/contracts
forge init --force --no-commit
```

- [ ] **Step 1.2: 编写 AgentIdentityRegistry 与 ValidationRegistry 合约**

创建 `contracts/src/identity/AgentIdentityRegistry.sol`:
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract AgentIdentityRegistry is ERC721, Ownable {
    struct AgentMetadata {
        string modelId;
        string serviceEndpoint;
        bytes32 teeAttestation;
        string capabilities;
        uint256 registeredAt;
    }

    uint256 private _nextTokenId;
    mapping(uint256 => AgentMetadata) private _agents;

    event AgentRegistered(uint256 indexed agentId, string modelId, string serviceEndpoint);
    event MetadataUpdated(uint256 indexed agentId, string modelId, string serviceEndpoint);

    constructor() ERC721("AgentPay Identity", "AGENT") Ownable(msg.sender) {}

    function registerAgent(AgentMetadata calldata metadata) external returns (uint256) {
        uint256 agentId = _nextTokenId++;
        _safeMint(msg.sender, agentId);
        _agents[agentId] = metadata;
        _agents[agentId].registeredAt = block.timestamp;
        emit AgentRegistered(agentId, metadata.modelId, metadata.serviceEndpoint);
        return agentId;
    }

    function updateMetadata(uint256 agentId, AgentMetadata calldata metadata) external {
        require(ownerOf(agentId) == msg.sender, "Not agent owner");
        _agents[agentId] = metadata;
        emit MetadataUpdated(agentId, metadata.modelId, metadata.serviceEndpoint);
    }

    function getAgent(uint256 agentId) external view returns (AgentMetadata memory) {
        require(_ownerOf(agentId) != address(0), "Agent does not exist");
        return _agents[agentId];
    }

    function verifyAgent(uint256 agentId) external view returns (bool) {
        return _ownerOf(agentId) != address(0);
    }

    // Soulbound implementation: prevent transfers
    function _update(address to, uint256 tokenId, address auth) internal override returns (address) {
        address from = _ownerOf(tokenId);
        if (from != address(0) && to != address(0)) {
            revert("Soulbound: Transfer not allowed");
        }
        return super._update(to, tokenId, auth);
    }
}
```

创建 `contracts/src/identity/ValidationRegistry.sol`:
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";

interface IValidator {
    function validate(uint256 agentId, bytes calldata proof) external view returns (bool);
}

contract ValidationRegistry is Ownable {
    mapping(string => address) private _validators;

    event ValidatorRegistered(string validationType, address validatorAddress);

    constructor() Ownable(msg.sender) {}

    function registerValidator(string calldata validationType, address validatorContract) external onlyOwner {
        require(validatorContract != address(0), "Invalid validator address");
        _validators[validationType] = validatorContract;
        emit ValidatorRegistered(validationType, validatorContract);
    }

    function validateProof(uint256 agentId, string calldata validationType, bytes calldata proof) external view returns (bool) {
        address validator = _validators[validationType];
        if (validator == address(0)) {
            // If no validator is registered for this type in development, default to mock (always true)
            return true;
        }
        return IValidator(validator).validate(agentId, proof);
    }
}
```

- [ ] **Step 1.3: 编写 Foundry 测试以验证身份注册与 Soulbound 限制**

创建 `contracts/test/AgentIdentityTest.t.sol`:
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../src/identity/AgentIdentityRegistry.sol";
import "../src/identity/ValidationRegistry.sol";

contract AgentIdentityTest is Test {
    AgentIdentityRegistry registry;
    ValidationRegistry validation;

    address owner = address(0x1);
    address agentOwner = address(0x2);
    address recipient = address(0x3);

    function setUp() public {
        vm.startPrank(owner);
        registry = new AgentIdentityRegistry();
        validation = new ValidationRegistry();
        vm.stopPrank();
    }

    function testRegisterAndGetAgent() public {
        vm.startPrank(agentOwner);
        AgentIdentityRegistry.AgentMetadata memory meta = AgentIdentityRegistry.AgentMetadata({
            modelId: "mock-model",
            serviceEndpoint: "http://localhost:3002",
            teeAttestation: bytes32(0),
            capabilities: "['chat']",
            registeredAt: 0
        });

        uint256 id = registry.registerAgent(meta);
        assertEq(id, 0);
        assertTrue(registry.verifyAgent(id));

        AgentIdentityRegistry.AgentMetadata memory fetched = registry.getAgent(id);
        assertEq(fetched.modelId, "mock-model");
        vm.stopPrank();
    }

    function testSoulboundTransferReverts() public {
        vm.startPrank(agentOwner);
        AgentIdentityRegistry.AgentMetadata memory meta = AgentIdentityRegistry.AgentMetadata({
            modelId: "mock-model",
            serviceEndpoint: "http://localhost:3002",
            teeAttestation: bytes32(0),
            capabilities: "['chat']",
            registeredAt: 0
        });

        uint256 id = registry.registerAgent(meta);
        vm.expectRevert("Soulbound: Transfer not allowed");
        registry.transferFrom(agentOwner, recipient, id);
        vm.stopPrank();
    }
}
```

- [ ] **Step 1.4: 运行 Foundry 测试，验证合约逻辑**

在 `/contracts` 下添加 OpenZeppelin 依赖并运行测试：
```bash
cd /Users/oraclez/code/AgentPay/contracts
forge install openzeppelin/openzeppelin-contracts --no-commit
forge test
```
Expected: Tests pass.

---

### Task 2: 信誉与支付托管合约开发 (PaymentEscrow & ReputationRegistry)

**Files:**
- Create: `contracts/src/identity/ReputationRegistry.sol`
- Create: `contracts/src/payment/PaymentEscrow.sol`
- Create: `contracts/test/PaymentEscrowTest.t.sol`

**Interfaces:**
- Consumes: `AgentIdentityRegistry`, `ValidationRegistry`
- Produces: 
  - `PaymentEscrow`: 微额支付锁定、释放与退款
  - `ReputationRegistry`: 支持支付验证的信誉评分

- [ ] **Step 2.1: 编写 ReputationRegistry 合约**

创建 `contracts/src/identity/ReputationRegistry.sol`:
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";

interface IPaymentEscrow {
    function hasPaid(address payer, uint256 agentId) external view returns (bool);
}

contract ReputationRegistry is Ownable {
    struct ReputationRecord {
        uint256 agentId;
        address caller;
        uint8 score;
        string taskHash;
        bool taskCompleted;
        uint256 timestamp;
    }

    IPaymentEscrow public paymentEscrow;
    mapping(uint256 => ReputationRecord[]) private _records;
    mapping(uint256 => uint256) private _totalScores;

    event FeedbackSubmitted(uint256 indexed agentId, address indexed caller, uint8 score);

    constructor() Ownable(msg.sender) {}

    function setPaymentEscrow(address escrowAddress) external onlyOwner {
        paymentEscrow = IPaymentEscrow(escrowAddress);
    }

    function submitFeedback(uint256 agentId, uint8 score, string calldata taskHash, bool completed) external {
        require(score >= 1 && score <= 5, "Score must be 1-5");
        if (address(paymentEscrow) != address(0)) {
            require(paymentEscrow.hasPaid(msg.sender, agentId), "Caller must have paid agent before");
        }

        _records[agentId].push(ReputationRecord({
            agentId: agentId,
            caller: msg.sender,
            score: score,
            taskHash: taskHash,
            taskCompleted: completed,
            timestamp: block.timestamp
        }));

        _totalScores[agentId] += score;
        emit FeedbackSubmitted(agentId, msg.sender, score);
    }

    function getReputation(uint256 agentId) external view returns (uint256 avgScore, uint256 totalReviews) {
        totalReviews = _records[agentId].length;
        if (totalReviews == 0) {
            return (0, 0);
        }
        avgScore = _totalScores[agentId] / totalReviews;
        return (avgScore, totalReviews);
    }
}
```

- [ ] **Step 2.2: 编写 PaymentEscrow 合约**

创建 `contracts/src/payment/PaymentEscrow.sol`:
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "../identity/ValidationRegistry.sol";

contract PaymentEscrow is Ownable {
    enum PaymentStatus { Locked, Released, Refunded }

    struct PaymentLock {
        address payer;
        uint256 agentId;
        uint256 amount;
        bytes32 requestHash;
        PaymentStatus status;
        uint256 lockedAt;
        uint256 expiresAt;
    }

    IERC20 public paymentToken; // USDC
    ValidationRegistry public validationRegistry;
    address public settler; // AA Bridge address allowed to settle

    mapping(bytes32 => PaymentLock) private _locks;
    mapping(address => mapping(uint256 => bool)) private _hasPaid;

    event PaymentLocked(bytes32 indexed lockId, address indexed payer, uint256 indexed agentId, uint256 amount);
    event PaymentReleased(bytes32 indexed lockId, uint256 agentId, address agentOwner);
    event PaymentRefunded(bytes32 indexed lockId, address indexed payer);

    modifier onlySettler() {
        require(msg.sender == settler, "Only settler can execute");
        _;
    }

    constructor(address tokenAddress, address validationAddress) Ownable(msg.sender) {
        paymentToken = IERC20(tokenAddress);
        validationRegistry = ValidationRegistry(validationAddress);
    }

    function setSettler(address settlerAddress) external onlyOwner {
        settler = settlerAddress;
    }

    function lockPayment(uint256 agentId, uint256 amount, bytes32 requestHash, uint256 duration) external returns (bytes32) {
        require(amount > 0, "Amount must be > 0");
        bytes32 lockId = keccak256(abi.encodePacked(msg.sender, agentId, requestHash, block.timestamp));
        require(_locks[lockId].amount == 0, "Lock already exists");

        // Lock funds
        require(paymentToken.transferFrom(msg.sender, address(this), amount), "Transfer failed");

        _locks[lockId] = PaymentLock({
            payer: msg.sender,
            agentId: agentId,
            amount: amount,
            requestHash: requestHash,
            status: PaymentStatus.Locked,
            lockedAt: block.timestamp,
            expiresAt: block.timestamp + duration
        });

        emit PaymentLocked(lockId, msg.sender, agentId, amount);
        return lockId;
    }

    function releasePayment(bytes32 lockId, bytes calldata proof, address agentOwner) external onlySettler {
        PaymentLock storage lock = _locks[lockId];
        require(lock.status == PaymentStatus.Locked, "Invalid payment status");
        require(block.timestamp <= lock.expiresAt, "Lock expired");

        // Validate proof
        require(validationRegistry.validateProof(lock.agentId, "TEE", proof), "Proof validation failed");

        lock.status = PaymentStatus.Released;
        _hasPaid[lock.payer][lock.agentId] = true;

        require(paymentToken.transfer(agentOwner, lock.amount), "Release transfer failed");
        emit PaymentReleased(lockId, lock.agentId, agentOwner);
    }

    function refund(bytes32 lockId) external {
        PaymentLock storage lock = _locks[lockId];
        require(lock.status == PaymentStatus.Locked, "Invalid payment status");
        require(block.timestamp > lock.expiresAt, "Lock not yet expired");

        lock.status = PaymentStatus.Refunded;
        require(paymentToken.transfer(lock.payer, lock.amount), "Refund transfer failed");
        emit PaymentRefunded(lockId, lock.payer);
    }

    function hasPaid(address payer, uint256 agentId) external view returns (bool) {
        return _hasPaid[payer][agentId];
    }

    function getLock(bytes32 lockId) external view returns (PaymentLock memory) {
        return _locks[lockId];
    }
}
```

- [ ] **Step 2.3: 编写 MockToken 并为托管合约编写测试**

创建 `contracts/test/MockERC20.sol`:
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract MockERC20 is ERC20 {
    constructor() ERC20("Mock USDC", "USDC") {
        _mint(msg.sender, 1000000 * 10**6);
    }
    
    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}
```

创建 `contracts/test/PaymentEscrowTest.t.sol`:
```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "./MockERC20.sol";
import "../src/identity/ValidationRegistry.sol";
import "../src/payment/PaymentEscrow.sol";

contract PaymentEscrowTest is Test {
    MockERC20 token;
    ValidationRegistry validation;
    PaymentEscrow escrow;

    address owner = address(0x1);
    address settler = address(0x2);
    address payer = address(0x3);
    address agentOwner = address(0x4);

    function setUp() public {
        vm.startPrank(owner);
        token = new MockERC20();
        validation = new ValidationRegistry();
        escrow = new PaymentEscrow(address(token), address(validation));
        escrow.setSettler(settler);
        vm.stopPrank();

        token.mint(payer, 1000 * 10**6);
    }

    function testLockAndRelease() public {
        vm.startPrank(payer);
        token.approve(address(escrow), 10 * 10**6);
        bytes32 lockId = escrow.lockPayment(0, 10 * 10**6, keccak256("req"), 300);
        vm.stopPrank();

        PaymentEscrow.PaymentLock memory lock = escrow.getLock(lockId);
        assertEq(lock.amount, 10 * 10**6);
        assertEq(uint(lock.status), uint(PaymentEscrow.PaymentStatus.Locked));

        // Release by settler
        vm.startPrank(settler);
        bytes memory proof = "mock-proof";
        escrow.releasePayment(lockId, proof, agentOwner);
        vm.stopPrank();

        lock = escrow.getLock(lockId);
        assertEq(uint(lock.status), uint(PaymentEscrow.PaymentStatus.Released));
        assertEq(token.balanceOf(agentOwner), 10 * 10**6);
        assertTrue(escrow.hasPaid(payer, 0));
    }
}
```

- [ ] **Step 2.4: 运行 Foundry 检查 Task 2 的合约测试**

```bash
cd /Users/oraclez/code/AgentPay/contracts
forge test
```
Expected: All tests pass.

---

### Task 3: AA Bridge 开发 (TypeScript 账户抽象胶水层)

**Files:**
- Create: `aa-bridge/package.json`
- Create: `aa-bridge/tsconfig.json`
- Create: `aa-bridge/src/index.ts`
- Create: `aa-bridge/src/kernel/account.ts`
- Create: `aa-bridge/src/kernel/permissions.ts`
- Create: `aa-bridge/src/paymaster/sponsor.ts`

**Interfaces:**
- Consumes: Node.js, ZeroDev SDK v3
- Produces: 
  - 智能账户的 Counterfactual 部署与 Session Key 模块的权限管理
  - `/aa/settle` 接口触发链上结算 releasePayment。

- [ ] **Step 3.1: 搭建 TypeScript 脚手架与依赖安装**

创建 `aa-bridge/package.json`:
```json
{
  "name": "aa-bridge",
  "version": "1.0.0",
  "main": "dist/index.js",
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js",
    "dev": "ts-node src/index.ts"
  },
  "dependencies": {
    "@zerodev/sdk": "^3.0.0",
    "@zerodev/ecdsa-validator": "^3.0.0",
    "fastify": "^4.20.0",
    "viem": "^2.15.0",
    "dotenv": "^16.4.5"
  },
  "devDependencies": {
    "ts-node": "^10.9.2",
    "typescript": "^5.4.5",
    "@types/node": "^20.12.12"
  }
}
```

安装并创建 tsconfig.json：
```bash
cd /Users/oraclez/code/AgentPay/aa-bridge
npm install
npx tsc --init
```

- [ ] **Step 3.2: 编写智能账户管理逻辑 (aa-bridge/src/kernel/account.ts)**

```typescript
import { Address } from "viem";

export interface AccountInfo {
  smartAccountAddress: Address;
  isDeployed: boolean;
}

// 模拟 ZeroDev v3 智能账户初始化，开发模式返回 Mock 地址以方便测试
export async function createAccount(ownerAddress: Address, agentId: bigint, salt: bigint): Promise<AccountInfo> {
  // 生产环境应使用 createKernelAccount() 
  // 此处模拟 counterfactual 地址
  return {
    smartAccountAddress: ownerAddress, // Mock 地址
    isDeployed: false
  };
}
```

- [ ] **Step 3.3: 编写 API 接口触发结算与权限管理 (aa-bridge/src/index.ts)**

```typescript
import Fastify from "fastify";
import { createAccount } from "./kernel/account";

const fastify = Fastify({ logger: true });

fastify.post("/aa/account/create", async (request, reply) => {
  const { ownerAddress, agentId, salt } = request.body as any;
  const info = await createAccount(ownerAddress, BigInt(agentId), BigInt(salt));
  return info;
});

fastify.post("/aa/settle", async (request, reply) => {
  const { lockId, proof, agentOwner } = request.body as any;
  // TODO: 调用 PaymentEscrow.releasePayment
  return { success: true, txHash: "0xMockTxHash" };
});

const start = async () => {
  try {
    await fastify.listen({ port: 3001, host: "127.0.0.1" });
  } catch (err) {
    fastify.log.error(err);
    process.exit(1);
  }
};
start();
```

---

### Task 4: Eliza Agent 的 ERC-8004 插件开发

**Files:**
- Create: `agent/package.json`
- Create: `agent/src/index.ts`
- Create: `agent/src/plugins/erc8004/proof.ts`
- Create: `agent/src/tee/mock.ts`

**Interfaces:**
- Consumes: Node.js, `viem`
- Produces: 
  - 生成带有 Agent 签名的 `InferenceProof` 推理证明。

- [ ] **Step 4.1: 创建 Agent 脚手架与依赖**

创建 `agent/package.json`:
```json
{
  "name": "agent",
  "version": "1.0.0",
  "scripts": {
    "start": "ts-node src/index.ts"
  },
  "dependencies": {
    "fastify": "^4.20.0",
    "viem": "^2.15.0",
    "ts-node": "^10.9.2",
    "typescript": "^5.4.5"
  }
}
```

安装依赖：
```bash
cd /Users/oraclez/code/AgentPay/agent
npm install
```

- [ ] **Step 4.2: 编写证明生成插件 (agent/src/plugins/erc8004/proof.ts)**

```typescript
import { keccak256, toBytes, Hex } from "viem";

export interface InferenceProof {
  agentId: bigint;
  inputHash: Hex;
  outputHash: Hex;
  modelId: string;
  timestamp: number;
  mockSignature: Hex;
}

export function generateProof(agentId: bigint, input: string, output: string, modelId: string): InferenceProof {
  const inputHash = keccak256(toBytes(input));
  const outputHash = keccak256(toBytes(output));
  return {
    agentId,
    inputHash,
    outputHash,
    modelId,
    timestamp: Math.floor(Date.now() / 1000),
    mockSignature: "0xMockAgentSignature"
  };
}
```

- [ ] **Step 4.3: 编写 Agent API 端点 (agent/src/index.ts)**

```typescript
import Fastify from "fastify";
import { generateProof } from "./plugins/erc8004/proof";

const fastify = Fastify({ logger: true });

fastify.post("/agent/execute", async (request, reply) => {
  const { input, agentId } = request.body as any;
  const output = `Processed: ${input}`;
  const proof = generateProof(BigInt(agentId), input, output, "mock-model");
  
  reply.header("X-Agent-Proof", Buffer.from(JSON.stringify(proof)).toString("base64"));
  return { output, proof };
});

fastify.listen({ port: 3002, host: "127.0.0.1" }, (err) => {
  if (err) {
    fastify.log.error(err);
    process.exit(1);
  }
});
```

---

### Task 5: Go 支付网关开发 (X-402 拦截逻辑与代理)

**Files:**
- Create: `gateway/go.mod`
- Create: `gateway/cmd/gateway/main.go`
- Create: `gateway/internal/middleware/x402.go`
- Create: `gateway/internal/proxy/reverse.go`
- Create: `gateway/internal/middleware/x402_test.go`

**Interfaces:**
- Consumes: Go 运行时，链上以太坊连接
- Produces: 
  - 过滤未经授权的请求，返回 `402 Payment Required`。
  - 带支付 Token 的请求，代理转发给 Eliza Agent 并异步结算。

- [ ] **Step 5.1: 初始化 Go Module**

```bash
cd /Users/oraclez/code/AgentPay/gateway
go mod init gateway
go get github.com/go-chi/chi/v5
```

- [ ] **Step 5.2: 编写 X-402 拦截中间件与反向代理**

创建 `gateway/internal/middleware/x402.go`:
```go
package middleware

import (
	"net/http"
)

func X402Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			w.Header().Set("X-402-Price", "1000")
			w.Header().Set("X-402-Currency", "USDC")
			w.Header().Set("X-402-Chain", "base-sepolia")
			w.Header().Set("X-402-Payment-Address", "0xEscrowContractAddress")
			w.WriteHeader(http.StatusPaymentRequired)
			w.Write([]byte(`{"error": "payment_required"}`))
			return
		}
		// 校验授权 Token ...
		next.ServeHTTP(w, r)
	})
}
```

创建 `gateway/cmd/gateway/main.go` 路由入口：
```go
package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"gateway/internal/middleware"
	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	
	targetURL, _ := url.Parse("http://127.0.0.1:3002")
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	r.Route("/agent", func(r chi.Router) {
		r.Use(middleware.X402Middleware)
		r.Post("/execute", func(w http.ResponseWriter, r *http.Request) {
			proxy.ServeHTTP(w, r)
		})
	})

	http.ListenAndServe(":8080", r)
}
```

- [ ] **Step 5.3: 编写 Go 单元测试以验证 402 拦截**

创建 `gateway/internal/middleware/x402_test.go`:
```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestX402Middleware(t *testing.T) {
	handler := X402Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: No authorization token
	req := httptest.NewRequest("POST", "/agent/execute", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("Expected 402, got %d", rr.Code)
	}

	// Case 2: Has authorization token
	req = httptest.NewRequest("POST", "/agent/execute", nil)
	req.Header.Set("Authorization", "Bearer mock-token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}
```

运行 Go 单测验证拦截正确性：
```bash
cd /Users/oraclez/code/AgentPay/gateway
go test ./...
```
Expected: Tests pass.

---

### Task 6: 客户端 SDK 与端到端集成测试 (E2E Test)

**Files:**
- Create: `sdk/package.json`
- Create: `sdk/src/client.ts`
- Create: `sdk/test/e2e.test.ts`
- Create: `docker-compose.yml`

**Interfaces:**
- Consumes: `gateway`, `agent`, `aa-bridge` 的联调交互
- Produces: 
  - 一键式自动支付与交互机制。

- [ ] **Step 6.1: 创建 SDK 脚手架**

创建 `sdk/package.json`:
```json
{
  "name": "agentpay-sdk",
  "version": "1.0.0",
  "main": "dist/client.js",
  "dependencies": {
    "viem": "^2.15.0"
  },
  "devDependencies": {
    "vitest": "^1.6.0",
    "ts-node": "^10.9.2"
  }
}
```

安装依赖：
```bash
cd /Users/oraclez/code/AgentPay/sdk
npm install
```

- [ ] **Step 6.2: 编写 SDK 客户端逻辑与自动重试逻辑 (sdk/src/client.ts)**

```typescript
export class AgentPayClient {
  private gatewayUrl: string;

  constructor(gatewayUrl: string) {
    this.gatewayUrl = gatewayUrl;
  }

  async execute(agentId: number, input: string): Promise<any> {
    // 第一次尝试请求 (未授权)
    let res = await fetch(`${this.gatewayUrl}/agent/execute`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ input, agentId })
    });

    if (res.status === 402) {
      const price = res.headers.get("X-402-Price");
      const paymentAddress = res.headers.get("X-402-Payment-Address");
      console.log(`[SDK] Got 402. Payment Required. Price: ${price}, Target: ${paymentAddress}`);

      // 模拟钱包支付并生成 EIP-3009 签名 Token
      const mockToken = "Bearer mock-token-signed";

      // 携带付款证明二次发起请求
      res = await fetch(`${this.gatewayUrl}/agent/execute`, {
        method: "POST",
        headers: { 
          "Content-Type": "application/json",
          "Authorization": mockToken
        },
        body: JSON.stringify({ input, agentId })
      });
    }

    return await res.json();
  }
}
```

- [ ] **Step 6.3: 创建 Docker Compose 用于多组件联调**

创建 `docker-compose.yml`:
```yaml
version: '3.8'

services:
  gateway:
    build: ./gateway
    ports:
      - "8080:8080"
  aa-bridge:
    build: ./aa-bridge
    ports:
      - "3001:3001"
  agent:
    build: ./agent
    ports:
      - "3002:3002"
```

- [ ] **Step 6.4: 编写一键联调验证脚本**

在 `sdk/test/e2e.test.ts` 中通过测试用例模拟端到端支付成功放行：
```typescript
import { describe, it, expect } from "vitest";
import { AgentPayClient } from "../src/client";

describe("AgentPay E2E flow", () => {
  it("should handle 402 error, generate payment token and resolve request", async () => {
    // 注意：需要确保本地 gateway 和 agent 服务处于运行状态
    const client = new AgentPayClient("http://127.0.0.1:8080");
    const result = await client.execute(0, "E2E Test Input");
    expect(result.output).toContain("Processed: E2E Test Input");
  });
});
```
