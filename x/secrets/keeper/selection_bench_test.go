package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Wall-clock cost of candidate enumeration, which gas does NOT measure.
//
// Gas prices store access, so it says nothing about the SHA-256 ticket computed
// per candidate or the sort over all of them — both pure CPU, both unmetered.
// That distinction matters because unmetered work is a block-time question
// rather than a fee question: a creator cannot be charged for it, and there is
// no per-block gas ceiling to bound it
// (docs/CHAIN_MECHANICS.md, Security Observations §1).
//
// Selection is O(eligible) by construction and cannot be less: sortition takes
// the lowest max_shares tickets across all candidates, so every ticket must be
// computed. These numbers are what "O(eligible)" costs in practice, and they
// feed docs/CHAIN_MECHANICS.md ("Performance & Scaling").
//
//	go test ./x/secrets/keeper/ -run '^$' -bench BenchmarkSelectGuardians -benchtime 20x

func benchmarkSelection(b *testing.B, eligible int) {
	t := &testing.T{}
	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(1)

	bond := types.BondAmount(1_000, types.MinBump, types.InitialBondK)
	stake := sdk.NewCoin(types.DefaultDenom, bond.MulRaw(4))
	for i := 0; i < eligible; i++ {
		address := sdk.AccAddress([]byte(fmt.Sprintf("bench_g_%012d", i))).String()
		guardian := types.Guardian{
			Address:             address,
			EncryptionPublicKey: getValidPublicKey(fmt.Sprintf("bench_key_%d", i)),
			AvailableFrom:       0,
			AvailableUntil:      1_000_000,
			Stake:               &stake,
			AcceptingSecrets:    true,
			BondK:               types.InitialBondK,
		}
		if err := f.keeper.SetGuardian(ctx, guardian); err != nil {
			b.Fatalf("seeding guardian %d: %v", i, err)
		}
	}
	selector := keeper.NewGuardianSelector(&f.keeper)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A small band against a large candidate set: the shape that shows
		// enumeration dominating, since the work is per candidate, not per share.
		selected, _, _, err := selector.SelectGuardians(ctx, 7, 500_000, 1_000, types.MinBump, uint64(i))
		if err != nil {
			b.Fatalf("selection failed at %d eligible: %v", eligible, err)
		}
		if len(selected) != 7 {
			b.Fatalf("expected 7 selected, got %d", len(selected))
		}
	}
}

func BenchmarkSelectGuardians_1kEligible(b *testing.B)  { benchmarkSelection(b, 1_000) }
func BenchmarkSelectGuardians_10kEligible(b *testing.B) { benchmarkSelection(b, 10_000) }
