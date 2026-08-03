package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// The eligibility index has exactly one correctness obligation: yield the SAME
// CANDIDATE SET the full guardian walk would have.
//
// It is allowed to yield it in a different order. ticket(g) = SHA256(seed ‖
// address) depends only on the seed and the guardian's own address, so selection
// is independent of enumeration order (docs/spec.md, "Guardian Selection
// (Normative)"; proved by TestSelectBySortition_OrderIndependence). Set equality
// is therefore the whole of the obligation — and the thing worth testing
// directly, because a range bound that is off by one guardian is a
// consensus-visible change in who can be selected, not a performance
// regression.

// eligibleByWalk is the reference implementation: the spec's eligibility
// predicate applied by walking every guardian record, exactly as candidate
// enumeration did before the index existed. Deliberately naive — it is the
// oracle, so it must be obviously correct rather than fast.
func eligibleByWalk(t *testing.T, f *fixture, ctx sdk.Context, revealEndBlock, distance, bump int64) map[string]int64 {
	t.Helper()
	height := ctx.BlockHeight()
	eligible := map[string]int64{}

	require.NoError(t, f.keeper.Guardians.Walk(ctx, nil, func(_ string, guardian types.Guardian) (bool, error) {
		if !guardian.AcceptingSecrets {
			return false, nil
		}
		if height < guardian.AvailableFrom || height > guardian.AvailableUntil {
			return false, nil
		}
		if guardian.AvailableUntil < revealEndBlock {
			return false, nil
		}
		if guardian.ActiveBondCount >= types.MaxActiveBondsPerGuardian {
			return false, nil
		}
		bond := types.BondAmount(distance, bump, types.ClampBondK(guardian.BondK))
		if keeper.UnlockedFloat(&guardian).LT(bond) {
			return false, nil
		}
		eligible[guardian.Address] = bond.Int64()
		return false, nil
	}))
	return eligible
}

// indexGuardian writes a guardian with a fully specified shape through the
// keeper's choke point, so the eligibility index tracks it as production would.
//
// Epoch 0 of the key history is seeded too: the guardian is otherwise invalid
// state (invariant 7), and a fixture that trips a different invariant is no use
// to a test that asserts one specific invariant fires.
func indexGuardian(t *testing.T, f *fixture, ctx sdk.Context, address string, g types.Guardian) {
	t.Helper()
	key := getValidPublicKey("index_" + address)
	g.Address = address
	g.EncryptionPublicKey = key
	require.NoError(t, f.keeper.SetGuardian(ctx, g))

	if has, err := f.keeper.GuardianKeyHistory.Has(ctx, collections.Join(address, uint64(0))); err == nil && !has {
		require.NoError(t, f.keeper.AppendGuardianKeyEpoch(ctx, address, 0, types.KeyHistoryEntry{
			PublicKey:           key,
			EffectiveFromHeight: 0,
		}))
	}
}

func coin(amount int64) *sdk.Coin {
	c := sdk.NewCoin(types.DefaultDenom, math.NewInt(amount))
	return &c
}

// TestEligibilityIndexMatchesWalk sweeps a registry containing every way a
// guardian can fail the predicate — plus the boundary cases on each — and
// asserts the index and the walk agree on the candidate set and on every
// candidate's frozen bond.
func TestEligibilityIndexMatchesWalk(t *testing.T) {
	const (
		height         = int64(1_000)
		revealEndBlock = int64(2_000)
		distance       = int64(1_000)
		bump           = types.MinBump
	)

	f := initFixture(t)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(height)

	bond := types.BondAmount(distance, bump, types.InitialBondK)
	ample := bond.MulRaw(4).Int64()

	// Each entry names the predicate clause it exercises. The eligible ones are
	// the assertion's substance; the ineligible ones are what a wrong range bound
	// would wrongly admit.
	cases := []struct {
		name     string
		guardian types.Guardian
		eligible bool
	}{
		{"plain eligible", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK}, true},

		{"available_until exactly at reveal end (inclusive boundary)", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK}, true},

		{"available_until one short of reveal end", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock - 1, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK}, false},

		{"lapsed long ago — the permanent-registration case", types.Guardian{
			AvailableFrom: 0, AvailableUntil: height - 500, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK}, false},

		{"available_from exactly now (inclusive boundary)", types.Guardian{
			AvailableFrom: height, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK}, true},

		{"not yet active", types.Guardian{
			AvailableFrom: height + 1, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK}, false},

		{"not accepting", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: false,
			Stake: coin(ample), BondK: types.InitialBondK}, false},

		{"at the concurrency cap", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK,
			ActiveBondCount: types.MaxActiveBondsPerGuardian}, false},

		{"one below the concurrency cap", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(ample), BondK: types.InitialBondK,
			ActiveBondCount: types.MaxActiveBondsPerGuardian - 1}, true},

		{"unlocked float exactly covers the bond", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(bond.Int64()), BondK: types.InitialBondK}, true},

		{"unlocked float one uveil short", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(bond.Int64() - 1), BondK: types.InitialBondK}, false},

		{"float ample but mostly locked, leaving too little", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(ample), LockedStake: coin(ample - bond.Int64() + 1),
			BondK: types.InitialBondK, ActiveBondCount: 1}, false},

		// A raised k prices a bigger bond, so the same float no longer covers it —
		// the bond must be priced per candidate, not once for the secret.
		{"raised k makes an otherwise ample float insufficient", types.Guardian{
			AvailableFrom: 0, AvailableUntil: revealEndBlock + 500, AcceptingSecrets: true,
			Stake: coin(bond.Int64()), BondK: types.MaxBondK}, false},
	}

	expectedEligible := map[string]bool{}
	for i, tc := range cases {
		address := sdk.AccAddress([]byte(fmt.Sprintf("idx_case_%02d_________", i))).String()
		indexGuardian(t, f, ctx, address, tc.guardian)
		if tc.eligible {
			expectedEligible[address] = true
		}
	}

	// The index must agree with the walk...
	walked := eligibleByWalk(t, f, ctx, revealEndBlock, distance, bump)
	indexed, _, err := f.keeper.EligibleCandidatesFor(ctx, revealEndBlock, distance, bump)
	require.NoError(t, err)

	fromIndex := map[string]int64{}
	for _, candidate := range indexed {
		require.NotContains(t, fromIndex, candidate.Address, "index yielded %s twice", candidate.Address)
		fromIndex[candidate.Address] = candidate.Bond
	}
	require.Equal(t, walked, fromIndex,
		"the index must yield exactly the candidate set — and the bonds — the guardian walk would")

	// ...and the walk itself must match the table, so a predicate that drifted in
	// BOTH implementations at once still fails.
	require.Len(t, walked, len(expectedEligible))
	for address := range expectedEligible {
		require.Contains(t, walked, address)
	}
}

// TestEligibilityIndexInvariantCatchesDrift proves invariant 9 actually bites.
//
// The invariant is the only thing standing between "SetGuardian is the sole
// guardian writer" as a claim and as a fact, so an invariant that passed
// regardless would be worse than none — it would license the assumption it
// pretends to check. Each case corrupts the index the way a real bypassing
// writer would.
func TestEligibilityIndexInvariantCatchesDrift(t *testing.T) {
	const height = int64(500)

	setup := func(t *testing.T) (*fixture, sdk.Context, types.Guardian) {
		t.Helper()
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(height)
		address := sdk.AccAddress([]byte("drift_guardian______")).String()
		indexGuardian(t, f, ctx, address, types.Guardian{
			AvailableFrom: 0, AvailableUntil: height + 1_000, AcceptingSecrets: true,
			Stake: coin(5_000_000_000), BondK: types.InitialBondK,
		})
		require.NoError(t, f.keeper.CheckStateInvariants(ctx), "the honest state must pass")
		guardian, found := f.keeper.GetGuardian(ctx, address)
		require.True(t, found)
		return f, ctx, guardian
	}

	t.Run("record written without the index", func(t *testing.T) {
		f, ctx, guardian := setup(t)
		require.NoError(t, f.keeper.GuardianEligibility.Remove(ctx,
			collections.Join(guardian.AvailableUntil, guardian.Address)))
		require.ErrorContains(t, f.keeper.CheckStateInvariants(ctx),
			"guardians are missing from the index")
	})

	t.Run("stale key left behind when the window moves", func(t *testing.T) {
		f, ctx, guardian := setup(t)
		// What writing the record directly would leave: the new key present, the
		// old one never retired.
		moved := guardian
		moved.AvailableUntil = guardian.AvailableUntil + 500
		require.NoError(t, f.keeper.Guardians.Set(ctx, moved.Address, moved))
		require.ErrorContains(t, f.keeper.CheckStateInvariants(ctx),
			"the previous key was not retired")
	})

	t.Run("projection drifts from the record", func(t *testing.T) {
		f, ctx, guardian := setup(t)
		stale := keeper.GuardianEligibilityOf(guardian)
		stale.ActiveBondCount = 99
		require.NoError(t, f.keeper.GuardianEligibility.Set(ctx,
			collections.Join(guardian.AvailableUntil, guardian.Address), stale))
		require.ErrorContains(t, f.keeper.CheckStateInvariants(ctx), "is stale")
	})

	t.Run("orphan entry with no guardian behind it", func(t *testing.T) {
		f, ctx, guardian := setup(t)
		require.NoError(t, f.keeper.GuardianEligibility.Set(ctx,
			collections.Join(int64(9_999), "tmflr1ghost"), keeper.GuardianEligibilityOf(guardian)))
		require.ErrorContains(t, f.keeper.CheckStateInvariants(ctx),
			"no guardian record exists at that address")
	})
}

// TestEligibilityIndexIgnoresIneligibleRegistrations is the property the index
// exists for: registrations that cannot be selected must cost the creator
// nothing.
//
// Registration is permanent — there is no deregistration — so without this the
// dead are charged to every future creator forever, and phase 1 eventually
// aborts out of gas for everyone. Measuring against a lapsed cohort is the only
// way to state that as a test rather than a hope.
func TestEligibilityIndexIgnoresIneligibleRegistrations(t *testing.T) {
	const (
		live     = 40
		dead     = 400
		band     = int64(7)
		distance = int64(1_000)
	)

	// The LIVE cohort is identical in both runs, prefix included: addresses are
	// derived from the prefix, so a longer one would change how many bytes each
	// index read charges for and show up as a difference that has nothing to do
	// with the dead cohort.
	measure := func(t *testing.T, deadPrefix string, deadCount int) uint64 {
		t.Helper()
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)

		registerSelectionGasGuardians(t, f, msgServer, "deadweight_live", live, 100000)

		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		f.ctx = sdkCtx.WithBlockHeight(sdkCtx.BlockHeight() + 1)
		ctx := sdk.UnwrapSDKContext(f.ctx)

		// Lapsed registrations: still accepting, still funded, but their window
		// closed before the current height. Written through the choke point, so the
		// index tracks them exactly as it would in production.
		bond := types.BondAmount(distance, types.MinBump, types.InitialBondK)
		for i := 0; i < deadCount; i++ {
			address := sdk.AccAddress([]byte(fmt.Sprintf("%s_dead_%06d", deadPrefix, i))).String()
			indexGuardian(t, f, ctx, address, types.Guardian{
				AvailableFrom:    0,
				AvailableUntil:   ctx.BlockHeight() - 1,
				AcceptingSecrets: true,
				Stake:            coin(bond.MulRaw(4).Int64()),
				BondK:            types.InitialBondK,
			})
		}

		creator := sdk.AccAddress([]byte("dead_weight_creator_")).String()
		var reqErr error
		used := measureGas(t, f, func(c sdk.Context) {
			_, reqErr = msgServer.UserRequestGuardians(c, &types.MsgUserRequestGuardians{
				Creator:       creator,
				DetectionHint: testDetectionHint(),
				Threshold:     3,
				MinShares:     band,
				MaxShares:     band,
				RevealWindow:  &types.RevealWindow{StartOffset: 400, Duration: testRevealDuration},
				Bump:          types.MinBump,
			})
		})
		require.NoError(t, reqErr)
		return used
	}

	withoutDead := measure(t, "deadcohort", 0)
	withDead := measure(t, "deadcohort", dead)

	t.Logf("phase-1 gas: %d with %d live guardians, %d with %d more lapsed registrations",
		withoutDead, live, withDead, dead)

	// Exactly equal is the honest expectation: lapsed entries sort below the range
	// start and are never read. A tolerance is allowed only for incidental
	// differences, not for per-guardian cost — at the old 2,092 per registration
	// this cohort alone would have added over 800,000 gas.
	require.InDelta(t, withoutDead, withDead, 1_000,
		"lapsed registrations must not be charged to the creator: %d extra gas for %d dead guardians. "+
			"The range read is bounded by available_until >= reveal_end_block, so anything below it "+
			"should never be touched", int64(withDead)-int64(withoutDead), dead)
}

// TestRebuildEligibilityIndex is the migration path: the index is derived state,
// and an in-place chain upgrade never runs InitGenesis, so without a rebuild a
// chain whose store predates the index resumes with an empty one and fails every
// creation with "insufficient guardians".
//
// Each case starts from a different kind of wrong state, because a rebuild that
// only handled "empty" would leave the drift cases — the ones an upsert cannot
// fix — in place.
func TestRebuildEligibilityIndex(t *testing.T) {
	const height = int64(500)

	// A registry with all three membership outcomes: indexed, excluded for not
	// accepting, and indexed at a moved window.
	setup := func(t *testing.T) (*fixture, sdk.Context, []string) {
		t.Helper()
		f := initFixture(t)
		ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(height)
		var accepting []string
		for i := 0; i < 6; i++ {
			address := sdk.AccAddress([]byte(fmt.Sprintf("rebuild_g_%010d", i))).String()
			acceptingSecrets := i%3 != 0 // two of every three accept
			indexGuardian(t, f, ctx, address, types.Guardian{
				AvailableFrom: 0, AvailableUntil: height + int64(1_000+i), AcceptingSecrets: acceptingSecrets,
				Stake: coin(5_000_000_000), BondK: types.InitialBondK,
			})
			if acceptingSecrets {
				accepting = append(accepting, address)
			}
		}
		require.NoError(t, f.keeper.CheckStateInvariants(ctx))
		return f, ctx, accepting
	}

	// snapshot reads the index into a comparable form.
	snapshot := func(t *testing.T, f *fixture, ctx sdk.Context) map[string]types.GuardianEligibility {
		t.Helper()
		out := map[string]types.GuardianEligibility{}
		require.NoError(t, f.keeper.GuardianEligibility.Walk(ctx, nil,
			func(key collections.Pair[int64, string], entry types.GuardianEligibility) (bool, error) {
				out[fmt.Sprintf("%d/%s", key.K1(), key.K2())] = entry
				return false, nil
			}))
		return out
	}

	corruptions := map[string]func(t *testing.T, f *fixture, ctx sdk.Context, accepting []string){
		"empty index — the pre-index chain a migration exists for": func(t *testing.T, f *fixture, ctx sdk.Context, _ []string) {
			require.NoError(t, f.keeper.GuardianEligibility.Clear(ctx, nil))
		},
		"orphan entry no guardian backs": func(t *testing.T, f *fixture, ctx sdk.Context, _ []string) {
			require.NoError(t, f.keeper.GuardianEligibility.Set(ctx,
				collections.Join(int64(9_999), "tmflr1ghostentry"),
				types.GuardianEligibility{Unlocked: sdk.NewCoin(types.DefaultDenom, math.OneInt())}))
		},
		"stale projection": func(t *testing.T, f *fixture, ctx sdk.Context, accepting []string) {
			guardian, found := f.keeper.GetGuardian(ctx, accepting[0])
			require.True(t, found)
			stale := keeper.GuardianEligibilityOf(guardian)
			stale.ActiveBondCount = 77
			require.NoError(t, f.keeper.GuardianEligibility.Set(ctx,
				collections.Join(guardian.AvailableUntil, guardian.Address), stale))
		},
		"key left at a window the guardian no longer has": func(t *testing.T, f *fixture, ctx sdk.Context, accepting []string) {
			guardian, found := f.keeper.GetGuardian(ctx, accepting[1])
			require.True(t, found)
			require.NoError(t, f.keeper.GuardianEligibility.Set(ctx,
				collections.Join(guardian.AvailableUntil+50, guardian.Address),
				keeper.GuardianEligibilityOf(guardian)))
		},
	}

	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			f, ctx, accepting := setup(t)
			want := snapshot(t, f, ctx)

			corrupt(t, f, ctx, accepting)

			// Every corruption must be observable, or the case proves nothing.
			require.NotEqual(t, want, snapshot(t, f, ctx), "the corruption did not change the index")

			require.NoError(t, f.keeper.RebuildEligibilityIndex(ctx))

			require.Equal(t, want, snapshot(t, f, ctx),
				"the rebuild must restore exactly the index SetGuardian would have produced")
			require.NoError(t, f.keeper.CheckStateInvariants(ctx),
				"invariant 9 must pass after a rebuild")

			// Idempotent: running it again changes nothing.
			require.NoError(t, f.keeper.RebuildEligibilityIndex(ctx))
			require.Equal(t, want, snapshot(t, f, ctx))
		})
	}

	t.Run("rebuilt index selects the same candidates as the walk", func(t *testing.T) {
		f, ctx, _ := setup(t)
		require.NoError(t, f.keeper.GuardianEligibility.Clear(ctx, nil))
		require.NoError(t, f.keeper.RebuildEligibilityIndex(ctx))

		const revealEnd = height + 900
		walked := eligibleByWalk(t, f, ctx, revealEnd, 1_000, types.MinBump)
		indexed, _, err := f.keeper.EligibleCandidatesFor(ctx, revealEnd, 1_000, types.MinBump)
		require.NoError(t, err)

		fromIndex := map[string]int64{}
		for _, c := range indexed {
			fromIndex[c.Address] = c.Bond
		}
		require.Equal(t, walked, fromIndex)
		require.NotEmpty(t, walked, "the fixture must produce candidates or this proves nothing")
	})
}

// TestInsufficientGuardiansExplainsWhy pins the strict gate's diagnostics.
//
// "need 13, have 0" is true and nearly useless: it gives a creator no way to tell
// whether nobody is available that far out, whether guardians are saturated, or
// whether they cannot afford the bond. The first dominates in practice, because
// selection requires available_until >= reveal_end_block while guardians register
// for windows far shorter than the protocol's one-year horizon — a creator asking
// for a year-long secret against a devnet of short-window guardians gets a
// failure with no hint that availability was the cause.
//
// Pinned as a test because an error message is the only part of a handler with no
// other consumer: nothing else breaks when it silently stops being accurate.
func TestInsufficientGuardiansExplainsWhy(t *testing.T) {
	const height = int64(1_000)

	newFixture := func(t *testing.T) (*fixture, sdk.Context) {
		t.Helper()
		f := initFixture(t)
		return f, sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(height)
	}

	// gateError runs selection expecting failure and returns the message.
	gateError := func(t *testing.T, f *fixture, ctx sdk.Context, maxShares, revealEnd int64) string {
		t.Helper()
		selector := keeper.NewGuardianSelector(&f.keeper)
		_, _, _, err := selector.SelectGuardians(ctx, maxShares, revealEnd, 1_000, types.MinBump, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "insufficient guardians",
			"the prefix is load-bearing — callers and tests match on it")
		return err.Error()
	}

	t.Run("no guardians at all", func(t *testing.T) {
		f, ctx := newFixture(t)
		require.Contains(t, gateError(t, f, ctx, 3, height+500),
			"no guardians are registered as accepting new assignments")
	})

	t.Run("windows too short — names the furthest one", func(t *testing.T) {
		f, ctx := newFixture(t)
		for i := 0; i < 5; i++ {
			indexGuardian(t, f, ctx, sdk.AccAddress([]byte(fmt.Sprintf("short_g_%012d", i))).String(),
				types.Guardian{
					AvailableFrom: 0, AvailableUntil: height + 100 + int64(i), AcceptingSecrets: true,
					Stake: coin(5_000_000_000), BondK: types.InitialBondK,
				})
		}

		// Ask for a reveal window ending well beyond every guardian's availability.
		msg := gateError(t, f, ctx, 3, height+50_000)
		require.Contains(t, msg, "needs guardians available through block 51000")
		require.Contains(t, msg, "furthest availability window in the network ends at block 1104",
			"the furthest window is the one fact that distinguishes 'ask for less time' from 'wait for capacity'")
		require.Contains(t, msg, "shorten the reveal window")
	})

	t.Run("available long enough but cannot afford the bond", func(t *testing.T) {
		f, ctx := newFixture(t)
		bond := types.BondAmount(1_000, types.MinBump, types.InitialBondK)
		for i := 0; i < 4; i++ {
			indexGuardian(t, f, ctx, sdk.AccAddress([]byte(fmt.Sprintf("poor_g_%013d", i))).String(),
				types.Guardian{
					AvailableFrom: 0, AvailableUntil: height + 50_000, AcceptingSecrets: true,
					Stake: coin(bond.Int64() - 1), BondK: types.InitialBondK,
				})
		}

		msg := gateError(t, f, ctx, 3, height+500)
		require.Contains(t, msg, "4 cannot afford this secret's bond")
		require.NotContains(t, msg, "shorten the reveal window",
			"availability is not the problem here, so it must not be blamed")
	})

	t.Run("available long enough but at the concurrency cap", func(t *testing.T) {
		f, ctx := newFixture(t)
		for i := 0; i < 4; i++ {
			indexGuardian(t, f, ctx, sdk.AccAddress([]byte(fmt.Sprintf("busy_g_%013d", i))).String(),
				types.Guardian{
					AvailableFrom: 0, AvailableUntil: height + 50_000, AcceptingSecrets: true,
					Stake: coin(5_000_000_000), BondK: types.InitialBondK,
					ActiveBondCount: types.MaxActiveBondsPerGuardian,
				})
		}

		msg := gateError(t, f, ctx, 3, height+500)
		require.Contains(t, msg, "at the concurrency cap")
	})
}
