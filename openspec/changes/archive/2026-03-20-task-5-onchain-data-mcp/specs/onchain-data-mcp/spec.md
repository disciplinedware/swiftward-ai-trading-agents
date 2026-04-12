## ADDED Requirements

### Requirement: onchain_data_mcp server
The system SHALL provide a FastMCP server at `src/onchain_data_mcp/` running on port 8003, exposing four tools via MCP JSON-RPC (`POST /mcp`): `get_funding_rate`, `get_open_interest`, `get_liquidations`, `get_netflow`.

The server SHALL follow the infra/service/server layering used by `price_feed_mcp`:
- `infra/binance_futures.py` — async HTTP client for Binance Futures public REST API
- `service/onchain_data.py` — `OnchainDataService` class with cache logic
- `server.py` — FastMCP setup; tool handlers are one-liners delegating to the service

`server.py` SHALL expose a `/health` route returning `{"status": "ok"}`.

#### Scenario: Health check
- **WHEN** `GET /health` is called on the running server
- **THEN** the response is `{"status": "ok"}` with HTTP 200

#### Scenario: MCP tool discovery
- **WHEN** an MCP client connects and lists tools
- **THEN** all four tools (`get_funding_rate`, `get_open_interest`, `get_liquidations`, `get_netflow`) are present

---

### Requirement: get_funding_rate tool
The tool SHALL accept `assets: list[str]` and return a dict mapping each asset symbol to its current funding rate data from Binance Futures `GET /fapi/v1/premiumIndex`.

Response per asset:
- `funding_rate: str` — current funding rate as a Decimal string (can be negative)
- `annualized_pct: str` — `funding_rate × 3 × 365 × 100` as a Decimal string
- `next_funding_time: str` — ISO 8601 UTC timestamp

Results SHALL be cached in Redis with a 5-minute TTL per asset key `"onchain:funding:{asset}"`.

#### Scenario: Positive funding rate
- **WHEN** `get_funding_rate(["BTC"])` is called and Binance returns `funding_rate = "0.000100"`
- **THEN** the response contains `funding_rate = "0.000100"` and `annualized_pct = "10.95"`

#### Scenario: Negative funding rate
- **WHEN** `get_funding_rate(["BTC"])` is called and Binance returns `funding_rate = "-0.000300"`
- **THEN** the response contains `funding_rate = "-0.000300"` and `annualized_pct = "-32.85"`

#### Scenario: Cache hit
- **WHEN** `get_funding_rate` is called twice within 5 minutes for the same asset
- **THEN** the second call returns cached data without making a Binance HTTP request

---

### Requirement: get_open_interest tool
The tool SHALL accept `assets: list[str]` and return a dict mapping each asset to its open interest and 24h change from Binance Futures.

The implementation SHALL fetch the last 2 daily snapshots via `GET /fapi/v1/openInterestHist?symbol={symbol}&period=1d&limit=2` and compute the % change server-side.

Response per asset:
- `oi_usd: str` — latest open interest in USD as a Decimal string
- `change_pct_24h: str` — `(latest - previous) / previous × 100` as a Decimal string (can be negative)

Results SHALL be cached in Redis with a 5-minute TTL per asset key `"onchain:oi:{asset}"`.

#### Scenario: OI increasing
- **WHEN** previous OI = 10,000,000 and latest OI = 11,200,000
- **THEN** `change_pct_24h = "12.00"`

#### Scenario: OI decreasing
- **WHEN** previous OI = 10,000,000 and latest OI = 9,500,000
- **THEN** `change_pct_24h = "-5.00"`

---

### Requirement: get_liquidations tool
The tool SHALL accept `assets: list[str]` and return a dict mapping each asset to aggregated liquidation data for the last 15-minute window, sourced from Binance Futures `GET /fapi/v1/allForceOrders`.

The server SHALL aggregate individual forced liquidation orders: `liquidated_usd = sum(averagePrice × executedQty)` for orders within `[now - 15min, now]`.

Response per asset:
- `liquidated_usd_15m: str` — total USD liquidated in the window
- `long_liquidated_usd: str` — USD from long liquidations (side = "SELL")
- `short_liquidated_usd: str` — USD from short liquidations (side = "BUY")

Results SHALL be cached in Redis with a 5-minute TTL per asset key `"onchain:liq:{asset}"`.

#### Scenario: Mixed liquidations
- **WHEN** Binance returns 2 orders: a long liquidation of $3,200,000 and a short of $1,300,000
- **THEN** `liquidated_usd_15m = "4500000"`, `long_liquidated_usd = "3200000"`, `short_liquidated_usd = "1300000"`

#### Scenario: No liquidations
- **WHEN** Binance returns an empty list for the 15-min window
- **THEN** all USD values are `"0"`

---

### Requirement: get_netflow tool
The tool SHALL accept no parameters and return a hardcoded neutral netflow dict for BTC and ETH.

Response:
```json
{
  "BTC": {"direction": "neutral", "note": "netflow data unavailable"},
  "ETH": {"direction": "neutral", "note": "netflow data unavailable"}
}
```

The implementation SHALL contain a TODO comment referencing the CryptoQuant endpoint (`GET /v1/{btc,eth}/exchange-flows/netflow-total`) for future integration when a paid plan is available. No upstream API call SHALL be made. No Redis cache is needed.

#### Scenario: Neutral response returned
- **WHEN** `get_netflow()` is called
- **THEN** the response contains `"BTC"` and `"ETH"` keys, each with `direction = "neutral"`

#### Scenario: No HTTP call made
- **WHEN** `get_netflow()` is called
- **THEN** no outbound HTTP requests are made
