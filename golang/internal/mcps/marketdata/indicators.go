package marketdata

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"ai-trading-agents/internal/marketdata"
)

// IndicatorSpec describes a single indicator to compute.
type IndicatorSpec struct {
	Name   string // "rsi", "ema", "sma", "macd", "bbands", "atr", "vwap"
	Params []int  // period(s), stddev multiplier, etc.
	Key    string // base key for output column(s)
}

// outputColumns returns the JSON/CSV column names produced by this spec.
func (s IndicatorSpec) outputColumns() []string {
	switch s.Name {
	case "macd":
		return []string{"macd", "macd_signal", "macd_hist"}
	case "bbands":
		return []string{"bb_upper", "bb_middle", "bb_lower"}
	default:
		return []string{s.Key}
	}
}

// CandleWithIndicators extends a Candle with computed indicator values.
type CandleWithIndicators struct {
	marketdata.Candle
	Indicators map[string]string // indicator column -> decimal string; empty string = null
}

// MarshalJSON produces a flat JSON object with candle fields + indicator fields.
func (c CandleWithIndicators) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"t": c.Timestamp,
		"o": c.Open,
		"h": c.High,
		"l": c.Low,
		"c": c.Close,
		"v": c.Volume,
	}
	for k, v := range c.Indicators {
		if v == "" {
			m[k] = nil
		} else {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// AllOutputColumns returns all output column names across all specs in order (deduped).
func AllOutputColumns(specs []IndicatorSpec) []string {
	var cols []string
	seen := map[string]bool{}
	for _, spec := range specs {
		for _, col := range spec.outputColumns() {
			if !seen[col] {
				cols = append(cols, col)
				seen[col] = true
			}
		}
	}
	return cols
}

// ParseIndicatorSpecs parses ["rsi_14", "ema_21", "macd"] into IndicatorSpec structs.
func ParseIndicatorSpecs(raw []string) ([]IndicatorSpec, error) {
	specs := make([]IndicatorSpec, 0, len(raw))
	for _, r := range raw {
		spec, err := parseIndicatorSpec(r)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func parseIndicatorSpec(raw string) (IndicatorSpec, error) {
	parts := strings.Split(strings.ToLower(raw), "_")
	name := parts[0]
	params := parts[1:]

	switch name {
	case "rsi":
		period := 14
		if len(params) == 1 {
			p, err := strconv.Atoi(params[0])
			if err != nil || p <= 0 {
				return IndicatorSpec{}, fmt.Errorf("invalid rsi period in %q", raw)
			}
			period = p
		} else if len(params) > 1 {
			return IndicatorSpec{}, fmt.Errorf("rsi takes at most one param (period), got %q", raw)
		}
		return IndicatorSpec{Name: "rsi", Params: []int{period}, Key: fmt.Sprintf("rsi_%d", period)}, nil

	case "ema":
		if len(params) != 1 {
			return IndicatorSpec{}, fmt.Errorf("ema requires exactly one param (period), e.g. ema_21")
		}
		p, err := strconv.Atoi(params[0])
		if err != nil || p <= 0 {
			return IndicatorSpec{}, fmt.Errorf("invalid ema period in %q", raw)
		}
		return IndicatorSpec{Name: "ema", Params: []int{p}, Key: fmt.Sprintf("ema_%d", p)}, nil

	case "sma":
		if len(params) != 1 {
			return IndicatorSpec{}, fmt.Errorf("sma requires exactly one param (period), e.g. sma_50")
		}
		p, err := strconv.Atoi(params[0])
		if err != nil || p <= 0 {
			return IndicatorSpec{}, fmt.Errorf("invalid sma period in %q", raw)
		}
		return IndicatorSpec{Name: "sma", Params: []int{p}, Key: fmt.Sprintf("sma_%d", p)}, nil

	case "macd":
		fast, slow, signal := 12, 26, 9
		if len(params) == 3 {
			f, err1 := strconv.Atoi(params[0])
			s, err2 := strconv.Atoi(params[1])
			sig, err3 := strconv.Atoi(params[2])
			if err1 != nil || err2 != nil || err3 != nil || f <= 0 || s <= 0 || sig <= 0 {
				return IndicatorSpec{}, fmt.Errorf("invalid macd params in %q (expected fast_slow_signal)", raw)
			}
			fast, slow, signal = f, s, sig
		} else if len(params) != 0 {
			return IndicatorSpec{}, fmt.Errorf("macd takes 0 or 3 params (fast_slow_signal), got %q", raw)
		}
		return IndicatorSpec{Name: "macd", Params: []int{fast, slow, signal}, Key: "macd"}, nil

	case "bbands":
		period, mult := 20, 2
		if len(params) == 2 {
			p, err1 := strconv.Atoi(params[0])
			m, err2 := strconv.Atoi(params[1])
			if err1 != nil || err2 != nil || p <= 0 || m <= 0 {
				return IndicatorSpec{}, fmt.Errorf("invalid bbands params in %q (expected period_stddev)", raw)
			}
			period, mult = p, m
		} else if len(params) != 0 {
			return IndicatorSpec{}, fmt.Errorf("bbands takes 0 or 2 params (period_stddev), got %q", raw)
		}
		return IndicatorSpec{Name: "bbands", Params: []int{period, mult}, Key: "bbands"}, nil

	case "atr":
		period := 14
		if len(params) == 1 {
			p, err := strconv.Atoi(params[0])
			if err != nil || p <= 0 {
				return IndicatorSpec{}, fmt.Errorf("invalid atr period in %q", raw)
			}
			period = p
		} else if len(params) > 1 {
			return IndicatorSpec{}, fmt.Errorf("atr takes at most one param (period), got %q", raw)
		}
		return IndicatorSpec{Name: "atr", Params: []int{period}, Key: fmt.Sprintf("atr_%d", period)}, nil

	case "vwap":
		if len(params) > 0 {
			return IndicatorSpec{}, fmt.Errorf("vwap takes no params, got %q", raw)
		}
		return IndicatorSpec{Name: "vwap", Params: nil, Key: "vwap"}, nil

	default:
		return IndicatorSpec{}, fmt.Errorf("unknown indicator %q (supported: rsi, ema, sma, macd, bbands, atr, vwap)", raw)
	}
}

// WarmupNeeded returns the number of extra candles to fetch before the requested limit
// so that all returned candles have valid indicator values.
func WarmupNeeded(specs []IndicatorSpec) int {
	maxWarm := 0
	for _, spec := range specs {
		if w := specWarmup(spec); w > maxWarm {
			maxWarm = w
		}
	}
	return maxWarm
}

func specWarmup(spec IndicatorSpec) int {
	switch spec.Name {
	case "rsi":
		return spec.Params[0] // first valid RSI at index period
	case "ema":
		return spec.Params[0] - 1 // first valid EMA at index period-1
	case "sma":
		return spec.Params[0] - 1 // first valid SMA at index period-1
	case "macd":
		// first valid signal at index slow-1 + signal-1 = slow+signal-2; we over-fetch by 2, so warmup = slow+signal
		return spec.Params[1] + spec.Params[2]
	case "bbands":
		return spec.Params[0] - 1 // first valid BBands at index period-1
	case "atr":
		return spec.Params[0] // first valid ATR at index period
	case "vwap":
		return 0 // always valid from first candle
	default:
		return 0
	}
}

// IndicatorEngine computes technical indicators on OHLCV candle data.
type IndicatorEngine struct{}

// Compute takes candles and indicator specs, returns candles with indicator values appended.
// Each candle gets a Indicators map with column -> decimal string (empty = null).
func (e *IndicatorEngine) Compute(candles []marketdata.Candle, specs []IndicatorSpec) ([]CandleWithIndicators, error) {
	n := len(candles)
	allCols := AllOutputColumns(specs)

	if len(specs) == 0 || n == 0 {
		result := make([]CandleWithIndicators, n)
		for i, c := range candles {
			result[i] = CandleWithIndicators{Candle: c, Indicators: make(map[string]string, len(allCols))}
		}
		return result, nil
	}

	highs, lows, closes, volumes, err := parseOHLCV(candles)
	if err != nil {
		return nil, err
	}

	// indicatorData maps column name -> []string (one per candle, "" = null)
	indicatorData := make(map[string][]string, len(allCols))

	for _, spec := range specs {
		switch spec.Name {
		case "rsi":
			indicatorData[spec.Key] = calcRSI(closes, spec.Params[0])
		case "ema":
			indicatorData[spec.Key] = calcEMA(closes, spec.Params[0])
		case "sma":
			indicatorData[spec.Key] = calcSMA(closes, spec.Params[0])
		case "macd":
			m, sig, hist := calcMACD(closes, spec.Params[0], spec.Params[1], spec.Params[2])
			indicatorData["macd"] = m
			indicatorData["macd_signal"] = sig
			indicatorData["macd_hist"] = hist
		case "bbands":
			u, mid, lo := calcBBands(closes, spec.Params[0], float64(spec.Params[1]))
			indicatorData["bb_upper"] = u
			indicatorData["bb_middle"] = mid
			indicatorData["bb_lower"] = lo
		case "atr":
			indicatorData[spec.Key] = calcATR(highs, lows, closes, spec.Params[0])
		case "vwap":
			indicatorData["vwap"] = calcVWAP(highs, lows, closes, volumes)
		}
	}

	result := make([]CandleWithIndicators, n)
	for i, c := range candles {
		indVals := make(map[string]string, len(indicatorData))
		for col, vals := range indicatorData {
			if i < len(vals) {
				indVals[col] = vals[i] // empty string = null
			}
		}
		result[i] = CandleWithIndicators{Candle: c, Indicators: indVals}
	}
	return result, nil
}

// parseOHLCV extracts numeric slices from candle strings.
func parseOHLCV(candles []marketdata.Candle) (highs, lows, closes, volumes []float64, err error) {
	n := len(candles)
	highs = make([]float64, n)
	lows = make([]float64, n)
	closes = make([]float64, n)
	volumes = make([]float64, n)

	for i, c := range candles {
		if highs[i], err = strconv.ParseFloat(c.High, 64); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("candle[%d] high: %w", i, err)
		}
		if lows[i], err = strconv.ParseFloat(c.Low, 64); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("candle[%d] low: %w", i, err)
		}
		if closes[i], err = strconv.ParseFloat(c.Close, 64); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("candle[%d] close: %w", i, err)
		}
		if volumes[i], err = strconv.ParseFloat(c.Volume, 64); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("candle[%d] volume: %w", i, err)
		}
	}
	return
}

// calcEMA returns EMA values as strings. Uses standard multiplier 2/(period+1).
// Seeded with SMA of first period closes. "" for indices before period-1 (warm-up).
func calcEMA(closes []float64, period int) []string {
	n := len(closes)
	result := make([]string, n)
	if period > n {
		return result
	}

	sum := 0.0
	for i := range period {
		sum += closes[i]
	}
	ema := sum / float64(period)
	result[period-1] = fmtPrice(ema)

	mult := 2.0 / float64(period+1)
	for i := period; i < n; i++ {
		ema = closes[i]*mult + ema*(1-mult)
		result[i] = fmtPrice(ema)
	}
	return result
}

// calcEMAFloat is like calcEMA but returns float64 (for use in other indicators).
// Returns 0 for indices before period-1 (invalid).
func calcEMAFloat(values []float64, period int) []float64 {
	n := len(values)
	result := make([]float64, n)
	if period > n {
		return result
	}

	sum := 0.0
	for i := range period {
		sum += values[i]
	}
	ema := sum / float64(period)
	result[period-1] = ema

	mult := 2.0 / float64(period+1)
	for i := period; i < n; i++ {
		ema = values[i]*mult + ema*(1-mult)
		result[i] = ema
	}
	return result
}

// calcSMA returns SMA values as strings. "" for indices before period-1.
func calcSMA(closes []float64, period int) []string {
	n := len(closes)
	result := make([]string, n)
	for i := period - 1; i < n; i++ {
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			sum += closes[j]
		}
		result[i] = fmtPrice(sum / float64(period))
	}
	return result
}

// calcRSI returns RSI values using Wilder's smoothing. "" for first period indices.
func calcRSI(closes []float64, period int) []string {
	n := len(closes)
	result := make([]string, n)
	if n < period+1 {
		return result
	}

	gains := make([]float64, n)
	losses := make([]float64, n)
	for i := 1; i < n; i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			gains[i] = change
		} else {
			losses[i] = -change
		}
	}

	// Initial avg gain/loss = SMA of first period changes (indices 1..period)
	avgGain, avgLoss := 0.0, 0.0
	for i := 1; i <= period; i++ {
		avgGain += gains[i]
		avgLoss += losses[i]
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	result[period] = fmtRSI(avgGain, avgLoss)

	// Wilder's smoothing: multiplier = 1/period
	for i := period + 1; i < n; i++ {
		avgGain = (avgGain*float64(period-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + losses[i]) / float64(period)
		result[i] = fmtRSI(avgGain, avgLoss)
	}
	return result
}

func fmtRSI(avgGain, avgLoss float64) string {
	if avgLoss == 0 {
		if avgGain == 0 {
			return "50.00"
		}
		return "100.00"
	}
	rs := avgGain / avgLoss
	return fmtDecimal(100.0-100.0/(1+rs), 2)
}

// calcMACD returns MACD line, signal line, and histogram as string slices.
// Uses EMA(fast) - EMA(slow) for the MACD line, EMA(signal) of MACD for the signal line.
// MACD line is valid from index slow-1; signal/hist from index slow-1+signal-1.
func calcMACD(closes []float64, fast, slow, signalPeriod int) (macdVals, signalVals, histVals []string) {
	n := len(closes)
	macdVals = make([]string, n)
	signalVals = make([]string, n)
	histVals = make([]string, n)

	fastEMA := calcEMAFloat(closes, fast)
	slowEMA := calcEMAFloat(closes, slow)

	// MACD line: valid from slow-1 onward
	macdLine := make([]float64, n)
	for i := slow - 1; i < n; i++ {
		macdLine[i] = fastEMA[i] - slowEMA[i]
		macdVals[i] = fmtPrice(macdLine[i])
	}

	// Signal EMA computed on the MACD slice starting at slow-1
	signalStart := slow - 1
	if signalStart >= n {
		return
	}

	macdSlice := macdLine[signalStart:]
	signalEMA := calcEMAFloat(macdSlice, signalPeriod)

	// Signal and hist valid from signalPeriod-1 into the macdSlice
	for i := signalPeriod - 1; i < len(signalEMA); i++ {
		absI := signalStart + i
		if absI < n {
			signalVals[absI] = fmtPrice(signalEMA[i])
			histVals[absI] = fmtPrice(macdLine[absI] - signalEMA[i])
		}
	}
	return
}

// calcBBands returns upper, middle (SMA), and lower Bollinger Bands.
// "" for indices before period-1.
func calcBBands(closes []float64, period int, stddevMult float64) (upperVals, middleVals, lowerVals []string) {
	n := len(closes)
	upperVals = make([]string, n)
	middleVals = make([]string, n)
	lowerVals = make([]string, n)

	for i := period - 1; i < n; i++ {
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			sum += closes[j]
		}
		sma := sum / float64(period)

		varSum := 0.0
		for j := i - period + 1; j <= i; j++ {
			d := closes[j] - sma
			varSum += d * d
		}
		stdDev := math.Sqrt(varSum / float64(period))

		upperVals[i] = fmtPrice(sma + stddevMult*stdDev)
		middleVals[i] = fmtPrice(sma)
		lowerVals[i] = fmtPrice(sma - stddevMult*stdDev)
	}
	return
}

// calcATR returns ATR values using Wilder's smoothing. "" for first period indices.
func calcATR(highs, lows, closes []float64, period int) []string {
	n := len(closes)
	result := make([]string, n)
	if n < period+1 {
		return result
	}

	tr := make([]float64, n)
	for i := 1; i < n; i++ {
		tr1 := highs[i] - lows[i]
		tr2 := math.Abs(highs[i] - closes[i-1])
		tr3 := math.Abs(lows[i] - closes[i-1])
		tr[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// Initial ATR = SMA of first period TRs (indices 1..period)
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += tr[i]
	}
	atr := sum / float64(period)
	result[period] = fmtPrice(atr)

	// Wilder's smoothing
	for i := period + 1; i < n; i++ {
		atr = (atr*float64(period-1) + tr[i]) / float64(period)
		result[i] = fmtPrice(atr)
	}
	return result
}

// calcVWAP returns cumulative VWAP. Accumulates across all provided candles.
// VWAP = cumulative(volume * typicalPrice) / cumulative(volume).
// typicalPrice = (high + low + close) / 3.
func calcVWAP(highs, lows, closes, volumes []float64) []string {
	n := len(closes)
	result := make([]string, n)
	cumPV := 0.0
	cumVol := 0.0
	for i := range n {
		typical := (highs[i] + lows[i] + closes[i]) / 3.0
		cumPV += typical * volumes[i]
		cumVol += volumes[i]
		if cumVol > 0 {
			result[i] = fmtPrice(cumPV / cumVol)
		}
	}
	return result
}

func fmtPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

func fmtDecimal(v float64, prec int) string {
	return strconv.FormatFloat(v, 'f', prec, 64)
}
