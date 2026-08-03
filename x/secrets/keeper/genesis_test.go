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

// TestGenesis_SecretCounterRoundTrip proves the secret counter is consensus
// state that survives export/import: a chain restarted from exported genesis
// continues the sequence — IDs never reissue and selection seeds never repeat.
func TestGenesis_SecretCounterRoundTrip(t *testing.T) {
	f := initFixture(t)

	// Consume a few counter values on the source chain
	var lastId string
	var lastCounter uint64
	for i := 0; i < 5; i++ {
		id, counter, err := f.keeper.NextSecretId(f.ctx)
		require.NoError(t, err)
		lastId, lastCounter = id, counter
	}
	require.Equal(t, uint64(4), lastCounter)

	// Export carries the next-to-assign value
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(5), exported.SecretCounter)

	// Import into a fresh chain: the sequence continues, it does not reset
	f2 := initFixture(t)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, exported))

	id, counter, err := f2.keeper.NextSecretId(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, lastCounter+1, counter, "imported chain must continue the sequence")
	require.NotEqual(t, lastId, id, "IDs must never reissue after import")

	// A second export reflects the continued sequence
	reExported, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(6), reExported.SecretCounter)
}

// validGenesisSecret builds a structurally valid genesis secret fixture for
// the Validate tests below.
func validGenesisSecret() *types.Secret {
	return &types.Secret{
		Id:               types.GenerateValidSecretID(),
		Creator:          "tmflr1creator",
		State:            types.SECRET_STATUS_RESERVED,
		Threshold:        2,
		MinShares:        3,
		MaxShares:        3,
		RevealStartBlock: 100,
		RevealEndBlock:   200,
		RewardPool:       sdk.NewInt64Coin(types.DefaultDenom, 1_000_000),
	}
}

// TestGenesisState_Validate_SecretCounter pins the structural rule: every
// stored secret consumed a counter value, so the counter can never be below
// the stored secret count.
func TestGenesisState_Validate_SecretCounter(t *testing.T) {
	secret := validGenesisSecret()

	valid := types.GenesisState{Secrets: []*types.Secret{secret}, SecretCounter: 1}
	require.NoError(t, valid.Validate())

	inconsistent := types.GenesisState{Secrets: []*types.Secret{secret}, SecretCounter: 0}
	err := inconsistent.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret counter")
}

// TestGenesisState_Validate_Structural pins the structural checks: legal FSM
// state, threshold ≤ shares, well-formed coins, side-store referential
// integrity, denormalised-counter consistency, retention consequences for
// terminal secrets, and tombstone/live-record exclusivity.
func TestGenesisState_Validate_Structural(t *testing.T) {
	guardianAddr := "tmflr1validguardian"
	base := func() (*types.Secret, types.GenesisState) {
		secret := validGenesisSecret()
		return secret, types.GenesisState{
			Secrets: []*types.Secret{secret},
			Guardians: []*types.Guardian{{
				Address:             guardianAddr,
				EncryptionPublicKey: getValidPublicKey("genesis_validate_key"),
				AvailableFrom:       0,
				AvailableUntil:      1000,
				BondK:               types.InitialBondK,
			}},
			SecretCounter: 1,
		}
	}

	t.Run("valid baseline", func(t *testing.T) {
		_, gs := base()
		require.NoError(t, gs.Validate())
	})

	t.Run("illegal FSM state", func(t *testing.T) {
		secret, gs := base()
		secret.State = "exploded"
		require.ErrorContains(t, gs.Validate(), "invalid state")
	})

	t.Run("min_shares below threshold", func(t *testing.T) {
		secret, gs := base()
		secret.MinShares = secret.Threshold - 1
		require.ErrorContains(t, gs.Validate(), "below threshold")
	})

	t.Run("malformed reward pool coin", func(t *testing.T) {
		secret, gs := base()
		secret.RewardPool = sdk.Coin{}
		require.ErrorContains(t, gs.Validate(), "reward pool")
	})

	t.Run("frozen bond count must match the selection", func(t *testing.T) {
		secret, gs := base()
		secret.SelectedGuardians = []string{guardianAddr}
		secret.GuardianBondAmounts = nil // one guardian, zero bonds
		require.ErrorContains(t, gs.Validate(), "one frozen bond per selected guardian")
	})

	t.Run("non-positive frozen bond", func(t *testing.T) {
		secret, gs := base()
		secret.SelectedGuardians = []string{guardianAddr}
		secret.GuardianBondAmounts = []int64{-5}
		require.ErrorContains(t, gs.Validate(), "must be positive")
	})

	t.Run("guardian bond multiplier outside the hard range", func(t *testing.T) {
		_, gs := base()
		gs.Guardians[0].BondK = types.MaxBondK + 1
		require.ErrorContains(t, gs.Validate(), "bond multiplier")
	})

	t.Run("share record referencing a missing secret", func(t *testing.T) {
		_, gs := base()
		gs.SecretShares = []types.StoredShare{{SecretId: "no-such-secret", GuardianAddress: guardianAddr}}
		require.ErrorContains(t, gs.Validate(), "nonexistent secret")
	})

	t.Run("assignment record referencing a missing guardian", func(t *testing.T) {
		secret, gs := base()
		gs.SecretAssignments = []types.StoredAssignment{{SecretId: secret.Id, GuardianAddress: "tmflr1ghost"}}
		require.ErrorContains(t, gs.Validate(), "nonexistent guardian")
	})

	t.Run("duplicate share record", func(t *testing.T) {
		secret, gs := base()
		entry := types.StoredShare{SecretId: secret.Id, GuardianAddress: guardianAddr}
		gs.SecretShares = []types.StoredShare{entry, entry}
		require.ErrorContains(t, gs.Validate(), "duplicate share record")
	})

	t.Run("accepted_count drifted from the assignment store", func(t *testing.T) {
		secret, gs := base()
		gs.SecretAssignments = []types.StoredAssignment{{
			SecretId:        secret.Id,
			GuardianAddress: guardianAddr,
			Record:          types.AssignmentRecord{Status: types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED},
		}}
		// secret.AcceptedCount stays 0 while the store holds one ACCEPTED record
		require.ErrorContains(t, gs.Validate(), "accepted_count")
	})

	t.Run("revealed_count drifted from the reveal store", func(t *testing.T) {
		secret, gs := base()
		gs.SecretReveals = []types.StoredReveal{{
			SecretId: secret.Id,
			Reveal:   types.RevealedShare{GuardianAddress: guardianAddr},
		}}
		require.ErrorContains(t, gs.Validate(), "revealed_count")
	})

	t.Run("terminal secret with lingering share records", func(t *testing.T) {
		secret, gs := base()
		secret.State = types.SECRET_STATUS_REVEALED
		gs.SecretShares = []types.StoredShare{{SecretId: secret.Id, GuardianAddress: guardianAddr}}
		require.ErrorContains(t, gs.Validate(), "deleted at the terminal transition")
	})

	t.Run("tombstone colliding with a live secret", func(t *testing.T) {
		secret, gs := base()
		gs.SecretTombstones = []types.StoredTombstone{{SecretId: secret.Id}}
		require.ErrorContains(t, gs.Validate(), "collides with a live secret")
	})
}

// TestGenesis_RoundTripEquality drives real state through the message
// handlers across three lifecycle stages (reserved, awaiting acceptance with
// a locked bond, pending), then proves export → import → export is the
// identity — and that real exported state passes both the structural
// Validate and the import-time integrity sweep (InitGenesis would error
// otherwise).
func TestGenesis_RoundTripEquality(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	// Secret B: activated to pending through real confirmations plus the
	// deadline finalisation. B is created FIRST and finalised before A and C
	// exist — finalisation drains every due commit entry, and A/C must stay
	// in their pre-activation states for the round-trip fixture.
	for i := 0; i < 4; i++ {
		registerConformanceGuardian(t, f, srv, fmt.Sprintf("genesis_b_g%02d", i), testFloatUnit())
	}
	setHeight(f, height(f)+1)
	secretBId, err := requestConformanceSecret(t, f, srv, types.MinBump, 2, 3, 3, 400, testRevealDuration)
	require.NoError(t, err)
	secretB, err := f.keeper.GetSecret(f.ctx, secretBId)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		require.NoError(t, acceptAs(t, f, srv, secretBId, secretB.SelectedGuardians[i]))
	}
	finaliseCommitDeadline(t, f, secretBId)
	secretB, err = f.keeper.GetSecret(f.ctx, secretBId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_PENDING, secretB.State)

	// Secret A: full Phases 1+2 (awaiting acceptance), one bond locked
	secretAId := setupBondTestSecret(t, f, srv, types.MinBump, 2, 3)
	secretA, err := f.keeper.GetSecret(f.ctx, secretAId)
	require.NoError(t, err)
	require.NoError(t, acceptAs(t, f, srv, secretAId, secretA.SelectedGuardians[0]))

	// Secret C: Phase 1 only (reserved — no payload yet)
	creator := sdk.AccAddress([]byte("creator_address")).String()
	_, err = srv.UserRequestGuardians(f.ctx, &types.MsgUserRequestGuardians{
		Creator:       creator,
		DetectionHint: testDetectionHint(),
		Threshold:     2,
		MinShares:     3,
		MaxShares:     3,
		RevealWindow:  &types.RevealWindow{StartOffset: 400, Duration: testRevealDuration},
		Bump:          types.MinBump,
	})
	require.NoError(t, err)

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate(), "real exported state must pass the structural Validate")

	// Import into a fresh chain — InitGenesis runs the integrity sweep
	f2 := initFixture(t)
	f2.ctx = sdk.UnwrapSDKContext(f2.ctx).WithBlockHeight(sdk.UnwrapSDKContext(f.ctx).BlockHeight())
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, exported))
	assertInvariants(t, f2)

	// Re-export: byte-for-byte the same state
	reExported, err := f2.keeper.ExportGenesis(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, reExported, "export → import → export must be the identity")
}

// TestGenesis_ImportSweepHaltsOnInconsistentState proves the ruled hard-halt:
// a genesis whose records are individually well-formed but mutually
// inconsistent (a locked float no live assignment accounts for) is rejected
// by the import-time sweep — it must never produce blocks.
func TestGenesis_ImportSweepHaltsOnInconsistentState(t *testing.T) {
	f := initFixture(t)

	total := sdk.NewCoin(types.DefaultDenom, math.NewInt(1_000_000))
	locked := sdk.NewCoin(types.DefaultDenom, math.NewInt(500_000)) // stranded: no secret backs it
	err := f.keeper.InitGenesis(f.ctx, types.GenesisState{
		Guardians: []*types.Guardian{{
			Address:             "tmflr1strandedbond",
			EncryptionPublicKey: getValidPublicKey("stranded_key"),
			AvailableFrom:       0,
			AvailableUntil:      1000,
			Stake:               &total,
			LockedStake:         &locked,
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "state-integrity sweep")
	require.Contains(t, err.Error(), "invariant 3")
}
