package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/marketdata"
	"ai-trading-agents/internal/mcp"
)

// mockSource implements marketdata.DataSource for testing.
type mockSource struct {
	tickers          []marketdata.Ticker
	candles          []marketdata.Candle
	markets          []marketdata.MarketInfo
	orderbook        *marketdata.Orderbook
	fundingData      *marketdata.FundingData
	openInterest     *marketdata.OpenInterest
	tickErr          error
	candErr          error
	mktErr           error
	obErr            error
	fundErr          error
	oiErr            error
	candleCallCount  int // incremented on each GetCandles call
}

func (m *mockSource) Name() string { return "mock" }

func (m *mockSource) GetTicker(_ context.Context, _ []string) ([]marketdata.Ticker, error) {
	return m.tickers, m.tickErr
}

func (m *mockSource) GetCandles(_ context.Context, _ string, _ marketdata.Interval, _ int, _ time.Time) ([]marketdata.Candle, error) {
	m.candleCallCount++
	return m.candles, m.candErr
}

func (m *mockSource) GetOrderbook(_ context.Context, _ string, _ int) (*marketdata.Orderbook, error) {
	return m.orderbook, m.obErr
}

func (m *mockSource) GetMarkets(_ context.Context, quote string) ([]marketdata.MarketInfo, error) {
	if m.mktErr != nil {
		return nil, m.mktErr
	}
	if quote == "" {
		return m.markets, nil
	}
	quote = strings.ToUpper(quote)
	var filtered []marketdata.MarketInfo
	for _, mk := range m.markets {
		if strings.EqualFold(mk.Quote, quote) {
			filtered = append(filtered, mk)
		}
	}
	return filtered, nil
}

func (m *mockSource) GetFundingRates(_ context.Context, _ string, _ int) (*marketdata.FundingData, error) {
	return m.fundingData, m.fundErr
}

func (m *mockSource) GetOpenInterest(_ context.Context, _ string) (*marketdata.OpenInterest, error) {
	return m.openInterest, m.oiErr
}

func newTestService(src marketdata.DataSource) *Service {
	return &Service{
		log:    zap.NewNop(),
		source: src,
		cache:  NewCache(),
		repo:   &db.MemRepository{},
		now:    time.Now,
	}
}

func newTestServiceWithClock(src marketdata.DataSource, now func() time.Time) *Service {
	return &Service{
		log:    zap.NewNop(),
		source: src,
		cache:  NewCache(),
		repo:   &db.MemRepository{},
		now:    now,
	}
}

// agentCtx returns a context with the given agent ID set (simulates X-Agent-ID header middleware).
func agentCtx(agentID string) context.Context {
	return context.WithValue(context.Background(), agentIDContextKey, agentID)
}

func TestToolGetPrices(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		args    map[string]any
		source  *mockSource
		wantErr string
		check   func(t *testing.T, result map[string]any)
	}{
		{
			name:    "missing markets",
			args:    map[string]any{},
			source:  &mockSource{},
			wantErr: "markets is required",
		},
		{
			name:    "empty markets",
			args:    map[string]any{"markets": []any{}},
			source:  &mockSource{},
			wantErr: "markets is required",
		},
		{
			name: "valid request",
			args: map[string]any{"markets": []any{"ETH-USDC"}},
			source: &mockSource{
				tickers: []marketdata.Ticker{
					{Market: "ETH-USDC", Bid: "3200.00", Ask: "3202.00", Last: "3201.00", Volume24h: "1000000", Change24hPct: "1.50", High24h: "3250.00", Low24h: "3100.00", Timestamp: now},
				},
			},
			check: func(t *testing.T, result map[string]any) {
				prices, ok := result["prices"].([]any)
				if !ok || len(prices) != 1 {
					t.Fatalf("expected 1 price, got %v", result["prices"])
				}
				p := prices[0].(map[string]any)
				if p["market"] != "ETH-USDC" {
					t.Errorf("expected market ETH-USDC, got %v", p["market"])
				}
				if result["source"] != "mock" {
					t.Errorf("expected source mock, got %v", result["source"])
				}
			},
		},
		{
			name:    "source error",
			args:    map[string]any{"markets": []any{"ETH-USDC"}},
			source:  &mockSource{tickErr: fmt.Errorf("connection refused")},
			wantErr: "get prices: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(tt.source)
			result, err := svc.toolGetPrices(context.Background(), tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			parsed := parseResult(t, result)
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}

func TestToolGetCandles(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Hour)
	sampleCandles := []marketdata.Candle{
		{Timestamp: now.Add(-2 * time.Hour), Open: "3100.00", High: "3120.00", Low: "3095.00", Close: "3115.00", Volume: "50000000"},
		{Timestamp: now.Add(-1 * time.Hour), Open: "3115.00", High: "3140.00", Low: "3110.00", Close: "3135.00", Volume: "45000000"},
	}

	tests := []struct {
		name    string
		args    map[string]any
		source  *mockSource
		wantErr string
		check   func(t *testing.T, result map[string]any)
	}{
		{
			name:    "missing market",
			args:    map[string]any{"interval": "1h"},
			source:  &mockSource{},
			wantErr: "market and interval are required",
		},
		{
			name:    "missing interval",
			args:    map[string]any{"market": "ETH-USDC"},
			source:  &mockSource{},
			wantErr: "market and interval are required",
		},
		{
			name:    "invalid interval",
			args:    map[string]any{"market": "ETH-USDC", "interval": "3h"},
			source:  &mockSource{},
			wantErr: "invalid interval",
		},
		{
			name: "json format",
			args: map[string]any{"market": "ETH-USDC", "interval": "1h", "limit": float64(100)},
			source: &mockSource{candles: sampleCandles},
			check: func(t *testing.T, result map[string]any) {
				if result["market"] != "ETH-USDC" {
					t.Errorf("expected market ETH-USDC, got %v", result["market"])
				}
				candles, ok := result["candles"].([]any)
				if !ok || len(candles) != 2 {
					t.Fatalf("expected 2 candles, got %v", result["candles"])
				}
				c := candles[0].(map[string]any)
				if c["o"] != "3100.00" {
					t.Errorf("expected open 3100.00, got %v", c["o"])
				}
			},
		},
		{
			name:   "csv format",
			args:   map[string]any{"market": "ETH-USDC", "interval": "1h", "format": "csv", "limit": float64(100)},
			source: &mockSource{candles: sampleCandles},
			check: func(t *testing.T, result map[string]any) {
				if result["format"] != "csv" {
					t.Errorf("expected format csv, got %v", result["format"])
				}
				data, ok := result["data"].(string)
				if !ok || data == "" {
					t.Fatal("expected non-empty csv data")
				}
				if !strings.Contains(data, "timestamp,open,high,low,close,volume") {
					t.Error("CSV missing header row")
				}
				lines := strings.Split(strings.TrimSpace(data), "\n")
				if len(lines) != 3 { // header + 2 data rows
					t.Errorf("expected 3 CSV lines, got %d", len(lines))
				}
			},
		},
		{
			name: "limit capped at 720",
			args: map[string]any{"market": "ETH-USDC", "interval": "1h", "limit": float64(5000)},
			source: &mockSource{candles: sampleCandles},
			check: func(t *testing.T, result map[string]any) {
				// Should not error, just cap
				count := result["count"].(float64)
				if count != 2 {
					t.Errorf("expected 2 candles, got %.0f", count)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(tt.source)
			result, err := svc.toolGetCandles(context.Background(), tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			parsed := parseResult(t, result)
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}

func TestToolListMarkets(t *testing.T) {
	sampleMarkets := []marketdata.MarketInfo{
		{Pair: "ETH-USDC", Base: "ETH", Quote: "USDC", LastPrice: "3200.00", Volume24h: "1000000", Change24hPct: "2.50", Tradeable: true},
		{Pair: "BTC-USDC", Base: "BTC", Quote: "USDC", LastPrice: "65000.00", Volume24h: "5000000", Change24hPct: "1.20", Tradeable: true},
		{Pair: "SOL-USDT", Base: "SOL", Quote: "USDT", LastPrice: "150.00", Volume24h: "500000", Change24hPct: "-0.80", Tradeable: false},
	}

	tests := []struct {
		name    string
		args    map[string]any
		source  *mockSource
		wantErr string
		check   func(t *testing.T, result map[string]any)
	}{
		{
			name:   "all markets",
			args:   map[string]any{},
			source: &mockSource{markets: sampleMarkets},
			check: func(t *testing.T, result map[string]any) {
				count := result["count"].(float64)
				if count != 3 {
					t.Errorf("expected 3 markets, got %.0f", count)
				}
			},
		},
		{
			name:   "filter by quote",
			args:   map[string]any{"quote": "USDC"},
			source: &mockSource{markets: sampleMarkets},
			check: func(t *testing.T, result map[string]any) {
				count := result["count"].(float64)
				if count != 2 {
					t.Errorf("expected 2 USDC markets, got %.0f", count)
				}
			},
		},
		{
			name:   "sort by name",
			args:   map[string]any{"sort_by": "name"},
			source: &mockSource{markets: sampleMarkets},
			check: func(t *testing.T, result map[string]any) {
				mkts := result["markets"].([]any)
				first := mkts[0].(map[string]any)["pair"].(string)
				if first != "BTC-USDC" {
					t.Errorf("expected first market BTC-USDC (alphabetical), got %s", first)
				}
			},
		},
		{
			name:   "sort by change",
			args:   map[string]any{"sort_by": "change"},
			source: &mockSource{markets: sampleMarkets},
			check: func(t *testing.T, result map[string]any) {
				mkts := result["markets"].([]any)
				first := mkts[0].(map[string]any)["pair"].(string)
				if first != "ETH-USDC" {
					t.Errorf("expected ETH-USDC first (highest change), got %s", first)
				}
			},
		},
		{
			name:   "limit results",
			args:   map[string]any{"limit": float64(1)},
			source: &mockSource{markets: sampleMarkets},
			check: func(t *testing.T, result map[string]any) {
				count := result["count"].(float64)
				if count != 1 {
					t.Errorf("expected 1 market (limited), got %.0f", count)
				}
			},
		},
		{
			name:   "sort by volume (default)",
			args:   map[string]any{},
			source: &mockSource{markets: sampleMarkets},
			check: func(t *testing.T, result map[string]any) {
				mkts := result["markets"].([]any)
				// BTC-USDC has highest volume (5000000), must come first.
				first := mkts[0].(map[string]any)["pair"].(string)
				if first != "BTC-USDC" {
					t.Errorf("expected BTC-USDC first (highest volume), got %s", first)
				}
			},
		},
		{
			name:    "source error",
			args:    map[string]any{},
			source:  &mockSource{mktErr: fmt.Errorf("source unavailable")},
			wantErr: "list markets: source unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(tt.source)
			result, err := svc.toolListMarkets(context.Background(), tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			parsed := parseResult(t, result)
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}

func TestHandleTool(t *testing.T) {
	svc := newTestService(&mockSource{
		tickers: []marketdata.Ticker{{Market: "ETH-USDC"}},
	})

	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		wantErr  string
	}{
		{"known tool", "market/get_prices", map[string]any{"markets": []any{"ETH-USDC"}}, ""},
		{"unknown tool", "market/unknown", map[string]any{}, "unknown tool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.handleTool(context.Background(), tt.toolName, tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestFormatCandlesCSV(t *testing.T) {
	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	candles := []marketdata.Candle{
		{Timestamp: ts, Open: "3100.00", High: "3120.00", Low: "3095.00", Close: "3115.00", Volume: "50000000"},
	}

	csv := FormatCandlesCSV(candles)
	lines := strings.Split(strings.TrimSpace(csv), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 row), got %d", len(lines))
	}

	if lines[0] != "timestamp,open,high,low,close,volume" {
		t.Errorf("unexpected header: %s", lines[0])
	}

	parts := strings.Split(lines[1], ",")
	if len(parts) != 6 {
		t.Fatalf("expected 6 columns, got %d", len(parts))
	}
	if parts[1] != "3100.00" {
		t.Errorf("expected open 3100.00, got %s", parts[1])
	}
}

func TestCacheGetPut(t *testing.T) {
	c := NewCache()
	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	candles := []marketdata.Candle{
		{Timestamp: ts.Add(-2 * time.Hour), Open: "3100.00", High: "3120.00", Low: "3095.00", Close: "3115.00", Volume: "50000000"},
		{Timestamp: ts.Add(-1 * time.Hour), Open: "3115.00", High: "3140.00", Low: "3110.00", Close: "3135.00", Volume: "45000000"},
	}

	tests := []struct {
		name     string
		key      string
		limit    int
		endTime  time.Time
		putFirst bool
		wantHit  bool
		wantLen  int
	}{
		{"cache miss", "ETH-USDC:1h", 100, time.Time{}, false, false, 0},
		{"cache hit", "ETH-USDC:1h", 100, time.Time{}, true, true, 2},
		{"cache hit with limit", "ETH-USDC:1h", 1, time.Time{}, true, true, 1},
		// endTime=ts-30min: candle1 closes at ts-1h (before endTime) → included; candle2 closes at ts (after endTime) → excluded
		{"cache hit with endTime", "ETH-USDC:1h", 100, ts.Add(-30 * time.Minute), true, true, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache()
			if tt.putFirst {
				cache.PutCandles(tt.key, candles, 100)
			}

			result, hit := cache.GetCandles(tt.key, tt.limit, tt.endTime, time.Hour)
			if hit != tt.wantHit {
				t.Errorf("expected hit=%v, got %v", tt.wantHit, hit)
			}
			if tt.wantHit && len(result) != tt.wantLen {
				t.Errorf("expected %d candles, got %d", tt.wantLen, len(result))
			}
		})
	}

	_ = c
}

// TestCacheExhaustedSource verifies that a cache populated with limit=N but fewer
// candles (source exhausted) is still considered a hit for requests with limit>N,
// preventing perpetual cache misses (e.g. Binance returning 999 for limit=1000).
func TestCacheExhaustedSource(t *testing.T) {
	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	key := "BTC-USDC:1h"

	// Simulate Binance returning 999 candles when 1000 were requested.
	candles := make([]marketdata.Candle, 999)
	for i := range candles {
		candles[i] = marketdata.Candle{
			Timestamp: ts.Add(time.Duration(i-999) * time.Hour),
			Open:      "50000.00",
			High:      "50100.00",
			Low:       "49900.00",
			Close:     "50050.00",
			Volume:    "1000",
		}
	}

	cache := NewCache()
	cache.PutCandles(key, candles, 1000) // requested 1000, got 999

	// Request with limit=1000: should be a hit (source was exhausted, not under-fetched).
	result, hit := cache.GetCandles(key, 1000, time.Time{}, time.Hour)
	if !hit {
		t.Fatal("expected cache hit when source was exhausted at prior limit >= current limit")
	}
	if len(result) != 999 {
		t.Errorf("expected 999 candles, got %d", len(result))
	}

	// GetRequestedLimit must reflect what was stored.
	if got := cache.GetRequestedLimit(key); got != 1000 {
		t.Errorf("expected requestedLimit=1000, got %d", got)
	}

	// Request with limit=500: should also hit (we have enough candles).
	result2, hit2 := cache.GetCandles(key, 500, time.Time{}, time.Hour)
	if !hit2 {
		t.Fatal("expected cache hit for smaller limit")
	}
	if len(result2) != 500 {
		t.Errorf("expected 500 candles, got %d", len(result2))
	}
}

// TestCacheEmptySourceRetry verifies that when a source returns 0 candles,
// the result is NOT cached so subsequent requests re-fetch from the source.
// An empty response may be transient (e.g. no candles have closed yet), so
// we must keep retrying rather than locking in an empty result forever.
func TestCacheEmptySourceRetry(t *testing.T) {
	src := &mockSource{candles: nil} // source always returns empty
	svc := newTestService(src)
	args := map[string]any{"market": "NEW-USDC", "interval": "1h", "limit": float64(100)}

	// First call: cache miss, source is hit once.
	if _, err := svc.toolGetCandles(context.Background(), args); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if src.candleCallCount != 1 {
		t.Fatalf("expected 1 source call after first request, got %d", src.candleCallCount)
	}

	// Second call: empty was not cached, so source must be called again.
	if _, err := svc.toolGetCandles(context.Background(), args); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if src.candleCallCount != 2 {
		t.Errorf("expected 2 source calls (empty not cached), got %d", src.candleCallCount)
	}
}

// TestCacheEmptyResponseDoesNotAdvanceRequestedLimits verifies that a transient empty
// response from the source does not advance requestedLimits. If it did, the
// exhausted-source heuristic would falsely suppress future fetches even though the
// cache only holds underfilled data from a prior call.
//
// Scenario:
//  1. limit=50 fetch returns 30 candles: cache=[30], requestedLimits=50
//  2. limit=100 fetch transiently returns 0 candles: cache must stay=[30], requestedLimits must stay=50
//  3. limit=100 fetch: source now has data; priorLimit=50 < 100 so re-fetch fires, returns 100 candles
func TestCacheEmptyResponseDoesNotAdvanceRequestedLimits(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	key := "ETH-USDC:1h"

	make30 := func() []marketdata.Candle {
		c := make([]marketdata.Candle, 30)
		for i := range c {
			c[i] = marketdata.Candle{
				Timestamp: now.Add(time.Duration(i-30) * time.Hour),
				Open:      "3000.00", High: "3010.00", Low: "2990.00", Close: "3005.00", Volume: "10000",
			}
		}
		return c
	}

	cache := NewCache()

	// Step 1: 30 candles for limit=50 stored; requestedLimits[key] must be 50.
	cache.PutCandles(key, make30(), 50)
	if got := cache.GetRequestedLimit(key); got != 50 {
		t.Fatalf("step1: want requestedLimits=50, got %d", got)
	}

	// Step 2: transient empty response for limit=100; requestedLimits must NOT advance.
	cache.PutCandles(key, nil, 100)
	if got := cache.GetRequestedLimit(key); got != 50 {
		t.Fatalf("step2: empty PutCandles advanced requestedLimits to %d (want 50)", got)
	}

	// Step 3: cache still returns the 30 candles and priorLimit=50.
	result, hit, priorLimit := cache.GetCandlesWithRequestedLimit(key, 100, time.Time{}, time.Hour)
	if !hit {
		t.Fatal("step3: expected cache hit (30 candles still stored)")
	}
	if len(result) != 30 {
		t.Errorf("step3: want 30 cached candles, got %d", len(result))
	}
	if priorLimit != 50 {
		t.Errorf("step3: want priorLimit=50, got %d (exhausted-source heuristic would suppress re-fetch)", priorLimit)
	}
}

// TestCacheHistoricalEndTimeRefetch verifies that historical requests (non-zero endTime)
// re-fetch from source when the cached candle count is less than the requested limit,
// because the cache (keyed by market:interval only) may cover a different time window.
func TestCacheHistoricalEndTimeRefetch(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	// 300 candles covering T-300h to T.
	candles := make([]marketdata.Candle, 300)
	for i := range candles {
		candles[i] = marketdata.Candle{
			Timestamp: now.Add(time.Duration(i-300) * time.Hour),
			Open:      "50000.00",
			High:      "50100.00",
			Low:       "49900.00",
			Close:     "50050.00",
			Volume:    "1000",
		}
	}
	src := &mockSource{candles: candles}
	// Freeze clock at `now` so the newest candle close (now) is not considered stale.
	svc := newTestServiceWithClock(src, func() time.Time { return now })

	// First call: no endTime, limit=300. Primes the cache with 300 candles.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "BTC-USDC", "interval": "1h", "limit": float64(300),
	}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	callsAfterFirst := src.candleCallCount

	// Second call: same params, no endTime. Should be a cache hit (source not called).
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "BTC-USDC", "interval": "1h", "limit": float64(300),
	}); err != nil {
		t.Fatalf("second call (no endTime): %v", err)
	}
	if src.candleCallCount != callsAfterFirst {
		t.Errorf("second call (no endTime): source should not be called again (calls: %d)", src.candleCallCount)
	}

	// Third call: historical endTime that limits the cache result to fewer candles.
	// endTime = T-100h => only 200 candles in cache are before this time.
	// Since 200 < 300 (limit) and endTime is non-zero, source must be re-fetched.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market":   "BTC-USDC",
		"interval": "1h",
		"limit":    float64(300),
		"end_time": now.Add(-100 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("third call (historical endTime): %v", err)
	}
	if src.candleCallCount == callsAfterFirst {
		t.Error("third call (historical endTime): source should have been called again for historical request with fewer cached candles than limit")
	}
}

// TestCacheHistoricalRequestedLimitsNoPollution verifies that a historical fetch (non-zero
// endTime) does not inflate requestedLimits, which would otherwise suppress a subsequent
// live fetch for a larger limit (the source-exhausted heuristic would fire incorrectly).
func TestCacheHistoricalRequestedLimitsNoPollution(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	// Source always returns 100 candles.
	candles100 := make([]marketdata.Candle, 100)
	for i := range candles100 {
		candles100[i] = marketdata.Candle{
			Timestamp: now.Add(time.Duration(i-100) * time.Hour),
			Open: "50000.00", High: "50100.00", Low: "49900.00", Close: "50050.00", Volume: "1000",
		}
	}
	src := &mockSource{candles: candles100}
	svc := newTestService(src)

	// First call: live, limit=100. Primes cache with requestedLimits[key]=100.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "BTC-USDC", "interval": "1h", "limit": float64(100),
	}); err != nil {
		t.Fatalf("live call: %v", err)
	}
	callsAfterLive := src.candleCallCount

	// Historical call with limit=300: should re-fetch from source but must NOT
	// set requestedLimits[key]=300 (would suppress the next live fetch for limit=300).
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market":   "BTC-USDC",
		"interval": "1h",
		"limit":    float64(300),
		"end_time": now.Add(-50 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("historical call: %v", err)
	}
	if src.candleCallCount == callsAfterLive {
		t.Fatal("historical call should have gone to source")
	}
	callsAfterHistorical := src.candleCallCount

	// Live call with limit=300: cache has only 100 candles and requestedLimits[key]=100
	// (not inflated by the historical call). Since 100 < 300, source must be called.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "BTC-USDC", "interval": "1h", "limit": float64(300),
	}); err != nil {
		t.Fatalf("live call after historical: %v", err)
	}
	if src.candleCallCount == callsAfterHistorical {
		t.Error("live call with limit=300 should have re-fetched: requestedLimits was polluted by the historical call (bug not fixed)")
	}
}

// TestCacheHistoricalFirstDoesNotPolluteLiveCache verifies that a historical request
// (non-zero endTime) that arrives first does not populate the live cache, so a
// subsequent live request with the same limit always goes to source.
// Regression test for: historical fetch stores N candles with putLimit=0; live request
// with limit=N sees len(candles)==limit, skips re-fetch, returns stale historical data.
func TestCacheHistoricalFirstDoesNotPolluteLiveCache(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	// Source returns exactly 100 candles.
	candles := make([]marketdata.Candle, 100)
	for i := range candles {
		candles[i] = marketdata.Candle{
			Timestamp: now.Add(time.Duration(i-100) * time.Hour),
			Open:      "50000.00",
			High:      "50100.00",
			Low:       "49900.00",
			Close:     "50050.00",
			Volume:    "1000",
		}
	}
	src := &mockSource{candles: candles}
	svc := newTestService(src)

	// First call: historical with limit=100 (exactly matches source count).
	// Cache is empty, so source is called and returns 100 candles.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market":   "BTC-USDC",
		"interval": "1h",
		"limit":    float64(100),
		"end_time": now.Add(-50 * time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("historical call: %v", err)
	}
	if src.candleCallCount != 1 {
		t.Fatalf("expected 1 source call after historical request, got %d", src.candleCallCount)
	}

	// Second call: live (no endTime), same limit=100.
	// Without the fix, the cache holds 100 historical candles and len==limit, so
	// the re-fetch condition (len < limit) is false and historical data is returned.
	// With the fix, historical requests never write to the cache, so this is a
	// cache miss and the source must be called.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "BTC-USDC", "interval": "1h", "limit": float64(100),
	}); err != nil {
		t.Fatalf("live call after historical: %v", err)
	}
	if src.candleCallCount != 2 {
		t.Errorf("live call after historical: source should have been called (stale cache bug); calls=%d", src.candleCallCount)
	}
}

func TestCachePutMerge(t *testing.T) {
	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	key := "ETH-USDC:1h"

	first := []marketdata.Candle{
		{Timestamp: ts.Add(-3 * time.Hour), Open: "3000.00", High: "3010.00", Low: "2990.00", Close: "3005.00", Volume: "10000"},
		{Timestamp: ts.Add(-2 * time.Hour), Open: "3005.00", High: "3020.00", Low: "3000.00", Close: "3015.00", Volume: "12000"},
	}
	second := []marketdata.Candle{
		// Overlapping: same timestamp as first[1] - must not be duplicated.
		{Timestamp: ts.Add(-2 * time.Hour), Open: "3005.00", High: "3020.00", Low: "3000.00", Close: "3015.00", Volume: "12000"},
		// New: newer than first batch.
		{Timestamp: ts.Add(-1 * time.Hour), Open: "3015.00", High: "3030.00", Low: "3010.00", Close: "3025.00", Volume: "11000"},
	}

	cache := NewCache()
	cache.PutCandles(key, first, 100)
	cache.PutCandles(key, second, 100)

	result, hit := cache.GetCandles(key, 100, time.Time{}, time.Hour)
	if !hit {
		t.Fatal("expected cache hit after two puts")
	}
	// Merge must produce 3 unique candles (no duplicate for ts-2h).
	if len(result) != 3 {
		t.Errorf("expected 3 candles after merge, got %d (duplicate prevention may be broken)", len(result))
	}
	// Last candle must be the newest.
	if !result[len(result)-1].Timestamp.Equal(ts.Add(-1 * time.Hour)) {
		t.Errorf("last candle has wrong timestamp: %v", result[len(result)-1].Timestamp)
	}
}

// TestCachePutMergeBackfill verifies that PutCandles backfills older candles when
// a larger-limit fetch returns history that predates the existing cache.
// Regression: previously, only candles NEWER than lastExisting were appended,
// so a limit=300 fetch after a limit=100 cache would leave cache at 100 candles
// while requestedLimits[key] rose to 300, causing the next limit=300 call to
// falsely treat the underfilled cache as "source exhausted".
func TestCachePutMergeBackfill(t *testing.T) {
	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	key := "ETH-USDC:1h"

	tests := []struct {
		name       string
		first      []marketdata.Candle
		second     []marketdata.Candle
		wantLen    int
		wantFirst  time.Time
		wantLast   time.Time
	}{
		{
			name: "backfill older history",
			first: []marketdata.Candle{
				{Timestamp: ts.Add(-2 * time.Hour), Open: "3005.00", High: "3020.00", Low: "3000.00", Close: "3015.00", Volume: "12000"},
				{Timestamp: ts.Add(-1 * time.Hour), Open: "3015.00", High: "3030.00", Low: "3010.00", Close: "3025.00", Volume: "11000"},
			},
			second: []marketdata.Candle{
				// Older history returned by a larger limit fetch
				{Timestamp: ts.Add(-4 * time.Hour), Open: "2990.00", High: "3000.00", Low: "2980.00", Close: "2995.00", Volume: "9000"},
				{Timestamp: ts.Add(-3 * time.Hour), Open: "2995.00", High: "3010.00", Low: "2990.00", Close: "3005.00", Volume: "10000"},
				// Overlapping candles: already in cache, must not duplicate
				{Timestamp: ts.Add(-2 * time.Hour), Open: "3005.00", High: "3020.00", Low: "3000.00", Close: "3015.00", Volume: "12000"},
				{Timestamp: ts.Add(-1 * time.Hour), Open: "3015.00", High: "3030.00", Low: "3010.00", Close: "3025.00", Volume: "11000"},
			},
			wantLen:   4,
			wantFirst: ts.Add(-4 * time.Hour),
			wantLast:  ts.Add(-1 * time.Hour),
		},
		{
			name: "backfill older and append newer simultaneously",
			first: []marketdata.Candle{
				{Timestamp: ts.Add(-2 * time.Hour), Open: "3005.00", High: "3020.00", Low: "3000.00", Close: "3015.00", Volume: "12000"},
				{Timestamp: ts.Add(-1 * time.Hour), Open: "3015.00", High: "3030.00", Low: "3010.00", Close: "3025.00", Volume: "11000"},
			},
			second: []marketdata.Candle{
				{Timestamp: ts.Add(-3 * time.Hour), Open: "2995.00", High: "3005.00", Low: "2990.00", Close: "3000.00", Volume: "8000"},
				{Timestamp: ts.Add(-2 * time.Hour), Open: "3005.00", High: "3020.00", Low: "3000.00", Close: "3015.00", Volume: "12000"},
				{Timestamp: ts.Add(-1 * time.Hour), Open: "3015.00", High: "3030.00", Low: "3010.00", Close: "3025.00", Volume: "11000"},
				{Timestamp: ts.Add(0 * time.Hour), Open: "3025.00", High: "3040.00", Low: "3020.00", Close: "3035.00", Volume: "13000"},
			},
			wantLen:   4,
			wantFirst: ts.Add(-3 * time.Hour),
			wantLast:  ts.Add(0 * time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache()
			cache.PutCandles(key, tt.first, 100)
			cache.PutCandles(key, tt.second, 300)

			result, hit := cache.GetCandles(key, 1000, time.Time{}, time.Hour)
			if !hit {
				t.Fatal("expected cache hit")
			}
			if len(result) != tt.wantLen {
				t.Errorf("want %d candles, got %d", tt.wantLen, len(result))
			}
			if len(result) > 0 {
				if !result[0].Timestamp.Equal(tt.wantFirst) {
					t.Errorf("first candle: want %v, got %v", tt.wantFirst, result[0].Timestamp)
				}
				if !result[len(result)-1].Timestamp.Equal(tt.wantLast) {
					t.Errorf("last candle: want %v, got %v", tt.wantLast, result[len(result)-1].Timestamp)
				}
			}
		})
	}
}

// TestCacheLargerLimitDoesNotReturnUnderfilled verifies the full service-level flow:
// after a limit=100 live fetch followed by a limit=300 live fetch, a subsequent
// limit=300 request must return 300 candles, not the 100 from the initial cache.
// This is the end-to-end regression for the "permanently underfilled cache" bug
// where PutCandles failed to backfill older history on a larger-limit refetch.
func TestCacheLargerLimitDoesNotReturnUnderfilled(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	// limit-aware mock: returns exactly `limit` candles ending at now-1h.
	src := &limitAwareMockSource{now: now}
	// Freeze clock at `now` so the newest candle close (now) is not considered stale.
	svc := newTestServiceWithClock(src, func() time.Time { return now })

	// First call: limit=100. Primes cache with 100 candles.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "ETH-USDC", "interval": "1h", "limit": float64(100),
	}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if src.callCount != 1 {
		t.Fatalf("expected 1 source call, got %d", src.callCount)
	}

	// Second call: limit=300. Cache has 100 candles, priorLimit=100<300 -> re-fetch.
	// PutCandles must backfill the 200 older candles into the cache.
	if _, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "ETH-USDC", "interval": "1h", "limit": float64(300),
	}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if src.callCount != 2 {
		t.Fatalf("expected 2 source calls after limit=300, got %d", src.callCount)
	}

	// Third call: limit=300 again. Cache should now hold 300 candles (after backfill).
	// priorLimit=300 >= fetchLimit=300, so this must be a cache hit returning 300 candles.
	res, err := svc.toolGetCandles(context.Background(), map[string]any{
		"market": "ETH-USDC", "interval": "1h", "limit": float64(300),
	})
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if src.callCount != 2 {
		t.Errorf("third call must be a cache hit (source was called again: %d total)", src.callCount)
	}
	// Verify the returned count is 300, not 100.
	var data map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &data); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	count, _ := data["count"].(float64)
	if int(count) != 300 {
		t.Errorf("third call: want 300 candles, got %d (cache was underfilled)", int(count))
	}
}

// limitAwareMockSource returns exactly `limit` candles on each GetCandles call.
type limitAwareMockSource struct {
	now       time.Time
	callCount int
}

func (m *limitAwareMockSource) Name() string { return "limit-aware-mock" }

func (m *limitAwareMockSource) GetCandles(_ context.Context, _ string, _ marketdata.Interval, limit int, _ time.Time) ([]marketdata.Candle, error) {
	m.callCount++
	candles := make([]marketdata.Candle, limit)
	for i := range candles {
		candles[i] = marketdata.Candle{
			Timestamp: m.now.Add(time.Duration(i-limit) * time.Hour),
			Open:      "3000.00",
			High:      "3010.00",
			Low:       "2990.00",
			Close:     "3005.00",
			Volume:    "10000",
		}
	}
	return candles, nil
}

func (m *limitAwareMockSource) GetTicker(_ context.Context, _ []string) ([]marketdata.Ticker, error) {
	return nil, nil
}
func (m *limitAwareMockSource) GetMarkets(_ context.Context, _ string) ([]marketdata.MarketInfo, error) {
	return nil, nil
}
func (m *limitAwareMockSource) GetOrderbook(_ context.Context, _ string, _ int) (*marketdata.Orderbook, error) {
	return nil, nil
}
func (m *limitAwareMockSource) GetFundingRates(_ context.Context, _ string, _ int) (*marketdata.FundingData, error) {
	return nil, nil
}
func (m *limitAwareMockSource) GetOpenInterest(_ context.Context, _ string) (*marketdata.OpenInterest, error) {
	return nil, nil
}

func TestToolGetOrderbook(t *testing.T) {
	now := time.Now().UTC()
	sampleOrderbook := &marketdata.Orderbook{
		Market: "ETH-USDC",
		Bids: []marketdata.OrderbookLevel{
			{Price: "3204.50", Size: "12.5"},
			{Price: "3204.00", Size: "8.2"},
		},
		Asks: []marketdata.OrderbookLevel{
			{Price: "3206.20", Size: "10.1"},
			{Price: "3207.00", Size: "15.3"},
		},
		Timestamp: now,
	}

	tests := []struct {
		name    string
		args    map[string]any
		source  *mockSource
		wantErr string
		check   func(t *testing.T, result map[string]any)
	}{
		{
			name:    "missing market",
			args:    map[string]any{},
			source:  &mockSource{},
			wantErr: "market is required",
		},
		{
			name:   "valid request",
			args:   map[string]any{"market": "ETH-USDC"},
			source: &mockSource{orderbook: sampleOrderbook},
			check: func(t *testing.T, result map[string]any) {
				if result["market"] != "ETH-USDC" {
					t.Errorf("expected market ETH-USDC, got %v", result["market"])
				}
				bids, ok := result["bids"].([]any)
				if !ok || len(bids) != 2 {
					t.Fatalf("expected 2 bids, got %v", result["bids"])
				}
				asks, ok := result["asks"].([]any)
				if !ok || len(asks) != 2 {
					t.Fatalf("expected 2 asks, got %v", result["asks"])
				}
				// bid_total = 12.5 + 8.2 = 20.7
				if result["bid_total"] != "20.7000" {
					t.Errorf("expected bid_total 20.7000, got %v", result["bid_total"])
				}
				// ask_total = 10.1 + 15.3 = 25.4
				if result["ask_total"] != "25.4000" {
					t.Errorf("expected ask_total 25.4000, got %v", result["ask_total"])
				}
				// spread = 3206.20 - 3204.50 = 1.70
				if result["spread"] != "1.7000" {
					t.Errorf("expected spread 1.7000, got %v", result["spread"])
				}
				// imbalance = 20.7 / (20.7 + 25.4) = 0.449...
				imbalance, _ := strconv.ParseFloat(result["imbalance"].(string), 64)
				if imbalance < 0.44 || imbalance > 0.46 {
					t.Errorf("expected imbalance ~0.449, got %v", result["imbalance"])
				}
				if result["source"] != "mock" {
					t.Errorf("expected source mock, got %v", result["source"])
				}
			},
		},
		{
			name:    "source error",
			args:    map[string]any{"market": "ETH-USDC"},
			source:  &mockSource{obErr: fmt.Errorf("connection refused")},
			wantErr: "get orderbook: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(tt.source)
			result, err := svc.toolGetOrderbook(context.Background(), tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			parsed := parseResult(t, result)
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}

func TestToolGetFunding(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		args    map[string]any
		source  *mockSource
		wantErr string
		check   func(t *testing.T, result map[string]any)
	}{
		{
			name:    "missing market",
			args:    map[string]any{},
			source:  &mockSource{},
			wantErr: "market is required",
		},
		{
			name: "neutral signal",
			args: map[string]any{"market": "ETH-USDC"},
			source: &mockSource{fundingData: &marketdata.FundingData{
				Market:          "ETH-USDC",
				CurrentRate:     "0.00005",
				AnnualizedPct:   "5.48",
				NextFundingTime: now.Add(4 * time.Hour),
				History:         []marketdata.FundingRate{{Timestamp: now.Add(-8 * time.Hour), Rate: "0.00004"}},
			}},
			check: func(t *testing.T, result map[string]any) {
				if result["signal"] != "neutral" {
					t.Errorf("expected signal neutral, got %v", result["signal"])
				}
				if result["market"] != "ETH-USDC" {
					t.Errorf("expected market ETH-USDC, got %v", result["market"])
				}
				if result["source"] != "mock" {
					t.Errorf("expected source mock, got %v", result["source"])
				}
			},
		},
		{
			name: "bullish_crowd signal",
			args: map[string]any{"market": "ETH-USDC"},
			source: &mockSource{fundingData: &marketdata.FundingData{
				Market:          "ETH-USDC",
				CurrentRate:     "0.000500",
				AnnualizedPct:   "54.75",
				NextFundingTime: now.Add(4 * time.Hour),
				History:         nil,
			}},
			check: func(t *testing.T, result map[string]any) {
				if result["signal"] != "bullish_crowd" {
					t.Errorf("expected signal bullish_crowd, got %v", result["signal"])
				}
			},
		},
		{
			name: "extreme_bullish signal",
			args: map[string]any{"market": "ETH-USDC"},
			source: &mockSource{fundingData: &marketdata.FundingData{
				Market:          "ETH-USDC",
				CurrentRate:     "0.002000",
				AnnualizedPct:   "219.00",
				NextFundingTime: now.Add(4 * time.Hour),
				History:         nil,
			}},
			check: func(t *testing.T, result map[string]any) {
				if result["signal"] != "extreme_bullish" {
					t.Errorf("expected signal extreme_bullish, got %v", result["signal"])
				}
			},
		},
		{
			name: "bearish_crowd signal",
			args: map[string]any{"market": "ETH-USDC"},
			source: &mockSource{fundingData: &marketdata.FundingData{
				Market:          "ETH-USDC",
				CurrentRate:     "-0.000300",
				AnnualizedPct:   "-32.85",
				NextFundingTime: now.Add(4 * time.Hour),
				History:         nil,
			}},
			check: func(t *testing.T, result map[string]any) {
				if result["signal"] != "bearish_crowd" {
					t.Errorf("expected signal bearish_crowd, got %v", result["signal"])
				}
			},
		},
		{
			name: "extreme_bearish signal",
			args: map[string]any{"market": "ETH-USDC"},
			source: &mockSource{fundingData: &marketdata.FundingData{
				Market:          "ETH-USDC",
				CurrentRate:     "-0.005000",
				AnnualizedPct:   "-547.50",
				NextFundingTime: now.Add(4 * time.Hour),
				History:         nil,
			}},
			check: func(t *testing.T, result map[string]any) {
				if result["signal"] != "extreme_bearish" {
					t.Errorf("expected signal extreme_bearish, got %v", result["signal"])
				}
			},
		},
		{
			name:   "source error returns graceful result",
			args:   map[string]any{"market": "ETH-USDC"},
			source: &mockSource{fundErr: fmt.Errorf("perp not supported")},
			check: func(t *testing.T, result map[string]any) {
				if result["error"] == nil {
					t.Errorf("expected error field in graceful result, got %v", result)
				}
				if result["market"] != "ETH-USDC" {
					t.Errorf("expected market=ETH-USDC in graceful result")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(tt.source)
			result, err := svc.toolGetFunding(context.Background(), tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			parsed := parseResult(t, result)
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}

func TestClassifyFundingSignal(t *testing.T) {
	tests := []struct {
		rate string
		want string
	}{
		{"0.00005", "neutral"},
		{"-0.00005", "neutral"},
		{"0", "neutral"},
		{"invalid", "neutral"},
		{"0.000100", "neutral"},  // exactly at boundary - not strictly greater
		{"0.000101", "bullish_crowd"},
		{"-0.000101", "bearish_crowd"},
		{"0.001000", "bullish_crowd"}, // exactly 0.001 - not strictly greater
		{"0.001001", "extreme_bullish"},
		{"-0.001001", "extreme_bearish"},
		{"0.005000", "extreme_bullish"},
		{"-0.005000", "extreme_bearish"},
	}

	for _, tt := range tests {
		t.Run(tt.rate, func(t *testing.T) {
			got := classifyFundingSignal(tt.rate)
			if got != tt.want {
				t.Errorf("classifyFundingSignal(%q) = %q, want %q", tt.rate, got, tt.want)
			}
		})
	}
}

func TestToolGetOpenInterest(t *testing.T) {
	sampleOI := &marketdata.OpenInterest{
		Market:         "ETH-USDC",
		OpenInterest:   "1250000000",
		OIChange1hPct:  "0.50",
		OIChange4hPct:  "2.10",
		OIChange24hPct: "3.20",
		LongShortRatio: "1.15",
	}

	tests := []struct {
		name    string
		args    map[string]any
		source  *mockSource
		wantErr string
		check   func(t *testing.T, result map[string]any)
	}{
		{
			name:    "missing market",
			args:    map[string]any{},
			source:  &mockSource{},
			wantErr: "market is required",
		},
		{
			name:   "valid request",
			args:   map[string]any{"market": "ETH-USDC"},
			source: &mockSource{openInterest: sampleOI},
			check: func(t *testing.T, result map[string]any) {
				if result["market"] != "ETH-USDC" {
					t.Errorf("expected market ETH-USDC, got %v", result["market"])
				}
				if result["open_interest"] != "1250000000" {
					t.Errorf("expected open_interest 1250000000, got %v", result["open_interest"])
				}
				if result["oi_change_1h_pct"] != "0.50" {
					t.Errorf("expected oi_change_1h_pct 0.50, got %v", result["oi_change_1h_pct"])
				}
				if result["long_short_ratio"] != "1.15" {
					t.Errorf("expected long_short_ratio 1.15, got %v", result["long_short_ratio"])
				}
				if result["source"] != "mock" {
					t.Errorf("expected source mock, got %v", result["source"])
				}
			},
		},
		{
			name:   "source error returns graceful result",
			args:   map[string]any{"market": "ETH-USDC"},
			source: &mockSource{oiErr: fmt.Errorf("data unavailable")},
			check: func(t *testing.T, result map[string]any) {
				if result["error"] == nil {
					t.Errorf("expected error field in graceful result, got %v", result)
				}
				if result["market"] != "ETH-USDC" {
					t.Errorf("expected market=ETH-USDC in graceful result")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(tt.source)
			result, err := svc.toolGetOpenInterest(context.Background(), tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			parsed := parseResult(t, result)
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}

func TestToolSetAlert(t *testing.T) {
	now := time.Now().UTC()
	src := &mockSource{
		tickers: []marketdata.Ticker{
			{Market: "ETH-USDC", Last: "3200.00", Timestamp: now},
		},
	}

	tests := []struct {
		name    string
		ctx     context.Context
		args    map[string]any
		wantErr string
		check   func(t *testing.T, result map[string]any)
	}{
		{
			name:    "missing agent id",
			ctx:     context.Background(),
			args:    map[string]any{"market": "ETH-USDC", "condition": "above", "value": float64(3500)},
			wantErr: "agent_id is required",
		},
		{
			name:    "missing market",
			ctx:     agentCtx("agent-1"),
			args:    map[string]any{"condition": "above", "value": float64(3500)},
			wantErr: "market and condition are required",
		},
		{
			name:    "missing condition",
			ctx:     agentCtx("agent-1"),
			args:    map[string]any{"market": "ETH-USDC", "value": float64(3500)},
			wantErr: "market and condition are required",
		},
		{
			name:    "missing value",
			ctx:     agentCtx("agent-1"),
			args:    map[string]any{"market": "ETH-USDC", "condition": "above"},
			wantErr: "value is required",
		},
		{
			name:    "invalid condition",
			ctx:     agentCtx("agent-1"),
			args:    map[string]any{"market": "ETH-USDC", "condition": "crossed", "value": float64(3500)},
			wantErr: "invalid condition",
		},
		{
			name: "above condition",
			ctx:  agentCtx("agent-1"),
			args: map[string]any{"market": "ETH-USDC", "condition": "above", "value": float64(3500)},
			check: func(t *testing.T, result map[string]any) {
				alertID, ok := result["alert_id"].(string)
				if !ok || !strings.HasPrefix(alertID, "alert-") {
					t.Errorf("expected alert_id with prefix alert-, got %v", result["alert_id"])
				}
				if result["status"] != "active" {
					t.Errorf("expected status active, got %v", result["status"])
				}
			},
		},
		{
			name: "below condition",
			ctx:  agentCtx("agent-1"),
			args: map[string]any{"market": "ETH-USDC", "condition": "below", "value": float64(3000), "note": "buy the dip"},
			check: func(t *testing.T, result map[string]any) {
				if result["status"] != "active" {
					t.Errorf("expected status active, got %v", result["status"])
				}
			},
		},
		{
			name: "change_pct condition fetches ref price",
			ctx:  agentCtx("agent-1"),
			args: map[string]any{"market": "ETH-USDC", "condition": "change_pct", "value": float64(5), "window": "1h"},
			check: func(t *testing.T, result map[string]any) {
				if result["status"] != "active" {
					t.Errorf("expected status active, got %v", result["status"])
				}
			},
		},
		{
			name: "idempotent: same params = same alert_id",
			ctx:  agentCtx("agent-1"),
			args: map[string]any{"market": "BTC-USDC", "condition": "above", "value": float64(70000)},
			check: func(t *testing.T, result map[string]any) {
				id1 := result["alert_id"].(string)
				// Call again with same params on a new service instance.
				// IDs are deterministic (SHA-256 of params), so both must match.
				svc2 := newTestService(src)
				r2, err := svc2.toolSetAlert(agentCtx("agent-1"), map[string]any{"market": "BTC-USDC", "condition": "above", "value": float64(70000)})
				if err != nil {
					t.Fatalf("second set_alert failed: %v", err)
				}
				parsed2 := parseResult(t, r2)
				if parsed2["alert_id"] != id1 {
					t.Errorf("expected same deterministic alert_id for same params: got %v vs %v", id1, parsed2["alert_id"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(src)
			result, err := svc.toolSetAlert(tt.ctx, tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			parsed := parseResult(t, result)
			if tt.check != nil {
				tt.check(t, parsed)
			}
		})
	}
}

func TestToolCancelAlert(t *testing.T) {
	src := &mockSource{}

	tests := []struct {
		name    string
		ctx     context.Context
		args    map[string]any
		wantErr string
	}{
		{
			name:    "missing agent id",
			ctx:     context.Background(),
			args:    map[string]any{"alert_id": "alert-abc123"},
			wantErr: "agent_id is required",
		},
		{
			name:    "missing alert_id",
			ctx:     agentCtx("agent-1"),
			args:    map[string]any{},
			wantErr: "alert_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(src)
			_, err := svc.toolCancelAlert(tt.ctx, tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConditionMet(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		value     float64
		refPrice  float64
		price     float64
		want      bool
	}{
		{"above triggers", "above", 3000, 0, 3200, true},
		{"above does not trigger below threshold", "above", 4000, 0, 3200, false},
		{"below triggers", "below", 4000, 0, 3200, true},
		{"below does not trigger above threshold", "below", 3000, 0, 3200, false},
		{"change_pct triggers", "change_pct", 5, 3000, 3200, true},
		{"change_pct does not trigger", "change_pct", 10, 3000, 3100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &alertCondition{Condition: tt.condition, Value: tt.value, refPrice: tt.refPrice}
			got := conditionMet(a, tt.price)
			if got != tt.want {
				t.Errorf("conditionMet(%s, %.0f) = %v, want %v", tt.condition, tt.price, got, tt.want)
			}
		})
	}
}

// parseResult extracts the JSON content from an MCP ToolResult.
func parseResult(t *testing.T, result *mcp.ToolResult) map[string]any {
	t.Helper()
	if result == nil {
		t.Fatal("nil result")
	}
	if len(result.Content) == 0 {
		t.Fatal("empty content")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	return parsed
}
