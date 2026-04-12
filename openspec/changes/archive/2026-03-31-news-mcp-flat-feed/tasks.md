## 1. Infra — Remove currency tagging

- [x] 1.1 Remove `currencies` field from `NewsPost` TypedDict
- [x] 1.2 Delete `_ASSET_KEYWORDS` dict
- [x] 1.3 Simplify `_parse_post` to strip all currency tagging (paid-tier and keyword fallback)

## 2. Service — Single cache key and flat headlines

- [x] 2.1 Replace per-asset cache keys with single `news:headlines:all` key in `get_headlines`
- [x] 2.2 Remove `_group_by_asset` function
- [x] 2.3 Update `_get_analysis` to fetch flat `list[NewsPost]` instead of grouped dict
- [x] 2.4 Remove `assets` parameter from `get_headlines` (or make it unused)

## 3. LLM — Flat prompt

- [x] 3.1 Rewrite `_build_prompt` to accept `list[NewsPost]` and `list[str]` assets; flat list capped at 50, asset list at top

## 4. Server — Update tool signature

- [x] 4.1 Update `get_headlines` tool: remove `assets` param, change return type to `list[dict]`, update docstring

## 5. Tests

- [x] 5.1 Update `test_service.py` to match new `get_headlines` signature and return type
- [x] 5.2 Update or add tests for single cache key behaviour
- [x] 5.3 Update `test_llm.py` (if exists) for new `_build_prompt` signature
