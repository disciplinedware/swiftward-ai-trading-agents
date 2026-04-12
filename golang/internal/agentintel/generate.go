package agentintel

import (
	"embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

//go:embed templates/*
var templateFS embed.FS

// Generator produces static HTML from computed data.
type Generator struct {
	paths Paths
	log   *zap.Logger
}

// NewGenerator creates a site generator.
func NewGenerator(paths Paths, log *zap.Logger) *Generator {
	return &Generator{paths: paths, log: log}
}

// Generate renders all HTML pages.
func (g *Generator) Generate() error {
	// Load computed data.
	var agents []ComputedAgent
	found, err := LoadJSON(filepath.Join(g.paths.Computed, "agents.json"), &agents)
	if err != nil {
		return fmt.Errorf("load agents: %w", err)
	}
	if !found {
		return fmt.Errorf("no computed data found - run calculate first")
	}

	// Sort by hackathon score (validation + reputation average) descending.
	// Tie-break by net PnL descending. NetPnL alone is a bad leaderboard metric
	// because it scales with deployed capital and paper-leverage.
	sort.Slice(agents, func(i, j int) bool {
		si := agents[i].State.ValidationScore + agents[i].State.ReputationScore
		sj := agents[j].State.ValidationScore + agents[j].State.ReputationScore
		if si != sj {
			return si > sj
		}
		pi, _ := decimal.NewFromString(agents[i].Summary.NetPnL)
		pj, _ := decimal.NewFromString(agents[j].Summary.NetPnL)
		return pi.GreaterThan(pj)
	})

	// Parse templates.
	funcMap := template.FuncMap{
		"add":       func(a, b int) int { return a + b },
		"shortAddr": shortAddr,
		"pnlClass":  pnlClass,
		"fmtTime":   fmtTime,
		"hasPrefix": strings.HasPrefix,
		"join":      strings.Join,
		"fmtUSD": func(s string) template.HTML {
			if s == "" || s == "-" {
				return "-"
			}
			if strings.HasPrefix(s, "-") {
				return template.HTML("-$" + s[1:])
			}
			return template.HTML("$" + s)
		},
		"fmtUnix": func(ts int64) string {
			if ts == 0 {
				return "-"
			}
			return time.Unix(ts, 0).UTC().Format("Jan 02 15:04")
		},
		"actionClass": func(action string) string {
			upper := strings.ToUpper(action)
			if upper == "BUY" || upper == "LONG" {
				return "action-buy"
			}
			if upper == "SELL" || upper == "SHORT" || strings.HasPrefix(upper, "CLOSE") {
				return "action-sell"
			}
			return "action-other"
		},
		"feedbackTypeName": func(ft int) string {
			switch ft {
			case 0:
				return "Trade"
			case 1:
				return "Risk"
			case 2:
				return "Strategy"
			case 3:
				return "General"
			default:
				return fmt.Sprintf("%d", ft)
			}
		},
		"deref": func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		},
		"positionAfter": func(trade ComputedTrade) string {
			cp := trade.CanonicalPair
			if pos, ok := trade.PortfolioAfter[cp]; ok {
				return pos.Qty
			}
			return "0"
		},
		"positionBefore": func(trades []ComputedTrade, idx int) string {
			if idx == 0 {
				return "0"
			}
			cp := trades[idx].CanonicalPair
			// Walk back to find the most recent trade with this pair's position.
			for j := idx - 1; j >= 0; j-- {
				if pos, ok := trades[j].PortfolioAfter[cp]; ok {
					return pos.Qty
				}
			}
			return "0"
		},
		"isPnLNegative": func(s *string) bool {
			if s == nil {
				return false
			}
			d, _ := decimal.NewFromString(*s)
			return d.IsNegative()
		},
	}

	indexTmpl, err := template.New("index.html.tmpl").Funcs(funcMap).ParseFS(templateFS, "templates/index.html.tmpl")
	if err != nil {
		return fmt.Errorf("parse index template: %w", err)
	}

	agentTmpl, err := template.New("agent.html.tmpl").Funcs(funcMap).ParseFS(templateFS, "templates/agent.html.tmpl")
	if err != nil {
		return fmt.Errorf("parse agent template: %w", err)
	}

	// Load LLM analysis and extract verdicts.
	analyses := make(map[int64]string) // agentID -> markdown
	for i, a := range agents {
		path := filepath.Join(g.paths.Computed, "agents", fmt.Sprintf("%d_analysis.md", a.Agent.ID))
		data, err := os.ReadFile(path)
		if err == nil {
			text := string(data)
			analyses[a.Agent.ID] = text
			verdict := extractVerdict(text)
			agents[i].Summary.AIVerdict = verdict
			if verdict != "" {
				g.log.Debug("Extracted verdict", zap.Int64("agent", a.Agent.ID), zap.String("verdict", verdict))
			}
		} else {
			g.log.Debug("No analysis file", zap.Int64("agent", a.Agent.ID), zap.String("path", path))
		}
	}

	// Compute summary stats. Sum of peak exposures across all agents - shows
	// the total paper capital committed across the cohort at peak.
	totalTrades := 0
	totalExposure := decimal.Zero
	for _, a := range agents {
		totalTrades += a.Summary.TotalTrades
		v, _ := decimal.NewFromString(a.Summary.MaxExposure)
		totalExposure = totalExposure.Add(v)
	}

	// Render index.
	indexData := struct {
		Agents        []ComputedAgent
		GeneratedAt   string
		TotalAgents   int
		TotalTrades   int
		TotalExposure string
	}{
		Agents:        agents,
		GeneratedAt:   time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		TotalAgents:   len(agents),
		TotalTrades:   totalTrades,
		TotalExposure: totalExposure.StringFixed(2),
	}

	indexPath := filepath.Join(g.paths.Site, "index.html")
	if err := renderToFile(indexTmpl, indexPath, indexData); err != nil {
		return fmt.Errorf("render index: %w", err)
	}

	// Render per-agent pages.
	for i, a := range agents {
		agentData := struct {
			Agent       ComputedAgent
			Rank        int
			Analysis    string
			GeneratedAt string
		}{
			Agent:       a,
			Rank:        i + 1,
			Analysis:    analyses[a.Agent.ID],
			GeneratedAt: time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		}

		agentPath := filepath.Join(g.paths.Site, "agents", fmt.Sprintf("%d.html", a.Agent.ID))
		if err := renderToFile(agentTmpl, agentPath, agentData); err != nil {
			return fmt.Errorf("render agent %d: %w", a.Agent.ID, err)
		}
	}

	// Copy style.css.
	cssData, err := templateFS.ReadFile("templates/style.css")
	if err != nil {
		return fmt.Errorf("read style.css: %w", err)
	}
	if err := os.WriteFile(filepath.Join(g.paths.Site, "style.css"), cssData, 0o644); err != nil {
		return fmt.Errorf("write style.css: %w", err)
	}

	g.log.Info("Site generated",
		zap.String("path", g.paths.Site),
		zap.Int("agents", len(agents)),
	)
	return nil
}

func renderToFile(tmpl *template.Template, path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return tmpl.Execute(f, data)
}

func shortAddr(addr string) string {
	if len(addr) < 12 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}

// extractVerdict pulls the verdict line from analysis text and categorizes it.
func extractVerdict(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "verdict:") {
			verdict := strings.TrimSpace(line[8:])
			// Categorize into badges.
			lv := strings.ToLower(verdict)
			switch {
			case strings.Contains(lv, "leaderboard gamer"):
				return "Gamer"
			case strings.Contains(lv, "real trader") || strings.Contains(lv, "legitimate") || strings.Contains(lv, "genuine"):
				return "Real Trader"
			case strings.Contains(lv, "broken") || strings.Contains(lv, "abandoned") || strings.Contains(lv, "inactive"):
				return "Inactive"
			case strings.Contains(lv, "test") || strings.Contains(lv, "minimal") || strings.Contains(lv, "placeholder"):
				return "Test"
			default:
				// Truncate to first sentence.
				if idx := strings.IndexAny(verdict, ".!"); idx > 0 && idx < 60 {
					return verdict[:idx]
				}
				if len(verdict) > 60 {
					return verdict[:57] + "..."
				}
				return verdict
			}
		}
	}
	return ""
}

func pnlClass(pnl string) string {
	d, err := decimal.NewFromString(pnl)
	if err != nil {
		return ""
	}
	if d.IsPositive() {
		return "positive"
	}
	if d.IsNegative() {
		return "negative"
	}
	return ""
}

func fmtTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("Jan 02 15:04")
}
