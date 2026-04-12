package claude_runtime

import "time"

// DefaultLifecycleStream is the Swiftward stream for agent lifecycle events.
const DefaultLifecycleStream = "lifecycle"

// Config holds the configuration for the Claude agent runtime.
type Config struct {
	// AgentID is the unique identifier for this agent instance (e.g. "agent-claude-alpha").
	AgentID string `koanf:"agent_id"`

	// ClaudeCmd is the path to the claude binary. Defaults to "claude".
	ClaudeCmd string `koanf:"claude_cmd"`

	// Interval is how often to run a trading session. Default: 30 minutes.
	Interval time.Duration `koanf:"interval"`

	// SessionTimeout kills any single Claude session that exceeds this duration. Default: 0 (disabled).
	SessionTimeout time.Duration `koanf:"session_timeout"`

	// IdleTimeout kills the session if Claude produces no output for this duration. Default: 10m.
	// Claude may think for extended periods; session timeout is the hard wall-clock limit.
	IdleTimeout time.Duration `koanf:"idle_timeout"`

	// MaxSessionsPerDay is the daily session cap. 0 = unlimited.
	MaxSessionsPerDay int `koanf:"max_sessions_per_day"`

	// MaxConsecutiveErrors triggers needs_attention after this many consecutive failures. 0 = disabled.
	MaxConsecutiveErrors int `koanf:"max_consecutive_errors"`

	// SwiftwardURL is the base URL of the Swiftward ingestion API for lifecycle events.
	// e.g. "http://swiftward-server:8080"
	SwiftwardURL string `koanf:"swiftward_url"`

	// LifecycleStream is the Swiftward stream name for lifecycle events.
	// Default: "lifecycle"
	LifecycleStream string `koanf:"lifecycle_stream"`

	// StartupPromptFile is the path to the startup prompt template.
	// Default: "/workspace/.claude/startup-prompt.md"
	StartupPromptFile string `koanf:"startup_prompt_file"`

	// TradingMCPURL is the trading MCP endpoint for alert polling.
	// The loop polls this between sessions to detect triggered position alerts.
	// Default: "http://trading-server:8091/mcp/trading"
	TradingMCPURL string `koanf:"trading_mcp_url"`

	// MarketMCPURL is the market data MCP endpoint for alert polling.
	// The loop polls this between sessions to detect triggered price alerts.
	// Default: "http://trading-server:8091/mcp/market"
	MarketMCPURL string `koanf:"market_mcp_url"`

	// AlertPollInterval is how often to poll for triggered alerts during the wait interval.
	// Default: 30s
	AlertPollInterval time.Duration `koanf:"alert_poll_interval"`

	// SessionTrailFile is the path to the JSONL file for session audit trail.
	// Each session appends one JSON line: {session_id, started_at, elapsed_ms, signal, error, output_snippet}.
	// Default: "/workspace/logs/sessions.jsonl"
	SessionTrailFile string `koanf:"session_trail_file"`

	// WakeupCooldown is the minimum time between alert-triggered early wakeups.
	// Prevents the agent from thrashing on flapping alerts. Default: 5m.
	WakeupCooldown time.Duration `koanf:"wakeup_cooldown"`

	// MaxWakeupsPerHour caps the number of alert-triggered early wakeups per hour.
	// 0 = unlimited. Default: 6.
	MaxWakeupsPerHour int `koanf:"max_wakeups_per_hour"`

	// Model is the Claude model to use for full sessions. "" = claude's default (Sonnet).
	// Example: "claude-sonnet-4-6"
	Model string `koanf:"model"`

	// TriageModel is the lightweight model for Haiku triage pre-filter sessions.
	// Default: "claude-haiku-4-5-20251001"
	TriageModel string `koanf:"triage_model"`

	// TelegramBotToken is the Telegram bot token. Empty = Telegram disabled.
	TelegramBotToken string `koanf:"telegram_bot_token"`

	// TelegramChatID is the target Telegram chat to send messages to.
	TelegramChatID int64 `koanf:"telegram_chat_id"`

	// TelegramTopicID is the forum topic/thread ID within the chat. 0 = no topic.
	TelegramTopicID int `koanf:"telegram_topic_id"`

	// TelegramAllowUIDs is the list of Telegram user IDs allowed to send messages.
	// Empty = only messages from TelegramChatID owner are accepted.
	TelegramAllowUIDs []int64 `koanf:"telegram_allow_uids"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ClaudeCmd:            "claude",
		Interval:             30 * time.Minute,
		SessionTimeout:       0,
		IdleTimeout:          10 * time.Minute,
		MaxSessionsPerDay:    60,
		MaxConsecutiveErrors: 3,
		LifecycleStream:      DefaultLifecycleStream,
		StartupPromptFile:    "/workspace/.claude/startup-prompt.md",
		TradingMCPURL:        "http://trading-server:8091/mcp/trading",
		MarketMCPURL:         "http://trading-server:8091/mcp/market",
		AlertPollInterval:    30 * time.Second,
		SessionTrailFile:     "/workspace/logs/sessions.jsonl",
		WakeupCooldown:       5 * time.Minute,
		MaxWakeupsPerHour:    6,
		TriageModel:          "claude-haiku-4-5-20251001",
	}
}
