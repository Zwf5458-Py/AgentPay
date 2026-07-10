// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../src/payment/PaymentEscrow.sol";
import "../src/identity/ReputationRegistry.sol";
import "../src/identity/ValidationRegistry.sol";
import "./MockERC20.sol";
import "../src/payment/AgentTokenBoundAccount.sol";
import "../src/identity/AgentIdentityRegistry.sol";

contract MockValidator is IValidator {
    bool private _shouldPass;

    constructor(bool shouldPass) {
        _shouldPass = shouldPass;
    }

    function setShouldPass(bool shouldPass) external {
        _shouldPass = shouldPass;
    }

    function validate(uint256 /* agentId */, bytes calldata /* proof */) external view override returns (bool) {
        return _shouldPass;
    }
}

contract MockERC6551Registry is IERC6551Registry {

    function createAccount(
        address implementation,
        bytes32 salt,
        uint256 chainId,
        address tokenContract,
        uint256 tokenId
    ) external override returns (address) {
        address addr = account(implementation, salt, chainId, tokenContract, tokenId);
        if (addr.code.length == 0) {
            new AgentTokenBoundAccount{salt: salt}(chainId, tokenContract, tokenId);
        }
        return addr;
    }

    function account(
        address /* implementation */,
        bytes32 salt,
        uint256 chainId,
        address tokenContract,
        uint256 tokenId
    ) public view override returns (address) {
        bytes32 codeHash = keccak256(
            abi.encodePacked(
                type(AgentTokenBoundAccount).creationCode,
                abi.encode(chainId, tokenContract, tokenId)
            )
        );
        
        bytes32 data = keccak256(
            abi.encodePacked(
                bytes1(0xff),
                address(this),
                salt,
                codeHash
            )
        );
        return address(uint160(uint256(data)));
    }
}

contract PaymentEscrowTest is Test {
    PaymentEscrow public escrow;
    ReputationRegistry public reputation;
    ValidationRegistry public validationRegistry;
    MockERC20 public usdc;
    MockValidator public validator;

    MockERC6551Registry public registry;
    AgentTokenBoundAccount public tbaImplementation;
    AgentIdentityRegistry public agentIdentityRegistry;

    address public settler = address(0x1);
    address public payer = address(0x2);
    address public agentOwner = address(0x3);
    uint256 public agentId = 88;

    bytes32 public requestHash = keccak256("test_request");
    bytes public dummyProof = "valid_tee_proof";

    function setUp() public {
        usdc = new MockERC20("USDC Mock", "USDC");
        validationRegistry = new ValidationRegistry();

        registry = new MockERC6551Registry();
        tbaImplementation = new AgentTokenBoundAccount(block.chainid, address(0), 0);
        agentIdentityRegistry = new AgentIdentityRegistry();

        escrow = new PaymentEscrow(
            address(usdc),
            address(validationRegistry),
            settler,
            address(registry),
            address(tbaImplementation),
            address(agentIdentityRegistry)
        );
        reputation = new ReputationRegistry(address(escrow));

        // 注册 TEE 验证器
        validator = new MockValidator(true);
        validationRegistry.registerValidator("TEE", address(validator));

        // 循环注册 Agent NFT 使得 agentId = 88 的 owner 是 address(0x3) (原本的 EOA agentOwner)
        address nftOwner = address(0x3);
        for (uint256 i = 1; i <= 88; i++) {
            vm.prank(nftOwner);
            agentIdentityRegistry.registerAgent("model", "endpoint", bytes32(0), "caps");
        }

        // 部署专属 TBA 并赋值给 agentOwner
        agentOwner = registry.createAccount(
            address(tbaImplementation),
            bytes32(0),
            block.chainid,
            address(agentIdentityRegistry),
            agentId
        );

        // 给 payer 分发代币并授权
        usdc.mint(payer, 10000 * 10**6);
        vm.prank(payer);
        usdc.approve(address(escrow), type(uint256).max);
    }

    // 0. 验证 EIP-712 域分隔符名称为 "AgentPay"
    function test_DomainSeparatorName() public view {
        bytes32 expectedDomainSeparator = keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256(bytes("AgentPay")),
                keccak256(bytes("1")),
                block.chainid,
                address(escrow)
            )
        );
        assertEq(escrow.DOMAIN_SEPARATOR(), expectedDomainSeparator);
    }

    // 1. 测试正常资金锁定与释放流程
    function test_LockAndReleaseSuccess() public {
        uint256 amount = 100 * 10**6;
        uint256 duration = 3600;

        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, amount, requestHash, duration);

        PaymentEscrow.PaymentLock memory lock = escrow.getLock(lockId);
        assertEq(lock.payer, payer);
        assertEq(lock.agentId, agentId);
        assertEq(lock.amount, amount);
        assertEq(lock.requestHash, requestHash);
        assertEq(lock.expiresAt, block.timestamp + duration);
        assertEq(uint256(lock.status), 1); // Locked

        assertEq(usdc.balanceOf(address(escrow)), amount);

        // settler 释放款项
        vm.prank(settler);
        escrow.releasePayment(lockId, dummyProof, agentOwner);

        lock = escrow.getLock(lockId);
        assertEq(uint256(lock.status), 2); // Released
        assertEq(usdc.balanceOf(agentOwner), amount);
    }

    // 2. 测试非 Settler 越权释放拦截
    function test_ReleaseForbiddenForNonSettler() public {
        uint256 amount = 100 * 10**6;
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, amount, requestHash, 3600);

        vm.expectRevert(PaymentEscrow.NotSettler.selector);
        vm.prank(payer);
        escrow.releasePayment(lockId, dummyProof, agentOwner);
    }

    // 3. 测试超时释放拦截
    function test_ReleaseExpiredLockFails() public {
        uint256 amount = 100 * 10**6;
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, amount, requestHash, 3600);

        // 快进时间到超时后
        skip(3601);

        vm.expectRevert(PaymentEscrow.LockExpired.selector);
        vm.prank(settler);
        escrow.releasePayment(lockId, dummyProof, agentOwner);
    }

    // 4. 测试无效 Proof 验证拦截
    function test_ReleaseInvalidProofFails() public {
        uint256 amount = 100 * 10**6;
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, amount, requestHash, 3600);

        // 将验证器设置为失败
        validator.setShouldPass(false);

        vm.expectRevert(PaymentEscrow.ProofValidationFailed.selector);
        vm.prank(settler);
        escrow.releasePayment(lockId, dummyProof, agentOwner);
    }

    // 5. 测试退款：未超时拦截与超时退款成功
    function test_RefundSuccessAndLockNotExpiredRevert() public {
        uint256 amount = 100 * 10**6;
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, amount, requestHash, 3600);

        // 未超时退款应被拦截
        vm.expectRevert(PaymentEscrow.LockNotExpired.selector);
        escrow.refund(lockId);

        // 快进时间到超时后
        skip(3601);

        uint256 payerBalanceBefore = usdc.balanceOf(payer);
        escrow.refund(lockId);

        PaymentEscrow.PaymentLock memory lock = escrow.getLock(lockId);
        assertEq(uint256(lock.status), 3); // Refunded
        assertEq(usdc.balanceOf(payer), payerBalanceBefore + amount);
    }

    // 6. 测试信誉评价整合与分页查询
    function test_ReputationRegistryFullFlow() public {
        vm.prank(payer);
        bytes32 lockId1 = escrow.lockPayment(agentId, 100 * 10**6, requestHash, 3600);

        bytes32 requestHash2 = keccak256("test_request2");
        vm.prank(payer);
        bytes32 lockId2 = escrow.lockPayment(agentId, 150 * 10**6, requestHash2, 3600);

        // 释放这两个付款
        vm.prank(settler);
        escrow.releasePayment(lockId1, dummyProof, agentOwner);
        vm.prank(settler);
        escrow.releasePayment(lockId2, dummyProof, agentOwner);

        // 添加评分
        vm.prank(payer);
        reputation.addFeedback(lockId1, 5, true);
        vm.prank(payer);
        reputation.addFeedback(lockId2, 4, true);

        // 校验平均分和记录数
        (uint256 averageScore, uint256 totalReviews) = reputation.getReputation(agentId);
        assertEq(totalReviews, 2);
        assertEq(averageScore, 450); // (5 + 4) * 100 / 2 = 450

        // 校验分页明细列表
        ReputationRegistry.FeedbackRecord[] memory records = reputation.getRecords(agentId, 0, 10);
        assertEq(records.length, 2);
        assertEq(records[0].score, 5);
        assertEq(records[0].lockId, lockId1);
        assertTrue(records[0].completed);
        assertEq(records[0].reviewer, payer);

        // 测试分页越界及边界
        ReputationRegistry.FeedbackRecord[] memory paged1 = reputation.getRecords(agentId, 0, 1);
        assertEq(paged1.length, 1);
        assertEq(paged1[0].score, 5);

        ReputationRegistry.FeedbackRecord[] memory paged2 = reputation.getRecords(agentId, 1, 1);
        assertEq(paged2.length, 1);
        assertEq(paged2[0].score, 4);

        ReputationRegistry.FeedbackRecord[] memory pagedEmpty = reputation.getRecords(agentId, 2, 5);
        assertEq(pagedEmpty.length, 0);
    }

    // 7. 逆向测试：验证对同一个 lockId 两次评价抛出 LockAlreadyEvaluated() 异常
    function test_addFeedbackDuplicateReverts() public {
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, 100 * 10**6, requestHash, 3600);

        vm.prank(settler);
        escrow.releasePayment(lockId, dummyProof, agentOwner);

        vm.prank(payer);
        reputation.addFeedback(lockId, 5, true);

        vm.expectRevert(ReputationRegistry.LockAlreadyEvaluated.selector);
        vm.prank(payer);
        reputation.addFeedback(lockId, 4, true);
    }

    // 8. 逆向测试：验证非该锁的 payer 尝试评分会被拒绝并抛出 NotPayer()
    function test_addFeedbackUnauthorizedPayerReverts() public {
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, 100 * 10**6, requestHash, 3600);

        vm.prank(settler);
        escrow.releasePayment(lockId, dummyProof, agentOwner);

        address hacker = address(0x999);
        vm.expectRevert(ReputationRegistry.NotPayer.selector);
        vm.prank(hacker);
        reputation.addFeedback(lockId, 5, true);
    }

    // 9. 逆向测试：验证锁定但未释放的锁无法评分 (PaymentNotReleased)
    function test_addFeedbackNotReleasedReverts() public {
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, 100 * 10**6, requestHash, 3600);

        vm.expectRevert(ReputationRegistry.PaymentNotReleased.selector);
        vm.prank(payer);
        reputation.addFeedback(lockId, 5, true);
    }

    // 10. 逆向测试：验证 settler 传入零地址接收人会被拦截 (InvalidAddress)
    function test_releaseZeroAddressReverts() public {
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, 100 * 10**6, requestHash, 3600);

        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        vm.prank(settler);
        escrow.releasePayment(lockId, dummyProof, address(0));
    }

    // 11. 边界加固测试：验证 lockPayment 限制 duration > 0 (InvalidDuration)
    function test_lockPaymentZeroDurationReverts() public {
        vm.expectRevert(PaymentEscrow.InvalidDuration.selector);
        vm.prank(payer);
        escrow.lockPayment(agentId, 100 * 10**6, requestHash, 0);
    }

    // 12. 边界加固测试：验证构造函数空地址防御
    function test_constructorZeroAddressReverts() public {
        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        new PaymentEscrow(address(0), address(validationRegistry), settler, address(registry), address(tbaImplementation), address(agentIdentityRegistry));

        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        new PaymentEscrow(address(usdc), address(0), settler, address(registry), address(tbaImplementation), address(agentIdentityRegistry));

        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        new PaymentEscrow(address(usdc), address(validationRegistry), address(0), address(registry), address(tbaImplementation), address(agentIdentityRegistry));

        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        new PaymentEscrow(address(usdc), address(validationRegistry), settler, address(0), address(tbaImplementation), address(agentIdentityRegistry));

        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        new PaymentEscrow(address(usdc), address(validationRegistry), settler, address(registry), address(0), address(agentIdentityRegistry));

        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        new PaymentEscrow(address(usdc), address(validationRegistry), settler, address(registry), address(tbaImplementation), address(0));

        vm.expectRevert(ReputationRegistry.InvalidAddress.selector);
        new ReputationRegistry(address(0));
    }

    bytes32 public constant CHANNEL_HOLD_TYPEHASH = keccak256("ChannelHold(bytes32 channelId,uint256 holdAmount,uint256 nonce,uint256 expiration)");

    // 13. 测试正常状态通道锁定与 EIP-712 批量结算流程
    function test_ChannelBatchSettle() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        // 充值并授权
        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        uint256 maxAmount = 500 * 10**6;
        uint256 duration = 3600;

        // 锁定通道
        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, maxAmount, duration);

        // 检查 channels 存储
        (
            address channelPayer,
            uint256 channelAgentId,
            uint256 channelMaxAmount,
            uint256 channelSettledAmount,
            uint256 channelExpiresAt,
            PaymentEscrow.PaymentStatus channelStatus
        ) = escrow.channels(channelId);

        assertEq(channelPayer, customPayer);
        assertEq(channelAgentId, agentId);
        assertEq(channelMaxAmount, maxAmount);
        assertEq(channelSettledAmount, 0);
        assertEq(channelExpiresAt, block.timestamp + duration);
        assertEq(uint256(channelStatus), 1); // Locked

        // 线下生成 EIP-712 签名
        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        uint256 agentOwnerBalanceBefore = usdc.balanceOf(agentOwner);
        uint256 payerBalanceBefore = usdc.balanceOf(customPayer);

        // settler 结算
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);

        // 验证通道状态
        (,,,,, channelStatus) = escrow.channels(channelId);
        assertEq(uint256(channelStatus), 2); // Released
        
        // 验证余额：agentOwner 获得 accumulatedAmount，payer 获得退回的 remainder (maxAmount - accumulatedAmount)
        assertEq(usdc.balanceOf(agentOwner), agentOwnerBalanceBefore + accumulatedAmount);
        assertEq(usdc.balanceOf(customPayer), payerBalanceBefore + (maxAmount - accumulatedAmount));
    }

    // 14. 逆向测试：伪造签名结算应失败
    function test_ChannelBatchSettleInvalidSignatureReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 500 * 10**6, 3600);

        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        // 用错误的私钥签名
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(0xBAD, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        vm.expectRevert(PaymentEscrow.InvalidSignature.selector);
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);
    }

    // 15. 逆向测试：状态通道超时后结算应被拦截
    function test_ChannelBatchSettleExpiredReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 500 * 10**6, 3600);

        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 快进时间到超时
        skip(3601);

        vm.expectRevert(PaymentEscrow.ChannelExpired.selector);
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);
    }

    // 16. 测试状态通道退款：未超时退款失败与超时退款成功
    function test_ChannelRefundSuccessAndNotExpiredRevert() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        uint256 maxAmount = 500 * 10**6;
        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, maxAmount, 3600);

        // 未超时退款应被拦截
        vm.expectRevert(PaymentEscrow.ChannelNotExpired.selector);
        escrow.refundChannel(channelId);

        // 快进时间到超时后
        skip(3601);

        uint256 payerBalanceBefore = usdc.balanceOf(customPayer);
        escrow.refundChannel(channelId);

        (,,,,, PaymentEscrow.PaymentStatus channelStatus) = escrow.channels(channelId);
        assertEq(uint256(channelStatus), 3); // Refunded
        assertEq(usdc.balanceOf(customPayer), payerBalanceBefore + maxAmount);
    }

    // 17. 增加 test_ChannelBatchSettleNonSettlerReverts
    function test_ChannelBatchSettleNonSettlerReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 500 * 10**6, 3600);

        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 模拟非 Settler 尝试结算
        vm.expectRevert(PaymentEscrow.NotSettler.selector);
        vm.prank(payer);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);
    }

    // 18. 增加 test_ChannelBatchSettleZeroAddressRecipientReverts
    function test_ChannelBatchSettleZeroAddressRecipientReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 500 * 10**6, 3600);

        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 结算传入的 agentOwner = address(0)
        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, address(0));
    }

    // 19. 增加 test_ChannelBatchSettleZeroAmountReverts
    function test_ChannelBatchSettleZeroAmountReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 500 * 10**6, 3600);

        // 累计消费 accumulatedAmount = 0
        uint256 accumulatedAmount = 0;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        vm.expectRevert(PaymentEscrow.InvalidAmount.selector);
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);
    }

    // 20. 增加 test_ChannelBatchSettleExceedMaxAmountReverts
    function test_ChannelBatchSettleExceedMaxAmountReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        uint256 maxAmount = 500 * 10**6;
        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, maxAmount, 3600);

        // 累计消费超过通道最大上限 maxAmount + 1
        uint256 accumulatedAmount = maxAmount + 1;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        vm.expectRevert(PaymentEscrow.InvalidAmount.selector);
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);
    }

    // 21. 增加 test_ChannelBatchSettleDoubleSettleReverts
    function test_ChannelBatchSettleDoubleSettleReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        uint256 maxAmount = 500 * 10**6;
        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, maxAmount, 3600);

        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 第一遍成功结算
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);

        // 尝试进行二次结算
        vm.expectRevert(PaymentEscrow.InvalidStatus.selector);
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);
    }

    // 22. 测试批量结算资金流入 TBA 账户并且 payer 收到退款
    function test_BatchSettleToTBARecipientSuccess() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        // 充值并授权
        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        uint256 maxAmount = 500 * 10**6;
        uint256 duration = 3600;

        // 锁定通道
        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, maxAmount, duration);

        // 线下生成 EIP-712 签名
        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        uint256 agentOwnerBalanceBefore = usdc.balanceOf(agentOwner);
        uint256 payerBalanceBefore = usdc.balanceOf(customPayer);

        // settler 结算
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, agentOwner);

        // 验证资金：TBA 账户收到 300，payer 收到退款 200
        assertEq(usdc.balanceOf(agentOwner), agentOwnerBalanceBefore + accumulatedAmount);
        assertEq(usdc.balanceOf(customPayer), payerBalanceBefore + (maxAmount - accumulatedAmount));
    }

    // 23. 测试非 TBA 结算接收人被 Revert 拦截
    function test_BatchSettleToNonTBARecipientReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 500 * 10**6, 3600);

        uint256 accumulatedAmount = 300 * 10**6;
        uint256 nonce = 123; uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            500 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 传入一个普通 EOA 地址作为接收方，应该被 Revert
        address nonTbaRecipient = address(0x999);
        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        vm.prank(settler);
        escrow.batchSettle(channelId, accumulatedAmount, 500 * 10**6, nonce, expiration, signature, nonTbaRecipient);
    }

    // 24. 测试 TBA.execute 只有 NFT owner 可调用
    function test_TBAExecuteOnlyOwner() public {
        // 先往 TBA 账户中转入一些 USDC 作为余额
        uint256 tbaBalance = 100 * 10**6;
        usdc.mint(agentOwner, tbaBalance);
        assertEq(usdc.balanceOf(agentOwner), tbaBalance);

        // NFT owner 是 address(0x3) (nftOwner)
        address nftOwner = address(0x3);
        address hacker = address(0x888);

        // 构造 transfer 调用数据
        bytes memory callData = abi.encodeWithSelector(
            IERC20.transfer.selector,
            address(0x555),
            50 * 10**6
        );

        // 非 NFT owner (hacker) 尝试 execute，应该 Revert
        vm.expectRevert(AgentTokenBoundAccount.NotOwner.selector);
        vm.prank(hacker);
        AgentTokenBoundAccount(payable(agentOwner)).execute(address(usdc), 0, callData);

        // NFT owner 尝试 execute，应该成功
        vm.prank(nftOwner);
        AgentTokenBoundAccount(payable(agentOwner)).execute(address(usdc), 0, callData);

        assertEq(usdc.balanceOf(address(0x555)), 50 * 10**6);
        assertEq(usdc.balanceOf(agentOwner), 50 * 10**6);
    }

    // 25. 测试 TBA execute 成功后能够使 state 递增
    function test_TBAExecuteIncrementsState() public {
        uint256 tbaBalance = 100 * 10**6;
        usdc.mint(agentOwner, tbaBalance);

        address nftOwner = address(0x3);
        
        bytes memory callData = abi.encodeWithSelector(
            IERC20.transfer.selector,
            address(0x555),
            50 * 10**6
        );

        uint256 stateBefore = AgentTokenBoundAccount(payable(agentOwner)).state();

        vm.prank(nftOwner);
        AgentTokenBoundAccount(payable(agentOwner)).execute(address(usdc), 0, callData);

        uint256 stateAfter = AgentTokenBoundAccount(payable(agentOwner)).state();

        assertEq(stateAfter, stateBefore + 1);
    }

    // 26. 测试 splitSettle 成功路径
    function test_SplitSettleSuccess() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        // 充值并授权
        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        uint256 maxAmount = 1000 * 10**6;
        uint256 duration = 3600;

        // 锁定通道
        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, maxAmount, duration);

        // 线下生成 EIP-712 签名
        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            maxAmount,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        uint256 modelCost = 200 * 10**6;
        uint256 serviceFee = 100 * 10**6;
        uint16 platformBps = 1000; // 10%

        uint256 expectedPlatformFee = (accumulatedAmount * platformBps) / 10000; // 60 USDC
        uint256 expectedAgentPayout = accumulatedAmount - modelCost - expectedPlatformFee; // 340 USDC
        uint256 expectedRemainder = maxAmount - accumulatedAmount; // 400 USDC

        uint256 payerBalanceBefore = usdc.balanceOf(customPayer);
        uint256 modelProviderBalanceBefore = usdc.balanceOf(modelProvider);
        uint256 treasuryBalanceBefore = usdc.balanceOf(treasury);
        uint256 agentOwnerBalanceBefore = usdc.balanceOf(agentOwner);

        // 预期抛出事件
        vm.expectEmit(true, false, false, true, address(escrow));
        emit PaymentEscrow.ChannelSplitSettled(
            channelId,
            expectedAgentPayout,
            modelCost,
            expectedPlatformFee,
            modelProvider,
            treasury
        );

        // settler 结算
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            modelCost,
            serviceFee,
            modelProvider,
            treasury,
            platformBps,
            maxAmount,
            nonce,
            expiration,
            signature
        );

        // 校验通道状态
        (
            ,,,
            uint256 channelSettledAmount,,
            PaymentEscrow.PaymentStatus channelStatus
        ) = escrow.channels(channelId);

        assertEq(uint256(channelStatus), 2); // Released
        assertEq(channelSettledAmount, accumulatedAmount);

        // 校验余额
        assertEq(usdc.balanceOf(modelProvider), modelProviderBalanceBefore + modelCost);
        assertEq(usdc.balanceOf(treasury), treasuryBalanceBefore + expectedPlatformFee);
        assertEq(usdc.balanceOf(agentOwner), agentOwnerBalanceBefore + expectedAgentPayout);
        assertEq(usdc.balanceOf(customPayer), payerBalanceBefore + expectedRemainder);
    }

    // 27. 测试 splitSettle 零地址校验拦截
    function test_SplitSettleZeroAddressReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 1000 * 10**6, 3600);

        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            1000 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // modelProvider 是零地址时应该 Revert
        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            address(0),
            treasury,
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );

        // treasury 是零地址时应该 Revert
        vm.expectRevert(PaymentEscrow.InvalidAddress.selector);
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            modelProvider,
            address(0),
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );
    }

    // 测试 splitSettle 平台费率超出 10000 拦截
    function test_SplitSettleInvalidPlatformBpsReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 1000 * 10**6, 3600);

        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            1000 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // platformBps > 10000 时应该 Revert
        vm.expectRevert(PaymentEscrow.InvalidAmount.selector);
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            modelProvider,
            treasury,
            10001,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );
    }

    // 28. 测试 splitSettle 非 settler 拦截
    function test_SplitSettleNonSettlerReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 1000 * 10**6, 3600);

        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            1000 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 非 settler 账户调用应该 Revert
        vm.expectRevert(PaymentEscrow.NotSettler.selector);
        vm.prank(customPayer);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            modelProvider,
            treasury,
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );
    }

    // 29. 测试 splitSettle 签名错误拦截
    function test_SplitSettleInvalidSignatureReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 1000 * 10**6, 3600);

        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            1000 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        // 用错误的私钥签名
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(0xBAD, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        vm.expectRevert(PaymentEscrow.InvalidSignature.selector);
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            modelProvider,
            treasury,
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );
    }

    // 30. 测试 splitSettle 超支拦截 (modelCost + serviceFee + platformFee > accumulatedAmount)
    function test_SplitSettleExceedAccumulatedAmountReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 1000 * 10**6, 3600);

        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            1000 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // modelCost = 400, serviceFee = 200, platformBps = 1000 (platformFee = 60).
        // 400 + 200 + 60 = 660 > 600, 应该 Revert
        vm.expectRevert(PaymentEscrow.InvalidAmount.selector);
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            400 * 10**6,
            200 * 10**6,
            modelProvider,
            treasury,
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );
    }

    // 31. 测试 splitSettle 状态不正确拦截 (例如，二次结算)
    function test_SplitSettleInvalidStatusReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 1000 * 10**6, 3600);

        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            1000 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 第一次成功结算
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            modelProvider,
            treasury,
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );

        // 第二次结算，应该 Revert InvalidStatus
        vm.expectRevert(PaymentEscrow.InvalidStatus.selector);
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            modelProvider,
            treasury,
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );
    }

    // 32. 测试 splitSettle 超时通道拦截
    function test_SplitSettleExpiredReverts() public {
        uint256 payerPrivateKey = 0xA11CE;
        address customPayer = vm.addr(payerPrivateKey);
        address modelProvider = address(0x4);
        address treasury = address(0x5);

        usdc.mint(customPayer, 1000 * 10**6);
        vm.prank(customPayer);
        usdc.approve(address(escrow), type(uint256).max);

        vm.prank(customPayer);
        bytes32 channelId = escrow.lockChannel(agentId, 1000 * 10**6, 3600);

        uint256 accumulatedAmount = 600 * 10**6;
        uint256 nonce = 123;
        uint256 expiration = 3600;
        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_HOLD_TYPEHASH,
            channelId,
            1000 * 10**6,
            nonce,
            expiration
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            escrow.DOMAIN_SEPARATOR(),
            hashStruct
        ));

        (uint8 v, bytes32 r, bytes32 s) = vm.sign(payerPrivateKey, digest);
        bytes memory signature = abi.encodePacked(r, s, v);

        // 时间快进到超时
        skip(3601);

        vm.expectRevert(PaymentEscrow.ChannelExpired.selector);
        vm.prank(settler);
        escrow.splitSettle(
            channelId,
            accumulatedAmount,
            200 * 10**6,
            100 * 10**6,
            modelProvider,
            treasury,
            1000,
            1000 * 10**6,
            nonce,
            expiration,
            signature
        );
    }
}
