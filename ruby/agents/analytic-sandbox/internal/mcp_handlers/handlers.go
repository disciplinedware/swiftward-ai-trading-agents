package mcp_handlers

import (
	"analytic-sandbox/internal/sandbox"
	"analytic-sandbox/internal/session"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mitchellh/mapstructure"
)

type Handlers struct {
	srv  *server.MCPServer
	sess *session.Session
	dm   *sandbox.DockerManager
	mu   sync.Mutex
}

func NewHandlers(srv *server.MCPServer, sess *session.Session, dm *sandbox.DockerManager) *Handlers {
	return &Handlers{
		srv:  srv,
		sess: sess,
		dm:   dm,
	}
}

type ListFilesParams struct {
	Path string `json:"path"`
}

type ReadFileParams struct {
	Path       string `json:"path"`
	Offset     int    `json:"offset"`
	Count      int    `json:"count,omitempty"`
	WidthLimit int    `json:"width_limit,omitempty"`
	Hexdump    bool   `json:"hexdump,omitempty"`
}

type WriteFileParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type EditFileParams struct {
	Path         string `json:"path"`
	Mode         string `json:"mode"`
	Target       string `json:"target"`
	Content      string `json:"content"`
	SearchBlock  string `json:"search_block,omitempty"`
	ReplaceBlock string `json:"replace_block,omitempty"`
}

type GrepFileParams struct {
	Path          string `json:"path"`
	Pattern       string `json:"pattern"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	ContextLines  int    `json:"context_lines,omitempty"`
	WidthLimit    int    `json:"width_limit,omitempty"`
	MaxMatches    int    `json:"max_matches,omitempty"`
}

type GetFileInfoParams struct {
	Path string `json:"path"`
}

type ShellParams struct {
	Command string `json:"command"`
}

func (h *Handlers) Register() {
	h.srv.AddTool(mcp.NewTool(
		"list_files",
		mcp.WithDescription("Lists all files and directories in the workspace. Defaults to root if path is omitted."),
		mcp.WithString("path", mcp.Description("Directory path (optional, defaults to root)")),
	), h.handleListFiles)

	h.srv.AddTool(mcp.NewTool(
		"read_file",
		mcp.WithDescription("Reads lines from a file with optional pagination. Results include line numbers. For binary files use hexdump mode."),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path relative to workspace root.")),
		mcp.WithNumber("offset", mcp.Description("Start line (text) or start byte (hexdump). Default: 0.")),
		mcp.WithNumber("count", mcp.Description("Number of lines (text) or bytes (hexdump) to read. Default: 100.")),
		mcp.WithNumber("width_limit", mcp.Description("Max characters per line (text only). Default: 1000.")),
		mcp.WithBoolean("hexdump", mcp.Description("If true, read in binary hexdump format.")),
	), h.handleReadFile)

	h.srv.AddTool(mcp.NewTool(
		"write_file",
		mcp.WithDescription("Creates or overwrites a file in the workspace. Useful for creating scripts, configuration files, or data files."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Filename (e.g. 'script.py', 'config.json')")),
		mcp.WithString("content", mcp.Required(), mcp.Description("File content")),
	), h.handleWriteFile)

	h.srv.AddTool(mcp.NewTool(
		"edit_file",
		mcp.WithDescription("Advanced: Edits a file. Preferred mode: 'block' (search for search_block and replace with replace_block). Also supports 'line' (replace line number), 'string' (literal replacement), and 'regex' (regex replacement). Requires unique search_block for 'block' mode. Syntax validation is performed for .py, .go, .rb."),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
		mcp.WithString("mode", mcp.Required(), mcp.Description("Edit mode: 'block', 'line', 'string', or 'regex'")),
		mcp.WithString("target", mcp.Description("For 'line' (number), 'string' (literal), 'regex' (pattern)")),
		mcp.WithString("content", mcp.Description("New content (for 'line', 'string', 'regex' modes)")),
		mcp.WithString("search_block", mcp.Description("Exact block to find (for 'block' mode)")),
		mcp.WithString("replace_block", mcp.Description("New block to insert (for 'block' mode)")),
	), h.handleEditFile)

	h.srv.AddTool(mcp.NewTool(
		"grep_file",
		mcp.WithDescription("Searches for a regex pattern in a file and returns matches with surrounding context lines. Results include line numbers."),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regex pattern to search for")),
		mcp.WithBoolean("case_sensitive", mcp.Description("Whether search is case sensitive")),
		mcp.WithNumber("context_lines", mcp.Description("Number of lines of context")),
		mcp.WithNumber("width_limit", mcp.Description("Max characters per line")),
		mcp.WithNumber("max_matches", mcp.Description("Max matches to return")),
	), h.handleGrepFile)

	h.srv.AddTool(mcp.NewTool(
		"get_file_info",
		mcp.WithDescription("Returns comprehensive metadata about a file including size, MIME type, and previews (Head/Tail/Random). Use this first to understand the input file."),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path")),
	), h.handleGetFileInfo)

	h.srv.AddTool(mcp.NewTool(
		"shell",
		mcp.WithDescription(fmt.Sprintf("Executes a shell command. WORKSPACE is at /app. ISOLATION: %d CPU(s), %dMB RAM, %dMB Disk, %ds timeout. LANGUAGES: Python 3.12 (python3), Ruby 3.2 (ruby), Go 1.22 (go run). INSTALLED TOOLS:\n"+
			"- duckdb: SQL engine for CSV/Parquet/JSON/JSONL\n"+
			"- xsv: CSV toolkit (slice, select, search, stats)\n"+
			"- polars + pandas + numpy + openpyxl: Python dataframes\n"+
			"- parallel: Run jobs in parallel using all CPUs\n"+
			"- datamash: Column statistics\n"+
			"- ripgrep (rg): Fast text search\n"+
			"- fd (fdfind): Fast file search\n"+
			"- pv: Monitor pipe progress\n"+
			"- zstd: Fast compression\n"+
			"- xlsx2csv: Convert XLSX to CSV\n"+
			"- jq: JSON processor\n"+
			"- pandoc: Document converter\n"+
			"- poppler-utils (pdftotext, pdfinfo): PDF tools\n"+
			"- ffmpeg: Audio/video processor\n"+
			"- imagemagick (convert, identify): Image tools\n"+
			"- shellcheck: Shell script linter\n"+
			"- sqlite3: SQLite database\n"+
			"- bc: Arbitrary precision calculator",
			h.sess.CpuCount, h.sess.MemoryMB, h.sess.DiskMB, h.sess.TimeoutSeconds)),
		mcp.WithString("command", mcp.Required(), mcp.Description("Shell command to execute")),
	), h.handleShell)
}

func (h *Handlers) parseParams(args any, out interface{}) error {
	config := &mapstructure.DecoderConfig{
		Metadata:         nil,
		Result:           out,
		WeaklyTypedInput: true,
		TagName:          "json",
	}
	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}
	return decoder.Decode(args)
}

func (h *Handlers) logCall(tool string, input any, res *mcp.CallToolResult) {
	isError := false
	if res != nil {
		isError = res.IsError
	}
	log.Printf("[TOOL_CALL] Tool: %s, Args: %+v, Error: %v", tool, input, isError)

	// Update session metadata/history
	if h.sess != nil && h.sess.Manager != nil {
		// We launch this in a goroutine to not block the response?
		// Or strictly synchronous to ensure order?
		// Synchronous is safer for now to avoid race conditions on the file write if specific order matters,
		// though Manager implementation has a lock.
		// Given it's a file write, it might add a few ms.
		if err := h.sess.Manager.AddHistoryEvent(h.sess.ID, tool, input, isError); err != nil {
			log.Printf("Failed to update session metadata: %v", err)
		}
	}
}

// ResolvePath resolves a user-provided path to an absolute host path.
func (h *Handlers) ResolvePath(userPath string) (string, error) {
	cleanPath := strings.TrimSpace(userPath)

	if cleanPath == "" {
		return "", fmt.Errorf("path is required")
	}

	// Handle /app alias for workspace root
	if cleanPath == "/app" {
		cleanPath = "."
	} else if strings.HasPrefix(cleanPath, "/app/") {
		cleanPath = strings.TrimPrefix(cleanPath, "/app/")
	}

	fullPath := filepath.Join(h.sess.HostWorkDir, cleanPath)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	if !strings.HasPrefix(absPath, h.sess.HostWorkDir) {
		return "", fmt.Errorf("access denied: path escapes workspace")
	}

	return absPath, nil
}

func (h *Handlers) sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Replace host work dir with /app
	if h.sess != nil && h.sess.HostWorkDir != "" {
		msg = strings.ReplaceAll(msg, h.sess.HostWorkDir, "/app")
	}
	return msg
}

func (h *Handlers) handleListFiles(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params ListFilesParams
	if err := h.parseParams(request.Params.Arguments, &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	if params.Path == "" {
		params.Path = "."
	}

	hostPath, err := h.ResolvePath(params.Path)
	if err != nil {
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	entries, err := os.ReadDir(hostPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list directory: %s", h.sanitizeError(err))), nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Listing for %s:\n", params.Path))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue // Hide hidden files like .outputs
		}

		info, _ := entry.Info()
		size := info.Size()

		kind := "[FILE]"
		if entry.IsDir() {
			kind = "[DIR] "
		}
		result.WriteString(fmt.Sprintf("%s %s (%d bytes)\n", kind, name, size))
	}

	res := mcp.NewToolResultText(result.String())
	h.logCall("list_files", params, res)
	return res, nil
}

func (h *Handlers) handleReadFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params ReadFileParams
	if err := h.parseParams(request.Params.Arguments, &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	hostPath, err := h.ResolvePath(params.Path)
	if err != nil {
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	if params.Hexdump {
		if params.Count == 0 {
			params.Count = 512
		}
		if params.Count > 4096 {
			params.Count = 4096
		}

		hexOutput, err := readBytesAsHex(hostPath, int64(params.Offset), int64(params.Count))
		if err != nil {
			return mcp.NewToolResultError(h.sanitizeError(err)), nil
		}

		res := mcp.NewToolResultText(hexOutput)
		h.logCall("read_file", params, res)
		return res, nil
	}

	// Set default count based on file extension if not provided
	if params.Count == 0 {
		ext := strings.ToLower(filepath.Ext(hostPath))
		if ext == ".py" || ext == ".go" || ext == ".rb" {
			params.Count = 300
		} else {
			params.Count = 20
		}
	}

	result, err := h.getLines(hostPath, params.Offset, params.Count, params.WidthLimit)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("File not found: %s", params.Path)), nil
		}
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	res := mcp.NewToolResultText(result)
	h.logCall("read_file", params, res)
	return res, nil
}

func (h *Handlers) handleWriteFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params WriteFileParams
	if err := h.parseParams(request.Params.Arguments, &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	hostPath, err := h.ResolvePath(params.Path)
	if err != nil {
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	h.sess.ExecMutex.Lock()
	defer h.sess.ExecMutex.Unlock()

	// Syntax validation for supported languages
	if err := h.validateSyntax(ctx, hostPath, params.Content); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := os.WriteFile(hostPath, []byte(params.Content), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write file: %s", h.sanitizeError(err))), nil
	}

	res := mcp.NewToolResultText(fmt.Sprintf("Successfully wrote %d bytes to %s", len(params.Content), params.Path))
	h.logCall("write_file", params, res)
	return res, nil
}

func (h *Handlers) handleEditFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params EditFileParams
	if err := h.parseParams(request.Params.Arguments, &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	hostPath, err := h.ResolvePath(params.Path)
	if err != nil {
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	h.sess.ExecMutex.Lock()
	defer h.sess.ExecMutex.Unlock()

	content, err := os.ReadFile(hostPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read file: %s", h.sanitizeError(err))), nil
	}

	fullContent := string(content)
	newContent := ""
	mode := strings.ToLower(params.Mode)

	if mode == "block" {
		if params.SearchBlock == "" {
			return mcp.NewToolResultError("search_block is required for mode 'block'"), nil
		}
		// Count occurrences to ensure uniqueness
		count := strings.Count(fullContent, params.SearchBlock)
		if count == 0 {
			return mcp.NewToolResultError("search_block not found in file"), nil
		}
		if count > 1 {
			return mcp.NewToolResultError(fmt.Sprintf("search_block found %d times, must be unique", count)), nil
		}

		// Find indentation of the first line of search_block
		idx := strings.Index(fullContent, params.SearchBlock)
		lineStart := strings.LastIndex(fullContent[:idx], "\n") + 1
		prefix := fullContent[lineStart:idx]

		searchLeadingWhitespace := ""
		for _, r := range params.SearchBlock {
			if r == ' ' || r == '\t' {
				searchLeadingWhitespace += string(r)
			} else {
				break
			}
		}
		indent := prefix + searchLeadingWhitespace

		// Correct indentation for replace_block
		replaceLines := strings.Split(params.ReplaceBlock, "\n")
		// Find common prefix indentation in replace_block to strip it
		var commonPrefix string
		if len(replaceLines) > 0 {
			firstReplaceLine := replaceLines[0]
			for _, r := range firstReplaceLine {
				if r == ' ' || r == '\t' {
					commonPrefix += string(r)
				} else {
					break
				}
			}
		}

		for i, line := range replaceLines {
			var trimmed string
			if strings.HasPrefix(line, commonPrefix) {
				trimmed = strings.TrimPrefix(line, commonPrefix)
			} else {
				trimmed = strings.TrimLeft(line, " \t")
			}

			if strings.TrimSpace(line) == "" {
				replaceLines[i] = "" // Keep empty lines empty
			} else {
				replaceLines[i] = indent + trimmed
			}
		}
		finalReplaceBlock := strings.Join(replaceLines, "\n")

		// Ensure no "dirty" merging by adding newlines if needed
		// (Simplified check: ensure block starts/ends with what seems like a safe boundary if it's replacing a block)
		// But usually search-and-replace of whole blocks is safe.

		newContent = strings.Replace(fullContent, params.SearchBlock, finalReplaceBlock, 1)
	} else if mode == "line" {
		lines := strings.Split(fullContent, "\n")
		targetLine, err := strconv.Atoi(params.Target)
		if err != nil {
			return mcp.NewToolResultError("Target must be a line number for mode 'line'"), nil
		}
		if targetLine < 0 || targetLine >= len(lines) {
			return mcp.NewToolResultError(fmt.Sprintf("Line number %d out of bounds (0-%d)", targetLine, len(lines)-1)), nil
		}
		lines[targetLine] = params.Content
		newContent = strings.Join(lines, "\n")
	} else if mode == "string" {
		newContent = strings.ReplaceAll(fullContent, params.Target, params.Content)
	} else if mode == "regex" {
		re, err := regexp.Compile(params.Target)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid regex: %s", h.sanitizeError(err))), nil
		}
		newContent = re.ReplaceAllString(fullContent, params.Content)
	} else {
		return mcp.NewToolResultError("Invalid mode. Use 'block', 'line', 'string', or 'regex'"), nil
	}

	// Syntax validation
	if err := h.validateSyntax(ctx, hostPath, newContent); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Syntax validation failed and changes were NOT applied:\n%v", h.sanitizeError(err))), nil
	}

	if err := os.WriteFile(hostPath, []byte(newContent), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write file: %s", h.sanitizeError(err))), nil
	}

	// Generate Diff
	diff, err := h.diffContent(fullContent, newContent)
	msg := fmt.Sprintf("Successfully edited %s", params.Path)
	if err == nil && diff != "" {
		msg += fmt.Sprintf("\n\nChanges:\n%s", diff)
	}

	res := mcp.NewToolResultText(msg)
	h.logCall("edit_file", params, res)
	return res, nil
}

// diffContent generates a unified diff between old and new content
func (h *Handlers) diffContent(oldContent, newContent string) (string, error) {
	// Create temp files
	oldFile, err := os.CreateTemp("", "mcp_diff_old_")
	if err != nil {
		return "", err
	}
	defer os.Remove(oldFile.Name())
	defer oldFile.Close()

	newFile, err := os.CreateTemp("", "mcp_diff_new_")
	if err != nil {
		return "", err
	}
	defer os.Remove(newFile.Name())
	defer newFile.Close()

	if _, err := oldFile.WriteString(oldContent); err != nil {
		return "", err
	}
	if _, err := newFile.WriteString(newContent); err != nil {
		return "", err
	}

	// Run diff command
	// diff -u --label Original --label Modified old new
	cmd := exec.Command("diff", "-u", "--label", "Original", "--label", "Modified", oldFile.Name(), newFile.Name())
	out, err := cmd.CombinedOutput()

	// diff returns exit code 1 if differences are found, which is NOT an error for us.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// Differences found, this is good.
				return string(out), nil
			}
		}
		// Real error or exit code > 1
		return "", err
	}
	// Exit code 0 means no diff
	return "", nil
}

func (h *Handlers) handleGrepFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params GrepFileParams
	if err := h.parseParams(request.Params.Arguments, &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	hostPath, err := h.ResolvePath(params.Path)
	if err != nil {
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	result, err := h.grep(hostPath, params.Pattern, params.CaseSensitive, params.ContextLines, params.MaxMatches, params.WidthLimit)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("File not found: %s", params.Path)), nil
		}
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	res := mcp.NewToolResultText(result)
	h.logCall("grep_file", params, res)
	return res, nil
}

func (h *Handlers) handleGetFileInfo(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params GetFileInfoParams
	if err := h.parseParams(request.Params.Arguments, &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	hostPath, err := h.ResolvePath(params.Path)
	if err != nil {
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	result, err := h.getFileStatistics(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("File not found: %s", params.Path)), nil
		}
		return mcp.NewToolResultError(h.sanitizeError(err)), nil
	}

	res := mcp.NewToolResultText(result)
	h.logCall("get_file_info", params, res)
	return res, nil
}

func (h *Handlers) handleShell(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params ShellParams
	if err := h.parseParams(request.Params.Arguments, &params); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid parameters: %v", err)), nil
	}

	// Wrap command in sh -c and inject environment variables
	fullCmd := h.buildShellCommand(params)

	h.sess.ExecMutex.Lock()
	defer h.sess.ExecMutex.Unlock()

	timeout := time.Duration(h.sess.TimeoutSeconds) * time.Second

	// Execute
	startTime := time.Now()
	outputDir := filepath.Join(h.sess.HostWorkDir, ".outputs")
	execRes, err := h.dm.Exec(ctx, h.sess.ContainerID, fullCmd, timeout, outputDir)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	duration := time.Since(startTime)

	// Format output
	warning := ""
	if duration >= timeout-1*time.Second {
		warning = fmt.Sprintf(" (NOTE: seems your command is timed out, we have %d seconds timeout, optimize your code)", h.sess.TimeoutSeconds)
	}

	output := fmt.Sprintf("Exit Code: %d\nExecution Time: %.2fs%s\n\nSTDOUT:\n%s\n\nSTDERR:\n%s",
		execRes.ExitCode, duration.Seconds(), warning, execRes.Stdout, execRes.Stderr)

	var res *mcp.CallToolResult
	if execRes.ExitCode != 0 {
		res = mcp.NewToolResultError(output)
	} else {
		res = mcp.NewToolResultText(output)
	}

	h.logCall("shell", params, res)
	return res, nil
}

func (h *Handlers) buildShellCommand(params ShellParams) []string {
	return []string{"sh", "-c", params.Command}
}
