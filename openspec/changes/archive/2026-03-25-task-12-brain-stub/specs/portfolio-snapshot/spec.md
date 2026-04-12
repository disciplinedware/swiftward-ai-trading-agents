## ADDED Requirements

### Requirement: PortfolioSnapshot common model
The system SHALL define a `PortfolioSnapshot` Pydantic `BaseModel` in `src/common/models/portfolio_snapshot.py`. It SHALL not import from `trading_mcp`.

Fields:
- `total_usd: DecimalField`
- `stablecoin_balance: DecimalField`
- `open_position_count: int`
- `realized_pnl_today: DecimalField`
- `current_drawdown_pct: DecimalField`
- `open_positions: list[OpenPositionView]` — default empty list

`OpenPositionView` (defined in the same file):
- `asset: str`
- `entry_price: DecimalField`
- `stop_loss: DecimalField`
- `take_profit: DecimalField`
- `size_pct: DecimalField`
- `strategy: str`

`DecimalField` SHALL be imported from `common.models.trade_intent`. Decimal fields SHALL serialize to `str` in `model_dump()`.

#### Scenario: Round-trip serialization
- **WHEN** a `PortfolioSnapshot` is serialized with `model_dump()` and re-validated with `model_validate()`
- **THEN** all `Decimal` fields SHALL have equal value before and after

#### Scenario: Empty portfolio
- **WHEN** a `PortfolioSnapshot` is constructed with no `open_positions`
- **THEN** `open_position_count` SHALL equal `0` and `open_positions` SHALL be an empty list

#### Scenario: Held assets are identifiable
- **WHEN** `open_positions` contains entries for assets `["SOL", "AVAX"]`
- **THEN** `{p.asset for p in snapshot.open_positions}` SHALL equal `{"SOL", "AVAX"}`
