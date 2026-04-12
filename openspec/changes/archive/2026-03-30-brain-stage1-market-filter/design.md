## Context

The `Brain` protocol (`src/agent/brain/base.py`) is already wired — the factory, clock loop, and all trigger loops call `brain.run(signal_bundle)`. `StubBrain` fills that slot today with random asset selection. `DeterministicLLMBrain` replaces it with a three-stage pipeline; this change implements Stage 1 only (the market health gate) while stubbing Stages 2–3 with `NotImplementedError`.

All signal data the stage needs is already present in `SignalBundle` — except `netflow`, which is dropped from Stage 1 (see Decisions).

The `TradeIntent` model currently requires `asset`, `stop_loss`, and `take_profit` for every intent. Stage 1 may produce a "close everything" signal (`RISK_OFF`) before any specific asset is selected, so the model must support a fleet-wide exit action.

## Goals / Non-Goals

**Goals:**
- Implement `DeterministicLLMBrain.run()` with a fully working Stage 1 (deterministic health score + LLM downgrade)
- Ship a reusable `LLMClient` with XML+JSON parsing, encoding normalization, and retry
- Extend `TradeIntent` with `FLAT_ALL`; update `execute_swap` in trading-mcp to handle it
- Remove `netflow` from config weights model; keep config and code in sync

**Non-Goals:**
- Stages 2 & 3 of `DeterministicLLMBrain` (Tasks 16–17)
- Any change to the trigger loops or how `brain.run()` is called
- Backtesting integration
- LLM streaming, function-calling mode, or vision

## Decisions

### D1 — Drop netflow from Stage 1 health score

**Decision**: Remove `netflow` from the market filter entirely (not placeholder/neutral). Redistribute weight: `ema200 → 0.35`, `fear_greed → 0.25`, `btc_trend → 0.20`, `funding → 0.20`.

**Why**: `OnchainData` has no netflow field and the onchain-mcp `get_netflow` result is not plumbed into `SignalBundle`. Adding it would require touching the MCP server, the model, and the MCP client — scope creep for a 0.15-weight signal. The four remaining signals cover the same macro thesis (trend + sentiment + on-chain activity).

**Alternative considered**: Default netflow to neutral (0.5) when missing. Rejected — silent defaults make the score harder to reason about and test.

### D2 — Use `openai` Python package for LLM client

**Decision**: Thin wrapper around `openai.AsyncOpenAI` with configurable `base_url`. No LangChain.

**Why**: The config already provides `llm.base_url`, `llm.model`, `llm.api_key`. One `chat.completions.create()` call is all we need. LangChain would add ~20 transitive deps for zero benefit. The wrapper owns retry, encoding normalization, and XML+JSON parsing — all straightforward with direct SDK use.

### D3 — LLM can only downgrade the deterministic verdict

**Decision**: After parsing the LLM JSON decision, apply: `final = min(det_verdict, llm_verdict)` where `RISK_OFF < UNCERTAIN < RISK_ON` (more conservative = smaller). If the LLM tries to upgrade, silently clamp to the deterministic value and log a warning.

**Why**: This makes the LLM a risk-tightening layer only. A misconfigured or hallucinating LLM cannot make the agent more aggressive than the deterministic math allows.

**Alternative considered**: Hard-reject the LLM response on upgrade attempt. Rejected — clamping is more resilient to borderline/ambiguous outputs.

### D4 — FLAT_ALL action with optional fields on TradeIntent

**Decision**: Add `FLAT_ALL` to `action: Literal[...]`. Make `asset: str | None`, `stop_loss: DecimalField | None`, `take_profit: DecimalField | None` — all default `None`. Add a `@model_validator` that enforces: `LONG` requires all three; `FLAT` requires `asset`; `FLAT_ALL` requires none.

**Why**: A `FLAT_ALL` intent has no meaningful asset, stop, or target — forcing callers to pass sentinel values (`asset="ALL"`) would be confusing. Optional fields with a validator is the standard Pydantic pattern for discriminated unions without separate model classes.

**Alternative considered**: Separate `FlatAllIntent` model. Rejected — adds a second branch everywhere the type is used; the single model with a validator is simpler.

### D5 — btc_trend_norm scaling

**Decision**: `btc_trend_norm = clamp(btc_24h_change_pct / 10.0, -1.0, 1.0)` then map to `[0, 1]`: `(val + 1.0) / 2.0`. The clamp range (±10%) is a config-tunable (`brain.market_filter.btc_trend_clamp_pct`, default 10.0).

**Why**: A ±100% clamp would make the signal near-neutral on almost every real day. ±10% captures the full meaningful range for daily BTC moves while leaving room to adjust.

### D6 — ema200 filter uses BTC as market proxy

**Decision**: `ema200_filter = 1.0 if prices["BTC"].price > prices["BTC"].ema_200 else 0.0`. If BTC is missing from the signal bundle, default to 0.5 (neutral) and log a warning.

**Why**: BTC dominates total crypto market cap and is the strongest macro indicator. The EMA200 signal is a market-regime gate, not a per-asset filter.

### D7 — funding_score averaged across tracked assets

**Decision**: Average the per-asset funding score across all assets in `signal_bundle.onchain` that have non-null `funding_rate`. Score per asset: `1.0` if `funding_rate ≥ 0`, linearly interpolated toward `0.0` at `−0.03%` (3× daily cost). If no funding data, default to `0.5`.

**Why**: Averaging gives a market-wide view. BTC and ETH will dominate in practice since they're most reliably present. The interpolation avoids a hard cliff at zero.

## Risks / Trade-offs

- **LLM latency adds to clock loop latency** → LLM call is bounded by `llm.max_tokens=1000` and 3 retries with backoff. Worst case ~10s; acceptable within the 15-min clock loop.
- **LLM unavailability** → On all retries exhausted, log error and fall back to the deterministic verdict. Brain continues; no crash.
- **BTC missing from signal bundle** → Handled by D6 neutral default. Should never happen in production since BTC is always in tracked assets.
- **Stage 1 returns FLAT_ALL but no open positions exist** → `execute_swap(FLAT_ALL)` with empty position list is a no-op; the trading-mcp returns success with 0 trades executed.

## Open Questions

None — all decisions resolved in explore phase.
