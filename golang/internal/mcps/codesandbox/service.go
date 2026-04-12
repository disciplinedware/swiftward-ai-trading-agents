package codesandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/observability"
	"ai-trading-agents/internal/platform"
)

type contextKey string

const agentIDContextKey contextKey = "agent_id"

type containerState struct {
	containerName string
	host          string // "localhost" or container name (on Docker network)
	port          int
	lastUsed      time.Time
}

// Service implements the Code Sandbox MCP - persistent Python sandbox containers per agent.
type Service struct {
	svcCtx            *platform.ServiceContext
	log               *zap.Logger
	workspaceDir      string // container-local workspace path (e.g., "/data/workspace") — for reading files
	hostWorkspacePath string // absolute host path for DinD bind mounts (set via HOST_WORKSPACE_PATH)
	sandboxImage      string
	dockerNetwork     string // Docker network for sandbox containers; empty = host port mapping
	idleTimeout       time.Duration
	startupTimeout    time.Duration
	mu                sync.Mutex
	containers        map[string]*containerState
	// creating tracks agents whose container is currently being started.
	// Concurrent calls for the same agent wait on createCond instead of
	// racing to run docker run twice with the same container name.
	creating   map[string]bool
	createCond *sync.Cond
}

// NewService creates the Code Sandbox MCP service.
func NewService(svcCtx *platform.ServiceContext) *Service {
	cfg := svcCtx.Config().CodeMCP
	sandboxImage := cfg.SandboxImage
	if sandboxImage == "" {
		sandboxImage = "ghcr.io/disciplinedware/ai-trading-agents/sandbox-python:latest"
	}
	idleTimeout := 30 * time.Minute
	if cfg.IdleTimeout != "" {
		if d, err := time.ParseDuration(cfg.IdleTimeout); err == nil {
			idleTimeout = d
		}
	}
	startupTimeout := 5 * time.Minute
	if cfg.StartupTimeout != "" {
		if d, err := time.ParseDuration(cfg.StartupTimeout); err == nil {
			startupTimeout = d
		}
	}
	svc := &Service{
		svcCtx:            svcCtx,
		log:               svcCtx.Logger().Named("code_sandbox_mcp"),
		workspaceDir:      cfg.WorkspaceDir,
		hostWorkspacePath: cfg.HostWorkspacePath,
		sandboxImage:      sandboxImage,
		dockerNetwork:     cfg.DockerNetwork,
		idleTimeout:       idleTimeout,
		startupTimeout:    startupTimeout,
		containers:        make(map[string]*containerState),
		creating:          make(map[string]bool),
	}
	svc.createCond = sync.NewCond(&svc.mu)
	return svc
}

func (s *Service) Initialize() error {
	mcpServer := mcp.NewServer("code-sandbox-mcp", "1.0.0", s.tools(), s.handleTool)

	s.svcCtx.Router().Post("/mcp/code", func(w http.ResponseWriter, r *http.Request) {
		agentID := r.Header.Get("X-Agent-ID")
		if agentID != "" {
			ctx := context.WithValue(r.Context(), agentIDContextKey, agentID)
			ctx = observability.WithLogger(ctx, s.log.With(zap.String("agent_id", agentID)))
			r = r.WithContext(ctx)
		}
		mcpServer.ServeHTTP(w, r)
	})

	hostPath := s.hostWorkspacePath
	if hostPath == "" {
		hostPath = "(not set - workspace not mounted in containers)"
	}
	s.log.Info("Code Sandbox MCP registered at /mcp/code",
		zap.String("sandbox_image", s.sandboxImage),
		zap.Duration("idle_timeout", s.idleTimeout),
		zap.String("host_workspace_path", hostPath),
	)
	return nil
}

func (s *Service) Start() error {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.svcCtx.Context().Done():
			return nil
		case <-ticker.C:
			s.evictIdleContainers()
		}
	}
}

func (s *Service) Stop() error {
	s.mu.Lock()
	toStop := make(map[string]*containerState, len(s.containers))
	for id, state := range s.containers {
		toStop[id] = state
	}
	s.containers = make(map[string]*containerState)
	s.mu.Unlock()

	for agentID, state := range toStop {
		s.log.Info("Stopping sandbox container on shutdown",
			zap.String("agent_id", agentID),
			zap.String("container", state.containerName),
		)
		_ = exec.Command("docker", "stop", state.containerName).Run()
		_ = exec.Command("docker", "rm", state.containerName).Run()
	}
	s.log.Info("Code Sandbox MCP stopped")
	return nil
}

func (s *Service) evictIdleContainers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for agentID, state := range s.containers {
		if now.Sub(state.lastUsed) > s.idleTimeout {
			s.log.Info("Evicting idle sandbox container",
				zap.String("agent_id", agentID),
				zap.String("container", state.containerName),
			)
			_ = exec.Command("docker", "stop", state.containerName).Run()
			_ = exec.Command("docker", "rm", state.containerName).Run()
			delete(s.containers, agentID)
		}
	}
}

// validateAgentID enforces a strict allowlist: letters, digits, hyphens, underscores, dots.
// This ensures the agent ID is a valid Docker container name component and safe as a path segment.
func validateAgentID(agentID string) error {
	if agentID == "" {
		return fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}
	if agentID == "." || strings.Contains(agentID, "..") {
		return fmt.Errorf("invalid agent_id %q: must not be or contain '..'", agentID)
	}
	for _, c := range agentID {
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		isSafe := c == '-' || c == '_' || c == '.'
		if !isLetter && !isDigit && !isSafe {
			return fmt.Errorf("invalid agent_id %q: only letters, digits, hyphens, underscores, and dots are allowed", agentID)
		}
	}
	return nil
}

func (s *Service) getOrCreateContainer(agentID string) (*containerState, error) {
	containerName := "trading-sandbox-" + agentID

	s.mu.Lock()
	// If another goroutine is already creating this container, wait for it to finish.
	for s.creating[agentID] {
		s.createCond.Wait()
	}

	// Check map after waiting (the creator may have already registered the state).
	if state, ok := s.containers[agentID]; ok {
		s.mu.Unlock()
		// Verify the container is still running.
		out, err := exec.Command("docker", "inspect", "--format={{.State.Running}}", containerName).Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			s.mu.Lock()
			state.lastUsed = time.Now()
			s.mu.Unlock()
			return state, nil
		}
		// Container stopped or gone - fall through to recreate.
		s.mu.Lock()
		delete(s.containers, agentID)
	}

	// Claim exclusive creation rights for this agentID.
	s.creating[agentID] = true
	s.mu.Unlock()

	// All Docker ops run outside the lock. On any failure, release the sentinel
	// and wake waiters so they can retry or surface an error.
	state, err := s.startContainer(agentID, containerName)

	s.mu.Lock()
	delete(s.creating, agentID)
	if err == nil {
		s.containers[agentID] = state
	}
	s.createCond.Broadcast()
	s.mu.Unlock()

	if err != nil {
		return nil, err
	}

	s.log.Info("Started sandbox container",
		zap.String("agent_id", agentID),
		zap.String("container", containerName),
		zap.Int("port", state.port),
	)
	return state, nil
}

// startContainer performs the actual Docker container creation. Called without holding s.mu.
func (s *Service) startContainer(agentID, containerName string) (*containerState, error) {
	// Remove any existing container with the same name (stopped or running).
	_, inspectErr := exec.Command("docker", "inspect", containerName).Output()
	if inspectErr == nil {
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
	}

	// Start a new container.
	// If HOST_WORKSPACE_PATH is set, bind-mount the agent workspace so Python code can
	// read/write files visible from the host (e.g. for monitoring, CSV exports, etc.).
	runArgs := []string{
		"run", "-d",
		"--name", containerName,
		"--restart", "no",
	}
	if s.dockerNetwork != "" {
		// Use Docker network: address sandbox by container name, no host port exposure.
		runArgs = append(runArgs, "--network", s.dockerNetwork)
	} else {
		// No network configured: map a random host port for localhost access (local dev).
		runArgs = append(runArgs, "-p", "0:8099")
	}
	if s.hostWorkspacePath != "" {
		agentHostPath := filepath.Join(s.hostWorkspacePath, agentID)
		// Pre-create the host dir so Docker doesn't auto-create it as root-owned
		// when this is the first tool call for a dynamic agent (e.g. Ruby arena).
		// Subsequent Files MCP writes from the trading-server user would otherwise
		// fail with EACCES on a root-owned directory.
		if err := os.MkdirAll(agentHostPath, 0o755); err != nil {
			return nil, fmt.Errorf("create agent workspace dir %s: %w", agentHostPath, err)
		}
		runArgs = append(runArgs, "-v", agentHostPath+":/workspace")
	}
	runArgs = append(runArgs, s.sandboxImage)
	if out, err := exec.Command("docker", runArgs...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker run: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// Determine the sandbox address.
	var host string
	var port int
	if s.dockerNetwork != "" {
		// On a shared Docker network, reach sandbox by container name on the internal port.
		host = containerName
		port = 8099
	} else {
		// No network: get the random host port.
		portOut, err := exec.Command("docker", "port", containerName, "8099/tcp").Output()
		if err != nil {
			_ = exec.Command("docker", "rm", "-f", containerName).Run()
			return nil, fmt.Errorf("docker port: %w", err)
		}
		p, err := parseDockerPort(string(portOut))
		if err != nil {
			_ = exec.Command("docker", "rm", "-f", containerName).Run()
			return nil, fmt.Errorf("parse container port: %w", err)
		}
		host = "localhost"
		port = p
	}

	// Wait for repl.py to be ready.
	if err := s.waitForRepl(host, port); err != nil {
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		return nil, err
	}

	return &containerState{
		containerName: containerName,
		host:          host,
		port:          port,
		lastUsed:      time.Now(),
	}, nil
}

func parseDockerPort(output string) (int, error) {
	line := strings.TrimSpace(output)
	parts := strings.Split(line, ":")
	if len(parts) == 0 {
		return 0, fmt.Errorf("unexpected docker port output: %q", line)
	}
	portStr := parts[len(parts)-1]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", portStr, err)
	}
	return port, nil
}

func (s *Service) waitForRepl(host string, port int) error {
	client := &http.Client{Timeout: 1 * time.Second}
	interval := 500 * time.Millisecond
	deadline := time.Now().Add(s.startupTimeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://%s:%d/", host, port))
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("repl.py not ready after %s", s.startupTimeout)
}

func (s *Service) callRepl(host string, port int, path string, payload any) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 130 * time.Second}
	resp, err := client.Post(fmt.Sprintf("http://%s:%d%s", host, port, path), "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("repl call %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse repl response: %w", err)
	}
	return result, nil
}

func (s *Service) tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "code/execute",
			Description: "Execute Python code in a persistent sandbox. " +
				"State is preserved between calls within a session — load a CSV once, analyze across multiple calls. " +
				"Returns: {stdout, stderr, error (if exception), duration_ms}. " +
				"Pre-installed: pandas, numpy, scipy, matplotlib, pandas-ta, scikit-learn, statsmodels, plotly, requests. " +
				"Install extras: subprocess.run(['pip','install','pkg'], capture_output=True). " +
				"CSVs saved by market/get_candles with save_to_file=true are readable at the path returned in saved_to — use that value directly in pd.read_csv(). " +
				"For complex scripts: write with files/write to scripts/analysis.py, then execute with file=\"scripts/analysis.py\" instead of inline code.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code":    map[string]any{"type": "string", "description": "Python code to execute inline. To load a CSV, use pd.read_csv(saved_to) where saved_to is the path from market/get_candles. Mutually exclusive with file."},
					"file":    map[string]any{"type": "string", "description": "Path to a .py script to execute (e.g. scripts/analysis.py). Only for Python scripts, not data files. Mutually exclusive with code."},
					"timeout": map[string]any{"type": "integer", "description": "Execution timeout in seconds (default 30, max 120)"},
				},
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
	case "code/execute":
		return s.toolExecute(agentID, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (s *Service) toolExecute(agentID string, args map[string]any) (*mcp.ToolResult, error) {
	code, _ := args["code"].(string)
	file, _ := args["file"].(string)

	if code == "" && file == "" {
		return nil, fmt.Errorf("either code or file is required")
	}
	if code != "" && file != "" {
		return nil, fmt.Errorf("code and file are mutually exclusive")
	}

	if file != "" {
		if s.workspaceDir == "" {
			return nil, fmt.Errorf("file execution requires TRADING__CODE_MCP__WORKSPACE_DIR to be set")
		}
		if !strings.HasSuffix(file, ".py") {
			return nil, fmt.Errorf("file must be a .py script, not a data file — to load CSVs use code with pd.read_csv()")
		}
		// Strip /workspace/ prefix if the LLM passes the sandbox-absolute path.
		file = strings.TrimPrefix(file, "/workspace/")
		absPath := filepath.Join(s.workspaceDir, agentID, filepath.Clean(file))
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("read script %s: %w", file, err)
		}
		code = string(data)
	}

	timeout := 30
	if t, ok := args["timeout"].(float64); ok {
		timeout = int(t)
	}
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 120 {
		timeout = 120
	}

	state, err := s.getOrCreateContainer(agentID)
	if err != nil {
		return nil, fmt.Errorf("get sandbox: %w", err)
	}

	start := time.Now()
	result, err := s.callRepl(state.host, state.port, "/execute", map[string]any{
		"code":    code,
		"timeout": timeout,
	})
	if err != nil {
		return nil, err
	}
	result["duration_ms"] = time.Since(start).Milliseconds()

	s.mu.Lock()
	if c, ok := s.containers[agentID]; ok {
		c.lastUsed = time.Now()
	}
	s.mu.Unlock()

	return mcp.JSONResult(result)
}

