package db

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// MemRepository is an in-memory implementation of Repository for testing.
type MemRepository struct {
	mu     sync.RWMutex
	agents map[string]*AgentState
	trades []TradeRecord
	nonces map[string]uint64
	traces map[string]*DecisionTrace // decision_hash -> trace
	// latestTraceHash tracks the most recent decision_hash per agent (by insertion seq).
	latestTraceHash map[string]string       // agent_id -> decision_hash
	alerts          map[string]*AlertRecord // alert_id -> record
	nextID          int64
	nextTraceSeq    int64
}

// NewMemRepository creates a new in-memory repository.
func NewMemRepository() *MemRepository {
	return &MemRepository{
		agents:          make(map[string]*AgentState),
		nonces:          make(map[string]uint64),
		traces:          make(map[string]*DecisionTrace),
		latestTraceHash: make(map[string]string),
	}
}

func (r *MemRepository) LockAgent(_ context.Context, _ string) (func(), error) {
	return func() {}, nil
}

func (r *MemRepository) SetAgentHalted(_ context.Context, agentID string, halted bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}
	s.Halted = halted
	return nil
}

func (r *MemRepository) GetOrCreateAgent(_ context.Context, agentID string, initialCash decimal.Decimal) (*AgentState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.agents[agentID]; ok {
		return r.copyState(s), nil
	}
	s := &AgentState{
		AgentID:      agentID,
		Cash:         initialCash,
		InitialValue: initialCash,
		PeakValue:    initialCash,
	}
	r.agents[agentID] = s
	return r.copyState(s), nil
}

func (r *MemRepository) GetAgent(_ context.Context, agentID string) (*AgentState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.agents[agentID]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}
	return r.copyState(s), nil
}

func (r *MemRepository) ListAgents(_ context.Context) ([]*AgentState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.agents))
	for id := range r.agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]*AgentState, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.copyState(r.agents[id]))
	}
	return out, nil
}

func (r *MemRepository) UpdateAgentState(_ context.Context, state *AgentState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.agents[state.AgentID]
	if !ok {
		return fmt.Errorf("agent %s not found", state.AgentID)
	}
	updated := r.copyState(state)
	if existing.PeakValue.GreaterThan(updated.PeakValue) {
		updated.PeakValue = existing.PeakValue
	}
	r.agents[state.AgentID] = updated
	return nil
}

func (r *MemRepository) UpdateLastSeen(_ context.Context, agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.agents[agentID]
	if !ok {
		return nil // best-effort: agent may not exist yet on first tool call
	}
	now := time.Now()
	s.LastSeenAt = &now
	return nil
}

func (r *MemRepository) UpdatePeakValue(_ context.Context, agentID string, peakValue decimal.Decimal) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}
	if peakValue.GreaterThan(s.PeakValue) {
		s.PeakValue = peakValue
	}
	return nil
}

func (r *MemRepository) NextNonce(_ context.Context, agentID string) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.agents[agentID]; !ok {
		return 0, fmt.Errorf("agent %s not found", agentID)
	}
	n := r.nonces[agentID]
	r.nonces[agentID] = n + 1
	return n, nil
}

func (r *MemRepository) RecordTrade(_ context.Context, update *StateUpdate, trade *TradeRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.agents[update.AgentID]
	if !ok {
		return fmt.Errorf("agent %s not found", update.AgentID)
	}
	newCash := existing.Cash.Add(update.CashDelta)
	if newCash.IsNegative() {
		return fmt.Errorf("cash would be negative (%s) for agent %s", newCash, update.AgentID)
	}
	existing.Cash = newCash
	existing.FillCount += update.FillCountIncr
	existing.RejectCount += update.RejectIncr
	existing.TotalFees = existing.TotalFees.Add(update.FeeDelta)
	if update.PeakValue.GreaterThan(existing.PeakValue) {
		existing.PeakValue = update.PeakValue
	}

	r.nextID++
	t := *trade
	t.ID = r.nextID
	trade.ID = r.nextID // set on caller's struct too
	if t.Timestamp.IsZero() {
		t.Timestamp = time.Now()
	}
	r.trades = append(r.trades, t)
	return nil
}

func (r *MemRepository) UpdateTradeStatus(_ context.Context, tradeID int64, status, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.trades {
		if r.trades[i].ID == tradeID {
			r.trades[i].Status = status
			r.trades[i].Reason = reason
			return nil
		}
	}
	return nil
}

func (r *MemRepository) RejectPendingTrade(_ context.Context, agentID string, tradeID int64, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.trades {
		if r.trades[i].ID == tradeID && r.trades[i].Status == "pending" {
			r.trades[i].Status = "reject"
			r.trades[i].Reason = reason
			if agent, ok := r.agents[agentID]; ok {
				agent.RejectCount++
			}
			return nil
		}
	}
	return nil // already resolved
}

func (r *MemRepository) FinalizeTrade(_ context.Context, update *StateUpdate, fill *TradeFillUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.agents[update.AgentID]
	if !ok {
		return fmt.Errorf("agent %s not found", update.AgentID)
	}
	newCash := existing.Cash.Add(update.CashDelta)
	if newCash.IsNegative() {
		return fmt.Errorf("cash would be negative (%s) for agent %s", newCash, update.AgentID)
	}
	existing.Cash = newCash
	existing.FillCount += update.FillCountIncr
	existing.TotalFees = existing.TotalFees.Add(update.FeeDelta)
	if update.PeakValue.GreaterThan(existing.PeakValue) {
		existing.PeakValue = update.PeakValue
	}

	for i := range r.trades {
		if r.trades[i].ID == fill.TradeID && r.trades[i].Status == "pending" {
			r.trades[i].Status = fill.Status
			r.trades[i].Qty = fill.Qty
			r.trades[i].Price = fill.Price
			r.trades[i].Value = fill.Value
			r.trades[i].PnL = fill.PnL
			r.trades[i].ValueAfter = fill.ValueAfter
			r.trades[i].FillID = fill.FillID
			r.trades[i].TxHash = fill.TxHash
			r.trades[i].Fee = fill.Fee
			r.trades[i].FeeAsset = fill.FeeAsset
			r.trades[i].FeeValue = fill.FeeValue
			if fill.DecisionHash != "" {
				r.trades[i].DecisionHash = fill.DecisionHash
			}
			if fill.Evidence != nil {
				if r.trades[i].Evidence == nil {
					r.trades[i].Evidence = make(map[string]any)
				}
				for k, v := range fill.Evidence {
					r.trades[i].Evidence[k] = v
				}
			}
			if fill.Trace != nil {
				if r.traces == nil {
					r.traces = make(map[string]*DecisionTrace)
				}
				cp := *fill.Trace
				r.traces[fill.Trace.DecisionHash] = &cp
				r.latestTraceHash[fill.Trace.AgentID] = fill.Trace.DecisionHash
			}
			return nil
		}
	}
	return fmt.Errorf("trade %d already finalized or not pending", fill.TradeID)
}

func (r *MemRepository) GetFilledTradesWithoutEvidence(_ context.Context, agentID, missingKey string) ([]TradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []TradeRecord
	for _, t := range r.trades {
		if t.AgentID != agentID || t.Status != "fill" {
			continue
		}
		if t.Evidence == nil || t.Evidence[missingKey] == nil {
			result = append(result, t)
		}
	}
	return result, nil
}

func (r *MemRepository) GetFilledTradesPendingAttestation(_ context.Context, agentID string) ([]TradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []TradeRecord
	for _, t := range r.trades {
		if t.AgentID != agentID || t.Status != "fill" {
			continue
		}
		// Match four recoverable states:
		//   1. Null attestation (crash between FinalizeTrade and writeAttestationPending,
		//      or DB write failure during the inline path) - treat as "needs attempt"
		//   2. "pending" (crashed mid-flight or technical retry budget not yet spent)
		//   3. "pending_confirm" (tx landed on-chain but success DB write failed;
		//      recovery finalizes without another chain call)
		//   4. "waiting_for_gas" (previous attempt hit insufficient funds)
		att, ok := t.Evidence["attestation"].(map[string]any)
		if !ok {
			// No attestation key at all - recoverable.
			result = append(result, t)
			continue
		}
		status, _ := att["status"].(string)
		if status == "" || status == "pending" || status == "pending_confirm" || status == "waiting_for_gas" {
			result = append(result, t)
		}
	}
	return result, nil
}

func (r *MemRepository) UpdateEvidence(_ context.Context, tradeID int64, data map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.trades {
		if r.trades[i].ID == tradeID {
			if r.trades[i].Evidence == nil {
				r.trades[i].Evidence = make(map[string]any)
			}
			for k, v := range data {
				r.trades[i].Evidence[k] = v
			}
			return nil
		}
	}
	return fmt.Errorf("trade %d not found", tradeID)
}

func (r *MemRepository) GetPendingTrades(_ context.Context, agentID string) ([]TradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var pending []TradeRecord
	for _, t := range r.trades {
		if t.Status == "pending" && (agentID == "" || t.AgentID == agentID) {
			pending = append(pending, t)
		}
	}
	return pending, nil
}

func (r *MemRepository) GetFilledFillIDs(_ context.Context, agentID string) (map[string]struct{}, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make(map[string]struct{})
	for _, t := range r.trades {
		if t.AgentID == agentID && t.Status == "fill" && t.FillID != "" {
			ids[t.FillID] = struct{}{}
		}
	}
	return ids, nil
}

func (r *MemRepository) UpdateTradeHash(_ context.Context, fillID string, decisionHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.trades {
		if r.trades[i].FillID == fillID && r.trades[i].DecisionHash == "" {
			r.trades[i].DecisionHash = decisionHash
			return nil
		}
	}
	return nil
}

func (r *MemRepository) InsertTrace(_ context.Context, trace *DecisionTrace) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.traces[trace.DecisionHash]; exists {
		return nil // idempotent
	}
	cp := *trace
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	r.nextTraceSeq++
	r.traces[trace.DecisionHash] = &cp
	// Match pgx behavior: latest = highest seq (BIGSERIAL, monotonically increasing).
	r.latestTraceHash[trace.AgentID] = cp.DecisionHash
	return nil
}

func (r *MemRepository) GetTrace(_ context.Context, decisionHash string) (*DecisionTrace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.traces[decisionHash]
	if !ok {
		return nil, ErrTraceNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *MemRepository) GetLatestTraceHash(_ context.Context, agentID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hash := r.latestTraceHash[agentID] // "" if no traces yet
	return hash, nil
}

func (r *MemRepository) InsertTrade(_ context.Context, trade *TradeRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	t := *trade
	t.ID = r.nextID
	trade.ID = r.nextID // set on caller's struct so FinalizeTrade can find it
	if t.Timestamp.IsZero() {
		t.Timestamp = time.Now()
	}
	r.trades = append(r.trades, t)
	return nil
}

func (r *MemRepository) GetTradeHistory(_ context.Context, agentID string, limit int, pair, status string) ([]TradeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []TradeRecord
	for i := len(r.trades) - 1; i >= 0; i-- {
		t := r.trades[i]
		if t.AgentID != agentID {
			continue
		}
		if pair != "" && t.Pair != pair {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		result = append(result, t)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (r *MemRepository) GetDayStartValue(_ context.Context, agentID string) (decimal.Decimal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	// Walk trades in reverse (newest first), find first fill before today.
	for i := len(r.trades) - 1; i >= 0; i-- {
		t := r.trades[i]
		if t.AgentID != agentID || t.Status != "fill" || !t.ValueAfter.IsPositive() {
			continue
		}
		if t.Timestamp.Before(today) {
			return t.ValueAfter, nil
		}
	}
	return decimal.Zero, nil
}

func (r *MemRepository) GetRollingPeak24h(_ context.Context, agentID string) (decimal.Decimal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	since := time.Now().UTC().Add(-24 * time.Hour)
	peak := decimal.Zero
	for i := len(r.trades) - 1; i >= 0; i-- {
		t := r.trades[i]
		if t.AgentID != agentID || t.Status != "fill" || !t.ValueAfter.IsPositive() {
			continue
		}
		if t.Timestamp.Before(since) {
			continue // skip trades outside the 24h window (can't break - multi-agent shared slice)
		}
		if t.ValueAfter.GreaterThan(peak) {
			peak = t.ValueAfter
		}
	}
	return peak, nil
}

func (r *MemRepository) GetOpenPositions(_ context.Context, agentID string) ([]OpenPosition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type posAcc struct {
		qty       decimal.Decimal
		costBasis decimal.Decimal
	}
	acc := make(map[string]*posAcc)

	for _, t := range r.trades {
		if t.AgentID != agentID || t.Status != "fill" || t.Price.IsZero() {
			continue
		}
		p, ok := acc[t.Pair]
		if !ok {
			p = &posAcc{}
			acc[t.Pair] = p
		}
		if t.Side == "buy" {
			p.costBasis = p.costBasis.Add(t.Value)
			p.qty = p.qty.Add(t.Qty)
		} else {
			if p.qty.GreaterThan(threshold) {
				fraction := t.Qty.Div(p.qty)
				if fraction.GreaterThan(decimal.NewFromInt(1)) {
					fraction = decimal.NewFromInt(1)
				}
				p.costBasis = p.costBasis.Sub(p.costBasis.Mul(fraction)).Round(8)
			}
			p.qty = p.qty.Sub(t.Qty)
			if p.qty.LessThanOrEqual(threshold) {
				p.qty = decimal.Zero
				p.costBasis = decimal.Zero
			}
		}
	}

	var positions []OpenPosition
	for pair, p := range acc {
		if p.qty.LessThanOrEqual(threshold) {
			continue
		}
		pos := OpenPosition{
			Pair:  pair,
			Side:  "long",
			Qty:   p.qty,
			Value: p.costBasis,
		}
		if p.qty.IsPositive() {
			pos.AvgPrice = p.costBasis.Div(p.qty)
		}
		positions = append(positions, pos)
	}
	// Deterministic order: Go map iteration above is randomized.
	sort.Slice(positions, func(i, j int) bool { return positions[i].Pair < positions[j].Pair })
	return positions, nil
}

func (r *MemRepository) ComputeEquity(ctx context.Context, agentID string, prices map[string]decimal.Decimal) (decimal.Decimal, error) {
	state, err := r.GetAgent(ctx, agentID)
	if err != nil {
		return decimal.Zero, err
	}
	positions, err := r.GetOpenPositions(ctx, agentID)
	if err != nil {
		return decimal.Zero, err
	}

	equity := state.Cash
	for _, pos := range positions {
		if price, ok := prices[pos.Pair]; ok {
			equity = equity.Add(pos.Qty.Mul(price))
		} else {
			equity = equity.Add(pos.Value)
		}
	}
	return equity, nil
}

func (r *MemRepository) ComputeMetrics(ctx context.Context, agentID string, prices map[string]decimal.Decimal) (*ReputationMetrics, error) {
	state, err := r.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	equity, err := r.ComputeEquity(ctx, agentID, prices)
	if err != nil {
		return nil, err
	}
	trades, err := r.GetTradeHistory(ctx, agentID, 0, "", "fill")
	if err != nil {
		return nil, err
	}

	for i, j := 0, len(trades)-1; i < j; i, j = i+1, j-1 {
		trades[i], trades[j] = trades[j], trades[i]
	}

	return computeMetricsFromTrades(trades, state, equity), nil
}

func (r *MemRepository) copyState(s *AgentState) *AgentState {
	c := *s
	return &c
}

// ── Alert storage (in-memory, for tests) ──────────────────────────────────

func (r *MemRepository) UpsertAlert(_ context.Context, record *AlertRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.alerts == nil {
		r.alerts = make(map[string]*AlertRecord)
	}
	// Match Pgx behavior: only upsert if existing record is active (or new).
	if existing, ok := r.alerts[record.AlertID]; ok && existing.Status != "active" {
		return ErrAlertNotActive
	}
	cp := *record
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now()
	}
	r.alerts[record.AlertID] = &cp
	return nil
}

func (r *MemRepository) GetActiveAlerts(_ context.Context, agentID, service string) ([]AlertRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []AlertRecord
	for _, a := range r.alerts {
		if a.Status != "active" {
			continue
		}
		if agentID != "" && a.AgentID != agentID {
			continue
		}
		if service != "" && a.Service != service {
			continue
		}
		out = append(out, *a)
	}
	return out, nil
}

func (r *MemRepository) CountActiveAlerts(_ context.Context, agentID, service string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, a := range r.alerts {
		if a.Status != "active" {
			continue
		}
		if agentID != "" && a.AgentID != agentID {
			continue
		}
		if service != "" && a.Service != service {
			continue
		}
		count++
	}
	return count, nil
}

func (r *MemRepository) GetDueTimeAlerts(_ context.Context) ([]AlertRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now()
	var out []AlertRecord
	for _, a := range r.alerts {
		if a.Status != "active" || a.Service != "time" {
			continue
		}
		fireAtStr, _ := a.Params["fire_at"].(string)
		if fireAtStr == "" {
			continue
		}
		fireAt, err := time.Parse(time.RFC3339, fireAtStr)
		if err != nil {
			continue
		}
		if !fireAt.After(now) {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (r *MemRepository) MarkAlertTriggered(_ context.Context, alertID, triggeredPrice string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.alerts[alertID]
	if !ok || a.Status != "active" {
		return false, nil
	}
	now := time.Now()
	a.TriggeredAt = &now
	a.TriggerCount++
	if triggeredPrice != "" {
		if a.Params == nil {
			a.Params = make(map[string]any)
		}
		a.Params["triggered_price"] = triggeredPrice
	}
	if a.MaxTriggers > 0 && a.TriggerCount >= a.MaxTriggers {
		a.Status = "exhausted"
	}
	return true, nil
}

func (r *MemRepository) CancelAlert(_ context.Context, agentID, alertID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.alerts[alertID]
	if !ok {
		return fmt.Errorf("alert %s not found or not active", alertID)
	}
	if a.AgentID != agentID {
		return fmt.Errorf("alert %s not found or not active", alertID)
	}
	if a.Status != "active" && a.Status != "exhausted" {
		return fmt.Errorf("alert %s not found or not active (status=%s)", alertID, a.Status)
	}
	a.Status = "cancelled"
	return nil
}

func (r *MemRepository) GetTriggeredAlerts(_ context.Context, agentID string) ([]AlertRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []AlertRecord
	for _, a := range r.alerts {
		if a.AgentID != agentID || a.TriggeredAt == nil {
			continue
		}
		switch a.Status {
		case "active", "exhausted", "failed":
			out = append(out, *a)
		}
	}
	return out, nil
}

func (r *MemRepository) AckTriggeredAlerts(_ context.Context, alertIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range alertIDs {
		if a, ok := r.alerts[id]; ok {
			// Clear triggered_at so alert stops appearing in GetTriggeredAlerts.
			// Mirrors pgx: only clears triggered_at, never changes status.
			// Status transitions (active -> exhausted) happen in MarkAlertTriggered.
			a.TriggeredAt = nil
		}
	}
	return nil
}

func (r *MemRepository) RearmAlert(_ context.Context, alertID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.alerts[alertID]
	if !ok {
		return fmt.Errorf("alert %s not found", alertID)
	}
	a.Status = "active"
	a.TriggeredAt = nil
	return nil
}

func (r *MemRepository) FailAlert(_ context.Context, alertID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.alerts[alertID]
	if !ok {
		return fmt.Errorf("alert %s not found", alertID)
	}
	a.Status = "failed"
	if a.TriggeredAt == nil {
		now := time.Now()
		a.TriggeredAt = &now
	}
	if a.Params == nil {
		a.Params = make(map[string]any)
	}
	a.Params["fail_reason"] = reason
	return nil
}

func (r *MemRepository) GetFailedAlerts(_ context.Context, agentID, service string) ([]AlertRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []AlertRecord
	for _, a := range r.alerts {
		if a.Status == "failed" && a.AgentID == agentID && a.Service == service {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (r *MemRepository) CancelActiveAlertsForPair(_ context.Context, agentID, pair string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.alerts {
		if a.AgentID == agentID && a.Status == "active" {
			if p, _ := a.Params["pair"].(string); p == pair {
				a.Status = "cancelled"
			}
		}
	}
	return nil
}

func (r *MemRepository) CancelAlertsByGroup(_ context.Context, agentID, groupID, excludeAlertID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.alerts {
		if a.AgentID == agentID && a.GroupID == groupID && a.AlertID != excludeAlertID && a.Status == "active" {
			a.Status = "cancelled"
		}
	}
	return nil
}

func (r *MemRepository) RestoreCancelledSiblings(_ context.Context, agentID, groupID, excludeAlertID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.alerts {
		if a.AgentID == agentID && a.GroupID == groupID && a.AlertID != excludeAlertID && a.Status == "cancelled" {
			a.Status = "active"
			a.TriggeredAt = nil
		}
	}
	return nil
}
func (r *MemRepository) GetLatestBuyParams(_ context.Context, agentID, pair string) (map[string]any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.trades) - 1; i >= 0; i-- {
		t := r.trades[i]
		if t.AgentID == agentID && t.Pair == pair && t.Side == "buy" && t.Status == "fill" && t.Params != nil {
			return t.Params, nil
		}
	}
	return nil, nil
}
