package agentintel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// MarketSyncer downloads price data from Kraken spot (Trades endpoint) and
// Kraken Futures (public charts endpoint). Pairs are routed to the futures
// fetcher when their canonical name matches a known perpetual/flexible
// futures instrument; everything else falls through to spot.
type MarketSyncer struct {
	httpClient     *http.Client
	paths          Paths
	log            *zap.Logger
	futuresSymbols map[string]string // canonical -> exchange symbol, e.g. "PIXBTUSD" -> "PI_XBTUSD"
	futuresLoaded  bool
}

// NewMarketSyncer creates a market data downloader.
func NewMarketSyncer(paths Paths, log *zap.Logger) *MarketSyncer {
	return &MarketSyncer{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		paths:      paths,
		log:        log,
	}
}

// SyncMarketData downloads candles for all known pairs incrementally.
// Caller must hold the sync lock (via AcquireSyncLock) before calling this.
func (m *MarketSyncer) SyncMarketData(ctx context.Context, meta *Meta) error {
	// Load Kraken Futures instrument list lazily so perp pairs (PI_*, PF_*) can
	// be routed to the futures charts endpoint instead of failing against spot.
	m.ensureFuturesSymbols(ctx)

	ts, err := LoadBlockTimestamps(m.paths)
	if err != nil {
		return fmt.Errorf("load timestamps: %w", err)
	}

	// Find earliest trade timestamp per canonical pair by walking per-agent intent files.
	// This runs once per market sync and is bounded by (agents × intents per agent).
	earliestByPair := make(map[string]int64)
	for _, agentID := range meta.KnownAgentIDs {
		intents, err := ReadJSONL[TradeIntent](m.paths.AgentIntents(agentID))
		if err != nil {
			m.log.Warn("Failed to read agent intents for market start-time lookup",
				zap.Int64("agent", agentID), zap.Error(err))
			continue
		}
		for _, intent := range intents {
			key := fmt.Sprintf("%d", intent.Block)
			blockTS, ok := ts[key]
			if !ok {
				continue
			}
			cp := intent.CanonicalPair
			if cp == "" {
				cp = CanonicalPair(intent.Pair)
			}
			if existing, ok := earliestByPair[cp]; !ok || blockTS < existing {
				earliestByPair[cp] = blockTS
			}
		}
	}

	// Deduplicate canonical pairs (many raw pairs map to same canonical).
	seen := make(map[string]bool)
	var uniquePairs []string
	for _, pair := range meta.KnownPairs {
		if !seen[pair] {
			seen[pair] = true
			uniquePairs = append(uniquePairs, pair)
		}
	}

	// Per-pair freshness: skip API polling entirely if the last candle is within
	// MarketFreshnessWindow of now. Saves one HTTP call per caught-up pair on re-syncs.
	const marketFreshnessWindow = 2 * time.Minute

	skippedFresh := 0
	for i, pair := range uniquePairs {
		cursor := meta.MarketCursors[pair]

		// Skip if we already have a candle within freshness window.
		if cursor.LastTS > 0 && time.Since(time.Unix(cursor.LastTS, 0)) < marketFreshnessWindow {
			skippedFresh++
			continue
		}

		// Determine start time (unix seconds).
		var startTS int64
		if cursor.LastTS > 0 {
			startTS = cursor.LastTS
		} else if earliest, ok := earliestByPair[pair]; ok {
			startTS = earliest - 60 // 1 minute pad before earliest trade
		} else {
			m.log.Info("No trades for pair, skipping", zap.String("pair", pair))
			continue
		}

		m.log.Info("Fetching market data",
			zap.String("pair", pair),
			zap.Int("progress", i+1),
			zap.Int("total", len(uniquePairs)),
		)

		var (
			newCandles []Candle
			lastCursor string
			source     string
			fetchErr   error
		)
		if exchSym, ok := m.futuresSymbols[pair]; ok {
			source = "kraken-futures"
			newCandles, fetchErr = m.fetchKrakenFuturesCandles(ctx, pair, exchSym, startTS)
		} else {
			source = "kraken"
			sinceNS := cursor.LastCursorNS
			if sinceNS == "" {
				sinceNS = fmt.Sprintf("%d", startTS*1_000_000_000)
			}
			newCandles, lastCursor, fetchErr = m.fetchKrakenTrades(ctx, pair, sinceNS)
		}
		if fetchErr != nil {
			m.log.Warn("Failed to fetch market data, skipping",
				zap.String("pair", pair),
				zap.String("source", source),
				zap.Error(fetchErr),
			)
			continue
		}

		if len(newCandles) > 0 {
			path := filepath.Join(m.paths.Raw, "marketdata", pair+".jsonl")
			recs := make([]any, len(newCandles))
			for i := range newCandles {
				recs[i] = newCandles[i]
			}
			if err := AppendJSONL(path, recs...); err != nil {
				return fmt.Errorf("append candles for %s: %w", pair, err)
			}

			cursor.LastCursorNS = lastCursor
			cursor.LastTS = newCandles[len(newCandles)-1].T
			cursor.Source = source
			meta.MarketCursors[pair] = cursor

			m.log.Info("Downloaded market data",
				zap.String("pair", pair),
				zap.String("source", source),
				zap.Int("candles", len(newCandles)),
			)

			// Save meta after each pair so progress is not lost on error/interrupt.
			if err := SaveMeta(m.paths, *meta); err != nil {
				m.log.Warn("Failed to save meta", zap.Error(err))
			}
		}
	}
	if skippedFresh > 0 {
		m.log.Info("Market pairs skipped as fresh",
			zap.Int("skipped", skippedFresh),
			zap.Int("total", len(uniquePairs)),
		)
	}
	return nil
}

// krakenTradesResponse is the Kraken /0/public/Trades response.
type krakenTradesResponse struct {
	Error  []string               `json:"error"`
	Result map[string]interface{} `json:"result"`
}

// fetchKrakenTrades downloads raw trades from Kraken and aggregates into 1-min candles.
// Paginates using the `last` cursor until caught up to now.
func (m *MarketSyncer) fetchKrakenTrades(ctx context.Context, pair, sinceNS string) ([]Candle, string, error) {
	krakenPair := pairToKraken(pair)
	cursor := sinceNS
	now := time.Now().Unix()
	maxRequests := 500 // safety limit

	// Collect all raw trades across pages, then aggregate once to avoid
	// overlapping candles at page boundaries.
	var allRawTrades []krakenRawTrade

	for i := 0; i < maxRequests; i++ {
		select {
		case <-ctx.Done():
			return aggregateToCandles(allRawTrades, pair), cursor, ctx.Err()
		default:
		}

		url := fmt.Sprintf("https://api.kraken.com/0/public/Trades?pair=%s&since=%s", krakenPair, cursor)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, cursor, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", "agent-intel/1.0")

		resp, err := m.httpClient.Do(req)
		if err != nil {
			return nil, cursor, fmt.Errorf("fetch trades: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		var result krakenTradesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, cursor, fmt.Errorf("unmarshal: %w", err)
		}
		if len(result.Error) > 0 {
			return nil, cursor, fmt.Errorf("kraken error: %v", result.Error)
		}

		// Find the trades array (key varies by pair).
		var trades []interface{}
		var lastCursor string
		for k, v := range result.Result {
			if k == "last" {
				switch lv := v.(type) {
				case string:
					lastCursor = lv
				case float64:
					lastCursor = strconv.FormatInt(int64(lv), 10)
				}
				continue
			}
			if arr, ok := v.([]interface{}); ok {
				trades = arr
			}
		}

		if len(trades) == 0 {
			break
		}

		// Safety check: if Kraken didn't return a cursor (missing "last" key) or
		// returned the same cursor we sent, break to avoid refetching from the
		// beginning in an infinite loop.
		if lastCursor == "" || lastCursor == cursor {
			m.log.Warn("Kraken returned empty or unchanged cursor, stopping pagination",
				zap.String("pair", pair),
				zap.String("sent_cursor", cursor),
				zap.String("received_cursor", lastCursor),
			)
			break
		}

		// Parse trades: [price, volume, time, buy/sell, market/limit, misc, trade_id]
		for _, t := range trades {
			arr, ok := t.([]interface{})
			if !ok || len(arr) < 3 {
				continue
			}
			priceStr, _ := arr[0].(string)
			volStr, _ := arr[1].(string)
			tsFloat, _ := arr[2].(float64)

			price, _ := decimal.NewFromString(priceStr)
			vol, _ := decimal.NewFromString(volStr)
			allRawTrades = append(allRawTrades, krakenRawTrade{price: price, volume: vol, ts: tsFloat})
		}

		cursor = lastCursor

		// Check if last trade is recent enough (within 2 minutes of now).
		if len(allRawTrades) > 0 {
			lastTradeTS := int64(allRawTrades[len(allRawTrades)-1].ts)
			if now-lastTradeTS < 120 {
				break // caught up
			}
		}

		// Rate limit: 2 seconds between requests.
		time.Sleep(2 * time.Second)
	}

	candles := aggregateToCandles(allRawTrades, pair)
	return candles, cursor, nil
}

// krakenRawTrade is a single trade from the Kraken Trades endpoint.
type krakenRawTrade struct {
	price  decimal.Decimal
	volume decimal.Decimal
	ts     float64
}

// aggregateToCandles groups raw trades into 1-minute OHLCV candles.
func aggregateToCandles(trades []krakenRawTrade, pair string) []Candle {
	if len(trades) == 0 {
		return nil
	}

	// Group by minute (floor to 60-second boundary).
	type minuteBucket struct {
		open, high, low, close decimal.Decimal
		volume                 decimal.Decimal
		quoteVolume            decimal.Decimal // sum(price * volume) for VWAP
		ts                     int64
	}
	buckets := make(map[int64]*minuteBucket)

	for _, t := range trades {
		minute := (int64(t.ts) / 60) * 60
		b, ok := buckets[minute]
		if !ok {
			b = &minuteBucket{
				open: t.price, high: t.price, low: t.price,
				close: t.price, volume: decimal.Zero,
				quoteVolume: decimal.Zero, ts: minute,
			}
			buckets[minute] = b
		}
		if t.price.GreaterThan(b.high) {
			b.high = t.price
		}
		if t.price.LessThan(b.low) {
			b.low = t.price
		}
		b.close = t.price
		b.volume = b.volume.Add(t.volume)
		b.quoteVolume = b.quoteVolume.Add(t.price.Mul(t.volume))
	}

	// Sort by time.
	var minutes []int64
	for m := range buckets {
		minutes = append(minutes, m)
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i] < minutes[j] })

	candles := make([]Candle, 0, len(minutes))
	for _, m := range minutes {
		b := buckets[m]
		vwap := b.close // fallback to close if volume is zero
		if b.volume.IsPositive() {
			vwap = b.quoteVolume.Div(b.volume)
		}
		candles = append(candles, Candle{
			T:        b.ts,
			Open:     b.open.String(),
			High:     b.high.String(),
			Low:      b.low.String(),
			Close:    b.close.String(),
			Volume:   b.volume.String(),
			VWAP:     vwap.String(),
			Source:   "kraken",
			Interval: 60,
		})
	}
	return candles
}

// ensureFuturesSymbols loads the Kraken Futures instrument list once per
// MarketSyncer lifetime. On failure it logs a warning and leaves the map
// empty, in which case all pairs fall through to the spot fetcher (preserving
// pre-existing behavior).
func (m *MarketSyncer) ensureFuturesSymbols(ctx context.Context) {
	if m.futuresLoaded {
		return
	}
	m.futuresLoaded = true

	const url = "https://futures.kraken.com/derivatives/api/v3/instruments"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		m.log.Warn("Build futures instruments request failed", zap.Error(err))
		return
	}
	req.Header.Set("User-Agent", "agent-intel/1.0")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.log.Warn("Fetch Kraken Futures instruments failed", zap.Error(err))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		m.log.Warn("Read futures instruments body failed", zap.Error(err))
		return
	}

	var parsed struct {
		Result      string `json:"result"`
		Instruments []struct {
			Symbol    string `json:"symbol"`
			Type      string `json:"type"`
			Tradeable bool   `json:"tradeable"`
		} `json:"instruments"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		m.log.Warn("Parse futures instruments failed", zap.Error(err))
		return
	}

	symbols := make(map[string]string, len(parsed.Instruments))
	for _, inst := range parsed.Instruments {
		if !inst.Tradeable {
			continue
		}
		// Only perpetual-like instruments: PI_* (inverse) and PF_* (flexible).
		// Skip dated futures (FI_*) - those have expiries and are less relevant.
		if inst.Type != "futures_inverse" && inst.Type != "flexible_futures" {
			continue
		}
		canonical := CanonicalPair(inst.Symbol)
		if canonical == "" {
			continue
		}
		// First writer wins if two instruments canonicalize to the same key.
		if _, exists := symbols[canonical]; !exists {
			symbols[canonical] = inst.Symbol
		}
	}
	m.futuresSymbols = symbols
	m.log.Info("Loaded Kraken Futures instruments",
		zap.Int("total", len(parsed.Instruments)),
		zap.Int("tradeable_perps", len(symbols)),
	)
}

// krakenFuturesCandlesResponse is the Kraken Futures /charts/v1 trade response.
type krakenFuturesCandlesResponse struct {
	Candles []struct {
		Time   int64  `json:"time"` // milliseconds
		Open   string `json:"open"`
		High   string `json:"high"`
		Low    string `json:"low"`
		Close  string `json:"close"`
		Volume string `json:"volume"`
	} `json:"candles"`
}

// fetchKrakenFuturesCandles downloads 1-minute OHLC candles from the Kraken
// Futures public charts endpoint. `from` and `to` are unix seconds; the
// endpoint returns pre-aggregated candles with no pagination cursor.
func (m *MarketSyncer) fetchKrakenFuturesCandles(
	ctx context.Context, canonicalPair, exchSymbol string, sinceTS int64,
) ([]Candle, error) {
	now := time.Now().Unix()
	if sinceTS >= now {
		return nil, nil
	}
	url := fmt.Sprintf(
		"https://futures.kraken.com/api/charts/v1/trade/%s/1m?from=%d&to=%d",
		exchSymbol, sinceTS, now,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "agent-intel/1.0")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch futures candles: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kraken futures http %d: %s", resp.StatusCode, string(body))
	}

	var parsed krakenFuturesCandlesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	candles := make([]Candle, 0, len(parsed.Candles))
	for _, c := range parsed.Candles {
		ts := c.Time / 1000 // ms -> seconds
		if ts <= sinceTS {
			continue // skip boundary candle that sinceTS anchors to
		}
		// Futures charts don't return VWAP; fall back to close price so
		// getPrice() (which prefers VWAP) still has a sensible value.
		candles = append(candles, Candle{
			T:        ts,
			Open:     c.Open,
			High:     c.High,
			Low:      c.Low,
			Close:    c.Close,
			Volume:   c.Volume,
			VWAP:     c.Close,
			Source:   "kraken-futures",
			Interval: 60,
		})
	}
	return candles, nil
}

// pairToKraken converts canonical pair to Kraken API format.
func pairToKraken(pair string) string {
	// Kraken uses specific naming for some pairs.
	krakenMap := map[string]string{
		"XBTUSD":       "XBTUSD",
		"ETHUSD":       "ETHUSD",
		"SOLUSD":       "SOLUSD",
		"AVAXUSD":      "AVAXUSD",
		"LINKUSD":      "LINKUSD",
		"DOTUSD":       "DOTUSD",
		"ZECUSD":       "ZECUSD",
		"XRPUSD":       "XRPUSD",
		"ALGOUSD":      "ALGOUSD",
		"NEARUSD":      "NEARUSD",
		"SUIUSD":       "SUIUSD",
		"TAOUSD":       "TAOUSD",
		"DAIUSD":       "DAIUSD",
		"USDTUSD":      "USDTUSD",
		"USDCUSD":      "USDCUSD",
		"BNBUSD":       "BNBUSD",
		"MONUSD":       "MONUSD",
		"PEPEUSD":      "PEPEUSD",
		"FARTCOINUSD":  "FARTCOINUSD",
		"HYPEUSD":      "HYPEUSD",
		"RENDERUSD":    "RENDERUSD",
		"RIVERUSD":     "RIVERUSD",
		"ZEREBROUSD":   "ZEREBROUSD",
		"DOGEUSD":      "XDGUSD",
		"SHIBUSD":      "SHIBUSD",
	}
	if k, ok := krakenMap[pair]; ok {
		return k
	}
	return pair
}
