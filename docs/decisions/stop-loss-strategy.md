# Stop-Loss Strategy — ADR

**Status**: ✅ Shipped
**Decision date**: 2026-04 (hackathon)

## Context

Trading agents must have enforceable stop-loss and take-profit coverage. Options considered at design time:

1. **Silent algorithmic liquidation** - server detects SL hit and market-sells; no agent involvement. Fast, reliable, zero LLM cost.
2. **Post-mortem liquidation** - same silent exit, plus a sub-agent analysis afterwards that writes lessons to agent memory.
3. **Agent-led liquidation** - wake the agent on SL hit, let it decide (with a 30-second hard fallback to silent liquidation).

Each option has different trade-offs between safety, cost, and agent autonomy.

## Decision

We ship a **two-tier software + native hybrid** that combines silent execution with optional post-mortem learning:

### Tier 1 - native exchange stop orders

When the exchange client implements `StopOrderProvider` (Kraken does, `golang/internal/exchange/kraken_client.go`), the Trading MCP registers native stop-loss and take-profit orders on the exchange side during `trade/submit_order`. The exchange itself executes the exit atomically even if our server is down. This is the hard safety net.

### Tier 2 - software polling via conditional orders

For venues without native stop support, or as a secondary layer even where native stops exist, the Trading MCP creates software conditional orders via `trade/set_conditional` (`golang/internal/mcps/trading/service.go:3456`). A background poller checks every ~10 seconds and fires the configured action when the trigger price is hit. OCO linking via `group_id` cancels siblings when one fires, so an SL+TP pair never double-fills.

### Agent involvement

Silent by default - Tier 1 and Tier 2 execute without waking the agent. The `inform_agent` flag on `trade/set_conditional` is available when an operator wants the agent to be notified on a trigger (e.g. for post-mortem analysis), but is not the default path. This is effectively "Strategy 1 (Silent)" with "Strategy 2 (Post-Mortem)" available opt-in.

Agent-Led Liquidation (Strategy 3) is **not shipped** - the 30-second hard fallback is structurally equivalent to silent execution with extra latency, and autonomy here would contradict the "deterministic, replayable, audit-ready" goal.

### Policy enforcement

Swiftward's trading ruleset (`swiftward/policies/rulesets/trading/v1/rules.yaml`) enforces:

- `require_stop_loss` - every buy order must include `stop_loss` in its params
- `reject_inverted_stop_loss` - the stop price must be below the entry for buys
- `validate_stop_loss_proximity` - the stop must be within ~15% of the entry price

Agents physically cannot submit a stop-less buy; the policy gate rejects it before any signature is produced.

## Consequences

**Pros**:
- Native exchange stops give a hardware-level safety net that does not depend on our server's liveness
- Software conditional orders handle OCO linking and provide venue-portable SL / TP
- Swiftward policy makes SL mandatory at the rule level, not at the agent's discretion
- Silent execution keeps latency low and costs zero LLM tokens
- Post-mortem learning remains available via the `inform_agent` opt-in

**Cons**:
- Dual enforcement paths (native + software) need to stay in sync; OCO linking mitigates but does not eliminate the risk
- No formal circuit-breaker for bad ticks yet - the market data source chain is the first line of defense and the policy engine's `heartbeat_trailing_24h` rule is the backstop

## References

- `golang/internal/mcps/trading/service.go:3456` - `trade/set_conditional` tool
- `golang/internal/mcps/trading/service.go:3685` - `trade/cancel_conditional` tool
- `golang/internal/mcps/trading/service.go:3757` - auto-creation of SL / TP conditionals from fills with OCO linking
- `golang/internal/exchange/kraken_client.go` - `StopOrderProvider` implementation (native stops)
- `swiftward/policies/rulesets/trading/v1/rules.yaml` - `require_stop_loss`, `reject_inverted_stop_loss`, `validate_stop_loss_proximity`
- `docs/plans/completed/realistic-risk-rules.md` - full trading ruleset description
- `docs/plans/completed/universal-trading-mcp.md` - Trading MCP tool surface including conditionals
