# Polymarket Integration (Prediction Markets MCP)

> **Status**: ✅ Shipped (v1, read-only)
> **Package**: `golang/internal/mcps/polymarket/`
> **MCP endpoint**: `POST /mcp/polymarket` (via MCP Gateway)

## What was built

A lean, read-only prediction-markets MCP exposing Polymarket events and markets to trading agents. Two tools - `search_markets` for discovery and `get_market` for deep analysis - let agents fold crowd-wisdom signals (crypto odds, geopolitics, politics) into their trading decisions. No Polymarket auth, no wallet, no order placement. Just fast, structured, LLM-readable reads.

## MCP tools

| Tool | What it does | File:line |
|------|--------------|-----------|
| `polymarket/search_markets` | Fetch events grouped by category (Crypto, Geopolitics, Politics, ...), filter by free-text query, sort by volume or recency. Returns up to 20 events; each event carries its inline markets with current odds, 24h volume, liquidity, and time-to-close. Near-resolved markets (>99% or <1%) are stripped so the agent only sees decision-relevant signal. | `service.go:63-171` |
| `polymarket/get_market` | Full deep-dive on one market: description, resolution criteria, current odds, order book snapshot (best bid/ask with depth summary), volumes (24h / 7d / total), fees, and all sibling markets in the parent event. Composes 3-4 parallel API calls (Gamma market + CLOB book + parent event) into one structured response. | `service.go:89-259` |

## Data sources

- **Gamma API** (`gamma.go:1-224`): Polymarket's public REST for market metadata. Endpoints used: `/markets` (list), `/markets/{id}` (detail), `/events` (discovery), `/events/{id}` (event with sibling markets). A custom `UnmarshalJSON` (`gamma.go:43`) normalizes Polymarket's quirk of encoding arrays as JSON-inside-strings (e.g. `OutcomePrices: "[\"0.23\",\"0.77\"]"`).
- **CLOB API** (`clob.go:1-64`): read-only order book snapshots via `GET /book?token_id={tokenID}`. The formatter (`format.go:185-211`) compresses ladders into "Best bid: $X | Depth within 2c: $Y" lines for LLM readability - agents do not need raw order book data.

## Key files

- `service.go:1-270` - MCP server entry point. Registers tools, dispatches tool calls, composes API calls. `ToolSearchMarkets` (line 130) does query matching + category filtering + limit. `ToolGetMarket` (line 223) fetches market + book + parent event.
- `gamma.go:1-224` - Gamma REST client. `ListEvents` (line 184), `GetMarket` (line 157), `GetEvent` (line 166). Custom `UnmarshalJSON` (line 43) handles the string-encoded arrays.
- `clob.go:1-64` - CLOB order book client. Single method: `GetBook` (line 42) returning the bid/ask ladder for one token.
- `format.go:1-252` - response formatting. `formatEventResults` (line 14) groups markets by event. `formatMarketDetail` (line 43) structures the deep-dive output. `summarizeBook` (line 185) extracts best bid/ask + depth buckets.

## Tests

- `service_test.go:1-248` - unit tests against a mock Gamma server. `TestToolSearchMarkets` (line 116) exercises query matching, category filtering, limits, and near-resolved filtering. `TestFilterEvents` (line 193) checks text search across event titles and market questions. Table-driven per repo convention.
- `integration/gamma_integration_test.go:1-80` - real Gamma API tests, flagged `//go:build integration`. `TestGammaListMarkets` (line 19) validates that `Outcomes`, `OutcomePrices`, and `ClobTokenIDs` all decode correctly against live data. Skips gracefully on network failure so CI does not break.
- `integration/search_integration_test.go:1-80` - real Gamma tests at the MCP tool layer. `TestSearchMarkets_NoFilter_ReturnsEvents` (line 26) and `TestSearchMarkets_CryptoCategory_ReturnsCryptoEvents` (line 49) verify tool output against live API.

## Integration points

Exposed at `POST /mcp/polymarket` through the MCP Gateway (`swiftward-server:8095`). Guardrails are intentionally disabled for this MCP: the tools are read-only, stateless, and do not touch capital. Category filters map to Gamma tag IDs (e.g. "Crypto" → tag_id "21", "Geopolitics" → "100265") in `service.go:118-126`.

## Notes

- **v1 is read-only by design**. `place_order`, `cancel_orders`, and `get_portfolio` are not shipped. The hackathon goal was "Polymarket signals for Kraken trading", not "trade on Polymarket". Order placement and wallet integration are future work.
- **Order book is YES-side only**. `get_market` fetches the YES token's book (`clobTokenIDs[0]`). NO side and multi-outcome markets are not fully surfaced.
- **No price history** - CLOB `/prices-history` is not integrated. Agents cannot compute on-chain volatility or short-term trend from this MCP.
- **Event grouping** mirrors Gamma's hierarchy: `search_markets` returns events (not flat markets), with up to 5 inline markets per event plus a "+N more markets" hint for larger events.
