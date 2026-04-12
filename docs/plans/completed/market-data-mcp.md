# Market Data MCP

> **Status**: ✅ Shipped
> **Package**: `golang/internal/mcps/marketdata/`
> **Source implementations**: `golang/internal/marketdata/{kraken,binance,bybit,simulated}/`
> **Endpoint**: `POST /mcp/market`

## What was built

A unified, multi-source market data MCP providing read-only access to prices, OHLCV candles with server-side indicators, orderbook depth, perpetual funding rates, open interest, and price / funding / volume alerts. Agents trade on one venue but can analyse many - data is sourced from Kraken, Binance, Bybit, or simulated GBM, with an automatic fallback chain. Server-side indicators (RSI, MACD, EMA, Bollinger Bands, ATR, VWAP) are computed in Go so agents don't waste context on arithmetic. The `save_to_file` pattern writes large candle datasets straight to the workspace so the Python sandbox can read them without inflating the LLM context.

## MCP tools

| Tool | Inputs | Outputs | File:line |
|------|--------|---------|-----------|
| `market/get_prices` | `markets: []string` | bid/ask/last, 24h volume, 24h change, high/low per market | `service.go:528` |
| `market/get_candles` | `market, interval, limit, end_time?, format?, save_to_file?, indicators?` | JSON candles (+indicators) or CSV text or `{saved_to, rows, columns}` | `service.go:554` |
| `market/list_markets` | `quote?, sort_by?, limit?` | tradeable markets with 24h stats | `service.go:787` |
| `market/get_orderbook` | `market, depth?` | bids/asks ladders, spread, imbalance | `service.go:834` |
| `market/get_funding` | `market, limit?` | current funding rate, annualized, interval, signal, history | `service.go:900` |
| `market/get_open_interest` | `market` | OI and 1h/4h/24h change, long-short ratio | `service.go:956` |
| `market/set_alert` | `market, condition, value, window?, note?` | `{alert_id, status: "active"}` | `service.go:988` |
| `market/cancel_alert` | `alert_id` | `{success: true}` | `service.go:1066` |

**Optional PRISM tools** (when `PRISM_ENABLED=true`): `market/get_fear_greed`, `market/get_technicals` (daily TA panel), `market/get_signals` (AI signal summary per asset).

## Sources and fallback chain

`ChainSource` (`golang/internal/marketdata/chain.go`) tries sources in order and falls back on error.

| Source | Candles | Orderbook | Funding | OI | Notes |
|--------|---------|-----------|---------|-----|-------|
| Kraken | up to 720 | yes | yes | yes | default primary |
| Binance | up to 1000 | yes | yes | yes | deep archive |
| Bybit | up to 200 | yes | yes | yes | spot ticker has a hardcoded "use binance" error; chain falls through |
| Simulated (GBM) | on-demand | synthetic 20-level book | oscillating around 0 | random walk | dev / demo mode only |

The active chain is configured via `TRADING__MARKET_DATA__SOURCES` (default `kraken,bybit`). In simulated mode, all sources read from the same shared `exchange.Client` so prices stay consistent with trading fills.

## Server-side indicators

`IndicatorEngine` (`golang/internal/mcps/marketdata/indicators.go:212`) computes:

- `rsi_<period>` - Wilder's Relative Strength Index
- `ema_<period>` - Exponential Moving Average
- `sma_<period>` - Simple Moving Average
- `macd` or `macd_<fast>_<slow>_<signal>` - MACD with histogram (`macd`, `macd_signal`, `macd_hist`)
- `bbands` or `bbands_<period>_<stddev>` - Bollinger Bands (`bb_upper`, `bb_middle`, `bb_lower`)
- `atr_<period>` - Average True Range
- `vwap` - cumulative Volume Weighted Average Price

Values are decimal strings with empty string for warm-up periods. The engine automatically fetches extra candles to warm up slow indicators and then trims them before returning.

## Caching

`Cache` (`golang/internal/mcps/marketdata/cache.go`) keeps closed candles in memory keyed by `MARKET:INTERVAL`:

- Closed candles are immutable - full cache hits, no TTL
- Live requests (no `end_time`) invalidate and re-fetch when a new candle closes
- Historical requests (with `end_time`) never use the cache
- An exhaustion heuristic marks a source as unable to serve a requested window and skips the cache until a larger limit is requested

## Alerts

`set_alert` creates a server-side alert polled roughly every 10 seconds by `runAlertPoller` (`service.go:93`). Supported conditions:

- `above` / `below` - price crosses threshold
- `change_pct` - absolute % move from reference price at creation
- `volume_spike` - |24h change %| exceeds value (proxy for unusual activity)
- `funding_threshold` - |funding rate| >= value
- `oi_change_pct` - |1h OI change| exceeds value

Alerts are stored in PostgreSQL with deterministic IDs from `(agent_id, market, condition, value, window)` to prevent duplicates. When an alert fires, it is marked `triggered` with the trigger price and fed back into the agent via the same path as Telegram injection. Alerts are scoped by `X-Agent-ID` header.

## Key files

- `golang/internal/mcps/marketdata/service.go` - MCP service, tool handlers, alert poller
- `golang/internal/mcps/marketdata/indicators.go` - indicator parsing and computation
- `golang/internal/mcps/marketdata/cache.go` - candle cache with exhaustion tracking
- `golang/internal/mcps/marketdata/alerts.go` - alert condition validation
- `golang/internal/mcps/marketdata/format.go` - CSV/JSON formatting
- `golang/internal/marketdata/source.go` - `DataSource` interface
- `golang/internal/marketdata/chain.go` - fallback chain
- `golang/internal/marketdata/kraken/source.go` - Kraken adapter
- `golang/internal/marketdata/binance/source.go` - Binance adapter
- `golang/internal/marketdata/bybit/source.go` - Bybit adapter
- `golang/internal/marketdata/simulated/source.go` - GBM synthesis

## Tests

- `golang/internal/mcps/marketdata/service_test.go` - tool handlers, cache behavior, alert lifecycle
- `golang/internal/mcps/marketdata/indicators_test.go` - indicator math, warm-up trimming
- `golang/internal/marketdata/{kraken,binance,bybit}/source_test.go` - per-provider unit tests
- `golang/internal/marketdata/{kraken,binance,bybit}/integration/*_test.go` - optional live integration tests

## Notes

- **Why `save_to_file` matters**: 1,000 one-minute candles is 50K+ tokens inline. With `save_to_file=true`, the tool writes CSV to `{workspace}/{agent_id}/market/{market}_{interval}.csv` and returns only the path. The agent's Python sandbox reads the CSV directly via pandas.
- **Simulated consistency**: in demo mode, a filled trade updates the simulated price, and the next `market/get_prices` reflects it. Agents never see price divergence between their trades and the market.
- **Bybit spot ticker quirk**: Bybit's spot endpoint returns a hardcoded "use binance" error. The chain handles this transparently by falling through to the next source. Binance was deliberately not added as a fallback (to avoid making it required) - if Kraken is down and Bybit can't serve spot, the chain correctly fails loud.
- **Agent-facing cap vs backtesting warehouse**: the MCP's `market/get_candles` caps `limit` at 720 per call - that is the right bound for an agent tool invoked inside a live trading loop. Deep historical data for backtesting is handled separately by the Ruby Arena's `BinanceOhlcDataFetcherJob`, which paginates the Binance klines REST API with `limit=1000` over arbitrary date ranges (currently 3 months of 5-minute candles) and writes them to the `ohlc_candles` table. Arena batches read from that warehouse directly; they never hit the MCP for history.
- **Funding interval variability**: different perps have different intervals (1h / 4h / 8h). `funding_interval_h` is returned along with `annualized_pct` so downstream math stays correct.
