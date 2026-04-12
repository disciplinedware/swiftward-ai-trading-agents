package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("failed to write JSON response: %v", err)
	}
}

func TestClient(t *testing.T) {
	tests := []struct {
		name       string
		handler    func(t *testing.T) http.HandlerFunc
		method     string // "call", "list", "initialize"
		wantErr    bool
		wantErrMsg string
		checkFn    func(t *testing.T, client *Client)
	}{
		{
			name: "CallTool sends Accept header",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if !strings.Contains(r.Header.Get("Accept"), "application/json") {
						http.Error(w, `{"error":"Accept header must include application/json"}`, http.StatusNotAcceptable)
						return
					}
					writeJSON(t, w, JSONRPCResponse{
						JSONRPC: "2.0", ID: 1,
						Result: ToolResult{Content: []Content{{Type: "text", Text: "ok"}}},
					})
				}
			},
			method: "call",
		},
		{
			name: "ListTools sends Accept header",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if !strings.Contains(r.Header.Get("Accept"), "application/json") {
						http.Error(w, `{"error":"Accept header must include application/json"}`, http.StatusNotAcceptable)
						return
					}
					writeJSON(t, w, JSONRPCResponse{
						JSONRPC: "2.0", ID: 1,
						Result: map[string]any{"tools": []any{}},
					})
				}
			},
			method: "list",
		},
		{
			name: "Initialize sends Accept header",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if !strings.Contains(r.Header.Get("Accept"), "application/json") {
						http.Error(w, `{"error":"Accept header must include application/json"}`, http.StatusNotAcceptable)
						return
					}
					writeJSON(t, w, JSONRPCResponse{
						JSONRPC: "2.0", ID: 1,
						Result: InitializeResult{
							ProtocolVersion: "2025-03-26",
							ServerInfo:      ServerInfo{Name: "test", Version: "1.0"},
						},
					})
				}
			},
			method: "initialize",
		},
		{
			name: "CallTool sends Authorization header",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer test-key" {
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					writeJSON(t, w, JSONRPCResponse{
						JSONRPC: "2.0", ID: 1,
						Result: ToolResult{Content: []Content{{Type: "text", Text: "ok"}}},
					})
				}
			},
			method: "call",
		},
		{
			name: "CallTool sends custom headers",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("X-Agent-ID") != "agent-001" {
						http.Error(w, "missing agent ID", http.StatusBadRequest)
						return
					}
					writeJSON(t, w, JSONRPCResponse{
						JSONRPC: "2.0", ID: 1,
						Result: ToolResult{Content: []Content{{Type: "text", Text: "ok"}}},
					})
				}
			},
			method: "call",
			checkFn: func(t *testing.T, client *Client) {
				client.SetHeader("X-Agent-ID", "agent-001")
			},
		},
		{
			name: "CallTool handles RPC error",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					writeJSON(t, w, JSONRPCResponse{
						JSONRPC: "2.0", ID: 1,
						Error: &JSONRPCError{Code: -32600, Message: "invalid request"},
					})
				}
			},
			method:     "call",
			wantErr:    true,
			wantErrMsg: "rpc error -32600: invalid request",
		},
		{
			name: "CallTool handles HTTP error",
			handler: func(_ *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "server down", http.StatusInternalServerError)
				}
			},
			method:     "call",
			wantErr:    true,
			wantErrMsg: "MCP server returned HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler(t))
			defer srv.Close()

			client := NewClient(srv.URL, "test-key", 5*1e9)
			if tt.checkFn != nil {
				tt.checkFn(t, client)
			}

			var err error
			switch tt.method {
			case "call":
				_, err = client.CallTool("trade/submit_order", map[string]any{"pair": "ETH-USDC"})
			case "list":
				_, err = client.ListTools()
			case "initialize":
				_, err = client.Initialize()
			}

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClient_SessionIDTracking(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: server sends session ID
			w.Header().Set("Mcp-Session-Id", "sess-42")
			writeJSON(t, w, JSONRPCResponse{
				JSONRPC: "2.0", ID: 1,
				Result: ToolResult{Content: []Content{{Type: "text", Text: "first"}}},
			})
			return
		}
		// Second call: verify client sends session ID back
		if sid := r.Header.Get("Mcp-Session-Id"); sid != "sess-42" {
			t.Errorf("second call missing session ID, got %q", sid)
		}
		writeJSON(t, w, JSONRPCResponse{
			JSONRPC: "2.0", ID: 2,
			Result: ToolResult{Content: []Content{{Type: "text", Text: "second"}}},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "", 5*1e9)

	// First call - server sets session ID
	if _, err := client.CallTool("test", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call - client should send session ID
	if _, err := client.CallTool("test", nil); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}
