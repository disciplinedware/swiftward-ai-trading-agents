package main

import (
	"analytic-sandbox/internal/sandbox"
	"analytic-sandbox/internal/session"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSandboxHeaders(t *testing.T) {
	t.Run("defaults when no headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		opts := parseSandboxHeaders(req)

		if opts.AllowNetwork != false {
			t.Errorf("expected AllowNetwork=false, got %v", opts.AllowNetwork)
		}
		if opts.MemoryMB != 512 {
			t.Errorf("expected MemoryMB=512, got %d", opts.MemoryMB)
		}
		if opts.CpuCount != 1 {
			t.Errorf("expected CpuCount=1, got %d", opts.CpuCount)
		}
		if opts.DiskMB != 200 {
			t.Errorf("expected DiskMB=200, got %d", opts.DiskMB)
		}
		if opts.TimeoutSeconds != 120 {
			t.Errorf("expected TimeoutSeconds=120, got %d", opts.TimeoutSeconds)
		}
		if opts.WorkdirUUID != "" {
			t.Errorf("expected empty WorkdirUUID, got %q", opts.WorkdirUUID)
		}
	})

	t.Run("valid headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-Sandbox-Allow-Network", "true")
		req.Header.Set("X-Sandbox-Workdir-UUID", "550e8400-e29b-41d4-a716-446655440000")
		req.Header.Set("X-Sandbox-Memory-MB", "2048")
		req.Header.Set("X-Sandbox-Cpu-Count", "8")
		req.Header.Set("X-Sandbox-Disk-MB", "4096")
		req.Header.Set("X-Sandbox-Timeout-Seconds", "300")

		opts := parseSandboxHeaders(req)

		if !opts.AllowNetwork {
			t.Errorf("expected AllowNetwork=true")
		}
		if opts.WorkdirUUID != "550e8400-e29b-41d4-a716-446655440000" {
			t.Errorf("expected WorkdirUUID, got %q", opts.WorkdirUUID)
		}
		if opts.MemoryMB != 2048 {
			t.Errorf("expected MemoryMB=2048, got %d", opts.MemoryMB)
		}
		if opts.CpuCount != 8 {
			t.Errorf("expected CpuCount=8, got %d", opts.CpuCount)
		}
		if opts.DiskMB != 4096 {
			t.Errorf("expected DiskMB=4096, got %d", opts.DiskMB)
		}
		if opts.TimeoutSeconds != 300 {
			t.Errorf("expected TimeoutSeconds=300, got %d", opts.TimeoutSeconds)
		}
	})

	t.Run("clamp to maximums", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-Sandbox-Memory-MB", "99999")
		req.Header.Set("X-Sandbox-Cpu-Count", "999")
		req.Header.Set("X-Sandbox-Disk-MB", "99999")
		req.Header.Set("X-Sandbox-Timeout-Seconds", "9999")

		opts := parseSandboxHeaders(req)

		if opts.MemoryMB != 8192 {
			t.Errorf("expected MemoryMB clamped to 8192, got %d", opts.MemoryMB)
		}
		if opts.CpuCount != 16 {
			t.Errorf("expected CpuCount clamped to 16, got %d", opts.CpuCount)
		}
		if opts.DiskMB != 51200 {
			t.Errorf("expected DiskMB clamped to 51200, got %d", opts.DiskMB)
		}
		if opts.TimeoutSeconds != 600 {
			t.Errorf("expected TimeoutSeconds clamped to 600, got %d", opts.TimeoutSeconds)
		}
	})

	t.Run("clamp invalid values to defaults", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-Sandbox-Memory-MB", "0")
		req.Header.Set("X-Sandbox-Cpu-Count", "-1")
		req.Header.Set("X-Sandbox-Disk-MB", "0")
		req.Header.Set("X-Sandbox-Timeout-Seconds", "-5")

		opts := parseSandboxHeaders(req)

		if opts.MemoryMB != 512 {
			t.Errorf("expected MemoryMB=512 default, got %d", opts.MemoryMB)
		}
		if opts.CpuCount != 1 {
			t.Errorf("expected CpuCount=1 default, got %d", opts.CpuCount)
		}
		if opts.DiskMB != 200 {
			t.Errorf("expected DiskMB=200 default, got %d", opts.DiskMB)
		}
		if opts.TimeoutSeconds != 120 {
			t.Errorf("expected TimeoutSeconds=120 default, got %d", opts.TimeoutSeconds)
		}
	})

	t.Run("non-numeric values fall back to defaults", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-Sandbox-Memory-MB", "abc")
		req.Header.Set("X-Sandbox-Cpu-Count", "not-a-number")

		opts := parseSandboxHeaders(req)

		if opts.MemoryMB != 512 {
			t.Errorf("expected MemoryMB=512 default for non-numeric, got %d", opts.MemoryMB)
		}
		if opts.CpuCount != 1 {
			t.Errorf("expected CpuCount=1 default for non-numeric, got %d", opts.CpuCount)
		}
	})

	t.Run("allow network with value 1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("X-Sandbox-Allow-Network", "1")

		opts := parseSandboxHeaders(req)

		if !opts.AllowNetwork {
			t.Errorf("expected AllowNetwork=true for '1'")
		}
	})
}

func TestStatelessInitialize(t *testing.T) {
	tmpDir := t.TempDir()
	dm, _ := sandbox.NewDockerManager("")
	sm := session.NewManager(dm, tmpDir)
	handler := NewAppHandler(sm, dm, tmpDir, "") // No auth for basic test

	reqBody := `{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2024-11-05",
			"capabilities": {},
			"clientInfo": {"name": "test-client", "version": "1.0.0"}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", rr.Code)
		t.Logf("Response body: %s", rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["id"].(float64) != 1 {
		t.Errorf("expected id 1, got %v", resp["id"])
	}

	result := resp["result"].(map[string]any)
	if result["protocolVersion"] == "" {
		t.Errorf("expected protocolVersion in result")
	}
}

func TestAuthorizationWithValidToken(t *testing.T) {
	tmpDir := t.TempDir()
	dm, _ := sandbox.NewDockerManager("")
	sm := session.NewManager(dm, tmpDir)
	handler := NewAppHandler(sm, dm, tmpDir, "test-secret-token")

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK with valid token, got %v", rr.Code)
	}
}

func TestAuthorizationWithoutToken(t *testing.T) {
	tmpDir := t.TempDir()
	dm, _ := sandbox.NewDockerManager("")
	sm := session.NewManager(dm, tmpDir)
	handler := NewAppHandler(sm, dm, tmpDir, "test-secret-token")

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status Unauthorized without token, got %v", rr.Code)
	}
}

func TestAuthorizationWithInvalidToken(t *testing.T) {
	tmpDir := t.TempDir()
	dm, _ := sandbox.NewDockerManager("")
	sm := session.NewManager(dm, tmpDir)
	handler := NewAppHandler(sm, dm, tmpDir, "test-secret-token")

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer wrong-token")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status Unauthorized with invalid token, got %v", rr.Code)
	}
}

func TestNoAuthWhenTokenNotSet(t *testing.T) {
	tmpDir := t.TempDir()
	dm, _ := sandbox.NewDockerManager("")
	sm := session.NewManager(dm, tmpDir)
	handler := NewAppHandler(sm, dm, tmpDir, "") // No token = no auth

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header, but should work because no token is configured

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK when no token configured, got %v", rr.Code)
	}
}
