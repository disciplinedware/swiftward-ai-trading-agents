# submit_order Pipeline

`toolSubmitOrder` in `golang/internal/mcps/trading/service.go:979`

## Pipeline steps

All validation steps run sequentially. Cheapest checks first, on-chain last.
Exchange fill only happens after ALL validations pass.

| Step | What | Blocking | Reversible | Typical latency |
|------|------|----------|------------|-----------------|
| 1 | **Balance/position checks** (cash, position exists, clamp) | sync | - | ~1ms |
| 2 | **Swiftward policy eval** (gRPC to swiftward-server) | sync | - | ~50ms |
| 3 | **RiskRouter on-chain validation** (submitTradeIntent) | sync | **NO** (nonce incremented) | **10-30s** |
| 4 | **Exchange fill** (Kraken paper/live) | sync | **NO** | ~500ms |
| 5 | Compute PnL, equity, peak | sync | - | ~1ms |
| 6 | Add tx_hash to result | sync | - | ~0ms |
| 7 | Evidence trace (hash chain, DB insert) | sync | - | ~5ms |
| 8 | **DB persist** (trade record + agent state, atomic) | sync | - | ~5ms |
| 9 | Auto-create SL/TP conditional orders | sync | - | ~10ms |
| 10 | EIP-712 attestation to ValidationRegistry | **async** (goroutine) | - | 10-30s |
| 11 | Emit execution_report to Swiftward | **async** | - | ~50ms |

### Design principles

1. **Cheapest validations first**: balance check (~1ms) before policy eval (~50ms) before RiskRouter (~10-30s). No point burning gas if the agent has no cash.
2. **All validations before exchange fill**: RiskRouter is a validation gate. If it rejects, no exchange trade happens.
3. **DB persist right after fill**: no slow operations (blockchain) between exchange fill and DB persist. Prevents ledger drift.
4. **Async for non-critical**: attestation and execution_report use `context.Background()` goroutines - they don't block the response.

### Fail-open behavior

- Swiftward policy error: log warning, proceed (step 2)
- RiskRouter network error: log error, proceed without on-chain record (step 3)
- Evidence trace error: log error, proceed without hash chain (step 7)
- DB persist error after fill: log error, return success with `persist_error` field (step 8)
- Attestation error: log error in goroutine (step 10)

### RiskRouter rejection vs error

- **Rejection** (`!submission.Success`): trade is rejected, return early. RiskRouter emitted `TradeRejected` event on-chain, nonce was NOT incremented (only approved trades increment nonce per contract).
- **Network/tx error**: fail-open, proceed to exchange without on-chain record. Trade will have no tx_hash.

### What if exchange fails after RiskRouter approved?

RiskRouter already incremented the nonce and emitted `TradeApproved` on-chain. This is a nonce gap but no harm - the trade intent was approved, just not executed. No checkpoint/attestation is posted (matching hackathon template behavior).

## Timeout layers

Three layers, outermost wins:

```
Agent MCP client (5m) -> MCP Gateway (0s = none) -> Trading server handler
```

- **Agent**: `http.Client.Timeout = 5m` (set in agent code, all MCP clients)
- **MCP Gateway**: `TIMEOUT: "0s"` = no timeout, transparent proxy. Caller's deadline controls.
- **Trading server**: no handler timeout. Internal blockchain calls use `context.Background()` with 60s.

The gateway MUST NOT impose its own deadline. It is the client's decision (agent) and the specific MCP server's internal logic.

## Context ownership

- `ctx` comes from the HTTP request handler (agent's 5m timeout)
- Steps 1-9 share this context
- Steps 10-11 create `context.Background()` with independent 60s timeout

## Blockchain calls per filled trade

1. **submitTradeIntent** (step 3) - RiskRouter contract, synchronous, pre-trade validation
2. **PostEIP712Attestation** (step 10) - ValidationRegistry, async goroutine, hackathon leaderboard

Plus `end_cycle` (separate MCP tool) posts its own EIP-712 attestation.

## Matches hackathon template

Flow aligns with `ai-trading-agent-template/src/agent/index.ts`:

```
1. Strategy decision
2. Build + sign TradeIntent (EIP-712)
3. RiskRouter.submitTradeIntent() - on-chain validation
4. If rejected: convert to HOLD
5. If approved: Kraken.placeOrder()
6. Generate checkpoint + post to ValidationRegistry
```
