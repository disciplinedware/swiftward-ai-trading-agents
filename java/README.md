# java

Java subsystem: the `agent-gamma-java` trading agent. A Spring Boot service running a strategy-driven brain against the shared MCP tool surface.

## Key files

- [`src/main/java/com/trading/agent/GammaAgent.java`](src/main/java/com/trading/agent/GammaAgent.java) — agent orchestrator: scheduled loop, MCP calls, trade dispatch.
- [`src/main/java/com/trading/agent/brain/StrategyBrain.java`](src/main/java/com/trading/agent/brain/StrategyBrain.java) — strategy-driven decision logic.
- [`src/main/java/com/trading/mcp/McpClient.java`](src/main/java/com/trading/mcp/McpClient.java) — JSON-RPC MCP client over HTTP.
