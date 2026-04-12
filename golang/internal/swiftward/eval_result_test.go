package swiftward

import (
	"testing"

	"google.golang.org/grpc/codes"
)

func TestShouldBlock(t *testing.T) {
	tests := []struct {
		name       string
		result     EvalResult
		wantBlock  bool
		wantReason string
	}{
		{
			name:       "rejected_default_reason",
			result:     EvalResult{Verdict: VerdictRejected},
			wantBlock:  true,
			wantReason: "Policy violation",
		},
		{
			name:       "rejected_custom_reason",
			result:     EvalResult{Verdict: VerdictRejected, Response: map[string]any{"reason": "exceeds position limit"}},
			wantBlock:  true,
			wantReason: "exceeds position limit",
		},
		{
			name:       "flagged_default_reason",
			result:     EvalResult{Verdict: VerdictFlagged},
			wantBlock:  true,
			wantReason: "Policy violation",
		},
		{
			name:       "flagged_custom_reason",
			result:     EvalResult{Verdict: VerdictFlagged, Response: map[string]any{"reason": "unusual pattern"}},
			wantBlock:  true,
			wantReason: "unusual pattern",
		},
		{
			name:       "rejected_empty_reason_falls_back_to_default",
			result:     EvalResult{Verdict: VerdictRejected, Response: map[string]any{"reason": ""}},
			wantBlock:  true,
			wantReason: "Policy violation",
		},
		{
			name:      "approved_not_blocked",
			result:    EvalResult{Verdict: VerdictApproved},
			wantBlock: false,
		},
		{
			name:       "behavior_block_with_default_reason",
			result:     EvalResult{Verdict: VerdictApproved, Response: map[string]any{"behavior": "block"}},
			wantBlock:  true,
			wantReason: "Blocked by policy",
		},
		{
			name:       "behavior_block_with_custom_reason",
			result:     EvalResult{Verdict: VerdictApproved, Response: map[string]any{"behavior": "block", "reason": "rate limit"}},
			wantBlock:  true,
			wantReason: "rate limit",
		},
		{
			name:      "behavior_other_not_blocked",
			result:    EvalResult{Verdict: VerdictApproved, Response: map[string]any{"behavior": "allow"}},
			wantBlock: false,
		},
		{
			name:      "nil_response_approved",
			result:    EvalResult{Verdict: VerdictApproved, Response: nil},
			wantBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, reason := tt.result.ShouldBlock()
			if blocked != tt.wantBlock {
				t.Errorf("blocked = %v, want %v", blocked, tt.wantBlock)
			}
			if tt.wantBlock && reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestEvalErrorIsTransient(t *testing.T) {
	tests := []struct {
		code      codes.Code
		transient bool
	}{
		{codes.Unavailable, true},
		{codes.DeadlineExceeded, true},
		{codes.Aborted, true},
		{codes.ResourceExhausted, true},
		{codes.NotFound, false},
		{codes.Internal, false},
		{codes.InvalidArgument, false},
		{codes.PermissionDenied, false},
		{codes.Unknown, false},
		{codes.OK, false},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			e := &EvalError{Code: tt.code}
			if got := e.IsTransient(); got != tt.transient {
				t.Errorf("IsTransient() = %v, want %v for code %s", got, tt.transient, tt.code)
			}
		})
	}
}

func TestEvalErrorMessage(t *testing.T) {
	e := &EvalError{
		Code:    codes.Unavailable,
		Message: "connection refused",
		Err:     nil,
	}
	msg := e.Error()
	if msg == "" {
		t.Error("Error() returned empty string")
	}
	// Must contain the code name
	if !containsStr(msg, "Unavailable") {
		t.Errorf("Error() %q does not mention code", msg)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
