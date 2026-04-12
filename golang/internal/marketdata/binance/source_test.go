package binance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata"
)

func newTestSource(t *testing.T, spot, futures *httptest.Server) *Source {
	t.Helper()
	registry := marketdata.NewSymbolRegistry()
	log, _ := zap.NewDevelopment()
	return NewSource(registry, Config{
		SpotURL:    spot.URL,
		FuturesURL: futures.URL,
	}, log)
}

func newNopServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	}))
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- GetTicker ---

func TestGetTicker_Single(t *testing.T) {
	spotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/ticker/24hr" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, binanceTicker24h{
			Symbol:             "ETHUSDT",
			PriceChangePercent: "2.50",
			LastPrice:          "4100.00",
			BidPrice:           "4099.00",
			AskPrice:           "4101.00",
			HighPrice:          "4150.00",
			LowPrice:           "3980.00",
			QuoteVolume:        "50000000.00",
		})
	}))
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	tickers, err := src.GetTicker(context.Background(), []string{"ETH-USDT"})
	if err != nil {
		t.Fatalf("GetTicker: %v", err)
	}
	if len(tickers) != 1 {
		t.Fatalf("expected 1 ticker, got %d", len(tickers))
	}
	got := tickers[0]
	if got.Market != "ETH-USDT" {
		t.Errorf("Market = %q, want ETH-USDT", got.Market)
	}
	if got.Last != "4100.00" {
		t.Errorf("Last = %q, want 4100.00", got.Last)
	}
	if got.Bid != "4099.00" {
		t.Errorf("Bid = %q, want 4099.00", got.Bid)
	}
	if got.Ask != "4101.00" {
		t.Errorf("Ask = %q, want 4101.00", got.Ask)
	}
	if got.Change24hPct != "2.50" {
		t.Errorf("Change24hPct = %q, want 2.50", got.Change24hPct)
	}
}

func TestGetTicker_All(t *testing.T) {
	spotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []binanceTicker24h{
			{Symbol: "ETHUSDT", LastPrice: "4100.00", BidPrice: "4099.00", AskPrice: "4101.00"},
			{Symbol: "BTCUSDT", LastPrice: "95000.00", BidPrice: "94990.00", AskPrice: "95010.00"},
			{Symbol: "UNKNWN", LastPrice: "1.00"}, // unmapped, should be skipped
		})
	}))
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	tickers, err := src.GetTicker(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetTicker all: %v", err)
	}
	// Only ETH and BTC should be returned (UNKNWN has no registry mapping)
	if len(tickers) != 2 {
		t.Fatalf("expected 2 tickers, got %d", len(tickers))
	}
}

func TestGetTicker_UnknownSymbol(t *testing.T) {
	spotSrv := newNopServer()
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	_, err := src.GetTicker(context.Background(), []string{"FAKE-COIN"})
	// Registry has a Binance fallback (FAKE-COIN -> FAKECOINUSDT), but the nop server
	// returns HTTP 500, so GetTicker must return an error.
	if err == nil {
		t.Error("expected error for unknown symbol with HTTP 500 server, got nil")
	}
}

// --- GetCandles ---

func TestGetCandles(t *testing.T) {
	openTime := time.Now().Add(-2 * time.Hour).UnixMilli()

	spotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 2 klines
		klines := [][]interface{}{
			{openTime, "4050.00", "4100.00", "4030.00", "4080.00", "1234.56", openTime + 3600000, "5000000.00", 100, "600.00", "2400000.00", "0"},
			{openTime + 3600000, "4080.00", "4120.00", "4060.00", "4100.00", "2000.00", openTime + 7200000, "8200000.00", 150, "1000.00", "4100000.00", "0"},
		}
		writeJSON(w, klines)
	}))
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	candles, err := src.GetCandles(context.Background(), "ETH-USDT", marketdata.Interval1h, 10, time.Time{})
	if err != nil {
		t.Fatalf("GetCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(candles))
	}
	if candles[0].Open != "4050.00" {
		t.Errorf("first candle Open = %q, want 4050.00", candles[0].Open)
	}
	if candles[1].Close != "4100.00" {
		t.Errorf("second candle Close = %q, want 4100.00", candles[1].Close)
	}
	if candles[0].Timestamp.Unix() != time.UnixMilli(openTime).Unix() {
		t.Errorf("first candle Timestamp = %v, want %v", candles[0].Timestamp, time.UnixMilli(openTime))
	}
}

func TestGetCandles_MalformedRow(t *testing.T) {
	spotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mix valid and malformed rows
		raw := `[[1700000000000,"4050.00","4100.00","4030.00","4080.00","1234.56",1700003600000,"5000000.00"],["bad"]]`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(raw))
	}))
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	candles, err := src.GetCandles(context.Background(), "ETH-USDT", marketdata.Interval1h, 10, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Malformed row is skipped, valid one is kept
	if len(candles) != 1 {
		t.Errorf("expected 1 candle (malformed skipped), got %d", len(candles))
	}
}

// --- GetOrderbook ---

func TestGetOrderbook(t *testing.T) {
	spotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"lastUpdateId": 12345,
			"bids":         [][]string{{"4099.00", "1.5"}, {"4098.00", "2.0"}},
			"asks":         [][]string{{"4101.00", "0.8"}, {"4102.00", "1.2"}},
		})
	}))
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	ob, err := src.GetOrderbook(context.Background(), "ETH-USDT", 2)
	if err != nil {
		t.Fatalf("GetOrderbook: %v", err)
	}
	if ob.Market != "ETH-USDT" {
		t.Errorf("Market = %q, want ETH-USDT", ob.Market)
	}
	if len(ob.Bids) != 2 {
		t.Errorf("expected 2 bids, got %d", len(ob.Bids))
	}
	if len(ob.Asks) != 2 {
		t.Errorf("expected 2 asks, got %d", len(ob.Asks))
	}
	if ob.Bids[0].Price != "4099.00" {
		t.Errorf("best bid = %q, want 4099.00", ob.Bids[0].Price)
	}
	if ob.Asks[0].Price != "4101.00" {
		t.Errorf("best ask = %q, want 4101.00", ob.Asks[0].Price)
	}
}

// --- GetMarkets ---

func TestGetMarkets(t *testing.T) {
	spotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"symbols": []map[string]interface{}{
				{"symbol": "ETHUSDT", "status": "TRADING", "baseAsset": "ETH", "quoteAsset": "USDT"},
				{"symbol": "BTCUSDT", "status": "TRADING", "baseAsset": "BTC", "quoteAsset": "USDT"},
				{"symbol": "BNBUSDT", "status": "TRADING", "baseAsset": "BNB", "quoteAsset": "USDT"},   // no registry mapping
				{"symbol": "ETHUSDT", "status": "BREAK", "baseAsset": "ETH", "quoteAsset": "USDT"},    // not TRADING
			},
		})
	}))
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	markets, err := src.GetMarkets(context.Background(), "")
	if err != nil {
		t.Fatalf("GetMarkets: %v", err)
	}
	// ETH, BTC, and BNB (algorithmically mapped). BREAK status filtered out.
	if len(markets) != 3 {
		t.Errorf("expected 3 markets (BREAK skipped), got %d", len(markets))
	}
	for _, m := range markets {
		if !m.Tradeable {
			t.Errorf("market %s should be tradeable", m.Pair)
		}
	}
}

// --- futuresNative ---

func TestFuturesNative(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"ETH-USD", "ETHUSDT", false},   // USD -> USDT
		{"BTC-USD", "BTCUSDT", false},   // USD -> USDT
		{"ETH-USDT", "ETHUSDT", false},  // passthrough
		{"ETH-USDC", "ETHUSDC", false},  // passthrough
		{"SOL-USD", "SOLUSDT", false},   // USD -> USDT
		{"INVALID", "", true},           // no separator
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := futuresNative(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v got err=%v", tt.wantErr, err)
			}
			if got != tt.want {
				t.Errorf("futuresNative(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- GetFundingRates ---

func TestGetFundingRates(t *testing.T) {
	now := time.Now().UTC().Truncate(8 * time.Hour)
	past1 := now.Add(-16 * time.Hour)
	past2 := now.Add(-8 * time.Hour)

	futuresSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]interface{}{
			{"symbol": "ETHUSDT", "fundingRate": "0.000050", "fundingTime": past1.UnixMilli()},
			{"symbol": "ETHUSDT", "fundingRate": "0.000100", "fundingTime": past2.UnixMilli()},
			{"symbol": "ETHUSDT", "fundingRate": "0.000125", "fundingTime": now.UnixMilli()},
		})
	}))
	defer futuresSrv.Close()
	spotSrv := newNopServer()
	defer spotSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	data, err := src.GetFundingRates(context.Background(), "ETH-USDT", 3)
	if err != nil {
		t.Fatalf("GetFundingRates: %v", err)
	}
	if data.Market != "ETH-USDT" {
		t.Errorf("Market = %q, want ETH-USDT", data.Market)
	}
	if data.CurrentRate != "0.000125" {
		t.Errorf("CurrentRate = %q, want 0.000125", data.CurrentRate)
	}
	if len(data.History) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(data.History))
	}
	// annualized = 0.000125 * 3 * 365 * 100
	wantAnnualized := 0.000125 * 3 * 365 * 100
	gotAnnualized, _ := strconv.ParseFloat(data.AnnualizedPct, 64)
	if abs(gotAnnualized-wantAnnualized) > 0.01 {
		t.Errorf("AnnualizedPct = %v, want ~%.2f", data.AnnualizedPct, wantAnnualized)
	}
	// Next funding = last funding + 8h
	wantNext := now.Add(8 * time.Hour)
	if !data.NextFundingTime.Equal(wantNext) {
		t.Errorf("NextFundingTime = %v, want %v", data.NextFundingTime, wantNext)
	}
}

func TestGetFundingRates_EmptyResponse(t *testing.T) {
	futuresSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []interface{}{})
	}))
	defer futuresSrv.Close()
	spotSrv := newNopServer()
	defer spotSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	_, err := src.GetFundingRates(context.Background(), "ETH-USDT", 10)
	if err == nil {
		t.Error("expected error for empty response, got nil")
	}
}

// --- GetOpenInterest ---

func TestGetOpenInterest(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Hour)

	// Build 28 hourly entries + current (29 total, then 30th is "latest")
	entries := make([]map[string]interface{}, 30)
	for i := 0; i < 30; i++ {
		ts := now.Add(-time.Duration(29-i) * time.Hour)
		val := 50000000.0 + float64(i)*100000
		entries[i] = map[string]interface{}{
			"sumOpenInterestValue": strconv.FormatFloat(val, 'f', 2, 64),
			"timestamp":            ts.UnixMilli(),
		}
	}

	futuresSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, entries)
	}))
	defer futuresSrv.Close()
	spotSrv := newNopServer()
	defer spotSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	oi, err := src.GetOpenInterest(context.Background(), "ETH-USDT")
	if err != nil {
		t.Fatalf("GetOpenInterest: %v", err)
	}
	if oi.Market != "ETH-USDT" {
		t.Errorf("Market = %q, want ETH-USDT", oi.Market)
	}
	// Latest is entries[29], value = 50000000 + 29*100000 = 52900000
	wantOI := 50000000.0 + 29*100000.0
	gotOI, _ := strconv.ParseFloat(oi.OpenInterest, 64)
	if abs(gotOI-wantOI) > 1 {
		t.Errorf("OpenInterest = %v, want %.2f", oi.OpenInterest, wantOI)
	}
	// Changes should be non-zero (OI is growing)
	change1h, _ := strconv.ParseFloat(oi.OIChange1hPct, 64)
	if change1h == 0 {
		t.Error("OIChange1hPct should be non-zero")
	}
}

func TestGetOpenInterest_EmptyResponse(t *testing.T) {
	futuresSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []interface{}{})
	}))
	defer futuresSrv.Close()
	spotSrv := newNopServer()
	defer spotSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	_, err := src.GetOpenInterest(context.Background(), "ETH-USDT")
	if err == nil {
		t.Error("expected error for empty response, got nil")
	}
}

// --- computeOIChanges ---

func TestComputeOIChanges(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	currentOI := 100.0

	// entries at 1h, 4h, 24h ago with known values
	hist := []oiHistItem{
		{SumOpenInterestValue: "80.0", Timestamp: now.Add(-25 * time.Hour).UnixMilli()},  // >24h
		{SumOpenInterestValue: "90.0", Timestamp: now.Add(-24 * time.Hour).UnixMilli()},  // exactly 24h
		{SumOpenInterestValue: "95.0", Timestamp: now.Add(-5 * time.Hour).UnixMilli()},   // >4h
		{SumOpenInterestValue: "96.0", Timestamp: now.Add(-4 * time.Hour).UnixMilli()},   // exactly 4h
		{SumOpenInterestValue: "99.0", Timestamp: now.Add(-2 * time.Hour).UnixMilli()},   // >1h
		{SumOpenInterestValue: "99.5", Timestamp: now.Add(-1 * time.Hour).UnixMilli()},   // exactly 1h
	}

	change1h, change4h, change24h := computeOIChanges(currentOI, now, hist)

	// change1h: closest snapshot >= 1h ago is at -1h: 99.5 -> (100-99.5)/99.5*100 ~= 0.5025
	want1h := (100.0 - 99.5) / 99.5 * 100
	if abs(change1h-want1h) > 0.001 {
		t.Errorf("change1h = %.4f, want %.4f", change1h, want1h)
	}

	// change4h: closest snapshot >= 4h ago is at -4h: 96.0 -> (100-96)/96*100 ~= 4.167
	want4h := (100.0 - 96.0) / 96.0 * 100
	if abs(change4h-want4h) > 0.001 {
		t.Errorf("change4h = %.4f, want %.4f", change4h, want4h)
	}

	// change24h: closest snapshot >= 24h ago is at -24h: 90.0 -> (100-90)/90*100 ~= 11.11
	want24h := (100.0 - 90.0) / 90.0 * 100
	if abs(change24h-want24h) > 0.001 {
		t.Errorf("change24h = %.4f, want %.4f", change24h, want24h)
	}
}

// --- Rate limit ---

func TestRateLimit(t *testing.T) {
	spotSrv := newNopServer()
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	registry := marketdata.NewSymbolRegistry()
	log, _ := zap.NewDevelopment()
	src := NewSource(registry, Config{
		SpotURL:     spotSrv.URL,
		FuturesURL:  futuresSrv.URL,
		WeightLimit: 5, // very low for testing
	}, log)

	// Consume all weight
	err := src.checkWeight(5)
	if err != nil {
		t.Fatalf("first checkWeight: %v", err)
	}

	// Next call should fail
	err = src.checkWeight(1)
	if err == nil {
		t.Error("expected rate limit error, got nil")
	}

	// Simulate window reset
	src.mu.Lock()
	src.windowStart = time.Now().Add(-2 * time.Minute)
	src.mu.Unlock()

	// Should succeed after reset
	err = src.checkWeight(1)
	if err != nil {
		t.Errorf("after window reset: %v", err)
	}
}

// --- HTTP error handling ---

func TestGetTicker_HTTPError(t *testing.T) {
	spotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":-1121,"msg":"Invalid symbol"}`, http.StatusBadRequest)
	}))
	defer spotSrv.Close()
	futuresSrv := newNopServer()
	defer futuresSrv.Close()

	src := newTestSource(t, spotSrv, futuresSrv)
	_, err := src.GetTicker(context.Background(), []string{"ETH-USDT"})
	if err == nil {
		t.Error("expected error for HTTP 400, got nil")
	}
}

// --- Integration tests (skipped unless BINANCE_INTEGRATION_TEST=1) ---

func TestBinanceSource_Integration_GetTicker(t *testing.T) {
	if os.Getenv("BINANCE_INTEGRATION_TEST") != "1" {
		t.Skip("set BINANCE_INTEGRATION_TEST=1 to run against real Binance API")
	}

	registry := marketdata.NewSymbolRegistry()
	log, _ := zap.NewDevelopment()
	src := NewSource(registry, Config{}, log)

	tickers, err := src.GetTicker(context.Background(), []string{"ETH-USDT"})
	if err != nil {
		t.Fatalf("GetTicker: %v", err)
	}
	if len(tickers) != 1 {
		t.Fatalf("expected 1 ticker, got %d", len(tickers))
	}
	price, err := strconv.ParseFloat(tickers[0].Last, 64)
	if err != nil || price <= 0 {
		t.Errorf("unexpected last price: %q", tickers[0].Last)
	}
	t.Logf("ETH-USDT: last=%s bid=%s ask=%s change=%s%%", tickers[0].Last, tickers[0].Bid, tickers[0].Ask, tickers[0].Change24hPct)
}

func TestBinanceSource_Integration_GetCandles(t *testing.T) {
	if os.Getenv("BINANCE_INTEGRATION_TEST") != "1" {
		t.Skip("set BINANCE_INTEGRATION_TEST=1 to run against real Binance API")
	}

	registry := marketdata.NewSymbolRegistry()
	log, _ := zap.NewDevelopment()
	src := NewSource(registry, Config{}, log)

	candles, err := src.GetCandles(context.Background(), "ETH-USDT", marketdata.Interval1h, 10, time.Time{})
	if err != nil {
		t.Fatalf("GetCandles: %v", err)
	}
	if len(candles) == 0 {
		t.Fatal("expected candles, got none")
	}
	t.Logf("Got %d candles, first: t=%v o=%s h=%s l=%s c=%s", len(candles),
		candles[0].Timestamp, candles[0].Open, candles[0].High, candles[0].Low, candles[0].Close)
}

// --- Helpers ---

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

