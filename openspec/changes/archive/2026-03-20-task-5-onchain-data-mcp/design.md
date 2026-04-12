## Context

The agent brain's Stage 1 (Market Filter) requires on-chain signals: funding rates (15% weight), exchange netflow (15% weight), and supporting data for the Tier 2 liquidation-spike loop. Tasks 3 and 4 established the MCP server pattern (`price_feed_mcp`, `fear_greed_mcp`). This task follows the same infra/service/server layering used there.

## Goals / Non-Goals

**Goals:**
- Expose `get_funding_rate`, `get_open_interest`, `get_liquidations`, `get_netflow` via MCP JSON-RPC on port 8003
- Cover funding, OI, and liquidations with Binance Futures public API (no new API keys)
- Unblock Stage 1 and the Tier 2 liquidation loop without external paid dependencies

**Non-Goals:**
- Real exchange netflow data (deferred — requires CryptoQuant paid plan)
- Cross-exchange liquidation aggregation (Binance-only is sufficient as a spike detector)
- On-chain RPC calls (too complex, too slow for a 15-min loop)

## Decisions

### 1. Binance Futures for funding, OI, and liquidations

All three use the Binance Futures public REST API — no auth required, same HTTP client pattern as `price_feed_mcp`. This means a single `BinanceFuturesClient` in `infra/` covers 3 of 4 tools.

Alternatives considered:
- **Coinglass v4** for liquidations: paid only, no free tier with useful data — rejected
- **Bybit/OKX** for liquidations: adds complexity with a second client for marginal coverage improvement — rejected for hackathon scope

### 2. Netflow returns hardcoded neutral

`get_netflow` returns `{"BTC": {"direction": "neutral"}, "ETH": {"direction": "neutral"}}` with no upstream call. The brain treats `direction == "neutral"` as `netflow_score = 0.5` (no impact on health score).

Alternatives considered:
- **CryptoQuant free tier**: only covers Price OHLCV — no netflow data available
- **Funding rate as proxy**: would double-count funding signal (already 15% weight) — misleading
- **Blockchair DIY**: requires curated exchange address list — too unreliable for a signal

A TODO comment in the service points to `GET /v1/{btc,eth}/exchange-flows/netflow-total` for future CryptoQuant integration.

### 3. Cache TTLs

| Tool | TTL | Rationale |
|------|-----|-----------|
| `get_funding_rate` | 5 min | Funding settles every 8h — 5 min is more than fresh enough |
| `get_open_interest` | 5 min | OI trends over hours — 5 min cache has negligible staleness |
| `get_liquidations` | 5 min | 15-min window aggregated server-side; cache avoids redundant fan-out |
| `get_netflow` | — | Static response, no upstream call, no cache needed |

### 4. `get_open_interest` % change — 24h window

Binance Futures `GET /fapi/v1/openInterestHist` with `period=1d&limit=2` returns the last 2 daily snapshots. % change is computed server-side: `(latest - prev) / prev × 100`. This is the most interpretable window for the LLM ("OI up 12% in 24h = building conviction").

### 5. `get_liquidations` — Binance `allForceOrders`, 15-min window

`GET /fapi/v1/allForceOrders?symbol=BTCUSDT&startTime={now-15min}&endTime={now}` returns individual forced liquidation orders. Server aggregates: `liquidated_usd = sum(averagePrice × executedQty)`. Binance is ~35-40% of crypto futures volume — directionally correct as a spike detector.

### 6. Netflow scope — BTC and ETH only

On-chain flow data is meaningful at exchange-flow granularity only for BTC and ETH. `get_netflow` returns a fixed dict keyed by `"BTC"` and `"ETH"` — no `assets` parameter.

## Risks / Trade-offs

- **Binance-only liquidations** → cross-exchange liquidation cascades may be underrepresented. Mitigation: the signal is used for spike detection, not precise totals; directional accuracy is sufficient.
- **Neutral netflow** → 15% of the health score is always 0.5. Mitigation: in balanced markets this is a conservative bias; the LLM sees "data unavailable" and can contextualize accordingly. Swap in real data when CryptoQuant plan is obtained.
- **Binance Futures rate limits** → `allForceOrders` has a 20-weight cost per request, 2400 weight/minute limit. With 5-min cache and 10 assets, worst case is 10 requests per cache miss — well within limits.
