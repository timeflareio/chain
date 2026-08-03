package types_test

import (
	stdmath "math"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// The rebate's two ceilings are pure arithmetic, so they are pinned here rather
// than inferred from keeper behaviour. Reference figures come from the plan
// (docs/planning/done/DONE_RECIPIENT_REBATE_PLAN.md §4): a 700,000,000 VEIL pool
// accrues 14 VEIL per block, one day of that is the burst cap, and the ratio
// ceiling is 30% of the creator's irrecoverable spend.

const (
	veil     = int64(1_000_000)          // 1 VEIL in uveil
	poolFull = int64(700_000_000) * veil // 700M VEIL: the genesis rebate pool
)

func TestRebateAccrualPerBlock(t *testing.T) {
	tests := []struct {
		name    string
		balance int64
		want    int64
	}{
		{"genesis pool accrues 14 VEIL per block", poolFull, 14 * veil},
		{"half the pool halves the rate", poolFull / 2, 7 * veil},
		{"a tenth of the pool tenths the rate", poolFull / 10, 14 * veil / 10},
		{"empty pool accrues nothing", 0, 0},
		{"a balance below the divisor accrues nothing", types.RebateAccrualDivisor - 1, 0},
		{"a balance at the divisor accrues one uveil", types.RebateAccrualDivisor, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := types.RebateAccrualPerBlock(math.NewInt(tc.balance))
			require.Equal(t, tc.want, got.Int64())
		})
	}
}

// The accrual rate is what bounds the drain, and the plan's headline number is
// 10% of the remaining balance per year under full claim. That figure is
// GEOMETRIC: each payout lowers the balance, so the rate falls as the pool
// pays. Summing a year of accruals at the starting balance instead gives
// 10.51%, which overstates the real drain — a distinction worth pinning,
// because the naive product is the easy mistake when re-deriving the divisor.
func TestRebateAccrual_AnnualDrainIsTenPercent(t *testing.T) {
	const blocksPerYear = 5_256_000.0 // 6s blocks

	perBlockFraction := 1.0 / float64(types.RebateAccrualDivisor)
	drained := 1.0 - stdmath.Pow(1.0-perBlockFraction, blocksPerYear)

	require.InDelta(t, 0.10, drained, 0.001,
		"fully claimed, a year must drain ~10%% of the balance; got %.4f%%", drained*100)

	// The bound holds at every scale, not just annually: a day of accrual at
	// the genesis pool is the burst cap, 201,600 VEIL.
	dayOfAccrual := types.RebateAccrualPerBlock(math.NewInt(poolFull)).MulRaw(types.RebateBurstBlocks)
	require.Equal(t, int64(201_600)*veil, dayOfAccrual.Int64())
}

func TestRebateAllowanceCap_IsOneDayOfAccrual(t *testing.T) {
	balance := math.NewInt(poolFull)
	require.Equal(t,
		types.RebateAccrualPerBlock(balance).MulRaw(types.RebateBurstBlocks),
		types.RebateAllowanceCap(balance),
	)
	// One day at 6s blocks, and 201,600 VEIL at the genesis pool.
	require.Equal(t, int64(14_400), types.RebateBurstBlocks)
	require.Equal(t, int64(201_600)*veil, types.RebateAllowanceCap(balance).Int64())
}

func TestAccrueRebateAllowance(t *testing.T) {
	balance := math.NewInt(poolFull)
	perBlock := types.RebateAccrualPerBlock(balance).Int64()
	full := types.RebateAllowanceCap(balance).Int64()

	tests := []struct {
		name      string
		allowance int64
		elapsed   int64
		want      int64
	}{
		{"no elapsed blocks accrue nothing", 5 * veil, 0, 5 * veil},
		{"negative elapsed accrues nothing", 5 * veil, -100, 5 * veil},
		{"one block accrues one block", 0, 1, perBlock},
		{"ten blocks accrue ten blocks", 0, 10, 10 * perBlock},
		{"accrual accumulates onto an existing allowance", 3 * perBlock, 2, 5 * perBlock},
		{"a day of blocks reaches the cap exactly", 0, types.RebateBurstBlocks, full},
		{"a month of idle blocks is capped at one day", 0, types.RebateBurstBlocks * 30, full},
		{"an allowance already at the cap stays there", full, 100, full},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := types.AccrueRebateAllowance(math.NewInt(tc.allowance), balance, tc.elapsed)
			require.Equal(t, tc.want, got.Int64())
		})
	}
}

// An allowance carried over from a larger balance must be trimmed to the cap
// the CURRENT balance supports — otherwise a drained pool could still pay out
// at yesterday's rate.
func TestAccrueRebateAllowance_CapFollowsTheBalanceDown(t *testing.T) {
	stale := types.RebateAllowanceCap(math.NewInt(poolFull))
	shrunk := math.NewInt(poolFull / 100)

	got := types.AccrueRebateAllowance(stale, shrunk, 0)
	require.Equal(t, types.RebateAllowanceCap(shrunk), got)
	require.True(t, got.LT(stale))
}

func TestRebateRatioOf(t *testing.T) {
	require.Equal(t, int64(30), types.RebateRatioPercent, "the ratio is protocol-visible; a change is a spec change")

	tests := []struct {
		name  string
		spend int64
		want  int64
	}{
		{"30% of a thousand VEIL", 1000 * veil, 300 * veil},
		{"30% of one VEIL", veil, 300_000},
		{"rounds down", 10, 3},
		{"zero spend earns nothing", 0, 0},
		{"negative spend earns nothing", -1000, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, types.RebateRatioOf(math.NewInt(tc.spend)).Int64())
		})
	}
}

func TestRebateAmount(t *testing.T) {
	dayOfAllowance := types.RebateAllowanceCap(math.NewInt(poolFull)).Int64() // 201,600 VEIL

	tests := []struct {
		name          string
		spend         int64
		allowance     int64
		settlingCount int64
		want          int64
	}{
		{
			name:          "the ratio binds when the allowance is ample",
			spend:         1000 * veil,
			allowance:     dayOfAllowance,
			settlingCount: 1,
			want:          300 * veil,
		},
		{
			name:          "the allowance share binds when it is scarce",
			spend:         1000 * veil,
			allowance:     100 * veil,
			settlingCount: 1,
			want:          100 * veil,
		},
		{
			name:          "settling secrets divide the allowance equally",
			spend:         1000 * veil,
			allowance:     100 * veil,
			settlingCount: 4,
			want:          25 * veil,
		},
		{
			name:          "a large cluster still gets the full ratio if the allowance covers it",
			spend:         1000 * veil,
			allowance:     dayOfAllowance,
			settlingCount: 600,
			want:          300 * veil,
		},
		{
			name:          "a share below the dust floor credits nothing",
			spend:         1000 * veil,
			allowance:     types.RebateDustFloor * 4,
			settlingCount: 5,
			want:          0,
		},
		{
			name:          "a share exactly at the dust floor is credited",
			spend:         1000 * veil,
			allowance:     types.RebateDustFloor,
			settlingCount: 1,
			want:          types.RebateDustFloor,
		},
		{
			// A day-long three-share secret: pool 0.043 VEIL, so 30% is 0.013
			// VEIL — below the floor, and not worth a transaction to collect.
			name:          "a spend too small to clear the dust floor credits nothing",
			spend:         43_200,
			allowance:     dayOfAllowance,
			settlingCount: 1,
			want:          0,
		},
		{
			// A month-long five-share secret: pool 2.16 VEIL, rebate 0.648 VEIL.
			// The realistic case the floor must NOT exclude.
			name:          "a month-long secret's rebate clears the floor",
			spend:         2_160_000,
			allowance:     dayOfAllowance,
			settlingCount: 1,
			want:          648_000,
		},
		{
			name:          "no allowance credits nothing",
			spend:         1000 * veil,
			allowance:     0,
			settlingCount: 1,
			want:          0,
		},
		{
			name:          "a zero settling count credits nothing",
			spend:         1000 * veil,
			allowance:     dayOfAllowance,
			settlingCount: 0,
			want:          0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := types.RebateAmount(math.NewInt(tc.spend), math.NewInt(tc.allowance), tc.settlingCount)
			require.Equal(t, tc.want, got.Int64())
		})
	}
}

// The invariant the whole mechanism rests on: a rebate can never exceed the
// ratio of the spend that produced it, whatever the allowance. If this fails,
// self-dealing stops being a loss.
func TestRebateAmount_NeverExceedsTheRatio(t *testing.T) {
	spends := []int64{veil, 10 * veil, 1000 * veil, 100_000 * veil, poolFull}
	allowances := []int64{0, veil, 1000 * veil, poolFull}
	counts := []int64{1, 2, 7, 600, 5000}

	for _, spend := range spends {
		ceiling := types.RebateRatioOf(math.NewInt(spend))
		for _, allowance := range allowances {
			for _, count := range counts {
				got := types.RebateAmount(math.NewInt(spend), math.NewInt(allowance), count)
				require.True(t, got.LTE(ceiling),
					"rebate %s exceeded the ratio ceiling %s (spend %d, allowance %d, count %d)",
					got, ceiling, spend, allowance, count)
				require.True(t, got.LTE(math.NewInt(allowance)),
					"rebate %s exceeded the whole allowance %d", got, allowance)
			}
		}
	}
}

// A height's credits must never sum above the allowance, however the cluster
// divides — the property that makes the per-block bound hold.
func TestRebateAmount_HeightNeverExceedsTheAllowance(t *testing.T) {
	for _, count := range []int64{1, 2, 3, 7, 13, 100, 601} {
		allowance := math.NewInt(1000 * veil)
		total := math.ZeroInt()
		for i := int64(0); i < count; i++ {
			amount := types.RebateAmount(math.NewInt(100_000*veil), allowance, count)
			total = total.Add(amount)
		}
		require.True(t, total.LTE(math.NewInt(1000*veil)),
			"%d settling secrets credited %s against a 1000 VEIL allowance", count, total)
	}
}
