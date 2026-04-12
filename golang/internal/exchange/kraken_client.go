package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// KrakenClient executes trades via the Kraken CLI binary.
// Two modes:
//   - sandbox=true:  paper trading, no API key needed, local state
//   - sandbox=false: real trading, CLI reads KRAKEN_API_KEY + KRAKEN_API_SECRET from env
//
// Per-agent isolation: when StateDir is set, ForAgent(agentID) returns a
// client whose CLI calls run with HOME={stateDir}/{agentID}, giving each
// agent its own Kraken paper account. Prices (ticker) are shared.
//
// Kraken CLI returns fees in every trade response - we use them directly,
// no custom fee calculation needed.
type KrakenClient struct {
	log        *zap.Logger
	krakenBin  string
	sandbox    bool
	stateDir   string // base dir for per-agent state (e.g. /data/kraken)
	homeDir    string // per-agent HOME override (set by ForAgent)
	pairs      []string
	mu         sync.Mutex
	lastPrices map[string]decimal.Decimal
	inited     bool

	// per-agent client cache (only used on root client)
	agentsMu sync.Mutex
	agents   map[string]*KrakenClient
}

type KrakenConfig struct {
	Bin      string   // path to kraken CLI binary, default "kraken"
	Sandbox  bool     // true = paper trading, false = real (needs KRAKEN_API_KEY env var)
	StateDir string   // base dir for per-agent state isolation (e.g. /data/kraken)
	Pairs    []string // known trading pairs for fill history (e.g. ["ETH-USD","BTC-USD"])
}

func NewKrakenClient(log *zap.Logger, cfg KrakenConfig) *KrakenClient {
	bin := cfg.Bin
	if bin == "" {
		bin = "kraken"
	}
	return &KrakenClient{
		log:        log,
		krakenBin:  bin,
		sandbox:    cfg.Sandbox,
		stateDir:   cfg.StateDir,
		pairs:      append([]string(nil), cfg.Pairs...),
		lastPrices: make(map[string]decimal.Decimal),
		agents:     make(map[string]*KrakenClient),
	}
}

// ForAgent returns an agent-scoped client with isolated Kraken CLI state.
// Prices are shared (same lastPrices map). Paper account state is isolated
// via HOME env var override. If no stateDir is configured, returns self.
func (c *KrakenClient) ForAgent(agentID string) Client {
	if c.stateDir == "" {
		return c
	}

	c.agentsMu.Lock()
	defer c.agentsMu.Unlock()

	if ac, ok := c.agents[agentID]; ok {
		return ac
	}

	home := c.stateDir + "/" + agentID
	if err := os.MkdirAll(home, 0o755); err != nil {
		c.log.Error("Failed to create agent Kraken state dir", zap.String("agent_id", agentID), zap.Error(err))
	}

	ac := &KrakenClient{
		log:        c.log.With(zap.String("agent_id", agentID)),
		krakenBin:  c.krakenBin,
		sandbox:    c.sandbox,
		stateDir:   c.stateDir,
		homeDir:    home,
		pairs:      append([]string(nil), c.pairs...),
		lastPrices: c.lastPrices, // shared - prices are global, not per-agent
	}
	c.agents[agentID] = ac
	c.log.Info("Created per-agent Kraken client", zap.String("agent_id", agentID), zap.String("home", home))
	return ac
}

// run executes a kraken CLI command and returns the raw JSON output.
// When homeDir is set (per-agent client), overrides HOME so Kraken CLI
// reads/writes state from the agent-specific directory.
func (c *KrakenClient) run(args ...string) ([]byte, error) {
	args = append(args, "-o", "json")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.krakenBin, args...)
	if c.homeDir != "" {
		cmd.Env = append(os.Environ(), "HOME="+c.homeDir)
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			combined := string(out) + string(exitErr.Stderr)
			return nil, fmt.Errorf("kraken cli error: %s", strings.TrimSpace(combined))
		}
		return nil, fmt.Errorf("kraken cli: %w", err)
	}
	return out, nil
}

// ensurePaperInit initializes the paper account if not yet done.
func (c *KrakenClient) ensurePaperInit() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.inited || !c.sandbox {
		return nil
	}
	_, err := c.run("paper", "balance")
	if err == nil {
		c.inited = true
		return nil
	}
	_, err = c.run("paper", "init", "--balance", "10000", "--currency", "USD")
	if err != nil {
		if strings.Contains(err.Error(), "already initialized") {
			c.inited = true
			return nil
		}
		return fmt.Errorf("paper init: %w", err)
	}
	c.inited = true
	c.log.Info("Kraken paper account initialized with $10,000 USD")
	return nil
}

// toKrakenPair maps canonical pair format to Kraken's native format.
// "BTC-USD" -> "XBTUSD", "ETH-USD" -> "ETHUSD", "DOGE-USD" -> "XDGUSD"
// No cross-quote conversion: USD, USDT, USDC are distinct instruments.
func toKrakenPair(pair string) string {
	p := strings.ReplaceAll(pair, "-", "")
	p = strings.Replace(p, "BTC", "XBT", 1)
	p = strings.Replace(p, "DOGE", "XDG", 1)
	return p
}

// fromKrakenPair converts Kraken pair format back to canonical: XBTUSD -> BTC-USD.
func fromKrakenPair(kp string) string {
	p := strings.Replace(kp, "XBT", "BTC", 1)
	p = strings.Replace(p, "XDG", "DOGE", 1)
	// Insert dash before known quote suffixes (longest first to avoid partial match).
	for _, q := range []string{"USDT", "USDC", "USD", "EUR", "GBP", "BTC", "ETH"} {
		if strings.HasSuffix(p, q) && len(p) > len(q) {
			return p[:len(p)-len(q)] + "-" + q
		}
	}
	return p
}

// krakenFillResponse is the JSON shape returned by both `kraken paper buy/sell`
// and `kraken order buy/sell` (market orders).
type krakenFillResponse struct {
	Action  string  `json:"action"` // "market_order_filled"
	Cost    float64 `json:"cost"`   // total cost in quote currency
	Fee     float64 `json:"fee"`    // fee in quote currency (Kraken calculates)
	Mode    string  `json:"mode"`   // "paper" or "live"
	OrderID string  `json:"order_id"`
	Pair    string  `json:"pair"`
	Price   float64 `json:"price"` // fill price
	Side    string  `json:"side"`  // "buy" or "sell"
	TradeID string  `json:"trade_id"`
	Volume  float64 `json:"volume"` // filled volume in base
	// Real mode may have additional fields - we ignore them.
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

func (c *KrakenClient) SubmitTrade(req *TradeRequest) (*TradeResponse, error) {
	if c.sandbox {
		if err := c.ensurePaperInit(); err != nil {
			return nil, err
		}
	}

	krakenPair := toKrakenPair(req.Pair)

	var volume decimal.Decimal
	if req.Qty.IsPositive() {
		// Qty-based sizing: use base qty directly, no price round-trip.
		volume = req.Qty.Round(8)
	} else {
		// Value-based sizing: convert quote value to base qty at current price.
		price, ok := c.GetPrice(req.Pair)
		if !ok {
			return nil, fmt.Errorf("kraken: cannot get price for %s", req.Pair)
		}
		if price.IsZero() {
			return nil, fmt.Errorf("kraken: zero price for %s", req.Pair)
		}
		volume = req.Value.Div(price).Round(8)
	}

	var out []byte
	var err error

	orderType := "market"
	if req.Params != nil {
		if ot, ok := req.Params["order_type"].(string); ok && ot != "" {
			orderType = ot
		}
	}

	if c.sandbox {
		out, err = c.run("paper", req.Side, krakenPair, volume.String())
	} else {
		// Real mode: CLI reads KRAKEN_API_KEY + KRAKEN_API_SECRET from env vars.
		out, err = c.run("order", req.Side, krakenPair, volume.String(), "--type", orderType)
	}
	if err != nil {
		return nil, fmt.Errorf("kraken %s %s: %w", req.Side, krakenPair, err)
	}

	return c.parseFill(out, req)
}

// parseFill parses a trade response from Kraken CLI (both paper and real).
// Kraken returns fee in quote currency. We convert per our interface contract:
//   - Buy:  Fee in base, Qty = net base received
//   - Sell: Fee in quote, QuoteQty = net cash received
func (c *KrakenClient) parseFill(out []byte, req *TradeRequest) (*TradeResponse, error) {
	var fill krakenFillResponse
	if err := json.Unmarshal(out, &fill); err != nil {
		return nil, fmt.Errorf("kraken: parse fill: %w (raw: %s)", err, string(out))
	}

	// Check for error responses.
	if fill.Error != "" {
		return &TradeResponse{
			Status: StatusRejected,
			Pair:   req.Pair,
			Side:   req.Side,
		}, fmt.Errorf("kraken: %s: %s", fill.Error, fill.Message)
	}
	if fill.Action != "market_order_filled" {
		return &TradeResponse{
			Status: StatusRejected,
			Pair:   req.Pair,
			Side:   req.Side,
		}, fmt.Errorf("kraken: unexpected action %q (raw: %s)", fill.Action, string(out))
	}

	fillPrice := decimal.NewFromFloat(fill.Price)
	grossQty := decimal.NewFromFloat(fill.Volume)
	cost := decimal.NewFromFloat(fill.Cost)
	feeQuote := decimal.NewFromFloat(fill.Fee) // Kraken always returns fee in quote (USD)

	// Convert fee per our interface contract.
	var qty, quoteQty, fee decimal.Decimal
	if req.Side == "buy" {
		feeInBase := feeQuote.Div(fillPrice).Round(8)
		qty = grossQty.Sub(feeInBase)
		quoteQty = cost
		fee = feeInBase
	} else {
		qty = grossQty
		quoteQty = cost.Sub(feeQuote)
		fee = feeQuote
	}

	c.mu.Lock()
	c.lastPrices[req.Pair] = fillPrice
	c.mu.Unlock()

	fillID := fill.TradeID
	if fillID == "" {
		fillID = fill.OrderID
	}

	c.log.Info("Kraken trade filled",
		zap.String("fill_id", fillID),
		zap.String("mode", fill.Mode),
		zap.String("pair", req.Pair),
		zap.String("side", req.Side),
		zap.String("price", fillPrice.String()),
		zap.String("qty", qty.String()),
		zap.String("cost", cost.String()),
		zap.String("fee", fee.String()),
	)

	return &TradeResponse{
		FillID:   fillID,
		Status:   StatusFilled,
		Price:    fillPrice,
		Qty:      qty,
		QuoteQty: quoteQty,
		Fee:      fee,
		Pair:     req.Pair,
		Side:     req.Side,
	}, nil
}

// tickerEntry is the JSON shape from `kraken ticker PAIR -o json`.
type tickerEntry struct {
	C []string `json:"c"` // [last_price, lot_volume]
	A []string `json:"a"` // [ask_price, whole_lot_volume, lot_volume]
	B []string `json:"b"` // [bid_price, whole_lot_volume, lot_volume]
}

func (c *KrakenClient) GetPrice(pair string) (decimal.Decimal, bool) {
	krakenPair := toKrakenPair(pair)
	out, err := c.run("ticker", krakenPair)
	if err != nil {
		c.log.Debug("Kraken ticker failed", zap.String("pair", pair), zap.Error(err))
		c.mu.Lock()
		p, ok := c.lastPrices[pair]
		c.mu.Unlock()
		return p, ok
	}

	var raw map[string]tickerEntry
	if err := json.Unmarshal(out, &raw); err != nil {
		c.log.Debug("Kraken ticker parse failed", zap.String("pair", pair), zap.Error(err))
		c.mu.Lock()
		p, ok := c.lastPrices[pair]
		c.mu.Unlock()
		return p, ok
	}

	for _, entry := range raw {
		if len(entry.C) == 0 {
			continue
		}
		price, err := decimal.NewFromString(entry.C[0])
		if err != nil || price.IsZero() {
			continue
		}
		c.mu.Lock()
		c.lastPrices[pair] = price
		c.mu.Unlock()
		return price, true
	}

	c.mu.Lock()
	p, ok := c.lastPrices[pair]
	c.mu.Unlock()
	return p, ok
}

// GetBalance returns current balances from Kraken CLI.
// Paper mode: `kraken paper balance -o json`
// Real mode: `kraken balance -o json` (CLI reads API key from env)
func (c *KrakenClient) GetBalance() ([]BalanceEntry, error) {
	var out []byte
	var err error

	if c.sandbox {
		if initErr := c.ensurePaperInit(); initErr != nil {
			return nil, initErr
		}
		out, err = c.run("paper", "balance")
	} else {
		out, err = c.run("balance")
	}
	if err != nil {
		return nil, fmt.Errorf("kraken balance: %w", err)
	}

	if c.sandbox {
		// Paper: {"balances":{"USD":{"available":9990.42,"reserved":0,"total":9990.42},"BTC":{"available":0.0001,...}}}
		var resp struct {
			Balances map[string]struct {
				Available float64 `json:"available"`
				Reserved  float64 `json:"reserved"`
				Total     float64 `json:"total"`
			} `json:"balances"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("kraken: parse paper balance: %w", err)
		}
		entries := make([]BalanceEntry, 0, len(resp.Balances))
		for asset, bal := range resp.Balances {
			entries = append(entries, BalanceEntry{
				Asset:     asset,
				Available: decimal.NewFromFloat(bal.Available),
				Reserved:  decimal.NewFromFloat(bal.Reserved),
				Total:     decimal.NewFromFloat(bal.Total),
			})
		}
		return entries, nil
	}

	// Real: {"result":{"ZUSD":"9990.42","XXBT":"0.0001",...}}
	var resp struct {
		Result map[string]string `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("kraken: parse real balance: %w", err)
	}
	entries := make([]BalanceEntry, 0, len(resp.Result))
	for asset, valStr := range resp.Result {
		val, _ := decimal.NewFromString(valStr)
		entries = append(entries, BalanceEntry{
			Asset:     asset,
			Available: val,
			Total:     val,
		})
	}
	return entries, nil
}

func (c *KrakenClient) GetPrices() map[string]decimal.Decimal {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]decimal.Decimal, len(c.lastPrices))
	for k, v := range c.lastPrices {
		out[k] = v
	}
	return out
}

// PlaceStopOrder places a native stop-loss or take-profit order on Kraken.
// This implements StopOrderProvider for Tier 1 stop-loss support.
// In sandbox mode, Kraken CLI stop orders may not be supported — returns an error gracefully.
func (c *KrakenClient) PlaceStopOrder(pair, side, orderType string, stopPrice, qty decimal.Decimal) (*StopOrderResult, error) {
	krakenPair := toKrakenPair(pair)
	cliOrderType := "stop-loss"
	if orderType == "take_profit" {
		cliOrderType = "take-profit"
	}
	args := []string{
		"order", "add",
		"--pair", krakenPair,
		"--side", side,
		"--type", cliOrderType,
		"--price", stopPrice.StringFixed(4),
	}
	if qty.IsZero() {
		args = append(args, "--qty", "all")
	} else {
		args = append(args, "--qty", qty.StringFixed(8))
	}

	if c.sandbox {
		args = append(args, "--sandbox")
	}

	out, err := c.run(args...)
	if err != nil {
		return nil, fmt.Errorf("place stop order: %w", err)
	}

	// Parse order ID from CLI JSON output (best-effort; format varies by CLI version).
	var resp struct {
		OrderID string `json:"order_id"`
		TxID    string `json:"txid"`
	}
	if jsonErr := json.Unmarshal(out, &resp); jsonErr != nil {
		// CLI may not return JSON for stop orders — treat raw output as order ID if non-empty.
		rawID := strings.TrimSpace(string(out))
		if rawID == "" {
			return nil, fmt.Errorf("place stop order: empty response")
		}
		resp.OrderID = rawID
	}
	orderID := resp.OrderID
	if orderID == "" {
		orderID = resp.TxID
	}

	return &StopOrderResult{
		OrderID:   orderID,
		Pair:      pair,
		Side:      side,
		StopPrice: stopPrice,
		Qty:       qty,
		OrderType: orderType,
	}, nil
}

// CancelStopOrder cancels a native Kraken stop order by order ID.
func (c *KrakenClient) CancelStopOrder(orderID string) error {
	args := []string{"order", "cancel", "--order-id", orderID}
	if c.sandbox {
		args = append(args, "--sandbox")
	}
	_, err := c.run(args...)
	if err != nil {
		return fmt.Errorf("cancel stop order %s: %w", orderID, err)
	}
	return nil
}

// GetFillHistory returns all historical fills from Kraken CLI.
// For per-agent isolation, pass agentID to use the agent's HOME dir.
// Implements FillHistoryProvider.
func (c *KrakenClient) GetFillHistory(agentID string) ([]ExchangeFill, error) {
	// Use agent-scoped client if available.
	agent := c.forAgentClient(agentID)

	var resp struct {
		Trades []krakenHistoryEntry `json:"trades"`
	}

	if agent.sandbox {
		if initErr := agent.ensurePaperInit(); initErr != nil {
			return nil, fmt.Errorf("kraken fill history: %w", initErr)
		}
		out, err := agent.run("paper", "history")
		if err != nil {
			return nil, fmt.Errorf("kraken fill history: %w", err)
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("kraken: parse fill history: %w (raw: %s)", err, string(out))
		}
	} else {
		// Real mode: CLI requires a PAIR argument. Query each known pair.
		pairs := agent.pairs
		if len(pairs) == 0 {
			// Defensive fallback for clients created before pairs were wired.
			pairs = c.pairs
		}
		if len(pairs) == 0 {
			agent.log.Warn("KrakenConfig.Pairs is empty, using hardcoded fallback for fill history")
			pairs = []string{"BTC-USD", "ETH-USD", "SOL-USD"}
		}
		var successCount int
		for _, pair := range pairs {
			out, err := agent.run("trades", toKrakenPair(pair))
			if err != nil {
				agent.log.Warn("Kraken trades query failed for pair", zap.String("pair", pair), zap.Error(err))
				continue
			}
			var pairResp struct {
				Trades []krakenHistoryEntry `json:"trades"`
			}
			if err := json.Unmarshal(out, &pairResp); err != nil {
				agent.log.Warn("Kraken trades parse failed", zap.String("pair", pair), zap.Error(err))
				continue
			}
			successCount++
			resp.Trades = append(resp.Trades, pairResp.Trades...)
		}
		if successCount == 0 {
			return nil, fmt.Errorf("kraken fill history: all %d pairs failed - check API credentials", len(pairs))
		}
	}

	fills := make([]ExchangeFill, 0, len(resp.Trades))
	for _, t := range resp.Trades {
		if t.Status != "" && t.Status != "filled" {
			continue // skip cancelled/pending orders
		}
		fillTime, parseErr := parseKrakenHistoryTime(t.Time)
		if parseErr != nil && t.Time != "" {
			agent.log.Warn("Skipping fill with unparseable time",
				zap.String("trade_id", t.TradeID), zap.String("order_id", t.OrderID), zap.String("raw_time", t.Time))
			continue
		}
		if fillTime.IsZero() {
			continue // skip fills with no timestamp
		}
		tradeID := normalizeKrakenHistoryID(t, fillTime)
		if t.ID == "" && t.TradeID == "" && t.OrderID == "" {
			agent.log.Warn("Kraken fill missing id/order fields, generated synthetic fill id",
				zap.String("synthetic_fill_id", tradeID),
				zap.String("pair", t.Pair),
				zap.String("side", t.Side),
				zap.String("time", fillTime.Format(time.RFC3339Nano)))
		}
		fills = append(fills, ExchangeFill{
			TradeID: tradeID,
			OrderID: t.OrderID,
			Pair:    fromKrakenPair(t.Pair),
			Side:    t.Side,
			Price:   decimal.NewFromFloat(t.Price),
			Volume:  decimal.NewFromFloat(t.Volume),
			Cost:    decimal.NewFromFloat(t.Cost),
			Fee:     decimal.NewFromFloat(t.Fee),
			Time:    fillTime,
		})
	}
	return fills, nil
}

func normalizeKrakenHistoryID(t krakenHistoryEntry, fillTime time.Time) string {
	if t.ID != "" {
		return t.ID
	}
	if t.TradeID != "" {
		return t.TradeID
	}
	if t.OrderID != "" {
		return t.OrderID
	}
	return fmt.Sprintf("synthetic:%s:%s:%d:%.10f:%.10f",
		t.Pair, t.Side, fillTime.UnixNano(), t.Price, t.Volume)
}

func parseKrakenHistoryTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC(), nil
		}
	}
	// Some Kraken surfaces emit Unix seconds with fractional part.
	if secs, err := strconv.ParseFloat(raw, 64); err == nil {
		secPart, fracPart := math.Modf(secs)
		nano := int64(fracPart * float64(time.Second))
		return time.Unix(int64(secPart), nano).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time format: %q", raw)
}

// forAgentClient returns the per-agent KrakenClient (or self if no isolation).
func (c *KrakenClient) forAgentClient(agentID string) *KrakenClient {
	if c.stateDir == "" || agentID == "" {
		return c
	}
	if agent, ok := c.forAgent(agentID); ok {
		return agent
	}
	return c
}

// forAgent returns cached agent client and whether it exists.
func (c *KrakenClient) forAgent(agentID string) (*KrakenClient, bool) {
	c.agentsMu.Lock()
	defer c.agentsMu.Unlock()
	if agent, ok := c.agents[agentID]; ok {
		return agent, true
	}
	return nil, false
}

// krakenHistoryEntry is the JSON shape for a single trade in `kraken paper history -o json`.
type krakenHistoryEntry struct {
	ID      string  `json:"id"`
	TradeID string  `json:"trade_id"`
	OrderID string  `json:"order_id"`
	Pair    string  `json:"pair"`
	Side    string  `json:"side"`
	Price   float64 `json:"price"`
	Volume  float64 `json:"volume"`
	Cost    float64 `json:"cost"`
	Fee     float64 `json:"fee"`
	Time    string  `json:"time"`
	Status  string  `json:"status"`
	Mode    string  `json:"mode"`
}
