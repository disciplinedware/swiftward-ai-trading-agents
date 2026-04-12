## ADDED Requirements

### Requirement: price_feed MCP server runs on port 8001
The system SHALL expose a FastMCP server at `POST /mcp` (stateless HTTP, MCP JSON-RPC 2.0) and `GET /health` on port 8001.

#### Scenario: Health check responds
- **WHEN** `GET /health` is called
- **THEN** response is `200 OK` with `{"status": "ok"}`

### Requirement: get_prices_latest tool
The server SHALL expose a `get_prices_latest` tool accepting `assets: list[str]` (short format: `["BTC", "ETH"]`) and returning a dict mapping each asset to its current price as a `str`-encoded Decimal.

Internally maps `BTC → BTCUSDT` (hardcoded USDT suffix) and fetches from Binance `GET /api/v3/ticker/price`.

#### Scenario: Returns current prices
- **WHEN** `get_prices_latest(assets=["BTC", "SOL"])` is called
- **THEN** response contains `{"BTC": "<price_str>", "SOL": "<price_str>"}` with valid decimal strings

#### Scenario: Unknown asset raises error
- **WHEN** `get_prices_latest(assets=["FAKE"])` is called and Binance returns an error
- **THEN** the tool raises an `MCPError` with a descriptive message

### Requirement: get_prices_change tool
The server SHALL expose a `get_prices_change` tool accepting `assets: list[str]` and returning a nested dict: asset → window → `str`-encoded percentage change. Windows: `1m`, `5m`, `1h`, `4h`, `24h`.

- `1m`: from last 2 candles of 1m klines
- `5m`: from last 2 candles of 5m klines
- `1h`, `4h`, `24h`: derived from 1h klines (last 2, 5, 25 candles respectively)

#### Scenario: Returns change across all windows
- **WHEN** `get_prices_change(assets=["ETH"])` is called
- **THEN** response contains `{"ETH": {"1m": "...", "5m": "...", "1h": "...", "4h": "...", "24h": "..."}}` with all five windows populated as decimal strings

### Requirement: get_indicators tool
The server SHALL expose a `get_indicators` tool accepting `assets: list[str]` and returning a nested dict: asset → indicator name → value (as `str` for numeric, `bool` for flags).

Indicators returned per asset:
- `rsi_14`: RSI 14-period on 15m candles
- `ema_20`, `ema_50`: EMA 20/50 on 15m candles
- `ema_200`: EMA 200 on 1h candles
- `atr_14`: ATR 14-period on 15m candles
- `bb_upper`, `bb_mid`, `bb_lower`: Bollinger Bands 20-period on 15m candles
- `volume_ratio`: current candle volume / 20-period average volume on 15m candles

#### Scenario: Returns all indicators for requested assets
- **WHEN** `get_indicators(assets=["BTC"])` is called
- **THEN** response contains `{"BTC": {"rsi_14": "...", "ema_20": "...", ...}}` with all 9 indicator keys present

### Requirement: Server starts with a single uv command
The server SHALL be startable with `uv run python src/price_feed_mcp/server.py`. The `server.py` module SHALL have an `if __name__ == "__main__":` block that calls `mcp.run(transport="streamable-http", host="0.0.0.0", port=8001)`.

#### Scenario: Server starts from repo root
- **WHEN** `uv run python src/price_feed_mcp/server.py` is executed from the `python/` directory
- **THEN** the server starts and listens on port 8001

### Requirement: Redis caching with 30s TTL
The server SHALL cache raw Binance kline responses in Redis with 30s TTL. Cache key format: `price_feed:{symbol}:{interval}` (e.g., `price_feed:BTCUSDT:15m`). On cache hit, Binance is NOT called. On Redis unavailability, the server SHALL fall through to Binance directly.

#### Scenario: Cache hit prevents Binance call
- **WHEN** `get_prices_latest` is called twice within 30 seconds for the same asset
- **THEN** Binance is called only once; the second call returns the cached response

#### Scenario: Cache miss after TTL triggers re-fetch
- **WHEN** a cached value expires (TTL elapsed)
- **THEN** the next call fetches fresh data from Binance and updates the cache

### Requirement: Symbol mapping
The server SHALL map short asset names (`BTC`, `ETH`) to Binance symbol format by appending `USDT` (hardcoded, regardless of config stablecoin value).

#### Scenario: BTC maps to BTCUSDT
- **WHEN** any tool is called with `assets=["BTC"]`
- **THEN** the Binance client uses symbol `"BTCUSDT"` in its requests
