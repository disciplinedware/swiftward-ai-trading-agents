package polymarket

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const maxMarketsPerEvent = 5

// formatEventResults formats events with their markets grouped for the LLM.
func formatEventResults(events []GammaEvent) string {
	if len(events) == 0 {
		return "No markets found matching your criteria."
	}
	var sb strings.Builder
	for i, e := range events {
		fmt.Fprintf(&sb, "%d. EVENT: %s\n", i+1, e.Title)
		shown := e.Markets
		if len(shown) > maxMarketsPerEvent {
			shown = shown[:maxMarketsPerEvent]
		}
		for _, m := range shown {
			odds := formatOdds(m.OutcomePrices, m.Outcomes)
			vol := formatVolume(m.Volume24hr)
			closes := formatTimeUntil(m.EndDate)
			fmt.Fprintf(&sb, "   - %s | %s | Vol 24h: %s | Closes: %s | market_id: %s\n",
				m.Question, odds, vol, closes, m.ID)
		}
		if len(e.Markets) > maxMarketsPerEvent {
			fmt.Fprintf(&sb, "   ... +%d more markets in this event\n", len(e.Markets)-maxMarketsPerEvent)
		}
		if i < len(events)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// formatMarketDetail formats a full market deep-dive for the LLM.
func formatMarketDetail(market *GammaMarket, book *OrderBook, event *GammaEvent) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "MARKET: %q\n\n", market.Question)

	if market.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n", market.Description)
	}
	if market.ResolutionSource != "" {
		fmt.Fprintf(&sb, "Resolution source: %s\n", market.ResolutionSource)
	}
	closes := formatTimeUntil(market.EndDate)
	fmt.Fprintf(&sb, "Closes: %s (%s)\n", market.EndDate, closes)

	sb.WriteString("\nCURRENT STATE:\n")
	odds := formatOdds(market.OutcomePrices, market.Outcomes)
	fmt.Fprintf(&sb, "  Odds: %s\n", odds)
	if market.Spread > 0 {
		fmt.Fprintf(&sb, "  Spread: %.0fc ($%.2f)\n", market.Spread*100, market.Spread)
	}
	if market.LastTradePrice > 0 {
		fmt.Fprintf(&sb, "  Last trade: $%.4f\n", market.LastTradePrice)
	}

	sb.WriteString("\nVOLUME:\n")
	vol24 := formatVolume(market.Volume24hr)
	vol7d := formatVolume(market.Volume1wk)
	volTotal := formatVolume(parseFloat(market.Volume))
	liq := formatVolume(market.LiquidityNum)
	fmt.Fprintf(&sb, "  24h: %s | 7d: %s | Total: %s\n", vol24, vol7d, volTotal)
	fmt.Fprintf(&sb, "  Liquidity: %s\n", liq)

	if book != nil && (len(book.Bids) > 0 || len(book.Asks) > 0) {
		sb.WriteString("\nORDER BOOK (YES side):\n")
		sb.WriteString("  " + summarizeBook(book) + "\n")
	}

	fee := formatFeeSchedule(market.FeeSchedule, market.FeeType)
	fmt.Fprintf(&sb, "\nFEES: %s\n", fee)

	if event != nil && len(event.Markets) > 1 {
		sb.WriteString("\nRELATED MARKETS (same event):\n")
		for _, sibling := range event.Markets {
			if sibling.ID == market.ID {
				continue
			}
			sibOdds := formatOdds(sibling.OutcomePrices, sibling.Outcomes)
			fmt.Fprintf(&sb, "  - %q - %s (market_id: %s)\n", sibling.Question, sibOdds, sibling.ID)
		}
	}

	return sb.String()
}

// formatOdds formats outcome prices into human-readable odds string.
// e.g. "YES 82% / NO 18%" or "A 40% / B 35% / C 25%"
func formatOdds(outcomePrices []string, outcomes []string) string {
	if len(outcomePrices) == 0 {
		return "unknown"
	}
	parts := make([]string, 0, len(outcomePrices))
	for i, priceStr := range outcomePrices {
		p := parseFloat(priceStr)
		pct := int(math.Round(p * 100))
		label := fmt.Sprintf("Option%d", i+1)
		if i < len(outcomes) && outcomes[i] != "" {
			label = outcomes[i]
		}
		parts = append(parts, fmt.Sprintf("%s %d%%", label, pct))
	}
	return strings.Join(parts, " / ")
}

// formatVolume formats a dollar value compactly: $3.5M, $800K, $1,200.
func formatVolume(v float64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("$%.1fM", v/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("$%.1fK", v/1_000)
	}
	return fmt.Sprintf("$%.0f", v)
}

// formatTimeUntil returns a human-readable duration until endDate (RFC3339 or date string).
func formatTimeUntil(endDate string) string {
	if endDate == "" {
		return "unknown"
	}
	formats := []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"}
	var t time.Time
	var parsed bool
	for _, f := range formats {
		if pt, err := time.Parse(f, endDate); err == nil {
			t = pt
			parsed = true
			break
		}
	}
	if !parsed {
		return endDate
	}
	d := time.Until(t)
	if d <= 0 {
		return "closed"
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}

// formatFeeSchedule formats fee information for display.
func formatFeeSchedule(fs *FeeSchedule, feeType string) string {
	if fs == nil {
		if feeType != "" {
			return feeType
		}
		return "unknown"
	}
	if fs.Rate == 0 {
		label := "free"
		if feeType != "" {
			label = fmt.Sprintf("0%% (%s)", feeType)
		}
		return label
	}
	base := fmt.Sprintf("%.1f%% taker", fs.Rate*100)
	if fs.RebateRate > 0 {
		base += fmt.Sprintf(" / +%.1f%% maker rebate", fs.RebateRate*100)
	}
	if feeType != "" {
		base += fmt.Sprintf(" (%s)", feeType)
	}
	return base
}

// summarizeBook returns a compact summary of the order book.
func summarizeBook(book *OrderBook) string {
	if len(book.Bids) == 0 && len(book.Asks) == 0 {
		return "empty"
	}

	bestBidPrice, bestBidSize := topLevel(book.Bids, true)
	bestAskPrice, bestAskSize := topLevel(book.Asks, false)

	var parts []string
	if bestBidPrice > 0 {
		parts = append(parts, fmt.Sprintf("Best bid: $%.2f (%.0f shares)", bestBidPrice, bestBidSize))
	}
	if bestAskPrice > 0 {
		parts = append(parts, fmt.Sprintf("Best ask: $%.2f (%.0f shares)", bestAskPrice, bestAskSize))
	}

	depth2c := depthWithin(book.Bids, bestBidPrice, 0.02)
	depth5c := depthWithin(book.Bids, bestBidPrice, 0.05)
	if depth2c > 0 {
		parts = append(parts, fmt.Sprintf("Depth within 2c of best bid: %s", formatVolume(depth2c)))
	}
	if depth5c > 0 {
		parts = append(parts, fmt.Sprintf("Depth within 5c of best bid: %s", formatVolume(depth5c)))
	}

	return strings.Join(parts, " | ")
}

// topLevel returns the price and size of the best bid or ask level.
// For bids: highest price; for asks: lowest price.
func topLevel(levels []OrderBookLevel, isBid bool) (price, size float64) {
	found := false
	for _, l := range levels {
		p := parseFloat(l.Price)
		if p <= 0 {
			continue
		}
		if !found || (isBid && p > price) || (!isBid && p < price) {
			price = p
			size = parseFloat(l.Size)
			found = true
		}
	}
	return price, size
}

// depthWithin returns total dollar value of bids within `within` cents of the best bid.
func depthWithin(bids []OrderBookLevel, bestBid, within float64) float64 {
	if bestBid <= 0 {
		return 0
	}
	var total float64
	floor := bestBid - within
	for _, l := range bids {
		p := parseFloat(l.Price)
		s := parseFloat(l.Size)
		if p >= floor {
			total += p * s
		}
	}
	return total
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
