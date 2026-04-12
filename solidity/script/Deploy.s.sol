// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script, console} from "forge-std/Script.sol";
import {SwiftwardAgentWallet} from "../src/SwiftwardAgentWallet.sol";

/// @notice Deploy SwiftwardAgentWallet to Sepolia.
/// Usage: make solidity-deploy AGENT=SIMPLE_LLM_GOLANG
///
/// The guardian is the deployer (--private-key address).
/// Each agent deploys with its own private key → its address becomes the guardian.
contract DeployScript is Script {
    function run() external {
        address guardian = msg.sender;

        vm.startBroadcast();
        SwiftwardAgentWallet wallet = new SwiftwardAgentWallet(guardian);
        vm.stopBroadcast();

        console.log("Deployed at: %s", address(wallet));
        console.log("Guardian:    %s", guardian);
        console.log("Add to .env: AGENT_<NAME>_WALLET_ADDR=%s", address(wallet));
    }
}
