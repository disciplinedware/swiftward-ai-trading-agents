package mcp_handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestReadFileRepro(t *testing.T) {
	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	// Create a file with 20 lines in the workspace
	content := ""
	for i := 0; i < 20; i++ {
		content += "line " + string(rune('A'+i)) + "\n"
	}
	testFile := filepath.Join(sess.HostWorkDir, "test.txt")
	_ = os.WriteFile(testFile, []byte(content), 0644)

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("read partial lines", func(t *testing.T) {
		params := ReadFileParams{
			Path:   "test.txt",
			Offset: 5,
			Count:  5,
		}
		args := make(map[string]interface{})
		args["path"] = params.Path
		args["offset"] = params.Offset
		args["count"] = params.Count

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "read_file",
				Arguments: args,
			},
		}

		res, err := h.handleReadFile(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}

		text := res.Content[0].(mcp.TextContent).Text
		t.Logf("Result:\n%s", text)
	})
}
