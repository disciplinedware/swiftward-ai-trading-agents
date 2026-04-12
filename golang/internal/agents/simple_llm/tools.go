package simple_llm

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"ai-trading-agents/internal/mcp"
)

// MCPToolset manages multiple MCP clients and their tool definitions.
// It discovers tools from each MCP server, converts them to OpenAI format,
// and routes tool calls to the correct client based on tool name prefix.
type MCPToolset struct {
	clients map[string]*mcp.Client // keyed by name: "trading", "memory"
	tools   []mcp.Tool             // all discovered tools across all clients
	// routes maps tool name prefix to client name.
	// e.g., "trade/" -> "trading", "market/" -> "trading", "memory/" -> "memory"
	routes map[string]string
	// openaiToMCP maps sanitized OpenAI function names back to original MCP tool names.
	// OpenAI requires ^[a-zA-Z0-9_-]+$ so slashes are replaced with double underscores.
	openaiToMCP map[string]string
}

// NewMCPToolset creates a new toolset with the given MCP clients.
// Routes map tool name prefixes to client names (e.g., "trade/" -> "trading").
func NewMCPToolset(clients map[string]*mcp.Client, routes map[string]string) *MCPToolset {
	return &MCPToolset{
		clients: clients,
		routes:  routes,
	}
}

// DiscoverTools calls tools/list on each MCP client and collects all tool definitions.
// Clients are iterated in sorted order to produce a deterministic tool list,
// which ensures consistent JSON serialization for LLM prompt caching.
func (ts *MCPToolset) DiscoverTools() error {
	ts.tools = nil

	names := make([]string, 0, len(ts.clients))
	for name := range ts.clients {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		tools, err := ts.clients[name].ListTools()
		if err != nil {
			return fmt.Errorf("list tools from %s: %w", name, err)
		}
		ts.tools = append(ts.tools, tools...)
	}
	return nil
}

// Tools returns the discovered MCP tool definitions.
func (ts *MCPToolset) Tools() []mcp.Tool {
	return ts.tools
}

// toOpenAIName converts an MCP tool name to a valid OpenAI function name.
// OpenAI requires function names to match ^[a-zA-Z0-9_-]+$ so slashes are replaced.
func toOpenAIName(mcpName string) string {
	return strings.ReplaceAll(mcpName, "/", "__")
}

// ToOpenAITools converts all discovered MCP tools to OpenAI function-calling format.
// Tool names are sanitized (slashes replaced with double underscores) because
// OpenAI only allows alphanumeric characters, underscores, and hyphens.
func (ts *MCPToolset) ToOpenAITools() []openai.Tool {
	ts.openaiToMCP = make(map[string]string, len(ts.tools))
	result := make([]openai.Tool, 0, len(ts.tools))
	for _, t := range ts.tools {
		params := t.InputSchema
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}

		openaiName := toOpenAIName(t.Name)
		ts.openaiToMCP[openaiName] = t.Name

		result = append(result, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        openaiName,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return result
}

// CallTool routes a tool call to the correct MCP client based on the tool name prefix
// and returns the result. The name can be either an OpenAI-sanitized name (with __)
// or the original MCP name (with /). It is resolved back to the MCP name before routing.
func (ts *MCPToolset) CallTool(name string, args map[string]any) (*mcp.ToolResult, error) {
	mcpName := ts.toMCPName(name)

	clientName, err := ts.resolveClient(mcpName)
	if err != nil {
		return nil, err
	}

	client, ok := ts.clients[clientName]
	if !ok {
		return nil, fmt.Errorf("client %q not found for tool %q", clientName, mcpName)
	}

	return client.CallTool(mcpName, args)
}

// CallToolJSON parses a JSON arguments string and routes the tool call.
func (ts *MCPToolset) CallToolJSON(name string, argsJSON string) (*mcp.ToolResult, error) {
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return nil, fmt.Errorf("parse tool arguments: %w", err)
		}
	}
	return ts.CallTool(name, args)
}

// toMCPName converts an OpenAI function name back to the original MCP tool name.
// Falls back to the input if no mapping is found.
func (ts *MCPToolset) toMCPName(openaiName string) string {
	if mcpName, ok := ts.openaiToMCP[openaiName]; ok {
		return mcpName
	}
	return openaiName
}

// Client returns the MCP client registered under the given name, or nil if not found.
func (ts *MCPToolset) Client(name string) *mcp.Client {
	return ts.clients[name]
}

// resolveClient finds the client name for a given tool name using prefix matching.
func (ts *MCPToolset) resolveClient(toolName string) (string, error) {
	for prefix, clientName := range ts.routes {
		if strings.HasPrefix(toolName, prefix) {
			return clientName, nil
		}
	}
	return "", fmt.Errorf("no MCP client registered for tool %q", toolName)
}
