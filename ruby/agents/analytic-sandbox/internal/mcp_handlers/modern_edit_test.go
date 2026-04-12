package mcp_handlers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestEditFile_BlockMode(t *testing.T) {
	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("basic block replacement", func(t *testing.T) {
		testFile := "block.txt"
		initialContent := "line 1\n    to be replaced\nline 3"
		_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte(initialContent), 0644)

		params := EditFileParams{
			Path:         testFile,
			Mode:         "block",
			SearchBlock:  "    to be replaced",
			ReplaceBlock: "replaced line",
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
			t.Errorf("expected no error, got: %v", res.Content[0])
		}

		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, testFile))
		// The indent from search block (4 spaces) should be applied to replaced line
		expected := "line 1\n    replaced line\nline 3"
		if string(content) != expected {
			t.Errorf("expected %q, got %q", expected, string(content))
		}
	})

	t.Run("multiline block with indentation correction", func(t *testing.T) {
		testFile := "multi.txt"
		initialContent := "def main():\n    # existing comment\n    print('hello')"
		_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte(initialContent), 0644)

		params := EditFileParams{
			Path:         testFile,
			Mode:         "block",
			SearchBlock:  "    # existing comment",
			ReplaceBlock: "# first new line\nprint('new logic')",
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
			t.Errorf("expected no error, got: %v", res.Content[0])
		}

		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, testFile))
		expected := "def main():\n    # first new line\n    print('new logic')\n    print('hello')"
		if string(content) != expected {
			t.Errorf("expected %q, got %q", expected, string(content))
		}
	})

	t.Run("non-unique search block", func(t *testing.T) {
		testFile := "non-unique.txt"
		initialContent := "duplicate\nduplicate"
		_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte(initialContent), 0644)

		params := EditFileParams{
			Path:         testFile,
			Mode:         "block",
			SearchBlock:  "duplicate",
			ReplaceBlock: "unique",
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "edit_file",
				Arguments: toParams(t, params),
			},
		}

		res, _ := h.handleEditFile(context.Background(), req)
		if !res.IsError {
			t.Error("expected error for non-unique search block")
		}
		text := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, "found 2 times") {
			t.Errorf("expected 'found 2 times' error, got: %s", text)
		}
	})
}

func TestSyntaxValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker test")
	}

	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("write_file with syntax error", func(t *testing.T) {
		params := WriteFileParams{
			Path:    "bad.py",
			Content: "def foo(:", // Missing closing parenthesis
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
		if !res.IsError {
			t.Error("expected syntax error to be caught")
		}
		text := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, "Python syntax validation FAILED") {
			t.Errorf("expected python syntax error message, got: %s", text)
		}

		// Verify file was NOT created
		if _, err := os.Stat(filepath.Join(sess.HostWorkDir, "bad.py")); !os.IsNotExist(err) {
			t.Error("file should not have been created")
		}
	})

	t.Run("edit_file introducing syntax error", func(t *testing.T) {
		testFile := "good.rb"
		_ = os.WriteFile(filepath.Join(sess.HostWorkDir, testFile), []byte("puts 'hello'"), 0644)

		params := EditFileParams{
			Path:         testFile,
			Mode:         "block",
			SearchBlock:  "puts 'hello'",
			ReplaceBlock: "puts 'hello\"", // Unclosed string
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
		if !res.IsError {
			t.Error("expected syntax error to be caught")
		}
		text := res.Content[0].(mcp.TextContent).Text
		if !strings.Contains(text, "Ruby syntax validation FAILED") {
			t.Errorf("expected ruby syntax error message, got: %s", text)
		}

		// Verify file was NOT modified
		content, _ := os.ReadFile(filepath.Join(sess.HostWorkDir, testFile))
		if string(content) != "puts 'hello'" {
			t.Errorf("file should not have been modified, got: %q", string(content))
		}
	})
}
