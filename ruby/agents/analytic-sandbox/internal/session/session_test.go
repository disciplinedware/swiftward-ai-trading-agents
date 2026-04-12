package session

import (
	"analytic-sandbox/internal/sandbox"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

func TestSessionResumption(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping in CI/limited environment if needed")
	}

	// 1. Setup
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	dm, err := sandbox.NewDockerManager("")
	if err != nil {
		t.Skip("Docker not available")
	}

	// 2. Create Session 1
	sm1 := NewManager(dm, dataDir)
	ctx := context.Background()

	factory := func(sess *Session) (*server.MCPServer, http.Handler) {
		return server.NewMCPServer("test", "1.0"), nil
	}

	sess1, err := sm1.GetOrCreate(ctx, SessionParams{AllowNetwork: false, MemoryMB: 512, CpuCount: 4, DiskMB: 1024, TimeoutSeconds: 120}, factory)
	if err != nil {
		t.Fatalf("Failed to create session 1: %v", err)
	}
	id1 := sess1.ID
	t.Logf("Created session %s", id1)

	// Verify container dir exists
	containerDir := filepath.Join(dataDir, "containers", id1)
	if _, err := os.Stat(containerDir); os.IsNotExist(err) {
		t.Fatalf("Container dir not created: %s", containerDir)
	}

	t.Cleanup(func() {
		_ = dm.StopContainer(ctx, sess1.ContainerID)
	})

	sm2 := NewManager(dm, dataDir)

	// 4. Resume Session
	sess2, err := sm2.GetOrCreate(ctx, SessionParams{AllowNetwork: false, RequestedSessionID: id1, MemoryMB: 512, CpuCount: 4, DiskMB: 1024, TimeoutSeconds: 120}, factory)
	if err != nil {
		t.Fatalf("Failed to resume session: %v", err)
	}

	if sess2.ID != id1 {
		t.Errorf("Resumed session ID mismatch. Got %s, want %s", sess2.ID, id1)
	}

	if sess2.ContainerID != sess1.ContainerID {
		t.Logf("Container IDs: Original=%s, Resumed=%s", sess1.ContainerID, sess2.ContainerID)
	} else {
		t.Log("Verified Container ID reuse")
	}
}

func TestGetStatus(t *testing.T) {
	sm := &Manager{
		sessions: make(map[string]*Session),
	}

	now := time.Now()
	sm.sessions["s1"] = &Session{
		ID:             "s1",
		ContainerID:    "c1",
		CreatedAt:      now.Add(-1 * time.Hour),
		LastActivityAt: now,
		Stopped:        false,
	}
	sm.sessions["s2"] = &Session{
		ID:             "s2",
		ContainerID:    "c2",
		CreatedAt:      now.Add(-2 * time.Hour),
		LastActivityAt: now.Add(-30 * time.Minute),
		Stopped:        true,
	}

	status := sm.GetStatus()

	if status.TotalSessions != 2 {
		t.Errorf("Expected TotalSessions=2, got %d", status.TotalSessions)
	}
	if status.ActiveCount != 1 {
		t.Errorf("Expected ActiveCount=1, got %d", status.ActiveCount)
	}
	if status.StoppedCount != 1 {
		t.Errorf("Expected StoppedCount=1, got %d", status.StoppedCount)
	}
	if len(status.Sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(status.Sessions))
	}

	var s1Status *SessionStatus
	for i := range status.Sessions {
		if status.Sessions[i].ID == "s1" {
			s1Status = &status.Sessions[i]
			break
		}
	}
	if s1Status == nil {
		t.Fatal("s1 not found in status")
	}
	if s1Status.ContainerID != "c1" {
		t.Errorf("Expected ContainerID=c1, got %s", s1Status.ContainerID)
	}
	if s1Status.Stopped {
		t.Error("Expected s1 to be active")
	}
}

func TestUpdateActivity(t *testing.T) {
	sm := &Manager{
		sessions: make(map[string]*Session),
	}

	oldTime := time.Now().Add(-1 * time.Hour)
	sm.sessions["test"] = &Session{
		ID:             "test",
		LastActivityAt: oldTime,
	}

	time.Sleep(10 * time.Millisecond)
	sm.UpdateActivity("test")

	if sm.sessions["test"].LastActivityAt.Before(oldTime) || sm.sessions["test"].LastActivityAt.Equal(oldTime) {
		t.Error("Expected LastActivityAt to be updated to a newer time")
	}
}

func TestStopInactiveContainers(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping in CI")
	}

	dm, err := sandbox.NewDockerManager("")
	if err != nil {
		t.Skip("Docker not available")
	}

	sm := NewManager(dm, t.TempDir())
	ctx := context.Background()

	now := time.Now()
	sm.sessions["active"] = &Session{
		ID:             "active",
		ContainerID:    "fake-container-1",
		LastActivityAt: now,
		Stopped:        false,
	}
	sm.sessions["inactive"] = &Session{
		ID:             "inactive",
		ContainerID:    "fake-container-2",
		LastActivityAt: now.Add(-2 * time.Hour),
		Stopped:        false,
	}

	sm.StopInactiveContainers(ctx, 30*time.Minute)

	if sm.sessions["active"].Stopped {
		t.Error("Active session should not be stopped")
	}
	if !sm.sessions["inactive"].Stopped {
		t.Error("Inactive session should be stopped")
	}
}
