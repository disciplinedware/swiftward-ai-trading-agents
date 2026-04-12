package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// newTestService creates a Service wired to the given test servers.
func newTestService(gammaURL, clobURL string) *Service {
	return &Service{
		log:   zap.NewNop(),
		gamma: NewGammaClient(gammaURL),
		clob:  NewCLOBClient(clobURL),
	}
}

// testMarket returns a minimal valid GammaMarket.
func testMarket(id, question string, yesOdds, liquidity float64) GammaMarket {
	return GammaMarket{
		ID:            id,
		Question:      question,
		EndDate:       "2026-12-31T00:00:00Z",
		Outcomes:      []string{"Yes", "No"},
		OutcomePrices: []string{fmt.Sprintf("%g", yesOdds), fmt.Sprintf("%g", 1-yesOdds)},
		LiquidityNum:  liquidity,
		Volume24hr:    50000,
		Volume1wk:     200000,
		Volume:        "1000000",
		ClobTokenIDs:  []string{"token-" + id},
		Spread:        0.02,
		Active:        true,
		FeeSchedule:   &FeeSchedule{Rate: 0},
	}
}

func testEvents() []GammaEvent {
	return []GammaEvent{
		{
			ID:    "evt-btc",
			Title: "Bitcoin Price April",
			Markets: []GammaMarket{
				testMarket("m1", "Will BTC dip to $60k?", 0.45, 5000),
				testMarket("m2", "Will BTC dip to $50k?", 0.10, 3000),
				testMarket("m3", "Will BTC hit $100k?", 0.20, 8000),
			},
		},
		{
			ID:    "evt-politics",
			Title: "US Politics 2028",
			Markets: []GammaMarket{
				testMarket("m4", "Will Trump win 2028 election?", 0.60, 10000),
			},
		},
		{
			ID:    "evt-eth",
			Title: "Ethereum Flippening",
			Markets: []GammaMarket{
				testMarket("m5", "Will ETH flip BTC in 2026?", 0.15, 800),
			},
		},
		{
			ID:    "evt-resolved",
			Title: "All Resolved",
			Markets: []GammaMarket{
				testMarket("m6", "Near-resolved: 99.5% YES", 0.995, 5000),
			},
		},
	}
}

func testCryptoEvents() []GammaEvent {
	return []GammaEvent{
		{
			ID:    "evt-btc",
			Title: "Bitcoin Price April",
			Markets: []GammaMarket{
				testMarket("c1", "Will BTC exceed $100k by end of 2026?", 0.45, 5000),
				testMarket("c2", "Will BTC exceed $200k by end of 2026?", 0.10, 3000),
			},
		},
		{
			ID:    "evt-eth",
			Title: "Ethereum Flippening",
			Markets: []GammaMarket{
				testMarket("c3", "Will ETH flip BTC in 2026?", 0.15, 800),
			},
		},
	}
}

func newGammaServer(allEvents, cryptoEvents []GammaEvent) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/events":
			tagID := r.URL.Query().Get("tag_id")
			if tagID == "21" { // Crypto
				_ = json.NewEncoder(w).Encode(cryptoEvents)
			} else {
				_ = json.NewEncoder(w).Encode(allEvents)
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestToolSearchMarkets(t *testing.T) {
	gamma := newGammaServer(testEvents(), testCryptoEvents())
	defer gamma.Close()

	cases := []struct {
		name         string
		args         map[string]any
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "default returns events with markets",
			args:         map[string]any{},
			wantContains: []string{"EVENT: Bitcoin Price April", "EVENT: US Politics 2028", "BTC dip to $60k"},
			wantAbsent:   []string{"EVENT: All Resolved"},
		},
		{
			name:         "query filters events by market question",
			args:         map[string]any{"query": "Trump"},
			wantContains: []string{"EVENT: US Politics 2028", "Trump"},
			wantAbsent:   []string{"Bitcoin", "ETH"},
		},
		{
			name:         "query matches event title",
			args:         map[string]any{"query": "Bitcoin"},
			wantContains: []string{"EVENT: Bitcoin Price April", "BTC dip"},
			wantAbsent:   []string{"Trump"},
		},
		{
			name:         "category Crypto uses tag_id",
			args:         map[string]any{"category": "Crypto"},
			wantContains: []string{"EVENT: Bitcoin Price April", "EVENT: Ethereum Flippening"},
			wantAbsent:   []string{"Trump", "US Politics"},
		},
		{
			name:         "no match returns empty message",
			args:         map[string]any{"query": "xyzzy_no_match"},
			wantContains: []string{"No markets found"},
		},
		{
			name:         "limit 1 returns single event",
			args:         map[string]any{"limit": 1.0},
			wantContains: []string{"EVENT: Bitcoin Price April"},
			wantAbsent:   []string{"EVENT: US Politics"},
		},
		{
			name:         "events with only near-resolved markets are skipped",
			args:         map[string]any{},
			wantAbsent:   []string{"All Resolved", "99.5%"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(gamma.URL, "")
			result, err := svc.ToolSearchMarkets(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Content) == 0 {
				t.Fatal("got empty result")
			}
			text := result.Content[0].Text
			for _, want := range tc.wantContains {
				if !strings.Contains(text, want) {
					t.Errorf("want %q in output, got:\n%s", want, text)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(text, absent) {
					t.Errorf("want %q absent from output, got:\n%s", absent, text)
				}
			}
		})
	}
}

func TestFilterEvents(t *testing.T) {
	events := testEvents()

	cases := []struct {
		name       string
		query      string
		limit      int
		wantTitles []string
	}{
		{
			name:       "no query returns all non-resolved",
			limit:      10,
			wantTitles: []string{"Bitcoin Price April", "US Politics 2028", "Ethereum Flippening"},
		},
		{
			name:       "query matches event title",
			query:      "Bitcoin",
			limit:      10,
			wantTitles: []string{"Bitcoin Price April"},
		},
		{
			name:       "query matches market question across events",
			query:      "ETH",
			limit:      10,
			wantTitles: []string{"Ethereum Flippening"},
		},
		{
			name:       "respects limit",
			limit:      1,
			wantTitles: []string{"Bitcoin Price April"},
		},
		{
			name:       "filters out all-resolved events",
			limit:      10,
			wantTitles: []string{"Bitcoin Price April", "US Politics 2028", "Ethereum Flippening"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterEvents(events, tc.query, tc.limit)
			if len(got) != len(tc.wantTitles) {
				titles := make([]string, len(got))
				for i, e := range got {
					titles[i] = e.Title
				}
				t.Fatalf("got %d events %v, want %d %v", len(got), titles, len(tc.wantTitles), tc.wantTitles)
			}
			for i, e := range got {
				if e.Title != tc.wantTitles[i] {
					t.Errorf("got[%d].Title = %s, want %s", i, e.Title, tc.wantTitles[i])
				}
			}
		})
	}
}

func TestToolGetMarket(t *testing.T) {
	market := testMarket("m42", "Will peace deal happen in 2026?", 0.35, 8000)
	market.Description = "Resolves YES if a formal peace deal is signed by Dec 31."
	market.ResolutionSource = "Reuters, AP"
	market.Events = []GammaEventRef{{ID: "evt1", Title: "Peace Negotiations"}}

	sibling := testMarket("m43", "Will talks collapse in Q1?", 0.20, 2000)
	event := GammaEvent{
		ID:      "evt1",
		Title:   "Peace Negotiations",
		Markets: []GammaMarket{market, sibling},
	}

	book := OrderBook{
		Bids: []OrderBookLevel{{Price: "0.34", Size: "500"}, {Price: "0.33", Size: "300"}},
		Asks: []OrderBookLevel{{Price: "0.36", Size: "200"}},
	}

	gamma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/markets/m42":
			_ = json.NewEncoder(w).Encode(market)
		case "/events/evt1":
			_ = json.NewEncoder(w).Encode(event)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gamma.Close()

	clob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/book" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(book)
			return
		}
		http.NotFound(w, r)
	}))
	defer clob.Close()

	cases := []struct {
		name         string
		args         map[string]any
		wantErr      bool
		wantContains []string
	}{
		{
			name: "full market detail",
			args: map[string]any{"market_id": "m42"},
			wantContains: []string{
				"Will peace deal happen",
				"CURRENT STATE",
				"VOLUME",
				"ORDER BOOK",
				"FEES",
				"RELATED MARKETS",
				"Will talks collapse",
			},
		},
		{
			name:    "missing market_id returns error",
			args:    map[string]any{},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(gamma.URL, clob.URL)
			result, err := svc.ToolGetMarket(context.Background(), tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Content) == 0 {
				t.Fatal("got empty result")
			}
			text := result.Content[0].Text
			for _, want := range tc.wantContains {
				if !strings.Contains(text, want) {
					t.Errorf("want %q in output, got:\n%s", want, text)
				}
			}
		})
	}
}

func TestFormatOdds(t *testing.T) {
	cases := []struct {
		name          string
		outcomePrices []string
		outcomes      []string
		want          string
	}{
		{
			name:          "binary YES/NO",
			outcomePrices: []string{"0.82", "0.18"},
			outcomes:      []string{"Yes", "No"},
			want:          "Yes 82% / No 18%",
		},
		{
			name:          "empty",
			outcomePrices: nil,
			want:          "unknown",
		},
		{
			name:          "fallback labels",
			outcomePrices: []string{"0.5", "0.5"},
			want:          "Option1 50% / Option2 50%",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatOdds(tc.outcomePrices, tc.outcomes)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatVolume(t *testing.T) {
	cases := []struct {
		input float64
		want  string
	}{
		{3_500_000, "$3.5M"},
		{800_000, "$800.0K"},
		{1_200, "$1.2K"},
		{500, "$500"},
	}
	for _, tc := range cases {
		got := formatVolume(tc.input)
		if got != tc.want {
			t.Errorf("formatVolume(%.0f) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestContextCancellation(t *testing.T) {
	ready := make(chan struct{})
	readyClosed := false
	gamma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !readyClosed {
			close(ready)
			readyClosed = true
		}
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer gamma.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	svc := newTestService(gamma.URL, "")
	_, err := svc.ToolSearchMarkets(ctx, map[string]any{})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	<-ready
}

func TestFormatTimeUntil(t *testing.T) {
	cases := []struct {
		endDate string
		want    string
	}{
		{"", "unknown"},
		{"not-a-date", "not-a-date"},
		{"2020-01-01T00:00:00Z", "closed"},
	}
	for _, tc := range cases {
		got := formatTimeUntil(tc.endDate)
		if got != tc.want {
			t.Errorf("formatTimeUntil(%q) = %q, want %q", tc.endDate, got, tc.want)
		}
	}
}
