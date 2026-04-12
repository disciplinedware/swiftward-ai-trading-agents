package marketdata

import (
	"strings"
	"time"

	"ai-trading-agents/internal/marketdata"
)

// FormatCandlesCSV converts candles to CSV string with full column names for pandas.
func FormatCandlesCSV(candles []marketdata.Candle) string {
	var b strings.Builder
	b.WriteString("timestamp,open,high,low,close,volume\n")

	for _, c := range candles {
		b.WriteString(c.Timestamp.Format(time.RFC3339))
		b.WriteByte(',')
		b.WriteString(c.Open)
		b.WriteByte(',')
		b.WriteString(c.High)
		b.WriteByte(',')
		b.WriteString(c.Low)
		b.WriteByte(',')
		b.WriteString(c.Close)
		b.WriteByte(',')
		b.WriteString(c.Volume)
		b.WriteByte('\n')
	}

	return b.String()
}

// FormatCandlesWithIndicatorsCSV formats candles with indicator columns as CSV.
// indicatorCols defines the column order and names for the header.
// Missing or null indicator values are written as empty fields.
func FormatCandlesWithIndicatorsCSV(candles []CandleWithIndicators, indicatorCols []string) string {
	var b strings.Builder

	b.WriteString("timestamp,open,high,low,close,volume")
	for _, col := range indicatorCols {
		b.WriteByte(',')
		b.WriteString(col)
	}
	b.WriteByte('\n')

	for _, c := range candles {
		b.WriteString(c.Timestamp.Format(time.RFC3339))
		b.WriteByte(',')
		b.WriteString(c.Open)
		b.WriteByte(',')
		b.WriteString(c.High)
		b.WriteByte(',')
		b.WriteString(c.Low)
		b.WriteByte(',')
		b.WriteString(c.Close)
		b.WriteByte(',')
		b.WriteString(c.Volume)
		for _, col := range indicatorCols {
			b.WriteByte(',')
			b.WriteString(c.Indicators[col]) // empty string for null
		}
		b.WriteByte('\n')
	}

	return b.String()
}
