// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

interface IValidationRegistry {
    function validateProof(
        uint256 agentId,
        string calldata validationType,
        bytes calldata proof
    ) external view returns (bool);
}

contract PaymentEscrow {
    using SafeERC20 for IERC20;

    enum PaymentStatus { None, Locked, Released, Refunded }

    struct PaymentLock {
        address payer;
        uint256 agentId;
        uint256 amount;
        bytes32 requestHash;
        uint256 expiresAt;
        PaymentStatus status;
    }

    IERC20 public immutable paymentToken;
    IValidationRegistry public immutable validationRegistry;
    address public immutable settler;

    uint256 private _nonce;
    mapping(bytes32 => PaymentLock) private _locks;
    mapping(address => mapping(uint256 => bool)) private _hasPaid;

    event PaymentLocked(
        bytes32 indexed lockId,
        address indexed payer,
        uint256 indexed agentId,
        uint256 amount,
        bytes32 requestHash,
        uint256 expiresAt
    );

    event PaymentReleased(bytes32 indexed lockId, address indexed agentOwner);
    event PaymentRefunded(bytes32 indexed lockId, address indexed payer);

    error InvalidAmount();
    error NotSettler();
    error InvalidStatus();
    error LockExpired();
    error LockNotExpired();
    error ProofValidationFailed();

    modifier onlySettler() {
        if (msg.sender != settler) revert NotSettler();
        _;
    }

    constructor(address _paymentToken, address _validationRegistry, address _settler) {
        paymentToken = IERC20(_paymentToken);
        validationRegistry = IValidationRegistry(_validationRegistry);
        settler = _settler;
    }

    function lockPayment(
        uint256 agentId,
        uint256 amount,
        bytes32 requestHash,
        uint256 duration
    ) external returns (bytes32 lockId) {
        if (amount == 0) revert InvalidAmount();

        paymentToken.safeTransferFrom(msg.sender, address(this), amount);

        lockId = keccak256(abi.encodePacked(
            msg.sender,
            agentId,
            amount,
            requestHash,
            block.timestamp,
            _nonce++
        ));

        uint256 expiresAt = block.timestamp + duration;

        _locks[lockId] = PaymentLock({
            payer: msg.sender,
            agentId: agentId,
            amount: amount,
            requestHash: requestHash,
            expiresAt: expiresAt,
            status: PaymentStatus.Locked
        });

        emit PaymentLocked(lockId, msg.sender, agentId, amount, requestHash, expiresAt);
    }

    function releasePayment(
        bytes32 lockId,
        bytes calldata proof,
        address agentOwner
    ) external onlySettler {
        PaymentLock storage lock = _locks[lockId];
        if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
        if (block.timestamp > lock.expiresAt) revert LockExpired();

        bool isValid = validationRegistry.validateProof(lock.agentId, "TEE", proof);
        if (!isValid) revert ProofValidationFailed();

        lock.status = PaymentStatus.Released;
        _hasPaid[lock.payer][lock.agentId] = true;

        paymentToken.safeTransfer(agentOwner, lock.amount);

        emit PaymentReleased(lockId, agentOwner);
    }

    function refund(bytes32 lockId) external {
        PaymentLock storage lock = _locks[lockId];
        if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
        if (block.timestamp <= lock.expiresAt) revert LockNotExpired();

        lock.status = PaymentStatus.Refunded;

        paymentToken.safeTransfer(lock.payer, lock.amount);

        emit PaymentRefunded(lockId, lock.payer);
    }

    function hasPaid(address payer, uint256 agentId) external view returns (bool) {
        return _hasPaid[payer][agentId];
    }

    function getLock(bytes32 lockId) external view returns (PaymentLock memory) {
        return _locks[lockId];
    }
}
