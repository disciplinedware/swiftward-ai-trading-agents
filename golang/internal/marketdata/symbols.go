package marketdata

import (
	"fmt"
	"strings"
	"sync"
)

// SymbolRegistry maps between canonical (BASE-QUOTE) and source-native symbol formats.
type SymbolRegistry struct {
	mu sync.RWMutex
	// canonical -> source -> native symbol
	mappings map[string]map[string]string
	// source -> native -> canonical (reverse lookup)
	reverse map[string]map[string]string
}

// Default mappings for pairs where the native symbol is non-obvious.
// Only needed for Kraken legacy names (XBT=BTC, XDG=DOGE).
// All other pairs are derived algorithmically by ToNative/FromNative.
var defaultMappings = map[string]map[string]string{
	"BTC-USD":  {"kraken": "XBTUSD"},
	"DOGE-USD": {"kraken": "XDGUSD"},
}

// NewSymbolRegistry creates a registry pre-loaded with default mappings.
func NewSymbolRegistry() *SymbolRegistry {
	r := &SymbolRegistry{
		mappings: make(map[string]map[string]string),
		reverse:  make(map[string]map[string]string),
	}
	for canonical, sources := range defaultMappings {
		for source, native := range sources {
			r.addMapping(canonical, source, native)
		}
	}
	return r
}

// addMapping adds a mapping without locking (used during construction).
func (r *SymbolRegistry) addMapping(canonical, source, native string) {
	if r.mappings[canonical] == nil {
		r.mappings[canonical] = make(map[string]string)
	}
	r.mappings[canonical][source] = native

	if r.reverse[source] == nil {
		r.reverse[source] = make(map[string]string)
	}
	r.reverse[source][native] = canonical
}

// AddMapping adds or updates a symbol mapping (thread-safe).
func (r *SymbolRegistry) AddMapping(canonical, source, native string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addMapping(canonical, source, native)
}

// TryAddMapping registers a mapping only if one doesn't already exist for (canonical, source).
// Returns true if the mapping was added, false if it already existed.
func (r *SymbolRegistry) TryAddMapping(canonical, source, native string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sources, ok := r.mappings[canonical]; ok {
		if _, exists := sources[source]; exists {
			return false
		}
	}
	r.addMapping(canonical, source, native)
	return true
}

// ToNative converts a canonical symbol to source-native format.
// If no explicit mapping exists, attempts algorithmic conversion.
func (r *SymbolRegistry) ToNative(canonical, source string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if sources, ok := r.mappings[canonical]; ok {
		if native, ok := sources[source]; ok {
			return native, nil
		}
	}

	// Algorithmic fallback: direct conversion, no cross-quote substitution.
	base, quote, err := ParseCanonical(canonical)
	if err != nil {
		return "", fmt.Errorf("no mapping for %q on %s: %w", canonical, source, err)
	}

	switch source {
	case "binance":
		return base + quote, nil
	case "kraken":
		switch base {
		case "BTC":
			base = "XBT"
		case "DOGE":
			base = "XDG"
		}
		return base + quote, nil
	default:
		return "", fmt.Errorf("no mapping for %q on source %q", canonical, source)
	}
}

// FromNative converts a source-native symbol to canonical format.
// Falls back to algorithmic conversion if no explicit mapping exists.
func (r *SymbolRegistry) FromNative(native, source string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if sourceMap, ok := r.reverse[source]; ok {
		if canonical, ok := sourceMap[native]; ok {
			return canonical, nil
		}
	}

	return fromNativeAlgorithmic(native, source)
}

// FromNativeBaseQuote converts separate base/quote asset names to canonical format.
// Handles source-specific naming (Kraken X/Z prefixes, XBT→BTC, etc.).
func FromNativeBaseQuote(base, quote, source string) string {
	base = normalizeBase(base, source)
	quote = normalizeQuote(quote, source)
	return base + "-" + quote
}

// krakenLegacyBases maps Kraken's X-prefixed legacy base assets to their stripped form.
var krakenLegacyBases = map[string]string{
	"XXBT": "XBT", "XETH": "ETH", "XLTC": "LTC",
	"XXMR": "XMR", "XXRP": "XRP", "XXLM": "XLM",
	"XZEC": "ZEC", "XREP": "REP", "XMLN": "MLN",
	"XXDG": "XDG", "XETC": "ETC",
}

// krakenLegacyQuotes maps Kraken's Z-prefixed legacy quote assets.
var krakenLegacyQuotes = map[string]string{
	"ZUSD": "USD", "ZEUR": "EUR", "ZGBP": "GBP",
	"ZJPY": "JPY", "ZCAD": "CAD", "ZAUD": "AUD",
}

// normalizeBase converts source-specific base asset names to canonical.
func normalizeBase(base, source string) string {
	if source == "kraken" {
		if stripped, ok := krakenLegacyBases[base]; ok {
			base = stripped
		}
	}
	// Known renames across all sources.
	switch base {
	case "XBT":
		return "BTC"
	case "XDG":
		return "DOGE"
	}
	return base
}

// normalizeQuote converts source-specific quote names to canonical.
// USD, USDT, USDC are kept separate - they are different instruments.
func normalizeQuote(quote, source string) string {
	if source == "kraken" {
		if stripped, ok := krakenLegacyQuotes[quote]; ok {
			quote = stripped
		}
	}
	return quote
}

// fromNativeAlgorithmic derives canonical format from a native symbol string.
func fromNativeAlgorithmic(native, source string) (string, error) {
	switch source {
	case "binance":
		// BTCUSDT → BTC-USDT, ETHUSDC → ETH-USDC, ETHBTC → ETH-BTC
		for _, q := range []string{"USDT", "USDC", "BUSD", "BTC", "ETH", "BNB"} {
			if strings.HasSuffix(native, q) {
				base := strings.TrimSuffix(native, q)
				if base == "" {
					continue
				}
				return normalizeBase(base, source) + "-" + normalizeQuote(q, source), nil
			}
		}
		return "", fmt.Errorf("cannot parse binance symbol %q", native)
	case "kraken":
		// XBTUSD → BTC-USD, ETHUSD → ETH-USD, ADAUSDT → ADA-USDT
		for _, q := range []string{"USDT", "USDC", "USD", "EUR", "GBP"} {
			if strings.HasSuffix(native, q) {
				base := strings.TrimSuffix(native, q)
				if base == "" {
					continue
				}
				return normalizeBase(base, source) + "-" + normalizeQuote(q, source), nil
			}
		}
		return "", fmt.Errorf("cannot parse kraken symbol %q", native)
	default:
		return "", fmt.Errorf("no mapping for native %q from source %q", native, source)
	}
}

// HasCanonical returns true if the canonical symbol exists in the registry.
func (r *SymbolRegistry) HasCanonical(canonical string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.mappings[canonical]
	return ok
}

// Canonicals returns all registered canonical symbols.
func (r *SymbolRegistry) Canonicals() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, 0, len(r.mappings))
	for k := range r.mappings {
		result = append(result, k)
	}
	return result
}

// ParseCanonical splits "ETH-USDC" into base="ETH", quote="USDC".
func ParseCanonical(symbol string) (base, quote string, err error) {
	parts := strings.SplitN(symbol, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid canonical symbol %q (expected BASE-QUOTE)", symbol)
	}
	return parts[0], parts[1], nil
}
