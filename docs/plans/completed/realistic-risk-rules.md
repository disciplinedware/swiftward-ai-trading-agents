# Trading Policy Ruleset v1

> **Status**: ✅ Shipped
> **Location**: `swiftward/policies/rulesets/trading/v1/`
> **Stream**: `trading`
> **Engine**: Swiftward (closed-source, Docker from GHCR)

## What was built

A declarative YAML policy ruleset that gates every trade intent and monitors portfolio health between trades. Graduated risk tiers respond to intraday drawdown. Independent kill switches respond to trailing and absolute drawdown. A loss-streak circuit breaker catches tilt / revenge trading. Every decision emits structured events that are hash-chained into the decision trace and surfaced in the dashboard, Telegram, and on-chain ValidationRegistry.

## Pre-trade rules (trade_order events)

| Rule | Priority | What it does | Tier |
|------|----------|--------------|------|
| `halted_check` | 110 | Block all trades if agent is halted | - |
| `close_only_mode` | 105 | Only exits allowed when tier 3 (DD <= -5%) | 3 |
| `loss_streak_check` | 100 | Block new positions while 1h pause flag is active | - |
| `loss_streak_pause` | 100 | 3 consecutive losses → activate 1h pause, reset counter | - |
| `max_order_value_absolute` | 96 | Hard cap: order value <= 1,000 quote currency | - |
| `tier2_order_size` | 95 | Max 5% of portfolio per order (DD -3.5% to -5%) | 2 |
| `tier1_order_size` | 93 | Max 10% of portfolio per order (DD -2% to -3.5%) | 1 |
| `max_order_size` | 90 | Max 15% of portfolio per order (normal) | 0 |
| `require_stop_loss` | 89 | Buy orders must include `stop_loss` param | - |
| `pair_whitelist` | 88 | Reject pairs not in permitted list (currently disabled) | - |
| `tier2_concentration` | 87 | Max 25% in one asset (tier 2) | 2 |
| `reject_inverted_stop_loss` | 87 | Stop-loss must be below price for buys | - |
| `tier1_concentration` | 86 | Max 35% in one asset (tier 1) | 1 |
| `concentration_limit` | 85 | Max 50% in one asset (normal) | 0 |
| `validate_stop_loss_proximity` | 84 | Stop-loss within 15% of entry price | - |
| `order_rate_limit` | 80 | Max 50 orders per 10-minute window | - |
| `count_orders` | 1 | Always increments the rate bucket | - |

Exits (`is_risk_reducing=true`) are never blocked by size or concentration rules. Only the loss-streak pause and close-only mode can block exits, and close-only explicitly allows them.

## Graduated risk tiers

| Tier | Intraday drawdown | Max order size | Max concentration | Mode |
|------|-------------------|----------------|-------------------|------|
| 0 (normal) | 0% to -2% | 15% | 50% | full trading |
| 1 (caution) | -2% to -3.5% | 10% | 35% | reduced sizing |
| 2 (warning) | -3.5% to -5% | 5% | 25% | minimal new exposure |
| 3 (close-only) | below -5% | 0% new | - | exits only |
| halted | - | - | - | kill switch tripped |

Tier is recalculated on every `trade_order` from current intraday drawdown (`portfolio.value / portfolio.day_start_value`). No sticky state - as equity recovers, the tier drops back automatically.

## Heartbeat rules (kill switches)

| Rule | Priority | Trigger | Action |
|------|----------|---------|--------|
| `heartbeat_trailing_24h` | 96 | Drawdown <= -8% from 24h rolling peak | halt + Telegram |
| `heartbeat_absolute` | 97 | Drawdown <= -15% from initial capital | halt + Telegram |

Two independent layers: the trailing 24h peak catches medium-term damage and is recoverable (as equity rises the peak rises with it), while the absolute floor is the true kill switch protecting the account from catastrophic decay.

## Post-trade tracking (execution_report events)

| Rule | What it does |
|------|--------------|
| `track_fill` | Increment `fill_count`, log the execution |
| `track_losing_sell` | Increment `loss_streak` on losing position close (only while pause not active) |
| `track_winning_sell` | Reset `loss_streak` to 0 on any winning close |
| `router_rejection_alert` | Log a warning if the on-chain RiskRouter rejects a Swiftward-approved trade |

## Loss streak detection

Independent of drawdown. Catches tilt / revenge trading even when the overall account is green.

- Trigger: 3 consecutive losing position closes
- Action: `loss_streak_paused` flag on for 1 hour, new positions blocked, exits always allowed
- Reset: any winning close resets the counter to 0; the pause flag auto-expires after 1h TTL

## Eval fixtures (14)

Validate rule behavior end-to-end across pre-trade, heartbeat, and post-trade scenarios. Files live in `swiftward/policies/rulesets/trading/v1/evals/`:

1. `01_small_trade_approved.json` - normal trade within all limits
2. `02_position_limit_rejected.json` - order exceeds position limit
3. `03_concentration_rejected.json` - would exceed concentration limit
4. `04_drawdown_breaker.json` - tier 3 hit, buy rejected, exit allowed
5. `05_velocity_rejected.json` - rate limit (50 / 10m) exceeded
6. `06_unapproved_market.json` - pair not whitelisted (rule currently disabled)
7. `08_drawdown_cooldown_active.json` - tier 1 / tier 2 in effect
8. `09_multiple_rejections.json` - multiple rules fire on the same trade
9. `10_successful_trade_result.json` - fill tracking and PnL counters update
10. `11_router_rejection_alert.json` - on-chain rejection of a Swiftward-approved trade
11. `12_heartbeat_approved.json` - heartbeat within thresholds, agent remains active
12. `13_heartbeat_daily_drawdown_halted.json` - trailing 24h loss past -8%, halt
13. `14_halted_agent_rejected.json` - halted agent attempts a trade, blocked
14. `15_loss_streak_check.json` - pause active, new positions blocked, exits allowed

## How it runs

**Engine**: the Swiftward policy evaluator (Go + YAML DSL v2).

**Event flow**:
1. `trade_order` - Trading MCP posts the intent enriched with portfolio state; Swiftward runs the pre-trade rules; decision is hash-chained and returned as ACCEPT / REJECT
2. `heartbeat` - Trading MCP periodically sends liveness with live prices; Swiftward evaluates the two heartbeat rules and sets `halted` if breached
3. `execution_report` - After a fill, the MCP emits the outcome; Swiftward updates counters (`fill_count`, `loss_streak`, `reject_count`)

**Enriched event data** (built by Trading MCP):
- `portfolio.day_start_value` - last trade's `value_after` before today's UTC midnight (fallback: `initial_value`)
- `portfolio.rolling_peak_24h` - MAX(value_after) over the last 24 hours
- `order.is_risk_reducing` - true if selling a held position
- `order.sl_distance_pct`, `order.sl_inverted` - pre-computed stop-loss validation

**State model**:
- Counters: `fill_count`, `reject_count`, `loss_streak`
- Labels: `halted` (binary, manual resume)
- Flags: `loss_streak_paused` (1h TTL), `loss_streak_paused_until` (timestamp)
- Buckets: `orders_10m` (fixed 10-minute window, UTC)

## Key files

- `swiftward/policies/rulesets/trading/v1/rules.yaml` - full ruleset in DSL v2
- `swiftward/policies/streams/trading.yaml` - stream routing to v1
- `swiftward/policies/rulesets/trading/v1/evals/*.json` - 14 eval fixtures

Rule constants (thresholds, tier boundaries, absolute caps, permitted pairs) live in the same YAML so one file change propagates everywhere.

## Notes

- **Why tiers, not cooldown buckets**: intraday drawdown is computed fresh on every `trade_order`. No sticky cooldown state. Recovery is automatic.
- **Why exits are always allowed**: blocking a winning exit would trap risk. Only the loss-streak pause and explicit halt can block an exit.
- **Loss streak vs drawdown**: orthogonal. An agent can be tier 1 (small DD) but also in loss streak pause (three bad closes) - both gate new positions independently.
- **Telegram alerts**: fire on any rejected pre-trade rule and on heartbeat breaches. Each alert carries the `decision_hash` that links back to on-chain evidence.
- **v2 ruleset**: a stricter version was shadow-tested (tier 3 at -3% instead of -5%) but is currently disabled pending a Swiftward state-isolation fix. The stream config still points at v1.
