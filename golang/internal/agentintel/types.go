package agentintel

import (
	"strings"
	"time"
)

// Contract addresses (hackathon Sepolia deployment).
const (
	AgentRegistryAddr      = "0x97b07dDc405B0c28B17559aFFE63BdB3632d0ca3"
	ValidationRegistryAddr = "0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1"
	ReputationRegistryAddr = "0x423a9904e39537a9997fbaF0f220d79D7d545763"
	RiskRouterAddr         = "0xd6A6952545FF6E6E6681c2d15C59f9EB8F40FdBC"
	HackathonVaultAddr     = "0x0E7CD8ef9743FEcf94f9103033a044caBD45fC90"

	TakerFeeRate = "0.0026" // Kraken lowest-tier taker fee (0.26%)
)

// Meta holds sync cursors and state for incremental downloads.
type Meta struct {
	LastSyncedBlock  int64                   `json:"last_synced_block"`
	SyncStartedFrom  int64                   `json:"sync_started_from_block"`
	LastSyncTime     time.Time               `json:"last_sync_time"`
	LastStateRefresh time.Time               `json:"last_state_refresh"` // throttle for snapshot state phase
	KnownAgentIDs    []int64                 `json:"known_agent_ids"`
	KnownPairs       []string                `json:"known_pairs"`
	MarketCursors    map[string]MarketCursor `json:"market_cursors"`

	// PendingBlocks holds block numbers that had events but whose timestamp
	// fetch failed. The sync retries them on every run until resolved.
	// Missing block timestamps break calc (trades with timestamp=0 get no market price).
	PendingBlocks []int64 `json:"pending_blocks,omitempty"`

	// PendingBlockAttempts tracks how many times each pending block has been
	// retried. After MaxPendingBlockAttempts the block is moved to
	// UnresolvableBlocks and never retried again.
	PendingBlockAttempts map[int64]int `json:"pending_block_attempts,omitempty"`

	// UnresolvableBlocks is the set of block numbers we've given up trying to
	// fetch a timestamp for (after MaxPendingBlockAttempts). These are still
	// logged in a warning at the start of each sync so the user can investigate.
	UnresolvableBlocks []int64 `json:"unresolvable_blocks,omitempty"`
}

// MaxPendingBlockAttempts is how many times we retry a block timestamp fetch
// before giving up and moving it to UnresolvableBlocks.
const MaxPendingBlockAttempts = 10

// MarketCursor tracks incremental market data download state per pair.
type MarketCursor struct {
	LastCursorNS string `json:"last_cursor_ns"` // Kraken Trades endpoint nanosecond cursor
	LastTS       int64  `json:"last_ts"`         // Unix timestamp of last candle
	Source       string `json:"source"`          // "kraken" or "binance"
}

// Agent holds on-chain registration data from AgentRegistry.getAgent().
// Registration data never changes after first fetch. For runtime snapshot
// fields (scores, vault flag, attestation count) see AgentState.
type Agent struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	OperatorWallet string   `json:"operator_wallet"`
	AgentWallet    string   `json:"agent_wallet"`
	Description    string   `json:"description"`
	Capabilities   []string `json:"capabilities"`
	RegisteredAt   int64    `json:"registered_at"` // unix timestamp
	Active         bool     `json:"active"`
}

// AgentState is a snapshot of an agent's current on-chain runtime state.
// Lives in raw/agents/{id}/state.json and is refreshed by the sync state phase.
// All fields come from contract view calls at a point in time.
type AgentState struct {
	ID               int64     `json:"id"`
	ValidationScore  int       `json:"validation_score"`  // from ValidationRegistry.getAverageValidationScore()
	ReputationScore  int       `json:"reputation_score"`  // from ReputationRegistry.getAverageScore()
	AttestationCount int       `json:"attestation_count"` // from ValidationRegistry.attestationCount()
	VaultClaimed     bool      `json:"vault_claimed"`     // from HackathonVault.hasClaimed()
	RefreshedAt      time.Time `json:"refreshed_at"`
}

// TradeIntent is a normalized TradeIntentSubmitted event from RiskRouter.
type TradeIntent struct {
	Block          int64  `json:"block"`
	LogIndex       int    `json:"log_index"`
	TxHash         string `json:"tx_hash"`
	AgentID        int64  `json:"agent_id"`
	IntentHash     string `json:"intent_hash"`
	Pair           string `json:"pair"`            // raw pair from on-chain (e.g. "XBTUSD", "ZEC-USD", "WETH/USDC")
	CanonicalPair  string `json:"canonical_pair"`  // normalized for market data lookup
	Action         string `json:"action"`          // BUY, SELL, LONG, SHORT
	AmountUSDScaled int64 `json:"amount_usd_scaled"` // divide by 100 for dollars
	Timestamp      int64  `json:"timestamp"`       // block timestamp (unix)
}

// TradeOutcome is a TradeApproved or TradeRejected event.
type TradeOutcome struct {
	Block      int64  `json:"block"`
	LogIndex   int    `json:"log_index"`
	TxHash     string `json:"tx_hash"`
	AgentID    int64  `json:"agent_id"`
	IntentHash string `json:"intent_hash"`
	Approved   bool   `json:"approved"`
	Reason     string `json:"reason,omitempty"` // only for rejections
}

// Attestation from ValidationRegistry.getAttestations().
type Attestation struct {
	Block          int64  `json:"block"`
	LogIndex       int    `json:"log_index"`
	TxHash         string `json:"tx_hash"`
	AgentID        int64  `json:"agent_id"`
	Validator      string `json:"validator"`
	CheckpointHash string `json:"checkpoint_hash"`
	Score          int    `json:"score"`
	ProofType      int    `json:"proof_type"`
	Notes          string `json:"notes"`
	Timestamp      int64  `json:"timestamp"` // unix seconds; populated from block timestamp in calc
}

// ReputationFeedback from hackathon ReputationRegistry FeedbackSubmitted event.
type ReputationFeedback struct {
	Block        int64  `json:"block"`
	LogIndex     int    `json:"log_index"`
	TxHash       string `json:"tx_hash"`
	AgentID      int64  `json:"agent_id"`
	Validator    string `json:"validator"`
	Score        int    `json:"score"`          // uint8, 0-100
	OutcomeRef   string `json:"outcome_ref"`    // hex bytes32
	FeedbackType int    `json:"feedback_type"`  // 0=TRADE_EXECUTION, 1=RISK_MANAGEMENT, 2=STRATEGY_QUALITY, 3=GENERAL
	Comment      string `json:"comment"`        // from tx calldata, may be empty
}

// Candle is a 1-minute OHLCV bar aggregated from Kraken trades.
type Candle struct {
	T        int64  `json:"t"`        // unix timestamp (start of minute)
	Open     string `json:"o"`
	High     string `json:"h"`
	Low      string `json:"l"`
	Close    string `json:"c"`
	Volume   string `json:"v"`
	VWAP     string `json:"vwap"`     // volume-weighted average price
	Source   string `json:"src"`      // "kraken" or "binance"
	Interval int    `json:"interval"` // seconds (60 for 1-min)
}

// --- Computed types (output of PnL calculation) ---

// ComputedAgent is the full analysis result for one agent.
type ComputedAgent struct {
	Agent        Agent                `json:"agent"`
	State        AgentState           `json:"state"`
	Trades       []ComputedTrade      `json:"trades"`
	Summary      AgentSummary         `json:"summary"`
	Attestations []Attestation        `json:"attestations"`
	Reputation   []ReputationFeedback `json:"reputation"`
}

// ComputedTrade is a single trade with market price, fee, PnL, and portfolio snapshot.
type ComputedTrade struct {
	Index          int               `json:"index"`
	Block          int64             `json:"block"`
	Timestamp      string            `json:"timestamp"` // ISO 8601
	Pair           string            `json:"pair"`
	CanonicalPair  string            `json:"canonical_pair"`
	Action         string            `json:"action"`
	AmountUSD      string            `json:"amount_usd"`
	MarketPrice    string            `json:"market_price"`
	Qty            string            `json:"qty"`
	FeeUSD         string            `json:"fee_usd"`
	FeeRate        string            `json:"fee_rate"`
	Outcome        string            `json:"outcome"` // APPROVED, REJECTED, UNKNOWN
	RejectReason   string            `json:"reject_reason,omitempty"`
	RealizedPnL    *string           `json:"realized_pnl"`    // nil for BUY trades
	PositionBefore string            `json:"position_before"` // qty on this pair before trade
	PositionAfter  string            `json:"position_after"`  // qty on this pair after trade
	CashAfter      string            `json:"cash_after"`      // USD cash after trade
	EquityAfter    string            `json:"equity_after"`    // cash + mark-to-market positions
	CumulativePnL  string            `json:"cumulative_pnl"`
	PortfolioAfter map[string]Position `json:"portfolio_after"`
}

// Position tracks a single pair's holding after a trade.
type Position struct {
	Qty       string `json:"qty"`
	AvgCost   string `json:"avg_cost"`   // average cost per unit
	TotalCost string `json:"total_cost"` // total USD invested
}

// AgentSummary aggregates PnL and activity metrics.
type AgentSummary struct {
	TotalTrades       int               `json:"total_trades"`
	ApprovedTrades    int               `json:"approved_trades"`
	RejectedTrades    int               `json:"rejected_trades"`
	MaxExposure       string            `json:"max_exposure"` // peak capital committed = $10K - min(cash_after). Honest measure vs gross turnover.
	TotalFees         string            `json:"total_fees"`
	RealizedPnL       string            `json:"realized_pnl"`
	UnrealizedPnL     string            `json:"unrealized_pnl"`
	NetPnL            string            `json:"net_pnl"`
	ReturnPct         string            `json:"return_pct"` // net_pnl / initial_capital ($10K) * 100
	PairsTraded       []string          `json:"pairs_traded"`
	WinCount          int               `json:"win_count"`
	LossCount         int               `json:"loss_count"`
	WinRatePct        string            `json:"win_rate_pct"`
	AvgTradeProfit    string            `json:"avg_trade_profit"`
	PnLByPair         map[string]PairPnL `json:"pnl_by_pair"`
	AttestationCount  int               `json:"attestation_count"`
	AvgValidationScore int              `json:"avg_validation_score"`
	AvgReputationScore int              `json:"avg_reputation_score"`
	RepFeedbackCount  int               `json:"rep_feedback_count"`
	RepUniqueValidators int             `json:"rep_unique_validators"`
	// Hackathon leaderboard score (organizer formula, as of 2026-04-10).
	// validation_half = (avg_validation_score * 0.5) + (min(approved, 10) * 3) + (vault ? 10 : 0) + (attestations > 0 ? 10 : 0)  // 0-100
	// final = validation_half * 0.5 + avg_reputation_score * 0.5  // 0-100
	HackathonScore       int `json:"hackathon_score"`        // final combined score 0-100
	HackathonValidation  int `json:"hackathon_validation"`   // validation half 0-100
	HackathonActivityPts int `json:"hackathon_activity_pts"` // min(approved,10)*3, 0-30
	HackathonVaultPts    int `json:"hackathon_vault_pts"`    // 10 or 0
	HackathonCheckpointPts int `json:"hackathon_checkpoint_pts"` // 10 or 0
	// AI analysis
	GamingFlags       []string          `json:"gaming_flags,omitempty"`
	AIVerdict         string            `json:"ai_verdict,omitempty"` // extracted from analysis markdown
}

// PairPnL is per-pair PnL summary.
type PairPnL struct {
	Trades      int    `json:"trades"`
	BuyVolume   string `json:"buy_volume"`
	SellVolume  string `json:"sell_volume"`
	RealizedPnL string `json:"realized_pnl"`
	OpenQty     string `json:"open_qty"`
	Unrealized  string `json:"unrealized"`
}

// --- Pair normalization ---

// specialPairs handles exceptions that rules can't cover.
var specialPairs = map[string]string{
	"WETH": "ETH", // WETH = wrapped ETH, same price
	"BTC":  "XBT", // Kraken uses XBT for Bitcoin
	"XDG":  "DOGE", // Kraken internal name
}

// CanonicalPair normalizes an on-chain pair name to a canonical Kraken pair for market data.
// Rules-based: strip quotes, remove separators, normalize quote currency to USD, apply renames.
func CanonicalPair(raw string) string {
	// Strip quotes and normalize case.
	s := strings.ToUpper(strings.Trim(raw, "\"' "))

	// Remove separators (- / _).
	s = strings.NewReplacer("-", "", "/", "", "_", "").Replace(s)

	// Normalize quote currency: USDT, USDC -> USD.
	for _, suffix := range []string{"USDT", "USDC"} {
		if strings.HasSuffix(s, suffix) && len(s) > len(suffix) {
			s = s[:len(s)-len(suffix)] + "USD"
			break
		}
	}

	// Apply special renames (WETH->ETH, BTC->XBT).
	if strings.HasSuffix(s, "USD") {
		base := s[:len(s)-3]
		if replacement, ok := specialPairs[base]; ok {
			s = replacement + "USD"
		}
	}

	return s
}
