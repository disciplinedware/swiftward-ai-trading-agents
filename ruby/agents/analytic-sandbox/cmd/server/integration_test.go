package main

import (
	"bytes"
	"encoding/json"
	"analytic-sandbox/internal/sandbox"
	"analytic-sandbox/internal/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestToolSuiteIntegration replicates scripts/tool_suite_test.sh
func TestToolSuiteIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 1. Setup Environment
	tmpDir := t.TempDir()

	dm, err := sandbox.NewDockerManager("")
	if err != nil {
		t.Skip("Docker not available")
	}

	// Ensure cleanup of any previous containers (best effort)
	_ = dm.CleanupOrphans(nil)

	sm := session.NewManager(dm, tmpDir)
	defer sm.Cleanup(nil)

	handler := NewAppHandler(sm, dm, tmpDir, "")

	// Helper to make requests
	call := func(method, sessionID string, body map[string]any) map[string]any {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		if sessionID != "" {
			req.Header.Set("Mcp-Session-Id", sessionID)
		}

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("request failed with status %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		return resp
	}

	// 2. Initialize
	initBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "go-test", "version": "1.0.0"},
		},
	}

	initReqBody, _ := json.Marshal(initBody)
	initReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(initReqBody))
	initReq.Header.Set("Content-Type", "application/json")
	initRR := httptest.NewRecorder()
	handler.ServeHTTP(initRR, initReq)

	sessionID := initRR.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("Failed to get Session ID from initialize response")
	}
	t.Logf("Session ID: %s", sessionID)

	// 3. shell (verify workspace is accessible)
	resp := call("tools/call", sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{
			"name": "shell",
			"arguments": map[string]any{
				"command": "ls -la /app",
			},
		},
	})

	resContent := resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(resContent, "total") {
		t.Errorf("shell ls failed: %s", resContent)
	}
}
