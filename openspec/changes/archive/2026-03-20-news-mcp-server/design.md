## Context

The agent brain requires three news signals at every decision cycle: per-asset sentiment scores, recent headlines, and a global macro event flag. These signals are consumed by Stage 1 (market filter health score) and the Tier 2 loop (macro flag check). All five MCP servers follow the same `infra / service / server.py` layout established by `price_feed_mcp` — this server follows that pattern exactly.

CryptoPanic was chosen as the news provider (already integrated on the Go side). Sentiment scoring and macro flag detection require an LLM; the news MCP gets its own lighter/cheaper model config (`news_llm`) separate from the brain's `llm` config.

## Goals / Non-Goals

**Goals:**
- Expose `get_headlines`, `get_sentiment`, `get_macro_flag` MCP tools on port 8002
- Cache all three at the Redis per-asset level (5 min TTL) to minimize CryptoPanic and LLM API calls
- Single batched LLM call produces both sentiment scores and macro flag — results fanned out to per-asset cache keys
- Degrade gracefully: LLM failure → neutral defaults (0.0 sentiment, macro=false); CryptoPanic failure → MCPError

**Non-Goals:**
- Historical news or sentiment (backtesting uses a stub returning neutral)
- Per-asset macro flags (macro is a global market-wide signal by design)
- Keyword-based macro detection (LLM handles novel event descriptions)

## Decisions

### 1. Per-asset cache keys (not composite)

**Decision**: Cache analysis results at `news:analysis:{ASSET}` and `news:analysis:macro` rather than `news:analysis:{BTC,ETH,...}`.

**Rationale**: Composite keys break cache reuse when the requested asset set changes. `get_sentiment(["BTC","ETH"])` and `get_sentiment(["BTC","SOL"])` should share the cached BTC result. Ordering issues (["BTC","ETH"] vs ["ETH","BTC"]) are also eliminated.

**Alternative considered**: Single composite key with `sorted(assets)` join — rejected because it causes redundant LLM calls whenever the asset subset differs, even slightly.

### 2. Single combined LLM call for sentiment + macro

**Decision**: One LLM prompt returns `{"sentiment": {asset: float}, "macro": {"triggered": bool, "reason": str|null}}`. Results stored in separate per-asset and macro cache keys.

**Rationale**: Halves LLM cost vs. two calls. Sentiment and macro are computed from the same headline batch so there's no benefit to splitting them.

**Alternative considered**: Separate calls — rejected as wasteful; macro flag doesn't need its own independent context.

### 3. Separate `news_llm` config

**Decision**: New `news_llm` config section with its own `base_url`, `model`, `api_key`, `max_tokens` — independent of the brain's `llm` config.

**Rationale**: Sentiment scoring is a simpler task than brain reasoning; a cheaper/faster model (e.g. `gpt-4o-mini`, small Ollama model) is appropriate and avoids competing for capacity with the brain's LLM calls.

**Alternative considered**: Reuse `config.llm` — rejected because it couples news scoring model choice to brain model choice and prevents independent cost optimization.

### 4. LLM failure degrades to neutral

**Decision**: If the LLM call fails (timeout, API error, bad JSON), return 0.0 for all asset sentiments and `macro_flag=false`. Log a warning, do not raise MCPError.

**Rationale**: News sentiment is a soft signal. A transient LLM failure should not block the brain's decision cycle — it simply proceeds with neutral news input. CryptoPanic failure (upstream HTTP error) does raise MCPError since headlines are the foundation of the entire tool.

### 5. Headlines capped at 10 per asset in LLM prompt

**Decision**: When building the LLM prompt, include at most 10 headlines per asset.

**Rationale**: Keeps prompt size bounded within `max_tokens`. CryptoPanic returns up to 50 posts per API call; the most recent 10 per asset carry sufficient signal.

## Risks / Trade-offs

- **LLM latency inside MCP server**: The news MCP makes an LLM call synchronously on cache miss. If the brain calls `get_sentiment` and the news_llm endpoint is slow, the brain's decision cycle is blocked. Mitigation: short `max_tokens` (500), fast model, 5-min cache means LLM is called infrequently.
- **CryptoPanic free tier limits**: ~200 req/hour authenticated. With 5-min cache and per-asset keys, worst case is 10 assets × 1 call = 10 req per cache miss cycle. Well within limits. Mitigation: headlines are cached independently so a partial cache hit reduces calls further.
- **LLM JSON non-compliance**: Some models ignore `response_format: json_object`. Mitigation: `_parse_response` has a fallback to neutral defaults on JSON parse failure.

## Open Questions

None — all design decisions resolved during exploration.
