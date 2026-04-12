## ADDED Requirements

### Requirement: Config and logger initialize on startup
The agent process SHALL load `config/config.yaml` via `get_config()` and initialize the structlog logger before any other work begins.

#### Scenario: Valid config file present
- **WHEN** the agent starts with a valid `config/config.yaml`
- **THEN** config is loaded and logger is initialized without error

#### Scenario: Missing config file
- **WHEN** `config/config.yaml` does not exist
- **THEN** the process raises `ConfigError` and exits before any MCP connection is attempted

### Requirement: All MCP servers are health-checked in parallel on startup
The agent SHALL send `GET /health` to all five MCP server URLs concurrently. If any server returns a non-200 response or raises a connection error, the agent SHALL log an error identifying the failing server and exit with a non-zero status code.

#### Scenario: All servers healthy
- **WHEN** all five `GET /health` requests return HTTP 200
- **THEN** startup continues to portfolio load

#### Scenario: One server unhealthy
- **WHEN** any `GET /health` request returns non-200 or raises a connection error
- **THEN** the agent logs which server failed and exits with status code 1

### Requirement: Portfolio is loaded from trading-mcp on startup
After health checks pass, the agent SHALL call `get_portfolio` on `TradingMCPClient` and log the current open position count and balance.

#### Scenario: Successful portfolio load
- **WHEN** `get_portfolio` returns successfully
- **THEN** open position count and stablecoin balance are logged at INFO level

### Requirement: All trigger loops run concurrently
The agent SHALL start the clock loop and three noop placeholder loops via `asyncio.gather`. The process runs until interrupted.

#### Scenario: Normal operation
- **WHEN** startup completes successfully
- **THEN** `asyncio.gather` is called with clock loop and three noop coroutines, running indefinitely
