## ADDED Requirements

### Requirement: PriceFeedData concrete fields
`PriceFeedData` in `src/common/models/signal_bundle.py` SHALL be a Pydantic `BaseModel` with the following fields (all `str`, representing Decimal-encoded values from the MCP server), with defaults of `"0"` for numeric fields:

- `price: str = "0"`
- `change_1m: str = "0"`
- `change_5m: str = "0"`
- `change_1h: str = "0"`
- `change_4h: str = "0"`
- `change_24h: str = "0"`
- `rsi_14: str = "0"`
- `ema_20: str = "0"`
- `ema_50: str = "0"`
- `ema_200: str = "0"`
- `atr_14: str = "0"`
- `bb_upper: str = "0"`
- `bb_mid: str = "0"`
- `bb_lower: str = "0"`
- `volume_ratio: str = "1"`

Field names SHALL match exactly what `price_feed_mcp/service/indicators.py` and `price_feed_mcp/service/price_feed.py` return.

#### Scenario: Default PriceFeedData is safe for stub brain
- **WHEN** `PriceFeedData()` is constructed with no arguments
- **THEN** all numeric fields SHALL have string value `"0"` (or `"1"` for volume_ratio) and the brain can safely convert them to `Decimal` without error

#### Scenario: Round-trip serialization
- **WHEN** a `PriceFeedData` is serialized with `model_dump()` and re-validated
- **THEN** all field values SHALL be preserved exactly

### Requirement: FearGreedData concrete fields
`FearGreedData` in `src/common/models/signal_bundle.py` SHALL be a Pydantic `BaseModel` with:

- `value: int = 50`
- `classification: str = "Neutral"`
- `timestamp: str = ""`

#### Scenario: Default FearGreedData is neutral
- **WHEN** `FearGreedData()` is constructed with no arguments
- **THEN** `value` SHALL equal `50` and `classification` SHALL equal `"Neutral"`

### Requirement: NewsData concrete fields
`NewsData` in `src/common/models/signal_bundle.py` SHALL be a Pydantic `BaseModel` with:

- `sentiment: float = 0.0`
- `macro_flag: bool = False`

#### Scenario: Default NewsData is neutral
- **WHEN** `NewsData()` is constructed with no arguments
- **THEN** `sentiment` SHALL equal `0.0` and `macro_flag` SHALL be `False`
