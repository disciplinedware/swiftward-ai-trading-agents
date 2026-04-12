package random

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/platform"
)

// Trading behavior modes simulate a retail day-trader:
//   - scalp:      ~40% - quick small trades, bread and butter
//   - swing:      ~25% - medium conviction, hold for a while
//   - fomo:       ~12% - "it's pumping!" oversized buy
//   - take_profit: ~8% - sell after a good run
//   - yolo:        ~5% - meme coin gamble (unapproved pairs - rejected by policy)
//   - dca:         ~7% - dollar-cost averaging into favorite asset (builds concentration)
//   - panic_sell:  ~3% - dump everything in one market
const (
	modeScalp       = "scalp"
	modeSwing       = "swing"
	modeFOMO        = "fomo"
	modeTakeProfit  = "take_profit"
	modeYOLO        = "yolo"
	modeDCA         = "dca"
	modePanicSell   = "panic_sell"
)

// memeCoins are NOT in the approved list - always rejected by pair_whitelist rule.
var memeCoins = []string{"DOGE-USD", "SHIB-USD", "PEPE-USD"}

// Service implements the random trading agent.
// Simulates a retail day-trader with mood swings, favorite markets,
// and occasional bad decisions - all constrained by Swiftward policy.
type Service struct {
	svcCtx *platform.ServiceContext
	log    *zap.Logger
	cfg    config.RandomAgentConfig
	client *mcp.Client
	cancel context.CancelFunc

	// Trader personality (set once at startup, stable across session)
	favoriteMarket string   // DCA target, most traded
	markets        []string // approved markets from config

	// Mood shifts every ~20 ticks. Bullish = more buys, bearish = more sells.
	bullish   bool
	moodTicks int

	// cooldownUntil: when set, agent skips ticks until this time.
	cooldownUntil time.Time
}

// NewService creates a new random trading agent.
func NewService(svcCtx *platform.ServiceContext) *Service {
	cfg := svcCtx.Config().RandomAgent
	log := svcCtx.Logger().Named("random_agent")

	client := mcp.NewClient(cfg.MCPURL, cfg.APIKey, 5*time.Minute)
	client.SetHeader("X-Agent-ID", cfg.AgentID)

	markets := cfg.Markets
	if len(markets) == 0 {
		markets = []string{"ETH-USDC", "BTC-USDC"}
	}

	return &Service{
		svcCtx:         svcCtx,
		log:            log,
		cfg:            cfg,
		client:         client,
		markets:        markets,
		favoriteMarket: markets[0],
		bullish:        rand.Float64() > 0.5,
		moodTicks:      15 + rand.Intn(10), // first mood shift in 15-25 ticks
	}
}

func (s *Service) Initialize() error {
	s.log.Info("Random agent initialized",
		zap.String("agent_id", s.cfg.AgentID),
		zap.String("mcp_url", s.cfg.MCPURL),
		zap.String("interval", s.cfg.Interval),
		zap.Strings("markets", s.cfg.Markets),
		zap.Float64("max_size", s.cfg.MaxSize),
		zap.String("favorite", s.favoriteMarket),
		zap.Bool("bullish", s.bullish),
	)
	return nil
}

func (s *Service) Start() error {
	ctx, cancel := context.WithCancel(s.svcCtx.Context())
	s.cancel = cancel

	interval, err := time.ParseDuration(s.cfg.Interval)
	if err != nil {
		interval = 10 * time.Second
	}

	// Wait a moment for HTTP server to start before first tick
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return nil
	}

	// Initialize MCP connection
	initResult, err := s.client.Initialize()
	if err != nil {
		s.log.Warn("MCP initialize failed (will retry on first trade)", zap.Error(err))
	} else {
		s.log.Info("MCP connection initialized", zap.String("server", initResult.ServerInfo.Name))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.log.Info("Random agent started", zap.Duration("interval", interval))

	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *Service) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.log.Info("Random agent stopped")
	return nil
}

// pickMode simulates trader psychology with weighted modes.
func pickMode() string {
	r := rand.Intn(100)
	switch {
	case r < 40:
		return modeScalp
	case r < 65:
		return modeSwing
	case r < 77:
		return modeFOMO
	case r < 85:
		return modeTakeProfit
	case r < 90:
		return modeYOLO
	case r < 97:
		return modeDCA
	default:
		return modePanicSell
	}
}

// pickSide returns buy/sell biased by current mood.
func (s *Service) pickSide() string {
	threshold := 0.35 // bearish mood: 35% chance of buying
	if s.bullish {
		threshold = 0.65 // bullish mood: 65% chance of buying
	}
	if rand.Float64() < threshold {
		return "buy"
	}
	return "sell"
}

// pickMarket returns a market with bias toward the favorite.
func (s *Service) pickMarket() string {
	// 40% chance to trade the favorite market
	if rand.Float64() < 0.4 {
		return s.favoriteMarket
	}
	return s.markets[rand.Intn(len(s.markets))]
}

func (s *Service) tick() {
	if !s.cooldownUntil.IsZero() && time.Now().Before(s.cooldownUntil) {
		s.log.Debug("Cooldown active, skipping tick",
			zap.Time("cooldown_until", s.cooldownUntil),
			zap.Duration("remaining", time.Until(s.cooldownUntil)),
		)
		s.endCycle("skip - cooldown active")
		return
	}
	s.cooldownUntil = time.Time{} // clear expired cooldown

	// Mood shifts periodically - trader sentiment changes
	s.moodTicks--
	if s.moodTicks <= 0 {
		s.bullish = !s.bullish
		s.moodTicks = 15 + rand.Intn(10)
		mood := "bearish"
		if s.bullish {
			mood = "bullish"
		}
		s.log.Info("Mood shifted", zap.String("mood", mood))
	}

	// 50% chance to skip - simulates a real trader not acting every cycle.
	if rand.Float64() < 0.5 {
		s.log.Debug("Skipping tick (no opportunity)")
		s.endCycle("skip - no trade this cycle")
		return
	}

	mode := pickMode()

	maxSize := s.cfg.MaxSize
	if maxSize <= 0 {
		maxSize = 3000.0
	}

	var market, side string
	var size float64

	switch mode {
	case modeScalp:
		// Quick small trades - bread and butter of a day trader.
		market = s.pickMarket()
		side = s.pickSide()
		size = 30.0 + rand.Float64()*300.0 // 30-330 USD

	case modeSwing:
		// Medium conviction trades, larger size.
		market = s.pickMarket()
		side = s.pickSide()
		size = 200.0 + rand.Float64()*800.0 // 200-1000 USD

	case modeFOMO:
		// "It's pumping!" - oversized buy, often triggers position_limit.
		market = s.pickMarket()
		side = "buy"
		minFOMO := maxSize * 0.4
		if minFOMO < 1200.0 {
			minFOMO = 1200.0
		}
		size = minFOMO + rand.Float64()*(maxSize-minFOMO) // 1200-maxSize USD

	case modeTakeProfit:
		// Selling after a good run.
		market = s.pickMarket()
		side = "sell"
		size = 300.0 + rand.Float64()*700.0 // 300-1000 USD

	case modeYOLO:
		// Meme coin gamble - always rejected by pair_whitelist.
		market = memeCoins[rand.Intn(len(memeCoins))]
		side = "buy"
		size = 100.0 + rand.Float64()*500.0 // 100-600 USD

	case modeDCA:
		// Dollar-cost averaging into favorite. Builds concentration over time.
		market = s.favoriteMarket
		side = "buy"
		size = 200.0 + rand.Float64()*600.0 // 200-800 USD

	case modePanicSell:
		// Panic dump - large sell in one market.
		market = s.pickMarket()
		side = "sell"
		size = 800.0 + rand.Float64()*2200.0 // 800-3000 USD
	}

	s.log.Info("Submitting trade",
		zap.String("mode", mode),
		zap.String("pair", market),
		zap.String("side", side),
		zap.Float64("value", size),
	)

	args := map[string]any{
		"pair":  market,
		"side":  side,
		"value": size,
	}
	// Buy orders must include SL/TP in params (required by Swiftward policy).
	// Use estimate_order to get current price for SL/TP calculation.
	if side == "buy" {
		price := s.estimatePrice(market, size)
		if price <= 0 {
			s.log.Warn("Cannot estimate price for stop_loss, skipping buy",
				zap.String("pair", market),
				zap.String("mode", mode),
			)
			s.endCycle(fmt.Sprintf("skip - estimate failed %s %s", mode, market))
			return
		}
		args["params"] = map[string]any{
			"stop_loss":    price * 0.95, // -5% hard stop
			"take_profit":  price * 1.10, // +10% target
			"strategy": "random",
			"confidence":   0.5,
		}
	}
	result, err := s.client.CallTool("trade/submit_order", args)

	// Post session checkpoint after every trade attempt (fill or reject).
	s.endCycle(fmt.Sprintf("%s %s %s $%.0f", mode, side, market, size))

	if err != nil {
		s.log.Error("Trade call failed", zap.Error(err))
		return
	}

	if result.IsError {
		s.log.Warn("Trade rejected",
			zap.String("mode", mode),
			zap.String("pair", market),
			zap.String("error", result.Content[0].Text),
		)
		s.maybeCooldown(result.Content[0].Text)
		return
	}

	// Parse and log result
	if len(result.Content) > 0 {
		var resp map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err == nil {
			status, _ := resp["status"].(string)
			if status == "fill" {
				fill, _ := resp["fill"].(map[string]any)
				s.log.Info("Trade filled",
					zap.String("mode", mode),
					zap.Any("fill.id", fill["id"]),
					zap.Any("fill.price", fill["price"]),
					zap.Any("fill.qty", fill["qty"]),
				)
			} else {
				reject, _ := resp["reject"].(map[string]any)
				reason, _ := reject["reason"].(string)
				tag, _ := reject["tag"].(string)
				s.log.Info("Trade rejected",
					zap.String("mode", mode),
					zap.String("pair", market),
					zap.String("side", side),
					zap.String("reason", reason),
					zap.String("tag", tag),
				)
				s.maybeCooldownFromReject(reject)
			}
		}
	}

	// Check portfolio periodically (every ~5 ticks to reduce log noise)
	if rand.Intn(5) == 0 {
		s.checkPortfolio()
	}
}

// estimatePrice calls trade/estimate_order and returns the current price, or 0 on failure.
func (s *Service) estimatePrice(market string, size float64) float64 {
	estimate, err := s.client.CallTool("trade/estimate_order", map[string]any{
		"pair": market, "side": "buy", "value": size,
	})
	if err != nil || estimate.IsError || len(estimate.Content) == 0 {
		return 0
	}
	var est map[string]any
	if json.Unmarshal([]byte(estimate.Content[0].Text), &est) != nil {
		return 0
	}
	priceStr, ok := est["price"].(string)
	if !ok {
		return 0
	}
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil || price <= 0 {
		return 0
	}
	return price
}

// maybeCooldown checks an error text (from IsError responses) for cooldown signals.
func (s *Service) maybeCooldown(text string) {
	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return
	}
	s.maybeCooldownFromReject(resp)
}

// maybeCooldownFromReject parses reject response and enters cooldown.
// Rules compute retry_after (RFC3339) via trading/compute_window_end UDF.
// On loss_streak or close_only rejections, tries to close open positions first
// (risk-reducing sells are always allowed by policy).
func (s *Service) maybeCooldownFromReject(reject map[string]any) {
	tag, _ := reject["tag"].(string)

	// On loss streak / close-only, try to exit positions before going idle.
	if tag == "loss_streak" || tag == "close_only" {
		s.tryClosePositions(tag)
	}

	if retryAfter, ok := reject["retry_after"].(string); ok {
		if t, err := time.Parse(time.RFC3339, retryAfter); err == nil {
			s.cooldownUntil = t
			s.log.Warn("Entering cooldown until retry_after",
				zap.String("tag", tag),
				zap.Time("until", s.cooldownUntil),
			)
			return
		}
	}

	s.log.Warn("Rejected without retry_after - will retry next tick",
		zap.String("tag", tag),
	)
}

// tryClosePositions fetches open positions and submits sell orders to close them.
// Called when the agent hits loss_streak or close_only mode - policy allows
// risk-reducing exits even when new positions are blocked.
func (s *Service) tryClosePositions(reason string) {
	result, err := s.client.CallTool("trade/get_portfolio", map[string]any{})
	if err != nil {
		s.log.Warn("Cannot fetch positions to close", zap.Error(err))
		return
	}
	if len(result.Content) == 0 {
		return
	}

	var snap map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &snap); err != nil {
		return
	}

	positions, _ := snap["positions"].([]any)
	if len(positions) == 0 {
		s.log.Info("No open positions to close", zap.String("trigger", reason))
		return
	}

	s.log.Info("Attempting to close positions due to trading restriction",
		zap.String("trigger", reason),
		zap.Int("open_positions", len(positions)),
	)

	for _, p := range positions {
		pos, ok := p.(map[string]any)
		if !ok {
			continue
		}
		pair, _ := pos["pair"].(string)
		valueStr, _ := pos["value"].(string)
		if pair == "" || valueStr == "" {
			continue
		}
		valueFloat, err := strconv.ParseFloat(valueStr, 64)
		if err != nil || valueFloat <= 0 {
			s.log.Warn("Skipping position with invalid value",
				zap.String("pair", pair), zap.String("value", valueStr))
			continue
		}

		s.log.Info("Closing position",
			zap.String("pair", pair),
			zap.Float64("value", valueFloat),
		)

		closeResult, err := s.client.CallTool("trade/submit_order", map[string]any{
			"pair":  pair,
			"side":  "sell",
			"value": valueFloat,
		})
		if err != nil {
			s.log.Warn("Close position failed", zap.String("pair", pair), zap.Error(err))
			continue
		}

		if len(closeResult.Content) > 0 {
			var resp map[string]any
			if err := json.Unmarshal([]byte(closeResult.Content[0].Text), &resp); err == nil {
				status, _ := resp["status"].(string)
				if status == "fill" {
					fill, _ := resp["fill"].(map[string]any)
					s.log.Info("Position closed",
						zap.String("pair", pair),
						zap.Any("fill.price", fill["price"]),
						zap.Any("fill.qty", fill["qty"]),
					)
				} else {
					reject, _ := resp["reject"].(map[string]any)
					s.log.Warn("Close position rejected",
						zap.String("pair", pair),
						zap.Any("reason", reject["reason"]),
					)
				}
			}
		}
	}
}

func (s *Service) endCycle(summary string) {
	if _, err := s.client.CallTool("trade/end_cycle", map[string]any{"summary": summary}); err != nil {
		s.log.Warn("end_cycle failed", zap.Error(err))
	}
}

func (s *Service) checkPortfolio() {
	portfolio, err := s.client.CallTool("trade/get_portfolio", map[string]any{})
	if err != nil {
		s.log.Warn("Portfolio check failed", zap.Error(err))
		return
	}
	if len(portfolio.Content) > 0 {
		var snap map[string]any
		if err := json.Unmarshal([]byte(portfolio.Content[0].Text), &snap); err == nil {
			portfolio, _ := snap["portfolio"].(map[string]any)
			s.log.Info("Portfolio snapshot",
				zap.Any("portfolio.value", portfolio["value"]),
				zap.Any("portfolio.cash", portfolio["cash"]),
				zap.Any("fill_count", snap["fill_count"]),
			)
		}
	}
}
