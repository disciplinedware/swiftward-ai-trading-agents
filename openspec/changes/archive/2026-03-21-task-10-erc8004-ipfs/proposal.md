## Why

The trading agent needs to anchor all trade reasoning on-chain via ERC-8004 to satisfy the hackathon's trust and validation scoring criteria. Without IPFS upload and registry hooks, the agent accumulates no on-chain evidence — no identity, no reasoning trace validation, no reputation score — which disqualifies it from the Compliance & Risk, Validation & Trust, and Trustless Trading tracks.

## What Changes

- New `src/trading_mcp/erc8004/` package with IPFS provider abstraction and registry hooks
- `IpfsProvider` ABC with `MockIpfs` (temp files) and `PinataIpfs` (real upload) implementations
- `ERC8004Registry` class with three hooks: `register_identity`, `submit_validation`, `submit_reputation`
- Minimal ABI JSON files for the three ERC-8004 registries
- All registry calls are fire-and-forget (non-blocking) to avoid delaying trade confirmation
- `web3>=6.0` and `aiohttp>=3.9` added as runtime dependencies

## Capabilities

### New Capabilities

- `erc8004-ipfs`: IPFS provider abstraction — upload JSON data, return URI; mock and Pinata implementations
- `erc8004-registry`: ERC-8004 registry hooks — identity registration, validation submission, reputation feedback

### Modified Capabilities

## Impact

- `pyproject.toml`: add `web3>=6.0`, `aiohttp>=3.9`
- New package `src/trading_mcp/erc8004/` (no changes to existing modules)
- `src/trading_mcp/domain/entity/agent.py` — used by `register_identity` to check/persist agentId (read-only from existing schema)
- `src/trading_mcp/domain/entity/position.py` — `validation_uri` field updated by `submit_validation`
- Tests: no existing tests affected; two new test files added
