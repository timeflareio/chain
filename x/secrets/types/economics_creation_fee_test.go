package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// The creation-fee derivation tests pin the ruled design (July 2026, see
// docs/spec.md "Creation Fee"): a linear basis-point curve 10% → 5% over
// 30 days of distance, a gas-denominated floor of 60,000 uveil, and
// fee = max(floor, P × bps ÷ 10,000) with truncating division.

// TestCreationFeeConstants guards the ruled values and the floor's
// gas-denomination (it must track MinRequiredFee, not a wage).
func TestCreationFeeConstants(t *testing.T) {
	require.Equal(t, int64(1_000), types.CreationFeeMaxBps)
	require.Equal(t, int64(500), types.CreationFeeMinBps)
	require.Equal(t, int64(432_000), types.CreationFeeCurveEndBlocks)
	require.Equal(t, uint64(600_000), types.CreationFeeFloorGas)
	require.Equal(t, int64(60_000), types.CreationFeeFloor().Int64(),
		"floor must equal MinRequiredFee(600,000 gas) = 60,000 uveil")
	require.Equal(t, types.MinRequiredFee(types.CreationFeeFloorGas).Int64(), types.CreationFeeFloor().Int64(),
		"the floor is gas-denominated by construction")
}

// TestCreationFeeBps pins the curve at its anchors and the truncating
// interior points from the spec's worked invoice.
func TestCreationFeeBps(t *testing.T) {
	cases := []struct {
		name     string
		distance int64
		want     int64
	}{
		{"zero distance is the 10% ceiling", 0, 1_000},
		{"1-day secret (spec table: 9.84%)", 14_400, 984},
		{"7-day secret (spec table: 8.84%)", 100_800, 884},
		{"curve end is exactly 5%", 432_000, 500},
		{"beyond the curve stays flat at 5%", 5_256_000, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, types.CreationFeeBps(tc.distance))
		})
	}
}

// TestCreationFee_WorkedInvoice reproduces the spec's worked invoice table
// row by row (rate = 1 uveil, amounts in uveil).
func TestCreationFee_WorkedInvoice(t *testing.T) {
	cases := []struct {
		name       string
		distance   int64
		maxShares  int64
		bump       int64
		wantTime   int64
		wantPool   int64
		wantAccept int64
		wantFee    int64
		floorPrice bool
	}{
		{"1-day sealed bid (3g, bump 1)", 14_400, 3, 100, 43_200, 82_200, 36_000, 60_000, true},
		{"7-day announcement (5g, bump 1)", 100_800, 5, 100, 504_000, 569_000, 60_000, 60_000, true},
		{"30-day dead-man's handle (5g, bump 2)", 432_000, 5, 200, 4_320_000, 4_385_000, 60_000, 216_000, false},
		{"70-day escrow (9g, bump 5)", 1_000_000, 9, 500, 45_000_000, 45_117_000, 108_000, 2_250_000, false},
		{"1-year max (32g, bump 10)", 5_256_000, 32, 1_000, 1_681_920_000, 1_682_336_000, 384_000, 84_096_000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			timeComponent := types.TimeComponentAmount(tc.distance, tc.maxShares, tc.bump)
			pool := types.RewardPoolAmount(tc.distance, tc.maxShares, tc.bump)
			require.Equal(t, tc.wantTime, timeComponent.Int64(), "time component")
			require.Equal(t, tc.wantPool, pool.Int64(), "reward pool (time + reveal legs)")
			require.Equal(t, tc.wantAccept, types.AcceptFeesAmount(tc.maxShares).Int64(), "escrowed accept fees")
			// The fee is charged on the time component, so it is exactly what
			// it was before the pool carried gas reimbursements — the draw
			// price does not move because a pass-through was added
			require.Equal(t, tc.wantFee, types.CreationFee(timeComponent, tc.distance).Int64(), "creation fee")
			require.Equal(t, tc.floorPrice, types.CreationFeeIsFloorPriced(timeComponent, tc.distance), "pricing regime")
		})
	}
}

// TestCreationFee_FloorCurveCrossover walks the boundary where the curve
// overtakes the floor. The fee is monotone non-decreasing in distance up
// to INTEGER-TRUNCATION DUST: each 1-bps step of the truncating curve can
// dip the fee by at most P ÷ 10,000 (one basis point of the pool — ~1
// uveil at devnet shapes, ≤ 0.01 VEIL at the largest). The dust can never
// make a longer secret meaningfully cheaper, and the floor is absolute.
func TestCreationFee_FloorCurveCrossover(t *testing.T) {
	prev := int64(0)
	flipped := false
	for d := int64(1_000); d <= 1_000_000; d += 1_000 {
		timeComponent := types.TimeComponentAmount(d, 5, 100)
		fee := types.CreationFee(timeComponent, d).Int64()
		dust := timeComponent.QuoRaw(10_000).Int64() // one basis point of the base
		require.GreaterOrEqual(t, fee, prev-dust,
			"fee may dip only by truncation dust, never more (d=%d)", d)
		require.GreaterOrEqual(t, fee, int64(60_000), "fee can never sit below the floor")
		if !types.CreationFeeIsFloorPriced(timeComponent, d) {
			flipped = true
		}
		prev = fee
	}
	require.True(t, flipped, "the sweep must cross from floor-priced to curve-priced")

	// The overall climb dwarfs the dust: doubling distance always costs
	// more across the whole sweep at any meaningful step size.
	for d := int64(10_000); d <= 500_000; d += 10_000 {
		feeAt := func(dd int64) int64 {
			return types.CreationFee(types.TimeComponentAmount(dd, 5, 100), dd).Int64()
		}
		require.Greater(t, feeAt(2*d), feeAt(d)-1,
			"doubling distance must never reduce the fee (d=%d)", d)
	}
}

// TestCreationFeeWith_Parameterised proves the constants are not baked into
// the arithmetic core.
func TestCreationFeeWith_Parameterised(t *testing.T) {
	// 20% flat curve (max = min = 2,000 bps), floor 5: fee = max(5, p/5)
	fee := types.CreationFeeWith(math.NewInt(100), 10, 2_000, 2_000, 100, math.NewInt(5))
	require.Equal(t, int64(20), fee.Int64())
	fee = types.CreationFeeWith(math.NewInt(10), 10, 2_000, 2_000, 100, math.NewInt(5))
	require.Equal(t, int64(5), fee.Int64(), "the floor binds when the curve is below it")
}
