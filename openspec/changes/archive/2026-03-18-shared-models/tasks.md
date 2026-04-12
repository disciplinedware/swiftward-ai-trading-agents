## 1. Package Setup

- [x] 1.1 Create `src/common/models/` directory with `__init__.py` that re-exports `TradeIntent`, `Position`, `SignalBundle`, `PriceFeedData`, `FearGreedData`, `OnchainData`, `NewsData`

## 2. TradeIntent Model

- [x] 2.1 Create `src/common/models/trade_intent.py` with `TradeIntent(BaseModel)` — all fields, `Literal` types for `action` / `strategy` / `trigger_reason`, `Decimal` for financial fields
- [x] 2.2 Add custom Pydantic serializer: `Decimal` → `str` on `model_dump()`, accepts `str | Decimal | int | float` on `model_validate()`

## 3. Position Model

- [x] 3.1 Create `src/common/models/position.py` with `Position(BaseModel)` — all fields per spec, `Literal` for `status` / `action` / `strategy` / `exit_reason`, `Optional` closed-state fields default to `None`
- [x] 3.2 Apply same `Decimal` → `str` serializer as TradeIntent

## 4. SignalBundle Model

- [x] 4.1 Create `src/common/models/signal_bundle.py` with stub sub-classes: `PriceFeedData`, `FearGreedData`, `OnchainData`, `NewsData` (each `BaseModel` with `pass` body)
- [x] 4.2 Add `SignalBundle(BaseModel)` with typed fields: `prices: dict[str, PriceFeedData]`, `fear_greed: FearGreedData`, `onchain: dict[str, OnchainData]`, `news: dict[str, NewsData]`

## 5. Tests

- [x] 5.1 Create `tests/common/test_models.py` with parametrized round-trip tests for `TradeIntent` (valid fields, Decimal as str, invalid Literal values)
- [x] 5.2 Add parametrized round-trip tests for `Position` (open position, closed position, Decimal serialization)
- [x] 5.3 Add tests for `SignalBundle` (empty bundle round-trip, stub sub-object construction, sub-class imports from `common.models`)

## 6. Doc Update

- [x] 6.1 Update `python/CLAUDE.md`: change `src/agent/models/` to `src/common/models/` in the project layout section; add note that models use Pydantic `BaseModel` (not `@dataclass`)
