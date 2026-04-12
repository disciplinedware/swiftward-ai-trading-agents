use std::env;

/// Valid roles for the trading platform.
pub const ROLE_TRADING_MCP: &str = "trading_mcp";
pub const ROLE_RISK_MCP: &str = "risk_mcp";
pub const ROLE_MEMORY_MCP: &str = "memory_mcp";
pub const ROLE_RANDOM_AGENT: &str = "random_agent";
pub const ROLE_LLM_AGENT: &str = "llm_agent";

const VALID_ROLES: &[&str] = &[
    ROLE_TRADING_MCP,
    ROLE_RISK_MCP,
    ROLE_MEMORY_MCP,
    ROLE_RANDOM_AGENT,
    ROLE_LLM_AGENT,
];

/// Top-level configuration loaded from environment variables.
#[derive(Debug, Clone)]
pub struct Config {
    pub role: String,
    pub logging: LoggingConfig,
    pub llm_agent: LlmAgentConfig,
}

#[derive(Debug, Clone)]
pub struct LoggingConfig {
    pub level: String,
    pub format: String,
}

/// LLM agent configuration. Matches the Go side's `TRADING__LLM_AGENT__*` env prefix
/// so Go and Rust runtimes share the same config shape.
#[derive(Debug, Clone)]
pub struct LlmAgentConfig {
    pub agent_id: String,
    pub api_key: String,
    pub trading_mcp_url: String,
    pub files_mcp_url: String,
    pub market_data_mcp_url: String,
    pub news_mcp_url: String,
    pub code_mcp_url: String,
    pub polymarket_mcp_url: String,
    pub openai_api_key: String,
    pub model: String,
    pub max_iterations: usize,
    pub prompt_file: String,
    pub mode: String,
    pub interval: String,
}

fn env_or(key: &str, default: &str) -> String {
    env::var(key).unwrap_or_else(|_| default.to_string())
}

impl Config {
    /// Load configuration from environment variables with TRADING__ prefix.
    pub fn load() -> Self {
        Self {
            role: env_or("TRADING__ROLE", ""),
            logging: LoggingConfig {
                level: env_or("TRADING__LOGGING__LEVEL", "info"),
                format: env_or("TRADING__LOGGING__FORMAT", "console"),
            },
            llm_agent: LlmAgentConfig {
                agent_id: env_or("TRADING__LLM_AGENT__AGENT_ID", "agent-llm-002"),
                api_key: env_or("TRADING__LLM_AGENT__API_KEY", ""),
                trading_mcp_url: env_or(
                    "TRADING__LLM_AGENT__TRADING_MCP_URL",
                    "http://localhost:8091/mcp/trading",
                ),
                files_mcp_url: env_or(
                    "TRADING__LLM_AGENT__FILES_MCP_URL",
                    "http://localhost:8091/mcp/files",
                ),
                market_data_mcp_url: env_or(
                    "TRADING__LLM_AGENT__MARKET_DATA_MCP_URL",
                    "http://localhost:8091/mcp/market",
                ),
                news_mcp_url: env_or(
                    "TRADING__LLM_AGENT__NEWS_MCP_URL",
                    "http://localhost:8091/mcp/news",
                ),
                code_mcp_url: env_or(
                    "TRADING__LLM_AGENT__CODE_MCP_URL",
                    "http://localhost:8091/mcp/code",
                ),
                polymarket_mcp_url: env_or(
                    "TRADING__LLM_AGENT__POLYMARKET_MCP_URL",
                    "http://localhost:8091/mcp/polymarket",
                ),
                openai_api_key: env_or("TRADING__LLM_AGENT__OPENAI_API_KEY", ""),
                model: env_or("TRADING__LLM_AGENT__MODEL", ""),
                max_iterations: env_or("TRADING__LLM_AGENT__MAX_ITERATIONS", "50")
                    .parse()
                    .unwrap_or(50),
                prompt_file: env_or(
                    "TRADING__LLM_AGENT__PROMPT_FILE",
                    "./prompts/agent-epsilon-rust/prompt.md",
                ),
                mode: env_or("TRADING__LLM_AGENT__MODE", "once"),
                interval: env_or("TRADING__LLM_AGENT__INTERVAL", "5m"),
            },
        }
    }
}

/// Parse and validate roles from config.
pub fn read_roles(cfg: &Config) -> anyhow::Result<Vec<String>> {
    if cfg.role.is_empty() {
        anyhow::bail!("TRADING__ROLE is required");
    }

    let roles: Vec<String> = cfg
        .role
        .split(',')
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .collect();

    for role in &roles {
        if !VALID_ROLES.contains(&role.as_str()) {
            anyhow::bail!("unknown role {role:?} (valid: {VALID_ROLES:?})");
        }
    }

    if roles.is_empty() {
        anyhow::bail!("no valid roles found in TRADING__ROLE={:?}", cfg.role);
    }

    Ok(roles)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_cfg(role: &str) -> Config {
        Config {
            role: role.to_string(),
            logging: LoggingConfig {
                level: "info".to_string(),
                format: "console".to_string(),
            },
            llm_agent: LlmAgentConfig {
                agent_id: "test".to_string(),
                api_key: String::new(),
                trading_mcp_url: String::new(),
                files_mcp_url: String::new(),
                market_data_mcp_url: String::new(),
                news_mcp_url: String::new(),
                code_mcp_url: String::new(),
                polymarket_mcp_url: String::new(),
                openai_api_key: String::new(),
                model: "gpt-5.4-mini".to_string(),
                max_iterations: 50,
                prompt_file: String::new(),
                mode: "once".to_string(),
                interval: "5m".to_string(),
            },
        }
    }

    #[test]
    fn test_read_roles_valid() {
        let roles = read_roles(&test_cfg("llm_agent")).unwrap();
        assert_eq!(roles, vec!["llm_agent"]);
    }

    #[test]
    fn test_read_roles_multiple() {
        let roles = read_roles(&test_cfg("trading_mcp, memory_mcp")).unwrap();
        assert_eq!(roles, vec!["trading_mcp", "memory_mcp"]);
    }

    #[test]
    fn test_read_roles_empty() {
        assert!(read_roles(&test_cfg("")).is_err());
    }

    #[test]
    fn test_read_roles_invalid() {
        assert!(read_roles(&test_cfg("bogus_role")).is_err());
    }
}
