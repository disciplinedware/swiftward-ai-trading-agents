package exchange

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata"
)

// PaperClient executes trades at real market prices without hitting a real exchange.
// Prices are fetched from the DataSource on every trade - no drift, no random walk.
// Use with market_data.source=binance for realistic paper trading.
type PaperClient struct {
	source         marketdata.DataSource
	log            *zap.Logger
	commissionRate decimal.Decimal

	mu         sync.Mutex
	lastPrices map[string]decimal.Decimal // updated on each trade, used for GetPrice/GetPrices
}

// NewPaperClient creates a paper trading client backed by a real market data source.
// commissionRate is the side-dependent fee rate (e.g., 0.001 = 0.1%).
func NewPaperClient(source marketdata.DataSource, log *zap.Logger, commissionRate decimal.Decimal) *PaperClient {
	return &PaperClient{
		source:         source,
		log:            log,
		commissionRate: commissionRate,
		lastPrices:     make(map[string]decimal.Decimal),
	}
}

func (c *PaperClient) SubmitTrade(req *TradeRequest) (*TradeResponse, error) {
	tickers, err := c.source.GetTicker(context.Background(), []string{req.Pair})
	if err != nil {
		return nil, fmt.Errorf("paper trade: get price for %s: %w", req.Pair, err)
	}
	if len(tickers) == 0 {
		return nil, fmt.Errorf("paper trade: no price data for %s", req.Pair)
	}

	price, err := decimal.NewFromString(tickers[0].Last)
	if err != nil {
		return nil, fmt.Errorf("paper trade: invalid price %q for %s: %w", tickers[0].Last, req.Pair, err)
	}
	if price.IsZero() {
		return nil, fmt.Errorf("paper trade: zero price for %s", req.Pair)
	}

	var grossQty decimal.Decimal
	if req.Qty.IsPositive() {
		grossQty = req.Qty.Round(6)
	} else {
		grossQty = req.Value.Div(price).Round(6)
	}
	one := decimal.NewFromInt(1)

	var qty, quoteQty, fee decimal.Decimal
	if req.Side == "buy" {
		// Fee in base: receive less qty.
		fee = grossQty.Mul(c.commissionRate).Round(6)
		qty = grossQty.Sub(fee)
		if req.Qty.IsPositive() {
			quoteQty = grossQty.Mul(price).Round(2)
		} else {
			quoteQty = req.Value // full cash paid
		}
	} else {
		// Fee in quote: receive less cash.
		qty = grossQty // full qty sold
		grossQuote := grossQty.Mul(price)
		fee = grossQuote.Mul(c.commissionRate).Round(2)
		quoteQty = grossQuote.Mul(one.Sub(c.commissionRate)).Round(2)
	}

	fillID := fmt.Sprintf("PAPER-%d", time.Now().UnixNano())

	c.mu.Lock()
	c.lastPrices[req.Pair] = price
	c.mu.Unlock()

	c.log.Info("Paper trade filled",
		zap.String("fill_id", fillID),
		zap.String("pair", req.Pair),
		zap.String("side", req.Side),
		zap.String("price", price.String()),
		zap.String("value", req.Value.String()),
		zap.String("qty", qty.String()),
		zap.String("fee", fee.String()),
	)

	return &TradeResponse{
		FillID:   fillID,
		Status:   StatusFilled,
		Price:    price,
		Qty:      qty,
		QuoteQty: quoteQty,
		Fee:      fee,
		Pair:     req.Pair,
		Side:     req.Side,
	}, nil
}

// GetPrice returns the last known real price for a market.
// Falls back to a live data source fetch when no trade has been made for this market yet.
func (c *PaperClient) GetPrice(market string) (decimal.Decimal, bool) {
	c.mu.Lock()
	p, ok := c.lastPrices[market]
	c.mu.Unlock()
	if ok {
		return p, true
	}
	// No cached price yet - fetch live from the data source.
	tickers, err := c.source.GetTicker(context.Background(), []string{market})
	if err != nil || len(tickers) == 0 {
		return decimal.Zero, false
	}
	price, err := decimal.NewFromString(tickers[0].Last)
	if err != nil || price.IsZero() {
		return decimal.Zero, false
	}
	c.mu.Lock()
	c.lastPrices[market] = price
	c.mu.Unlock()
	return price, true
}

// GetPrices returns last known real prices for all traded markets.
func (c *PaperClient) GetPrices() map[string]decimal.Decimal {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]decimal.Decimal, len(c.lastPrices))
	for k, v := range c.lastPrices {
		out[k] = v
	}
	return out
}
