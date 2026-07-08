// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";

interface IValidator {
    function validate(uint256 agentId, bytes calldata proof) external view returns (bool);
}

contract ValidationRegistry is Ownable {
    // 映射验证类型（如 "TEE"）到其验证器合约地址
    mapping(string => address) private _validators;

    event ValidatorRegistered(string indexed validationType, address indexed validatorAddress);

    constructor() Ownable(msg.sender) {}

    // 注册/修改验证器地址，只有 Owner 可操作
    function registerValidator(
        string calldata validationType,
        address validatorAddress
    ) external onlyOwner {
        _validators[validationType] = validatorAddress;
        emit ValidatorRegistered(validationType, validatorAddress);
    }

    // 获取验证器地址
    function getValidator(string calldata validationType) external view returns (address) {
        return _validators[validationType];
    }

    // 校验证明
    function validateProof(
        uint256 agentId,
        string calldata validationType,
        bytes calldata proof
    ) external view returns (bool) {
        address validator = _validators[validationType];
        
        // 如果没有注册对应的验证器，默认放行返回 true（Mock 验证）
        if (validator == address(0)) {
            return true;
        }

        // 调用对应验证器合约进行验证
        try IValidator(validator).validate(agentId, proof) returns (bool success) {
            return success;
        } catch {
            return false;
        }
    }
}
