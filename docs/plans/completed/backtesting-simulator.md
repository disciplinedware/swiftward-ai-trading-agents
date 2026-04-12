# Backtesting

> **Status**: ✅ Shipped (two complementary mechanisms)

## What was built

The platform offers two ways to backtest trading strategies before letting them touch live capital:

1. **Ruby Arena** - a Rails-based orchestrator running multiple agent instances in parallel over historical data, with a web UI for batch creation, progress monitoring, and result inspection
2. **Claude Code ad-hoc backtests** - the Claude agent writes and runs Python code in its isolated sandbox, feeding on historical candles exported from the Market Data MCP

Together they cover both batch statistical evaluation (Arena) and rapid interactive iteration (Claude).

## 1. Ruby Arena (parallel strategy evaluation)

> **Source**: `ruby/agents/solid_loop_trading/`
> **URL**: http://localhost:7175
> **Runtime**: Rails 7 + GoodJob (ActiveJob) + SolidLoop framework

The Ruby Arena runs multiple agent instances concurrently against the same strategy to collect statistically meaningful performance numbers. It is the right tool when a strategy needs "does this work on average?" rather than "let me iterate one idea live."

### How a batch runs

A batch (`AgentBatch`) defines concurrency, per-strategy attempts, time bounds (`start_at`, `end_at`, `step_interval`), and the strategy prompt. When it starts, the orchestrator creates `AgentRun` records, each wrapping a `SolidLoop::Loop` - a conversational loop that pretends to live in simulated `virtual_time`. The orchestrator keeps `concurrency` runs active, starts new ones as others finish, and aggregates results when the batch completes.

Collected metrics include success rate, tokens used, model latency, tool reliance percentages, and per-run PnL, drawdown, Sharpe, and fills.

### Key files

- `ruby/agents/solid_loop_trading/app/models/agent_batch.rb` - batch model with `aggregate_stats` (success rate, avg tokens/sec, tool reliance) and `progress_stats` (completed/running)
- `ruby/agents/solid_loop_trading/app/models/agent_run.rb` - one agent execution, owns its `SolidLoop::Loop`, tracks `orchestrator_status` (initial/running/waiting/stopped) and `current_virtual_time`
- `ruby/agents/solid_loop_trading/app/controllers/agent_batches_controller.rb` - HTTP endpoints: create, start/stop, download results
- `ruby/agents/solid_loop_trading/app/controllers/agent_runs_controller.rb` - per-run inspection, MCP tool output trace
- `ruby/agents/solid_loop_trading/app/controllers/backtesting_data_controller.rb` - candle warehouse status and job triggers (`DownloadCandlesJob`, `ParseNewsJob`)
- `ruby/agents/solid_loop_trading/app/views/agent_batches/` - batch index, creation form, run tables, statistics
- `ruby/agents/solid_loop_trading/app/views/agent_runs/` - run detail with MCP call inspector and performance footer
- `ruby/agents/solid_loop_trading/spec/requests/backtesting_data_spec.rb` - request specs

### Strategies tested

Predefined strategies live as prompt files in `ruby/agents/solid_loop_trading/docs/prompts/`:

- `trend_follower.md`
- `quant_analyst.md`
- `fundamental_trader.md`

They are loaded into the DB during `bin/rails db:setup` and selectable in the batch creation UI. The Arena has historically been run against additional variants (Mean Reversion, Macro, Scalper, Portfolio Rebalancer) as screenshots on the landing page show.

## 2. Claude Code agent ad-hoc backtests

The Claude Code agent (Go harness, jailed in Docker) is often asked in prompts to "backtest strategy X over the last N days before committing to a trade." It does this without any special backtest MCP - just the tools it already has.

### How it works

1. User (or self-prompt) asks Claude to evaluate strategy X on pair Y over date range Z
2. Claude calls `market/get_candles` with `save_to_file=true`, which writes the candle series as CSV into the sandbox workspace and returns `{saved_to: "market/BTC-USDC_1h.csv", rows: 1440, ...}` (without pulling the whole series into the LLM context)
3. Claude calls `code/execute` to run Python in its sandbox - reads the CSV with `pandas`, computes returns/Sharpe/drawdown/etc.
4. Claude reads the output, iterates on parameters, and either saves a reusable script to `/workspace/scripts/` or proceeds to place a live trade

### Key files

- `golang/internal/mcps/codesandbox/service.go` - Code Sandbox MCP: per-agent persistent Python container, `code/execute`, workspace file I/O
- `golang/internal/mcps/marketdata/service.go` - `market/get_candles` with `save_to_file` flag, indicators (RSI, EMA, SMA, MACD, BBands, ATR, VWAP) computed server-side
- `golang/internal/mcps/files/service.go` - Files MCP for reading/writing workspace files
- `prompts/agent-gamma-claude/skills/strategy-update.md` - example skill prompt that tells agents to persist reusable backtest scripts in `/workspace/scripts/`

## Historical market data sources

Multiple historical data backends are available, selectable at runtime via the Market Data MCP's source chain:

| Source | Package | Notes |
|--------|---------|-------|
| Kraken | `golang/internal/marketdata/kraken/source.go` | Spot candles via Kraken public API |
| Binance | `golang/internal/marketdata/binance/source.go` | REST historical candles |
| Bybit | `golang/internal/marketdata/bybit/source.go` | Spot + futures |
| Simulated (GBM) | `golang/internal/marketdata/simulated/source.go` | Synthetic OHLCV using Geometric Brownian Motion; parameters: annualized vol (default 80%), history depth (default 500 candles per interval). All intervals (1m, 5m, 15m, 1h, 4h, 1d) supported. |

The Arena's `backtesting_data_controller.rb` dashboard surfaces which ticker symbols and candle counts are cached in the local warehouse and provides buttons to trigger downloads or news parsing jobs.

### Candle warehouse (real pagination)

The Arena maintains its own candle warehouse by paginating the Binance klines REST API. `DownloadCandlesJob` (`ruby/agents/solid_loop_trading/app/jobs/download_candles_job.rb`) fans out `BinanceOhlcDataFetcherJob` per ticker in `OhlcCandle::TICKERS`, each job loops with `limit=1000` and advancing `startTime` until it has covered the full window - currently 3 months of 5-minute candles. Results are upserted into the `ohlc_candles` Postgres table (unique on `(symbol, timestamp)`). Arena batches read from this table directly, so backtests can span the full 3-month history without going through the agent-facing Market Data MCP.

## Notes

- **Ruby Arena** is best for statistical testing: multiple agent instances, multiple attempts, aggregate metrics. UI shows real-time progress.
- **Claude ad-hoc** is best for iterative strategy design and rapid exploration. No UI - just prompts.
- **Simulated data** is great for reproducible unit tests and offline development but will not match real market microstructure.
- **save_to_file** on `market/get_candles` keeps large candle series out of the LLM context window, which is what makes backtesting from a Claude agent practical at all.
- The Arena schema supports both backtesting mode (historical data) and live Swiftward mode (real trading) - same `AgentBatch` and `AgentRun` tables, different data sources.
