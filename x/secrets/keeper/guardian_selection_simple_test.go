package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// removeSelectionGuardian deletes a guardian from BOTH the record store and the
// eligibility index.
//
// Production never removes a guardian — registration is permanent — so there is
// no keeper method for it, and dropping only the record would leave the index
// claiming a candidate that no longer exists. Subtests here reuse one fixture,
// so a half-removal leaks eligible candidates into the next subtest and quietly
// turns a strict-gate assertion into a pass.
func removeSelectionGuardian(t *testing.T, f *fixture, ctx sdk.Context, address string) {
	t.Helper()
	guardian, found := f.keeper.GetGuardian(ctx, address)
	require.True(t, found, "guardian %s should exist before removal", address)
	require.NoError(t, f.keeper.GuardianEligibility.Remove(ctx,
		collections.Join(guardian.AvailableUntil, guardian.Address)))
	require.NoError(t, f.keeper.Guardians.Remove(ctx, address))
}

// setSelectionGuardian writes a guardian record shaped for selection tests.
func setSelectionGuardian(t *testing.T, f *fixture, ctx sdk.Context, address string, availableUntil int64) {
	t.Helper()
	stakeCoin := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	guardian := types.Guardian{
		Address:             address,
		EncryptionPublicKey: getValidPublicKey("test_enckey_" + address),
		Stake:               &stakeCoin,
		AcceptingSecrets:    true,
		AvailableFrom:       100,
		AvailableUntil:      availableUntil,
	}
	require.NoError(t, f.keeper.SetGuardian(ctx, guardian))
}

func TestGuardianSelection_BasicFunctionality(t *testing.T) {
	f := initFixture(t)
	gs := keeper.NewGuardianSelector(&f.keeper)

	currentHeight := int64(1000)
	revealStartBlock := currentHeight + 100  // Block 1100
	revealEndBlock := revealStartBlock + 150 // Block 1250

	// Set context block height
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	ctx := sdkCtx.WithBlockHeight(currentHeight)

	t.Run("No guardians available", func(t *testing.T) {
		selected, _, _, err := gs.SelectGuardians(
			ctx,
			1, // shares
			revealEndBlock,
			351,           // distance
			types.MinBump, // bump
			1,             // secret counter
		)

		require.Error(t, err, "Should fail when no guardians available")
		require.Contains(t, err.Error(), "insufficient guardians", "Should fail due to insufficient guardians")
		require.Nil(t, selected, "Selected guardians should be nil")
	})

	t.Run("Guardian available throughout reveal window", func(t *testing.T) {
		// Create guardians available throughout reveal window
		addresses := []string{"guardian1", "guardian2", "guardian3"}
		for _, address := range addresses {
			setSelectionGuardian(t, f, ctx, address, revealEndBlock+100)
		}

		selected, bonds, seed, err := gs.SelectGuardians(
			ctx,
			2, // max_shares (select exactly 2 guardians)
			revealEndBlock,
			351,           // distance
			types.MinBump, // bump
			1,             // secret counter
		)

		require.NoError(t, err, "Guardian selection should succeed")
		require.Len(t, selected, 2, "Should select exactly max_shares guardians")
		require.Len(t, seed, 32, "Selection seed should be a SHA256 digest")
		require.Len(t, bonds, 2, "One frozen bond per selected guardian")
		for i, b := range bonds {
			require.Equal(t, types.BondAmount(351, types.MinBump, types.InitialBondK).Int64(), b,
				"fresh guardians all price at the floor k (bond %d)", i)
		}

		// Clean up
		for _, address := range addresses {
			removeSelectionGuardian(t, f, ctx, address)
		}
	})

	t.Run("Guardian expires during reveal window", func(t *testing.T) {
		// Create guardian that expires before the reveal window closes
		setSelectionGuardian(t, f, ctx, "guardian2", revealEndBlock-50)

		selected, _, _, err := gs.SelectGuardians(
			ctx,
			1, // shares
			revealEndBlock,
			351,           // distance
			types.MinBump, // bump
			1,             // secret counter
		)

		require.Error(t, err, "Guardian selection should fail")
		require.Contains(t, err.Error(), "insufficient guardians", "Should fail due to guardian expiring during reveal")
		require.Nil(t, selected, "Selected guardians should be nil on error")

		// Clean up
		removeSelectionGuardian(t, f, ctx, "guardian2")
	})

	t.Run("Strict gate: fewer eligible than max_shares fails", func(t *testing.T) {
		// max_shares = 6 needs 6 eligible candidates; provide only 5.
		// The gate is strict — there is no reduced-band fallback.
		addresses := make([]string, 5)
		for i := range addresses {
			addresses[i] = sdk.AccAddress([]byte{byte(i + 1), 'g', 'a', 't', 'e'}).String()
			setSelectionGuardian(t, f, ctx, addresses[i], revealEndBlock+100)
		}

		selected, _, _, err := gs.SelectGuardians(ctx, 6, revealEndBlock, 351, types.MinBump, 1)
		require.Error(t, err, "Selection must fail with fewer than max_shares eligible")
		require.Contains(t, err.Error(), "insufficient guardians")
		require.Nil(t, selected)

		for _, address := range addresses {
			removeSelectionGuardian(t, f, ctx, address)
		}
	})
}

func TestGuardianSelection_SelectedGuardianCount(t *testing.T) {
	f := initFixture(t)
	gs := keeper.NewGuardianSelector(&f.keeper)

	currentHeight := int64(1000)
	revealStartBlock := currentHeight + 100
	revealEndBlock := revealStartBlock + 150

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	ctx := sdkCtx.WithBlockHeight(currentHeight)

	// Create multiple guardians (more than max_shares = 6)
	guardians := make([]string, 12)
	for i := 0; i < 12; i++ {
		guardians[i] = sdk.AccAddress([]byte("guardian" + string(rune(i+65)))).String() // A, B, C, etc.
		setSelectionGuardian(t, f, ctx, guardians[i], revealEndBlock+100)
	}

	// Test: Select exactly max_shares = 6 guardians
	selected, _, _, err := gs.SelectGuardians(
		ctx,
		6, // max_shares
		revealEndBlock,
		351,           // distance
		types.MinBump, // bump
		42,            // secret counter
	)

	require.NoError(t, err, "Guardian selection should succeed")
	require.Len(t, selected, 6, "Should select exactly max_shares guardians")

	// Verify all guardians are unique
	guardianAddresses := make(map[string]bool)
	for _, address := range selected {
		require.False(t, guardianAddresses[address],
			"Guardian %s should not appear multiple times", address)
		guardianAddresses[address] = true
	}

	// Clean up guardians
	for _, address := range guardians {
		removeSelectionGuardian(t, f, ctx, address)
	}
}

func TestGuardianSelection_DeterministicBehavior(t *testing.T) {
	f := initFixture(t)
	gs := keeper.NewGuardianSelector(&f.keeper)

	currentHeight := int64(1000)
	revealStartBlock := currentHeight + 100
	revealEndBlock := revealStartBlock + 150

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	ctx := sdkCtx.WithBlockHeight(currentHeight)

	// Create 6 guardians (more than max_shares = 2)
	for i := 0; i < 6; i++ {
		address := sdk.AccAddress([]byte("guardian" + string(rune(i+65)))).String()
		setSelectionGuardian(t, f, ctx, address, revealEndBlock+100)
	}

	// Run selection multiple times with the same inputs — same block context
	// and same secret counter must reproduce the same selection on every
	// validator (consensus determinism)
	var firstSelection []string
	for i := 0; i < 3; i++ {
		selected, _, _, err := gs.SelectGuardians(
			ctx,
			2, // max_shares (select exactly 2 guardians)
			revealEndBlock,
			351,           // distance
			types.MinBump, // bump
			7,             // same secret counter every time
		)

		require.NoError(t, err, "Selection should succeed on iteration %d", i)
		require.Len(t, selected, 2, "Should select exactly max_shares guardians on iteration %d", i)

		if i == 0 {
			firstSelection = selected
		} else {
			require.Equal(t, firstSelection, selected,
				"Guardian selection must be deterministic for identical inputs (iteration %d)", i)
		}
	}

	// A different secret counter re-seeds the sortition: two secrets in the
	// same block draw independent selections
	otherCounter, _, _, err := gs.SelectGuardians(ctx, 2, revealEndBlock, 351, types.MinBump, 8)
	require.NoError(t, err)
	require.NotEqual(t, firstSelection, otherCounter,
		"A different secret counter should (with 6 candidates) yield a different selection")

	// Clean up - remove the 6 guardians we created
	for i := 0; i < 6; i++ {
		address := sdk.AccAddress([]byte("guardian" + string(rune(i+65)))).String()
		removeSelectionGuardian(t, f, ctx, address)
	}
}
