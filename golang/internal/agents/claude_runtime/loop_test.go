package claude_runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRemoveBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		open  string
		close string
		want  string
	}{
		{
			name:  "removes block",
			input: "before {{#if FOO}}\nsome content\n{{/if}} after",
			open:  "{{#if FOO}}",
			close: "{{/if}}",
			want:  "before  after",
		},
		{
			name:  "removes multiple blocks",
			input: "a {{#if FOO}}x{{/if}} b {{#if FOO}}y{{/if}} c",
			open:  "{{#if FOO}}",
			close: "{{/if}}",
			want:  "a  b  c",
		},
		{
			name:  "no block present",
			input: "no block here",
			open:  "{{#if FOO}}",
			close: "{{/if}}",
			want:  "no block here",
		},
		{
			name:  "open without close — leaves as-is",
			input: "before {{#if FOO}} after",
			open:  "{{#if FOO}}",
			close: "{{/if}}",
			want:  "before {{#if FOO}} after",
		},
		{
			name:  "empty input",
			input: "",
			open:  "{{#if FOO}}",
			close: "{{/if}}",
			want:  "",
		},
		{
			name:  "nested same marker — removes first open to first close, then second pass removes remainder",
			input: "{{#if FOO}}a{{#if FOO}}b{{/if}}c{{/if}}",
			open:  "{{#if FOO}}",
			close: "{{/if}}",
			// First pass removes "{{#if FOO}}a{{#if FOO}}b{{/if}}" → "c{{/if}}"
			// No open left → stops. Remaining "c{{/if}}" is left as-is.
			want: "c{{/if}}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := removeBlock(tc.input, tc.open, tc.close)
			if got != tc.want {
				t.Errorf("removeBlock() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResetDailyCounterIfNeeded(t *testing.T) {
	l := &Loop{
		dailySessions: 5,
	}

	// Same day — should NOT reset.
	l.lastReset = time.Now().UTC()
	l.resetDailyCounterIfNeeded()
	if l.dailySessions != 5 {
		t.Errorf("expected dailySessions=5 (no reset), got %d", l.dailySessions)
	}

	// Different day — should reset.
	l.lastReset = time.Now().UTC().AddDate(0, 0, -1)
	l.resetDailyCounterIfNeeded()
	if l.dailySessions != 0 {
		t.Errorf("expected dailySessions=0 after reset, got %d", l.dailySessions)
	}
	if l.lastReset.Day() != time.Now().UTC().Day() {
		t.Error("lastReset should be updated to today")
	}
}

func TestIsClearCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/clear", true},
		{"/reset", true},
		{"/clear@mybotname", true},
		{"/reset@mybotname", true},
		{" /clear ", true},
		{"/clear some args", true},
		{"/help", false},
		{"clear", false},
		{"", false},
		{"hello /clear", false},
		{"/clearall", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := isClearCommand(tc.input); got != tc.want {
				t.Errorf("isClearCommand(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestHasTriage(t *testing.T) {
	tests := []struct {
		name   string
		alerts []map[string]any
		want   bool
	}{
		{"nil alerts", nil, false},
		{"empty alerts", []map[string]any{}, false},
		{"no triage", []map[string]any{{"on_trigger": "wake"}}, false},
		{"has triage", []map[string]any{{"on_trigger": "wake_triage"}}, true},
		{"mixed", []map[string]any{{"on_trigger": "wake"}, {"on_trigger": "wake_triage"}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasTriage(tc.alerts); got != tc.want {
				t.Errorf("hasTriage() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFormatAlertInjection(t *testing.T) {
	alerts := []map[string]any{
		{"service": "trading", "type": "price_cross", "pair": "ETH-USDC", "triggered_at": "14:30", "note": "crossed above 2100"},
		{"service": "market", "market": "BTC-USDC", "condition": "above 70000", "triggered_at": "14:31", "note": "breakout"},
		{"service": "time", "fire_at": "2026-04-09T15:00:00Z", "triggered_at": "15:00", "note": "check ETH position"},
		{"service": "news", "markets": []any{"BTC", "ETH"}, "categories": []any{"regulation"}, "triggered_at": "15:05", "note": "SEC filing"},
	}
	got := formatAlertInjection(alerts)
	if !strings.Contains(got, "[SYSTEM ALERT]") {
		t.Error("expected [SYSTEM ALERT] prefix")
	}
	if !strings.Contains(got, "ETH-USDC") {
		t.Error("expected ETH-USDC in output")
	}
	if !strings.Contains(got, "BTC-USDC") {
		t.Error("expected BTC-USDC in output")
	}
	if !strings.Contains(got, "[reminder]") {
		t.Error("expected [reminder] for time alert")
	}
	if !strings.Contains(got, "check ETH position") {
		t.Error("expected time alert note in output")
	}
	if !strings.Contains(got, "[news]") {
		t.Error("expected [news] for news alert")
	}
	if !strings.Contains(got, "regulation") {
		t.Error("expected news categories in output")
	}
}

func TestFormatSingleAlert(t *testing.T) {
	tests := []struct {
		name     string
		alert    map[string]any
		contains []string
	}{
		{
			name:     "trading with triggered_price",
			alert:    map[string]any{"service": "trading", "type": "stop_loss", "pair": "BTC-USDC", "triggered_at": "14:30", "triggered_price": "42000.50", "note": "stop hit"},
			contains: []string{"[trading]", "stop_loss", "BTC-USDC", "(price: 42000.50)", "stop hit"},
		},
		{
			name:     "trading without triggered_price",
			alert:    map[string]any{"service": "trading", "type": "take_profit", "pair": "ETH-USDC", "triggered_at": "14:30", "note": ""},
			contains: []string{"[trading]", "take_profit", "ETH-USDC"},
		},
		{
			name:     "market alert",
			alert:    map[string]any{"service": "market", "market": "BTC-USDC", "condition": "above", "triggered_at": "14:31", "triggered_price": "70000.00", "note": "breakout"},
			contains: []string{"[market]", "above", "BTC-USDC", "(price: 70000.00)"},
		},
		{
			name:     "time reminder with fire_at",
			alert:    map[string]any{"service": "time", "fire_at": "2026-04-09T15:00:00Z", "triggered_at": "15:00", "note": "rebalance check"},
			contains: []string{"[reminder]", "2026-04-09T15:00:00Z", "rebalance check"},
		},
		{
			name:     "time reminder without fire_at",
			alert:    map[string]any{"service": "time", "triggered_at": "15:00", "note": "just a note"},
			contains: []string{"[reminder]", "Triggered at 15:00", "just a note"},
		},
		{
			name:     "news alert with markets and categories",
			alert:    map[string]any{"service": "news", "markets": []any{"BTC", "ETH"}, "categories": []any{"regulation", "macro"}, "triggered_at": "15:05", "note": "SEC news"},
			contains: []string{"[news]", "BTC, ETH", "regulation, macro", "SEC news"},
		},
		{
			name:     "news alert with no filters",
			alert:    map[string]any{"service": "news", "triggered_at": "15:05", "note": "breaking"},
			contains: []string{"[news]", "general", "breaking"},
		},
		{
			name:     "unknown service fallback",
			alert:    map[string]any{"service": "custom", "foo": "bar", "triggered_at": "15:10"},
			contains: []string{"[custom]", "foo=bar"},
		},
		{
			name:     "missing triggered_at defaults to unknown time",
			alert:    map[string]any{"service": "time", "fire_at": "2026-04-09T15:00:00Z", "note": "test"},
			contains: []string{"unknown time"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatSingleAlert(tc.alert)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in output %q", want, got)
				}
			}
		})
	}
}

func TestAnyToString(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"slice", []any{"a", "b", "c"}, "a, b, c"},
		{"int", 42, "42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := anyToString(tc.val)
			if got != tc.want {
				t.Errorf("anyToString(%v) = %q, want %q", tc.val, got, tc.want)
			}
		})
	}
}

func TestTimerHelpers(t *testing.T) {
	l := &Loop{}

	// No timer - timerCh returns nil (blocks forever in select).
	if ch := l.timerCh(); ch != nil {
		t.Error("expected nil channel when no timer")
	}

	// Set timer.
	l.intervalTimer = time.NewTimer(time.Hour)
	defer l.intervalTimer.Stop()
	if ch := l.timerCh(); ch == nil {
		t.Error("expected non-nil channel when timer active")
	}

	// Stop timer.
	l.stopTimer()
	if l.intervalTimer != nil {
		t.Error("expected nil timer after stopTimer")
	}
	if ch := l.timerCh(); ch != nil {
		t.Error("expected nil channel after stopTimer")
	}
}

func TestIsError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		checkFn func(error) bool
		want    bool
	}{
		{
			name:    "nil error returns false",
			err:     nil,
			checkFn: func(err error) bool { var t *AuthError; return isError(err, &t) },
			want:    false,
		},
		{
			name:    "matching auth error",
			err:     &AuthError{Message: "expired"},
			checkFn: func(err error) bool { var t *AuthError; return isError(err, &t) },
			want:    true,
		},
		{
			name:    "matching rate limit error",
			err:     &RateLimitError{Message: "too fast"},
			checkFn: func(err error) bool { var t *RateLimitError; return isError(err, &t) },
			want:    true,
		},
		{
			name:    "auth error does not match rate limit",
			err:     &AuthError{Message: "expired"},
			checkFn: func(err error) bool { var t *RateLimitError; return isError(err, &t) },
			want:    false,
		},
		{
			name:    "wrapped auth error is detected",
			err:     fmt.Errorf("outer: %w", &AuthError{Message: "inner"}),
			checkFn: func(err error) bool { var t *AuthError; return isError(err, &t) },
			want:    true,
		},
		{
			name:    "unrelated error",
			err:     fmt.Errorf("some other error"),
			checkFn: func(err error) bool { var t *AuthError; return isError(err, &t) },
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.checkFn(tc.err)
			if got != tc.want {
				t.Errorf("isError() = %v, want %v", got, tc.want)
			}
		})
	}
}
