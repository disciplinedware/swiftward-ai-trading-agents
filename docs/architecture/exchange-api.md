# Exchange API Specification

Internal Go interface for trade execution. Not agent-facing - agents interact through MCP tools. The exchange client backs the **Trading MCP** only. Market Data MCP uses `marketdata.DataSource` (separate interface, separate backends).

## Separation of Concerns

```
Agent
  |
  v
MCP Tools (agent-facing: format handling, policy eval, enrichment)
  |              |
  v              v
exchange.Client  marketdata.DataSource
(trades/prices)  (candles, orderbook, funding, OI)
  |              |
  v              v
Exchange Backend  Market Data Backend
(sim/paper/real)  (simulated/binance/composite)
```

| Layer | Responsibility | Examples |
|-------|---------------|----------|
| **MCP** | Agent UX: enrichment, policy gateway, indicators, alerts | `trade/submit_order` adds policy eval; `market/get_candles` adds RSI/EMA |
| **exchange.Client** | Trade execution + price cache | `SubmitTrade` -> fill at current price; `GetPrice` -> last known |
| **marketdata.DataSource** | Market data (candles, ticker, orderbook, funding, OI) | Binance REST, simulated GBM |

## Interface

The exchange client has a minimal interface scoped to what the Trading MCP actually needs:

```go
package exchange

import "github.com/shopspring/decimal"

// Client is the exchange interface used by Trading MCP.
type Client interface {
    // SubmitTrade executes a buy or sell. Returns fill details.
    SubmitTrade(req *TradeRequest) (*TradeResponse, error)

    // GetPrice returns the last known price for a market and whether it exists.
    GetPrice(market string) (decimal.Decimal, bool)

    // GetPrices returns last known prices for all markets.
    GetPrices() map[string]decimal.Decimal
}
```

Market Data MCP does NOT use this interface - it uses `marketdata.DataSource` directly (a richer interface with candles, orderbook, funding rates, etc.). See `internal/marketdata/source.go`.

## Types

```go
// TradeRequest represents a trade to submit to the exchange.
type TradeRequest struct {
    Pair  string          `json:"pair"`
    Side  string          `json:"side"` // "buy" or "sell"
    Value decimal.Decimal `json:"value"` // Trade value in portfolio currency
}

// TradeResponse represents the exchange's response.
type TradeResponse struct {
    ID    string          `json:"id"`
    Status string         `json:"status"` // "fill", "reject"
    Price decimal.Decimal `json:"price"`
    Qty   decimal.Decimal `json:"qty"`
    Pair  string          `json:"pair"`
    Side  string          `json:"side"`
}
```

All monetary fields use `decimal.Decimal` (shopspring/decimal). No float64 anywhere in the exchange layer.

## How MCP Tools Map to Exchange Client

### Market Data MCP -> marketdata.DataSource (NOT exchange.Client)

Market Data MCP uses `marketdata.DataSource` - a separate interface in `internal/marketdata/source.go`. It is NOT wired to the exchange client. This separation means you can run `TRADING__EXCHANGE__MODE=sim` (fake fills) with `TRADING__MARKET_DATA__SOURCES=kraken` (real candles) independently.

| MCP Tool | DataSource Method | MCP Adds |
|----------|------------------|----------|
| `market/get_prices` | `GetTicker(markets)` | Reformats to map |
| `market/get_candles` | `GetCandles(req)` | Indicator computation (EMA, RSI, MACD, etc.), in-memory cache with TTL. CSV format (write to sandbox) planned after Code Sandbox MCP is built. |
| `market/get_orderbook` | `GetOrderbook(market, depth)` | Computes `bid_total`, `ask_total` aggregates |
| `market/list_markets` | `GetMarkets()` | Optional `quote` filter |
| `market/get_funding` | `GetFundingRates(market, limit)` | Adds `signal` classification (neutral/bullish/bearish) |
| `market/get_open_interest` | `GetOpenInterest(market)` | Direct pass-through |
| `market/set_alert` | - | MCP-only. In-memory store, checked by background poller via `GetTicker`. |

### Trading MCP -> exchange.Client

| MCP Tool | Exchange Method | MCP Adds |
|----------|----------------|----------|
| `trade/submit_order` | `SubmitTrade(req)` | Validation pipeline: balance check -> Swiftward policy -> RiskRouter on-chain -> exchange fill -> DB persist -> evidence hash -> attestation. See [submit-order-pipeline.md](submit-order-pipeline.md). |
| `trade/estimate_order` | `GetPrice(market)` | Computes estimated fill qty, checks cash, shows position % after. No state changes. |
| `trade/get_portfolio` | (reads from DB) | Cash, open positions (computed from trade log), trade count, equity. |
| `trade/get_history` | (reads from DB) | Trade log with verdict, decision hash. Filters by market/verdict. |
| `trade/get_portfolio_history` | (reads from DB) | Equity curve from `equity_after` on approved trades. |
| `trade/get_limits` | (reads from DB) | Current equity, drawdown %, largest position %, halt state. |
| `trade/heartbeat` | `GetPrices()` + (reads from DB) | Recomputes equity, updates peak if higher, returns drawdown. |

### Key Insight: MCP is NOT a thin wrapper

The MCP layer does significant work:
- **Policy evaluation** on every trade (the core value prop)
- **Format conversion** (CSV for analysis, JSON for quick decisions)
- **Data enrichment** (combine exchange data + position tracker + policy state)
- **Agent-specific state** (alerts, portfolio history, halt flags)
- **Cross-sandbox data transfer** (candle CSV -> code sandbox)

The exchange client is a clean, thin abstraction. The MCP layer is where business logic lives.

## Backends

### 1. Simulated (`TRADING__EXCHANGE__MODE=sim`, default)

Generates fake but realistic prices via Geometric Brownian Motion. No external dependencies. Used for local development and CI.

The `SimClient` owns a price map updated by a background ticker. `SubmitTrade` fills instantly at the current simulated price. `GetPrice`/`GetPrices` return the last known simulated price.

Config knobs (via `TRADING__MARKET_DATA__*` since the simulated Market Data source shares the same GBM engine):
- `TRADING__MARKET_DATA__MARKETS` - markets to simulate (default `ETH-USDC,BTC-USDC,SOL-USDC`)
- `TRADING__MARKET_DATA__VOLATILITY` - annualized vol % (default `80`)
- `TRADING__MARKET_DATA__CANDLE_HISTORY` - pre-generated historical candles (default `500`)

### 2. Paper (`TRADING__EXCHANGE__MODE=paper`)

Real market prices, fake fills. Uses Binance public API to fetch the current price on every `SubmitTrade`. No API key required - Binance's public ticker endpoint is unauthenticated.

```
TRADING__EXCHANGE__MODE=paper
```

Behavior:
- **Prices**: Fetched live from Binance on every trade via `GetTicker`. Accurate to the current market.
- **Fills**: Simulated - no order is sent anywhere. Fill is always at the fetched price, quantity = `value / price`.
- **Order ID**: `PAPER-{unix_nano}` - clearly distinguishable in logs and DB.
- **`GetPrice`/`GetPrices`**: Returns the last price fetched during a `SubmitTrade`. Empty map until first trade on a given market.
- **Use case**: Realistic strategy backtesting where price accuracy matters but real capital must not be risked.

Implementation: `golang/internal/exchange/paper_client.go` (`PaperClient` struct, implements `exchange.Client`).

Note: the execution mode (`TRADING__EXCHANGE__MODE`) is independent of the market data source chain (`TRADING__MARKET_DATA__SOURCES`, default `kraken,bybit`). You can run `paper` execution with real Kraken candles by leaving the default source chain in place.

### 3. Kraken Paper (`TRADING__EXCHANGE__MODE=kraken_paper`) - DEFAULT

Wraps the Kraken CLI binary in sandbox mode. Each agent gets an isolated `$10k USD` paper account via per-agent `HOME` override. No API key required. This is the default for local dev and the hackathon demo.

```
TRADING__EXCHANGE__MODE=kraken_paper
TRADING__EXCHANGE__KRAKEN_BIN=kraken
TRADING__EXCHANGE__KRAKEN_STATE_DIR=/data/kraken
```

Implementation: `golang/internal/exchange/kraken_client.go` wired in `golang/cmd/server/main.go:106`.

### 4. Kraken Real (`TRADING__EXCHANGE__MODE=kraken_real`)

Same wrapper, live trading. Reads `KRAKEN_API_KEY` and `KRAKEN_API_SECRET` from environment. Used for the Kraken Challenge PnL leaderboard.

### 5. Real (`TRADING__EXCHANGE__MODE=real`)

Generic real-exchange slot. Not implemented - `kraken_real` covers the shipped live path.


## Implementation Status

### Shipped

- `exchange.Client` interface: `SubmitTrade`, `GetPrice`, `GetPrices`
- Optional interfaces: `BalanceProvider`, `FillHistoryProvider`, `StopOrderProvider`, `AgentClientProvider`
- `SimClient`: GBM random walk prices, instant fills
- `PaperClient`: live Binance prices via `GetTicker`, fake fills, price cache
- `KrakenClient`: wraps Kraken CLI binary, supports paper (`kraken_paper`) and live (`kraken_real`), native stop orders, per-agent HOME isolation, fill history recovery
- `NewClient` factory for `sim` / `paper` / `kraken` / `real` (paper and kraken require direct constructors because they need a `DataSource` or `KrakenConfig`)
- Main wiring in `golang/cmd/server/main.go:100-125`

### Phase 2: Hackathon Backend (Day 1)

1. Read exchange API docs (Risk Router ABI + Capital Vault)
2. Implement `RealClient` in `internal/exchange/real_client.go` (or similar)
3. Extend `Client` interface if needed (e.g. cancel order, get positions)
4. Swap via config: `TRADING__EXCHANGE__MODE=real`
5. Test with `trade/estimate_order` (dry-run) before live trades

### File Layout

```
golang/internal/exchange/
    client.go          # Client interface + NewClient factory
    sim_client.go      # SimClient - GBM random walk prices, instant fills
    paper_client.go    # PaperClient - real Binance prices, fake fills
```

## Design Decisions

1. **`decimal.Decimal` for all financial values.** float64 causes rounding errors. Every price, size, and PnL uses `decimal.Decimal`. Logged as strings, never floats.

2. **Trade execution separated from market data.** `exchange.Client` only handles fills and price cache. Candles, orderbook, funding, OI go through `marketdata.DataSource`. This lets each be configured independently (e.g. sim trades + binance candles).

3. **Minimal interface, expand when needed.** The hackathon exchange API is unknown until Day 1. The current 3-method interface (`SubmitTrade`, `GetPrice`, `GetPrices`) covers everything built so far. Extend it when the real exchange requires cancel, modify, etc.
