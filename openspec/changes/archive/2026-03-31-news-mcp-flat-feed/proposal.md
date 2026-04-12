## Why

The current news pipeline does per-asset currency tagging (keyword heuristics + paid-tier API field) to group headlines before passing them to the LLM. This adds complexity for no quality gain — the LLM is better at deciding which headlines are relevant to which asset than a keyword list. The grouping and tagging code can be deleted entirely.

## What Changes

- **BREAKING** Remove `currencies` field from `NewsPost` — currency tagging is gone from `CryptoPanicClient`
- Remove `_ASSET_KEYWORDS` dict and all keyword-matching logic from `cryptopanic.py`
- Replace per-asset headline cache keys (`news:headlines:{ASSET}`) with a single `news:headlines:all` key
- Remove `_group_by_asset()` from `news.py` — no per-asset fan-out in headline fetching
- Change `get_headlines` tool return type from `dict[str, list[dict]]` to `list[dict]` (flat)
- Drop the `assets` parameter from `get_headlines` (or ignore it) — with a single cache key it's meaningless
- Replace grouped-by-asset LLM prompt with a flat headline list; the LLM infers relevance per asset

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `news-headlines`: cache key changes to single `news:headlines:all`; return type becomes a flat list; `assets` parameter removed; currency tagging removed
- `news-sentiment`: LLM prompt changes from grouped-by-asset to flat list; per-asset headline cap changes from 10-per-asset to a total cap across all headlines
- `news-macro-flag`: no behavioral change — macro detection benefits from seeing the full unfiltered feed

## Impact

- `python/src/news_mcp/infra/cryptopanic.py`: remove `_ASSET_KEYWORDS`, simplify `_parse_post`, remove `currencies` from `NewsPost`
- `python/src/news_mcp/service/news.py`: remove `_group_by_asset`, single cache key, update `get_headlines` return type
- `python/src/news_mcp/service/llm.py`: rewrite `_build_prompt` to use flat list
- `python/src/news_mcp/server.py`: update `get_headlines` tool signature and docstring
- `python/tests/news_mcp/`: update tests to match new interfaces
