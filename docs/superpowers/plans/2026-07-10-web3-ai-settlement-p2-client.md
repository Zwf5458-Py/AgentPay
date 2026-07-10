# Web3 AI Settlement P2 (Scenario & Client) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Modify the AI Agent into a Solidity smart contract code security auditor, and implement the `client.html` user interface featuring dual-mode wallet signing (private key + MetaMask) and interactive Markdown audit report rendering.

**Architecture:**
- **AI Agent**: Upgrade the prompt configuration in `agent/src/index.ts` to instruct the local LLM (Hermes) to act as a Solidity safety auditor, enforcing structured Markdown output format with security scores.
- **Client Frontend**: Introduce `client.html` in the root folder, featuring a split pane dark neon design with Web3/private-key connect support and Markdown renderer (marked.js via CDN).

**Tech Stack:** TypeScript (Fastify, tsx), HTML5/CSS3 (Vanilla, Glassmorphism), ethers.js / viem (via CDN in browser).

## Global Constraints
- **Platform Name**: `"AgentPay"` (EIP-712 Domain Name)
- **Chain ID**: `84532` (Base Sepolia) / `31337` (Anvil Local)
- **Escrow Contract**: PaymentEscrow (`0x4d5e11be368a8f5ea304197475467b3173c25eea` or configured contract)

---

### Task 1: Modify Agent into a Solidity Code Auditor

**Files:**
- Modify: `agent/src/index.ts`

**Interfaces:**
- Input: `{ agentId: number, input: string }`
- Output: `{ output: string, proof: InferenceProof, usage: { prompt_tokens, completion_tokens } }`

- [ ] **Step 1: Update the messages array in agent/src/index.ts to instruct the LLM**
  Modify lines 40-70 in `agent/src/index.ts`. Construct the chat completion messages block to enforce Solidity audit safety rules:
  ```typescript
  const systemPrompt = `你是一个顶级的 Web3 智能合约安全专家。请对用户提交的 Solidity 代码进行安全审计。
要求必须返回以下格式的结构化 Markdown 审计报告：

# 智能合约安全审计报告

## 1. 漏洞概览
- 🔴 高风险漏洞：[数量]
- 🟡 中风险漏洞：[数量]
- 🟢 低风险漏洞：[数量]

## 2. 安全综合评分
[分值，例如：85/100] 🛡️ [安全性评语]

## 3. 漏洞详情与防范建议
### [漏洞名称] ([风险级别])
- **行号**: [大概行号或相关代码片段]
- **原理说明**: [漏洞产生原因简述]
- **防范建议**: [修复建议与安全代码示例]
`;

  const messages = [
    { role: 'system', content: systemPrompt },
    { role: 'user', content: input }
  ];
  ```

- [ ] **Step 2: Update the fetch body to send systemPrompt and user input**
  ```typescript
  body: JSON.stringify({
    model: llmModel,
    messages: messages,
    temperature: 0.2 // Lower temperature for consistent auditing reports
  })
  ```

- [ ] **Step 3: Run the agent locally and verify formatting via curl**
  Start agent: `cd agent && npm run dev`
  Query agent directly:
  ```bash
  curl -s -X POST http://127.0.0.1:3002/agent/execute \
    -H "Content-Type: application/json" \
    -d '{"agentId": 888, "input": "contract Malicious { mapping(address => uint) balances; function withdraw() public { msg.sender.call{value: balances[msg.sender]}(); balances[msg.sender] = 0; } }"}'
  ```
  Expected: Outputs a Markdown formatted report indicating a high-risk Reentrancy vulnerability.

- [ ] **Step 4: Commit changes**
  ```bash
  git add agent/src/index.ts
  git commit -m "feat(agent): update LLM prompt to Solidity security auditing assistant"
  ```

---

### Task 2: Create client.html UI Layout & Static Markdown Renderer

**Files:**
- Create: `client.html`

**Interfaces:**
- HTML single-page client interface accessible via `file://` or local dev server.

- [ ] **Step 1: Scaffold client.html structure with split view**
  Create `client.html` in the repository root. Link to Google Fonts (Inter/Outfit) and marked.js (via CDN) for Markdown rendering:
  ```html
  <!DOCTYPE html>
  <html lang="zh">
  <head>
    <meta charset="UTF-8">
    <title>AgentPay AI Code Auditor</title>
    <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
    <!-- Add styles for dark neon glassmorphic split view -->
  </head>
  <body>
    <!-- Left Pane: Wallet input, Solidity code editor textarea, Audit button -->
    <!-- Right Pane: Timeline, Markdown Report display container, Split fee receipt invoice -->
  </body>
  </html>
  ```

- [ ] **Step 2: Style elements to premium dark neon aesthetics**
  Use CSS custom variables for neon purple, amber, and green glows. Add active pulse animations for loading steps and timeline pulse dot indicators. Make textareas look like terminal editors with custom fonts.

- [ ] **Step 3: Check html rendering in browser**
  Open the file directly in Chrome/Safari to verify visual layouts.

- [ ] **Step 4: Commit changes**
  ```bash
  git add client.html
  git commit -m "feat(client): create client.html base split-pane UI and styling"
  ```

---

### Task 3: Implement Dual-Mode Wallet Connection in client.html

**Files:**
- Modify: `client.html`

**Interfaces:**
- Supports connecting MetaMask (`window.ethereum`) and manual private key input.

- [ ] **Step 1: Implement private key input connection**
  Provide input for private key with default fallback `0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80`. Load ethers.js via CDN:
  `<script src="https://cdn.ethers.io/lib/ethers-5.6.umd.min.js" type="application/javascript"></script>`
  Verify that we can derive target address and balances of USDC test token from this private key.

- [ ] **Step 2: Implement MetaMask window.ethereum connection**
  Add a button "Connect Browser Wallet". When clicked:
  ```javascript
  const provider = new ethers.providers.Web3Provider(window.ethereum);
  await provider.send("eth_requestAccounts", []);
  const signer = provider.getSigner();
  const address = await signer.getAddress();
  ```
  Update connection badge to indicate "MetaMask Connected" with address.

- [ ] **Step 3: Test wallet connection locally**
  Ensure addresses are derived correctly in both modes.

- [ ] **Step 4: Commit changes**
  ```bash
  git add client.html
  git commit -m "feat(client): add MetaMask and private-key connection modes to client"
  ```

---

### Task 4: Connect End-to-End Self-heal & Split Invoice Presentation

**Files:**
- Modify: `client.html`

**Interfaces:**
- Intercepts 402, requests signature, retries, and parses `X-402-Settle-Receipt`.
- Displays Markdown audit report and three-party payment splits.

- [ ] **Step 1: Write EIP-712 signing handler supporting both modes**
  When 402 is returned, sign the `ChannelHold` struct:
  - If MetaMask: call `signer._signTypedData(...)`
  - If Private Key: instantiate `ethers.Wallet` and call `wallet._signTypedData(...)`

- [ ] **Step 2: Parse and render result Markdown report and invoice splits**
  Upon receiving 200:
  - Extract the output from response JSON body and render using `marked.parse(resultData.output)`.
  - Extract `X-402-Settle-Receipt` header: `<channelId>:<holdAmount>:<actualCost>:<nonce>:<sig>`.
  - Extract `X-402-Platform-Bps` and `X-402-Model-Provider`.
  - Calculate `platformFee = actualCost * platformBps / 10000`, `modelCost = modelCost`, `serviceFee = actualCost - platformFee - modelCost`.
  - Render these breakdown values into the right-pane fee receipt card.

- [ ] **Step 3: Perform end-to-end audit query**
  Submit a reentrancy-vulnerable contract in `client.html`, audit, and confirm that the audit report is fully rendered and payment splits are correctly calculated.

- [ ] **Step 4: Commit changes**
  ```bash
  git add client.html
  git commit -m "feat(client): implement 402 self-heal signing and markdown rendering in client"
  ```
