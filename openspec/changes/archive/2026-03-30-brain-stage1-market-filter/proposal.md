## Why

The agent currently runs a `StubBrain` that picks assets randomly with no market awareness. Task 15 introduces the first real decision-making layer: a deterministic health score that gates all trading activity based on macro market conditions, augmented by an LLM that can only make the verdict more conservative — never more aggressive.

## What Changes

- **New file** `src/agent/brain/deterministic_llm.py` — `DeterministicLLMBrain` class implementing the `Brain` protocol; Stage 1 complete, Stages 2 & 3 stubbed
- **New file** `src/agent/brain/llm_client.py` — thin OpenAI-compatible async client (configurable `base_url`/`model`/`api_key`); XML+JSON response parsing with 4-level fallback; Chinese/fullwidth character normalization; exponential-backoff retry
- **Modified** `src/agent/brain/factory.py` — wire `deterministic_llm` implementation (removes the `NotImplementedError` placeholder)
- **Modified** `src/common/models/trade_intent.py` — add `FLAT_ALL` action type; make `asset`, `stop_loss`, `take_profit` optional (None only valid for `FLAT_ALL`); add model validator
- **Modified** `src/mcp_servers/trading/server.py` + engine — `execute_swap` handles `FLAT_ALL` by querying all open positions and closing each atomically
- **Modified** `config/config.example.yaml` — remove `netflow` weight, update market filter weights to sum to 1.0 (`ema200: 0.35`, `fear_greed: 0.25`, `btc_trend: 0.20`, `funding: 0.20`)
- **Modified** `src/common/config.py` — remove `netflow` from `MarketFilterWeightsConfig`

## Capabilities

### New Capabilities

- `deterministic-llm-brain`: `DeterministicLLMBrain` class — Stage 1 market filter (deterministic health score + LLM downgrade-only verdict); Stages 2 & 3 raise `NotImplementedError` pending Tasks 16–17
- `llm-client`: Thin async OpenAI-compatible LLM client with XML+JSON parsing, encoding normalization, and retry logic

### Modified Capabilities

- `trade-intent`: Add `FLAT_ALL` action type; `asset`/`stop_loss`/`take_profit` become optional (None iff `FLAT_ALL`)
- `trading-mcp-server`: `execute_swap` handles `FLAT_ALL` by closing all open positions atomically

## Impact

- `src/agent/brain/deterministic_llm.py` — new
- `src/agent/brain/llm_client.py` — new
- `src/agent/brain/factory.py` — minor update (remove `NotImplementedError`)
- `src/common/models/trade_intent.py` — **BREAKING**: `asset`, `stop_loss`, `take_profit` become `Optional`; existing callers unaffected (they still pass values)
- `src/common/config.py` — remove `netflow` field from `MarketFilterWeightsConfig`
- `config/config.example.yaml` — updated weights (no `netflow` key)
- `src/mcp_servers/trading/` — `execute_swap` extended for `FLAT_ALL`
- New dependency: `openai` Python package (already in `pyproject.toml` per Task 1)
- Tests: `tests/agent/brain/test_deterministic_llm.py`, `tests/agent/brain/test_llm_client.py`
