# Trading MCP (13 tools)

Trade execution, portfolio management, risk limits, trade history, conditional orders, alerts, and cycle lifecycle. Every trade goes through the policy engine before execution. Agent identity is set via `X-Agent-ID` header (not a tool parameter).

All monetary values are returned as decimal strings (not floats) to preserve precision.

**Endpoint**: `POST /mcp/trading` (direct) or `POST /mcp/trading` via MCP Gateway at `swiftward-server:8095`.

**Tool surface**:
- Core execution: `trade/submit_order`, `trade/estimate_order`
- Portfolio & history: `trade/get_portfolio`, `trade/get_portfolio_history`, `trade/get_history`, `trade/get_limits`
- Liveness: `trade/heartbeat`
- Conditional orders & reminders: `trade/set_conditional`, `trade/cancel_conditional`, `trade/set_reminder`
- Cycle lifecycle: `trade/end_cycle`
- Alerts: `alert/triggered`, `alert/list`

## `trade/submit_order`

Submit a trade. Validation pipeline: balance check -> Swiftward policy -> RiskRouter on-chain -> exchange fill. See [submit-order-pipeline.md](../architecture/submit-order-pipeline.md) for full step-by-step.

**Params:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `pair` | string | yes | e.g., "ETH-USDC" |
| `side` | string | yes | `buy` or `sell` (validated strictly - no other values accepted) |
| `value` | float | yes | Trade size in USD (must be > 0) |

**Result (approved):**
```json
{
  "status": "fill",
  "fill": {
    "id": "sim-1234",
    "pair": "ETH-USDC",
    "side": "buy",
    "price": "3205.50",
    "qty": "0.0312",
    "value": "100.01"
  },
  "decision_hash": "0xabc...",
  "prev_hash": "0xdef..."
}
```

If on-chain submission is configured, also includes `tx_hash` and `chain_success`.

**Result (rejected by policy):**
```json
{
  "status": "reject",
  "reject": {
    "source": "policy",
    "reason": "exceeds daily limit",
    "verdict": "rejected",
    "exec_id": "eval-001"
  }
}
```

**Result (agent halted):**
```json
{
  "status": "reject",
  "reject": {
    "source": "halt",
    "reason": "agent_halted"
  }
}
```

**Irreversible side effects**: If the exchange fill succeeds but DB persistence fails, the response includes `"persist_error": "..."` alongside the fill data. The trade happened and can't be undone.

**Evidence chain**: Each successful trade produces a keccak256 hash-chained decision trace stored in the `decision_traces` table. The `prev_hash` field links to the previous trace for this agent, forming a tamper-evident chain.

## `trade/estimate_order`

Dry-run: check current price, estimated quantity, and portfolio impact. No state changes.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `pair` | string | yes | e.g., "ETH-USDC" |
| `side` | string | yes | `buy` or `sell` (validated strictly) |
| `value` | float | yes | Trade size in USD (must be > 0) |

**Result:**
```json
{
  "pair": "ETH-USDC",
  "side": "buy",
  "value": "100",
  "price": "3204.80",
  "qty": "0.0312",
  "portfolio": {
    "cash": "9500",
    "value": "10000"
  },
  "halted": false,
  "position_pct_after": 5.2,
  "warning": "insufficient cash"
}
```

The `warning` field only appears for buys that exceed available cash. `position_pct_after` shows the estimated position concentration after the trade.

## `trade/get_portfolio`

Full portfolio state: cash, open positions, fill count, portfolio value.

**Result:**
```json
{
  "portfolio": {
    "value": "10000.00",
    "cash": "9500.00",
    "peak": "10050.00"
  },
  "positions": [
    {
      "pair": "ETH-USDC",
      "side": "long",
      "size": "0.0312",
      "avg_price": "3205.50",
      "value": "100.01"
    }
  ],
  "fill_count": 5,
  "reject_count": 1,
  "halted": false
}
```

Positions are computed from the trade log (no separate positions table). Cost basis uses proportional reduction on sells with flat reset.

## `trade/get_history`

Trade history with filtering. Returns trades in reverse chronological order.

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 50 | Max trades to return |
| `pair` | string | all | Filter by pair |
| `status` | string | all | Filter: `fill`, `reject`, `agent_halted` |

**Result:**
```json
{
  "trades": [
    {
      "timestamp": "2026-03-07T12:00:00Z",
      "pair": "ETH-USDC",
      "side": "buy",
      "qty": "0.0312",
      "status": "fill",
      "price": "3205.50",
      "value": "100.01",
      "pnl_value": "0",
      "portfolio": {
        "value_after": "10000.00"
      },
      "fill": {
        "id": "sim-1234"
      },
      "decision_hash": "0xabc..."
    }
  ],
  "count": 1
}
```

Rejected trades omit `price`, `value`, `pnl_value`, `portfolio.value_after`.

## `trade/get_portfolio_history`

Equity curve from trade history. Returns portfolio value from approved trades.

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 100 | Max data points |

**Result:**
```json
{
  "equity_curve": [
    {
      "timestamp": "2026-03-07T11:00:00Z",
      "portfolio_value": "10000.00",
      "pair": "ETH-USDC",
      "side": "buy"
    },
    {
      "timestamp": "2026-03-07T12:00:00Z",
      "portfolio_value": "10050.00",
      "pair": "ETH-USDC",
      "side": "sell"
    }
  ],
  "count": 2
}
```

Points are in chronological order (oldest first). Only approved trades with positive portfolio value are included.

## `trade/get_limits`

Current usage against policy limits: portfolio value, largest position, halt state.

**Result:**
```json
{
  "portfolio": {
    "value": "10000.00",
    "cash": "9500.00"
  },
  "fill_count": 5,
  "reject_count": 1,
  "largest_position_pct": 5.2,
  "largest_position_pair": "ETH-USDC",
  "halted": false
}
```

## `trade/heartbeat`

Recompute portfolio equity from current prices, update peak value if new high, return current status.

**Result:**
```json
{
  "agent_id": "agent-alpha-001",
  "portfolio": {
    "value": "10050.00",
    "cash": "9500.00",
    "peak": "10050.00"
  },
  "drawdown_pct": 0,
  "fill_count": 5,
  "reject_count": 1,
  "halted": false,
  "timestamp": "2026-03-07T12:30:00Z"
}
```

Peak value only increases (never rolls back). Uses a targeted conditional update (`SET peak = $2 WHERE peak < $2`), safe without the full advisory lock.

## `trade/set_conditional`

Create a software conditional order (stop-loss, take-profit, or price alert). Monitored by the platform; fires when the price condition is met.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `pair` | string | yes | e.g., "ETH-USDC" |
| `type` | string | yes | `stop_loss`, `take_profit`, or `price_alert` |
| `trigger_price` | number | yes | Price at which the condition fires |
| `note` | string | no | Free-form note shown in the alert feed |
| `inform_agent` | bool | no | If true, triggering wakes the agent via Telegram; default false |

**Result**: `{alert_id, status: "active", tier}`. Conditional orders auto-create during `trade/submit_order` when `params.stop_loss` or `params.take_profit` are provided; each SL / TP pair shares a `group_id` for OCO linking (one triggers, siblings cancel).

## `trade/cancel_conditional`

Cancel a software conditional order by `alert_id`. Also cancels native exchange stop orders if the client is a Tier-1 `StopOrderProvider`.

| Param | Type | Required |
|-------|------|----------|
| `alert_id` | string | yes |

**Result**: `{cancelled: true}`.

## `trade/set_reminder`

Schedule a time-based wake for the agent. Useful for "check back in N minutes" prompts without staying in the current session.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `when` | string | yes | ISO-8601 timestamp or relative (`"30m"`, `"2h"`) |
| `note` | string | no | Message surfaced when the reminder fires |

**Result**: `{alert_id, status: "active"}`.

## `trade/end_cycle`

Close a trading cycle. Updates the peak equity, writes a cycle-end marker to the trade log, and resets per-cycle counters. Agents call this at the end of an analysis loop before sleeping.

| Param | Type | Required |
|-------|------|----------|
| `note` | string | no |

**Result**: `{success: true}`.

## `alert/triggered`

Fetch the queue of alerts that have fired since the last call. Draining this queue clears the "new" flag for the agent.

**Result**: `{alerts: [{alert_id, type, pair, trigger_price, triggered_at, note, group_id}, ...]}`.

## `alert/list`

List all active alerts for this agent, with distance-to-trigger computed against the current price.

**Result**: `{alerts: [{alert_id, type, pair, trigger_price, current_price, distance_pct, note, status}, ...]}`.
