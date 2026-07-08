// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

contract AgentIdentityRegistry is ERC721, Ownable {
    struct AgentMetadata {
        string modelId;
        string serviceEndpoint;
        bytes32 teeAttestation;
        string capabilities;
        uint256 registeredAt;
    }

    // 存储 agentId (即 tokenId) 到其元数据的映射
    mapping(uint256 => AgentMetadata) private _agentMetadata;
    
    // tokenId 计数器
    uint256 private _nextTokenId;

    event AgentRegistered(
        uint256 indexed agentId,
        address indexed owner,
        string modelId,
        string serviceEndpoint,
        bytes32 teeAttestation,
        string capabilities
    );

    event MetadataUpdated(
        uint256 indexed agentId,
        string modelId,
        string serviceEndpoint,
        bytes32 teeAttestation,
        string capabilities
    );

    error NotAgentOwner();
    error SoulboundTransferBlocked();
    error AgentDoesNotExist();
    error InvalidMetadata();

    constructor() ERC721("Agent Identity NFT", "AGENT") Ownable(msg.sender) {
        _nextTokenId = 1;
    }

    // 注册 Agent
    function registerAgent(
        string calldata modelId,
        string calldata serviceEndpoint,
        bytes32 teeAttestation,
        string calldata capabilities
    ) external returns (uint256) {
        if (bytes(modelId).length == 0 || bytes(serviceEndpoint).length == 0) {
            revert InvalidMetadata();
        }

        uint256 agentId = _nextTokenId++;
        _safeMint(msg.sender, agentId);

        _agentMetadata[agentId] = AgentMetadata({
            modelId: modelId,
            serviceEndpoint: serviceEndpoint,
            teeAttestation: teeAttestation,
            capabilities: capabilities,
            registeredAt: block.timestamp
        });

        emit AgentRegistered(
            agentId,
            msg.sender,
            modelId,
            serviceEndpoint,
            teeAttestation,
            capabilities
        );

        return agentId;
    }

    // 更新元数据
    function updateMetadata(
        uint256 agentId,
        string calldata modelId,
        string calldata serviceEndpoint,
        bytes32 teeAttestation,
        string calldata capabilities
    ) external {
        address agentOwner = _ownerOf(agentId);
        if (agentOwner == address(0)) {
            revert AgentDoesNotExist();
        }
        // 只有 NFT 拥有者可以修改
        if (agentOwner != msg.sender) {
            revert NotAgentOwner();
        }

        AgentMetadata storage metadata = _agentMetadata[agentId];
        metadata.modelId = modelId;
        metadata.serviceEndpoint = serviceEndpoint;
        metadata.teeAttestation = teeAttestation;
        metadata.capabilities = capabilities;

        emit MetadataUpdated(
            agentId,
            modelId,
            serviceEndpoint,
            teeAttestation,
            capabilities
        );
    }

    // 获取 Agent 元数据
    function getAgent(uint256 agentId) external view returns (
        string memory modelId,
        string memory serviceEndpoint,
        bytes32 teeAttestation,
        string memory capabilities,
        uint256 registeredAt,
        address owner
    ) {
        address agentOwner = _ownerOf(agentId);
        if (agentOwner == address(0)) {
            revert AgentDoesNotExist();
        }

        AgentMetadata memory metadata = _agentMetadata[agentId];
        return (
            metadata.modelId,
            metadata.serviceEndpoint,
            metadata.teeAttestation,
            metadata.capabilities,
            metadata.registeredAt,
            agentOwner
        );
    }

    // 验证 Agent 存在
    function verifyAgent(uint256 agentId) external view returns (bool) {
        address owner = _ownerOf(agentId);
        return owner != address(0);
    }

    // 允许拥有者销毁自己的 Agent Identity NFT
    function burn(uint256 agentId) external {
        address agentOwner = _ownerOf(agentId);
        if (agentOwner == address(0)) {
            revert AgentDoesNotExist();
        }
        if (agentOwner != msg.sender) {
            revert NotAgentOwner();
        }
        _burn(agentId);
    }

    // 重写 _update 函数以确保 Soulbound 属性 (禁止转让)
    function _update(
        address to,
        uint256 tokenId,
        address auth
    ) internal virtual override returns (address) {
        address previousOwner = _ownerOf(tokenId);
        
        // 如果 previousOwner != address(0) 且 to != address(0)，则是 transfer，需要禁止
        if (previousOwner != address(0) && to != address(0)) {
            revert SoulboundTransferBlocked();
        }
        
        return super._update(to, tokenId, auth);
    }
}
