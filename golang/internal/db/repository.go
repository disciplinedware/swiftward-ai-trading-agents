package db

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/shopspring/decimal"
)

// ErrTraceNotFound is returned by GetTrace when the requested decision hash does not exist.
var ErrTraceNotFound = errors.New("trace not found")

// TradeRecord represents a single trade in the database.
type TradeRecord struct {
	ID           int64
	AgentID      string
	Timestamp    time.Time
	Pair         string
	Side         string
	Qty          decimal.Decimal
	Price        decimal.Decimal
	Value        decimal.Decimal
	PnL          decimal.Decimal
	Status       string
	ValueAfter   decimal.Decimal
	DecisionHash string
	FillID       string
	TxHash       string
	Reason       string
	Fee          decimal.Decimal // fee amount (in base for buy, in quote for sell)
	FeeAsset     string          // fee currency ("ETH", "BTC", "USDC", etc.)
	FeeValue     decimal.Decimal // fee converted to portfolio currency at trade time
	Params       map[string]any  // agent-provided params bag (SL/TP, strategy, reasoning, etc.)
	Evidence     map[string]any  // pipeline audit trail (swiftward, risk_router, fill, hash_chain, attestation)
}

// AgentState represents the persistent state of an agent.
type AgentState struct {
	AgentID      string
	Cash         decimal.Decimal
	InitialValue decimal.Decimal
	PeakValue    decimal.Decimal
	FillCount    int
	RejectCount  int
	Halted       bool
	TotalFees    decimal.Decimal // cumulative fees in portfolio currency
	LastSeenAt   *time.Time      // last MCP tool call from this agent
}

// StateUpdate holds deltas to apply atomically in RecordTrade.
type StateUpdate struct {
	AgentID       string
	CashDelta     decimal.Decimal // added to cash (negative for buys)
	PeakValue     decimal.Decimal // GREATEST(current, this) applied
	FillCountIncr int             // added to fill_count (0 or 1)
	RejectIncr    int             // added to reject_count (0 or 1)
	FeeDelta      decimal.Decimal // added to total_fees (in portfolio currency)
}

// TradeFillUpdate holds fill data to apply to a pending trade record.
type TradeFillUpdate struct {
	TradeID    int64
	Qty        decimal.Decimal
	Price      decimal.Decimal
	Value      decimal.Decimal
	PnL        decimal.Decimal
	Status     string // "fill"
	ValueAfter decimal.Decimal
	FillID     string
	TxHash     string
	Fee        decimal.Decimal
	FeeAsset   string
	FeeValue   decimal.Decimal
	// Evidence fields merged atomically in FinalizeTrade.
	Evidence     map[string]any // merged into evidence JSONB (fill + hash_chain keys)
	DecisionHash string         // backfilled on trade record
	Trace        *DecisionTrace // inserted into decision_traces table (nil = skip)
}

// DecisionTrace represents a hash-chained decision trace stored in the database.
type DecisionTrace struct {
	DecisionHash string
	AgentID      string
	PrevHash     string
	TraceJSON    []byte
	CreatedAt    time.Time
}

// OpenPosition represents a computed open position from the trade log.
type OpenPosition struct {
	Pair     string
	Side     string
	Qty      decimal.Decimal
	AvgPrice decimal.Decimal
	Value    decimal.Decimal
}

// ReputationMetrics holds computed metrics for on-chain reputation feedback.
// These are derived display values, so float64 is acceptable.
type ReputationMetrics struct {
	TotalReturnPct float64
	MaxDrawdownPct float64
	WinRate        float64
	SharpeRatio    float64
	CompliancePct  float64
	GuardrailSaves int
}

// Repository defines the data access interface for trading persistence.
type Repository interface {
	// LockAgent acquires a distributed lock for an agent (DB advisory lock).
	// Returns an unlock function that MUST be called (typically via defer).
	// Serializes submit_order across instances so policy checks see fresh state.
	LockAgent(ctx context.Context, agentID string) (unlock func(), err error)

	// Agent state
	GetOrCreateAgent(ctx context.Context, agentID string, initialCash decimal.Decimal) (*AgentState, error)
	GetAgent(ctx context.Context, agentID string) (*AgentState, error)

	// ListAgents returns every agent_state row, including auto-registered agents
	// that were created on first trade via X-Agent-ID and never appeared in static
	// config. Used by the dashboard so dynamically-onboarded agents are visible.
	ListAgents(ctx context.Context) ([]*AgentState, error)

	// UpdatePeakValue sets peak_value only if new value is higher (safe for concurrent use).
	UpdatePeakValue(ctx context.Context, agentID string, peakValue decimal.Decimal) error

	// SetAgentHalted sets the halt flag for an agent (persisted, visible across instances).
	SetAgentHalted(ctx context.Context, agentID string, halted bool) error

	// UpdateLastSeen bumps last_seen_at to NOW() for the given agent.
	UpdateLastSeen(ctx context.Context, agentID string) error

	// NextNonce atomically increments and returns the chain nonce for an agent.
	NextNonce(ctx context.Context, agentID string) (uint64, error)

	// Trades
	InsertTrade(ctx context.Context, trade *TradeRecord) error
	GetTradeHistory(ctx context.Context, agentID string, limit int, pair, status string) ([]TradeRecord, error)

	// RecordTrade atomically applies state deltas and inserts a trade record.
	// Uses relative updates (cash += delta) to be safe under concurrent access.
	RecordTrade(ctx context.Context, update *StateUpdate, trade *TradeRecord) error

	// UpdateTradeHash backfills decision_hash on an already-persisted trade record.
	UpdateTradeHash(ctx context.Context, fillID string, decisionHash string) error

	// UpdateTradeStatus updates status and reason on a trade record by ID.
	// Does NOT update agent_state counters - use RejectPendingTrade for that.
	UpdateTradeStatus(ctx context.Context, tradeID int64, status, reason string) error

	// RejectPendingTrade atomically rejects a pending trade AND increments reject_count.
	RejectPendingTrade(ctx context.Context, agentID string, tradeID int64, reason string) error

	// FinalizeTrade atomically updates a pending trade to filled, applies state deltas,
	// merges fill evidence, inserts decision trace, and backfills decision_hash.
	FinalizeTrade(ctx context.Context, update *StateUpdate, fill *TradeFillUpdate) error

	// UpdateEvidence merges data into the evidence JSONB column of a trade.
	// Uses Postgres jsonb || operator for atomic merge.
	UpdateEvidence(ctx context.Context, tradeID int64, data map[string]any) error

	// GetFilledTradesWithoutEvidence returns filled trades missing a specific evidence key.
	// Used by hash-chain reconciliation to backfill missing decision hashes.
	GetFilledTradesWithoutEvidence(ctx context.Context, agentID, missingKey string) ([]TradeRecord, error)

	// GetFilledTradesPendingAttestation returns filled trades whose attestation
	// state is recoverable: status is 'pending' (crashed mid-flight) or
	// 'waiting_for_gas' (previous attempt hit insufficient funds; retry once
	// wallet is topped up). Normal-lifecycle states (success/error/disabled)
	// are never returned. Used by reconciliation to safely retry attestations
	// without re-touching historical trades.
	GetFilledTradesPendingAttestation(ctx context.Context, agentID string) ([]TradeRecord, error)

	// GetPendingTrades returns trades with status='pending' for the given agent.
	GetPendingTrades(ctx context.Context, agentID string) ([]TradeRecord, error)

	// GetFilledFillIDs returns the set of fill_id values for all filled trades of an agent.
	// Used by reconciliation to detect exchange fills missing from DB.
	GetFilledFillIDs(ctx context.Context, agentID string) (map[string]struct{}, error)

	// Evidence chain (hash-chained decision traces)
	InsertTrace(ctx context.Context, trace *DecisionTrace) error
	GetTrace(ctx context.Context, decisionHash string) (*DecisionTrace, error)
	GetLatestTraceHash(ctx context.Context, agentID string) (string, error)

	// Risk data (derived from trades - no extra columns needed)

	// GetDayStartValue returns yesterday's closing equity (last value_after before today's midnight UTC).
	// Falls back to zero if no prior trades exist (caller should use InitialValue).
	GetDayStartValue(ctx context.Context, agentID string) (decimal.Decimal, error)

	// GetRollingPeak24h returns the highest value_after from trades in the last 24 hours.
	// Returns zero if no trades in the window (caller should use current equity).
	GetRollingPeak24h(ctx context.Context, agentID string) (decimal.Decimal, error)

	// Positions (computed from trades)
	GetOpenPositions(ctx context.Context, agentID string) ([]OpenPosition, error)

	// Portfolio value (cash + positions at current prices)
	ComputeEquity(ctx context.Context, agentID string, prices map[string]decimal.Decimal) (decimal.Decimal, error)

	// Metrics (for reputation)
	ComputeMetrics(ctx context.Context, agentID string, prices map[string]decimal.Decimal) (*ReputationMetrics, error)

	// Alerts — persistent cross-session alerts evaluated by MCP services
	UpsertAlert(ctx context.Context, alert *AlertRecord) error
	GetActiveAlerts(ctx context.Context, agentID, service string) ([]AlertRecord, error)
	// CountActiveAlerts returns the number of active (non-exhausted, non-cancelled) alerts
	// for a given agent and service. Used to enforce per-agent per-service limits.
	CountActiveAlerts(ctx context.Context, agentID, service string) (int, error)
	// GetDueTimeAlerts returns active time-service alerts whose fire_at time has passed.
	GetDueTimeAlerts(ctx context.Context) ([]AlertRecord, error)
	// MarkAlertTriggered atomically claims an active alert and marks it triggered.
	// triggeredPrice is merged into params (e.g. "2050.12" for price alerts, "" to skip).
	// Returns (true, nil) if this caller claimed it, (false, nil) if already claimed by another goroutine.
	MarkAlertTriggered(ctx context.Context, alertID, triggeredPrice string) (bool, error)
	CancelAlert(ctx context.Context, agentID, alertID string) error
	GetTriggeredAlerts(ctx context.Context, agentID string) ([]AlertRecord, error)
	AckTriggeredAlerts(ctx context.Context, alertIDs []string) error
	// RearmAlert resets an exhausted/triggered alert back to active so it can fire again.
	RearmAlert(ctx context.Context, alertID string) error
	// FailAlert marks an alert as permanently failed (auto-execute could not complete).
	// Stores the failure reason in params.fail_reason for agent visibility.
	FailAlert(ctx context.Context, alertID, reason string) error
	// GetFailedAlerts returns alerts with status='failed' for an agent+service.
	// Used by the position protection poller to avoid recreating alerts that failed permanently.
	GetFailedAlerts(ctx context.Context, agentID, service string) ([]AlertRecord, error)
	// CancelActiveAlertsForPair cancels all active trading alerts for an agent+pair.
	// Used for proactive cleanup when a position is sold.
	CancelActiveAlertsForPair(ctx context.Context, agentID, pair string) error
	// CancelAlertsByGroup cancels all active alerts in a group except the excluded one.
	// Used for OCO behavior (SL fires -> cancel TP).
	CancelAlertsByGroup(ctx context.Context, agentID, groupID, excludeAlertID string) error
	// RestoreCancelledSiblings flips status from 'cancelled' back to 'active' for all
	// alerts in a group except the excluded one. Used to undo OCO sibling cancellation
	// when the firing alert's auto-execute fails, so the position stays protected.
	RestoreCancelledSiblings(ctx context.Context, agentID, groupID, excludeAlertID string) error
	// GetLatestBuyParams returns the params JSONB from the most recent filled buy trade for a pair.
	// Used to enrich get_portfolio positions with strategy and other metadata.
	GetLatestBuyParams(ctx context.Context, agentID, pair string) (map[string]any, error)
}

// ErrAlertNotActive is returned when UpsertAlert finds a conflicting alert_id that is not in 'active' status.
// Callers should generate a new alert ID (e.g. with a timestamp suffix) and retry.
var ErrAlertNotActive = fmt.Errorf("alert exists but is not active (exhausted/cancelled)")

// AlertRecord is the DB-persisted alert shared across Market Data MCP, Trading MCP, and the agent runtime.
type AlertRecord struct {
	AlertID      string         `json:"alert_id"`
	AgentID      string         `json:"agent_id"`
	Service      string         `json:"service"`      // "market" | "trading" | "news" | "time"
	Status       string         `json:"status"`       // "active" | "triggered" | "exhausted" | "cancelled" | "failed"
	OnTrigger    string         `json:"on_trigger"`   // "auto_execute" | "wake_triage" | "wake_full"
	MaxTriggers  int            `json:"max_triggers"` // 0 = unlimited
	TriggerCount int            `json:"trigger_count"`
	Params       map[string]any `json:"params"`        // service-specific condition params
	TriagePrompt string         `json:"triage_prompt"` // hint for Haiku triage
	Note         string         `json:"note"`
	GroupID      string         `json:"group_id"` // OCO linking: alerts in same group cancel each other
	ExpiresAt    *time.Time     `json:"expires_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	TriggeredAt  *time.Time     `json:"triggered_at,omitempty"`
}

var threshold = decimal.NewFromFloat(0.0001)

// computeMetricsFromTrades computes reputation metrics from a trade list + agent state.
func computeMetricsFromTrades(trades []TradeRecord, state *AgentState, equity decimal.Decimal) *ReputationMetrics {
	m := &ReputationMetrics{
		GuardrailSaves: state.RejectCount,
	}

	// Return %
	if state.InitialValue.IsPositive() {
		m.TotalReturnPct = equity.Sub(state.InitialValue).Div(state.InitialValue).Mul(decimal.NewFromInt(100)).InexactFloat64()
	}

	// Max drawdown from equity curve
	peak := state.InitialValue
	worstDrawdown := 0.0
	for _, t := range trades {
		if !t.ValueAfter.IsPositive() {
			continue
		}
		if t.ValueAfter.GreaterThan(peak) {
			peak = t.ValueAfter
		}
		dd := t.ValueAfter.Sub(peak).Div(peak).Mul(decimal.NewFromInt(100)).InexactFloat64()
		if dd < worstDrawdown {
			worstDrawdown = dd
		}
	}
	m.MaxDrawdownPct = worstDrawdown

	// Win rate + returns for Sharpe
	var wins, closes int
	var returns []float64
	for _, t := range trades {
		if t.Side == "sell" && t.Value.IsPositive() {
			closes++
			if t.PnL.IsPositive() {
				wins++
			}
			returns = append(returns, t.PnL.Div(t.Value).InexactFloat64())
		}
	}
	if closes > 0 {
		m.WinRate = float64(wins) / float64(closes)
	}

	// Simplified Sharpe (statistical - float64 is fine)
	if len(returns) >= 2 {
		var sum float64
		for _, r := range returns {
			sum += r
		}
		mean := sum / float64(len(returns))

		var variance float64
		for _, r := range returns {
			diff := r - mean
			variance += diff * diff
		}
		variance /= float64(len(returns) - 1)
		stddev := math.Sqrt(variance)

		if stddev > 0 {
			m.SharpeRatio = (mean / stddev) * math.Sqrt(252)
		}
	}

	// Compliance
	total := state.FillCount + state.RejectCount
	if total > 0 {
		m.CompliancePct = float64(state.FillCount) / float64(total) * 100
	}

	return m
}
