package exchange

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// ExchangeFill represents a historical fill from the exchange.
// Used by reconciliation to detect DB/exchange drift.
type ExchangeFill struct {
	TradeID string
	OrderID string
	Pair    string          // canonical format: ETH-USD
	Side    string          // "buy" or "sell"
	Price   decimal.Decimal
	Volume  decimal.Decimal // gross base qty
	Cost    decimal.Decimal // total quote
	Fee     decimal.Decimal // fee in quote currency
	Time    time.Time
}

// FillHistoryProvider is an optional interface for exchange clients that
// can return historical fills. Used by reconciliation to detect missing
// DB records and resolve pending trades after crashes.
type FillHistoryProvider interface {
	GetFillHistory(agentID string) ([]ExchangeFill, error)
}

// Exchange-level status values returned in TradeResponse.Status.
const (
	StatusFilled   = "filled"
	StatusRejected = "rejected"
)

// TradeRequest represents a trade to submit to the exchange.
//
// Sizing: pass either Value (notional in quote currency) OR Qty (base qty).
// When Qty is positive, the exchange MUST use it directly and ignore Value.
// This avoids the value/price round-trip that drifts when the live price
// differs from the caller's reference price (e.g. stop-loss exits).
type TradeRequest struct {
	Pair   string          `json:"pair"`
	Side   string          `json:"side"`  // "buy" or "sell"
	Value  decimal.Decimal `json:"value"` // notional in quote currency (used when Qty is zero)
	Qty    decimal.Decimal `json:"qty"`   // base qty; when positive, takes precedence over Value
	Params map[string]any  `json:"params"` // provider-specific params (order_type, etc.); nil = market order
}

// TradeResponse represents the exchange's response.
type TradeResponse struct {
	FillID   string          `json:"fill_id"`
	Status   string          `json:"status"` // "filled", "rejected"
	Price    decimal.Decimal `json:"price"`
	Qty      decimal.Decimal `json:"qty"`       // Base qty: net (after fee) for buy, full for sell.
	QuoteQty decimal.Decimal `json:"quote_qty"` // Quote amount: full value for buy, net (after fee) for sell.
	Fee      decimal.Decimal `json:"fee"`       // Fee amount: in base for buy, in quote for sell.
	Pair     string          `json:"pair"`
	Side     string          `json:"side"`
}

// Client is the exchange interface used by Trading MCP.
type Client interface {
	SubmitTrade(req *TradeRequest) (*TradeResponse, error)
	GetPrice(pair string) (decimal.Decimal, bool)
	GetPrices() map[string]decimal.Decimal
}

// BalanceEntry represents a single asset balance from the exchange.
type BalanceEntry struct {
	Asset     string          `json:"asset"`
	Available decimal.Decimal `json:"available"`
	Reserved  decimal.Decimal `json:"reserved"`
	Total     decimal.Decimal `json:"total"`
}

// BalanceProvider is an optional interface for exchange clients that can
// report real balances. When available, Trading MCP uses these instead of
// computing from our DB (exchange is the source of truth for current state).
type BalanceProvider interface {
	GetBalance() ([]BalanceEntry, error)
}

// AgentClientProvider is an optional interface for exchange clients that
// support per-agent isolation. When implemented, Trading MCP calls
// ForAgent(agentID) to get an agent-scoped Client (e.g., Kraken paper
// with separate HOME dirs per agent).
type AgentClientProvider interface {
	ForAgent(agentID string) Client
}

// StopOrderResult is returned when a native stop order is placed on the exchange.
type StopOrderResult struct {
	OrderID    string          `json:"order_id"`
	Pair       string          `json:"pair"`
	Side       string          `json:"side"`
	StopPrice  decimal.Decimal `json:"stop_price"`
	Qty        decimal.Decimal `json:"qty"`       // 0 = all (close position)
	OrderType  string          `json:"order_type"` // "stop_loss" or "take_profit"
}

// StopOrderProvider is an optional interface for exchange clients that support
// native exchange-level stop orders (Tier 1). When available, position alerts
// with on_trigger=auto_execute use this instead of software polling (Tier 2).
// Implementations: KrakenClient (via kraken --stop-loss CLI flag).
type StopOrderProvider interface {
	PlaceStopOrder(pair, side, orderType string, stopPrice, qty decimal.Decimal) (*StopOrderResult, error)
	CancelStopOrder(orderID string) error
}

// NewClient creates an exchange client based on mode.
// Modes: "sim" (random walk), "kraken" (Kraken CLI), "real" (real exchange API).
// For "paper" mode use NewPaperClient directly - it requires a DataSource.
// For "kraken" mode use NewKrakenClient directly - it requires KrakenConfig.
func NewClient(mode, baseURL string, log *zap.Logger, commissionRate decimal.Decimal) (Client, error) {
	switch mode {
	case "sim", "":
		return NewSimClient(log, commissionRate), nil
	case "paper":
		return nil, fmt.Errorf("paper mode requires a DataSource - create via exchange.NewPaperClient in main")
	case "kraken":
		return nil, fmt.Errorf("kraken mode requires KrakenConfig - create via exchange.NewKrakenClient in main")
	case "real":
		return nil, fmt.Errorf("real exchange client not implemented yet")
	default:
		return nil, fmt.Errorf("unknown exchange mode %q (valid: sim, paper, kraken, real)", mode)
	}
}
