# Candle Fetching and save_to_file

> **Status**: ✅ Shipped
> **Package**: `golang/internal/mcps/marketdata/`, `golang/internal/marketdata/{kraken,binance,bybit,simulated}/`

## What was built

The `market/get_candles` MCP tool fetches OHLCV candles from multiple sources (Kraken, Binance, Bybit, Simulated) and returns them to agents with optional server-side indicators. The headline feature is `save_to_file=true`, which writes large candle datasets to the agent's workspace as CSV instead of embedding them inline in the LLM response - keeping deep history out of the context window and making real backtesting from a Claude Code agent practical.

## Tool shape

Inputs:

- `market` (string, required) - trading pair, e.g. `ETH-USDC`
- `interval` (string, required) - `1m`, `5m`, `15m`, `1h`, `4h`, `1d`
- `limit` (int, required) - number of closed candles (1-720)
- `end_time` (string, optional) - ISO-8601 timestamp; default is now
- `format` (string, optional) - `json` or `csv`; default `json`
- `save_to_file` (bool, optional) - write CSV to workspace instead of inline response
- `indicators` (array, optional) - server-side indicators: `rsi_14`, `ema_<N>`, `sma_<N>`, `macd`, `bbands`, `atr_<N>`, `vwap`

Output when `save_to_file=false`:

```json
{
  "market": "ETH-USDC",
  "interval": "1h",
  "count": 200,
  "source": "kraken",
  "candles": [{"t": "...", "o": "...", "h": "...", "l": "...", "c": "...", "v": "..."}],
  "indicators_computed": ["rsi_14"]
}
```

Output when `save_to_file=true`:

```json
{
  "saved_to": "/workspace/market/ETH-USDC_1h.csv",
  "market": "ETH-USDC",
  "interval": "1h",
  "rows": 500,
  "columns": ["timestamp", "open", "high", "low", "close", "volume", "rsi_14"],
  "updated_at": "2026-04-11T15:30:00Z"
}
```

## How it works

1. **Request validation**: `limit` is capped at 720 (Kraken's per-request max) unless the source permits more. Source-specific limits: Kraken 720, Binance 1000, Simulated 1000.
2. **Indicator warm-up**: if indicators are requested, the handler fetches extra candles at the start (e.g. SMA-200 needs 200 historical points). Total fetch is capped to avoid exceeding source limits. After computation, only the last `limit` candles are returned with their indicator columns.
3. **Caching**: live requests (no `end_time`) are cached; cache is invalidated when a new candle closes. Historical requests (with `end_time`) are never cached because they target a fixed window.
4. **File output**: when `save_to_file=true`, the handler writes CSV to `{workspace}/{agent_id}/market/{market}_{interval}.csv` and returns the relative path. The agent then passes this path to `code/execute` with `pd.read_csv(...)` to avoid context bloat.
5. **Data format**: OHLCV values are decimal strings (never float64). Timestamps are UTC. Only closed candles are returned.

## Source support

| Source | Package | Max per request | Notes |
|--------|---------|-----------------|-------|
| Kraken | `golang/internal/marketdata/kraken/source.go:367` | 720 | Supports `endTime` |
| Binance | `golang/internal/marketdata/binance/source.go:202` | 1000 | Filters open candles |
| Bybit | `golang/internal/marketdata/bybit/source.go` | - | Spot ticker returns a hardcoded error; candles are served via the next source in the chain |
| Simulated | `golang/internal/marketdata/simulated/source.go:139` | 1000 | GBM synthesis on demand |

The active source chain is configured via `TRADING__MARKET_DATA__SOURCES` (default `kraken,bybit`). Sources are tried in order; on failure, the chain falls through to the next source.

## Key files

- `golang/internal/mcps/marketdata/service.go:554` - `toolGetCandles` handler with caching and `save_to_file`
- `golang/internal/mcps/marketdata/service.go:759` - `saveCSVToWorkspace` writes CSV to the agent workspace
- `golang/internal/mcps/marketdata/cache.go` - candle cache with warm-up tracking
- `golang/internal/mcps/marketdata/indicators.go` - server-side indicator engine
- `golang/internal/mcps/marketdata/format.go` - OHLCV + indicator CSV formatting
- `golang/internal/marketdata/kraken/source.go:367` - Kraken `GetCandles` with 720 max
- `golang/internal/marketdata/binance/source.go:202` - Binance `GetCandles` with 1000 max and open-candle filtering
- `golang/internal/marketdata/simulated/source.go:139` - GBM synthesis

## Tests

- `golang/internal/mcps/marketdata/service_test.go` - cache hit/miss, indicator warm-up trimming, limit boundaries
- `golang/internal/mcps/marketdata/indicators_test.go` - indicator computation (RSI, EMA, SMA, MACD, BBands, ATR, VWAP)
- `golang/internal/marketdata/kraken/source_test.go` - Kraken source limit enforcement
- `golang/internal/marketdata/binance/source_test.go` - Binance open-candle filtering

## Notes

**Why `save_to_file` matters**: an agent working with 1,000+ candles (1-minute data for 7 days) would consume 50K+ tokens if candles were returned inline. `save_to_file` offloads storage to the workspace, the agent references it by path, and only loads it into memory inside `code/execute` during analysis. This is what makes backtesting from inside a Claude Code agent context-feasible.

**Indicators computed server-side**: rather than returning raw candles and having each agent reimplement RSI/EMA/MACD, the handler computes indicators at fetch time. This guarantees consistent math across all agents and keeps the agent prompts simpler.

**Two candle paths, two cap stories**:

1. **Agent live path** (Go Market Data MCP): `market/get_candles` caps `limit` at 720 per single call (`service.go:571`). A transparent multi-page fetch from an agent-facing tool is future work.
2. **Backtesting warehouse path** (Ruby Arena): the Ruby Arena maintains its own candle warehouse and downloads deep history by **real pagination over the Binance klines REST API**. `BinanceOhlcDataFetcherJob` (`ruby/agents/solid_loop_trading/app/jobs/binance_ohlc_data_fetcher_job.rb`) loops with `limit=1000` and advancing `startTime` until it covers the full date range - currently 3 months of 5-minute candles for all `OhlcCandle::TICKERS`, triggered by `DownloadCandlesJob` and stored in the `ohlc_candles` Postgres table for Arena backtests to read directly.

Paging across a multi-month history is the Arena's job; the agent-facing MCP tool stays simple and bounded.
