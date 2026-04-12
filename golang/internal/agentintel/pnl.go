package agentintel

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

var feeRate = decimal.RequireFromString(TakerFeeRate) // 0.0026

// Calculator computes FIFO PnL for all agents.
type Calculator struct {
	paths Paths
	log   *zap.Logger
}

// NewCalculator creates a PnL calculator.
func NewCalculator(paths Paths, log *zap.Logger) *Calculator {
	return &Calculator{paths: paths, log: log}
}

// CalculateAll runs PnL calculation for all agents and produces computed output.
func (c *Calculator) CalculateAll() error {
	ts, err := LoadBlockTimestamps(c.paths)
	if err != nil {
		return fmt.Errorf("load timestamps: %w", err)
	}

	// Load all market data once (shared across agents).
	marketData, err := c.loadMarketData()
	if err != nil {
		return fmt.Errorf("load market data: %w", err)
	}

	// Load meta for known agent IDs.
	meta, err := LoadMeta(c.paths)
	if err != nil {
		return fmt.Errorf("load meta: %w", err)
	}

	// Calculate per agent — all reads scoped to one agent's directory.
	var allAgents []ComputedAgent
	for _, agentID := range meta.KnownAgentIDs {
		var agent Agent
		if found, _ := LoadJSON(c.paths.AgentInfo(agentID), &agent); !found {
			continue
		}

		// Load per-agent state snapshot (scores, counts, vault).
		// Missing file = zero state, which is fine for an agent that never had
		// the state phase run yet. Corrupt file = log warning, use zeros — the
		// user's hackathon score will be wrong until the next state refresh.
		var state AgentState
		if _, err := LoadJSON(c.paths.AgentState(agentID), &state); err != nil {
			c.log.Warn("Failed to load agent state, using zeros",
				zap.Int64("agent", agentID), zap.Error(err))
		}

		// Load per-agent event files.
		agentIntents, _ := ReadJSONL[TradeIntent](c.paths.AgentIntents(agentID))
		agentIntents = dedup(agentIntents, func(t TradeIntent) string {
			return fmt.Sprintf("%s:%d", t.TxHash, t.LogIndex)
		})
		agentApprovals, _ := ReadJSONL[TradeOutcome](c.paths.AgentApprovals(agentID))
		agentApprovals = dedup(agentApprovals, func(t TradeOutcome) string {
			return fmt.Sprintf("%s:%d", t.TxHash, t.LogIndex)
		})
		agentRejections, _ := ReadJSONL[TradeOutcome](c.paths.AgentRejections(agentID))
		agentRejections = dedup(agentRejections, func(t TradeOutcome) string {
			return fmt.Sprintf("%s:%d", t.TxHash, t.LogIndex)
		})

		// Build per-agent outcome lookup.
		outcomes := make(map[string]TradeOutcome)
		for _, a := range agentApprovals {
			outcomes[a.IntentHash] = a
		}
		for _, r := range agentRejections {
			outcomes[r.IntentHash] = r
		}

		// Assign timestamps and normalize pair.
		for i := range agentIntents {
			key := fmt.Sprintf("%d", agentIntents[i].Block)
			if t, ok := ts[key]; ok {
				agentIntents[i].Timestamp = t
			}
			if agentIntents[i].CanonicalPair == "" {
				agentIntents[i].CanonicalPair = CanonicalPair(agentIntents[i].Pair)
			}
		}
		sort.Slice(agentIntents, func(i, j int) bool {
			if agentIntents[i].Block != agentIntents[j].Block {
				return agentIntents[i].Block < agentIntents[j].Block
			}
			return agentIntents[i].LogIndex < agentIntents[j].LogIndex
		})

		computed := c.calculateAgent(agent, agentIntents, outcomes, marketData)
		computed.State = state

		// Load per-agent attestations (append-only event log with tx-calldata notes).
		attestations, _ := ReadJSONL[Attestation](c.paths.AgentAttestations(agentID))
		attestations = dedup(attestations, func(a Attestation) string {
			return fmt.Sprintf("%s:%d", a.TxHash, a.LogIndex)
		})
		// Hydrate attestation timestamps from the block cache (events don't carry them).
		for i := range attestations {
			if attestations[i].Timestamp == 0 && attestations[i].Block > 0 {
				if t, ok := ts[fmt.Sprintf("%d", attestations[i].Block)]; ok {
					attestations[i].Timestamp = t
				}
			}
		}
		computed.Attestations = attestations

		// Load per-agent reputation feedback.
		reputation, _ := ReadJSONL[ReputationFeedback](c.paths.AgentReputation(agentID))
		reputation = dedup(reputation, func(r ReputationFeedback) string {
			return fmt.Sprintf("%s:%d", r.TxHash, r.LogIndex)
		})
		computed.Reputation = reputation

		// Use on-chain snapshot state for summary fields.
		computed.Summary.AttestationCount = state.AttestationCount
		computed.Summary.AvgValidationScore = state.ValidationScore
		computed.Summary.AvgReputationScore = state.ReputationScore

		// Reputation feedback stats for gaming detection.
		computed.Summary.RepFeedbackCount = len(reputation)
		uniqueValidators := make(map[string]bool)
		for _, r := range reputation {
			uniqueValidators[r.Validator] = true
		}
		computed.Summary.RepUniqueValidators = len(uniqueValidators)

		// Hackathon leaderboard score (organizer formula, 2026-04-10).
		computeHackathonScore(&computed.Summary, state)

		// Detect gaming.
		computed.Summary.GamingFlags = detectGaming(computed)

		// Save per-agent computed data.
		outPath := filepath.Join(c.paths.Computed, "agents", fmt.Sprintf("%d.json", agentID))
		if err := SaveJSON(outPath, computed); err != nil {
			return fmt.Errorf("save computed agent %d: %w", agentID, err)
		}

		allAgents = append(allAgents, computed)
		c.log.Info("Calculated PnL",
			zap.Int64("agent", agentID),
			zap.String("name", agent.Name),
			zap.Int("trades", len(agentIntents)),
			zap.String("net_pnl", computed.Summary.NetPnL),
		)
	}

	// Save leaderboard.
	if err := SaveJSON(filepath.Join(c.paths.Computed, "agents.json"), allAgents); err != nil {
		return fmt.Errorf("save leaderboard: %w", err)
	}

	return nil
}

// calculateAgent runs FIFO PnL for one agent's trades using FIFOTracker.
func (c *Calculator) calculateAgent(agent Agent, intents []TradeIntent, outcomes map[string]TradeOutcome, marketData map[string][]Candle) ComputedAgent {
	result := ComputedAgent{Agent: agent}

	trackers := make(map[string]*FIFOTracker) // canonical pair -> tracker
	initialCash := decimal.NewFromInt(10000) // Kraken paper init balance
	cash := initialCash
	minCash := initialCash // tracks the lowest cash_after value, for max exposure computation
	totalRealizedPnL := decimal.Zero
	totalFees := decimal.Zero
	winCount, lossCount := 0, 0
	approvedCount, rejectedCount := 0, 0
	pairsTraded := make(map[string]bool)
	pairPnLs := make(map[string]*pairPnLAccum)

	for i, intent := range intents {
		amountUSD := decimal.NewFromInt(intent.AmountUSDScaled).Div(decimal.NewFromInt(100))

		// Determine outcome.
		outcome := "UNKNOWN"
		rejectReason := ""
		if o, ok := outcomes[intent.IntentHash]; ok {
			if o.Approved {
				outcome = "APPROVED"
				approvedCount++
			} else {
				outcome = "REJECTED"
				rejectedCount++
				rejectReason = o.Reason
			}
		}

		// Get market price. Always re-canonicalize (stored values may be stale).
		cp := CanonicalPair(intent.Pair)
		pairsTraded[cp] = true
		price := c.getPrice(marketData, cp, intent.Timestamp)

		// Initialize pair accumulator.
		if pairPnLs[cp] == nil {
			pairPnLs[cp] = &pairPnLAccum{}
		}
		pairPnLs[cp].trades++

		ct := ComputedTrade{
			Index:         i,
			Block:         intent.Block,
			Timestamp:     time.Unix(intent.Timestamp, 0).UTC().Format(time.RFC3339),
			Pair:          intent.Pair,
			CanonicalPair: cp,
			Action:        intent.Action,
			AmountUSD:     amountUSD.StringFixed(2),
			MarketPrice:   price.StringFixed(2),
			FeeRate:       TakerFeeRate,
			Outcome:       outcome,
			RejectReason:  rejectReason,
		}

		// Only include APPROVED trades in PnL calculation.
		// The RiskRouter is the gate: approval -> Kraken execution -> trade happened.
		// UNKNOWN = missing approval event (sync gap) - exclude until fixed.
		// REJECTED = router blocked the trade - definitely did not execute.
		if outcome != "APPROVED" || price.IsZero() {
			ct.Qty = "0"
			ct.FeeUSD = "0.00"
			if tracker := trackers[cp]; tracker != nil {
				ct.PositionBefore = tracker.Position().StringFixed(6)
				ct.PositionAfter = tracker.Position().StringFixed(6)
			} else {
				ct.PositionBefore = "0"
				ct.PositionAfter = "0"
			}
			ct.CashAfter = cash.StringFixed(2)
			ct.EquityAfter = "-"
			ct.CumulativePnL = totalRealizedPnL.StringFixed(2)
			ct.PortfolioAfter = snapshotTrackers(trackers)
			result.Trades = append(result.Trades, ct)
			continue
		}

		qty := amountUSD.Div(price)
		ct.Qty = qty.StringFixed(6)

		if trackers[cp] == nil {
			trackers[cp] = NewFIFOTracker(feeRate)
		}
		tracker := trackers[cp]

		// Record position before trade.
		ct.PositionBefore = tracker.Position().StringFixed(6)

		action := strings.ToUpper(intent.Action)
		isOpen := action == "BUY" || action == "LONG"
		// CLOSE:+X.XX / CLOSE:-X.XX are close-position actions from Triumvirate (treated as SELL).
		isClose := action == "SELL" || action == "SHORT" || strings.HasPrefix(action, "CLOSE:")

		var tr TradeResult
		if isOpen {
			tr = tracker.Buy(qty, price)
			pairPnLs[cp].buyVol = pairPnLs[cp].buyVol.Add(amountUSD)
		} else if isClose {
			tr = tracker.Sell(qty, price)
			pairPnLs[cp].sellVol = pairPnLs[cp].sellVol.Add(amountUSD)
		} else {
			c.log.Warn("Unknown trade action, skipping PnL",
				zap.Int64("agent", agent.ID),
				zap.String("action", intent.Action),
				zap.String("pair", intent.Pair),
				zap.Int64("block", intent.Block),
			)
		}

		ct.FeeUSD = tr.FeeUSD.StringFixed(2)
		totalFees = totalFees.Add(tr.FeeUSD)

		if tr.IsClosing || tr.IsShortOpen {
			pnlStr := tr.RealizedPnL.StringFixed(2)
			if tr.IsShortOpen {
				pnlStr = "SHORT_OPEN"
			}
			ct.RealizedPnL = &pnlStr
			totalRealizedPnL = totalRealizedPnL.Add(tr.RealizedPnL)
			pairPnLs[cp].realizedPnL = pairPnLs[cp].realizedPnL.Add(tr.RealizedPnL)

			if tr.RealizedPnL.IsPositive() {
				winCount++
			} else if tr.RealizedPnL.IsNegative() {
				lossCount++
			}
		}

		// Update cash. The full qty trades on the exchange regardless of
		// whether part opens a new position (reversal). Fee is on full amount.
		tradeUSD := qty.Mul(price)
		if isOpen {
			cash = cash.Sub(tradeUSD).Sub(tr.FeeUSD) // pay price + fee
		} else if isClose {
			cash = cash.Add(tradeUSD).Sub(tr.FeeUSD) // receive price - fee
		}

		// Track lowest cash_after for max-exposure computation (peak deployed capital).
		if cash.LessThan(minCash) {
			minCash = cash
		}

		// Compute equity = cash + sum(position_qty * current_price).
		equity := cash
		for pair, trk := range trackers {
			pos := trk.Position()
			if !pos.IsZero() {
				p := c.getPrice(marketData, pair, intent.Timestamp)
				if p.IsPositive() {
					equity = equity.Add(pos.Mul(p))
				}
			}
		}

		ct.PositionAfter = tracker.Position().StringFixed(6)
		ct.CashAfter = cash.StringFixed(2)
		ct.EquityAfter = equity.StringFixed(2)
		ct.CumulativePnL = totalRealizedPnL.StringFixed(2)
		ct.PortfolioAfter = snapshotTrackers(trackers)
		result.Trades = append(result.Trades, ct)
	}

	// Calculate unrealized PnL for open positions.
	totalUnrealized := decimal.Zero
	for cp, tracker := range trackers {
		pos := tracker.Position()
		if pos.IsZero() {
			continue
		}
		candles := marketData[cp]
		if len(candles) == 0 {
			continue
		}
		lastPrice, _ := decimal.NewFromString(candles[len(candles)-1].Close)
		if !lastPrice.IsPositive() {
			continue
		}

		if pos.IsPositive() {
			// Long: unrealized = position * lastPrice - totalCost.
			marketValue := pos.Mul(lastPrice)
			unrealized := marketValue.Sub(tracker.TotalCost())
			totalUnrealized = totalUnrealized.Add(unrealized)
			if pairPnLs[cp] != nil {
				pairPnLs[cp].openQty = pos
				pairPnLs[cp].unrealized = unrealized
			}
		} else {
			// Short: unrealized = totalCost - |position| * lastPrice.
			marketValue := pos.Abs().Mul(lastPrice)
			unrealized := tracker.TotalCost().Sub(marketValue)
			totalUnrealized = totalUnrealized.Add(unrealized)
			if pairPnLs[cp] != nil {
				pairPnLs[cp].openQty = pos
				pairPnLs[cp].unrealized = unrealized
			}
		}
	}

	// Fees charged in USD (quote): buy increases cost basis, sell reduces proceeds.
	// totalFees is tracked separately for display only.
	netPnL := totalRealizedPnL.Add(totalUnrealized)

	// Build pairs list.
	var pairsList []string
	for p := range pairsTraded {
		pairsList = append(pairsList, p)
	}
	sort.Strings(pairsList)

	// Build per-pair PnL map.
	pairPnLMap := make(map[string]PairPnL)
	for cp, acc := range pairPnLs {
		pairPnLMap[cp] = PairPnL{
			Trades:      acc.trades,
			BuyVolume:   acc.buyVol.StringFixed(2),
			SellVolume:  acc.sellVol.StringFixed(2),
			RealizedPnL: acc.realizedPnL.StringFixed(2),
			OpenQty:     acc.openQty.StringFixed(6),
			Unrealized:  acc.unrealized.StringFixed(2),
		}
	}

	// Compute win rate and avg profit.
	winRatePct := decimal.Zero
	avgTradeProfit := decimal.Zero
	totalClosingTrades := winCount + lossCount
	if totalClosingTrades > 0 {
		winRatePct = decimal.NewFromInt(int64(winCount)).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromInt(int64(totalClosingTrades)))
		avgTradeProfit = totalRealizedPnL.Div(decimal.NewFromInt(int64(totalClosingTrades)))
	}

	// Peak deployed capital = initial cash - lowest cash_after.
	// For disciplined agents: reflects actual capital at risk at peak.
	// For paper-leveraged agents (cash went negative): reflects the blowup.
	maxExposure := initialCash.Sub(minCash)
	if maxExposure.IsNegative() {
		maxExposure = decimal.Zero // agent only added cash (impossible on $10K paper, but defensive)
	}

	result.Summary = AgentSummary{
		TotalTrades:    len(intents),
		ApprovedTrades: approvedCount,
		RejectedTrades: rejectedCount,
		MaxExposure:    maxExposure.StringFixed(2),
		TotalFees:      totalFees.StringFixed(2),
		RealizedPnL:    totalRealizedPnL.StringFixed(2),
		UnrealizedPnL:  totalUnrealized.StringFixed(2),
		NetPnL:         netPnL.StringFixed(2),
		ReturnPct: func() string {
			// Return on initial capital ($10K). Clean comparison across agents.
			return netPnL.Mul(decimal.NewFromInt(100)).Div(initialCash).StringFixed(2)
		}(),
		PairsTraded:    pairsList,
		WinCount:       winCount,
		LossCount:      lossCount,
		WinRatePct:     winRatePct.StringFixed(1),
		AvgTradeProfit: avgTradeProfit.StringFixed(2),
		PnLByPair:      pairPnLMap,
	}
	return result
}

type pairPnLAccum struct {
	trades      int
	buyVol      decimal.Decimal
	sellVol     decimal.Decimal
	realizedPnL decimal.Decimal
	openQty     decimal.Decimal
	unrealized  decimal.Decimal
}

// getPrice finds the VWAP of the candle covering the trade execution time.
//
// The on-chain timestamp is when the intent was posted. The actual Kraken execution
// happens at or after this time. We use the candle starting at or after the trade
// timestamp - this is the minute during which execution most likely occurred.
//
// Why VWAP: represents the average execution price during that minute.
func (c *Calculator) getPrice(marketData map[string][]Candle, pair string, ts int64) decimal.Decimal {
	candles := marketData[pair]
	if len(candles) == 0 {
		return decimal.Zero
	}

	// Find the candle whose minute contains the trade timestamp.
	// Candle T = start of minute. Trade at ts falls into the candle where T <= ts < T+interval.
	// Prefer the containing/next candle (execution happens at or after trade time).
	// Fall back to previous candle if trade is after all available candles.

	tsMinute := (ts / 60) * 60
	idx := sort.Search(len(candles), func(i int) bool {
		return candles[i].T >= tsMinute
	})

	best := -1
	if idx < len(candles) {
		best = idx // candle at or after trade minute
	} else if idx > 0 {
		best = idx - 1 // trade is after all candles - use the most recent one
	}

	if best < 0 {
		return decimal.Zero
	}

	// Reject if the candle is more than 10 minutes away from the trade.
	dist := candles[best].T - ts
	if dist < 0 {
		dist = -dist
	}
	if dist > 600 {
		return decimal.Zero
	}

	// Prefer VWAP; fall back to Close if VWAP is missing.
	candle := candles[best]
	if candle.VWAP != "" {
		price, err := decimal.NewFromString(candle.VWAP)
		if err == nil && price.IsPositive() {
			return price
		}
	}
	price, _ := decimal.NewFromString(candle.Close)
	return price
}

// snapshotTrackers creates a position snapshot from FIFOTrackers.
func snapshotTrackers(trackers map[string]*FIFOTracker) map[string]Position {
	result := make(map[string]Position)
	for pair, tracker := range trackers {
		if !tracker.IsFlat() {
			result[pair] = Position{
				Qty:       tracker.Position().StringFixed(6), // negative = short
				AvgCost:   tracker.AvgCost().StringFixed(4),
				TotalCost: tracker.TotalCost().StringFixed(2),
			}
		}
	}
	return result
}

// loadMarketData reads all candle files from raw/marketdata/.
func (c *Calculator) loadMarketData() (map[string][]Candle, error) {
	result := make(map[string][]Candle)

	pattern := filepath.Join(c.paths.Raw, "marketdata", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		base := filepath.Base(f)
		pair := base[:len(base)-len(".jsonl")]
		candles, err := ReadJSONL[Candle](f)
		if err != nil {
			c.log.Warn("Failed to read candles", zap.String("pair", pair), zap.Error(err))
			continue
		}
		// Stable sort by timestamp so appended (newer) candles come after older ones
		// for the same timestamp. Then dedup keeps LAST = most complete candle.
		sort.SliceStable(candles, func(i, j int) bool { return candles[i].T < candles[j].T })
		deduped := candles[:0]
		for i := range candles {
			if i == len(candles)-1 || candles[i].T != candles[i+1].T {
				deduped = append(deduped, candles[i])
			}
		}
		result[pair] = deduped
	}
	return result, nil
}

// dedup removes duplicate records based on a key function, preserving order.
func dedup[T any](items []T, key func(T) string) []T {
	seen := make(map[string]bool)
	var result []T
	for _, item := range items {
		k := key(item)
		if !seen[k] {
			seen[k] = true
			result = append(result, item)
		}
	}
	return result
}

// computeHackathonScore implements the organizer's leaderboard formula (confirmed 2026-04-10):
//
//	validation_half = (avg_validation_score * 0.5)      // 0-50 pts
//	                + (min(approved_trades, 10) * 3)    // 0-30 pts
//	                + (vault_claimed ? 10 : 0)          // 0-10 pts
//	                + (attestations > 0 ? 10 : 0)       // 0-10 pts (activity bonus)
//	final = validation_half * 0.5 + avg_reputation_score * 0.5   // 0-100
func computeHackathonScore(s *AgentSummary, state AgentState) {
	activityPts := s.ApprovedTrades
	if activityPts > 10 {
		activityPts = 10
	}
	activityPts *= 3

	vaultPts := 0
	if state.VaultClaimed {
		vaultPts = 10
	}

	checkpointPts := 0
	if s.AttestationCount > 0 {
		checkpointPts = 10
	}

	validationHalf := s.AvgValidationScore/2 + activityPts + vaultPts + checkpointPts
	if validationHalf > 100 {
		validationHalf = 100
	}

	final := (validationHalf + s.AvgReputationScore) / 2

	s.HackathonActivityPts = activityPts
	s.HackathonVaultPts = vaultPts
	s.HackathonCheckpointPts = checkpointPts
	s.HackathonValidation = validationHalf
	s.HackathonScore = final
}

// detectGaming identifies gaming patterns.
func detectGaming(agent ComputedAgent) []string {
	var flags []string

	// Attestation spam: >100 attestations from multiple validators (not the contract itself).
	// A single validator (typically the ValidationRegistry contract) posting many attestations
	// just means the agent traded a lot - that's not gaming.
	if len(agent.Attestations) > 100 {
		attValidators := make(map[string]bool)
		for _, a := range agent.Attestations {
			attValidators[a.Validator] = true
		}
		if len(attValidators) > 1 {
			flags = append(flags, fmt.Sprintf("attestation_spam (%d attestations from %d validators)", len(agent.Attestations), len(attValidators)))
		}
	}

	// Reputation sybil: many unique validators each posting once.
	if agent.Summary.RepFeedbackCount > 10 && agent.Summary.RepUniqueValidators == agent.Summary.RepFeedbackCount {
		flags = append(flags, fmt.Sprintf("reputation_sybil (%d unique wallets)", agent.Summary.RepUniqueValidators))
	}

	// Stablecoin padding: >40% volume in stablecoin pairs.
	stablePairs := map[string]bool{"USDTUSD": true, "USDCUSD": true, "DAIUSD": true}
	stableVol := decimal.Zero
	totalVol := decimal.Zero
	for pair, ppnl := range agent.Summary.PnLByPair {
		bv, _ := decimal.NewFromString(ppnl.BuyVolume)
		sv, _ := decimal.NewFromString(ppnl.SellVolume)
		vol := bv.Add(sv)
		totalVol = totalVol.Add(vol)
		if stablePairs[pair] {
			stableVol = stableVol.Add(vol)
		}
	}
	if totalVol.IsPositive() && stableVol.Div(totalVol).GreaterThan(decimal.RequireFromString("0.4")) {
		pct := stableVol.Div(totalVol).Mul(decimal.NewFromInt(100)).IntPart()
		flags = append(flags, fmt.Sprintf("stablecoin_padding (%d%% volume)", pct))
	}

	return flags
}
