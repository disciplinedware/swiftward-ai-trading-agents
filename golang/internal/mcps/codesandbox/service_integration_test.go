//go:build integration

package codesandbox

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

const testSandboxImage = "sandbox-python:local"

// execResult is the parsed payload returned by code/execute.
type execResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker not available:", err)
	}
}

func newIntegrationSvc(t *testing.T) *Service {
	t.Helper()
	skipIfNoDocker(t)
	if err := exec.Command("docker", "image", "inspect", testSandboxImage).Run(); err != nil {
		t.Skipf("sandbox image %q not found - run: make sandbox-build", testSandboxImage)
	}
	svc := &Service{
		log:            zaptest.NewLogger(t),
		sandboxImage:   testSandboxImage,
		idleTimeout:    30 * time.Minute,
		startupTimeout: 30 * time.Second,
		containers:     make(map[string]*containerState),
		creating:       make(map[string]bool),
	}
	svc.createCond = sync.NewCond(&svc.mu)
	return svc
}

// cleanupAgent registers a t.Cleanup that stops and removes the agent's container.
func cleanupAgent(t *testing.T, agentID string) {
	t.Helper()
	name := "trading-sandbox-" + agentID
	t.Cleanup(func() {
		_ = exec.Command("docker", "stop", name).Run()
		_ = exec.Command("docker", "rm", name).Run()
	})
}

func parseExecResult(t *testing.T, agentID string, args map[string]any, svc *Service) execResult {
	t.Helper()
	result, err := svc.toolExecute(agentID, args)
	if err != nil {
		t.Fatalf("toolExecute: %v", err)
	}
	if result.IsError || len(result.Content) == 0 {
		msg := ""
		if len(result.Content) > 0 {
			msg = result.Content[0].Text
		}
		t.Fatalf("tool returned error: %s", msg)
	}
	var r execResult
	if err := json.Unmarshal([]byte(result.Content[0].Text), &r); err != nil {
		t.Fatalf("parse exec result: %v", err)
	}
	return r
}

func TestIntegration_Execute_SimpleOutput(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-simple"
	cleanupAgent(t, agentID)

	r := parseExecResult(t, agentID, map[string]any{"code": "print('hello world')"}, svc)

	if r.Stdout != "hello world\n" {
		t.Errorf("stdout = %q, want %q", r.Stdout, "hello world\n")
	}
	if r.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0; stderr: %s", r.ExitCode, r.Stderr)
	}
}

func TestIntegration_Execute_StatePersisted(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-state"
	cleanupAgent(t, agentID)

	// Set a variable in the first call.
	parseExecResult(t, agentID, map[string]any{"code": "x = 42"}, svc)

	// Read it back - state must be preserved.
	r := parseExecResult(t, agentID, map[string]any{"code": "print(x)"}, svc)
	if r.Stdout != "42\n" {
		t.Errorf("stdout = %q, want %q (state not preserved)", r.Stdout, "42\n")
	}
}

func TestIntegration_Execute_AgentIsolation(t *testing.T) {
	svc := newIntegrationSvc(t)
	agent1, agent2 := "test-integ-iso-1", "test-integ-iso-2"
	cleanupAgent(t, agent1)
	cleanupAgent(t, agent2)

	// Set x=100 in agent1.
	parseExecResult(t, agent1, map[string]any{"code": "x = 100"}, svc)

	// agent2 should NOT see agent1's x.
	r := parseExecResult(t, agent2, map[string]any{
		"code": "print('x' in dir())",
	}, svc)
	if r.Stdout != "False\n" {
		t.Errorf("agent2 sees agent1 state; stdout = %q, want \"False\\n\"", r.Stdout)
	}
}

func TestIntegration_Execute_Timeout(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-timeout"
	cleanupAgent(t, agentID)

	r := parseExecResult(t, agentID, map[string]any{
		"code":    "import time; time.sleep(60)",
		"timeout": float64(2),
	}, svc)

	if r.ExitCode != 124 {
		t.Errorf("exit_code = %d, want 124 (timeout); stderr: %s", r.ExitCode, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "TimeoutError") && !strings.Contains(r.Stderr, "timeout") {
		t.Errorf("stderr %q does not mention timeout", r.Stderr)
	}
}

func TestIntegration_Execute_SyntaxError(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-syntax"
	cleanupAgent(t, agentID)

	r := parseExecResult(t, agentID, map[string]any{"code": "def broken(:"}, svc)

	if r.ExitCode == 0 {
		t.Errorf("expected non-zero exit code for syntax error, got 0")
	}
	if !strings.Contains(r.Stderr, "SyntaxError") {
		t.Errorf("stderr %q does not mention SyntaxError", r.Stderr)
	}
}

func TestIntegration_Execute_PandasAvailable(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-pandas"
	cleanupAgent(t, agentID)

	r := parseExecResult(t, agentID, map[string]any{
		"code": "import pandas as pd; print(pd.__version__)",
	}, svc)

	if r.ExitCode != 0 {
		t.Errorf("pandas import failed; stderr: %s", r.Stderr)
	}
	if r.Stdout == "" {
		t.Errorf("expected pandas version in stdout, got empty")
	}
}

func TestIntegration_Execute_SubprocessInstall(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-pip"
	cleanupAgent(t, agentID)

	// Install via subprocess (the intended pattern - no separate install tool).
	r := parseExecResult(t, agentID, map[string]any{
		"code": `import subprocess
result = subprocess.run(['pip', 'install', 'httpx'], capture_output=True, text=True)
print('exit:', result.returncode)`,
	}, svc)

	if r.ExitCode != 0 {
		t.Fatalf("subprocess pip install failed; stderr: %s", r.Stderr)
	}
	if r.Stdout == "" || r.ExitCode != 0 {
		t.Errorf("unexpected output: %s", r.Stdout)
	}

	// Verify installed package is importable in next call (state persists on disk).
	r2 := parseExecResult(t, agentID, map[string]any{"code": "import httpx; print('ok')"}, svc)
	if r2.Stdout != "ok\n" {
		t.Errorf("installed package not importable; stdout=%s stderr=%s", r2.Stdout, r2.Stderr)
	}
}

// TestIntegration_Execute_WriteScriptThenRun verifies the agent workflow of writing
// a Python script to a file inside the sandbox, then executing it as a subprocess.
// This is the recommended pattern for longer multi-step programs.
func TestIntegration_Execute_WriteScriptThenRun(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-script"
	cleanupAgent(t, agentID)

	// Step 1: write a Python script to /tmp inside the sandbox.
	parseExecResult(t, agentID, map[string]any{
		"code": `
with open('/tmp/analyze.py', 'w') as f:
    f.write("""
import statistics
data = [1, 2, 3, 4, 5]
print('mean:', statistics.mean(data))
print('stdev:', round(statistics.stdev(data), 4))
""")
print('script written')
`,
	}, svc)

	// Step 2: run the script as a subprocess.
	r := parseExecResult(t, agentID, map[string]any{
		"code": `
import subprocess
result = subprocess.run(['python', '/tmp/analyze.py'], capture_output=True, text=True)
print(result.stdout, end='')
`,
	}, svc)

	if r.ExitCode != 0 {
		t.Fatalf("script execution failed; stderr: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "mean: 3") {
		t.Errorf("expected mean output, got: %q", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "stdev:") {
		t.Errorf("expected stdev output, got: %q", r.Stdout)
	}
}

// TestIntegration_Execute_ExecFileInSandbox verifies running a previously written
// script via exec(open(path).read()) - the simpler single-process approach.
func TestIntegration_Execute_ExecFileInSandbox(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-execfile"
	cleanupAgent(t, agentID)

	// Write script.
	parseExecResult(t, agentID, map[string]any{
		"code": `
with open('/tmp/helper.py', 'w') as f:
    f.write("RESULT = 42\nprint('helper loaded')\n")
`,
	}, svc)

	// Exec the script - variables defined in it become available in REPL state.
	r := parseExecResult(t, agentID, map[string]any{
		"code": `exec(open('/tmp/helper.py').read())`,
	}, svc)
	if r.ExitCode != 0 {
		t.Fatalf("exec failed; stderr: %s", r.Stderr)
	}

	// RESULT should now be in scope (persistent REPL state).
	r2 := parseExecResult(t, agentID, map[string]any{
		"code": `print(RESULT)`,
	}, svc)
	if r2.Stdout != "42\n" {
		t.Errorf("stdout = %q, want '42\\n' (exec did not inject into scope)", r2.Stdout)
	}
}

func TestIntegration_Execute_ConcurrentSameAgent(t *testing.T) {
	svc := newIntegrationSvc(t)
	agentID := "test-integ-concurrent"
	cleanupAgent(t, agentID)

	// Warm up: ensure container exists before concurrent calls.
	parseExecResult(t, agentID, map[string]any{"code": "pass"}, svc)

	// Fire 4 concurrent calls - all must succeed (container shared, repl is sequential per Python GIL).
	const n = 4
	outputs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			result, err := svc.toolExecute(agentID, map[string]any{
				"code": fmt.Sprintf("print(%d)", i),
			})
			if err != nil {
				errs[i] = err
				return
			}
			var r execResult
			if parseErr := json.Unmarshal([]byte(result.Content[0].Text), &r); parseErr != nil {
				errs[i] = parseErr
				return
			}
			outputs[i] = r.Stdout
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	for i, out := range outputs {
		if out != fmt.Sprintf("%d\n", i) {
			t.Errorf("goroutine %d: stdout = %q, want %q", i, out, fmt.Sprintf("%d\n", i))
		}
	}
}
