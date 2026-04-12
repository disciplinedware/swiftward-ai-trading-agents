use std::collections::HashMap;

use anyhow::bail;

use crate::mcp::{McpClient, Tool, ToolResult};

/// Manages multiple MCP clients and routes tool calls by prefix.
/// Mirrors Go's MCPToolset.
pub struct McpToolset {
    clients: HashMap<String, McpClient>,
    tools: Vec<Tool>,
    /// Tool name prefix -> client name. e.g. "trade/" -> "trading"
    routes: HashMap<String, String>,
    /// OpenAI-sanitized name -> original MCP name. e.g. "trade__submit_intent" -> "trade/submit_intent"
    openai_to_mcp: HashMap<String, String>,
}

impl McpToolset {
    pub fn new(clients: HashMap<String, McpClient>, routes: HashMap<String, String>) -> Self {
        Self {
            clients,
            tools: Vec::new(),
            routes,
            openai_to_mcp: HashMap::new(),
        }
    }

    /// Call list_tools on each MCP client and collect all tool definitions.
    pub async fn discover_tools(&mut self) -> anyhow::Result<()> {
        self.tools.clear();
        for (name, client) in &self.clients {
            let tools = client
                .list_tools()
                .await
                .map_err(|e| anyhow::anyhow!("list tools from {name}: {e}"))?;
            self.tools.extend(tools);
        }
        Ok(())
    }

    /// Get all discovered tools.
    pub fn tools(&self) -> &[Tool] {
        &self.tools
    }

    /// Convert MCP tools to OpenAI function-calling format.
    /// Returns JSON array suitable for the `tools` field in ChatCompletionRequest.
    pub fn to_openai_tools(&mut self) -> Vec<serde_json::Value> {
        self.openai_to_mcp.clear();
        let mut result = Vec::with_capacity(self.tools.len());

        for t in &self.tools {
            let schema = t.input_schema.clone().unwrap_or_else(|| {
                serde_json::json!({"type": "object", "properties": {}})
            });

            let openai_name = to_openai_name(&t.name);
            self.openai_to_mcp
                .insert(openai_name.clone(), t.name.clone());

            result.push(serde_json::json!({
                "type": "function",
                "function": {
                    "name": openai_name,
                    "description": t.description.as_deref().unwrap_or(""),
                    "parameters": schema,
                }
            }));
        }

        result
    }

    /// Route a tool call to the correct MCP client by prefix.
    pub async fn call_tool(
        &self,
        name: &str,
        args: Option<serde_json::Map<String, serde_json::Value>>,
    ) -> anyhow::Result<ToolResult> {
        let mcp_name = self.to_mcp_name(name);
        let client_name = self.resolve_client(&mcp_name)?;

        let client = self
            .clients
            .get(&client_name)
            .ok_or_else(|| anyhow::anyhow!("client {client_name:?} not found for tool {mcp_name:?}"))?;

        client.call_tool(&mcp_name, args).await
    }

    /// Parse JSON args string and route the tool call.
    pub async fn call_tool_json(
        &self,
        name: &str,
        args_json: &str,
    ) -> anyhow::Result<ToolResult> {
        let args = if args_json.is_empty() {
            None
        } else {
            let map: serde_json::Map<String, serde_json::Value> =
                serde_json::from_str(args_json)
                    .map_err(|e| anyhow::anyhow!("parse tool arguments: {e}"))?;
            Some(map)
        };
        self.call_tool(name, args).await
    }

    /// Get a client by name (e.g. "files").
    pub fn client(&self, name: &str) -> Option<&McpClient> {
        self.clients.get(name)
    }

    fn to_mcp_name(&self, openai_name: &str) -> String {
        self.openai_to_mcp
            .get(openai_name)
            .cloned()
            .unwrap_or_else(|| openai_name.to_string())
    }

    fn resolve_client(&self, tool_name: &str) -> anyhow::Result<String> {
        for (prefix, client_name) in &self.routes {
            if tool_name.starts_with(prefix) {
                return Ok(client_name.clone());
            }
        }
        bail!("no MCP client registered for tool {tool_name:?}");
    }
}

/// Convert MCP name to OpenAI-safe name: "/" -> "__"
fn to_openai_name(mcp_name: &str) -> String {
    mcp_name.replace('/', "__")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::mcp::Tool;
    use std::time::Duration;
    use wiremock::matchers::method;
    use wiremock::{Mock, MockServer, ResponseTemplate};

    #[test]
    fn test_to_openai_name() {
        assert_eq!(to_openai_name("trade/submit_intent"), "trade__submit_intent");
        assert_eq!(to_openai_name("memory/read"), "memory__read");
        assert_eq!(to_openai_name("no_slash"), "no_slash");
    }

    #[test]
    fn test_resolve_client() {
        let routes = HashMap::from([
            ("trade/".to_string(), "trading".to_string()),
            ("market/".to_string(), "trading".to_string()),
            ("memory/".to_string(), "memory".to_string()),
        ]);

        let ts = McpToolset::new(HashMap::new(), routes);

        assert_eq!(ts.resolve_client("trade/submit_intent").unwrap(), "trading");
        assert_eq!(ts.resolve_client("market/get_prices").unwrap(), "trading");
        assert_eq!(ts.resolve_client("memory/read").unwrap(), "memory");
        assert!(ts.resolve_client("unknown/tool").is_err());
    }

    #[test]
    fn test_to_openai_tools() {
        let tools = vec![
            Tool {
                name: "trade/submit_intent".to_string(),
                description: Some("Submit a trade".to_string()),
                input_schema: Some(serde_json::json!({
                    "type": "object",
                    "properties": {
                        "market": {"type": "string"}
                    }
                })),
            },
            Tool {
                name: "memory/read".to_string(),
                description: Some("Read memory".to_string()),
                input_schema: None,
            },
        ];

        let mut ts = McpToolset::new(HashMap::new(), HashMap::new());
        ts.tools = tools;

        let openai_tools = ts.to_openai_tools();
        assert_eq!(openai_tools.len(), 2);

        // Check first tool
        let first = &openai_tools[0];
        assert_eq!(first["type"], "function");
        assert_eq!(first["function"]["name"], "trade__submit_intent");
        assert_eq!(first["function"]["description"], "Submit a trade");

        // Check name mapping
        assert_eq!(
            ts.to_mcp_name("trade__submit_intent"),
            "trade/submit_intent"
        );
        assert_eq!(ts.to_mcp_name("memory__read"), "memory/read");

        // Fallback for unknown names
        assert_eq!(ts.to_mcp_name("direct/name"), "direct/name");
    }

    #[tokio::test]
    async fn test_discover_tools() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "tools": [
                        {"name": "trade/submit_intent", "description": "Submit"},
                        {"name": "trade/get_portfolio", "description": "Portfolio"}
                    ]
                }
            })))
            .mount(&server)
            .await;

        let client = McpClient::new(&server.uri(), "", Duration::from_secs(5));
        let clients = HashMap::from([("trading".to_string(), client)]);
        let routes = HashMap::from([("trade/".to_string(), "trading".to_string())]);

        let mut ts = McpToolset::new(clients, routes);
        ts.discover_tools().await.unwrap();

        assert_eq!(ts.tools().len(), 2);
        assert_eq!(ts.tools()[0].name, "trade/submit_intent");
    }

    #[tokio::test]
    async fn test_call_tool_routing() {
        let trading_server = MockServer::start().await;
        let memory_server = MockServer::start().await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [{"type": "text", "text": "traded"}],
                    "isError": false
                }
            })))
            .mount(&trading_server)
            .await;

        Mock::given(method("POST"))
            .respond_with(ResponseTemplate::new(200).set_body_json(serde_json::json!({
                "jsonrpc": "2.0",
                "id": 1,
                "result": {
                    "content": [{"type": "text", "text": "remembered"}],
                    "isError": false
                }
            })))
            .mount(&memory_server)
            .await;

        let clients = HashMap::from([
            ("trading".to_string(), McpClient::new(&trading_server.uri(), "", Duration::from_secs(5))),
            ("memory".to_string(), McpClient::new(&memory_server.uri(), "", Duration::from_secs(5))),
        ]);
        let routes = HashMap::from([
            ("trade/".to_string(), "trading".to_string()),
            ("memory/".to_string(), "memory".to_string()),
        ]);

        let mut ts = McpToolset::new(clients, routes);
        // Set up openai_to_mcp mapping
        ts.openai_to_mcp.insert("trade__submit_intent".to_string(), "trade/submit_intent".to_string());
        ts.openai_to_mcp.insert("memory__read".to_string(), "memory/read".to_string());

        let result = ts.call_tool("trade__submit_intent", None).await.unwrap();
        assert_eq!(result.content[0].text.as_deref(), Some("traded"));

        let result = ts.call_tool("memory__read", None).await.unwrap();
        assert_eq!(result.content[0].text.as_deref(), Some("remembered"));
    }
}
