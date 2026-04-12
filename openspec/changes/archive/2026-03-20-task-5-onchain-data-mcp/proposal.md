## Why

The agent brain needs on-chain market context — funding rates, open interest, and liquidation volume — to compute the market health score and trigger Tier 2 events. Without this server, Stage 1 (Market Filter) and the Tier 2 liquidation-spike loop cannot function.

## What Changes

- New `onchain_data_mcp` FastAPI/FastMCP server on port 8003
- Four MCP tools: `get_funding_rate`, `get_open_interest`, `get_liquidations`, `get_netflow`
- Binance Futures public REST API covers funding, OI, and liquidations (no auth required)
- `get_netflow` returns a hardcoded neutral response (no upstream call) with a TODO to integrate CryptoQuant when a paid plan is available
- Redis cache on all tools (5 min TTL for funding/OI/liquidations, netflow is static so no cache needed)

## Capabilities

### New Capabilities

- `onchain-data-mcp`: FastMCP server exposing on-chain market signals (funding rate, open interest, liquidations, netflow) via MCP JSON-RPC on port 8003

### Modified Capabilities

- `signal-bundle`: `OnchainData` stub in `SignalBundle` will be filled in with the fields this server returns

## Impact

- New package: `src/onchain_data_mcp/`
- Binance Futures public endpoints (no new API keys required)
- `config/config.example.yaml`: no new keys needed (CryptoQuant deferred)
- `src/common/models/signal_bundle.py`: `OnchainData` fields updated to match tool response shapes
