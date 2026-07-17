#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const os = require('os');

const homedir = os.homedir();
const mcpPaths = [
    path.join(homedir, '.kiro/settings/mcp.json'),
    path.join(homedir, '.qoderworkcn/bin/computer-use/mcp.json'),
    path.join(homedir, 'Library/Application Support/Code/User/mcp.json')
];

const bridgeScriptPath = '/Users/oraclez/code/AgentPay/mcp_bridge.js';

console.log("=== AgentPay MCP Auto Installer ===");

mcpPaths.forEach(mcpPath => {
    if (!fs.existsSync(mcpPath)) {
        return;
    }

    try {
        const data = fs.readFileSync(mcpPath, 'utf8');
        let mcpJson = JSON.parse(data);

        if (!mcpJson.mcpServers) {
            mcpJson.mcpServers = {};
        }

        // 注入 AgentPay stdio 桥接器配置
        mcpJson.mcpServers["AgentPay"] = {
            "command": "node",
            "args": [bridgeScriptPath]
        };

        fs.writeFileSync(mcpPath, JSON.stringify(mcpJson, null, 2), 'utf8');
        console.log(`[Success] Successfully installed AgentPay MCP to: ${mcpPath}`);
    } catch (err) {
        console.error(`[Error] Failed to patch ${mcpPath}: ${err.message}`);
    }
});

console.log("=== Installation Completed ===");
