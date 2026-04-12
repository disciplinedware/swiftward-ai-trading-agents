package mcp_handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestReadFileDefaults(t *testing.T) {
	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	// Create a file with many lines
	var lines []string
	for i := 1; i <= 400; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")

	// Write test files to session workspace
	pyFile := filepath.Join(sess.HostWorkDir, "test.py")
	_ = os.WriteFile(pyFile, []byte(content), 0644)

	txtFile := filepath.Join(sess.HostWorkDir, "test.txt")
	_ = os.WriteFile(txtFile, []byte(content), 0644)

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("python default lines", func(t *testing.T) {
		params := ReadFileParams{
			Path: "test.py",
			// Count is 0 (default)
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "read_file",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleReadFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		text := res.Content[0].(mcp.TextContent).Text

		// Should have 300 lines (indices 0 to 299)
		if !strings.Contains(text, "299: line 300") {
			t.Error("test.py should read 300 lines by default")
		}
		if strings.Contains(text, "300: line 301") {
			t.Error("test.py should read ONLY 300 lines by default")
		}
	})

	t.Run("txt default lines", func(t *testing.T) {
		params := ReadFileParams{
			Path: "test.txt",
			// Count is 0 (default)
		}
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "read_file",
				Arguments: toParams(t, params),
			},
		}

		res, err := h.handleReadFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		text := res.Content[0].(mcp.TextContent).Text

		// Should have 20 lines (indices 0 to 19)
		if !strings.Contains(text, "19: line 20") {
			t.Logf("Got text:\n%s", text)
			t.Error("test.txt should read 20 lines by default")
		}
		if strings.Contains(text, "20: line 21") {
			t.Error("test.txt should read ONLY 20 lines by default")
		}
	})
}
