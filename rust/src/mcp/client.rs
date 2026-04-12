use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use anyhow::{Context, bail};
use reqwest::Client;

use super::types::*;

/// MCP HTTP client - calls MCP servers over JSON-RPC 2.0.
/// Mirrors Go's mcp.Client.
pub struct McpClient {
    base_url: String,
    api_key: String,
    http: Client,
    next_id: AtomicU64,
    headers: HashMap<String, String>,
}

impl McpClient {
    pub fn new(base_url: &str, api_key: &str, timeout: Duration) -> Self {
        let http = Client::builder()
            .timeout(timeout)
            .build()
            .expect("failed to build HTTP client");

        let mut headers = HashMap::new();
        headers.insert("Accept".to_string(), "application/json".to_string());

        Self {
            base_url: base_url.to_string(),
            api_key: api_key.to_string(),
            http,
            next_id: AtomicU64::new(0),
            headers,
        }
    }

    /// Add a custom header to all requests.
    pub fn set_header(&mut self, key: &str, value: &str) {
        self.headers.insert(key.to_string(), value.to_string());
    }

    /// Send a JSON-RPC request and return the parsed response.
    async fn rpc_call(&self, method: &str, params: Option<serde_json::Value>) -> anyhow::Result<serde_json::Value> {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed) + 1;

        let req_body = JsonRpcRequest {
            jsonrpc: "2.0".to_string(),
            id,
            method: method.to_string(),
            params,
        };

        let mut builder = self.http.post(&self.base_url)
            .header("Content-Type", "application/json");

        if !self.api_key.is_empty() {
            builder = builder.header("Authorization", format!("Bearer {}", self.api_key));
        }
        for (k, v) in &self.headers {
            builder = builder.header(k, v);
        }

        let resp = builder
            .json(&req_body)
            .send()
            .await
            .context("http request")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            let truncated = if body.len() > 512 { &body[..512] } else { &body };
            bail!("MCP server returned HTTP {status}: {truncated}");
        }

        let rpc_resp: JsonRpcResponse = resp.json().await.context("decode response")?;

        if let Some(err) = rpc_resp.error {
            bail!("rpc error {}: {}", err.code, err.message);
        }

        rpc_resp.result.context("response has no result")
    }

    /// Invoke a tool on the MCP server.
    pub async fn call_tool(
        &self,
        tool_name: &str,
        args: Option<serde_json::Map<String, serde_json::Value>>,
    ) -> anyhow::Result<ToolResult> {
        let params = ToolCallParams {
            name: tool_name.to_string(),
            arguments: args,
        };

        let result = self
            .rpc_call("tools/call", Some(serde_json::to_value(&params)?))
            .await?;

        serde_json::from_value(result).context("unmarshal tool result")
    }

    /// List all tools available on the MCP server.
    pub async fn list_tools(&self) -> anyhow::Result<Vec<Tool>> {
        let result = self.rpc_call("tools/list", None).await?;
        let list: ToolsListResult = serde_json::from_value(result).context("unmarshal tools list")?;
        Ok(list.tools)
    }

    /// Send the MCP initialize handshake.
    pub async fn initialize(&self) -> anyhow::Result<InitializeResult> {
        let params = serde_json::json!({
            "protocolVersion": "2025-03-26",
            "capabilities": {},
            "clientInfo": {
                "name": "ai-trading-agent-rust",
                "version": "1.0.0"
            }
        });

        let result = self.rpc_call("initialize", Some(params)).await?;
        serde_json::from_value(result).context("unmarshal initialize result")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{header, method};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    async fn setup_mock() -> (MockServer, McpClient) {
        let server = MockServer::start().await;
        let mut client = McpClient::new(&server.uri(), "test-key", Duration::from_secs(5));
        client.set_header("X-Agent-ID", "test-agent");
        (server, client)
    }

    #[tokio::test]
    async fn test_call_tool_success() {
        let (server, client) = setup_mock().await;

        Mock::given(method("POST"))
            .and(header("Authorization", "Bearer test-key"))
            .and(header("X-Agent-ID", "test-agent"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [{"type": "text", "text": "trade submitted"}],
                    "isError": false
                }
            })))
            .mount(&server)
            .await;

        let result = client.call_tool("trade/submit_intent", None).await.unwrap();
        assert!(!result.is_error);
        assert_eq!(result.content.len(), 1);
        assert_eq!(result.content[0].text.as_deref(), Some("trade submitted"));
    }

    #[tokio::test]
    async fn test_call_tool_rpc_error() {
        let (server, client) = setup_mock().await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "error": {"code": -32600, "message": "invalid request"}
            })))
            .mount(&server)
            .await;

        let err = client.call_tool("trade/submit_intent", None).await.unwrap_err();
        assert!(err.to_string().contains("rpc error -32600"));
    }

    #[tokio::test]
    async fn test_call_tool_http_error() {
        let (server, client) = setup_mock().await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(500).set_body_string("internal error"))
            .mount(&server)
            .await;

        let err = client.call_tool("trade/submit_intent", None).await.unwrap_err();
        assert!(err.to_string().contains("HTTP 500"));
    }

    #[tokio::test]
    async fn test_list_tools() {
        let (server, client) = setup_mock().await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "tools": [
                        {"name": "trade/submit_intent", "description": "Submit a trade"},
                        {"name": "memory/read", "description": "Read memory"}
                    ]
                }
            })))
            .mount(&server)
            .await;

        let tools = client.list_tools().await.unwrap();
        assert_eq!(tools.len(), 2);
        assert_eq!(tools[0].name, "trade/submit_intent");
        assert_eq!(tools[1].name, "memory/read");
    }

    #[tokio::test]
    async fn test_initialize() {
        let (server, client) = setup_mock().await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "protocolVersion": "2025-03-26",
                    "capabilities": {"tools": {"listChanged": false}},
                    "serverInfo": {"name": "trading-mcp", "version": "1.0.0"}
                }
            })))
            .mount(&server)
            .await;

        let init = client.initialize().await.unwrap();
        assert_eq!(init.protocol_version, "2025-03-26");
        assert_eq!(init.server_info.name, "trading-mcp");
    }

    #[tokio::test]
    async fn test_call_tool_with_args() {
        let (server, client) = setup_mock().await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [{"type": "text", "text": "ok"}],
                    "isError": false
                }
            })))
            .mount(&server)
            .await;

        let mut args = serde_json::Map::new();
        args.insert("market".to_string(), serde_json::json!("ETH-USDC"));
        args.insert("size".to_string(), serde_json::json!(1.5));

        let result = client.call_tool("trade/submit_intent", Some(args)).await.unwrap();
        assert!(!result.is_error);
    }

    #[tokio::test]
    async fn test_no_api_key() {
        let server = MockServer::start().await;
        let client = McpClient::new(&server.uri(), "", Duration::from_secs(5));

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [],
                    "isError": false
                }
            })))
            .mount(&server)
            .await;

        // Should work without API key (no Authorization header sent)
        let result = client.call_tool("test/tool", None).await.unwrap();
        assert!(!result.is_error);
    }
}
