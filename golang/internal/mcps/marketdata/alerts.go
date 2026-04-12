package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
)

// alertCondition is a transient struct used by conditionMet to evaluate price conditions.
// Not persisted - DB alerts store these fields in AlertRecord.Params JSONB.
type alertCondition struct {
	Condition string
	Value     float64
	refPrice  float64 // reference price for change_pct condition
}

func conditionMet(a *alertCondition, currentPrice float64) bool {
	switch a.Condition {
	case "above":
		return currentPrice > a.Value
	case "below":
		return currentPrice < a.Value
	case "change_pct":
		if a.refPrice <= 0 {
			return false
		}
		changePct := (currentPrice - a.refPrice) / a.refPrice * 100
		return math.Abs(changePct) >= math.Abs(a.Value)
	}
	return false
}

func validateAlertCondition(condition string) error {
	switch condition {
	case "above", "below", "change_pct", "volume_spike", "funding_threshold", "oi_change_pct":
		return nil
	default:
		return fmt.Errorf("invalid condition %q: must be above, below, change_pct, volume_spike, funding_threshold, or oi_change_pct", condition)
	}
}

// isComplexCondition returns true for conditions that require extra data beyond price tickers.
func isComplexCondition(condition string) bool {
	switch condition {
	case "volume_spike", "funding_threshold", "oi_change_pct":
		return true
	}
	return false
}

// makeAlertID generates a deterministic alert ID from the alert parameters.
// Same inputs always produce the same ID, preventing duplicate alerts.
func makeAlertID(agentID, market, condition string, value float64, window string) string {
	raw := fmt.Sprintf("%s:%s:%s:%.6f:%s", agentID, market, condition, value, window)
	sum := sha256.Sum256([]byte(raw))
	return "alert-" + hex.EncodeToString(sum[:])[:16]
}
