# Solidity — SwiftwardAgentWallet (ERC-1271)

On-chain smart contract wallet for AI Trading Agents Harness. One `SwiftwardAgentWallet` is deployed per agent and linked to the agent's ERC-8004 Identity via `IdentityRegistry.setAgentWallet()`.

## What it does

- Implements **EIP-1271** `isValidSignature(hash, signature)` so the rest of the ERC-8004 stack (Validation Registry, Reputation Registry) can verify agent signatures through a smart-contract wallet instead of a raw EOA.
- Holds an immutable `GUARDIAN` address — the agent's EOA. Only signatures produced by the guardian validate.
- Exposes `execute(to, value, data)` for on-chain actions (e.g. RiskRouter trade intent submission).

## Why a contract wallet

Decouples the agent's **signing key** (always a raw EOA) from **on-chain identity** (the `agentId` ERC-721 NFT). The wallet contract can be upgraded without changing the `agentId`, and leaderboards / validators only need to check `isValidSignature` rather than special-casing EOAs.

## Files

- `src/SwiftwardAgentWallet.sol` — the contract itself
- `script/Deploy.s.sol` — Foundry deployment script
- `test/SwiftwardAgentWallet.t.sol` — Foundry tests for `isValidSignature` and `execute`

## Build and test

```bash
forge build
forge test
forge script script/Deploy.s.sol --rpc-url $SEPOLIA_RPC_URL --broadcast
```

## Where it fits in the stack

Trade flow: agent signs `TradeIntent` with its EOA → RiskRouter calls `isValidSignature` on the `SwiftwardAgentWallet` → validates against the guardian → accepts. Same flow for `TradeCheckpoint` posted to the Validation Registry.

See [`docs/plans/completed/onchain-trading.md`](../docs/plans/completed/onchain-trading.md) for the full ERC-8004 integration story and [`docs/architecture/on-chain-transactions.md`](../docs/architecture/on-chain-transactions.md) for gas / nonce / replay protection details.

## Foundry reference

This subsystem uses Foundry (Forge, Cast, Anvil). For toolchain docs see https://book.getfoundry.sh/.
