package mcp_handlers

import (
	"analytic-sandbox/internal/sandbox"
	"analytic-sandbox/internal/session"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func toParams(t *testing.T, v interface{}) map[string]interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func newTestSession(t *testing.T) (*session.Session, *sandbox.DockerManager, func()) {
	t.Helper()
	dir := t.TempDir()

	dm, err := sandbox.NewDockerManager("")
	if err != nil {
		t.Skip("Docker not available")
	}

	sm := session.NewManager(dm, dir)
	ctx := context.Background()
	sess, err := sm.GetOrCreate(ctx, session.SessionParams{
		AllowNetwork:   false,
		MemoryMB:       512,
		CpuCount:       4,
		DiskMB:         1024,
		TimeoutSeconds: 120,
	}, func(s *session.Session) (*server.MCPServer, http.Handler) {
		return server.NewMCPServer("test", "1.0"), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return sess, dm, func() { sm.Cleanup(ctx) }
}

func TestListFiles(t *testing.T) {
	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	// Create a dummy file in workspace
	_ = os.WriteFile(filepath.Join(sess.HostWorkDir, "created.txt"), []byte("data"), 0644)

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("list root", func(t *testing.T) {
		params := ListFilesParams{Path: "."}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "list_files",
				Arguments: toParams(t, params),
			},
		}
		res, err := h.handleListFiles(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		text := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, "created.txt") {
			t.Errorf("expected created.txt in listing, got: %s", text)
		}
	})
}

func TestWriteFile(t *testing.T) {
	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("write new file", func(t *testing.T) {
		params := WriteFileParams{
			Path:    "config.json",
			Content: "{\"key\": \"value\"}",
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "write_file",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleWriteFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("expected no error")
		}

		// Verify file exists
		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, "config.json"))
		if string(content) != params.Content {
			t.Errorf("content mismatch")
		}
	})
}

func TestEditFile(t *testing.T) {
	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	// Create a dummy file to edit
	testFile := "editable.txt"
	initialContent := "line 1\nline 2\nline 3"
	_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte(initialContent), 0644)

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("line replacement", func(t *testing.T) {
		params := EditFileParams{
			Path:    testFile,
			Mode:    "line",
			Target:  "1", // 0-based index, so line 2
			Content: "modified line 2",
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "edit_file",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleEditFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("expected no error")
		}

		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, testFile))
		expected := "line 1\nmodified line 2\nline 3"
		if string(content) != expected {
			t.Errorf("expected %q, got %q", expected, string(content))
		}
	})

	t.Run("string replacement", func(t *testing.T) {
		// Reset file
		_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte("hello world"), 0644)

		params := EditFileParams{
			Path:    testFile,
			Mode:    "string",
			Target:  "world",
			Content: "mcp",
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "edit_file",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleEditFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("expected no error")
		}

		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, testFile))
		if string(content) != "hello mcp" {
			t.Errorf("expected 'hello mcp', got %q", string(content))
		}
	})

	t.Run("string replacement with specials", func(t *testing.T) {
		// Reset file
		_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte("hello (world)"), 0644)

		params := EditFileParams{
			Path:    testFile,
			Mode:    "string",
			Target:  "(world)",
			Content: "[mcp]",
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "edit_file",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleEditFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("expected no error")
		}

		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, testFile))
		if string(content) != "hello [mcp]" {
			t.Errorf("expected 'hello [mcp]', got %q", string(content))
		}
	})

	t.Run("regex replacement", func(t *testing.T) {
		// Reset file
		_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte("hello world"), 0644)

		params := EditFileParams{
			Path:    testFile,
			Mode:    "regex",
			Target:  "w.*d",
			Content: "earth",
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "edit_file",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleEditFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("expected no error")
		}

		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, testFile))
		if string(content) != "hello earth" {
			t.Errorf("expected 'hello earth', got %q", string(content))
		}
	})
}

func TestRunUtility(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker test")
	}

	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	// Create a test file to find with ls
	_ = os.WriteFile(filepath.Join(sess.HostWorkDir, "test.txt"), []byte("input content"), 0644)

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("ls command", func(t *testing.T) {
		params := ShellParams{
			Command: "ls -la /app",
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "shell",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleShell(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		text := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, "test.txt") {
			t.Errorf("expected test.txt in ls output, got: %s", text)
		}
	})
}

func TestWeakTypeParsing(t *testing.T) {
	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	// Create test file in workspace
	testFile := "data.txt"
	_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte("line 1\nline 2\nline 3"), 0644)

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("string instead of int", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "read_file",
				Arguments: map[string]interface{}{
					"path":   testFile,
					"offset": "1", // String "1" instead of int 1
					"count":  "1", // String "1" instead of int 1
				},
			},
		}

		res, err := h.handleReadFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("expected no error, got: %v", res.Content[0])
		}

		text := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, "1: line 2") {
			t.Errorf("expected line 2, got: %s", text)
		}
	})
}
