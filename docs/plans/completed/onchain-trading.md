# ERC-8004 On-Chain Integration

> **Status**: ✅ Shipped (RiskRouter intentionally stubbed — see notes)
> **Chain**: Sepolia testnet (chainId 11155111)
> **Packages**: `golang/internal/chain/`, `golang/internal/mcps/trading/`, `solidity/src/SwiftwardAgentWallet.sol`, `golang/cmd/erc8004-setup/`

## What was built

Full ERC-8004 integration across Identity, Validation, and Reputation registries on Sepolia. Every agent is an ERC-721 NFT backed by an EIP-1271 `SwiftwardAgentWallet` smart contract. Trade intents are EIP-712 signed by the agent's EOA, trade checkpoints are posted automatically to the ValidationRegistry after each fill, and reputation feedback is posted to the ReputationRegistry. The evidence trail is queryable publicly via `GET /v1/evidence/{hash}`. The RiskRouter contract is wired but stubbed as a pass-through — Swiftward is the real enforcement layer.

## Deployed agents on Sepolia

| Agent | agentId | Operator wallet |
|-------|---------|-----------------|
| Swiftward Alpha | 32 | `0x6Cd7...5d13` |
| Random Trader | 37 | `0x7a2F...15e0` |
| Swiftward Gamma | 43 | `0xC5e0...bF62` |
| Haia Trading Agent | 49 | `0xFa7b...40B7` |

All agents are live on the hackathon leaderboard at `https://ai-trading.swiftward.dev/audit/`.

## Three ERC-8004 registries

### Identity Registry

Each agent is an ERC-721 NFT. Mint happens via `register(agentWallet, name, description, capabilities, agentURI)`. `agentURI` points to IPFS JSON metadata with the agent's name, description, capabilities, and AgentWallet contract address.

- `golang/internal/chain/identity.go` - `RegisterHackathon()` (lines 142-180), `IsAgentRegistered()` (224-242), `VerifyAgentOwnership()` (246-272), `SetAgentURI()` (276-301), `SetAgentWallet()` (per-agent wallet linking)
- `golang/cmd/erc8004-setup/main.go` - `runRegister()` (103-195) reads `erc8004/agents/{agent}.json`, mints the NFT, prints the agentId to store in `.env`

### Validation Registry

After every fill, the Trading MCP posts an EIP-712 signed attestation with the trade's checkpoint hash and evidence reference. The leaderboard uses these to compute `getAverageValidationScore`.

- `golang/internal/chain/identity.go:452-492` - `PostEIP712Attestation()` packs and submits the attestation
- `golang/internal/chain/signer.go:127-169` - `checkpointTypedData()` builds the EIP-712 struct with 10 fields (agentId, timestamp, action, asset, pair, amountUsdScaled, priceUsdScaled, reasoningHash, confidenceScaled, intentHash)
- `golang/internal/mcps/trading/service.go:578` - attestation call site, fire-and-forget per-agent

**Attestation lifecycle** (`service.go:94-96`, `:109-117`, `:2539-2543`):
`pending → waiting_for_gas → pending_confirm → success` (or `error` / `disabled`). A per-agent circuit breaker disables attestation for the process lifetime after repeated failures so one bad credential does not hammer the chain. On restart a recovery loop re-posts pending attestations.

### Reputation Registry

Validators post six numeric metrics via `giveFeedback()`:

- `perf/sharpe`, `perf/return_pct`, `perf/win_rate`
- `risk/max_drawdown_pct`
- `trust/compliance_pct`, `trust/guardrail_saves`

Files:

- `golang/internal/chain/reputation.go:82-124` - `GiveFeedback()` wrapper with `tag1` (category), `tag2` (metric), `value`, `valueDecimals`, `endpoint`, `feedbackURI`, `feedbackHash`
- `golang/cmd/erc8004-setup/main.go:runBooster` (lines 397-498) - manual CLI that posts feedback from a validator wallet (self-feedback is rejected by the contract)

## EIP-712 signing

Three signed structures are used on-chain:

- **TradeIntentData** - submitted to the RiskRouter. Domain `"RiskRouter"`. Signed by the agent's EOA. Fields: `agentId`, `agentWallet`, `pair`, `action`, `amountUsdScaled`, `maxSlippageBps`, `nonce`, `deadline`. `golang/internal/chain/signer.go:85-109` (`SignTradeIntent`), `:43-81` (typed data).
- **TradeCheckpointData** - posted as validation attestation. Domain `"AITradingAgent"`, verifyingContract = Identity Registry. Signed by agent EOA. `signer.go:173-200` (`SignCheckpoint`), `:127-169` (typed data).
- **SetAgentWallet** - one-time linking of the ERC-1271 wallet to the agentId. Domain `"ERC8004IdentityRegistry"`. `signer.go:284-308`.

All signatures use 65-byte `v`-normalized form.

## AgentWallet (ERC-1271)

Per-agent smart contract wallet. `solidity/src/SwiftwardAgentWallet.sol`:

- Immutable `GUARDIAN` = agent EOA
- `isValidSignature(hash, signature)` returns the ERC-1271 magic value `0x1626ba7e` when the signature is produced by the guardian
- `execute(to, value, data)` for on-chain actions (e.g. RiskRouter calls)
- Linked to the agentId via `IdentityRegistry.setAgentWallet()` using an EIP-712 authorization from the guardian

This decouples the agent's signing key from on-chain identity: the wallet contract can be upgraded without changing the agentId.

## Integration with the trade flow

`golang/internal/mcps/trading/service.go` (lines 2089-2245):

1. **Swiftward policy gate** (`:2089-2091`) - fail-closed, retried on transient errors
2. **RiskRouter gate** (`:2152-2245`) - if configured, sign TradeIntent, read on-chain nonce, submit, wait for `TradeApproved` / `TradeRejected` event
3. **Exchange execution** - Kraken CLI (off-chain)
4. **Attestation posting** (`:578`, `:3948`) - fire-and-forget EIP-712 checkpoint to ValidationRegistry, with the per-agent circuit breaker

`GET /v1/evidence/{hash}` (`service.go:815`) serves the full decision trace JSON publicly.

## RiskRouter

`golang/internal/chain/risk_router.go`:

- `SubmitTrade()` (lines 169-281) - sign intent, submit, parse events
- `GetIntentNonce()` (lines 128-159) - read on-chain nonce for replay protection
- Minimal ABI: `submitTradeIntent`, `getIntentNonce`, events `TradeApproved`, `TradeRejected`

**Why stubbed**: the hackathon organisers were to provide finalised RiskRouter contract addresses and risk parameters. They never arrived. The stub passes through approvals so agents can submit on-chain intents, generate evidence, and accumulate validation scores. The real policy enforcement layer is Swiftward, which ran to production quality throughout the hackathon.

## Setup tooling

- `golang/cmd/erc8004-setup/main.go` - one-time CLI with subcommands: `register`, `set-uri`, `set-wallet`, `claim-vault`, `booster`
- `golang/cmd/gen-wallets/main.go` - generate fresh Ethereum keypairs (private key + address) for agents or validators
- `solidity/script/Deploy.s.sol` - AgentWallet deployment
- Makefile targets: `make erc8004-setup`, `make erc8004-validate`, `make erc8004-feedback`

## Key files

**Chain interaction** (`golang/internal/chain/`):
- `client.go` - `SendTx()` with EIP-1559 fees, gas bumping (10s interval, 1.2x multiplier, 3 max attempts), receipt polling
- `signer.go` - EIP-712 for TradeIntent, TradeCheckpoint, SetAgentWallet
- `identity.go` - IdentityRegistry + ValidationRegistry wrappers
- `risk_router.go` - RiskRouter stub with real EIP-712 signing and event parsing
- `reputation.go` - ReputationRegistry `giveFeedback()` wrapper

**Trading MCP** (`golang/internal/mcps/trading/service.go`):
- `:2089-2150` - Swiftward gate + RiskRouter gate (fail-closed on RPC errors)
- `:578`, `:3948` - attestation posting
- `:94-96`, `:109-117`, `:207-274`, `:2539-2543` - circuit breaker state machine

**Contracts & setup**:
- `solidity/src/SwiftwardAgentWallet.sol` - ERC-1271 wallet contract
- `solidity/test/SwiftwardAgentWallet.t.sol` - Foundry tests
- `golang/cmd/erc8004-setup/main.go` - registration + admin CLI

## Tests

- `golang/internal/chain/client_test.go` - EIP-1559 fee bumping (`TestBumpFee`, `TestBumpFeeDoesNotMutateInput`, `TestBumpFeeConsecutiveBumpsExceedGethMinimum`)
- `golang/internal/chain/signer_test.go` - signature generation and recovery
- `solidity/test/SwiftwardAgentWallet.t.sol` - ERC-1271 signature validation in Foundry

## Notes

- **Sepolia, not mainnet** - free gas, fast finality, shared faucets. Mainnet would add cost and risk without any benefit for a hackathon.
- **RiskRouter stub is a feature, not a bug** - Swiftward is the real policy engine. The stub keeps the on-chain flow complete so evidence and reputation work end to end.
- **Fire-and-forget attestation** - a failed attestation never blocks a fill. The trade is already executed; attestation is evidence.
- **Per-agent circuit breaker** - one bad key or out-of-gas situation disables that agent's attestation for the process lifetime. A restart with a recovery loop resubmits any pending attestations.
- **EIP-1559 fee bumping** - 1.2x multiplier with a 1 gwei floor for zero-tip markets; three bumps is enough to cross the geth 10% minimum.
- **Public evidence API** - `/v1/evidence/{hash}` is unauthenticated. Any observer can audit any agent's decision chain without trusting the backend.
