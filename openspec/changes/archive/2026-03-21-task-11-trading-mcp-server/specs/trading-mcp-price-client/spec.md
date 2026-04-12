## ADDED Requirements

### Requirement: PriceClient fetches current prices from price_feed_mcp
`PriceClient` SHALL call the `get_prices_latest` MCP tool on `price_feed_mcp` via `POST /mcp` JSON-RPC and return a `dict[str, Decimal]` mapping asset symbol to current price.

#### Scenario: Returns prices as Decimal
- **WHEN** `get_prices_latest(["ETH", "BTC"])` is called and price_feed_mcp responds with `{"ETH": "2000.00", "BTC": "50000.00"}`
- **THEN** `PriceClient.get_prices_latest(["ETH", "BTC"])` returns `{"ETH": Decimal("2000.00"), "BTC": Decimal("50000.00")}`

### Requirement: PriceClient fetches a single asset price
`PriceClient` SHALL expose `get_price(asset: str) -> Decimal` as a convenience wrapper around `get_prices_latest([asset])`.

#### Scenario: Single price lookup
- **WHEN** `get_price("ETH")` is called
- **THEN** the returned value is `Decimal` equal to the price returned by `price_feed_mcp` for ETH

### Requirement: PriceClient propagates upstream errors
If `price_feed_mcp` returns a JSON-RPC error response or the HTTP call fails, `PriceClient` SHALL raise `MCPError` with the upstream error message.

#### Scenario: Upstream error raises MCPError
- **WHEN** `price_feed_mcp` responds with a JSON-RPC error object
- **THEN** `PriceClient` raises `MCPError` containing the error message
