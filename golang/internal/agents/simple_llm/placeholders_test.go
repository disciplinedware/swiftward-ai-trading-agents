package simple_llm

import (
	"context"
	"strings"
	"testing"
)

func TestExpandPlaceholders(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		prompt       string
		placeholders map[string]PlaceholderFetcher
		want         string
		wantCalled   map[string]bool // which fetchers should have been called
	}{
		{
			name:   "no placeholders - no fetchers called",
			prompt: "Just a plain prompt with no tokens",
			placeholders: map[string]PlaceholderFetcher{
				"{{memory}}": func(_ context.Context) string { return "SHOULD NOT APPEAR" },
			},
			want:       "Just a plain prompt with no tokens",
			wantCalled: map[string]bool{"{{memory}}": false},
		},
		{
			name:   "single placeholder replaced",
			prompt: "Hello {{memory}} World",
			placeholders: map[string]PlaceholderFetcher{
				"{{memory}}": func(_ context.Context) string { return "MY MEMORY" },
			},
			want:       "Hello MY MEMORY World",
			wantCalled: map[string]bool{"{{memory}}": true},
		},
		{
			name:   "multiple occurrences of same placeholder all replaced",
			prompt: "{{memory}} and again {{memory}}",
			placeholders: map[string]PlaceholderFetcher{
				"{{memory}}": func(_ context.Context) string { return "DATA" },
			},
			want: "DATA and again DATA",
		},
		{
			name:   "fetcher returns empty - placeholder removed",
			prompt: "Before {{memory}} After",
			placeholders: map[string]PlaceholderFetcher{
				"{{memory}}": func(_ context.Context) string { return "" },
			},
			want: "Before  After",
		},
		{
			name:   "multiple placeholders - only present ones fetched",
			prompt: "A: {{memory}} B: {{market_context}}",
			placeholders: map[string]PlaceholderFetcher{
				"{{memory}}":         func(_ context.Context) string { return "MEM" },
				"{{market_context}}": func(_ context.Context) string { return "MKT" },
			},
			want: "A: MEM B: MKT",
		},
		{
			name:   "absent placeholder fetcher not called",
			prompt: "Only {{memory}} here",
			placeholders: map[string]PlaceholderFetcher{
				"{{memory}}":         func(_ context.Context) string { return "MEM" },
				"{{market_context}}": func(_ context.Context) string { return "SHOULD NOT APPEAR" },
			},
			want: "Only MEM here",
		},
		{
			name:         "empty prompt unchanged",
			prompt:       "",
			placeholders: map[string]PlaceholderFetcher{"{{memory}}": func(_ context.Context) string { return "X" }},
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPlaceholders(ctx, tt.prompt, tt.placeholders)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandPlaceholders_LazyEvaluation(t *testing.T) {
	// Verify fetcher is NOT called when placeholder is absent.
	ctx := context.Background()
	called := false
	expandPlaceholders(ctx, "no placeholder here", map[string]PlaceholderFetcher{
		"{{memory}}": func(_ context.Context) string {
			called = true
			return "data"
		},
	})
	if called {
		t.Error("fetcher should not be called when placeholder is absent")
	}
}

func TestFormatPortfolioText(t *testing.T) {
	tests := []struct {
		name     string
		input    *portfolioData
		wantNil  bool
		contains []string
		absent   []string
	}{
		{
			name:    "nil portfolio returns empty",
			input:   nil,
			wantNil: true,
		},
		{
			name: "portfolio with no positions",
			input: &portfolioData{
				Portfolio:   portfolioNested{Value: "10000.00", Cash: "10000.00", Peak: "10000.00"},
				FillCount:   0,
				RejectCount: 0,
				Positions:   nil,
			},
			contains: []string{"### trade/get_portfolio", "10000.00", "No open positions", "Trades: 0 fills, 0 rejects"},
			absent:   []string{"### Open Positions"},
		},
		{
			name: "portfolio with open positions",
			input: &portfolioData{
				Portfolio:   portfolioNested{Value: "11200.00", Cash: "8000.00", Peak: "11500.00"},
				FillCount:   3,
				RejectCount: 1,
				Positions: []positionData{
					{Pair: "ETH-USDC", Side: "long", Qty: "0.500", AvgPrice: "2800.00", Value: "1400.00"},
					{Pair: "BTC-USDC", Side: "short", Qty: "0.010", AvgPrice: "61000.00", Value: "610.00"},
				},
			},
			contains: []string{
				"### trade/get_portfolio",
				"8000.00", "11200.00", "11500.00",
				"Trades: 3 fills, 1 rejects",
				"ETH-USDC", "LONG", "2800.00",
				"BTC-USDC", "SHORT",
				"### Open Positions",
			},
		},
		{
			name: "side is uppercased",
			input: &portfolioData{
				Portfolio: portfolioNested{Value: "1000.00", Cash: "1000.00", Peak: "1000.00"},
				Positions: []positionData{{Pair: "ETH-USDC", Side: "long", Qty: "1", AvgPrice: "2000", Value: "2000"}},
			},
			contains: []string{"LONG"},
			absent:   []string{"long"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPortfolioText(tt.input)
			if tt.wantNil {
				if got != "" {
					t.Errorf("expected empty string for nil input, got %q", got)
				}
				return
			}
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("expected output to contain %q\ngot: %s", want, got)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("expected output NOT to contain %q\ngot: %s", absent, got)
				}
			}
		})
	}
}

func TestFormatPricesText(t *testing.T) {
	tests := []struct {
		name     string
		input    []priceData
		wantNil  bool
		contains []string
	}{
		{
			name:    "empty prices returns empty",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "empty slice returns empty",
			input:   []priceData{},
			wantNil: true,
		},
		{
			name: "single price formatted correctly",
			input: []priceData{
				{Market: "ETH-USDC", Last: "2845.32", Change24hPct: "+2.10", High24h: "2900.00", Low24h: "2780.00"},
			},
			contains: []string{"ETH-USDC", "2845.32", "+2.10%", "2900.00", "2780.00", "### market/get_prices"},
		},
		{
			name: "multiple prices all included",
			input: []priceData{
				{Market: "ETH-USDC", Last: "2845.32", Change24hPct: "+2.10", High24h: "2900.00", Low24h: "2780.00"},
				{Market: "BTC-USDC", Last: "62100.00", Change24hPct: "-0.50", High24h: "62500.00", Low24h: "61800.00"},
			},
			contains: []string{"ETH-USDC", "BTC-USDC", "62100.00", "-0.50%"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPricesText(tt.input)
			if tt.wantNil {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("expected output to contain %q\ngot: %s", want, got)
				}
			}
		})
	}
}

func TestContainsStr(t *testing.T) {
	tests := []struct {
		slice []string
		s     string
		want  bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{nil, "a", false},
		{[]string{}, "a", false},
		{[]string{"ETH-USDC"}, "ETH-USDC", true},
	}
	for _, tt := range tests {
		got := containsStr(tt.slice, tt.s)
		if got != tt.want {
			t.Errorf("containsStr(%v, %q) = %v, want %v", tt.slice, tt.s, got, tt.want)
		}
	}
}
