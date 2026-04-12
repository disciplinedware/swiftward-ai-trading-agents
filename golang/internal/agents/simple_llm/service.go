package simple_llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/observability"
	"ai-trading-agents/internal/platform"
)

const previewMaxLen = 120

// agentIDTransport injects X-Agent-ID header into every outgoing HTTP request.
// Used to identify the agent in LLM Gateway guardrails (entity_id for policy evaluation).
type agentIDTransport struct {
	agentID string
	base    http.RoundTripper
}

func (t *agentIDTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("X-Agent-ID", t.agentID)
	return t.base.RoundTrip(req)
}

func messagePreview(messages []openai.ChatCompletionMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Content != "" {
			return observability.LogPreview(messages[i].Content, previewMaxLen)
		}
	}
	return "(no text content)"
}

// stripThinking removes <think>...</think> blocks from model output.
// Some models (GLM, Qwen, DeepSeek) emit chain-of-thought in these tags.
// Returns the cleaned decision text and the thinking content (if any).
func stripThinking(content string) (decision, thinking string) {
	const openTag = "<think>"
	const closeTag = "</think>"

	for {
		start := strings.Index(content, openTag)
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], closeTag)
		if end == -1 {
			// Unclosed tag - strip from <think> to end.
			thinking += content[start+len(openTag):]
			content = content[:start]
			break
		}
		thinking += content[start+len(openTag) : start+end]
		content = content[:start] + content[start+end+len(closeTag):]
	}
	return strings.TrimSpace(content), strings.TrimSpace(thinking)
}

// responsePreview returns a short summary of an LLM response message:
// text content preview if present, otherwise a list of tool names being called.
func responsePreview(msg openai.ChatCompletionMessage) string {
	if msg.Content != "" {
		return observability.LogPreview(msg.Content, previewMaxLen)
	}
	if len(msg.ToolCalls) == 0 {
		return "(empty)"
	}
	names := make([]string, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		names[i] = tc.Function.Name
	}
	return "calls: " + observability.LogPreview(strings.Join(names, ", "), previewMaxLen)
}

// ErrSessionDone signals that the agent completed its session and the process should exit.
// Used by once mode to trigger graceful shutdown.
var ErrSessionDone = errors.New("session done")

// ChatClient abstracts the OpenAI chat completion API for testing.
type ChatClient interface {
	CreateChatCompletion(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

// Service implements the LLM trading agent.
// It uses OpenAI tool-calling to autonomously trade via MCP tools.
type Service struct {
	svcCtx  *platform.ServiceContext
	log       *zap.Logger // root: llm_agent
	initLog   *zap.Logger // llm_agent.init — placeholder prefetch orchestration
	memoryLog *zap.Logger // llm_agent.init.memory — memory file reads
	marketLog *zap.Logger // llm_agent.init.market — portfolio + price fetches
	cfg     config.LLMAgentConfig
	toolset *MCPToolset
	chat    ChatClient
	cancel  context.CancelFunc

	// promptLoader reads the prompt file. Defaults to os.ReadFile.
	// Overridable for testing.
	promptLoader func(string) ([]byte, error)

	// stdinReader provides lines from stdin for CLI mode.
	// Defaults to a bufio.Scanner over os.Stdin.
	// Overridable for testing.
	stdinReader func() *bufio.Scanner

	// startupDelay is the time to wait for MCP servers before discovering tools.
	// Defaults to 2s. Set to 0 in tests.
	startupDelay time.Duration
}

// NewService creates a new LLM trading agent.
func NewService(svcCtx *platform.ServiceContext) *Service {
	cfg := svcCtx.Config().LLMAgent
	log := svcCtx.Logger().Named("llm_agent")

	tradingClient := mcp.NewClient(cfg.TradingMCPURL, cfg.APIKey, 5*time.Minute)
	tradingClient.SetHeader("X-Agent-ID", cfg.AgentID)
	filesClient := mcp.NewClient(cfg.FilesMCPURL, cfg.APIKey, 5*time.Minute)
	filesClient.SetHeader("X-Agent-ID", cfg.AgentID)
	marketClient := mcp.NewClient(cfg.MarketDataMCPURL, cfg.APIKey, 5*time.Minute)
	marketClient.SetHeader("X-Agent-ID", cfg.AgentID)
	// Code MCP needs a long timeout: first call starts a sandbox container (docker pull + start + repl ready)
	// which can take 30-60s, plus Python execution itself can run up to 120s.
	// Configurable via TRADING__LLM_AGENT__CODE_MCP_TIMEOUT (default 5m).
	codeMCPTimeout := 5 * time.Minute
	if d, err := time.ParseDuration(cfg.CodeMCPTimeout); err == nil && d > 0 {
		codeMCPTimeout = d
	}
	codeClient := mcp.NewClient(cfg.CodeMCPURL, cfg.APIKey, codeMCPTimeout)
	codeClient.SetHeader("X-Agent-ID", cfg.AgentID)
	newsClient := mcp.NewClient(cfg.NewsMCPURL, cfg.APIKey, 5*time.Minute)
	newsClient.SetHeader("X-Agent-ID", cfg.AgentID)
	polymarketClient := mcp.NewClient(cfg.PolymarketMCPURL, cfg.APIKey, 5*time.Minute)
	polymarketClient.SetHeader("X-Agent-ID", cfg.AgentID)

	toolset := NewMCPToolset(
		map[string]*mcp.Client{
			"trading":    tradingClient,
			"files":      filesClient,
			"market":     marketClient,
			"code":       codeClient,
			"news":       newsClient,
			"polymarket": polymarketClient,
		},
		map[string]string{
			"trade/":      "trading",
			"alert/":      "trading",
			"market/":     "market",
			"files/":      "files",
			"code/":       "code",
			"news/":       "news",
			"polymarket/": "polymarket",
		},
	)

	apiKey := cfg.OpenAIAPIKey
	if apiKey == "" {
		apiKey = "local" // LM Studio / Ollama ignore the key but the client requires non-empty
	}
	openaiCfg := openai.DefaultConfig(apiKey)
	if cfg.LLMURL != "" {
		openaiCfg.BaseURL = cfg.LLMURL
	}
	llmTimeout := 5 * time.Minute
	if d, err := time.ParseDuration(cfg.LLMTimeout); err == nil && d > 0 {
		llmTimeout = d
	}
	openaiCfg.HTTPClient = &http.Client{
		Timeout:   llmTimeout,
		Transport: &agentIDTransport{agentID: cfg.AgentID, base: http.DefaultTransport},
	}
	openaiClient := openai.NewClientWithConfig(openaiCfg)

	initLog := log.Named("init")
	return &Service{
		svcCtx:       svcCtx,
		log:          log,
		initLog:      initLog,
		memoryLog:    initLog.Named("memory"),
		marketLog:    initLog.Named("market"),
		cfg:          cfg,
		toolset:      toolset,
		chat:         openaiClient,
		promptLoader: os.ReadFile,
		stdinReader: func() *bufio.Scanner {
			return bufio.NewScanner(os.Stdin)
		},
		startupDelay: 2 * time.Second,
	}
}

func (s *Service) Initialize() error {
	if s.cfg.OpenAIAPIKey == "" && s.cfg.LLMURL == "" {
		return fmt.Errorf("set TRADING__LLM_AGENT__OPENAI_API_KEY (OpenAI) or TRADING__LLM_AGENT__LLM_URL (local model)")
	}
	llmURL := s.cfg.LLMURL
	if llmURL == "" {
		llmURL = "https://api.openai.com/v1 (direct)"
	}
	s.log.Info("LLM agent initialized",
		zap.String("agent_id", s.cfg.AgentID),
		zap.String("model", s.cfg.Model),
		zap.String("mode", s.cfg.Mode),
		zap.Int("max_iterations", s.cfg.MaxIterations),
		zap.String("prompt_file", s.cfg.PromptFile),
		zap.String("llm_url", llmURL),
	)
	return nil
}

func (s *Service) Start() error {
	ctx, cancel := context.WithCancel(s.svcCtx.Context())
	s.cancel = cancel

	// Load prompt file
	promptBytes, err := s.promptLoader(s.cfg.PromptFile)
	if err != nil {
		return fmt.Errorf("load prompt file %s: %w", s.cfg.PromptFile, err)
	}
	systemPrompt := strings.TrimSpace(string(promptBytes))
	if systemPrompt == "" {
		return fmt.Errorf("prompt file %s is empty", s.cfg.PromptFile)
	}
	systemPrompt += fmt.Sprintf("\n\nCurrent date and time: %s", time.Now().UTC().Format("2006-01-02 15:04 UTC"))

	// Wait for MCP servers to be ready.
	if s.startupDelay > 0 {
		select {
		case <-time.After(s.startupDelay):
		case <-ctx.Done():
			return nil
		}
	}

	// Discover available tools
	if err := s.toolset.DiscoverTools(); err != nil {
		return fmt.Errorf("tool discovery failed: %w", err)
	}
	tools := s.toolset.Tools()

	// Group tools by MCP prefix for a readable summary.
	mcpGroups := make(map[string][]string)
	for _, t := range tools {
		prefix := t.Name
		if idx := strings.Index(prefix, "/"); idx != -1 {
			prefix = prefix[:idx]
		}
		mcpGroups[prefix] = append(mcpGroups[prefix], t.Name)
	}
	var summary []string
	for prefix, toolNames := range mcpGroups {
		summary = append(summary, fmt.Sprintf("%s (%d): %s", prefix, len(toolNames), strings.Join(toolNames, ", ")))
	}
	slices.Sort(summary)
	toolsJSON, _ := json.Marshal(tools)
	s.log.Info(fmt.Sprintf("Tools discovered: %d across %d MCPs - %s", len(tools), len(mcpGroups), strings.Join(summary, "; ")),
		zap.ByteString("tools", toolsJSON))

	// Mode dispatch. Each mode calls buildSessionPrompt() to inject fresh memory context.
	switch s.cfg.Mode {
	case "once":
		return s.runOnce(ctx, systemPrompt)
	case "cli":
		return s.runCLI(ctx, systemPrompt)
	case "server":
		return s.runServer(ctx, systemPrompt)
	default:
		return fmt.Errorf("unknown mode %q (valid: once, cli, server)", s.cfg.Mode)
	}
}

// buildSessionPrompt expands placeholders in the base prompt with live data.
// Only fetches data for placeholders that appear in the prompt (lazy).
// Called before each session so server-mode ticks get up-to-date context.
func (s *Service) buildSessionPrompt(ctx context.Context, basePrompt string) string {
	placeholders := map[string]PlaceholderFetcher{
		"{{memory}}": func(_ context.Context) string {
			return s.loadMemoryContext(s.toolset.Client("files"))
		},
		"{{market_context}}": s.fetchMarketContext,
		"{{max_steps}}": func(_ context.Context) string {
			return fmt.Sprintf("%d", s.cfg.MaxIterations)
		},
	}
	return expandPlaceholders(ctx, basePrompt, placeholders)
}

func (s *Service) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.log.Info("LLM agent stopped")
	return nil
}

// runOnce runs a single session and returns.
func (s *Service) runOnce(ctx context.Context, basePrompt string) error {
	s.log.Info("Running single session (once mode)")
	sessionPrompt := s.buildSessionPrompt(ctx, basePrompt)
	result, err := s.runSession(ctx, sessionPrompt, "Execute your trading strategy now. Analyze the market and make decisions.")
	if err != nil {
		s.log.Error("Session failed", zap.Error(err))
		return err
	}
	s.log.Info("agent decision: " + result)
	return ErrSessionDone
}

// runCLI runs an interactive loop reading from stdin.
func (s *Service) runCLI(ctx context.Context, basePrompt string) error {
	s.log.Info("Starting CLI mode (type input, Ctrl-D to exit)")
	scanner := s.stdinReader()

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break // EOF or error
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}

		sessionPrompt := s.buildSessionPrompt(ctx, basePrompt)
		result, err := s.runSession(ctx, sessionPrompt, input)
		if err != nil {
			s.log.Error("Session failed", zap.Error(err))
			continue
		}
		s.log.Info("agent decision: " + result)
	}
	return nil
}

// runServer runs sessions on a ticker interval.
func (s *Service) runServer(ctx context.Context, basePrompt string) error {
	interval, err := time.ParseDuration(s.cfg.Interval)
	if err != nil {
		s.log.Warn("Invalid interval, using 5m default",
			zap.String("configured", s.cfg.Interval), zap.Error(err))
		interval = 5 * time.Minute
	}

	s.log.Info("Starting server mode", zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run first session immediately.
	s.runServerTick(ctx, basePrompt)

	for {
		select {
		case <-ticker.C:
			s.runServerTick(ctx, basePrompt)
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *Service) runServerTick(ctx context.Context, basePrompt string) {
	s.log.Info("Server tick - starting session")
	sessionPrompt := s.buildSessionPrompt(ctx, basePrompt)
	result, err := s.runSession(ctx, sessionPrompt, "Execute your trading strategy now. Analyze the market and make decisions.")
	if err != nil {
		s.log.Error("Session failed", zap.Error(err))
		return
	}
	s.log.Info("Session completed", zap.String("result", result))
}

// runSession is the core LLM tool-calling loop.
// It sends messages to OpenAI, executes any tool calls, feeds results back,
// and repeats until the LLM returns a text-only response or max iterations are reached.
func (s *Service) runSession(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	log := s.log.Named("llm") // llm_agent.llm — session-level LLM + tool call logs

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userMessage},
	}

	tools := s.toolset.ToOpenAITools()

	for i := 0; i < s.cfg.MaxIterations; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// ilog is scoped to this iteration: llm_agent.llm.0, llm_agent.llm.1, …
		// SigNoz shows: timestamp | llm_agent.llm.N | message
		ilog := log.Named(fmt.Sprintf("%d", i))

		lastPreview := messagePreview(messages)
		msgText := fmt.Sprintf("LLM request: %s + %d tools", lastPreview, len(tools))
		if reqJSON, err := json.Marshal(messages); err == nil {
			fields := []zap.Field{
				zap.String("model", s.cfg.Model),
				zap.ByteString("messages", reqJSON),
			}
			// Include full tool definitions on first iteration only (same every time).
			if i == 0 {
				if toolsJSON, err := json.Marshal(tools); err == nil {
					fields = append(fields, zap.ByteString("tools", toolsJSON))
				}
			}
			ilog.Info(msgText, fields...)
		} else {
			ilog.Info(msgText, zap.String("model", s.cfg.Model), zap.Int("messages", len(messages)))
		}

		callStart := time.Now()
		resp, err := s.chat.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    s.cfg.Model,
			Messages: messages,
			Tools:    tools,
		})
		durationMs := time.Since(callStart).Milliseconds()
		if err != nil {
			return "", fmt.Errorf("openai chat completion (iteration %d): %w", i, err)
		}

		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("openai returned no choices (iteration %d)", i)
		}

		choice := resp.Choices[0]
		msg := choice.Message

		// Log the response: token usage + timing in message for easy scanning, full payload in field.
		respPreview := responsePreview(msg)
		cachedTokens := 0
		if resp.Usage.PromptTokensDetails != nil {
			cachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
		}
		if respJSON, err := json.Marshal(msg); err == nil {
			ilog.Info(fmt.Sprintf("LLM response: %din %dout %dtok cached:%d | %s (%dms)",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens,
				cachedTokens, respPreview, durationMs),
				zap.Int("prompt_tokens", resp.Usage.PromptTokens),
				zap.Int("completion_tokens", resp.Usage.CompletionTokens),
				zap.Int("total_tokens", resp.Usage.TotalTokens),
				zap.Int("cached_tokens", cachedTokens),
				zap.String("finish_reason", string(choice.FinishReason)),
				zap.Int64("duration_ms", durationMs),
				zap.ByteString("message", respJSON),
			)
		}

		// If the LLM returned text with no tool calls, it's the final decision.
		if len(msg.ToolCalls) == 0 {
			decision, thinking := stripThinking(msg.Content)
			if thinking != "" {
				ilog.Debug("LLM thinking: " + observability.LogPreview(thinking, previewMaxLen))
			}
			return decision, nil
		}

		// Append the assistant message with tool calls.
		messages = append(messages, msg)

		// Execute each tool call and append results.
		for _, tc := range msg.ToolCalls {
			toolName := tc.Function.Name
			toolArgs := tc.Function.Arguments

			ilog.Info("Tool call: "+toolName+" "+observability.LogPreview(toolArgs, previewMaxLen),
				zap.String("tool", toolName), zap.String("args", toolArgs))

			toolStart := time.Now()
			result, toolErr := s.toolset.CallToolJSON(toolName, toolArgs)
			toolMs := time.Since(toolStart).Milliseconds()

			var resultText string
			if toolErr != nil {
				resultText = fmt.Sprintf("error: %v", toolErr)
				ilog.Error(fmt.Sprintf("Tool error: %s %s (%dms)", toolName, observability.LogPreview(resultText, previewMaxLen), toolMs),
					zap.String("tool", toolName), zap.String("error", resultText), zap.Int64("duration_ms", toolMs))
			} else if result.IsError && len(result.Content) > 0 {
				resultText = result.Content[0].Text
				ilog.Warn(fmt.Sprintf("Tool rejected: %s %s (%dms)", toolName, observability.LogPreview(resultText, previewMaxLen), toolMs),
					zap.String("tool", toolName), zap.String("reason", resultText), zap.Int64("duration_ms", toolMs))
			} else if result.IsError {
				resultText = "tool returned error with no details"
				ilog.Warn(fmt.Sprintf("Tool rejected: %s (%dms)", toolName, toolMs),
					zap.String("tool", toolName), zap.String("reason", resultText), zap.Int64("duration_ms", toolMs))
			} else if len(result.Content) > 0 {
				resultText = result.Content[0].Text
				ilog.Info(fmt.Sprintf("Tool result: %s %s (%dms)", toolName, observability.LogPreview(resultText, previewMaxLen), toolMs),
					zap.String("tool", toolName), zap.String("result", resultText), zap.Int64("duration_ms", toolMs))
			} else {
				resultText = "success"
				ilog.Info(fmt.Sprintf("Tool result: %s (%dms)", toolName, toolMs),
					zap.String("tool", toolName), zap.String("result", resultText), zap.Int64("duration_ms", toolMs))
			}

			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultText,
				ToolCallID: tc.ID,
			})
		}
	}

	// Max iterations reached - return whatever we have.
	log.Warn("Max iterations reached", zap.Int("max", s.cfg.MaxIterations))
	lastContent := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == openai.ChatMessageRoleAssistant && messages[i].Content != "" {
			lastContent = messages[i].Content
			break
		}
	}
	decision, _ := stripThinking(lastContent)
	return decision, nil
}
