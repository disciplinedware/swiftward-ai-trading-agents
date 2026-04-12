package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata"
)

const defaultBaseURL = "https://api.bybit.com"

// Config holds Bybit source configuration.
type Config struct {
	BaseURL string        // defaults to https://api.bybit.com
	Timeout time.Duration // defaults to 10s
}

// Source implements marketdata.DataSource for OI and funding data via the Bybit public API.
// No API key required. Spot data is not supported - ChainSource falls through to Binance.
type Source struct {
	baseURL    string
	httpClient *http.Client
	log        *zap.Logger
}

func NewSource(cfg Config, log *zap.Logger) *Source {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Source{
		baseURL:    cfg.BaseURL,
		httpClient: &http.Client{Timeout: timeout},
		log:        log,
	}
}

func (s *Source) Name() string { return "bybit" }

func (s *Source) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bybit API %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	return body, nil
}

// futuresSymbol converts a canonical pair to Bybit linear futures format.
// USD is substituted with USDT (Bybit perps are USDT-margined); other quotes pass through.
func futuresSymbol(symbol string) (string, error) {
	parts := strings.SplitN(symbol, "-", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", fmt.Errorf("cannot extract base coin from symbol %q", symbol)
	}
	quote := parts[1]
	if quote == "USD" {
		quote = "USDT"
	}
	return parts[0] + quote, nil
}

// --- GetOpenInterest ---

type oiResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []struct {
			OpenInterest string `json:"openInterest"`
			Timestamp    string `json:"timestamp"`
		} `json:"list"`
	} `json:"result"`
}

func (s *Source) GetOpenInterest(ctx context.Context, symbol string) (*marketdata.OpenInterest, error) {
	fSym, err := futuresSymbol(symbol)
	if err != nil {
		return nil, err
	}

	// Fetch 30 1h periods to compute 1h/4h/24h change windows.
	path := fmt.Sprintf("/v5/market/open-interest?category=linear&symbol=%s&intervalTime=1h&limit=30", fSym)
	body, err := s.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("bybit OI %s: %w", symbol, err)
	}

	var resp oiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bybit parse OI %s: %w", symbol, err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit OI %s: retCode=%d msg=%s", symbol, resp.RetCode, resp.RetMsg)
	}
	if len(resp.Result.List) == 0 {
		return nil, fmt.Errorf("bybit OI %s: empty response", symbol)
	}

	// Bybit returns newest-first; reverse so we can compute changes oldest→newest.
	list := resp.Result.List
	latest := list[0]
	currentOI, _ := strconv.ParseFloat(latest.OpenInterest, 64)
	latestTs, _ := strconv.ParseInt(latest.Timestamp, 10, 64)
	now := time.UnixMilli(latestTs).UTC()

	change1h, change4h, change24h := computeOIChanges(currentOI, now, list[1:])

	return &marketdata.OpenInterest{
		Market:         symbol,
		OpenInterest:   fmt.Sprintf("%.2f", currentOI),
		OIChange1hPct:  fmt.Sprintf("%.2f", change1h),
		OIChange4hPct:  fmt.Sprintf("%.2f", change4h),
		OIChange24hPct: fmt.Sprintf("%.2f", change24h),
		LongShortRatio: "n/a",
	}, nil
}

// computeOIChanges finds the closest historical snapshot to each lookback window
// and returns the % change to currentOI. hist is newest-first.
func computeOIChanges(currentOI float64, now time.Time, hist []struct {
	OpenInterest string `json:"openInterest"`
	Timestamp    string `json:"timestamp"`
}) (change1h, change4h, change24h float64) {
	found1h, found4h, found24h := false, false, false

	for _, h := range hist {
		ts, _ := strconv.ParseInt(h.Timestamp, 10, 64)
		t := time.UnixMilli(ts).UTC()
		age := now.Sub(t)
		val, _ := strconv.ParseFloat(h.OpenInterest, 64)
		if val <= 0 {
			continue
		}
		if !found1h && age >= time.Hour {
			change1h = (currentOI - val) / val * 100
			found1h = true
		}
		if !found4h && age >= 4*time.Hour {
			change4h = (currentOI - val) / val * 100
			found4h = true
		}
		if !found24h && age >= 24*time.Hour {
			change24h = (currentOI - val) / val * 100
			found24h = true
		}
		if found1h && found4h && found24h {
			break
		}
	}
	return
}

// --- GetFundingRates ---

type fundingResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []struct {
			Symbol               string `json:"symbol"`
			FundingRate          string `json:"fundingRate"`
			FundingRateTimestamp string `json:"fundingRateTimestamp"`
		} `json:"list"`
	} `json:"result"`
}

func (s *Source) GetFundingRates(ctx context.Context, symbol string, limit int) (*marketdata.FundingData, error) {
	if limit <= 0 {
		limit = 10
	}
	// Fetch at least 2 entries so InferFundingIntervalH can compute the actual interval.
	fetchLimit := limit
	if fetchLimit < 2 {
		fetchLimit = 2
	}

	fSym, err := futuresSymbol(symbol)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/v5/market/funding/history?category=linear&symbol=%s&limit=%d", fSym, fetchLimit)
	body, err := s.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("bybit funding %s: %w", symbol, err)
	}

	var resp fundingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bybit parse funding %s: %w", symbol, err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit funding %s: retCode=%d msg=%s", symbol, resp.RetCode, resp.RetMsg)
	}
	if len(resp.Result.List) == 0 {
		return nil, fmt.Errorf("bybit funding %s: empty response", symbol)
	}

	// Bybit returns newest-first.
	latest := resp.Result.List[0]
	rateF, _ := strconv.ParseFloat(latest.FundingRate, 64)

	history := make([]marketdata.FundingRate, len(resp.Result.List))
	for i, r := range resp.Result.List {
		ts, _ := strconv.ParseInt(r.FundingRateTimestamp, 10, 64)
		history[i] = marketdata.FundingRate{
			Timestamp: time.UnixMilli(ts).UTC(),
			Rate:      r.FundingRate,
		}
	}

	intervalH := marketdata.InferFundingIntervalH(history)
	periodsPerDay := 24.0 / float64(intervalH)
	annualized := rateF * periodsPerDay * 365 * 100

	latestTs, _ := strconv.ParseInt(latest.FundingRateTimestamp, 10, 64)
	nextFunding := time.UnixMilli(latestTs).UTC().Add(time.Duration(intervalH) * time.Hour)

	return &marketdata.FundingData{
		Market:           symbol,
		CurrentRate:      latest.FundingRate,
		AnnualizedPct:    fmt.Sprintf("%.2f", annualized),
		FundingIntervalH: intervalH,
		NextFundingTime:  nextFunding,
		History:          history,
	}, nil
}

// --- Unsupported spot methods ---

func (s *Source) GetTicker(_ context.Context, _ []string) ([]marketdata.Ticker, error) {
	return nil, fmt.Errorf("bybit: spot ticker not supported, use binance")
}

func (s *Source) GetCandles(_ context.Context, _ string, _ marketdata.Interval, _ int, _ time.Time) ([]marketdata.Candle, error) {
	return nil, fmt.Errorf("bybit: candles not supported, use binance")
}

func (s *Source) GetOrderbook(_ context.Context, _ string, _ int) (*marketdata.Orderbook, error) {
	return nil, fmt.Errorf("bybit: orderbook not supported, use binance")
}

func (s *Source) GetMarkets(_ context.Context, _ string) ([]marketdata.MarketInfo, error) {
	return nil, fmt.Errorf("bybit: markets not supported, use binance")
}
