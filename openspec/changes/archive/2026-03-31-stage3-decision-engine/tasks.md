## 1. Core Stage 3 Implementation

- [x] 1.1 Add `Stage3Trace` TypedDict to `deterministic_llm.py` (fields: `intents_produced`, `skipped_atr_zero`, `skipped_rr`, `skipped_held`, `skipped_validation`, `stage3_reasoning` per asset)
- [x] 1.2 Add `_REGIME_MULTIPLIERS` and `_REGIME_TO_STRATEGY` module-level constants
- [x] 1.3 Implement `validate_trade_intent(intent, tracked_assets, min_rr, max_size) -> list[str]` as a standalone module-level function
- [x] 1.4 Implement `_stage3(signal_bundle, stage1_trace, selected, stage2_trace) -> tuple[list[TradeIntent], Stage3Trace]`
- [x] 1.5 Wire `_stage3` into `run()`: replace `raise NotImplementedError` with `return await self._stage3(...)`

## 2. Tests

- [x] 2.1 Add `_make_config_stage3()` helper to the test file (extends existing `_make_config` with `half_kelly_fraction`, `atr_stop_multiplier`, `atr_target_multiplier`, `min_reward_risk_ratio`, `tracked_assets`)
- [x] 2.2 Add table-driven `test_stage3_happy_path` — STRONG_UPTREND, BREAKOUT, RANGING, WEAK_MIXED each producing correct `size_pct`, `stop_loss`, `take_profit`, `strategy`
- [x] 2.3 Add `test_stage3_skip_cases` — ATR=0, R:R below min, already-held asset
- [x] 2.4 Add `test_validate_trade_intent` — valid intent returns `[]`; each violation condition returns non-empty list
- [x] 2.5 Add `test_stage3_ordering` — verify FLAT before LONG in returned list

## 3. Progress Update

- [x] 3.1 Update `python/docs/progress.md` Task 17 status to `done`
