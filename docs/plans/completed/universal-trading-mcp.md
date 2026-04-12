# Universal Trading MCP

> **Status**: ✅ Shipped
> **Package**: `golang/internal/mcps/trading/`
> **Exchange clients**: `golang/internal/exchange/`

## What was built

The Universal Trading MCP is a unified agent-facing interface that abstracts exchange differences. A single set of tools (submit_order, get_portfolio, estimate_order, set_conditional, ...) works across Kraken real trading, Kraken paper trading, a simulated (GBM) source, and a "paper-against-live-prices" adapter. Agents never touch exchange-specific APIs. The MCP also integrates Swiftward policy evaluation and an optional on-chain RiskRouter check before execution, and posts ERC-8004 attestation evidence after fills.

## MCP tools

| Tool | What it does | File:line |
|------|--------------|-----------|
| `trade/submit_order` | Submit a market order; goes through policy + chain gates | `service.go:1864` |
| `trade/get_portfolio` | Current positions + equity snapshot | `service.go:2951` |
| `trade/get_history` | Fill and rejection history | `service.go:3097` |
| `trade/get_limits` | Live policy limits from Swiftward | `service.go:3179` |
| `trade/get_portfolio_history` | Equity snapshots over time | `service.go:3247` |
| `trade/estimate_order` | Fee + slippage estimate before submission | `service.go:3285` |
| `trade/heartbeat` | Price refresh and agent liveness update | `service.go:3364` |
| `trade/set_conditional` | Software SL/TP/price alert | `service.go:3456` |
| `trade/set_reminder` | Time-based alert | `service.go:3789` |
| `trade/cancel_conditional` | Cancel a software alert (and native exchange stop if Tier 1) | `service.go:3685` |
| `trade/end_cycle` | Close a trading cycle; update peak equity | `service.go:3890` |
| `alert/triggered` | Consume triggered alert notifications | `service.go:3583` |
| `alert/list` | Active alerts with distance-to-trigger | `service.go:3623` |

All tools are served at `POST /mcp/trading` through the chi router (`service.go:800`), and agent identity is carried via the `X-Agent-ID` request header, extracted into context at `service.go:1820`.

## Exchange abstraction

The `Client` interface (`golang/internal/exchange/client.go:65-69`) is the contract every venue implements:

```go
type Client interface {
    SubmitTrade(req *TradeRequest) (*TradeResponse, error)
    GetPrice(pair string) (decimal.Decimal, bool)
    GetPrices() map[string]decimal.Decimal
}
```

Richer venues opt in to additional interfaces:

- `BalanceProvider` (`client.go:82-84`) - fetch real exchange balances
- `FillHistoryProvider` (`client.go:28-30`) - pull fills for crash recovery
- `StopOrderProvider` (`client.go:108-111`) - place and cancel native exchange stop orders (Tier 1 protection)
- `AgentClientProvider` (`client.go:90-92`) - per-agent isolation via `ForAgent(agentID)` returning a scoped client

Exchange selection is a startup decision: config picks the mode (`sim`, `kraken`, `paper`), and the Trading MCP is handed one client. Tools never branch on venue.

## Clients shipped

- **`kraken_client.go`** - adapter over the Kraken CLI binary. Supports paper trading (sandbox, $10k simulated USD) and live trading with `KRAKEN_API_KEY`. Implements `StopOrderProvider` for native SL/TP, `FillHistoryProvider` for recovery, and `AgentClientProvider` via HOME-directory isolation per agent (`kraken_client.go:67-95`, `:505+`).
- **`paper_client.go`** - paper trading at live market prices. Fetches real tickers from a `DataSource` on every trade, so fills price exactly at current market. Implements `BalanceProvider`.
- **`sim_client.go`** - fully simulated exchange with synthetic random-walk price movement (±0.1% per trade). Pre-seeds BTC-USDC, BTC-USDT, ETH-USDC, ETH-USDT with realistic decimal precision. Used for local dev and unit tests.

## Policy integration

`trade/submit_order` goes through two gates before it ever reaches the exchange:

1. **Swiftward policy** (`service.go:2089-2091`) - the MCP posts a `trade_order` event enriched with order details, portfolio state, drawdown, concentration, stop-loss distance, and inversion flags. Swiftward returns `ACCEPT` or `REJECT` with reasoning. The gate is **fail-closed**: if Swiftward is unreachable after retries, the trade is rejected (`service.go:2083-2107`).
2. **On-chain RiskRouter** (`service.go:2152-2245`) - if configured, the MCP submits a signed intent (tokenId = agent identity, nonce, order details, ECDSA signature) to the on-chain RiskRouter contract. Rejects are permanent; technical errors retry with exponential backoff then fail; approvals proceed to the exchange (`service.go:2220-2232`).

Post-fill, the MCP posts an attestation event on-chain as evidence. A per-agent circuit breaker disables attestation for the process lifetime if it fails (`service.go:96`, `:109-117`, `:2539-2543`) so one bad credential does not hammer the chain.

## Key files

- `service.go` (~4,600 lines) - Trading MCP: tool handlers, policy gates, reconciliation, attestation, alert polling
- `client.go` - `Client` interface, optional provider interfaces, `TradeRequest`/`TradeResponse`, `NewClient()` factory
- `kraken_client.go` - Kraken CLI adapter, paper + live modes, stop orders, fill history, per-agent isolation
- `paper_client.go` - paper trading against live market data
- `sim_client.go` - simulated venue with random-walk price model
- `service_test.go` - table-driven tool handler tests with `testTradingService`, `callMCP`, `extractResult` helpers
- `kraken_client_test.go` - table-driven tests for Kraken pair format (BTC-USD ↔ XBTUSD, DOGE-USD ↔ XDGUSD) and fill parsing
- `paper_client_test.go` - paper client price caching, source fallback, fill computation

## Notes

- **Decimal precision**: all monetary values use `shopspring/decimal`. `float64` never touches money except when serializing for wire format.
- **Logging**: structured zap, with separate loggers for `orderLog`, `alertLog`, `chainLog`, `policyLog`, and `reconcileLog`.
- **Evidence endpoint**: `GET /v1/evidence/{hash}` (`service.go:815`) serves the on-chain attestation data publicly so auditors can verify any trade decision.
- **Known limits**: only market orders are exposed to agents today. Limit orders are a Tier-2 extension planned post-hackathon. Per-agent Kraken isolation requires a configured state directory; without it, all agents share one Kraken account.
