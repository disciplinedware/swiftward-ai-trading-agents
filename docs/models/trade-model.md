# Trading Data Models

> **Status**: Shipped. Naming shown in every "Proposed name" column below is the current, shipped naming in code and events. The "Current name" columns describe the pre-rename state kept for historical context. Treat the "Proposed" column as authoritative.

## Trade Amounts: Three Currencies

Every trade involves up to three currencies. For pair ETH-BTC with portfolio currency USD:

```
base (ETH)     quote (BTC)              portfolio (USD)
  qty            quote_qty                 value              <- agent sends ONE of these
  0.5 ETH        0.015 BTC                $1,500

  fill.qty   *  fill.price  =  fill.quote_qty  (* rate)  =  fill.value   <- exchange returns
  0.5 ETH    *  0.03 BTC    =  0.015 BTC       (* 100k)  =  $1,500
```

**Order input** - three mutually exclusive fields, no prefix (context is the order):

| Field | Currency | Example | Description |
|-------|----------|---------|-------------|
| `value` | portfolio | 1500 (USD) | "buy $1500 worth" |
| `qty` | base | 0.5 (ETH) | "buy 0.5 ETH" |
| `quote_qty` | quote | 0.015 (BTC) | "spend 0.015 BTC" |

No grouping prefix - follows FIX (`OrderQty` / `CashOrderQty`) and Binance (`quantity` /
`quoteOrderQty`) convention: mutually exclusive fields, validated by API, documented as
"exactly one required." Currently only `value` is implemented.

**Fill output** - nested under `fill` object in both MCP response and Swiftward events.
No `fill_` prefix - the nesting provides the namespace. May differ from order due to slippage.

| Field | Currency | Example | Description |
|-------|----------|---------|-------------|
| `fill.qty` | base | 0.5 ETH | quantity filled |
| `fill.price` | quote/base | 0.03 BTC/ETH | price per unit |
| `fill.quote_qty` | quote | 0.015 BTC | total in quote (qty * price) |
| `fill.value` | portfolio | $1,500 USD | notional in portfolio currency |

When quote IS the portfolio currency (ETH-USDC, BTC-USDT), `fill.quote_qty` = `fill.value`.
This is the current state - all permitted pairs quote in USD stablecoins.

## Leverage and Margin

`fill.value` is always the **notional** (full position exposure). With leverage > 1, the
cash actually locked is less:

```
cash locked = fill.value / fill.leverage

Example: buy $1500 of ETH at 2x leverage
  fill.value    = $1,500  (notional - what you're exposed to)
  fill.leverage = 2
  cash locked   = $750    (computed: 1500 / 2)
```

No separate `margin_value` field - trivially computed from `value / leverage`.
When `leverage = 1` (default), cash locked = value.

Policy rules check **notional** (`order.value`, `fill.value`) for position sizing and
concentration limits. Cash sufficiency checks use `value / leverage`.

## Fees

Fees can be charged in the base asset, quote asset, or a third token (e.g. platform token).
Nested under `fee` sub-object inside `fill`:

| Field | Description | Example |
|-------|-------------|---------|
| `fee.amount` | fee charged, in `fee.asset` units | 0.001 |
| `fee.asset` | which currency the fee is in | "ETH", "USDC" |
| `fee.value` | fee converted to portfolio currency | $3.00 |

Currently no fees in simulated mode. Fields will be zero/empty until real exchange
integration. The algosmithy model (reference) supports three fee modes: quote-currency,
base-currency, and side-dependent (buy=base, sell=quote).

## Portfolio Currency

All `.value` fields (e.g. `portfolio.value`, `fill.value`, position `value`, `pnl_value`)
are denominated in the agent's **portfolio currency**. Currently implicitly USD (enforced
by the permitted pairs whitelist, not explicitly configured).

Currently defined in: `AgentConfig.InitialBalance` (number, no currency) in
`golang/internal/config/config.go`. The permitted-pairs whitelist limits trading to
USD-quoted markets so a dedicated `portfolio_currency` field is not needed today;
it would become required if non-USD quotes (e.g. ETH-BTC) were added, and is a
known future-work item.

## Fill Model

Single fill per order. No partial fills. The Risk Router is all-or-nothing (not an
order book). Agent sends an order (with `value`, `qty`, or `quote_qty`), gets back
one result: filled (with `fill.qty` + `fill.price`) or rejected (with `reason`).

## Event Nesting Convention

Swiftward events nest domain data under named objects to avoid flat-namespace collisions:

- `order` - agent's order fields (pair, side, value, leverage, etc.)
- `portfolio` - enriched portfolio context (value, peak, cash)
- `fill` - exchange fill fields (in `execution_report` events)
- `fill.fee` - fee sub-object (amount, asset, value)
- `reject` - rejection details (in `execution_report` events)

This eliminates the need for `order_` or `fill_` prefixes in events. Field names inside
the nested object match the MCP field names (without prefix). Example paths in rules YAML:
`event.data.order.value`, `event.data.portfolio.value`, `event.data.fill.price`,
`event.data.fill.fee.value`.

---

## Order Request

Agent submits via `trade/submit_order` (MCP tool call). Validation pipeline runs in order:
balance check -> Swiftward policy -> RiskRouter on-chain -> exchange fill.
See [submit-order-pipeline.md](../architecture/submit-order-pipeline.md) for the full step-by-step flow.

| Field | Type | Current name | Proposed name | MCP input | Event | Source |
|-------|------|-------------|---------------|-----------|-------|--------|
| trading pair | string | `market` | `pair` | `pair` (required) | `order.pair` | agent |
| direction | string | `side` | `side` | `side` (required) | `order.side` | agent |
| size (portfolio) | number | `size` | `value` | `value` (one of*) | `order.value` | agent |
| size (base) | number | - | `qty` | `qty` (one of*) | `order.qty` | agent |
| size (quote) | number | - | `quote_qty` | `quote_qty` (one of*) | `order.quote_qty` | agent |
| ref price | number | - | `price` | `price` (optional) | `order.price` | agent |
| leverage | number | - | `leverage` | `leverage` (optional, default 1) | `order.leverage` | agent |
| slippage | number | - | `max_slippage_pct` | `max_slippage_pct` (optional) | `order.max_slippage_pct` | agent |
| reason | string | - | `reason` | `reason` (optional) | `order.reason` | agent |
| portfolio value | number | `portfolio_value` | `portfolio.value` | - | `portfolio.value` | enriched |
| peak value | number | `peak_value` | `portfolio.peak` | - | `portfolio.peak` | enriched |
| cash | number | `cash` | `portfolio.cash` | - | `portfolio.cash` | enriched |

*Three mutually exclusive ways to specify order size - exactly one required:
- `value` - "buy $1500 worth of ETH" (portfolio currency). Trading MCP converts to qty.
- `qty` - "buy 0.5 ETH" (base currency). Trading MCP converts to value for policy check.
- `quote_qty` - "spend 0.015 BTC on ETH" (quote currency). Useful for non-USD pairs.

**Agent sends exactly one. Trading MCP always populates `order.value` (portfolio currency,
notional) regardless of which field the agent used - converting from qty or quote_qty
using current prices if needed. Policy rules use `event.data.order.value` and
`event.data.portfolio.value` for size checks.

Example event:
```json
{
  "type": "trade_order",
  "entity_id": "agent-42",
  "data": {
    "order": {
      "pair": "ETH-USDC",
      "side": "buy",
      "value": 1500,
      "price": 3000,
      "leverage": 1,
      "max_slippage_pct": 0.5,
      "reason": "RSI oversold"
    },
    "portfolio": {
      "value": 50000,
      "peak": 52000,
      "cash": 30000
    }
  }
}
```

Shipped tool name: `trade/submit_order`. Event type: `trade_order`.

Not included (by design):
- `order_type` - always market. No limit orders through Risk Router.
- `time_in_force` - not applicable (single fill, no order book).
- `stop_loss` / `take_profit` - agent manages these via subsequent orders.
- `reduce_only` - could be useful but not in scope.

---

## Execution Report

Result of `trade/submit_order`. Three possible paths:

1. **Policy reject** - Swiftward rejects pre-trade. No exchange interaction. No event.
2. **Router reject** - Swiftward approves, Risk Router rejects on-chain. Event sent to Swiftward.
3. **Fill** - Swiftward approves, Risk Router executes. Event sent to Swiftward.

Shipped event type: `execution_report`.

`status` determines which sub-object is present:
- `status == "fill"`: `fill` object
- `status == "reject"`: `reject` object

### Top-level fields (always present)

| Field | Type | Current name | Proposed name | MCP output | Event | Description |
|-------|------|-------------|---------------|------------|-------|-------------|
| outcome | string | `status` | `status` | `status` | `status` | "fill" or "reject" |
| decision hash | string | `decision_hash` | `decision_hash` | `decision_hash` | `decision_hash` | Swiftward decision hash (always present - policy always evaluates) |

### `reject` object (status == "reject")

| Field | Type | Current name | Proposed name | MCP output | Event | Source |
|-------|------|-------------|---------------|------------|-------|--------|
| source | string | - | `source` | `reject.source` | `reject.source` | "policy" or "router" |
| reason | string | `reason` | `reason` | `reject.reason` | `reject.reason` | human-readable, always |
| verdict | string | `verdict` | `verdict` | `reject.verdict` | - | policy only: "rejected", "agent_halted" |
| exec ID | string | `eval_id` | `exec_id` | `reject.exec_id` | - | policy only: Swiftward execution ID |
| tx hash | string | - | `tx_hash` | `reject.tx_hash` | `reject.tx_hash` | router only: on-chain tx (if reverted) |

Policy rejects: no `execution_report` event (Swiftward made the decision, it already knows).
Router rejects: `execution_report` event sent so `router_rejection_alert` rule fires.
`verdict` and `exec_id` are policy-only (not in events). `tx_hash` is router-only.

### `fill` object (status == "fill")

| Field | Type | Current name | Proposed name | MCP output | Event | Source |
|-------|------|-------------|---------------|------------|-------|--------|
| fill ID | string | `order_id` | `id` | `fill.id` | `fill.id` | Trading MCP |
| trading pair | string | `market` | `pair` | `fill.pair` | `fill.pair` | from fill |
| direction | string | `side` | `side` | `fill.side` | `fill.side` | from fill |
| price | string/number | `price` | `price` | `fill.price` | `fill.price` | from fill |
| quantity | string/number | `filled_qty` | `qty` | `fill.qty` | `fill.qty` | from fill |
| in quote | string/number | - | `quote_qty` | `fill.quote_qty` | `fill.quote_qty` | computed |
| in portfolio | string/number | - | `value` | `fill.value` | `fill.value` | computed (notional) |
| leverage | number | - | `leverage` | `fill.leverage` | `fill.leverage` | from order (default 1) |
| fee amount | string/number | - | `fee.amount` | `fill.fee.amount` | `fill.fee.amount` | from exchange |
| fee asset | string | - | `fee.asset` | `fill.fee.asset` | `fill.fee.asset` | from exchange |
| fee in portfolio | string/number | - | `fee.value` | `fill.fee.value` | `fill.fee.value` | computed |
| tx hash | string | `tx_hash` | `tx_hash` | `fill.tx_hash` | `fill.tx_hash` | from chain |

MCP output uses decimal strings. Event uses numbers.
`fill.value` = notional (full exposure). Cash deducted = `value / leverage`.

### Examples

MCP response (fill):
```json
{
  "status": "fill",
  "decision_hash": "0x...",
  "fill": {
    "id": "fill-001",
    "pair": "ETH-USDC",
    "side": "buy",
    "price": "3000",
    "qty": "0.5",
    "quote_qty": "1500",
    "value": "1500",
    "leverage": 1,
    "fee": {"amount": "0", "asset": "", "value": "0"},
    "tx_hash": "0x..."
  }
}
```

MCP response (policy reject):
```json
{
  "status": "reject",
  "decision_hash": "0x...",
  "reject": {
    "source": "policy",
    "reason": "Position would exceed 10% of portfolio",
    "verdict": "rejected",
    "exec_id": "exec-abc123"
  }
}
```

MCP response (router reject):
```json
{
  "status": "reject",
  "decision_hash": "0x...",
  "reject": {
    "source": "router",
    "reason": "Insufficient margin",
    "tx_hash": "0x..."
  }
}
```

Event (fill) - sent to Swiftward:
```json
{
  "type": "execution_report",
  "entity_id": "agent-42",
  "data": {
    "status": "fill",
    "decision_hash": "0x...",
    "fill": {
      "id": "fill-001",
      "pair": "ETH-USDC",
      "side": "buy",
      "price": 3000,
      "qty": 0.5,
      "quote_qty": 1500,
      "value": 1500,
      "leverage": 1,
      "fee": {"amount": 1, "asset": "EUR", "value": 1.1},
      "tx_hash": "0x..."
    }
  }
}
```

Event (router reject) - sent to Swiftward:
```json
{
  "type": "execution_report",
  "entity_id": "agent-42",
  "data": {
    "status": "reject",
    "decision_hash": "0x...",
    "reject": {
      "source": "router",
      "reason": "Insufficient margin"
    }
  }
}
```

---

## Heartbeat

Agent calls `trade/heartbeat` with **no arguments**. Trading MCP fetches live prices,
recomputes portfolio equity, and sends `heartbeat` event to Swiftward for drawdown checks.

### heartbeat event (to Swiftward)

| Field | Type | Current name | Proposed name | Source | Description |
|-------|------|-------------|---------------|--------|-------------|
| prices | object | `prices` | `prices` | live exchange | {pair: price}, e.g. {"ETH-USDC": 3000} |
| type | string | `check_type` | `check_type` | Trading MCP | "periodic" or "on_demand" |

### heartbeat response (to agent)

| Field | Type | Current name | Proposed name | Description |
|-------|------|-------------|---------------|-------------|
| total value | string | `equity` | `portfolio.value` | current portfolio value |
| cash | string | `cash` | `portfolio.cash` | available cash |
| peak | string | `peak_value` | `portfolio.peak` | all-time high |
| fills | int | `trade_count` | `fill_count` | total fills |
| rejections | int | `reject_count` | `reject_count` | router rejections |
| halted | bool | `halted` | `halted` | whether agent is halted |
| time | string | `timestamp` | `timestamp` | RFC3339 timestamp |

---

## Other MCP Tools

### trade/estimate_order

Dry-run a trade without executing. Same input fields as submit_order.
Current tool name: `trade/estimate` -> proposed: `trade/estimate_order`

Purely informational - no fill or reject. Returns projected numbers + portfolio context
+ optional warning. No Swiftward evaluation. Agent uses this to preview before committing.

| Field | Type | Current name | Proposed name | Description |
|-------|------|-------------|---------------|-------------|
| trading pair | string | `market` | `pair` | echoed from input |
| direction | string | `side` | `side` | echoed from input |
| current price | string | `current_price` | `price` | current market price in quote currency |
| projected qty | string | `estimated_qty` | `qty` | projected quantity in base (value / price) |
| projected value | string | `size_usd` | `value` | order value in portfolio currency (echoed) |
| portfolio value | string | `equity` | `portfolio.value` | current portfolio value |
| cash | string | `cash_available` | `portfolio.cash` | available cash in portfolio currency |
| peak | string | - | `portfolio.peak` | all-time high portfolio value |
| fill count | int | - | `fill_count` | total fills |
| reject count | int | - | `reject_count` | router rejections |
| halted | bool | `halted` | `halted` | whether agent is halted |
| position % after | float | `position_pct_after` | `position_pct_after` | projected position % of portfolio |
| warning | string | `warning` | `warning` | e.g. "insufficient cash" (optional) |

### trade/get_portfolio

| Field | Type | Current name | Proposed name | Description |
|-------|------|-------------|---------------|-------------|
| total value | string | `portfolio_value` | `portfolio.value` | cash + positions value |
| cash | string | `cash` | `portfolio.cash` | available cash in portfolio currency |
| peak | string | `peak_value` | `portfolio.peak` | all-time high portfolio value |
| fills | int | `trade_count` | `fill_count` | total successful fills |
| rejections | int | `reject_count` | `reject_count` | fills rejected by Risk Router |
| halted | bool | - | `halted` | whether agent is halted |
| positions | array | `positions` | `positions` | open positions (see format below) |

Position format: `[{pair, side, qty, avg_price, quote_qty, value, leverage}]`

| Field | Type | Current name | Proposed name | Description |
|-------|------|-------------|---------------|-------------|
| trading pair | string | `market` | `pair` | e.g. "ETH-USDC" |
| direction | string | `side` | `side` | "buy" (long) or "sell" (short) |
| quantity | string | `size` | `qty` | position size in base currency |
| avg entry price | string | `avg_price` | `avg_price` | average entry price in quote currency |
| in quote | string | - | `quote_qty` | entry cost in quote currency (qty * avg_price) |
| in portfolio | string | `cost_usd` | `value` | entry cost in portfolio currency (notional) |
| leverage | number | - | `leverage` | leverage used for this position (default 1) |

Same fields as fill - position is accumulated fills. All amounts are entry cost,
not current market value. Current value = `qty * current_price` (computed live).
Cash locked = `value / leverage`.

### trade/get_history

Returns `{trades: [...], count}`. Same structure as execution report - `status` determines
which sub-object is present. Plus top-level order info and portfolio impact.

| Field | Type | Current name | Proposed name | Description |
|-------|------|-------------|---------------|-------------|
| time | string | `timestamp` | `timestamp` | when the order was submitted |
| trading pair | string | `market` | `pair` | trading pair |
| direction | string | `side` | `side` | buy/sell |
| outcome | string | `verdict` | `status` | "fill" or "reject" |
| decision hash | string | `decision_hash` | `decision_hash` | Swiftward hash |
| reject source | string | - | `reject.source` | "policy" or "router" (reject only) |
| reject reason | string | - | `reject.reason` | human-readable (reject only) |
| fill ID | string | `order_id` | `fill.id` | unique fill ID (fill only) |
| fill price | string | `price` | `fill.price` | price in quote currency (fill only) |
| fill qty | string | `qty` | `fill.qty` | quantity in base (fill only) |
| fill in quote | string | - | `fill.quote_qty` | total in quote currency (fill only) |
| fill in portfolio | string | `cost_usd` | `fill.value` | notional in portfolio currency (fill only) |
| leverage | number | - | `fill.leverage` | leverage used (fill only) |
| fee in portfolio | string | - | `fill.fee.value` | fee in portfolio currency (fill only) |
| pnl | string | `pnl` | `pnl_value` | realized PnL in portfolio currency (fill only) |
| portfolio after | string | `equity_after` | `portfolio.value_after` | portfolio value after fill (fill only) |

### trade/get_portfolio_history

Returns `{equity_curve: [...], count}`. Each point:

| Field | Type | Current name | Proposed name | Description |
|-------|------|-------------|---------------|-------------|
| time | string | `timestamp` | `timestamp` | when the trade happened |
| portfolio value | string | `equity` | `portfolio.value` | portfolio value after trade |
| traded pair | string | `trade_market` | `pair` | which pair was filled |
| traded side | string | `trade_side` | `side` | buy/sell |

### trade/get_limits

| Field | Type | Current name | Proposed name | Description |
|-------|------|-------------|---------------|-------------|
| total value | string | `equity` | `portfolio.value` | current portfolio value |
| cash | string | `cash` | `portfolio.cash` | available cash |
| peak | string | - | `portfolio.peak` | all-time high portfolio value |
| fills | int | `trade_count` | `fill_count` | total fills |
| rejections | int | `reject_count` | `reject_count` | router rejections |
| halted | bool | `halted` | `halted` | whether agent is halted |
| largest pos % | float | `largest_position_pct` | `largest_position_pct` | biggest position as % of portfolio |
| largest pos pair | string | `largest_position_mkt` | `largest_position_pair` | which pair |

### MCP tool name renames

| Current | Proposed |
|---------|----------|
| `trade/submit_intent` | `trade/submit_order` |
| `trade/estimate` | `trade/estimate_order` |
| `trade/get_portfolio` | keep |
| `trade/get_history` | keep |
| `trade/get_limits` | keep |
| `trade/get_portfolio_history` | keep |
| `trade/heartbeat` | keep |

---

## Swiftward State (persisted in Swiftward DB)

State accumulated across events. Used by heartbeat rules and execution_report bookkeeping.

### State model: agent

| Section | Field | Current name | Proposed name | Updated by | Description |
|---------|-------|-------------|---------------|------------|-------------|
| counters | fills | `trade_count` | `fill_count` | execution_report (success) | total successful fills |
| counters | rejections | `rejection_count` | `reject_count` | execution_report (failure) | Risk Router rejections |
| metadata | portfolio | `portfolio_value` | `portfolio_value` | execution_report | last known total value (portfolio currency) |
| metadata | peak | `peak_value` | `peak_value` | execution_report | all-time high value (portfolio currency) |
| metadata | positions | `positions` | `positions` | execution_report | JSON: {"ETH-USDC": {qty, value}, ...} |
| metadata | cash | `cash_balance` | `cash` | agent init | available cash (portfolio currency) |
| metadata | token ID | `agent_token_id` | `agent_token_id` | agent init | ERC-8004 NFT token ID |
| metadata | day start | `day_start_value` | `day_start_value` | daily reset | portfolio value at day start |
| metadata | initial | `initial_capital` | `initial_value` | agent init | original capital allocation |
| labels | halted | `halted` | `halted` | heartbeat | blocks all trading until manual resume |
| buckets | velocity | `trades_10m` | `orders_10m` | trade_order | rolling 10min order count |
| buckets | cooldown | `drawdown_cooldown` | `drawdown_cooldown` | drawdown_breaker rule | >0 = portfolio dropped >5%, trading paused (1h window) |

---

## Signals (computed during rule evaluation)

Signals are UDFs that compute derived values from event data and state. They run inside
Swiftward during rule evaluation - Trading MCP and agents never see them.

### Pre-trade signals (trade_order)

| Signal | Current name | Proposed name | Inputs | Output | Notes |
|--------|-------------|---------------|--------|--------|-------|
| order size % | `position_risk` | (inline expr?) | event.data.order.value, event.data.portfolio.value | fraction (0.0-1.0) | Just order.value / portfolio.value. Could be expression if DSL supports it. |
| concentration | `concentration_after` | `concentration_check` | event.data.order.pair, event.data.order.value, event.data.order.side, state.positions, event.data.portfolio.value | fraction (0.0-1.0) | Projected % in one asset after trade. Real logic - needs UDF. |
| drawdown | `daily_drawdown` | (inline expr?) | event.data.portfolio.value, event.data.portfolio.peak | negative fraction | Just (portfolio.value - portfolio.peak) / portfolio.peak. Could be expression. |
| order rate | `trade_velocity` | `order_rate` | entity_id, bucket | count | Reads orders_10m bucket. May be built-in. |

### Heartbeat signals

| Signal | Current name | Proposed name | Inputs | Output | Notes |
|--------|-------------|---------------|--------|--------|-------|
| mark-to-market | `mark_to_market` | `mark_to_market` | positions, prices, cash | {portfolio_value} | Recalculates from live prices. |
| 3-layer drawdown | `drawdown_3layer` | `drawdown_3layer` | portfolio_value, peak_value, day_start_value, initial_value | {daily_pct, peak_pct, floor_pct} | Three drawdown calculations. |

### Post-trade signals (execution_report)

| Signal | Current name | Proposed name | Inputs | Output | Notes |
|--------|-------------|---------------|--------|--------|-------|
| state update | `portfolio_update` | `fill_state_update` | event.data.fill.{pair, side, qty, price, value, leverage, fee.value}, state.agent.metadata.{positions, portfolio_value, peak_value} | {updated_*} | Computes new state after fill. |

---

## Rule names

| Current name | Proposed name | Event | Description |
|-------------|---------------|-------|-------------|
| `halted_check` | `halted_check` | trade_order | Hard block - agent halted |
| `drawdown_cooldown_active` | `drawdown_cooldown_active` | trade_order | Soft block - drawdown cooldown active |
| `position_limit` | `max_order_size` | trade_order | Order too large vs portfolio |
| `concentration_limit` | `concentration_limit` | trade_order | Too much in one asset |
| `drawdown_breaker` | `drawdown_breaker` | trade_order | Portfolio >5% drawdown triggers cooldown |
| `velocity_limit` | `order_rate_limit` | trade_order | Too many orders in 10min |
| `approved_markets_only` | `pair_whitelist` | trade_order | Only permitted pairs |
| `count_intent` | `count_orders` | trade_order | Bookkeeping - velocity bucket |
| `leverage_cap` | `leverage_cap` | trade_order | v2 only - max leverage |
| `heartbeat_drawdown_daily` | `heartbeat_drawdown_daily` | heartbeat | Daily drawdown halt |
| `heartbeat_drawdown_peak` | `heartbeat_drawdown_peak` | heartbeat | Peak drawdown halt |
| `heartbeat_drawdown_absolute` | `heartbeat_drawdown_absolute` | heartbeat | Absolute drawdown halt |
| `track_and_request_validation` | `track_fill` | execution_report | Update state after fill |
| `router_rejection_alert` | `router_rejection_alert` | execution_report | Drift detection |

---

## Constant names

| Current name | Proposed name | Value (v1) | Value (v2) | Description |
|-------------|---------------|------------|------------|-------------|
| `max_position_pct` | `max_order_pct` | 0.15 | 0.10 | Max single order as % of portfolio |
| `max_concentration` | `max_concentration` | 0.50 | 0.35 | Max % of portfolio in one asset |
| `max_drawdown` | `max_drawdown` | -0.05 | -0.03 | Pre-trade daily loss limit |
| `max_drawdown_daily` | `max_drawdown_daily` | -0.05 | -0.03 | Heartbeat daily loss halt |
| `max_drawdown_peak` | `max_drawdown_peak` | -0.10 | -0.07 | Heartbeat peak loss halt |
| `max_drawdown_absolute` | `max_drawdown_absolute` | -0.20 | -0.15 | Heartbeat absolute loss halt |
| `max_velocity_10m` | `max_orders_10m` | 50 | 25 | Max orders per 10min window |
| `max_leverage` | `max_leverage` | - | 2 | v2 only |
| `approved_markets` | `permitted_pairs` | [ETH-USDC, BTC-USDC, ETH-USDT, BTC-USDT] | same | Allowed trading pairs |
