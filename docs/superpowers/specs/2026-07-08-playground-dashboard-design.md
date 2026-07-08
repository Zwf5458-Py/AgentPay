# Design Spec: High-Texture Web Playground (playground.html)

**Date**: 2026-07-08  
**Status**: Draft  

---

## 1. 目标与视觉美学

### 1.1 视觉风格 (Vanilla CSS)
- **背景与背光**：
  - 底色：深色暗灰 `#0b0f19`。
  - 霓虹背光：在背景中使用一个或多个巨大的 `radial-gradient` 圆形光晕，色彩从深紫 (`rgba(139, 92, 246, 0.15)`)、靛蓝 (`rgba(99, 102, 241, 0.15)`) 逐渐淡入底色，形成暗部溢出的科幻背光效。
- **毛玻璃卡片 (Glassmorphism)**：
  - 核心属性：`backdrop-filter: blur(16px) saturate(180%); -webkit-backdrop-filter: blur(16px) saturate(180%);`。
  - 填充背景：极高半透明暗蓝灰 `rgba(15, 23, 42, 0.65)`。
  - 细微边框：`1px solid rgba(255, 255, 255, 0.08)`。
  - hover 动效：`transform: translateY(-2px)`，边框渐变或过渡至 `rgba(99, 102, 241, 0.4)`，并伴有细微的外部阴影发光 (`box-shadow: 0 8px 32px 0 rgba(99, 102, 241, 0.2)`)。
- **排版字体**：
  - 引入 Google Fonts 的 `Outfit` 字体家族，设置无衬线主字体。

---

## 2. 界面布局与三大分区 (DOM 结构)

页面将采用经典的横向三栏自适应布局 (`display: flex` 或 `display: grid; grid-template-columns: 1fr 1.2fr 1fr;`)，使得监控一目了然：

```
+-----------------------------------------------------------------------------------+
|                                  PLAYGROUND DASHBOARD                             |
+--------------------------+------------------------------+-------------------------+
| [左侧控制面板]           | [中间数据面板]               | [右侧追踪面板]          |
| 1. 调试模式切换          | 1. 通道账本 (Local Channels)  | 1. 垂直时序自愈图       |
|    (Mock vs Direct)      |                              |    (Timeline Nodes 1-4) |
| 2. 网络红绿灯监控        | 2. SQLite 队列监视表         |                         |
|    (Gateway & AA Bridge) |    (精致状态徽章霓虹灯)      | 2. 命令行终端滚动日志   |
| 3. 测试输入框 (Agent ID) |                              |    (Console Log Stream) |
| 4. 操作按钮 (Execute)    |                              |                         |
+--------------------------+------------------------------+-------------------------+
```

### 2.1 左侧控制面板 (Control Panel)
- **调试模式切换**：一个精美的毛玻璃滑块或高对比度双态按钮，控制全局 JS 运行模式。
- **健康指示灯**：两个带呼吸动效的原点，对应 `Gateway (8080)` 与 `AA Bridge (3001)`。直连模式下，每 3秒发起一次 OPTIONS 探测；若请求成功显示绿色 (`#10b981`)，失败显示红色 (`#ef4444`)。
- **输入框**：
  - Agent ID (默认值 `888`)
  - Input 数据 (默认值 `Hello`)
  - Max Price (默认值 `5000`)
- **操作按钮组**：
  - `Execute` 单次执行按钮。
  - `Double Click Concurrent (Promise.all)` 并发锁压力测试按钮。

### 2.2 中间数据面板 (Monitor Center)
- **通道账本**：
  - 动态显示：通道 ID (如 `channel-888`)，已确认额度 `confirmedSpend`，累计额度 `accumulatedSpend`。
- **SQLite 队列监视表**：
  - 渲染 `http://127.0.0.1:8080/debug/tasks` 返回的任务。
  - 包含列：Lock ID, Status, Retry Count, Updated At。
  - 状态霓虹微弱闪烁徽章：
    - `Pending` (黄灯呼吸 `#f59e0b`)
    - `Success` (绿灯常亮 `#10b981`)
    - `Failed` (红灯呼吸 `#ef4444`)

### 2.3 右侧追踪面板 (Timeline & Console)
- **垂直时序图**：
  - 四个流程节点：
    1. `Send Request` (发出请求)
    2. `Intercept 402` (捕获 402 挑战)
    3. `Generate Signature` (签名通道余额)
    4. `Successful Settle` (二次发送并最终结算)
  - 节点根据流程步骤，通过 JS 切换激活状态 (`.active`) 并亮起对应颜色的霓虹呼吸灯。
- **滚动日志终端**：
  - 高度固定、超溢自动滚动的暗黑命令行视窗，支持按级别输出彩色日志，并提供 `Clear` 清屏按钮。

---

## 3. JavaScript 双轨协议自愈交互

### 3.1 核心状态管理 (Single State Store)
```javascript
const state = {
  mode: 'mock', // 'mock' 或 'direct'
  agentId: 888,
  input: 'Hello',
  maxPrice: 5000,
  gatewayHealth: 'unknown',
  bridgeHealth: 'unknown',
  channels: {
    // 缓存每个 agentId 的通道状态
    888: { id: 'channel-888', confirmedSpend: 0, accumulatedSpend: 0, lastPrice: 0 }
  }
};
```

### 3.2 自愈执行流程 (自右侧 Timeline 同步点亮)
无论 Mock 还是 Direct 模式，都支持异步重试自愈：

1. **Step 1: 发起请求** -> 时序图 Node 1 闪烁黄灯。
2. **Step 2: 捕获 402** -> 收到 402 响应，时序图 Node 2 变亮；解析出 `X-402-Price` 等 Headers。如果价格超出 Max Price，抛出异常并在终端打印红字错误。
3. **Step 3: 签名与通道生成** -> 时序图 Node 3 变亮。
   - 累加额度：`accumulatedSpend = confirmedSpend + price`。
   - 签名：生成一个格式为 `mock-channel-sig` 或符合以太坊格式的伪签名（由 `0x` + 130位随机十六进制字符组成）。
   - 组装头部：`Authorization: Bearer <channelId>:<accumulatedSpend>:<sig>`。
4. **Step 4: 二次请求与最终结算** -> 时序图 Node 4 变亮。
   - 携带自愈头部再次发送请求，网关验证通过返回 200 并返回 AI 答复。
   - 前端更新通道缓存：`confirmedSpend = accumulatedSpend`。

---

## 5. 验证与无错测试
- 使用 Python 3 启动本地服务：`python3 -m http.server 8000`
- 在 Chrome/Edge 下访问并测试各模式，确认在没有物理网关运行时，Mock 模式可完美跑通全套视觉与流程，控制台无任何 JS 报错。
