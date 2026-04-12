package simple_llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/observability"

	"go.uber.org/zap"
)

// PlaceholderFetcher fetches data to fill a placeholder.
// Returns empty string on failure (non-fatal).
type PlaceholderFetcher func(ctx context.Context) string

// expandPlaceholders replaces known placeholder tokens in prompt with fetched data.
// Only invokes the fetcher when the token is present in the prompt (lazy - no call if absent).
func expandPlaceholders(ctx context.Context, prompt string, placeholders map[string]PlaceholderFetcher) string {
	for token, fetch := range placeholders {
		if !strings.Contains(prompt, token) {
			continue
		}
		prompt = strings.ReplaceAll(prompt, token, fetch(ctx))
	}
	return prompt
}

// loadMemoryContext reads memory/MEMORY.md and recent session logs from the Files MCP
// and returns formatted text for injection into the {{memory}} placeholder.
//
// Design: section headers use the exact file path (e.g. "memory/MEMORY.md") so the LLM
// recognises which files are already loaded and does not redundantly call files/read for
// them during the session. The outer header carries an explicit "do NOT re-fetch" note.
//
// All failures are non-fatal — a new agent with no memory starts cleanly.
func (s *Service) loadMemoryContext(filesClient *mcp.Client) string {
	if filesClient == nil {
		return ""
	}
	s.memoryLog.Info("Prefetching memory files")

	var parts []string
	parts = append(parts, "## Pre-loaded Memory\n*Loaded at session start.*")

	// Only pre-load MEMORY.md - the curated persistent memory.
	// Session logs are write-only journals: the agent appends to them but doesn't need
	// them pre-loaded. Use files/read to look back if needed.
	content := s.readFileContent(filesClient, "memory/MEMORY.md")
	parts = append(parts, "\n### memory/MEMORY.md")
	if content != "" {
		parts = append(parts, content)
	} else {
		parts = append(parts, "No core memory yet. This is your first session — create memory/MEMORY.md at the end.")
	}

	return strings.Join(parts, "\n")
}

// readFileContent calls files/read for a single path and returns the content.
// Returns empty string on any error so callers can always use the fallback text.
// Distinguishes connection errors (Warn) from missing files (Info) for clearer ops visibility.
func (s *Service) readFileContent(client *mcp.Client, path string) string {
	result, err := client.CallTool("files/read", map[string]any{"path": path})
	if err != nil {
		s.memoryLog.Warn("files MCP unreachable, skipping: "+path, zap.Error(err))
		return ""
	}
	if result.IsError || len(result.Content) == 0 {
		s.memoryLog.Info("memory file not found (will be created this session): " + path)
		return ""
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		s.memoryLog.Warn("memory file parse failed: "+path, zap.Error(err))
		return ""
	}
	preview := observability.LogPreview(payload.Content, 120)
	s.memoryLog.Info(fmt.Sprintf("memory loaded: %s (%d bytes): %s", path, len(payload.Content), preview))
	// Trim trailing newlines so the content shown in the prompt matches the file exactly.
	// strings.Join adds "\n" between parts, which would make the file appear to end with
	// a newline it may not have — causing old_text mismatches in files/edit.
	return strings.TrimRight(payload.Content, "\n")
}

// fetchMarketContext fetches portfolio + prices and returns formatted text for the
// {{market_context}} placeholder. Market coverage: open position markets first (so the
// agent always sees its own exposure), then cfg.Markets as defaults.
//
// Design: sub-headers use MCP tool names ("trade/get_portfolio", "market/get_prices")
// so the LLM can match them to tool calls it might consider and skip the redundant fetch.
//
// Both MCP calls are non-fatal: partial data is returned on failure.
func (s *Service) fetchMarketContext(ctx context.Context) string {
	s.marketLog.Info("Prefetching market data")
	tradingClient := s.toolset.Client("trading")
	marketClient := s.toolset.Client("market")

	// 1. Get portfolio and extract open position markets.
	portfolio, positionMarkets := s.fetchPortfolio(ctx, tradingClient)

	// 2. Union with configured default markets (preserving position markets first).
	markets := positionMarkets
	for _, m := range s.cfg.Markets {
		if !containsStr(markets, m) {
			markets = append(markets, m)
		}
	}

	// 3. Get prices for relevant markets.
	var pricesText string
	if len(markets) > 0 && marketClient != nil {
		pricesText = s.fetchPricesText(ctx, marketClient, markets)
	}

	// 4. Combine into a single context block.
	portfolioText := formatPortfolioText(portfolio)
	if portfolioText == "" && pricesText == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Pre-loaded Market Context — %s\n*Already fetched — do NOT re-call trade/get_portfolio or market/get_prices this session.*",
		time.Now().UTC().Format("2006-01-02 15:04 UTC")))
	if portfolioText != "" {
		sb.WriteString("\n\n")
		sb.WriteString(portfolioText)
	}
	if pricesText != "" {
		sb.WriteString("\n\n")
		sb.WriteString(pricesText)
	}
	return sb.String()
}

// portfolioData is the parsed response from trade/get_portfolio.
type portfolioData struct {
	Portfolio   portfolioNested `json:"portfolio"`
	Positions   []positionData `json:"positions"`
	FillCount   int            `json:"fill_count"`
	RejectCount int            `json:"reject_count"`
	Halted      bool           `json:"halted"`
}

type portfolioNested struct {
	Value string `json:"value"`
	Cash  string `json:"cash"`
	Peak  string `json:"peak"`
}

type positionData struct {
	Pair     string `json:"pair"`
	Side     string `json:"side"`
	Qty      string `json:"qty"`
	AvgPrice string `json:"avg_price"`
	Value    string `json:"value"`
}

// priceData holds ticker fields from market/get_prices response.
type priceData struct {
	Market       string `json:"market"`
	Last         string `json:"last"`
	Change24hPct string `json:"change_24h_pct"`
	High24h      string `json:"high_24h"`
	Low24h       string `json:"low_24h"`
}

// fetchPortfolio calls trade/get_portfolio and returns the parsed portfolio
// and the list of markets from open positions.
func (s *Service) fetchPortfolio(ctx context.Context, client *mcp.Client) (*portfolioData, []string) {
	if client == nil {
		return nil, nil
	}
	result, err := client.CallTool("trade/get_portfolio", nil)
	if err != nil || result.IsError || len(result.Content) == 0 {
		s.marketLog.Warn("trade/get_portfolio failed", zap.Error(err))
		return nil, nil
	}
	var p portfolioData
	if err := json.Unmarshal([]byte(result.Content[0].Text), &p); err != nil {
		s.marketLog.Warn("trade/get_portfolio parse failed", zap.Error(err))
		return nil, nil
	}
	var markets []string
	for _, pos := range p.Positions {
		if pos.Pair != "" && !containsStr(markets, pos.Pair) {
			markets = append(markets, pos.Pair)
		}
	}
	s.marketLog.Info(fmt.Sprintf("portfolio: %d positions, markets: %v, cash: %s", len(p.Positions), markets, p.Portfolio.Cash))
	return &p, markets
}

// fetchPricesText calls market/get_prices for the given markets and returns formatted text.
func (s *Service) fetchPricesText(ctx context.Context, client *mcp.Client, markets []string) string {
	s.marketLog.Info(fmt.Sprintf("fetching prices for %v", markets))
	marketsArg := make([]any, len(markets))
	for i, m := range markets {
		marketsArg[i] = m
	}
	result, err := client.CallTool("market/get_prices", map[string]any{"markets": marketsArg})
	if err != nil || result.IsError || len(result.Content) == 0 {
		s.marketLog.Warn("market/get_prices failed", zap.Error(err))
		return ""
	}
	var resp struct {
		Prices []priceData `json:"prices"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &resp); err != nil {
		s.marketLog.Warn("market/get_prices parse failed", zap.Error(err))
		return ""
	}
	s.marketLog.Info(fmt.Sprintf("prices loaded: %d markets", len(resp.Prices)))
	return formatPricesText(resp.Prices)
}

// formatPortfolioText formats portfolio data as readable text for LLM context.
// Returns empty string if portfolio is nil.
func formatPortfolioText(p *portfolioData) string {
	if p == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("### trade/get_portfolio\n")
	sb.WriteString(fmt.Sprintf("Cash: %s\n", p.Portfolio.Cash))
	sb.WriteString(fmt.Sprintf("Portfolio Value: %s (peak: %s)\n", p.Portfolio.Value, p.Portfolio.Peak))
	sb.WriteString(fmt.Sprintf("Trades: %d fills, %d rejects", p.FillCount, p.RejectCount))

	if len(p.Positions) > 0 {
		sb.WriteString("\n\n### Open Positions\n")
		for _, pos := range p.Positions {
			sb.WriteString(fmt.Sprintf("- %s %s: %s units @ %s (value: %s)\n",
				pos.Pair, strings.ToUpper(pos.Side), pos.Qty, pos.AvgPrice, pos.Value))
		}
	} else {
		sb.WriteString("\nNo open positions.")
	}
	return sb.String()
}

// formatPricesText formats ticker data as readable text for LLM context.
// Returns empty string if prices is empty.
func formatPricesText(prices []priceData) string {
	if len(prices) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("### market/get_prices\n")
	for _, p := range prices {
		sb.WriteString(fmt.Sprintf("- %s: %s | 24h: %s%% | H: %s | L: %s\n",
			p.Market, p.Last, p.Change24hPct, p.High24h, p.Low24h))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// containsStr returns true if slice contains s.
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
