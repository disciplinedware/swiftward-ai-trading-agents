## ADDED Requirements

### Requirement: Position model
The system SHALL provide a `Position` Pydantic `BaseModel` at `src/common/models/position.py` representing a single open or closed trading position.

Fields (all financial values as `Decimal`):
- `id: int | None = None` — DB primary key; None for in-memory positions
- `asset: str`
- `status: Literal["open", "closed"]`
- `action: Literal["LONG", "FLAT"]`
- `entry_price: Decimal`
- `size_usd: Decimal`
- `size_pct: Decimal`
- `stop_loss: Decimal`
- `take_profit: Decimal`
- `strategy: Literal["trend_following", "breakout", "mean_reversion"]`
- `trigger_reason: str`
- `reasoning_uri: str`
- `opened_at: datetime`
- `tx_hash_open: str`
- `closed_at: datetime | None = None`
- `exit_reason: Literal["take_profit", "stop_loss", "flat_intent"] | None = None`
- `exit_price: Decimal | None = None`
- `realized_pnl_usd: Decimal | None = None`
- `realized_pnl_pct: Decimal | None = None`
- `tx_hash_close: str | None = None`
- `validation_uri: str | None = None`

All `Decimal` fields SHALL serialize to `str` in `model_dump()`.

#### Scenario: Open position round-trip
- **WHEN** a `Position` with `status="open"` and all closed-state fields as `None` is serialized and re-validated
- **THEN** the result equals the original, with closed-state fields remaining `None`

#### Scenario: Closed position round-trip
- **WHEN** a `Position` with `status="closed"` and all fields populated is serialized and re-validated
- **THEN** the result equals the original with all Decimal fields as `Decimal` instances

#### Scenario: Decimal serialization in open position
- **WHEN** an open `Position` is dumped to dict
- **THEN** `entry_price`, `stop_loss`, `take_profit`, `size_usd`, `size_pct` are all `str` in the dict
