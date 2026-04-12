## Context

The news MCP currently tags each `NewsPost` with matching currency codes (via CryptoPanic's paid-tier `currencies` field or a keyword-fallback dict) and groups headlines by asset before passing them to the LLM. This grouping was designed to reduce LLM context and focus the prompt per asset, but it adds fragile heuristics that the LLM can do better.

The change removes tagging and grouping entirely: all fetched posts go into a single flat cache key, and the LLM receives a flat list and decides which posts are relevant to each asset.

## Goals / Non-Goals

**Goals:**
- Delete `_ASSET_KEYWORDS` and all currency-tagging code
- Single cache key for all headlines
- Flat LLM prompt with a total headline cap (not per-asset)
- `get_headlines` tool returns a flat `list[dict]`

**Non-Goals:**
- Changing LLM model, config, or sentiment output schema
- Changing cache TTL or Redis setup
- Modifying `get_sentiment` or `get_macro_flag` tool signatures

## Decisions

### Drop `currencies` from `NewsPost` entirely
Alternatives: keep the field but always set `[]`; keep tagging for paid-tier only.
Decision: remove the field — an always-empty field is a lie, and paid-tier detection adds conditional complexity we don't need.

### Single cache key `news:headlines:all`
Alternatives: per-asset keys (current); per-request key (no caching).
Decision: one key. CryptoPanic is called once with all tracked assets; result is one blob. TTL 300s unchanged.
Implication: `get_headlines(assets)` no longer does partial cache hits per asset — it's all-or-nothing. Acceptable because the API call always fetches all tracked assets anyway.

### `get_headlines` returns `list[dict]`, `assets` param removed
Alternatives: return `dict[str, list[dict]]` and reconstruct grouping in the tool handler.
Decision: flat return matches the new model honestly. The agent doesn't call `get_headlines` today; the signature change is low-risk.

### Flat LLM prompt with total cap of 50 headlines
Alternatives: keep 10-per-asset cap (requires grouping knowledge); no cap (unbounded tokens).
Decision: cap at 50 total, ordered by recency. The LLM prompt instructs the model to self-assign headlines to assets.

## Risks / Trade-offs

- **LLM receives mixed headlines** → The LLM must do more reasoning to assign headlines to assets. Mitigated by clear prompt instruction listing the assets to score and instructing per-asset attribution.
- **No partial cache invalidation** → Previously BTC cache could be hot while ETH was cold. Now it's all-or-nothing. At 5-min TTL this is a minor trade-off for simpler code.
- **`get_headlines` API break** → Return type changes from grouped dict to flat list. Agents not using this tool are unaffected; if a future caller needs grouped data it can re-group client-side.
