package db

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// PgxRepository implements Repository using pgxpool.
type PgxRepository struct {
	pool *pgxpool.Pool
}

// NewPgxRepository creates a new pgx-backed repository.
func NewPgxRepository(pool *pgxpool.Pool) *PgxRepository {
	return &PgxRepository{pool: pool}
}

func (r *PgxRepository) LockAgent(ctx context.Context, agentID string) (func(), error) {
	// Acquire a dedicated connection (advisory locks are connection-scoped)
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire conn for lock: %w", err)
	}

	// Hash agent_id to int64 for pg_advisory_lock
	h := fnv.New64a()
	_, _ = h.Write([]byte(agentID))
	lockID := int64(h.Sum64())

	_, err = conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockID)
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("advisory lock for %s: %w", agentID, err)
	}

	var released bool
	unlock := func() {
		if released {
			return
		}
		released = true
		// Use Background context: the caller's ctx may be cancelled by the time
		// defer unlock() runs, but we must still release the advisory lock.
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID)
		conn.Release()
	}
	return unlock, nil
}

func (r *PgxRepository) GetOrCreateAgent(ctx context.Context, agentID string, initialCash decimal.Decimal) (*AgentState, error) {
	state, err := r.GetAgent(ctx, agentID)
	if err == nil {
		return state, nil
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO agent_state (agent_id, cash, initial_value, peak_value, fill_count, reject_count, halted)
		 VALUES ($1, $2, $2, $2, 0, 0, false)
		 ON CONFLICT (agent_id) DO NOTHING`,
		agentID, initialCash,
	)
	if err != nil {
		return nil, fmt.Errorf("insert agent: %w", err)
	}

	return r.GetAgent(ctx, agentID)
}

func (r *PgxRepository) GetAgent(ctx context.Context, agentID string) (*AgentState, error) {
	var s AgentState
	err := r.pool.QueryRow(ctx,
		`SELECT agent_id, cash, initial_value, peak_value, fill_count, reject_count, halted, total_fees, last_seen_at
		 FROM agent_state WHERE agent_id = $1`,
		agentID,
	).Scan(&s.AgentID, &s.Cash, &s.InitialValue, &s.PeakValue, &s.FillCount, &s.RejectCount, &s.Halted, &s.TotalFees, &s.LastSeenAt)
	if err != nil {
		return nil, fmt.Errorf("get agent %s: %w", agentID, err)
	}
	return &s, nil
}

func (r *PgxRepository) ListAgents(ctx context.Context) ([]*AgentState, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT agent_id, cash, initial_value, peak_value, fill_count, reject_count, halted, total_fees, last_seen_at
		 FROM agent_state
		 ORDER BY agent_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var out []*AgentState
	for rows.Next() {
		var s AgentState
		if err := rows.Scan(&s.AgentID, &s.Cash, &s.InitialValue, &s.PeakValue, &s.FillCount, &s.RejectCount, &s.Halted, &s.TotalFees, &s.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan agent row: %w", err)
		}
		out = append(out, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}
	return out, nil
}

func (r *PgxRepository) SetAgentHalted(ctx context.Context, agentID string, halted bool) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE agent_state SET halted = $2 WHERE agent_id = $1`,
		agentID, halted,
	)
	if err != nil {
		return fmt.Errorf("set agent halted: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found", agentID)
	}
	return nil
}

func (r *PgxRepository) UpdateLastSeen(ctx context.Context, agentID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE agent_state SET last_seen_at = NOW() WHERE agent_id = $1`,
		agentID,
	)
	if err != nil {
		return fmt.Errorf("update last seen: %w", err)
	}
	return nil
}

func (r *PgxRepository) UpdateAgentState(ctx context.Context, state *AgentState) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE agent_state
		 SET cash = $2, peak_value = GREATEST(peak_value, $3), fill_count = $4, reject_count = $5
		 WHERE agent_id = $1`,
		state.AgentID, state.Cash, state.PeakValue, state.FillCount, state.RejectCount,
	)
	if err != nil {
		return fmt.Errorf("update agent state: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found", state.AgentID)
	}
	return nil
}

func (r *PgxRepository) UpdatePeakValue(ctx context.Context, agentID string, peakValue decimal.Decimal) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE agent_state SET peak_value = $2 WHERE agent_id = $1 AND peak_value < $2`,
		agentID, peakValue,
	)
	if err != nil {
		return fmt.Errorf("update peak value: %w", err)
	}
	return nil
}

func (r *PgxRepository) NextNonce(ctx context.Context, agentID string) (uint64, error) {
	var nonce int64
	err := r.pool.QueryRow(ctx,
		`UPDATE agent_state SET chain_nonce = chain_nonce + 1 WHERE agent_id = $1 RETURNING chain_nonce - 1`,
		agentID,
	).Scan(&nonce)
	if err != nil {
		return 0, fmt.Errorf("next nonce for %s: %w", agentID, err)
	}
	return uint64(nonce), nil
}

func (r *PgxRepository) RecordTrade(ctx context.Context, update *StateUpdate, trade *TradeRecord) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ct, err := tx.Exec(ctx,
		`UPDATE agent_state
		 SET cash = cash + $2,
		     peak_value = GREATEST(peak_value, $3),
		     fill_count = fill_count + $4,
		     reject_count = reject_count + $5,
		     total_fees = total_fees + $6
		 WHERE agent_id = $1 AND cash + $2 >= 0`,
		update.AgentID, update.CashDelta, update.PeakValue, update.FillCountIncr, update.RejectIncr, update.FeeDelta,
	)
	if err != nil {
		return fmt.Errorf("update agent state: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found or cash would be negative", update.AgentID)
	}

	var paramsJSON []byte
	if trade.Params != nil {
		paramsJSON, err = json.Marshal(trade.Params)
		if err != nil {
			return fmt.Errorf("marshal trade params: %w", err)
		}
	}
	var evidenceJSON []byte
	if trade.Evidence != nil {
		evidenceJSON, err = json.Marshal(trade.Evidence)
		if err != nil {
			return fmt.Errorf("marshal trade evidence: %w", err)
		}
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO trades (agent_id, timestamp, pair, side, qty, price, value, pnl, status, value_after, decision_hash, fill_id, tx_hash, reason, fee, fee_asset, fee_value, params, evidence)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, COALESCE($19::jsonb, '{}'))`,
		trade.AgentID, trade.Timestamp, trade.Pair, trade.Side, trade.Qty,
		nilIfZeroDec(trade.Price), nilIfZeroDec(trade.Value), trade.PnL, trade.Status,
		nilIfZeroDec(trade.ValueAfter), nilIfEmpty(trade.DecisionHash),
		nilIfEmpty(trade.FillID), nilIfEmpty(trade.TxHash), nilIfEmpty(trade.Reason),
		trade.Fee, nilIfEmpty(trade.FeeAsset), trade.FeeValue, paramsJSON, evidenceJSON,
	)
	if err != nil {
		return fmt.Errorf("insert trade: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *PgxRepository) UpdateTradeHash(ctx context.Context, fillID string, decisionHash string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE trades SET decision_hash = $2 WHERE fill_id = $1 AND (decision_hash IS NULL OR decision_hash = '')`,
		fillID, decisionHash)
	return err
}

func (r *PgxRepository) InsertTrade(ctx context.Context, trade *TradeRecord) error {
	var paramsJSON []byte
	if trade.Params != nil {
		var err error
		paramsJSON, err = json.Marshal(trade.Params)
		if err != nil {
			return fmt.Errorf("marshal trade params: %w", err)
		}
	}
	var evidenceJSON []byte
	if trade.Evidence != nil {
		var err error
		evidenceJSON, err = json.Marshal(trade.Evidence)
		if err != nil {
			return fmt.Errorf("marshal trade evidence: %w", err)
		}
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO trades (agent_id, timestamp, pair, side, qty, price, value, pnl, status, value_after, decision_hash, fill_id, tx_hash, reason, fee, fee_asset, fee_value, params, evidence)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, COALESCE($19::jsonb, '{}'))
		 RETURNING id`,
		trade.AgentID, trade.Timestamp, trade.Pair, trade.Side, trade.Qty,
		nilIfZeroDec(trade.Price), nilIfZeroDec(trade.Value), trade.PnL, trade.Status,
		nilIfZeroDec(trade.ValueAfter), nilIfEmpty(trade.DecisionHash),
		nilIfEmpty(trade.FillID), nilIfEmpty(trade.TxHash), nilIfEmpty(trade.Reason),
		trade.Fee, nilIfEmpty(trade.FeeAsset), trade.FeeValue, paramsJSON, evidenceJSON,
	).Scan(&trade.ID)
	if err != nil {
		return fmt.Errorf("insert trade: %w", err)
	}
	return nil
}

func (r *PgxRepository) UpdateTradeStatus(ctx context.Context, tradeID int64, status, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE trades SET status = $2, reason = $3 WHERE id = $1`,
		tradeID, status, reason)
	return err
}

func (r *PgxRepository) RejectPendingTrade(ctx context.Context, agentID string, tradeID int64, reason string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ct, err := tx.Exec(ctx,
		`UPDATE trades SET status = 'reject', reason = $2 WHERE id = $1 AND status = 'pending'`,
		tradeID, reason)
	if err != nil {
		return fmt.Errorf("reject trade: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return nil // already resolved
	}

	_, err = tx.Exec(ctx,
		`UPDATE agent_state SET reject_count = reject_count + 1 WHERE agent_id = $1`,
		agentID)
	if err != nil {
		return fmt.Errorf("increment reject_count: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *PgxRepository) FinalizeTrade(ctx context.Context, update *StateUpdate, fill *TradeFillUpdate) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Update agent state deltas. Guard against negative cash.
	ct, err := tx.Exec(ctx,
		`UPDATE agent_state
		 SET cash = cash + $2,
		     peak_value = GREATEST(peak_value, $3),
		     fill_count = fill_count + $4,
		     total_fees = total_fees + $5
		 WHERE agent_id = $1 AND cash + $2 >= 0`,
		update.AgentID, update.CashDelta, update.PeakValue, update.FillCountIncr, update.FeeDelta,
	)
	if err != nil {
		return fmt.Errorf("update agent state: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found or cash would be negative", update.AgentID)
	}

	// Update pending trade to filled + merge evidence + backfill decision_hash.
	// Check RowsAffected to prevent double-apply.
	var evidenceJSON []byte
	if fill.Evidence != nil {
		evidenceJSON, err = json.Marshal(fill.Evidence)
		if err != nil {
			return fmt.Errorf("marshal evidence: %w", err)
		}
	}
	ct2, err := tx.Exec(ctx,
		`UPDATE trades SET status = $2, qty = $3, price = $4, value = $5, pnl = $6,
		 value_after = $7, fill_id = $8, tx_hash = $9, fee = $10, fee_asset = $11, fee_value = $12,
		 decision_hash = COALESCE(NULLIF($13, ''), decision_hash),
		 evidence = COALESCE(evidence, '{}') || COALESCE($14::jsonb, '{}')
		 WHERE id = $1 AND status = 'pending'`,
		fill.TradeID, fill.Status, fill.Qty, fill.Price, fill.Value, fill.PnL,
		fill.ValueAfter, fill.FillID, nilIfEmpty(fill.TxHash),
		fill.Fee, nilIfEmpty(fill.FeeAsset), fill.FeeValue,
		nilIfEmpty(fill.DecisionHash), evidenceJSON,
	)
	if err != nil {
		return fmt.Errorf("finalize trade: %w", err)
	}
	if ct2.RowsAffected() == 0 {
		// Trade already finalized (by reconciliation or concurrent call) - rollback state delta.
		return fmt.Errorf("trade %d already finalized or not pending", fill.TradeID)
	}

	// Insert decision trace in the same transaction.
	if fill.Trace != nil {
		_, traceErr := tx.Exec(ctx,
			`INSERT INTO decision_traces (decision_hash, agent_id, prev_hash, trace_json)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (decision_hash) DO NOTHING`,
			fill.Trace.DecisionHash, fill.Trace.AgentID, fill.Trace.PrevHash, fill.Trace.TraceJSON,
		)
		if traceErr != nil {
			return fmt.Errorf("insert trace: %w", traceErr)
		}
	}

	return tx.Commit(ctx)
}

func (r *PgxRepository) GetFilledFillIDs(ctx context.Context, agentID string) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT fill_id FROM trades WHERE agent_id = $1 AND status = 'fill' AND fill_id IS NOT NULL AND fill_id != ''`,
		agentID)
	if err != nil {
		return nil, fmt.Errorf("query filled fill_ids: %w", err)
	}
	defer rows.Close()

	ids := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}

func (r *PgxRepository) GetPendingTrades(ctx context.Context, agentID string) ([]TradeRecord, error) {
	var query string
	var args []any
	if agentID != "" {
		query = `SELECT id, agent_id, timestamp, pair, side, qty, status, params, evidence FROM trades WHERE agent_id = $1 AND status = 'pending' ORDER BY id`
		args = []any{agentID}
	} else {
		query = `SELECT id, agent_id, timestamp, pair, side, qty, status, params, evidence FROM trades WHERE status = 'pending' ORDER BY id`
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query pending trades: %w", err)
	}
	defer rows.Close()

	var trades []TradeRecord
	for rows.Next() {
		var t TradeRecord
		var paramsJSON, evidenceJSON []byte
		if err := rows.Scan(&t.ID, &t.AgentID, &t.Timestamp, &t.Pair, &t.Side, &t.Qty, &t.Status, &paramsJSON, &evidenceJSON); err != nil {
			return nil, fmt.Errorf("scan pending trade: %w", err)
		}
		if paramsJSON != nil {
			_ = json.Unmarshal(paramsJSON, &t.Params)
		}
		if evidenceJSON != nil {
			_ = json.Unmarshal(evidenceJSON, &t.Evidence)
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}

func (r *PgxRepository) GetTradeHistory(ctx context.Context, agentID string, limit int, pair, status string) ([]TradeRecord, error) {
	query := `SELECT id, agent_id, timestamp, pair, side, qty,
	                 COALESCE(price, 0), COALESCE(value, 0), pnl, status,
	                 COALESCE(value_after, 0), COALESCE(decision_hash, ''),
	                 COALESCE(fill_id, ''), COALESCE(tx_hash, ''), COALESCE(reason, ''),
	                 COALESCE(fee, 0), COALESCE(fee_asset, ''), COALESCE(fee_value, 0),
	                 params
	          FROM trades WHERE agent_id = $1`
	args := []any{agentID}
	argIdx := 2

	if pair != "" {
		query += fmt.Sprintf(" AND pair = $%d", argIdx)
		args = append(args, pair)
		argIdx++
	}
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	query += " ORDER BY timestamp DESC, id DESC"

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query trades: %w", err)
	}
	defer rows.Close()

	return scanTrades(rows)
}

func (r *PgxRepository) InsertTrace(ctx context.Context, trace *DecisionTrace) error {
	// Omit created_at - let Postgres DEFAULT NOW() assign it from the DB server clock.
	// App-node clocks can skew across instances, breaking ORDER BY created_at DESC
	// used by GetLatestTraceHash to find the chain predecessor.
	_, err := r.pool.Exec(ctx,
		`INSERT INTO decision_traces (decision_hash, agent_id, prev_hash, trace_json)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (decision_hash) DO NOTHING`,
		trace.DecisionHash, trace.AgentID, trace.PrevHash, trace.TraceJSON,
	)
	if err != nil {
		return fmt.Errorf("insert trace: %w", err)
	}
	return nil
}

func (r *PgxRepository) GetFilledTradesWithoutEvidence(ctx context.Context, agentID, missingKey string) ([]TradeRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, timestamp, pair, side,
		        COALESCE(qty, 0), COALESCE(price, 0), COALESCE(value, 0), COALESCE(pnl, 0),
		        status, COALESCE(value_after, 0),
		        COALESCE(decision_hash, ''), COALESCE(fill_id, ''), COALESCE(tx_hash, ''),
		        COALESCE(reason, ''), COALESCE(fee, 0), COALESCE(fee_asset, ''), COALESCE(fee_value, 0),
		        params, evidence
		 FROM trades
		 WHERE agent_id = $1 AND status = 'fill'
		   AND (evidence IS NULL OR NOT evidence ? $2)
		 ORDER BY id ASC`,
		agentID, missingKey,
	)
	if err != nil {
		return nil, fmt.Errorf("query trades without evidence key %s: %w", missingKey, err)
	}
	return scanTradesWithEvidence(rows)
}

func (r *PgxRepository) GetFilledTradesPendingAttestation(ctx context.Context, agentID string) ([]TradeRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, timestamp, pair, side,
		        COALESCE(qty, 0), COALESCE(price, 0), COALESCE(value, 0), COALESCE(pnl, 0),
		        status, COALESCE(value_after, 0),
		        COALESCE(decision_hash, ''), COALESCE(fill_id, ''), COALESCE(tx_hash, ''),
		        COALESCE(reason, ''), COALESCE(fee, 0), COALESCE(fee_asset, ''), COALESCE(fee_value, 0),
		        params, evidence
		 FROM trades
		 WHERE agent_id = $1 AND status = 'fill'
		   AND (evidence->'attestation'->>'status' IS NULL
		        OR evidence->'attestation'->>'status' = 'pending'
		        OR evidence->'attestation'->>'status' = 'pending_confirm'
		        OR evidence->'attestation'->>'status' = 'waiting_for_gas')
		 ORDER BY id ASC`,
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("query trades with recoverable attestation state: %w", err)
	}
	return scanTradesWithEvidence(rows)
}

// scanTradesWithEvidence drains a rows iterator into TradeRecord slice for
// queries that also select the params+evidence JSONB columns (20 columns total).
// Owns closing the rows iterator.
func scanTradesWithEvidence(rows pgx.Rows) ([]TradeRecord, error) {
	defer rows.Close()

	var trades []TradeRecord
	for rows.Next() {
		var t TradeRecord
		var paramsJSON, evidenceJSON []byte
		if err := rows.Scan(
			&t.ID, &t.AgentID, &t.Timestamp, &t.Pair, &t.Side, &t.Qty, &t.Price, &t.Value, &t.PnL,
			&t.Status, &t.ValueAfter, &t.DecisionHash, &t.FillID, &t.TxHash, &t.Reason,
			&t.Fee, &t.FeeAsset, &t.FeeValue, &paramsJSON, &evidenceJSON,
		); err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		if paramsJSON != nil {
			_ = json.Unmarshal(paramsJSON, &t.Params)
		}
		if evidenceJSON != nil {
			_ = json.Unmarshal(evidenceJSON, &t.Evidence)
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}

func (r *PgxRepository) UpdateEvidence(ctx context.Context, tradeID int64, data map[string]any) error {
	evidenceJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	ct, err := r.pool.Exec(ctx,
		`UPDATE trades SET evidence = COALESCE(evidence, '{}') || $2::jsonb WHERE id = $1`,
		tradeID, evidenceJSON,
	)
	if err != nil {
		return fmt.Errorf("update evidence: %w", err)
	}
	if ct.RowsAffected() == 0 {
		// Matches MemRepository behaviour. Silently succeeding on a missing
		// row masks correctness bugs: for example, the attestation state
		// machine uses the return of writeAttestationPending as a precondition
		// for sending the on-chain tx - a silent no-op would let a tx fire
		// without any DB marker, making the trade unrecoverable after a crash.
		return fmt.Errorf("update evidence: trade %d not found", tradeID)
	}
	return nil
}

func (r *PgxRepository) GetTrace(ctx context.Context, decisionHash string) (*DecisionTrace, error) {
	var t DecisionTrace
	err := r.pool.QueryRow(ctx,
		`SELECT decision_hash, agent_id, prev_hash, trace_json, created_at
		 FROM decision_traces WHERE decision_hash = $1`,
		decisionHash,
	).Scan(&t.DecisionHash, &t.AgentID, &t.PrevHash, &t.TraceJSON, &t.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrTraceNotFound
		}
		return nil, fmt.Errorf("get trace %s: %w", decisionHash, err)
	}
	return &t, nil
}

func (r *PgxRepository) GetLatestTraceHash(ctx context.Context, agentID string) (string, error) {
	var hash string
	err := r.pool.QueryRow(ctx,
		`SELECT decision_hash FROM decision_traces
		 WHERE agent_id = $1 ORDER BY seq DESC LIMIT 1`,
		agentID,
	).Scan(&hash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil // no traces yet - not an error
		}
		return "", fmt.Errorf("get latest trace hash: %w", err)
	}
	return hash, nil
}

func (r *PgxRepository) GetDayStartValue(ctx context.Context, agentID string) (decimal.Decimal, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var val decimal.Decimal
	err := r.pool.QueryRow(ctx,
		`SELECT value_after FROM trades
		 WHERE agent_id = $1 AND status = 'fill' AND value_after IS NOT NULL AND timestamp < $2
		 ORDER BY timestamp DESC, id DESC LIMIT 1`,
		agentID, today,
	).Scan(&val)
	if err != nil {
		if err == pgx.ErrNoRows {
			return decimal.Zero, nil
		}
		return decimal.Zero, fmt.Errorf("get day start value: %w", err)
	}
	return val, nil
}

func (r *PgxRepository) GetRollingPeak24h(ctx context.Context, agentID string) (decimal.Decimal, error) {
	since := time.Now().UTC().Add(-24 * time.Hour)
	var val decimal.Decimal
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(value_after), 0) FROM trades
		 WHERE agent_id = $1 AND status = 'fill' AND value_after IS NOT NULL AND timestamp > $2`,
		agentID, since,
	).Scan(&val)
	if err != nil {
		return decimal.Zero, fmt.Errorf("get rolling peak 24h: %w", err)
	}
	return val, nil
}

func (r *PgxRepository) GetOpenPositions(ctx context.Context, agentID string) ([]OpenPosition, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT pair, side, qty, value
		 FROM trades
		 WHERE agent_id = $1 AND status = 'fill' AND price IS NOT NULL
		 ORDER BY timestamp ASC, id ASC`,
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("query trades for positions: %w", err)
	}
	defer rows.Close()

	type posAcc struct {
		qty       decimal.Decimal
		costBasis decimal.Decimal
	}
	acc := make(map[string]*posAcc)

	for rows.Next() {
		var pair, side string
		var qty, value decimal.Decimal
		if err := rows.Scan(&pair, &side, &qty, &value); err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		p, ok := acc[pair]
		if !ok {
			p = &posAcc{}
			acc[pair] = p
		}
		if side == "buy" {
			p.costBasis = p.costBasis.Add(value)
			p.qty = p.qty.Add(qty)
		} else {
			if p.qty.GreaterThan(threshold) {
				fraction := qty.Div(p.qty)
				if fraction.GreaterThan(decimal.NewFromInt(1)) {
					fraction = decimal.NewFromInt(1)
				}
				p.costBasis = p.costBasis.Sub(p.costBasis.Mul(fraction)).Round(8)
			}
			p.qty = p.qty.Sub(qty)
			if p.qty.LessThanOrEqual(threshold) {
				p.qty = decimal.Zero
				p.costBasis = decimal.Zero
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
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

func (r *PgxRepository) ComputeEquity(ctx context.Context, agentID string, prices map[string]decimal.Decimal) (decimal.Decimal, error) {
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

func (r *PgxRepository) ComputeMetrics(ctx context.Context, agentID string, prices map[string]decimal.Decimal) (*ReputationMetrics, error) {
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

	reverseTradeRecords(trades)

	return computeMetricsFromTrades(trades, state, equity), nil
}

func scanTrades(rows pgx.Rows) ([]TradeRecord, error) {
	var trades []TradeRecord
	for rows.Next() {
		var t TradeRecord
		var paramsJSON []byte
		if err := rows.Scan(
			&t.ID, &t.AgentID, &t.Timestamp, &t.Pair, &t.Side, &t.Qty,
			&t.Price, &t.Value, &t.PnL, &t.Status,
			&t.ValueAfter, &t.DecisionHash, &t.FillID, &t.TxHash, &t.Reason,
			&t.Fee, &t.FeeAsset, &t.FeeValue,
			&paramsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		if len(paramsJSON) > 0 {
			if err := json.Unmarshal(paramsJSON, &t.Params); err != nil {
				return nil, fmt.Errorf("unmarshal trade params: %w", err)
			}
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}

func reverseTradeRecords(trades []TradeRecord) {
	for i, j := 0, len(trades)-1; i < j; i, j = i+1, j-1 {
		trades[i], trades[j] = trades[j], trades[i]
	}
}

func nilIfZeroDec(v decimal.Decimal) any {
	if v.IsZero() {
		return nil
	}
	return v
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ── Alert methods ────────────────────────────────────────────────────────────

func (r *PgxRepository) UpsertAlert(ctx context.Context, alert *AlertRecord) error {
	params, err := marshalJSON(alert.Params)
	if err != nil {
		return fmt.Errorf("marshal alert params: %w", err)
	}
	// Only re-arm alerts that are still 'active'. Triggered/exhausted/cancelled alerts
	// are NOT reset by a duplicate upsert - prevents resurrecting a consumed stop-loss.
	// If the conflicting row exists but is not active, this is a no-op and returns
	// ErrAlertNotActive so callers can generate a new alert ID or inform the agent.
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO alerts
			(alert_id, agent_id, service, status, on_trigger, max_triggers, trigger_count,
			 params, triage_prompt, note, group_id, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
		ON CONFLICT (alert_id) DO UPDATE SET
			on_trigger    = EXCLUDED.on_trigger,
			max_triggers  = EXCLUDED.max_triggers,
			params        = EXCLUDED.params,
			note          = EXCLUDED.note,
			triage_prompt = EXCLUDED.triage_prompt,
			group_id      = EXCLUDED.group_id,
			expires_at    = EXCLUDED.expires_at
		WHERE alerts.status = 'active'`,
		alert.AlertID, alert.AgentID, alert.Service,
		coalesce(alert.Status, "active"),
		coalesce(alert.OnTrigger, "wake_full"),
		alert.MaxTriggers, 0,
		params, alert.TriagePrompt, alert.Note,
		nilIfEmpty(alert.GroupID),
		alert.ExpiresAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertNotActive
	}
	return nil
}

func (r *PgxRepository) GetActiveAlerts(ctx context.Context, agentID, service string) ([]AlertRecord, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if agentID == "" {
		// Return active alerts for ALL agents of the given service (used by pollers).
		rows, err = r.pool.Query(ctx, `
			SELECT alert_id, agent_id, service, status, on_trigger, max_triggers, trigger_count,
			       params, triage_prompt, note, COALESCE(group_id, ''), expires_at, created_at, triggered_at
			FROM alerts
			WHERE service=$1 AND status='active'
			  AND (expires_at IS NULL OR expires_at > NOW())`, service)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT alert_id, agent_id, service, status, on_trigger, max_triggers, trigger_count,
			       params, triage_prompt, note, COALESCE(group_id, ''), expires_at, created_at, triggered_at
			FROM alerts
			WHERE agent_id=$1 AND service=$2 AND status='active'
			  AND (expires_at IS NULL OR expires_at > NOW())`, agentID, service)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func (r *PgxRepository) CountActiveAlerts(ctx context.Context, agentID, service string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts
		 WHERE agent_id=$1 AND service=$2 AND status='active'
		   AND (expires_at IS NULL OR expires_at > NOW())`,
		agentID, service).Scan(&n)
	return n, err
}

func (r *PgxRepository) GetDueTimeAlerts(ctx context.Context) ([]AlertRecord, error) {
	// For time alerts, allow expires_at >= fire_at (agent often sets them equal).
	// Use a 1-minute grace to avoid missing alerts at the exact boundary.
	rows, err := r.pool.Query(ctx, `
		SELECT alert_id, agent_id, service, status, on_trigger, max_triggers, trigger_count,
		       params, triage_prompt, note, COALESCE(group_id, ''), expires_at, created_at, triggered_at
		FROM alerts
		WHERE service='time' AND status='active'
		  AND (expires_at IS NULL OR expires_at > NOW() - INTERVAL '1 minute')
		  AND (params->>'fire_at') IS NOT NULL
		  AND (params->>'fire_at')::timestamptz <= NOW()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func (r *PgxRepository) MarkAlertTriggered(ctx context.Context, alertID, triggeredPrice string) (bool, error) {
	var tag interface{ RowsAffected() int64 }
	var err error
	if triggeredPrice != "" {
		// Merge triggered_price into params so the agent can see where it fired.
		tag, err = r.pool.Exec(ctx, `
			UPDATE alerts SET
				triggered_at  = NOW(),
				trigger_count = trigger_count + 1,
				params        = params || jsonb_build_object('triggered_price', $2::text),
				status = CASE
					WHEN max_triggers > 0 AND trigger_count + 1 >= max_triggers THEN 'exhausted'
					ELSE status
				END
			WHERE alert_id = $1 AND status = 'active'`, alertID, triggeredPrice)
	} else {
		tag, err = r.pool.Exec(ctx, `
			UPDATE alerts SET
				triggered_at  = NOW(),
				trigger_count = trigger_count + 1,
				status = CASE
					WHEN max_triggers > 0 AND trigger_count + 1 >= max_triggers THEN 'exhausted'
					ELSE status
				END
			WHERE alert_id = $1 AND status = 'active'`, alertID)
	}
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PgxRepository) CancelAlert(ctx context.Context, agentID, alertID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status='cancelled' WHERE alert_id=$1 AND agent_id=$2 AND status IN ('active','exhausted')`,
		alertID, agentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("alert %s not found or not active (may be already triggered/exhausted/cancelled)", alertID)
	}
	return nil
}

func (r *PgxRepository) GetTriggeredAlerts(ctx context.Context, agentID string) ([]AlertRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT alert_id, agent_id, service, status, on_trigger, max_triggers, trigger_count,
		       params, triage_prompt, note, COALESCE(group_id, ''), expires_at, created_at, triggered_at
		FROM alerts
		WHERE agent_id=$1 AND triggered_at IS NOT NULL AND status IN ('active','exhausted','failed')
		ORDER BY triggered_at ASC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func (r *PgxRepository) AckTriggeredAlerts(ctx context.Context, alertIDs []string) error {
	if len(alertIDs) == 0 {
		return nil
	}
	// Clear triggered_at so alerts stop appearing in GetTriggeredAlerts.
	// Works for all statuses including 'failed' - the agent sees the alert once,
	// then it disappears. Failed alerts stay in 'failed' status (won't re-trigger
	// since checkPositionAlerts only fires 'active' alerts).
	_, err := r.pool.Exec(ctx,
		`UPDATE alerts SET triggered_at=NULL WHERE alert_id = ANY($1)`,
		alertIDs)
	return err
}

func (r *PgxRepository) RearmAlert(ctx context.Context, alertID string) error {
	// Match 'exhausted' (max_triggers=1 used up) and 'active' with triggered_at set (max_triggers=0 pending).
	// The triggered_at guard on 'active' prevents spurious rearming of an alert that wasn't triggered.
	_, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status='active', triggered_at=NULL, trigger_count=GREATEST(trigger_count-1, 0)
		 WHERE alert_id=$1 AND (status='exhausted' OR (status='active' AND triggered_at IS NOT NULL))`, alertID)
	return err
}

func (r *PgxRepository) FailAlert(ctx context.Context, alertID, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE alerts
		 SET status='failed',
		     triggered_at=COALESCE(triggered_at, NOW()),
		     params=jsonb_set(COALESCE(params, '{}'), '{fail_reason}', to_jsonb($2::text))
		 WHERE alert_id=$1`, alertID, reason)
	return err
}

func (r *PgxRepository) GetFailedAlerts(ctx context.Context, agentID, service string) ([]AlertRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT alert_id, agent_id, service, status, on_trigger, max_triggers, trigger_count,
		       params, triage_prompt, note, COALESCE(group_id, ''), expires_at, created_at, triggered_at
		FROM alerts
		WHERE agent_id=$1 AND service=$2 AND status='failed'`, agentID, service)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func (r *PgxRepository) CancelActiveAlertsForPair(ctx context.Context, agentID, pair string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status='cancelled'
		 WHERE agent_id=$1 AND service='trading' AND status='active' AND params->>'pair'=$2`, agentID, pair)
	return err
}

func (r *PgxRepository) GetLatestBuyParams(ctx context.Context, agentID, pair string) (map[string]any, error) {
	var paramsJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT params FROM trades
		 WHERE agent_id=$1 AND pair=$2 AND side='buy' AND status='fill' AND params IS NOT NULL
		 ORDER BY timestamp DESC LIMIT 1`, agentID, pair).Scan(&paramsJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // no prior buy with params - not an error
		}
		return nil, err
	}
	if len(paramsJSON) == 0 {
		return nil, nil
	}
	var params map[string]any
	if err := json.Unmarshal(paramsJSON, &params); err != nil {
		return nil, err
	}
	return params, nil
}

func (r *PgxRepository) CancelAlertsByGroup(ctx context.Context, agentID, groupID, excludeAlertID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status='cancelled'
		 WHERE agent_id=$1 AND status='active' AND group_id=$2 AND alert_id!=$3`, agentID, groupID, excludeAlertID)
	return err
}

func (r *PgxRepository) RestoreCancelledSiblings(ctx context.Context, agentID, groupID, excludeAlertID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE alerts SET status='active', triggered_at=NULL
		 WHERE agent_id=$1 AND status='cancelled' AND group_id=$2 AND alert_id!=$3`, agentID, groupID, excludeAlertID)
	return err
}

func scanAlerts(rows pgx.Rows) ([]AlertRecord, error) {
	var result []AlertRecord
	for rows.Next() {
		var a AlertRecord
		var paramsJSON []byte
		if err := rows.Scan(
			&a.AlertID, &a.AgentID, &a.Service, &a.Status, &a.OnTrigger,
			&a.MaxTriggers, &a.TriggerCount, &paramsJSON,
			&a.TriagePrompt, &a.Note, &a.GroupID, &a.ExpiresAt, &a.CreatedAt, &a.TriggeredAt,
		); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		if err := unmarshalJSON(paramsJSON, &a.Params); err != nil {
			return nil, fmt.Errorf("unmarshal alert params: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func coalesce(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func marshalJSON(v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(v)
}

func unmarshalJSON(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
