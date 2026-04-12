# Hackathon Landing Page

> **Status**: ✅ Shipped
> **URL**: https://ai-trading.swiftward.dev
> **Source**: `landing/`

## What was built

A single-page marketing website for the AI Trading Agents Hackathon that showcases the full platform: multi-agent analysis, MCP trading platform, declarative risk engine, and on-chain evidence via ERC-8004. Targets hackathon jury and investors with live screenshots from the running system, demo videos, team profiles, and a linked AgentIntel audit of every agent in the hackathon.

## Structure

The landing page (`landing/index.html`, ~1,130 lines, vanilla HTML/CSS/JS) follows a linear narrative:

- **Nav** (lines 540-554): sticky top bar with section anchors and GitHub CTA
- **Ticker** (lines 556-559): animated crypto price row (ETH, BTC, NEAR, ZEC, SOL, ADA)
- **Hero** (lines 562-592): headline, subheadline, two CTAs, stats row (3 agent architectures, 45 MCP tools, 31 risk rules, 4,772 on-chain trades)
- **Video preview** (lines 594-617): embedded MP4 demo + Descript walkthrough iframe
- **Three pillars intro** (lines 620-646): Smarter Agents / Trading Platform / Super Safe
- **Pillar 1 - Smarter Agents** (lines 650-760, `#pillar1`): 6 agent cards with screenshots of debate, deterministic quant, arena, code sandbox, self-improvement, harness runtime
- **Pillar 2 - Trading Platform** (lines 762-883, `#pillar2`): 6 feature cards plus on-chain agent table (agentId 32, 37, 43, 49) linking Sepolia wallets
- **Pillar 3 - Super Safe** (lines 885-972, `#pillar3`): 6 safety cards covering declarative rules, graduated tiers, safe rollout, 3 gateways, observability, audit trail
- **Team** (lines 974-1013, `#team`): four cards with real photos and LinkedIn links
- **Agent Intel callout** (lines 1015-1033, `#agentintel`): link to the bundled AgentIntel audit of all 57 hackathon agents
- **CTA** (lines 1035-1050, `#cta`): "Run it yourself" with GitHub and quick-start command
- **Footer** (lines 1052-1057)

## Key files

- `landing/index.html` - single-page app (~1,130 lines). Inline `<style>` (~530 lines of CSS) and vanilla JS (~200 lines) for particle canvas, scroll reveal, ticker loop, lightbox, mobile nav toggle.
- `landing/Swiftward_ AI-Driven Automated Trading_720p_caption.mp4` - primary demo video (captioned)
- `landing/Swiftward_ AI-Trading-Agents Overview_720p_caption.mp4` - secondary overview video
- `landing/Swiftward_Trading_Harness.pdf` / `.pptx` - pitch deck
- `landing/Universal_AI_Trading_Harness.pdf` / `.pptx` - extended deck
- `landing/audit/` - AgentIntel audit subsite: `index.html` (leaderboard table of 57 agents) + per-agent detail pages + `style.css`

### Screenshots (real dashboards, not mockups)

Grouped by pillar:

- **Agent Intelligence**: `alert-fake-news.png`, `telegram-3+2subagents.png`, `telegram-self-improving-learning.png`, `telegram-newagent-learns.png`, `telegram-interact.jpg`, `telegram-alerts-existing.png`, `telegram-alert-triggered.png`, `arena-runs.png`, `agena-batch.png`, `arena-agent-analysis.png`, `determemistic_agent_flow.svg`, `agents-are-isolaged-in-folders.png`
- **Trading Platform**: `dashboard-market-prices.png`, `dashboard-agent-position.png`, `dashboard-agent-active-alerts.png`, `dashboard-newsfeed.png`, `currentposition.png`, `trades-with-profit.jpg`, `market-alert-set-and-triggeted.png`
- **Evidence / On-chain**: `dashboard-trades-and-evidences.png`, `dashboard-decision-hash-chain.png`, `dashboard-decision-hash-chain-item.png`, `dashboard-evidence-url.png`
- **Risk engine**: `dashboard-risk-policies.png`, `dashboard-trades-and-rejected-by-policy.png`, `swiftward-analytics.png`, `swiftward-investigate-events-trade-order.png`, `swiftward-loss-streak.png`, `swiftward-blocked-tier10pct.png`, `swiftward-blocked-opening-new-positions-due-to-3-losses.png`, `swiftward-inet-blocked.png`, `loss-streak-block.png`, `signoz-logs.png`
- **Team**: `kostya.jpg`, `tikhon.jpeg`, `ivan.jpg`, `ruslan.jpg`

## Tech / delivery

- **Stack**: vanilla HTML + CSS + JS. No framework. Google Fonts API for Inter + Space Grotesk. Design system via CSS custom properties (`--green`, `--blue`, `--purple`, `--bg`, `--card`, `--border`, etc.).
- **Animations**: particle canvas background, `fadeUp` scroll reveal, ticker loop, lightbox modal for screenshots, sticky nav with mobile toggle at 768px.
- **Hosting**: static site at `https://ai-trading.swiftward.dev`. Open Graph and Twitter Card meta tags configured (lines 9-24) for social sharing previews.
- **Responsive**: single-column fallback below 768px, hero stats and pillar grids reflow, screen grid collapses to 1 column.

## Notes

- Every screenshot is a real capture from the running system. No mockups, no placeholders. This is intentional: the site doubles as proof-of-ship for the jury.
- The AgentIntel subsite at `landing/audit/` is an independent analysis of all 57 hackathon agents across 4,772 trades and $919K volume, with per-agent AI verdicts ("Real Trader" vs "Gamer") and sybil-pattern detection. It is unique to this submission.
- Team bios link real LinkedIn profiles (`in/ktrunin`, `in/jokly`, `in/ivan-puchkov`, `in/ruslan-mukhametov-5838bb1a2`).
- The site shares the top-level domain with `swiftward.dev` (the policy engine). Attentive visitors can strip the subdomain to discover the product that powers Pillar 3.
