package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// TestParameterisedCoresMatchWrappers guards the parameterised-core
// refactor (introduced for the since-decommissioned economic simulator —
// docs/planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md §6): the
// constant-bound wrappers the chain calls must produce bit-identical
// results to the parameterised cores, evaluated at the v1 constants.
func TestParameterisedCoresMatchWrappers(t *testing.T) {
	for _, tc := range []struct{ distance, bump, k int64 }{
		{100, types.MinBump, types.MinBondK},
		{151, 100, types.MinBondK},
		{86400, 137, 504},
		{1_000_000, 250, 1270},
		{5_256_000, types.MaxBump, types.MaxBondK},
	} {
		require.Equal(t, types.BondAmount(tc.distance, tc.bump, tc.k),
			types.BondAmountWith(types.RatePerGuardianBlock, tc.distance, tc.bump, tc.k),
			"distance=%d bump=%d k=%d", tc.distance, tc.bump, tc.k)
	}

	for _, tc := range []struct{ distance, guardians, bump int64 }{
		{100, 5, 100},
		{150, 5, 100},
		{86400, 15, 500},
		{1, 1, types.MinBump},
		{5256000, 32, types.MaxBump}, // ~1y of 6s blocks, max shares, max bump
	} {
		require.Equal(t, types.RewardPoolAmount(tc.distance, tc.guardians, tc.bump),
			types.RewardPoolAmountWith(types.RatePerGuardianBlock, tc.distance, tc.guardians, tc.bump),
			"distance=%d guardians=%d bump=%d", tc.distance, tc.guardians, tc.bump)
	}
}

// TestSlashSplitConservation: burn + creator + remainder must equal the bond
// exactly for every input — the dust always rides with the remainder.
func TestSlashSplitConservation(t *testing.T) {
	for _, bond := range []int64{0, 1, 3, 99, 100, 101, 5_000_000_000, 12_345_678_901} {
		b := math.NewInt(bond)
		burn, creator, remainder := types.SlashSplitWith(b, types.NoRevealBurnPercent, types.NoRevealCreatorPercent)
		require.Equal(t, b, burn.Add(creator).Add(remainder), "bond %d", bond)
		require.False(t, burn.IsNegative())
		require.False(t, creator.IsNegative())
		require.False(t, remainder.IsNegative())
	}
}

// TestSlashSplitsMatchScenarioAmounts pins the exact splits the e2e scenario
// suite asserts on-chain (S1 no-show, S3 early-reveal): the suite's bump-1.00,
// distance-151, k-4.00 bond of 604 uveil splits 40% / 10% / remainder, with
// division dust riding the remainder.
func TestSlashSplitsMatchScenarioAmounts(t *testing.T) {
	bond := types.BondAmount(151, 100, types.MinBondK) // the suite's S1/S3 bond
	require.Equal(t, math.NewInt(604), bond)

	burn, creator, returned := types.NoRevealSlashSplit(bond)
	require.Equal(t, math.NewInt(241), burn)
	require.Equal(t, math.NewInt(60), creator)
	require.Equal(t, math.NewInt(303), returned)

	burn, creator, reporter := types.EarlyRevealSlashSplit(bond)
	require.Equal(t, math.NewInt(241), burn)
	require.Equal(t, math.NewInt(60), creator)
	require.Equal(t, math.NewInt(303), reporter)
}

// TestBondKAdjustments pins the ruled k-curve (docs/spec.md "The Per-Guardian
// Bond Multiplier k"): × 1.26 per slash reaching the ceiling in exactly 8
// steps from the floor, × 0.963 per reveal recovering floor-ward ~6× slower,
// truncating integer arithmetic, hard-clamped to [4.00, 24.00].
func TestBondKAdjustments(t *testing.T) {
	// The exact 8-step climb from the spec: 4.00 → 5.04 → 6.35 → 8.00 →
	// 10.08 → 12.70 → 16.00 → 20.16 → 24.00 (truncating at hundredths).
	climb := []int64{400, 504, 635, 800, 1008, 1270, 1600, 2016, 2400}
	k := types.InitialBondK
	for i, want := range climb {
		require.Equal(t, want, k, "climb step %d", i)
		k = types.NextBondKAfterSlash(k)
	}
	require.Equal(t, types.MaxBondK, types.NextBondKAfterSlash(types.MaxBondK), "ceiling-clamped: min, not max")

	// Recovery: strictly decreasing until the floor, never below it, and the
	// full descent from the ceiling lands within the ~48-step design target.
	k = types.MaxBondK
	steps := 0
	for k > types.MinBondK {
		next := types.NextBondKAfterReveal(k)
		require.Less(t, next, k, "recovery must strictly decrease above the floor")
		k = next
		steps++
		require.LessOrEqual(t, steps, 60, "recovery must terminate")
	}
	require.Equal(t, types.MinBondK, types.NextBondKAfterReveal(types.MinBondK), "floor-clamped: max, not min")
	require.InDelta(t, 48, steps, 3, "full recovery takes ~48 reveals")

	// One slash takes ~6 reveals to unwind from the floor.
	k = types.NextBondKAfterSlash(types.MinBondK)
	steps = 0
	for k > types.MinBondK {
		k = types.NextBondKAfterReveal(k)
		steps++
	}
	require.InDelta(t, 6, steps, 1, "one slash ≈ six reveals to unwind")

	// Defensive clamping: zero / out-of-range inputs normalise into the range.
	require.Equal(t, types.MinBondK, types.ClampBondK(0))
	require.Equal(t, types.MaxBondK, types.ClampBondK(9_999))
}

// TestGuardianIsNeverOutOfPocket is the invariant the work component exists to
// establish: a guardian that completes the job — accepts, holds, reveals — is
// paid at least what its two transactions cost, at EVERY distance the protocol
// permits, down to a single block. Before the work component the pool paid
// only for time, so any secret shorter than ~22,900 blocks settled the
// guardian at a loss (docs/planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md).
func TestGuardianIsNeverOutOfPocket(t *testing.T) {
	// What the two transactions cost at the consensus gas floor
	gasCost := types.MinRequiredFee(types.GuardianAcceptGas).
		Add(types.MinRequiredFee(types.GuardianRevealGas))

	for _, distance := range []int64{1, 10, 150, 1_000, 14_400, 100_800, 432_000, 5_256_000} {
		for _, maxShares := range []int64{2, 5, 7, 32} {
			// The lone-revealer case is not the tight one: a guardian that
			// reveals while others do not takes a LARGER share. The binding
			// case is every slot filled and every guardian revealing.
			pool := types.RewardPoolAmount(distance, maxShares, types.MinBump)
			acceptFees := types.AcceptFeesAmount(maxShares)

			perGuardian := pool.Quo(math.NewInt(maxShares)).
				Add(types.PerGuardianAcceptFee(acceptFees, maxShares))

			require.True(t, perGuardian.GTE(gasCost),
				"distance=%d maxShares=%d: guardian receives %s against %s of gas — completing the job must never cost it money",
				distance, maxShares, perGuardian, gasCost)
		}
	}
}

// TestAcceptFeesDivideExactly guards the property every terminal-state payout
// relies on: the escrowed accept fees split into whole uveil per guardian, so
// no path has to reason about dust on this bucket.
func TestAcceptFeesDivideExactly(t *testing.T) {
	for _, maxShares := range []int64{1, 2, 3, 5, 7, 9, 32} {
		total := types.AcceptFeesAmount(maxShares)
		perGuardian := types.PerGuardianAcceptFee(total, maxShares)
		require.True(t, perGuardian.Mul(math.NewInt(maxShares)).Equal(total),
			"maxShares=%d: %s does not divide exactly into %s", maxShares, total, perGuardian)
		require.Equal(t, types.AcceptLeg(), perGuardian,
			"each guardian's slice is exactly one accept leg")
	}
	require.True(t, types.PerGuardianAcceptFee(math.NewInt(100), 0).IsZero(),
		"a zero band cannot divide by zero")
}

// TestWorkComponentIsGasDenominated proves the reimbursements track the
// consensus gas floor rather than a hand-set wage — a retune of the floor
// moves them automatically, exactly as it moves the creation-fee floor.
func TestWorkComponentIsGasDenominated(t *testing.T) {
	require.Equal(t, types.MinRequiredFee(types.GuardianAcceptGas), types.AcceptLeg())
	require.Equal(t, types.MinRequiredFee(types.GuardianRevealGas), types.RevealLeg())
	require.Equal(t, int64(12_000), types.AcceptLeg().Int64(), "120,000 gas at 0.1 uveil/gas")
	require.Equal(t, int64(13_000), types.RevealLeg().Int64(), "130,000 gas at 0.1 uveil/gas")

	// The pool is the reveal legs plus the time component, exactly
	pool := types.RewardPoolAmount(14_400, 3, types.MinBump)
	require.Equal(t,
		types.RevealLeg().MulRaw(3).Add(types.TimeComponentAmount(14_400, 3, types.MinBump)),
		pool)
}
