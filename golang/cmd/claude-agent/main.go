// claude-agent is the Go harness that orchestrates Claude Code as a trading agent.
// It runs inside the claude-agent Docker container as the main process, spawning
// claude --print as a child process on a configurable interval.
//
// Modes:
//   - Loop mode (default): runs sessions every CLAUDE_AGENT__INTERVAL, polls alerts between sessions
//   - Once mode (--once): runs a single session and exits. Use for testing and interactive terminal use.
//
// Architecture (mirrors ralphex pattern):
//
//	claude-agent (this binary, PID 1 in container)
//	  └── claude --print --output-format stream-json (child, new process group)
//	        └── MCP clients → swiftward-server:8095 (trading, market, news)
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/providers/env"
	"go.uber.org/zap"

	"ai-trading-agents/internal/agents/claude_runtime"
	"ai-trading-agents/internal/observability"
)

func main() {
	os.Exit(run())
}

func run() int {
	// --once mode: single session, exit when done
	once := false
	for _, arg := range os.Args[1:] {
		if arg == "--once" || arg == "-once" {
			once = true
		}
	}

	log, err := observability.NewLogger(os.Getenv("CLAUDE_AGENT__LOG_FORMAT"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init: %v\n", err)
		return 1
	}
	defer func() {
		observability.ShutdownLogProvider(context.Background())
		_ = log.Sync()
	}()

	cfg, err := loadConfig()
	if err != nil {
		log.Error("Config load failed", zap.Error(err))
		return 1
	}

	if cfg.AgentID == "" {
		log.Error("CLAUDE_AGENT__AGENT_ID is required")
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if once {
		return runOnce(ctx, cfg, log)
	}

	log.Info("Claude agent starting",
		zap.String("agent_id", cfg.AgentID),
		zap.String("model", cfg.Model),
		zap.Duration("interval", cfg.Interval),
		zap.Duration("session_timeout", cfg.SessionTimeout),
		zap.Int("max_sessions_per_day", cfg.MaxSessionsPerDay),
		zap.Bool("telegram", cfg.TelegramBotToken != "" && cfg.TelegramChatID != 0),
	)

	loop := claude_runtime.NewLoop(*cfg, log.Named("loop"))
	if err := loop.Run(ctx); err != nil {
		log.Error("Loop exited with error", zap.Error(err))
		return 1
	}

	return 0
}

// runOnce executes a single trading session and exits. For interactive terminal use.
func runOnce(ctx context.Context, cfg *claude_runtime.Config, log *zap.Logger) int {
	log.Info("Claude agent single session",
		zap.String("agent_id", cfg.AgentID),
		zap.Duration("session_timeout", cfg.SessionTimeout),
	)

	exec := claude_runtime.NewExecutor(log.Named("executor"))
	exec.IdleTimeout = cfg.IdleTimeout
	if cfg.Model != "" {
		exec.Model = cfg.Model
	}
	exec.OutputHandler = func(text string) {
		fmt.Print(text) // real-time terminal output
	}

	prompt := fmt.Sprintf("Session at %s\nAgent ID: %s\n\nRun /trading-session",
		"now", cfg.AgentID)

	// Try to read the startup prompt template
	if data, err := os.ReadFile(cfg.StartupPromptFile); err == nil {
		prompt = string(data)
		prompt = strings.ReplaceAll(prompt, "{{UTC_TIME}}", "now")
		prompt = strings.ReplaceAll(prompt, "{{SESSION_NUMBER}}", "1")
		prompt = strings.ReplaceAll(prompt, "{{DAILY_SESSION_COUNT}}", "1")
		prompt = strings.ReplaceAll(prompt, "{{AGENT_ID}}", cfg.AgentID)
		// remove conditional blocks
		for _, block := range []string{"{{#if TRIGGERED_ALERTS}}", "{{#if LAST_SESSION_SNIPPET}}"} {
			prompt = claude_runtime.RemoveBlock(prompt, block, "{{/if}}")
		}
	}

	result := exec.Run(ctx, prompt)

	if result.Error != nil {
		log.Error("Session failed", zap.Error(result.Error))
		return 1
	}
	return 0
}

func loadConfig() (*claude_runtime.Config, error) {
	cfg := claude_runtime.DefaultConfig()

	k := koanf.New(".")
	// CLAUDE_AGENT__AGENT_ID → agent_id, etc.
	if err := k.Load(env.Provider("CLAUDE_AGENT__", ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, "CLAUDE_AGENT__"))
	}), nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}

