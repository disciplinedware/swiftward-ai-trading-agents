## Why

The agent brain needs real-time news signals — per-asset sentiment scores and a global macro event flag — to make informed trading decisions. Without this, the brain's Stage 1 market filter is blind to news-driven market moves (ETF rulings, exchange collapses, regulatory actions).

## What Changes

- New `news_mcp` FastAPI service on port 8002 exposing three MCP tools: `get_headlines`, `get_sentiment`, `get_macro_flag`
- New `news_llm` config section for a lighter/cheaper LLM model dedicated to sentiment scoring (separate from the brain's `llm` config)
- Rename `external_apis.news_api_key` → `external_apis.cryptopanic_api_key` to reflect the chosen provider **BREAKING** (config change)
- Add `openai>=1.0.0` to `pyproject.toml` (first use of OpenAI SDK in this service)

## Capabilities

### New Capabilities

- `news-headlines`: Fetches recent crypto news from CryptoPanic, grouped by asset, cached 5 min in Redis
- `news-sentiment`: Per-asset sentiment scores (-1.0 to +1.0) via batched LLM call over headlines, cached 5 min per asset
- `news-macro-flag`: Global macro event detection (Fed policy, ETF events, exchange collapse/hack) via same LLM call, cached 5 min separately

### Modified Capabilities

- `config`: `external_apis.news_api_key` renamed to `external_apis.cryptopanic_api_key`; new `news_llm` section added

## Impact

- **New service**: `src/news_mcp/` (infra/cryptopanic, service/llm, service/news, server.py)
- **Config**: `src/common/config.py` + `config/config.example.yaml` — new `NewsLLMConfig` model, field rename
- **Dependency**: `openai>=1.0.0` added to `pyproject.toml`
- **Tests**: `tests/news_mcp/` — CryptoPanic response parsing, service cache logic, LLM prompt/parse
- **Consumers**: Agent brain (Stage 1 signal bundle) and Tier 2 loop (macro flag check) — both call this server via `mcp_servers.news_url`
