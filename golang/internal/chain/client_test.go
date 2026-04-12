package chain

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBumpFee(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want int64
	}{
		{"normal_fee", 1_000_000_000, 1_200_000_000},    // 1 gwei -> 1.2 gwei
		{"large_fee", 100_000_000_000, 120_000_000_000}, // 100 gwei -> 120 gwei
		{"small_fee", 100, 120},                          // 100 wei -> 120 wei
		{"zero_gets_floor", 0, 1_000_000_000},            // 0 -> 1 gwei floor
		{"tiny_no_floor", 1, 1},                          // 1 * 120 / 100 = 1 (integer division), non-zero so no floor
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, big.NewInt(tt.want), bumpFee(big.NewInt(tt.in)))
		})
	}
}

func TestBumpFeeDoesNotMutateInput(t *testing.T) {
	original := big.NewInt(1_000_000_000)
	originalCopy := new(big.Int).Set(original)

	_ = bumpFee(original)

	assert.Equal(t, originalCopy, original, "bumpFee must not mutate input")
}

func TestBumpFeeConsecutiveBumpsExceedGethMinimum(t *testing.T) {
	// Geth requires replacement txs to have both gasTipCap and gasFeeCap at least
	// 10% higher than the pending tx. Verify 3 consecutive bumps (our max) always
	// exceed this threshold, including edge cases.
	tests := []struct {
		name    string
		initial int64
	}{
		{"typical_tip_2gwei", 2_000_000_000},
		{"low_tip_100wei", 100},
		{"zero_tip", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := big.NewInt(tt.initial)
			for i := range 3 {
				bumped := bumpFee(prev)

				// Must be strictly greater (or equal via integer division for tiny values).
				assert.True(t, bumped.Cmp(prev) >= 0,
					"bump %d: %s must be >= %s", i+1, bumped, prev)

				// Geth's 10% check: new >= old * 110 / 100
				// (integer division - same as geth)
				gethMin := new(big.Int).Mul(prev, big.NewInt(110))
				gethMin.Div(gethMin, big.NewInt(100))
				assert.True(t, bumped.Cmp(gethMin) >= 0,
					"bump %d: %s must pass geth 10%% minimum %s", i+1, bumped, gethMin)

				prev = bumped
			}
		})
	}
}

func TestWeiToGwei(t *testing.T) {
	tests := []struct {
		name string
		wei  int64
		want string
	}{
		{"zero", 0, "0"},
		{"one_gwei", 1_000_000_000, "1"},
		{"ten_gwei", 10_000_000_000, "10"},
		{"sub_gwei_truncates", 500_000_000, "0"},
		{"large", 150_000_000_000, "150"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, weiToGwei(big.NewInt(tt.wei)))
		})
	}
}
