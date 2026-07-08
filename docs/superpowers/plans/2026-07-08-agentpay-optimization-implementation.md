# AgentPay 架构优化与安全加固 (Phase 2) 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SQLite 异步任务队列保障结算可靠性，引入累积签名状态通道，整合 ERC-6551 TBA 收款结构，并引入网关令牌桶限流。

**Architecture:** Go 网关内置轻量级 SQLite 重试 Worker；合约层通过 EIP-712 单边签名验证清算；AA Bridge 动态校验并预部署 TBA，网关通过中间件执行四七层硬隔离拦截。

**Tech Stack:** Go 1.20+ (`golang.org/x/time/rate`, `modernc.org/sqlite`), Node.js (TypeScript, viem, Fastify), Solidity 0.8.20 (Foundry).

## Global Constraints
- Solidity 版本统一为 `0.8.20`。
- Go 依赖不能引入外部 C 库，SQLite 驱动必须使用 CGO-free 的纯 Go 实现 (`modernc.org/sqlite`)。
- 全局密钥变量为 `INTERNAL_SECRET`。

---

### Task 1: 智能合约层状态通道扩展与 EIP-712 批量结算

**Files:**
- Create: `contracts/src/interfaces/IERC6551Registry.sol`
- Modify: `contracts/src/payment/PaymentEscrow.sol`
- Modify: `contracts/test/PaymentEscrowTest.t.sol`

**Interfaces:**
- Produces: `lockChannel`, `batchSettle`, `refundChannel` 合约方法。

- [ ] **Step 1: 写入 ERC-6551 注册表接口**
  在 `contracts/src/interfaces/IERC6551Registry.sol` 写入：
  ```solidity
  // SPDX-License-Identifier: MIT
  pragma solidity ^0.8.20;

  interface IERC6551Registry {
      event ERC6551AccountCreated(
          address account,
          address indexed implementation,
          bytes32 salt,
          uint256 chainId,
          address indexed tokenContract,
          uint256 indexed tokenId
      );

      function createAccount(
          address implementation,
          bytes32 salt,
          uint256 chainId,
          address tokenContract,
          uint256 tokenId
      ) external returns (address account);

      function account(
          address implementation,
          bytes32 salt,
          uint256 chainId,
          address tokenContract,
          uint256 tokenId
      ) external view returns (address account);
  }
  ```

- [ ] **Step 2: 重构 `PaymentEscrow.sol` 以适配状态通道**
  在 `contracts/src/payment/PaymentEscrow.sol` 中添加 EIP-712 结构体、ChannelLock 与校验方法。
  添加结构体与事件：
  ```solidity
  struct ChannelLock {
      address payer;
      uint256 agentId;
      uint256 maxAmount;
      uint256 settledAmount;
      uint256 expiresAt;
      PaymentStatus status;
  }

  mapping(bytes32 => ChannelLock) public channels;

  bytes32 public constant CHANNEL_SETTLE_TYPEHASH = keccak256(
      "ChannelSettle(bytes32 channelId,uint256 accumulatedAmount)"
  );
  ```
  实现 `lockChannel` 方法：
  ```solidity
  function lockChannel(uint256 agentId, uint256 amount, uint256 duration) external returns (bytes32 channelId) {
      require(amount > 0, "Amount must be greater than zero");
      require(duration > 0, "Duration must be greater than zero");
      channelId = keccak256(abi.encodePacked(msg.sender, agentId, block.timestamp));
      
      SafeERC20.safeTransferFrom(IERC20(usdcToken), msg.sender, address(this), amount);
      
      channels[channelId] = ChannelLock({
          payer: msg.sender,
          agentId: agentId,
          maxAmount: amount,
          settledAmount: 0,
          expiresAt: block.timestamp + duration,
          status: PaymentStatus.Locked
      });
      emit PaymentLocked(channelId, msg.sender, agentId, amount, block.timestamp + duration);
  }
  ```
  实现 `batchSettle` 方法：
  ```solidity
  function batchSettle(
      bytes32 channelId, 
      uint256 accumulatedAmount, 
      bytes calldata signature, 
      address agentOwner
  ) external onlySettler {
      ChannelLock storage lock = channels[channelId];
      require(lock.status == PaymentStatus.Locked, "Channel not in Locked status");
      require(block.timestamp <= lock.expiresAt, "Channel expired");
      require(accumulatedAmount <= lock.maxAmount, "Amount exceeds limit");

      // Verify signature matches payer
      bytes32 digest = _hashTypedDataV4(keccak256(abi.encode(
          CHANNEL_SETTLE_TYPEHASH,
          channelId,
          accumulatedAmount
      )));
      address signer = ECDSA.recover(digest, signature);
      require(signer == lock.payer, "Invalid cumulative signature");

      lock.status = PaymentStatus.Settled;
      lock.settledAmount = accumulatedAmount;

      // Transfer funds
      SafeERC20.safeTransfer(IERC20(usdcToken), agentOwner, accumulatedAmount);
      if (lock.maxAmount > accumulatedAmount) {
          SafeERC20.safeTransfer(IERC20(usdcToken), lock.payer, lock.maxAmount - accumulatedAmount);
      }
  }
  ```

- [ ] **Step 3: 运行 `forge test` 并断言失败**
  预期：因为尚未引入 ECDSA 相关引入，编译报错。

- [ ] **Step 4: 合约编译与单元测试适配**
  在 `PaymentEscrow.sol` 中添加：
  ```solidity
  import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
  import "@openzeppelin/contracts/utils/cryptography/EIP712.sol";
  ```
  继承 `EIP712("AgentPay", "1")`，在 `PaymentEscrowTest.t.sol` 中编写 `test_ChannelBatchSettle()` 测试通道锁仓与累积签名批量清结算。
  
- [ ] **Step 5: 运行并测试通过，执行 Commit**
  Run: `forge test --match-test test_ChannelBatchSettle -v`
  Expected: PASS
  ```bash
  git add .
  git commit -m "feat: implement cumulative batchSettle and EIP-712 validation in Escrow contract"
  ```

---

### Task 2: ERC-6551 TBA 智能体收款执行账户部署与防御

**Files:**
- Create: `contracts/src/interfaces/IERC6551Account.sol`
- Create: `contracts/src/payment/AgentTokenBoundAccount.sol`
- Modify: `contracts/src/payment/PaymentEscrow.sol`
- Modify: `contracts/test/PaymentEscrowTest.t.sol`

**Interfaces:**
- Produces: `AgentTokenBoundAccount` 默认实现账户，`PaymentEscrow._verifyAgentTBA` 校验防御。

- [ ] **Step 1: 编写 `IERC6551Account.sol`**
  ```solidity
  // SPDX-License-Identifier: MIT
  pragma solidity ^0.8.20;

  interface IERC6551Account {
      receive() external payable;
      function token() external view returns (uint256 chainId, address tokenContract, uint256 tokenId);
      function state() external view returns (uint256);
      function isValidSigner(address signer, bytes calldata context) external view returns (bytes4 magicValue);
  }
  ```

- [ ] **Step 2: 编写 `AgentTokenBoundAccount.sol`**
  编写支持划出资金与持有资产的 TBA 合约：
  ```solidity
  // SPDX-License-Identifier: MIT
  pragma solidity ^0.8.20;

  import "../interfaces/IERC6551Account.sol";
  import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

  contract AgentTokenBoundAccount is IERC6551Account {
      address public erc6551Registry;

      constructor(address _registry) {
          erc6551Registry = _registry;
      }

      receive() external payable {}

      function token() external view returns (uint256 chainId, address tokenContract, uint256 tokenId) {
          // Mock or simple static mapping for demonstration
          return (block.chainid, address(0), 0);
      }

      function state() external view returns (uint256) {
          return 0;
      }

      function isValidSigner(address signer, bytes calldata) external view returns (bytes4) {
          return 0x523efd54; // magic value
      }

      function execute(address to, uint256 value, bytes calldata data) external returns (bytes memory) {
          (bool success, bytes memory result) = to.call{value: value}(data);
          require(success, "Execution failed");
          return result;
      }
  }
  ```

- [ ] **Step 3: 托管合约添加 TBA 收款防御校验**
  修改 `PaymentEscrow.sol`，添加 `erc6551Registry`，`tbaImplementation`，`agentIdentityRegistry` 地址变量。
  在 `batchSettle` 内转账给 `agentOwner` 之前添加强制拦截：
  ```solidity
  address computedTBA = IERC6551Registry(erc6551Registry).account(
      tbaImplementation,
      bytes32(0),
      block.chainid,
      agentIdentityRegistry,
      lock.agentId
  );
  require(agentOwner == computedTBA, "Recipient must be Agent TBA address");
  ```

- [ ] **Step 4: 测试并提交**
  在测试用例中注册 Mock 注册表，验证非 TBA 账户被拒绝结算。
  Run: `forge test -v`
  Expected: PASS
  ```bash
  git add .
  git commit -m "feat: integrate ERC-6551 TBA constraint and verification on Escrow settlements"
  ```

---

### Task 3: Go Gateway 本地 SQLite 任务持久化队列实现

**Files:**
- Create: `gateway/internal/queue/sqlite_queue.go`
- Modify: `gateway/internal/proxy/reverse.go`
- Modify: `gateway/cmd/gateway/main.go`

**Interfaces:**
- Consumes: `X-Internal-Secret` 环境变量，SQLite 数据持久化。
- Produces: `QueueManager` 后台 Worker。

- [ ] **Step 1: 编写 `sqlite_queue.go`**
  引入 CGO-free 的 SQLite 纯 Go 驱动，并在网关启动时自动初始化表 `settle_tasks`：
  ```go
  package queue

  import (
      "database/sql"
      "time"
      _ "modernc.org/sqlite"
  )

  type SettleTask struct {
      LockID        string
      Proof         string
      AgentOwner    string
      EscrowAddress string
  }

  type QueueManager struct {
      db             *sql.DB
      bridgeURL      string
      internalSecret string
  }

  func NewQueueManager(dbPath, bridgeURL, secret string) (*QueueManager, error) {
      db, err := sql.Open("sqlite", dbPath)
      if err != nil {
          return nil, err
      }
      _, err = db.Exec(`CREATE TABLE IF NOT EXISTS settle_tasks (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          lock_id TEXT UNIQUE NOT NULL,
          proof TEXT NOT NULL,
          agent_owner TEXT NOT NULL,
          escrow_address TEXT NOT NULL,
          status TEXT NOT NULL,
          retry_count INTEGER DEFAULT 0,
          next_retry_at DATETIME NOT NULL,
          created_at DATETIME DEFAULT CURRENT_TIMESTAMP
      )`)
      return &QueueManager{db: db, bridgeURL: bridgeURL, internalSecret: secret}, err
  }

  func (q *QueueManager) Enqueue(task SettleTask) error {
      _, err := q.db.Exec(`INSERT OR IGNORE INTO settle_tasks 
          (lock_id, proof, agent_owner, escrow_address, status, next_retry_at) 
          VALUES (?, ?, ?, ?, 'pending', ?)`,
          task.LockID, task.Proof, task.AgentOwner, task.EscrowAddress, time.Now())
      return err
  }
  ```

- [ ] **Step 2: 编写 SQLite 消费 Worker 重试循环**
  在 `sqlite_queue.go` 内实现 LoopWorker，捕获 API 结算响应，支持指数级延迟规避：
  ```go
  func (q *QueueManager) StartWorker() {
      go func() {
          for {
              time.Sleep(2 * time.Second)
              rows, err := q.db.Query(`SELECT lock_id, proof, agent_owner, escrow_address, retry_count 
                  FROM settle_tasks WHERE status='pending' AND next_retry_at <= ?`, time.Now())
              if err != nil {
                  continue
              }
              for rows.Next() {
                  var lockID, proof, owner, escrow string
                  var retryCount int
                  rows.Scan(&lockID, &proof, &owner, &escrow, &retryCount)
                  
                  // Trigger post settle to bridge
                  success := q.postToBridge(lockID, proof, owner, escrow)
                  if success {
                      q.db.Exec("UPDATE settle_tasks SET status='success' WHERE lock_id=?", lockID)
                  } else {
                      newRetry := retryCount + 1
                      if newRetry >= 5 {
                          q.db.Exec("UPDATE settle_tasks SET status='failed' WHERE lock_id=?", lockID)
                          // Trigger critical log alert
                      } else {
                          nextRetry := time.Now().Add(time.Duration(1<<newRetry) * time.Second)
                          q.db.Exec("UPDATE settle_tasks SET retry_count=?, next_retry_at=? WHERE lock_id=?", newRetry, nextRetry, lockID)
                      }
                  }
              }
              rows.Close()
          }
      }()
  }
  ```

- [ ] **Step 3: 适配网关反向代理逻辑**
  修改 `gateway/internal/proxy/reverse.go`，捕获 `X-Agent-Proof` 响应后，不再直接发起同步/协程结算，而是调用 `QueueManager.Enqueue` 压入 SQLite 本地队列，随后直接给客户端返回结果。

- [ ] **Step 4: 测试并提交**
  适配 `x402_test.go`。
  Run: `go test -v ./...`
  Expected: PASS
  ```bash
  git add .
  git commit -m "feat: implement sqlite task queue for gateway failover and retries"
  ```

---

### Task 4: Go Gateway 令牌桶限流 rate-limiting 实现

**Files:**
- Create: `gateway/internal/middleware/rate_limit.go`
- Modify: `gateway/cmd/gateway/main.go`

**Interfaces:**
- Produces: `RateLimitMiddleware` 中间件。

- [ ] **Step 1: 编写限流中间件 `rate_limit.go`**
  ```go
  package middleware

  import (
      "net/http"
      "sync"
      "golang.org/x/time/rate"
  )

  type IPRateLimiter struct {
      ips map[string]*rate.Limiter
      mu  sync.RWMutex
      r   rate.Limit
      b   int
  }

  func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
      return &IPRateLimiter{
          ips: make(map[string]*rate.Limiter),
          r:   r,
          b:   b,
      }
  }

  func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
      i.mu.Lock()
      defer i.mu.Unlock()

      limiter, exists := i.ips[ip]
      if !exists {
          limiter = rate.NewLimiter(i.r, i.b)
          i.ips[ip] = limiter
      }
      return limiter
  }

  func RateLimitMiddleware(limiter *IPRateLimiter) func(http.Handler) http.Handler {
      return func(next http.Handler) http.Handler {
          return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
              ip := r.RemoteAddr
              lim := limiter.GetLimiter(ip)
              if !lim.Allow() {
                  w.Header().Set("Content-Type", "application/json")
                  w.WriteHeader(http.StatusTooManyRequests)
                  w.Write([]byte(`{"error":"rate_limit_exceeded","message":"too many requests"}`))
                  return
              }
              next.ServeHTTP(w, r)
          })
      }
  }
  ```

- [ ] **Step 2: 挂载中间件并测试提交**
  在 `gateway/cmd/gateway/main.go` 中挂载该限制，并在测试文件中对 429 频率拦截增加断言。
  Run: `go test -v ./...`
  Expected: PASS
  ```bash
  git add .
  git commit -m "feat: implement gateway-level ip rate limiting returning 429"
  ```

---

### Task 5: 客户端 SDK 与 AA Bridge 批量结算适配

**Files:**
- Modify: `sdk/src/client.ts`
- Modify: `aa-bridge/src/index.ts`
- Modify: `sdk/test/e2e.test.ts`

**Interfaces:**
- Consumes: Channel lockId, accumulatedSpend signatures.

- [ ] **Step 1: 修改 TS SDK 累积签名机制**
  在 `sdk/src/client.ts` 内部增加 `accumulatedSpend` 属性。
  当捕获 402 后，如果本地无通道信息，计算或记录 `channelId`。
  调用 viem 以 EIP-712 对 `[channelId, accumulatedSpend]` 执行自签名（Mock模式下，返回 `channelId:accumulatedSpend:signature`）。
  在重试请求中传入：`Authorization: Bearer <channelId>:<accumulatedSpend>:<signature>`。

- [ ] **Step 2: 修改 AA Bridge 批量上链与 TBA 部署**
  在 `aa-bridge/src/index.ts` 中拦截 `/aa/settle`：
  - 解析出 `channelId`, `accumulatedAmount`, `signature`。
  - 获取链上 TBA 地址的部署状态（code.length），若为 0 且在非 Mock 模式，调用 Registry.createAccount 预先部署该 TBA 账户。
  - 调用 `PaymentEscrow.batchSettle(channelId, accumulatedAmount, signature, agentTBA)` 触发最终链上批量清算分配。

- [ ] **Step 3: 测试验证与提交**
  更新 `sdk/test/e2e.test.ts` 全链路 Mock 集成测试。
  Run: `npm run test` (在 sdk 目录下)
  Expected: PASS
  ```bash
  git add .
  git commit -m "feat: support state-channel cumulative signing in SDK and dynamic TBA deployment in bridge"
  ```
