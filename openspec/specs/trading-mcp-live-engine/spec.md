## ADDED Requirements

### Requirement: LiveEngine implements the same interface as PaperEngine
`LiveEngine` SHALL expose `async execute_swap(intent: TradeIntent, current_price: Decimal) -> ExecutionResult` with identical input/output contract to `PaperEngine`.

#### Scenario: Same return type as PaperEngine
- **WHEN** `LiveEngine.execute_swap` completes (or raises)
- **THEN** on success it returns an `ExecutionResult` with `status`, `tx_hash`, `executed_price`, `slippage_pct`

### Requirement: LiveEngine raises when Risk Router is not configured
`LiveEngine` SHALL raise `RuntimeError("Risk Router address not configured")` if `config.chain.risk_router_address` is empty or equals the placeholder value `"0x..."`.

#### Scenario: Unconfigured Risk Router raises immediately
- **WHEN** `LiveEngine.execute_swap` is called with `risk_router_address=""` or `"0x..."`
- **THEN** `RuntimeError` is raised before any on-chain interaction

### Requirement: EIP-712 calldata construction
`LiveEngine` SHALL construct a valid EIP-712 typed-data payload for the TradeIntent struct using the configured chain ID, domain name `"TradingAgent"`, and version `"1"`.

#### Scenario: EIP-712 domain includes chain_id
- **WHEN** `_build_eip712_payload(intent)` is called
- **THEN** the returned domain dict contains `chainId` matching `config.chain.chain_id`

### Requirement: Transaction confirmation wait
`LiveEngine` SHALL wait up to 60 seconds for the submitted transaction to be mined. If the transaction is not confirmed within 60 seconds, the engine SHALL raise `TimeoutError`.

#### Scenario: Confirmed transaction returns tx hash
- **WHEN** the Risk Router transaction is mined within 60 seconds
- **THEN** `ExecutionResult.tx_hash` contains the on-chain tx hash (hex string)

#### Scenario: Unconfirmed transaction raises TimeoutError
- **WHEN** the transaction is not mined within 60 seconds
- **THEN** `TimeoutError` is raised

### Requirement: LiveEngine produces DB records via TradingService after confirmation
After `LiveEngine.execute_swap` returns a successful `ExecutionResult`, `TradingService` SHALL call `PortfolioService.record_open` or `record_close` to persist the trade to the database. The LiveEngine itself remains DB-free.

#### Scenario: Portfolio readable after live LONG
- **WHEN** a LONG executes successfully via LiveEngine
- **THEN** `get_portfolio` and `get_positions` return the new position

#### Scenario: Portfolio readable after live FLAT
- **WHEN** a FLAT executes successfully via LiveEngine
- **THEN** the closed position appears with realized PnL in `get_daily_pnl`
