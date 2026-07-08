// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../src/payment/PaymentEscrow.sol";
import "../src/identity/ReputationRegistry.sol";
import "../src/identity/ValidationRegistry.sol";
import "./MockERC20.sol";

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

contract PaymentEscrowTest is Test {
    PaymentEscrow public escrow;
    ReputationRegistry public reputation;
    ValidationRegistry public validationRegistry;
    MockERC20 public usdc;
    MockValidator public validator;

    address public settler = address(0x1);
    address public payer = address(0x2);
    address public agentOwner = address(0x3);
    uint256 public agentId = 88;

    bytes32 public requestHash = keccak256("test_request");
    bytes public dummyProof = "valid_tee_proof";

    function setUp() public {
        usdc = new MockERC20("USDC Mock", "USDC");
        validationRegistry = new ValidationRegistry();
        
        escrow = new PaymentEscrow(address(usdc), address(validationRegistry), settler);
        reputation = new ReputationRegistry(address(escrow));

        // 注册 TEE 验证器
        validator = new MockValidator(true);
        validationRegistry.registerValidator("TEE", address(validator));

        // 给 payer 分发代币并授权
        usdc.mint(payer, 10000 * 10**6);
        vm.prank(payer);
        usdc.approve(address(escrow), type(uint256).max);
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
        assertTrue(escrow.hasPaid(payer, agentId));
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

    // 6. 测试信誉评价整合
    function test_ReputationRegistryFullFlow() public {
        bytes32 taskHash = keccak256("task_1");

        // 未付款进行评分，期望 Revert NotPaid()
        vm.expectRevert(ReputationRegistry.NotPaid.selector);
        vm.prank(payer);
        reputation.addFeedback(agentId, 5, taskHash, true);

        // 付款锁定但未释放（未确认付款完成），仍然应该 Revert
        vm.prank(payer);
        bytes32 lockId = escrow.lockPayment(agentId, 100 * 10**6, requestHash, 3600);

        vm.expectRevert(ReputationRegistry.NotPaid.selector);
        vm.prank(payer);
        reputation.addFeedback(agentId, 5, taskHash, true);

        // 释放付款
        vm.prank(settler);
        escrow.releasePayment(lockId, dummyProof, agentOwner);

        // 此时应该允许评价
        vm.prank(payer);
        reputation.addFeedback(agentId, 5, taskHash, true);

        // 重复评价同一个 taskHash 期望 Revert TaskAlreadyEvaluated()
        vm.expectRevert(ReputationRegistry.TaskAlreadyEvaluated.selector);
        vm.prank(payer);
        reputation.addFeedback(agentId, 4, taskHash, true);

        // 评分超限 1-5 期望 Revert InvalidScore()
        bytes32 taskHash2 = keccak256("task_2");
        vm.expectRevert(ReputationRegistry.InvalidScore.selector);
        vm.prank(payer);
        reputation.addFeedback(agentId, 6, taskHash2, true);

        // 成功添加另一个有效评分
        vm.prank(payer);
        reputation.addFeedback(agentId, 4, taskHash2, true);

        // 校验平均分和记录数
        (uint256 averageScore, uint256 totalReviews) = reputation.getReputation(agentId);
        assertEq(totalReviews, 2);
        // (5 + 4) * 100 / 2 = 450
        assertEq(averageScore, 450);

        // 校验评价明细列表
        ReputationRegistry.FeedbackRecord[] memory records = reputation.getRecords(agentId);
        assertEq(records.length, 2);
        assertEq(records[0].score, 5);
        assertEq(records[0].taskHash, taskHash);
        assertTrue(records[0].taskCompleted);
        assertEq(records[0].reviewer, payer);

        assertEq(records[1].score, 4);
        assertEq(records[1].taskHash, taskHash2);
        assertTrue(records[1].taskCompleted);
        assertEq(records[1].reviewer, payer);
    }
}
