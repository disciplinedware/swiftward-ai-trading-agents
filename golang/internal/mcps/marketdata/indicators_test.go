package marketdata

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-trading-agents/internal/marketdata"
)

func TestParseIndicatorSpecs(t *testing.T) {
	tests := []struct {
		name     string
		raw      []string
		wantErr  string
		wantName string
		wantKey  string
		wantParams []int
	}{
		{"rsi default period", []string{"rsi"}, "", "rsi", "rsi_14", []int{14}},
		{"rsi_14", []string{"rsi_14"}, "", "rsi", "rsi_14", []int{14}},
		{"rsi_21", []string{"rsi_21"}, "", "rsi", "rsi_21", []int{21}},
		{"ema_21", []string{"ema_21"}, "", "ema", "ema_21", []int{21}},
		{"sma_50", []string{"sma_50"}, "", "sma", "sma_50", []int{50}},
		{"macd default", []string{"macd"}, "", "macd", "macd", []int{12, 26, 9}},
		{"macd_3_6_2", []string{"macd_3_6_2"}, "", "macd", "macd", []int{3, 6, 2}},
		{"bbands default", []string{"bbands"}, "", "bbands", "bbands", []int{20, 2}},
		{"bbands_10_3", []string{"bbands_10_3"}, "", "bbands", "bbands", []int{10, 3}},
		{"atr default", []string{"atr"}, "", "atr", "atr_14", []int{14}},
		{"atr_7", []string{"atr_7"}, "", "atr", "atr_7", []int{7}},
		{"vwap", []string{"vwap"}, "", "vwap", "vwap", nil},
		{"unknown", []string{"unknown"}, "unknown indicator", "", "", nil},
		{"ema no period", []string{"ema"}, "exactly one param", "", "", nil},
		{"sma no period", []string{"sma"}, "exactly one param", "", "", nil},
		{"macd bad params", []string{"macd_12_26"}, "0 or 3 params", "", "", nil},
		{"bbands bad params", []string{"bbands_20"}, "0 or 2 params", "", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs, err := ParseIndicatorSpecs(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(specs) != 1 {
				t.Fatalf("expected 1 spec, got %d", len(specs))
			}
			spec := specs[0]
			if spec.Name != tt.wantName {
				t.Errorf("Name: want %q, got %q", tt.wantName, spec.Name)
			}
			if spec.Key != tt.wantKey {
				t.Errorf("Key: want %q, got %q", tt.wantKey, spec.Key)
			}
			if len(spec.Params) != len(tt.wantParams) {
				t.Errorf("Params len: want %v, got %v", tt.wantParams, spec.Params)
			} else {
				for i, p := range tt.wantParams {
					if spec.Params[i] != p {
						t.Errorf("Params[%d]: want %d, got %d", i, p, spec.Params[i])
					}
				}
			}
		})
	}
}

func TestAllOutputColumns(t *testing.T) {
	specs, _ := ParseIndicatorSpecs([]string{"rsi_14", "macd", "bbands", "ema_21"})
	cols := AllOutputColumns(specs)
	want := []string{"rsi_14", "macd", "macd_signal", "macd_hist", "bb_upper", "bb_middle", "bb_lower", "ema_21"}
	if len(cols) != len(want) {
		t.Fatalf("want cols %v, got %v", want, cols)
	}
	for i, c := range want {
		if cols[i] != c {
			t.Errorf("col[%d]: want %q, got %q", i, c, cols[i])
		}
	}
}

func TestWarmupNeeded(t *testing.T) {
	tests := []struct {
		name  string
		specs []string
		want  int
	}{
		{"rsi_14", []string{"rsi_14"}, 14},
		{"ema_21", []string{"ema_21"}, 20},
		{"sma_50", []string{"sma_50"}, 49},
		{"macd", []string{"macd"}, 35},    // 26 + 9
		{"bbands", []string{"bbands"}, 19}, // 20 - 1
		{"atr_14", []string{"atr_14"}, 14},
		{"vwap", []string{"vwap"}, 0},
		// max across multiple specs
		{"rsi+macd", []string{"rsi_14", "macd"}, 35},
		{"vwap+ema", []string{"vwap", "ema_5"}, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs, err := ParseIndicatorSpecs(tt.specs)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := WarmupNeeded(specs)
			if got != tt.want {
				t.Errorf("WarmupNeeded: want %d, got %d", tt.want, got)
			}
		})
	}
}

// --- Individual indicator tests with known reference values ---

func TestCalcSMA(t *testing.T) {
	// SMA-3 of [10, 20, 30, 40, 50]
	// index 2: (10+20+30)/3 = 20
	// index 3: (20+30+40)/3 = 30
	// index 4: (30+40+50)/3 = 40
	closes := []float64{10, 20, 30, 40, 50}
	result := calcSMA(closes, 3)

	cases := []struct {
		idx  int
		want string
	}{
		{0, ""},
		{1, ""},
		{2, "20.0000"},
		{3, "30.0000"},
		{4, "40.0000"},
	}
	for _, c := range cases {
		if result[c.idx] != c.want {
			t.Errorf("SMA[%d]: want %q, got %q", c.idx, c.want, result[c.idx])
		}
	}
}

func TestCalcEMA(t *testing.T) {
	// EMA-3 of [10, 10, 10, 14]
	// seed = (10+10+10)/3 = 10, mult = 2/(3+1) = 0.5
	// ema[3] = 14*0.5 + 10*0.5 = 12
	closes := []float64{10, 10, 10, 14}
	result := calcEMA(closes, 3)

	if result[0] != "" || result[1] != "" {
		t.Errorf("expected null for warm-up, got %v, %v", result[0], result[1])
	}
	assertApprox(t, "EMA[2]", result[2], 10.0)
	assertApprox(t, "EMA[3]", result[3], 12.0)
}

func TestCalcRSI(t *testing.T) {
	// RSI-3 of [10, 11, 12, 11]
	// changes: +1, +1, -1
	// initial avg_gain = (1+1+0)/3 = 2/3
	// initial avg_loss = (0+0+1)/3 = 1/3
	// RS = 2.0, RSI = 100 - 100/3 = 66.67
	closes := []float64{10, 11, 12, 11}
	result := calcRSI(closes, 3)

	if result[0] != "" || result[1] != "" || result[2] != "" {
		t.Errorf("expected null for warm-up indices 0-2, got %v, %v, %v", result[0], result[1], result[2])
	}
	assertApprox(t, "RSI[3]", result[3], 66.67)
}

func TestCalcRSI_AllGains(t *testing.T) {
	// All upward moves: RSI should approach 100
	closes := make([]float64, 20)
	for i := range closes {
		closes[i] = float64(i + 1)
	}
	result := calcRSI(closes, 14)
	// RSI[14] should be 100 (no losses in first 14 changes)
	if result[14] != "100.00" {
		t.Errorf("expected 100.00 for all-gain RSI, got %q", result[14])
	}
}

func TestCalcRSI_AllLosses(t *testing.T) {
	// All downward moves: RSI should be 0
	closes := make([]float64, 20)
	for i := range closes {
		closes[i] = float64(20 - i)
	}
	result := calcRSI(closes, 14)
	// RSI[14] should be 0 (no gains in first 14 changes)
	if result[14] != "0.00" {
		t.Errorf("expected 0.00 for all-loss RSI, got %q", result[14])
	}
}

func TestCalcMACD(t *testing.T) {
	// MACD(3, 5, 2) on [10, 11, 12, 13, 14, 15, 16, 17, 18, 19]
	// MACD line valid from index slow-1 = 4
	// Signal valid from index slow-1 + signal-1 = 5
	closes := []float64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	macd, signal, hist := calcMACD(closes, 3, 5, 2)

	// Before slow-1, all should be null
	for i := range 4 {
		if macd[i] != "" || signal[i] != "" || hist[i] != "" {
			t.Errorf("expected null at index %d, got macd=%q sig=%q hist=%q", i, macd[i], signal[i], hist[i])
		}
	}
	// MACD line valid at index 4, signal/hist still null at index 4
	if macd[4] == "" {
		t.Error("MACD line should be valid at index 4")
	}
	if signal[4] != "" || hist[4] != "" {
		t.Errorf("signal/hist should be null at index 4, got sig=%q hist=%q", signal[4], hist[4])
	}
	// Signal valid from index 5
	for i := 5; i < len(closes); i++ {
		if macd[i] == "" || signal[i] == "" || hist[i] == "" {
			t.Errorf("expected non-null at index %d, got macd=%q sig=%q hist=%q", i, macd[i], signal[i], hist[i])
		}
		// hist = macd - signal (verify consistency)
		m, _ := strconv.ParseFloat(macd[i], 64)
		s, _ := strconv.ParseFloat(signal[i], 64)
		h, _ := strconv.ParseFloat(hist[i], 64)
		if math.Abs((m-s)-h) > 0.001 {
			t.Errorf("hist[%d] = %f != macd(%f) - signal(%f)", i, h, m, s)
		}
	}
}

func TestCalcBBands(t *testing.T) {
	// BBands(3, 2) on [10, 10, 10]: stddev=0, upper=middle=lower=10
	closes := []float64{10, 10, 10}
	upper, middle, lower := calcBBands(closes, 3, 2)

	// indices 0,1 null
	if upper[0] != "" || upper[1] != "" {
		t.Error("expected null for first period-1 indices")
	}
	// index 2: all equal (zero stddev)
	assertApprox(t, "bb_upper[2]", upper[2], 10.0)
	assertApprox(t, "bb_middle[2]", middle[2], 10.0)
	assertApprox(t, "bb_lower[2]", lower[2], 10.0)

	// BBands with spread: upper > middle > lower
	closes2 := []float64{8, 10, 12, 14, 16}
	upper2, middle2, lower2 := calcBBands(closes2, 3, 2)
	for i := 2; i < 5; i++ {
		if upper2[i] == "" {
			continue
		}
		u, _ := strconv.ParseFloat(upper2[i], 64)
		m, _ := strconv.ParseFloat(middle2[i], 64)
		lo, _ := strconv.ParseFloat(lower2[i], 64)
		if u <= m || m <= lo {
			t.Errorf("at index %d: expected upper(%f) > middle(%f) > lower(%f)", i, u, m, lo)
		}
	}
}

func TestCalcATR(t *testing.T) {
	// ATR-2 on 4 candles
	// Candle[0]: H=10, L=8, C=9
	// Candle[1]: H=12, L=9, C=11  -> TR = max(3, 3, 0) = 3
	// Candle[2]: H=11, L=8, C=10  -> TR = max(3, 0, 3) = 3
	// Candle[3]: H=14, L=11, C=12 -> TR = max(3, 4, 1) = 4
	// Initial ATR = (3+3)/2 = 3.0 at index 2
	// ATR[3] = (3*1 + 4) / 2 = 3.5
	highs := []float64{10, 12, 11, 14}
	lows := []float64{8, 9, 8, 11}
	closes := []float64{9, 11, 10, 12}

	result := calcATR(highs, lows, closes, 2)

	if result[0] != "" || result[1] != "" {
		t.Errorf("expected null for first 2 indices, got %v, %v", result[0], result[1])
	}
	assertApprox(t, "ATR[2]", result[2], 3.0)
	assertApprox(t, "ATR[3]", result[3], 3.5)
}

func TestCalcVWAP(t *testing.T) {
	// Candle[0]: H=10, L=8,  C=9,  V=100  -> typical=9.0,  cumPV=900,   cumVol=100,  vwap=9.0
	// Candle[1]: H=11, L=9,  C=10, V=200  -> typical=10.0, cumPV=2900,  cumVol=300,  vwap=9.6667
	// Candle[2]: H=12, L=10, C=11, V=300  -> typical=11.0, cumPV=6200,  cumVol=600,  vwap=10.3333
	highs := []float64{10, 11, 12}
	lows := []float64{8, 9, 10}
	closes := []float64{9, 10, 11}
	volumes := []float64{100, 200, 300}

	result := calcVWAP(highs, lows, closes, volumes)

	assertApprox(t, "VWAP[0]", result[0], 9.0)
	assertApprox(t, "VWAP[1]", result[1], 9.6667)
	assertApprox(t, "VWAP[2]", result[2], 10.3333)
}

// --- IndicatorEngine.Compute tests ---

func TestIndicatorEngineCompute_SMA(t *testing.T) {
	now := time.Now().UTC()
	candles := []marketdata.Candle{
		{Timestamp: now.Add(-4 * time.Hour), Open: "10.00", High: "10.50", Low: "9.50", Close: "10.00", Volume: "1000"},
		{Timestamp: now.Add(-3 * time.Hour), Open: "20.00", High: "20.50", Low: "19.50", Close: "20.00", Volume: "2000"},
		{Timestamp: now.Add(-2 * time.Hour), Open: "30.00", High: "30.50", Low: "29.50", Close: "30.00", Volume: "3000"},
		{Timestamp: now.Add(-1 * time.Hour), Open: "40.00", High: "40.50", Low: "39.50", Close: "40.00", Volume: "4000"},
		{Timestamp: now, Open: "50.00", High: "50.50", Low: "49.50", Close: "50.00", Volume: "5000"},
	}

	specs, _ := ParseIndicatorSpecs([]string{"sma_3"})
	engine := &IndicatorEngine{}
	result, err := engine.Compute(candles, specs)
	if err != nil {
		t.Fatalf("compute error: %v", err)
	}
	if len(result) != 5 {
		t.Fatalf("expected 5 candles, got %d", len(result))
	}

	// Verify null for warm-up and correct values after
	if result[0].Indicators["sma_3"] != "" || result[1].Indicators["sma_3"] != "" {
		t.Error("expected null for indices 0-1")
	}
	assertApprox(t, "sma_3[2]", result[2].Indicators["sma_3"], 20.0)
	assertApprox(t, "sma_3[3]", result[3].Indicators["sma_3"], 30.0)
	assertApprox(t, "sma_3[4]", result[4].Indicators["sma_3"], 40.0)
}

func TestIndicatorEngineCompute_VWAP(t *testing.T) {
	now := time.Now().UTC()
	candles := []marketdata.Candle{
		{Timestamp: now.Add(-2 * time.Hour), Open: "9.00", High: "10.00", Low: "8.00", Close: "9.00", Volume: "100"},
		{Timestamp: now.Add(-1 * time.Hour), Open: "10.00", High: "11.00", Low: "9.00", Close: "10.00", Volume: "200"},
		{Timestamp: now, Open: "11.00", High: "12.00", Low: "10.00", Close: "11.00", Volume: "300"},
	}

	specs, _ := ParseIndicatorSpecs([]string{"vwap"})
	engine := &IndicatorEngine{}
	result, err := engine.Compute(candles, specs)
	if err != nil {
		t.Fatalf("compute error: %v", err)
	}

	// VWAP starts from first candle
	if result[0].Indicators["vwap"] == "" {
		t.Error("VWAP should be valid from first candle")
	}
	assertApprox(t, "vwap[0]", result[0].Indicators["vwap"], 9.0)
	assertApprox(t, "vwap[1]", result[1].Indicators["vwap"], 9.6667)
	assertApprox(t, "vwap[2]", result[2].Indicators["vwap"], 10.3333)
}

func TestIndicatorEngineCompute_MultipleSpecs(t *testing.T) {
	// Verify multiple indicators are computed and stored separately
	now := time.Now().UTC()
	candles := make([]marketdata.Candle, 20)
	for i := range candles {
		p := strconv.Itoa(100 + i)
		candles[i] = marketdata.Candle{
			Timestamp: now.Add(time.Duration(-20+i) * time.Hour),
			Open:      p + ".00",
			High:      p + ".50",
			Low:       p + ".00",
			Close:     p + ".00",
			Volume:    "1000",
		}
	}

	specs, _ := ParseIndicatorSpecs([]string{"rsi_14", "vwap"})
	engine := &IndicatorEngine{}
	result, err := engine.Compute(candles, specs)
	if err != nil {
		t.Fatalf("compute error: %v", err)
	}

	// VWAP always present; RSI null for first 14
	for i, r := range result {
		if r.Indicators["vwap"] == "" {
			t.Errorf("VWAP should be present at index %d", i)
		}
		if i < 14 && r.Indicators["rsi_14"] != "" {
			t.Errorf("RSI should be null at index %d (warm-up)", i)
		}
		if i >= 14 && r.Indicators["rsi_14"] == "" {
			t.Errorf("RSI should be present at index %d", i)
		}
	}
}

func TestIndicatorEngineCompute_MarshalJSON(t *testing.T) {
	// Verify flat JSON structure with both candle and indicator fields
	now := time.Now().UTC().Truncate(time.Hour)
	candles := []marketdata.Candle{
		{Timestamp: now.Add(-2 * time.Hour), Open: "10.00", High: "11.00", Low: "9.00", Close: "10.00", Volume: "500"},
		{Timestamp: now.Add(-1 * time.Hour), Open: "10.00", High: "11.00", Low: "9.00", Close: "10.00", Volume: "500"},
		{Timestamp: now, Open: "10.00", High: "11.00", Low: "9.00", Close: "10.00", Volume: "500"},
	}
	specs, _ := ParseIndicatorSpecs([]string{"sma_3", "vwap"})
	engine := &IndicatorEngine{}
	result, _ := engine.Compute(candles, specs)

	// Marshal last candle (has valid sma_3 and vwap)
	data, err := result[2].MarshalJSON()
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	s := string(data)

	// Must contain candle short keys
	for _, key := range []string{"\"t\"", "\"o\"", "\"h\"", "\"l\"", "\"c\"", "\"v\""} {
		if !strings.Contains(s, key) {
			t.Errorf("JSON missing key %s: %s", key, s)
		}
	}
	// Must contain indicator keys
	if !strings.Contains(s, "\"sma_3\"") {
		t.Errorf("JSON missing sma_3: %s", s)
	}
	if !strings.Contains(s, "\"vwap\"") {
		t.Errorf("JSON missing vwap: %s", s)
	}
}

// --- Service handler tests for indicators ---

func TestToolGetCandles_WithIndicators(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Hour)
	// Build 20 candles so RSI-14 can warm up
	sampleCandles := make([]marketdata.Candle, 20)
	for i := range sampleCandles {
		p := strconv.FormatFloat(float64(100+i), 'f', 2, 64)
		sampleCandles[i] = marketdata.Candle{
			Timestamp: now.Add(time.Duration(-20+i) * time.Hour),
			Open:      p, High: p, Low: p, Close: p, Volume: "1000",
		}
	}

	tests := []struct {
		name       string
		args       map[string]any
		wantErr    string
		checkResult func(t *testing.T, result map[string]any)
	}{
		{
			name: "invalid indicator",
			args: map[string]any{
				"market":     "ETH-USDC",
				"interval":   "1h",
				"limit":      float64(100),
				"indicators": []any{"unknown_indicator"},
			},
			wantErr: "unknown indicator",
		},
		{
			name: "json with vwap",
			args: map[string]any{
				"market":     "ETH-USDC",
				"interval":   "1h",
				"limit":      float64(100),
				"indicators": []any{"vwap"},
			},
			checkResult: func(t *testing.T, result map[string]any) {
				inds, ok := result["indicators_computed"].([]any)
				if !ok || len(inds) != 1 || inds[0] != "vwap" {
					t.Errorf("expected indicators_computed=[vwap], got %v", result["indicators_computed"])
				}
				candles, ok := result["candles"].([]any)
				if !ok || len(candles) == 0 {
					t.Fatal("expected non-empty candles")
				}
				// Each candle should have vwap key
				c := candles[0].(map[string]any)
				if _, hasVwap := c["vwap"]; !hasVwap {
					t.Errorf("candle missing vwap key: %v", c)
				}
			},
		},
		{
			name: "csv with vwap",
			args: map[string]any{
				"market":     "ETH-USDC",
				"interval":   "1h",
				"limit":      float64(100),
				"format":     "csv",
				"indicators": []any{"vwap"},
			},
			checkResult: func(t *testing.T, result map[string]any) {
				data, ok := result["data"].(string)
				if !ok || data == "" {
					t.Fatal("expected non-empty csv data")
				}
				if !strings.Contains(data, "vwap") {
					t.Errorf("CSV header missing vwap: %s", strings.SplitN(data, "\n", 2)[0])
				}
			},
		},
		{
			name: "indicators_computed in response",
			args: map[string]any{
				"market":     "ETH-USDC",
				"interval":   "1h",
				"limit":      float64(100),
				"indicators": []any{"sma_3", "rsi_14"},
			},
			checkResult: func(t *testing.T, result map[string]any) {
				inds, ok := result["indicators_computed"].([]any)
				if !ok {
					t.Fatalf("indicators_computed missing or wrong type: %v", result["indicators_computed"])
				}
				wantCols := map[string]bool{"sma_3": true, "rsi_14": true}
				for _, ind := range inds {
					delete(wantCols, ind.(string))
				}
				if len(wantCols) != 0 {
					t.Errorf("missing indicators_computed entries: %v", wantCols)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(&mockSource{candles: sampleCandles})
			result, err := svc.toolGetCandles(t.Context(), tt.args)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			parsed := parseResult(t, result)
			if tt.checkResult != nil {
				tt.checkResult(t, parsed)
			}
		})
	}
}

func TestFormatCandlesWithIndicatorsCSV(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	candles := []CandleWithIndicators{
		{
			Candle: marketdata.Candle{
				Timestamp: now, Open: "100.00", High: "101.00", Low: "99.00", Close: "100.50", Volume: "1000",
			},
			Indicators: map[string]string{"vwap": "100.1667", "rsi_14": ""},
		},
	}

	csv := FormatCandlesWithIndicatorsCSV(candles, []string{"vwap", "rsi_14"})
	lines := strings.Split(strings.TrimSpace(csv), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 row), got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "timestamp,open,high,low,close,volume,vwap,rsi_14") {
		t.Errorf("unexpected header: %s", lines[0])
	}
	parts := strings.Split(lines[1], ",")
	if len(parts) != 8 { // 6 candle cols + 2 indicator cols
		t.Fatalf("expected 8 columns, got %d: %v", len(parts), parts)
	}
	if parts[6] != "100.1667" {
		t.Errorf("expected vwap=100.1667, got %q", parts[6])
	}
	if parts[7] != "" {
		t.Errorf("expected null (empty) rsi_14, got %q", parts[7])
	}
}

// --- Helpers ---

// assertApprox parses a decimal string and checks it's within 0.01 of want.
func assertApprox(t *testing.T, label, gotStr string, want float64) {
	t.Helper()
	if gotStr == "" {
		t.Errorf("%s: expected ~%f, got null (empty string)", label, want)
		return
	}
	got, err := strconv.ParseFloat(gotStr, 64)
	if err != nil {
		t.Errorf("%s: parse error: %v (value: %q)", label, err, gotStr)
		return
	}
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: want ~%f, got %f", label, want, got)
	}
}
