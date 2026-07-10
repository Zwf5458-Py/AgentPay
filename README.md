# AgentPay 智能体微支付与状态通道结算协议

AgentPay 是一个专为 AI 智能体（AI Agents）设计的高并发、低延迟微支付与结算协议系统。它有机地融合了 **X-402 支付协商标准**、**EIP-712 预授权信贷锁定**、**Stripe/Crypto 混合支付机制** 与以太坊 **ERC-6551 智能账户（Token Bound Account, TBA）**，并在此基础上扩展实现了支持链下累计签名、链上三方拆分清算的状态通道，以极大降低智能体之间微支付的 Layer-2 Gas 损耗与响应延迟。

---

## 1. 系统架构分层

整个项目采用了微服务分层设计，以保障高并发网关吞吐、混合支付路由与特权管理监控大屏的隔离：

```
┌────────────────────────────────────────────────────────┐
│            Client Portal & Admin Portal                │
│    - client.html (MetaMask 插件/私钥直签/Stripe 弹窗)    │
│    - admin.html (霓虹毛玻璃特权看板，数据实时监控)       │
└──────────────────────────┬─────────────────────────────┘
                           │ 1. API 访问 / X-402 交互自愈
                           ▼
┌────────────────────────────────────────────────────────┐
│             Go Gateway (微支付协商限流网关)              │
│    - Token Bucket Rate Limiter (IP 限流防御)           │
│    - X-402 Challenge intercept (捕获与法币/代币重定向)   │
│    - SQLite DB Queue (防双花防重放 stripe/crypto 锁)    │
│    - Admin API Security (生产环境空密钥安全熔断)         │
└──────────────────────────┬─────────────────────────────┘
                           │ 2. 异步投递结算任务 (CORS / Secret)
                           ▼
┌────────────────────────────────────────────────────────┐
│            Smart Account Bridge (AA 网桥)              │
│    - Auto-Deploy TBA (反查并自动部署未初始化 TBA)         │
│    - escrowConfigCache (RPC 多节点缓存与静态优化)        │
└──────────────────────────┬─────────────────────────────┘
                           │ 3. 链上批量拆分分成结算
                           ▼
┌────────────────────────────────────────────────────────┐
│             Solidity Contracts (智能合约)              │
│    - PaymentEscrow.sol (分流拆分 splitSettle 与抽佣)     │
│    - AgentTokenBoundAccount.sol (ERC-6551 收款账户)    │
└────────────────────────────────────────────────────────┘
```

---

## 2. 核心特性

- **X-402 协议自愈与双模混合支付**：当客户端发起没有授权的请求时，网关返回 HTTP 402 挑战。客户端 SDK 会根据用户的偏好选择 **Crypto 代币通道** 或 **Stripe 法币渠道**。
- **Stripe 双模法币 Popup 交互与防双花锁**：
  - 前端支持弹窗拉起 Stripe 会话，隔离 Mock（2s 自动关闭）与真实环境（轮询 `popup.closed`）的窗口关闭机制。
  - 网关设计了 SQLite 独占锁的防双花状态机表 `consumed_stripe_sessions`，支持 `TryLock` -> `Verify` -> `Commit` 的三段式状态校验锁，彻底杜绝重放攻击，并在冷启动时自愈清理 pending 死锁状态。
- **智能合约 splitSettle 三方拆分结算**：
  - 合约完美支持将实际消费额分成给：平台（限 10% Platform Bps 抽佣硬防线）、AI 服务商（服务费）和模型提供商（模型分成），从根本上规避重入风险。
  - 网关执行计费保底轧账公式，防止大额模型费下扣减抽佣溢出导致 revert：`actualCost = (modelCost + serviceFeeVal) * 10000 / (10000 - platformBps)`。
- **特权大屏看板与管理组加固**：
  - 拥有磨砂毛玻璃未来霓虹美学风格的 `admin.html`，展示累计结算金额、平台收入与 Stripe 统计。
  - 所有特权管理与调试接口均受 `AdminAuthMiddleware` 保护。生产环境下若未配置 `INTERNAL_SECRET` 则**直接强熔断（返回 401）**，开发环境下则单次警告放行，防护极其严密。
- **DevOps 一键自动化部署 (`deploy.sh`)**：
  - 根目录内置 `deploy.sh` 部署脚本，自检 Docker、Docker Compose、Foundry 和 Python3 依赖。
  - 一键编译并运行 Anvil 节点（带 RPC 连通和 1s 缓冲等待），动态运行智能合约编译发布，利用 Python 提取 `PaymentEscrow` 地址，并重新导出（re-export）当前 Shell 环境以规避 Docker Compose 环境变量覆盖优先级 Bug，最后拉起微服务进行 cURL 安全隔离和健康度轮询测试。

---

## 3. 技术栈与模块目录

- **`contracts/`**：以太坊智能合约部分，基于 Foundry 编译测试。
  - `PaymentEscrow.sol`：状态通道累计三方拆分清算与超时清退合约。
- **`gateway/`**：微支付协商网关，基于 Go 开发。
  - 核心功能：限流、X-402 挑战拦截、Stripe 客户端核销、SQLite 持久化防双花双模任务队列、Admin 加固。
- **`aa-bridge/`**：智能账户桥接器，基于 Fastify + Viem + TypeScript 开发。
- **`agent/`**：模拟智能体推理计算，基于 Fastify + TypeScript 开发，可执行结构化 Solidity 安全审计。
- **`sdk/`**：客户端 TS SDK，支持 Promise 排队锁销毁与 EIP-712 签名。

---

## 4. 快速开始

### 4.1 前置环境依赖
请确保本地已安装以下环境：
- Docker & Docker Compose
- Node.js (推荐 v20+)
- Go (推荐 v1.21+)
- Foundry (编译 Solidity 必备)
- Python3

### 4.2 一键部署运行
在项目根目录下执行以下脚本，即可全自动部署合约、写入环境变量、并拉起整个微服务容器组：
```bash
./deploy.sh
```
部署成功后，控制台会输出带有 ASCII 横幅的访问指南及随机生成的管理 Secret。

### 4.3 单元测试验证
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
  cd aa-bridge && npm run test
  ```
- **TS SDK 测试**：
  ```bash
  cd sdk && npm run test
  ```

---

## 5. 前端面板访问入口

- **智能体审计客户端**：[client.html](file:///Users/oraclez/code/AgentPay/client.html)
  - 可体验 Solidity 安全审计助手，使用 MetaMask 签名 / 本地私钥直签或 Stripe Popup 弹窗完成 X-402 链下自愈结算。
- **特权大屏仪表盘**：[admin.html](file:///Users/oraclez/code/AgentPay/admin.html)
  - 填入部署脚本生成的 Secret，可实时刷新结算金额、重试挂起/失败的任务，或清空 Stripe 缓存。
