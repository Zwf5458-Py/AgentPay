# AgentPay 智能体支付协议 — 详细功能介绍

本文件对 AgentPay 协议套件的八大核心技术特性进行深度原理解析，帮助开发者理解系统底层的密码学设计、并发安全机制、金融账本设计以及计费系统边界。

---

## 一、 X-402 协议协商与自愈流 (X-402 Protocol Self-Healing Flow)

X-402 协议基于 HTTP 状态码扩展，为微支付（Micropayments）场景提供自动化的即时协商与支付自愈。

### 1.1 协议数据流
1. **首发请求 (Unauthenticated Request)**：客户端向网关发送请求，未携带授权标头。
2. **拦截并下发挑战 (402 Challenge)**：网关拦截并识别到无 Token，向客户端写回 `HTTP 402 Payment Required`，同时在 Header 中挂载协商数据：
   - `X-402-Payment-Address`：网关托管的智能合约地址（USDC 划拨中心）。
   - `X-402-Price`：当前推理服务单次调用的价格（例如 1000）。
   - `X-402-Payment-Type`：支付结算类型（如状态通道 `channel` 或直接单笔锁定 `lock`）。
3. **客户端自愈 (Client-Side Healing)**：
   - TS SDK 或沙盒前端捕获 402 状态码，提取参数，向本地通道 Map 累加已消费额度。
   - 使用私钥对最新的 `[channelId, accumulatedSpend]` 数据进行 EIP-712 标准签名。
4. **携带凭证重试 (Authorized Retry)**：
   - SDK 自动组装 `Authorization: Bearer <channelId>:<accumulatedSpend>:<sig>`，立即重新发起请求。
   - 网关识别到合法 Token，放行请求中继给 Eliza Agent 计算并写回 200 结果，整个过程对终端用户无感。

---

## 二、 EIP-712 状态通道与累计签名批量结算 (Cumulative Signatures & Batch Settle)

为了解决 Layer-2 频繁转账所产生的大量 Gas 消耗，协议采用了状态通道累进式签名的设计。

### 2.1 链下累进签名与链上单次结算
- **链下累计**：在整个服务期内，Payer（消费者）与 Agent 并不在链上实时划扣 USDC，而是通过累计消费额度进行签名。例如，第 1 次使用签名 `accumulatedAmount = 1000`；第 2 次使用签名 `accumulatedAmount = 2000`，以此类推。
- **EIP-712 签名防篡改**：签名数据经过强结构化配置，包含 `Domain Separator`（指定 chainId、合约地址）与类型哈希，彻底杜绝了重放攻击。其编码结构符合：
  $$\text{computedHash} = \text{keccak256}(\text{abi.encode}(\text{CHANNEL\_SETTLE\_TYPEHASH}, \text{channelId}, \text{accumulatedAmount}))$$
- **链上单次清算**：在通道到期或主动关闭时，AA Bridge 将最新的一笔累积消费额度与对应的签名提交给 `PaymentEscrow.batchSettle`。合约通过 ECDSA 验签成功后，执行单次链上划扣：将累计发生的 $N$ USDC 转入收款方，并将剩余锁定资金退回给 Payer，最大化节省了链上 Gas。

---

## 三、 ERC-6551 专属 TBA 收款隔离 (TBA Isolation & Auto-Deploy)

AgentPay 深度融合了 ERC-6551 规范，为每一个 AI 智能体（代表一个 Soulbound NFT）分配独占的代币绑定账户（Token Bound Account, TBA）。

### 3.1 TBA 派生与部署生命周期
- **计数反查与部署**：当向 Agent 发起结算时，AA Bridge 从托管合约读取 Registry，通过 viem 调用 `IERC6551Registry.account(...)` 反查当前 AgentId 在链上的 counterfactual 专属 TBA 地址。
- **按需延迟部署**：通过 `getBytecode` 检测地址，如果尚未部署（bytecode 长度为 0），则立即在链上执行 `createAccount` 进行自动部署。
- **强制收款防越权校验**：
  - 为了杜绝 Settler（结算人）伪造收款人地址转移资金，`PaymentEscrow.sol` 增加了地址校验防线。
  - 在 `batchSettle` 与 `releasePayment` 发生资金划转前，合约会强制计算当前 Agent 的 counterfactual 专属 TBA 地址，并与传入的收款地址比对，若不相等则直接 `revert` 拦截，确保资金 100% 流入 Agent 的独占金库。
- **TBA 权限隔离**：TBA 账户受 NFT 所有权约束，仅限持有该 NFT 拥有权的 Owner 地址才能调用 `execute()` 提取资金，实现了资金控制权的平滑漂移。

---

## 四、 Go Gateway 高吞吐 SQLite 任务持久化队列 (SQLite Persistent Queue)

为了保证在大并发微支付下，网关掉线或区块链 RPC 发生故障时资金凭证不丢失，网关实现了轻量级持久化任务队列。

### 4.1 核心技术要点
- **CGO-free 纯 Go 驱动**：使用 `modernc.org/sqlite`，消除了复杂的 C 库编译依赖。
- **高并发连接控制 (Anti-Lock)**：
  - 挂载 `db.SetMaxOpenConns(1)`，强制 SQLite 使用单一连接进行写操作，彻底杜绝 `database is locked` 文件冲突锁死。
  - 开启 `PRAGMA journal_mode=WAL;` 预写日志，大幅提升了并发读取吞吐性能。
  - 设置 `PRAGMA busy_timeout=5000;`，在高并发发生竞争时，允许连接自动忙碌等待并重试最多 5 秒。
- **优雅退出屏障**：在网关内部使用 `sync.WaitGroup` 追踪后台 Worker 协程，在进程或测试退出时，首先通知 Context 取消，接着阻塞等待 Worker 完全安全退出后，再关闭 DB 连接，彻底杜绝了 `sql: database is closed` 竞态报错。
- **指数级退避重试**：
  - 任务入库后状态设为 `pending`。
  - 后台 Worker 协程每 2 秒扫描一次过期任务，向 Bridge 发起结算投递。
  - 如果遭遇网络或 Bridge 掉线，增加 `retry_count`。如果不足 5 次，按照退避公式计算下一次重试时间：`delay := time.Duration(1 << newRetryCount) * time.Second`（即 $2^{retry\_count}$ 秒）。若超过 5 次，将状态置为 `failed` 并输出 Critical 告警。

---

## 五、 限流、垃圾回收与防 IP 欺骗防御 (Rate Limiting & anti-Spoofing)

网关最外层设置了健壮的安全屏障，以阻断高频扫描和恶意请求攻击。

### 5.1 安全防线设计
- **最顶层挂载**：限流中间件挂载在 Chi 路由器的最外侧，优先于 X-402 验证逻辑，起到前置限流保护作用。超过令牌桶速率限制（Rate=5, Burst=10）立即拦截并返回 `HTTP 429` 响应。
- **动态 TTL 内存回收防 OOM**：为了防止攻击者利用海量随机伪造的 IP 头发起攻击，导致网关内存中无限堆积限流对象引起崩溃。网关将限流器与活跃时间戳绑定，每隔 1 分钟执行一次后台扫描，清除已过期 5 分钟未活跃的 IP 限流实例，实现平稳的垃圾回收。
- **防 IP 欺骗策略**：限制只在 `TRUST_PROXY=true` 时才采信 `X-Forwarded-For` 头部首个 IP，其余情况下只提取 `r.RemoteAddr` 底层套接字 IP，防止本地环境被伪造的 Header 流量轻易击穿限流规则。

---

## 六、 并发串行排队锁 (Client-Side Concurrency Lock)

由于微支付状态通道的余额累加和签名重试是异步网络交互，并发请求可能会读取到相同的初始额度，从而产生完全相同的额度和签名被网关拒绝。

### 6.1 SDK 与 Playground 内部的串行锁
- **Promise 串联机制**：在 SDK (`src/client.ts`) 以及沙盒前端 (`playground.html`) 内部，使用 `Map<number, Promise<any>>` 对相同 `agentId` 通道的多次 execute 请求进行强制的 Promise 链串联排队：
  ```javascript
  const currentLock = channelLocks.get(agentId) || Promise.resolve();
  const nextLock = currentLock.then(() => executeRequestInternal(agentId, input));
  channelLocks.set(agentId, nextLock.catch(() => {}));
  ```
- **死锁防范**：排队外壳中配置了 `.catch(() => {})` 机制。即使前面的某次网络请求由于断网或价格超额而挂掉，后面的队列也能正常解开并运行，不会发生整个通信通道的死锁挂起。
- **自愈额度直通**：排队锁的物理隔离保证了并发请求（如 `Promise.all`）被串行解耦发送。当请求 #2 发送时，请求 #1 已自愈并完成了额度累增。此时，请求 #2 直接带着 1000 + 1000 = 2000 的累计额度直接一发直通，不再需要经过第二次 402 自愈，提高了并发情况下的支付吞吐。

---

## 七、 可插拔复式记账账本 (Double-Entry Bookkeeping Ledger)

为了提供金融级交易可追溯性并防范对账纠纷，系统引入了 `ledger` 账本模块。

### 7.1 双式记账平衡与清结算分离
- **轧差平衡**：在对任意 `Invoice` 执行 `SettleInvoice` 时，系统在单笔数据库事务中，为付款人（User 账户）记入一笔 `debit`（负值余额），为收款智能体及平台记入相等的 `credit`（正值余额），确保全局借贷余额轧差恒等于零。
- **清结算隔离解耦**：
  - `PaymentRail` 抽象物理结算动作。`CryptoRail.Split` 解析底层签名凭证透传数据（`extData`），向链上中继（`aa-bridge`）广播交易。
  - `SettleInvoice` 在确定物理分账成功之后，方进行本地双式入账并标记 `Invoice` 状态为 `settled`。

---

## 八、 多计费模型与周期配额管理 (Multi-Model Billing & Quota)

通用计费引擎 `pricing` 模块提供弹性的可编程计费方式，解除网关与单一定价的硬编码耦合。

### 8.1 三大计费模型
- **按次计费 (Per-Call)**：按调用次数或 tokens 用量线性收费（不足 1k 按 1k token 向上取整）。
- **阶梯计费 (Tiered)**：获取该智能体在此账户下的当前总累积 token 数，自动落入对应优惠比例区间（例如消耗 500k token 后自动打 8 折）。
- **订阅计费 (Subscription)**：支持对特定 Agent 预购周期配额包。在配额可用次数内优先消耗配额并执行 `RecordUsage`，超额后平滑退避为普通的线性按次扣款模式。
