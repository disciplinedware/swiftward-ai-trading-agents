package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/providers/env"
)

// Role constants
const (
	RoleTradingMCP    = "trading_mcp"
	RoleRiskMCP       = "risk_mcp"
	RoleMarketDataMCP = "market_data_mcp"
	RoleFilesMCP      = "files_mcp"
	RoleCodeMCP       = "code_mcp"
	RoleNewsMCP       = "news_mcp"
	RolePolymarketMCP = "polymarket_mcp"
	RoleDashboard     = "dashboard"
	RoleRandomAgent   = "random_agent"
	RoleLLMAgent      = "llm_agent"
)

// DBConfig holds the trading database connection string.
type DBConfig struct {
	URL string `koanf:"url"` // e.g., "postgres://trading:trading@postgres:5432/trading?sslmode=disable"
}

// Config is the top-level configuration.
type Config struct {
	Role   string       `koanf:"role"`
	Server ServerConfig `koanf:"server"`

	Logging LoggingConfig `koanf:"logging"`

	DB DBConfig `koanf:"db"`

	Exchange ExchangeConfig `koanf:"exchange"`

	// Agent registry — one entry per managed agent.
	// Agents not in this map can still trade via dynamic registration (X-Agent-ID header).
	Agents map[string]AgentConfig `koanf:"agents"`

	// Default initial balance for dynamically registered agents (default 10000).
	DefaultInitialBalance float64 `koanf:"default_initial_balance"`

	// Files MCP configuration
	FilesMCP FilesMCPConfig `koanf:"files_mcp"`

	// Code MCP configuration
	CodeMCP CodeMCPConfig `koanf:"code_mcp"`

	// Trading MCP configuration (conditional orders, alert polling)
	TradingMCP TradingMCPConfig `koanf:"trading_mcp"`

	// Market Data MCP configuration
	MarketData MarketDataConfig `koanf:"market_data"`

	// News MCP configuration
	News NewsConfig `koanf:"news"`

	// Random agent configuration
	RandomAgent RandomAgentConfig `koanf:"random_agent"`

	// LLM agent configuration
	LLMAgent LLMAgentConfig `koanf:"llm_agent"`

	// Chain configuration (EIP-712 signing + ERC-8004 registry)
	Chain ChainConfig `koanf:"chain"`

	// Swiftward policy engine integration
	Swiftward SwiftwardConfig `koanf:"swiftward"`

	// Evidence API port for GET /evidence/{hash} (default ":8092")
	EvidencePort string `koanf:"evidence_port"`
}

// ChainConfig holds blockchain connection and platform-level key configuration.
// Per-agent private keys live in AgentConfig, not here.
// Env prefix: TRADING__CHAIN__*
type ChainConfig struct {
	RPCURL  string `koanf:"rpc_url"`  // e.g. "https://sepolia.infura.io/v3/KEY"
	ChainID string `koanf:"chain_id"` // e.g. "11155111" (Sepolia)

	// Swiftward validator — independent platform identity, separate from all agents.
	// Responds to ERC-8004 validation requests after trades.
	// Address is derived from the private key at runtime.
	ValidatorPrivateKey string `koanf:"validator_private_key"`

	// ERC-8004 contract addresses on-chain
	IdentityRegistryAddr   string `koanf:"identity_registry_addr"`
	ValidationRegistryAddr string `koanf:"validation_registry_addr"`
	RiskRouterAddr         string `koanf:"risk_router_addr"`

	// Hardcoded confidence score (0-100) for validation checkpoints.
	// When > 0, overrides the agent's self-reported confidence.
	// When 0, uses the agent's confidence from trade params.
	HardcodedConfidence int `koanf:"hardcoded_confidence"`

	// Max length for reasoning text in attestation notes (on-chain).
	// Full reasoning is preserved in the decision hash; notes carry a truncated preview.
	// Default: 200. Set to 0 to disable truncation.
	MaxNotesReasoning int `koanf:"max_notes_reasoning"`
}

// SwiftwardConfig holds the Swiftward ingestion gRPC address + toggles.
// Env prefix: TRADING__SWIFTWARD__*
type SwiftwardConfig struct {
	Enabled    bool   `koanf:"enabled"`     // toggle policy evaluation on/off
	IngestAddr string `koanf:"ingest_addr"` // e.g. "swiftward-server:50051"
	Stream     string `koanf:"stream"`      // ingestion stream name
	Timeout    string `koanf:"timeout"`     // e.g. "5s"
}

type ServerConfig struct {
	Addr string `koanf:"addr"` // e.g., ":8091"
}

type LoggingConfig struct {
	Level  string `koanf:"level"`  // debug, info, warn, error
	Format string `koanf:"format"` // json, console
}

type ExchangeConfig struct {
	Mode           string `koanf:"mode"`             // "sim" (default), "paper" (real prices via Binance), "kraken_paper", "kraken_real", "real"
	URL            string `koanf:"url"`              // Hackathon exchange API URL (real mode only)
	Timeout        string `koanf:"timeout"`          // Request timeout (e.g., "10s")
	CommissionRate string `koanf:"commission_rate"`  // Side-dependent fee rate for sim/paper modes (default "0.001"). Kraken modes use Kraken's own fees.
	KrakenBin      string `koanf:"kraken_bin"`       // Path to kraken CLI binary (default "kraken"). For real mode: set KRAKEN_API_KEY + KRAKEN_API_SECRET env vars.
	KrakenStateDir string `koanf:"kraken_state_dir"` // Base dir for per-agent Kraken state (e.g. /data/kraken). Each agent gets HOME={dir}/{agent_id}.
}

type AgentConfig struct {
	ID              string  `koanf:"id"`
	Name            string  `koanf:"name"`
	APIKey          string  `koanf:"api_key"`
	InitialBalance  float64 `koanf:"initial_balance"` // parsed to decimal.Decimal in service
	PrivateKey      string  `koanf:"private_key"`      // agent's own EOA key - signs EIP-712 TradeIntents
	WalletAddr      string  `koanf:"wallet_addr"`      // SwiftwardAgentWallet contract (guardian = this agent's EOA)
	ERC8004AgentID  string  `koanf:"erc8004_agent_id"` // ERC-721 agentId from Identity Registry (set after registration)
	RegistrationURI string  `koanf:"registration_uri"` // IPFS URI to agent registration JSON
}

type FilesMCPConfig struct {
	RootDir string `koanf:"root_dir"` // e.g., "/data/workspace"
}

type CodeMCPConfig struct {
	WorkspaceDir      string `koanf:"workspace_dir"`       // e.g., "/data/workspace"
	SandboxImage      string `koanf:"sandbox_image"`       // e.g., "ghcr.io/disciplinedware/ai-trading-agents/sandbox-python:latest"
	IdleTimeout       string `koanf:"idle_timeout"`        // e.g., "30m"
	StartupTimeout    string `koanf:"startup_timeout"`     // e.g., "60s" - time to wait for repl.py on cold container start
	HostWorkspacePath string `koanf:"host_workspace_path"` // absolute host path for DinD bind mounts (e.g., "/Users/name/project/data/workspace")
	DockerNetwork     string `koanf:"docker_network"`      // Docker network for sandbox containers (e.g., "ai-trading-agents_default"). When set, uses container name instead of host ports.
}

// TradingMCPConfig holds Trading MCP settings (conditional orders, alert polling).
// Env prefix: TRADING__TRADING_MCP__*
type TradingMCPConfig struct {
	// EnableNativeStops enables Tier 1 native exchange stop orders (e.g. Kraken).
	// When false (default), all conditional orders use Tier 2 (platform polling).
	EnableNativeStops bool   `koanf:"enable_native_stops"`
	AlertPollInterval string `koanf:"alert_poll_interval"` // e.g., "10s"
	// ReconcileInterval is how often to reconcile DB with exchange fill history.
	// Default "5m". Set "0" to disable periodic reconciliation (startup still runs).
	ReconcileInterval string `koanf:"reconcile_interval"` // e.g., "5m"
}

type MarketDataConfig struct {
	// Sources is an ordered list of data source names tried in sequence (degradation chain).
	// Valid values: "kraken", "binance", "bybit", "simulated" (dev/test only, standalone only).
	// Example: "kraken,binance,bybit" (first is primary, rest are fallbacks).
	Sources           []string `koanf:"sources"`
	Markets           []string `koanf:"markets"`
	Volatility        float64  `koanf:"volatility"`          // annualized vol % (simulated mode, default 80)
	AlertPollInterval string   `koanf:"alert_poll_interval"` // e.g., "10s"
	CandleHistory     int      `koanf:"candle_history"`      // pre-generate N candles per interval (simulated)

	// Timeout is the base HTTP timeout applied to all real market data sources
	// (kraken, binance, bybit). Kraken uses it as the first-attempt deadline and
	// grows it on retry (see MaxAttempts). Default: "10s".
	Timeout string `koanf:"timeout"`
	// MaxAttempts is the retry count for kraken source requests. Attempt N uses
	// timeout = Timeout * N (e.g., 10s, 20s, 30s for 3 attempts). Default: 3.
	MaxAttempts int `koanf:"max_attempts"`

	// PRISM API - market intelligence enrichment (fear/greed, technicals, signals).
	// Disabled by default. Enable with TRADING__MARKET_DATA__PRISM__ENABLED=true + API key.
	Prism PrismConfig `koanf:"prism"`
}

// PrismConfig holds PRISM API (prismapi.ai) settings.
// Env prefix: TRADING__MARKET_DATA__PRISM__*
type PrismConfig struct {
	Enabled          bool   `koanf:"enabled"`           // master toggle
	APIKey           string `koanf:"api_key"`           // X-API-Key header value
	BaseURL          string `koanf:"base_url"`          // default: "https://api.prismapi.ai"
	Timeout          string `koanf:"timeout"`           // HTTP timeout (default: "10s")
	FailureThreshold int    `koanf:"failure_threshold"` // consecutive failures to trip circuit breaker (default: 3)
	Cooldown         string `koanf:"cooldown"`          // time in open state before probe (default: "60s")
}

// NewsConfig holds the News MCP configuration.
// Env prefix: TRADING__NEWS__*
type NewsConfig struct {
	// Sources specifies the news source. Only the first entry is used.
	// Valid values: "cryptocompare", "cryptopanic".
	Sources []string `koanf:"sources"`

	// CryptoPanic auth token (legacy, free tier discontinued).
	CryptoPanicToken string `koanf:"cryptopanic_token"`
}

type RandomAgentConfig struct {
	Interval string   `koanf:"interval"` // e.g., "10s"
	Markets  []string `koanf:"markets"`  // e.g., ["ETH-USDC", "BTC-USDC"]
	MaxSize  float64  `koanf:"max_size"` // Max trade size in USD
	AgentID  string   `koanf:"agent_id"` // Which agent identity to use
	APIKey   string   `koanf:"api_key"`  // API key for MCP authentication
	MCPURL   string   `koanf:"mcp_url"`  // Trading MCP endpoint (e.g., http://localhost:8091/mcp/trading)
}

type LLMAgentConfig struct {
	AgentID          string   `koanf:"agent_id"`
	APIKey           string   `koanf:"api_key"`
	TradingMCPURL    string   `koanf:"trading_mcp_url"`
	FilesMCPURL      string   `koanf:"files_mcp_url"`
	MarketDataMCPURL string   `koanf:"market_data_mcp_url"`
	NewsMCPURL       string   `koanf:"news_mcp_url"`
	CodeMCPURL       string   `koanf:"code_mcp_url"`
	CodeMCPTimeout   string   `koanf:"code_mcp_timeout"` // e.g. "5m" — must cover container start + Python execution
	PolymarketMCPURL string   `koanf:"polymarket_mcp_url"`
	OpenAIAPIKey     string   `koanf:"openai_api_key"`
	LLMURL           string   `koanf:"llm_url"`     // Optional: LLM Gateway base URL. If set, routes LLM calls through the gateway.
	LLMTimeout       string   `koanf:"llm_timeout"` // HTTP timeout for LLM calls (e.g. "5m"). Local models need longer.
	Model            string   `koanf:"model"`
	MaxIterations    int      `koanf:"max_iterations"`
	PromptFile       string   `koanf:"prompt_file"`
	Mode             string   `koanf:"mode"`     // "once", "cli", "server"
	Interval         string   `koanf:"interval"` // duration string, only used in server mode
	Markets          []string `koanf:"markets"`  // default markets for {{market_context}} when no open positions
}

// Load loads configuration from environment variables.
// Env prefix: TRADING__ (double underscore = path separator).
func Load() (*Config, error) {
	k := koanf.New(".")

	// Load from environment variables (TRADING__* prefix)
	if err := k.Load(env.Provider("TRADING__", ".", func(s string) string {
		trimmed := strings.TrimPrefix(s, "TRADING__")
		trimmed = strings.ReplaceAll(trimmed, "__", ".")
		return strings.ToLower(trimmed)
	}), nil); err != nil {
		return nil, fmt.Errorf("error loading env vars: %w", err)
	}

	setDefaults(k)

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// koanf's env.Provider stores all values as strings. When a []string field is set via
	// an env var (e.g. TRADING__MARKET_DATA__MARKETS="ETH-USDC,BTC-USDC"), mapstructure
	// wraps it into a single-element slice ["ETH-USDC,BTC-USDC"] instead of splitting it.
	// Normalize those fields here.
	cfg.MarketData.Sources = splitCSVSlice(cfg.MarketData.Sources)
	cfg.MarketData.Markets = splitCSVSlice(cfg.MarketData.Markets)
	cfg.News.Sources = splitCSVSlice(cfg.News.Sources)
	cfg.RandomAgent.Markets = splitCSVSlice(cfg.RandomAgent.Markets)
	cfg.LLMAgent.Markets = splitCSVSlice(cfg.LLMAgent.Markets)

	return &cfg, nil
}

// splitCSVSlice handles the case where a []string field was populated from a single
// comma-separated env var value. If the slice has exactly one element containing
// commas, it is split into multiple trimmed elements.
func splitCSVSlice(s []string) []string {
	if len(s) == 1 && strings.Contains(s[0], ",") {
		parts := strings.Split(s[0], ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	return s
}

// ReadRoles parses the comma-separated role string.
func ReadRoles(cfg *Config) ([]string, error) {
	if cfg.Role == "" {
		return nil, fmt.Errorf("role is required (set TRADING__ROLE)")
	}

	validRoles := map[string]bool{
		RoleTradingMCP:    true,
		RoleRiskMCP:       true,
		RoleMarketDataMCP: true,
		RoleFilesMCP:      true,
		RoleCodeMCP:       true,
		RoleNewsMCP:       true,
		RolePolymarketMCP: true,
		RoleDashboard:     true,
		RoleRandomAgent:   true,
		RoleLLMAgent:      true,
	}

	var roles []string
	for _, part := range strings.Split(cfg.Role, ",") {
		role := strings.TrimSpace(part)
		if role == "" {
			continue
		}
		if !validRoles[role] {
			return nil, fmt.Errorf("unknown role %q in %q", role, cfg.Role)
		}
		roles = append(roles, role)
	}

	if len(roles) == 0 {
		return nil, fmt.Errorf("no valid roles in %q", cfg.Role)
	}
	return roles, nil
}

func setDefaults(k *koanf.Koanf) {
	d := func(key string, val interface{}) {
		if !k.Exists(key) {
			_ = k.Set(key, val)
		}
	}

	d("server.addr", ":8091")
	d("logging.level", "info")
	d("logging.format", "console")

	d("db.url", "postgres://trading:trading@postgres:5432/trading?sslmode=disable")

	d("exchange.url", "http://localhost:9999") // stub
	d("exchange.timeout", "10s")
	d("exchange.commission_rate", "0.001") // 0.1% Bybit-style

	d("files_mcp.root_dir", "/data/workspace")

	d("code_mcp.workspace_dir", "/data/workspace")
	d("code_mcp.sandbox_image", "ghcr.io/disciplinedware/ai-trading-agents/sandbox-python:latest")
	d("code_mcp.idle_timeout", "30m")
	d("code_mcp.startup_timeout", "60s")

	d("trading_mcp.enable_native_stops", false)
	d("trading_mcp.alert_poll_interval", "10s")

	d("market_data.sources", []string{"kraken", "bybit"})
	d("market_data.markets", []string{"ETH-USD", "BTC-USD", "SOL-USD"})
	d("market_data.volatility", 80.0)
	d("market_data.alert_poll_interval", "10s")
	d("market_data.candle_history", 500)
	d("market_data.timeout", "10s")
	d("market_data.max_attempts", 3)

	d("market_data.prism.enabled", false)
	d("market_data.prism.base_url", "https://api.prismapi.ai")
	d("market_data.prism.timeout", "10s")
	d("market_data.prism.failure_threshold", 3)
	d("market_data.prism.cooldown", "60s")

	d("news.sources", []string{"cryptocompare"})

	d("random_agent.interval", "10s")
	d("random_agent.markets", []string{"ETH-USD", "BTC-USD"})
	d("random_agent.max_size", 100.0)
	d("random_agent.agent_id", "agent-random-001")
	d("random_agent.mcp_url", "http://localhost:8091/mcp/trading")

	d("llm_agent.agent_id", "agent-llm-001")
	d("llm_agent.trading_mcp_url", "http://localhost:8091/mcp/trading")
	d("llm_agent.files_mcp_url", "http://localhost:8091/mcp/files")
	d("llm_agent.market_data_mcp_url", "http://localhost:8091/mcp/market")
	d("llm_agent.news_mcp_url", "http://localhost:8091/mcp/news")
	d("llm_agent.code_mcp_url", "http://localhost:8091/mcp/code")
	d("llm_agent.code_mcp_timeout", "5m")
	d("llm_agent.polymarket_mcp_url", "http://localhost:8091/mcp/polymarket")
	d("llm_agent.llm_timeout", "5m")
	d("llm_agent.model", "gpt-5.4-mini")
	d("llm_agent.max_iterations", 10)
	d("llm_agent.prompt_file", "./prompts/agent-delta-go/prompt.md")
	d("llm_agent.mode", "once")
	d("llm_agent.interval", "5m")
	d("llm_agent.markets", []string{"ETH-USD", "BTC-USD", "SOL-USD"})

	d("chain.chain_id", "11155111")          // Sepolia testnet
	d("chain.max_notes_reasoning", 200)      // truncate reasoning in attestation notes

	d("swiftward.enabled", false)
	d("swiftward.ingest_addr", "swiftward-server:50051")
	d("swiftward.stream", "trading")
	d("swiftward.timeout", "5s")

	d("evidence_port", ":8092")
}
