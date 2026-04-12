## ADDED Requirements

### Requirement: Engine is a pure execution boundary
An Engine implementation SHALL only be responsible for submitting a trade and returning the fill result. It SHALL NOT read from or write to the database, SHALL NOT hold a reference to `PortfolioService`, and SHALL NOT build domain objects (Position, Trade, PortfolioSnapshot).

#### Scenario: Engine has no DB dependency
- **WHEN** an Engine implementation is constructed
- **THEN** it requires no `session_factory`, no `PortfolioService`, and no DB connection

#### Scenario: Engine returns ExecutionResult only
- **WHEN** `execute_swap(intent, current_price)` completes
- **THEN** it returns an `ExecutionResult` and performs no side effects beyond trade submission

### Requirement: ExecutionResult carries optional filled size
`ExecutionResult` SHALL include an optional `size_usd: Decimal | None` field representing the actual USD size filled. Engines that know the filled size at execution time (e.g., `PaperEngine`) SHALL populate it. Engines that do not (e.g., `LiveEngine` with on-chain fills) SHALL leave it as `None`.

#### Scenario: PaperEngine populates size_usd
- **WHEN** `PaperEngine.execute_swap` fills a LONG
- **THEN** `result.size_usd` equals `stablecoin_balance × intent.size_pct` at execution time

#### Scenario: LiveEngine leaves size_usd as None
- **WHEN** `LiveEngine.execute_swap` returns
- **THEN** `result.size_usd` is `None`
