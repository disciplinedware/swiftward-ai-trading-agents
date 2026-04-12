package config

import (
	"testing"
)

func TestLoadLLMAgentConfig(t *testing.T) {
	tests := []struct {
		name     string
		envs     map[string]string
		checkCfg func(t *testing.T, cfg *Config)
	}{
		{
			name: "defaults",
			envs: map[string]string{
				"TRADING__ROLE": "llm_agent",
			},
			checkCfg: func(t *testing.T, cfg *Config) {
				if cfg.LLMAgent.AgentID != "agent-llm-001" {
					t.Errorf("agent_id: got %q, want %q", cfg.LLMAgent.AgentID, "agent-llm-001")
				}
				if cfg.LLMAgent.Model != "gpt-5.4-mini" {
					t.Errorf("model: got %q, want %q", cfg.LLMAgent.Model, "gpt-5.4-mini")
				}
				if cfg.LLMAgent.MaxIterations != 10 {
					t.Errorf("max_iterations: got %d, want %d", cfg.LLMAgent.MaxIterations, 10)
				}
				if cfg.LLMAgent.Mode != "once" {
					t.Errorf("mode: got %q, want %q", cfg.LLMAgent.Mode, "once")
				}
				if cfg.LLMAgent.Interval != "5m" {
					t.Errorf("interval: got %q, want %q", cfg.LLMAgent.Interval, "5m")
				}
				if cfg.LLMAgent.TradingMCPURL != "http://localhost:8091/mcp/trading" {
					t.Errorf("trading_mcp_url: got %q, want default", cfg.LLMAgent.TradingMCPURL)
				}
				if cfg.LLMAgent.FilesMCPURL != "http://localhost:8091/mcp/files" {
					t.Errorf("files_mcp_url: got %q, want default", cfg.LLMAgent.FilesMCPURL)
				}
			},
		},
		{
			name: "env overrides",
			envs: map[string]string{
				"TRADING__ROLE":                        "llm_agent",
				"TRADING__LLM_AGENT__AGENT_ID":         "my-agent",
				"TRADING__LLM_AGENT__API_KEY":          "sk-test-key",
				"TRADING__LLM_AGENT__TRADING_MCP_URL":  "http://swiftward:8091/mcp/trading",
				"TRADING__LLM_AGENT__OPENAI_API_KEY":   "sk-openai-test",
				"TRADING__LLM_AGENT__MODEL":            "gpt-4o-mini",
				"TRADING__LLM_AGENT__MAX_ITERATIONS":   "25",
				"TRADING__LLM_AGENT__PROMPT_FILE":      "/prompts/strategy.md",
				"TRADING__LLM_AGENT__MODE":             "server",
				"TRADING__LLM_AGENT__INTERVAL":         "10m",
			},
			checkCfg: func(t *testing.T, cfg *Config) {
				if cfg.LLMAgent.AgentID != "my-agent" {
					t.Errorf("agent_id: got %q, want %q", cfg.LLMAgent.AgentID, "my-agent")
				}
				if cfg.LLMAgent.APIKey != "sk-test-key" {
					t.Errorf("api_key: got %q, want %q", cfg.LLMAgent.APIKey, "sk-test-key")
				}
				if cfg.LLMAgent.TradingMCPURL != "http://swiftward:8091/mcp/trading" {
					t.Errorf("trading_mcp_url: got %q", cfg.LLMAgent.TradingMCPURL)
				}

				if cfg.LLMAgent.OpenAIAPIKey != "sk-openai-test" {
					t.Errorf("openai_api_key: got %q", cfg.LLMAgent.OpenAIAPIKey)
				}
				if cfg.LLMAgent.Model != "gpt-4o-mini" {
					t.Errorf("model: got %q, want %q", cfg.LLMAgent.Model, "gpt-4o-mini")
				}
				if cfg.LLMAgent.MaxIterations != 25 {
					t.Errorf("max_iterations: got %d, want %d", cfg.LLMAgent.MaxIterations, 25)
				}
				if cfg.LLMAgent.PromptFile != "/prompts/strategy.md" {
					t.Errorf("prompt_file: got %q", cfg.LLMAgent.PromptFile)
				}
				if cfg.LLMAgent.Mode != "server" {
					t.Errorf("mode: got %q, want %q", cfg.LLMAgent.Mode, "server")
				}
				if cfg.LLMAgent.Interval != "10m" {
					t.Errorf("interval: got %q, want %q", cfg.LLMAgent.Interval, "10m")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			tt.checkCfg(t, cfg)
		})
	}
}

func TestChainAndSwiftwardDefaults(t *testing.T) {
	t.Setenv("TRADING__ROLE", "trading_mcp")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Chain.ChainID != "11155111" {
		t.Errorf("chain.chain_id: got %q, want 11155111", cfg.Chain.ChainID)
	}
	if cfg.Chain.RPCURL != "" {
		t.Errorf("chain.rpc_url: got %q, want empty (not set)", cfg.Chain.RPCURL)
	}

	if cfg.Swiftward.Enabled {
		t.Error("swiftward.enabled: got true, want false (disabled by default)")
	}
	if cfg.Swiftward.IngestAddr != "swiftward-server:50051" {
		t.Errorf("swiftward.ingest_addr: got %q, want swiftward-server:50051", cfg.Swiftward.IngestAddr)
	}
	if cfg.Swiftward.Stream != "trading" {
		t.Errorf("swiftward.stream: got %q, want trading", cfg.Swiftward.Stream)
	}
	if cfg.Swiftward.Timeout != "5s" {
		t.Errorf("swiftward.timeout: got %q, want 5s", cfg.Swiftward.Timeout)
	}

	if cfg.EvidencePort != ":8092" {
		t.Errorf("evidence_port: got %q, want :8092", cfg.EvidencePort)
	}
}

func TestChainAndSwiftwardEnvOverrides(t *testing.T) {
	t.Setenv("TRADING__ROLE", "trading_mcp")
	t.Setenv("TRADING__CHAIN__RPC_URL", "https://sepolia.infura.io/v3/testkey")
	t.Setenv("TRADING__CHAIN__CHAIN_ID", "1")
	t.Setenv("TRADING__CHAIN__VALIDATOR_PRIVATE_KEY", "0xdeadbeef")
	t.Setenv("TRADING__CHAIN__RISK_ROUTER_ADDR", "0xRouter")
	t.Setenv("TRADING__SWIFTWARD__ENABLED", "true")
	t.Setenv("TRADING__SWIFTWARD__INGEST_ADDR", "my-sw:50051")
	t.Setenv("TRADING__SWIFTWARD__STREAM", "custom-stream")
	t.Setenv("TRADING__SWIFTWARD__TIMEOUT", "10s")
	t.Setenv("TRADING__EVIDENCE_PORT", ":9092")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Chain.RPCURL != "https://sepolia.infura.io/v3/testkey" {
		t.Errorf("chain.rpc_url: got %q", cfg.Chain.RPCURL)
	}
	if cfg.Chain.ChainID != "1" {
		t.Errorf("chain.chain_id: got %q, want 1", cfg.Chain.ChainID)
	}
	if cfg.Chain.ValidatorPrivateKey != "0xdeadbeef" {
		t.Errorf("chain.validator_private_key: got %q", cfg.Chain.ValidatorPrivateKey)
	}
	if cfg.Chain.RiskRouterAddr != "0xRouter" {
		t.Errorf("chain.risk_router_addr: got %q", cfg.Chain.RiskRouterAddr)
	}
	if !cfg.Swiftward.Enabled {
		t.Error("swiftward.enabled: got false, want true")
	}
	if cfg.Swiftward.IngestAddr != "my-sw:50051" {
		t.Errorf("swiftward.ingest_addr: got %q", cfg.Swiftward.IngestAddr)
	}
	if cfg.Swiftward.Stream != "custom-stream" {
		t.Errorf("swiftward.stream: got %q", cfg.Swiftward.Stream)
	}
	if cfg.Swiftward.Timeout != "10s" {
		t.Errorf("swiftward.timeout: got %q", cfg.Swiftward.Timeout)
	}
	if cfg.EvidencePort != ":9092" {
		t.Errorf("evidence_port: got %q", cfg.EvidencePort)
	}
}

func TestAgentPrivateKeyConfig(t *testing.T) {
	t.Setenv("TRADING__ROLE", "trading_mcp")
	t.Setenv("TRADING__AGENTS__0__ID", "agent-alpha")
	t.Setenv("TRADING__AGENTS__0__PRIVATE_KEY", "0xdeadbeefdeadbeef")
	t.Setenv("TRADING__AGENTS__0__WALLET_ADDR", "0xAgentWalletContract")
	t.Setenv("TRADING__AGENTS__1__ID", "agent-beta")
	// agent-beta has no private key — must remain empty

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// koanf keys agents by numeric index ("0", "1", ...) not by agent ID
	agent0, ok0 := cfg.Agents["0"]
	if !ok0 {
		t.Fatal("agents[0] not found")
	}
	if agent0.ID != "agent-alpha" {
		t.Errorf("agents[0].id: got %q, want agent-alpha", agent0.ID)
	}
	if agent0.PrivateKey != "0xdeadbeefdeadbeef" {
		t.Errorf("agents[0].private_key: got %q, want 0xdeadbeefdeadbeef", agent0.PrivateKey)
	}
	if agent0.WalletAddr != "0xAgentWalletContract" {
		t.Errorf("agents[0].wallet_addr: got %q, want 0xAgentWalletContract", agent0.WalletAddr)
	}
	agent1, ok1 := cfg.Agents["1"]
	if ok1 && agent1.PrivateKey != "" {
		t.Errorf("agents[1].private_key: got %q, want empty", agent1.PrivateKey)
	}
}

func TestReadRolesLLMAgent(t *testing.T) {
	tests := []struct {
		name      string
		role      string
		wantRoles []string
		wantErr   bool
	}{
		{"llm_agent alone", "llm_agent", []string{"llm_agent"}, false},
		{"llm_agent with trading", "trading_mcp,llm_agent", []string{"trading_mcp", "llm_agent"}, false},
		{"all server roles", "trading_mcp,risk_mcp,market_data_mcp,files_mcp,code_mcp,news_mcp,dashboard", []string{"trading_mcp", "risk_mcp", "market_data_mcp", "files_mcp", "code_mcp", "news_mcp", "dashboard"}, false},
		{"trailing comma ignored", "trading_mcp,", []string{"trading_mcp"}, false},
		{"unknown role fails fast", "invalid_role", nil, true},
		{"mixed valid and invalid fails fast", "trading_mcp,dashbaord", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Role: tt.role}
			roles, err := ReadRoles(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadRoles() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(roles) != len(tt.wantRoles) {
					t.Errorf("got %d roles, want %d", len(roles), len(tt.wantRoles))
					return
				}
				for i, r := range roles {
					if r != tt.wantRoles[i] {
						t.Errorf("role[%d]: got %q, want %q", i, r, tt.wantRoles[i])
					}
				}
			}
		})
	}
}
