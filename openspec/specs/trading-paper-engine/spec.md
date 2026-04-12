## ADDED Requirements

### Requirement: LONG open fills at entry + slippage
The system SHALL fill a LONG TradeIntent at `current_price × 1.001` (0.1% slippage) and return an `ExecutionResult` with `status="executed"`, a `tx_hash` starting with `"paper_"`, `executed_price`, `slippage_pct=0.001`, and `size_usd` set to the filled USD amount. It SHALL NOT write any database rows — all DB writes are handled by `PortfolioService.record_open` after this call returns.

#### Scenario: LONG fill price includes slippage
- **WHEN** current_price=2000 and intent.action="LONG"
- **THEN** executed_price=2002.0 and slippage_pct=0.001

#### Scenario: LONG does not write to DB
- **WHEN** execute_swap is called with a valid LONG intent
- **THEN** no Position, Trade, or PortfolioSnapshot rows are written by the engine

#### Scenario: LONG returns size_usd
- **WHEN** execute_swap fills a LONG with stablecoin_balance=10000 and size_pct=0.1
- **THEN** result.size_usd=1000.0

### Requirement: FLAT close fills at current price
The system SHALL close the open position for the intent's asset at exactly `current_price` (no slippage on exits) and return an `ExecutionResult` with `status="executed"` and a `tx_hash` starting with `"paper_"`. It SHALL NOT write any database rows — all DB writes are handled by `PortfolioService.record_close` after this call returns.

#### Scenario: FLAT PnL calculation is deferred to PortfolioService
- **WHEN** execute_swap is called with action="FLAT"
- **THEN** the engine returns executed_price=current_price and performs no DB writes

#### Scenario: FLAT with no open position returns rejected
- **WHEN** no open position exists for the asset (engine checks via passed-in position info)
- **THEN** status="rejected", no DB writes

### Requirement: PaperEngine has no PortfolioService dependency
`PaperEngine` SHALL be constructable without a `PortfolioService`, `session_factory`, `starting_balance_usdc`, or `max_concurrent_positions` argument. These concerns belong to `PortfolioService`.

#### Scenario: PaperEngine constructor takes no service deps
- **WHEN** PaperEngine is instantiated
- **THEN** no PortfolioService, session_factory, or balance config is required

### Requirement: ExecutionResult returned on every call
Every call to execute_swap SHALL return an ExecutionResult with status ("executed" | "rejected"), tx_hash (paper_<uuid4> or ""), executed_price, slippage_pct, and reason.

#### Scenario: Successful execution result
- **WHEN** execute_swap succeeds
- **THEN** result.status="executed", result.tx_hash starts with "paper_"

#### Scenario: Rejected execution result
- **WHEN** execute_swap is rejected
- **THEN** result.status="rejected", result.tx_hash="", result.reason is non-empty

## REMOVED Requirements

### Requirement: Max concurrent positions enforced
**Reason**: Capacity enforcement moved to `PortfolioService.record_open` where it can be applied atomically for all engine types.
**Migration**: `TradingService` performs a pre-check via `portfolio_service.can_open_position()` before calling the engine, and `PortfolioService.record_open` enforces the limit atomically under the write lock.
