package simple_llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"ai-trading-agents/internal/mcp"
)

// fakeMCPServer returns an httptest.Server that handles tools/list and tools/call.
func fakeMCPServer(t *testing.T, tools []mcp.Tool, callHandler func(name string, args map[string]any) *mcp.ToolResult) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeRPCError(w, nil, -32700, "parse error")
			return
		}

		switch req.Method {
		case "tools/list":
			writeRPCResult(w, req.ID, map[string]any{"tools": tools})
		case "tools/call":
			paramsBytes, _ := json.Marshal(req.Params)
			var params mcp.ToolCallParams
			_ = json.Unmarshal(paramsBytes, &params)
			if callHandler != nil {
				result := callHandler(params.Name, params.Arguments)
				writeRPCResult(w, req.ID, result)
			} else {
				writeRPCResult(w, req.ID, &mcp.ToolResult{
					Content: []mcp.Content{{Type: "text", Text: "ok"}},
				})
			}
		default:
			writeRPCError(w, req.ID, -32601, "method not found")
		}
	}))
}

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcp.JSONRPCError{Code: code, Message: msg},
	})
}

func TestDiscoverTools(t *testing.T) {
	tradingTools := []mcp.Tool{
		{Name: "trade/submit_order", Description: "Submit a trade order", InputSchema: map[string]any{"type": "object"}},
		{Name: "trade/get_portfolio", Description: "Get portfolio"},
	}
	memoryTools := []mcp.Tool{
		{Name: "memory/read", Description: "Read memory", InputSchema: map[string]any{"type": "object"}},
	}

	tradingSrv := fakeMCPServer(t, tradingTools, nil)
	defer tradingSrv.Close()
	memorySrv := fakeMCPServer(t, memoryTools, nil)
	defer memorySrv.Close()

	ts := NewMCPToolset(
		map[string]*mcp.Client{
			"trading": mcp.NewClient(tradingSrv.URL, "", 0),
			"memory":  mcp.NewClient(memorySrv.URL, "", 0),
		},
		map[string]string{
			"trade/":  "trading",
			"market/": "trading",
			"memory/": "memory",
		},
	)

	if err := ts.DiscoverTools(); err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}

	if got := len(ts.Tools()); got != 3 {
		t.Errorf("expected 3 tools, got %d", got)
	}
}

func TestToOpenAITools(t *testing.T) {
	tests := []struct {
		name      string
		mcpTools  []mcp.Tool
		wantCount int
		check     func(t *testing.T, tools []openai.Tool)
	}{
		{
			name:      "empty tools",
			mcpTools:  nil,
			wantCount: 0,
		},
		{
			name: "converts name, description, and schema",
			mcpTools: []mcp.Tool{
				{
					Name:        "trade/submit_order",
					Description: "Submit a trade order",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"pair": map[string]any{"type": "string"},
							"side": map[string]any{"type": "string"},
						},
						"required": []any{"pair", "side"},
					},
				},
			},
			wantCount: 1,
			check: func(t *testing.T, tools []openai.Tool) {
				tool := tools[0]
				if tool.Type != openai.ToolTypeFunction {
					t.Errorf("expected type %q, got %q", openai.ToolTypeFunction, tool.Type)
				}
				// OpenAI name has slashes replaced with double underscores.
				if tool.Function.Name != "trade__submit_order" {
					t.Errorf("expected name %q, got %q", "trade__submit_order", tool.Function.Name)
				}
				if tool.Function.Description != "Submit a trade order" {
					t.Errorf("expected description %q, got %q", "Submit a trade order", tool.Function.Description)
				}
				// Parameters should be non-nil
				if tool.Function.Parameters == nil {
					t.Error("expected non-nil parameters")
				}
			},
		},
		{
			name: "nil input schema gets default object",
			mcpTools: []mcp.Tool{
				{Name: "memory/list", Description: "List memory files"},
			},
			wantCount: 1,
			check: func(t *testing.T, tools []openai.Tool) {
				params, ok := tools[0].Function.Parameters.(map[string]any)
				if !ok {
					t.Fatalf("expected map[string]any, got %T", tools[0].Function.Parameters)
				}
				if params["type"] != "object" {
					t.Errorf("expected type=object, got %v", params["type"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &MCPToolset{tools: tt.mcpTools}
			result := ts.ToOpenAITools()
			if len(result) != tt.wantCount {
				t.Fatalf("expected %d tools, got %d", tt.wantCount, len(result))
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestCallToolRouting(t *testing.T) {
	var calledServer string
	var calledTool string

	tradingSrv := fakeMCPServer(t, nil, func(name string, _ map[string]any) *mcp.ToolResult {
		calledServer = "trading"
		calledTool = name
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: `{"status":"ok"}`}}}
	})
	defer tradingSrv.Close()

	memorySrv := fakeMCPServer(t, nil, func(name string, _ map[string]any) *mcp.ToolResult {
		calledServer = "memory"
		calledTool = name
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: `{"data":"hello"}`}}}
	})
	defer memorySrv.Close()

	ts := NewMCPToolset(
		map[string]*mcp.Client{
			"trading": mcp.NewClient(tradingSrv.URL, "", 0),
			"memory":  mcp.NewClient(memorySrv.URL, "", 0),
		},
		map[string]string{
			"trade/":  "trading",
			"market/": "trading",
			"memory/": "memory",
		},
	)

	tests := []struct {
		name       string
		toolName   string
		args       map[string]any
		wantServer string
		wantTool   string
		wantErr    bool
	}{
		{
			name:       "trade routes to trading",
			toolName:   "trade/submit_order",
			args:       map[string]any{"pair": "ETH-USDC"},
			wantServer: "trading",
			wantTool:   "trade/submit_order",
		},
		{
			name:       "market routes to trading",
			toolName:   "market/get_prices",
			args:       map[string]any{},
			wantServer: "trading",
			wantTool:   "market/get_prices",
		},
		{
			name:       "memory routes to memory",
			toolName:   "memory/read",
			args:       map[string]any{"path": "analysis.md"},
			wantServer: "memory",
			wantTool:   "memory/read",
		},
		{
			name:     "unknown prefix returns error",
			toolName: "news/search",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calledServer = ""
			calledTool = ""

			result, err := ts.CallTool(tt.toolName, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if calledServer != tt.wantServer {
				t.Errorf("expected server %q, got %q", tt.wantServer, calledServer)
			}
			if calledTool != tt.wantTool {
				t.Errorf("expected tool %q, got %q", tt.wantTool, calledTool)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}
}

func TestCallToolJSON(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		wantErr bool
	}{
		{name: "valid json", args: `{"pair":"ETH-USDC"}`, wantErr: false},
		{name: "empty string", args: "", wantErr: false},
		{name: "invalid json", args: `{bad`, wantErr: true},
	}

	srv := fakeMCPServer(t, nil, func(_ string, _ map[string]any) *mcp.ToolResult {
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "ok"}}}
	})
	defer srv.Close()

	ts := NewMCPToolset(
		map[string]*mcp.Client{"trading": mcp.NewClient(srv.URL, "", 0)},
		map[string]string{"trade/": "trading"},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ts.CallToolJSON("trade/submit_order", tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("CallToolJSON(%q): err=%v, wantErr=%v", tt.args, err, tt.wantErr)
			}
		})
	}
}

func TestOpenAINameConversion(t *testing.T) {
	tests := []struct {
		mcpName    string
		wantOpenAI string
	}{
		{"trade/submit_order", "trade__submit_order"},
		{"market/get_prices", "market__get_prices"},
		{"memory/read", "memory__read"},
		{"no_slash", "no_slash"},
		{"a/b/c", "a__b__c"},
	}

	for _, tt := range tests {
		got := toOpenAIName(tt.mcpName)
		if got != tt.wantOpenAI {
			t.Errorf("toOpenAIName(%q) = %q, want %q", tt.mcpName, got, tt.wantOpenAI)
		}
	}
}

func TestCallToolWithOpenAINames(t *testing.T) {
	// Verify that CallTool correctly resolves OpenAI-sanitized names back to MCP names.
	var calledTool string

	srv := fakeMCPServer(t, []mcp.Tool{
		{Name: "trade/submit_order", Description: "Submit trade order"},
	}, func(name string, _ map[string]any) *mcp.ToolResult {
		calledTool = name
		return &mcp.ToolResult{Content: []mcp.Content{{Type: "text", Text: "ok"}}}
	})
	defer srv.Close()

	ts := NewMCPToolset(
		map[string]*mcp.Client{"trading": mcp.NewClient(srv.URL, "", 0)},
		map[string]string{"trade/": "trading"},
	)

	// Discover tools to populate the openaiToMCP map.
	if err := ts.DiscoverTools(); err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}
	// Build the name map by calling ToOpenAITools.
	ts.ToOpenAITools()

	// Call with the OpenAI-sanitized name (as OpenAI would return it).
	_, err := ts.CallTool("trade__submit_order", map[string]any{"pair": "ETH-USDC"})
	if err != nil {
		t.Fatalf("CallTool with OpenAI name: %v", err)
	}

	// The MCP server should have received the original name with slashes.
	if calledTool != "trade/submit_order" {
		t.Errorf("MCP server received tool name %q, want %q", calledTool, "trade/submit_order")
	}
}

func TestResolveClient(t *testing.T) {
	ts := &MCPToolset{
		routes: map[string]string{
			"trade/":  "trading",
			"market/": "trading",
			"memory/": "memory",
		},
	}

	tests := []struct {
		name     string
		toolName string
		want     string
		wantErr  bool
	}{
		{"trade prefix", "trade/submit_order", "trading", false},
		{"market prefix", "market/get_prices", "trading", false},
		{"memory prefix", "memory/write", "memory", false},
		{"unknown prefix", "code/execute", "", true},
		{"no prefix", "something", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ts.resolveClient(tt.toolName)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveClient(%q): err=%v, wantErr=%v", tt.toolName, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("resolveClient(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}
