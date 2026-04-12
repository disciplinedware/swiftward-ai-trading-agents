package marketdata

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ChainSource tries each DataSource in order, returning the first successful result.
// If all sources fail, returns the last error. Never falls back to simulated/fake data -
// only real sources should be in the chain.
type ChainSource struct {
	sources []DataSource
	log     *zap.Logger
}

// NewChainSource creates a degradation chain from an ordered list of real data sources.
// First source is primary; subsequent sources are fallbacks tried on error.
func NewChainSource(sources []DataSource, log *zap.Logger) *ChainSource {
	return &ChainSource{sources: sources, log: log}
}

func (c *ChainSource) Name() string {
	names := make([]string, len(c.sources))
	for i, s := range c.sources {
		names[i] = s.Name()
	}
	return "chain(" + strings.Join(names, "->") + ")"
}

func (c *ChainSource) GetTicker(ctx context.Context, symbols []string) ([]Ticker, error) {
	return tryChain(c, "GetTicker", func(s DataSource) ([]Ticker, error) {
		return s.GetTicker(ctx, symbols)
	})
}

func (c *ChainSource) GetCandles(ctx context.Context, symbol string, interval Interval, limit int, endTime time.Time) ([]Candle, error) {
	return tryChain(c, "GetCandles", func(s DataSource) ([]Candle, error) {
		return s.GetCandles(ctx, symbol, interval, limit, endTime)
	})
}

func (c *ChainSource) GetOrderbook(ctx context.Context, symbol string, depth int) (*Orderbook, error) {
	return tryChain(c, "GetOrderbook", func(s DataSource) (*Orderbook, error) {
		return s.GetOrderbook(ctx, symbol, depth)
	})
}

func (c *ChainSource) GetMarkets(ctx context.Context, quote string) ([]MarketInfo, error) {
	return tryChain(c, "GetMarkets", func(s DataSource) ([]MarketInfo, error) {
		return s.GetMarkets(ctx, quote)
	})
}

func (c *ChainSource) GetFundingRates(ctx context.Context, symbol string, limit int) (*FundingData, error) {
	return tryChain(c, "GetFundingRates", func(s DataSource) (*FundingData, error) {
		return s.GetFundingRates(ctx, symbol, limit)
	})
}

func (c *ChainSource) GetOpenInterest(ctx context.Context, symbol string) (*OpenInterest, error) {
	return tryChain(c, "GetOpenInterest", func(s DataSource) (*OpenInterest, error) {
		return s.GetOpenInterest(ctx, symbol)
	})
}

// tryChain tries each source in order, logging failures and falling through to the next.
func tryChain[T any](c *ChainSource, op string, fn func(DataSource) (T, error)) (T, error) {
	var lastErr error
	for i, src := range c.sources {
		result, err := fn(src)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if i < len(c.sources)-1 {
			c.log.Warn("source failed, trying next",
				zap.String("op", op),
				zap.String("source", src.Name()),
				zap.String("next", c.sources[i+1].Name()),
				zap.Error(err),
			)
		}
	}
	var zero T
	return zero, fmt.Errorf("%s: all sources failed: %w", op, lastErr)
}
