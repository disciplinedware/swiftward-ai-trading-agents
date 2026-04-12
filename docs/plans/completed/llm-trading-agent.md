# Go LLM Trading Agent (Delta)

> **Status**: ✅ Shipped
> **Package**: `golang/internal/agents/simple_llm/`
> **Entry**: `golang/cmd/server/main.go` (role-based dispatcher; `TRADING__ROLE=llm_agent`)
> **Compose**: `agent-delta-go` (profile: `delta`)

## What was built

A production Go LLM trading agent that uses OpenAI-compatible tool calling to drive the MCP toolchain. Delta autonomously runs on a configurable interval (default 5 minutes), discovers tools at startup from six MCP servers, fetches live context (portfolio + market prices + memory), asks the LLM what to do next, and routes tool calls back to the right MCP. It supports three run modes: `once` (single tick), `cli` (interactive stdin loop for debugging), and `server` (infinite ticker).

## How it works

**Startup**:
1. Discover tools from all MCP clients (`MCPToolset.DiscoverTools`) in deterministic sorted order - critical for prompt caching
2. Sanitize tool names for OpenAI (replace `/` with `__` so `trade/submit_order` becomes `trade__submit_order`)
3. Convert to OpenAI function-calling schema

**Per-session flow** (`runSession`, ~395 lines):
1. Expand placeholders in the system prompt: `{{memory}}` from `files/read memory/MEMORY.md`, `{{market_context}}` from `trade/get_portfolio` + `market/get_prices`, `{{max_steps}}` from config
2. Inject current UTC time to anchor temporal reasoning
3. Call the LLM with system prompt + user directive
4. If response contains tool calls, route each call to the correct MCP by prefix (`trade/` → Trading MCP, `market/` → Market Data MCP, ...) and feed results back as tool messages
5. Loop until the model produces a final text response or hits `MaxIterations` (default 10)

**State**: persistent memory lives in the agent workspace (`memory/MEMORY.md`), read at the start of every session but only written when the agent explicitly asks.

**Observability**: every LLM request, tool call, and result is logged with timing, token counts (including cache hits), and zap structured fields for log aggregation.

## Tools used

- **Trading MCP** (`trade/*`, `alert/*`) - execute orders, query portfolio, manage alerts
- **Market Data MCP** (`market/*`) - prices, candles, indicators, orderbook, funding, OI
- **Files MCP** (`files/*`) - read / write / edit memory and session logs
- **News MCP** (`news/*`) - sentiment and event feeds
- **Code Sandbox MCP** (`code/*`) - Python analysis in isolated container
- **Polymarket MCP** (`polymarket/*`) - prediction market events and markets

## Key files

- `golang/internal/agents/simple_llm/service.go` - lifecycle, `runOnce` / `runCLI` / `runServer` modes, `runSession` tool-calling loop, `stripThinking` for CoT models
- `golang/internal/agents/simple_llm/tools.go` - `MCPToolset`, `DiscoverTools`, `ToOpenAITools`, `CallTool` (sanitized-name → real-tool routing)
- `golang/internal/agents/simple_llm/placeholders.go` - lazy placeholder expansion, `loadMemoryContext`, `fetchMarketContext` (portfolio first, then config markets)
- `golang/internal/agents/simple_llm/service_test.go` - table-driven tests with `mockChatClient`, covering all three modes, the tool-calling loop, and placeholder expansion
- `golang/internal/agents/simple_llm/tools_test.go` - name sanitization, routing, tool discovery order
- `golang/internal/agents/simple_llm/placeholders_test.go` - lazy loading, portfolio-first ordering, missing-MCP fallback
- `golang/internal/config/config.go` - `LLMAgentConfig` (agent ID, API keys, MCP URLs, OpenAI key, LLM URL, model, max iterations, prompt file, mode, interval)

## Notes

- **Prompt caching**: deterministic sort order for tool discovery + conversion means tool definitions are byte-identical across runs, so OpenAI's prompt cache amortizes cost and latency.
- **Error recovery**: tool call failures are non-fatal - the error message becomes a tool result message and the LLM can retry, switch tools, or decide without the data. Market context and memory prefetch failures degrade gracefully with empty sections rather than aborting the tick.
- **Agent header**: every HTTP request (LLM and MCP) carries `X-Agent-ID` for policy evaluation and audit trails.
- **Code sandbox cold start**: the first `code/execute` can take 30-60 seconds as Docker pulls the sandbox image. The code MCP client timeout is long (default 5 minutes, `CODE_MCP_TIMEOUT`) to accommodate this.
- **Why simpler than the Claude Code agent**: Delta is the minimal viable LLM agent - one process, one LLM call per step, no stream-json parsing, no Docker jail. It lives in the same binary as the trading server and is ideal for baseline comparisons.
