package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Guardian key rotation conformance (docs/spec.md "Guardian Key Rotation",
// docs/planning/PENDING_GUARDIAN_KEY_ROTATION_PLAN.md §9): forward-only
// epochs, the effective-next-block rule, the minimum interval, global
// permanent key uniqueness, the burned rotation fee, old-epoch slashing
// evidence, and genesis round-trips with multi-epoch histories.

// registerRotationGuardian registers a guardian with a year-scale
// availability window, so fixtures can advance past the 432,000-block
// rotation interval and still select it.
func registerRotationGuardian(t *testing.T, f *fixture, msgServer types.MsgServer, name string, deposit math.Int) string {
	t.Helper()
	addr := sdk.AccAddress([]byte(fmt.Sprintf("%-20s", name))).String()
	dep := sdk.NewCoin(types.DefaultDenom, deposit)
	_, err := msgServer.GuardianRegister(f.ctx, &types.MsgGuardianRegister{
		Guardian:            addr,
		EncryptionPublicKey: getValidPublicKey(name + "_enckey"),
		AvailableFrom:       0,
		AvailableUntil:      5_000_000,
		Deposit:             &dep,
		AcceptingSecrets:    true,
	})
	require.NoError(t, err)
	return addr
}

// rotateAs submits a MsgGuardianRotateKey for the guardian.
func rotateAs(f *fixture, msgServer types.MsgServer, guardian string, newKey []byte) (*types.MsgGuardianRotateKeyResponse, error) {
	return msgServer.GuardianRotateKey(f.ctx, &types.MsgGuardianRotateKey{
		Guardian: guardian,
		NewKey:   newKey,
	})
}

// firstRotationHeight is the first height at which a guardian registered at
// registrationHeight may rotate: epoch 0 becomes effective at registration
// height + 1 and starts the interval clock.
func firstRotationHeight(registrationHeight int64) int64 {
	return registrationHeight + 1 + types.KeyRotationMinIntervalBlocks
}

func TestGuardianRotateKey_HappyPathAndFeeBurn(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	g := registerRotationGuardian(t, f, msgServer, "rot_happy", testFloatUnit())
	oldKey := getValidPublicKey("rot_happy_enckey")
	newKey := getValidPublicKey("rot_happy_next")

	rotateHeight := firstRotationHeight(100)
	setHeight(f, rotateHeight)
	burntBefore := *bank.burnt

	resp, err := rotateAs(f, msgServer, g, newKey)
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp.NewEpoch)
	require.Equal(t, rotateHeight+1, resp.EffectiveFromHeight, "rotation must take effect from the NEXT block")

	// Record advanced: current key + epoch pointer
	guardian, found := f.keeper.GetGuardian(f.ctx, g)
	require.True(t, found)
	require.Equal(t, newKey, guardian.EncryptionPublicKey)
	require.Equal(t, uint64(1), guardian.CurrentKeyEpoch)

	// History is append-only and complete: epoch 0 (registration, effective
	// at registration height + 1) and epoch 1
	epoch0, _, err := f.keeper.GuardianEpochInForce(f.ctx, g, rotateHeight)
	require.NoError(t, err)
	require.Equal(t, uint64(0), epoch0, "the old epoch stays in force through the rotation block")
	epoch1, entry1, err := f.keeper.GuardianEpochInForce(f.ctx, g, rotateHeight+1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), epoch1)
	require.Equal(t, newKey, entry1.PublicKey)

	// Both keys stay reserved forever
	taken, holder := f.keeper.KeyEverRegistered(f.ctx, oldKey)
	require.True(t, taken)
	require.Equal(t, g, holder)
	taken, _ = f.keeper.KeyEverRegistered(f.ctx, newKey)
	require.True(t, taken)

	// The flat rotation fee was burned — exactly rate × 14,400, nothing else
	require.True(t, types.KeyRotationFee().Equal(bank.burnt.Sub(burntBefore)),
		"rotation must burn exactly the flat fee (rate × KeyRotationFeeBlocks)")

	// Event emitted with the epoch and effectiveness attributes
	var rotatedEvent *sdk.Event
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type == types.EventGuardianKeyRotated {
			rotatedEvent = &ev
			break
		}
	}
	require.NotNil(t, rotatedEvent, "guardian_key_rotated event must be emitted")

	assertInvariants(t, f)
}

func TestGuardianRotateKey_IntervalBoundary(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	g := registerRotationGuardian(t, f, msgServer, "rot_boundary", testFloatUnit())

	// One block early: elapsed = 431,999 — rejected, and the interval applies
	// to the FIRST rotation after registration (epoch 0 starts the clock)
	setHeight(f, firstRotationHeight(100)-1)
	_, err := rotateAs(f, msgServer, g, getValidPublicKey("rot_boundary_early"))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRotationTooSoon)

	// Exactly at the boundary: elapsed = 432,000 — accepted
	setHeight(f, firstRotationHeight(100))
	_, err = rotateAs(f, msgServer, g, getValidPublicKey("rot_boundary_ontime"))
	require.NoError(t, err)

	// A second rotation immediately after is rate-limited off the NEW
	// epoch's effective height
	setHeight(f, height(f)+1)
	_, err = rotateAs(f, msgServer, g, getValidPublicKey("rot_boundary_again"))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrRotationTooSoon)

	assertInvariants(t, f)
}

func TestGuardianRotateKey_GlobalUniquenessForever(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	g1 := registerRotationGuardian(t, f, msgServer, "rot_uniq_a", testFloatUnit())
	g2 := registerRotationGuardian(t, f, msgServer, "rot_uniq_b", testFloatUnit())
	g1Key := getValidPublicKey("rot_uniq_a_enckey")
	g2Key := getValidPublicKey("rot_uniq_b_enckey")

	setHeight(f, firstRotationHeight(100))

	// Rotating to ANOTHER guardian's live key is rejected
	_, err := rotateAs(f, msgServer, g1, g2Key)
	require.ErrorIs(t, err, types.ErrKeyAlreadyRegistered)

	// Rotating to one's OWN current key is rejected (it is registered too)
	_, err = rotateAs(f, msgServer, g1, g1Key)
	require.ErrorIs(t, err, types.ErrKeyAlreadyRegistered)

	// Rotate g1 properly, retiring g1Key
	_, err = rotateAs(f, msgServer, g1, getValidPublicKey("rot_uniq_a_next"))
	require.NoError(t, err)

	// The RETIRED key stays reserved forever: g2 cannot rotate to it…
	_, err = rotateAs(f, msgServer, g2, g1Key)
	require.ErrorIs(t, err, types.ErrKeyAlreadyRegistered)

	// …and no new guardian can register with it
	dep := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	newcomer := sdk.AccAddress([]byte("rot_uniq_newcomer___")).String()
	_, err = msgServer.GuardianRegister(f.ctx, &types.MsgGuardianRegister{
		Guardian:            newcomer,
		EncryptionPublicKey: g1Key,
		AvailableFrom:       0,
		AvailableUntil:      100000,
		Deposit:             &dep,
		AcceptingSecrets:    true,
	})
	require.ErrorIs(t, err, types.ErrKeyAlreadyRegistered)

	assertInvariants(t, f)
}

func TestGuardianRotateKey_RejectsUnknownGuardianAndZeroKey(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	// Unregistered guardian
	stranger := sdk.AccAddress([]byte("rot_stranger________")).String()
	_, err := rotateAs(f, msgServer, stranger, getValidPublicKey("rot_stranger_key"))
	require.ErrorIs(t, err, types.ErrGuardianNotFound)

	// All-zero key fails format validation: it is the degenerate X25519
	// small-order point, so an exchange against it yields an all-zero shared
	// secret and any key derived from it is publicly computable.
	g := registerRotationGuardian(t, f, msgServer, "rot_zero", testFloatUnit())
	setHeight(f, firstRotationHeight(100))
	_, err = rotateAs(f, msgServer, g, make([]byte, types.PublicKeyLength))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a usable X25519 public key")
}

// TestKeyRotation_SameBlockSelectionHandsPreRotationKey pins the
// effective-next-block rule end to end: a rotation and a selection landing in
// the SAME block (rotation ordered first, so the record already holds the new
// key) still hand the creator the pre-rotation key, and the derivation
// agrees; from the next block, selections hand the new key.
func TestKeyRotation_SameBlockSelectionHandsPreRotationKey(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	var guardians []string
	for i := range 3 {
		guardians = append(guardians, registerRotationGuardian(t, f, msgServer, fmt.Sprintf("rot_blk_%02d", i), testFloatUnit()))
	}
	rotator := guardians[0]
	oldKey := getValidPublicKey("rot_blk_00_enckey")
	newKey := getValidPublicKey("rot_blk_00_next")

	rotateHeight := firstRotationHeight(100)
	setHeight(f, rotateHeight)

	// Rotation executes FIRST in the block — the record now holds the new key
	_, err := rotateAs(f, msgServer, rotator, newKey)
	require.NoError(t, err)

	requestAll := func() *types.MsgUserRequestGuardiansResponse {
		resp, err := msgServer.UserRequestGuardians(f.ctx, &types.MsgUserRequestGuardians{
			Creator:       sdk.AccAddress([]byte("creator_address")).String(),
			DetectionHint: testDetectionHint(),
			Threshold:     2,
			MinShares:     3,
			MaxShares:     3, // selects all three — the rotator is guaranteed in
			RevealWindow:  &types.RevealWindow{StartOffset: 400, Duration: testRevealDuration},
			Bump:          types.MinBump,
		})
		require.NoError(t, err)
		return resp
	}
	handedKey := func(resp *types.MsgUserRequestGuardiansResponse, addr string) []byte {
		for _, info := range resp.GuardianAssignments {
			if info.Address == addr {
				return info.PublicKey
			}
		}
		t.Fatalf("guardian %s not in the Phase-1 response", addr)
		return nil
	}

	// Same block: the creator is handed the PRE-rotation key…
	sameBlock := requestAll()
	require.Equal(t, oldKey, handedKey(sameBlock, rotator),
		"a same-block selection must hand the creator the pre-rotation key")
	// …and the derivation agrees (the secret's creation height = this height)
	epoch, entry, err := f.keeper.GuardianEpochInForce(f.ctx, rotator, rotateHeight)
	require.NoError(t, err)
	require.Equal(t, uint64(0), epoch)
	require.Equal(t, oldKey, entry.PublicKey)

	// Next block: selections hand the NEW key, and the derivation agrees
	setHeight(f, rotateHeight+1)
	nextBlock := requestAll()
	require.Equal(t, newKey, handedKey(nextBlock, rotator),
		"selections after the rotation block must hand the new key")
	epoch, entry, err = f.keeper.GuardianEpochInForce(f.ctx, rotator, rotateHeight+1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), epoch)
	require.Equal(t, newKey, entry.PublicKey)

	assertInvariants(t, f)
}

// TestKeyRotation_LifecycleSpansRotation drives a full lifecycle across a
// rotation: Phase 1 → rotate (mid-commit — a non-event on-chain) → Phase 2/3
// → reveal → settle, all on the old epoch's binding; the next secret selects
// under the new epoch.
func TestKeyRotation_LifecycleSpansRotation(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	var guardians []string
	for i := range 3 {
		guardians = append(guardians, registerRotationGuardian(t, f, msgServer, fmt.Sprintf("rot_life_%02d", i), testFloatUnit()))
	}
	rotator := guardians[0]

	createHeight := firstRotationHeight(100)
	setHeight(f, createHeight)

	// Phase 1 + 2 (request + distribute), selected under epoch 0
	secretId, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 3, 3, 400, testRevealDuration)
	require.NoError(t, err)

	// Rotation lands mid-commit — no freeze, no per-secret pin, nothing to
	// defend (the chain never validates ciphertext against a key)
	setHeight(f, createHeight+1)
	_, err = rotateAs(f, msgServer, rotator, getValidPublicKey("rot_life_00_next"))
	require.NoError(t, err)

	// Phase 3: everyone accepts, including the rotated guardian
	for _, g := range guardians {
		require.NoError(t, acceptAs(t, f, msgServer, secretId, g))
	}
	finaliseCommitDeadline(t, f, secretId)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_PENDING, secret.State)

	// Reveal window: all reveal (HMACs are epoch-agnostic)
	setHeight(f, secret.RevealStartBlock)
	for _, g := range guardians {
		require.NoError(t, conformanceReveal(t, f, msgServer, secretId, g))
	}

	// Settle at end + 1 — bonds released, pool paid, state terminal
	setHeight(f, secret.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))
	settled, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_REVEALED, settled.State)
	rotated, _ := f.keeper.GetGuardian(f.ctx, rotator)
	require.True(t, rotated.LockedStake.IsZero(), "the rotated guardian's bond must settle normally")

	// The next secret selects under the NEW epoch
	resp, err := msgServer.UserRequestGuardians(f.ctx, &types.MsgUserRequestGuardians{
		Creator:       sdk.AccAddress([]byte("creator_address")).String(),
		DetectionHint: testDetectionHint(),
		Threshold:     2,
		MinShares:     3,
		MaxShares:     3,
		RevealWindow:  &types.RevealWindow{StartOffset: 400, Duration: testRevealDuration},
		Bump:          types.MinBump,
	})
	require.NoError(t, err)
	for _, info := range resp.GuardianAssignments {
		if info.Address == rotator {
			require.Equal(t, getValidPublicKey("rot_life_00_next"), info.PublicKey,
				"post-rotation selections must hand the new epoch's key")
		}
	}

	assertInvariants(t, f)
}

// TestKeyRotation_EarlyRevealEvidenceSurvivesRotation proves slashing
// evidence against an old-epoch assignment still verifies and slashes after
// the guardian rotates — HMACs derive from (secretID, guardianAddress), not
// the encryption key.
func TestKeyRotation_EarlyRevealEvidenceSurvivesRotation(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	var guardians []string
	for i := range 3 {
		guardians = append(guardians, registerRotationGuardian(t, f, msgServer, fmt.Sprintf("rot_slash_%02d", i), testFloatUnit()))
	}
	leaker := guardians[0]

	createHeight := firstRotationHeight(100)
	setHeight(f, createHeight)
	secretId, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 3, 3, 400, testRevealDuration)
	require.NoError(t, err)

	for _, g := range guardians {
		require.NoError(t, acceptAs(t, f, msgServer, secretId, g))
	}
	finaliseCommitDeadline(t, f, secretId)

	// The guardian rotates AFTER accepting under epoch 0
	_, err = rotateAs(f, msgServer, leaker, getValidPublicKey("rot_slash_00_next"))
	require.NoError(t, err)

	// Early-reveal evidence (the share whose HMAC is stored on-chain) is
	// reported before the reveal window opens — it must verify and slash the
	// FULL bond despite the rotation
	before, _ := f.keeper.GetGuardian(f.ctx, leaker)
	require.False(t, before.LockedStake.IsZero())
	bond := before.LockedStake.Amount

	reporter := sdk.AccAddress([]byte("rot_slash_reporter__")).String()
	_, err = msgServer.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: leaker,
		Evidence:        testShareBytes(secretId, leaker),
		ReporterAddress: reporter,
		Reason:          "early reveal after rotation",
		SecretId:        secretId,
	})
	require.NoError(t, err, "old-epoch evidence must verify after rotation")

	after, _ := f.keeper.GetGuardian(f.ctx, leaker)
	require.True(t, after.LockedStake.IsZero(), "the full bond must be slashed")
	require.True(t, before.Stake.Amount.Sub(bond).Equal(after.Stake.Amount),
		"the whole bond leaves the float on an early-reveal slash")
	require.True(t, f.keeper.IsGuardianSlashedForEarlyReveal(f.ctx, leaker, secretId))
}

func TestGenesis_RoundTripWithMultiEpochHistories(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	registerRotationGuardian(t, f, msgServer, "rot_gen_static", testFloatUnit())
	rotator := registerRotationGuardian(t, f, msgServer, "rot_gen_rotator", testFloatUnit())

	setHeight(f, firstRotationHeight(100))
	_, err := rotateAs(f, msgServer, rotator, getValidPublicKey("rot_gen_rotator_next"))
	require.NoError(t, err)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.GuardianKeyHistories, 3, "epoch 0 × 2 guardians + epoch 1")

	// Import into a fresh fixture (InitGenesis runs the invariant sweep,
	// including key-history integrity and the rebuilt uniqueness index)
	f2 := initFixture(t)
	setHeight(f2, height(f))
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, exported))

	reExported, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, exported.GuardianKeyHistories, reExported.GuardianKeyHistories)
	require.ElementsMatch(t, exported.Guardians, reExported.Guardians)

	// Derivation works identically on the imported state
	epoch, _, err := f2.keeper.GuardianEpochInForce(f2.ctx, rotator, height(f))
	require.NoError(t, err)
	require.Equal(t, uint64(0), epoch, "rotation effective next block — old epoch in force at the rotation height")
	epoch, _, err = f2.keeper.GuardianEpochInForce(f2.ctx, rotator, height(f)+1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), epoch)
}

// TestGenesis_SynthesisesEpochZero pins the migration path: a genesis whose
// guardians carry no key-history entries (pre-rotation state) imports with
// epoch 0 synthesised from each record, and the uniqueness index rebuilt.
func TestGenesis_SynthesisesEpochZero(t *testing.T) {
	f := initFixture(t)
	setHeight(f, 10)

	key := getValidPublicKey("rot_synth_enckey")
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	locked := sdk.NewCoin(types.DefaultDenom, math.ZeroInt())
	genState := types.GenesisState{
		Guardians: []*types.Guardian{{
			Address:             sdk.AccAddress([]byte("rot_synth_guardian__")).String(),
			EncryptionPublicKey: key,
			AvailableFrom:       1,
			AvailableUntil:      100000,
			Stake:               &stake,
			LockedStake:         &locked,
			AcceptingSecrets:    true,
			BondK:               types.InitialBondK,
			ActiveBondCount:     0,
		}},
	}
	require.NoError(t, genState.Validate())
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genState))

	addr := genState.Guardians[0].Address
	epoch, entry, err := f.keeper.GuardianEpochInForce(f.ctx, addr, 10)
	require.NoError(t, err)
	require.Equal(t, uint64(0), epoch)
	require.Equal(t, key, entry.PublicKey)
	taken, holder := f.keeper.KeyEverRegistered(f.ctx, key)
	require.True(t, taken)
	require.Equal(t, addr, holder)
}

// TestGenesisValidate_KeyHistoryShapes rejects malformed key-history genesis
// states before they can assemble a store the runtime could never have
// produced.
func TestGenesisValidate_KeyHistoryShapes(t *testing.T) {
	guardian := func(name string, epoch uint64, key []byte) *types.Guardian {
		stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
		locked := sdk.NewCoin(types.DefaultDenom, math.ZeroInt())
		return &types.Guardian{
			Address:             sdk.AccAddress([]byte(fmt.Sprintf("%-20s", name))).String(),
			EncryptionPublicKey: key,
			CurrentKeyEpoch:     epoch,
			AvailableFrom:       1,
			AvailableUntil:      100000,
			Stake:               &stake,
			LockedStake:         &locked,
			BondK:               types.InitialBondK,
		}
	}
	entry := func(addr string, epoch uint64, key []byte, effective int64) types.StoredKeyHistoryEntry {
		return types.StoredKeyHistoryEntry{
			GuardianAddress: addr,
			Epoch:           epoch,
			Entry:           types.KeyHistoryEntry{PublicKey: key, EffectiveFromHeight: effective},
		}
	}
	keyA := getValidPublicKey("rot_val_a")
	keyB := getValidPublicKey("rot_val_b")

	cases := []struct {
		name    string
		mutate  func(gs *types.GenesisState)
		wantErr string
	}{
		{
			name: "current epoch above history",
			mutate: func(gs *types.GenesisState) {
				gs.Guardians[0].CurrentKeyEpoch = 1 // only epoch 0 exists
			},
			wantErr: "missing epoch 1",
		},
		{
			name: "no history but nonzero current epoch",
			mutate: func(gs *types.GenesisState) {
				gs.GuardianKeyHistories = nil
				gs.Guardians[0].CurrentKeyEpoch = 2
			},
			wantErr: "no key history entries",
		},
		{
			name: "effective heights must increase",
			mutate: func(gs *types.GenesisState) {
				gs.Guardians[0].CurrentKeyEpoch = 1
				gs.Guardians[0].EncryptionPublicKey = keyB
				gs.GuardianKeyHistories = append(gs.GuardianKeyHistories,
					entry(gs.Guardians[0].Address, 1, keyB, 2)) // not above epoch 0's 2
			},
			wantErr: "does not increase",
		},
		{
			name: "keys globally unique across histories",
			mutate: func(gs *types.GenesisState) {
				gs.Guardians[0].CurrentKeyEpoch = 1
				gs.Guardians[0].EncryptionPublicKey = keyB
				gs.GuardianKeyHistories = append(gs.GuardianKeyHistories,
					entry(gs.Guardians[0].Address, 1, keyB, 10),
					entry(gs.Guardians[0].Address, 2, keyA, 20)) // reuses epoch 0's key
				gs.Guardians[0].CurrentKeyEpoch = 2
				gs.Guardians[0].EncryptionPublicKey = keyA
			},
			wantErr: "reuses a key",
		},
		{
			name: "record key must match current epoch",
			mutate: func(gs *types.GenesisState) {
				gs.Guardians[0].EncryptionPublicKey = keyB // history holds keyA
			},
			wantErr: "does not match its current key epoch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := guardian("rot_val_guardian", 0, keyA)
			gs := types.GenesisState{
				Guardians:            []*types.Guardian{g},
				GuardianKeyHistories: []types.StoredKeyHistoryEntry{entry(g.Address, 0, keyA, 2)},
			}
			tc.mutate(&gs)
			err := gs.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
