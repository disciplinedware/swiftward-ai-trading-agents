## ADDED Requirements

### Requirement: Each MCP server has a dedicated typed client class
The system SHALL provide five client classes in `src/agent/infra/`: `TradingMCPClient`, `PriceFeedMCPClient`, `FearGreedMCPClient`, `OnchainMCPClient`, and `NewsMCPClient`. Each class is constructed with a `base_url` and an optional `httpx.AsyncClient`.

#### Scenario: Client construction
- **WHEN** a client is instantiated with a base URL
- **THEN** it stores the URL (trailing slash stripped) and creates an `httpx.AsyncClient` if none is provided

### Requirement: Each client exposes a health_check method
Every client class SHALL implement `async def health_check() -> bool` that sends `GET /health` to the server. Returns `True` on HTTP 200, `False` on any other status or connection error (never raises).

#### Scenario: Server responds 200
- **WHEN** `GET /health` returns HTTP 200
- **THEN** `health_check()` returns `True`

#### Scenario: Server unreachable
- **WHEN** `GET /health` raises a connection error
- **THEN** `health_check()` returns `False` without propagating the exception

### Requirement: HTTP errors and JSON-RPC errors raise MCPError
All client methods (except `health_check`) SHALL raise `MCPError` when the HTTP response is non-200 or when the JSON-RPC response body contains an `"error"` key.

#### Scenario: Non-200 HTTP response
- **WHEN** a JSON-RPC call returns HTTP 4xx or 5xx
- **THEN** `MCPError` is raised with the status code in the message

#### Scenario: JSON-RPC error in response body
- **WHEN** the response body contains `{"error": {...}}`
- **THEN** `MCPError` is raised with the error details

### Requirement: TradingMCPClient provides get_portfolio and execute_swap
`TradingMCPClient` SHALL implement:
- `async def get_portfolio() -> PortfolioSnapshot` — calls `get_portfolio` tool, validates response with `PortfolioSnapshot.model_validate`
- `async def execute_swap(intent: TradeIntent) -> dict` — calls `execute_swap` tool with intent fields as params, returns raw result dict

#### Scenario: get_portfolio success
- **WHEN** trading-mcp returns a valid portfolio JSON
- **THEN** a `PortfolioSnapshot` is returned

#### Scenario: execute_swap success
- **WHEN** trading-mcp accepts the intent
- **THEN** the raw result dict (containing `status`, `tx_hash`, etc.) is returned

### Requirement: PriceFeedMCPClient returns dict[str, PriceFeedData]
`PriceFeedMCPClient` SHALL implement `async def get_prices(assets: list[str]) -> dict[str, PriceFeedData]`. It calls `get_indicators` (which includes prices, changes, and indicators) and maps the response into `PriceFeedData` instances keyed by asset symbol.

#### Scenario: Prices returned for all requested assets
- **WHEN** `get_prices(["BTC", "ETH"])` is called
- **THEN** a dict with keys `"BTC"` and `"ETH"` mapping to `PriceFeedData` instances is returned

### Requirement: FearGreedMCPClient returns FearGreedData
`FearGreedMCPClient` SHALL implement `async def get_index() -> FearGreedData` that calls `get_index` and maps the response to a `FearGreedData` instance.

#### Scenario: Index returned
- **WHEN** `get_index()` is called
- **THEN** a `FearGreedData` with `value`, `classification`, and `timestamp` is returned

### Requirement: OnchainMCPClient returns dict[str, OnchainData]
`OnchainMCPClient` SHALL implement `async def get_all(assets: list[str]) -> dict[str, OnchainData]` that calls `get_funding_rate`, `get_open_interest`, and `get_liquidations` in parallel and merges results per asset into `OnchainData` instances.

#### Scenario: Onchain data returned
- **WHEN** `get_all(["BTC", "ETH"])` is called
- **THEN** a dict with keys `"BTC"` and `"ETH"` mapping to merged `OnchainData` instances is returned

### Requirement: NewsMCPClient returns dict[str, NewsData]
`NewsMCPClient` SHALL implement `async def get_signals(assets: list[str]) -> dict[str, NewsData]` that calls `get_sentiment` and `get_macro_flag` in parallel and merges results per asset into `NewsData` instances.

#### Scenario: News signals returned
- **WHEN** `get_signals(["BTC"])` is called
- **THEN** a dict with `"BTC"` mapped to a `NewsData` instance containing `sentiment` and `macro_flag` is returned
