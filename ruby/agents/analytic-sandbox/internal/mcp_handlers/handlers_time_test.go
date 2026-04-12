package mcp_handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestShellExecutionTime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker test")
	}

	sess, dm, cleanup := newTestSession(t)
	defer cleanup()

	h := NewHandlers(sess.MCP, sess, dm)

	t.Run("displays execution time", func(t *testing.T) {
		params := ShellParams{
			Command: "sleep 1.1",
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

		if !strings.Contains(text, "Execution Time:") {
			t.Errorf("expected execution time in output, got: %s", text)
		}

		lines := strings.Split(text, "\n")
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, "Execution Time:") {
				found = true
				parts := strings.Fields(line)
				if len(parts) < 3 {
					continue
				}
				timeStr := strings.TrimSuffix(parts[2], "s")
				if len(timeStr) == 0 {
					t.Errorf("empty time string")
				}
			}
		}
		if !found {
			t.Errorf("Execution Time line not found")
		}
	})
}
