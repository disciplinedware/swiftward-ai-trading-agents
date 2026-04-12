## ADDED Requirements

### Requirement: PriceFeedMCPClient provides slim price-only fetch
`PriceFeedMCPClient` SHALL expose a `get_prices_latest(assets: list[str]) -> dict[str, Decimal]` method that fetches only current prices (no indicators, no change percentages) via a single `get_prices_latest` JSON-RPC call.

#### Scenario: Prices fetched for listed assets
- **WHEN** `get_prices_latest(["BTC", "ETH"])` is called
- **THEN** it SHALL return `{"BTC": Decimal("..."), "ETH": Decimal("...")}` with one entry per requested asset

#### Scenario: Empty asset list
- **WHEN** `get_prices_latest([])` is called
- **THEN** it SHALL return an empty dict without making any HTTP call
