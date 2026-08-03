package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// The regression guard behind the creator's declared phase-1 gas.
//
// Phase 1 enumerates guardian candidates inside MsgUserRequestGuardians, so that
// work is metered against the CREATOR's transaction — not spread over
// validators. The SDK declares a fitted model
// (typescript-sdk/src/protocol/constants.ts) with a fixed margin, so any term
// that grows with the registry eats that margin until creation aborts out of gas
// for everyone, on a purely organic trigger. Registration is permanent, so the
// registry only ever grows.
//
// That is not hypothetical: a duplicate store read per registered guardian cost
// 2,092 gas each and reached a shipped default unnoticed, because nothing pinned
// gas against registry size. At the 36-guardian devnet the model was fitted on
// it was already 53% of phase-1 handler gas, and creation would have begun
// failing at 62–79 registrations depending on band.
//
// These tests pin the SHAPE, not the absolute: a slope ceiling per registered
// guardian, and — the property the eligibility index exists to provide —
// near-zero cost for registrations that are not eligible. Asserting absolutes
// would fail on every unrelated handler change and teach the next person to
// re-baseline it, which is how the growth term survived in the first place.
//
// ⚠️ On the harness: measureGas's mock bank keeper under-reads handlers that
// move coins, and phase 1 escrows the reward pool. That affects the CONSTANT
// term only — the bank calls do not repeat per guardian — so the slope measured
// here transfers to a real chain even though the intercept does not. Absolute
// phase-1 gas is calibrated against a live devnet in testdata/vectors/tx_gas.json.

// maxGasPerRegisteredGuardian bounds the per-registration slope of phase-1 gas.
//
// Enumerating one candidate from the eligibility index costs 360 gas (measured
// both here and on a live chain — testdata/vectors/tx_gas.json); walking the
// guardian collection once (no index) cost 564. The ceiling sits above both
// and far below the 2,092 that a second read per guardian costs, so this trips
// on a reintroduced record read while tolerating ordinary drift in either shape.
const maxGasPerRegisteredGuardian = 900

// selectionGasAddr builds a distinct 20-byte account address per index.
func selectionGasAddr(prefix string, i int) sdk.AccAddress {
	return sdk.AccAddress([]byte(fmt.Sprintf("%s_guardian_%06d", prefix, i)))
}

// registerSelectionGasGuardians registers n guardians, all accepting and
// available through availableUntil, each with float enough to afford a bond.
func registerSelectionGasGuardians(t *testing.T, f *fixture, msgServer types.MsgServer, prefix string, n int, availableUntil int64) {
	t.Helper()
	deposit := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	for i := 0; i < n; i++ {
		_, err := msgServer.GuardianRegister(f.ctx, &types.MsgGuardianRegister{
			Guardian:            selectionGasAddr(prefix, i).String(),
			EncryptionPublicKey: getValidPublicKey(fmt.Sprintf("%s_key_%d", prefix, i)),
			AvailableFrom:       0,
			AvailableUntil:      availableUntil,
			Deposit:             &deposit,
			AcceptingSecrets:    true,
		})
		require.NoError(t, err)
	}
}

// measurePhase1Gas registers n eligible guardians and reports the handler gas
// one MsgUserRequestGuardians burns against that registry.
func measurePhase1Gas(t *testing.T, prefix string, n int, band int64) uint64 {
	t.Helper()
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	registerSelectionGasGuardians(t, f, msgServer, prefix, n, 100000)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockHeight(sdkCtx.BlockHeight() + 1)

	creator := sdk.AccAddress([]byte("selection_gas_creator"))
	var reqErr error
	used := measureGas(t, f, func(ctx sdk.Context) {
		_, reqErr = msgServer.UserRequestGuardians(ctx, phase1Request(creator, band))
	})
	require.NoError(t, reqErr)
	return used
}

func phase1Request(creator sdk.AccAddress, band int64) *types.MsgUserRequestGuardians {
	return &types.MsgUserRequestGuardians{
		Creator:       creator.String(),
		DetectionHint: testDetectionHint(),
		Threshold:     3,
		MinShares:     band,
		MaxShares:     band,
		RevealWindow: &types.RevealWindow{
			StartOffset: 400,
			Duration:    testRevealDuration,
		},
		Bump: types.MinBump,
	}
}

// TestPhase1GasSlopePerRegisteredGuardian measures at two registry sizes and
// divides, which cancels the constant term — so the assertion is about growth
// alone and does not move when unrelated handler work changes.
func TestPhase1GasSlopePerRegisteredGuardian(t *testing.T) {
	const (
		small = 40
		large = 240
		band  = int64(7)
	)

	smallGas := measurePhase1Gas(t, "slope_small", small, band)
	largeGas := measurePhase1Gas(t, "slope_large", large, band)

	require.Greater(t, largeGas, smallGas, "more registrations should not cost less")
	slope := float64(largeGas-smallGas) / float64(large-small)

	t.Logf("phase-1 gas: %d at %d registered, %d at %d registered → %.0f gas per registered guardian",
		smallGas, small, largeGas, large, slope)

	require.LessOrEqual(t, slope, float64(maxGasPerRegisteredGuardian),
		"phase-1 gas is growing at %.0f per registered guardian (ceiling %d). "+
			"Something in the candidate path reads per-guardian state again; find it "+
			"rather than raising the ceiling — this term is charged to every creator "+
			"and ends in out-of-gas failures as the registry grows", slope, maxGasPerRegisteredGuardian)
}
