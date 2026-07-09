// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../test/MockERC20.sol";
import "../src/identity/ValidationRegistry.sol";
import "../src/identity/ReputationRegistry.sol";
import "../src/identity/AgentIdentityRegistry.sol";
import "../src/payment/AgentTokenBoundAccount.sol";
import "../src/payment/PaymentEscrow.sol";

// Import the Mock ERC6551 Registry and Validator from tests
import "../test/PaymentEscrowTest.t.sol";

contract DeployScript is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("FOUNDRY_PRIVATE_KEY");
        address settler = vm.envAddress("GATEWAY_SETTLER_ADDRESS");

        vm.startBroadcast(deployerPrivateKey);

        // 1. Deploy Mock USDC
        MockERC20 usdc = new MockERC20("USDC Mock", "USDC");
        console.log("MockERC20 (USDC) deployed at:", address(usdc));

        // 2. Deploy Validation Registry
        ValidationRegistry validationRegistry = new ValidationRegistry();
        console.log("ValidationRegistry deployed at:", address(validationRegistry));

        // 3. Deploy Mock Validator and register
        MockValidator validator = new MockValidator(true);
        validationRegistry.registerValidator("TEE", address(validator));
        console.log("MockValidator deployed at:", address(validator));

        // 4. Deploy Agent Identity Registry
        AgentIdentityRegistry agentIdentityRegistry = new AgentIdentityRegistry();
        console.log("AgentIdentityRegistry deployed at:", address(agentIdentityRegistry));

        // 5. Deploy TBA Implementation & Registry
        AgentTokenBoundAccount tbaImpl = new AgentTokenBoundAccount(block.chainid, address(0), 0);
        console.log("AgentTokenBoundAccount Impl deployed at:", address(tbaImpl));

        MockERC6551Registry erc6551Registry = new MockERC6551Registry();
        console.log("MockERC6551Registry deployed at:", address(erc6551Registry));

        // 6. Deploy Payment Escrow
        PaymentEscrow escrow = new PaymentEscrow(
            address(usdc),
            address(validationRegistry),
            settler,
            address(erc6551Registry),
            address(tbaImpl),
            address(agentIdentityRegistry)
        );
        console.log("PaymentEscrow deployed at:", address(escrow));

        // 7. Deploy Reputation Registry
        ReputationRegistry reputation = new ReputationRegistry(address(escrow));
        console.log("ReputationRegistry deployed at:", address(reputation));

        // Mint some test tokens to deployer for testing
        usdc.mint(vm.addr(deployerPrivateKey), 1000000 * 10**6);

        vm.stopBroadcast();
    }
}
