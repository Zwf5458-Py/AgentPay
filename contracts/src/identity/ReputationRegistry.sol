// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IPaymentEscrow {
    function hasPaid(address payer, uint256 agentId) external view returns (bool);
}

contract ReputationRegistry {
    struct FeedbackRecord {
        uint8 score;
        bytes32 taskHash;
        bool taskCompleted;
        address reviewer;
    }

    IPaymentEscrow public immutable paymentEscrow;

    // agentId => feedback records
    mapping(uint256 => FeedbackRecord[]) private _records;
    // agentId => total score
    mapping(uint256 => uint256) private _totalScore;
    // taskHash => evaluated status
    mapping(bytes32 => bool) public evaluatedTasks;

    event FeedbackAdded(
        uint256 indexed agentId,
        address indexed reviewer,
        uint8 score,
        bytes32 indexed taskHash,
        bool taskCompleted
    );

    error InvalidScore();
    error TaskAlreadyEvaluated();
    error NotPaid();

    constructor(address escrowAddress) {
        paymentEscrow = IPaymentEscrow(escrowAddress);
    }

    function addFeedback(
        uint256 agentId,
        uint8 score,
        bytes32 taskHash,
        bool taskCompleted
    ) external {
        if (score < 1 || score > 5) revert InvalidScore();
        if (evaluatedTasks[taskHash]) revert TaskAlreadyEvaluated();
        if (!paymentEscrow.hasPaid(msg.sender, agentId)) revert NotPaid();

        FeedbackRecord memory newRecord = FeedbackRecord({
            score: score,
            taskHash: taskHash,
            taskCompleted: taskCompleted,
            reviewer: msg.sender
        });

        _records[agentId].push(newRecord);
        _totalScore[agentId] += score;
        evaluatedTasks[taskHash] = true;

        emit FeedbackAdded(agentId, msg.sender, score, taskHash, taskCompleted);
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

    function getRecords(uint256 agentId) external view returns (FeedbackRecord[] memory) {
        return _records[agentId];
    }
}
