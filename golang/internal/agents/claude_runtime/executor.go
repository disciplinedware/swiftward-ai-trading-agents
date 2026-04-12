// Package claude_runtime orchestrates Claude Code as a child process.
// Borrows the executor pattern from ralphex (github.com/umputun/ralphex).
//
// Uses the stream-json bidirectional protocol (same as the official Claude Agent SDK):
//   - stdin:  NDJSON events (user messages, control responses)
//   - stdout: NDJSON events (system init, stream events, assistant messages, results)
package claude_runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/observability"
)

// SessionResult holds the outcome of a single Claude session.
type SessionResult struct {
	Output      string        // full accumulated text
	Error       error         // process-level error, nil on normal exit
	Elapsed     time.Duration // wall-clock session duration
	ToolCalls   int           // number of tool calls made
	OutputChars int           // total text output characters
}

// ToolEvent is emitted when Claude calls a tool.
type ToolEvent struct {
	Name  string // tool name (e.g. "Bash", "Read", "Edit", "Agent")
	Input string // tool arguments preview
}

// AuthError is returned when Claude outputs an authentication error pattern.
type AuthError struct{ Message string }

func (e *AuthError) Error() string { return "claude auth error: " + e.Message }

// RateLimitError is returned when Claude outputs a rate-limit pattern.
type RateLimitError struct{ Message string }

func (e *RateLimitError) Error() string { return "claude rate limit: " + e.Message }

// Executor runs Claude Code as a child process with the stream-json bidirectional protocol.
type Executor struct {
	// Command is the claude binary name/path. Defaults to "claude".
	Command string
	// Model is the Claude model ID to pass via --model. "" = claude's default.
	Model string
	// Continue resumes the most recent conversation instead of starting fresh.
	Continue bool
	// IdleTimeout kills the session if no output is received for this duration. Zero = disabled.
	IdleTimeout time.Duration
	// OutputHandler is called for each text chunk (for real-time display). May be nil.
	OutputHandler func(text string)
	// EventHandler is called for each stream event type (e.g. "thinking", "tool_use").
	// Used to trigger typing indicators. May be nil.
	EventHandler func(eventType string)
	// ToolHandler is called when Claude calls a tool or receives a tool result. May be nil.
	ToolHandler func(ToolEvent)
	// TurnDoneCh is signaled when Claude emits a result/success event, indicating
	// the process finished all work and is about to exit. Buffered(1), non-blocking send.
	TurnDoneCh chan struct{}

	log *zap.Logger

	// stdinPipe is the write end of the stdin pipe to the Claude process.
	// Protected by stdinMu for concurrent InjectMessage calls.
	stdinMu   sync.Mutex
	stdinPipe io.WriteCloser
}

// NewExecutor creates an Executor with the given logger.
// IdleTimeout defaults to 0 (disabled) - Claude may think for extended periods.
func NewExecutor(log *zap.Logger) *Executor {
	return &Executor{
		Command: "claude",
		log:     log,
	}
}

// Run spawns claude with the given prompt and returns the session result.
// The prompt is sent as a stream-json user message via stdin.
// Blocks until the session completes or ctx is cancelled.
func (e *Executor) Run(ctx context.Context, prompt string) SessionResult {
	start := time.Now()
	cmd := e.command()

	// Set up stdin pipe - kept open for message injection.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return SessionResult{Error: fmt.Errorf("create stdin pipe: %w", err), Elapsed: time.Since(start)}
	}
	cmd.Stdin = stdinR

	e.stdinMu.Lock()
	e.stdinPipe = stdinW
	e.stdinMu.Unlock()

	// Merge stdout and stderr into a single pipe.
	pr, pw, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		return SessionResult{Error: fmt.Errorf("create stdout pipe: %w", err), Elapsed: time.Since(start)}
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	// Idle timeout: kills stuck sessions that produce no output.
	// Derives a cancellable context; timer resets on each output line.
	// Stopped on result/success (session done, process waiting for stdin - not stuck).
	execCtx := ctx
	touch := func() {}
	stopIdleTimer := func() {}
	if e.IdleTimeout > 0 {
		var idleCancel context.CancelFunc
		execCtx, idleCancel = context.WithCancel(ctx)
		defer idleCancel()
		var timerDisarmed atomic.Bool
		timer := time.AfterFunc(e.IdleTimeout, func() {
			if timerDisarmed.Swap(true) {
				return // stopIdleTimer already disarmed us
			}
			e.log.Warn("Idle timeout reached, killing session")
			idleCancel()
		})
		defer timer.Stop()
		touch = func() { timer.Reset(e.IdleTimeout) }
		stopIdleTimer = func() {
			if timerDisarmed.Swap(true) {
				return // timer callback already fired
			}
			timer.Stop()
		}
	}

	if err := cmd.Start(); err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = pr.Close()
		_ = pw.Close()
		return SessionResult{Error: fmt.Errorf("start claude: %w", err), Elapsed: time.Since(start)}
	}

	// Close read end of stdin and write end of stdout in parent.
	_ = stdinR.Close()
	_ = pw.Close()

	cleanup := newProcGroupCleanup(cmd, execCtx.Done(), e.log)
	defer func() {
		_ = pr.Close()
		e.stdinMu.Lock()
		if e.stdinPipe != nil {
			_ = e.stdinPipe.Close()
			e.stdinPipe = nil
		}
		e.stdinMu.Unlock()
	}()

	// Send initialize handshake (same as official Claude Agent SDK).
	if err := e.writeInitialize(); err != nil {
		e.log.Error("Failed to send initialize", zap.Error(err))
		return SessionResult{Error: fmt.Errorf("send initialize: %w", err), Elapsed: time.Since(start)}
	}

	// Send the initial prompt as a user message.
	if err := e.writeUserMessage(prompt); err != nil {
		e.log.Error("Failed to send initial prompt", zap.Error(err))
		return SessionResult{Error: fmt.Errorf("send prompt: %w", err), Elapsed: time.Since(start)}
	}

	result := e.parseStream(execCtx, pr, touch, stopIdleTimer)
	waitErr := cleanup.Wait()
	result.Elapsed = time.Since(start)

	// Propagate non-zero exit code only if the process died on its own.
	// When we kill intentionally (idle timeout, recycle, shutdown), execCtx is
	// cancelled first and the resulting SIGTERM exit code is expected.
	if waitErr != nil && result.Error == nil && execCtx.Err() == nil {
		result.Error = fmt.Errorf("claude process exited: %w", waitErr)
	}

	// Propagate external shutdown (parent ctx cancelled = SIGINT/SIGTERM to us).
	if result.Error == nil && ctx.Err() != nil {
		result.Error = fmt.Errorf("session killed: %w", ctx.Err())
	}

	// Classify errors from output patterns (auth, rate limit, etc.)
	if result.Error == nil {
		result.Error = classifyOutput(result.Output)
	}

	return result
}

// InjectMessage sends a user message to the running Claude session via stdin.
// Thread-safe. Returns error if no session is active.
func (e *Executor) InjectMessage(content string) error {
	return e.writeUserMessage(content)
}


// writeInitialize sends the initialize control request (same as official Claude Agent SDK).
func (e *Executor) writeInitialize() error {
	msg := map[string]any{
		"type":       "control_request",
		"request_id": "req_1_init",
		"request": map[string]any{
			"subtype": "initialize",
			"hooks":   nil,
			"agents":  nil,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal initialize: %w", err)
	}
	data = append(data, '\n')

	e.stdinMu.Lock()
	defer e.stdinMu.Unlock()
	if e.stdinPipe == nil {
		return fmt.Errorf("no active session")
	}
	_, err = e.stdinPipe.Write(data)
	return err
}

// writeUserMessage writes a stream-json user message to stdin.
func (e *Executor) writeUserMessage(content string) error {
	msg := map[string]any{
		"type":               "user",
		"session_id":         "",
		"parent_tool_use_id": nil,
		"message": map[string]any{
			"role":    "user",
			"content": content,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal user message: %w", err)
	}
	data = append(data, '\n')

	e.stdinMu.Lock()
	defer e.stdinMu.Unlock()
	if e.stdinPipe == nil {
		return fmt.Errorf("no active session")
	}
	if _, err := e.stdinPipe.Write(data); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

// command builds the exec.Cmd for claude using the stream-json protocol.
func (e *Executor) command() *exec.Cmd {
	name := e.Command
	if name == "" {
		name = "claude"
	}
	args := []string{
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
	}
	if e.Continue {
		args = append(args, "--continue")
	}
	if e.Model != "" {
		args = append(args, "--model", e.Model)
	}
	cmd := exec.Command(name, args...) //nolint:noctx // process group kill handles cancellation
	// Filter ANTHROPIC_API_KEY (claude uses its own auth) and CLAUDECODE (prevents nested session errors).
	// Remap CLAUDE_AGENT__* prefixed vars to standard names for Claude CLI and HTTP clients.
	cmd.Env = remapEnv(filterEnv(os.Environ(), "ANTHROPIC_API_KEY", "CLAUDECODE"))
	// New process group: allows killing all descendants (node subagents, MCP servers).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// streamEvent mirrors the JSON events emitted by Claude's stream-json protocol.
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Message struct {
		Content []contentBlock `json:"content"`
	} `json:"message"`
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	Result json.RawMessage `json:"result"`
}

// contentBlock represents a content block in an assistant message.
// Can be text, tool_use, or tool_result.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`          // tool_use: call ID
	Name      string          `json:"name,omitempty"`        // tool_use: tool name
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use: arguments
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result: reference
	Content   json.RawMessage `json:"content,omitempty"`     // tool_result: result data
}

// parseStream reads the NDJSON output stream from Claude.
// Signals TurnDoneCh on result/success (Working -> Idle). Does NOT close stdin.
// stopIdleTimer is called on result/success to prevent the idle timeout from
// killing a healthy process that's just waiting for stdin.
func (e *Executor) parseStream(ctx context.Context, r io.Reader, touch func(), stopIdleTimer func()) SessionResult {
	var out strings.Builder
	var toolCalls int

	err := readLines(ctx, r, func(line string) {
		touch()
		if line == "" {
			return
		}

		var ev streamEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			return
		}

		// Log and notify on event types.
		if ev.Type != "" {
			eventType := ev.Type
			if ev.Type == "stream_event" {
				if ev.Event.Delta.Type != "" {
					eventType = ev.Event.Delta.Type
				} else if ev.Event.Type != "" {
					eventType = ev.Event.Type
				}
			}
			// Log all event types at DEBUG so we can discover what Claude Code sends
			// (subagents, tool results, thinking, etc.)
			if eventType != "text_delta" { // skip text_delta - too noisy
				e.log.Debug("stream event",
					zap.String("type", ev.Type),
					zap.String("event_type", eventType),
					zap.String("subtype", ev.Subtype),
				)
			}
			if e.EventHandler != nil {
				e.EventHandler(eventType)
			}

			// result/success = Claude Code finished all work. Process waits for stdin.
			// Stop idle timer - process is not stuck, just idle.
			if eventType == "result" && ev.Subtype == "success" {
				stopIdleTimer()
				if e.TurnDoneCh != nil {
					e.log.Info("Turn completed, session idle")
					select {
					case e.TurnDoneCh <- struct{}{}:
					default:
					}
				}
			}

		}

		// Extract tool calls from assistant messages and tool results from user messages.
		if ev.Type == "assistant" {
			for _, c := range ev.Message.Content {
				if c.Type == "tool_use" {
					toolCalls++
					if e.ToolHandler != nil {
						e.ToolHandler(ToolEvent{
							Name:  c.Name,
							Input: observability.LogPreview(string(c.Input), 120),
						})
					}
				}
			}
		}
		if ev.Type == "user" {
			for _, c := range ev.Message.Content {
				if c.Type == "tool_result" && e.ToolHandler != nil {
					e.ToolHandler(ToolEvent{
						Name:  "result:" + c.ToolUseID,
						Input: observability.LogPreview(string(c.Content), 200),
					})
				}
			}
		}

		// NEVER close stdin on result event or anywhere else during the session.
		// Reason: closing stdin kills the Claude process immediately, making it
		// impossible to inject Telegram/alert messages into the Idle session.
		// The process must stay alive in Idle state (stdin open, waiting for input).
		// Session termination is always via context cancel -> SIGTERM (procGroupCleanup).
		// The defer in Run() closes stdin as part of post-mortem cleanup only.
		// See docs/claude-runtime-state-machine.md.

		text := e.extractText(&ev)
		if text == "" {
			return
		}

		out.WriteString(text)
		if e.OutputHandler != nil {
			e.OutputHandler(text)
		}
	})

	output := out.String()
	return SessionResult{
		Output:      output,
		Error:       err,
		ToolCalls:   toolCalls,
		OutputChars: len(output),
	}
}

// extractText pulls displayable text from a stream event.
func (e *Executor) extractText(ev *streamEvent) string {
	switch ev.Type {
	case "assistant":
		var parts []string
		for i := range ev.Message.Content {
			if ev.Message.Content[i].Type == "text" && ev.Message.Content[i].Text != "" {
				parts = append(parts, ev.Message.Content[i].Text)
			}
		}
		return strings.Join(parts, "")

	case "stream_event":
		// Stream events wrap an inner event.
		if ev.Event.Delta.Type == "text_delta" {
			return ev.Event.Delta.Text
		}

	case "result":
		if len(ev.Result) == 0 {
			return ""
		}
		var s string
		if json.Unmarshal(ev.Result, &s) == nil {
			return "" // session summary - content already streamed
		}
		var obj struct {
			Output string `json:"output"`
		}
		if json.Unmarshal(ev.Result, &obj) == nil {
			return obj.Output
		}
	}
	return ""
}

// classifyOutput inspects the full output for known error patterns.
func classifyOutput(output string) error {
	lower := strings.ToLower(output)

	authPatterns := []string{
		"authentication required",
		"please login",
		"invalid api key",
		"401 unauthorized",
		"credentials expired",
	}
	for _, p := range authPatterns {
		if strings.Contains(lower, p) {
			return &AuthError{Message: p}
		}
	}

	limitPatterns := []string{
		"rate limit exceeded",
		"too many requests",
		"you've hit your limit",
		"usage limit reached",
		"429 too many",
	}
	for _, p := range limitPatterns {
		if strings.Contains(lower, p) {
			return &RateLimitError{Message: p}
		}
	}

	return nil
}


// remapEnv maps CLAUDE_AGENT__* prefixed env vars to standard names for the Claude CLI subprocess.
// HTTP_PROXY/HTTPS_PROXY/NO_PROXY are set directly on the container (not prefixed),
// so both the Go harness and Claude CLI see them without remapping.
func remapEnv(env []string) []string {
	remap := map[string]string{
		"CLAUDE_AGENT__ANTHROPIC_BASE_URL": "ANTHROPIC_BASE_URL",
		"CLAUDE_AGENT__CLAUDE_HOME":        "CLAUDE_CONFIG_DIR",
	}
	out := make([]string, 0, len(env)+len(remap))
	for _, e := range env {
		out = append(out, e)
		for prefixed, standard := range remap {
			if strings.HasPrefix(e, prefixed+"=") {
				val := e[len(prefixed)+1:]
				out = append(out, standard+"="+val)
			}
		}
	}
	return out
}

func filterEnv(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		skip := false
		for _, k := range keys {
			if strings.HasPrefix(e, k+"=") {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, e)
		}
	}
	return out
}

// readLines reads lines from r, calling handler for each, until EOF or ctx cancellation.
// EOF takes precedence over context cancel to avoid masking a clean process exit.
func readLines(ctx context.Context, r io.Reader, handler func(string)) error {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			handler(strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read lines: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("read lines: %w", ctx.Err())
		default:
		}
	}
}
