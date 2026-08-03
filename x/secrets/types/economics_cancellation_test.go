package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// The cancellation wage derives from the secret's STORED pool, never the live
// rate constant — the in-flight repricing guarantee of the immutable-economics
// ruling (docs/planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md, work
// item 2). These tests pin both halves of that guarantee.

// At the live constants, the stored-pool derivation must equal the
// creation-time formula (rate × elapsed × bump ÷ scale) exactly — no
// precision drift across the parameter space.
func TestCancellationPayoutEqualsCreationFormula(t *testing.T) {
	cases := []struct{ distance, shares, bump, elapsed int64 }{
		{100_000, 5, 100, 0},             // spec example, cancel in commit phase
		{100_000, 5, 100, 1},             // first block of the hold
		{1_000_000, 9, 500, 400_000},     // spec's 40%-travelled example
		{5_256_000, 15, 1000, 5_000_000}, // max-horizon, max-bump
		{150, 2, 100, 149},               // smallest viable window, last block
		{1_000_003, 7, 333, 999_999},     // deliberately non-round values
	}
	for _, c := range cases {
		pool := types.RewardPoolAmount(c.distance, c.shares, c.bump)
		got := types.ProRataCancellationPayout(pool, c.distance, c.shares, c.elapsed)
		// The wage half, exactly as the creation-time formula computes it…
		wage := math.NewInt(types.RatePerGuardianBlock).
			Mul(math.NewInt(c.elapsed)).
			Mul(math.NewInt(c.bump)).
			Quo(math.NewInt(types.BumpScale))
		// …plus the pool's reveal leg, which accrues over the hold on the same
		// clock: a creator who cancels at activation pays neither, one who
		// cancels at the last block pays both.
		revealAccrued := types.RevealLeg().Mul(math.NewInt(c.elapsed)).Quo(math.NewInt(c.distance))
		want := wage.Add(revealAccrued)
		// Both sides floor independently, so they agree to within one uveil
		diff := got.Sub(want).Abs()
		require.True(t, diff.LTE(math.OneInt()),
			"distance=%d shares=%d bump=%d elapsed=%d: stored-derived %s != wage+accrued reveal leg %s",
			c.distance, c.shares, c.bump, c.elapsed, got, want)
	}
}

// Simulated software upgrade: a secret whose pool was escrowed under a
// DIFFERENT (creation-time) rate must be paid at that rate — the live
// constant must be invisible to the wage. This is the "mutate the constant
// mid-lifecycle" test expressed against stored state (Go constants cannot be
// mutated; a pool built from another rate is the same observable situation).
func TestCancellationPayoutTracksStoredPoolNotLiveConstant(t *testing.T) {
	const (
		creationRate = types.RatePerGuardianBlock * 2 // the rate "before the upgrade"
		distance     = int64(1_000_000)
		shares       = int64(5)
		bump         = int64(500)
		elapsed      = int64(400_000)
	)
	// The pool this secret escrowed at creation, priced at the old rate.
	oldRatePool := math.NewInt(creationRate).
		Mul(math.NewInt(distance)).
		Mul(math.NewInt(shares)).
		Mul(math.NewInt(bump)).
		Quo(math.NewInt(types.BumpScale))

	got := types.ProRataCancellationPayout(oldRatePool, distance, shares, elapsed)

	wageAtCreationRate := math.NewInt(creationRate).
		Mul(math.NewInt(elapsed)).Mul(math.NewInt(bump)).Quo(math.NewInt(types.BumpScale))
	wageAtLiveConstant := math.NewInt(types.RatePerGuardianBlock).
		Mul(math.NewInt(elapsed)).Mul(math.NewInt(bump)).Quo(math.NewInt(types.BumpScale))

	require.True(t, got.Equal(wageAtCreationRate),
		"wage must be priced at the creation-time rate: got %s want %s", got, wageAtCreationRate)
	require.False(t, got.Equal(wageAtLiveConstant),
		"wage must NOT track the live constant after a simulated retune")

	// Conservation: the creator's refund arithmetic never overdraws the pool.
	require.True(t, got.MulRaw(shares).LTE(oldRatePool),
		"total wages must never exceed the escrowed pool")
}

// Degenerate inputs never panic and never pay.
func TestCancellationPayoutDegenerateInputs(t *testing.T) {
	pool := types.RewardPoolAmount(100_000, 5, 100)
	require.True(t, types.ProRataCancellationPayout(pool, 100_000, 5, -50).IsZero(), "negative elapsed floors to 0")
	require.True(t, types.ProRataCancellationPayout(pool, 0, 5, 10).IsZero(), "zero distance pays nothing")
	require.True(t, types.ProRataCancellationPayout(pool, 100_000, 0, 10).IsZero(), "zero shares pays nothing")
	require.True(t, types.ProRataCancellationPayout(math.ZeroInt(), 100_000, 5, 10).IsZero(), "zero pool pays nothing")
}
