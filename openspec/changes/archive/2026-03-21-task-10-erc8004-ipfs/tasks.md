## 1. Dependencies

- [x] 1.1 Add `web3>=6.0` and `aiohttp>=3.9` to pyproject.toml runtime deps and run `uv sync`

## 2. IPFS Provider

- [x] 2.1 Create `src/trading_mcp/erc8004/__init__.py` (empty)
- [x] 2.2 Create `src/trading_mcp/erc8004/ipfs.py` with `IpfsProvider` ABC, `MockIpfs`, and `PinataIpfs`

## 3. ABI Files

- [x] 3.1 Create `src/trading_mcp/erc8004/abis/identity_registry.json` with minimal `register` function ABI
- [x] 3.2 Create `src/trading_mcp/erc8004/abis/validation_registry.json` with `validationRequest` ABI
- [x] 3.3 Create `src/trading_mcp/erc8004/abis/reputation_registry.json` with `giveFeedback` ABI

## 4. Registry

- [x] 4.1 Create `src/trading_mcp/erc8004/registry.py` with `ERC8004Config` dataclass and `ERC8004Registry` class
- [x] 4.2 Implement `register_identity()` — DB check, IPFS upload, contract call, persist agentId
- [x] 4.3 Implement `submit_validation(position_id)` — load position, IPFS upload, contract call, update validation_uri
- [x] 4.4 Implement `submit_reputation(position_id)` — load closed position, score formula, contract call

## 5. Tests

- [x] 5.1 Write `tests/trading_mcp/test_erc8004_ipfs.py` covering MockIpfs round trip and PinataIpfs success/failure
- [x] 5.2 Write `tests/trading_mcp/test_erc8004_registry.py` covering identity skip-on-existing and reputation score formula

## 6. Verification

- [x] 6.1 Run `make lint` — zero ruff errors
- [x] 6.2 Run `make test` — all tests pass
