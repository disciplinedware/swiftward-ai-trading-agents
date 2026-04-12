package marketdata

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/observability"
	"ai-trading-agents/internal/marketdata"
	"ai-trading-agents/internal/marketdata/prism"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/platform"
)

type contextKey string

const agentIDContextKey contextKey = "agent_id"

// Service implements the Market Data MCP - read-only market data for trading agents.
type Service struct {
	svcCtx       *platform.ServiceContext
	log          *zap.Logger
	source       marketdata.DataSource
	cache        *Cache
	repo         db.Repository
	pollInterval time.Duration
	filesRoot    string           // workspace root for save_to_file support
	now          func() time.Time // injectable clock for deterministic tests
	prism        *prism.Client    // nil if PRISM disabled
}

// NewService creates the Market Data MCP service.
func NewService(svcCtx *platform.ServiceContext, source marketdata.DataSource, repo db.Repository) *Service {
	pollInterval := 10 * time.Second
	filesRoot := "/data/workspace"
	if cfg := svcCtx.Config(); cfg != nil {
		if d, err := time.ParseDuration(cfg.MarketData.AlertPollInterval); err == nil && d > 0 {
			pollInterval = d
		}
		if cfg.FilesMCP.RootDir != "" {
			filesRoot = cfg.FilesMCP.RootDir
		}
	}
	return &Service{
		svcCtx:       svcCtx,
		log:          svcCtx.Logger().Named("market_data_mcp"),
		source:       source,
		cache:        NewCache(),
		repo:         repo,
		pollInterval: pollInterval,
		filesRoot:    filesRoot,
		now:          time.Now,
	}
}

// SetPrism attaches an optional PRISM client for market intelligence tools.
// Must be called before Initialize. If nil, PRISM tools are not registered.
func (s *Service) SetPrism(c *prism.Client) {
	s.prism = c
}

func (s *Service) Initialize() error {
	mcpServer := mcp.NewServer("market-data-mcp", "1.0.0", s.tools(), s.handleTool)
	// Wrap handler to extract X-Agent-ID header into context.
	s.svcCtx.Router().Post("/mcp/market", func(w http.ResponseWriter, r *http.Request) {
		if agentID := r.Header.Get("X-Agent-ID"); agentID != "" {
			ctx := context.WithValue(r.Context(), agentIDContextKey, agentID)
			ctx = observability.WithLogger(ctx, s.log.With(zap.String("agent_id", agentID)))
			r = r.WithContext(ctx)
		}
		mcpServer.ServeHTTP(w, r)
	})
	s.log.Info("Market Data MCP registered at /mcp/market", zap.String("source", s.source.Name()))
	return nil
}

func (s *Service) Start() error {
	go s.runAlertPoller()
	<-s.svcCtx.Context().Done()
	return nil
}

func (s *Service) runAlertPoller() {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	ctx := s.svcCtx.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkDBAlerts(ctx)
		}
	}
}

// checkDBAlerts fetches active market alerts from DB, checks prices, and marks triggered ones.
func (s *Service) checkDBAlerts(ctx context.Context) {
	alerts, err := s.repo.GetActiveAlerts(ctx, "", "market")
	if err != nil {
		s.log.Warn("alert poller: get active alerts failed", zap.Error(err))
		return
	}
	if len(alerts) == 0 {
		return
	}

	// Collect unique markets.
	markets := make(map[string]struct{})
	for _, a := range alerts {
		if m, ok := a.Params["market"].(string); ok && m != "" {
			markets[m] = struct{}{}
		}
	}
	mktList := make([]string, 0, len(markets))
	for m := range markets {
		mktList = append(mktList, m)
	}

	tickers, err := s.source.GetTicker(ctx, mktList)
	if err != nil {
		s.log.Warn("alert poller: get ticker failed", zap.Error(err))
		return
	}
	priceMap := make(map[string]float64, len(tickers))
	for _, t := range tickers {
		if p, parseErr := strconv.ParseFloat(t.Last, 64); parseErr == nil {
			priceMap[t.Market] = p
		}
	}

	for _, a := range alerts {
		market, _ := a.Params["market"].(string)
		condition, _ := a.Params["condition"].(string)
		value, _ := a.Params["value"].(float64)
		refPrice, _ := a.Params["ref_price"].(float64)

		var fired bool
		var triggerPriceStr string

		if isComplexCondition(condition) {
			fired = s.checkComplexCondition(ctx, a.AlertID, market, condition, value)
			if fired {
				triggerPriceStr = "" // no price context for non-price conditions
			}
		} else {
			currentPrice, ok := priceMap[market]
			if !ok {
				continue
			}
			alert := &alertCondition{Condition: condition, Value: value, refPrice: refPrice}
			if !conditionMet(alert, currentPrice) {
				continue
			}
			fired = true
			triggerPriceStr = fmt.Sprintf("%.4f", currentPrice)
		}

		if !fired {
			continue
		}

		triggered, markErr := s.repo.MarkAlertTriggered(ctx, a.AlertID, triggerPriceStr)
		if markErr != nil {
			s.log.Warn("alert poller: mark triggered failed", zap.String("alert_id", a.AlertID), zap.Error(markErr))
			continue
		}
		if !triggered {
			continue // another instance already claimed it
		}
		s.log.Info("alert triggered",
			zap.String("alert_id", a.AlertID),
			zap.String("agent_id", a.AgentID),
			zap.String("market", market),
			zap.String("condition", condition),
			zap.Float64("value", value),
		)
	}
}

// checkComplexCondition checks volume_spike, funding_threshold, and oi_change_pct conditions.
func (s *Service) checkComplexCondition(ctx context.Context, alertID, market, condition string, value float64) bool {
	switch condition {
	case "volume_spike":
		// NOTE: volume_spike currently uses 24h price change % as a proxy for unusual
		// activity. Real volume-vs-average data is not available from ticker API.
		// The tool description documents this behavior.
		tickers, err := s.source.GetTicker(ctx, []string{market})
		if err != nil || len(tickers) == 0 {
			return false
		}
		changePct, parseErr := strconv.ParseFloat(tickers[0].Change24hPct, 64)
		if parseErr != nil {
			return false
		}
		return math.Abs(changePct) >= math.Abs(value)

	case "funding_threshold":
		funding, err := s.source.GetFundingRates(ctx, market, 1)
		if err != nil || funding == nil {
			return false
		}
		rate, parseErr := strconv.ParseFloat(funding.CurrentRate, 64)
		if parseErr != nil {
			return false
		}
		// funding_threshold: fire when |funding rate| >= value (e.g. value=0.001 = 0.1%)
		return math.Abs(rate) >= math.Abs(value)

	case "oi_change_pct":
		oi, err := s.source.GetOpenInterest(ctx, market)
		if err != nil || oi == nil {
			return false
		}
		// Use 1h OI change as the trigger metric.
		oiChg, parseErr := strconv.ParseFloat(oi.OIChange1hPct, 64)
		if parseErr != nil {
			return false
		}
		return math.Abs(oiChg) >= math.Abs(value)
	}
	return false
}

func (s *Service) Stop() error {
	s.log.Info("Market Data MCP stopped")
	return nil
}

func (s *Service) tools() []mcp.Tool {
	tools := []mcp.Tool{
		{
			Name: "market/get_prices",
			Description: "Get current price snapshots for one or more markets. " +
				"Returns: {prices: [{market, last, bid, ask, change_24h_pct, high_24h, low_24h, volume_24h}]}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"markets": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Canonical symbols, e.g. [\"ETH-USDC\", \"BTC-USDC\"]",
					},
				},
				"required": []string{"markets"},
			},
		},
		{
			Name: "market/get_candles",
			Description: "Get historical OHLCV candles with optional server-side indicators. Only closed candles are returned. " +
				"All values (prices and volume) are in quote currency. " +
				"Intervals: 1m, 5m, 15m, 1h, 4h, 1d. " +
				"Indicators: rsi_14, ema_<N> (e.g. ema_21), sma_<N>, macd, bbands, atr_<N>, vwap. " +
				"Indicator values are null for the warm-up candles at the start of the series. " +
				"With save_to_file=true: writes CSV to workspace and returns {saved_to, market, interval, rows, columns, updated_at} — " +
				"pass saved_to directly to pd.read_csv() in code/execute. Without save_to_file: returns inline JSON or CSV.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"market": map[string]any{
						"type":        "string",
						"description": "Canonical symbol, e.g. \"ETH-USDC\"",
					},
					"interval": map[string]any{
						"type":        "string",
						"enum":        []string{"1m", "5m", "15m", "1h", "4h", "1d"},
						"description": "Candle interval",
					},
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     720,
						"description": "Number of candles (required). Choose based on your analysis: 50-100 for screening, 200-500 for backtesting. Max 720.",
					},
					"end_time": map[string]any{
						"type":        "string",
						"description": "ISO-8601 timestamp — return candles closed before this time. Default: now.",
					},
					"format": map[string]any{
						"type":        "string",
						"enum":        []string{"json", "csv"},
						"description": "Response format (default json). Ignored when save_to_file=true.",
					},
					"save_to_file": map[string]any{
						"type":        "boolean",
						"description": "Write CSV to workspace at market/{market}_{interval}.csv and return path metadata. Use before code/execute to keep candle data out of the LLM context window.",
					},
					"indicators": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Indicators to compute and append: rsi_14, ema_21, sma_20, macd, bbands, atr_14, vwap (use any period, e.g. ema_9, atr_7).",
					},
				},
				"required": []string{"market", "interval", "limit"},
			},
		},
		{
			Name: "market/list_markets",
			Description: "List available trading pairs with prices. " +
				"Use quote filter (e.g. quote=USD) to get enriched price/volume/change data. " +
				"Without quote filter, prices may be empty when the exchange has too many pairs. " +
				"Returns: {markets: [{pair, base, quote, last_price, volume_24h, change_24h_pct, tradeable}], count, source}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"quote": map[string]any{
						"type":        "string",
						"description": "Filter by quote currency, e.g. \"USDC\". Default: all.",
					},
					"sort_by": map[string]any{
						"type":        "string",
						"enum":        []string{"volume", "name", "change"},
						"description": "Sort order (default: volume)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max results (default 50)",
					},
				},
			},
		},
		{
			Name: "market/get_orderbook",
			Description: "Get orderbook depth with computed aggregates. " +
				"Returns: {bids: [[price_str, size_str], ...], asks: [[price_str, size_str], ...], bid_total, ask_total, spread, spread_pct, imbalance, market, source, timestamp}. " +
				"bids/asks are arrays of [price, size] string pairs, sorted best-first. " +
				"imbalance is bid_total/(bid_total+ask_total): 0.5=balanced, >0.5=more buy pressure, <0.5=more sell pressure.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"market": map[string]any{
						"type":        "string",
						"description": "Canonical symbol, e.g. \"ETH-USDC\"",
					},
					"depth": map[string]any{
						"type":        "integer",
						"description": "Number of price levels per side (default 20)",
					},
				},
				"required": []string{"market"},
			},
		},
		{
			Name: "market/get_funding",
			Description: "Get perpetual futures funding rate for a market. " +
				"Funding rate is a periodic payment between long and short holders to keep perp price near spot. " +
				"Positive rate = longs pay shorts (bullish crowd), negative = shorts pay longs (bearish crowd). " +
				"Returns: {current_rate, annualized_pct, funding_interval_h, next_funding_time, signal, history: [{timestamp, rate}]}. " +
				"signal: \"neutral\", \"bullish_crowd\", \"bearish_crowd\", \"extreme_bullish\", \"extreme_bearish\". " +
				"funding_interval_h varies by pair (8h, 4h, or 1h). annualized_pct accounts for actual interval.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"market": map[string]any{
						"type":        "string",
						"description": "Canonical symbol, e.g. \"ETH-USDC\"",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Number of historical periods to return (default 10)",
					},
				},
				"required": []string{"market"},
			},
		},
		{
			Name: "market/get_open_interest",
			Description: "Get open interest with change deltas and long/short ratio. " +
				"Returns: {open_interest, oi_change_1h_pct, oi_change_4h_pct, oi_change_24h_pct, long_short_ratio}. " +
				"Interpretation: rising OI + rising price = trend confirmed; rising OI + falling price = potential squeeze; falling OI = trend weakening.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"market": map[string]any{
						"type":        "string",
						"description": "Canonical symbol, e.g. \"ETH-USDC\"",
					},
				},
				"required": []string{"market"},
			},
		},
		{
			Name: "market/set_alert",
			Description: "Create a market alert polled every 10s. Returns: {alert_id, status:\"active\"}. " +
				"Conditions:\n" +
				"- \"above\" / \"below\": fires when price crosses the threshold (value = price level)\n" +
				"- \"change_pct\": fires when price moves X% from the reference price at creation time (value = % magnitude)\n" +
				"- \"volume_spike\": fires when 24h price change % magnitude exceeds value (proxy for unusual activity)\n" +
				"- \"funding_threshold\": fires when |funding rate| >= value (value = rate, e.g. 0.001 = 0.1%)\n" +
				"- \"oi_change_pct\": fires when 1h open interest change exceeds value (value = % magnitude)\n" +
				"Check list_alerts for triggered alerts.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"market": map[string]any{
						"type":        "string",
						"description": "Canonical symbol, e.g. \"ETH-USDC\"",
					},
					"condition": map[string]any{
						"type":        "string",
						"enum":        []string{"above", "below", "change_pct", "volume_spike", "funding_threshold", "oi_change_pct"},
						"description": "Trigger condition type",
					},
					"value": map[string]any{
						"type":        "number",
						"description": "Threshold value: price level (above/below), % magnitude (change_pct, volume_spike, oi_change_pct), or rate (funding_threshold)",
					},
					"window": map[string]any{
						"type":        "string",
						"description": "Advisory label stored with the alert (e.g. 5m, 15m, 1h). Default: 5m.",
					},
					"note": map[string]any{
						"type":        "string",
						"description": "Optional reminder text injected when alert fires",
					},
				},
				"required": []string{"market", "condition", "value"},
			},
		},
		{
			Name:        "market/cancel_alert",
			Description: "Cancel an active price alert. Returns: {success: true}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"alert_id": map[string]any{
						"type":        "string",
						"description": "Alert ID returned by set_alert",
					},
				},
				"required": []string{"alert_id"},
			},
		},
	}

	// PRISM market intelligence tools (only if client is configured).
	if s.prism != nil {
		tools = append(tools,
			mcp.Tool{
				Name: "market/get_fear_greed",
				Description: "Get the current Crypto Fear & Greed Index (0-100). " +
					"0-25: Extreme Fear, 25-45: Fear, 45-55: Neutral, 55-75: Greed, 75-100: Extreme Greed. " +
					"Returns: {value, label, source}. Source: PRISM API.",
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			mcp.Tool{
				Name: "market/get_technicals",
				Description: "Get pre-computed technical indicators from PRISM (independent second opinion from a different data source). " +
					"Returns: RSI, MACD (value/signal/histogram/trend), SMA (20/50/200), EMA (12/26/50), Bollinger Bands, " +
					"Stochastic, ATR, ADX, Williams %R, CCI, overall_signal (bullish/bearish/neutral), current_price. " +
					"Source: PRISM API (Coinbase+OKX data, daily timeframe).",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"market": map[string]any{
							"type":        "string",
							"description": "Canonical symbol, e.g. \"ETH-USDC\" (base asset is extracted automatically)",
						},
					},
					"required": []string{"market"},
				},
			},
			mcp.Tool{
				Name: "market/get_signals",
				Description: "Get AI signal summary from PRISM for one or more assets. " +
					"Returns per asset: overall_signal, direction, strength, bullish/bearish scores, active signals with types. " +
					"Source: PRISM API.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"markets": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Canonical symbols, e.g. [\"ETH-USDC\", \"BTC-USDC\"]",
						},
					},
					"required": []string{"markets"},
				},
			},
		)
	}

	return tools
}

func (s *Service) handleTool(ctx context.Context, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	switch toolName {
	case "market/get_prices":
		return s.toolGetPrices(ctx, args)
	case "market/get_candles":
		return s.toolGetCandles(ctx, args)
	case "market/list_markets":
		return s.toolListMarkets(ctx, args)
	case "market/get_orderbook":
		return s.toolGetOrderbook(ctx, args)
	case "market/get_funding":
		return s.toolGetFunding(ctx, args)
	case "market/get_open_interest":
		return s.toolGetOpenInterest(ctx, args)
	case "market/set_alert":
		return s.toolSetAlert(ctx, args)
	case "market/cancel_alert":
		return s.toolCancelAlert(ctx, args)
	case "market/get_fear_greed":
		return s.toolGetFearGreed(ctx, args)
	case "market/get_technicals":
		return s.toolGetTechnicals(ctx, args)
	case "market/get_signals":
		return s.toolGetSignals(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (s *Service) toolGetPrices(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	marketsRaw, ok := args["markets"].([]any)
	if !ok || len(marketsRaw) == 0 {
		return nil, fmt.Errorf("markets is required and must be a non-empty array")
	}

	markets := make([]string, len(marketsRaw))
	for i, m := range marketsRaw {
		ms, ok := m.(string)
		if !ok {
			return nil, fmt.Errorf("markets[%d] must be a string", i)
		}
		markets[i] = ms
	}

	tickers, err := s.source.GetTicker(ctx, markets)
	if err != nil {
		return nil, fmt.Errorf("get prices: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"prices": tickers,
		"source": s.source.Name(),
	})
}

func (s *Service) toolGetCandles(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	market, _ := args["market"].(string)
	intervalStr, _ := args["interval"].(string)
	if market == "" || intervalStr == "" {
		return nil, fmt.Errorf("market and interval are required")
	}

	interval, err := marketdata.ParseInterval(intervalStr)
	if err != nil {
		return nil, err
	}

	limitF, ok := args["limit"].(float64)
	if !ok || limitF < 1 {
		return nil, fmt.Errorf("limit is required (1-720)")
	}
	limit := int(limitF)
	if limit > 720 {
		limit = 720
	}

	endTime, endTimeErr := parseOptionalTime(args, "end_time")
	if endTimeErr != nil {
		return nil, fmt.Errorf("invalid end_time: %w", endTimeErr)
	}
	saveToFile, _ := args["save_to_file"].(bool)
	format, _ := args["format"].(string)
	if format == "" {
		format = "json"
	}

	// Parse optional indicators
	var specs []IndicatorSpec
	var indicatorCols []string
	if indsRaw, ok := args["indicators"].([]any); ok && len(indsRaw) > 0 {
		rawStrs := make([]string, 0, len(indsRaw))
		for i, v := range indsRaw {
			str, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("invalid indicator at index %d: expected string, got %T", i, v)
			}
			rawStrs = append(rawStrs, str)
		}
		if len(rawStrs) > 0 {
			specs, err = ParseIndicatorSpecs(rawStrs)
			if err != nil {
				return nil, fmt.Errorf("parse indicators: %w", err)
			}
			indicatorCols = AllOutputColumns(specs)
		}
	}

	// Fetch extra candles for indicator warm-up.
	// Cap at 1000 so we don't request more than sources can return.
	fetchLimit := limit
	if len(specs) > 0 {
		fetchLimit = limit + WarmupNeeded(specs)
		if fetchLimit > 1000 {
			fetchLimit = 1000
		}
	}

	// Check cache first. Read candles and priorLimit atomically to avoid a race
	// where a concurrent PutCandles updates requestedLimits between two separate reads.
	cacheKey := market + ":" + string(interval)
	candles, cacheHit, priorLimit := s.cache.GetCandlesWithRequestedLimit(cacheKey, fetchLimit, endTime, interval.Duration())
	// When the cache returns fewer candles than requested, decide whether to re-fetch.
	if cacheHit && len(candles) < fetchLimit {
		if endTime.IsZero() {
			// Current data: apply exhausted-source heuristic. If the cache was already
			// populated with a limit >= fetchLimit but the source returned fewer candles,
			// the source is exhausted and re-fetching would loop forever without gaining anything.
			if priorLimit < fetchLimit {
				cacheHit = false
			}
		} else {
			// Historical requests use a specific endTime, so the cache (keyed only by
			// market:interval) may cover a completely different time window. The
			// exhausted-source heuristic based on requestedLimit does not apply across
			// different time ranges, so always re-fetch from the source.
			cacheHit = false
		}
	}
	// For live (no endTime) requests: if a new candle has closed since the cache was
	// last populated, re-fetch so polling agents get current data.
	if cacheHit && endTime.IsZero() && len(candles) > 0 {
		newestClose := candles[len(candles)-1].Timestamp.Add(interval.Duration())
		if newestClose.Before(s.now()) {
			cacheHit = false
		}
	}
	if !cacheHit {
		candles, err = s.source.GetCandles(ctx, market, interval, fetchLimit, endTime)
		if err != nil {
			return nil, fmt.Errorf("get candles: %w", err)
		}
		// Only cache live (no-endTime) fetches. Storing historical candles in the
		// shared live cache would allow them to satisfy a subsequent live request
		// when len(historical_candles) == limit, bypassing the re-fetch entirely
		// and returning stale data.
		if endTime.IsZero() {
			s.cache.PutCandles(cacheKey, candles, fetchLimit)
		}
	}

	// With indicators: compute then trim warm-up candles
	if len(specs) > 0 {
		engine := &IndicatorEngine{}
		candlesWithInds, compErr := engine.Compute(candles, specs)
		if compErr != nil {
			return nil, fmt.Errorf("compute indicators: %w", compErr)
		}
		// Trim to last `limit` candles (warm-up candles are at the start)
		if len(candlesWithInds) > limit {
			candlesWithInds = candlesWithInds[len(candlesWithInds)-limit:]
		}

		if saveToFile {
			csvData := FormatCandlesWithIndicatorsCSV(candlesWithInds, indicatorCols)
			savedTo, writeErr := s.saveCSVToWorkspace(ctx, market, intervalStr, csvData)
			if writeErr != nil {
				return nil, fmt.Errorf("save to file: %w", writeErr)
			}
			cols := append([]string{"timestamp", "open", "high", "low", "close", "volume"}, indicatorCols...)
			return mcp.JSONResult(map[string]any{
				"saved_to":   savedTo,
				"market":     market,
				"interval":   intervalStr,
				"rows":       len(candlesWithInds),
				"columns":    cols,
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			})
		}
		if format == "csv" {
			csvData := FormatCandlesWithIndicatorsCSV(candlesWithInds, indicatorCols)
			return mcp.JSONResult(map[string]any{
				"market":              market,
				"interval":            intervalStr,
				"count":               len(candlesWithInds),
				"format":              "csv",
				"source":              s.source.Name(),
				"indicators_computed": indicatorCols,
				"data":                csvData,
			})
		}

		return mcp.JSONResult(map[string]any{
			"market":              market,
			"interval":            intervalStr,
			"count":               len(candlesWithInds),
			"source":              s.source.Name(),
			"indicators_computed": indicatorCols,
			"candles":             candlesWithInds,
		})
	}

	if saveToFile {
		csvData := FormatCandlesCSV(candles)
		savedTo, writeErr := s.saveCSVToWorkspace(ctx, market, intervalStr, csvData)
		if writeErr != nil {
			return nil, fmt.Errorf("save to file: %w", writeErr)
		}
		return mcp.JSONResult(map[string]any{
			"saved_to":   savedTo,
			"market":     market,
			"interval":   intervalStr,
			"rows":       len(candles),
			"columns":    []string{"timestamp", "open", "high", "low", "close", "volume"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
	if format == "csv" {
		csvData := FormatCandlesCSV(candles)
		return mcp.JSONResult(map[string]any{
			"market":   market,
			"interval": intervalStr,
			"count":    len(candles),
			"format":   "csv",
			"source":   s.source.Name(),
			"data":     csvData,
		})
	}

	return mcp.JSONResult(map[string]any{
		"market":   market,
		"interval": intervalStr,
		"count":    len(candles),
		"source":   s.source.Name(),
		"candles":  candles,
	})
}

func validateAgentID(agentID string) error {
	if agentID == "" {
		return fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}
	if agentID == "." {
		return fmt.Errorf("invalid agent_id: must not be '.'")
	}
	if strings.ContainsAny(agentID, "/\\") || strings.Contains(agentID, "..") {
		return fmt.Errorf("invalid agent_id: must not contain path separators or '..'")
	}
	return nil
}

// saveCSVToWorkspace writes CSV data to {filesRoot}/{agentID}/market/{market}_{interval}.csv.
// Returns the sandbox-visible path (/workspace/market/...) on success.
func (s *Service) saveCSVToWorkspace(ctx context.Context, market, interval, csvData string) (string, error) {
	agentID := s.agentIDFromCtx(ctx)
	if agentID == "" {
		return "", fmt.Errorf("X-Agent-ID header required for save_to_file")
	}
	if err := validateAgentID(agentID); err != nil {
		return "", err
	}
	// Sanitize market for filename: replace any path separators (ETH-USDC -> ETH-USDC, safe as-is)
	filename := strings.ReplaceAll(strings.ReplaceAll(market, "/", "-"), "..", "") + "_" + interval + ".csv"
	dir := filepath.Join(s.filesRoot, agentID, "market")
	// Verify dir is still inside filesRoot after Join (defence against symlink attacks)
	if !strings.HasPrefix(dir, filepath.Clean(s.filesRoot)+string(filepath.Separator)) {
		return "", fmt.Errorf("resolved path escapes workspace root")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create market dir: %w", err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(csvData), 0o644); err != nil {
		return "", fmt.Errorf("write csv: %w", err)
	}
	s.log.Info(fmt.Sprintf("saved %d bytes to %s", len(csvData), path))
	return "/workspace/market/" + filename, nil
}

func (s *Service) toolListMarkets(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	quote, _ := args["quote"].(string)
	sortBy, _ := args["sort_by"].(string)
	if sortBy == "" {
		sortBy = "volume"
	}
	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	markets, err := s.source.GetMarkets(ctx, quote)
	if err != nil {
		return nil, fmt.Errorf("list markets: %w", err)
	}

	// Sort
	switch sortBy {
	case "name":
		sort.Slice(markets, func(i, j int) bool {
			return markets[i].Pair < markets[j].Pair
		})
	case "change":
		sort.Slice(markets, func(i, j int) bool {
			ci, _ := strconv.ParseFloat(markets[i].Change24hPct, 64)
			cj, _ := strconv.ParseFloat(markets[j].Change24hPct, 64)
			return ci > cj // descending
		})
	default: // volume
		sort.Slice(markets, func(i, j int) bool {
			vi, _ := strconv.ParseFloat(markets[i].Volume24h, 64)
			vj, _ := strconv.ParseFloat(markets[j].Volume24h, 64)
			return vi > vj // descending
		})
	}

	if len(markets) > limit {
		markets = markets[:limit]
	}

	return mcp.JSONResult(map[string]any{
		"markets": markets,
		"count":   len(markets),
		"source":  s.source.Name(),
	})
}

func (s *Service) toolGetOrderbook(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	market, _ := args["market"].(string)
	if market == "" {
		return nil, fmt.Errorf("market is required")
	}

	depth := 20
	if d, ok := args["depth"].(float64); ok && d > 0 {
		depth = int(d)
	}

	ob, err := s.source.GetOrderbook(ctx, market, depth)
	if err != nil {
		return nil, fmt.Errorf("get orderbook: %w", err)
	}

	// Convert levels and compute totals
	bidsArr := make([][]string, len(ob.Bids))
	bidTotal := 0.0
	for i, b := range ob.Bids {
		bidsArr[i] = []string{b.Price, b.Size}
		if sz, parseErr := strconv.ParseFloat(b.Size, 64); parseErr == nil {
			bidTotal += sz
		}
	}

	asksArr := make([][]string, len(ob.Asks))
	askTotal := 0.0
	for i, a := range ob.Asks {
		asksArr[i] = []string{a.Price, a.Size}
		if sz, parseErr := strconv.ParseFloat(a.Size, 64); parseErr == nil {
			askTotal += sz
		}
	}

	// Compute spread and imbalance
	spread := 0.0
	spreadPct := 0.0
	imbalance := 0.5
	if len(ob.Bids) > 0 && len(ob.Asks) > 0 {
		bestBid, _ := strconv.ParseFloat(ob.Bids[0].Price, 64)
		bestAsk, _ := strconv.ParseFloat(ob.Asks[0].Price, 64)
		spread = bestAsk - bestBid
		mid := (bestBid + bestAsk) / 2
		if mid > 0 {
			spreadPct = spread / mid * 100
		}
	}
	if bidTotal+askTotal > 0 {
		imbalance = bidTotal / (bidTotal + askTotal)
	}

	return mcp.JSONResult(map[string]any{
		"market":     ob.Market,
		"bids":       bidsArr,
		"asks":       asksArr,
		"bid_total":  fmt.Sprintf("%.4f", bidTotal),
		"ask_total":  fmt.Sprintf("%.4f", askTotal),
		"spread":     fmt.Sprintf("%.4f", spread),
		"spread_pct": fmt.Sprintf("%.4f", spreadPct),
		"imbalance":  fmt.Sprintf("%.4f", imbalance),
		"source":     s.source.Name(),
		"timestamp":  ob.Timestamp,
	})
}

func (s *Service) toolGetFunding(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	market, _ := args["market"].(string)
	if market == "" {
		return nil, fmt.Errorf("market is required")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	fd, err := s.source.GetFundingRates(ctx, market, limit)
	if err != nil {
		s.log.Debug("GetFundingRates unavailable", zap.String("market", market), zap.Error(err))
		return mcp.JSONResult(map[string]any{
			"market":  market,
			"error":   "funding data not available from this source",
			"source":  s.source.Name(),
		})
	}

	return mcp.JSONResult(map[string]any{
		"market":             fd.Market,
		"current_rate":       fd.CurrentRate,
		"annualized_pct":     fd.AnnualizedPct,
		"funding_interval_h": fd.FundingIntervalH,
		"next_funding_time":  fd.NextFundingTime,
		"history":            fd.History,
		"signal":             classifyFundingSignal(fd.CurrentRate),
		"source":             s.source.Name(),
	})
}

// classifyFundingSignal returns a human-readable signal label for the current funding rate.
// Thresholds: |rate| < 0.0001 = neutral, > 0.001 = extreme.
func classifyFundingSignal(rateStr string) string {
	rate, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		return "neutral"
	}
	absRate := math.Abs(rate)
	switch {
	case absRate > 0.001:
		if rate > 0 {
			return "extreme_bullish"
		}
		return "extreme_bearish"
	case rate > 0.0001:
		return "bullish_crowd"
	case rate < -0.0001:
		return "bearish_crowd"
	default:
		return "neutral"
	}
}

func (s *Service) toolGetOpenInterest(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	market, _ := args["market"].(string)
	if market == "" {
		return nil, fmt.Errorf("market is required")
	}

	oi, err := s.source.GetOpenInterest(ctx, market)
	if err != nil {
		s.log.Debug("GetOpenInterest unavailable", zap.String("market", market), zap.Error(err))
		return mcp.JSONResult(map[string]any{
			"market": market,
			"error":  "open interest data not available from this source",
			"source": s.source.Name(),
		})
	}

	return mcp.JSONResult(map[string]any{
		"market":             oi.Market,
		"open_interest":      oi.OpenInterest,
		"oi_change_1h_pct":  oi.OIChange1hPct,
		"oi_change_4h_pct":  oi.OIChange4hPct,
		"oi_change_24h_pct": oi.OIChange24hPct,
		"long_short_ratio":  oi.LongShortRatio,
		"source":            s.source.Name(),
	})
}

func (s *Service) agentIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(agentIDContextKey).(string)
	return id
}

func (s *Service) toolSetAlert(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	agentID := s.agentIDFromCtx(ctx)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	market, _ := args["market"].(string)
	condition, _ := args["condition"].(string)
	if market == "" || condition == "" {
		return nil, fmt.Errorf("market and condition are required")
	}

	rawValue, ok := args["value"].(float64)
	if !ok {
		return nil, fmt.Errorf("value is required and must be a number")
	}

	window, _ := args["window"].(string)
	if window == "" {
		window = "5m"
	}
	note, _ := args["note"].(string)

	// Fetch current price as reference for change_pct condition
	refPrice := 0.0
	if condition == "change_pct" {
		tickers, fetchErr := s.source.GetTicker(ctx, []string{market})
		if fetchErr != nil {
			return nil, fmt.Errorf("set alert: could not fetch reference price for %s: %w", market, fetchErr)
		}
		if len(tickers) == 0 {
			return nil, fmt.Errorf("set alert: no price data for %s", market)
		}
		p, parseErr := strconv.ParseFloat(tickers[0].Last, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("set alert: invalid price %q for %s: %w", tickers[0].Last, market, parseErr)
		}
		refPrice = p
	}

	alertID := makeAlertID(agentID, market, condition, rawValue, window)

	if err := validateAlertCondition(condition); err != nil {
		return nil, err
	}
	count, countErr := s.repo.CountActiveAlerts(ctx, agentID, "market")
	if countErr != nil {
		return nil, fmt.Errorf("set alert: count check: %w", countErr)
	}
	if count >= 10 {
		return nil, fmt.Errorf("set alert: limit reached (%d active market alerts, max 10)", count)
	}
	record := &db.AlertRecord{
		AlertID:     alertID,
		AgentID:     agentID,
		Service:     "market",
		Status:      "active",
		OnTrigger:   "wake_full",
		MaxTriggers: 1,
		Params: map[string]any{
			"market":    market,
			"condition": condition,
			"value":     rawValue,
			"window":    window,
			"ref_price": refPrice,
		},
		Note: note,
	}
	if upsertErr := s.repo.UpsertAlert(ctx, record); upsertErr != nil {
		return nil, fmt.Errorf("set alert: %w", upsertErr)
	}

	return mcp.JSONResult(map[string]any{
		"alert_id": alertID,
		"status":   "active",
	})
}

func (s *Service) toolCancelAlert(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	agentID := s.agentIDFromCtx(ctx)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	alertID, _ := args["alert_id"].(string)
	if alertID == "" {
		return nil, fmt.Errorf("alert_id is required")
	}

	if err := s.repo.CancelAlert(ctx, agentID, alertID); err != nil {
		return nil, fmt.Errorf("cancel alert: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"success": true,
	})
}

// parseOptionalTime parses an RFC3339 timestamp from args[key].
// Returns (zero, nil) if the key is absent or empty.
// Returns (zero, error) if the key is present but not a valid RFC3339 string.
func parseOptionalTime(args map[string]any, key string) (time.Time, error) {
	s, ok := args[key].(string)
	if !ok || s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339 (e.g. 2006-01-02T15:04:05Z), got %q", key, s)
	}
	return t, nil
}

// --- PRISM market intelligence tools ---

func (s *Service) prismUnavailableResult(err error) (*mcp.ToolResult, error) {
	result := map[string]any{
		"error":  "PRISM market intelligence temporarily unavailable",
		"source": "prism",
	}
	if ue, ok := err.(*prism.UnavailableError); ok {
		result["status"] = "circuit_open"
		result["retry_after_seconds"] = ue.RetryAfterSecs
	}
	return mcp.JSONResult(result)
}

func (s *Service) toolGetFearGreed(ctx context.Context, _ map[string]any) (*mcp.ToolResult, error) {
	if s.prism == nil {
		return nil, fmt.Errorf("PRISM is not configured")
	}
	resp, err := s.prism.GetFearGreed(ctx)
	if err != nil {
		return s.prismUnavailableResult(err)
	}
	return mcp.JSONResult(map[string]any{
		"value":  resp.Value,
		"label":  resp.Label,
		"source": "prism",
	})
}

func (s *Service) toolGetTechnicals(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	if s.prism == nil {
		return nil, fmt.Errorf("PRISM is not configured")
	}
	market, ok := args["market"].(string)
	if !ok || market == "" {
		return nil, fmt.Errorf("market is required")
	}
	symbol := prism.CanonicalToBase(market)

	resp, err := s.prism.GetTechnicals(ctx, symbol)
	if err != nil {
		return s.prismUnavailableResult(err)
	}
	return mcp.JSONResult(map[string]any{
		"symbol":          resp.Symbol,
		"timeframe":       resp.Timeframe,
		"rsi":             resp.RSI,
		"rsi_signal":      resp.RSISignal,
		"macd":            resp.MACD,
		"macd_signal":     resp.MACDSignal,
		"macd_histogram":  resp.MACDHistogram,
		"macd_trend":      resp.MACDTrend,
		"sma_20":          resp.SMA20,
		"sma_50":          resp.SMA50,
		"sma_200":         resp.SMA200,
		"ema_12":          resp.EMA12,
		"ema_26":          resp.EMA26,
		"ema_50":          resp.EMA50,
		"bb_upper":        resp.BBUpper,
		"bb_middle":       resp.BBMiddle,
		"bb_lower":        resp.BBLower,
		"bb_width":        resp.BBWidth,
		"stoch_k":         resp.StochK,
		"stoch_d":         resp.StochD,
		"stoch_signal":    resp.StochSignal,
		"atr":             resp.ATR,
		"atr_percent":     resp.ATRPercent,
		"adx":             resp.ADX,
		"plus_di":         resp.PlusDI,
		"minus_di":        resp.MinusDI,
		"adx_trend":       resp.ADXTrend,
		"williams_r":      resp.WilliamsR,
		"cci":             resp.CCI,
		"current_price":   resp.CurrentPrice,
		"price_change_24h": resp.PriceChange24h,
		"overall_signal":  resp.OverallSignal,
		"bullish_signals": resp.BullishSignals,
		"bearish_signals": resp.BearishSignals,
		"source":          "prism",
	})
}

func (s *Service) toolGetSignals(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	if s.prism == nil {
		return nil, fmt.Errorf("PRISM is not configured")
	}
	marketsRaw, ok := args["markets"].([]any)
	if !ok || len(marketsRaw) == 0 {
		return nil, fmt.Errorf("markets is required and must be a non-empty array")
	}
	symbols := make([]string, len(marketsRaw))
	for i, m := range marketsRaw {
		ms, ok := m.(string)
		if !ok {
			return nil, fmt.Errorf("markets[%d] must be a string", i)
		}
		symbols[i] = prism.CanonicalToBase(ms)
	}

	resp, err := s.prism.GetSignalsSummary(ctx, symbols)
	if err != nil {
		return s.prismUnavailableResult(err)
	}

	signals := make([]map[string]any, len(resp.Data))
	for i, entry := range resp.Data {
		signals[i] = map[string]any{
			"symbol":         entry.Symbol,
			"overall_signal": entry.OverallSignal,
			"direction":      entry.Direction,
			"strength":       entry.Strength,
			"bullish_score":  entry.BullishScore,
			"bearish_score":  entry.BearishScore,
			"net_score":      entry.NetScore,
			"current_price":  entry.CurrentPrice,
			"active_signals": entry.ActiveSignals,
			"signal_count":   entry.SignalCount,
		}
	}
	return mcp.JSONResult(map[string]any{
		"signals": signals,
		"summary": map[string]any{
			"total":          resp.Summary.Total,
			"strong_bullish": resp.Summary.StrongBullish,
			"bullish":        resp.Summary.Bullish,
			"neutral":        resp.Summary.Neutral,
			"bearish":        resp.Summary.Bearish,
			"strong_bearish": resp.Summary.StrongBearish,
		},
		"source": "prism",
	})
}
