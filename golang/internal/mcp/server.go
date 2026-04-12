package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// ToolHandler is called when a tools/call request is dispatched.
type ToolHandler func(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error)

// Server implements MCP Streamable HTTP (POST-only).
type Server struct {
	name    string
	version string
	tools   []Tool
	handler ToolHandler
}

// NewServer creates a new MCP server endpoint.
func NewServer(name, version string, tools []Tool, handler ToolHandler) *Server {
	return &Server{
		name:    name,
		version: version,
		tools:   tools,
		handler: handler,
	}
}

// ServeHTTP handles JSON-RPC 2.0 requests for the MCP protocol.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Echo Mcp-Session-Id if provided by client/gateway.
	if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
		w.Header().Set("Mcp-Session-Id", sid)
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nil, -32700, "Parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		writeError(w, req.ID, -32600, "Invalid JSON-RPC version")
		return
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, req.ID)
	case "tools/list":
		s.handleToolsList(w, req.ID)
	case "tools/call":
		s.handleToolsCall(w, r, req.ID, req.Params)
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	default:
		writeError(w, req.ID, -32601, "Method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, id any) {
	// Generate session ID if not already echoed from request.
	if w.Header().Get("Mcp-Session-Id") == "" {
		w.Header().Set("Mcp-Session-Id", uuid.NewString())
	}

	result := InitializeResult{
		ProtocolVersion: "2025-03-26",
		Capabilities: Capabilities{
			Tools: &ToolsCapability{},
		},
		ServerInfo: ServerInfo{
			Name:    s.name,
			Version: s.version,
		},
	}
	writeResult(w, id, result)
}

func (s *Server) handleToolsList(w http.ResponseWriter, id any) {
	writeResult(w, id, map[string]any{"tools": s.tools})
}

func (s *Server) handleToolsCall(w http.ResponseWriter, r *http.Request, id any, params any) {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		writeError(w, id, -32602, "Invalid params")
		return
	}
	var callParams ToolCallParams
	if err := json.Unmarshal(paramsBytes, &callParams); err != nil {
		writeError(w, id, -32602, "Invalid tool call params")
		return
	}

	if callParams.Name == "" {
		writeError(w, id, -32602, "Missing tool name")
		return
	}

	result, err := s.handler(r.Context(), callParams.Name, callParams.Arguments)
	if err != nil {
		writeResult(w, id, &ToolResult{
			Content: []Content{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
		return
	}

	writeResult(w, id, result)
}

func writeResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	})
}

// TextResult returns a single text content block.
func TextResult(text string) *ToolResult {
	return &ToolResult{
		Content: []Content{{Type: "text", Text: text}},
	}
}

// JSONResult returns a JSON-serialized result.
func JSONResult(v any) (*ToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return TextResult(string(data)), nil
}
