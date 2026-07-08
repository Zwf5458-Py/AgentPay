// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IPaymentEscrow {
    enum PaymentStatus { None, Locked, Released, Refunded }

    struct PaymentLock {
        address payer;
        uint256 agentId;
        uint256 amount;
        bytes32 requestHash;
        uint256 expiresAt;
        PaymentStatus status;
    }

    function getLock(bytes32 lockId) external view returns (PaymentLock memory);
}

contract ReputationRegistry {
    struct FeedbackRecord {
        uint8 score;
        bytes32 lockId;
        bool completed;
        address reviewer;
    }

    IPaymentEscrow public immutable paymentEscrow;

    // agentId => feedback records
    mapping(uint256 => FeedbackRecord[]) private _records;
    // agentId => total score
    mapping(uint256 => uint256) private _totalScore;
    // lockId => evaluated status
    mapping(bytes32 => bool) private _evaluatedLocks;

    event FeedbackAdded(
        uint256 indexed agentId,
        address indexed reviewer,
        uint8 score,
        bytes32 indexed lockId,
        bool completed
    );

    error InvalidScore();
    error LockAlreadyEvaluated();
    error NotPayer();
    error PaymentNotReleased();
    error InvalidAddress();

    constructor(address escrowAddress) {
        if (escrowAddress == address(0)) revert InvalidAddress();
        paymentEscrow = IPaymentEscrow(escrowAddress);
    }

    function addFeedback(
        bytes32 lockId,
        uint8 score,
        bool completed
    ) external {
        if (score < 1 || score > 5) revert InvalidScore();
        if (_evaluatedLocks[lockId]) revert LockAlreadyEvaluated();

        IPaymentEscrow.PaymentLock memory lock = paymentEscrow.getLock(lockId);
        if (lock.payer != msg.sender) revert NotPayer();
        if (lock.status != IPaymentEscrow.PaymentStatus.Released) revert PaymentNotReleased();

        FeedbackRecord memory newRecord = FeedbackRecord({
            score: score,
            lockId: lockId,
            completed: completed,
            reviewer: msg.sender
        });

        _records[lock.agentId].push(newRecord);
        _totalScore[lock.agentId] += score;
        _evaluatedLocks[lockId] = true;

        emit FeedbackAdded(lock.agentId, msg.sender, score, lockId, completed);
    }

    function getReputation(
        uint256 agentId
    ) external view returns (uint256 averageScore, uint256 totalReviews) {
        totalReviews = _records[agentId].length;
        if (totalReviews == 0) {
            return (0, 0);
        }
        averageScore = (_totalScore[agentId] * 100) / totalReviews;
        return (averageScore, totalReviews);
    }

    function getRecords(
        uint256 agentId,
        uint256 offset,
        uint256 limit
    ) external view returns (FeedbackRecord[] memory) {
        FeedbackRecord[] storage allRecords = _records[agentId];
        uint256 total = allRecords.length;

        if (offset >= total || limit == 0) {
            return new FeedbackRecord[](0);
        }

        uint256 size = limit;
        if (offset + limit > total) {
            size = total - offset;
        }

        FeedbackRecord[] memory result = new FeedbackRecord[](size);
        for (uint256 i = 0; i < size; i++) {
            result[i] = allRecords[offset + i];
        }
        return result;
    }

    function isLockEvaluated(bytes32 lockId) external view returns (bool) {
        return _evaluatedLocks[lockId];
    }
}
