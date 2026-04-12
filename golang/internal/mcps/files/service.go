package files

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"go.uber.org/zap"

	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/observability"
	"ai-trading-agents/internal/platform"
)

type contextKey string

const agentIDContextKey contextKey = "agent_id"

// maxFileBytes is the maximum allowed size for a single file write or append.
// Prevents LLM agents from exhausting disk space or RAM via unbounded writes.
const maxFileBytes = 10 * 1024 * 1024 // 10 MB

// Service implements the Files MCP - persistent file-based workspace for trading agents.
type Service struct {
	svcCtx  *platform.ServiceContext
	log     *zap.Logger
	rootDir string // from config, e.g. "./data/workspace"
}

// NewService creates the Files MCP service.
func NewService(svcCtx *platform.ServiceContext) *Service {
	rootDir := svcCtx.Config().FilesMCP.RootDir
	if rootDir == "" {
		rootDir = "./data/workspace"
	}
	return &Service{
		svcCtx:  svcCtx,
		log:     svcCtx.Logger().Named("files_mcp"),
		rootDir: rootDir,
	}
}

func (s *Service) Initialize() error {
	mcpServer := mcp.NewServer("files-mcp", "1.0.0", s.tools(), s.handleTool)

	// Wrap MCP handler to extract X-Agent-ID header into context.
	s.svcCtx.Router().Post("/mcp/files", func(w http.ResponseWriter, r *http.Request) {
		agentID := r.Header.Get("X-Agent-ID")
		if agentID != "" {
			ctx := context.WithValue(r.Context(), agentIDContextKey, agentID)
			ctx = observability.WithLogger(ctx, s.log.With(zap.String("agent_id", agentID)))
			r = r.WithContext(ctx)
		}
		mcpServer.ServeHTTP(w, r)
	})

	s.log.Info("Files MCP registered at /mcp/files", zap.String("root_dir", s.rootDir))
	return nil
}

func (s *Service) Start() error {
	<-s.svcCtx.Context().Done()
	return nil
}

func (s *Service) Stop() error {
	s.log.Info("Files MCP stopped")
	return nil
}

// validateAgentID ensures the agent ID is safe for use as a directory name.
func validateAgentID(agentID string) error {
	if agentID == "" {
		return fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}
	if agentID == "." {
		return fmt.Errorf("invalid agent_id: must not be '.'")
	}
	if strings.ContainsAny(agentID, "/\\") || strings.Contains(agentID, "..") {
		return fmt.Errorf("invalid agent_id: must not contain path separators or '..'")
	}
	return nil
}

// agentDir returns the root directory for a given agent's workspace.
func (s *Service) agentDir(agentID string) string {
	return filepath.Join(s.rootDir, agentID)
}

// safePath resolves a file path within an agent's workspace directory,
// preventing directory traversal and symlink escape attacks.
func (s *Service) safePath(agentID, file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("file is required")
	}

	base := s.agentDir(agentID)
	joined := filepath.Join(base, file)
	resolved, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}

	// Must be strictly under agent dir (not equal to it - that's a directory, not a file)
	if !strings.HasPrefix(resolved, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal not allowed: %s", file)
	}

	// Symlink check: resolve real paths to prevent symlink escape
	if err := checkSymlinkEscape(resolved, absBase); err != nil {
		return "", fmt.Errorf("path traversal not allowed: %s", file)
	}

	return resolved, nil
}

// safeSearchPath validates the search path is within the agent's directory.
func (s *Service) safeSearchPath(agentID, searchPath string) (string, error) {
	dir := s.agentDir(agentID)
	combined := filepath.Join(dir, searchPath)

	absDir, err := filepath.Abs(combined)
	if err != nil {
		return "", fmt.Errorf("invalid search path: %w", err)
	}

	absAgent, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("invalid agent path: %w", err)
	}

	if !strings.HasPrefix(absDir, absAgent+string(filepath.Separator)) && absDir != absAgent {
		return "", fmt.Errorf("search path traversal not allowed: %s", searchPath)
	}

	// Symlink check: resolve real paths to prevent symlink escape
	if err := checkSymlinkEscape(absDir, absAgent); err != nil {
		return "", fmt.Errorf("search path traversal not allowed: %s", searchPath)
	}

	return absDir, nil
}

// checkSymlinkEscape verifies that the real filesystem path of target
// (resolving all symlinks) stays at or under base. For non-existent paths,
// walks up the ancestor chain to find the deepest existing component and
// verifies it resolves under base.
func checkSymlinkEscape(target, base string) error {
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		// Base doesn't exist yet - no symlinks possible
		return nil
	}

	isUnder := func(path string) bool {
		return path == realBase || strings.HasPrefix(path, realBase+string(filepath.Separator))
	}

	// Walk up from target until we find a component that exists on disk,
	// then resolve its symlinks and verify it's under base.
	cur := target
	for {
		if realCur, err := filepath.EvalSymlinks(cur); err == nil {
			if !isUnder(realCur) {
				return fmt.Errorf("symlink escape detected")
			}
			return nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root without finding an existing component
			break
		}
		cur = parent
	}

	return nil
}

func (s *Service) tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "files/read",
			Description: "Read file content. Returns: {content, total_lines, truncated}. offset is 1-based line number (0 = start). limit=0 returns all lines. Max 10 MB.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "Path relative to workspace root."},
					"offset": map[string]any{"type": "integer", "description": "1-based line number to start from (0 = start of file)."},
					"limit":  map[string]any{"type": "integer", "description": "Maximum number of lines to return (0 = all lines)."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "files/write",
			Description: "Create or overwrite a file. Creates parent directories. Returns: {success, path, size_bytes}. Max 10 MB.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path relative to workspace root."},
					"content": map[string]any{"type": "string", "description": "Full file content. This REPLACES any existing content."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "files/edit",
			Description: "Exact string replacement in a file. Returns: {replacements}. Errors if old_text not found.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Path relative to workspace root."},
					"old_text":    map[string]any{"type": "string", "description": "Exact text to find and replace."},
					"new_text":    map[string]any{"type": "string", "description": "Replacement text."},
					"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences. Default false (first only)."},
				},
				"required": []string{"path", "old_text", "new_text"},
			},
		},
		{
			Name:        "files/append",
			Description: "Append content to end of file. Creates the file if it doesn't exist. Returns: {success, path, size_bytes}. Fails if result exceeds 10 MB.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path relative to workspace root."},
					"content": map[string]any{"type": "string", "description": "Content to append."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "files/delete",
			Description: "Delete a file or directory. Returns: {deleted: path}. For non-empty directories, recursive=true is required.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Path relative to workspace root."},
					"recursive": map[string]any{"type": "boolean", "description": "Delete directory and all contents. Default false."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "files/list",
			Description: "List files and directories. Returns: {entries: [{name, path, is_dir, size_bytes, modified_at}], total_files, total_dirs, total_size_bytes}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Subdirectory to list. Default: workspace root."},
					"recursive": map[string]any{"type": "boolean", "description": "List all descendants. Default false (single level only)."},
				},
			},
		},
		{
			Name:        "files/find",
			Description: "Find files by glob pattern, sorted by modification time (newest first). Returns: {matches: [{path, size_bytes, modified_at}], total}. Supports ** for recursive matching.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Glob pattern, e.g. \"*.py\", \"**/*.md\", \"src/*.go\"."},
					"path":    map[string]any{"type": "string", "description": "Subdirectory to search from. Default: workspace root."},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "files/search",
			Description: "Case-insensitive text search. output_mode: \"content\" (default) returns {matches: [{file, line, match, context}]}; \"files_only\" returns {files: [...]}; \"count\" returns {counts: [{file, count}]}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":         map[string]any{"type": "string", "description": "Text to search for (case-insensitive)."},
					"path":          map[string]any{"type": "string", "description": "Subdirectory to limit search. Default: all files."},
					"context_lines": map[string]any{"type": "integer", "description": "Lines of context around each match. Default 3."},
					"glob":          map[string]any{"type": "string", "description": "File filter glob, e.g. \"*.py\". Default: all files."},
					"output_mode":   map[string]any{"type": "string", "description": "Output mode: \"content\" (default), \"files_only\", \"count\"."},
					"max_results":   map[string]any{"type": "integer", "description": "Maximum results to return. Default 50."},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (s *Service) handleTool(ctx context.Context, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	agentID, _ := ctx.Value(agentIDContextKey).(string)
	if err := validateAgentID(agentID); err != nil {
		return nil, err
	}

	switch toolName {
	case "files/read":
		return s.toolRead(agentID, args)
	case "files/write":
		return s.toolWrite(agentID, args)
	case "files/edit":
		return s.toolEdit(agentID, args)
	case "files/append":
		return s.toolAppend(agentID, args)
	case "files/delete":
		return s.toolDelete(agentID, args)
	case "files/list":
		return s.toolList(agentID, args)
	case "files/find":
		return s.toolFind(agentID, args)
	case "files/search":
		return s.toolSearch(agentID, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (s *Service) toolRead(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	path, _ := args["path"].(string)
	offset := 0
	if v, ok := args["offset"].(float64); ok {
		offset = int(v)
	}
	limit := 0
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	absPath, err := s.safePath(agentID, path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s (use files/list to browse directories)", path)
	}
	if info.Size() > int64(maxFileBytes) {
		return nil, fmt.Errorf("file too large to read (%d bytes, limit %d MB)", info.Size(), maxFileBytes/1024/1024)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	allLines := strings.Split(string(data), "\n")
	// If file ends with newline, the last element is ""; don't count it as a line
	totalLines := len(allLines)
	if totalLines > 0 && allLines[totalLines-1] == "" {
		totalLines--
	}

	// Apply offset (1-based: offset=1 means start at line 1, i.e. index 0)
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start > len(allLines) {
		start = len(allLines)
	}

	selected := allLines[start:]

	truncated := false
	if limit > 0 && len(selected) > limit {
		selected = selected[:limit]
		truncated = true
	}

	content := strings.Join(selected, "\n")

	return mcp.JSONResult(map[string]any{
		"path":        path,
		"content":     content,
		"size_bytes":  info.Size(),
		"total_lines": totalLines,
		"truncated":   truncated,
	})
}

func (s *Service) toolWrite(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	absPath, err := s.safePath(agentID, path)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	data := []byte(content)
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("content exceeds maximum file size (%d bytes, limit %d MB)", len(data), maxFileBytes/1024/1024)
	}
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"success":    true,
		"path":       path,
		"size_bytes": len(data),
	})
}

func (s *Service) toolEdit(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	path, _ := args["path"].(string)
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if oldText == "" {
		return nil, fmt.Errorf("old_text is required")
	}

	absPath, err := s.safePath(agentID, path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory: %s", path)
	}
	if info.Size() > int64(maxFileBytes) {
		return nil, fmt.Errorf("file too large to edit (%d bytes, limit %d MB)", info.Size(), maxFileBytes/1024/1024)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	content := string(data)
	if !strings.Contains(content, oldText) {
		return nil, fmt.Errorf("old_text not found in file: %s — call files/read to get the current content before editing", path)
	}

	var result string
	var replacements int
	if replaceAll {
		replacements = strings.Count(content, oldText)
		result = strings.ReplaceAll(content, oldText, newText)
	} else {
		result = strings.Replace(content, oldText, newText, 1)
		replacements = 1
	}

	if err := os.WriteFile(absPath, []byte(result), 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"replacements": replacements,
	})
}

func (s *Service) toolAppend(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	absPath, err := s.safePath(agentID, path)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	// Check resulting size before appending.
	existing, statErr := os.Stat(absPath)
	if statErr == nil {
		if existing.Size()+int64(len(content)) > int64(maxFileBytes) {
			return nil, fmt.Errorf("append would exceed maximum file size (%d MB)", maxFileBytes/1024/1024)
		}
	}

	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(content); err != nil {
		return nil, fmt.Errorf("append to file: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"success":    true,
		"path":       path,
		"size_bytes": info.Size(),
	})
}

func (s *Service) toolDelete(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	path, _ := args["path"].(string)
	recursive, _ := args["recursive"].(bool)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	absPath, err := s.safePath(agentID, path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		return nil, fmt.Errorf("stat path: %w", err)
	}

	if info.IsDir() {
		if !recursive {
			// Check if directory is empty
			entries, err := os.ReadDir(absPath)
			if err != nil {
				return nil, fmt.Errorf("read directory: %w", err)
			}
			if len(entries) > 0 {
				return nil, fmt.Errorf("directory not empty, use recursive=true to delete with contents")
			}
		}
		if recursive {
			if err := os.RemoveAll(absPath); err != nil {
				return nil, fmt.Errorf("delete directory: %w", err)
			}
		} else {
			if err := os.Remove(absPath); err != nil {
				return nil, fmt.Errorf("delete directory: %w", err)
			}
		}
	} else {
		if err := os.Remove(absPath); err != nil {
			return nil, fmt.Errorf("delete file: %w", err)
		}
	}

	return mcp.JSONResult(map[string]any{
		"deleted": path,
	})
}

func (s *Service) toolList(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	subPath, _ := args["path"].(string)
	recursive, _ := args["recursive"].(bool)

	dir := s.agentDir(agentID)
	if subPath != "" {
		safeDir, err := s.safeSearchPath(agentID, subPath)
		if err != nil {
			return nil, err
		}
		dir = safeDir
	}

	agentRoot := s.agentDir(agentID)
	absAgentRoot, _ := filepath.Abs(agentRoot)

	// If dir doesn't exist, return empty list
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return mcp.JSONResult(map[string]any{
			"entries":          []any{},
			"total_files":      0,
			"total_dirs":       0,
			"total_size_bytes": int64(0),
		})
	}

	type entry struct {
		Name       string    `json:"-"`
		Path       string    `json:"path"`
		IsDir      bool      `json:"is_dir"`
		SizeBytes  int64     `json:"size_bytes"`
		ModifiedAt time.Time `json:"modified_at"`
	}

	var entries []entry
	var totalSize int64
	var totalDirs int

	if recursive {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != dir { // count subdirs, not the root itself
					totalDirs++
				}
				return nil
			}

			// Skip symlinks that resolve outside the agent directory
			if d.Type()&fs.ModeSymlink != 0 {
				if checkSymlinkEscape(path, absAgentRoot) != nil {
					return nil
				}
			}

			info, err := d.Info()
			if err != nil {
				return err
			}

			rel, err := filepath.Rel(agentRoot, path)
			if err != nil {
				return err
			}

			totalSize += info.Size()
			entries = append(entries, entry{
				Name:       d.Name(),
				Path:       rel,
				IsDir:      false,
				SizeBytes:  info.Size(),
				ModifiedAt: info.ModTime().UTC(),
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("list files: %w", err)
		}
	} else {
		// Single level listing
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read directory: %w", err)
		}

		for _, d := range dirEntries {
			info, err := d.Info()
			if err != nil {
				continue
			}

			rel, err := filepath.Rel(agentRoot, filepath.Join(dir, d.Name()))
			if err != nil {
				continue
			}

			if d.IsDir() {
				totalDirs++
				entries = append(entries, entry{
					Name:       d.Name(),
					Path:       rel,
					IsDir:      true,
					SizeBytes:  0,
					ModifiedAt: info.ModTime().UTC(),
				})
			} else {
				totalSize += info.Size()
				entries = append(entries, entry{
					Name:       d.Name(),
					Path:       rel,
					IsDir:      false,
					SizeBytes:  info.Size(),
					ModifiedAt: info.ModTime().UTC(),
				})
			}
		}
	}

	totalFiles := 0
	for _, e := range entries {
		if !e.IsDir {
			totalFiles++
		}
	}

	result := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		result = append(result, map[string]any{
			"name":        e.Name,
			"path":        e.Path,
			"is_dir":      e.IsDir,
			"size_bytes":  e.SizeBytes,
			"modified_at": e.ModifiedAt.Format(time.RFC3339),
		})
	}

	return mcp.JSONResult(map[string]any{
		"entries":          result,
		"total_files":      totalFiles,
		"total_dirs":       totalDirs,
		"total_size_bytes": totalSize,
	})
}

// matchGlob matches a glob pattern against relPath (forward-slash path relative to agent root).
// Uses doublestar for full ** support (e.g. "**/*.py", "memory/**/*.md").
// If the pattern contains no '/', it is also matched against just the filename so that
// "*.md" finds .md files at any depth, not only at the root.
func matchGlob(pattern, relPath string) bool {
	// Normalize to forward slashes (relPath is built on Linux; safe on all targets).
	rel := filepath.ToSlash(relPath)
	base := path.Base(rel)

	// Full-path match (handles **, *, ?, [...]).
	if ok, _ := doublestar.Match(pattern, rel); ok {
		return true
	}

	// Basename fallback: "*.md" should match files at any depth.
	if !strings.Contains(pattern, "/") {
		if ok, _ := doublestar.Match(pattern, base); ok {
			return true
		}
	}

	return false
}

func (s *Service) toolFind(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	pattern, _ := args["pattern"].(string)
	subPath, _ := args["path"].(string)

	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	agentRoot := s.agentDir(agentID)
	dir := agentRoot
	if subPath != "" {
		safeDir, err := s.safeSearchPath(agentID, subPath)
		if err != nil {
			return nil, err
		}
		dir = safeDir
	}

	absAgentRoot, _ := filepath.Abs(agentRoot)

	type match struct {
		Path       string
		SizeBytes  int64
		ModifiedAt time.Time
	}

	var matches []match

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return mcp.JSONResult(map[string]any{
			"matches": []any{},
			"total":   0,
		})
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Skip symlinks that resolve outside the agent directory
		if d.Type()&fs.ModeSymlink != 0 {
			if checkSymlinkEscape(path, absAgentRoot) != nil {
				return nil
			}
		}

		rel, err := filepath.Rel(agentRoot, path)
		if err != nil {
			return nil
		}

		if !matchGlob(pattern, rel) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		matches = append(matches, match{
			Path:       rel,
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("find files: %w", err)
	}

	// Sort by modification time descending (newest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].ModifiedAt.After(matches[j].ModifiedAt)
	})

	result := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		result = append(result, map[string]any{
			"path":        m.Path,
			"size_bytes":  m.SizeBytes,
			"modified_at": m.ModifiedAt.Format(time.RFC3339),
		})
	}

	return mcp.JSONResult(map[string]any{
		"matches": result,
		"total":   len(result),
	})
}

func (s *Service) toolSearch(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	query, _ := args["query"].(string)
	searchPath, _ := args["path"].(string)
	glob, _ := args["glob"].(string)
	outputMode, _ := args["output_mode"].(string)
	contextLines := 3
	if cl, ok := args["context_lines"].(float64); ok {
		contextLines = int(cl)
	}
	if contextLines < 0 {
		contextLines = 0
	}
	maxResults := 50
	if mr, ok := args["max_results"].(float64); ok {
		maxResults = int(mr)
	}
	if outputMode == "" {
		outputMode = "content"
	}

	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	agentRoot := s.agentDir(agentID)
	dir := agentRoot
	if searchPath != "" {
		safeDir, err := s.safeSearchPath(agentID, searchPath)
		if err != nil {
			return nil, err
		}
		dir = safeDir
	}

	// If dir doesn't exist, return empty matches
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return s.emptySearchResult(outputMode)
	}

	queryLower := strings.ToLower(query)
	absAgentRoot, _ := filepath.Abs(agentRoot)

	type contentMatch struct {
		File    string
		Line    int
		Match   string
		Context string
	}

	var contentMatches []contentMatch
	fileMatchMap := make(map[string]int)
	filesOnlySet := make(map[string]struct{})
	totalMatches := 0

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if totalMatches >= maxResults {
			return filepath.SkipAll
		}

		// Skip symlinks that resolve outside the agent directory
		if d.Type()&fs.ModeSymlink != 0 {
			if checkSymlinkEscape(path, absAgentRoot) != nil {
				return nil
			}
		}

		rel, err := filepath.Rel(agentRoot, path)
		if err != nil {
			return nil
		}

		// Apply glob filter if provided
		if glob != "" && !matchGlob(glob, rel) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}

		allLines := strings.Split(string(data), "\n")
		// Remove trailing empty element from final newline
		if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
			allLines = allLines[:len(allLines)-1]
		}

		fileHasMatch := false
		for i, line := range allLines {
			if totalMatches >= maxResults {
				break
			}
			if !strings.Contains(strings.ToLower(line), queryLower) {
				continue
			}

			fileHasMatch = true
			totalMatches++
			fileMatchMap[rel]++

			if outputMode == "content" {
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				end := i + contextLines + 1
				if end > len(allLines) {
					end = len(allLines)
				}
				contextStr := strings.Join(allLines[start:end], "\n")

				contentMatches = append(contentMatches, contentMatch{
					File:    rel,
					Line:    i + 1,
					Match:   line,
					Context: contextStr,
				})
			}
		}

		if fileHasMatch {
			filesOnlySet[rel] = struct{}{}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search files: %w", err)
	}

	switch outputMode {
	case "files_only":
		files := make([]string, 0, len(filesOnlySet))
		for f := range filesOnlySet {
			files = append(files, f)
		}
		sort.Strings(files)
		return mcp.JSONResult(map[string]any{
			"files":       files,
			"total_files": len(files),
		})

	case "count":
		type countEntry struct {
			File  string
			Count int
		}
		counts := make([]countEntry, 0, len(fileMatchMap))
		for f, c := range fileMatchMap {
			counts = append(counts, countEntry{f, c})
		}
		sort.Slice(counts, func(i, j int) bool {
			return counts[i].File < counts[j].File
		})
		result := make([]map[string]any, 0, len(counts))
		total := 0
		for _, c := range counts {
			result = append(result, map[string]any{
				"file":  c.File,
				"count": c.Count,
			})
			total += c.Count
		}
		return mcp.JSONResult(map[string]any{
			"counts":        result,
			"total_matches": total,
		})

	default: // "content"
		if contentMatches == nil {
			contentMatches = []contentMatch{}
		}
		result := make([]map[string]any, 0, len(contentMatches))
		for _, m := range contentMatches {
			result = append(result, map[string]any{
				"file":    m.File,
				"line":    m.Line,
				"match":   m.Match,
				"context": m.Context,
			})
		}
		return mcp.JSONResult(map[string]any{
			"matches":       result,
			"total_matches": len(result),
		})
	}
}

func (s *Service) emptySearchResult(outputMode string) (*mcp.ToolResult, error) {
	switch outputMode {
	case "files_only":
		return mcp.JSONResult(map[string]any{
			"files":       []any{},
			"total_files": 0,
		})
	case "count":
		return mcp.JSONResult(map[string]any{
			"counts":        []any{},
			"total_matches": 0,
		})
	default:
		return mcp.JSONResult(map[string]any{
			"matches":       []any{},
			"total_matches": 0,
		})
	}
}

// countFileLines counts newlines in a file by streaming through it in chunks,
// avoiding loading the entire file into memory.
func countFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 32*1024)
	count := 0
	lastByte := byte('\n')
	hasContent := false

	for {
		n, err := f.Read(buf)
		if n > 0 {
			hasContent = true
			count += bytes.Count(buf[:n], []byte{'\n'})
			lastByte = buf[n-1]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}

	// If file has content but doesn't end with newline, count the last line
	if hasContent && lastByte != '\n' {
		count++
	}
	return count, nil
}
