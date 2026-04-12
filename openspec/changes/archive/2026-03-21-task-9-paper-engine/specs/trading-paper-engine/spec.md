## ADDED Requirements

### Requirement: LONG open fills at entry + slippage
The system SHALL fill a LONG TradeIntent at `current_price × 1.001` (0.1% slippage). It SHALL write a Position row (status="open"), a Trade row (direction="open"), and a PortfolioSnapshot row atomically via PortfolioService.

#### Scenario: LONG fill price includes slippage
- **WHEN** current_price=2000 and intent.action="LONG"
- **THEN** executed_price=2002.0 and slippage_pct=0.001

#### Scenario: LONG writes position and trade
- **WHEN** execute_swap is called with a valid LONG intent
- **THEN** a Position row with status="open" and a Trade row with direction="open" are persisted

### Requirement: FLAT close fills at current price
The system SHALL close the open position for the intent's asset at exactly `current_price` (no slippage on exits). It SHALL write status="closed" on the Position, a Trade row (direction="close"), and a PortfolioSnapshot, with realized_pnl_usd computed as `(exit_price - entry_price) / entry_price × size_usd`.

#### Scenario: FLAT PnL calculation
- **WHEN** entry_price=2000, size_usd=1000, exit_price=2200
- **THEN** realized_pnl_usd=100, realized_pnl_pct=0.10

#### Scenario: FLAT with no open position
- **WHEN** no open position exists for the asset
- **THEN** status="rejected", no DB writes

### Requirement: Max concurrent positions enforced
The system SHALL return status="rejected" with reason="max_concurrent_positions reached" if the open position count is already at the configured maximum when a LONG intent is submitted.

#### Scenario: At limit, LONG rejected
- **WHEN** max_concurrent_positions=2 and 2 positions are already open
- **THEN** status="rejected", no new Position or Trade row written

### Requirement: ExecutionResult returned on every call
Every call to execute_swap SHALL return an ExecutionResult with status ("executed" | "rejected"), tx_hash (paper_<uuid4> or ""), executed_price, slippage_pct, and reason.

#### Scenario: Successful execution result
- **WHEN** execute_swap succeeds
- **THEN** result.status="executed", result.tx_hash starts with "paper_"

#### Scenario: Rejected execution result
- **WHEN** execute_swap is rejected (limit or no position)
- **THEN** result.status="rejected", result.tx_hash="", result.reason is non-empty
