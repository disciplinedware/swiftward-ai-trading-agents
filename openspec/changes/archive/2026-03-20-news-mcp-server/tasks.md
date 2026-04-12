## 1. Config & Dependencies

- [x] 1.1 Add `openai>=1.0.0` to `pyproject.toml` dependencies
- [x] 1.2 Add `NewsLLMConfig` Pydantic model to `src/common/config.py`
- [x] 1.3 Rename `external_apis.news_api_key` → `cryptopanic_api_key` in `ExternalAPIsConfig` and `AgentConfig`
- [x] 1.4 Add `news_llm: NewsLLMConfig` field to `AgentConfig`
- [x] 1.5 Update `config/config.example.yaml`: add `news_llm` section, rename `news_api_key` → `cryptopanic_api_key`

## 2. CryptoPanic Infra Client

- [x] 2.1 Create `src/news_mcp/__init__.py`, `src/news_mcp/infra/__init__.py`, `src/news_mcp/service/__init__.py`
- [x] 2.2 Implement `src/news_mcp/infra/cryptopanic.py` — `CryptoPanicClient` with `connect`, `close`, `get_posts(currencies, limit)` returning `list[dict]` with keys `title, url, published_at, source, currencies`
- [x] 2.3 Write `tests/news_mcp/__init__.py` and `tests/news_mcp/test_cryptopanic.py` — test response parsing, currency grouping, non-200 raises MCPError

## 3. LLM Scorer

- [x] 3.1 Implement `src/news_mcp/service/llm.py` — `NewsLLMScorer` with `analyze(assets, headlines_by_asset) -> AnalysisResult`; `_build_prompt` caps at 10 headlines/asset; `_parse_response` clamps scores to [-1.0, 1.0] and falls back to neutral on JSON error
- [x] 3.2 Write `tests/news_mcp/test_llm.py` — test prompt construction (headline cap, asset ordering), JSON parse success, JSON parse failure → neutral, score clamping

## 4. NewsService

- [x] 4.1 Implement `src/news_mcp/service/news.py` — `NewsService` with `get_headlines(assets)`, `get_sentiment(assets)`, `get_macro_flag(assets)`, and `_get_analysis(assets)` using per-asset cache keys `news:headlines:{ASSET}`, `news:analysis:{ASSET}`, `news:analysis:macro`
- [x] 4.2 Ensure `_get_analysis` only calls LLM for uncached assets; fans results out to individual per-asset keys on write; macro result always written to `news:analysis:macro`
- [x] 4.3 Write `tests/news_mcp/test_service.py` — test: headlines cache hit/miss per asset, partial cache hit fetches only uncached, LLM called once when sentiment + macro_flag requested with same assets (shared LLM call), macro flag triggered/not triggered from analysis result, LLM failure → neutral defaults

## 5. FastMCP Server

- [x] 5.1 Implement `src/news_mcp/server.py` — FastMCP on port 8002, lifespan wires `CryptoPanicClient`, `NewsLLMScorer`, `RedisCache` into `NewsService`; tools `get_headlines`, `get_sentiment`, `get_macro_flag`; `get_macro_flag` tool passes `cfg.assets.tracked` to service; `/health` route
- [x] 5.2 Verify server runs: `python -m news_mcp.server` starts without error (manual smoke test)
