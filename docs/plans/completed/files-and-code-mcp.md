# Files MCP

> **Status**: ✅ Shipped
> **Package**: `golang/internal/mcps/files/`
> **Endpoint**: `POST /mcp/files`

## What was built

A persistent per-agent workspace filesystem exposed via eight MCP tools. Each agent gets isolated storage at `{rootDir}/{agent-id}/` where it can read, write, edit, search, and delete files across sessions. The same directory is mounted into the Code Sandbox container at `/workspace/`, so files written by the Files MCP are immediately visible to Python code executed by `code/execute`, and vice versa.

## MCP tools

| Tool | Inputs | Outputs | File:line |
|------|--------|---------|-----------|
| `files/read` | `path`, `offset` (1-based), `limit` (0 = all) | `{content, total_lines, truncated}` | `service.go:330` |
| `files/write` | `path`, `content` | `{success, path, size_bytes}` | `service.go:400` |
| `files/edit` | `path`, `old_text`, `new_text`, `replace_all` | `{replacements}` | `service.go:432` |
| `files/append` | `path`, `content` | `{success, path, size_bytes}` | `service.go:493` |
| `files/delete` | `path`, `recursive` | `{deleted: path}` | `service.go:540` |
| `files/list` | `path?`, `recursive?` | `{entries, total_files, total_dirs, total_size_bytes}` | `service.go:592` |
| `files/find` | `pattern` (glob), `path?` | `{matches, total}` sorted newest-first | `service.go:762` |
| `files/search` | `query`, `path?`, `glob?`, `context_lines?`, `output_mode?`, `max_results?` | matches / files / counts | `service.go:857` |

All paths are relative to the agent workspace root. Single writes and appends are capped at 10 MB (`maxFileBytes` at `service.go:31`).

## Workspace layout

Root is configurable via `FilesMCPConfig.RootDir` (default `./data/workspace`). Each agent is sandboxed:

- **Agent identity**: middleware at `service.go:57-65` extracts and validates `X-Agent-ID`. Empty IDs or IDs containing `/`, `\`, or `..` are rejected.
- **Per-agent directory**: `agentDir(agentID)` returns `{rootDir}/{agentID}` (`service.go:96-97`).
- **Path validation**: every file operation goes through `safePath()` (`service.go:102-157`) which resolves the requested path and confirms it stays inside the agent's subtree. `checkSymlinkEscape` catches symlinks pointing outside the workspace.
- **Shared with Code Sandbox**: the same volume backs both MCPs, so writes from `files/write` are immediately readable from Python code inside the sandbox container.

Example: agent `alpha` calling `files/write` with `path=memory/notes.md` writes to `./data/workspace/alpha/memory/notes.md`. Agent `gamma` cannot see it.

## Key files

- `golang/internal/mcps/files/service.go` (~1,060 lines) - tool handlers and path safety
- `golang/internal/mcps/files/service_test.go` (~1,280 lines) - comprehensive test suite
- `golang/internal/config/config.go` - `FilesMCPConfig.RootDir`
- `golang/cmd/server/main.go` - wiring the Files MCP at `/mcp/files`

## Tests

35+ test functions, table-driven where relevant:

- **Path safety**: `TestValidateAgentID`, `TestSafePath`, `TestSafeSearchPath`, `TestSymlinkEscapeRead`, `TestSymlinkEscapeWrite`
- **Core tools**: `TestToolRead_*` (basic, offset, limit, combined), `TestToolWrite`, `TestToolEdit`, `TestToolAppend`, `TestToolDelete`, `TestToolList_*`, `TestToolFind`, `TestToolSearch_*` (content, context lines, files-only, count)
- **Edge cases**: empty files, missing trailing newline, glob `**`, case-insensitive search, output mode variations

Run with `go test -v ./golang/internal/mcps/files/`.

## Notes

- **10 MB cap** on single writes/appends prevents runaway disk / RAM usage.
- **`X-Agent-ID` is required** - the MCP fails the request if missing.
- **Case-insensitive text search** by default in `files/search`.
- **Glob support** via `github.com/bmatcuk/doublestar/v4` - `**` for recursive matches.
- **Sorted results**: `files/find` sorts newest-first by mtime, `files/list` sorts by name.
- **Search output modes**: `content` (matches with context), `files_only` (unique files), `count` (per-file match counts).
- **No separate Code MCP spec**: `code/execute` lives in the Code Sandbox MCP (`docs/plans/completed/code-sandbox-mcp.md`). The Files MCP and the Code Sandbox share the same workspace volume, which is what makes the "read a file from Files, process it in Python, write result back" pattern work end-to-end.
