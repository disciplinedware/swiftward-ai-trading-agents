package bybit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newTestSource(t *testing.T, srv *httptest.Server) *Source {
	t.Helper()
	log, _ := zap.NewDevelopment()
	return NewSource(Config{BaseURL: srv.URL}, log)
}

func newServer(t *testing.T, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// --- futuresSymbol ---

func TestFuturesSymbol(t *testing.T) {
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
			got, err := futuresSymbol(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v got err=%v", tt.wantErr, err)
			}
			if got != tt.want {
				t.Errorf("futuresSymbol(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- GetOpenInterest ---

func TestGetOpenInterest(t *testing.T) {
	tests := []struct {
		name       string
		response   any
		symbol     string
		wantErr    bool
		wantOI     string
		wantChg1h  string
		wantChg24h string
	}{
		{
			name:   "valid response with change windows",
			symbol: "ETH-USDC",
			response: map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{"openInterest": "1100.00", "timestamp": "1700010000000"}, // newest (t=0)
						{"openInterest": "1000.00", "timestamp": "1700006400000"}, // exactly 1h ago
						{"openInterest": "900.00", "timestamp": "1699923600000"},  // exactly 24h ago
					},
				},
			},
			wantErr:    false,
			wantOI:     "1100.00",
			wantChg1h:  "10.00",  // (1100-1000)/1000*100
			wantChg24h: "22.22",  // (1100-900)/900*100
		},
		{
			name:   "retCode non-zero",
			symbol: "ETH-USDC",
			response: map[string]any{
				"retCode": 10001,
				"retMsg":  "invalid symbol",
				"result":  map[string]any{"list": []any{}},
			},
			wantErr: true,
		},
		{
			name:   "empty list",
			symbol: "ETH-USDC",
			response: map[string]any{
				"retCode": 0,
				"result":  map[string]any{"list": []any{}},
			},
			wantErr: true,
		},
		{
			name:    "bad symbol",
			symbol:  "INVALID",
			response: map[string]any{"retCode": 0, "result": map[string]any{"list": []any{}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, tt.response)
			defer srv.Close()
			src := newTestSource(t, srv)

			result, err := src.GetOpenInterest(context.Background(), tt.symbol)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if result.OpenInterest != tt.wantOI {
				t.Errorf("OpenInterest: want %q got %q", tt.wantOI, result.OpenInterest)
			}
			if result.OIChange1hPct != tt.wantChg1h {
				t.Errorf("OIChange1hPct: want %q got %q", tt.wantChg1h, result.OIChange1hPct)
			}
			if result.OIChange24hPct != tt.wantChg24h {
				t.Errorf("OIChange24hPct: want %q got %q", tt.wantChg24h, result.OIChange24hPct)
			}
			if result.Market != tt.symbol {
				t.Errorf("Market: want %q got %q", tt.symbol, result.Market)
			}
		})
	}
}

// --- GetFundingRates ---

func TestGetFundingRates(t *testing.T) {
	tests := []struct {
		name        string
		response    any
		symbol      string
		wantErr     bool
		wantRate    string
		wantHistory int
	}{
		{
			name:   "valid response",
			symbol: "BTC-USDC",
			response: map[string]any{
				"retCode": 0,
				"result": map[string]any{
					"list": []map[string]any{
						{"symbol": "BTCUSDT", "fundingRate": "0.00010000", "fundingRateTimestamp": "1700010000000"},
						{"symbol": "BTCUSDT", "fundingRate": "0.00008000", "fundingRateTimestamp": "1699981200000"},
					},
				},
			},
			wantErr:     false,
			wantRate:    "0.00010000",
			wantHistory: 2,
		},
		{
			name:   "retCode non-zero",
			symbol: "BTC-USDC",
			response: map[string]any{
				"retCode": 10001,
				"retMsg":  "error",
				"result":  map[string]any{"list": []any{}},
			},
			wantErr: true,
		},
		{
			name:   "empty list",
			symbol: "BTC-USDC",
			response: map[string]any{
				"retCode": 0,
				"result":  map[string]any{"list": []any{}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, tt.response)
			defer srv.Close()
			src := newTestSource(t, srv)

			result, err := src.GetFundingRates(context.Background(), tt.symbol, 10)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				return
			}
			if result.CurrentRate != tt.wantRate {
				t.Errorf("CurrentRate: want %q got %q", tt.wantRate, result.CurrentRate)
			}
			if len(result.History) != tt.wantHistory {
				t.Errorf("History length: want %d got %d", tt.wantHistory, len(result.History))
			}
			if result.Market != tt.symbol {
				t.Errorf("Market: want %q got %q", tt.symbol, result.Market)
			}
		})
	}
}

// --- HTTP error ---

func TestGetOpenInterest_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()
	src := newTestSource(t, srv)

	_, err := src.GetOpenInterest(context.Background(), "ETH-USDC")
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

// --- Unsupported methods ---

func TestUnsupportedMethods(t *testing.T) {
	srv := newServer(t, map[string]any{})
	defer srv.Close()
	src := newTestSource(t, srv)
	ctx := context.Background()

	if _, err := src.GetTicker(ctx, nil); err == nil {
		t.Error("GetTicker: expected error")
	}
	if _, err := src.GetCandles(ctx, "ETH-USDC", "", 10, time.Time{}); err == nil {
		t.Error("GetCandles: expected error")
	}
	if _, err := src.GetOrderbook(ctx, "ETH-USDC", 20); err == nil {
		t.Error("GetOrderbook: expected error")
	}
	if _, err := src.GetMarkets(ctx, ""); err == nil {
		t.Error("GetMarkets: expected error")
	}
}
