// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "../interfaces/IERC6551Account.sol";
import "@openzeppelin/contracts/token/ERC721/IERC721.sol";

contract AgentTokenBoundAccount is IERC6551Account {
    uint256 public immutable chainIdVal;
    address public immutable tokenContractVal;
    uint256 public immutable tokenIdVal;

    error NotOwner();
    error CallFailed();

    constructor(uint256 _chainId, address _tokenContract, uint256 _tokenId) {
        chainIdVal = _chainId;
        tokenContractVal = _tokenContract;
        tokenIdVal = _tokenId;
    }

    receive() external payable override {}

    function token()
        external
        view
        override
        returns (
            uint256,
            address,
            uint256
        )
    {
        return (chainIdVal, tokenContractVal, tokenIdVal);
    }

    function state() external pure override returns (uint256) {
        return 0;
    }

    function isValidSigner(address signer, bytes calldata)
        external
        view
        override
        returns (bytes4 magicValue)
    {
        if (signer == IERC721(tokenContractVal).ownerOf(tokenIdVal)) {
            return IERC6551Account.isValidSigner.selector;
        }
        return bytes4(0);
    }

    function execute(
        address to,
        uint256 value,
        bytes calldata data
    ) external payable returns (bytes memory) {
        if (msg.sender != IERC721(tokenContractVal).ownerOf(tokenIdVal)) {
            revert NotOwner();
        }

        (bool success, bytes memory result) = to.call{value: value}(data);
        if (!success) {
            revert CallFailed();
        }

        return result;
    }
}
