package binance

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
	defaultSpotURL    = "https://api.binance.com"
	defaultFuturesURL = "https://fapi.binance.com"

	// Binance weight-based rate limit: 1200 weight per minute.
	defaultWeightLimit = 1200
	weightWindowDur    = time.Minute
)

// Config holds Binance source configuration.
// SpotURL and FuturesURL can be overridden for testing.
type Config struct {
	SpotURL     string        // defaults to https://api.binance.com
	FuturesURL  string        // defaults to https://fapi.binance.com
	WeightLimit int           // max weight per minute; defaults to 1200
	Timeout     time.Duration // HTTP request timeout; defaults to 10s
}

// Source implements marketdata.DataSource using the Binance public API.
// Spot endpoints for prices, candles, orderbook, and markets.
// Futures (fapi) endpoints for funding rates and open interest.
type Source struct {
	spotURL     string
	futuresURL  string
	registry    *marketdata.SymbolRegistry
	httpClient  *http.Client
	log         *zap.Logger
	weightLimit int

	mu          sync.Mutex
	usedWeight  int
	windowStart time.Time
}

// NewSource creates a Binance data source.
func NewSource(registry *marketdata.SymbolRegistry, cfg Config, log *zap.Logger) *Source {
	if cfg.SpotURL == "" {
		cfg.SpotURL = defaultSpotURL
	}
	if cfg.FuturesURL == "" {
		cfg.FuturesURL = defaultFuturesURL
	}
	if cfg.WeightLimit <= 0 {
		cfg.WeightLimit = defaultWeightLimit
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Source{
		spotURL:     cfg.SpotURL,
		futuresURL:  cfg.FuturesURL,
		registry:    registry,
		httpClient:  &http.Client{Timeout: timeout},
		log:         log,
		weightLimit: cfg.WeightLimit,
		windowStart: time.Now(),
	}
}

func (s *Source) Name() string { return "binance" }

// checkWeight returns an error if the call would exceed the rate limit.
// Resets the window counter once a minute has passed.
func (s *Source) checkWeight(weight int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Sub(s.windowStart) >= weightWindowDur {
		s.usedWeight = 0
		s.windowStart = now
	}

	if s.usedWeight+weight > s.weightLimit {
		return fmt.Errorf("binance rate limit: %d/%d weight used in current window", s.usedWeight, s.weightLimit)
	}
	s.usedWeight += weight
	return nil
}

func (s *Source) get(ctx context.Context, baseURL, path string, weight int) ([]byte, error) {
	if err := s.checkWeight(weight); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance API %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	return body, nil
}

// --- GetTicker ---

type binanceTicker24h struct {
	Symbol             string `json:"symbol"`
	PriceChangePercent string `json:"priceChangePercent"`
	LastPrice          string `json:"lastPrice"`
	BidPrice           string `json:"bidPrice"`
	AskPrice           string `json:"askPrice"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	QuoteVolume        string `json:"quoteVolume"` // volume in quote currency
}

func binanceTickerToCanonical(canonical string, r binanceTicker24h) marketdata.Ticker {
	return marketdata.Ticker{
		Market:       canonical,
		Bid:          r.BidPrice,
		Ask:          r.AskPrice,
		Last:         r.LastPrice,
		Volume24h:    r.QuoteVolume,
		Change24hPct: r.PriceChangePercent,
		High24h:      r.HighPrice,
		Low24h:       r.LowPrice,
		Timestamp:    time.Now().UTC(),
	}
}

// GetTicker returns 24h price snapshots for the given symbols.
// If symbols is empty, fetches all available (weight: 40). Per symbol: weight 1 each.
func (s *Source) GetTicker(ctx context.Context, symbols []string) ([]marketdata.Ticker, error) {
	if len(symbols) == 0 {
		body, err := s.get(ctx, s.spotURL, "/api/v3/ticker/24hr", 40)
		if err != nil {
			return nil, fmt.Errorf("all tickers: %w", err)
		}
		var raw []binanceTicker24h
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("parse tickers: %w", err)
		}
		var result []marketdata.Ticker
		for _, r := range raw {
			canonical, err := s.registry.FromNative(r.Symbol, "binance")
			if err != nil {
				continue // skip unmapped symbols
			}
			result = append(result, binanceTickerToCanonical(canonical, r))
		}
		return result, nil
	}

	var result []marketdata.Ticker
	for _, sym := range symbols {
		native, err := s.registry.ToNative(sym, "binance")
		if err != nil {
			return nil, fmt.Errorf("symbol %q: %w", sym, err)
		}
		body, err := s.get(ctx, s.spotURL, "/api/v3/ticker/24hr?symbol="+native, 1)
		if err != nil {
			return nil, fmt.Errorf("ticker %s: %w", sym, err)
		}
		var raw binanceTicker24h
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("parse ticker %s: %w", sym, err)
		}
		result = append(result, binanceTickerToCanonical(sym, raw))
	}
	return result, nil
}

// --- GetCandles ---

// GetCandles fetches OHLCV bars from the klines endpoint.
// Binance interval format matches ours (1m, 5m, 15m, 1h, 4h, 1d) 1:1.
// Weight: 2 per request.
func (s *Source) GetCandles(ctx context.Context, symbol string, interval marketdata.Interval, limit int, endTime time.Time) ([]marketdata.Candle, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	native, err := s.registry.ToNative(symbol, "binance")
	if err != nil {
		return nil, fmt.Errorf("symbol %q: %w", symbol, err)
	}

	// Fetch one extra candle to account for the open candle that will be filtered out.
	// Without this, responses are consistently limit-1 causing perpetual cache misses.
	fetchLimit := limit + 1
	if fetchLimit > 1000 {
		fetchLimit = 1000
	}
	path := fmt.Sprintf("/api/v3/klines?symbol=%s&interval=%s&limit=%d", native, string(interval), fetchLimit)
	if !endTime.IsZero() {
		path += fmt.Sprintf("&endTime=%d", endTime.UnixMilli())
	}

	body, err := s.get(ctx, s.spotURL, path, 2)
	if err != nil {
		return nil, fmt.Errorf("klines %s: %w", symbol, err)
	}

	var raw [][]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse klines: %w", err)
	}

	// Filter to closed candles only. The Binance klines endpoint can include
	// the currently-open candle (open time <= endTime but not yet closed).
	// A candle is closed when its close time (open time + interval) <= refTime.
	// Cap refTime at now: a future end_time must not admit the currently-open candle.
	now := time.Now().UTC()
	refTime := endTime
	if refTime.IsZero() || refTime.After(now) {
		refTime = now
	}
	intervalDur := interval.Duration()

	candles := make([]marketdata.Candle, 0, len(raw))
	for _, row := range raw {
		c, err := parseKline(row)
		if err != nil {
			s.log.Warn("skipping malformed kline", zap.Error(err))
			continue
		}
		closeTime := c.Timestamp.Add(intervalDur)
		if closeTime.After(refTime) {
			continue // candle not yet closed
		}
		candles = append(candles, c)
	}
	// Trim to the original requested limit. We fetched limit+1 to account for the open
	// candle, but if no open candle was filtered the result can exceed limit.
	if len(candles) > limit {
		candles = candles[len(candles)-limit:]
	}
	return candles, nil
}

// parseKline parses a single Binance kline array:
// [openTime(ms), open, high, low, close, volume, closeTime, ...]
// parseKline parses a Binance kline array:
// [openTime(ms), open, high, low, close, baseVolume, closeTime, quoteVolume, ...]
// Volume uses quoteAssetVolume (index 7) for cross-pair comparability in USD terms.
func parseKline(row []json.RawMessage) (marketdata.Candle, error) {
	if len(row) < 8 {
		return marketdata.Candle{}, fmt.Errorf("kline row too short: %d fields", len(row))
	}

	var openTimeMs int64
	if err := json.Unmarshal(row[0], &openTimeMs); err != nil {
		return marketdata.Candle{}, fmt.Errorf("openTime: %w", err)
	}

	// indices: 1=open, 2=high, 3=low, 4=close, 7=quoteAssetVolume
	fields := make([]string, 5)
	for i, idx := range []int{1, 2, 3, 4, 7} {
		if err := json.Unmarshal(row[idx], &fields[i]); err != nil {
			return marketdata.Candle{}, fmt.Errorf("field %d: %w", idx, err)
		}
	}

	return marketdata.Candle{
		Timestamp: time.UnixMilli(openTimeMs).UTC(),
		Open:      fields[0],
		High:      fields[1],
		Low:       fields[2],
		Close:     fields[3],
		Volume:    fields[4],
	}, nil
}

// --- GetOrderbook ---

// GetOrderbook fetches bids and asks from the depth endpoint.
// Weight: 1 for depth <=100, 5 for depth <=500.
func (s *Source) GetOrderbook(ctx context.Context, symbol string, depth int) (*marketdata.Orderbook, error) {
	if depth <= 0 {
		depth = 20
	}

	weight := 1
	if depth > 100 {
		weight = 5
	}

	native, err := s.registry.ToNative(symbol, "binance")
	if err != nil {
		return nil, fmt.Errorf("symbol %q: %w", symbol, err)
	}

	path := fmt.Sprintf("/api/v3/depth?symbol=%s&limit=%d", native, depth)
	body, err := s.get(ctx, s.spotURL, path, weight)
	if err != nil {
		return nil, fmt.Errorf("orderbook %s: %w", symbol, err)
	}

	var raw struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse orderbook: %w", err)
	}

	bids := make([]marketdata.OrderbookLevel, len(raw.Bids))
	for i, b := range raw.Bids {
		if len(b) >= 2 {
			bids[i] = marketdata.OrderbookLevel{Price: b[0], Size: b[1]}
		}
	}
	asks := make([]marketdata.OrderbookLevel, len(raw.Asks))
	for i, a := range raw.Asks {
		if len(a) >= 2 {
			asks[i] = marketdata.OrderbookLevel{Price: a[0], Size: a[1]}
		}
	}

	return &marketdata.Orderbook{
		Market:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now().UTC(),
	}, nil
}

// --- GetMarkets ---

// GetMarkets fetches all TRADING-status symbols from exchangeInfo.
// Only returns symbols that have a mapping in the symbol registry.
// Weight: 10.
func (s *Source) GetMarkets(ctx context.Context, quote string) ([]marketdata.MarketInfo, error) {
	body, err := s.get(ctx, s.spotURL, "/api/v3/exchangeInfo", 10)
	if err != nil {
		return nil, fmt.Errorf("exchangeInfo: %w", err)
	}

	var raw struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			Status     string `json:"status"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse exchangeInfo: %w", err)
	}

	quote = strings.ToUpper(quote)

	var markets []marketdata.MarketInfo
	var canonicals []string
	for _, sym := range raw.Symbols {
		if sym.Status != "TRADING" {
			continue
		}

		// Filter by quote currency early so ticker enrichment stays under limits.
		if quote != "" && !strings.EqualFold(sym.QuoteAsset, quote) {
			continue
		}

		// Try explicit registry first, then derive canonical from base/quote.
		canonical, err := s.registry.FromNative(sym.Symbol, "binance")
		if err != nil {
			canonical = marketdata.FromNativeBaseQuote(sym.BaseAsset, sym.QuoteAsset, "binance")
		}

		// Register mapping so subsequent GetTicker/GetCandles calls work.
		// TryAdd preserves explicit defaultMappings over algorithmic ones.
		s.registry.TryAddMapping(canonical, "binance", sym.Symbol)

		markets = append(markets, marketdata.MarketInfo{
			Pair:      canonical,
			Base:      sym.BaseAsset,
			Quote:     sym.QuoteAsset,
			Tradeable: true,
		})
		canonicals = append(canonicals, canonical)
	}

	// Enrich with 24h stats (price, volume, change). One call per symbol (weight 1 each).
	// Skip when too many pairs - each costs 1 weight, Binance limit is 1200/min.
	const maxTickerEnrich = 50
	if len(canonicals) > 0 && len(canonicals) <= maxTickerEnrich {
		tickers, tickerErr := s.GetTicker(ctx, canonicals)
		if tickerErr == nil {
			byPair := make(map[string]marketdata.Ticker, len(tickers))
			for _, t := range tickers {
				byPair[t.Market] = t
			}
			for i, m := range markets {
				if t, ok := byPair[m.Pair]; ok {
					markets[i].LastPrice = t.Last
					markets[i].Volume24h = t.Volume24h
					markets[i].Change24hPct = t.Change24hPct
				}
			}
		}
	}

	return markets, nil
}

// --- GetFundingRates ---

// futuresNative converts a canonical pair to Binance futures format.
// USD is substituted with USDT (Binance perps are USDT-margined); other quotes pass through.
func futuresNative(symbol string) (string, error) {
	parts := strings.SplitN(symbol, "-", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", fmt.Errorf("cannot extract base from symbol %q", symbol)
	}
	quote := parts[1]
	if quote == "USD" {
		quote = "USDT"
	}
	return parts[0] + quote, nil
}

// GetFundingRates fetches funding rate history from the futures API.
// Weight: 1.
func (s *Source) GetFundingRates(ctx context.Context, symbol string, limit int) (*marketdata.FundingData, error) {
	if limit <= 0 {
		limit = 10
	}
	// Fetch at least 2 entries so InferFundingIntervalH can compute the actual interval.
	fetchLimit := limit
	if fetchLimit < 2 {
		fetchLimit = 2
	}

	native, err := futuresNative(symbol)
	if err != nil {
		return nil, fmt.Errorf("symbol %q: %w", symbol, err)
	}

	path := fmt.Sprintf("/fapi/v1/fundingRate?symbol=%s&limit=%d", native, fetchLimit)
	body, err := s.get(ctx, s.futuresURL, path, 1)
	if err != nil {
		return nil, fmt.Errorf("fundingRate %s: %w", symbol, err)
	}

	var raw []struct {
		FundingRate string `json:"fundingRate"`
		FundingTime int64  `json:"fundingTime"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse fundingRate: %w", err)
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("no funding rate data for %s", symbol)
	}

	history := make([]marketdata.FundingRate, len(raw))
	for i, r := range raw {
		history[i] = marketdata.FundingRate{
			Timestamp: time.UnixMilli(r.FundingTime).UTC(),
			Rate:      r.FundingRate,
		}
	}

	latest := raw[len(raw)-1]
	rateF, _ := strconv.ParseFloat(latest.FundingRate, 64)

	intervalH := marketdata.InferFundingIntervalH(history)
	periodsPerDay := 24.0 / float64(intervalH)
	annualized := rateF * periodsPerDay * 365 * 100

	nextFunding := time.UnixMilli(latest.FundingTime).UTC().Add(time.Duration(intervalH) * time.Hour)

	return &marketdata.FundingData{
		Market:           symbol,
		CurrentRate:      latest.FundingRate,
		AnnualizedPct:    fmt.Sprintf("%.2f", annualized),
		FundingIntervalH: intervalH,
		NextFundingTime:  nextFunding,
		History:          history,
	}, nil
}

// --- GetOpenInterest ---

type oiHistItem struct {
	SumOpenInterestValue string `json:"sumOpenInterestValue"` // notional USD
	Timestamp            int64  `json:"timestamp"`
}

// GetOpenInterest fetches OI history (30 1h periods) and returns notional OI with change deltas.
// Weight: 1 (hist endpoint).
func (s *Source) GetOpenInterest(ctx context.Context, symbol string) (*marketdata.OpenInterest, error) {
	native, err := futuresNative(symbol)
	if err != nil {
		return nil, fmt.Errorf("symbol %q: %w", symbol, err)
	}

	// 30 1h periods covers 1h, 4h, and 24h change windows.
	path := fmt.Sprintf("/fapi/v1/openInterestHist?symbol=%s&period=1h&limit=30", native)
	body, err := s.get(ctx, s.futuresURL, path, 1)
	if err != nil {
		return nil, fmt.Errorf("OI history %s: %w", symbol, err)
	}

	var hist []oiHistItem
	if err := json.Unmarshal(body, &hist); err != nil {
		return nil, fmt.Errorf("parse OI history: %w", err)
	}

	if len(hist) == 0 {
		return nil, fmt.Errorf("no OI data for %s", symbol)
	}

	latest := hist[len(hist)-1]
	currentOI, _ := strconv.ParseFloat(latest.SumOpenInterestValue, 64)
	now := time.UnixMilli(latest.Timestamp).UTC()

	change1h, change4h, change24h := computeOIChanges(currentOI, now, hist[:len(hist)-1])

	return &marketdata.OpenInterest{
		Market:         symbol,
		OpenInterest:   fmt.Sprintf("%.2f", currentOI),
		OIChange1hPct:  fmt.Sprintf("%.2f", change1h),
		OIChange4hPct:  fmt.Sprintf("%.2f", change4h),
		OIChange24hPct: fmt.Sprintf("%.2f", change24h),
		LongShortRatio: "1.00", // Binance requires a separate endpoint for this
	}, nil
}

// computeOIChanges finds the closest historical snapshot to each lookback threshold
// (1h, 4h, 24h) and computes the % change from that snapshot to currentOI.
// hist must be sorted oldest-first. Iterates newest-first to find the closest match.
func computeOIChanges(currentOI float64, now time.Time, hist []oiHistItem) (change1h, change4h, change24h float64) {
	found1h, found4h, found24h := false, false, false

	for i := len(hist) - 1; i >= 0; i-- {
		h := hist[i]
		ts := time.UnixMilli(h.Timestamp).UTC()
		age := now.Sub(ts)
		val, _ := strconv.ParseFloat(h.SumOpenInterestValue, 64)
		if val <= 0 {
			continue
		}

		if !found1h && age >= time.Hour {
			change1h = (currentOI - val) / val * 100
			found1h = true
		}
		if !found4h && age >= 4*time.Hour {
			change4h = (currentOI - val) / val * 100
			found4h = true
		}
		if !found24h && age >= 24*time.Hour {
			change24h = (currentOI - val) / val * 100
			found24h = true
		}
		if found1h && found4h && found24h {
			break
		}
	}
	return
}

