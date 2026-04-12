package kraken

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata"
)

const (
	defaultBaseURL = "https://api.kraken.com"

	// Kraken public API: 15-point budget, 1 point per request, decays 1/sec.
	maxPoints    = 15
	maxCandleReq = 720
)

// Interval mapping: canonical -> Kraken minutes.
var krakenIntervals = map[marketdata.Interval]int{
	marketdata.Interval1m:  1,
	marketdata.Interval5m:  5,
	marketdata.Interval15m: 15,
	marketdata.Interval1h:  60,
	marketdata.Interval4h:  240,
	marketdata.Interval1d:  1440,
}

// intervalDurations maps intervals to their time.Duration for since-parameter computation.
var intervalDurations = map[marketdata.Interval]time.Duration{
	marketdata.Interval1m:  time.Minute,
	marketdata.Interval5m:  5 * time.Minute,
	marketdata.Interval15m: 15 * time.Minute,
	marketdata.Interval1h:  time.Hour,
	marketdata.Interval4h:  4 * time.Hour,
	marketdata.Interval1d:  24 * time.Hour,
}

// Config holds Kraken source configuration.
type Config struct {
	BaseURL     string        // defaults to https://api.kraken.com
	Timeout     time.Duration // Base per-attempt HTTP timeout; defaults to 10s.
	MaxAttempts int           // Max request attempts. Attempt N uses timeout=Timeout*N. Defaults to 3.
}

// Source implements marketdata.DataSource using the Kraken public REST API.
// Spot data only (ticker, candles, orderbook, markets).
// Funding rates and open interest are not supported (spot exchange).
type Source struct {
	baseURL     string
	registry    *marketdata.SymbolRegistry
	httpClient  *http.Client
	baseTimeout time.Duration
	maxAttempts int
	log         *zap.Logger

	mu        sync.Mutex
	points    int
	lastDecay time.Time
}

// NewSource creates a Kraken data source.
func NewSource(registry *marketdata.SymbolRegistry, cfg Config, log *zap.Logger) *Source {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	// No client-level timeout; per-attempt deadline is enforced via request context
	// so we can grow it on retry.
	return &Source{
		baseURL:     cfg.BaseURL,
		registry:    registry,
		httpClient:  &http.Client{},
		baseTimeout: timeout,
		maxAttempts: attempts,
		log:         log,
		lastDecay:   time.Now(),
	}
}

func (s *Source) Name() string { return "kraken" }

// checkRate implements a leaky bucket rate limiter (15 points max, decays 1/sec).
func (s *Source) checkRate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.lastDecay)
	decay := int(elapsed.Seconds())
	if decay > 0 {
		s.points -= decay
		if s.points < 0 {
			s.points = 0
		}
		s.lastDecay = s.lastDecay.Add(time.Duration(decay) * time.Second)
	}

	if s.points+1 > maxPoints {
		return fmt.Errorf("kraken rate limit: %d/%d points used", s.points, maxPoints)
	}
	s.points++
	return nil
}

// krakenResponse is the standard Kraken API response wrapper.
type krakenResponse struct {
	Error  []string        `json:"error"`
	Result json.RawMessage `json:"result"`
}

// getResult performs a GET request and unwraps the Kraken response.
// Retries on transport errors (notably context deadline exceeded) up to
// s.maxAttempts, growing the per-attempt timeout linearly: attempt N uses
// s.baseTimeout * N. HTTP-level errors (non-200 status, kraken API error)
// are returned immediately - no point retrying a 400 or a rate-limit reply.
func (s *Source) getResult(ctx context.Context, path string) (json.RawMessage, error) {
	if err := s.checkRate(); err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= s.maxAttempts; attempt++ {
		body, status, err := s.doRequest(ctx, path, s.baseTimeout*time.Duration(attempt))
		if err != nil {
			lastErr = err
			// Caller cancelled - don't retry.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("http get %s: %w", path, err)
			}
			s.log.Warn("kraken request failed, retrying",
				zap.String("path", path),
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", s.maxAttempts),
				zap.Duration("attempt_timeout", s.baseTimeout*time.Duration(attempt)),
				zap.Error(err))
			continue
		}

		if status != http.StatusOK {
			return nil, fmt.Errorf("kraken API %s returned %d: %s", path, status, string(body))
		}

		var kr krakenResponse
		if err := json.Unmarshal(body, &kr); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(kr.Error) > 0 {
			return nil, fmt.Errorf("kraken API error: %s", strings.Join(kr.Error, "; "))
		}
		return kr.Result, nil
	}
	return nil, fmt.Errorf("http get %s: all %d attempts failed: %w", path, s.maxAttempts, lastErr)
}

// doRequest performs a single HTTP GET with its own deadline and returns the
// raw body + status. Transport errors are returned as-is for the retry loop.
func (s *Source) doRequest(ctx context.Context, path string, timeout time.Duration) ([]byte, int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return body, resp.StatusCode, nil
}

// findResultValue finds a value in a Kraken response map by matching the input pair key.
// Kraken response keys may differ from input (e.g., XBTUSD input -> XXBTZUSD response key).
// Strategy: try exact match, then try X-prefix + Z-prefix variant, then substring match.
func findResultValue(result map[string]json.RawMessage, inputPair string) (json.RawMessage, bool) {
	// Exact match.
	if v, ok := result[inputPair]; ok {
		return v, true
	}

	// Try X/Z prefix expansion: XBTUSD -> XXBTZUSD, ETHUSD -> XETHZUSD.
	// Kraken prefixes crypto bases with X and fiat quotes with Z for legacy pairs.
	expanded := expandKrakenKey(inputPair)
	if expanded != inputPair {
		if v, ok := result[expanded]; ok {
			return v, true
		}
	}

	// Fallback: iterate and try any key whose expanded form matches.
	for k, v := range result {
		if k == "last" {
			continue // cursor key in OHLC responses
		}
		// Strip prefixes from response key and compare.
		if stripKrakenPairPrefix(k) == inputPair {
			return v, true
		}
	}
	return nil, false
}

// expandKrakenKey adds X/Z prefixes to a Kraken input pair to match response keys.
// XBTUSD -> XXBTZUSD, ETHUSD -> XETHZUSD, SOLUSD -> SOLUSD (no change for modern pairs).
func expandKrakenKey(pair string) string {
	// Known fiat suffixes that get Z prefix in responses.
	for _, fiat := range []string{"USD", "EUR", "GBP", "JPY", "CAD", "AUD"} {
		if strings.HasSuffix(pair, fiat) {
			base := strings.TrimSuffix(pair, fiat)
			return "X" + base + "Z" + fiat
		}
	}
	return pair
}

// stripKrakenPairPrefix removes X/Z prefixes from a Kraken response pair key.
// XXBTZUSD -> XBTUSD, XETHZUSD -> ETHUSD.
func stripKrakenPairPrefix(key string) string {
	// Try to find a Z-prefixed fiat in the key.
	for _, fiat := range []string{"USD", "EUR", "GBP", "JPY", "CAD", "AUD"} {
		zfiat := "Z" + fiat
		if idx := strings.Index(key, zfiat); idx > 0 {
			base := key[:idx]
			// Strip X prefix from base if present.
			if len(base) > 1 && base[0] == 'X' {
				base = base[1:]
			}
			return base + fiat
		}
	}
	return key
}

// --- GetTicker ---

func (s *Source) GetTicker(ctx context.Context, symbols []string) ([]marketdata.Ticker, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("kraken: GetTicker requires explicit symbols (use binance for bulk fetch)")
	}

	// Build canonical -> native mapping.
	type pairEntry struct {
		canonical string
		native    string
	}
	pairs := make([]pairEntry, 0, len(symbols))
	nativeList := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		native, err := s.registry.ToNative(sym, "kraken")
		if err != nil {
			s.log.Debug("Skipping unmapped symbol for ticker", zap.String("symbol", sym), zap.Error(err))
			continue
		}
		pairs = append(pairs, pairEntry{canonical: sym, native: native})
		nativeList = append(nativeList, native)
	}
	if len(nativeList) == 0 {
		return nil, fmt.Errorf("kraken: no valid symbols to fetch")
	}

	result, err := s.getResult(ctx, "/0/public/Ticker?pair="+strings.Join(nativeList, ","))
	if err != nil {
		return nil, fmt.Errorf("kraken GetTicker: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, fmt.Errorf("decode ticker result: %w", err)
	}

	tickers := make([]marketdata.Ticker, 0, len(pairs))
	for _, p := range pairs {
		data, ok := findResultValue(raw, p.native)
		if !ok {
			s.log.Debug("Ticker key not found in response", zap.String("native", p.native))
			continue
		}

		var t krakenTicker
		if err := json.Unmarshal(data, &t); err != nil {
			s.log.Warn("Failed to parse ticker", zap.String("pair", p.native), zap.Error(err))
			continue
		}

		last := safeIndex(t.Close, 0)
		open := t.Open

		// Compute 24h change %
		changePct := ""
		if openF, err1 := strconv.ParseFloat(open, 64); err1 == nil && openF > 0 {
			if lastF, err2 := strconv.ParseFloat(last, 64); err2 == nil {
				changePct = strconv.FormatFloat((lastF-openF)/openF*100, 'f', 2, 64)
			}
		}

		// Volume: Kraken reports base currency. Convert to quote by multiplying by last price.
		vol24h := safeIndex(t.Volume, 1) // 24h volume in base
		if lastF, err1 := strconv.ParseFloat(last, 64); err1 == nil {
			if volF, err2 := strconv.ParseFloat(vol24h, 64); err2 == nil {
				vol24h = strconv.FormatFloat(volF*lastF, 'f', 2, 64)
			}
		}

		tickers = append(tickers, marketdata.Ticker{
			Market:       p.canonical,
			Bid:          safeIndex(t.Bid, 0),
			Ask:          safeIndex(t.Ask, 0),
			Last:         last,
			Volume24h:    vol24h,
			Change24hPct: changePct,
			High24h:      safeIndex(t.High, 1),
			Low24h:       safeIndex(t.Low, 1),
			Timestamp:    time.Now().UTC(),
		})
	}

	if len(tickers) == 0 {
		return nil, fmt.Errorf("kraken: no tickers returned")
	}
	return tickers, nil
}

// krakenTicker maps the Kraken ticker response fields.
// Each field is an array: [today, last24h] except Open which is a scalar.
type krakenTicker struct {
	Ask    []string `json:"a"` // [price, wholeLotVol, lotVol]
	Bid    []string `json:"b"` // [price, wholeLotVol, lotVol]
	Close  []string `json:"c"` // [price, lotVol]
	Volume []string `json:"v"` // [today, last24h]
	High   []string `json:"h"` // [today, last24h]
	Low    []string `json:"l"` // [today, last24h]
	Open   string   `json:"o"` // today's opening price
}

func safeIndex(arr []string, i int) string {
	if i < len(arr) {
		return arr[i]
	}
	return "0"
}

// --- GetCandles ---

func (s *Source) GetCandles(ctx context.Context, symbol string, interval marketdata.Interval, limit int, endTime time.Time) ([]marketdata.Candle, error) {
	native, err := s.registry.ToNative(symbol, "kraken")
	if err != nil {
		return nil, fmt.Errorf("kraken GetCandles: %w", err)
	}

	minutes, ok := krakenIntervals[interval]
	if !ok {
		return nil, fmt.Errorf("kraken: unsupported interval %q", interval)
	}

	if limit <= 0 {
		limit = 100
	}
	if limit > maxCandleReq {
		limit = maxCandleReq
	}

	path := fmt.Sprintf("/0/public/OHLC?pair=%s&interval=%d", native, minutes)

	// Compute since parameter to get the right window of candles.
	dur, ok := intervalDurations[interval]
	if ok && !endTime.IsZero() {
		since := endTime.Add(-time.Duration(limit+1) * dur)
		path += fmt.Sprintf("&since=%d", since.Unix())
	} else if ok && endTime.IsZero() {
		// Get latest candles: compute since from now.
		since := time.Now().Add(-time.Duration(limit+1) * dur)
		path += fmt.Sprintf("&since=%d", since.Unix())
	}

	result, err := s.getResult(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("kraken GetCandles: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, fmt.Errorf("decode candles result: %w", err)
	}

	// Find the candle data (skip the "last" cursor key).
	data, ok := findResultValue(raw, native)
	if !ok {
		return nil, fmt.Errorf("kraken: pair %q not found in OHLC response", native)
	}

	var rows [][]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decode candle rows: %w", err)
	}

	candles := make([]marketdata.Candle, 0, len(rows))
	for _, row := range rows {
		if len(row) < 7 {
			continue
		}
		var ts float64
		if err := json.Unmarshal(row[0], &ts); err != nil {
			continue
		}
		// Convert base volume to quote volume (base_vol * close_price) for cross-pair comparability.
		baseVol := unquote(row[6])
		closePrice := unquote(row[4])
		quoteVol := baseToQuoteVolume(baseVol, closePrice)

		candles = append(candles, marketdata.Candle{
			Timestamp: time.Unix(int64(ts), 0).UTC(),
			Open:      unquote(row[1]),
			High:      unquote(row[2]),
			Low:       unquote(row[3]),
			Close:     closePrice,
			Volume:    quoteVol,
		})
	}

	// Trim to requested limit (take last N, they're already oldest-first).
	if len(candles) > limit {
		candles = candles[len(candles)-limit:]
	}

	return candles, nil
}

// unquote extracts a string from a JSON value (may be quoted string or number).
func unquote(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Might be a number without quotes.
	return strings.Trim(string(raw), " \t\n\"")
}

// baseToQuoteVolume converts base currency volume to quote currency: vol * price.
// Returns base volume unchanged if parsing fails.
func baseToQuoteVolume(baseVol, price string) string {
	volF, err1 := strconv.ParseFloat(baseVol, 64)
	priceF, err2 := strconv.ParseFloat(price, 64)
	if err1 != nil || err2 != nil || priceF <= 0 {
		return baseVol
	}
	return strconv.FormatFloat(volF*priceF, 'f', 2, 64)
}

// --- GetOrderbook ---

func (s *Source) GetOrderbook(ctx context.Context, symbol string, depth int) (*marketdata.Orderbook, error) {
	native, err := s.registry.ToNative(symbol, "kraken")
	if err != nil {
		return nil, fmt.Errorf("kraken GetOrderbook: %w", err)
	}

	if depth <= 0 {
		depth = 20
	}
	if depth > 500 {
		depth = 500
	}

	path := fmt.Sprintf("/0/public/Depth?pair=%s&count=%d", native, depth)
	result, err := s.getResult(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("kraken GetOrderbook: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, fmt.Errorf("decode orderbook result: %w", err)
	}

	data, ok := findResultValue(raw, native)
	if !ok {
		return nil, fmt.Errorf("kraken: pair %q not found in Depth response", native)
	}

	var book struct {
		Asks [][]json.RawMessage `json:"asks"`
		Bids [][]json.RawMessage `json:"bids"`
	}
	if err := json.Unmarshal(data, &book); err != nil {
		return nil, fmt.Errorf("decode orderbook: %w", err)
	}

	parseLevel := func(entry []json.RawMessage) marketdata.OrderbookLevel {
		price, size := "", ""
		if len(entry) >= 2 {
			price = unquote(entry[0])
			size = unquote(entry[1])
		}
		return marketdata.OrderbookLevel{Price: price, Size: size}
	}

	ob := &marketdata.Orderbook{
		Market:    symbol,
		Asks:      make([]marketdata.OrderbookLevel, 0, len(book.Asks)),
		Bids:      make([]marketdata.OrderbookLevel, 0, len(book.Bids)),
		Timestamp: time.Now().UTC(),
	}
	for _, a := range book.Asks {
		ob.Asks = append(ob.Asks, parseLevel(a))
	}
	for _, b := range book.Bids {
		ob.Bids = append(ob.Bids, parseLevel(b))
	}

	return ob, nil
}

// --- GetMarkets ---

func (s *Source) GetMarkets(ctx context.Context, quote string) ([]marketdata.MarketInfo, error) {
	result, err := s.getResult(ctx, "/0/public/AssetPairs")
	if err != nil {
		return nil, fmt.Errorf("kraken GetMarkets: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, fmt.Errorf("decode asset pairs: %w", err)
	}

	quote = strings.ToUpper(quote)

	var markets []marketdata.MarketInfo
	var tickerSymbols []string

	for _, data := range raw {
		var pair struct {
			Altname string `json:"altname"`
			Base    string `json:"base"`
			Quote   string `json:"quote"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal(data, &pair); err != nil {
			continue
		}
		if pair.Status != "online" {
			continue
		}

		// Try explicit registry first, then derive canonical from base/quote.
		canonical, err := s.registry.FromNative(pair.Altname, "kraken")
		if err != nil {
			canonical = marketdata.FromNativeBaseQuote(pair.Base, pair.Quote, "kraken")
		}

		// Register mapping so subsequent GetTicker/GetCandles calls work.
		// TryAdd preserves explicit defaultMappings over algorithmic ones.
		s.registry.TryAddMapping(canonical, "kraken", pair.Altname)

		base := stripKrakenPrefix(pair.Base)
		q := stripKrakenPrefix(pair.Quote)

		// Filter by quote currency early so ticker enrichment stays under limits.
		if quote != "" && !strings.EqualFold(q, quote) {
			continue
		}

		markets = append(markets, marketdata.MarketInfo{
			Pair:      canonical,
			Base:      base,
			Quote:     q,
			Tradeable: true,
		})
		tickerSymbols = append(tickerSymbols, canonical)
	}

	// Enrich with ticker data (best-effort) in batches.
	// Kraken URL length limit caps each request at ~50 symbols.
	const batchSize = 50
	if len(tickerSymbols) > 0 {
		tickerMap := make(map[string]marketdata.Ticker, len(tickerSymbols))
		for i := 0; i < len(tickerSymbols); i += batchSize {
			end := i + batchSize
			if end > len(tickerSymbols) {
				end = len(tickerSymbols)
			}
			tickers, err := s.GetTicker(ctx, tickerSymbols[i:end])
			if err != nil {
				s.log.Debug("Failed to enrich markets batch with ticker data",
					zap.Int("batch_start", i), zap.Error(err))
				continue
			}
			for _, t := range tickers {
				tickerMap[t.Market] = t
			}
		}
		for i, m := range markets {
			if t, ok := tickerMap[m.Pair]; ok {
				markets[i].LastPrice = t.Last
				markets[i].Volume24h = t.Volume24h
				markets[i].Change24hPct = t.Change24hPct
			}
		}
	}

	return markets, nil
}

// stripKrakenPrefix removes X (crypto) or Z (fiat) prefix from Kraken asset names.
// XXBT -> XBT, ZUSD -> USD, XETH -> ETH. Modern assets like SOL have no prefix.
func stripKrakenPrefix(asset string) string {
	if len(asset) <= 3 {
		return asset
	}
	// X prefix for crypto (XXBT, XETH, XLTC), Z prefix for fiat (ZUSD, ZEUR).
	if (asset[0] == 'X' || asset[0] == 'Z') && len(asset) == 4 {
		return asset[1:]
	}
	return asset
}

// --- Unsupported (spot exchange) ---

func (s *Source) GetFundingRates(_ context.Context, _ string, _ int) (*marketdata.FundingData, error) {
	return nil, fmt.Errorf("kraken: funding rates not supported (spot exchange), use binance or bybit")
}

func (s *Source) GetOpenInterest(_ context.Context, _ string) (*marketdata.OpenInterest, error) {
	return nil, fmt.Errorf("kraken: open interest not supported (spot exchange), use binance or bybit")
}
