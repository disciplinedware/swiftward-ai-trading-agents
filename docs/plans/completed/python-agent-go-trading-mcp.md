# Python Deterministic Agent + Go Trading MCP Integration

> **Status**: ✅ Shipped
> **Python package**: `python/src/agent/`
> **Adapter**: `python/src/agent/infra/go_trading_mcp.py`
> **Go MCP**: `golang/internal/mcps/trading/`
> **Compose service**: `agent-python-deterministic`

## What was built

A Python deterministic trading agent (Tikhon's implementation) with a three-stage brain (market filter → rotation selector → decision engine), four concurrent asyncio trigger loops, and five Python MCP servers for data (price feed, news, on-chain, fear & greed, trading). The agent connects to the Go Trading MCP through an HTTP adapter (`go_trading_mcp.py`) so it inherits Swiftward policy enforcement, Kraken execution, conditional orders with OCO, and automated ERC-8004 attestation. The React dashboard shows per-position SL and TP, populated by the Go backend.

## Agent architecture

**Three-stage brain** (deterministic core with optional LLM touchups):

1. **Stage 1 - Market Filter**: health score from EMA proximity, funding, volatility, BTC trend, and macro events. Verdict: `RISK_OFF` / `UNCERTAIN` / `RISK_ON`.
2. **Stage 2 - Rotation Selector**: ranks assets by momentum, relative strength, and volume. Assigns regime (`STRONG_UPTREND`, `BREAKOUT`, `RANGING`, `WEAK_MIXED`). One-of-BTC-or-ETH OCO filter applied.
3. **Stage 3 - Decision Engine**: ATR-based SL / TP levels, Kelly half-fraction sizing, R:R validation, and a fully assembled `TradeIntent` with strategy tag, reasoning text, confidence score, and trigger reason.

**Four concurrent asyncio loops**:

- **Clock** (15 min) - full three-stage brain cycle
- **Price spike** (1 min) - BTC / ETH ±3% moves fire an ad-hoc brain cycle
- **Tier 2** (5 min) - macro calendar and sentiment circuit breakers
- **Exit watchdog** (2 min) - SL / TP polling (disabled when the Go backend is active because platform conditional orders run server-side)

## Python MCP servers

| MCP | Port | Key tools | Path |
|-----|------|-----------|------|
| `price_feed_mcp` | 8001 | `get_ohlcv`, `get_indicators`, `list_assets` | `python/src/price_feed_mcp/` |
| `news_mcp` | 8002 | `get_headlines`, `get_sentiment`, `get_macro_calendar` | `python/src/news_mcp/` |
| `onchain_data_mcp` | 8003 | `get_funding_rates`, `get_open_interest`, `get_liquidations`, `get_netflow` | `python/src/onchain_data_mcp/` |
| `fear_greed_mcp` | 8004 | `get_fear_greed_index`, `get_daily_average` | `python/src/fear_greed_mcp/` |
| `trading_mcp` | 8006 | `trade/submit_order`, `trade/get_portfolio`, `trade/get_history` | `python/src/trading_mcp/` |

Each server is a FastMCP instance with `stateless_http=True`. Three-layer structure: infra (external clients), service (pure logic), server (one-line tool handlers).

## `go_trading_mcp.py` adapter

`python/src/agent/infra/go_trading_mcp.py` implements the `TradingClient` protocol, replacing `TradingMCPClient` when `config.trading.backend="go"`:

- Sends `X-Agent-ID` header (required by the Go Trading MCP)
- Uses Go tool names: `trade/get_portfolio`, `trade/submit_order`, `trade/end_cycle`, `trade/set_conditional`, `trade/cancel_conditional`
- Converts Python `TradeIntent` (asset, action, size_pct) to Go format (pair, side, value in quote currency)
- Passes SL / TP / strategy / reasoning / trigger_reason / confidence in `params`
- Maps Go responses (`status: "fill"` → `"executed"`, `status: "reject"` → `"rejected"`)
- Reshapes Go portfolio snapshot (with `stop_loss`, `take_profit`, `concentration_pct`) to the Python `PortfolioSnapshot` decimal model
- Posts JSON-RPC 2.0, handles both SSE and plain JSON responses

## Go Trading MCP contributions (set_conditional, cancel_conditional, OCO)

The Python plan pulled several features into the Go Trading MCP (`golang/internal/mcps/trading/service.go`):

- `trade/set_conditional` (tool registered around line 3456) - manual SL / TP / price alert with `pair`, `type`, `trigger_price`, optional `inform_agent`, `note`
- `trade/cancel_conditional` (~line 3685) - cancel by `alert_id`
- **Auto-creation from fills** (~line 3757) - when a fill carries `params.stop_loss` or `params.take_profit`, the service creates platform conditional orders. Both orders share a `group_id`.
- **OCO linking** - the alert poller cancels siblings in the same `group_id` when either fires, preventing double-execution
- **Swiftward policy** - `require_stop_loss` and `validate_stop_loss_proximity` rules enforce that buy orders ship with a stop-loss within ~15% of the entry price

## Dashboard integration

`typescript/src/components/agent/PortfolioTab.tsx` shows SL and TP columns in the positions table:

- `SL` and `TP` columns display the configured stop-loss and take-profit prices
- Distance from the current price is computed client-side and rendered next to the level (e.g. `-5.1%`)
- Positions without SL / TP render `—`

The `TradesTab.tsx` shows trade metadata (strategy, trigger_reason, confidence) and links to the evidence API via the decision hash.

## Tests

**Python** (`pytest` with `asyncio_mode=auto`, `ruff` lint):

- `python/tests/agent/infra/test_go_trading_mcp.py` - LONG / FLAT mapping, response mapping, portfolio reshaping
- `python/tests/agent/brain/test_deterministic_llm.py` - three-stage brain trace, verdict ordering, regime assignment
- `python/tests/agent/trigger/test_*.py` - clock loop timing, spike threshold, macro circuit breaker, SL / TP watchdog
- `respx` for HTTP mocks, `time-machine` for deterministic clock

**Go** (conditional orders + OCO):

- `golang/internal/mcps/trading/service_test.go` - `submit_order` with / without params, OCO cancel on sibling trigger, proactive cancel on position sell, Swiftward policy compliance
- Table-driven per repo convention

## Key files

- `python/src/agent/main.py` - entry point, wires MCPs and selects backend, starts the four loops
- `python/src/agent/brain/deterministic_llm.py` - three-stage brain
- `python/src/agent/infra/go_trading_mcp.py` - Go Trading MCP HTTP adapter
- `python/src/agent/trigger/clock.py` - 15-minute brain cycle
- `python/src/agent/trigger/price_spike.py` - 1-minute spike monitor
- `python/src/agent/trigger/tier2.py` - 5-minute macro circuit breaker
- `python/src/agent/trigger/exit_watchdog.py` - SL / TP polling (disabled with Go backend)
- `python/src/agent/trigger/cooldown.py` - 30-minute cooldown gate
- `python/config/config.example.yaml` - MCP URLs and backend selector
- `python/pyproject.toml` - uv / pytest / ruff / pydantic / structlog / mcp dependencies
- `golang/internal/mcps/trading/service.go:3456-3685` - conditional orders tool handlers
- `golang/internal/mcps/trading/service.go:3757` - OCO linkage in fill handler
- `typescript/src/components/agent/PortfolioTab.tsx` - SL / TP table columns

## Notes

- **Tooling**: `uv` (not pip / poetry), `ruff` (not flake8 / pylint), `pytest` async mode auto, `structlog` for structured logs, `pydantic` for config and models.
- **Decimal precision**: all monetary values are `decimal.Decimal`. Float64 only at config boundaries.
- **Backend switch**: set `config.trading.backend="go"` to use the Go Trading MCP adapter. The Python trading MCP (port 8006) remains available for standalone tests and for the exit watchdog fallback path.
- **SL / TP lifecycle**: auto-created on fills, OCO-linked, cancelled on position close. The agent never has to manage cancel lists itself - the Go service does it.
- **Performance**: a clock-loop brain cycle averages ~500 ms (price fetch + indicators + decision). Between cycles the cooldown gate prevents overtrading.
- **Guardrails**: Swiftward policy is the enforcement layer; the agent cannot bypass SL requirements or concentration limits even if its own logic misbehaves.
