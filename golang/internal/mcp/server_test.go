package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() *Server {
	return NewServer("test-server", "1.0.0", []Tool{
		{Name: "echo", Description: "echo tool"},
	}, func(_ context.Context, name string, args map[string]any) (*ToolResult, error) {
		return TextResult("echoed: " + name), nil
	})
}

func TestServeHTTP_MethodNotAllowed(t *testing.T) {
	srv := newTestServer()

	tests := []struct {
		method string
	}{
		{"GET"},
		{"DELETE"},
		{"PUT"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/mcp", nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("got status %d, want 405", w.Code)
			}
			if allow := w.Header().Get("Allow"); allow != "POST" {
				t.Errorf("Allow header = %q, want POST", allow)
			}
		})
	}
}

func TestServeHTTP_SessionIDEcho(t *testing.T) {
	srv := newTestServer()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "session-abc-123")
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", w.Code)
	}
	if sid := w.Header().Get("Mcp-Session-Id"); sid != "session-abc-123" {
		t.Errorf("echoed session ID = %q, want %q", sid, "session-abc-123")
	}
}

func TestServeHTTP_InitializeGeneratesSessionID(t *testing.T) {
	srv := newTestServer()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", w.Code)
	}

	sid := w.Header().Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("initialize should generate Mcp-Session-Id, got empty")
	}

	// Verify response body
	var resp JSONRPCResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestServeHTTP_InitializeEchoesExistingSessionID(t *testing.T) {
	srv := newTestServer()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "existing-session")
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if sid := w.Header().Get("Mcp-Session-Id"); sid != "existing-session" {
		t.Errorf("should echo existing session ID, got %q", sid)
	}
}
