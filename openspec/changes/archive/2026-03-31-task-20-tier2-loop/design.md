## Context

The agent runs four concurrent trigger loops. Three are implemented:
- `ClockLoop` (15 min) — base brain cycle
- `ExitWatchdog` (2 min) — stop-loss/take-profit
- `PriceSpikeLoop` (1 min) — price move detection

The fourth, `Tier2Loop`, is wired as a `_noop_loop()` placeholder in `main.py`. It needs to watch for macro-level events that warrant an unscheduled brain run: extreme sentiment (Fear & Greed) and high-impact news.

All existing loops follow the same pattern: poll on an interval, check conditions, call `clock._run_once()` if triggered.

## Goals / Non-Goals

**Goals:**
- Implement `Tier2Loop` in `trigger/tier2.py` following existing loop conventions
- Detect F&G threshold crossings via `fear_greed.get_index()`, thresholds read from config
- Detect macro news flag via `news.get_signals()` (reuses existing client, extracts `macro_flag`)
- Fire at most one brain cycle per 5-minute poll regardless of how many conditions are true
- Wire into `main.py` replacing `_noop_loop()`

**Non-Goals:**
- Liquidation spike detection — data source unavailable (Binance endpoint decommissioned)
- "Stage 1 only" brain path for F&G — full `_run_once()` is simpler and Stage 1 short-circuits to RISK_OFF naturally under extreme F&G anyway
- "Stage 1 only" brain path for F&G — full `_run_once()` is simpler and Stage 1 short-circuits to RISK_OFF naturally under extreme F&G anyway

## Decisions

**Reuse `news.get_signals()` for macro flag (not a new MCP method)**
The existing `NewsMCPClient.get_signals(assets)` fetches both sentiment scores and the macro flag in one call. The tier2 loop only needs the macro flag, so sentiment data is discarded. Alternative: add a cheap `get_macro_flag()` method to `NewsMCPClient`. Rejected — adds surface area for a small gain; the news-mcp server caches sentiment for 5 min so the extra work is minimal.

**Fetch F&G and news in parallel, then evaluate**
Both fetches are independent. `asyncio.gather()` cuts latency roughly in half (both are cached server-side).

**Deduplicate: fire brain at most once per cycle**
If both F&G and news conditions are true simultaneously, `_run_once()` is called once. The brain doesn't need to know what triggered it — `ClockLoop._run_once()` handles cooldown gate enforcement internally.

**Full brain run (not Stage 1 only) for all conditions**
The spec mentions "Stage 1 only for F&G crossing" as an optimization. Decided against it: the Brain protocol has no `stage1_only` parameter, adding one touches the protocol + both implementations, and Stage 1 already short-circuits to RISK_OFF on extreme F&G. Full run is equivalent in outcome with zero extra complexity.

**F&G thresholds are config-driven (`brain.tier2.fear_greed_low` / `brain.tier2.fear_greed_high`)**
Defaults: 20 / 80. Placing them in config allows tuning without a code change — useful when backtesting shows a different threshold is optimal. Added under `brain.tier2` to keep tier2 parameters grouped. `Tier2Loop` reads them from `AgentConfig` at construction time.

## Risks / Trade-offs

**`_run_once()` is not thread-safe against other loop invocations** → Mitigation: asyncio is single-threaded; concurrent calls are interleaved not simultaneous. Accepted risk (same as price_spike loop which has the same pattern).

**Macro flag is stuck in a 5-min news-mcp cache** → The tier2 loop polls every 5 min; the cache TTL matches. In the worst case there's a 10-min delay (cache stale + one missed poll). Acceptable for a hackathon context.

**Sentiment LLM call wasted** → `get_signals()` triggers a batched LLM sentiment call internally (cached 5 min). Tier2 discards it. Cost: negligible at hackathon scale, and the cache means it's only computed once per 5-min window across all callers.

## Migration Plan

1. Write `trigger/tier2.py`
2. Write `tests/agent/trigger/test_tier2.py`
3. Update `main.py`: replace `_noop_loop()` with `Tier2Loop(...)`
4. Update `docs/progress.md`: Task 20 → done
