## 1. Model changes

- [x] 1.1 Extend `TradeIntent.action` to `Literal["LONG", "FLAT", "FLAT_ALL"]`; make `asset`, `stop_loss`, `take_profit` optional; add `@model_validator` enforcing field constraints per action type
- [x] 1.2 Update `MarketFilterWeightsConfig` in `src/common/config.py`: remove `netflow` field, add `btc_trend_clamp_pct: float = 10.0`
- [x] 1.3 Update `config/config.example.yaml`: remove `netflow` weight key, set new weights (`ema200: 0.35`, `fear_greed: 0.25`, `btc_trend: 0.20`, `funding: 0.20`), add `btc_trend_clamp_pct: 10.0`

## 2. LLM client

- [x] 2.1 Create `src/agent/brain/llm_client.py` with `LLMClient` class wrapping `openai.AsyncOpenAI`
- [x] 2.2 Implement `call(system, user) -> tuple[str, dict]`: raw completion → encoding normalization → XML+JSON parsing with 4-level fallback
- [x] 2.3 Implement encoding normalization: Chinese quotes, fullwidth brackets/braces/colon/comma → ASCII
- [x] 2.4 Implement retry loop: up to `config.llm.retries` attempts, `2**attempt` second backoff, re-raise on exhaustion

## 3. DeterministicLLMBrain — Stage 1

- [x] 3.1 Create `src/agent/brain/deterministic_llm.py` with `DeterministicLLMBrain` class skeleton
- [x] 3.2 Implement `_compute_health_score(signal_bundle) -> tuple[float, dict]`: four signal components using config weights; return score + signal breakdown dict
- [x] 3.3 Implement `_deterministic_verdict(score) -> str`: map score to `RISK_ON / UNCERTAIN / RISK_OFF` using config thresholds
- [x] 3.4 Implement `_build_stage1_prompt(score, signal_breakdown, trigger_reason, positions) -> tuple[str, str]`: return (system, user) strings matching the spec prompt format
- [x] 3.5 Implement `_call_llm_stage1(prompt) -> tuple[str, str]`: call `LLMClient`, extract verdict + reasoning; apply downgrade-only clamping; log warning on upgrade attempt; fallback to deterministic on LLM error
- [x] 3.6 Implement `run(signal_bundle) -> list[TradeIntent]`: Stage 1 full flow — score → verdict → RISK_OFF short-circuit (FLAT intents) or LLM call; Stage 2 and Stage 3 raise `NotImplementedError`
- [x] 3.7 Wire `uncertainty_multiplier` into in-progress trace dict (`0.5` for UNCERTAIN, `1.0` for RISK_ON)

## 4. Factory update

- [x] 4.1 Update `src/agent/brain/factory.py`: instantiate `DeterministicLLMBrain(config)` for `implementation="deterministic_llm"` (remove `NotImplementedError` placeholder)

## 5. trading-mcp FLAT_ALL

- [x] 5.1 Update `execute_swap` handler in `src/mcp_servers/trading/server.py` to route `FLAT_ALL` to a new `_execute_flat_all()` helper
- [x] 5.2 Implement `_execute_flat_all()`: query open positions, close each via engine, schedule ERC-8004 reputation hooks, return aggregated result; no-op if no positions

## 6. Tests

- [x] 6.1 Create `tests/agent/brain/test_llm_client.py`: parametrized tests for XML+JSON parsing (all 4 fallback levels), encoding normalization, retry behavior (mock `AsyncOpenAI`)
- [x] 6.2 Create `tests/agent/brain/test_deterministic_llm.py`: parametrized tests for health score calculation (all-bullish, all-bearish, BTC absent, no funding); verdict mapping; RISK_OFF short-circuit (with/without positions); LLM downgrade enforcement; LLM upgrade clamped; LLM error fallback; uncertainty multiplier values
- [x] 6.3 Update `tests/agent/brain/test_stub.py` and any snapshot tests that construct `TradeIntent` without optional fields — verify they still pass after model change
- [x] 6.4 Update progress.md: mark Task 15 as `done`
