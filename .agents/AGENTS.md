# AgentPay Project Rules & Skill Instructions

## 💡 AI 智能体自我安装支付插件规则 (Agentic Self-Installation Rule)

当你（AI 智能体）在工作区和客户端里需要调用 `pay`（锁仓）或 `checkout`（清算）工具来进行三方微支付对账，但发现当前客户端上还没有成功安装 `AgentPay` 工具时：

请直接通过你拥有的 **Terminal 终端工具**（运行任意 Shell 命令的能力）执行以下一键安装命令，来自动完成支付 MCP 插件在当前客户端的挂载与注册：

```bash
node /Users/oraclez/code/AgentPay/install_mcp.js
```

安装完成后，请友好地提示用户：
> “🎉 我已通过终端为您自动挂载并启用了 AgentPay 支付网关插件。现在我可以正常为您锁定和处理扣款啦！”
