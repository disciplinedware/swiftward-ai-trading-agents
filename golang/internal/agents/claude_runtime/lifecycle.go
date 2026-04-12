package claude_runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// LifecycleEmitter sends agent lifecycle events to the Swiftward ingestion API.
// Events are fire-and-forget — failures are logged but never block the session loop.
type LifecycleEmitter struct {
	cfg    Config
	client *http.Client
	log    *zap.Logger
}

// NewLifecycleEmitter creates a LifecycleEmitter. If SwiftwardURL is empty, events are no-ops.
func NewLifecycleEmitter(cfg Config, log *zap.Logger) *LifecycleEmitter {
	return &LifecycleEmitter{
		cfg: cfg,
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: http.DefaultTransport,
		},
		log: log,
	}
}

// lifecycleEvent is the Swiftward HTTP ingestion payload (matches HTTPIngestRequest in swiftward-core).
type lifecycleEvent struct {
	EntityID string         `json:"entity_id"`
	Stream   string         `json:"stream"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
}

// Emit sends a lifecycle event asynchronously.
// action: "started" | "healthy" | "session.started" | "session.completed" | "session.failed" | "error" | "needs_attention" | "stopped"
// sessionID: unique per-session string, empty for agent-level events
// extra: additional fields merged into event.data (may be nil)
func (e *LifecycleEmitter) Emit(ctx context.Context, action, sessionID string, extra map[string]any) {
	if e.cfg.SwiftwardURL == "" {
		return
	}

	data := map[string]any{
		"action":     action,
		"agent_id":   e.cfg.AgentID,
		"emitted_at": time.Now().UTC().Format(time.RFC3339),
	}
	if sessionID != "" {
		data["session_id"] = sessionID
	}
	for k, v := range extra {
		data[k] = v
	}

	stream := e.cfg.LifecycleStream
	if stream == "" {
		stream = DefaultLifecycleStream
	}
	ev := lifecycleEvent{
		EntityID: e.cfg.AgentID,
		Stream:   stream,
		Type:     "agent.lifecycle",
		Data:     data,
	}

	// fire and forget - use background context so lifecycle events survive session ctx cancellation
	go func() {
		if err := e.send(context.Background(), ev); err != nil {
			e.log.Warn("Lifecycle event failed",
				zap.String("action", action),
				zap.Error(err),
			)
		}
	}()
}

// EmitSync sends a lifecycle event synchronously. Use for shutdown events where the
// process may exit immediately after.
func (e *LifecycleEmitter) EmitSync(ctx context.Context, action, sessionID string, extra map[string]any) {
	if e.cfg.SwiftwardURL == "" {
		return
	}

	data := map[string]any{
		"action":     action,
		"agent_id":   e.cfg.AgentID,
		"emitted_at": time.Now().UTC().Format(time.RFC3339),
	}
	if sessionID != "" {
		data["session_id"] = sessionID
	}
	for k, v := range extra {
		data[k] = v
	}

	stream := e.cfg.LifecycleStream
	if stream == "" {
		stream = DefaultLifecycleStream
	}
	ev := lifecycleEvent{
		EntityID: e.cfg.AgentID,
		Stream:   stream,
		Type:     "agent.lifecycle",
		Data:     data,
	}

	if err := e.send(ctx, ev); err != nil {
		e.log.Warn("Lifecycle event failed (sync)",
			zap.String("action", action),
			zap.Error(err),
		)
	}
}

func (e *LifecycleEmitter) send(ctx context.Context, ev lifecycleEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal lifecycle event: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/ingest/async", e.cfg.SwiftwardURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build lifecycle request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("send lifecycle event: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("swiftward returned %d for lifecycle event", resp.StatusCode)
	}

	return nil
}
