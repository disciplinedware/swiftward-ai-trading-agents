## Context

The trading-mcp server needs to call three ERC-8004 registries on Sepolia:
- **Identity Registry** (`0x8004A169...`): register agent once on first startup
- **Validation Registry** (`0x...`): record reasoning trace hash on every trade open
- **Reputation Registry** (`0x8004BAa1...`): report PnL score on every trade close

All calls go through web3.py. IPFS is needed to store JSON blobs before registering their URI + hash on-chain. Pinata is the chosen provider (free tier, reliable API). A mock provider allows all of this to be tested without a live chain or real IPFS node.

Current state: schema, portfolio service, and paper engine exist. No ERC-8004 code yet.

## Goals / Non-Goals

**Goals:**
- IPFS provider abstraction with two implementations: `MockIpfs` and `PinataIpfs`
- Three registry hook methods on `ERC8004Registry`: `register_identity`, `submit_validation`, `submit_reputation`
- All registry calls are non-blocking (fire-and-forget via `asyncio.create_task`)
- Identity registration is idempotent — if `agents` table has a row, skip
- Reputation score formula: `clamp(int((pnl_pct + 1.0) * 50), 0, 100)`
- Tested without a live chain using `AsyncMock` for web3 contracts

**Non-Goals:**
- Wiring the registry into the FastAPI server entrypoint (Task 11)
- Live engine / real trade submission (Task 11)
- EIP-712 signed trade intents (separate concern)

## Decisions

**1. IpfsProvider as ABC, not protocol**
Using `abc.ABC` + `@abstractmethod` over `typing.Protocol` for explicit subclassing. Tests subclass `MockIpfs` directly; Pinata swapped in via config.

**2. web3.py AsyncWeb3 with AsyncHTTPProvider**
`web3>=6.0` ships `AsyncWeb3` natively. The registry calls are I/O-bound (HTTP RPC), so async is natural. Avoids spawning threads for every registry call.

**3. Minimal ABI JSON files**
Only the function signatures we actually call, not the full contract ABI. Keeps the files tiny and avoids pulling in a code-gen tool. Three files:
- `abis/identity_registry.json` — `register(string uri, bytes32 hash)`
- `abis/validation_registry.json` — `validationRequest(address validator, uint256 agentId, string uri, bytes32 hash)`
- `abis/reputation_registry.json` — `giveFeedback(uint256 agentId, uint8 score, string[] tags, string validationUri)`

**4. Fire-and-forget via `asyncio.create_task`**
Registry calls must not delay trade confirmation. Wrapping in `create_task` lets the caller return immediately. Errors are logged but do not propagate.

**5. `ERC8004Config` dataclass**
Pulls chain config from the existing `config.yaml` structure under `chain:`. Fields: `rpc_url`, `identity_registry_address`, `reputation_registry_address`, `validation_registry_address`, `ipfs_provider` (`pinata|mock`), `pinata_jwt`. Validated at construction.

**6. `register_identity` idempotency via DB check**
Query `agents` table for any row before calling the registry. This is simpler and cheaper than catching on-chain errors or checking tx receipts.

## Risks / Trade-offs

- **Registry addresses not yet known** (validation registry TBD from hackathon): placeholder `"0x..."` in config example; mock tests don't need real addresses. → Mitigation: fill in on Day 1; mock used in tests.
- **`asyncio.create_task` in tests**: tasks created inside a unit test may not complete before the test ends, causing spurious warnings. → Mitigation: tests that check registry calls use `await asyncio.sleep(0)` to flush the event loop.
- **Pinata free tier rate limit** (100 req/day on free): one upload per trade open + one per trade close. For a hackathon with ~10 trades/day this is fine. → Mitigation: none needed at this scale.

## Migration Plan

1. `uv add web3 aiohttp` (adds to pyproject.toml)
2. New package `src/trading_mcp/erc8004/` — no changes to existing code
3. Task 11 wires `ERC8004Registry` into the FastAPI lifespan and calls hooks after paper/live execution
