# Task 1 Brief: 合约脚手架与 ERC-8004 身份/验证合约开发

## 目标
初始化 Foundry 项目，编写并测试 `AgentIdentityRegistry.sol`（用于注册并以 NFT 形式代表 Agent 身份）和 `ValidationRegistry.sol`（用于管理并校验推理证明的路由合约）。

## 涉及文件
- 新增: `contracts/src/identity/AgentIdentityRegistry.sol`
- 新增: `contracts/src/identity/ValidationRegistry.sol`
- 新增: `contracts/test/AgentIdentityTest.t.sol`

## 全局约束
- 链基础: Base Sepolia (Chain ID: 84532)
- 语言和框架: Solidity + Foundry (EntryPoint v0.7 / OpenZeppelin v5)

## 需求与步骤
1. **初始化 Foundry**:
   在 `/Users/oraclez/code/AgentPay/contracts` 目录下初始化 Foundry，安装 OpenZeppelin Contracts。
2. **AgentIdentityRegistry.sol**:
   - 继承 ERC721。
   - 注册元数据包含：`modelId` (string), `serviceEndpoint` (string), `teeAttestation` (bytes32), `capabilities` (string), `registeredAt` (uint256)。
   - 实现 `registerAgent` 铸造 Soulbound NFT，禁止转让 (`_update` 阶段 revert)。
   - 提供 `getAgent`, `updateMetadata`, `verifyAgent` 接口。
3. **ValidationRegistry.sol**:
   - 允许 Owner 注册不同类型的验证器（如 "TEE" 校验合约）。
   - 提供 `validateProof(uint256 agentId, string calldata validationType, bytes calldata proof)` 接口路由验证。如果没有注册对应验证器，默认放行返回 true（Mock 验证）。
4. **测试验证 (TDD)**:
   - 编写 `AgentIdentityTest.t.sol` 验证注册、元数据读取。
   - 验证转让会被 Revert。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test
```
预期结果：所有测试全部通过。
