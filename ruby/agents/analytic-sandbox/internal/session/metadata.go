package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type SessionEvent struct {
	Timestamp time.Time   `json:"timestamp"`
	Tool      string      `json:"tool"`
	Arguments interface{} `json:"arguments,omitempty"`
	IsError   bool        `json:"is_error"`
}

type SessionMetadata struct {
	SessionID      string         `json:"session_id"`
	WorkdirUUID    string         `json:"workdir_uuid,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	LastTool       string         `json:"last_tool,omitempty"`
	LastToolAt     time.Time      `json:"last_tool_at,omitempty"`
	History        []SessionEvent `json:"history,omitempty"`
	AllowNetwork   bool           `json:"allow_network,omitempty"`
	MemoryMB       int            `json:"memory_mb,omitempty"`
	CpuCount       int            `json:"cpu_count,omitempty"`
	DiskMB         int            `json:"disk_mb,omitempty"`
	TimeoutSeconds int            `json:"timeout_seconds,omitempty"`
}

// metadataPath returns the path to the metadata JSON file for a given session ID
func (m *Manager) metadataPath(sessionID string) string {
	return filepath.Join(m.baseDataDir, "sessions", sessionID+".json")
}

// SaveMetadata writes session metadata to disk
func (m *Manager) SaveMetadata(sessionID string, meta SessionMetadata) error {
	path := m.metadataPath(sessionID)

	// ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session metadata: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write session metadata: %w", err)
	}
	return nil
}

// LoadMetadata reads session metadata from disk
func (m *Manager) LoadMetadata(sessionID string) (*SessionMetadata, error) {
	path := m.metadataPath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Return nil if no metadata exists
		}
		return nil, fmt.Errorf("failed to read session metadata: %w", err)
	}

	var meta SessionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session metadata: %w", err)
	}
	return &meta, nil
}

// AddHistoryEvent appends a new event to the session history and updates last tool info
func (m *Manager) AddHistoryEvent(sessionID string, tool string, args interface{}, isError bool) error {
	// Simple implementation: load, append, save.
	// For high concurrency this might need a lock, but session usage is generally sequential.
	// Manager lock could be used if we expose it, but for now we rely on file system atomic write (mostly).
	// Ideally Manager should handle locking for metadata update.

	// We need to lock the manager to ensure consistent read/write of metadata file if concurrent
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, err := m.LoadMetadata(sessionID)
	if err != nil {
		return err
	}

	now := time.Now()

	// specific case: if sessions was just created in memory but metadata file not written yet (race?),
	// or if we are adding history to a session that lacks metadata.
	if meta == nil {
		// Should generally exist if initialized properly, but be safe
		meta = &SessionMetadata{
			SessionID: sessionID,
			CreatedAt: now,
		}
	}

	meta.LastTool = tool
	meta.LastToolAt = now

	event := SessionEvent{
		Timestamp: now,
		Tool:      tool,
		Arguments: args,
		IsError:   isError,
	}
	meta.History = append(meta.History, event)

	return m.SaveMetadata(sessionID, *meta)
}
