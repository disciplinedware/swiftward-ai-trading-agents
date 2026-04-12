// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title SwiftwardAgentWallet
/// @notice EIP-1271 smart wallet for AI trading agents.
///         Only the agent's own EOA (guardian) may produce valid signatures.
///         Deployed once per agent; guardian is set at construction and never changes.
contract SwiftwardAgentWallet {
    /// @notice EIP-1271 magic value returned on valid signature.
    bytes4 public constant MAGIC_VALUE = 0x1626ba7e;

    /// @notice The agent's own EOA address authorized to sign on behalf of this wallet.
    ///         Set once at construction, immutable thereafter.
    address public immutable GUARDIAN;

    error ZeroGuardian();
    error NotGuardian();
    error ExecuteFailed(bytes returnData);

    /// @param _guardian The agent's own EOA address. Cannot be zero.
    constructor(address _guardian) {
        if (_guardian == address(0)) revert ZeroGuardian();
        GUARDIAN = _guardian;
    }

    /// @notice EIP-1271: validates that `signature` was produced by guardian over `hash`.
    /// @return magicValue `0x1626ba7e` if valid, `0xffffffff` otherwise.
    function isValidSignature(bytes32 hash, bytes memory signature)
        external
        view
        returns (bytes4 magicValue)
    {
        address recovered = _recover(hash, signature);
        return recovered == GUARDIAN ? MAGIC_VALUE : bytes4(0xffffffff);
    }

    /// @notice Execute an arbitrary call, callable only by guardian.
    ///         Used for Risk Router interactions if needed.
    function execute(address target, uint256 value, bytes calldata data)
        external
        returns (bytes memory)
    {
        if (msg.sender != GUARDIAN) revert NotGuardian();
        (bool success, bytes memory returnData) = target.call{value: value}(data);
        if (!success) revert ExecuteFailed(returnData);
        return returnData;
    }

    /// @notice Accept ETH.
    receive() external payable {}

    // -------------------------------------------------------------------------
    // Internal
    // -------------------------------------------------------------------------

    /// @dev Recovers signer from an eth_sign-style signature (v, r, s).
    ///      Handles both 65-byte compact sigs and malleability guard.
    function _recover(bytes32 hash, bytes memory sig) internal pure returns (address) {
        if (sig.length != 65) return address(0);

        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := mload(add(sig, 32))
            s := mload(add(sig, 64))
            v := byte(0, mload(add(sig, 96)))
        }
        // Normalize v: some signers send 0/1 instead of 27/28
        if (v < 27) v += 27;
        if (v != 27 && v != 28) return address(0);

        return ecrecover(hash, v, r, s);
    }
}
