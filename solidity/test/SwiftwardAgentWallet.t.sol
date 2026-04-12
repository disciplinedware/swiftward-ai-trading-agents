// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Test} from "forge-std/Test.sol";
import {SwiftwardAgentWallet} from "../src/SwiftwardAgentWallet.sol";

contract SwiftwardAgentWalletTest is Test {
    // EIP-1271 magic value
    bytes4 constant MAGIC_VALUE = 0x1626ba7e;
    bytes4 constant REJECT_VALUE = 0xffffffff;

    // Foundry cheat: well-known private key → deterministic address
    uint256 constant GUARDIAN_PK = 0xA11CE;
    uint256 constant STRANGER_PK = 0xB0B;

    address guardian;
    address stranger;
    SwiftwardAgentWallet wallet;

    function setUp() public {
        guardian = vm.addr(GUARDIAN_PK);
        stranger = vm.addr(STRANGER_PK);
        wallet = new SwiftwardAgentWallet(guardian);
    }

    // -------------------------------------------------------------------------
    // testGuardianIsSet
    // -------------------------------------------------------------------------
    function testGuardianIsSet() public view {
        assertEq(wallet.GUARDIAN(), guardian);
    }

    // -------------------------------------------------------------------------
    // testValidSignatureFromGuardian
    // -------------------------------------------------------------------------
    function testValidSignatureFromGuardian() public view {
        bytes32 hash = keccak256("test payload");
        bytes memory sig = _sign(GUARDIAN_PK, hash);
        bytes4 result = wallet.isValidSignature(hash, sig);
        assertEq(result, MAGIC_VALUE, "guardian sig must return MAGIC_VALUE");
    }

    // -------------------------------------------------------------------------
    // testRejectSignatureFromStranger
    // -------------------------------------------------------------------------
    function testRejectSignatureFromStranger() public view {
        bytes32 hash = keccak256("test payload");
        bytes memory sig = _sign(STRANGER_PK, hash);
        bytes4 result = wallet.isValidSignature(hash, sig);
        assertEq(result, REJECT_VALUE, "stranger sig must return 0xffffffff");
    }

    // -------------------------------------------------------------------------
    // testExecuteOnlyByGuardian
    // -------------------------------------------------------------------------
    function testExecuteOnlyByGuardian() public {
        // Use an EOA as target — EVM calls to EOAs with empty data always succeed.
        address eoa = vm.addr(0xDEAD);

        // Guardian can call execute
        vm.prank(guardian);
        wallet.execute(eoa, 0, "");

        // Stranger must revert with NotGuardian
        vm.prank(stranger);
        vm.expectRevert(SwiftwardAgentWallet.NotGuardian.selector);
        wallet.execute(eoa, 0, "");
    }

    // -------------------------------------------------------------------------
    // testCanReceiveETH
    // -------------------------------------------------------------------------
    function testCanReceiveETH() public {
        vm.deal(address(this), 1 ether);
        (bool ok,) = address(wallet).call{value: 0.5 ether}("");
        assertTrue(ok, "wallet must accept ETH");
        assertEq(address(wallet).balance, 0.5 ether);
    }

    // -------------------------------------------------------------------------
    // testCannotDeployWithZeroGuardian
    // -------------------------------------------------------------------------
    function testCannotDeployWithZeroGuardian() public {
        vm.expectRevert(SwiftwardAgentWallet.ZeroGuardian.selector);
        new SwiftwardAgentWallet(address(0));
    }

    // -------------------------------------------------------------------------
    // testSignatureNotReplayableAcrossHashes
    // A guardian signature on hashA MUST NOT validate for a different hashB.
    // -------------------------------------------------------------------------
    function testSignatureNotReplayableAcrossHashes() public view {
        bytes32 hashA = keccak256("trade A");
        bytes32 hashB = keccak256("trade B");
        bytes memory sigA = _sign(GUARDIAN_PK, hashA);
        // Valid for hashA
        assertEq(wallet.isValidSignature(hashA, sigA), MAGIC_VALUE, "sigA must be valid for hashA");
        // Must NOT be valid for hashB
        assertEq(wallet.isValidSignature(hashB, sigA), REJECT_VALUE, "sigA must not validate hashB");
    }

    // -------------------------------------------------------------------------
    // Bonus: wrong-length signature returns 0xffffffff (not revert)
    // -------------------------------------------------------------------------
    function testShortSignatureReturnsFalse() public view {
        bytes32 hash = keccak256("short sig test");
        bytes memory badSig = new bytes(10);
        bytes4 result = wallet.isValidSignature(hash, badSig);
        assertEq(result, REJECT_VALUE, "malformed sig must return 0xffffffff");
    }

    // -------------------------------------------------------------------------
    // Internal helpers
    // -------------------------------------------------------------------------

    /// @dev Signs `hash` with `pk` using vm.sign, packs into 65-byte sig.
    function _sign(uint256 pk, bytes32 hash) internal pure returns (bytes memory) {
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(pk, hash);
        return abi.encodePacked(r, s, v);
    }
}
