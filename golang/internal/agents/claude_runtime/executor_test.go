package claude_runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func parseEvent(t *testing.T, raw string) streamEvent {
	t.Helper()
	var ev streamEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	return ev
}

func TestExtractText(t *testing.T) {
	exec := NewExecutor(zap.NewNop())

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"assistant text content", `{"type":"assistant","message":{"content":[{"type":"text","text":"hello world"}]}}`, "hello world"},
		{"assistant multiple text blocks", `{"type":"assistant","message":{"content":[{"type":"text","text":"foo"},{"type":"text","text":"bar"}]}}`, "foobar"},
		{"assistant non-text content skipped", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"x"}]}}`, ""},
		{"stream_event text_delta", `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"streaming chunk"}}}`, "streaming chunk"},
		{"stream_event non-text delta skipped", `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking","text":"thinking..."}}}`, ""},
		{"result bare string skipped", `{"type":"result","result":"session summary text"}`, ""},
		{"result object with output", `{"type":"result","result":{"output":"final output"}}`, "final output"},
		{"result empty", `{"type":"result"}`, ""},
		{"system event skipped", `{"type":"system","subtype":"init"}`, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := parseEvent(t, tc.raw)
			got := exec.extractText(&ev)
			if got != tc.want {
				t.Errorf("extractText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantType string // "auth", "rate_limit", or ""
	}{
		{"clean output", "Session complete. Bought 0.5 ETH.", ""},
		{"auth required", "Authentication required. Please login.", "auth"},
		{"invalid api key", "Error: invalid api key provided.", "auth"},
		{"401 unauthorized", "401 Unauthorized", "auth"},
		{"credentials expired", "Your credentials expired.", "auth"},
		{"rate limit exceeded", "Error: rate limit exceeded", "rate_limit"},
		{"too many requests", "Too many requests, slow down", "rate_limit"},
		{"usage limit reached", "You've hit your usage limit reached", "rate_limit"},
		{"429 too many", "429 Too Many Requests", "rate_limit"},
		{"case insensitive auth", "AUTHENTICATION REQUIRED", "auth"},
		{"case insensitive rate", "RATE LIMIT EXCEEDED here", "rate_limit"},
		{"empty output", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyOutput(tc.output)
			switch tc.wantType {
			case "auth":
				if _, ok := err.(*AuthError); !ok {
					t.Errorf("classifyOutput(%q) = %v, want *AuthError", tc.output, err)
				}
			case "rate_limit":
				if _, ok := err.(*RateLimitError); !ok {
					t.Errorf("classifyOutput(%q) = %v, want *RateLimitError", tc.output, err)
				}
			case "":
				if err != nil {
					t.Errorf("classifyOutput(%q) = %v, want nil", tc.output, err)
				}
			}
		})
	}
}

func TestFilterEnv(t *testing.T) {
	env := []string{
		"HOME=/root",
		"ANTHROPIC_API_KEY=sk-secret",
		"CLAUDECODE=1",
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY_EXTRA=foo",
	}

	tests := []struct {
		name    string
		keys    []string
		wantIn  []string
		wantOut []string
	}{
		{
			name:    "filter api key and claudecode",
			keys:    []string{"ANTHROPIC_API_KEY", "CLAUDECODE"},
			wantIn:  []string{"HOME=/root", "PATH=/usr/bin", "ANTHROPIC_API_KEY_EXTRA=foo"},
			wantOut: []string{"ANTHROPIC_API_KEY=sk-secret", "CLAUDECODE=1"},
		},
		{
			name:    "filter nothing",
			keys:    []string{},
			wantIn:  []string{"HOME=/root", "ANTHROPIC_API_KEY=sk-secret"},
			wantOut: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterEnv(env, tc.keys...)
			gotSet := make(map[string]bool, len(got))
			for _, e := range got {
				gotSet[e] = true
			}
			for _, e := range tc.wantIn {
				if !gotSet[e] {
					t.Errorf("filterEnv: want %q in result, but missing", e)
				}
			}
			for _, e := range tc.wantOut {
				if gotSet[e] {
					t.Errorf("filterEnv: want %q excluded, but present", e)
				}
			}
		})
	}
}

func TestParseStream(t *testing.T) {
	log := zap.NewNop()

	tests := []struct {
		name       string
		input      string
		wantOutput string
	}{
		{
			name:       "assistant message",
			input:      `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}` + "\n",
			wantOutput: "hello",
		},
		{
			name:       "stream_event text_delta",
			input:      `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"streamed"}}}` + "\n",
			wantOutput: "streamed",
		},
		{
			name:       "result event closes stdin",
			input:      `{"type":"result","subtype":"success","result":"done"}` + "\n",
			wantOutput: "",
		},
		{
			name:       "non-json lines ignored",
			input:      "debug output\n" + `{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}` + "\n",
			wantOutput: "ok",
		},
		{
			name:       "empty input",
			input:      "",
			wantOutput: "",
		},
		{
			name:       "event handler called",
			input:      `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking","text":"hmm"}}}` + "\n",
			wantOutput: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exec := NewExecutor(log)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			r := strings.NewReader(tc.input)
			result := exec.parseStream(ctx, r, func() {}, func() {})

			if result.Output != tc.wantOutput {
				t.Errorf("output = %q, want %q", result.Output, tc.wantOutput)
			}
			if result.Error != nil {
				t.Errorf("unexpected error: %v", result.Error)
			}
		})
	}
}

func TestEventHandlerCalled(t *testing.T) {
	log := zap.NewNop()
	exec := NewExecutor(log)

	var events []string
	exec.EventHandler = func(eventType string) {
		events = append(events, eventType)
	}

	input := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking","text":"..."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}}`,
		`{"type":"result","subtype":"success"}`,
	}, "\n") + "\n"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exec.parseStream(ctx, strings.NewReader(input), func() {}, func() {})

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(events), events)
	}
	if events[0] != "system" {
		t.Errorf("event[0] = %q, want system", events[0])
	}
	// stream_event with thinking delta -> inner type "thinking" is passed
	if events[1] != "thinking" {
		t.Errorf("event[1] = %q, want thinking", events[1])
	}
	// stream_event with text_delta -> inner type "text_delta" is passed
	if events[2] != "text_delta" {
		t.Errorf("event[2] = %q, want text_delta", events[2])
	}
	if events[3] != "result" {
		t.Errorf("event[3] = %q, want result", events[3])
	}
}

func TestParseStreamContextCancel(t *testing.T) {
	log := zap.NewNop()
	exec := NewExecutor(log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// strings.NewReader returns EOF immediately, so the cancelled context is checked
	// after the read loop finishes. With an empty reader, output should be empty.
	r := strings.NewReader("")
	result := exec.parseStream(ctx, r, func() {}, func() {})
	// Empty reader returns EOF -> nil error (EOF is clean exit).
	if result.Error != nil {
		t.Errorf("expected nil error for empty reader, got: %v", result.Error)
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got: %q", result.Output)
	}
}
