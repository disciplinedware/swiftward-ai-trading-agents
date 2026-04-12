## MODIFIED Requirements

### Requirement: TradeIntent model
The system SHALL provide a `TradeIntent` Pydantic `BaseModel` at `src/common/models/trade_intent.py` representing a trading instruction from the agent brain to the execution layer.

Fields:
- `asset: str | None` — ticker symbol (e.g. `"SOL"`); `None` only valid when `action="FLAT_ALL"`
- `action: Literal["LONG", "FLAT", "FLAT_ALL"]`
- `size_pct: Decimal` — fraction of portfolio (e.g. `Decimal("0.09")`)
- `stop_loss: Decimal | None` — price level; required for `LONG`, optional for `FLAT`/`FLAT_ALL`
- `take_profit: Decimal | None` — price level; required for `LONG`, optional for `FLAT`/`FLAT_ALL`
- `strategy: Literal["trend_following", "breakout", "mean_reversion"]`
- `reasoning: str` — plain-text rationale; trading MCP embeds it in the IPFS validation trace
- `trigger_reason: Literal["clock", "price_spike", "stop_loss", "news", "liquidation", "fear_greed"]`

A `@model_validator` SHALL enforce:
- `action="LONG"` → `asset`, `stop_loss`, `take_profit` all non-None
- `action="FLAT"` → `asset` non-None; `stop_loss`/`take_profit` may be None
- `action="FLAT_ALL"` → `asset`, `stop_loss`, `take_profit` all None

All `Decimal` fields SHALL serialize to `str` in `model_dump()` and accept `str | Decimal | int | float` in `model_validate()`.

#### Scenario: Round-trip serialization
- **WHEN** a `TradeIntent` is created with valid fields and `.model_dump()` is called
- **THEN** `Decimal` fields appear as `str` in the dict, and `model_validate()` of that dict returns an equal `TradeIntent`

#### Scenario: Invalid action rejected
- **WHEN** `TradeIntent` is constructed with `action="SHORT"`
- **THEN** Pydantic raises `ValidationError`

#### Scenario: Invalid strategy rejected
- **WHEN** `TradeIntent` is constructed with `strategy="scalping"`
- **THEN** Pydantic raises `ValidationError`

#### Scenario: Decimal accepted as string
- **WHEN** `TradeIntent` is validated from a dict with `stop_loss="142.30"` (string)
- **THEN** the resulting model has `stop_loss == Decimal("142.30")`

#### Scenario: FLAT_ALL with no asset/stop/take is valid
- **WHEN** `TradeIntent` is created with `action="FLAT_ALL"`, `asset=None`, `stop_loss=None`, `take_profit=None`
- **THEN** validation succeeds

#### Scenario: FLAT_ALL with asset raises validation error
- **WHEN** `TradeIntent` is created with `action="FLAT_ALL"` and `asset="SOL"`
- **THEN** Pydantic raises `ValidationError`

#### Scenario: LONG with missing stop_loss raises validation error
- **WHEN** `TradeIntent` is created with `action="LONG"` and `stop_loss=None`
- **THEN** Pydantic raises `ValidationError`

#### Scenario: FLAT with asset and no stop/take is valid
- **WHEN** `TradeIntent` is created with `action="FLAT"`, `asset="SOL"`, `stop_loss=None`, `take_profit=None`
- **THEN** validation succeeds
