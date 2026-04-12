package trading

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/db"
)

// flakeRepo wraps MemRepository and makes UpdateEvidence flaky for testing
// retry behaviour. The first `failN` UpdateEvidence calls return an error; the
// remaining calls delegate to MemRepository. A sleep can be injected before
// each failing call so the caller can race against the retry-loop's ctx timer.
type flakeRepo struct {
	*db.MemRepository
	failN       int32 // number of UpdateEvidence calls that should fail before success (atomic)
	calls       int32 // total UpdateEvidence calls observed (atomic)
	beforeSleep time.Duration
}

func newFlakeRepo(failN int32, beforeSleep time.Duration) *flakeRepo {
	return &flakeRepo{
		MemRepository: db.NewMemRepository(),
		failN:         failN,
		beforeSleep:   beforeSleep,
	}
}

func (r *flakeRepo) UpdateEvidence(ctx context.Context, tradeID int64, data map[string]any) error {
	atomic.AddInt32(&r.calls, 1)
	if r.beforeSleep > 0 {
		time.Sleep(r.beforeSleep)
	}
	if atomic.LoadInt32(&r.failN) > 0 {
		atomic.AddInt32(&r.failN, -1)
		return errors.New("simulated DB failure")
	}
	return r.MemRepository.UpdateEvidence(ctx, tradeID, data)
}

// newTestService builds a bare Service suitable for helper-method tests.
// Only the repo and log fields are populated - everything else stays zero.
func newTestService(repo db.Repository) *Service {
	return &Service{
		repo: repo,
		log:  zap.NewNop(),
	}
}

func TestClassifyAttestationFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want attestationFailureKind
	}{
		{"nil error falls through to terminal", nil, failureKindTerminal},
		{
			"insufficient funds -> waiting_for_gas",
			errors.New("send tx: insufficient funds for gas * price + value: balance 123, tx cost 456"),
			failureKindWaitingForGas,
		},
		{
			"insufficient funds case-insensitive",
			errors.New("wrap: Insufficient Funds for gas"),
			failureKindWaitingForGas,
		},
		{
			"INSUFFICIENT FUNDS uppercase",
			errors.New("RPC: INSUFFICIENT FUNDS"),
			failureKindWaitingForGas,
		},
		{
			"not an authorized validator is terminal",
			errors.New("not an authorized validator"),
			failureKindTerminal,
		},
		{
			"contract revert is terminal",
			errors.New("execution reverted: CheckpointAlreadyExists"),
			failureKindTerminal,
		},
		{
			"connection refused is technical (retryable)",
			errors.New("dial tcp: connection refused"),
			failureKindTechnical,
		},
		{
			"context deadline exceeded is technical",
			errors.New("Post https://rpc: context deadline exceeded"),
			failureKindTechnical,
		},
		{
			"i/o timeout is technical",
			errors.New("read: i/o timeout"),
			failureKindTechnical,
		},
		{
			"503 service unavailable is technical",
			errors.New("http 503 service unavailable"),
			failureKindTechnical,
		},
		{
			"429 rate limit is technical",
			errors.New("HTTP 429 too many requests"),
			failureKindTechnical,
		},
		{
			"EOF is technical",
			errors.New("read tcp: EOF"),
			failureKindTechnical,
		},
		{
			"unknown error is terminal (conservative - don't retry forever)",
			errors.New("bad request: malformed call data"),
			failureKindTerminal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyAttestationFailure(tt.err); got != tt.want {
				t.Errorf("classifyAttestationFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestReadAttestationState(t *testing.T) {
	tests := []struct {
		name        string
		evidence    map[string]any
		wantStatus  string
		wantAttempt int
	}{
		{"nil evidence", nil, "", 0},
		{"empty evidence", map[string]any{}, "", 0},
		{"no attestation key", map[string]any{"hash": "0xabc"}, "", 0},
		{
			"attestation not a map",
			map[string]any{"attestation": "corrupt"},
			"",
			0,
		},
		{
			"pending with no counter",
			map[string]any{"attestation": map[string]any{"status": "pending"}},
			"pending",
			0,
		},
		{
			"pending with float counter (post-JSON-unmarshal)",
			map[string]any{"attestation": map[string]any{"status": "pending", "attempt_count": float64(2)}},
			"pending",
			2,
		},
		{
			"pending with int counter (direct map set)",
			map[string]any{"attestation": map[string]any{"status": "pending", "attempt_count": 3}},
			"pending",
			3,
		},
		{
			"waiting_for_gas does NOT return attempt_count (separate track)",
			map[string]any{"attestation": map[string]any{"status": "waiting_for_gas", "attempt_count": 5}},
			"waiting_for_gas",
			0,
		},
		{
			"success returns status only",
			map[string]any{"attestation": map[string]any{"status": "success"}},
			"success",
			0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, gotAttempt := readAttestationState(tt.evidence)
			if gotStatus != tt.wantStatus || gotAttempt != tt.wantAttempt {
				t.Errorf("readAttestationState = (%q, %d), want (%q, %d)",
					gotStatus, gotAttempt, tt.wantStatus, tt.wantAttempt)
			}
		})
	}
}

func TestReadPendingConfirmData(t *testing.T) {
	tests := []struct {
		name       string
		evidence   map[string]any
		wantScore  int
		wantCpHash string
		wantTxHash string
		wantOk     bool
	}{
		{"nil evidence", nil, 0, "", "", false},
		{"no attestation key", map[string]any{"hash": "0xabc"}, 0, "", "", false},
		{
			"missing tx_hash",
			map[string]any{"attestation": map[string]any{
				"status": "pending_confirm", "checkpoint_hash": "0xcp",
			}},
			0, "0xcp", "", false,
		},
		{
			"missing checkpoint_hash",
			map[string]any{"attestation": map[string]any{
				"status": "pending_confirm", "tx_hash": "0xtx",
			}},
			0, "", "0xtx", false,
		},
		{
			"complete pending_confirm (float score from JSON)",
			map[string]any{"attestation": map[string]any{
				"status":          "pending_confirm",
				"tx_hash":         "0xtxabc",
				"checkpoint_hash": "0xcpdef",
				"score":           float64(85),
			}},
			85, "0xcpdef", "0xtxabc", true,
		},
		{
			"complete pending_confirm (int score from direct map)",
			map[string]any{"attestation": map[string]any{
				"status":          "pending_confirm",
				"tx_hash":         "0xtx",
				"checkpoint_hash": "0xcp",
				"score":           70,
			}},
			70, "0xcp", "0xtx", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, cp, tx, ok := readPendingConfirmData(tt.evidence)
			if score != tt.wantScore || cp != tt.wantCpHash || tx != tt.wantTxHash || ok != tt.wantOk {
				t.Errorf("readPendingConfirmData = (%d, %q, %q, %v), want (%d, %q, %q, %v)",
					score, cp, tx, ok, tt.wantScore, tt.wantCpHash, tt.wantTxHash, tt.wantOk)
			}
		})
	}
}

func TestUpdateEvidenceWithRetry(t *testing.T) {
	ctx := context.Background()

	// Seed a trade we can point UpdateEvidence at. Not strictly needed for
	// flakeRepo behaviour but keeps things realistic.
	payload := map[string]any{"attestation": map[string]any{"status": "pending"}}

	t.Run("succeeds on first attempt", func(t *testing.T) {
		// Insert the trade so the underlying MemRepository accepts the update,
		// then assert the retry loop calls UpdateEvidence exactly once.
		repo := newFlakeRepo(0, 0)
		if err := repo.InsertTrade(ctx, &db.TradeRecord{
			AgentID: "a", Pair: "ETH-USD", Side: "buy", Status: "fill",
		}); err != nil {
			t.Fatal(err)
		}
		s := newTestService(repo)
		if err := s.updateEvidenceWithRetry(ctx, 1, payload); err != nil {
			t.Fatalf("expected success on first attempt, got %v", err)
		}
		if got := atomic.LoadInt32(&repo.calls); got != 1 {
			t.Errorf("calls=%d, want 1 (no retries needed)", got)
		}
	})

	t.Run("succeeds after 2 failures when trade exists", func(t *testing.T) {
		repo := newFlakeRepo(2, 0)
		// Insert a trade so the underlying MemRepository accepts the update.
		if err := repo.InsertTrade(ctx, &db.TradeRecord{
			AgentID: "a", Pair: "ETH-USD", Side: "buy", Status: "fill",
		}); err != nil {
			t.Fatal(err)
		}
		// InsertTrade assigns ID=1 to the first trade.
		s := newTestService(repo)
		if err := s.updateEvidenceWithRetry(ctx, 1, payload); err != nil {
			t.Errorf("expected success after retries, got %v", err)
		}
		if got := atomic.LoadInt32(&repo.calls); got != 3 {
			t.Errorf("calls=%d, want 3 (2 failures + 1 success)", got)
		}
	})

	t.Run("all 3 attempts fail returns error", func(t *testing.T) {
		repo := newFlakeRepo(5, 0)
		s := newTestService(repo)
		err := s.updateEvidenceWithRetry(ctx, 1, payload)
		if err == nil {
			t.Fatal("expected error after 3 failed attempts")
		}
		if got := atomic.LoadInt32(&repo.calls); got != 3 {
			t.Errorf("calls=%d, want 3", got)
		}
	})

	t.Run("ctx cancelled during retry sleep exits early", func(t *testing.T) {
		// Fail every call; cancel ctx quickly so the retry loop should see it
		// during its backoff sleep. Without ctx-aware sleep, the loop blocks
		// for the full ~600ms before returning.
		repo := newFlakeRepo(10, 0)
		s := newTestService(repo)

		cancelCtx, cancel := context.WithCancel(ctx)
		go func() {
			// Cancel shortly after the first failure (before the 200ms sleep ends).
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		err := s.updateEvidenceWithRetry(cancelCtx, 1, payload)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected error from cancelled ctx")
		}
		// With ctx-aware sleep, we should return within a few ms of cancellation.
		// Test cancels at t=50ms; allow 50ms of scheduling jitter = 100ms budget,
		// with some extra slack for slow CI. A broken impl that sleeps through
		// cancellation takes the full 200ms first backoff.
		if elapsed >= 150*time.Millisecond {
			t.Errorf("retry loop slept through cancellation: elapsed=%v, expected <150ms", elapsed)
		}
	})
}

// fetchAttestation returns the attestation sub-object for the first trade
// of the given agent. Used in helper tests to inspect evidence after a write.
func fetchAttestation(t *testing.T, s *Service, agentID string) map[string]any {
	t.Helper()
	trades, err := s.repo.GetTradeHistory(context.Background(), agentID, 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) == 0 {
		t.Fatalf("no trades for agent %q", agentID)
	}
	att, _ := trades[0].Evidence["attestation"].(map[string]any)
	return att
}

// TestRecoverHashChainsLegacyStamp verifies that trades with no evidence.data
// get stamped with an explicit attestation.status=disabled, NOT with the
// legacy "attestation":"legacy" string marker. With the string marker, the
// attestation recovery query would incorrectly treat the trade as null-status
// and try to post a garbage on-chain attestation with "legacy" as the hash.
func TestRecoverHashChainsLegacyStamp(t *testing.T) {
	ctx := context.Background()
	repo := db.NewMemRepository()
	s := newTestService(repo)

	// Create a fill trade with no evidence.data (the legacy case)
	trade := &db.TradeRecord{
		AgentID: "legacy-agent",
		Pair:    "ETH-USD",
		Side:    "buy",
		Status:  "fill",
		// Evidence is nil - this is exactly the condition recoverHashChains
		// stamps as "unrecoverable" (no evidence.data to build a hash from).
	}
	if err := repo.InsertTrade(ctx, trade); err != nil {
		t.Fatal(err)
	}

	// Run hash chain recovery. It should find the trade and stamp it.
	s.recoverHashChains(ctx, "legacy-agent")

	// Verify the stamp wrote a proper disabled state, not a "legacy" string.
	trades, err := repo.GetTradeHistory(ctx, "legacy-agent", 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) == 0 {
		t.Fatal("expected 1 trade after recovery")
	}
	att, ok := trades[0].Evidence["attestation"].(map[string]any)
	if !ok {
		t.Fatalf("evidence.attestation should be a map (disabled state object), got %T: %v",
			trades[0].Evidence["attestation"], trades[0].Evidence["attestation"])
	}
	if status, _ := att["status"].(string); status != attestationStatusDisabled {
		t.Errorf("attestation.status = %q, want %q", status, attestationStatusDisabled)
	}

	// And critically: the attestation recovery query should NOT pick it up.
	// If the stamp wrote the bare string "legacy", the query would match
	// (since ->>'status' on a string returns null), and recovery would try
	// to post a bogus on-chain attestation for this corrupt trade.
	recoverable, err := repo.GetFilledTradesPendingAttestation(ctx, "legacy-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(recoverable) != 0 {
		t.Errorf("legacy-stamped trade should NOT be returned by GetFilledTradesPendingAttestation, got %d", len(recoverable))
	}
}

func TestWriteAttestationWaitingForGas(t *testing.T) {
	ctx := context.Background()

	setup := func(t *testing.T) (*Service, int64) {
		t.Helper()
		repo := db.NewMemRepository()
		s := newTestService(repo)
		trade := &db.TradeRecord{AgentID: "a", Pair: "ETH-USD", Side: "buy", Status: "fill"}
		if err := repo.InsertTrade(ctx, trade); err != nil {
			t.Fatal(err)
		}
		return s, trade.ID
	}

	t.Run("gasAttemptCount > 0 writes the counter under gas_attempt_count", func(t *testing.T) {
		s, id := setup(t)
		if err := s.writeAttestationWaitingForGas(ctx, id, 3, "insufficient funds"); err != nil {
			t.Fatal(err)
		}
		att := fetchAttestation(t, s, "a")
		if n, ok := att["gas_attempt_count"]; !ok {
			t.Error("expected gas_attempt_count to be present for gasAttemptCount=3")
		} else if n != 3 {
			t.Errorf("gas_attempt_count = %v, want 3", n)
		}
		// Must NOT write the technical attempt_count (separate track).
		if _, ok := att["attempt_count"]; ok {
			t.Error("WFG state must not carry attempt_count (technical retry counter)")
		}
	})

	t.Run("gasAttemptCount == 0 omits the counter", func(t *testing.T) {
		s, id := setup(t)
		if err := s.writeAttestationWaitingForGas(ctx, id, 0, "circuit-breaker blocked"); err != nil {
			t.Fatal(err)
		}
		att := fetchAttestation(t, s, "a")
		if v, ok := att["gas_attempt_count"]; ok {
			t.Errorf("expected gas_attempt_count to be omitted for gasAttemptCount=0, got %v", v)
		}
	})
}
