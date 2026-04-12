# rust

Rust subsystem: the `agent-epsilon-rust` LLM trading agent. A minimal Tokio service that runs a single LLM brain against the shared MCP tool surface.

## Key files

- [`src/agents/simple_llm/service.rs`](src/agents/simple_llm/service.rs) — agent loop: prompt assembly, LLM call, tool dispatch.
- [`src/agents/simple_llm/tools.rs`](src/agents/simple_llm/tools.rs) — tool definitions exposed to the LLM (wrappers over MCP calls).
- [`src/mcp/client.rs`](src/mcp/client.rs) — JSON-RPC MCP client over HTTP.
- [`src/mcp/types.rs`](src/mcp/types.rs) — MCP request/response types.
- [`src/platform/mod.rs`](src/platform/mod.rs) — platform integration (trading server, policy engine wiring).
