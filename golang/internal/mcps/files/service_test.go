package files

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/platform"
)

// --- test helpers ---

func testService(t *testing.T) *Service {
	t.Helper()
	return &Service{rootDir: t.TempDir()}
}

func ctxWithAgent(agentID string) context.Context {
	return context.WithValue(context.Background(), agentIDContextKey, agentID)
}

func writeTestFile(t *testing.T, svc *Service, agentID, file, content string) {
	t.Helper()
	path := filepath.Join(svc.rootDir, agentID, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, svc *Service, agentID, file string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(svc.rootDir, agentID, file))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// parseJSON unmarshals the JSON text from a ToolResult into a map.
func parseJSON(t *testing.T, result *mcp.ToolResult) map[string]any {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("nil or empty result")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &m); err != nil {
		t.Fatalf("unmarshal result JSON: %v\nraw: %s", err, result.Content[0].Text)
	}
	return m
}

func jsonStr(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key].(string)
	if !ok {
		t.Fatalf("key %q not a string (got %T: %v)", key, m[key], m[key])
	}
	return v
}

func jsonFloat(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("key %q not a number (got %T: %v)", key, m[key], m[key])
	}
	return v
}

func jsonBool(t *testing.T, m map[string]any, key string) bool {
	t.Helper()
	v, ok := m[key].(bool)
	if !ok {
		t.Fatalf("key %q not a bool (got %T: %v)", key, m[key], m[key])
	}
	return v
}

func jsonArr(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("key %q not an array (got %T: %v)", key, m[key], m[key])
	}
	return v
}

// testHandler is an HTTP handler that wraps the files MCP for integration tests.
type testHandler struct {
	svc *Service
}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	agentID := r.Header.Get("X-Agent-ID")
	if agentID != "" {
		ctx := context.WithValue(r.Context(), agentIDContextKey, agentID)
		r = r.WithContext(ctx)
	}
	mcpServer := mcp.NewServer("files-mcp", "1.0.0", h.svc.tools(), h.svc.handleTool)
	mcpServer.ServeHTTP(w, r)
}

func newTestHandler(t *testing.T) (*testHandler, *Service) {
	svc := testService(t)
	return &testHandler{svc: svc}, svc
}

func testServiceContext(t *testing.T, rootDir string) *platform.ServiceContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	log := zap.NewNop()
	cfg := &config.Config{
		FilesMCP: config.FilesMCPConfig{RootDir: rootDir},
	}
	router := chi.NewRouter()
	return platform.NewServiceContext(ctx, log, cfg, router, []string{config.RoleFilesMCP})
}

// --- countFileLines ---

func TestCountFileLines(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"empty", "", 0},
		{"single line no newline", "hello", 1},
		{"single line with newline", "hello\n", 1},
		{"two lines", "a\nb\n", 2},
		{"two lines no trailing newline", "a\nb", 2},
		{"three lines", "a\nb\nc\n", 3},
		{"blank lines", "\n\n\n", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".txt")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := countFileLines(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("countFileLines(%q) = %d, want %d", tt.content, got, tt.want)
			}
		})
	}
}

// --- validateAgentID ---

func TestValidateAgentID(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		wantErr string
	}{
		{"valid simple", "agent-1", ""},
		{"valid with dots", "agent.v2.001", ""},
		{"valid with dashes", "agent-random-001", ""},
		{"empty", "", "agent_id is required"},
		{"dot", ".", "must not be '.'"},
		{"slash", "agent/evil", "path separators"},
		{"backslash", "agent\\evil", "path separators"},
		{"dot-dot", "..", "path separators"},
		{"dot-dot-slash", "../etc", "path separators"},
		{"embedded dot-dot", "agent..001", "path separators"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgentID(tt.agentID)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- safePath ---

func TestSafePath(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name    string
		agentID string
		file    string
		wantErr string
	}{
		{"valid simple file", "agent-1", "test.md", ""},
		{"valid nested path", "agent-1", "src/main.go", ""},
		{"valid deep nesting", "agent-1", "a/b/c/d.md", ""},
		{"traversal parent dir", "agent-1", "../other-agent/test.md", "traversal"},
		{"traversal absolute", "agent-1", "../../etc/passwd", "traversal"},
		{"traversal encoded dots", "agent-1", "src/../../secret.md", "traversal"},
		{"file is dot", "agent-1", ".", "traversal"},
		{"empty file", "agent-1", "", "file is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := svc.safePath(tt.agentID, tt.file)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got path=%q", tt.wantErr, path)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			absAgent, _ := filepath.Abs(svc.agentDir(tt.agentID))
			if !strings.HasPrefix(path, absAgent+string(filepath.Separator)) {
				t.Errorf("path %q not under agent dir %q", path, absAgent)
			}
		})
	}
}

// --- safeSearchPath ---

func TestSafeSearchPath(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name    string
		agentID string
		path    string
		wantErr string
	}{
		{"valid subdir", "agent-1", "src/", ""},
		{"valid nested", "agent-1", "src/pkg/", ""},
		{"traversal parent", "agent-1", "../other-agent", "traversal"},
		{"traversal absolute-style", "agent-1", "../../etc", "traversal"},
		{"traversal embedded", "agent-1", "src/../../secret", "traversal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.safeSearchPath(tt.agentID, tt.path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// --- symlink escape ---

func TestSymlinkEscapeRead(t *testing.T) {
	svc := testService(t)

	// Create a secret file at the workspace root (outside any agent dir)
	secretPath := filepath.Join(svc.rootDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create agent dir and a symlink pointing to the secret file
	agentDir := filepath.Join(svc.rootDir, "agent-1")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, filepath.Join(agentDir, "link.txt")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Reading via symlink should fail
	_, err := svc.toolRead("agent-1", map[string]any{"path": "link.txt"})
	if err == nil {
		t.Fatal("expected error for symlink escaping agent dir")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("error should mention traversal: %v", err)
	}
}

func TestSymlinkEscapeWrite(t *testing.T) {
	svc := testService(t)

	// Create agent dir with a symlink subdir pointing outside
	agentDir := filepath.Join(svc.rootDir, "agent-1")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideDir := filepath.Join(svc.rootDir, "outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(agentDir, "evil")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	// Writing through symlink subdir should fail
	_, err := svc.toolWrite("agent-1", map[string]any{"path": "evil/escape.txt", "content": "pwned"})
	if err == nil {
		t.Fatal("expected error for symlink escape via subdirectory")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("error should mention traversal: %v", err)
	}

	// Verify the file was NOT created outside
	if _, err := os.Stat(filepath.Join(outsideDir, "escape.txt")); !os.IsNotExist(err) {
		t.Error("file should not have been created outside agent dir")
	}

	// Writing through symlink with non-existent intermediate dirs should also fail.
	_, err = svc.toolWrite("agent-1", map[string]any{"path": "evil/new/deep/escape.txt", "content": "pwned"})
	if err == nil {
		t.Fatal("expected error for nested symlink escape")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("error should mention traversal: %v", err)
	}

	// Verify nothing was created outside
	if _, err := os.Stat(filepath.Join(outsideDir, "new")); !os.IsNotExist(err) {
		t.Error("nested dir should not have been created outside agent dir")
	}
}

// --- toolRead basic ---

func TestToolRead_Basic(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name        string
		agentID     string
		setup       func()
		args        map[string]any
		wantErr     string
		wantContent string
	}{
		{
			name:        "valid file",
			agentID:     "a1",
			setup:       func() { writeTestFile(t, svc, "a1", "data.txt", "hello world\n") },
			args:        map[string]any{"path": "data.txt"},
			wantContent: "hello world\n",
		},
		{
			name:    "file not found",
			agentID: "a1",
			args:    map[string]any{"path": "ghost.txt"},
			wantErr: "file not found",
		},
		{
			name:    "empty path",
			agentID: "a1",
			args:    map[string]any{},
			wantErr: "file is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			result, err := svc.toolRead(tt.agentID, tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			m := parseJSON(t, result)
			if got := jsonStr(t, m, "content"); got != tt.wantContent {
				t.Errorf("content = %q, want %q", got, tt.wantContent)
			}
			jsonFloat(t, m, "size_bytes")
			jsonFloat(t, m, "total_lines")
		})
	}
}

// --- toolRead offset ---

func TestToolRead_Offset(t *testing.T) {
	svc := testService(t)
	// 5-line file
	writeTestFile(t, svc, "a1", "lines.txt", "line1\nline2\nline3\nline4\nline5\n")

	tests := []struct {
		name        string
		offset      float64
		wantContent string
		wantTotal   int
	}{
		{"offset=0 returns all", 0, "line1\nline2\nline3\nline4\nline5\n", 5},
		{"offset=1 returns all (1-based start)", 1, "line1\nline2\nline3\nline4\nline5\n", 5},
		{"offset=3 skips first 2 lines", 3, "line3\nline4\nline5\n", 5},
		{"offset beyond EOF returns empty", 99, "", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.toolRead("a1", map[string]any{"path": "lines.txt", "offset": tt.offset})
			if err != nil {
				t.Fatal(err)
			}
			m := parseJSON(t, result)
			if got := jsonStr(t, m, "content"); got != tt.wantContent {
				t.Errorf("content = %q, want %q", got, tt.wantContent)
			}
			if int(jsonFloat(t, m, "total_lines")) != tt.wantTotal {
				t.Errorf("total_lines = %v, want %d", m["total_lines"], tt.wantTotal)
			}
		})
	}
}

// --- toolRead limit ---

func TestToolRead_Limit(t *testing.T) {
	svc := testService(t)
	writeTestFile(t, svc, "a1", "lines.txt", "line1\nline2\nline3\nline4\nline5\n")

	tests := []struct {
		name      string
		limit     float64
		wantLines string
		wantTrunc bool
	}{
		{"limit=0 returns all", 0, "line1\nline2\nline3\nline4\nline5\n", false},
		{"limit=2 truncated", 2, "line1\nline2", true},
		{"limit > file lines", 100, "line1\nline2\nline3\nline4\nline5\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.toolRead("a1", map[string]any{"path": "lines.txt", "limit": tt.limit})
			if err != nil {
				t.Fatal(err)
			}
			m := parseJSON(t, result)
			if got := jsonStr(t, m, "content"); got != tt.wantLines {
				t.Errorf("content = %q, want %q", got, tt.wantLines)
			}
			if got := jsonBool(t, m, "truncated"); got != tt.wantTrunc {
				t.Errorf("truncated = %v, want %v", got, tt.wantTrunc)
			}
		})
	}
}

// --- toolRead offset+limit ---

func TestToolRead_OffsetAndLimit(t *testing.T) {
	svc := testService(t)
	writeTestFile(t, svc, "a1", "lines.txt", "line1\nline2\nline3\nline4\nline5\n")

	// offset=2 (1-based), limit=3: returns lines 2,3,4
	result, err := svc.toolRead("a1", map[string]any{"path": "lines.txt", "offset": float64(2), "limit": float64(3)})
	if err != nil {
		t.Fatal(err)
	}
	m := parseJSON(t, result)
	want := "line2\nline3\nline4"
	if got := jsonStr(t, m, "content"); got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if int(jsonFloat(t, m, "total_lines")) != 5 {
		t.Errorf("total_lines = %v, want 5", m["total_lines"])
	}
	if !jsonBool(t, m, "truncated") {
		t.Error("truncated should be true")
	}
}

// --- toolWrite ---

func TestToolWrite(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name     string
		agentID  string
		setup    func()
		args     map[string]any
		wantErr  string
		wantPath string
		wantBody string
	}{
		{
			name:     "creates new file",
			agentID:  "a1",
			args:     map[string]any{"path": "main.go", "content": "package main\n"},
			wantPath: "main.go",
			wantBody: "package main\n",
		},
		{
			name:     "creates parent dirs",
			agentID:  "a2",
			args:     map[string]any{"path": "src/pkg/util.go", "content": "package pkg\n"},
			wantPath: "src/pkg/util.go",
			wantBody: "package pkg\n",
		},
		{
			name:     "overwrites existing",
			agentID:  "a1",
			setup:    func() { writeTestFile(t, svc, "a1", "over.txt", "old content") },
			args:     map[string]any{"path": "over.txt", "content": "new content"},
			wantPath: "over.txt",
			wantBody: "new content",
		},
		{
			name:    "empty path returns error",
			agentID: "a1",
			args:    map[string]any{"content": "x"},
			wantErr: "path is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			result, err := svc.toolWrite(tt.agentID, tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			m := parseJSON(t, result)
			if !jsonBool(t, m, "success") {
				t.Error("success should be true")
			}
			if jsonStr(t, m, "path") != tt.wantPath {
				t.Errorf("path = %q, want %q", m["path"], tt.wantPath)
			}
			jsonFloat(t, m, "size_bytes")
			got := readTestFile(t, svc, tt.agentID, tt.wantPath)
			if got != tt.wantBody {
				t.Errorf("disk content = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

// --- toolEdit ---

func TestToolEdit(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name          string
		agentID       string
		setup         func()
		args          map[string]any
		wantErr       string
		wantBody      string
		wantReplacements int
	}{
		{
			name:    "replaces first occurrence",
			agentID: "a1",
			setup:   func() { writeTestFile(t, svc, "a1", "code.go", "foo foo foo") },
			args:    map[string]any{"path": "code.go", "old_text": "foo", "new_text": "bar"},
			wantBody: "bar foo foo",
			wantReplacements: 1,
		},
		{
			name:    "replace_all replaces all occurrences",
			agentID: "a1",
			setup:   func() { writeTestFile(t, svc, "a1", "code.go", "foo foo foo") },
			args:    map[string]any{"path": "code.go", "old_text": "foo", "new_text": "bar", "replace_all": true},
			wantBody: "bar bar bar",
			wantReplacements: 3,
		},
		{
			name:    "old_text not found returns error",
			agentID: "a1",
			setup:   func() { writeTestFile(t, svc, "a1", "code.go", "hello world") },
			args:    map[string]any{"path": "code.go", "old_text": "missing", "new_text": "x"},
			wantErr: "old_text not found in file",
		},
		{
			name:    "empty old_text returns error",
			agentID: "a1",
			setup:   func() { writeTestFile(t, svc, "a1", "code.go", "hello") },
			args:    map[string]any{"path": "code.go", "old_text": "", "new_text": "x"},
			wantErr: "old_text is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			result, err := svc.toolEdit(tt.agentID, tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			m := parseJSON(t, result)
			if int(jsonFloat(t, m, "replacements")) != tt.wantReplacements {
				t.Errorf("replacements = %v, want %d", m["replacements"], tt.wantReplacements)
			}
			got := readTestFile(t, svc, tt.agentID, tt.args["path"].(string))
			if got != tt.wantBody {
				t.Errorf("disk content = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

// --- toolAppend ---

func TestToolAppend(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name     string
		agentID  string
		setup    func()
		args     map[string]any
		wantErr  string
		wantBody string
	}{
		{
			name:     "appends to existing file",
			agentID:  "a1",
			setup:    func() { writeTestFile(t, svc, "a1", "log.txt", "line1\n") },
			args:     map[string]any{"path": "log.txt", "content": "line2\n"},
			wantBody: "line1\nline2\n",
		},
		{
			name:     "creates new file",
			agentID:  "a2",
			args:     map[string]any{"path": "new.txt", "content": "first\n"},
			wantBody: "first\n",
		},
		{
			name:     "creates parent dirs",
			agentID:  "a2",
			args:     map[string]any{"path": "logs/day.txt", "content": "data\n"},
			wantBody: "data\n",
		},
		{
			name:    "empty path returns error",
			agentID: "a1",
			args:    map[string]any{"content": "x"},
			wantErr: "path is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			result, err := svc.toolAppend(tt.agentID, tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			m := parseJSON(t, result)
			if !jsonBool(t, m, "success") {
				t.Error("success should be true")
			}
			jsonStr(t, m, "path")
			jsonFloat(t, m, "size_bytes")
			got := readTestFile(t, svc, tt.agentID, tt.args["path"].(string))
			if got != tt.wantBody {
				t.Errorf("disk content = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

// --- toolDelete ---

func TestToolDelete(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name    string
		agentID string
		setup   func() string // returns the path created
		args    map[string]any
		wantErr string
	}{
		{
			name:    "deletes file",
			agentID: "a1",
			setup: func() string {
				writeTestFile(t, svc, "a1", "del.txt", "bye")
				return "del.txt"
			},
			args: map[string]any{"path": "del.txt"},
		},
		{
			name:    "deletes empty dir",
			agentID: "a1",
			setup: func() string {
				dir := filepath.Join(svc.rootDir, "a1", "emptydir")
				_ = os.MkdirAll(dir, 0o755)
				return "emptydir"
			},
			args: map[string]any{"path": "emptydir"},
		},
		{
			name:    "non-empty dir without recursive returns error",
			agentID: "a1",
			setup: func() string {
				writeTestFile(t, svc, "a1", "nonempty/file.txt", "content")
				return "nonempty"
			},
			args:    map[string]any{"path": "nonempty"},
			wantErr: "directory not empty",
		},
		{
			name:    "non-empty dir with recursive=true deletes all",
			agentID: "a1",
			setup: func() string {
				writeTestFile(t, svc, "a1", "todelete/sub/file.txt", "content")
				return "todelete"
			},
			args: map[string]any{"path": "todelete", "recursive": true},
		},
		{
			name:    "file not found returns error",
			agentID: "a1",
			setup:   func() string { return "" },
			args:    map[string]any{"path": "ghost.txt"},
			wantErr: "path not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var createdPath string
			if tt.setup != nil {
				createdPath = tt.setup()
			}
			result, err := svc.toolDelete(tt.agentID, tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			m := parseJSON(t, result)
			if jsonStr(t, m, "deleted") != tt.args["path"] {
				t.Errorf("deleted = %q, want %q", m["deleted"], tt.args["path"])
			}
			// Verify the path no longer exists on disk
			if createdPath != "" {
				fullPath := filepath.Join(svc.rootDir, tt.agentID, createdPath)
				if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
					t.Errorf("path %q should be deleted but still exists", fullPath)
				}
			}
		})
	}
}

// --- toolList single level ---

func TestToolList_SingleLevel(t *testing.T) {
	svc := testService(t)

	tests := []struct {
		name      string
		agentID   string
		setup     func()
		args      map[string]any
		wantFiles int
		wantDirs  int
	}{
		{
			name:      "empty agent dir returns empty entries",
			agentID:   "ghost",
			args:      map[string]any{},
			wantFiles: 0,
			wantDirs:  0,
		},
		{
			name:    "flat files show with is_dir=false",
			agentID: "a1",
			setup: func() {
				writeTestFile(t, svc, "a1", "a.txt", "aaa")
				writeTestFile(t, svc, "a1", "b.txt", "bbb")
			},
			args:      map[string]any{},
			wantFiles: 2,
			wantDirs:  0,
		},
		{
			name:    "with subdir shows dir entry with is_dir=true",
			agentID: "a2",
			setup: func() {
				writeTestFile(t, svc, "a2", "file.txt", "x")
				writeTestFile(t, svc, "a2", "subdir/nested.txt", "y")
			},
			args:      map[string]any{},
			wantFiles: 1,
			wantDirs:  1,
		},
		{
			name:    "subpath parameter lists a subdirectory",
			agentID: "a2",
			setup: func() {
				writeTestFile(t, svc, "a2", "subdir/nested.txt", "y")
				writeTestFile(t, svc, "a2", "subdir/other.txt", "z")
			},
			args:      map[string]any{"path": "subdir"},
			wantFiles: 2,
			wantDirs:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			result, err := svc.toolList(tt.agentID, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			m := parseJSON(t, result)
			entries := jsonArr(t, m, "entries")

			files := 0
			dirs := 0
			for _, e := range entries {
				entry := e.(map[string]any)
				if entry["is_dir"].(bool) {
					dirs++
				} else {
					files++
				}
			}
			if files != tt.wantFiles {
				t.Errorf("files = %d, want %d", files, tt.wantFiles)
			}
			if dirs != tt.wantDirs {
				t.Errorf("dirs = %d, want %d", dirs, tt.wantDirs)
			}
			if int(jsonFloat(t, m, "total_files")) != tt.wantFiles {
				t.Errorf("total_files = %v, want %d", m["total_files"], tt.wantFiles)
			}
			if int(jsonFloat(t, m, "total_dirs")) != tt.wantDirs {
				t.Errorf("total_dirs = %v, want %d", m["total_dirs"], tt.wantDirs)
			}
		})
	}
}

// --- toolList recursive ---

func TestToolList_Recursive(t *testing.T) {
	svc := testService(t)

	writeTestFile(t, svc, "a1", "root.txt", "r")
	writeTestFile(t, svc, "a1", "sub/a.txt", "a")
	writeTestFile(t, svc, "a1", "sub/deep/b.txt", "b")

	result, err := svc.toolList("a1", map[string]any{"recursive": true})
	if err != nil {
		t.Fatal(err)
	}

	m := parseJSON(t, result)
	entries := jsonArr(t, m, "entries")

	// Recursive should show all files but no dirs in entries
	for _, e := range entries {
		entry := e.(map[string]any)
		if entry["is_dir"].(bool) {
			t.Errorf("recursive listing should not include dirs, got: %v", entry["path"])
		}
	}

	if int(jsonFloat(t, m, "total_files")) != 3 {
		t.Errorf("total_files = %v, want 3", m["total_files"])
	}
	// Recursive mode counts all subdirectories (sub, sub/deep = 2)
	if int(jsonFloat(t, m, "total_dirs")) != 2 {
		t.Errorf("total_dirs = %v, want 2", m["total_dirs"])
	}
}

// --- toolFind ---

func TestToolFind(t *testing.T) {
	svc := testService(t)

	writeTestFile(t, svc, "a1", "README.md", "# readme")
	writeTestFile(t, svc, "a1", "notes.md", "# notes")
	writeTestFile(t, svc, "a1", "main.py", "print('hello')")
	writeTestFile(t, svc, "a1", "src/util.py", "# util")
	writeTestFile(t, svc, "a1", "src/core.go", "package main")

	tests := []struct {
		name       string
		args       map[string]any
		wantCount  int
		wantPaths  []string // subset check
	}{
		{
			name:      "*.md pattern finds markdown files by basename",
			args:      map[string]any{"pattern": "*.md"},
			wantCount: 2,
		},
		{
			name:      "*.py pattern finds python files by basename",
			args:      map[string]any{"pattern": "*.py"},
			wantCount: 2,
		},
		{
			name:      "pattern with path searches in subdir",
			args:      map[string]any{"pattern": "*.py", "path": "src"},
			wantCount: 1,
		},
		{
			name:      "no matches returns empty",
			args:      map[string]any{"pattern": "*.rb"},
			wantCount: 0,
		},
		{
			name:      "**/*.go finds go files recursively",
			args:      map[string]any{"pattern": "**/*.go"},
			wantCount: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.toolFind("a1", tt.args)
			if err != nil {
				t.Fatal(err)
			}
			m := parseJSON(t, result)
			matches := jsonArr(t, m, "matches")
			if len(matches) != tt.wantCount {
				paths := make([]string, 0)
				for _, match := range matches {
					entry := match.(map[string]any)
					paths = append(paths, entry["path"].(string))
				}
				t.Errorf("matches = %d, want %d (got paths: %v)", len(matches), tt.wantCount, paths)
			}
			if int(jsonFloat(t, m, "total")) != tt.wantCount {
				t.Errorf("total = %v, want %d", m["total"], tt.wantCount)
			}
			for _, wantPath := range tt.wantPaths {
				found := false
				for _, match := range matches {
					entry := match.(map[string]any)
					if entry["path"].(string) == wantPath {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected path %q not found in results", wantPath)
				}
			}
		})
	}
}

// --- toolSearch content mode ---

func TestToolSearch_ContentMode(t *testing.T) {
	svc := testService(t)

	writeTestFile(t, svc, "s1", "main.go", "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	writeTestFile(t, svc, "s1", "util.go", "package main\n\nfunc helper() {}\n")
	writeTestFile(t, svc, "s1", "README.md", "# Project\n\nThis is a Go project.\n")

	tests := []struct {
		name        string
		args        map[string]any
		wantErr     string
		wantMatches int
	}{
		{
			name:        "basic search",
			args:        map[string]any{"query": "package main"},
			wantMatches: 2,
		},
		{
			name:        "case insensitive",
			args:        map[string]any{"query": "PACKAGE MAIN"},
			wantMatches: 2,
		},
		{
			name:        "with glob filter",
			args:        map[string]any{"query": "package main", "glob": "*.go"},
			wantMatches: 2,
		},
		{
			name:        "glob filter reduces results",
			args:        map[string]any{"query": "Go", "glob": "*.md"},
			wantMatches: 1,
		},
		{
			name:    "missing query returns error",
			args:    map[string]any{},
			wantErr: "query is required",
		},
		{
			name:        "no matches returns empty",
			args:        map[string]any{"query": "xyzzy_not_found"},
			wantMatches: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.toolSearch("s1", tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			m := parseJSON(t, result)
			matches := jsonArr(t, m, "matches")
			if len(matches) != tt.wantMatches {
				t.Errorf("matches = %d, want %d", len(matches), tt.wantMatches)
			}
			if int(jsonFloat(t, m, "total_matches")) != tt.wantMatches {
				t.Errorf("total_matches = %v, want %d", m["total_matches"], tt.wantMatches)
			}
		})
	}
}

func TestToolSearch_WithContextLines(t *testing.T) {
	svc := testService(t)
	writeTestFile(t, svc, "s1", "file.go", "alpha\nbeta\ngamma\ndelta\nepsilon\n")

	result, err := svc.toolSearch("s1", map[string]any{"query": "gamma", "context_lines": float64(1)})
	if err != nil {
		t.Fatal(err)
	}

	m := parseJSON(t, result)
	matches := jsonArr(t, m, "matches")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	match := matches[0].(map[string]any)
	ctx := match["context"].(string)
	if !strings.Contains(ctx, "beta") {
		t.Errorf("context missing line before: %q", ctx)
	}
	if !strings.Contains(ctx, "gamma") {
		t.Errorf("context missing match line: %q", ctx)
	}
	if !strings.Contains(ctx, "delta") {
		t.Errorf("context missing line after: %q", ctx)
	}
	if strings.Contains(ctx, "alpha") {
		t.Errorf("context should not include alpha (2 lines before): %q", ctx)
	}
}

// --- toolSearch files_only mode ---

func TestToolSearch_FilesOnlyMode(t *testing.T) {
	svc := testService(t)

	writeTestFile(t, svc, "s2", "a.go", "package main\nfunc foo() {}\n")
	writeTestFile(t, svc, "s2", "b.go", "package main\nfunc bar() {}\n")
	writeTestFile(t, svc, "s2", "c.txt", "no package here\n")

	result, err := svc.toolSearch("s2", map[string]any{
		"query":       "package main",
		"output_mode": "files_only",
	})
	if err != nil {
		t.Fatal(err)
	}

	m := parseJSON(t, result)
	files := jsonArr(t, m, "files")
	if len(files) != 2 {
		t.Errorf("files = %d, want 2", len(files))
	}
	if int(jsonFloat(t, m, "total_files")) != 2 {
		t.Errorf("total_files = %v, want 2", m["total_files"])
	}

	// Should return file paths as strings
	for _, f := range files {
		if _, ok := f.(string); !ok {
			t.Errorf("files entry should be string, got %T", f)
		}
	}

	// Verify the right files are returned
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.(string))
	}
	sort.Strings(paths)
	if paths[0] != "a.go" || paths[1] != "b.go" {
		t.Errorf("unexpected files: %v", paths)
	}
}

// --- toolSearch count mode ---

func TestToolSearch_CountMode(t *testing.T) {
	svc := testService(t)

	writeTestFile(t, svc, "s3", "a.go", "foo\nfoo\nbar\n")
	writeTestFile(t, svc, "s3", "b.go", "foo\nbaz\n")

	result, err := svc.toolSearch("s3", map[string]any{
		"query":       "foo",
		"output_mode": "count",
	})
	if err != nil {
		t.Fatal(err)
	}

	m := parseJSON(t, result)
	counts := jsonArr(t, m, "counts")

	totalFromCounts := 0
	for _, c := range counts {
		entry := c.(map[string]any)
		totalFromCounts += int(entry["count"].(float64))
	}

	if int(jsonFloat(t, m, "total_matches")) != 3 {
		t.Errorf("total_matches = %v, want 3", m["total_matches"])
	}
	if totalFromCounts != 3 {
		t.Errorf("sum of per-file counts = %d, want 3", totalFromCounts)
	}
}

// --- security: agent isolation ---

func TestSecurity_AgentIsolation(t *testing.T) {
	svc := testService(t)

	// Write file as agent-A
	_, err := svc.toolWrite("agent-A", map[string]any{"path": "secret.txt", "content": "A's secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.toolWrite("agent-B", map[string]any{"path": "secret.txt", "content": "B's secret"})
	if err != nil {
		t.Fatal(err)
	}

	// agent-A reads its own file
	result, err := svc.toolRead("agent-A", map[string]any{"path": "secret.txt"})
	if err != nil {
		t.Fatal(err)
	}
	m := parseJSON(t, result)
	if jsonStr(t, m, "content") != "A's secret" {
		t.Errorf("agent-A got wrong content: %q", m["content"])
	}

	// agent-B cannot read agent-A's file via traversal
	_, err = svc.toolRead("agent-B", map[string]any{"path": "../agent-A/secret.txt"})
	if err == nil {
		t.Fatal("expected error for traversal attempt")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("expected traversal error, got: %v", err)
	}
}

// --- HTTP integration ---

func TestHTTP_Integration(t *testing.T) {
	rootDir := t.TempDir()
	svcCtx := testServiceContext(t, rootDir)
	svc := NewService(svcCtx)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(svcCtx.Router())
	defer ts.Close()

	call := func(id int, tool string, args map[string]any, agentID string) (map[string]any, bool) {
		t.Helper()
		reqBody := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "tools/call",
			"params":  map[string]any{"name": tool, "arguments": args},
		}
		data, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/files", strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
		if agentID != "" {
			req.Header.Set("X-Agent-ID", agentID)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()

		var rpcResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
			t.Fatal(err)
		}

		result, ok := rpcResp["result"].(map[string]any)
		if !ok {
			return nil, false
		}
		content, ok := result["content"].([]any)
		if !ok || len(content) == 0 {
			return nil, false
		}
		text, _ := content[0].(map[string]any)["text"].(string)
		isError, _ := result["isError"].(bool)

		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			// text may not be JSON if isError
			return map[string]any{"_raw": text}, isError
		}
		return m, isError
	}

	agent := "http-test-agent"

	// Write then read
	m, isErr := call(1, "files/write", map[string]any{"path": "hello.txt", "content": "hello world"}, agent)
	if isErr {
		t.Fatalf("write failed: %v", m)
	}
	if m["success"] != true {
		t.Error("write success should be true")
	}

	m, isErr = call(2, "files/read", map[string]any{"path": "hello.txt"}, agent)
	if isErr {
		t.Fatalf("read failed: %v", m)
	}
	if m["content"] != "hello world" {
		t.Errorf("content = %v, want hello world", m["content"])
	}

	// Missing X-Agent-ID header -> error
	m, isErr = call(3, "files/list", map[string]any{}, "")
	if !isErr {
		t.Errorf("expected error for missing agent ID, got: %v", m)
	}

	// Invalid agent ID -> error
	m, isErr = call(4, "files/list", map[string]any{}, "../evil")
	if !isErr {
		t.Errorf("expected error for invalid agent ID, got: %v", m)
	}

	// Unknown tool -> error
	m, isErr = call(5, "files/nonexistent", map[string]any{}, agent)
	if !isErr {
		t.Errorf("expected error for unknown tool, got: %v", m)
	}
}

// --- NewService lifecycle ---

func TestNewService(t *testing.T) {
	rootDir := t.TempDir()

	tests := []struct {
		name        string
		rootDir     string
		wantRootDir string
	}{
		{"configured root", rootDir, rootDir},
		{"empty root falls back to default", "", "./data/workspace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcCtx := testServiceContext(t, tt.rootDir)
			svc := NewService(svcCtx)
			if svc.rootDir != tt.wantRootDir {
				t.Errorf("rootDir = %q, want %q", svc.rootDir, tt.wantRootDir)
			}
			if svc.log == nil {
				t.Error("logger should not be nil")
			}
			if svc.svcCtx == nil {
				t.Error("svcCtx should not be nil")
			}
		})
	}
}

func TestInitializeRegistersRoute(t *testing.T) {
	rootDir := t.TempDir()
	svcCtx := testServiceContext(t, rootDir)
	svc := NewService(svcCtx)

	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	ts := httptest.NewServer(svcCtx.Router())
	defer ts.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	resp, err := http.Post(ts.URL+"/mcp/files", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var rpcResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatal(err)
	}

	result := rpcResp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 8 {
		t.Errorf("tools list returned %d tools, want 8", len(tools))
	}
}

func TestStop(t *testing.T) {
	svcCtx := testServiceContext(t, t.TempDir())
	svc := NewService(svcCtx)
	if err := svc.Stop(); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

// TestHandleToolDispatch verifies tool dispatch via context and the testHandler HTTP wrapper.
func TestHandleToolDispatch(t *testing.T) {
	h, svc := newTestHandler(t)
	writeTestFile(t, svc, "agent-1", "test.txt", "hello")

	// Verify dispatch via context
	ctx := ctxWithAgent("agent-1")
	result, err := svc.handleTool(ctx, "files/read", map[string]any{"path": "test.txt"})
	if err != nil {
		t.Fatalf("handleTool: %v", err)
	}
	m := parseJSON(t, result)
	if jsonStr(t, m, "content") != "hello" {
		t.Errorf("content = %q, want hello", m["content"])
	}

	// Verify testHandler wraps correctly over HTTP
	ts := httptest.NewServer(h)
	defer ts.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "agent-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Unknown tool returns error
	_, err = svc.handleTool(ctx, "files/unknown", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("error %q should mention unknown tool", err.Error())
	}
}
