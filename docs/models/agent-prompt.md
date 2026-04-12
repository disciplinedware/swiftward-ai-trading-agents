# Agent Prompt System

The LLM agent (Go, `internal/agents/simple_llm`) constructs the system prompt from three parts:

```
[strategy prompt from file]        <- static, loaded from TRADING__LLM_AGENT__PROMPT_FILE
[{{market_context}} expanded]      <- live portfolio + prices, fetched at session start
[{{memory}} expanded]              <- memory files, fetched at session start
```

## Strategy Prompt

Loaded from disk (default: `prompts/delta/prompt.md`). Contains the agent's mandate,
rules, workflow, and tool reference. Contains two placeholder tokens:

- `{{market_context}}` - replaced with pre-fetched portfolio state and current prices
- `{{memory}}` - replaced with pre-fetched memory files

Override via `TRADING__LLM_AGENT__PROMPT_FILE` env var.

## `{{market_context}}` — What Gets Injected

Fetched before each session by `fetchMarketContext()` in `placeholders.go`.

**Content:**
1. `trade/get_portfolio` — cash, portfolio value (peak), open positions with pair/side/qty/avg_price/value, trade count
2. `market/get_prices` — last price, 24h change %, 24h high/low for all tracked markets

**Market coverage:** open position markets first (agent always sees its own exposure),
then `TRADING__LLM_AGENT__MARKETS` config as defaults. Deduped, positions first.

**Header format** (uses tool names so agent skips redundant re-fetches):
```
## Pre-loaded Market Context — 2026-03-09 01:42 UTC
*Already fetched — do NOT re-call trade/get_portfolio or market/get_prices this session.*

### trade/get_portfolio
Cash: 6002.52 USDC
Portfolio Value: 10027.65 USDC (peak: 10027.65)
Trades: 2 fills, 0 rejects

### Open Positions
- BTC-USDC LONG: 0.018109 units @ 66569.25 (value: 1205.51 USDC)

### market/get_prices
- ETH-USDC: 1958.45 | 24h: -1.23% | H: 1980.00 | L: 1940.00
- BTC-USDC: 66569.25 | 24h: +0.42% | H: 67000.00 | L: 65800.00
```

Both fetches are non-fatal: partial data is returned if one MCP call fails.

## `{{memory}}` — What Gets Injected

Fetched before each session by `loadMemoryContext()` in `placeholders.go`.

**Files loaded** (in order):
1. `memory/MEMORY.md` — core memory index (hypotheses, lessons, regime notes)
2. `memory/sessions/YYYY-MM-DD.md` (today) — current day session log
3. `memory/sessions/YYYY-MM-DD.md` (yesterday) — previous day session log

Missing files get a fallback message (e.g. "No entries yet for today.") — not an error.

**Header format** (file paths as sub-headers so agent skips redundant re-reads):
```
## Pre-loaded Memory
*Already fetched — do NOT call files/read for these files this session.*

### memory/MEMORY.md
[content or fallback]

### memory/sessions/2026-03-09.md (today)
[content or fallback]

### memory/sessions/2026-03-08.md (yesterday)
[content or fallback]
```

**Error handling:** connection error to Files MCP → Warn log + empty string (session proceeds without memory). File not found → Info log + fallback text.

## MCP Clients Used at Session Start

| Client | Tool | Purpose |
|--------|------|---------|
| `trading` | `trade/get_portfolio` | Portfolio state for `{{market_context}}` |
| `market` | `market/get_prices` | Current prices for `{{market_context}}` |
| `files` | `files/read` × 3 | Memory files for `{{memory}}` |

These are the ONLY pre-session fetches. Candles, orderbook, funding, OI are NOT pre-fetched
— the agent fetches them on demand during the session using its tool calls.

## Tool Clients Available During Session

The agent discovers tools from 4 MCP servers at startup:

| Prefix | Client | URL |
|--------|--------|-----|
| `trade/` | trading | `TRADING__LLM_AGENT__TRADING_MCP_URL` (via Swiftward gateway) |
| `market/` | market | `TRADING__LLM_AGENT__MARKET_DATA_MCP_URL` (via Swiftward gateway) |
| `files/` | files | `TRADING__LLM_AGENT__FILES_MCP_URL` (via Swiftward gateway) |
| `code/` | code | `TRADING__LLM_AGENT__CODE_MCP_URL` (direct to trading-server) |

Tool names are converted to OpenAI function format: `trade/submit_order` → `trade__submit_order`.

## Adding a New Placeholder

1. Add a `PlaceholderFetcher` func in `placeholders.go`
2. Register it in `buildSessionPrompt()` in `service.go`
3. Add `{{token}}` to the prompt template
4. No other changes needed — `expandPlaceholders` is lazy (only calls fetcher if token present)
