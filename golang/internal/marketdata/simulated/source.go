package simulated

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"ai-trading-agents/internal/exchange"
	"ai-trading-agents/internal/marketdata"
)

// Source implements marketdata.DataSource with synthetic data.
// Prices are sourced from the shared exchange client for consistency.
// Candles, orderbook, funding, and OI are generated synthetically.
type Source struct {
	exchange   exchange.Client
	markets    []string
	volatility float64 // annualized vol fraction (e.g., 0.80 for 80%)
	history    int     // number of historical candles to pre-generate per interval

	mu          sync.RWMutex
	candleCache map[string][]marketdata.Candle // key: "MARKET:INTERVAL"
	oiHistory   map[string][]oiSnapshot
	log         *zap.Logger
}

type oiSnapshot struct {
	ts    time.Time
	value float64
}

// NewSource creates a SimulatedSource.
// volatility is in percent (e.g., 80 for 80% annualized vol).
// history is the number of candles to pre-generate per market/interval.
func NewSource(exchClient exchange.Client, markets []string, volatility float64, history int, log *zap.Logger) *Source {
	if volatility <= 0 {
		volatility = 80
	}
	if history <= 0 {
		history = 500
	}

	s := &Source{
		exchange:    exchClient,
		markets:     markets,
		volatility:  volatility / 100.0,
		history:     history,
		candleCache: make(map[string][]marketdata.Candle),
		oiHistory:   make(map[string][]oiSnapshot),
		log:         log,
	}

	s.pregenerate()
	return s
}

func (s *Source) Name() string { return "simulated" }

func (s *Source) GetTicker(_ context.Context, symbols []string) ([]marketdata.Ticker, error) {
	if len(symbols) == 0 {
		symbols = s.markets
	}

	now := time.Now().UTC()
	var tickers []marketdata.Ticker

	for _, sym := range symbols {
		price, ok := s.exchange.GetPrice(sym)
		if !ok {
			continue
		}
		pf, _ := price.Float64()
		spread := pf * 0.0005 // 0.05% spread
		bid := decimal.NewFromFloat(pf - spread/2)
		ask := decimal.NewFromFloat(pf + spread/2)

		// Derive 24h stats from the 1h candle cache for consistency.
		var changePct float64
		var high24h, low24h, volume24h float64
		s.mu.RLock()
		candles1h := s.candleCache[sym+":1h"]
		s.mu.RUnlock()
		if len(candles1h) >= 24 {
			last24 := candles1h[len(candles1h)-24:]
			open24, _ := decimal.NewFromString(last24[0].Open)
			if !open24.IsZero() {
				close24, _ := decimal.NewFromString(last24[len(last24)-1].Close)
				changePct, _ = close24.Sub(open24).Div(open24).Mul(decimal.NewFromInt(100)).Float64()
			}
			for _, c := range last24 {
				h, _ := decimal.NewFromString(c.High)
				l, _ := decimal.NewFromString(c.Low)
				v, _ := decimal.NewFromString(c.Volume)
				hf, _ := h.Float64()
				lf, _ := l.Float64()
				vf, _ := v.Float64()
				if hf > high24h {
					high24h = hf
				}
				if low24h == 0 || lf < low24h {
					low24h = lf
				}
				volume24h += vf
			}
		} else {
			// Not enough candle history yet; use price-derived stable values.
			changePct = 0
			high24h = pf * 1.01
			low24h = pf * 0.99
			volume24h = pf * 50000
		}
		high := decimal.NewFromFloat(high24h)
		low := decimal.NewFromFloat(low24h)
		volume := decimal.NewFromFloat(volume24h)

		tickers = append(tickers, marketdata.Ticker{
			Market:       sym,
			Bid:          bid.StringFixed(2),
			Ask:          ask.StringFixed(2),
			Last:         price.StringFixed(2),
			Volume24h:    volume.StringFixed(0),
			Change24hPct: fmt.Sprintf("%.2f", changePct),
			High24h:      high.StringFixed(2),
			Low24h:       low.StringFixed(2),
			Timestamp:    now,
		})
	}

	return tickers, nil
}

func (s *Source) GetCandles(_ context.Context, symbol string, interval marketdata.Interval, limit int, endTime time.Time) ([]marketdata.Candle, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if endTime.IsZero() {
		endTime = time.Now().UTC()
	}

	key := symbol + ":" + string(interval)

	s.mu.RLock()
	cached, ok := s.candleCache[key]
	s.mu.RUnlock()

	if !ok {
		// Generate candles on-the-fly for uncached market/interval combos
		price, exists := s.exchange.GetPrice(symbol)
		if !exists {
			return nil, fmt.Errorf("unknown market: %s", symbol)
		}
		pf, _ := price.Float64()
		cached = generateCandles(pf, interval, s.history, endTime, s.volatility)
		s.mu.Lock()
		s.candleCache[key] = cached
		s.mu.Unlock()
	}

	// Filter: only closed candles before endTime
	var result []marketdata.Candle
	for _, c := range cached {
		candleClose := c.Timestamp.Add(interval.Duration())
		if candleClose.Before(endTime) || candleClose.Equal(endTime) {
			result = append(result, c)
		}
	}

	// Return the last `limit` candles
	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, nil
}

func (s *Source) GetOrderbook(_ context.Context, symbol string, depth int) (*marketdata.Orderbook, error) {
	if depth <= 0 {
		depth = 20
	}

	price, ok := s.exchange.GetPrice(symbol)
	if !ok {
		return nil, fmt.Errorf("unknown market: %s", symbol)
	}
	pf, _ := price.Float64()

	spreadPct := 0.0005 // 0.05%
	midPrice := pf

	bids := make([]marketdata.OrderbookLevel, depth)
	asks := make([]marketdata.OrderbookLevel, depth)

	for i := 0; i < depth; i++ {
		bidPrice := midPrice * (1 - spreadPct/2 - float64(i)*0.0001)
		askPrice := midPrice * (1 + spreadPct/2 + float64(i)*0.0001)

		// Exponential distribution for sizes
		bidSize := 0.5 + rand.ExpFloat64()*2.0
		askSize := 0.5 + rand.ExpFloat64()*2.0

		bids[i] = marketdata.OrderbookLevel{
			Price: fmt.Sprintf("%.2f", bidPrice),
			Size:  fmt.Sprintf("%.4f", bidSize),
		}
		asks[i] = marketdata.OrderbookLevel{
			Price: fmt.Sprintf("%.2f", askPrice),
			Size:  fmt.Sprintf("%.4f", askSize),
		}
	}

	return &marketdata.Orderbook{
		Market:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now().UTC(),
	}, nil
}

func (s *Source) GetMarkets(_ context.Context, _ string) ([]marketdata.MarketInfo, error) {
	prices := s.exchange.GetPrices()
	var markets []marketdata.MarketInfo

	for _, sym := range s.markets {
		base, quote, err := marketdata.ParseCanonical(sym)
		if err != nil {
			continue
		}

		priceStr := "0.00"
		changePct := fmt.Sprintf("%.2f", (rand.Float64()-0.5)*6)
		volume := fmt.Sprintf("%.0f", 50000+rand.Float64()*200000)

		p, hasPrices := prices[sym]
		if hasPrices {
			priceStr = p.StringFixed(2)
			pf, _ := p.Float64()
			volume = fmt.Sprintf("%.0f", pf*(50000+rand.Float64()*100000))
		}

		markets = append(markets, marketdata.MarketInfo{
			Pair:         sym,
			Base:         base,
			Quote:        quote,
			LastPrice:    priceStr,
			Volume24h:    volume,
			Change24hPct: changePct,
			Tradeable:    hasPrices,
		})
	}

	return markets, nil
}

func (s *Source) GetFundingRates(_ context.Context, symbol string, limit int) (*marketdata.FundingData, error) {
	if limit <= 0 {
		limit = 10
	}

	_, ok := s.exchange.GetPrice(symbol)
	if !ok {
		return nil, fmt.Errorf("unknown market: %s", symbol)
	}

	now := time.Now().UTC()

	// Generate funding rate history - oscillates around 0
	history := make([]marketdata.FundingRate, limit)
	rate := 0.0001 * (rand.Float64() - 0.5) * 2 // start near zero
	for i := limit - 1; i >= 0; i-- {
		ts := now.Add(-time.Duration(i) * 8 * time.Hour)
		rate += 0.00005 * (rand.Float64() - 0.5)
		history[limit-1-i] = marketdata.FundingRate{
			Timestamp: ts.Truncate(8 * time.Hour),
			Rate:      fmt.Sprintf("%.6f", rate),
		}
	}

	currentRate := rate
	annualized := currentRate * 3 * 365 * 100 // simulated: always 8h intervals

	// Next funding time: next 8-hour boundary
	nextFunding := now.Truncate(8 * time.Hour).Add(8 * time.Hour)

	return &marketdata.FundingData{
		Market:           symbol,
		CurrentRate:      fmt.Sprintf("%.6f", currentRate),
		AnnualizedPct:    fmt.Sprintf("%.2f", annualized),
		FundingIntervalH: 8,
		NextFundingTime:  nextFunding,
		History:          history,
	}, nil
}

func (s *Source) GetOpenInterest(_ context.Context, symbol string) (*marketdata.OpenInterest, error) {
	price, ok := s.exchange.GetPrice(symbol)
	if !ok {
		return nil, fmt.Errorf("unknown market: %s", symbol)
	}

	pf, _ := price.Float64()
	// OI roughly proportional to price * some multiplier
	baseOI := pf * 500000

	s.mu.Lock()
	snapshots := s.oiHistory[symbol]
	now := time.Now().UTC()

	// Generate a new OI snapshot
	currentOI := baseOI
	if len(snapshots) > 0 {
		last := snapshots[len(snapshots)-1]
		delta := (rand.Float64() - 0.45) * 0.01 * last.value // slight positive bias
		currentOI = last.value + delta
	}
	snapshots = append(snapshots, oiSnapshot{ts: now, value: currentOI})
	// Keep last 100 snapshots
	if len(snapshots) > 100 {
		snapshots = snapshots[len(snapshots)-100:]
	}
	s.oiHistory[symbol] = snapshots
	s.mu.Unlock()

	change1h := 0.0
	change4h := 0.0
	change24h := 0.0
	found1h, found4h, found24h := false, false, false

	// Iterate newest-first to find the closest snapshot to each lookback threshold.
	for i := len(snapshots) - 1; i >= 0; i-- {
		snap := snapshots[i]
		age := now.Sub(snap.ts)
		if !found1h && age >= time.Hour {
			change1h = (currentOI - snap.value) / snap.value * 100
			found1h = true
		}
		if !found4h && age >= 4*time.Hour {
			change4h = (currentOI - snap.value) / snap.value * 100
			found4h = true
		}
		if !found24h && age >= 24*time.Hour {
			change24h = (currentOI - snap.value) / snap.value * 100
			found24h = true
		}
		if found1h && found4h && found24h {
			break
		}
	}

	return &marketdata.OpenInterest{
		Market:         symbol,
		OpenInterest:   fmt.Sprintf("%.0f", currentOI),
		OIChange1hPct:  fmt.Sprintf("%.2f", change1h),
		OIChange4hPct:  fmt.Sprintf("%.2f", change4h),
		OIChange24hPct: fmt.Sprintf("%.2f", change24h),
		LongShortRatio: fmt.Sprintf("%.2f", 0.9+rand.Float64()*0.3),
	}, nil
}

// pregenerate creates historical candles for all configured markets and intervals.
func (s *Source) pregenerate() {
	intervals := []marketdata.Interval{
		marketdata.Interval1m,
		marketdata.Interval5m,
		marketdata.Interval15m,
		marketdata.Interval1h,
		marketdata.Interval4h,
		marketdata.Interval1d,
	}

	now := time.Now().UTC()

	for _, sym := range s.markets {
		price, ok := s.exchange.GetPrice(sym)
		if !ok {
			s.log.Warn("skipping candle pre-generation for unknown market", zap.String("market", sym))
			continue
		}
		pf, _ := price.Float64()

		for _, interval := range intervals {
			key := sym + ":" + string(interval)
			candles := generateCandles(pf, interval, s.history, now, s.volatility)
			s.candleCache[key] = candles
		}

		s.log.Info("pre-generated candles",
			zap.String("market", sym),
			zap.Int("intervals", len(intervals)),
			zap.Int("candles_per_interval", s.history),
		)
	}
}

// generateCandles creates N historical OHLCV candles using Geometric Brownian Motion.
// The last candle closes at endTime. Returns candles in chronological order (oldest first).
func generateCandles(currentPrice float64, interval marketdata.Interval, count int, endTime time.Time, annualVol float64) []marketdata.Candle {
	dur := interval.Duration()
	if dur == 0 {
		return nil
	}

	// Work backwards from endTime to find the start
	// Align endTime to interval boundary
	alignedEnd := endTime.Truncate(dur)

	// Scale annualized vol to per-candle vol
	// Trading year ~= 365 days for crypto (24/7)
	periodsPerYear := float64(365*24*time.Hour) / float64(dur)
	perPeriodVol := annualVol / math.Sqrt(periodsPerYear)

	// Walk price backwards from current to find the starting price,
	// then walk forward to generate candles. Simpler: just generate forward
	// from a derived starting price.
	// Start price = current / exp(drift * N) approximately
	startPrice := currentPrice * math.Exp(-0.0001*float64(count)) // slight downward to get close to current

	candles := make([]marketdata.Candle, count)
	price := startPrice

	for i := 0; i < count; i++ {
		ts := alignedEnd.Add(-dur * time.Duration(count-1-i))

		open := price
		// Simulate intra-candle movement
		high := open
		low := open

		// 4 sub-steps within each candle
		p := open
		for step := 0; step < 4; step++ {
			drift := 0.0001 // slight positive bias
			shock := rand.NormFloat64() * perPeriodVol / 2
			p = p * math.Exp(drift+shock)
			if p > high {
				high = p
			}
			if p < low {
				low = p
			}
		}
		close := p
		price = close

		// Volume: random, loosely correlated with price movement magnitude
		moveSize := math.Abs(close-open) / open
		volume := currentPrice * (10000 + rand.Float64()*50000) * (1 + moveSize*10)

		candles[i] = marketdata.Candle{
			Timestamp: ts,
			Open:      fmt.Sprintf("%.2f", open),
			High:      fmt.Sprintf("%.2f", high),
			Low:       fmt.Sprintf("%.2f", low),
			Close:     fmt.Sprintf("%.2f", close),
			Volume:    fmt.Sprintf("%.0f", volume),
		}
	}

	// Sort chronologically (should already be, but ensure)
	sort.Slice(candles, func(i, j int) bool {
		return candles[i].Timestamp.Before(candles[j].Timestamp)
	})

	return candles
}
