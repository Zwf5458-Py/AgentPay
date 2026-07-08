# Task 3 修复 Brief: Web 沙盒并发排队锁加固与 UI 竞争修复

## 问题描述与修复要求

### 1. 修复前端 Promise 排队锁机制 (`playground.html`)
- 在 `<script>` 脚本区中定义全局通道锁容器：
  `const channelLocks = new Map();`
- 将原有的 `executeRequest(agentId, input)` 重构为业务核心方法：
  `async function executeRequestInternal(agentId, input)`
- 编写新的公有方法 `async function executeRequest(agentId, input)`，实现类似 TS SDK 的 Promise 排队链：
  - 检查 `channelLocks.get(agentId)`。如果有正在进行的 Promise，则利用 `.then()` 追加串联，并在 finally 阶段释放。
  - 在开始排队时，向日志终端打印：`[QUEUE] Request queued, waiting for channel lock...`。
  - 在排队结束锁释放时，打印：`[QUEUE] Lock released, starting request...`。
  - 保证通过 `.catch(() => {})` 捕获单次请求错误，防止队列崩溃。

### 2. 消除时序组件 UI 竞争闪烁
- 在 Promise 链强制串行执行后，通过阻断多请求并发进入 `setTimelineState` 阶段，消除右侧流程面板步骤亮起指示灯的相互覆盖和闪烁。

## 验证与测试命令
- 在本地双击或通过 python 服务打开 `playground.html`。
- 点击 `Concurrent Execute (Promise.all)` 并发测试按钮，观察右侧日志终端：
  - 断言输出 `[QUEUE]` 排队等待信息。
  - 断言两路自愈请求按序先后发出（第一路自愈后 confirmedSpend 从 0n 累加至 1000n 并返回 200；第二路请求自动读取已被第一路更新过的额度 1000n 直接叠加 1000n 发送，不需要经过第二次 402 自愈，一次通过）。
  - 观察右侧 Timeline 无任何闪烁，顺畅更新。
