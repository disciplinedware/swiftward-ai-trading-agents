use std::collections::HashMap;
use std::time::{Duration, Instant};

use anyhow::Context;
use async_trait::async_trait;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio_util::sync::CancellationToken;
use tracing::{debug, error, info, warn};

use crate::config::LlmAgentConfig;
use crate::mcp::McpClient;

use super::tools::McpToolset;

// ---- Error types ----

/// Signals the agent completed its session and the process should exit.
#[derive(Debug, thiserror::Error)]
#[error("session done")]
pub struct SessionDoneError;

// ---- OpenAI types ----

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct ChatMessage {
    pub role: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub content: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<Vec<ToolCall>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tool_call_id: Option<String>,
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct ToolCall {
    pub id: String,
    #[serde(rename = "type")]
    pub call_type: String,
    pub function: FunctionCall,
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct FunctionCall {
    pub name: String,
    pub arguments: String,
}

#[derive(Debug, serde::Serialize)]
pub(crate) struct ChatCompletionRequest {
    model: String,
    messages: Vec<ChatMessage>,
    #[serde(skip_serializing_if = "Option::is_none")]
    tools: Option<Vec<serde_json::Value>>,
}

#[derive(Debug, serde::Deserialize)]
pub struct ChatCompletionResponse {
    choices: Vec<Choice>,
    #[serde(default)]
    usage: Option<TokenUsage>,
}

#[derive(Debug, serde::Deserialize, Default)]
pub struct TokenUsage {
    #[serde(default)]
    prompt_tokens: u32,
    #[serde(default)]
    completion_tokens: u32,
    #[serde(default)]
    total_tokens: u32,
}

#[derive(Debug, serde::Deserialize)]
pub(crate) struct Choice {
    message: ChatMessage,
    #[serde(default)]
    finish_reason: Option<String>,
}

// ---- ChatClient trait ----

/// Abstracts the OpenAI chat completion API for testing.
/// Mirrors Go's ChatClient interface.
#[async_trait]
pub trait ChatClient: Send + Sync {
    async fn create_chat_completion(
        &self,
        messages: Vec<ChatMessage>,
        model: &str,
        tools: Option<Vec<serde_json::Value>>,
    ) -> anyhow::Result<ChatCompletionResponse>;
}

/// Real OpenAI client - raw reqwest POST.
pub struct OpenAiClient {
    api_key: String,
    http: reqwest::Client,
}

impl OpenAiClient {
    pub fn new(api_key: &str) -> Self {
        Self {
            api_key: api_key.to_string(),
            http: reqwest::Client::new(),
        }
    }
}

#[async_trait]
impl ChatClient for OpenAiClient {
    async fn create_chat_completion(
        &self,
        messages: Vec<ChatMessage>,
        model: &str,
        tools: Option<Vec<serde_json::Value>>,
    ) -> anyhow::Result<ChatCompletionResponse> {
        let req = ChatCompletionRequest {
            model: model.to_string(),
            messages,
            tools,
        };

        let resp = self
            .http
            .post("https://api.openai.com/v1/chat/completions")
            .header("Authorization", format!("Bearer {}", self.api_key))
            .header("Content-Type", "application/json")
            .json(&req)
            .send()
            .await
            .context("openai http request")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            anyhow::bail!("OpenAI API returned HTTP {status}: {body}");
        }

        resp.json().await.context("decode openai response")
    }
}

type PromptLoaderFn = Box<dyn Fn(&str) -> anyhow::Result<String> + Send + Sync>;

// ---- LLM Agent Service ----

pub struct LlmAgentService {
    cfg: LlmAgentConfig,
    toolset: McpToolset,
    chat: Box<dyn ChatClient>,
    cancel: CancellationToken,
    prompt_loader: PromptLoaderFn,
    startup_delay: Duration,
}

impl LlmAgentService {
    pub fn new(cfg: LlmAgentConfig, cancel: CancellationToken) -> Self {
        let default_timeout = Duration::from_secs(10);
        // Code MCP needs a long timeout: first call starts a sandbox container
        // (docker pull + start + repl ready) which can take 30-60s, plus Python
        // execution itself can run up to 120s. Matches Go's default of 5m.
        let code_timeout = Duration::from_secs(300);

        let mut trading_client =
            McpClient::new(&cfg.trading_mcp_url, &cfg.api_key, default_timeout);
        trading_client.set_header("X-Agent-ID", &cfg.agent_id);

        let mut files_client =
            McpClient::new(&cfg.files_mcp_url, &cfg.api_key, default_timeout);
        files_client.set_header("X-Agent-ID", &cfg.agent_id);

        let mut market_client =
            McpClient::new(&cfg.market_data_mcp_url, &cfg.api_key, default_timeout);
        market_client.set_header("X-Agent-ID", &cfg.agent_id);

        let mut news_client =
            McpClient::new(&cfg.news_mcp_url, &cfg.api_key, default_timeout);
        news_client.set_header("X-Agent-ID", &cfg.agent_id);

        let mut code_client =
            McpClient::new(&cfg.code_mcp_url, &cfg.api_key, code_timeout);
        code_client.set_header("X-Agent-ID", &cfg.agent_id);

        let mut polymarket_client =
            McpClient::new(&cfg.polymarket_mcp_url, &cfg.api_key, default_timeout);
        polymarket_client.set_header("X-Agent-ID", &cfg.agent_id);

        let clients = HashMap::from([
            ("trading".to_string(), trading_client),
            ("files".to_string(), files_client),
            ("market".to_string(), market_client),
            ("news".to_string(), news_client),
            ("code".to_string(), code_client),
            ("polymarket".to_string(), polymarket_client),
        ]);
        let routes = HashMap::from([
            ("trade/".to_string(), "trading".to_string()),
            ("alert/".to_string(), "trading".to_string()),
            ("market/".to_string(), "market".to_string()),
            ("files/".to_string(), "files".to_string()),
            ("news/".to_string(), "news".to_string()),
            ("code/".to_string(), "code".to_string()),
            ("polymarket/".to_string(), "polymarket".to_string()),
        ]);

        let toolset = McpToolset::new(clients, routes);
        let chat = Box::new(OpenAiClient::new(&cfg.openai_api_key));

        Self {
            cfg,
            toolset,
            chat,
            cancel,
            prompt_loader: Box::new(|path| {
                let content = std::fs::read_to_string(path)
                    .map_err(|e| anyhow::anyhow!("load prompt file {path}: {e}"))?;
                Ok(content)
            }),
            startup_delay: Duration::from_secs(2),
        }
    }

    /// For testing: inject a mock chat client.
    #[cfg(test)]
    fn with_chat(mut self, chat: Box<dyn ChatClient>) -> Self {
        self.chat = chat;
        self
    }

    /// For testing: inject a custom prompt loader.
    #[cfg(test)]
    fn with_prompt_loader(
        mut self,
        loader: Box<dyn Fn(&str) -> anyhow::Result<String> + Send + Sync>,
    ) -> Self {
        self.prompt_loader = loader;
        self
    }

    /// For testing: inject a pre-built toolset.
    #[cfg(test)]
    fn with_toolset(mut self, toolset: McpToolset) -> Self {
        self.toolset = toolset;
        self
    }

    /// For testing: skip startup delay.
    #[cfg(test)]
    fn with_no_delay(mut self) -> Self {
        self.startup_delay = Duration::ZERO;
        self
    }

    /// Validate configuration.
    pub fn initialize(&self) -> anyhow::Result<()> {
        if self.cfg.openai_api_key.is_empty() {
            anyhow::bail!(
                "OPENAI_API_KEY is required for LLM agent (set TRADING__LLM_AGENT__OPENAI_API_KEY)"
            );
        }
        if self.cfg.model.is_empty() {
            anyhow::bail!(
                "MODEL is required for LLM agent (set TRADING__LLM_AGENT__MODEL or LLM_AGENT_MODEL)"
            );
        }
        info!(
            agent_id = %self.cfg.agent_id,
            model = %self.cfg.model,
            mode = %self.cfg.mode,
            max_iterations = self.cfg.max_iterations,
            prompt_file = %self.cfg.prompt_file,
            "LLM agent initialized"
        );
        Ok(())
    }

    /// Start the agent. Load prompt, discover tools, dispatch by mode.
    pub async fn start(&mut self) -> anyhow::Result<()> {
        // Load prompt file
        let prompt_text = (self.prompt_loader)(&self.cfg.prompt_file)?;
        let system_prompt = prompt_text.trim().to_string();
        if system_prompt.is_empty() {
            anyhow::bail!("prompt file {} is empty", self.cfg.prompt_file);
        }
        let system_prompt = format!(
            "{}\n\nCurrent date and time: {}",
            system_prompt,
            chrono_now_utc()
        );

        // Wait for MCP servers
        if !self.startup_delay.is_zero() {
            tokio::select! {
                () = tokio::time::sleep(self.startup_delay) => {}
                () = self.cancel.cancelled() => return Ok(()),
            }
        }

        // Discover tools
        self.toolset.discover_tools().await?;
        let tool_names: Vec<&str> = self.toolset.tools().iter().map(|t| t.name.as_str()).collect();
        info!(?tool_names, "Tools discovered");

        // Dispatch by mode
        match self.cfg.mode.as_str() {
            "once" => self.run_once(&system_prompt).await,
            "cli" => self.run_cli(&system_prompt).await,
            "server" => self.run_server(&system_prompt).await,
            other => anyhow::bail!("unknown mode {other:?} (valid: once, cli, server)"),
        }
    }

    /// Build session prompt with fresh memory context.
    async fn build_session_prompt(&self, base_prompt: &str) -> String {
        if base_prompt.contains("{{memory}}") {
            info!(phase = "init", placeholder = "{{memory}}", "Placeholder found, will prefetch");
        } else {
            debug!(phase = "init", placeholder = "{{memory}}", "Placeholder not in prompt, skipping");
        }
        let memory_context = self.load_memory_context().await;
        if !memory_context.is_empty() {
            info!(phase = "init", bytes = memory_context.len(), "Memory context loaded");
            format!("{base_prompt}\n\n{memory_context}")
        } else {
            base_prompt.to_string()
        }
    }

    /// Load memory context via Files MCP. Only pre-loads `memory/MEMORY.md` - the curated
    /// persistent memory. Session logs are write-only journals; the agent uses files/read
    /// to look back if needed. Mirrors Go's Service.loadMemoryContext.
    async fn load_memory_context(&self) -> String {
        let files_client = match self.toolset.client("files") {
            Some(c) => c,
            None => return String::new(),
        };

        info!(phase = "init", "Prefetching memory files");

        let mut parts = vec!["## Pre-loaded Memory\n*Loaded at session start.*".to_string()];

        let path = "memory/MEMORY.md";
        let content = read_file_content(files_client, path).await;
        parts.push("\n### memory/MEMORY.md".to_string());
        if content.is_empty() {
            parts.push(
                "No core memory yet. This is your first session - create memory/MEMORY.md at the end."
                    .to_string(),
            );
        } else {
            parts.push(content);
        }

        parts.join("\n")
    }

    // ---- Mode implementations ----

    async fn run_once(&mut self, base_prompt: &str) -> anyhow::Result<()> {
        info!("Running single session (once mode)");
        let session_prompt = self.build_session_prompt(base_prompt).await;
        let result = self
            .run_session(
                &session_prompt,
                "Execute your trading strategy now. Analyze the market and make decisions.",
            )
            .await?;
        println!("\n--- Agent Decision ---");
        println!("{result}");
        Err(SessionDoneError.into())
    }

    async fn run_cli(&mut self, base_prompt: &str) -> anyhow::Result<()> {
        info!("Starting CLI mode (type input, Ctrl-D to exit)");
        let stdin = tokio::io::stdin();
        let reader = BufReader::new(stdin);
        let mut lines = reader.lines();

        loop {
            eprint!("> ");
            let line = tokio::select! {
                line = lines.next_line() => line?,
                () = self.cancel.cancelled() => return Ok(()),
            };

            let Some(line) = line else { break }; // EOF
            let input = line.trim().to_string();
            if input.is_empty() {
                continue;
            }

            let session_prompt = self.build_session_prompt(base_prompt).await;
            match self.run_session(&session_prompt, &input).await {
                Ok(result) => {
                    println!("\n--- Agent Decision ---");
                    println!("{result}");
                }
                Err(e) => {
                    error!(error = %e, "Session failed");
                    eprintln!("Error: {e}");
                }
            }
        }
        Ok(())
    }

    async fn run_server(&mut self, base_prompt: &str) -> anyhow::Result<()> {
        let interval = parse_duration(&self.cfg.interval).unwrap_or_else(|e| {
            warn!(
                configured = %self.cfg.interval,
                error = %e,
                "Invalid interval, using 5m default"
            );
            Duration::from_secs(300)
        });

        info!(?interval, "Starting server mode");

        let mut ticker = tokio::time::interval(interval);

        // First tick fires immediately
        loop {
            tokio::select! {
                _ = ticker.tick() => {
                    self.run_server_tick(base_prompt).await;
                }
                () = self.cancel.cancelled() => return Ok(()),
            }
        }
    }

    async fn run_server_tick(&mut self, base_prompt: &str) {
        info!("Server tick - starting session");
        let session_prompt = self.build_session_prompt(base_prompt).await;
        match self
            .run_session(
                &session_prompt,
                "Execute your trading strategy now. Analyze the market and make decisions.",
            )
            .await
        {
            Ok(result) => {
                info!(result = %result, "Session completed");
            }
            Err(e) => {
                error!(error = %e, "Session failed");
            }
        }
    }

    // ---- Core tool-calling loop ----

    async fn run_session(
        &mut self,
        system_prompt: &str,
        user_message: &str,
    ) -> anyhow::Result<String> {
        let mut messages = vec![
            ChatMessage {
                role: "system".to_string(),
                content: Some(system_prompt.to_string()),
                tool_calls: None,
                tool_call_id: None,
            },
            ChatMessage {
                role: "user".to_string(),
                content: Some(user_message.to_string()),
                tool_calls: None,
                tool_call_id: None,
            },
        ];

        let tools = self.toolset.to_openai_tools();
        let tools_param = if tools.is_empty() { None } else { Some(tools) };

        for i in 0..self.cfg.max_iterations {
            // Check cancellation
            if self.cancel.is_cancelled() {
                anyhow::bail!("cancelled");
            }

            // Log the full request payload.
            let req_json = serde_json::to_string(&messages).unwrap_or_default();
            info!(
                phase = "llm",
                iteration = i + 1,
                model = %self.cfg.model,
                tools = tools_param.as_ref().map(|t| t.len()).unwrap_or(0),
                messages = %req_json,
                "LLM request"
            );

            let call_start = Instant::now();
            let resp = self
                .chat
                .create_chat_completion(messages.clone(), &self.cfg.model, tools_param.clone())
                .await
                .map_err(|e| anyhow::anyhow!("openai chat completion (iteration {}): {e}", i + 1))?;
            let duration_ms = call_start.elapsed().as_millis();

            if resp.choices.is_empty() {
                anyhow::bail!("openai returned no choices (iteration {})", i + 1);
            }

            let choice = &resp.choices[0];
            let msg = &choice.message;

            // Log token usage + timing (mirrors swiftward-core LLM gateway logging).
            let usage = resp.usage.as_ref();
            info!(
                phase = "llm",
                iteration = i + 1,
                duration_ms = duration_ms,
                prompt_tokens = usage.map(|u| u.prompt_tokens).unwrap_or(0),
                completion_tokens = usage.map(|u| u.completion_tokens).unwrap_or(0),
                total_tokens = usage.map(|u| u.total_tokens).unwrap_or(0),
                finish_reason = choice.finish_reason.as_deref().unwrap_or(""),
                "LLM token usage"
            );

            // Log the full response message.
            let resp_json = serde_json::to_string(msg).unwrap_or_default();
            info!(phase = "llm", iteration = i + 1, message = %resp_json, "LLM response");

            // No tool calls - final decision
            let tool_calls = msg.tool_calls.as_ref();
            if tool_calls.is_none() || tool_calls.is_some_and(|tc| tc.is_empty()) {
                let content = msg.content.clone().unwrap_or_default();
                return Ok(content);
            }

            // LLM thought
            if let Some(content) = &msg.content {
                if !content.is_empty() {
                    info!(content = %content, "LLM thought");
                }
            }

            // Append assistant message with tool calls
            messages.push(msg.clone());

            // Execute each tool call
            let tool_calls = tool_calls.unwrap();
            for tc in tool_calls {
                let tool_name = &tc.function.name;
                let tool_args = &tc.function.arguments;

                info!(phase = "llm", tool = %tool_name, args = %tool_args, "Tool call");

                let result_text = match self.toolset.call_tool_json(tool_name, tool_args).await {
                    Err(e) => {
                        let text = format!("error: {e}");
                        error!(phase = "llm", tool = %tool_name, error = %text, "Tool error");
                        text
                    }
                    Ok(result) if result.is_error && !result.content.is_empty() => {
                        let text = result.content[0]
                            .text
                            .clone()
                            .unwrap_or_else(|| "tool returned error with no details".to_string());
                        warn!(phase = "llm", tool = %tool_name, reason = %text, "Tool rejected");
                        text
                    }
                    Ok(result) if result.is_error => {
                        let text = "tool returned error with no details".to_string();
                        warn!(phase = "llm", tool = %tool_name, reason = %text, "Tool rejected");
                        text
                    }
                    Ok(result) if !result.content.is_empty() => {
                        let text = result.content[0]
                            .text
                            .clone()
                            .unwrap_or_else(|| "success".to_string());
                        info!(phase = "llm", tool = %tool_name, result = %text, "Tool result");
                        text
                    }
                    Ok(_) => {
                        let text = "success".to_string();
                        info!(phase = "llm", tool = %tool_name, result = %text, "Tool result");
                        text
                    }
                };

                messages.push(ChatMessage {
                    role: "tool".to_string(),
                    content: Some(result_text),
                    tool_calls: None,
                    tool_call_id: Some(tc.id.clone()),
                });
            }
        }

        // Max iterations reached
        warn!(max = self.cfg.max_iterations, "Max iterations reached");
        let last_content = messages
            .iter()
            .rev()
            .find(|m| m.role == "assistant" && m.content.as_ref().is_some_and(|c| !c.is_empty()))
            .and_then(|m| m.content.clone())
            .unwrap_or_default();

        Ok(last_content)
    }
}

/// Read a file via Files MCP. Returns empty string on any error
/// (unreachable server, missing file, parse failure) so callers can fall back cleanly.
async fn read_file_content(client: &McpClient, path: &str) -> String {
    let mut args = serde_json::Map::new();
    args.insert("path".to_string(), serde_json::json!(path));

    info!(phase = "init", path, "Prefetch files/read");
    let result = match client.call_tool("files/read", Some(args)).await {
        Ok(r) => r,
        Err(e) => {
            warn!(phase = "init", path, error = %e, "files MCP unreachable, skipping");
            return String::new();
        }
    };

    if result.is_error || result.content.is_empty() {
        info!(phase = "init", path, "memory file not found (will be created this session)");
        return String::new();
    }

    let text = match &result.content[0].text {
        Some(t) => t,
        None => return String::new(),
    };

    // files/read wraps response in JSON: {"content":"...","total_lines":...,"truncated":...}
    #[derive(serde::Deserialize)]
    struct FilesReadPayload {
        content: String,
    }

    match serde_json::from_str::<FilesReadPayload>(text) {
        Ok(payload) => {
            // Trim trailing newlines so the content shown in the prompt matches the file exactly.
            // strings.Join adds "\n" between parts, which would make the file appear to end with
            // a newline it may not have - causing old_text mismatches in files/edit.
            let trimmed = payload.content.trim_end_matches('\n').to_string();
            info!(phase = "init", path, bytes = trimmed.len(), "files/read ok");
            trimmed
        }
        Err(e) => {
            warn!(phase = "init", path, error = %e, "files/read parse failed");
            String::new()
        }
    }
}

/// Parse a Go-style duration string like "5m", "30s", "1h".
fn parse_duration(s: &str) -> anyhow::Result<Duration> {
    let s = s.trim();
    if s.is_empty() {
        anyhow::bail!("empty duration");
    }

    let (num_str, unit) = if let Some(n) = s.strip_suffix("ms") {
        (n, "ms")
    } else if let Some(n) = s.strip_suffix('s') {
        (n, "s")
    } else if let Some(n) = s.strip_suffix('m') {
        (n, "m")
    } else if let Some(n) = s.strip_suffix('h') {
        (n, "h")
    } else {
        anyhow::bail!("unknown duration unit in {s:?}");
    };

    let num: u64 = num_str.parse().map_err(|e| anyhow::anyhow!("parse duration {s:?}: {e}"))?;

    match unit {
        "ms" => Ok(Duration::from_millis(num)),
        "s" => Ok(Duration::from_secs(num)),
        "m" => Ok(Duration::from_secs(num * 60)),
        "h" => Ok(Duration::from_secs(num * 3600)),
        _ => unreachable!(),
    }
}

/// Get current UTC time formatted like Go's "2006-01-02 15:04 UTC".
fn chrono_now_utc() -> String {
    // Use std time instead of adding chrono dependency
    let now = std::time::SystemTime::now();
    let secs = now
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs();

    // Simple UTC formatting without chrono
    let days = secs / 86400;
    let time_of_day = secs % 86400;
    let hours = time_of_day / 3600;
    let minutes = (time_of_day % 3600) / 60;

    // Days since Unix epoch to Y-M-D (simplified civil calendar)
    let (year, month, day) = days_to_date(days as i64);

    format!("{year:04}-{month:02}-{day:02} {hours:02}:{minutes:02} UTC")
}

/// Convert days since Unix epoch to (year, month, day).
fn days_to_date(days: i64) -> (i64, u32, u32) {
    // Algorithm from Howard Hinnant
    let z = days + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    (y, m, d)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_duration() {
        assert_eq!(parse_duration("5m").unwrap(), Duration::from_secs(300));
        assert_eq!(parse_duration("30s").unwrap(), Duration::from_secs(30));
        assert_eq!(parse_duration("1h").unwrap(), Duration::from_secs(3600));
        assert_eq!(parse_duration("500ms").unwrap(), Duration::from_millis(500));
        assert!(parse_duration("").is_err());
        assert!(parse_duration("5x").is_err());
    }

    #[test]
    fn test_chrono_now_utc_format() {
        let s = chrono_now_utc();
        // Should match pattern: "YYYY-MM-DD HH:MM UTC"
        assert!(s.ends_with(" UTC"), "got: {s}");
        assert_eq!(s.len(), 20, "got: {s}");
    }

    // ---- Mock ChatClient for testing ----

    struct MockChat {
        responses: std::sync::Mutex<Vec<ChatCompletionResponse>>,
    }

    impl MockChat {
        fn new(responses: Vec<ChatCompletionResponse>) -> Self {
            Self {
                responses: std::sync::Mutex::new(responses),
            }
        }
    }

    #[async_trait]
    impl ChatClient for MockChat {
        async fn create_chat_completion(
            &self,
            _messages: Vec<ChatMessage>,
            _model: &str,
            _tools: Option<Vec<serde_json::Value>>,
        ) -> anyhow::Result<ChatCompletionResponse> {
            let mut responses = self.responses.lock().unwrap();
            if responses.is_empty() {
                anyhow::bail!("no more mock responses");
            }
            Ok(responses.remove(0))
        }
    }

    fn make_text_response(text: &str) -> ChatCompletionResponse {
        ChatCompletionResponse {
            choices: vec![Choice {
                message: ChatMessage {
                    role: "assistant".to_string(),
                    content: Some(text.to_string()),
                    tool_calls: None,
                    tool_call_id: None,
                },
                finish_reason: Some("stop".to_string()),
            }],
            usage: None,
        }
    }

    fn make_tool_call_response(calls: Vec<(&str, &str)>) -> ChatCompletionResponse {
        ChatCompletionResponse {
            choices: vec![Choice {
                message: ChatMessage {
                    role: "assistant".to_string(),
                    content: None,
                    tool_calls: Some(
                        calls
                            .into_iter()
                            .enumerate()
                            .map(|(i, (name, args))| ToolCall {
                                id: format!("call_{i}"),
                                call_type: "function".to_string(),
                                function: FunctionCall {
                                    name: name.to_string(),
                                    arguments: args.to_string(),
                                },
                            })
                            .collect(),
                    ),
                    tool_call_id: None,
                },
                finish_reason: Some("tool_calls".to_string()),
            }],
            usage: None,
        }
    }

    fn test_config() -> LlmAgentConfig {
        LlmAgentConfig {
            agent_id: "test-agent".to_string(),
            api_key: "test-key".to_string(),
            trading_mcp_url: "http://localhost:8091/mcp/trading".to_string(),
            files_mcp_url: "http://localhost:8091/mcp/files".to_string(),
            market_data_mcp_url: "http://localhost:8091/mcp/market".to_string(),
            news_mcp_url: "http://localhost:8091/mcp/news".to_string(),
            code_mcp_url: "http://localhost:8091/mcp/code".to_string(),
            polymarket_mcp_url: "http://localhost:8091/mcp/polymarket".to_string(),
            openai_api_key: "test-openai-key".to_string(),
            model: "gpt-4o".to_string(),
            max_iterations: 10,
            prompt_file: "test.md".to_string(),
            mode: "once".to_string(),
            interval: "5m".to_string(),
        }
    }

    #[tokio::test]
    async fn test_run_session_direct_text() {
        let cancel = CancellationToken::new();
        let mock = MockChat::new(vec![make_text_response("I decided to hold.")]);

        let toolset = McpToolset::new(HashMap::new(), HashMap::new());

        let mut svc = LlmAgentService::new(test_config(), cancel)
            .with_chat(Box::new(mock))
            .with_toolset(toolset)
            .with_no_delay();

        let result = svc
            .run_session("You are a trader", "Trade now")
            .await
            .unwrap();

        assert_eq!(result, "I decided to hold.");
    }

    #[tokio::test]
    async fn test_run_session_with_tool_calls() {
        let cancel = CancellationToken::new();
        let trading_server = wiremock::MockServer::start().await;

        // Mock trading MCP response
        wiremock::Mock::given(wiremock::matchers::method("POST"))
            .respond_with(wiremock::ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [{"type": "text", "text": "{\"portfolio\": {\"balance\": 10000}}"}],
                    "isError": false
                }
            })))
            .mount(&trading_server)
            .await;

        // First response: tool call, then text
        let mock = MockChat::new(vec![
            make_tool_call_response(vec![("trade__get_portfolio", "{}")]),
            make_text_response("Portfolio looks good, holding."),
        ]);

        let clients = HashMap::from([(
            "trading".to_string(),
            McpClient::new(&trading_server.uri(), "", Duration::from_secs(5)),
        )]);
        let routes = HashMap::from([("trade/".to_string(), "trading".to_string())]);
        let mut toolset = McpToolset::new(clients, routes);
        // Pre-populate the openai_to_mcp mapping
        toolset.to_openai_tools(); // won't have tools, but that's ok
        // Manually add mapping for test
        let tools_field = &mut toolset;
        // We need to manually add since discover wasn't called with real server
        // Instead, let's just use the MCP name directly
        let mock_chat = MockChat::new(vec![
            make_tool_call_response(vec![("trade/get_portfolio", "{}")]),
            make_text_response("Portfolio looks good, holding."),
        ]);

        let mut svc = LlmAgentService::new(test_config(), cancel)
            .with_chat(Box::new(mock_chat))
            .with_toolset(McpToolset::new(
                HashMap::from([(
                    "trading".to_string(),
                    McpClient::new(&trading_server.uri(), "", Duration::from_secs(5)),
                )]),
                HashMap::from([("trade/".to_string(), "trading".to_string())]),
            ))
            .with_no_delay();

        let result = svc
            .run_session("You are a trader", "Trade now")
            .await
            .unwrap();

        assert_eq!(result, "Portfolio looks good, holding.");
    }

    #[tokio::test]
    async fn test_run_session_max_iterations() {
        let cancel = CancellationToken::new();
        let server = wiremock::MockServer::start().await;

        wiremock::Mock::given(wiremock::matchers::method("POST"))
            .respond_with(wiremock::ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [{"type": "text", "text": "ok"}],
                    "isError": false
                }
            })))
            .mount(&server)
            .await;

        // Always return tool calls - will hit max iterations
        let mut responses = Vec::new();
        for _ in 0..5 {
            responses.push(make_tool_call_response(vec![(
                "trade/get_portfolio",
                "{}",
            )]));
        }
        let mock = MockChat::new(responses);

        let mut cfg = test_config();
        cfg.max_iterations = 3; // Low limit

        let mut svc = LlmAgentService::new(cfg, cancel)
            .with_chat(Box::new(mock))
            .with_toolset(McpToolset::new(
                HashMap::from([(
                    "trading".to_string(),
                    McpClient::new(&server.uri(), "", Duration::from_secs(5)),
                )]),
                HashMap::from([("trade/".to_string(), "trading".to_string())]),
            ))
            .with_no_delay();

        let result = svc
            .run_session("You are a trader", "Trade now")
            .await
            .unwrap();

        // No assistant text was ever returned, so empty
        assert_eq!(result, "");
    }

    #[tokio::test]
    async fn test_run_session_tool_error() {
        let cancel = CancellationToken::new();

        // No mock server - calls will fail with connection error
        let mock = MockChat::new(vec![
            make_tool_call_response(vec![("trade/get_portfolio", "{}")]),
            make_text_response("Tool failed, holding position."),
        ]);

        let mut svc = LlmAgentService::new(test_config(), cancel)
            .with_chat(Box::new(mock))
            .with_toolset(McpToolset::new(
                HashMap::from([(
                    "trading".to_string(),
                    // Point to nonexistent server
                    McpClient::new("http://127.0.0.1:1", "", Duration::from_secs(1)),
                )]),
                HashMap::from([("trade/".to_string(), "trading".to_string())]),
            ))
            .with_no_delay();

        let result = svc
            .run_session("You are a trader", "Trade now")
            .await
            .unwrap();

        assert_eq!(result, "Tool failed, holding position.");
    }

    #[test]
    fn test_initialize_missing_key() {
        let cancel = CancellationToken::new();
        let mut cfg = test_config();
        cfg.openai_api_key = String::new();

        let svc = LlmAgentService::new(cfg, cancel);
        assert!(svc.initialize().is_err());
    }

    #[test]
    fn test_initialize_success() {
        let cancel = CancellationToken::new();
        let svc = LlmAgentService::new(test_config(), cancel);
        assert!(svc.initialize().is_ok());
    }
}
