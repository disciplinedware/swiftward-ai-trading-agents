package trading

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"ai-trading-agents/internal/chain"
	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/evidence"
	"ai-trading-agents/internal/exchange"
	"ai-trading-agents/internal/marketdata"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/observability"
	"ai-trading-agents/internal/platform"
	"ai-trading-agents/internal/swiftward"
)

type contextKey string

const agentIDContextKey contextKey = "agent_id"

// Trade status constants - used in MCP responses, DB records, and event data.
const (
	StatusPending = "pending" // write-ahead: inserted before exchange call
	StatusFill    = "fill"
	StatusReject  = "reject"
)

// Reject source constants.
const (
	RejectSourcePolicy = "policy"
	RejectSourceRouter = "router"
)

// policyEvaluator is the interface used to call Swiftward for policy evaluation.
// Defined here to allow test stubs without pulling in gRPC.
type policyEvaluator interface {
	EvaluateSync(ctx context.Context, stream, entityID, eventType string, eventData map[string]any) (*swiftward.EvalResult, error)
	EvaluateAsync(ctx context.Context, stream, entityID, eventType string, eventData map[string]any) (string, error)
}

// Service implements the Trading MCP - agent-facing tools for trading.
type Service struct {
	svcCtx       *platform.ServiceContext
	log          *zap.Logger
	orderLog     *zap.Logger
	alertLog     *zap.Logger
	chainLog     *zap.Logger
	policyLog    *zap.Logger
	reconcileLog *zap.Logger
	exchange     exchange.Client
	repo         db.Repository
	agents       map[string]*config.AgentConfig
	apiKeys      map[string]string // api_key -> agent_id

	// Swiftward policy evaluation (nil if disabled)
	evaluator      policyEvaluator
	evaluatorClose func() error // non-nil when evaluator needs cleanup (i.e. concrete *swiftward.Evaluator)

	// Chain (nil if chain not configured)
	chainClient   *chain.Client
	riskRouter    *chain.RiskRouter
	identityReg   *chain.IdentityRegistry
	validationReg *chain.ValidationRegistry

	// Agent wallet keys for on-chain signing (agent_id -> key)
	agentKeys map[string]*agentKeyEntry

	// Hardcoded confidence (0-100) for validation checkpoints. 0 = use agent's.
	hardcodedConfidence int
	// Max reasoning length in attestation notes. 0 = no truncation.
	maxNotesReasoning int
	// Circuit breaker: agents with on-chain attestation failures skip attestation
	// for the rest of this process lifetime. Reset only on restart.
	attestationDisabled sync.Map // agent_id -> struct{} (presence = disabled)

	// Evidence API server
	evidenceSrv *http.Server

	// Market data source for position alert polling (nil = use exchange.GetPrices())
	marketSource      marketdata.DataSource
	alertPollInterval time.Duration

	// Tier 1 native exchange stops (disabled by default)
	enableNativeStops bool
}

// isAttestationDisabled checks if attestation is disabled for an agent.
// Circuit breaker is per-process and permanent: once tripped (insufficient
// funds or not authorized), it stays tripped until the process restarts.
// That guarantees we never hammer the chain with failing txs in a single run,
// and recovery on next startup is the single retry boundary.
func (s *Service) isAttestationDisabled(agentID string) bool {
	_, ok := s.attestationDisabled.Load(agentID)
	return ok
}

// Attestation lifecycle states stored in trades.evidence.attestation.status.
//
// Non-terminal (recovery picks up):
//   - pending:         pre-tx durable marker. Carries attempt_count (technical retries).
//   - waiting_for_gas: last attempt hit insufficient funds; retried on next restart, no cap.
//   - pending_confirm: tx was successfully posted on-chain but the DB write
//     recording success failed. Recovery re-writes success
//     from the stored tx_hash - NO chain call.
//
// Terminal (recovery leaves alone):
//   - success:         confirmed on chain + DB updated.
//   - error:           gave up (non-technical failure, 3 technical retries exhausted,
//     or unrecoverable state like missing hash).
//   - disabled:        agent never eligible (ValidationRegistry missing, no keys, legacy trade).
const (
	attestationStatusPending        = "pending"
	attestationStatusWaitingForGas  = "waiting_for_gas"
	attestationStatusPendingConfirm = "pending_confirm"
	attestationStatusSuccess        = "success"
	attestationStatusError          = "error"
	attestationStatusDisabled       = "disabled"
)

// maxAttestationAttempts caps consecutive technical retries before a trade
// is marked terminal error. Gas failures (waiting_for_gas) are NOT counted.
const maxAttestationAttempts = 3

// attestationFailureKind classifies a chain call failure for the retry decision.
type attestationFailureKind int

const (
	// failureKindWaitingForGas: insufficient funds. Retry forever on restart.
	failureKindWaitingForGas attestationFailureKind = iota
	// failureKindTechnical: transient network/RPC/node problem. Retry up to
	// maxAttestationAttempts, then mark error.
	failureKindTechnical
	// failureKindTerminal: unrecoverable (unauthorized, revert, bad signature,
	// malformed request). Mark error immediately.
	failureKindTerminal
)

// technicalErrorPatterns are substrings in the lowercased error message that
// indicate a transient failure worth retrying. Anything NOT matching is
// treated as terminal (no point retrying a contract revert or auth error).
// Shared between the attestation classifier and the gate-retry helper
// (RiskRouter / Swiftward) so all three paths agree on what "technical" means.
var technicalErrorPatterns = []string{
	"timeout",
	"deadline exceeded",
	"connection refused",
	"connection reset",
	"broken pipe",
	"eof",
	"i/o timeout",
	"no such host",
	"dial tcp",
	"tls handshake",
	"network is unreachable",
	"server misbehaving",
	"temporary failure",
	"too many requests",
	"429",
	"502 bad gateway",
	"503 service",
	"504 gateway",
	"unavailable",      // grpc UNAVAILABLE
	"context canceled", // upstream cancelled mid-call, usually transport
}

// isTechnicalError reports whether an error looks transient (network, RPC
// rate limit, transport reset). Used by the gate-retry helper AND the
// attestation classifier so "technical" has a single definition.
func isTechnicalError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, pat := range technicalErrorPatterns {
		if strings.Contains(msg, pat) {
			return true
		}
	}
	return false
}

// classifyAttestationFailure returns the retry decision for a chain error.
// Match is case-insensitive because different RPC proxies format the same
// underlying condition with different capitalization.
func classifyAttestationFailure(err error) attestationFailureKind {
	if err == nil {
		return failureKindTerminal
	}
	if strings.Contains(strings.ToLower(err.Error()), "insufficient funds") {
		return failureKindWaitingForGas
	}
	if isTechnicalError(err) {
		return failureKindTechnical
	}
	return failureKindTerminal
}

// maxGateAttempts caps the number of gate calls (Swiftward eval, RiskRouter
// nonce/submit) per trade. Technical errors trigger exponential backoff
// between attempts; terminal errors fail immediately.
const maxGateAttempts = 3

// withTechnicalRetry runs fn and retries on technical errors with exponential
// backoff (2s, 4s, 8s). Terminal errors and success return immediately.
// On exhaustion, returns the last error wrapped with the label.
//
// Used for Swiftward and RiskRouter gate calls: fail-closed means "reject
// the trade on error", but a single 429 should not take down every trade in
// a cycle, so we retry transient errors the same way the chain client does
// internally on PendingNonceAt/HeaderByNumber.
func withTechnicalRetry[T any](
	ctx context.Context,
	log *zap.Logger,
	label string,
	fn func(context.Context) (T, error),
) (T, error) {
	var zero T
	var lastErr error
	for attempt := 1; attempt <= maxGateAttempts; attempt++ {
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isTechnicalError(err) {
			// Terminal - don't retry.
			return zero, err
		}
		if attempt >= maxGateAttempts {
			break
		}
		backoff := time.Duration(1<<attempt) * time.Second // 2s, 4s, 8s
		log.Warn("gate call technical error, backing off",
			zap.String("call", label),
			zap.Int("attempt", attempt),
			zap.Duration("backoff", backoff),
			zap.Error(err),
		)
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return zero, fmt.Errorf("%s: %w", label, ctx.Err())
		}
	}
	return zero, fmt.Errorf("%s: %d attempts exhausted: %w", label, maxGateAttempts, lastErr)
}

// attestationFailureKindLabel maps the retry decision to the evidence status
// string. Used to populate the HTTP response's "attestation" field for the
// inline path - operators should see the state that was written, not a
// generic "failed".
func attestationFailureKindLabel(kind attestationFailureKind, exhausted bool) string {
	switch kind {
	case failureKindWaitingForGas:
		return attestationStatusWaitingForGas
	case failureKindTechnical:
		if exhausted {
			return attestationStatusError
		}
		return attestationStatusPending
	default:
		return attestationStatusError
	}
}

// isAttestationCircuitBreakerError reports whether an error should trip the
// per-process circuit breaker (insufficient funds OR not an authorized validator).
// Case-insensitive, single source of truth so the inline, recovery, and
// end_cycle paths don't drift.
func isAttestationCircuitBreakerError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "insufficient funds") || strings.Contains(msg, "not an authorized validator")
}

// readAttestationState extracts the current status and technical attempt_count
// from trade evidence. attempt_count only tracks consecutive pending (technical)
// retries - WFG failures are a separate track and do NOT consume the 3-retry
// budget. For any non-pending state the returned count is 0.
func readAttestationState(evidence map[string]any) (status string, attemptCount int) {
	if evidence == nil {
		return "", 0
	}
	att, ok := evidence["attestation"].(map[string]any)
	if !ok {
		return "", 0
	}
	status, _ = att["status"].(string)
	if status != attestationStatusPending {
		return status, 0
	}
	// JSON unmarshal produces float64 for numbers.
	if n, ok := att["attempt_count"].(float64); ok {
		return status, int(n)
	}
	if n, ok := att["attempt_count"].(int); ok {
		return status, n
	}
	return status, 0
}

// readPendingConfirmData pulls the durable tx_hash / score / checkpoint_hash
// from a pending_confirm attestation object so recovery can finalize the
// trade without re-calling the chain.
func readPendingConfirmData(evidence map[string]any) (score int, cpHashHex, txHashHex string, ok bool) {
	att, attOk := evidence["attestation"].(map[string]any)
	if !attOk {
		return 0, "", "", false
	}
	txHashHex, _ = att["tx_hash"].(string)
	cpHashHex, _ = att["checkpoint_hash"].(string)
	if n, nOk := att["score"].(float64); nOk {
		score = int(n)
	} else if n, nOk := att["score"].(int); nOk {
		score = n
	}
	ok = txHashHex != "" && cpHashHex != ""
	return
}

// updateEvidenceWithRetry wraps repo.UpdateEvidence with a small retry loop.
// Attestation state writes must not silently fail: losing a 'success' write
// after a successful on-chain tx leaves the trade looking 'pending' forever
// and causes double-posting on next restart. 3 attempts with linear backoff
// covers transient DB hiccups without getting fancy. The backoff sleep is
// ctx-aware so a cancelled caller doesn't wait out the full budget. Caller
// propagates the final error and decides what to do with it.
func (s *Service) updateEvidenceWithRetry(ctx context.Context, tradeID int64, data map[string]any) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := s.repo.UpdateEvidence(ctx, tradeID, data); err != nil {
			lastErr = err
			if ctx.Err() != nil {
				// Preserve the underlying DB error in the message so operators
				// can distinguish "shutdown during healthy DB call" from "DB
				// was actually broken AND we were shutting down". %w still
				// wraps ctx.Err() so errors.Is(err, context.Canceled) works.
				return fmt.Errorf("update evidence during shutdown (db err: %v): %w", err, ctx.Err())
			}
			if attempt < maxAttempts {
				backoff := time.Duration(attempt) * 200 * time.Millisecond
				timer := time.NewTimer(backoff)
				select {
				case <-timer.C:
					// Timer fired naturally; channel already drained by the receive.
				case <-ctx.Done():
					// Canonical drain: Stop() returns false if the timer already fired
					// and the value is still in the channel. Drain it to avoid a
					// lingering channel buffer.
					if !timer.Stop() {
						<-timer.C
					}
					return fmt.Errorf("update evidence: %w", ctx.Err())
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("update evidence after %d attempts: %w", maxAttempts, lastErr)
}

// writeAttestationPending marks a trade as mid-flight with the technical
// attempt counter. MUST be called synchronously before PostEIP712Attestation.
// If this write fails, the caller MUST NOT proceed with the on-chain tx: a tx
// sent without a durable pending marker cannot be recovered after a crash.
//
// attemptCount is the 1-based "this is the Nth technical attempt" counter.
// errMsg is empty for pre-tx markers and non-empty when the caller is
// rewriting pending after a technical failure to capture the last error.
func (s *Service) writeAttestationPending(ctx context.Context, tradeID int64, attemptCount int, errMsg string) error {
	att := map[string]any{
		"status":          attestationStatusPending,
		"attempt_count":   attemptCount,
		"last_attempt_at": time.Now().UTC().Format(time.RFC3339),
	}
	if errMsg != "" {
		att["error"] = errMsg
	}
	return s.updateEvidenceWithRetry(ctx, tradeID, map[string]any{"attestation": att})
}

// writeAttestationPendingConfirm is the durable fallback when the on-chain tx
// succeeded but writeAttestationSuccess failed (DB transient error). The
// tx_hash is recorded so recovery can finalize the trade WITHOUT another
// chain call, eliminating the restart double-post.
func (s *Service) writeAttestationPendingConfirm(ctx context.Context, tradeID int64, score int, cpHashHex, txHashHex string) error {
	return s.updateEvidenceWithRetry(ctx, tradeID, map[string]any{
		"attestation": map[string]any{
			"status":          attestationStatusPendingConfirm,
			"score":           score,
			"checkpoint_hash": cpHashHex,
			"tx_hash":         txHashHex,
			"posted_at":       time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// writeAttestationSuccess records a confirmed on-chain attestation.
func (s *Service) writeAttestationSuccess(ctx context.Context, tradeID int64, score int, cpHashHex, txHashHex string) error {
	return s.updateEvidenceWithRetry(ctx, tradeID, map[string]any{
		"attestation": map[string]any{
			"status":          attestationStatusSuccess,
			"score":           score,
			"checkpoint_hash": cpHashHex,
			"tx_hash":         txHashHex,
			"posted_at":       time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// writeAttestationWaitingForGas records a recoverable failure (insufficient funds).
// Recovery loop picks these up on next restart once the wallet is topped up.
// Gas retries are UNLIMITED - this state is NOT subject to the technical
// retry cap (maxAttestationAttempts). gasAttemptCount is observability only:
// how many gas-failed attempts this trade has accumulated. Pass 0 when the
// chain was never actually called (circuit breaker blocked the attempt).
func (s *Service) writeAttestationWaitingForGas(ctx context.Context, tradeID int64, gasAttemptCount int, errMsg string) error {
	att := map[string]any{
		"status":          attestationStatusWaitingForGas,
		"error":           errMsg,
		"last_attempt_at": time.Now().UTC().Format(time.RFC3339),
	}
	if gasAttemptCount > 0 {
		att["gas_attempt_count"] = gasAttemptCount
	}
	return s.updateEvidenceWithRetry(ctx, tradeID, map[string]any{"attestation": att})
}

// previousGasAttemptCount reads the WFG observability counter.
func previousGasAttemptCount(evidence map[string]any) int {
	att, ok := evidence["attestation"].(map[string]any)
	if !ok {
		return 0
	}
	if n, ok := att["gas_attempt_count"].(float64); ok {
		return int(n)
	}
	if n, ok := att["gas_attempt_count"].(int); ok {
		return n
	}
	return 0
}

// writeAttestationError records a terminal failure (unauthorized, bad sig, contract revert).
// Never retried - requires code or contract-level fix.
func (s *Service) writeAttestationError(ctx context.Context, tradeID int64, errMsg string) error {
	return s.updateEvidenceWithRetry(ctx, tradeID, map[string]any{
		"attestation": map[string]any{
			"status":    attestationStatusError,
			"error":     errMsg,
			"failed_at": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// writeAttestationDisabled marks a trade that was never attested because
// ValidationRegistry wasn't configured at fill time. Never retried.
func (s *Service) writeAttestationDisabled(ctx context.Context, tradeID int64, reason string) error {
	return s.updateEvidenceWithRetry(ctx, tradeID, map[string]any{
		"attestation": map[string]any{
			"status": attestationStatusDisabled,
			"reason": reason,
		},
	})
}

// attestationAttemptResult summarizes the outcome of attemptAttestation for
// the HTTP response (inline path) or the recovery log (recovery path).
type attestationAttemptResult struct {
	// statusStr is the attestation.status written to evidence. One of:
	// success, pending_confirm, pending, waiting_for_gas, error.
	statusStr string
	// txHashHex is set on success or pending_confirm.
	txHashHex string
	// err is the chain error, if any. nil on success.
	err error
}

// attemptAttestation runs one on-chain attestation attempt for a filled trade
// and writes the resulting state-machine transition to evidence. Shared by
// inline trade submission and startup recovery so the retry/classification
// logic is IDENTICAL in both paths.
//
// The caller MUST have already verified:
//   - s.validationReg != nil
//   - agent circuit breaker is NOT tripped
//   - entry has a key and tokenID
//   - cpHash / score / notes are built
//
// Parameters:
//   - dbCtx: context for evidence DB writes (caller's request ctx).
//   - chainCtx: context for PostEIP712Attestation. The inline path uses
//     context.Background + 90s timeout so HTTP client disconnect does NOT
//     abort a tx in flight; the recovery path passes the reconcile ctx.
//   - currentStatus: current attestation.status from evidence ("" for a fresh trade).
//   - currentAttemptCount: current pending.attempt_count (0 for non-pending state).
//
// State machine (same in both paths):
//
//	currentStatus == pending && currentAttemptCount >= max   -> write error, skip chain
//	pre-tx write pending(next)                               -> chain call
//	tx success                                               -> write success
//	  (success DB write fails)                               -> write pending_confirm(tx_hash)
//	failureKindWaitingForGas                                 -> write WFG (no cap), trip breaker
//	failureKindTechnical && next < max                       -> rewrite pending(next, error) for next restart
//	failureKindTechnical && next >= max                      -> write error (exhausted)
//	failureKindTerminal                                      -> write error (+ maybe trip breaker)
func (s *Service) attemptAttestation(
	dbCtx, chainCtx context.Context,
	log *zap.Logger,
	tradeID int64,
	agentID string,
	entry *agentKeyEntry,
	cpHash [32]byte,
	score uint8,
	notes string,
	currentStatus string,
	currentAttemptCount int,
	currentGasAttemptCount int,
) attestationAttemptResult {
	// Cap check: a prior crash/retry sequence already burned all technical
	// attempts. Mark terminal error without another chain call.
	if currentStatus == attestationStatusPending && currentAttemptCount >= maxAttestationAttempts {
		errMsg := fmt.Sprintf("exhausted %d technical retries without success", maxAttestationAttempts)
		if writeErr := s.writeAttestationError(dbCtx, tradeID, errMsg); writeErr != nil {
			log.Error("Failed to mark attestation error after retry exhaustion",
				zap.Int64("trade_id", tradeID), zap.Error(writeErr))
		}
		return attestationAttemptResult{statusStr: attestationStatusError, err: fmt.Errorf("%s", errMsg)}
	}

	// Next technical attempt number. currentAttemptCount is 0 for non-pending
	// states (fresh trades, WFG pivots, null evidence), so the first call
	// always starts at 1.
	nextAttemptCount := currentAttemptCount + 1

	// Pre-tx durable marker: a crash between this write and the post-tx
	// write leaves the counter bumped so recovery can't loop forever.
	if err := s.writeAttestationPending(dbCtx, tradeID, nextAttemptCount, ""); err != nil {
		log.Error("Skipping attestation: failed to record pending marker",
			zap.Int64("trade_id", tradeID), zap.Error(err))
		return attestationAttemptResult{
			statusStr: attestationStatusError,
			err:       fmt.Errorf("db unreachable for pending marker: %w", err),
		}
	}

	txHash, attestErr := s.validationReg.PostEIP712Attestation(chainCtx, entry.key, entry.tokenID, cpHash, score, notes)

	if attestErr == nil {
		cpHashHex := fmt.Sprintf("0x%x", cpHash)
		txHashHex := txHash.Hex()
		if writeErr := s.writeAttestationSuccess(dbCtx, tradeID, int(score), cpHashHex, txHashHex); writeErr != nil {
			// CRITICAL path: tx confirmed on-chain, DB update failed.
			// Write a durable pending_confirm marker so recovery can finalize
			// from the stored tx_hash instead of re-posting (the bug we're
			// fixing). If this ALSO fails, log everything needed for manual
			// reconciliation.
			log.Error("attestation tx succeeded but failed to record success in DB - writing pending_confirm fallback",
				zap.Int64("trade_id", tradeID),
				zap.String("tx_hash", txHashHex),
				zap.String("checkpoint_hash", cpHashHex),
				zap.Int("score", int(score)),
				zap.Error(writeErr))
			if confirmErr := s.writeAttestationPendingConfirm(dbCtx, tradeID, int(score), cpHashHex, txHashHex); confirmErr != nil {
				log.Error("CRITICAL: attestation tx succeeded but BOTH success AND pending_confirm writes failed - manual reconciliation required",
					zap.Int64("trade_id", tradeID),
					zap.String("tx_hash", txHashHex),
					zap.String("checkpoint_hash", cpHashHex),
					zap.Int("score", int(score)),
					zap.Error(confirmErr))
				return attestationAttemptResult{
					statusStr: attestationStatusPending,
					txHashHex: txHashHex,
					err:       fmt.Errorf("success write + pending_confirm fallback both failed: %w", confirmErr),
				}
			}
			return attestationAttemptResult{
				statusStr: attestationStatusPendingConfirm,
				txHashHex: txHashHex,
			}
		}
		return attestationAttemptResult{statusStr: attestationStatusSuccess, txHashHex: txHashHex}
	}

	// Chain call failed. Classify and write the appropriate terminal/pending state.
	kind := classifyAttestationFailure(attestErr)
	log.Error("PostEIP712Attestation failed",
		zap.Int64("trade_id", tradeID),
		zap.Int("kind", int(kind)),
		zap.Int("attempt", nextAttemptCount),
		zap.Error(attestErr))

	switch kind {
	case failureKindWaitingForGas:
		// Unlimited gas retries. Bump the observability counter only.
		gasCount := currentGasAttemptCount + 1
		if writeErr := s.writeAttestationWaitingForGas(dbCtx, tradeID, gasCount, attestErr.Error()); writeErr != nil {
			log.Error("Failed to mark attestation waiting_for_gas after insufficient funds",
				zap.Int64("trade_id", tradeID), zap.Error(writeErr))
		}
		s.attestationDisabled.Store(agentID, struct{}{})
		log.Error("Attestation disabled for agent (insufficient funds) - trade marked waiting_for_gas",
			zap.String("agent_id", agentID))
		return attestationAttemptResult{statusStr: attestationStatusWaitingForGas, err: attestErr}

	case failureKindTechnical:
		if nextAttemptCount >= maxAttestationAttempts {
			// Exhausted. Convert pending marker to terminal error.
			errMsg := fmt.Sprintf("exhausted %d technical retries: %s", maxAttestationAttempts, attestErr.Error())
			if writeErr := s.writeAttestationError(dbCtx, tradeID, errMsg); writeErr != nil {
				log.Error("Failed to mark attestation error after technical retry exhaustion",
					zap.Int64("trade_id", tradeID), zap.Error(writeErr))
			}
			return attestationAttemptResult{statusStr: attestationStatusError, err: attestErr}
		}
		// Leave as pending so next restart retries. Rewrite the pending
		// object to capture the latest error (counter already bumped pre-tx).
		if writeErr := s.writeAttestationPending(dbCtx, tradeID, nextAttemptCount, attestErr.Error()); writeErr != nil {
			log.Error("Failed to update pending attestation with error detail",
				zap.Int64("trade_id", tradeID), zap.Error(writeErr))
		}
		return attestationAttemptResult{statusStr: attestationStatusPending, err: attestErr}

	default: // failureKindTerminal
		if writeErr := s.writeAttestationError(dbCtx, tradeID, attestErr.Error()); writeErr != nil {
			log.Error("Failed to mark attestation error after terminal tx failure",
				zap.Int64("trade_id", tradeID), zap.Error(writeErr))
		}
		if isAttestationCircuitBreakerError(attestErr) {
			s.attestationDisabled.Store(agentID, struct{}{})
			log.Error("Attestation disabled for agent (terminal chain error)",
				zap.String("agent_id", agentID), zap.Error(attestErr))
		}
		return attestationAttemptResult{statusStr: attestationStatusError, err: attestErr}
	}
}

// finalizePendingConfirm completes a trade that is in pending_confirm state:
// the on-chain tx already landed, but the DB write to record success failed
// on the prior attempt. Recovery re-writes success from the stored tx_hash
// with NO chain call, eliminating the double-post spam on restart.
func (s *Service) finalizePendingConfirm(ctx context.Context, log *zap.Logger, tradeID int64, evidence map[string]any) {
	score, cpHashHex, txHashHex, ok := readPendingConfirmData(evidence)
	if !ok {
		// pending_confirm object is corrupted - cannot finalize. Mark error so
		// it stops coming back in the recovery query. Operator can inspect by
		// tx_hash in logs if one was partially recorded.
		log.Error("Cannot finalize pending_confirm: missing tx_hash or checkpoint_hash in evidence",
			zap.Int64("trade_id", tradeID))
		if writeErr := s.writeAttestationError(ctx, tradeID, "pending_confirm marker corrupted (missing tx_hash/checkpoint_hash)"); writeErr != nil {
			log.Error("Failed to mark corrupted pending_confirm as error",
				zap.Int64("trade_id", tradeID), zap.Error(writeErr))
		}
		return
	}
	if writeErr := s.writeAttestationSuccess(ctx, tradeID, score, cpHashHex, txHashHex); writeErr != nil {
		// Still failing - leave as pending_confirm, try again on next restart.
		// DO NOT downgrade to pending: that would trigger another on-chain post.
		log.Error("Failed to finalize pending_confirm - leaving for next restart",
			zap.Int64("trade_id", tradeID),
			zap.String("tx_hash", txHashHex),
			zap.Error(writeErr))
		return
	}
	log.Info("Recovery: finalized pending_confirm without chain call",
		zap.Int64("trade_id", tradeID),
		zap.String("tx_hash", txHashHex))
}

// agentKeyEntry holds the on-chain state for a registered agent.
type agentKeyEntry struct {
	key     *ecdsa.PrivateKey
	tokenID *big.Int
}

// NewService creates the Trading MCP service.
func NewService(svcCtx *platform.ServiceContext, exchClient exchange.Client, repo db.Repository) *Service {
	log := svcCtx.Logger().Named("trading_mcp")
	cfg := svcCtx.Config()

	agents := make(map[string]*config.AgentConfig)
	apiKeys := make(map[string]string)

	for k, agentCfg := range cfg.Agents {
		a := agentCfg // copy
		agents[a.ID] = &a
		apiKeys[a.APIKey] = a.ID
		log.Info(fmt.Sprintf("Registered agent %q (id=%s)", a.Name, a.ID), zap.String("key", k), zap.String("id", a.ID), zap.String("name", a.Name))

		// Initialize agent in DB
		initialBalance := decimal.NewFromFloat(a.InitialBalance)
		if !initialBalance.IsPositive() {
			initialBalance = decimal.NewFromInt(10000)
		}
		if _, err := repo.GetOrCreateAgent(context.Background(), a.ID, initialBalance); err != nil {
			log.Error("Failed to initialize agent in DB", zap.Error(err), zap.String("id", a.ID))
		}
	}

	svc := &Service{
		svcCtx:              svcCtx,
		log:                 log,
		orderLog:            log.Named("orders"),
		alertLog:            log.Named("alerts"),
		chainLog:            log.Named("chain"),
		policyLog:           log.Named("policy"),
		reconcileLog:        log.Named("reconcile"),
		exchange:            exchClient,
		repo:                repo,
		agents:              agents,
		apiKeys:             apiKeys,
		agentKeys:           make(map[string]*agentKeyEntry),
		alertPollInterval:   parseTradingAlertPollInterval(cfg),
		enableNativeStops:   cfg.TradingMCP.EnableNativeStops,
		hardcodedConfidence: cfg.Chain.HardcodedConfidence,
		maxNotesReasoning:   cfg.Chain.MaxNotesReasoning,
	}

	// Wire Swiftward evaluator if enabled
	if cfg.Swiftward.Enabled && cfg.Swiftward.IngestAddr != "" {
		eval, err := swiftward.NewEvaluator(cfg.Swiftward.IngestAddr, cfg.Swiftward.Timeout, log)
		if err != nil {
			log.Error("Failed to create Swiftward evaluator - policy evaluation disabled", zap.Error(err))
		} else {
			svc.evaluator = eval
			svc.evaluatorClose = eval.Close
			log.Info("Swiftward policy evaluation enabled",
				zap.String("ingest_addr", cfg.Swiftward.IngestAddr),
				zap.String("stream", cfg.Swiftward.Stream),
			)
		}
	} else {
		log.Info("Swiftward policy evaluation disabled (TRADING__SWIFTWARD__ENABLED=false)")
	}

	// Wire chain if RPC URL is configured
	if cfg.Chain.RPCURL != "" && cfg.Chain.ChainID != "" {
		chainClient, err := chain.NewClient(cfg.Chain.RPCURL, cfg.Chain.ChainID, log)
		if err != nil {
			log.Error("Failed to create chain client - on-chain operations disabled", zap.Error(err))
		} else {
			svc.chainClient = chainClient
			log.Info("Chain client connected",
				zap.String("rpc_url", cfg.Chain.RPCURL),
				zap.String("chain_id", cfg.Chain.ChainID),
			)

			if cfg.Chain.RiskRouterAddr != "" {
				svc.riskRouter = chain.NewRiskRouter(chainClient, common.HexToAddress(cfg.Chain.RiskRouterAddr), log)
			}
			if cfg.Chain.IdentityRegistryAddr != "" {
				svc.identityReg = chain.NewIdentityRegistry(chainClient, common.HexToAddress(cfg.Chain.IdentityRegistryAddr), log)
			}
			if cfg.Chain.ValidationRegistryAddr != "" {
				svc.validationReg = chain.NewValidationRegistry(chainClient, common.HexToAddress(cfg.Chain.ValidationRegistryAddr), log)
			}
		}
	} else {
		log.Info("Chain not configured - on-chain operations disabled (set TRADING__CHAIN__RPC_URL)")
	}

	return svc
}

func (s *Service) Initialize() error {
	mcpServer := mcp.NewServer("trading-mcp", "1.0.0", s.tools(), s.handleTool)

	// Wrap MCP handler to extract X-Agent-ID header into context.
	s.svcCtx.Router().Post("/mcp/trading", func(w http.ResponseWriter, r *http.Request) {
		agentID := r.Header.Get("X-Agent-ID")
		if agentID != "" {
			ctx := context.WithValue(r.Context(), agentIDContextKey, agentID)
			ctx = observability.WithLogger(ctx, s.log.With(zap.String("agent_id", agentID)))
			r = r.WithContext(ctx)
		}
		mcpServer.ServeHTTP(w, r)
	})

	// Claude agent REST API (session trail + active alerts).
	s.svcCtx.Router().Get("/v1/claude-agent/sessions", s.handleClaudeAgentSessions)
	s.svcCtx.Router().Get("/v1/claude-agent/alerts", s.handleClaudeAgentAlerts)

	// Evidence API on the main router (same port as MCP - required for dashboard SPA).
	s.svcCtx.Router().Get("/v1/evidence/{hash}", s.handleEvidenceRequest)
	evidenceUsageHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"missing decision hash","usage":"GET /v1/evidence/{hash}"}`))
	}
	s.svcCtx.Router().Get("/v1/evidence", evidenceUsageHandler)
	s.svcCtx.Router().Get("/v1/evidence/", evidenceUsageHandler)

	// Evidence API: GET /v1/evidence/{hash} on a separate port.
	// Skip if evidence port conflicts with the main MCP server port.
	evidencePort := s.svcCtx.Config().EvidencePort
	mainAddr := s.svcCtx.Config().Server.Addr
	if evidencePort != "" && evidencePort != mainAddr {
		go s.serveEvidence(evidencePort)
	}

	s.log.Info("Trading MCP registered at /mcp/trading")
	return nil
}

func (s *Service) Start() error {
	// Load agent private keys and ERC-8004 state if chain is configured.
	// Registration is NOT done at startup - use `make erc8004-register` CLI instead.
	// This only loads already-registered agents (by ERC8004AgentID in config).
	if s.identityReg != nil {
		for agentID, agentCfg := range s.agents {
			if agentCfg.PrivateKey == "" {
				continue
			}
			key, err := chain.ParsePrivateKey(agentCfg.PrivateKey)
			if err != nil {
				s.chainLog.Error("Failed to parse agent private key", zap.Error(err), zap.String("id", agentID))
				continue
			}
			if agentCfg.ERC8004AgentID == "" {
				s.chainLog.Warn("Agent has no ERC8004_AGENT_ID - run: make erc8004-register AGENT=<NAME>",
					zap.String("id", agentID),
				)
				// Still store the key for EIP-712 signing, just without a tokenID.
				s.agentKeys[agentID] = &agentKeyEntry{key: key}
				continue
			}
			tokenID, ok := new(big.Int).SetString(agentCfg.ERC8004AgentID, 10)
			if !ok {
				s.chainLog.Error("Invalid ERC8004_AGENT_ID", zap.String("id", agentID), zap.String("value", agentCfg.ERC8004AgentID))
				continue
			}
			// Verify on-chain that this agentId exists and belongs to this wallet (free eth_call).
			agentAddr := chain.AddressFromKey(key)
			if err := s.identityReg.VerifyAgentOwnership(context.Background(), tokenID, agentAddr); err != nil {
				s.chainLog.Error("ERC-8004 identity verification failed",
					zap.String("id", agentID),
					zap.String("tokenID", tokenID.String()),
					zap.Error(err),
				)
				continue
			}
			s.agentKeys[agentID] = &agentKeyEntry{key: key, tokenID: tokenID}
			logFields := []zap.Field{
				zap.String("id", agentID),
				zap.String("tokenID", tokenID.String()),
				zap.String("owner", agentAddr.Hex()),
			}
			if wei, balErr := s.chainClient.BalanceWei(context.Background(), agentAddr); balErr == nil {
				logFields = append(logFields, zap.String("balance_wei", wei.String()))
			}
			s.chainLog.Info("Agent ERC-8004 identity verified on-chain", logFields...)
		}
	}

	// Reconcile DB with exchange before accepting trades.
	// isStartup=true: also retry any pending/waiting_for_gas attestations.
	s.reconcileAllAgents(context.Background(), true)

	go s.runReconciliationPoller()
	go s.runPositionAlertPoller()
	go s.runPositionProtectionPoller()
	go s.runTimeAlertPoller()
	<-s.svcCtx.Context().Done()
	return nil
}

func (s *Service) runPositionAlertPoller() {
	ticker := time.NewTicker(s.alertPollInterval)
	defer ticker.Stop()
	ctx := s.svcCtx.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkPositionAlerts(ctx)
		}
	}
}

// checkPositionAlerts fetches active trading alerts and evaluates stop_loss/take_profit conditions.
func (s *Service) checkPositionAlerts(ctx context.Context) {
	alerts, err := s.repo.GetActiveAlerts(ctx, "", "trading")
	if err != nil {
		s.alertLog.Warn("position alert poller: get active alerts failed", zap.Error(err))
		return
	}
	if len(alerts) == 0 {
		return
	}

	// Collect unique pairs.
	pairsSet := make(map[string]struct{})
	for _, a := range alerts {
		if pair, ok := a.Params["pair"].(string); ok && pair != "" {
			pairsSet[pair] = struct{}{}
		}
	}

	// Fetch live prices if market source is available; otherwise use exchange cache.
	priceMap := make(map[string]decimal.Decimal)
	if s.marketSource != nil {
		pairs := make([]string, 0, len(pairsSet))
		for p := range pairsSet {
			pairs = append(pairs, p)
		}
		tickers, fetchErr := s.marketSource.GetTicker(ctx, pairs)
		if fetchErr != nil {
			s.alertLog.Warn("position alert poller: get ticker failed", zap.Error(fetchErr))
			return
		}
		for _, t := range tickers {
			if p, parseErr := strconv.ParseFloat(t.Last, 64); parseErr == nil {
				priceMap[t.Market] = decimal.NewFromFloat(p)
			}
		}
	} else {
		for k, v := range s.exchange.GetPrices() {
			priceMap[k] = v
		}
	}

	for _, a := range alerts {
		// Tier 1: native exchange stop orders. The exchange executes these
		// autonomously - we MUST NOT also execute a sell here (double-execution).
		//
		// Known limitation (deferred until Tier 1 is enabled):
		// Reconciliation is incomplete. When a native stop fires on the exchange,
		// the fill bypasses our toolSubmitOrder path, so repo.GetOpenPositions
		// still shows the position as open (it derives from internally recorded
		// trades). To properly reconcile, we would need either:
		//   (a) exchange.StopOrderProvider.GetOrderStatus(orderID) to poll fills, or
		//   (b) exchange.BalanceProvider.GetBalance() to check actual holdings.
		// Until then, Tier 1 alerts may stay "active" in DB after exchange
		// execution. The sell-side cleanup path (CancelActiveAlertsForPair +
		// CancelStopOrder) handles the normal exit; this edge case only matters
		// if the native stop fires between poller ticks without a manual sell.
		//
		// Tier 1 is disabled by default (enable_native_stops=false).
		if tier, _ := a.Params["tier"].(string); tier == "1" {
			continue
		}

		pair, _ := a.Params["pair"].(string)
		alertType, _ := a.Params["type"].(string)
		triggerPrice, _ := a.Params["trigger_price"].(float64)
		if pair == "" || alertType == "" || triggerPrice <= 0 {
			continue
		}

		currentPrice, ok := priceMap[pair]
		if !ok {
			continue
		}
		trigger := decimal.NewFromFloat(triggerPrice)

		var conditionMet bool
		switch alertType {
		case "stop_loss":
			conditionMet = currentPrice.LessThanOrEqual(trigger)
		case "take_profit":
			conditionMet = currentPrice.GreaterThanOrEqual(trigger)
		}
		if !conditionMet {
			continue
		}

		triggered, markErr := s.repo.MarkAlertTriggered(ctx, a.AlertID, currentPrice.String())
		if markErr != nil {
			s.alertLog.Warn("position alert poller: mark triggered failed",
				zap.String("alert_id", a.AlertID), zap.Error(markErr))
			continue
		}
		if !triggered {
			// Another poller tick already claimed this alert (TOCTOU guard).
			continue
		}
		s.alertLog.Info(fmt.Sprintf("Position alert triggered: %s %s %s at %s (trigger=%v)", a.AgentID, pair, alertType, currentPrice.String(), triggerPrice),
			zap.String("alert_id", a.AlertID),
			zap.String("agent_id", a.AgentID),
			zap.String("pair", pair),
			zap.String("type", alertType),
			zap.Float64("trigger_price", triggerPrice),
			zap.String("current_price", currentPrice.String()),
		)

		// OCO: cancel sibling orders in the same group
		if groupID := a.GroupID; groupID != "" {
			if cancelErr := s.repo.CancelAlertsByGroup(ctx, a.AgentID, groupID, a.AlertID); cancelErr != nil {
				s.alertLog.Warn("OCO group cancel failed", zap.String("group_id", groupID), zap.Error(cancelErr))
			}
		}

		if a.OnTrigger == "auto_execute" {
			// Use a bounded context: shut down gracefully but don't leak on hang.
			autoCtx, autoCancel := context.WithTimeout(s.svcCtx.Context(), 30*time.Second)
			go func(ctx context.Context, agentID, pair, alertID, groupID string, params map[string]any, cancel context.CancelFunc) {
				defer cancel()
				s.autoExecuteSell(ctx, agentID, pair, alertID, groupID, params)
			}(autoCtx, a.AgentID, pair, a.AlertID, a.GroupID, a.Params, autoCancel)
		}
	}
}

// autoExecuteRetries is the number of retry attempts for transient errors
// (network, lock timeout). Retries happen within the same goroutine with a 5s delay.
// Keep low: 2 attempts + 1 delay = 5s + 2*execution_time, must fit in the 30s context.
const autoExecuteRetries = 2

// autoExecuteSell submits a sell-all order for the agent/pair on auto_execute trigger.
//
// Transient errors (network, lock) are retried up to autoExecuteRetries times within
// the goroutine's 30s context. Permanent rejections (policy, risk_router, exchange)
// fail immediately. In both cases the alert is marked as "failed" and OCO siblings
// are restored. The agent picks up the failed alert via alert/triggered (once) and
// can handle it manually (e.g. sell in chunks).
//
// "no position" means the position was already closed - alert is cancelled silently.
//
// alertParams carries inform_agent and other metadata from the original conditional order.
func (s *Service) autoExecuteSell(ctx context.Context, agentID, pair, alertID, groupID string, alertParams map[string]any) {
	s.alertLog.Info(fmt.Sprintf("auto_execute: selling %s for %s", pair, agentID), zap.String("agent_id", agentID), zap.String("pair", pair))

	var lastErr string
	for attempt := 1; attempt <= autoExecuteRetries; attempt++ {
		result, outcome := s.tryAutoSell(ctx, agentID, pair, alertID, alertParams, attempt)
		switch outcome {
		case autoSellOK:
			return
		case autoSellNoPosition:
			// Position already closed - cancel the alert. Siblings stay cancelled
			// (no position to protect). CancelActiveAlertsForPair handles cleanup
			// when positions are sold via the normal path.
			s.alertLog.Info("auto_execute: no position to sell - cancelling alert",
				zap.String("agent_id", agentID), zap.String("pair", pair),
				zap.String("alert_id", alertID))
			if s.repo != nil {
				_ = s.repo.CancelAlert(ctx, agentID, alertID)
			}
			return
		case autoSellPermanent:
			// Permanent rejection (policy, risk_router, exchange) - fail immediately.
			s.failAlertAndRestore(ctx, agentID, alertID, groupID, result)
			return
		case autoSellTransient:
			// Transient error - retry after delay if attempts remain.
			lastErr = result
			if attempt < autoExecuteRetries {
				s.alertLog.Warn("auto_execute: transient error, retrying",
					zap.String("alert_id", alertID), zap.String("reason", result),
					zap.Int("attempt", attempt), zap.Int("max", autoExecuteRetries))
				select {
				case <-ctx.Done():
					s.failAlertAndRestore(ctx, agentID, alertID, groupID, "context cancelled during retry")
					return
				case <-time.After(5 * time.Second):
				}
			}
		}
	}

	// All retries exhausted.
	s.alertLog.Error("auto_execute: all retries exhausted - marking alert as failed",
		zap.String("alert_id", alertID), zap.String("reason", lastErr),
		zap.Int("attempts", autoExecuteRetries))
	s.failAlertAndRestore(ctx, agentID, alertID, groupID, lastErr)
}

// autoSellOutcome classifies the result of a single sell attempt.
type autoSellOutcome int

const (
	autoSellOK         autoSellOutcome = iota // sell succeeded
	autoSellNoPosition                        // position already closed
	autoSellPermanent                         // permanent rejection (policy, risk_router, exchange)
	autoSellTransient                         // transient error (network, lock, parse error)
)

// tryAutoSell executes a single sell attempt and classifies the outcome.
// Returns (reason string, outcome). reason is empty for OK/NoPosition.
func (s *Service) tryAutoSell(ctx context.Context, agentID, pair, alertID string, alertParams map[string]any, attempt int) (string, autoSellOutcome) {
	// Use a very large value - sell validation clamps to open position size.
	result, err := s.toolSubmitOrder(ctx, agentID, map[string]any{
		"pair":  pair,
		"side":  "sell",
		"value": float64(1_000_000_000),
	})

	if err != nil {
		s.alertLog.Error("auto_execute: submit sell failed",
			zap.String("agent_id", agentID), zap.String("pair", pair),
			zap.String("alert_id", alertID), zap.Int("attempt", attempt), zap.Error(err))
		return "submit error: " + err.Error(), autoSellTransient
	}

	// toolSubmitOrder returns policy/validation rejections as JSONResult with status="reject".
	if result != nil && len(result.Content) > 0 {
		var payload map[string]any
		if parseErr := json.Unmarshal([]byte(result.Content[0].Text), &payload); parseErr != nil {
			s.alertLog.Error("auto_execute: unexpected non-JSON response",
				zap.String("alert_id", alertID), zap.Int("attempt", attempt), zap.Error(parseErr))
			return "non-JSON response", autoSellTransient
		}
		if status, _ := payload["status"].(string); status == "reject" {
			reason := ""
			source := ""
			if reject, ok := payload["reject"].(map[string]any); ok {
				reason, _ = reject["reason"].(string)
				source, _ = reject["source"].(string)
			}
			if strings.Contains(strings.ToLower(reason), "no position") {
				return "", autoSellNoPosition
			}
			// Policy, RiskRouter, and exchange rejections are permanent.
			if source == "policy" || source == "risk_router" || source == "exchange" {
				s.alertLog.Warn("auto_execute: permanent rejection",
					zap.String("agent_id", agentID), zap.String("pair", pair),
					zap.String("alert_id", alertID), zap.String("source", source),
					zap.String("reason", reason), zap.Int("attempt", attempt))
				return reason, autoSellPermanent
			}
			// Unknown or missing source - treat as transient.
			if reason == "" {
				reason = "rejected with no source/reason"
			}
			s.alertLog.Warn("auto_execute: rejection with unknown source",
				zap.String("alert_id", alertID), zap.String("source", source),
				zap.String("reason", reason), zap.Int("attempt", attempt))
			return reason, autoSellTransient
		}
	}

	// Sell succeeded.
	informAgent := true
	if v, ok := alertParams["inform_agent"].(bool); ok {
		informAgent = v
	}
	if !informAgent {
		if ackErr := s.repo.AckTriggeredAlerts(ctx, []string{alertID}); ackErr != nil {
			s.alertLog.Warn("auto_execute: silent ack failed", zap.String("alert_id", alertID), zap.Error(ackErr))
		}
		s.alertLog.Info(fmt.Sprintf("auto_execute: sold %s for %s (silent)", pair, agentID),
			zap.String("agent_id", agentID), zap.String("pair", pair), zap.String("alert_id", alertID))
	} else {
		s.alertLog.Info(fmt.Sprintf("auto_execute: sold %s for %s (notify agent)", pair, agentID),
			zap.String("agent_id", agentID), zap.String("pair", pair), zap.String("alert_id", alertID))
	}
	return "", autoSellOK
}

// failAlertAndRestore marks the alert as failed and restores OCO siblings
// so the position stays protected while the agent decides what to do.
func (s *Service) failAlertAndRestore(ctx context.Context, agentID, alertID, groupID, reason string) {
	if s.repo != nil {
		if err := s.repo.FailAlert(ctx, alertID, reason); err != nil {
			s.alertLog.Error("auto_execute: fail alert failed", zap.String("alert_id", alertID), zap.Error(err))
		}
	}
	if groupID == "" || s.repo == nil {
		return
	}
	if rerr := s.repo.RestoreCancelledSiblings(ctx, agentID, groupID, alertID); rerr != nil {
		s.alertLog.Warn("auto_execute: restore OCO sibling failed",
			zap.String("group_id", groupID), zap.String("reason", reason), zap.Error(rerr))
	} else {
		s.alertLog.Info("auto_execute: OCO sibling restored",
			zap.String("group_id", groupID), zap.String("reason", reason))
	}
}

// runPositionProtectionPoller periodically ensures every open position has both
// an active stop_loss and take_profit alert. If either is missing (because of an
// OCO race, a failed auto-execute that left siblings cancelled, or any other
// edge case), it re-creates them from the latest buy params.
func (s *Service) runPositionProtectionPoller() {
	// Run at 6x the alert poll interval (default: 1m if alerts poll every 10s).
	// Slow enough to be cheap, fast enough to recover within a minute.
	interval := s.alertPollInterval * 6
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	ctx := s.svcCtx.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkPositionProtection(ctx)
		}
	}
}

// checkPositionProtection iterates every known agent's open positions and
// ensures each has live stop_loss + take_profit alerts. Missing alerts are
// recreated from the latest buy trade's params (which carry the original
// SL/TP values the agent submitted).
func (s *Service) checkPositionProtection(ctx context.Context) {
	if s.repo == nil {
		return
	}

	// Collect all known agent IDs (configured + dynamic via pending trades).
	agentIDs := make(map[string]bool, len(s.agents))
	for agentID := range s.agents {
		agentIDs[agentID] = true
	}
	if pending, err := s.repo.GetPendingTrades(ctx, ""); err == nil {
		for _, pt := range pending {
			agentIDs[pt.AgentID] = true
		}
	}

	for agentID := range agentIDs {
		positions, err := s.repo.GetOpenPositions(ctx, agentID)
		if err != nil || len(positions) == 0 {
			continue
		}
		alerts, err := s.repo.GetActiveAlerts(ctx, agentID, "trading")
		if err != nil {
			s.alertLog.Warn("position protection: get active alerts failed",
				zap.String("agent_id", agentID), zap.Error(err))
			continue
		}
		// Also check for failed alerts - if auto-execute failed for a pair,
		// don't recreate the alert (the agent needs to handle it manually).
		failedAlerts, _ := s.repo.GetFailedAlerts(ctx, agentID, "trading")

		// Build lookup: pair -> {has_sl, has_tp}.
		type protection struct{ hasSL, hasTP bool }
		byPair := make(map[string]*protection)
		for _, a := range alerts {
			pair, _ := a.Params["pair"].(string)
			aType, _ := a.Params["type"].(string)
			if pair == "" {
				continue
			}
			p, ok := byPair[pair]
			if !ok {
				p = &protection{}
				byPair[pair] = p
			}
			switch aType {
			case "stop_loss":
				p.hasSL = true
			case "take_profit":
				p.hasTP = true
			}
		}
		// Count failed alerts as "present" to block recreation.
		for _, a := range failedAlerts {
			pair, _ := a.Params["pair"].(string)
			aType, _ := a.Params["type"].(string)
			if pair == "" {
				continue
			}
			p, ok := byPair[pair]
			if !ok {
				p = &protection{}
				byPair[pair] = p
			}
			switch aType {
			case "stop_loss":
				p.hasSL = true
			case "take_profit":
				p.hasTP = true
			}
		}

		for _, pos := range positions {
			p := byPair[pos.Pair]
			if p == nil {
				p = &protection{}
			}
			if p.hasSL && p.hasTP {
				continue
			}
			s.recreateMissingProtection(ctx, agentID, pos.Pair, p.hasSL, p.hasTP)
		}
	}
}

// recreateMissingProtection looks up the latest buy params for a pair and
// recreates whichever of SL/TP is missing. New alerts go in a fresh OCO group
// so they cancel each other (and any future siblings) cleanly.
func (s *Service) recreateMissingProtection(ctx context.Context, agentID, pair string, hasSL, hasTP bool) {
	buyParams, err := s.repo.GetLatestBuyParams(ctx, agentID, pair)
	if err != nil || buyParams == nil {
		s.alertLog.Warn("position protection: missing alerts but no buy params to restore from",
			zap.String("agent_id", agentID), zap.String("pair", pair),
			zap.Bool("has_sl", hasSL), zap.Bool("has_tp", hasTP))
		return
	}

	sl, slOK := paramDecimal(buyParams, "stop_loss")
	tp, tpOK := paramDecimal(buyParams, "take_profit")
	if (!hasSL && !slOK) && (!hasTP && !tpOK) {
		// Original buy did not specify SL/TP either - nothing to restore.
		return
	}

	// Fresh OCO group so the recreated alerts cancel each other on fire.
	groupID := fmt.Sprintf("oco-recover-%s-%d", pair, time.Now().UnixNano())

	informAgent := true
	if v, ok := buyParams["inform_agent"].(bool); ok {
		informAgent = v
	}

	if !hasSL && slOK && sl.IsPositive() {
		if cerr := s.createConditionalOrder(ctx, agentID, pair, "stop_loss", "below",
			sl, groupID, "auto-recovered: SL was missing for open position", informAgent); cerr != nil {
			s.alertLog.Error("position protection: failed to recreate stop_loss",
				zap.String("agent_id", agentID), zap.String("pair", pair), zap.Error(cerr))
		} else {
			s.alertLog.Info(fmt.Sprintf("position protection: recreated stop_loss for %s %s at %s", agentID, pair, sl.String()),
				zap.String("agent_id", agentID), zap.String("pair", pair),
				zap.String("trigger_price", sl.String()))
		}
	}
	if !hasTP && tpOK && tp.IsPositive() {
		if cerr := s.createConditionalOrder(ctx, agentID, pair, "take_profit", "above",
			tp, groupID, "auto-recovered: TP was missing for open position", informAgent); cerr != nil {
			s.alertLog.Error("position protection: failed to recreate take_profit",
				zap.String("agent_id", agentID), zap.String("pair", pair), zap.Error(cerr))
		} else {
			s.alertLog.Info(fmt.Sprintf("position protection: recreated take_profit for %s %s at %s", agentID, pair, tp.String()),
				zap.String("agent_id", agentID), zap.String("pair", pair),
				zap.String("trigger_price", tp.String()))
		}
	}
}

func (s *Service) Stop() error {
	if s.evidenceSrv != nil {
		if err := s.evidenceSrv.Close(); err != nil {
			s.log.Error("Evidence server close error", zap.Error(err))
		}
	}
	if s.evaluatorClose != nil {
		if err := s.evaluatorClose(); err != nil {
			s.log.Error("Evaluator close error", zap.Error(err))
		}
	}
	if s.chainClient != nil {
		s.chainClient.Close()
	}
	s.log.Info("Trading MCP stopped")
	return nil
}

// handleClaudeAgentSessions serves GET /v1/claude-agent/sessions.
// Reads the JSONL session trail from <workspace>/logs/sessions.jsonl and returns the last N entries.
func (s *Service) handleClaudeAgentSessions(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	agentID := r.URL.Query().Get("agent")
	if agentID == "" {
		http.Error(w, `{"error":"agent query param required"}`, http.StatusBadRequest)
		return
	}
	// Sanitize: agent ID must be simple alphanumeric+hyphens (no path traversal).
	for _, c := range agentID {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			http.Error(w, `{"error":"invalid agent id"}`, http.StatusBadRequest)
			return
		}
	}

	workspace := s.svcCtx.Config().FilesMCP.RootDir
	if workspace == "" {
		workspace = "/data/workspace"
	}
	sessionFile := filepath.Join(workspace, agentID, "logs", "sessions.jsonl")

	f, err := os.Open(sessionFile) //nolint:gosec // path is from server config
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sessions":[]}`))
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	// Read all lines, keep last N.
	var lines []json.RawMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		lines = append(lines, cp)
	}

	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}
	result := lines[start:]

	// Reverse so newest first.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	out, _ := json.Marshal(map[string]any{"sessions": result})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleClaudeAgentAlerts serves GET /v1/claude-agent/alerts.
// Returns all active alerts for a given agent_id across all services.
func (s *Service) handleClaudeAgentAlerts(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}

	var all []db.AlertRecord
	for _, svc := range []string{"trading", "market", "news", "time"} {
		alerts, err := s.repo.GetActiveAlerts(r.Context(), agentID, svc)
		if err != nil {
			s.alertLog.Warn(fmt.Sprintf("Failed to get active %s alerts for %s", svc, agentID), zap.String("service", svc), zap.Error(err))
			continue
		}
		all = append(all, alerts...)
	}
	if all == nil {
		all = []db.AlertRecord{}
	}

	out, _ := json.Marshal(map[string]any{"alerts": all})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleEvidenceRequest handles GET /v1/evidence/{hash} on the main chi router.
func (s *Service) handleEvidenceRequest(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if hash == "" {
		http.Error(w, "missing hash", http.StatusBadRequest)
		return
	}
	s.serveEvidenceHash(w, r, hash)
}

// serveEvidenceHash looks up a decision trace by hash and writes it as JSON.
// Returns the full envelope: {hash, prev_hash, agent_id, created_at, data: <evidence>}.
func (s *Service) serveEvidenceHash(w http.ResponseWriter, r *http.Request, hash string) {
	trace, err := s.repo.GetTrace(r.Context(), hash)
	if err != nil {
		if errors.Is(err, db.ErrTraceNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
		} else {
			s.log.Error("Evidence lookup failed", zap.Error(err), zap.String("hash", hash))
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	// Wrap raw trace data with chain metadata so the full picture is visible.
	envelope := map[string]any{
		"hash":       trace.DecisionHash,
		"prev_hash":  trace.PrevHash,
		"agent_id":   trace.AgentID,
		"created_at": trace.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		"data":       json.RawMessage(trace.TraceJSON),
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		s.log.Error("Evidence marshal failed", zap.Error(err), zap.String("hash", hash))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// serveEvidence starts a separate HTTP server for the evidence API.
func (s *Service) serveEvidence(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/evidence/", func(w http.ResponseWriter, r *http.Request) {
		hash := r.URL.Path[len("/v1/evidence/"):]
		if hash == "" {
			http.Error(w, "missing hash", http.StatusBadRequest)
			return
		}
		s.serveEvidenceHash(w, r, hash)
	})

	s.evidenceSrv = &http.Server{Addr: addr, Handler: mux}
	s.log.Info("Evidence API starting", zap.String("addr", addr))
	if err := s.evidenceSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.log.Error("Evidence API error", zap.Error(err))
	}
}

// getOrCreateAgent auto-registers an unknown agent with the default initial balance.
func (s *Service) getOrCreateAgent(ctx context.Context, agentID string) (*db.AgentState, error) {
	defaultBalance := decimal.NewFromFloat(s.svcCtx.Config().DefaultInitialBalance)
	if !defaultBalance.IsPositive() {
		defaultBalance = decimal.NewFromInt(10000)
	}
	return s.repo.GetOrCreateAgent(ctx, agentID, defaultBalance)
}

// IsHalted returns whether an agent is halted (reads from DB).
func (s *Service) IsHalted(agentID string) bool {
	state, err := s.repo.GetAgent(context.Background(), agentID)
	if err != nil {
		return false
	}
	return state.Halted
}

// SetHalted sets the halt state of an agent (persisted to DB, visible across instances).
// Acquires the per-agent advisory lock so halt synchronizes with in-flight submit_order calls.
// An in-flight trade will either complete before halt takes effect, or see the halted flag
// after halt returns - no race window where halt returns but a trade still passes.
func (s *Service) SetHalted(ctx context.Context, agentID string, halted bool) error {
	unlock, err := s.repo.LockAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("acquire agent lock for halt: %w", err)
	}
	defer unlock()
	return s.repo.SetAgentHalted(ctx, agentID, halted)
}

// Agents returns a snapshot copy of the agent registry. Returning a copy keeps
// callers safe if registration ever becomes dynamic - today s.agents is only
// written during init, but the cost (one shallow map copy per call) is cheap
// for the dozens-of-agents scale we run at.
func (s *Service) Agents() map[string]*config.AgentConfig {
	out := make(map[string]*config.AgentConfig, len(s.agents))
	for id, a := range s.agents {
		out[id] = a
	}
	return out
}

// Repo returns the database repository.
// SetMarketSource attaches an optional market data source for position alert polling.
// Must be called before Start. If nil, exchange.GetPrices() is used (may be stale).
func (s *Service) SetMarketSource(src marketdata.DataSource) {
	s.marketSource = src
}

func (s *Service) Repo() db.Repository {
	return s.repo
}

func parseTradingAlertPollInterval(cfg *config.Config) time.Duration {
	if cfg != nil {
		if d, err := time.ParseDuration(cfg.TradingMCP.AlertPollInterval); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Second
}

// Exchange returns the exchange client.
func (s *Service) Exchange() exchange.Client {
	return s.exchange
}

// exchangeFor returns an agent-scoped exchange client when the underlying
// client supports per-agent isolation (e.g. Kraken with separate HOME dirs).
// Falls back to the shared client otherwise.
func (s *Service) exchangeFor(agentID string) exchange.Client {
	if provider, ok := s.exchange.(exchange.AgentClientProvider); ok {
		return provider.ForAgent(agentID)
	}
	return s.exchange
}

func (s *Service) tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "trade/submit_order",
			Description: "Submit a trade order. Value is notional in quote currency of the pair. " +
				"Buy orders must include params.stop_loss (policy rejects buys without it; check policy via trade/get_limits). " +
				"params bag: stop_loss, take_profit (trigger prices - auto-create OCO conditional orders on fill), " +
				"inform_agent (bool, default true - set false for silent SL/TP), " +
				"strategy, reasoning, trigger_reason, confidence, max_slippage_bps. " +
				"On fill returns: {status:\"fill\", decision_hash, fill: {id, pair, side, price, qty, value, fee, fee_asset, fee_value}}. " +
				"On rejection returns: {status:\"reject\", reject: {source, reason, verdict?, tag?, retry_after?}}. " +
				"Call trade/get_portfolio after a fill to see updated positions and cash.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pair":  map[string]any{"type": "string", "description": "Trading pair, e.g. \"ETH-USD\"."},
					"side":  map[string]any{"type": "string", "enum": []string{"buy", "sell"}},
					"value": map[string]any{"type": "number", "description": "Order value in quote currency of the pair (notional)"},
					"params": map[string]any{
						"type":        "object",
						"description": "Optional params: stop_loss (required for buys by policy), take_profit, strategy, reasoning, trigger_reason, confidence (0-1), max_slippage_bps (default 50), order_type (only \"market\").",
					},
				},
				"required": []string{"pair", "side", "value"},
			},
		},
		{
			Name: "trade/get_portfolio",
			Description: "Get current portfolio snapshot. " +
				"Returns: {portfolio: {value, cash, peak}, fill_count, reject_count, halted, " +
				"positions: [{pair, side, qty, avg_price, value, unrealized_pnl, unrealized_pnl_pct}]}. " +
				"Call after a fill to get updated state.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "trade/get_history",
			Description: "Get trade history, most recent first. " +
				"Returns: {trades: [{timestamp, pair, side, status, decision_hash?, fill?: {id, price, qty, value, pnl_value}, reject?: {source, reason}, portfolio?: {value_after}}], count}. " +
				"Fills include price, pnl, portfolio.value_after. Rejects include reject.source and reject.reason.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":  map[string]any{"type": "number", "description": "Max trades to return (default 50)"},
					"pair":   map[string]any{"type": "string", "description": "Filter by pair, e.g. \"ETH-USDC\""},
					"status": map[string]any{"type": "string", "enum": []string{StatusFill, StatusReject}, "description": "Filter by outcome"},
				},
			},
		},
		{
			Name: "trade/get_limits",
			Description: "Get current risk state and policy usage. " +
				"Returns: {portfolio: {value, cash, peak}, fill_count, reject_count, largest_position_pct, largest_position_pair, halted}. " +
				"halted=true means trading is blocked.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "trade/get_portfolio_history",
			Description: "Get equity curve from fill history, chronological order. " +
				"Returns: {equity_curve: [{timestamp, portfolio: {value}, pair, side}], count}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "number", "description": "Max data points (default 100)"},
				},
			},
		},
		{
			Name: "trade/estimate_order",
			Description: "Dry-run a trade without executing. " +
				"Returns: {pair, side, value, price, qty, portfolio: {value, cash, peak}, fill_count, reject_count, halted, position_pct_after, warning?}. " +
				"All monetary values are decimal strings. Use to check sizing and portfolio impact before submitting.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pair":  map[string]any{"type": "string", "description": "Canonical trading pair, e.g. \"ETH-USDC\". Use market/list_markets to get valid symbols."},
					"side":  map[string]any{"type": "string", "enum": []string{"buy", "sell"}},
					"value": map[string]any{"type": "number", "description": "Order value in portfolio currency (notional)"},
				},
				"required": []string{"pair", "side", "value"},
			},
		},
		{
			Name: "trade/heartbeat",
			Description: "Recompute portfolio equity against live prices, update peak value, check drawdown. " +
				"Returns: {portfolio: {value, cash, peak}, fill_count, reject_count, halted, timestamp}. " +
				"Call periodically in long sessions to keep peak_value current.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "trade/set_conditional",
			Description: "Set a conditional order (stop-loss or take-profit) on an open position. " +
				"Platform monitors prices every 10s and auto-executes a sell-all when the condition is met. " +
				"Returns: {alert_id, status:\"active\", tier}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pair": map[string]any{
						"type":        "string",
						"description": "Canonical trading pair, e.g. \"ETH-USD\"",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"stop_loss", "take_profit"},
						"description": "stop_loss triggers when price falls to or below trigger_price; take_profit when price rises to or above",
					},
					"trigger_price": map[string]any{
						"type":        "number",
						"description": "Price threshold that fires the conditional order",
					},
					"inform_agent": map[string]any{
						"type":        "boolean",
						"description": "Whether the agent is notified when this fires (default true). Set false for silent auto-execution.",
					},
					"note": map[string]any{
						"type":        "string",
						"description": "Optional reminder text injected when the order fires",
					},
				},
				"required": []string{"pair", "type", "trigger_price"},
			},
		},
		{
			Name: "alert/triggered",
			Description: "Get alerts and conditional orders that have fired since last check across all services (trading, market, news, time). " +
				"Returns: {alerts: [{alert_id, service, pair, type, trigger_price, note, triggered_at, ...}]}. " +
				"Triggered alerts are acknowledged after retrieval.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "alert/list",
			Description: "List all active alerts and conditional orders for the current agent across all services (trading, market, news, time). " +
				"Returns: {alerts: [{alert_id, service, params, note, created_at, tier?, current_price?, distance_pct?, inform_agent?}], count}. " +
				"Trading alerts include current_price, distance_pct (positive = above trigger, negative = below), and tier.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "trade/cancel_conditional",
			Description: "Cancel an active conditional order (stop-loss or take-profit) by ID. " +
				"Use alert/list to find alert IDs. Returns {cancelled: true} on success.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"alert_id": map[string]any{
						"type":        "string",
						"description": "ID of the conditional order to cancel (e.g. palert-410c22f9df9adb53)",
					},
				},
				"required": []string{"alert_id"},
			},
		},
		{
			Name: "trade/set_reminder",
			Description: "Set a one-shot time-based reminder. The platform fires it at the scheduled time and " +
				"surfaces it via alert/triggered. Useful for scheduling periodic check-ins or timed exit reviews. " +
				"Returns: {alert_id, fire_at, status:\"active\"}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"fire_at": map[string]any{
						"type":        "string",
						"description": "RFC3339 timestamp when the reminder fires, e.g. \"2026-04-05T15:00:00Z\"",
					},
					"note": map[string]any{
						"type":        "string",
						"description": "Reminder text injected when it fires",
					},
					"on_trigger": map[string]any{
						"type":        "string",
						"enum":        []string{"wake_full", "wake_triage"},
						"description": "wake_full = run full session pipeline; wake_triage = quick check only (default: wake_full)",
					},
				},
				"required": []string{"fire_at"},
			},
		},
		{
			Name: "trade/end_cycle",
			Description: "Post a session checkpoint. Call once at the end of every analysis cycle, " +
				"whether you traded or not. Records your session summary on-chain for auditability.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{"type": "string", "description": "One-line session summary: what you analyzed, what you decided, market regime"},
				},
				"required": []string{"summary"},
			},
		},
	}
}

func (s *Service) handleTool(ctx context.Context, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	agentID, _ := ctx.Value(agentIDContextKey).(string)

	// Enrich context logger with tool name for all downstream logs.
	log := observability.LoggerFromCtx(ctx, s.log).With(zap.String("tool", toolName))
	ctx = observability.WithLogger(ctx, log)

	if agentID != "" && s.repo != nil {
		if err := s.repo.UpdateLastSeen(ctx, agentID); err != nil {
			log.Warn("failed to update last_seen_at", zap.Error(err))
		}
	}

	switch toolName {
	case "trade/submit_order":
		return s.toolSubmitOrder(ctx, agentID, args)
	case "trade/get_portfolio":
		return s.toolGetPortfolio(ctx, agentID)
	case "trade/get_history":
		return s.toolGetHistory(ctx, agentID, args)
	case "trade/get_limits":
		return s.toolGetLimits(ctx, agentID)
	case "trade/get_portfolio_history":
		return s.toolGetPortfolioHistory(ctx, agentID, args)
	case "trade/estimate_order":
		return s.toolEstimateOrder(ctx, agentID, args)
	case "trade/heartbeat":
		return s.toolHeartbeat(ctx, agentID)
	case "trade/set_conditional":
		return s.toolSetConditional(ctx, agentID, args)
	case "trade/set_reminder":
		return s.toolSetReminder(ctx, agentID, args)
	case "alert/triggered":
		return s.toolGetTriggeredAlerts(ctx, agentID)
	case "alert/list":
		return s.toolListAlerts(ctx, agentID)
	case "trade/cancel_conditional":
		return s.toolCancelConditional(ctx, agentID, args)
	case "trade/end_cycle":
		return s.toolEndCycle(ctx, agentID, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (s *Service) toolSubmitOrder(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.orderLog)
	pair, _ := args["pair"].(string)
	side, _ := args["side"].(string)
	orderValue := parseDecimalArg(args, "value")

	if agentID == "" || pair == "" || side == "" || !orderValue.IsPositive() {
		return nil, fmt.Errorf("agent_id (via X-Agent-ID header), pair, side, and value (>0) are required")
	}
	if side != "buy" && side != "sell" {
		return nil, fmt.Errorf("side must be \"buy\" or \"sell\", got %q", side)
	}

	// Parse optional params bag (SL/TP, metadata, execution params)
	var params map[string]any
	if raw, ok := args["params"]; ok {
		params, _ = raw.(map[string]any)
	}
	// Validate order_type if provided (only "market" supported now)
	if params != nil {
		if ot, ok := params["order_type"].(string); ok && ot != "" && ot != "market" {
			return nil, fmt.Errorf("order_type %q not supported, only \"market\" is allowed", ot)
		}
	}

	// Dynamic agent registration: any agent with X-Agent-ID can trade.
	// Pre-configured agents are already in DB from startup; unknown agents get auto-created.
	if _, regErr := s.getOrCreateAgent(ctx, agentID); regErr != nil {
		return nil, fmt.Errorf("register agent: %w", regErr)
	}

	// Distributed per-agent lock: serializes policy check + exchange + persist
	// across all trading-server instances sharing the same Postgres.
	unlock, lockErr := s.repo.LockAgent(ctx, agentID)
	if lockErr != nil {
		return nil, fmt.Errorf("acquire agent lock: %w", lockErr)
	}
	defer unlock()

	// Read agent state + positions for enrichment
	state, err := s.repo.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent state: %w", err)
	}

	if state.Halted {
		haltReason := "Agent is halted - trading paused by risk manager or heartbeat"
		resp := map[string]any{
			"status": StatusReject,
			"reject": map[string]any{
				"source":  RejectSourcePolicy,
				"reason":  haltReason,
				"verdict": "agent_halted",
			},
		}
		if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, haltReason); persistErr != "" {
			resp["persist_error"] = persistErr
		}
		return mcp.JSONResult(resp)
	}

	agentExch := s.exchangeFor(agentID)
	prices := agentExch.GetPrices()
	equity, _ := s.repo.ComputeEquity(ctx, agentID, prices)
	positions, _ := s.repo.GetOpenPositions(ctx, agentID)

	// 1. Pre-trade balance checks (cheapest validation - run first).
	if side == "buy" && orderValue.GreaterThan(state.Cash) {
		reason := fmt.Sprintf("insufficient cash: need %s, have %s", orderValue.StringFixed(2), state.Cash.StringFixed(2))
		log.Info(fmt.Sprintf("Trade rejected: %s BUY %s $%s insufficient cash (have $%s)", agentID, pair, orderValue.StringFixed(2), state.Cash.StringFixed(2)),
			zap.String("value", orderValue.String()),
			zap.String("cash", state.Cash.String()),
		)
		resp := map[string]any{
			"status": StatusReject,
			"reject": map[string]any{
				"source": RejectSourcePolicy,
				"reason": reason,
			},
		}
		if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, reason); persistErr != "" {
			resp["persist_error"] = persistErr
		}
		return mcp.JSONResult(resp)
	}
	// Reject sells that exceed the current long position (no short selling).
	// Without this, sells create untracked liabilities: cash increases but the
	// negative position is clamped to zero, inflating equity.
	//
	// sellQty: when set, the exchange uses base qty directly (no value/price
	// round-trip). This is critical for full-close exits during stop-loss
	// drawdowns, where current price < avg_price, so any value-based clamp
	// would imply a qty greater than actually held.
	var sellQty decimal.Decimal
	if side == "sell" {
		var positionQty, positionValue decimal.Decimal
		for _, pos := range positions {
			if pos.Pair == pair {
				positionQty = pos.Qty
				positionValue = pos.Qty.Mul(pos.AvgPrice)
				break
			}
		}
		if positionQty.IsZero() {
			reason := fmt.Sprintf("no position in %s to sell", pair)
			log.Info(fmt.Sprintf("Trade rejected: %s has no %s position to sell", agentID, pair),
				zap.String("pair", pair),
			)
			resp := map[string]any{
				"status": StatusReject,
				"reject": map[string]any{
					"source": RejectSourcePolicy,
					"reason": reason,
				},
			}
			if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, reason); persistErr != "" {
				resp["persist_error"] = persistErr
			}
			return mcp.JSONResult(resp)
		}
		// Full close: requested value >= position value (auto-exits send 1e9 to force this).
		// Partial sell: requested value < position value, sell the same fraction of qty.
		// In both cases pass qty directly so the exchange does not divide by live price.
		if orderValue.GreaterThanOrEqual(positionValue) {
			if orderValue.GreaterThan(positionValue) {
				log.Info(fmt.Sprintf("Sell clamped: %s %s $%s -> $%s", agentID, pair, orderValue.StringFixed(2), positionValue.StringFixed(2)),
					zap.String("pair", pair),
					zap.String("requested", orderValue.String()),
					zap.String("clamped_to", positionValue.String()),
				)
			}
			sellQty = positionQty
			orderValue = positionValue
		} else {
			// Partial sell: scale qty by the requested value fraction.
			sellQty = positionQty.Mul(orderValue).Div(positionValue)
		}
	}

	// Captured from Swiftward eval for attestation notes.
	var swVerdict, swEventID string
	var swResponse map[string]any

	// 2. Swiftward policy evaluation (if enabled) with enriched event data
	if s.evaluator != nil {
		swCfg := s.svcCtx.Config().Swiftward
		leverage := 1
		if v, ok := args["leverage"]; ok {
			if lv, ok := v.(float64); ok && lv > 0 {
				leverage = int(lv)
			}
		}

		// Risk data derived from trades table (no extra columns)
		dayStartValue, _ := s.repo.GetDayStartValue(ctx, agentID)
		if dayStartValue.IsZero() {
			dayStartValue = state.InitialValue
		}
		rollingPeak, _ := s.repo.GetRollingPeak24h(ctx, agentID)
		if rollingPeak.IsZero() {
			rollingPeak = equity
		}

		priceMap := make(map[string]float64, len(prices))
		for k, v := range prices {
			priceMap[k] = v.InexactFloat64()
		}

		orderEvent := map[string]any{
			"pair":             pair,
			"side":             side,
			"value":            orderValue.InexactFloat64(),
			"leverage":         leverage,
			"is_risk_reducing": isRiskReducing(side, pair, positions),
		}
		// Enrich with SL/TP from params for Swiftward policy rules.
		// InexactFloat64 is intentional here: the event is JSON-serialized for policy eval,
		// where float64 is the appropriate wire type. Decimal precision is preserved in
		// the conditional-order creation path below.
		if params != nil {
			if sl, ok := paramDecimal(params, "stop_loss"); ok && sl.IsPositive() {
				orderEvent["stop_loss"] = sl.InexactFloat64()
			}
			if tp, ok := paramDecimal(params, "take_profit"); ok && tp.IsPositive() {
				orderEvent["take_profit"] = tp.InexactFloat64()
			}
			if conf, ok := paramFloat(params, "confidence"); ok {
				orderEvent["confidence"] = conf
			}
		}
		// Add current price and SL distance for the pair (used by validate_stop_loss_proximity rule)
		if price, ok := prices[pair]; ok {
			priceF := price.InexactFloat64()
			orderEvent["current_price"] = priceF
			if sl, ok := orderEvent["stop_loss"].(float64); ok && priceF > 0 {
				dist := (priceF - sl) / priceF * 100 // positive = SL below price (normal for buy)
				if dist < 0 {
					dist = -dist
				}
				orderEvent["sl_distance_pct"] = dist
				// Flag inverted SL: for buy orders, SL should be below current price
				if side == "buy" && sl >= priceF {
					orderEvent["sl_inverted"] = true
				}
			}
		}
		eventData := map[string]any{
			"order": orderEvent,
			"portfolio": map[string]any{
				"value":            equity.InexactFloat64(),
				"peak":             state.PeakValue.InexactFloat64(),
				"cash":             state.Cash.InexactFloat64(),
				"positions":        positionsToJSON(positions),
				"day_start_value":  dayStartValue.InexactFloat64(),
				"rolling_peak_24h": rollingPeak.InexactFloat64(),
				"initial_value":    state.InitialValue.InexactFloat64(),
			},
			"prices": priceMap,
		}
		// Fail CLOSED. Swiftward is a gate: if it can't answer, we have no
		// permission to trade. Prior behaviour was fail-open - a silent bug
		// where an unreachable gate let every trade through. Technical
		// errors get exponential backoff; if all retries fail OR the error
		// is terminal, reject with a distinctive reason so operators can
		// see the gate was DOWN, not that the trade was explicitly denied.
		result, evalErr := withTechnicalRetry(ctx, log, "swiftward_evaluate",
			func(rctx context.Context) (*swiftward.EvalResult, error) {
				return s.evaluator.EvaluateSync(rctx, swCfg.Stream, agentID, "trade_order", eventData)
			})
		if evalErr != nil {
			reason := "swiftward_unavailable: " + evalErr.Error()
			log.Error("Swiftward evaluation failed after retries - rejecting trade (fail-closed)",
				zap.String("agent_id", agentID),
				zap.String("pair", pair),
				zap.String("side", side),
				zap.Error(evalErr),
			)
			resp := map[string]any{
				"status": StatusReject,
				"reject": map[string]any{
					"source": RejectSourcePolicy,
					"reason": reason,
					"error":  evalErr.Error(),
				},
			}
			if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, reason); persistErr != "" {
				resp["persist_error"] = persistErr
			}
			return mcp.JSONResult(resp)
		}
		{
			if blocked, reason := result.ShouldBlock(); blocked {
				log.Info(fmt.Sprintf("Trade blocked by policy: %s %s %s $%s verdict=%s", agentID, side, pair, orderValue.StringFixed(2), result.Verdict),
					zap.String("verdict", string(result.Verdict)),
					zap.String("reason", reason),
				)
				reject := map[string]any{
					"source":  RejectSourcePolicy,
					"reason":  reason,
					"verdict": string(result.Verdict),
					"exec_id": result.ID,
				}
				// Forward all policy response fields as-is (tag, retry_after, etc.)
				for k, v := range result.Response {
					if k == "reason" || k == "source" || k == "verdict" || k == "exec_id" {
						continue // skip keys already set in reject map
					}
					reject[k] = v
				}
				resp := map[string]any{
					"status": StatusReject,
					"reject": reject,
				}
				if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, reason); persistErr != "" {
					resp["persist_error"] = persistErr
				}
				return mcp.JSONResult(resp)
			}
			log.Debug(fmt.Sprintf("Trade approved: %s %s %s $%s", agentID, side, pair, orderValue.StringFixed(2)),
				zap.String("verdict", string(result.Verdict)),
				zap.String("event_id", result.ID),
			)
			swVerdict = string(result.Verdict)
			swEventID = result.ID
			swResponse = result.Response
		}
	}

	// 3. On-chain RiskRouter validation (if configured).
	// Runs BEFORE exchange fill - this is a VALIDATION GATE, same contract as
	// Swiftward: if the gate can't give a decision, the trade does NOT happen.
	//   - RiskRouter explicitly rejects      -> reject trade
	//   - RiskRouter RPC technical error     -> exponential backoff, then reject
	//   - RiskRouter terminal error / revert -> reject immediately
	//   - RiskRouter approves                -> proceed to exchange
	//
	// Prior behaviour on chain errors was fail-OPEN (log + keep going) which
	// meant any RPC 429 turned the gate into a no-op. Fixed: fail closed.
	var txHash string
	var intentHash [32]byte // captured for attestation checkpoint correlation
	if s.riskRouter != nil {
		if entry, ok := s.agentKeys[agentID]; ok && entry.key != nil && entry.tokenID != nil {
			nonce, nonceErr := withTechnicalRetry(ctx, log, "risk_router_get_nonce",
				func(rctx context.Context) (*big.Int, error) {
					return s.riskRouter.GetIntentNonce(rctx, entry.tokenID)
				})
			if nonceErr != nil {
				reason := "risk_router_unavailable: get_nonce: " + nonceErr.Error()
				log.Error("RiskRouter get_nonce failed after retries - rejecting trade (fail-closed)",
					zap.String("agent_id", agentID),
					zap.String("pair", pair),
					zap.String("side", side),
					zap.Error(nonceErr),
				)
				resp := map[string]any{
					"status": StatusReject,
					"reject": map[string]any{
						"source": RejectSourceRouter,
						"reason": reason,
						"error":  nonceErr.Error(),
					},
				}
				if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, reason); persistErr != "" {
					resp["persist_error"] = persistErr
				}
				return mcp.JSONResult(resp)
			}

			amountUsdScaled := orderValue.Mul(decimal.NewFromInt(100)).BigInt() // USD * 100
			maxSlippageBps := big.NewInt(50)                                    // default 0.5%
			if params != nil {
				if v, ok := paramFloat(params, "max_slippage_bps"); ok && v > 0 {
					maxSlippageBps = big.NewInt(int64(v))
				}
			}
			deadline := new(big.Int).SetInt64(9999999999) // far future for hackathon

			action := "BUY"
			if side == "sell" {
				action = "SELL"
			}

			intent := &chain.TradeIntentData{
				AgentID:         entry.tokenID,
				AgentWallet:     chain.AddressFromKey(entry.key),
				Pair:            pair,
				Action:          action,
				AmountUsdScaled: amountUsdScaled,
				MaxSlippageBps:  maxSlippageBps,
				Nonce:           nonce,
				Deadline:        deadline,
			}

			intentHash = chain.ComputeIntentHash(intent)
			submission, chainErr := withTechnicalRetry(ctx, log, "risk_router_submit_trade",
				func(rctx context.Context) (*chain.TradeSubmission, error) {
					return s.riskRouter.SubmitTrade(rctx, intent, entry.key)
				})
			if chainErr != nil {
				reason := "risk_router_unavailable: submit_trade: " + chainErr.Error()
				log.Error("RiskRouter submit_trade failed after retries - rejecting trade (fail-closed)",
					zap.String("agent_id", agentID),
					zap.String("pair", pair),
					zap.String("side", side),
					zap.Error(chainErr),
				)
				resp := map[string]any{
					"status": StatusReject,
					"reject": map[string]any{
						"source": RejectSourceRouter,
						"reason": reason,
						"error":  chainErr.Error(),
					},
				}
				if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, reason); persistErr != "" {
					resp["persist_error"] = persistErr
				}
				return mcp.JSONResult(resp)
			}
			if !submission.Success {
				reason := fmt.Sprintf("RiskRouter rejected: %s", submission.ErrorReason)
				log.Info("Trade rejected by RiskRouter",
					zap.String("reason", submission.ErrorReason),
					zap.String("tx_hash", submission.TxHash.Hex()))
				resp := map[string]any{
					"status": StatusReject,
					"reject": map[string]any{
						"source":  RejectSourceRouter,
						"reason":  reason,
						"tx_hash": submission.TxHash.Hex(),
					},
				}
				if persistErr := s.recordRejectedTrade(ctx, agentID, pair, side, orderValue, reason); persistErr != "" {
					resp["persist_error"] = persistErr
				}
				return mcp.JSONResult(resp)
			}
			txHash = submission.TxHash.Hex()
		}
	}

	// 4. Write-ahead: insert pending trade BEFORE touching the exchange.
	// If DB fails here, we don't call the exchange - no drift possible.
	// If process crashes after exchange fill, reconciliation finds this pending record on startup.
	//
	// Evidence.data is built up front with intent + gate results (swiftward, risk_router).
	// Fill + hash_chain are added atomically in FinalizeTrade.
	evidenceData := map[string]any{
		"intent": map[string]any{
			"pair":   pair,
			"side":   side,
			"value":  orderValue.String(),
			"params": params,
		},
	}
	if swEventID != "" {
		evidenceData["swiftward"] = map[string]any{
			"event_id": swEventID,
			"verdict":  swVerdict,
			"response": swResponse,
		}
	}
	// Record RiskRouter evidence for the attestation checkpoint. Gate is
	// fail-closed: reaching this point means either RiskRouter approved
	// (txHash set, intentHash computed) or RiskRouter was not configured
	// for this agent (both empty). There is no "failed_open" state anymore.
	var zeroIntentHash [32]byte
	if txHash != "" || intentHash != zeroIntentHash {
		rr := map[string]any{"status": "approved"}
		if txHash != "" {
			rr["tx_hash"] = txHash
		}
		if intentHash != zeroIntentHash {
			rr["intent_hash"] = fmt.Sprintf("0x%x", intentHash)
		}
		evidenceData["risk_router"] = rr
	}
	pendingTrade := &db.TradeRecord{
		AgentID:   agentID,
		Timestamp: time.Now(),
		Pair:      pair,
		Side:      side,
		Qty:       orderValue, // requested value, updated with actual qty after fill
		Status:    StatusPending,
		TxHash:    txHash,
		Params:    params,
		Evidence:  map[string]any{"data": evidenceData},
	}
	if err := s.repo.InsertTrade(ctx, pendingTrade); err != nil {
		return nil, fmt.Errorf("write-ahead trade record: %w", err)
	}

	// 5. Execute on exchange (irreversible - all validations passed above).
	resp, err := agentExch.SubmitTrade(&exchange.TradeRequest{
		Pair:   pair,
		Side:   side,
		Value:  orderValue,
		Qty:    sellQty, // zero for buys; set for sells to bypass value/price round-trip
		Params: params,
	})
	if err != nil {
		// Exchange failed - reject pending record and bump reject_count.
		if updateErr := s.repo.RejectPendingTrade(ctx, agentID, pendingTrade.ID, err.Error()); updateErr != nil {
			log.Error("Failed to mark pending trade as failed", zap.Error(updateErr))
		}
		return nil, fmt.Errorf("exchange error: %w", err)
	}

	// Handle exchange rejection (StatusRejected with nil error - defensive).
	if resp.Status != exchange.StatusFilled {
		reason := "exchange rejected order"
		if err := s.repo.RejectPendingTrade(ctx, agentID, pendingTrade.ID, reason); err != nil {
			log.Error("Failed to reject pending trade after exchange rejection", zap.Error(err))
		}
		return mcp.JSONResult(map[string]any{
			"status": StatusReject,
			"reject": map[string]any{
				"source": "exchange",
				"reason": reason,
			},
		})
	}

	// 5. Compute state changes for filled trade
	var pnl, fillValue, newEquity, cashDelta, feeValue decimal.Decimal
	var feeAsset string
	if resp.Status == exchange.StatusFilled {
		fillValue = resp.QuoteQty // actual cash exchanged (full value for buy, net for sell)

		if side == "buy" {
			cashDelta = fillValue.Neg()
			// Fee in base asset - convert to portfolio currency for tracking.
			feeAsset = pairBase(pair)
			feeValue = resp.Fee.Mul(resp.Price)
		} else {
			cashDelta = fillValue
			// Fee in quote asset - already in portfolio currency.
			feeAsset = pairQuote(pair)
			feeValue = resp.Fee
			for _, pos := range positions {
				if pos.Pair == pair && pos.AvgPrice.IsPositive() {
					// Gross PnL from price difference (buy fee baked into higher avg_price).
					// Subtract sell fee for net PnL.
					pnl = resp.Price.Sub(pos.AvgPrice).Mul(resp.Qty).Sub(resp.Fee)
					break
				}
			}
		}
		state.Cash = state.Cash.Add(cashDelta)
		state.FillCount++

		// Recompute equity including this fill.
		// Use already-fetched positions (trade not persisted yet, nothing changed under advisory lock).
		prices = agentExch.GetPrices()
		baseEquity := s.equityFromPositions(state, positions, prices)
		if side == "buy" {
			if mktPrice, ok := prices[pair]; ok {
				newEquity = baseEquity.Add(resp.Qty.Mul(mktPrice))
			} else {
				newEquity = baseEquity.Add(fillValue)
			}
		} else {
			if mktPrice, ok := prices[pair]; ok {
				newEquity = baseEquity.Sub(resp.Qty.Mul(mktPrice))
			} else {
				newEquity = baseEquity.Sub(fillValue)
			}
		}

		if newEquity.GreaterThan(state.PeakValue) {
			state.PeakValue = newEquity
		}
	}

	result := map[string]any{
		"status": StatusFill,
		"fill": map[string]any{
			"id":        resp.FillID,
			"pair":      resp.Pair,
			"side":      resp.Side,
			"price":     resp.Price.String(),
			"qty":       resp.Qty.String(),
			"value":     fillValue.String(),
			"fee":       resp.Fee.String(),
			"fee_asset": feeAsset,
			"fee_value": feeValue.String(),
		},
	}

	// 6. Compute evidence hash chain BEFORE finalize so it's atomic.
	// The hash covers evidence.data (intent + gates + fill).
	var decisionHash, prevHash string
	var traceRecord *db.DecisionTrace

	// Enrich evidenceData with fill results (intent + gates were set at step 4).
	evidenceData["fill"] = map[string]any{
		"id":        resp.FillID,
		"price":     resp.Price.String(),
		"qty":       resp.Qty.String(),
		"fee":       resp.Fee.String(),
		"fee_asset": feeAsset,
		"fee_value": feeValue.String(),
	}

	prevHash, prevErr := s.repo.GetLatestTraceHash(ctx, agentID)
	if prevErr != nil {
		log.Error("Failed to get latest trace hash - skipping evidence chain",
			zap.Error(prevErr))
	} else {
		if prevHash == "" {
			prevHash = evidence.ZeroHash
		}
		hash, hashErr := evidence.ComputeDecisionHash(s.log, evidenceData, prevHash)
		if hashErr != nil {
			log.Error("Failed to compute decision hash", zap.Error(hashErr))
		} else {
			decisionHash = hash
			if traceJSON, marshalErr := json.Marshal(evidenceData); marshalErr == nil {
				traceRecord = &db.DecisionTrace{
					DecisionHash: decisionHash,
					AgentID:      agentID,
					PrevHash:     prevHash,
					TraceJSON:    traceJSON,
				}
			}
			result["decision_hash"] = decisionHash
			result["prev_hash"] = prevHash
		}
	}

	// 7. Finalize pending trade: atomically update state, fill, evidence, and trace.
	// The pending record (step 4) guarantees we never lose a fill - even on crash,
	// reconciliation will find the pending record and resolve it.
	if resp.Status == exchange.StatusFilled {
		fillEvidence := map[string]any{
			"data": evidenceData,
		}
		if decisionHash != "" {
			fillEvidence["hash"] = decisionHash
			fillEvidence["prev_hash"] = prevHash
		}
		fillUpdate := &db.TradeFillUpdate{
			TradeID:      pendingTrade.ID,
			Qty:          resp.Qty,
			Price:        resp.Price,
			Value:        fillValue,
			PnL:          pnl,
			Status:       StatusFill,
			ValueAfter:   newEquity,
			FillID:       resp.FillID,
			TxHash:       txHash,
			Fee:          resp.Fee,
			FeeAsset:     feeAsset,
			FeeValue:     feeValue,
			Evidence:     fillEvidence,
			DecisionHash: decisionHash,
			Trace:        traceRecord,
		}
		stateUpdate := &db.StateUpdate{
			AgentID:       agentID,
			CashDelta:     cashDelta,
			PeakValue:     state.PeakValue,
			FillCountIncr: 1,
			FeeDelta:      feeValue,
		}
		if persistErr := s.repo.FinalizeTrade(ctx, stateUpdate, fillUpdate); persistErr != nil {
			log.Error("CRITICAL: Failed to finalize trade (exchange fill executed, pending record exists for reconciliation)",
				zap.Error(persistErr), zap.String("fill_id", resp.FillID),
				zap.Int64("pending_trade_id", pendingTrade.ID))
			result["persist_error"] = persistErr.Error()
		}
	}

	// 8. Add RiskRouter tx_hash to result if available.
	if txHash != "" {
		if fillMap, ok := result["fill"].(map[string]any); ok {
			fillMap["tx_hash"] = txHash
		}
		result["chain_success"] = true
	}

	// Release per-agent lock - DB is fully committed. Steps 9-10 (conditional orders,
	// attestation) don't need the lock and can take 15-90s for blockchain confirmation.
	unlock()
	unlock = func() {} //nolint:ineffassign,staticcheck // reassign to no-op so deferred unlock() is safe

	if resp.Status == exchange.StatusFilled {

		// 9. Auto-create conditional orders from params (SL/TP) + proactive cancel on sell.
		if side == "buy" && params != nil {
			groupID := fmt.Sprintf("oco-%s", resp.FillID)
			agentExch := s.exchangeFor(agentID)
			stopProv, hasNative := agentExch.(exchange.StopOrderProvider)
			useNative := s.enableNativeStops && hasNative

			// Respect inform_agent from params (default true).
			informAgent := true
			if v, ok := params["inform_agent"].(bool); ok {
				informAgent = v
			}

			if sl, ok := paramDecimal(params, "stop_loss"); ok && sl.IsPositive() {
				if useNative {
					res, nativeErr := stopProv.PlaceStopOrder(pair, "sell", "stop_loss", sl, decimal.Zero)
					if nativeErr != nil {
						log.Warn("Tier 1 SL from submit_order failed, falling back to Tier 2", zap.Error(nativeErr))
						_ = s.createConditionalOrder(ctx, agentID, pair, "stop_loss", "below",
							sl, groupID, "auto-created from submit_order", informAgent)
					} else {
						s.trackNativeAlert(ctx, agentID, pair, "stop_loss", sl, res.OrderID, groupID, "auto SL from order (native)", informAgent)
					}
				} else {
					if coErr := s.createConditionalOrder(ctx, agentID, pair, "stop_loss", "below",
						sl, groupID, "auto-created from submit_order", informAgent); coErr != nil {
						log.Error("Failed to create SL conditional order", zap.Error(coErr))
					}
				}
			}
			if tp, ok := paramDecimal(params, "take_profit"); ok && tp.IsPositive() {
				if useNative {
					res, nativeErr := stopProv.PlaceStopOrder(pair, "sell", "take_profit", tp, decimal.Zero)
					if nativeErr != nil {
						log.Warn("Tier 1 TP from submit_order failed, falling back to Tier 2", zap.Error(nativeErr))
						_ = s.createConditionalOrder(ctx, agentID, pair, "take_profit", "above",
							tp, groupID, "auto-created from submit_order", informAgent)
					} else {
						s.trackNativeAlert(ctx, agentID, pair, "take_profit", tp, res.OrderID, groupID, "auto TP from order (native)", informAgent)
					}
				} else {
					if coErr := s.createConditionalOrder(ctx, agentID, pair, "take_profit", "above",
						tp, groupID, "auto-created from submit_order", informAgent); coErr != nil {
						log.Error("Failed to create TP conditional order", zap.Error(coErr))
					}
				}
			}
		}
		if side == "sell" {
			// Proactive cancel: close all conditional orders for this pair on sell.
			// For Tier 1 alerts, cancel the native exchange order first.
			if stopProv, ok := s.exchangeFor(agentID).(exchange.StopOrderProvider); ok {
				pairAlerts, _ := s.repo.GetActiveAlerts(ctx, agentID, "trading")
				for _, pa := range pairAlerts {
					if p, _ := pa.Params["pair"].(string); p != pair {
						continue
					}
					if nativeID, _ := pa.Params["native_order_id"].(string); nativeID != "" {
						if cancelErr := stopProv.CancelStopOrder(nativeID); cancelErr != nil {
							log.Warn("Failed to cancel Tier 1 native order on sell",
								zap.String("native_order_id", nativeID), zap.Error(cancelErr))
						}
					}
				}
			}
			if cancelErr := s.repo.CancelActiveAlertsForPair(ctx, agentID, pair); cancelErr != nil {
				log.Error("Failed to cancel alerts for sold pair", zap.Error(cancelErr),
					zap.String("pair", pair))
			}
		}

		// 10. EIP-712 attestation to ValidationRegistry (hackathon leaderboard).
		// Synchronous - agent sees attestation result in the trade response. Every
		// path writes an explicit attestation status to evidence so recovery on
		// startup knows exactly which trades to retry (pending, waiting_for_gas)
		// and which to leave alone (success, error, disabled).
		//
		// Use a durable DB write context decoupled from the client request context:
		// attestation runs after the trade is already filled and persisted, so if
		// the caller disconnects we still want to record success/pending_confirm
		// markers deterministically and avoid duplicate reposts on restart.
		attestDBCtx, attestDBCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer attestDBCancel()
		if s.validationReg == nil {
			log.Debug("Attestation disabled: ValidationRegistry not configured")
			result["attestation"] = attestationStatusDisabled
			if writeErr := s.writeAttestationDisabled(attestDBCtx, pendingTrade.ID, "validation_registry_not_configured"); writeErr != nil {
				log.Error("Failed to mark attestation disabled", zap.Int64("trade_id", pendingTrade.ID), zap.Error(writeErr))
			}
		} else if s.isAttestationDisabled(agentID) {
			// Circuit breaker tripped in this process. Don't call the chain, but
			// record waiting_for_gas so startup recovery on the next run retries.
			// gasAttemptCount=0 omits the counter (we never actually tried).
			log.Debug("Attestation deferred: circuit breaker tripped for agent")
			result["attestation"] = attestationStatusWaitingForGas
			if writeErr := s.writeAttestationWaitingForGas(attestDBCtx, pendingTrade.ID, 0, "circuit breaker tripped (previous failure in this process)"); writeErr != nil {
				log.Error("Failed to mark attestation waiting_for_gas", zap.Int64("trade_id", pendingTrade.ID), zap.Error(writeErr))
			}
		} else if entry, ok := s.agentKeys[agentID]; !ok || entry.key == nil || entry.tokenID == nil {
			log.Debug("Attestation disabled: agent missing ERC-8004 keys/tokenID", zap.Bool("has_entry", ok))
			result["attestation"] = attestationStatusDisabled
			if writeErr := s.writeAttestationDisabled(attestDBCtx, pendingTrade.ID, "agent_missing_erc8004_keys"); writeErr != nil {
				log.Error("Failed to mark attestation disabled", zap.Int64("trade_id", pendingTrade.ID), zap.Error(writeErr))
			}
		} else {
			// Use background context - attestation must complete even if HTTP client disconnects.
			// The trade is already finalized; losing the attestation just means recovery retries later.
			attestCtx, attestCancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer attestCancel()

			action := "BUY"
			if side == "sell" {
				action = "SELL"
			}
			asset := pairBase(pair)
			amountScaled := orderValue.Mul(decimal.NewFromInt(100)).BigInt()
			priceScaled := resp.Price.Mul(decimal.NewFromInt(100)).BigInt()

			var reasoningHash [32]byte
			confidenceScaled := big.NewInt(0)
			if params != nil {
				if r, ok := params["reasoning"].(string); ok && r != "" {
					reasoningHash = chain.ReasoningHash(r)
				}
				if c, ok := params["confidence"].(float64); ok && c > 0 {
					confidenceScaled = big.NewInt(int64(math.Round(c * 1000)))
				}
			}

			cp := &chain.TradeCheckpointData{
				AgentID:          entry.tokenID,
				Timestamp:        big.NewInt(pendingTrade.Timestamp.Unix()),
				Action:           action,
				Asset:            asset,
				Pair:             pair,
				AmountUsdScaled:  amountScaled,
				PriceUsdScaled:   priceScaled,
				ReasoningHash:    reasoningHash,
				ConfidenceScaled: confidenceScaled,
				IntentHash:       intentHash,
			}

			registryAddr := common.HexToAddress(s.svcCtx.Config().Chain.IdentityRegistryAddr)
			_, cpHash, signErr := chain.SignCheckpoint(cp, entry.key, s.chainClient.ChainID(), registryAddr)
			if signErr != nil {
				log.Error("Failed to sign checkpoint", zap.Error(signErr))
				result["attestation"] = "error"
				result["attestation_error"] = signErr.Error()
				if writeErr := s.writeAttestationError(attestDBCtx, pendingTrade.ID, "sign checkpoint: "+signErr.Error()); writeErr != nil {
					log.Error("Failed to mark attestation error", zap.Int64("trade_id", pendingTrade.ID), zap.Error(writeErr))
				}
			} else {
				var score uint8
				if s.hardcodedConfidence > 0 {
					score = uint8(s.hardcodedConfidence)
				} else if params != nil {
					if c, ok := params["confidence"].(float64); ok && c > 0 {
						score = uint8(math.Round(c * 100))
					}
				}
				// Build on-chain notes from evidence. Truncate reasoning for calldata cost.
				notesData := truncateNotesReasoning(evidenceData, s.maxNotesReasoning)
				notesObj := map[string]any{
					"data":      notesData,
					"hash":      decisionHash,
					"prev_hash": prevHash,
				}
				notesJSON, marshalErr := json.Marshal(notesObj)
				if marshalErr != nil {
					log.Warn("Failed to marshal attestation notes, using fallback", zap.Error(marshalErr))
					notesJSON, _ = json.Marshal(map[string]any{
						"pair": pair, "side": side, "hash": decisionHash,
					})
				}
				notes := string(notesJSON)

				// Fresh trade - no prior attestation state. attemptAttestation
				// writes the pre-tx pending marker, posts the tx, classifies
				// the outcome (success / WFG / technical-retry / terminal),
				// and writes the terminal state. Shared with the recovery path
				// so the retry rules are IDENTICAL.
				attemptRes := s.attemptAttestation(
					attestDBCtx, attestCtx, log,
					pendingTrade.ID, agentID, entry,
					cpHash, score, notes,
					"", 0, 0,
				)
				result["attestation"] = attemptRes.statusStr
				if attemptRes.err != nil {
					result["attestation_error"] = attemptRes.err.Error()
				}
				if attemptRes.txHashHex != "" {
					result["attestation_tx"] = attemptRes.txHashHex
					if attemptRes.statusStr == attestationStatusSuccess {
						log.Info(fmt.Sprintf("Attestation posted: %s %s %s", agentID, side, pair),
							zap.String("tx_hash", attemptRes.txHashHex))
					}
				}
			}
		}

		// 11. Emit execution_report to Swiftward (async, fire-and-forget).
		// Portfolio data included in event so Swiftward doesn't need to maintain shadow state.
		// state.Cash already includes cashDelta (updated at line 1032).
		s.emitExecutionReport(ctx, agentID, decisionHash, map[string]any{
			"status":        StatusFill,
			"decision_hash": decisionHash,
			"fill": map[string]any{
				"pair":     pair,
				"side":     side,
				"qty":      resp.Qty.InexactFloat64(),
				"price":    resp.Price.InexactFloat64(),
				"value":    fillValue.InexactFloat64(),
				"pnl":      pnl.InexactFloat64(),
				"leverage": 1,
				"tx_hash":  txHash,
			},
			"portfolio": map[string]any{
				"value": newEquity.InexactFloat64(),
				"peak":  state.PeakValue.InexactFloat64(),
				"cash":  state.Cash.InexactFloat64(),
			},
		})
	}

	return mcp.JSONResult(result)
}

// recordRejectedTrade atomically inserts a rejected trade record and updates rejected count.
// Returns a persistence error string (empty on success) for inclusion in API response.
func (s *Service) recordRejectedTrade(ctx context.Context, agentID, pair, side string, orderValue decimal.Decimal, reason string) string {
	stateUpdate := &db.StateUpdate{
		AgentID:    agentID,
		RejectIncr: 1,
	}
	trade := &db.TradeRecord{
		AgentID:   agentID,
		Timestamp: time.Now(),
		Pair:      pair,
		Side:      side,
		Qty:       orderValue, // notional (no fill price to compute base qty)
		Status:    StatusReject,
		Reason:    reason,
	}
	if persistErr := s.repo.RecordTrade(ctx, stateUpdate, trade); persistErr != nil {
		s.log.Error("Failed to persist rejected trade", zap.Error(persistErr))
		return persistErr.Error()
	}
	return ""
}

// emitExecutionReport sends an async execution_report event to Swiftward.
// This is fire-and-forget - failures are logged but don't affect the trade response.
// The execution_report rules (track_fill, router_rejection_alert) update Swiftward's
// internal state (fill_count, portfolio_value, positions) for heartbeat drawdown checks.
func (s *Service) emitExecutionReport(ctx context.Context, agentID, decisionHash string, eventData map[string]any) {
	if s.evaluator == nil {
		return
	}
	swCfg := s.svcCtx.Config().Swiftward
	execID, err := s.evaluator.EvaluateAsync(ctx, swCfg.Stream, agentID, "execution_report", eventData)
	if err != nil {
		s.log.Warn("Failed to emit execution_report to Swiftward (non-fatal)",
			zap.String("agent_id", agentID),
			zap.String("decision_hash", decisionHash),
			zap.Error(err),
		)
		return
	}
	s.log.Debug("execution_report emitted",
		zap.String("agent_id", agentID),
		zap.String("exec_id", execID),
		zap.String("decision_hash", decisionHash),
	)
}

// pairBase extracts the base asset from a trading pair (e.g., "ETH-USDC" -> "ETH").
// parseDecimalArg reads a value from MCP tool arguments as decimal.Decimal.
// Accepts both JSON numbers (float64) and strings ("1234.56") for precision.
func parseDecimalArg(args map[string]any, key string) decimal.Decimal {
	switch v := args[key].(type) {
	case float64:
		return decimal.NewFromFloat(v)
	case string:
		d, err := decimal.NewFromString(v)
		if err != nil {
			return decimal.Zero
		}
		return d
	default:
		return decimal.Zero
	}
}

func pairBase(pair string) string {
	if i := strings.Index(pair, "-"); i > 0 {
		return pair[:i]
	}
	return pair
}

// positionsToJSON converts open positions to a JSON string of pair -> quantity (base asset)
// for Swiftward events. The concentration UDF expects a JSON string, not a map object.
// paramFloat reads a non-financial numeric param (e.g. confidence, basis points)
// that may arrive as float64 or a numeric string.
func paramFloat(params map[string]any, key string) (float64, bool) {
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}

// paramDecimal reads a financial price param (e.g. stop_loss, take_profit)
// that may arrive as float64 or a numeric string, returning decimal.Decimal
// to preserve full precision without float64 round-trips.
func paramDecimal(params map[string]any, key string) (decimal.Decimal, bool) {
	v, ok := params[key]
	if !ok {
		return decimal.Zero, false
	}
	switch t := v.(type) {
	case float64:
		return decimal.NewFromFloat(t), true
	case string:
		d, err := decimal.NewFromString(t)
		return d, err == nil
	}
	return decimal.Zero, false
}

func positionsToJSON(positions []db.OpenPosition) string {
	m := make(map[string]float64, len(positions))
	for _, p := range positions {
		m[p.Pair] = p.Qty.InexactFloat64()
	}
	data, _ := json.Marshal(m)
	return string(data)
}

// isRiskReducing returns true if the order reduces existing exposure (selling a held position).
func isRiskReducing(side, pair string, positions []db.OpenPosition) bool {
	for _, pos := range positions {
		if pos.Pair == pair && pos.Qty.IsPositive() {
			// Selling a long position = risk-reducing. Buying more = risk-increasing.
			return side == "sell"
		}
	}
	return false // no position in this pair - any order opens new exposure
}

// computeRiskTier returns 0-3 based on intraday drawdown (negative fraction, e.g. -0.03 = -3%).
// Thresholds match the YAML constants: tier1=-0.02, tier2=-0.035, tier3=-0.05.
// Uses <= (at-or-below) to match industry convention (FTMO, TopStep trigger AT the threshold).
func computeRiskTier(drawdownFraction float64) int {
	switch {
	case drawdownFraction <= -0.05:
		return 3 // close-only
	case drawdownFraction <= -0.035:
		return 2 // warning
	case drawdownFraction <= -0.02:
		return 1 // caution
	default:
		return 0 // normal
	}
}

func riskTierLabel(tier int) string {
	switch tier {
	case 0:
		return "normal"
	case 1:
		return "caution"
	case 2:
		return "warning"
	case 3:
		return "close_only"
	default:
		return "unknown"
	}
}

func riskTierMaxOrderPct(tier int) float64 {
	switch tier {
	case 0:
		return 15
	case 1:
		return 10
	case 2:
		return 5
	default:
		return 0
	}
}

func riskTierMaxConcentration(tier int) float64 {
	switch tier {
	case 0:
		return 50
	case 1:
		return 35
	case 2:
		return 25
	default:
		return 0
	}
}

// pairQuote extracts the quote asset from a trading pair (e.g., "ETH-USDC" -> "USDC").
func pairQuote(pair string) string {
	if i := strings.Index(pair, "-"); i >= 0 && i < len(pair)-1 {
		return pair[i+1:]
	}
	return "USD"
}

// computeEquityFromState computes equity using state.Cash + open positions from DB.
func (s *Service) computeEquityFromState(ctx context.Context, state *db.AgentState, prices map[string]decimal.Decimal) (decimal.Decimal, error) {
	positions, err := s.repo.GetOpenPositions(ctx, state.AgentID)
	if err != nil {
		return state.Cash, err
	}
	return s.equityFromPositions(state, positions, prices), nil
}

// equityFromPositions computes equity from pre-fetched state and positions (no DB call).
func (s *Service) equityFromPositions(state *db.AgentState, positions []db.OpenPosition, prices map[string]decimal.Decimal) decimal.Decimal {
	equity := state.Cash
	for _, pos := range positions {
		if price, ok := prices[pos.Pair]; ok {
			equity = equity.Add(pos.Qty.Mul(price))
		} else {
			equity = equity.Add(pos.Value)
		}
	}
	return equity
}

func (s *Service) toolGetPortfolio(ctx context.Context, agentID string) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.log)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	state, err := s.getOrCreateAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get portfolio for agent %s: %w", agentID, err)
	}

	agentExch := s.exchangeFor(agentID)
	prices := agentExch.GetPrices()
	positions, _ := s.repo.GetOpenPositions(ctx, agentID)

	// Refresh prices for all position pairs from marketSource (live ticker).
	// The exchange's GetPrices() returns a lazy cache that only updates on fill
	// or on explicit GetPrice calls, so values for pairs traded hours ago are
	// stale. marketSource is the source of truth for current price; the cache
	// is only a fallback when marketSource is unavailable.
	if s.marketSource != nil && len(positions) > 0 {
		pairs := make([]string, 0, len(positions))
		for _, pos := range positions {
			pairs = append(pairs, pos.Pair)
		}
		if tickers, err := s.marketSource.GetTicker(ctx, pairs); err == nil {
			for _, t := range tickers {
				if p, perr := decimal.NewFromString(t.Last); perr == nil && p.IsPositive() {
					prices[t.Market] = p
				}
			}
			log.Info("get_portfolio: refreshed prices from marketSource",
				zap.Int("requested", len(pairs)),
				zap.Int("received", len(tickers)),
				zap.Int("cached_total", len(prices)),
			)
		} else {
			log.Warn("get_portfolio: marketSource ticker fetch failed",
				zap.Strings("pairs", pairs), zap.Error(err))
		}
	}

	// Cash: use exchange balance if available (source of truth), else DB.
	cash := state.Cash
	if bp, ok := agentExch.(exchange.BalanceProvider); ok {
		if balances, balErr := bp.GetBalance(); balErr == nil {
			for _, b := range balances {
				if b.Asset == "USD" || b.Asset == "ZUSD" {
					cash = b.Available
					break
				}
			}
		}
	}

	// Equity = cash + sum of positions at current market prices.
	equity := cash
	posOut := make([]map[string]any, 0, len(positions))
	posValues := make(map[string]decimal.Decimal)
	for _, pos := range positions {
		mktPrice, ok := prices[pos.Pair]
		posValue := pos.Value
		if ok {
			posValue = pos.Qty.Mul(mktPrice)
		}
		equity = equity.Add(posValue)
		posValues[pos.Pair] = posValue
		entry := map[string]any{
			"pair":      pos.Pair,
			"side":      pos.Side,
			"qty":       pos.Qty.String(),
			"avg_price": pos.AvgPrice.String(),
			"value":     posValue.String(),
		}
		if ok {
			entry["current_price"] = mktPrice.StringFixed(8)
		}
		costBasis := pos.Qty.Mul(pos.AvgPrice)
		if costBasis.IsPositive() {
			unrealizedPnl := posValue.Sub(costBasis)
			hundred := decimal.NewFromInt(100)
			entry["unrealized_pnl"] = unrealizedPnl.StringFixed(2)
			entry["unrealized_pnl_pct"] = unrealizedPnl.Div(costBasis).Mul(hundred).StringFixed(2)
		}
		posOut = append(posOut, entry)
	}

	// Enrich positions with SL/TP from active alerts, strategy from trade params, size_pct.
	if len(posOut) > 0 {
		alerts, _ := s.repo.GetActiveAlerts(ctx, agentID, "trading")
		// Build lookup: pair -> {stop_loss, take_profit} from alerts
		slByPair := make(map[string]float64)
		tpByPair := make(map[string]float64)
		for _, a := range alerts {
			aPair, _ := a.Params["pair"].(string)
			aType, _ := a.Params["type"].(string)
			aPrice, _ := a.Params["trigger_price"].(float64)
			if aPair == "" || aPrice <= 0 {
				continue
			}
			switch aType {
			case "stop_loss":
				slByPair[aPair] = aPrice
			case "take_profit":
				tpByPair[aPair] = aPrice
			}
		}

		hundred := decimal.NewFromInt(100)
		for _, entry := range posOut {
			pair := entry["pair"].(string)
			if sl, ok := slByPair[pair]; ok {
				entry["stop_loss"] = fmt.Sprintf("%.8f", sl)
			}
			if tp, ok := tpByPair[pair]; ok {
				entry["take_profit"] = fmt.Sprintf("%.8f", tp)
			}
			// concentration: position value as % of equity (read-only, for display + AI awareness)
			if equity.IsPositive() {
				if pv, ok := posValues[pair]; ok {
					entry["concentration_pct"] = pv.Div(equity).Mul(hundred).StringFixed(2)
				}
			}
			// strategy from latest buy trade params
			if buyParams, err := s.repo.GetLatestBuyParams(ctx, agentID, pair); err == nil && buyParams != nil {
				if tag, ok := buyParams["strategy"].(string); ok {
					entry["strategy"] = tag
				}
			}
		}
	}

	return mcp.JSONResult(map[string]any{
		"portfolio": map[string]any{
			"value":         equity.StringFixed(8),
			"cash":          cash.String(),
			"peak":          state.PeakValue.String(),
			"initial_value": state.InitialValue.String(),
		},
		"positions":    posOut,
		"fill_count":   state.FillCount,
		"reject_count": state.RejectCount,
		"halted":       state.Halted,
		"total_fees":   state.TotalFees.String(),
	})
}

func (s *Service) toolGetHistory(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	pair, _ := args["pair"].(string)
	status, _ := args["status"].(string)

	trades, err := s.repo.GetTradeHistory(ctx, agentID, limit, pair, status)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}

	tradeList := make([]map[string]any, 0, len(trades))
	for _, t := range trades {
		entry := map[string]any{
			"timestamp": t.Timestamp.Format(time.RFC3339),
			"pair":      t.Pair,
			"side":      t.Side,
			"status":    t.Status,
		}
		if t.DecisionHash != "" {
			entry["decision_hash"] = t.DecisionHash
		}
		if t.Status == StatusFill && t.Price.IsPositive() {
			fill := map[string]any{
				"id":    t.FillID,
				"price": t.Price.String(),
				"qty":   t.Qty.String(),
				"value": t.Value.String(),
			}
			if t.Fee.IsPositive() {
				fill["fee"] = t.Fee.String()
				fill["fee_asset"] = t.FeeAsset
				fill["fee_value"] = t.FeeValue.String()
			}
			entry["fill"] = fill
			entry["pnl_value"] = t.PnL.String()
			entry["portfolio"] = map[string]any{
				"value_after": t.ValueAfter.String(),
			}
		}
		if t.Status == StatusReject {
			// Infer reject source from reason text (DB doesn't store source separately).
			source := RejectSourcePolicy
			if strings.HasPrefix(t.Reason, "RiskRouter") {
				source = "risk_router"
			} else if strings.HasPrefix(t.Reason, "exchange") ||
				strings.HasPrefix(t.Reason, "reconciliation:") ||
				strings.HasPrefix(t.Reason, "kraken") {
				source = "exchange"
			}
			entry["reject"] = map[string]any{
				"source": source,
				"reason": t.Reason,
			}
		}
		// Include metadata from params if available
		if t.Params != nil {
			meta := make(map[string]any)
			for _, key := range []string{"strategy", "trigger_reason", "confidence", "reasoning"} {
				if v, ok := t.Params[key]; ok {
					meta[key] = v
				}
			}
			if len(meta) > 0 {
				entry["metadata"] = meta
			}
		}
		tradeList = append(tradeList, entry)
	}

	return mcp.JSONResult(map[string]any{
		"trades": tradeList,
		"count":  len(tradeList),
	})
}

func (s *Service) toolGetLimits(ctx context.Context, agentID string) (*mcp.ToolResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	state, err := s.getOrCreateAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}

	agentExch := s.exchangeFor(agentID)
	prices := agentExch.GetPrices()
	equity, _ := s.computeEquityFromState(ctx, state, prices)
	positions, _ := s.repo.GetOpenPositions(ctx, agentID)

	// Find largest position as % of equity
	var largestPositionPct float64
	var largestPair string
	hundred := decimal.NewFromInt(100)
	if equity.IsPositive() {
		for _, pos := range positions {
			var posValue decimal.Decimal
			if price, ok := prices[pos.Pair]; ok {
				posValue = pos.Qty.Mul(price)
			} else {
				posValue = pos.Value
			}
			pct := posValue.Div(equity).Mul(hundred).InexactFloat64()
			if pct > largestPositionPct {
				largestPositionPct = pct
				largestPair = pos.Pair
			}
		}
	}

	// Risk tier computation
	dayStartValue, _ := s.repo.GetDayStartValue(ctx, agentID)
	if dayStartValue.IsZero() {
		dayStartValue = state.InitialValue
	}
	var intradayDrawdownPct float64
	if dayStartValue.IsPositive() {
		intradayDrawdownPct = equity.Sub(dayStartValue).Div(dayStartValue).InexactFloat64() * 100
	}
	tier := computeRiskTier(intradayDrawdownPct / 100) // tier thresholds are fractions

	return mcp.JSONResult(map[string]any{
		"portfolio": map[string]any{
			"value":           equity.String(),
			"cash":            state.Cash.String(),
			"peak":            state.PeakValue.String(),
			"initial_value":   state.InitialValue.String(),
			"day_start_value": dayStartValue.String(),
		},
		"fill_count":            state.FillCount,
		"reject_count":          state.RejectCount,
		"largest_position_pct":  largestPositionPct,
		"largest_position_pair": largestPair,
		"halted":                state.Halted,
		"total_fees":            state.TotalFees.String(),
		"risk_tier":             tier,
		"risk_tier_label":       riskTierLabel(tier),
		"intraday_drawdown_pct": intradayDrawdownPct,
		"close_only":            tier >= 3,
		"max_order_pct":         riskTierMaxOrderPct(tier),
		"max_concentration_pct": riskTierMaxConcentration(tier),
	})
}

func (s *Service) toolGetPortfolioHistory(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	limit := 100
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	// Get fills (they have value_after values)
	trades, err := s.repo.GetTradeHistory(ctx, agentID, limit, "", StatusFill)
	if err != nil {
		return nil, fmt.Errorf("get trade history: %w", err)
	}

	// Build equity curve (reverse to chronological order)
	points := make([]map[string]any, 0, len(trades))
	for i := len(trades) - 1; i >= 0; i-- {
		t := trades[i]
		if t.ValueAfter.IsPositive() {
			points = append(points, map[string]any{
				"timestamp": t.Timestamp.Format(time.RFC3339),
				"portfolio": map[string]any{
					"value": t.ValueAfter.String(),
				},
				"pair": t.Pair,
				"side": t.Side,
			})
		}
	}

	return mcp.JSONResult(map[string]any{
		"equity_curve": points,
		"count":        len(points),
	})
}

func (s *Service) toolEstimateOrder(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	pair, _ := args["pair"].(string)
	side, _ := args["side"].(string)
	orderValue := parseDecimalArg(args, "value")

	if pair == "" || side == "" || !orderValue.IsPositive() {
		return nil, fmt.Errorf("pair, side, and value (>0) are required")
	}
	if side != "buy" && side != "sell" {
		return nil, fmt.Errorf("side must be \"buy\" or \"sell\", got %q", side)
	}
	agentExch := s.exchangeFor(agentID)
	price, ok := agentExch.GetPrice(pair)
	if !ok {
		return nil, fmt.Errorf("unknown pair: %s", pair)
	}

	state, err := s.getOrCreateAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}

	prices := agentExch.GetPrices()
	equity, _ := s.computeEquityFromState(ctx, state, prices)

	qty := orderValue.Div(price)

	result := map[string]any{
		"pair":  pair,
		"side":  side,
		"value": orderValue.String(),
		"price": price.String(),
		"qty":   qty.String(),
		"portfolio": map[string]any{
			"value": equity.String(),
			"cash":  state.Cash.String(),
			"peak":  state.PeakValue.String(),
		},
		"fill_count":   state.FillCount,
		"reject_count": state.RejectCount,
		"halted":       state.Halted,
	}

	if side == "buy" && orderValue.GreaterThan(state.Cash) {
		result["warning"] = "insufficient cash"
	}

	hundred := decimal.NewFromInt(100)
	if equity.IsPositive() {
		positions, _ := s.repo.GetOpenPositions(ctx, agentID)
		currentValue := decimal.Zero
		for _, pos := range positions {
			if pos.Pair == pair {
				if mktPrice, ok := prices[pos.Pair]; ok {
					currentValue = pos.Qty.Mul(mktPrice)
				} else {
					currentValue = pos.Value
				}
			}
		}
		var afterValue decimal.Decimal
		if side == "buy" {
			afterValue = currentValue.Add(orderValue)
		} else {
			afterValue = currentValue.Sub(orderValue)
			if afterValue.IsNegative() {
				afterValue = decimal.Zero
			}
		}
		result["position_pct_after"] = afterValue.Div(equity).Mul(hundred).InexactFloat64()
	}

	return mcp.JSONResult(result)
}

func (s *Service) toolHeartbeat(ctx context.Context, agentID string) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.policyLog)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	state, err := s.getOrCreateAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}

	agentExch := s.exchangeFor(agentID)
	prices := agentExch.GetPrices()
	equity, _ := s.computeEquityFromState(ctx, state, prices)

	// Update peak if new high - uses targeted update (only peak_value, conditional)
	// so it's safe without the full agent lock
	if equity.GreaterThan(state.PeakValue) {
		if updateErr := s.repo.UpdatePeakValue(ctx, agentID, equity); updateErr != nil {
			log.Error("Failed to update peak value", zap.Error(updateErr))
		}
	}

	// Re-read to get the authoritative peak_value (another request may have raised it)
	state, err = s.repo.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("re-read agent after peak update: %w", err)
	}

	// Swiftward heartbeat evaluation - checks 3-layer drawdown, may halt the agent
	halted := state.Halted
	if s.evaluator != nil {
		swCfg := s.svcCtx.Config().Swiftward
		priceMap := make(map[string]float64, len(prices))
		for k, v := range prices {
			priceMap[k] = v.InexactFloat64()
		}
		// Risk data for heartbeat drawdown checks
		rollingPeak, _ := s.repo.GetRollingPeak24h(ctx, agentID)
		if rollingPeak.IsZero() {
			rollingPeak = equity
		}
		dayStartValue, _ := s.repo.GetDayStartValue(ctx, agentID)
		if dayStartValue.IsZero() {
			dayStartValue = state.InitialValue
		}

		eventData := map[string]any{
			"prices":     priceMap,
			"check_type": "periodic",
			"portfolio": map[string]any{
				"value":            equity.InexactFloat64(),
				"peak":             state.PeakValue.InexactFloat64(),
				"cash":             state.Cash.InexactFloat64(),
				"initial_value":    state.InitialValue.InexactFloat64(),
				"rolling_peak_24h": rollingPeak.InexactFloat64(),
				"day_start_value":  dayStartValue.InexactFloat64(),
			},
		}
		result, evalErr := s.evaluator.EvaluateSync(ctx, swCfg.Stream, agentID, "heartbeat", eventData)
		if evalErr != nil {
			log.Warn("Swiftward heartbeat evaluation error - proceeding",
				zap.Error(evalErr),
			)
		} else if blocked, reason := result.ShouldBlock(); blocked {
			log.Warn("Heartbeat triggered halt",
				zap.String("reason", reason),
				zap.String("verdict", string(result.Verdict)),
			)
			if haltErr := s.SetHalted(ctx, agentID, true); haltErr != nil {
				log.Error("Failed to halt agent after heartbeat rejection",
					zap.Error(haltErr),
				)
			} else {
				halted = true
			}
		}
	}

	return mcp.JSONResult(map[string]any{
		"portfolio": map[string]any{
			"value": equity.String(),
			"cash":  state.Cash.String(),
			"peak":  state.PeakValue.String(),
		},
		"fill_count":   state.FillCount,
		"reject_count": state.RejectCount,
		"halted":       halted,
		"timestamp":    time.Now().Format(time.RFC3339),
	})
}

func (s *Service) toolSetConditional(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.alertLog)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	pair, _ := args["pair"].(string)
	alertType, _ := args["type"].(string)
	if pair == "" || alertType == "" {
		return nil, fmt.Errorf("pair and type are required")
	}
	if alertType != "stop_loss" && alertType != "take_profit" {
		return nil, fmt.Errorf("type must be stop_loss or take_profit")
	}

	triggerPrice, ok := args["trigger_price"].(float64)
	if !ok || triggerPrice <= 0 {
		return nil, fmt.Errorf("trigger_price is required and must be positive")
	}

	informAgent := true
	if v, ok := args["inform_agent"].(bool); ok {
		informAgent = v
	}

	note, _ := args["note"].(string)

	count, countErr := s.repo.CountActiveAlerts(ctx, agentID, "trading")
	if countErr != nil {
		return nil, fmt.Errorf("set conditional: count check: %w", countErr)
	}
	if count >= 10 {
		return nil, fmt.Errorf("set conditional: limit reached (%d active trading alerts, max 10)", count)
	}

	alertID := makePositionAlertID(agentID, pair, alertType, triggerPrice)

	// Tier 1: native exchange stop orders (disabled by default).
	// When enabled, delegates SL/TP execution to the exchange for lower latency.
	stopProv, hasNativeStop := s.exchangeFor(agentID).(exchange.StopOrderProvider)
	if s.enableNativeStops && hasNativeStop {
		stopResult, stopErr := stopProv.PlaceStopOrder(pair, "sell", alertType,
			decimal.NewFromFloat(triggerPrice), decimal.Zero)
		if stopErr != nil {
			log.Warn("Tier 1 stop order failed, falling back to Tier 2 poller",
				zap.String("pair", pair),
				zap.String("alert_type", alertType),
				zap.Error(stopErr),
			)
		} else {
			log.Info("Tier 1 native stop order placed",
				zap.String("order_id", stopResult.OrderID),
				zap.String("pair", pair),
				zap.String("alert_type", alertType),
				zap.String("stop_price", decimal.NewFromFloat(triggerPrice).StringFixed(4)),
			)
			// Track Tier 1 in DB so alert/list and cancel work
			record := &db.AlertRecord{
				AlertID:     alertID,
				AgentID:     agentID,
				Service:     "trading",
				Status:      "active",
				OnTrigger:   "auto_execute",
				MaxTriggers: 1,
				Params: map[string]any{
					"pair":            pair,
					"type":            alertType,
					"trigger_price":   triggerPrice,
					"tier":            "1",
					"native_order_id": stopResult.OrderID,
					"inform_agent":    informAgent,
				},
				Note: note,
			}
			if dbErr := s.repo.UpsertAlert(ctx, record); dbErr != nil {
				// DB write failed: cancel the native order to avoid a dangling exchange stop
				// that is invisible to alert/list and cancel.
				if cancelErr := stopProv.CancelStopOrder(stopResult.OrderID); cancelErr != nil {
					log.Error("Tier 1 DB tracking failed AND native cancel failed - manual cleanup required",
						zap.String("order_id", stopResult.OrderID),
						zap.String("pair", pair),
						zap.NamedError("db_err", dbErr),
						zap.NamedError("cancel_err", cancelErr),
					)
				} else {
					log.Warn("Tier 1 DB tracking failed - native order cancelled to prevent dangling stop",
						zap.String("pair", pair), zap.Error(dbErr))
				}
				// Fall through to Tier 2
			} else {
				return mcp.JSONResult(map[string]any{
					"alert_id": alertID,
					"order_id": stopResult.OrderID,
					"status":   "active",
					"tier":     "1",
				})
			}
		}
	}

	// Tier 2: DB-backed software polling conditional order.
	record := &db.AlertRecord{
		AlertID:     alertID,
		AgentID:     agentID,
		Service:     "trading",
		Status:      "active",
		OnTrigger:   "auto_execute",
		MaxTriggers: 1,
		Params: map[string]any{
			"pair":          pair,
			"type":          alertType,
			"trigger_price": triggerPrice,
			"inform_agent":  informAgent,
		},
		Note: note,
	}
	if err := s.repo.UpsertAlert(ctx, record); err != nil {
		return nil, fmt.Errorf("set conditional: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"alert_id": alertID,
		"status":   "active",
		"tier":     "2",
	})
}

func (s *Service) toolGetTriggeredAlerts(ctx context.Context, agentID string) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.alertLog)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	triggered, err := s.repo.GetTriggeredAlerts(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get triggered alerts: %w", err)
	}

	// Return all triggered alerts across all services (unified endpoint) and ack them.
	var alertIDs []string
	result := make([]map[string]any, 0, len(triggered))
	for _, a := range triggered {
		alertIDs = append(alertIDs, a.AlertID)
		m := map[string]any{
			"alert_id":      a.AlertID,
			"service":       a.Service,
			"status":        a.Status,
			"on_trigger":    a.OnTrigger,
			"note":          a.Note,
			"triggered_at":  a.TriggeredAt,
			"triage_prompt": a.TriagePrompt,
		}
		for k, v := range a.Params {
			m[k] = v
		}
		result = append(result, m)
	}
	if len(alertIDs) > 0 {
		if ackErr := s.repo.AckTriggeredAlerts(ctx, alertIDs); ackErr != nil {
			log.Warn("alert/triggered: ack failed", zap.Error(ackErr))
		}
	}

	return mcp.JSONResult(map[string]any{"alerts": result})
}

// toolListAlerts returns active (non-triggered) alerts for the agent across all services.
func (s *Service) toolListAlerts(ctx context.Context, agentID string) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.alertLog)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	agentExch := s.exchangeFor(agentID)
	prices := agentExch.GetPrices()

	var all []map[string]any
	for _, svc := range []string{"trading", "market", "news", "time"} {
		alerts, err := s.repo.GetActiveAlerts(ctx, agentID, svc)
		if err != nil {
			log.Warn("list_alerts: GetActiveAlerts failed", zap.String("service", svc), zap.Error(err))
			continue
		}
		for _, a := range alerts {
			m := map[string]any{
				"alert_id":   a.AlertID,
				"service":    a.Service,
				"on_trigger": a.OnTrigger,
				"note":       a.Note,
				"created_at": a.CreatedAt,
				"params":     a.Params,
			}
			if a.ExpiresAt != nil {
				m["expires_at"] = a.ExpiresAt
			}
			// Enrich trading alerts with tier, inform_agent, current price, and distance to trigger
			if svc == "trading" {
				tier := "2"
				if t, _ := a.Params["tier"].(string); t != "" {
					tier = t
				}
				m["tier"] = tier

				informAgent := true
				if v, ok := a.Params["inform_agent"].(bool); ok {
					informAgent = v
				}
				m["inform_agent"] = informAgent

				if pair, _ := a.Params["pair"].(string); pair != "" {
					if triggerPrice, ok := a.Params["trigger_price"].(float64); ok && triggerPrice > 0 {
						if currentPrice, ok := prices[pair]; ok {
							trigger := decimal.NewFromFloat(triggerPrice)
							m["current_price"] = currentPrice.String()
							hundred := decimal.NewFromInt(100)
							m["distance_pct"] = currentPrice.Sub(trigger).Div(trigger).Mul(hundred).StringFixed(2)
						}
					}
				}
			}
			all = append(all, m)
		}
	}
	if all == nil {
		all = []map[string]any{}
	}
	return mcp.JSONResult(map[string]any{"alerts": all, "count": len(all)})
}

func (s *Service) toolCancelConditional(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.alertLog)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}
	alertID, _ := args["alert_id"].(string)
	if alertID == "" {
		return nil, fmt.Errorf("alert_id is required")
	}

	// If Tier 1: cancel native exchange order first
	alerts, lookupErr := s.repo.GetActiveAlerts(ctx, agentID, "trading")
	if lookupErr == nil {
		for _, a := range alerts {
			if a.AlertID == alertID {
				if nativeID, _ := a.Params["native_order_id"].(string); nativeID != "" {
					if stopProv, ok := s.exchangeFor(agentID).(exchange.StopOrderProvider); ok {
						if err := stopProv.CancelStopOrder(nativeID); err != nil {
							log.Warn("Tier 1 native cancel failed (proceeding with DB cancel)",
								zap.String("native_order_id", nativeID), zap.Error(err))
						}
					}
				}
				break
			}
		}
	}

	if err := s.repo.CancelAlert(ctx, agentID, alertID); err != nil {
		return nil, err
	}
	return mcp.JSONResult(map[string]any{"cancelled": true, "alert_id": alertID})
}

// makePositionAlertID generates a deterministic ID for a position alert.
func makePositionAlertID(agentID, pair, alertType string, triggerPrice float64) string {
	// Include UnixNano to avoid collisions when re-setting the same trigger level
	// after a previous alert was cancelled or exhausted.
	raw := fmt.Sprintf("%s:%s:%s:%.6f:%d", agentID, pair, alertType, triggerPrice, time.Now().UnixNano())
	sum := sha256.Sum256([]byte(raw))
	return "palert-" + hex.EncodeToString(sum[:])[:16]
}

// trackNativeAlert writes a DB record for a Tier 1 native exchange stop order.
// This makes it visible in alert/list and cancellable via trade/cancel_conditional.
func (s *Service) trackNativeAlert(ctx context.Context, agentID, pair, alertType string, triggerPrice decimal.Decimal, nativeOrderID, groupID, note string, informAgent bool) {
	raw := fmt.Sprintf("%s:%s:%s:%.6f:%s", agentID, pair, alertType, triggerPrice.InexactFloat64(), groupID)
	sum := sha256.Sum256([]byte(raw))
	alertID := "palert-" + hex.EncodeToString(sum[:])[:16]
	record := &db.AlertRecord{
		AlertID:     alertID,
		AgentID:     agentID,
		Service:     "trading",
		Status:      "active",
		OnTrigger:   "auto_execute",
		MaxTriggers: 1,
		Params: map[string]any{
			"pair":            pair,
			"type":            alertType,
			"trigger_price":   triggerPrice.InexactFloat64(),
			"tier":            "1",
			"native_order_id": nativeOrderID,
			"inform_agent":    informAgent,
		},
		GroupID: groupID,
		Note:    note,
	}
	if err := s.repo.UpsertAlert(ctx, record); err != nil {
		s.log.Warn("Tier 1 DB tracking failed", zap.String("order_id", nativeOrderID), zap.Error(err))
	}
}

// createConditionalOrder creates a platform conditional order (Tier 2) for SL/TP.
// Used by both submit_order (auto-create on fill) and trade/set_conditional (manual).
// groupID links OCO orders (e.g., SL and TP cancel each other when one triggers).
func (s *Service) createConditionalOrder(ctx context.Context, agentID, pair, alertType, condition string, triggerPrice decimal.Decimal, groupID, note string, informAgent bool) error {
	// Include groupID in the alert ID hash so each trade's SL/TP gets a unique ID.
	// Without this, a second buy at the same SL price would collide with the first
	// (exhausted/cancelled) alert and fail to create.
	raw := fmt.Sprintf("%s:%s:%s:%.6f:%s", agentID, pair, alertType, triggerPrice.InexactFloat64(), groupID)
	sum := sha256.Sum256([]byte(raw))
	alertID := "palert-" + hex.EncodeToString(sum[:])[:16]
	record := &db.AlertRecord{
		AlertID:     alertID,
		AgentID:     agentID,
		Service:     "trading",
		Status:      "active",
		OnTrigger:   "auto_execute",
		MaxTriggers: 1,
		Params: map[string]any{
			"pair":          pair,
			"type":          alertType,
			"trigger_price": triggerPrice.InexactFloat64(),
			"condition":     condition,
			"inform_agent":  informAgent,
		},
		GroupID: groupID,
		Note:    note,
	}
	return s.repo.UpsertAlert(ctx, record)
}

// toolSetReminder creates a one-shot time-based reminder alert.
// The agent provides a fire_at timestamp (RFC3339); the time poller fires it when due.
func (s *Service) toolSetReminder(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	fireAtStr, _ := args["fire_at"].(string)
	if fireAtStr == "" {
		return nil, fmt.Errorf("fire_at is required (RFC3339 timestamp, e.g. 2026-04-01T15:00:00Z)")
	}
	fireAt, err := time.Parse(time.RFC3339, fireAtStr)
	if err != nil {
		return nil, fmt.Errorf("fire_at: invalid RFC3339 timestamp: %w", err)
	}
	if fireAt.Before(time.Now().Add(-1 * time.Minute)) {
		return nil, fmt.Errorf("fire_at must be in the future (got %s)", fireAtStr)
	}

	note, _ := args["note"].(string)
	onTrigger, _ := args["on_trigger"].(string)
	if onTrigger == "" {
		onTrigger = "wake_full"
	}
	if onTrigger != "wake_full" && onTrigger != "wake_triage" {
		return nil, fmt.Errorf("on_trigger must be wake_full or wake_triage")
	}

	raw := fmt.Sprintf("%s:time:%s", agentID, fireAtStr)
	sum := sha256.Sum256([]byte(raw))
	alertID := "reminder-" + hex.EncodeToString(sum[:])[:16]

	count, countErr := s.repo.CountActiveAlerts(ctx, agentID, "time")
	if countErr != nil {
		return nil, fmt.Errorf("set reminder: count check: %w", countErr)
	}
	if count >= 10 {
		return nil, fmt.Errorf("set reminder: limit reached (%d active time alerts, max 10)", count)
	}

	record := &db.AlertRecord{
		AlertID:     alertID,
		AgentID:     agentID,
		Service:     "time",
		Status:      "active",
		OnTrigger:   onTrigger,
		MaxTriggers: 1,
		Params: map[string]any{
			"fire_at": fireAtStr,
		},
		Note: note,
	}
	if err := s.repo.UpsertAlert(ctx, record); err != nil {
		return nil, fmt.Errorf("set reminder: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"alert_id":   alertID,
		"fire_at":    fireAtStr,
		"on_trigger": onTrigger,
		"status":     "active",
	})
}

// runTimeAlertPoller polls for due time-based reminders and marks them triggered.
func (s *Service) runTimeAlertPoller() {
	ticker := time.NewTicker(s.alertPollInterval)
	defer ticker.Stop()
	ctx := s.svcCtx.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkTimeAlerts(ctx)
		}
	}
}

func (s *Service) checkTimeAlerts(ctx context.Context) {
	alerts, err := s.repo.GetDueTimeAlerts(ctx)
	if err != nil {
		s.alertLog.Warn("Time alert poll failed", zap.Error(err))
		return
	}
	for _, a := range alerts {
		triggered, err := s.repo.MarkAlertTriggered(ctx, a.AlertID, "")
		if err != nil {
			s.alertLog.Warn("Mark time alert triggered failed", zap.String("alert_id", a.AlertID), zap.Error(err))
			continue
		}
		if triggered {
			s.alertLog.Info(fmt.Sprintf("Time reminder triggered for %s: %s", a.AgentID, observability.LogPreview(a.Note, 80)),
				zap.String("alert_id", a.AlertID),
				zap.String("agent_id", a.AgentID),
				zap.String("note", a.Note),
			)
		}
	}
}

// toolEndCycle posts a session checkpoint attestation to the ValidationRegistry.
// Called by the agent at the end of every analysis cycle, whether it traded or not.
func (s *Service) toolEndCycle(ctx context.Context, agentID string, args map[string]any) (*mcp.ToolResult, error) {
	log := observability.LoggerFromCtx(ctx, s.chainLog)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}

	summary, _ := args["summary"].(string)
	if summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	if s.validationReg == nil {
		return mcp.JSONResult(map[string]any{"status": "ok", "attestation": attestationStatusDisabled})
	}
	if s.isAttestationDisabled(agentID) {
		return mcp.JSONResult(map[string]any{"status": "ok", "attestation": attestationStatusDisabled})
	}

	entry, ok := s.agentKeys[agentID]
	if !ok || entry.key == nil || entry.tokenID == nil {
		return mcp.JSONResult(map[string]any{"status": "ok", "attestation": attestationStatusDisabled})
	}

	attestCtx, attestCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer attestCancel()

	cp := &chain.TradeCheckpointData{
		AgentID:          entry.tokenID,
		Timestamp:        big.NewInt(time.Now().Unix()),
		Action:           "HOLD",
		Asset:            "",
		Pair:             "",
		AmountUsdScaled:  big.NewInt(0),
		PriceUsdScaled:   big.NewInt(0),
		ReasoningHash:    chain.ReasoningHash(summary),
		ConfidenceScaled: big.NewInt(0),
		IntentHash:       [32]byte{},
	}

	registryAddr := common.HexToAddress(s.svcCtx.Config().Chain.IdentityRegistryAddr)
	_, cpHash, signErr := chain.SignCheckpoint(cp, entry.key, s.chainClient.ChainID(), registryAddr)
	if signErr != nil {
		log.Error("Failed to sign end_cycle checkpoint", zap.Error(signErr))
		return mcp.JSONResult(map[string]any{"status": "error", "error": "attestation signing failed", "detail": signErr.Error()})
	}

	var score uint8
	if s.hardcodedConfidence > 0 {
		score = uint8(s.hardcodedConfidence)
	} else {
		score = 80
	}

	notes := summary
	if len(notes) > 200 {
		notes = notes[:200]
	}

	if _, err := s.validationReg.PostEIP712Attestation(attestCtx, entry.key, entry.tokenID, cpHash, score, notes); err != nil {
		log.Error("end_cycle attestation failed", zap.Error(err))
		// Session checkpoints have no trade row, so there's no state machine
		// to update. We classify the error to (a) trip the per-process circuit
		// breaker on terminal chain errors, and (b) tell the agent whether the
		// failure is recoverable (waiting_for_gas) or terminal (error) using
		// the same taxonomy as the trade-fill path.
		if isAttestationCircuitBreakerError(err) {
			s.attestationDisabled.Store(agentID, struct{}{})
			log.Error("Attestation disabled for agent (terminal chain error)", zap.Error(err))
		}
		_ = attestationFailureKindLabel(classifyAttestationFailure(err), false)
		return mcp.JSONResult(map[string]any{
			"status":      "ok",
			"attestation": "unavailable",
		})
	}

	log.Info(fmt.Sprintf("End cycle attestation posted for %s", agentID))
	return mcp.JSONResult(map[string]any{"status": "ok", "attestation": attestationStatusSuccess})
}

// --- Reconciliation: DB <-> Exchange consistency ---

// reconcileAllAgents runs reconciliation for every known agent (configured + dynamic).
// isStartup gates the attestation recovery phase: attestation retry is ONLY
// safe at startup, not on the periodic ticker. At startup we retry trades left
// in pending/waiting_for_gas by a previous crash. On the ticker we only run the
// cheap, DB-local reconciliation (fill history, hash chain recovery).
func (s *Service) reconcileAllAgents(ctx context.Context, isStartup bool) {
	// Start with configured agents.
	agentIDs := make(map[string]bool, len(s.agents))
	for agentID := range s.agents {
		agentIDs[agentID] = true
	}
	// Also include dynamic agents that have pending trades in DB.
	if pending, err := s.repo.GetPendingTrades(ctx, ""); err == nil {
		for _, pt := range pending {
			agentIDs[pt.AgentID] = true
		}
	}
	for agentID := range agentIDs {
		if err := s.reconcileAgent(ctx, agentID, isStartup); err != nil {
			s.reconcileLog.Error(fmt.Sprintf("Reconciliation failed for %s", agentID), zap.String("agent_id", agentID), zap.Error(err))
		}
	}
}

// reconcileAgent ensures DB and exchange are in sync for one agent.
// Phase 1: resolve pending trades using exchange fill history.
// Phase 2: detect orphan exchange fills missing from DB.
// Phase 3a: recover missing hash chains (always runs).
// Phase 3b: recover missing attestations (only when isStartup=true).
func (s *Service) reconcileAgent(ctx context.Context, agentID string, isStartup bool) error {
	// Only works with exchanges that support fill history.
	// Use per-agent exchange client (not root) to read the correct Kraken HOME dir.
	agentExch := s.exchangeFor(agentID)
	histProvider, ok := agentExch.(exchange.FillHistoryProvider)
	if !ok {
		return nil
	}

	// Acquire agent lock BEFORE fetching fill history to avoid TOCTOU race.
	// Without this, a trade could fill between GetFillHistory and LockAgent,
	// causing reconciliation to miss the fill and incorrectly reject a pending trade.
	unlock, lockErr := s.repo.LockAgent(ctx, agentID)
	if lockErr != nil {
		return fmt.Errorf("acquire lock: %w", lockErr)
	}
	defer unlock()

	fills, err := histProvider.GetFillHistory(agentID)
	if err != nil {
		return fmt.Errorf("get fill history: %w", err)
	}

	// Phase 1: resolve pending trades.
	matchedFillIDs := make(map[string]bool)
	pendingTrades, err := s.repo.GetPendingTrades(ctx, agentID)
	if err != nil {
		return fmt.Errorf("get pending trades: %w", err)
	}
	for _, pt := range pendingTrades {
		matched := matchPendingToFill(pt, fills, matchedFillIDs)
		if matched != nil {
			matchedFillIDs[matched.TradeID] = true // consume: prevents same fill matching another pending
			if err := s.finalizePendingFromHistory(ctx, agentID, pt, matched); err != nil {
				s.reconcileLog.Error("Failed to finalize pending trade from history",
					zap.Int64("trade_id", pt.ID), zap.Error(err))
			} else {
				s.reconcileLog.Info(fmt.Sprintf("Reconciliation: resolved trade #%d %s %s %s", pt.ID, agentID, pt.Side, pt.Pair),
					zap.Int64("trade_id", pt.ID),
					zap.String("fill_id", matched.TradeID),
					zap.String("pair", pt.Pair),
					zap.String("side", pt.Side))
			}
		} else if time.Since(pt.Timestamp) > 2*time.Minute {
			reason := "reconciliation: no matching exchange fill"
			if err := s.repo.RejectPendingTrade(ctx, agentID, pt.ID, reason); err != nil {
				s.reconcileLog.Error("Failed to reject stale pending trade", zap.Int64("trade_id", pt.ID), zap.Error(err))
			} else {
				s.reconcileLog.Info(fmt.Sprintf("Reconciliation: stale trade #%d %s %s rejected", pt.ID, agentID, pt.Pair),
					zap.Int64("trade_id", pt.ID),
					zap.String("pair", pt.Pair))
			}
		}
		// Recent pending trades (< 2 min) are left alone - might still be in-flight.
	}

	// Phase 2: detect orphan exchange fills.
	knownFillIDs, err := s.repo.GetFilledFillIDs(ctx, agentID)
	if err != nil {
		return fmt.Errorf("get filled fill_ids: %w", err)
	}
	for i := range fills {
		fill := &fills[i]
		if _, known := knownFillIDs[fill.TradeID]; known {
			continue // already in DB
		}
		if matchedFillIDs[fill.TradeID] {
			continue // just matched to a pending trade in Phase 1
		}
		if err := s.insertOrphanFill(ctx, agentID, fill); err != nil {
			s.reconcileLog.Error("Failed to insert orphan fill",
				zap.String("fill_id", fill.TradeID), zap.Error(err))
		} else {
			s.reconcileLog.Info("Reconciliation: orphan exchange fill inserted",
				zap.String("fill_id", fill.TradeID),
				zap.String("pair", fill.Pair),
				zap.String("side", fill.Side))
		}
	}

	// Phase 3a: recover missing hash chains (DB-only, fast, needs lock for chain ordering).
	s.recoverHashChains(ctx, agentID)

	// Release lock before attestation recovery - on-chain calls can take 90s+ per trade.
	unlock()
	unlock = func() {} //nolint:ineffassign,staticcheck // reassign to no-op so deferred unlock() is safe

	// Phase 3b: recover missing attestations — STARTUP ONLY. The user contract
	// is that normal restarts retry pending/waiting_for_gas trades; the periodic
	// ticker must NOT retry or we get a background retry storm whenever the
	// wallet is underfunded.
	if isStartup {
		s.recoverAttestations(ctx, agentID)
	}

	return nil
}

// recoverHashChains finds filled trades missing hash evidence and recomputes.
// Must be called while holding the agent advisory lock (chain ordering depends on it).
func (s *Service) recoverHashChains(ctx context.Context, agentID string) {
	log := s.log.With(zap.String("agent_id", agentID))

	tradesNoHash, err := s.repo.GetFilledTradesWithoutEvidence(ctx, agentID, "hash")
	if err != nil {
		log.Error("Failed to query trades without hash", zap.Error(err))
		return
	}
	for _, t := range tradesNoHash {
		var evidenceData map[string]any
		if t.Evidence != nil {
			if d, ok := t.Evidence["data"].(map[string]any); ok {
				evidenceData = d
			}
		}
		if evidenceData == nil {
			// No evidence.data - either a legacy trade (pre-migration 008, evidence={})
			// or a bug in a newer code path. Stamp to stop re-scanning every cycle.
			//
			// The attestation sub-object MUST be a proper state-machine map
			// (status=disabled). Writing the bare string "legacy" here (as was
			// done previously) silently defeats the attestation state machine:
			// `evidence->'attestation'->>'status'` on a string JSON value
			// returns NULL, so the recovery query would match it, pick it up,
			// and try to post a bogus on-chain attestation with "legacy" as
			// the decision hash. Using a proper disabled object makes the
			// trade terminal from the attestation recovery point of view.
			if len(t.Evidence) > 0 {
				log.Warn("Trade has evidence but no evidence.data - possible bug",
					zap.Int64("trade_id", t.ID), zap.Any("evidence_keys", t.Evidence))
			}
			if stampErr := s.updateEvidenceWithRetry(ctx, t.ID, map[string]any{
				"hash": "legacy",
				"attestation": map[string]any{
					"status": attestationStatusDisabled,
					"reason": "legacy_trade_no_evidence_data",
				},
			}); stampErr != nil {
				log.Error("Failed to stamp unrecoverable trade", zap.Int64("trade_id", t.ID), zap.Error(stampErr))
			} else {
				log.Info("Stamped unrecoverable trade (no evidence.data)",
					zap.Int64("trade_id", t.ID))
			}
			continue
		}
		prevHash, prevErr := s.repo.GetLatestTraceHash(ctx, agentID)
		if prevErr != nil {
			log.Error("Failed to get prev hash for recovery", zap.Error(prevErr))
			continue
		}
		if prevHash == "" {
			prevHash = evidence.ZeroHash
		}
		hash, hashErr := evidence.ComputeDecisionHash(log, evidenceData, prevHash)
		if hashErr != nil {
			log.Error("Failed to compute hash for recovery", zap.Error(hashErr))
			continue
		}
		// Insert trace FIRST, then update evidence. If crash between them:
		// - Trade still shows "no hash" (evidence not updated yet)
		// - Next run recomputes the SAME hash (deterministic: same data + same prev_hash)
		// - InsertTrace does ON CONFLICT DO NOTHING (same hash already exists)
		// - UpdateEvidence succeeds, completing the recovery
		// This ordering prevents hash chain gaps.
		traceJSON, _ := json.Marshal(evidenceData)
		if insertErr := s.repo.InsertTrace(ctx, &db.DecisionTrace{
			DecisionHash: hash,
			AgentID:      agentID,
			PrevHash:     prevHash,
			TraceJSON:    traceJSON,
		}); insertErr != nil {
			log.Error("Failed to insert recovered trace", zap.Error(insertErr))
			continue
		}
		if updateErr := s.updateEvidenceWithRetry(ctx, t.ID, map[string]any{
			"hash":      hash,
			"prev_hash": prevHash,
		}); updateErr != nil {
			log.Error("Failed to update hash evidence", zap.Error(updateErr))
			continue
		}
		if updateErr := s.repo.UpdateTradeHash(ctx, t.FillID, hash); updateErr != nil {
			log.Error("Failed to backfill decision_hash", zap.Error(updateErr))
		}
		log.Info("Recovery: hash chain restored",
			zap.Int64("trade_id", t.ID), zap.String("hash", hash))
	}
}

// truncateNotesReasoning returns a shallow copy of evidenceData with reasoning
// truncated to maxLen. Returns the original map if no truncation is needed.
func truncateNotesReasoning(evidenceData map[string]any, maxLen int) map[string]any {
	if maxLen <= 0 || evidenceData == nil {
		return evidenceData
	}
	intent, ok := evidenceData["intent"].(map[string]any)
	if !ok {
		return evidenceData
	}
	p, ok := intent["params"].(map[string]any)
	if !ok {
		return evidenceData
	}
	r, ok := p["reasoning"].(string)
	if !ok || len(r) <= maxLen {
		return evidenceData
	}
	pCopy := make(map[string]any, len(p))
	for k, v := range p {
		pCopy[k] = v
	}
	pCopy["reasoning"] = r[:maxLen]
	intentCopy := make(map[string]any, len(intent))
	for k, v := range intent {
		intentCopy[k] = v
	}
	intentCopy["params"] = pCopy
	out := make(map[string]any, len(evidenceData))
	for k, v := range evidenceData {
		out[k] = v
	}
	out["intent"] = intentCopy
	return out
}

// recoverAttestations retries attestations for filled trades in a recoverable
// state (null attestation, pending, or waiting_for_gas). Called only at startup
// (not periodic reconciliation) so a single restart is the retry boundary.
// Runs without the agent lock since on-chain calls can take 90s+ per trade.
func (s *Service) recoverAttestations(ctx context.Context, agentID string) {
	log := s.log.With(zap.String("agent_id", agentID))

	if s.validationReg == nil {
		log.Debug("recoverAttestations: skipped, ValidationRegistry not configured")
		return
	}
	if s.isAttestationDisabled(agentID) {
		log.Debug("recoverAttestations: skipped, attestation disabled for agent")
		return
	}
	recoverable, err := s.repo.GetFilledTradesPendingAttestation(ctx, agentID)
	if err != nil {
		log.Error("Failed to query trades with recoverable attestation state", zap.Error(err))
		return
	}
	entry, ok := s.agentKeys[agentID]
	if !ok || entry.key == nil || entry.tokenID == nil {
		log.Debug("recoverAttestations: skipped, agent missing ERC-8004 keys/tokenID", zap.Bool("has_entry", ok))
		return
	}
	if len(recoverable) == 0 {
		log.Debug("recoverAttestations: no recoverable trades")
		return
	}
	log.Warn(fmt.Sprintf("recoverAttestations: %d recoverable trades (null / pending / pending_confirm / waiting_for_gas)", len(recoverable)))
	for _, t := range recoverable {
		currentStatus, currentAttemptCount := readAttestationState(t.Evidence)

		// pending_confirm: tx was already posted on-chain on a previous run,
		// but the success DB write failed. Finalize from the stored tx_hash
		// with NO chain call - this is the whole point of the state.
		if currentStatus == attestationStatusPendingConfirm {
			s.finalizePendingConfirm(ctx, log, t.ID, t.Evidence)
			continue
		}

		// Need hash and evidence.data to build the attestation.
		var hashStr, prevHashStr string
		if t.Evidence != nil {
			if h, ok := t.Evidence["hash"].(string); ok {
				hashStr = h
			}
			if ph, ok := t.Evidence["prev_hash"].(string); ok {
				prevHashStr = ph
			}
		}
		if hashStr == "" {
			// Can't attest without hash - mark as terminal error so we don't spin on it forever.
			log.Warn("recoverAttestations: trade missing hash, marking as error", zap.Int64("trade_id", t.ID))
			if writeErr := s.writeAttestationError(ctx, t.ID, "recovery: trade missing decision hash, cannot build checkpoint"); writeErr != nil {
				log.Error("Recovery: failed to mark hash-missing trade as error",
					zap.Int64("trade_id", t.ID), zap.Error(writeErr))
			}
			continue
		}

		// Build checkpoint from trade data.
		action := "BUY"
		if t.Side == "sell" {
			action = "SELL"
		}
		amountScaled := t.Value.Mul(decimal.NewFromInt(100)).BigInt()
		priceScaled := t.Price.Mul(decimal.NewFromInt(100)).BigInt()

		var reasoningHash [32]byte
		confidenceScaled := big.NewInt(0)
		if t.Params != nil {
			if r, ok := t.Params["reasoning"].(string); ok && r != "" {
				reasoningHash = chain.ReasoningHash(r)
			}
			if c, ok := t.Params["confidence"].(float64); ok && c > 0 {
				confidenceScaled = big.NewInt(int64(math.Round(c * 1000)))
			}
		}

		// Recover intentHash from evidence if available.
		var intentHash [32]byte
		if t.Evidence != nil {
			if d, ok := t.Evidence["data"].(map[string]any); ok {
				if rr, ok := d["risk_router"].(map[string]any); ok {
					if ih, ok := rr["intent_hash"].(string); ok && len(ih) > 2 {
						decoded, decodeErr := hex.DecodeString(strings.TrimPrefix(ih, "0x"))
						if decodeErr != nil {
							// Corrupted evidence. Don't silently use zero - log so
							// operators can see why the on-chain correlation is lost.
							log.Warn("recoverAttestations: intent_hash hex decode failed, using zero hash",
								zap.Int64("trade_id", t.ID),
								zap.String("intent_hash_raw", ih),
								zap.Error(decodeErr))
						} else if len(decoded) != 32 {
							log.Warn("recoverAttestations: intent_hash has wrong length, using zero hash",
								zap.Int64("trade_id", t.ID),
								zap.String("intent_hash_raw", ih),
								zap.Int("decoded_len", len(decoded)))
						} else {
							copy(intentHash[:], decoded)
						}
					}
				}
			}
		}

		cp := &chain.TradeCheckpointData{
			AgentID:          entry.tokenID,
			Timestamp:        big.NewInt(t.Timestamp.Unix()),
			Action:           action,
			Asset:            pairBase(t.Pair),
			Pair:             t.Pair,
			AmountUsdScaled:  amountScaled,
			PriceUsdScaled:   priceScaled,
			ReasoningHash:    reasoningHash,
			ConfidenceScaled: confidenceScaled,
			IntentHash:       intentHash,
		}

		registryAddr := common.HexToAddress(s.svcCtx.Config().Chain.IdentityRegistryAddr)
		_, cpHash, signErr := chain.SignCheckpoint(cp, entry.key, s.chainClient.ChainID(), registryAddr)
		if signErr != nil {
			log.Error("Recovery: failed to sign checkpoint", zap.Error(signErr), zap.Int64("trade_id", t.ID))
			if writeErr := s.writeAttestationError(ctx, t.ID, "recovery: sign checkpoint: "+signErr.Error()); writeErr != nil {
				log.Error("Recovery: failed to mark sign-failure as error",
					zap.Int64("trade_id", t.ID), zap.Error(writeErr))
			}
			continue
		}

		var score uint8
		if s.hardcodedConfidence > 0 {
			score = uint8(s.hardcodedConfidence)
		} else if t.Params != nil {
			if c, ok := t.Params["confidence"].(float64); ok && c > 0 {
				score = uint8(math.Round(c * 100))
			}
		}

		// Build notes from evidence (truncate reasoning for calldata cost).
		var evidenceData map[string]any
		if t.Evidence != nil {
			if d, ok := t.Evidence["data"].(map[string]any); ok {
				evidenceData = d
			}
		}
		evidenceData = truncateNotesReasoning(evidenceData, s.maxNotesReasoning)
		notesObj := map[string]any{
			"data":      evidenceData,
			"hash":      hashStr,
			"prev_hash": prevHashStr,
		}
		notesJSON, _ := json.Marshal(notesObj)

		// Delegate to the shared helper for pre-tx marker, chain call,
		// classification, and state writes. Identical rules in inline + recovery.
		attestCtx, attestCancel := context.WithTimeout(ctx, 90*time.Second)
		gasCount := previousGasAttemptCount(t.Evidence)
		attemptRes := s.attemptAttestation(
			ctx, attestCtx, log,
			t.ID, agentID, entry,
			cpHash, score, string(notesJSON),
			currentStatus, currentAttemptCount, gasCount,
		)
		attestCancel()

		switch attemptRes.statusStr {
		case attestationStatusSuccess:
			log.Info("Recovery: attestation posted",
				zap.Int64("trade_id", t.ID), zap.String("tx_hash", attemptRes.txHashHex))
		case attestationStatusPendingConfirm:
			log.Warn("Recovery: wrote pending_confirm fallback after success DB write failed",
				zap.Int64("trade_id", t.ID), zap.String("tx_hash", attemptRes.txHashHex))
		case attestationStatusWaitingForGas:
			// Circuit breaker is now tripped. Remaining trades would hit the
			// same gas failure - stop early to avoid spamming with the exact
			// same RPC error.
			return
		case attestationStatusError:
			// Terminal chain error may have tripped the circuit breaker. If so,
			// the remaining trades would just be rejected on the breaker check
			// of the next retry, so stop early.
			if s.isAttestationDisabled(agentID) {
				return
			}
		}
	}
}

// matchPendingToFill finds the closest Kraken fill matching a pending trade by pair, side, and time.
// Already-consumed fills (matched to a previous pending trade in this run) are excluded.
func matchPendingToFill(pending db.TradeRecord, fills []exchange.ExchangeFill, consumed map[string]bool) *exchange.ExchangeFill {
	var best *exchange.ExchangeFill
	bestDelta := time.Duration(math.MaxInt64)

	for i := range fills {
		f := &fills[i]
		if consumed[f.TradeID] {
			continue // already matched to another pending trade
		}
		if f.Pair != pending.Pair || f.Side != pending.Side {
			continue
		}
		delta := f.Time.Sub(pending.Timestamp)
		if delta < 0 {
			delta = -delta
		}
		if delta > 60*time.Second {
			continue // too far apart
		}
		if delta < bestDelta {
			best = f
			bestDelta = delta
		}
	}
	return best
}

// finalizePendingFromHistory reconstructs fill data from Kraken history and finalizes a pending trade.
func (s *Service) finalizePendingFromHistory(ctx context.Context, agentID string, pending db.TradeRecord, fill *exchange.ExchangeFill) error {
	// Convert fee from quote to base for buys (same logic as KrakenClient.parseFill).
	var qty, quoteQty, fee, feeValue decimal.Decimal
	var feeAsset string
	if fill.Side == "buy" {
		feeInBase := fill.Fee.Div(fill.Price).Round(8)
		qty = fill.Volume.Sub(feeInBase)
		quoteQty = fill.Cost
		fee = feeInBase
		feeAsset = pairBase(fill.Pair)
		feeValue = fill.Fee // fee in quote = portfolio currency value
	} else {
		qty = fill.Volume
		quoteQty = fill.Cost.Sub(fill.Fee)
		fee = fill.Fee
		feeAsset = pairQuote(fill.Pair)
		feeValue = fill.Fee
	}

	// Compute cash delta.
	var cashDelta decimal.Decimal
	if fill.Side == "buy" {
		cashDelta = quoteQty.Neg()
	} else {
		cashDelta = quoteQty
	}

	// Compute PnL for sells.
	var pnl decimal.Decimal
	if fill.Side == "sell" {
		positions, _ := s.repo.GetOpenPositions(ctx, agentID)
		for _, pos := range positions {
			if pos.Pair == fill.Pair && pos.AvgPrice.IsPositive() {
				pnl = fill.Price.Sub(pos.AvgPrice).Mul(qty).Sub(fee)
				break
			}
		}
	}

	// Compute equity after fill.
	state, _ := s.repo.GetAgent(ctx, agentID)
	agentExch := s.exchangeFor(agentID)
	prices := agentExch.GetPrices()
	newCash := state.Cash.Add(cashDelta)
	positions, _ := s.repo.GetOpenPositions(ctx, agentID)
	equity := newCash
	for _, pos := range positions {
		if p, ok := prices[pos.Pair]; ok {
			equity = equity.Add(pos.Qty.Mul(p))
		}
	}
	// Adjust for this fill's contribution (positions are pre-fill snapshot).
	if fill.Side == "buy" {
		if p, ok := prices[fill.Pair]; ok {
			equity = equity.Add(qty.Mul(p))
		}
	} else {
		// Sell: subtract the sold qty from the existing position value.
		if p, ok := prices[fill.Pair]; ok {
			equity = equity.Sub(qty.Mul(p))
		}
	}
	peakValue := state.PeakValue
	if equity.GreaterThan(peakValue) {
		peakValue = equity
	}

	// Build evidence data for the reconciled fill.
	// Start from pending trade's existing evidence (may have intent/swiftward/risk_router from the original request).
	var evidenceData map[string]any
	if pending.Evidence != nil {
		if d, ok := pending.Evidence["data"].(map[string]any); ok {
			evidenceData = d
		}
	}
	if evidenceData == nil {
		// Minimal fallback if no evidence was stored on the pending record.
		evidenceData = map[string]any{
			"intent": map[string]any{
				"pair":  pending.Pair,
				"side":  pending.Side,
				"value": pending.Qty.String(),
			},
		}
	}
	evidenceData["fill"] = map[string]any{
		"id":        fill.TradeID,
		"price":     fill.Price.String(),
		"qty":       qty.String(),
		"fee":       fee.String(),
		"fee_asset": feeAsset,
		"fee_value": feeValue.String(),
	}

	// Compute hash chain.
	var decisionHash string
	var traceRecord *db.DecisionTrace
	prevHash, prevErr := s.repo.GetLatestTraceHash(ctx, agentID)
	if prevErr == nil {
		if prevHash == "" {
			prevHash = evidence.ZeroHash
		}
		hash, hashErr := evidence.ComputeDecisionHash(s.log, evidenceData, prevHash)
		if hashErr == nil {
			decisionHash = hash
			if traceJSON, marshalErr := json.Marshal(evidenceData); marshalErr == nil {
				traceRecord = &db.DecisionTrace{
					DecisionHash: decisionHash,
					AgentID:      agentID,
					PrevHash:     prevHash,
					TraceJSON:    traceJSON,
				}
			}
		}
	}

	fillEvidence := map[string]any{"data": evidenceData}
	if decisionHash != "" {
		fillEvidence["hash"] = decisionHash
		fillEvidence["prev_hash"] = prevHash
	}

	fillUpdate := &db.TradeFillUpdate{
		TradeID:      pending.ID,
		Qty:          qty,
		Price:        fill.Price,
		Value:        quoteQty,
		PnL:          pnl,
		Status:       StatusFill,
		ValueAfter:   equity,
		FillID:       fill.TradeID,
		Fee:          fee,
		FeeAsset:     feeAsset,
		FeeValue:     feeValue,
		Evidence:     fillEvidence,
		DecisionHash: decisionHash,
		Trace:        traceRecord,
	}
	stateUpdate := &db.StateUpdate{
		AgentID:       agentID,
		CashDelta:     cashDelta,
		PeakValue:     peakValue,
		FillCountIncr: 1,
		FeeDelta:      feeValue,
	}
	return s.repo.FinalizeTrade(ctx, stateUpdate, fillUpdate)
}

// insertOrphanFill inserts a new filled trade for an exchange fill that has no DB record.
func (s *Service) insertOrphanFill(ctx context.Context, agentID string, fill *exchange.ExchangeFill) error {
	// Convert fee from quote to base for buys.
	var qty, quoteQty, fee, feeValue decimal.Decimal
	var feeAsset string
	if fill.Side == "buy" {
		feeInBase := fill.Fee.Div(fill.Price).Round(8)
		qty = fill.Volume.Sub(feeInBase)
		quoteQty = fill.Cost
		fee = feeInBase
		feeAsset = pairBase(fill.Pair)
		feeValue = fill.Fee
	} else {
		qty = fill.Volume
		quoteQty = fill.Cost.Sub(fill.Fee)
		fee = fill.Fee
		feeAsset = pairQuote(fill.Pair)
		feeValue = fill.Fee
	}

	var cashDelta decimal.Decimal
	if fill.Side == "buy" {
		cashDelta = quoteQty.Neg()
	} else {
		cashDelta = quoteQty
	}

	trade := &db.TradeRecord{
		AgentID:   agentID,
		Timestamp: fill.Time,
		Pair:      fill.Pair,
		Side:      fill.Side,
		Qty:       qty,
		Price:     fill.Price,
		Value:     quoteQty,
		Status:    StatusFill,
		FillID:    fill.TradeID,
		Fee:       fee,
		FeeAsset:  feeAsset,
		FeeValue:  feeValue,
		Reason:    "reconciliation: orphan exchange fill",
		Evidence: map[string]any{
			"data": map[string]any{
				"intent": map[string]any{
					"pair":  fill.Pair,
					"side":  fill.Side,
					"value": quoteQty.String(),
				},
				"fill": map[string]any{
					"id":        fill.TradeID,
					"price":     fill.Price.String(),
					"qty":       qty.String(),
					"fee":       fee.String(),
					"fee_asset": feeAsset,
					"fee_value": feeValue.String(),
				},
			},
		},
	}
	stateUpdate := &db.StateUpdate{
		AgentID:       agentID,
		CashDelta:     cashDelta,
		FillCountIncr: 1,
		FeeDelta:      feeValue,
	}
	return s.repo.RecordTrade(ctx, stateUpdate, trade)
}

// runReconciliationPoller periodically reconciles DB with exchange.
func (s *Service) runReconciliationPoller() {
	cfg := s.svcCtx.Config().TradingMCP
	interval := 5 * time.Minute
	if cfg.ReconcileInterval != "" {
		if d, err := time.ParseDuration(cfg.ReconcileInterval); err == nil && d > 0 {
			interval = d
		} else if cfg.ReconcileInterval == "0" {
			s.reconcileLog.Info("Reconciliation poller disabled")
			return
		}
	}

	s.reconcileLog.Info("Reconciliation poller started", zap.Duration("interval", interval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	ctx := s.svcCtx.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// isStartup=false: skip attestation recovery. Periodic ticks only
			// run the DB-local reconciliation (fill history, hash chain backfill).
			s.reconcileAllAgents(ctx, false)
		}
	}
}
