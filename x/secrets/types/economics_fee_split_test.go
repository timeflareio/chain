package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// TestFeeSplitSharesTotal100 is the constants guard: the fee split shares
// must always total exactly 100 (constants.go).
func TestFeeSplitSharesTotal100(t *testing.T) {
	require.Equal(t, int64(100), types.FeeValidatorPercent+types.FeeBurnPercent,
		"fee split shares must total 100")
	require.Positive(t, types.FeeBurnPercent, "the burn share is the guaranteed-deflation guarantee — it must be > 0")
}

func TestSplitFeeAmount(t *testing.T) {
	cases := []struct {
		name          string
		amount        int64
		wantValidator int64
		wantBurn      int64
	}{
		{"exact split", 1000, 900, 100},
		{"division dust joins the burn", 15, 13, 2},
		{"single unit burns", 1, 0, 1},
		{"nine units", 9, 8, 1},
		{"zero", 0, 0, 0},
		{"large amount", 1_000_000_000_000, 900_000_000_000, 100_000_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validator, burn := types.SplitFeeAmount(math.NewInt(tc.amount))
			require.Equal(t, tc.wantValidator, validator.Int64())
			require.Equal(t, tc.wantBurn, burn.Int64())
			// Conservation is exact by construction
			require.Equal(t, tc.amount, validator.Add(burn).Int64())
		})
	}
}

// TestSplitFeeAmountWith exercises the parameterised core the economic
// simulator sweeps: any burn percentage, exact conservation, and the
// boundary regimes.
func TestSplitFeeAmountWith(t *testing.T) {
	// Boundary regimes
	v, b := types.SplitFeeAmountWith(math.NewInt(1000), 0)
	require.Equal(t, int64(1000), v.Int64())
	require.True(t, b.IsZero(), "0%% burn: everything to validators")

	v, b = types.SplitFeeAmountWith(math.NewInt(1000), 100)
	require.True(t, v.IsZero(), "100%% burn: everything burned")
	require.Equal(t, int64(1000), b.Int64())

	// Conservation holds for every percentage at awkward amounts
	for pct := int64(0); pct <= 100; pct++ {
		for _, amount := range []int64{1, 7, 99, 12345, 1_000_003} {
			validator, burn := types.SplitFeeAmountWith(math.NewInt(amount), pct)
			require.Equal(t, amount, validator.Add(burn).Int64(),
				"conservation must be exact at pct=%d amount=%d", pct, amount)
			require.False(t, validator.IsNegative())
			require.False(t, burn.IsNegative())
		}
	}
}

// TestMinRequiredFee pins the consensus fee floor derivation:
// required = ⌈gas × MinGasPriceUveilNum ÷ MinGasPriceUveilDen⌉ — ceiling
// division, so no gas limit prices to zero.
func TestMinRequiredFee(t *testing.T) {
	cases := []struct {
		name string
		gas  uint64
		want int64
	}{
		{"reference 200k-gas tx", 200_000, 20_000},
		{"exact division", 10, 1},
		{"ceiling: one over an even boundary", 200_001, 20_001},
		{"ceiling: below one uveil rounds up", 9, 1},
		{"ceiling: single gas unit", 1, 1},
		{"zero gas requires nothing", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, types.MinRequiredFee(tc.gas).Int64())
		})
	}
}

// TestMinRequiredFeeWith sweeps the parameterised core at a different
// fraction to prove the constants are not baked into the arithmetic.
func TestMinRequiredFeeWith(t *testing.T) {
	// 3/7 uveil per gas: 100 gas → ⌈42.857…⌉ = 43
	require.Equal(t, int64(43), types.MinRequiredFeeWith(100, 3, 7).Int64())
	// exact: 70 gas × 3/7 = 30
	require.Equal(t, int64(30), types.MinRequiredFeeWith(70, 3, 7).Int64())
}
