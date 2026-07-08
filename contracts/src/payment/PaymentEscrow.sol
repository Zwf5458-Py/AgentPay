// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import "../interfaces/IERC6551Registry.sol";

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

    struct ChannelLock {
        address payer;
        uint256 agentId;
        uint256 maxAmount;
        uint256 settledAmount;
        uint256 expiresAt;
        PaymentStatus status;
    }

    IERC20 public immutable paymentToken;
    IValidationRegistry public immutable validationRegistry;
    address public immutable settler;
    address public erc6551Registry;
    address public tbaImplementation;
    address public agentIdentityRegistry;

    uint256 private _nonce;
    mapping(bytes32 => PaymentLock) private _locks;
    mapping(bytes32 => ChannelLock) public channels;

    bytes32 public constant CHANNEL_SETTLE_TYPEHASH = keccak256("ChannelSettle(bytes32 channelId,uint256 accumulatedAmount)");
    bytes32 public immutable DOMAIN_SEPARATOR;

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

    event ChannelLocked(
        bytes32 indexed channelId,
        address indexed payer,
        uint256 indexed agentId,
        uint256 maxAmount,
        uint256 expiresAt
    );

    event ChannelSettled(
        bytes32 indexed channelId,
        address indexed agentOwner,
        uint256 settledAmount
    );

    event ChannelRefunded(
        bytes32 indexed channelId,
        address indexed payer,
        uint256 refundedAmount
    );

    error InvalidAmount();
    error NotSettler();
    error InvalidStatus();
    error LockExpired();
    error LockNotExpired();
    error ProofValidationFailed();
    error InvalidAddress();
    error InvalidDuration();
    error InvalidSignature();
    error ChannelExpired();
    error ChannelNotExpired();

    modifier onlySettler() {
        if (msg.sender != settler) revert NotSettler();
        _;
    }

    constructor(
        address _paymentToken,
        address _validationRegistry,
        address _settler,
        address _erc6551Registry,
        address _tbaImplementation,
        address _agentIdentityRegistry
    ) {
        if (
            _paymentToken == address(0) ||
            _validationRegistry == address(0) ||
            _settler == address(0) ||
            _erc6551Registry == address(0) ||
            _tbaImplementation == address(0) ||
            _agentIdentityRegistry == address(0)
        ) {
            revert InvalidAddress();
        }
        paymentToken = IERC20(_paymentToken);
        validationRegistry = IValidationRegistry(_validationRegistry);
        settler = _settler;
        erc6551Registry = _erc6551Registry;
        tbaImplementation = _tbaImplementation;
        agentIdentityRegistry = _agentIdentityRegistry;

        DOMAIN_SEPARATOR = keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256(bytes("PaymentEscrow")),
                keccak256(bytes("1")),
                block.chainid,
                address(this)
            )
        );
    }

    function lockPayment(
        uint256 agentId,
        uint256 amount,
        bytes32 requestHash,
        uint256 duration
    ) external returns (bytes32 lockId) {
        if (amount == 0) revert InvalidAmount();
        if (duration == 0) revert InvalidDuration();

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
        if (agentOwner == address(0)) revert InvalidAddress();

        PaymentLock storage lock = _locks[lockId];
        if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
        if (block.timestamp > lock.expiresAt) revert LockExpired();

        // 专属 TBA 校验防御
        address expectedTba = IERC6551Registry(erc6551Registry).account(
            tbaImplementation,
            bytes32(0),
            block.chainid,
            agentIdentityRegistry,
            lock.agentId
        );
        if (agentOwner != expectedTba) revert InvalidAddress();

        bool isValid = validationRegistry.validateProof(lock.agentId, "TEE", proof);
        if (!isValid) revert ProofValidationFailed();

        lock.status = PaymentStatus.Released;

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

    function lockChannel(
        uint256 agentId,
        uint256 amount,
        uint256 duration
    ) external returns (bytes32 channelId) {
        if (amount == 0) revert InvalidAmount();
        if (duration == 0) revert InvalidDuration();

        paymentToken.safeTransferFrom(msg.sender, address(this), amount);

        channelId = keccak256(abi.encodePacked(
            msg.sender,
            agentId,
            amount,
            block.timestamp,
            _nonce++
        ));

        uint256 expiresAt = block.timestamp + duration;

        channels[channelId] = ChannelLock({
            payer: msg.sender,
            agentId: agentId,
            maxAmount: amount,
            settledAmount: 0,
            expiresAt: expiresAt,
            status: PaymentStatus.Locked
        });

        emit ChannelLocked(channelId, msg.sender, agentId, amount, expiresAt);
    }

    function batchSettle(
        bytes32 channelId,
        uint256 accumulatedAmount,
        bytes calldata signature,
        address agentOwner
    ) external onlySettler {
        if (agentOwner == address(0)) revert InvalidAddress();

        ChannelLock storage lock = channels[channelId];
        if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
        if (block.timestamp > lock.expiresAt) revert ChannelExpired();
        if (accumulatedAmount == 0 || accumulatedAmount > lock.maxAmount) revert InvalidAmount();

        // 专属 TBA 校验防御
        address expectedTba = IERC6551Registry(erc6551Registry).account(
            tbaImplementation,
            bytes32(0),
            block.chainid,
            agentIdentityRegistry,
            lock.agentId
        );
        if (agentOwner != expectedTba) revert InvalidAddress();

        bytes32 hashStruct = keccak256(abi.encode(
            CHANNEL_SETTLE_TYPEHASH,
            channelId,
            accumulatedAmount
        ));
        bytes32 digest = keccak256(abi.encodePacked(
            "\x19\x01",
            DOMAIN_SEPARATOR,
            hashStruct
        ));
        address signer = ECDSA.recover(digest, signature);
        if (signer != lock.payer) revert InvalidSignature();

        lock.status = PaymentStatus.Released;
        lock.settledAmount = accumulatedAmount;

        paymentToken.safeTransfer(agentOwner, accumulatedAmount);

        uint256 remainder = lock.maxAmount - accumulatedAmount;
        if (remainder > 0) {
            paymentToken.safeTransfer(lock.payer, remainder);
        }

        emit ChannelSettled(channelId, agentOwner, accumulatedAmount);
    }

    function refundChannel(bytes32 channelId) external {
        ChannelLock storage lock = channels[channelId];
        if (lock.status != PaymentStatus.Locked) revert InvalidStatus();
        if (block.timestamp <= lock.expiresAt) revert ChannelNotExpired();

        lock.status = PaymentStatus.Refunded;

        paymentToken.safeTransfer(lock.payer, lock.maxAmount);

        emit ChannelRefunded(channelId, lock.payer, lock.maxAmount);
    }

    function getLock(bytes32 lockId) external view returns (PaymentLock memory) {
        return _locks[lockId];
    }
}
