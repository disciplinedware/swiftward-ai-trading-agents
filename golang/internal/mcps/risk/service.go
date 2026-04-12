package risk

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/mcps/trading"
	"ai-trading-agents/internal/platform"
)

// Service implements the Risk MCP - operator-facing tools for risk management.
type Service struct {
	svcCtx     *platform.ServiceContext
	log        *zap.Logger
	tradingSvc *trading.Service // shares agent registry and DB repository
}

// NewService creates the Risk MCP service.
func NewService(svcCtx *platform.ServiceContext, tradingSvc *trading.Service) *Service {
	return &Service{
		svcCtx:     svcCtx,
		log:        svcCtx.Logger().Named("risk_mcp"),
		tradingSvc: tradingSvc,
	}
}

func (s *Service) Initialize() error {
	mcpServer := mcp.NewServer("risk-mcp", "1.0.0", s.tools(), s.handleTool)
	s.svcCtx.Router().Post("/mcp/risk", mcpServer.ServeHTTP)
	s.log.Info("Risk MCP registered at /mcp/risk")
	return nil
}

func (s *Service) Start() error {
	<-s.svcCtx.Context().Done()
	return nil
}

func (s *Service) Stop() error {
	s.log.Info("Risk MCP stopped")
	return nil
}

func (s *Service) tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "risk/halt_agent",
			Description: "Halt an agent - blocks all trade orders immediately.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_id": map[string]any{"type": "string", "description": "Agent ID to halt"},
				},
				"required": []string{"agent_id"},
			},
		},
		{
			Name:        "risk/resume_agent",
			Description: "Resume a halted agent.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_id": map[string]any{"type": "string", "description": "Agent ID to resume"},
				},
				"required": []string{"agent_id"},
			},
		},
		{
			Name:        "risk/get_agent_status",
			Description: "Get agent status: portfolio, halt state.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_id": map[string]any{"type": "string", "description": "Agent ID"},
				},
				"required": []string{"agent_id"},
			},
		},
		{
			Name:        "risk/list_agents",
			Description: "List all registered agents with current status.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func (s *Service) handleTool(ctx context.Context, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	switch toolName {
	case "risk/halt_agent":
		return s.toolHaltAgent(ctx, args)
	case "risk/resume_agent":
		return s.toolResumeAgent(ctx, args)
	case "risk/get_agent_status":
		return s.toolGetAgentStatus(ctx, args)
	case "risk/list_agents":
		return s.toolListAgents(ctx)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// resolveAgent returns the display name for an agent, looking first at the
// static config registry and falling back to agent_state in the DB so that
// dynamically-onboarded agents are addressable from the operator UI too.
func (s *Service) resolveAgent(ctx context.Context, agentID string) (name string, err error) {
	if a, ok := s.tradingSvc.Agents()[agentID]; ok {
		return a.Name, nil
	}
	if _, err := s.tradingSvc.Repo().GetAgent(ctx, agentID); err != nil {
		return "", fmt.Errorf("unknown agent: %s", agentID)
	}
	return agentID, nil
}

func (s *Service) toolHaltAgent(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	if _, err := s.resolveAgent(ctx, agentID); err != nil {
		return nil, err
	}

	if err := s.tradingSvc.SetHalted(ctx, agentID, true); err != nil {
		s.log.Error("Halt agent failed", zap.String("agent_id", agentID), zap.Error(err))
		return nil, fmt.Errorf("halt agent %s: %w", agentID, err)
	}
	s.log.Info("Agent halted", zap.String("agent_id", agentID))

	return mcp.JSONResult(map[string]any{"status": "halted", "agent_id": agentID})
}

func (s *Service) toolResumeAgent(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	if _, err := s.resolveAgent(ctx, agentID); err != nil {
		return nil, err
	}

	if err := s.tradingSvc.SetHalted(ctx, agentID, false); err != nil {
		s.log.Error("Resume agent failed", zap.String("agent_id", agentID), zap.Error(err))
		return nil, fmt.Errorf("resume agent %s: %w", agentID, err)
	}
	s.log.Info("Agent resumed", zap.String("agent_id", agentID))

	return mcp.JSONResult(map[string]any{"status": "resumed", "agent_id": agentID})
}

func (s *Service) toolGetAgentStatus(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	name, err := s.resolveAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	status := map[string]any{
		"agent_id": agentID,
		"name":     name,
	}

	repo := s.tradingSvc.Repo()
	agentState, err := repo.GetAgent(ctx, agentID)
	if err == nil {
		prices := s.tradingSvc.Exchange().GetPrices()
		equity, _ := repo.ComputeEquity(ctx, agentID, prices)
		positions, _ := repo.GetOpenPositions(ctx, agentID)

		posOut := make([]map[string]any, 0, len(positions))
		for _, pos := range positions {
			posOut = append(posOut, map[string]any{
				"pair":      pos.Pair,
				"side":      pos.Side,
				"qty":       pos.Qty.String(),
				"avg_price": pos.AvgPrice.String(),
				"value":     pos.Value.String(),
			})
		}

		status["halted"] = agentState.Halted
		status["portfolio"] = map[string]any{
			"value":     equity.String(),
			"cash":      agentState.Cash.String(),
			"peak":      agentState.PeakValue.String(),
			"positions": posOut,
		}
		status["fill_count"] = agentState.FillCount
		status["reject_count"] = agentState.RejectCount
	}

	return mcp.JSONResult(status)
}

func (s *Service) toolListAgents(ctx context.Context) (*mcp.ToolResult, error) {
	repo := s.tradingSvc.Repo()
	prices := s.tradingSvc.Exchange().GetPrices()

	// Union the static config registry with everything in agent_state. This
	// surfaces dynamically-onboarded agents (auto-created by Trading MCP via
	// X-Agent-ID) on the dashboard, not just agents listed in compose env.
	configured := s.tradingSvc.Agents()

	dbAgents, err := repo.ListAgents(ctx)
	if err != nil {
		s.log.Warn("List agents from DB failed; falling back to config-only", zap.Error(err))
	}
	dbByID := make(map[string]*db.AgentState, len(dbAgents))
	for _, a := range dbAgents {
		dbByID[a.AgentID] = a
	}

	names := make(map[string]string, len(configured)+len(dbByID))
	for id, a := range configured {
		names[id] = a.Name
	}
	for id := range dbByID {
		if _, ok := names[id]; !ok {
			names[id] = id
		}
	}

	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	agents := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		entry := map[string]any{
			"agent_id": id,
			"name":     names[id],
		}

		// Reuse the AgentState already returned by ListAgents - avoids an N+1
		// round-trip per agent. Config-only agents that have never traded have
		// no row and are rendered without portfolio data.
		if state, ok := dbByID[id]; ok {
			equity, _ := repo.ComputeEquity(ctx, id, prices)
			entry["portfolio"] = map[string]any{
				"value": equity.String(),
			}
			entry["fill_count"] = state.FillCount
			entry["halted"] = state.Halted
			if state.LastSeenAt != nil {
				entry["last_seen_at"] = state.LastSeenAt.UTC().Format(time.RFC3339)
			}
		}

		agents = append(agents, entry)
	}

	return mcp.JSONResult(map[string]any{"agents": agents, "count": len(agents)})
}
