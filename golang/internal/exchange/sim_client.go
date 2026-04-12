package exchange

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

type marketInfo struct {
	price    decimal.Decimal
	priceDec int32 // price decimal places
	qtyDec   int32 // quantity decimal places
}

// SimClient returns simulated fills with random price movement.
type SimClient struct {
	log            *zap.Logger
	commissionRate decimal.Decimal
	mu             sync.Mutex
	markets        map[string]*marketInfo
}

// NewSimClient creates a simulated exchange client.
// commissionRate is the side-dependent fee rate (e.g., 0.001 = 0.1%).
// Buy: fee in base (receive less qty). Sell: fee in quote (receive less cash).
func NewSimClient(log *zap.Logger, commissionRate decimal.Decimal) *SimClient {
	return &SimClient{
		log:            log,
		commissionRate: commissionRate,
		markets: map[string]*marketInfo{
			"BTC-USDC": {price: decimal.NewFromInt(65000), priceDec: 2, qtyDec: 8},
			"BTC-USDT": {price: decimal.NewFromInt(65000), priceDec: 2, qtyDec: 8},
			"ETH-USDC": {price: decimal.NewFromInt(2500), priceDec: 2, qtyDec: 6},
			"ETH-USDT": {price: decimal.NewFromInt(2500), priceDec: 2, qtyDec: 6},
		},
	}
}

func (c *SimClient) SubmitTrade(req *TradeRequest) (*TradeResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	m, ok := c.markets[req.Pair]
	if !ok {
		return &TradeResponse{
			Status: StatusRejected,
			Pair:   req.Pair,
			Side:   req.Side,
		}, fmt.Errorf("unknown pair: %s", req.Pair)
	}

	// Simulate small price movement (+-0.1%)
	movement := decimal.NewFromFloat((rand.Float64() - 0.5) * 0.002).Mul(m.price)
	price := m.price.Add(movement).Round(m.priceDec)
	m.price = price

	var grossQty decimal.Decimal
	if req.Qty.IsPositive() {
		grossQty = req.Qty.Round(m.qtyDec)
	} else {
		grossQty = req.Value.Div(price).Round(m.qtyDec)
	}
	one := decimal.NewFromInt(1)

	var qty, quoteQty, fee decimal.Decimal
	if req.Side == "buy" {
		// Fee in base: receive less qty.
		fee = grossQty.Mul(c.commissionRate).Round(m.qtyDec)
		qty = grossQty.Sub(fee)
		if req.Qty.IsPositive() {
			quoteQty = grossQty.Mul(price).Round(m.priceDec)
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

	fillID := fmt.Sprintf("FILL-%d", time.Now().UnixNano())

	c.log.Info("Trade filled (simulated)",
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

func (c *SimClient) GetPrice(market string) (decimal.Decimal, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.markets[market]
	if !ok {
		return decimal.Zero, false
	}
	return m.price, true
}

func (c *SimClient) GetPrices() map[string]decimal.Decimal {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]decimal.Decimal, len(c.markets))
	for k, m := range c.markets {
		out[k] = m.price
	}
	return out
}
