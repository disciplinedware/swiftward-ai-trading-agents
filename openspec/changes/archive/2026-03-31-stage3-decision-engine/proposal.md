## Why

The brain pipeline stubs out Stage 3 with `raise NotImplementedError`, so the agent cannot produce any trade intents — it always crashes after Stage 2. This is the final piece needed to make the full trading loop functional.

## What Changes

- Implement `_stage3` in `deterministic_llm.py` — ATR-based stop/TP, half-Kelly position sizing, R:R gate, and TradeIntent assembly
- Add `validate_trade_intent(intent, tracked_assets, min_rr, max_size) -> list[str]` standalone validation function
- Add `Stage3Trace` TypedDict to match Stage1Trace / Stage2Trace pattern
- Wire `_stage3` into `run()` — replace the `NotImplementedError` with the real call
- Extend test suite with Stage 3 table-driven cases

## Capabilities

### New Capabilities
- `stage3-decision-engine`: ATR-based stop/TP computation, half-Kelly position sizing with regime and uncertainty multipliers, R:R validation gate, TradeIntent assembly with programmatic reasoning, and submission-order sorting

### Modified Capabilities
<!-- None — no existing spec-level requirement changes -->

## Impact

- `python/src/agent/brain/deterministic_llm.py` — primary change
- `python/tests/agent/brain/test_deterministic_llm.py` — new test cases
- `python/docs/progress.md` — Task 17 status → done
- No API or model changes; TradeIntent model is unchanged
