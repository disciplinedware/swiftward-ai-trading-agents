package kraken

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"ai-trading-agents/internal/marketdata"
)

func newTestSource(t *testing.T, handler http.Handler) *Source {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	reg := marketdata.NewSymbolRegistry()
	return NewSource(reg, Config{BaseURL: srv.URL}, zaptest.NewLogger(t))
}

func krakenResp(t *testing.T, result any) []byte {
	t.Helper()
	r, err := json.Marshal(result)
	require.NoError(t, err)
	resp := map[string]any{"error": []string{}, "result": json.RawMessage(r)}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	return b
}

func TestGetTicker(t *testing.T) {
	tests := []struct {
		name    string
		symbols []string
		resp    map[string]any
		wantN   int
		wantErr bool
	}{
		{
			name:    "single symbol",
			symbols: []string{"BTC-USD"},
			resp: map[string]any{
				"XXBTZUSD": map[string]any{
					"a": []string{"97000.00", "1", "1.000"},
					"b": []string{"96999.00", "1", "1.000"},
					"c": []string{"97000.00", "0.001"},
					"v": []string{"100.5", "1234.56"},
					"h": []string{"97500.00", "97500.00"},
					"l": []string{"95000.00", "94500.00"},
					"o": "96000.00",
				},
			},
			wantN: 1,
		},
		{
			name:    "multi symbol",
			symbols: []string{"BTC-USD", "ETH-USD"},
			resp: map[string]any{
				"XXBTZUSD": map[string]any{
					"a": []string{"97000.00"}, "b": []string{"96999.00"},
					"c": []string{"97000.00"}, "v": []string{"100", "1234"},
					"h": []string{"97500.00", "97500.00"}, "l": []string{"95000.00", "94500.00"},
					"o": "96000.00",
				},
				"XETHZUSD": map[string]any{
					"a": []string{"3500.00"}, "b": []string{"3499.00"},
					"c": []string{"3500.00"}, "v": []string{"500", "5000"},
					"h": []string{"3600.00", "3600.00"}, "l": []string{"3400.00", "3400.00"},
					"o": "3450.00",
				},
			},
			wantN: 2,
		},
		{
			name:    "empty symbols returns error",
			symbols: []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(krakenResp(t, tt.resp))
			}))

			tickers, err := src.GetTicker(context.Background(), tt.symbols)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, tickers, tt.wantN)

			if tt.wantN > 0 {
				// Verify first ticker has expected fields.
				assert.Equal(t, tt.symbols[0], tickers[0].Market)
				assert.NotEmpty(t, tickers[0].Last)
				assert.NotEmpty(t, tickers[0].Bid)
				assert.NotEmpty(t, tickers[0].Ask)
				assert.NotEmpty(t, tickers[0].Change24hPct)
			}
		})
	}
}

func TestGetCandles(t *testing.T) {
	candleData := [][]any{
		{1700000000, "96000.0", "96500.0", "95500.0", "96200.0", "96100.0", "123.456", 42},
		{1700003600, "96200.0", "96800.0", "96000.0", "96700.0", "96400.0", "234.567", 55},
		{1700007200, "96700.0", "97000.0", "96500.0", "96900.0", "96800.0", "345.678", 33},
	}

	tests := []struct {
		name     string
		symbol   string
		interval marketdata.Interval
		limit    int
		wantN    int
		wantErr  bool
	}{
		{
			name:     "valid candles",
			symbol:   "BTC-USD",
			interval: marketdata.Interval1h,
			limit:    10,
			wantN:    3,
		},
		{
			name:     "limit trims result",
			symbol:   "BTC-USD",
			interval: marketdata.Interval1h,
			limit:    2,
			wantN:    2,
		},
		{
			name:     "unsupported interval",
			symbol:   "BTC-USD",
			interval: "2h",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := map[string]any{
					"XXBTZUSD": candleData,
					"last":     1700007200,
				}
				_, _ = w.Write(krakenResp(t, resp))
			}))

			candles, err := src.GetCandles(context.Background(), tt.symbol, tt.interval, tt.limit, time.Time{})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, candles, tt.wantN)

			if tt.wantN > 0 {
				last := candles[len(candles)-1]
				assert.NotEmpty(t, last.Open)
				assert.NotEmpty(t, last.Close)
				assert.NotEmpty(t, last.Volume)
			}
		})
	}
}

func TestGetOrderbook(t *testing.T) {
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"XXBTZUSD": map[string]any{
				"asks": [][]any{
					{"97000.00", "1.234", 1700000000},
					{"97100.00", "2.345", 1700000000},
				},
				"bids": [][]any{
					{"96999.00", "3.456", 1700000000},
					{"96900.00", "4.567", 1700000000},
				},
			},
		}
		_, _ = w.Write(krakenResp(t, resp))
	}))

	ob, err := src.GetOrderbook(context.Background(), "BTC-USD", 0)
	require.NoError(t, err)
	assert.Equal(t, "BTC-USD", ob.Market)
	assert.Len(t, ob.Asks, 2)
	assert.Len(t, ob.Bids, 2)
	assert.Equal(t, "97000.00", ob.Asks[0].Price)
	assert.Equal(t, "96999.00", ob.Bids[0].Price)
}

func TestGetMarkets(t *testing.T) {
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/0/public/AssetPairs" {
			resp := map[string]any{
				"XXBTZUSD": map[string]any{
					"altname": "XBTUSD", "base": "XXBT", "quote": "ZUSD", "status": "online",
				},
				"XETHZUSD": map[string]any{
					"altname": "ETHUSD", "base": "XETH", "quote": "ZUSD", "status": "online",
				},
				"BOGUSPAIR": map[string]any{
					"altname": "BOGUS", "base": "BOG", "quote": "US", "status": "online",
				},
				"OFFLINEPAIR": map[string]any{
					"altname": "OFFL", "base": "OFF", "quote": "LINE", "status": "cancel_only",
				},
			}
			_, _ = w.Write(krakenResp(t, resp))
		} else {
			// Ticker enrichment call.
			resp := map[string]any{
				"XXBTZUSD": map[string]any{
					"a": []string{"97000.00"}, "b": []string{"96999.00"},
					"c": []string{"97000.00"}, "v": []string{"100", "1234"},
					"h": []string{"97500.00", "97500.00"}, "l": []string{"95000.00", "94500.00"},
					"o": "96000.00",
				},
				"XETHZUSD": map[string]any{
					"a": []string{"3500.00"}, "b": []string{"3499.00"},
					"c": []string{"3500.00"}, "v": []string{"500", "5000"},
					"h": []string{"3600.00", "3600.00"}, "l": []string{"3400.00", "3400.00"},
					"o": "3450.00",
				},
			}
			_, _ = w.Write(krakenResp(t, resp))
		}
	}))

	markets, err := src.GetMarkets(context.Background(), "")
	require.NoError(t, err)
	// BTC, ETH, and BOGUS (algorithmically mapped). OFFLINE filtered (not online).
	assert.Len(t, markets, 3)

	pairNames := make([]string, len(markets))
	for i, m := range markets {
		pairNames[i] = m.Pair
	}
	assert.Contains(t, pairNames, "BTC-USD")
	assert.Contains(t, pairNames, "ETH-USD")
	assert.Contains(t, pairNames, "BOG-US") // unknown pair still included via algorithmic mapping
}

func TestGetMarkets_BatchedTickerEnrichment(t *testing.T) {
	// Generate 120 pairs to force multiple ticker batches (batch size = 50).
	const totalPairs = 120
	assetPairs := make(map[string]any, totalPairs)
	for i := range totalPairs {
		name := fmt.Sprintf("TOKEN%dUSD", i)
		assetPairs[name] = map[string]any{
			"altname": name,
			"base":    fmt.Sprintf("TOKEN%d", i),
			"quote":   "ZUSD",
			"status":  "online",
		}
	}

	var tickerCalls int
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/0/public/AssetPairs" {
			_, _ = w.Write(krakenResp(t, assetPairs))
			return
		}
		// Ticker calls - track how many batches hit.
		tickerCalls++
		pairs := r.URL.Query().Get("pair")
		symbols := splitPairs(pairs)
		resp := make(map[string]any, len(symbols))
		for _, sym := range symbols {
			resp[sym] = map[string]any{
				"a": []string{"1.00"}, "b": []string{"0.99"},
				"c": []string{"1.00"}, "v": []string{"100", fmt.Sprintf("%d", 1000+tickerCalls)},
				"h": []string{"1.10", "1.10"}, "l": []string{"0.90", "0.90"},
				"o": "0.95",
			}
		}
		_, _ = w.Write(krakenResp(t, resp))
	}))

	markets, err := src.GetMarkets(context.Background(), "USD")
	require.NoError(t, err)
	assert.Len(t, markets, totalPairs)

	// Should have made ceil(120/50) = 3 ticker batch calls.
	assert.Equal(t, 3, tickerCalls, "expected 3 batched ticker requests for 120 pairs")

	// Every market should have enriched price data.
	for _, m := range markets {
		assert.NotEmpty(t, m.LastPrice, "market %s should have last_price after batched enrichment", m.Pair)
		assert.NotEmpty(t, m.Volume24h, "market %s should have volume_24h after batched enrichment", m.Pair)
		assert.NotEmpty(t, m.Change24hPct, "market %s should have change_24h_pct after batched enrichment", m.Pair)
	}
}

// splitPairs splits a comma-separated Kraken pair string.
func splitPairs(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func TestUnsupportedMethods(t *testing.T) {
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	_, err := src.GetFundingRates(context.Background(), "BTC-USD", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")

	_, err = src.GetOpenInterest(context.Background(), "BTC-USD")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestKrakenAPIError(t *testing.T) {
	src := newTestSource(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"error": []string{"EQuery:Unknown asset pair"}, "result": nil}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))

	_, err := src.GetTicker(context.Background(), []string{"BTC-USD"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown asset pair")
}

func TestFindResultValue(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		input string
		found bool
	}{
		{name: "exact match", keys: []string{"SOLUSD"}, input: "SOLUSD", found: true},
		{name: "extended BTC", keys: []string{"XXBTZUSD"}, input: "XBTUSD", found: true},
		{name: "extended ETH", keys: []string{"XETHZUSD"}, input: "ETHUSD", found: true},
		{name: "no match", keys: []string{"FOOBARZUSD"}, input: "XBTUSD", found: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := make(map[string]json.RawMessage)
			for _, k := range tt.keys {
				raw[k] = json.RawMessage(`"test"`)
			}
			_, ok := findResultValue(raw, tt.input)
			assert.Equal(t, tt.found, ok)
		})
	}
}

func TestRateLimit(t *testing.T) {
	src := &Source{
		points:    maxPoints, // Already at max.
		lastDecay: time.Now(),
	}
	err := src.checkRate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")

	// After enough time passes, should be able to make a request.
	src.lastDecay = time.Now().Add(-2 * time.Second) // 2 seconds ago = 2 points decayed.
	err = src.checkRate()
	assert.NoError(t, err)
}

func TestStripKrakenPrefix(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"XXBT", "XBT"},
		{"XETH", "ETH"},
		{"ZUSD", "USD"},
		{"SOL", "SOL"},
		{"AVAX", "AVAX"},
		{"USD", "USD"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, stripKrakenPrefix(tt.input), "stripKrakenPrefix(%q)", tt.input)
	}
}
