package session

import (
	"analytic-sandbox/internal/sandbox"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/server"
)

type Session struct {
	ID             string
	WorkdirUUID    string
	ContainerID    string
	HostWorkDir    string
	AllowNetwork   bool
	MCP            *server.MCPServer
	Handler        http.Handler
	ExecMutex      sync.Mutex
	CreatedAt      time.Time
	LastActivityAt time.Time
	Stopped        bool
	Manager        *Manager
	MemoryMB       int
	CpuCount       int
	DiskMB         int
	TimeoutSeconds int
}

type Manager struct {
	mu              sync.RWMutex
	sessions        map[string]*Session
	dockerManager   *sandbox.DockerManager
	baseDataDir     string
	hostBaseDataDir string
}

func NewManager(dm *sandbox.DockerManager, baseDataDir string) *Manager {
	if baseDataDir == "" {
		baseDataDir = "data"
	}
	absDataDir, _ := filepath.Abs(baseDataDir)

	hostBaseDataDir := os.Getenv("HOST_DATA_DIR")
	if hostBaseDataDir == "" {
		hostBaseDataDir = absDataDir
	}

	// Ensure essential directories exist
	dirs := []string{
		filepath.Join(absDataDir, "sessions"),
		filepath.Join(absDataDir, "containers"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("Warning: failed to create directory %s: %v\n", dir, err)
		}
	}

	return &Manager{
		sessions:        make(map[string]*Session),
		dockerManager:   dm,
		baseDataDir:     absDataDir,
		hostBaseDataDir: hostBaseDataDir,
	}
}

type SessionParams struct {
	AllowNetwork       bool
	RequestedSessionID string
	WorkdirUUID        string
	MemoryMB           int
	CpuCount           int
	DiskMB             int
	TimeoutSeconds     int
}

func (m *Manager) GetOrCreate(ctx context.Context, params SessionParams, factory func(sess *Session) (*server.MCPServer, http.Handler)) (*Session, error) {

	// If a specific session ID is requested, try to find it
	if params.RequestedSessionID != "" {
		m.mu.RLock()
		sess, ok := m.sessions[params.RequestedSessionID]
		m.mu.RUnlock()

		if ok {
			return sess, nil
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	sessionID := params.RequestedSessionID
	var localWorkDir string
	var dockerWorkDir string
	var currentWorkdirUUID string

	if sessionID != "" {
		// Resuming existing session from disk
		meta, _ := m.LoadMetadata(sessionID)
		if meta != nil {
			currentWorkdirUUID = meta.WorkdirUUID
			// Restore resource params from metadata if caller provides defaults
			if params.MemoryMB == 0 && meta.MemoryMB != 0 {
				params.MemoryMB = meta.MemoryMB
			}
			if params.CpuCount == 0 && meta.CpuCount != 0 {
				params.CpuCount = meta.CpuCount
			}
			if params.DiskMB == 0 && meta.DiskMB != 0 {
				params.DiskMB = meta.DiskMB
			}
			if params.TimeoutSeconds == 0 && meta.TimeoutSeconds != 0 {
				params.TimeoutSeconds = meta.TimeoutSeconds
			}
			if !params.AllowNetwork && meta.AllowNetwork {
				params.AllowNetwork = meta.AllowNetwork
			}
		}
		if currentWorkdirUUID == "" {
			currentWorkdirUUID = sessionID
		}

		localWorkDir = filepath.Join(m.baseDataDir, "containers", currentWorkdirUUID)
		dockerWorkDir = filepath.Join(m.hostBaseDataDir, "containers", currentWorkdirUUID)

		if _, err := os.Stat(localWorkDir); err != nil {
			return nil, fmt.Errorf("session expired or workdir missing, please create a new session")
		}
	} else {
		// New session
		sessionID = uuid.New().String()

		if params.WorkdirUUID != "" {
			u, err := uuid.Parse(params.WorkdirUUID)
			if err != nil || u.Version() != 4 {
				return nil, fmt.Errorf("invalid workdir_uuid: must be a valid UUID v4")
			}
			currentWorkdirUUID = u.String()
		} else {
			currentWorkdirUUID = sessionID
		}

		localWorkDir = filepath.Join(m.baseDataDir, "containers", currentWorkdirUUID)
		dockerWorkDir = filepath.Join(m.hostBaseDataDir, "containers", currentWorkdirUUID)
		if err := os.MkdirAll(localWorkDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create work dir: %w", err)
		}
		// Ensure the rails user (uid 1000) can write to this directory
		_ = os.Chown(localWorkDir, 1000, 1000)
	}

	containerID, err := m.dockerManager.StartContainer(ctx, sessionID, dockerWorkDir, params.AllowNetwork, sandbox.ResourceOptions{
		MemoryMB: params.MemoryMB,
		CpuCount: params.CpuCount,
		DiskMB:   params.DiskMB,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	sess := &Session{
		ID:             sessionID,
		WorkdirUUID:    currentWorkdirUUID,
		ContainerID:    containerID,
		HostWorkDir:    localWorkDir,
		AllowNetwork:   params.AllowNetwork,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
		Manager:        m,
		MemoryMB:       params.MemoryMB,
		CpuCount:       params.CpuCount,
		DiskMB:         params.DiskMB,
		TimeoutSeconds: params.TimeoutSeconds,
	}

	meta, _ := m.LoadMetadata(sessionID)
	if meta == nil {
		meta = &SessionMetadata{
			SessionID:      sessionID,
			WorkdirUUID:    currentWorkdirUUID,
			CreatedAt:      sess.CreatedAt,
			History:        make([]SessionEvent, 0),
			AllowNetwork:   params.AllowNetwork,
			MemoryMB:       params.MemoryMB,
			CpuCount:       params.CpuCount,
			DiskMB:         params.DiskMB,
			TimeoutSeconds: params.TimeoutSeconds,
		}
		if err := m.SaveMetadata(sessionID, *meta); err != nil {
			return nil, fmt.Errorf("failed to save session metadata: %w", err)
		}
	}

	mcpSrv, handler := factory(sess)
	sess.MCP = mcpSrv
	sess.Handler = handler

	m.sessions[sessionID] = sess
	return sess, nil
}

func (m *Manager) Register(id string, sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = sess
}

func (m *Manager) Cleanup(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sess := range m.sessions {
		_ = m.dockerManager.StopContainer(ctx, sess.ContainerID)
	}
	m.sessions = make(map[string]*Session)
}

func (m *Manager) UpdateActivity(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[sessionID]; ok {
		sess.LastActivityAt = time.Now()
	}
}

func (m *Manager) RestartContainerIfNeeded(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if !sess.Stopped {
		return nil
	}

	var dockerWorkDir string
	if filepath.IsAbs(sess.HostWorkDir) && strings.HasPrefix(sess.HostWorkDir, m.baseDataDir) {
		rel, _ := filepath.Rel(m.baseDataDir, sess.HostWorkDir)
		dockerWorkDir = filepath.Join(m.hostBaseDataDir, rel)
	} else {
		dockerWorkDir = sess.HostWorkDir
	}

	containerID, err := m.dockerManager.StartContainer(ctx, sess.ID, dockerWorkDir, sess.AllowNetwork, sandbox.ResourceOptions{
		MemoryMB: sess.MemoryMB,
		CpuCount: sess.CpuCount,
		DiskMB:   sess.DiskMB,
	})
	if err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}

	sess.ContainerID = containerID
	sess.Stopped = false
	sess.LastActivityAt = time.Now()
	return nil
}

func (m *Manager) StopInactiveContainers(ctx context.Context, inactiveTimeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, sess := range m.sessions {
		if sess.Stopped {
			continue
		}
		if now.Sub(sess.LastActivityAt) > inactiveTimeout {
			_ = m.dockerManager.StopContainer(ctx, sess.ContainerID)
			sess.Stopped = true
		}
	}
}

func (m *Manager) StartInactivityMonitor(ctx context.Context, inactiveTimeout time.Duration) {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.StopInactiveContainers(ctx, inactiveTimeout)
			}
		}
	}()
}

type SessionStatus struct {
	ID             string    `json:"id"`
	ContainerID    string    `json:"container_id"`
	CreatedAt      time.Time `json:"created_at"`
	LastActivityAt time.Time `json:"last_activity_at"`
	Stopped        bool      `json:"stopped"`
}

type ManagerStatus struct {
	Sessions      []SessionStatus `json:"sessions"`
	ActiveCount   int             `json:"active_count"`
	StoppedCount  int             `json:"stopped_count"`
	TotalSessions int             `json:"total_sessions"`
}

func (m *Manager) GetStatus() ManagerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := ManagerStatus{
		Sessions: make([]SessionStatus, 0),
	}

	seen := make(map[*Session]bool)
	for _, sess := range m.sessions {
		if seen[sess] {
			continue
		}
		seen[sess] = true

		sessStatus := SessionStatus{
			ID:             sess.ID,
			ContainerID:    sess.ContainerID,
			CreatedAt:      sess.CreatedAt,
			LastActivityAt: sess.LastActivityAt,
			Stopped:        sess.Stopped,
		}
		status.Sessions = append(status.Sessions, sessStatus)
		if sess.Stopped {
			status.StoppedCount++
		} else {
			status.ActiveCount++
		}
	}
	status.TotalSessions = len(status.Sessions)

	return status
}
