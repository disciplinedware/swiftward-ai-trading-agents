## 1. Common Models

- [x] 1.1 Create `src/common/models/portfolio_snapshot.py` with `OpenPositionView` and `PortfolioSnapshot` Pydantic models
- [x] 1.2 Fill in `PriceFeedData` fields in `src/common/models/signal_bundle.py` (price, changes, indicators — all `str` with safe defaults)
- [x] 1.3 Fill in `FearGreedData` fields (`value: int = 50`, `classification: str`, `timestamp: str`)
- [x] 1.4 Fill in `NewsData` fields (`sentiment: float = 0.0`, `macro_flag: bool = False`)
- [x] 1.5 Add `portfolio: PortfolioSnapshot` field to `SignalBundle` with a default empty snapshot

## 2. Config Extension

- [x] 2.1 Add `implementation: str` field to `BrainConfig` in `src/common/config.py`
- [x] 2.2 Add `brain.implementation: stub` to `config/config.example.yaml`

## 3. Brain Protocol and Factory

- [x] 3.1 Create `src/agent/__init__.py` and `src/agent/brain/__init__.py`
- [x] 3.2 Create `src/agent/brain/base.py` with `Brain` runtime-checkable Protocol
- [x] 3.3 Create `src/agent/brain/factory.py` with `make_brain(config) -> Brain` — returns `StubBrain` for `"stub"`, raises `ConfigError` for unknown values

## 4. StubBrain Implementation

- [x] 4.1 Create `src/agent/brain/stub.py` with `StubBrain` class skeleton and `__init__(self, config: AgentConfig)`
- [x] 4.2 Implement `_stage1_market_filter(signal_bundle) -> tuple[str, float]` — computes health_score, returns (verdict, score)
- [x] 4.3 Implement `_stage2_rotation_selector(signal_bundle, verdict) -> list[tuple[str, str]]` — returns [(asset, regime), ...]
- [x] 4.4 Implement `_stage3_decision_engine(signal_bundle, selections, verdict) -> list[TradeIntent]` — ATR sizing, R:R check, intent assembly
- [x] 4.5 Implement `run(signal_bundle) -> list[TradeIntent]` — orchestrates stages, returns FLAT intents first

## 5. Tests

- [x] 5.1 Create `tests/agent/__init__.py` and `tests/agent/brain/__init__.py`
- [x] 5.2 Add `PortfolioSnapshot` round-trip serialization tests to `tests/common/test_models.py`
- [x] 5.3 Create `tests/agent/brain/test_stub.py` with parametrized table-driven tests:
  - Stage 1: RISK_ON / UNCERTAIN / RISK_OFF score thresholds
  - Stage 1 RISK_OFF: returns FLAT intents for each open position, no Stage 2/3
  - Stage 1 UNCERTAIN: sizes halved in Stage 3 output
  - Stage 2: held-asset bonus applied to scores
  - Stage 2: BTC+ETH correlation filter drops ETH
  - Stage 2: WEAK_MIXED assets skipped (no intent)
  - Stage 3: R:R below minimum skips trade
  - Stage 3: correct stop_loss, take_profit, size_pct for each regime
  - Factory: returns StubBrain for "stub" config value
  - Factory: raises ConfigError for unknown implementation
