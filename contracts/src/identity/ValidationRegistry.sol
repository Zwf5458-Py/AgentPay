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

    error ValidatorNotContract();

    constructor() Ownable(msg.sender) {}

    // 注册/修改验证器地址，只有 Owner 可操作
    function registerValidator(
        string calldata validationType,
        address validatorAddress
    ) external onlyOwner {
        if (validatorAddress != address(0) && validatorAddress.code.length == 0) {
            revert ValidatorNotContract();
        }
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
        
        // 如果没有注册对应的验证器，直接返回 false
        if (validator == address(0)) {
            return false;
        }

        // 调用对应验证器合约进行验证
        try IValidator(validator).validate(agentId, proof) returns (bool result) {
            return result;
        } catch {
            return false;
        }
    }
}
