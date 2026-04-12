package simple_llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/platform"
)

// mockChatClient implements ChatClient for testing.
type mockChatClient struct {
	responses []openai.ChatCompletionResponse
	calls     []openai.ChatCompletionRequest
	callIndex int
	err       error
}

func (m *mockChatClient) CreateChatCompletion(_ context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	m.calls = append(m.calls, req)
	if m.err != nil {
		return openai.ChatCompletionResponse{}, m.err
	}
	if m.callIndex >= len(m.responses) {
		return openai.ChatCompletionResponse{}, fmt.Errorf("unexpected call %d (only %d responses configured)", m.callIndex+1, len(m.responses))
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}

// newTestService creates a Service with mocked dependencies.
func newTestService(cfg config.LLMAgentConfig, chat ChatClient) *Service {
	log := zap.NewNop()

	fullCfg := &config.Config{LLMAgent: cfg}
	svcCtx := platform.NewServiceContext(context.Background(), log, fullCfg, nil, []string{"llm_agent"})

	// Create a toolset with no real clients - tests that need tool calls
	// will use the mock chat client which returns predefined responses.
	toolset := NewMCPToolset(map[string]*mcp.Client{}, map[string]string{})
	// Pre-populate with fake tools so ToOpenAITools returns something.
	toolset.tools = []mcp.Tool{
		{Name: "trade/get_portfolio", Description: "Get portfolio", InputSchema: map[string]any{"type": "object"}},
		{Name: "files/read", Description: "Read file", InputSchema: map[string]any{"type": "object"}},
	}

	agentLog := log.Named("llm_agent")
	initLog := agentLog.Named("init")
	return &Service{
		svcCtx:    svcCtx,
		log:       agentLog,
		initLog:   initLog,
		memoryLog: initLog.Named("memory"),
		marketLog: initLog.Named("market"),
		cfg:     cfg,
		toolset: toolset,
		chat:    chat,
		promptLoader: func(path string) ([]byte, error) {
			if path == "missing.md" {
				return nil, fmt.Errorf("file not found")
			}
			if path == "empty.md" {
				return []byte(""), nil
			}
			return []byte("You are a trading agent. Analyze markets and trade."), nil
		},
		stdinReader: func() *bufio.Scanner {
			return bufio.NewScanner(strings.NewReader(""))
		},
		startupDelay: 0, // Skip startup wait in tests.
	}
}

func defaultCfg() config.LLMAgentConfig {
	return config.LLMAgentConfig{
		AgentID:       "test-agent",
		APIKey:        "test-key",
		TradingMCPURL: "http://localhost:8091/mcp/trading",
		FilesMCPURL:   "http://localhost:8091/mcp/files",
		OpenAIAPIKey:  "sk-test",
		Model:         "gpt-4o",
		MaxIterations: 10,
		PromptFile:    "prompt.md",
		Mode:          "once",
		Interval:      "5m",
	}
}

func TestRunSession(t *testing.T) {
	tests := []struct {
		name       string
		responses  []openai.ChatCompletionResponse
		chatErr    error
		wantResult string
		wantErr    bool
		wantCalls  int
	}{
		{
			name: "direct text response - no tool calls",
			responses: []openai.ChatCompletionResponse{
				{Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleAssistant,
						Content: "Market is flat, no trade today.",
					},
					FinishReason: openai.FinishReasonStop,
				}}},
			},
			wantResult: "Market is flat, no trade today.",
			wantCalls:  1,
		},
		{
			name: "tool call then text response",
			responses: []openai.ChatCompletionResponse{
				// First call: LLM wants to call a tool.
				{Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{
						Role: openai.ChatMessageRoleAssistant,
						ToolCalls: []openai.ToolCall{{
							ID:   "call_1",
							Type: openai.ToolTypeFunction,
							Function: openai.FunctionCall{
								Name:      "trade__get_portfolio",
								Arguments: "{}",
							},
						}},
					},
					FinishReason: openai.FinishReasonToolCalls,
				}}},
				// Second call: LLM returns final text.
				{Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleAssistant,
						Content: "Portfolio looks good, holding.",
					},
					FinishReason: openai.FinishReasonStop,
				}}},
			},
			wantResult: "Portfolio looks good, holding.",
			wantCalls:  2,
		},
		{
			name: "multiple tool calls in one response",
			responses: []openai.ChatCompletionResponse{
				{Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleAssistant,
						Content: "Let me check portfolio and memory.",
						ToolCalls: []openai.ToolCall{
							{
								ID:   "call_1",
								Type: openai.ToolTypeFunction,
								Function: openai.FunctionCall{
									Name:      "trade__get_portfolio",
									Arguments: "{}",
								},
							},
							{
								ID:   "call_2",
								Type: openai.ToolTypeFunction,
								Function: openai.FunctionCall{
									Name:      "files__read",
									Arguments: `{"path":"memory/analysis.md"}`,
								},
							},
						},
					},
					FinishReason: openai.FinishReasonToolCalls,
				}}},
				{Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleAssistant,
						Content: "Done analyzing.",
					},
					FinishReason: openai.FinishReasonStop,
				}}},
			},
			wantResult: "Done analyzing.",
			wantCalls:  2,
		},
		{
			name:    "openai api error",
			chatErr: fmt.Errorf("rate limit exceeded"),
			wantErr: true,
		},
		{
			name: "empty choices",
			responses: []openai.ChatCompletionResponse{
				{Choices: []openai.ChatCompletionChoice{}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockChatClient{responses: tt.responses, err: tt.chatErr}
			svc := newTestService(defaultCfg(), mock)

			result, err := svc.runSession(context.Background(), "system prompt", "user message")
			if (err != nil) != tt.wantErr {
				t.Fatalf("runSession() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if result != tt.wantResult {
					t.Errorf("runSession() = %q, want %q", result, tt.wantResult)
				}
				if tt.wantCalls > 0 && len(mock.calls) != tt.wantCalls {
					t.Errorf("got %d API calls, want %d", len(mock.calls), tt.wantCalls)
				}
			}
		})
	}
}

func TestRunSessionMaxIterations(t *testing.T) {
	// Create a mock that always returns tool calls - should hit max iterations.
	toolCallResp := openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleAssistant,
				ToolCalls: []openai.ToolCall{{
					ID:   "call_loop",
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      "trade__get_portfolio",
						Arguments: "{}",
					},
				}},
			},
			FinishReason: openai.FinishReasonToolCalls,
		}},
	}

	// Fill responses for max iterations.
	cfg := defaultCfg()
	cfg.MaxIterations = 3
	responses := make([]openai.ChatCompletionResponse, cfg.MaxIterations)
	for i := range responses {
		responses[i] = toolCallResp
	}

	mock := &mockChatClient{responses: responses}
	svc := newTestService(cfg, mock)

	result, err := svc.runSession(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return empty since no assistant text was ever produced.
	if result != "" {
		t.Errorf("expected empty result on max iterations, got %q", result)
	}
	if len(mock.calls) != cfg.MaxIterations {
		t.Errorf("expected %d calls, got %d", cfg.MaxIterations, len(mock.calls))
	}
}

func TestRunSessionToolCallResults(t *testing.T) {
	// Verify that tool results are fed back to the LLM in the correct message format.
	mock := &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{{
						ID:   "call_abc",
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      "trade__get_portfolio",
							Arguments: "{}",
						},
					}},
				},
				FinishReason: openai.FinishReasonToolCalls,
			}}},
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "Final answer.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	svc := newTestService(defaultCfg(), mock)
	_, err := svc.runSession(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The second call should have 4 messages: system, user, assistant (tool calls), tool result.
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(mock.calls))
	}
	secondCall := mock.calls[1]
	if len(secondCall.Messages) != 4 {
		t.Fatalf("expected 4 messages in second call, got %d", len(secondCall.Messages))
	}

	toolMsg := secondCall.Messages[3]
	if toolMsg.Role != openai.ChatMessageRoleTool {
		t.Errorf("expected tool role, got %q", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "call_abc" {
		t.Errorf("expected ToolCallID %q, got %q", "call_abc", toolMsg.ToolCallID)
	}
}

func TestPromptFileLoading(t *testing.T) {
	tests := []struct {
		name        string
		promptFile  string
		wantErr     bool
		wantDoneErr bool // expect ErrSessionDone (successful once-mode completion)
	}{
		{"valid prompt file", "prompt.md", true, true},
		{"missing file", "missing.md", true, false},
		{"empty file", "empty.md", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultCfg()
			cfg.PromptFile = tt.promptFile
			cfg.Mode = "once"

			// For valid file, mock a quick text response.
			mock := &mockChatClient{
				responses: []openai.ChatCompletionResponse{
					{Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{
							Role:    openai.ChatMessageRoleAssistant,
							Content: "Done.",
						},
						FinishReason: openai.FinishReasonStop,
					}}},
				},
			}

			svc := newTestService(cfg, mock)
			err := svc.Start()
			if (err != nil) != tt.wantErr {
				t.Errorf("Start() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantDoneErr && !errors.Is(err, ErrSessionDone) {
				t.Errorf("Start() expected ErrSessionDone, got: %v", err)
			}
		})
	}
}

func TestOnceMode(t *testing.T) {
	mock := &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "Executed trade.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	cfg := defaultCfg()
	cfg.Mode = "once"
	svc := newTestService(cfg, mock)

	err := svc.Start()
	if !errors.Is(err, ErrSessionDone) {
		t.Fatalf("once mode Start() expected ErrSessionDone, got: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Errorf("once mode: expected 1 API call, got %d", len(mock.calls))
	}
}

func TestCLIMode(t *testing.T) {
	// Mock multiple responses for multiple inputs.
	mock := &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "Response to first input.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "Response to second input.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	cfg := defaultCfg()
	cfg.Mode = "cli"
	svc := newTestService(cfg, mock)

	// Override stdinReader to provide two lines.
	svc.stdinReader = func() *bufio.Scanner {
		return bufio.NewScanner(strings.NewReader("check ETH price\nbuy some BTC\n"))
	}

	err := svc.Start()
	if err != nil {
		t.Fatalf("cli mode Start() error: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Errorf("cli mode: expected 2 API calls, got %d", len(mock.calls))
	}

	// Verify each call is independent (both start with system + user, no history from previous round).
	for i, call := range mock.calls {
		if len(call.Messages) != 2 {
			t.Errorf("call %d: expected 2 messages (system + user), got %d", i, len(call.Messages))
		}
	}
}

func TestServerMode(t *testing.T) {
	mock := &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "Tick 1 done.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "Tick 2 done.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	cfg := defaultCfg()
	cfg.Mode = "server"
	cfg.Interval = "50ms"
	svc := newTestService(cfg, mock)

	// Run in a goroutine, cancel after enough time for the 2s startup wait + at least 1 tick.
	ctx, cancel := context.WithCancel(context.Background())
	log := zap.NewNop()
	fullCfg := &config.Config{LLMAgent: cfg}
	svcCtx := platform.NewServiceContext(ctx, log, fullCfg, nil, []string{"llm_agent"})
	svc.svcCtx = svcCtx
	svc.startupDelay = 0 // Skip startup wait in tests.

	done := make(chan error, 1)
	go func() {
		done <- svc.Start()
	}()

	// Wait for at least the first tick (immediate) + one ticker tick.
	time.Sleep(200 * time.Millisecond)
	cancel()

	err := <-done
	if err != nil {
		t.Fatalf("server mode Start() error: %v", err)
	}
	// Should have at least 1 call (the immediate first session).
	if len(mock.calls) < 1 {
		t.Errorf("server mode: expected at least 1 API call, got %d", len(mock.calls))
	}
}

func TestRunSessionContextCancelled(t *testing.T) {
	mock := &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "should not reach",
				},
			}}},
		},
	}

	svc := newTestService(defaultCfg(), mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := svc.runSession(ctx, "system", "user")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestInitialize(t *testing.T) {
	mock := &mockChatClient{}
	svc := newTestService(defaultCfg(), mock)

	err := svc.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}
}

func TestStop(t *testing.T) {
	mock := &mockChatClient{}
	svc := newTestService(defaultCfg(), mock)

	// Stop without Start (cancel is nil) - should not panic.
	err := svc.Stop()
	if err != nil {
		t.Fatalf("Stop() without Start() error: %v", err)
	}

	// Start a once-mode session to set cancel, then Stop.
	svc.cfg.Mode = "once"
	svc.chat = &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "Done.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}
	_ = svc.Start()

	err = svc.Stop()
	if err != nil {
		t.Fatalf("Stop() after Start() error: %v", err)
	}
}

func TestUnknownMode(t *testing.T) {
	mock := &mockChatClient{}
	cfg := defaultCfg()
	cfg.Mode = "invalid_mode"
	svc := newTestService(cfg, mock)

	err := svc.Start()
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("expected 'unknown mode' error, got: %v", err)
	}
}

func TestNewServiceCreation(t *testing.T) {
	log := zap.NewNop()
	cfg := &config.Config{
		LLMAgent: config.LLMAgentConfig{
			AgentID:       "test-agent",
			APIKey:        "test-key",
			TradingMCPURL: "http://localhost:8091/mcp/trading",
			FilesMCPURL:   "http://localhost:8091/mcp/files",
			OpenAIAPIKey:  "sk-test",
			Model:         "gpt-4o",
			MaxIterations: 50,
			PromptFile:    "prompt.md",
			Mode:          "once",
			Interval:      "5m",
		},
	}
	svcCtx := platform.NewServiceContext(context.Background(), log, cfg, nil, []string{"llm_agent"})

	svc := NewService(svcCtx)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.cfg.AgentID != "test-agent" {
		t.Errorf("expected agent ID 'test-agent', got %q", svc.cfg.AgentID)
	}
	if svc.toolset == nil {
		t.Fatal("toolset is nil")
	}
	if svc.chat == nil {
		t.Fatal("chat client is nil")
	}
	if svc.startupDelay != 2*time.Second {
		t.Errorf("expected 2s startup delay, got %v", svc.startupDelay)
	}
}

func TestCLIModeWithError(t *testing.T) {
	// Test CLI mode when a session returns an error - should log and continue.
	mock := &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			// First input returns empty choices (error).
			{Choices: []openai.ChatCompletionChoice{}},
			// Second input succeeds.
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "OK got it.",
				},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	cfg := defaultCfg()
	cfg.Mode = "cli"
	svc := newTestService(cfg, mock)
	svc.stdinReader = func() *bufio.Scanner {
		return bufio.NewScanner(strings.NewReader("first\nsecond\n"))
	}

	err := svc.Start()
	if err != nil {
		t.Fatalf("cli mode with error Start() error: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 API calls, got %d", len(mock.calls))
	}
}

func TestOnceModeWithError(t *testing.T) {
	// Test that once mode returns the error from runSession.
	mock := &mockChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{}},
		},
	}

	cfg := defaultCfg()
	cfg.Mode = "once"
	svc := newTestService(cfg, mock)

	err := svc.Start()
	if err == nil {
		t.Fatal("expected error from once mode with empty choices")
	}
}

// mockFilesMCP creates a test HTTP server that responds to files/read calls.
// fileContents maps paths to their content. Missing paths return an RPC error.
func mockFilesMCP(fileContents map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			ID     int64  `json:"id"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Method != "tools/call" {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "method not found: " + req.Method},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		path, _ := req.Params.Arguments["path"].(string)
		content, ok := fileContents[path]

		w.Header().Set("Content-Type", "application/json")
		if !ok {
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32000, "message": "file not found: " + path},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		resultJSON, _ := json.Marshal(map[string]any{
			"path":          path,
			"content":       content,
			"size_bytes":    len(content),
			"total_lines":   1,
			"truncated":     false,
		})
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": string(resultJSON)},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestLoadMemoryContext(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string // nil = use nil client
		wantEmpty    bool
		wantContains []string
		wantMissing  []string
	}{
		{
			name:      "nil client returns empty",
			files:     nil,
			wantEmpty: true,
		},
		{
			name: "MEMORY.md exists",
			files: map[string]string{
				"memory/MEMORY.md": "# My Memory\n## Active Market Regime\nBullish",
			},
			wantContains: []string{
				"## Pre-loaded Memory",
				"### memory/MEMORY.md",
				"# My Memory",
				"Bullish",
			},
			wantMissing: []string{
				"No core memory yet",
			},
		},
		{
			name:  "no files exist (new agent)",
			files: map[string]string{},
			wantContains: []string{
				"## Pre-loaded Memory",
				"No core memory yet",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(defaultCfg(), &mockChatClient{})

			var client *mcp.Client
			if tt.files != nil {
				srv := mockFilesMCP(tt.files)
				defer srv.Close()
				client = mcp.NewClient(srv.URL, "", 5*time.Second)
			}

			result := svc.loadMemoryContext(client)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected empty result, got %q", result)
				}
				return
			}

			for _, substr := range tt.wantContains {
				if !strings.Contains(result, substr) {
					t.Errorf("result missing %q\ngot: %s", substr, result)
				}
			}
			for _, substr := range tt.wantMissing {
				if strings.Contains(result, substr) {
					t.Errorf("result should NOT contain %q\ngot: %s", substr, result)
				}
			}
		})
	}
}

func TestClientAccessor(t *testing.T) {
	client := mcp.NewClient("http://localhost:8091/mcp/files", "", 5*time.Second)
	toolset := NewMCPToolset(
		map[string]*mcp.Client{"files": client},
		map[string]string{"files/": "files"},
	)

	if got := toolset.Client("files"); got != client {
		t.Error("Client('files') did not return the expected client")
	}
	if got := toolset.Client("nonexistent"); got != nil {
		t.Error("Client('nonexistent') should return nil")
	}
}

