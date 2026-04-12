# python

Python subsystem: one orchestrated trading agent and five MCP servers that expose market data, news, on-chain signals, the Fear & Greed index, and trade execution.

## What's inside

- **py-agent** — orchestrated trading agent (3-stage brain + event-driven trigger loops).
- **py-price-feed-mcp** — OHLCV and technical indicators (Binance).
- **py-news-mcp** — crypto news, sentiment scoring, macro event classification (CryptoPanic, CoinDesk).
- **py-onchain-data-mcp** — funding rates, open interest, liquidations (Binance Futures).
- **py-fear-greed-mcp** — Alternative.me Fear & Greed index with daily cache.
- **py-trading-mcp** — trade execution, portfolio accounting, and ERC-8004 registry hooks.

## Key files

### py-agent

- [`src/agent/brain/deterministic_llm.py`](src/agent/brain/deterministic_llm.py) — deterministic + LLM hybrid brain: math for filter/rotation/sizing, LLM for narrative context. See [`docs/brain.md`](docs/brain.md) for the 3-stage pipeline walkthrough.
- [`src/agent/brain/llm_client.py`](src/agent/brain/llm_client.py) — LLM client used by the brain.
- [`src/agent/trigger/clock.py`](src/agent/trigger/clock.py) — 15-minute scheduled analysis loop.
- [`src/agent/trigger/price_spike.py`](src/agent/trigger/price_spike.py) — 1-minute price-spike trigger.
- [`src/agent/trigger/tier2.py`](src/agent/trigger/tier2.py) — 5-minute tier-2 analysis loop.
- [`src/agent/trigger/exit_watchdog.py`](src/agent/trigger/exit_watchdog.py) — stop-loss / take-profit exit watchdog.

### py-price-feed-mcp

- [`src/price_feed_mcp/service/price_feed.py`](src/price_feed_mcp/service/price_feed.py) — OHLCV orchestration and caching.
- [`src/price_feed_mcp/service/indicators.py`](src/price_feed_mcp/service/indicators.py) — RSI, EMA, MACD, Bollinger, ATR, VWAP.
- [`src/price_feed_mcp/infra/binance.py`](src/price_feed_mcp/infra/binance.py) — Binance HTTP client.

### py-news-mcp

- [`src/news_mcp/service/news.py`](src/news_mcp/service/news.py) — headline aggregation, deduplication, event classification.
- [`src/news_mcp/service/llm.py`](src/news_mcp/service/llm.py) — LLM sentiment scoring.
- [`src/news_mcp/infra/cryptopanic.py`](src/news_mcp/infra/cryptopanic.py) — CryptoPanic feed client.
- [`src/news_mcp/infra/coindesk.py`](src/news_mcp/infra/coindesk.py) — CoinDesk feed client.

### py-onchain-data-mcp

- [`src/onchain_data_mcp/service/onchain_data.py`](src/onchain_data_mcp/service/onchain_data.py) — funding, OI, liquidation, netflow aggregation.
- [`src/onchain_data_mcp/infra/binance_futures.py`](src/onchain_data_mcp/infra/binance_futures.py) — Binance Futures HTTP client.

### py-fear-greed-mcp

- [`src/fear_greed_mcp/service/fear_greed.py`](src/fear_greed_mcp/service/fear_greed.py) — daily-cache wrapper over the index.
- [`src/fear_greed_mcp/infra/alternative_me.py`](src/fear_greed_mcp/infra/alternative_me.py) — Alternative.me HTTP client.

### py-trading-mcp

- [`src/trading_mcp/service/trading_service.py`](src/trading_mcp/service/trading_service.py) — order submission, validation, execution routing.
- [`src/trading_mcp/service/portfolio_service.py`](src/trading_mcp/service/portfolio_service.py) — positions, P&L, and portfolio state.
- [`src/trading_mcp/engine/live.py`](src/trading_mcp/engine/live.py) — live execution engine.
- [`src/trading_mcp/erc8004/registry.py`](src/trading_mcp/erc8004/registry.py) — ERC-8004 AgentRegistry, ValidationRegistry, ReputationRegistry hooks.
