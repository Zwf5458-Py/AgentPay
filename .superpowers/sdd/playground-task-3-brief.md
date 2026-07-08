# Task 3 Brief: 编写高质感 Web 沙盒调试单页 (playground.html)

## 目标
在项目根目录下，新建并实现一个极富现代感、高交互性且拥有 Glassmorphism 毛玻璃美学的单页调试应用 `playground.html`，支持纯前端 Mock 轨迹仿真以及本地真实网关/网桥直连自愈测试。

## 涉及文件
- 新增: `playground.html` (根目录)

## 详细要求

### 1. 界面与 Vanilla CSS 视觉设计
- **禁用 TailwindCSS**：直接使用原生 HTML `<style>` 标签编写 Vanilla CSS。
- **背景风格**：深色调 `#0b0f19`，使用 `radial-gradient` 引入由紫色到靛蓝的暗部溢出霓虹背光效。
- **毛玻璃卡片 (Glassmorphism Card)**：
  - 使用 `backdrop-filter: blur(16px) saturate(180%);` 配合半透明的暗底 `rgba(15, 23, 42, 0.6)`，外加细微的白边 `1px solid rgba(255, 255, 255, 0.08)`。
  - 引入 hover 过渡动画，使卡片向上微移 2px，发光边框颜色向 indigo (`#6366f1`) 渐变。
- **排版字体**：
  - 从 Google Fonts 导入 `<link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;600;800&display=swap" rel="stylesheet">` 作为主字体。

### 2. UI 功能板块划分
- **左侧控制面板 (Control Panel)**：
  - **调试模式切换**：一个高对比度的 Toggle 或 Radio Group，支持切换 `Mock 仿真演示模式` 与 `本地直连调试模式`。
  - **网络红绿灯 (Health Indicators)**：
    - `● Gateway (8080)`
    - `● AA Bridge (3001)`
    - 在直连模式下，页面每 3 秒发起一次 OPTIONS 探测请求，若成功联通显示绿色，失联显示暗灰/红色。
  - **测试输入框**：输入框支持 Agent ID（默认为 888）、输入数据 Input（如 Hello）、以及限额 Max Price。
  - **操作**：
    - 按钮一：`发送单次请求 (Execute)`
    - 按钮二：`并发双击请求测试 (Promise.all)`，用于演示排队锁自愈。
- **中间数据面板 (Monitor Center)**：
  - **通道账本 (Local Channels)**：展示当前活跃通道、已确认额度 `confirmedSpend` 和累计发生额。
  - **网关 SQLite 任务监视表 (SQLite Queue Monitor)**：
    - 以精致的表格渲染最新的 SQLite 任务列表（在直连模式下，每 2 秒向 `http://127.0.0.1:8080/debug/tasks` 发起一次拉取）。
    - 表格列包括：Lock ID、状态（Pending / Success / Failed）、重试次数和更新时间。
    - 状态列带有霓虹呼吸灯徽章（如 Pending 为黄色微弱闪烁，Success 为常亮清澈绿灯，Failed 为暗红灯）。
- **右侧追踪面板 (Timeline & Console)**：
  - **垂直时序图 (Interactive Timeline)**：
    - 展示四个节点：`1. Send Request` -> `2. Intercept 402` -> `3. Generate Signature` -> `4. Successful Settle`。
    - 当发出请求自愈时，各个节点应根据真实/模拟所处的阶段亮起对应的呼吸指示灯和渐变色。
  - **滚动终端日志 (Console Log Terminal)**：
    - 模拟黑色命令行窗口，显示带有带彩色日志级别的时序日志流（如绿色 `[SUCCESS]`、黄色 `[WARN]`、紫色 `[SIGN]`、白色 `[INFO]`），支持清除日志。

### 3. JavaScript 交互与双轨协议自愈逻辑
- **Mock 模式实现**：
  - 拦截真正的 fetch 动作。使用 `setTimeout` 延迟分步演进：
    - 捕获 402：前端抛出 402 的 Headers（X-402-Price 等）。
    - 自签余额：将本地 confirmedSpend 从 0 累加至 1000，利用随机私钥生成伪 signature 头。
    - 二次重试：成功并吐出 mock 推理结果。
- **直连模式实现**：
  - 真实调用 `fetch('http://127.0.0.1:8080/agent/execute')`。
  - 对 402 响应进行捕获，检查并创建通道。
  - 使用本地随机私钥进行真实的 EIP-712 签名，将 Token 组装在 `Authorization: Bearer <channelId>:<accumulatedSpend>:<sig>` 中进行二次请求，以验证在浏览器中直连网关自愈的闭环行为。

## 验证与测试命令
- 在项目根目录下，您可以使用 python 的简易 http 模块进行本地托管测试：
  ```bash
  python3 -m http.server 8000
  ```
  然后在浏览器中打开 `http://localhost:8000/playground.html`，验证其视觉观感及双轨运行效果，检查无任何报错。
