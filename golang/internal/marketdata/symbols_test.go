package marketdata

import (
	"testing"
)

func TestParseCanonical(t *testing.T) {
	tests := []struct {
		name      string
		symbol    string
		wantBase  string
		wantQuote string
		wantErr   bool
	}{
		{"valid ETH-USDC", "ETH-USDC", "ETH", "USDC", false},
		{"valid BTC-USDC", "BTC-USDC", "BTC", "USDC", false},
		{"valid SOL-USDC", "SOL-USDC", "SOL", "USDC", false},
		{"no hyphen", "ETHUSDC", "", "", true},
		{"empty string", "", "", "", true},
		{"only hyphen", "-", "", "", true},
		{"missing base", "-USDC", "", "", true},
		{"missing quote", "ETH-", "", "", true},
		{"multiple hyphens", "ETH-USD-C", "ETH", "USD-C", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, quote, err := ParseCanonical(tt.symbol)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCanonical(%q) err=%v, wantErr=%v", tt.symbol, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if base != tt.wantBase || quote != tt.wantQuote {
					t.Errorf("ParseCanonical(%q) = (%q, %q), want (%q, %q)", tt.symbol, base, quote, tt.wantBase, tt.wantQuote)
				}
			}
		})
	}
}

func TestSymbolRegistry_ToNative(t *testing.T) {
	reg := NewSymbolRegistry()

	tests := []struct {
		name      string
		canonical string
		source    string
		want      string
		wantErr   bool
	}{
		// Algorithmic: no cross-quote conversion.
		{"ETH-USDC to binance", "ETH-USDC", "binance", "ETHUSDC", false},
		{"ETH-USDT to binance", "ETH-USDT", "binance", "ETHUSDT", false},
		{"BTC-USD to kraken (explicit)", "BTC-USD", "kraken", "XBTUSD", false},
		{"DOGE-USD to kraken (explicit)", "DOGE-USD", "kraken", "XDGUSD", false},
		{"ETH-USD to kraken (algorithmic)", "ETH-USD", "kraken", "ETHUSD", false},
		{"BTC-USDT to kraken", "BTC-USDT", "kraken", "XBTUSDT", false},
		{"SOL-USD to kraken", "SOL-USD", "kraken", "SOLUSD", false},
		{"unknown source", "XRP-USDC", "unknown_source", "", true},
		{"invalid canonical", "NOTHYPHENATED", "binance", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reg.ToNative(tt.canonical, tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToNative(%q, %q) err=%v, wantErr=%v", tt.canonical, tt.source, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToNative(%q, %q) = %q, want %q", tt.canonical, tt.source, got, tt.want)
			}
		})
	}
}

func TestSymbolRegistry_FromNative(t *testing.T) {
	reg := NewSymbolRegistry()

	tests := []struct {
		name    string
		native  string
		source  string
		want    string
		wantErr bool
	}{
		// Explicit registry lookups.
		{"kraken XBTUSD explicit", "XBTUSD", "kraken", "BTC-USD", false},
		{"kraken XDGUSD explicit", "XDGUSD", "kraken", "DOGE-USD", false},

		// Algorithmic fallback - binance (no cross-quote).
		{"binance ETHUSDT", "ETHUSDT", "binance", "ETH-USDT", false},
		{"binance BTCUSDT", "BTCUSDT", "binance", "BTC-USDT", false},
		{"binance ADAUSDC", "ADAUSDC", "binance", "ADA-USDC", false},
		{"binance BNBUSDT", "BNBUSDT", "binance", "BNB-USDT", false},
		{"binance ETHBTC", "ETHBTC", "binance", "ETH-BTC", false},
		{"binance no recognized quote", "FAKECOIN", "binance", "", true},

		// Algorithmic fallback - kraken (USD stays USD, not USDC).
		{"kraken ETHUSD", "ETHUSD", "kraken", "ETH-USD", false},
		{"kraken DOTUSD", "DOTUSD", "kraken", "DOT-USD", false},
		{"kraken ADAUSDT", "ADAUSDT", "kraken", "ADA-USDT", false},
		{"kraken ETHEUR", "ETHEUR", "kraken", "ETH-EUR", false},

		// Unknown source.
		{"unknown source", "ETHUSDT", "unknown", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reg.FromNative(tt.native, tt.source)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromNative(%q, %q) err=%v, wantErr=%v", tt.native, tt.source, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("FromNative(%q, %q) = %q, want %q", tt.native, tt.source, got, tt.want)
			}
		})
	}
}

func TestFromNativeBaseQuote(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		quote  string
		source string
		want   string
	}{
		// Kraken legacy assets with X/Z prefixes. USD stays USD.
		{"kraken XXBT/ZUSD", "XXBT", "ZUSD", "kraken", "BTC-USD"},
		{"kraken XETH/ZUSD", "XETH", "ZUSD", "kraken", "ETH-USD"},
		{"kraken XXDG/ZUSD", "XXDG", "ZUSD", "kraken", "DOGE-USD"},

		// Kraken modern assets (no prefix).
		{"kraken SOL/USD", "SOL", "USD", "kraken", "SOL-USD"},
		{"kraken DOT/USDT", "DOT", "USDT", "kraken", "DOT-USDT"},
		{"kraken ADA/USDC", "ADA", "USDC", "kraken", "ADA-USDC"},
		{"kraken ADA/EUR", "ADA", "EUR", "kraken", "ADA-EUR"},

		// Kraken XCAD should NOT strip X (not a legacy asset).
		{"kraken XCAD preserved", "XCAD", "ZUSD", "kraken", "XCAD-USD"},

		// Binance (no prefix handling).
		{"binance ETH/USDT", "ETH", "USDT", "binance", "ETH-USDT"},
		{"binance BTC/USDC", "BTC", "USDC", "binance", "BTC-USDC"},
		{"binance XRP/USDT", "XRP", "USDT", "binance", "XRP-USDT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromNativeBaseQuote(tt.base, tt.quote, tt.source)
			if got != tt.want {
				t.Errorf("FromNativeBaseQuote(%q, %q, %q) = %q, want %q", tt.base, tt.quote, tt.source, got, tt.want)
			}
		})
	}
}

func TestTryAddMapping(t *testing.T) {
	reg := NewSymbolRegistry()

	// BTC-USD already has kraken mapping "XBTUSD" from defaults.
	added := reg.TryAddMapping("BTC-USD", "kraken", "XBTUSDC")
	if added {
		t.Error("TryAddMapping should return false when mapping already exists")
	}

	// Verify original mapping preserved.
	native, err := reg.ToNative("BTC-USD", "kraken")
	if err != nil {
		t.Fatalf("ToNative: %v", err)
	}
	if native != "XBTUSD" {
		t.Errorf("mapping was overwritten: got %q, want XBTUSD", native)
	}

	// New mapping should succeed.
	added = reg.TryAddMapping("DOT-USD", "kraken", "DOTUSD")
	if !added {
		t.Error("TryAddMapping should return true for new mapping")
	}
	native, err = reg.ToNative("DOT-USD", "kraken")
	if err != nil {
		t.Fatalf("ToNative after TryAdd: %v", err)
	}
	if native != "DOTUSD" {
		t.Errorf("got %q, want DOTUSD", native)
	}
}

func TestSymbolRegistry_AddMapping(t *testing.T) {
	reg := NewSymbolRegistry()

	reg.AddMapping("MATIC-USDC", "binance", "MATICUSDT")

	native, err := reg.ToNative("MATIC-USDC", "binance")
	if err != nil {
		t.Fatalf("ToNative after AddMapping: %v", err)
	}
	if native != "MATICUSDT" {
		t.Errorf("ToNative = %q, want MATICUSDT", native)
	}

	canonical, err := reg.FromNative("MATICUSDT", "binance")
	if err != nil {
		t.Fatalf("FromNative after AddMapping: %v", err)
	}
	if canonical != "MATIC-USDC" {
		t.Errorf("FromNative = %q, want MATIC-USDC", canonical)
	}
}

func TestSymbolRegistry_HasCanonical(t *testing.T) {
	reg := NewSymbolRegistry()

	if !reg.HasCanonical("BTC-USD") {
		t.Error("HasCanonical(BTC-USD) = false, want true")
	}
	if reg.HasCanonical("FAKE-COIN") {
		t.Error("HasCanonical(FAKE-COIN) = true, want false")
	}
}

func TestSymbolRegistry_Canonicals(t *testing.T) {
	reg := NewSymbolRegistry()

	canonicals := reg.Canonicals()
	if len(canonicals) != len(defaultMappings) {
		t.Errorf("Canonicals() returned %d items, want %d", len(canonicals), len(defaultMappings))
	}

	seen := make(map[string]bool)
	for _, c := range canonicals {
		seen[c] = true
	}
	for expected := range defaultMappings {
		if !seen[expected] {
			t.Errorf("Canonicals() missing %q", expected)
		}
	}
}

func TestInterval_Duration(t *testing.T) {
	tests := []struct {
		interval Interval
		wantMin  float64
	}{
		{Interval1m, 1},
		{Interval5m, 5},
		{Interval15m, 15},
		{Interval1h, 60},
		{Interval4h, 240},
		{Interval1d, 1440},
		{Interval("invalid"), 0},
	}
	for _, tt := range tests {
		t.Run(string(tt.interval), func(t *testing.T) {
			got := tt.interval.Duration().Minutes()
			if got != tt.wantMin {
				t.Errorf("Interval(%q).Duration() = %v min, want %v min", tt.interval, got, tt.wantMin)
			}
		})
	}
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		input   string
		want    Interval
		wantErr bool
	}{
		{"1m", Interval1m, false},
		{"5m", Interval5m, false},
		{"15m", Interval15m, false},
		{"1h", Interval1h, false},
		{"4h", Interval4h, false},
		{"1d", Interval1d, false},
		{"2h", "", true},
		{"", "", true},
		{"1w", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseInterval(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseInterval(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseInterval(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSymbolRegistry_ToNative_BinanceFallback(t *testing.T) {
	reg := NewSymbolRegistry()

	tests := []struct {
		name      string
		canonical string
		want      string
	}{
		// No cross-quote conversion: USDC stays USDC, USDT stays USDT.
		{"USDC quote", "AAVE-USDC", "AAVEUSDC"},
		{"USDT quote", "AAVE-USDT", "AAVEUSDT"},
		{"USD quote", "AAVE-USD", "AAVEUSD"},
		{"non-USD quote", "ETH-BTC", "ETHBTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reg.ToNative(tt.canonical, "binance")
			if err != nil {
				t.Fatalf("ToNative(%q, binance) unexpected error: %v", tt.canonical, err)
			}
			if got != tt.want {
				t.Errorf("ToNative(%q, binance) = %q, want %q", tt.canonical, got, tt.want)
			}
		})
	}
}
