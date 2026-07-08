// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../src/identity/AgentIdentityRegistry.sol";
import "../src/identity/ValidationRegistry.sol";

// Mock 验证器用于测试自定义验证逻辑路由
contract MockValidator is IValidator {
    function validate(uint256 /*agentId*/, bytes calldata proof) external pure override returns (bool) {
        // 如果 proof 以 0x01 开头，返回 true，否则返回 false
        if (proof.length > 0 && proof[0] == 0x01) {
            return true;
        }
        return false;
    }
}

contract AgentIdentityTest is Test {
    AgentIdentityRegistry public identityRegistry;
    ValidationRegistry public validationRegistry;

    address public owner = address(0xABC);
    address public user1 = address(0x111);
    address public user2 = address(0x222);

    function setUp() public {
        // 切换到 owner 部署合约
        vm.startPrank(owner);
        identityRegistry = new AgentIdentityRegistry();
        validationRegistry = new ValidationRegistry();
        vm.stopPrank();
    }

    // 1. 测试 AgentIdentityRegistry 的注册和基本元数据读取
    function testRegisterAgent() public {
        vm.startPrank(user1);
        
        string memory modelId = "deepseek-r1";
        string memory serviceEndpoint = "https://api.deepseek.com/v1";
        bytes32 teeAttestation = keccak256("attestation-data");
        string memory capabilities = "inference,payment";

        uint256 agentId = identityRegistry.registerAgent(
            modelId,
            serviceEndpoint,
            teeAttestation,
            capabilities
        );

        // 验证返回的 agentId 应该为 1
        assertEq(agentId, 1);

        // 验证元数据是否正确写入
        (
            string memory rModelId,
            string memory rServiceEndpoint,
            bytes32 rTeeAttestation,
            string memory rCapabilities,
            uint256 rRegisteredAt,
            address rOwner
        ) = identityRegistry.getAgent(agentId);

        assertEq(rModelId, modelId);
        assertEq(rServiceEndpoint, serviceEndpoint);
        assertEq(rTeeAttestation, teeAttestation);
        assertEq(rCapabilities, capabilities);
        assertEq(rRegisteredAt, block.timestamp);
        assertEq(rOwner, user1);

        // 验证 verifyAgent 接口
        assertTrue(identityRegistry.verifyAgent(agentId));
        assertFalse(identityRegistry.verifyAgent(999)); // 不存在的 agentId

        vm.stopPrank();
    }

    // 2. 测试更新元数据
    function testUpdateMetadata() public {
        vm.startPrank(user1);
        uint256 agentId = identityRegistry.registerAgent(
            "model-a",
            "endpoint-a",
            keccak256("tee-a"),
            "cap-a"
        );

        // 拥有者成功更新
        string memory newModel = "model-b";
        string memory newEndpoint = "endpoint-b";
        bytes32 newTee = keccak256("tee-b");
        string memory newCap = "cap-b";

        identityRegistry.updateMetadata(agentId, newModel, newEndpoint, newTee, newCap);

        (
            string memory rModelId,
            string memory rServiceEndpoint,
            bytes32 rTeeAttestation,
            string memory rCapabilities,,
        ) = identityRegistry.getAgent(agentId);

        assertEq(rModelId, newModel);
        assertEq(rServiceEndpoint, newEndpoint);
        assertEq(rTeeAttestation, newTee);
        assertEq(rCapabilities, newCap);

        vm.stopPrank();

        // 非拥有者更新应该被 revert
        vm.startPrank(user2);
        vm.expectRevert(AgentIdentityRegistry.NotAgentOwner.selector);
        identityRegistry.updateMetadata(agentId, newModel, newEndpoint, newTee, newCap);
        vm.stopPrank();
    }

    // 3. 测试 Soulbound 限制 (禁止转让)
    function testSoulboundTransferBlocked() public {
        vm.startPrank(user1);
        uint256 agentId = identityRegistry.registerAgent(
            "model-a",
            "endpoint-a",
            keccak256("tee-a"),
            "cap-a"
        );

        // 尝试转让应该 Revert
        vm.expectRevert(AgentIdentityRegistry.SoulboundTransferBlocked.selector);
        identityRegistry.transferFrom(user1, user2, agentId);

        vm.expectRevert(AgentIdentityRegistry.SoulboundTransferBlocked.selector);
        identityRegistry.safeTransferFrom(user1, user2, agentId);

        vm.stopPrank();
    }

    // 4. 测试 ValidationRegistry 默认路由（未注册验证器时返回 false）
    function testValidationRegistryDefaultRouting() public {
        vm.prank(user1); // 避免 view 警告
        // 未注册验证器时，对任意 proof 验证应返回 false
        bytes memory mockProof = "some-proof";
        bool result = validationRegistry.validateProof(1, "TEE", mockProof);
        assertFalse(result);

        result = validationRegistry.validateProof(1, "MPC", mockProof);
        assertFalse(result);
    }

    // 5. 测试 ValidationRegistry 注册验证器及路由校验
    function testValidationRegistryCustomRouting() public {
        // 部署 Mock 验证器
        MockValidator mockValidator = new MockValidator();

        // 只有 Owner 可以注册验证器
        vm.startPrank(user1);
        vm.expectRevert(abi.encodeWithSignature("OwnableUnauthorizedAccount(address)", user1));
        validationRegistry.registerValidator("TEE", address(mockValidator));
        vm.stopPrank();

        // 非合约地址注册应该报错 ValidatorNotContract
        vm.startPrank(owner);
        vm.expectRevert(ValidationRegistry.ValidatorNotContract.selector);
        validationRegistry.registerValidator("TEE", user1);

        // Owner 注册验证器
        validationRegistry.registerValidator("TEE", address(mockValidator));
        assertEq(validationRegistry.getValidator("TEE"), address(mockValidator));
        vm.stopPrank();

        // 验证路由：传递以 0x01 开头的证明，应当验证通过 (true)
        bytes memory validProof = abi.encodePacked(uint8(0x01), "valid-attestation");
        bool success = validationRegistry.validateProof(1, "TEE", validProof);
        assertTrue(success);

        // 验证路由：传递其他开头的证明，应当验证失败 (false)
        bytes memory invalidProof = abi.encodePacked(uint8(0x00), "invalid-attestation");
        bool fail = validationRegistry.validateProof(1, "TEE", invalidProof);
        assertFalse(fail);

        // 验证路由：对于未注册的验证器类型返回 false
        bool result = validationRegistry.validateProof(1, "OTHER_TYPE", invalidProof);
        assertFalse(result);
    }

    // 6. 测试空字段校验
    function testRegisterAgentWithEmptyFields() public {
        vm.startPrank(user1);
        // modelId 为空
        vm.expectRevert(AgentIdentityRegistry.InvalidMetadata.selector);
        identityRegistry.registerAgent("", "endpoint-a", keccak256("tee-a"), "cap-a");
        
        // serviceEndpoint 为空
        vm.expectRevert(AgentIdentityRegistry.InvalidMetadata.selector);
        identityRegistry.registerAgent("model-a", "", keccak256("tee-a"), "cap-a");
        vm.stopPrank();
    }

    // 7. 测试 NFT 销毁（Burn）行为
    function testBurnAgent() public {
        vm.startPrank(user1);
        uint256 agentId = identityRegistry.registerAgent(
            "model-a",
            "endpoint-a",
            keccak256("tee-a"),
            "cap-a"
        );
        
        // 验证销毁
        identityRegistry.burn(agentId);
        assertFalse(identityRegistry.verifyAgent(agentId));
        
        // 验证已销毁代币不能再被 getAgent、updateMetadata 或再次 burn
        vm.expectRevert(AgentIdentityRegistry.AgentDoesNotExist.selector);
        identityRegistry.getAgent(agentId);
        
        vm.expectRevert(AgentIdentityRegistry.AgentDoesNotExist.selector);
        identityRegistry.updateMetadata(agentId, "model-b", "endpoint-b", keccak256("tee-b"), "cap-b");
        
        vm.expectRevert(AgentIdentityRegistry.AgentDoesNotExist.selector);
        identityRegistry.burn(agentId);
        
        vm.stopPrank();
    }
}
