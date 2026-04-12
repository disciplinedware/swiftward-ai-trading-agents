package marketdata

import (
	"testing"
	"time"
)

func TestInferFundingIntervalH(t *testing.T) {
	now := time.Date(2026, 4, 9, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		history []FundingRate
		want    int
	}{
		{
			name:    "empty history",
			history: nil,
			want:    8,
		},
		{
			name:    "single entry",
			history: []FundingRate{{Timestamp: now, Rate: "0.0001"}},
			want:    8,
		},
		{
			name: "8h interval (oldest first)",
			history: []FundingRate{
				{Timestamp: now, Rate: "0.0001"},
				{Timestamp: now.Add(8 * time.Hour), Rate: "0.0002"},
				{Timestamp: now.Add(16 * time.Hour), Rate: "0.0003"},
			},
			want: 8,
		},
		{
			name: "8h interval (newest first - Bybit order)",
			history: []FundingRate{
				{Timestamp: now.Add(16 * time.Hour), Rate: "0.0003"},
				{Timestamp: now.Add(8 * time.Hour), Rate: "0.0002"},
				{Timestamp: now, Rate: "0.0001"},
			},
			want: 8,
		},
		{
			name: "4h interval",
			history: []FundingRate{
				{Timestamp: now, Rate: "0.0001"},
				{Timestamp: now.Add(4 * time.Hour), Rate: "0.0002"},
			},
			want: 4,
		},
		{
			name: "1h interval",
			history: []FundingRate{
				{Timestamp: now.Add(1 * time.Hour), Rate: "0.0002"},
				{Timestamp: now, Rate: "0.0001"},
			},
			want: 1,
		},
		{
			name: "7h59m rounds to 8h",
			history: []FundingRate{
				{Timestamp: now, Rate: "0.0001"},
				{Timestamp: now.Add(7*time.Hour + 59*time.Minute), Rate: "0.0002"},
			},
			want: 8,
		},
		{
			name: "3h50m rounds to 4h",
			history: []FundingRate{
				{Timestamp: now.Add(3*time.Hour + 50*time.Minute), Rate: "0.0002"},
				{Timestamp: now, Rate: "0.0001"},
			},
			want: 4,
		},
		{
			name: "same timestamp falls back to 8h",
			history: []FundingRate{
				{Timestamp: now, Rate: "0.0001"},
				{Timestamp: now, Rate: "0.0002"},
			},
			want: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferFundingIntervalH(tt.history)
			if got != tt.want {
				t.Errorf("InferFundingIntervalH() = %d, want %d", got, tt.want)
			}
		})
	}
}
