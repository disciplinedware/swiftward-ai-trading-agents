## Context

`DeterministicLLMBrain.run()` currently raises `NotImplementedError` after Stage 2. Stages 1 and 2 are complete: Stage 1 produces a market verdict + `uncertainty_multiplier`; Stage 2 produces `selected = [{"asset": str, "regime": str}]`. Stage 3 needs to convert that selection into concrete `TradeIntent` objects with ATR-anchored stops, sized positions, and a reasoning string.

The `TradeIntent` model already has all required fields (`stop_loss`, `take_profit`, `size_pct`, `strategy`, `reasoning`, `trigger_reason`). No model changes needed.

## Goals / Non-Goals

**Goals:**
- Produce `TradeIntent(action="LONG")` for each selected asset that passes all guards
- ATR-based stop/TP with configurable multipliers
- Half-Kelly sizing modulated by regime and uncertainty multipliers
- Standalone `validate_trade_intent` function covering all pre-submission checks
- Programmatic reasoning assembly (no extra LLM call)
- Submission ordering: FLAT/FLAT_ALL before LONG (future-proofing)

**Non-Goals:**
- Rotation / FLAT generation for unselected positions — exits handled by stop-loss loop
- LLM call in Stage 3 — all decisions are deterministic at this point
- IPFS upload — that's trading-mcp's responsibility, not the brain's

## Decisions

### No LLM call in Stage 3
Stage 3 makes no decisions — asset selection and regime are already locked by Stage 2, and all math is deterministic. An LLM call here would only produce a narrative paragraph at extra latency and token cost. Instead, the `reasoning` field is assembled programmatically from Stage 1 reasoning + Stage 2 reasoning + Stage 3 math fields.

### ATR = 0 → skip (no fallback %)
If `atr_14 == 0`, the stop would equal entry price, making the R:R check fail anyway. But we log a warning explicitly before the R:R check so it's observable in traces. Using a magic fallback percentage would introduce an arbitrary constant with no volatility grounding — better to skip.

### No rotation
Producing FLAT intents for unselected positions in Stage 3 would couple the decision engine to position management logic that already lives in the stop-loss loop. Keeping Stage 3 LONG-only preserves clean separation of concerns.

### Decimal throughout
All price arithmetic (`entry_price`, `stop_loss`, `take_profit`, `size_pct`) uses `decimal.Decimal` to match `TradeIntent` field types. Input strings from `PriceFeedData` converted at the boundary.

### `validate_trade_intent` as standalone function
Defined at module level (not a method) so it can be imported and called from other contexts (e.g., the stop-loss loop, future backtesting). Signature: `validate_trade_intent(intent, tracked_assets, min_rr, max_size) -> list[str]`.

### `_stage3` returns `(list[TradeIntent], Stage3Trace)`
Consistent with `_stage1` and `_stage2` patterns. `run()` discards the trace for now (same as `_stage2_trace`), but it's available for future structured logging.

## Risks / Trade-offs

- **All selected assets fail guards** → Stage 3 returns `[]`, `run()` returns `[]`. Clock loop logs and sleeps. Acceptable — no trade is better than a bad trade.
- **`signal_bundle.prices` missing an asset** → `selected` from Stage 2 only contains assets that were in `signal_bundle.prices`, so KeyError shouldn't happen. Guard with a warning log + skip to be safe.
- **Regime not in known set** → Stage 2 validates regime against `_VALID_REGIMES` and raises `BrainError` for unknown regimes, so Stage 3 can assume regime is always valid. Use `.get()` with `WEAK_MIXED` fallback anyway for defense in depth.

## Migration Plan

Drop-in replacement. The `NotImplementedError` is replaced in-place. No config schema changes, no DB changes, no new dependencies. Existing Stage 1 / Stage 2 tests continue to pass unchanged.
