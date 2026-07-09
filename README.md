# AgentPay 智能体微支付与状态通道结算协议

AgentPay 是一个专为 AI 智能体（AI Agents）设计的高并发、低延迟微支付与结算协议系统。它有机地融合了 **X-402 支付协商标准**、**ERC-8004 密码学推理证明** 与以太坊 **ERC-6551 智能账户（Token Bound Account, TBA）**，并在此基础上扩展实现了支持链下累计签名、链上批量清算的状态通道，以极大降低智能体之间微支付的 Layer-2 Gas 损耗与响应延迟。

---

## 1. 系统架构分层

整个项目采用了微服务分层设计，以保障高并发网关吞吐与链上安全清算防线的隔离：

```
┌────────────────────────────────────────────────────────┐
│                    Client TS SDK                       │
└──────────────────────────┬─────────────────────────────┘
                           │ 1. POST /agent/execute
                           ▼
┌────────────────────────────────────────────────────────┐
│             Go Gateway (微支付协商限流网关)              │
│    - Token Bucket Rate Limiter (IP 限流防御)           │
│    - X-402 Challenge intercept (捕获与重定向)           │
│    - SQLite Persistent Queue (防阻断任务持久化)          │
│    - EIP-712 ChannelHold Auth & Receipt Settle         │
└──────────────────────────┬─────────────────────────────┘
                           │ 2. 异步投递结算任务 (CORS / Secret)
                           ▼
┌────────────────────────────────────────────────────────┐
│            Smart Account Bridge (AA 网桥)              │
│    - Kernel Smart Account (零知识会话密钥分配)          │
│    - Auto-Deploy TBA (反查并自动部署未初始化 TBA)         │
└──────────────────────────┬─────────────────────────────┘
                           │ 3. 链上批量结算
                           ▼
┌────────────────────────────────────────────────────────┐
│             Solidity Contracts (智能合约)              │
│    - PaymentEscrow.sol (累计签名状态通道/USDC 划扣)      │
│    - AgentTokenBoundAccount.sol (ERC-6551 执行账户)    │
└────────────────────────────────────────────────────────┘
```

---

## 2. 核心特性

- **X-402 协议自愈与状态通道集成**：当客户端发起没有授权的请求时，网关返回 HTTP 402 挑战。客户端 SDK 自动在链上锁定资金，本地通过 Promise 排队锁累加消费额度并自签名，网关识别放行，实现“即付即用，单次扣费”。
- **EIP-712 链下信贷锁定与清算自愈 (Pre-auth Hold & Settle Receipt)**：
  - 针对高并发与高价值 Agent 任务，网关对未授权请求发起大额 Hold 挑战（如 0.05 USDC 的微额度）。
  - 智能体 SDK 本地生成 EIP-712 `ChannelHold` 签名。网关使用 `crypto.SigToPub` 进行密码学签名恢复和有效性验证（包括 Expiration 过期校验与格式审计）。
  - 任务完成后，网关用 ECDSA 密钥签署最终的 `Settle Receipt` 凭证回传，客户端核实后自动将确认额修正回落为 `lastConfirmedSpend + actualCost`，实现全链下零 Gas 费的信贷解冻。
- **高性能 Go 持久化队列**：Go Gateway 采用 SQLite 作为本地持久化任务队列（使用 CGO-free 纯 Go 驱动，WAL 模式以及连接数控制防死锁），网关拦截 proof 后只入库即返回，消除阻塞，并由后台协程 Worker 配合指数级退避算法执行异步链上结算。支持通过 `channelID:nonce` 复合主键防范并发 SQLite 写入的主键冲突。
- **动态 TBA 收款安全防线**：AA Bridge 在结算时通过合约动态派生计算专属 TBA 收款地址。若收款地址未部署则自动上链部署，且托管合约限制释放时接收方必须完全等于派生 TBA，锁死资金流向。
- **抗 DDOS 与 IP 欺骗限流中间件**：网关最外层配备令牌桶限流，超限返回 429。内置 5 分钟 TTL 定期垃圾回收，防范海量随机 IP 伪造攻击造成的内存泄漏；仅在 `TRUST_PROXY=true` 时才采信 `X-Forwarded-For`。
- **高质感调试沙盒 (Playground)**：提供开箱即用、拥有毛玻璃科技感美学的 `playground.html` 单页。内置了并发排队锁，展示信贷锁定 Hold Amount 和 Settle Receipt 自愈过程，支持纯前端 Mock 演示与本地后端直连联调。

---

## 3. 技术栈与模块目录

- **`contracts/`**：以太坊智能合约部分，基于 Foundry 编译测试。
  - `PaymentEscrow.sol`：状态通道累计清算与超时清退合约。
  - `AgentTokenBoundAccount.sol`：满足 6551 规范的智能体收款控制账户。
- **`gateway/`**：微支付协商网关，基于 Go 开发。
  - 核心功能：限流、X-402 预授权校验拦截、Settle Receipt 签名生成、SQLite 持久化任务队列。
- **`aa-bridge/`**：零知识账户桥接器，基于 Fastify + Viem + TypeScript 开发。
  - 核心功能：TBA 地址反查与自动部署，发起合约交互结算。
- **`agent/`**：模拟智能体推理计算，基于 Fastify + TypeScript 开发。
  - 核心功能：生成符合 ERC-8004 的推理证明 Header `X-Agent-Proof`。
- **`sdk/`**：客户端 TS SDK。
  - 核心功能：402 拦截重试、并发串行排队锁（具有 `.finally()` 销毁防 Promise 内存泄露设计）、EIP-712 预授权本地签名、清算凭证解密更新。

---

## 4. 快速开始

### 4.1 前置环境依赖
请确保本地已安装以下环境：
- Docker & Docker Compose
- Node.js (推荐 v20+)
- Go (推荐 v1.21+)
- Foundry (编译 Solidity 必备)

### 4.2 单元测试验证
您可以分别进入对应目录下执行测试：

- **智能合约测试**：
  ```bash
  cd contracts && forge test -v
  ```
- **Go 网关测试**：
  ```bash
  cd gateway && go test -v ./...
  ```
- **AA 网桥测试**：
  ```bash
  cd aa-bridge && npm install && npm run test
  ```
- **TS SDK 测试**：
  ```bash
  cd sdk && npm install && npm run test
  ```

### 4.3 启动一键联调环境 (Docker Compose)
在项目根目录下执行以下命令，将一键拉起 Anvil 私链以及所有微服务：
```bash
docker compose up --build
```
启动后访问接口：
- 网关唯一公开入口：`http://127.0.0.1:8080/agent/execute`

---

## 5. Web 端沙盒调试工具 (Playground)

我们为开发者预置了开箱即用的可视化沙盒，用于体验 Mock 演示或直连本地后端调试。

### 5.1 使用方法
1. **启动本地静态服务器**：
   在项目根目录下，使用 Python 启动本地 Web 服务：
   ```bash
   python3 -m http.server 8000
   ```
2. **访问面板**：
   在浏览器中打开：`http://localhost:8000/playground.html`。
3. **双轨测试模式**：
   - **Mock 演示模式**：开启时无需启动任何后端进程，双击 `Execute` 或 `Concurrent Execute` 即可在控制台和 Timeline 时序图中动态查看 402 自愈与 Promise 并发排队锁、信贷预授权锁定、清算凭证解冻自愈的运作过程。
   - **本地直连调试**：开启时，面板红绿灯会自动检测本地 8080 和 3001 的健康状况。点击 Execute 会发送真实请求，中间面板的 SQLite 后台队列监视表每隔 2 秒自动刷新。

### 5.2 本地直连环境启动说明
为了跑通本地直连（direct）模式下的完整微支付与清算自愈流程，您需要在不同终端窗口中启动以下 3 个微服务：

1. **AA Bridge 网桥微服务**（使用 `tsx` 强兼容引擎启动，默认绑定 3001 端口）：
   ```bash
   cd aa-bridge
   # 在 .env 中填入拥有 Base Sepolia 余额的 PRIVATE_KEY 与 INTERNAL_SECRET=testsecret
   npm run dev
   ```
2. **Agent 模拟推理智能体**（默认绑定 3002 端口）：
   ```bash
   cd agent
   npm run dev
   ```
3. **Go Gateway 代理网关**（默认绑定 8080 端口）：
   ```bash
   cd gateway
   export ELIZA_AGENT_URL=http://127.0.0.1:3002
   export AA_BRIDGE_URL=http://127.0.0.1:3001/aa/settle
   export INTERNAL_SECRET=testsecret
   export CHAIN_ID=84532
   export GATEWAY_PRIVATE_KEY=您的以太坊私钥
   ./bin/gateway
   ```

### 5.3 调试技巧
- **私钥可见性明暗切换**：在高级连接设置中，点击“客户端私钥”输入框右侧的眼睛👀图标，可一键切换可见性以核查私钥准确度。
- **清空 SQLite 调试队列**：如果多次测试导致 SQLite 任务流水线被大量 `Failed` 任务塞满，可以点击流水线标题右侧的 **`清空队列 (Clear)`** 按钮一键擦除，重新发起全新的 402 预授权以查看最新任务如何成功扭转为 `Success` 状态。

---

## 6. 协议清算逻辑细节

### 6.1 预授权信贷挑战 (Channel Hold)
状态通道预授权采用 EIP-712 规范，类型散列配置如下：
$$\text{computedHash} = \text{keccak256}(\text{abi.encode}(\text{ChannelHold(bytes32 channelId,uint256 holdAmount,uint256 nonce,uint256 expiration)}, \text{channelId}, \text{holdAmount}, \text{nonce}, \text{expiration}))$$

### 6.2 链上乐观结算 (Batch Settle)
在结算阶段，网关作为 `settler` 可以单方面在链上提交结算。合约的 `batchSettle` 会验证客户端（Payer）签署的原始 `ChannelHold` 签名（包含锁定上限额度 `holdAmount`），只要网关提交的实际扣款金额 `accumulatedAmount` 不超过 `holdAmount`，合约即通过验证，无需客户端再次签署 final 签名。这彻底杜绝了客户端离线导致网关资金被卡死的风险：
$$\text{hashStruct} = \text{keccak256}(\text{abi.encode}(\text{CHANNEL\_HOLD\_TYPEHASH}, \text{channelId}, \text{holdAmount}, \text{nonce}, \text{expiration}))$$
$$\text{digest} = \text{keccak256}(\text{abi.encodePacked}(\text{"\textbackslash x19\textbackslash x01"}, \text{DOMAIN\_SEPARATOR}, \text{hashStruct}))$$
$$\text{signer} = \text{ECDSA.recover}(\text{digest}, \text{signature})$$
*(验证还原出的 `signer` 必须等于通道的 `payer`)*

