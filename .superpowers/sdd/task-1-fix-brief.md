# Task 1 修复 Brief: 合约优化与安全加固

## 问题描述与修复要求
基于 Task 1 评审报告，我们需要对 `contracts/src/identity/` 下的合约进行重构优化，排除设计隐患。

### 1. 安全加固 ValidationRegistry.sol
- **禁止空验证器默认放行**：如果 `_validators[validationType] == address(0)`，直接返回 `false`（不要默认返回 `true`）。
- **注册合约检查**：在 `registerValidator` 中，确保传入的 `validatorContract` 必须是合约账户（即 `validatorContract.code.length > 0`），除非该地址是 `address(0)`（用于注销该验证器）。
- **优化异常处理**：在 `validateProof` 中，`try IValidator(validator).validate(agentId, proof) returns (bool result)`，如果抛出异常或调用失败，必须安全返回 `false`。

### 2. 合约最佳实践改进 AgentIdentityRegistry.sol
- **自定义错误**：定义 `error NotAgentOwner()`、`error SoulboundTransferBlocked()`、`error AgentDoesNotExist()` 等自定义 Error 代替传统 `require` 的字符串 Revert，以优化 Gas。
- **CEI 优化**：在 `_update` 校验中，先执行 Soulbound 的转让判断，当转让不合规时提早 revert，然后再执行 `super._update(to, tokenId, auth)`。
- **空字段校验**：在 `registerAgent` 中校验 `bytes(metadata.modelId).length > 0` 且 `bytes(metadata.serviceEndpoint).length > 0`。

### 3. 测试适配与补充 AgentIdentityTest.t.sol
- **更新 Mock 测试**：因为移除了默认放行，测试中如果需要验证默认/Mock 行为，请显式部署一个辅助的 `MockValidator` 并通过 `registerValidator` 绑定到测试的 `ValidationRegistry`。
- **添加 Burn 测试**：补充对 NFT 销毁（Burn）行为的测试，验证 Soulbound 限制不会在 `to == address(0)`（即销毁）时产生阻拦。

## 验证与测试命令
在 `contracts` 目录下执行：
```bash
forge test
```
要求：所有测试无误通过。
