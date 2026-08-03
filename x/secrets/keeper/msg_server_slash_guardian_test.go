package keeper_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// computeEvidenceHMAC reproduces the production HMAC so tests can store a
// commitment that the submitted evidence verifies against.
func computeEvidenceHMAC(secretId, guardianAddr string, evidence []byte) []byte {
	keyHash := sha256.New()
	keyHash.Write([]byte(types.ModuleName))
	keyHash.Write([]byte(secretId))
	keyHash.Write([]byte(guardianAddr))
	keyHash.Write([]byte("hmac_salt"))
	hmacKey := keyHash.Sum(nil)

	h := hmac.New(sha256.New, hmacKey)
	h.Write(evidence)
	h.Write([]byte(guardianAddr))
	h.Write([]byte(secretId))
	return h.Sum(nil)
}

// setupSlashableGuardian stores a guardian with `bond` locked (as if it had
// accepted a secret — active-bond count 1, k at the registration floor) plus
// one spare bond of unlocked float. The key-epoch history is seeded exactly
// as registration would (epoch 0, unique key) so the key-history invariant
// holds over the fixture.
func setupSlashableGuardian(t *testing.T, f *fixture, addr string, bond math.Int) {
	t.Helper()
	total := sdk.NewCoin(types.DefaultDenom, bond.MulRaw(2))
	locked := sdk.NewCoin(types.DefaultDenom, bond)
	currentHeight := sdk.UnwrapSDKContext(f.ctx).BlockHeight()
	key := getValidPublicKey("slashable_" + addr)
	guardian := types.Guardian{
		Address:             addr,
		EncryptionPublicKey: key,
		AvailableFrom:       currentHeight,
		AvailableUntil:      currentHeight + 100000,
		Stake:               &total,
		LockedStake:         &locked,
		AcceptingSecrets:    true,
		BondK:               types.InitialBondK,
		ActiveBondCount:     1,
	}
	// SetGuardian, not Guardians.Set: it is the module's guardian-write choke
	// point and maintains the eligibility index. Writing the collection directly
	// builds state production cannot produce — an accepting guardian missing from
	// the index — which invariant 9 rejects, correctly.
	require.NoError(t, f.keeper.SetGuardian(f.ctx, guardian))
	// Idempotent across subtests sharing a fixture: the history is
	// append-only, so seed epoch 0 only on the first setup of this address.
	if has, err := f.keeper.GuardianKeyHistory.Has(f.ctx, collections.Join(addr, uint64(0))); err == nil && !has {
		require.NoError(t, f.keeper.AppendGuardianKeyEpoch(f.ctx, addr, 0, types.KeyHistoryEntry{
			PublicKey:           key,
			EffectiveFromHeight: 0,
		}))
	}
}

// setupEarlyRevealSecret stores a secret in PENDING state whose reveal window
// has NOT yet opened, with the guardian holding an accepted assignment whose
// HMAC matches `evidence`.
func setupEarlyRevealSecret(t *testing.T, f *fixture, guardianAddr, creator string, bond math.Int, evidence []byte) string {
	t.Helper()
	secretId := types.GenerateValidSecretID()
	currentHeight := sdk.UnwrapSDKContext(f.ctx).BlockHeight()
	secret := types.Secret{
		Id:                  secretId,
		Creator:             creator,
		Threshold:           2,
		State:               types.SECRET_STATUS_PENDING,
		RevealStartBlock:    currentHeight + 1000, // window NOT yet open
		RevealEndBlock:      currentHeight + 2000,
		RewardPool:          sdk.NewCoin(types.DefaultDenom, math.NewInt(1_000_000_000)),
		GuardianBondAmounts: []int64{bond.Int64()},
		Bump:                types.MinBump,
		SelectedGuardians:   []string{guardianAddr},
		AcceptedCount:       1,
	}
	// SetSecret, not Secrets.Set: the acceptance tally lives in its own record
	// and only the keeper API writes both halves.
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secretId)
	writeShareRecord(t, f, secretId, guardianAddr,
		[]byte("encrypted_share_data"), computeEvidenceHMAC(secretId, guardianAddr, evidence))
	writeAssignmentRecord(t, f, secretId, guardianAddr, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	enqueueForState(t, f, secret)
	return secretId
}

// TestSlashGuardian_EarlyReveal_SlashesFullBond proves a valid report before
// the reveal window slashes the guardian's ENTIRE bond for the secret —
// removed from both float total and locked portion — and marks the guardian
// for pool exclusion. Duplicate reports are rejected.
func TestSlashGuardian_EarlyReveal_SlashesFullBond(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	guardianAddr := sdk.AccAddress([]byte("guardian_address")).String()
	reporterAddr := sdk.AccAddress([]byte("reporter1_address"))
	creatorAddr := sdk.AccAddress([]byte("creator_address")).String()
	bond := testFloatUnit()
	evidence := []byte("decrypted_share_data_minimum_32_bytes_required_for_evidence")

	setupSlashableGuardian(t, f, guardianAddr, bond)
	secretId := setupEarlyRevealSecret(t, f, guardianAddr, creatorAddr, bond, evidence)

	before, _ := f.keeper.GetGuardian(f.ctx, guardianAddr)

	_, err := srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: guardianAddr,
		ReporterAddress: reporterAddr.String(),
		Reason:          "Early reveal detected",
		Evidence:        evidence,
		SecretId:        secretId,
	})
	require.NoError(t, err, "valid early-reveal report must succeed")

	// The FULL bond left the guardian's float: total and locked both shrank by B
	after, found := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.True(t, found, "guardian record persists — no eviction in the bonded model")
	require.True(t, before.Stake.Amount.Sub(bond).Equal(after.Stake.Amount),
		"float total must shrink by the bond: want %s, got %s", before.Stake.Amount.Sub(bond), after.Stake.Amount)
	require.True(t, before.LockedStake.Amount.Sub(bond).Equal(after.LockedStake.Amount),
		"locked portion must shrink by the bond: want %s, got %s", before.LockedStake.Amount.Sub(bond), after.LockedStake.Amount)

	// Marked for exclusion from the reward pool at settlement
	require.True(t, f.keeper.IsGuardianSlashedForEarlyReveal(f.ctx, guardianAddr, secretId))

	// Duplicate report is rejected and changes nothing
	_, err = srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: guardianAddr,
		ReporterAddress: sdk.AccAddress([]byte("reporter2_address")).String(),
		Reason:          "Early reveal detected",
		Evidence:        evidence,
		SecretId:        secretId,
	})
	require.Error(t, err, "duplicate report must be rejected")
	final, _ := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.True(t, after.Stake.Amount.Equal(final.Stake.Amount), "duplicate report must not slash again")

	assertInvariants(t, f)
}

// TestSlashGuardian_RejectedOnceWindowOpen proves reports are only accepted
// BEFORE the reveal window opens — once shares are due to be public, "early"
// evidence is moot.
func TestSlashGuardian_RejectedOnceWindowOpen(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	guardianAddr := sdk.AccAddress([]byte("guardian_address")).String()
	creatorAddr := sdk.AccAddress([]byte("creator_address")).String()
	bond := testFloatUnit()
	evidence := []byte("decrypted_share_data_minimum_32_bytes_required_for_evidence")

	setupSlashableGuardian(t, f, guardianAddr, bond)
	secretId := setupEarlyRevealSecret(t, f, guardianAddr, creatorAddr, bond, evidence)

	// Advance into the reveal window
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)

	_, err := srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: guardianAddr,
		ReporterAddress: sdk.AccAddress([]byte("reporter1_address")).String(),
		Reason:          "Early reveal detected",
		Evidence:        evidence,
		SecretId:        secretId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "only accepted before the reveal window opens")

	// Nothing was slashed
	after, _ := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.Equal(t, bond.MulRaw(2), after.Stake.Amount)

	assertInvariants(t, f)
}

// TestSlashGuardian_RequiresAcceptedAssignment proves a report against a
// guardian that never accepted (no bond locked) is rejected — pre-acceptance
// leaks have no collateral at stake.
func TestSlashGuardian_RequiresAcceptedAssignment(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	guardianAddr := sdk.AccAddress([]byte("guardian_address")).String()
	creatorAddr := sdk.AccAddress([]byte("creator_address")).String()
	bond := testFloatUnit()
	evidence := []byte("decrypted_share_data_minimum_32_bytes_required_for_evidence")

	setupSlashableGuardian(t, f, guardianAddr, bond)
	secretId := setupEarlyRevealSecret(t, f, guardianAddr, creatorAddr, bond, evidence)

	// Downgrade the assignment to PROPOSED (no acceptance, no bond lock)
	writeAssignmentRecord(t, f, secretId, guardianAddr, types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED, 0)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	secret.AcceptedCount = 0 // keep the counter consistent with the records
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))

	_, err = srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: guardianAddr,
		ReporterAddress: sdk.AccAddress([]byte("reporter1_address")).String(),
		Reason:          "Early reveal detected",
		Evidence:        evidence,
		SecretId:        secretId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no accepted assignment")
}

// TestSlashGuardian_InvalidEvidenceRejected proves HMAC verification gates the
// slash: garbage evidence cannot slash anyone.
func TestSlashGuardian_InvalidEvidenceRejected(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	guardianAddr := sdk.AccAddress([]byte("guardian_address")).String()
	creatorAddr := sdk.AccAddress([]byte("creator_address")).String()
	bond := testFloatUnit()
	evidence := []byte("decrypted_share_data_minimum_32_bytes_required_for_evidence")

	setupSlashableGuardian(t, f, guardianAddr, bond)
	secretId := setupEarlyRevealSecret(t, f, guardianAddr, creatorAddr, bond, evidence)

	_, err := srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: guardianAddr,
		ReporterAddress: sdk.AccAddress([]byte("reporter1_address")).String(),
		Reason:          "Early reveal detected",
		Evidence:        []byte("not_the_leaked_share_but_still_32_bytes_long_data"),
		SecretId:        secretId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HMAC verification failed")
}

func TestHasGuardianRevealed_DoubleBashingProtection(t *testing.T) {
	f := initFixture(t)
	guardianAddr := "test_guardian_address"

	// Guardian has not revealed yet (no reveal record in the side-store)
	secretId := types.GenerateValidSecretID()
	require.False(t, f.keeper.HasGuardianRevealed(f.ctx, secretId, guardianAddr))

	// Guardian has revealed their share
	secretIdWithReveal := types.GenerateValidSecretID()
	writeReveal(t, f, secretIdWithReveal, guardianAddr, []byte("revealed_share_data"), 100)
	require.True(t, f.keeper.HasGuardianRevealed(f.ctx, secretIdWithReveal, guardianAddr))

	// Different guardian revealed (not the one we're checking)
	secretIdWithOtherReveal := types.GenerateValidSecretID()
	writeReveal(t, f, secretIdWithOtherReveal, "different_guardian_address", []byte("revealed_share_data"), 100)
	require.False(t, f.keeper.HasGuardianRevealed(f.ctx, secretIdWithOtherReveal, guardianAddr))
}

// Additional SlashGuardian tests moved from keeper_test.go
func TestSlashGuardian_ValidatesEvidence(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	// Register guardian to slash
	guardianAddr := sdk.AccAddress([]byte("slash_evidence_test"))
	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("test_enckey"),
		AvailableFrom:       0,
		AvailableUntil:      1000,
		Deposit:             &validStake,
		AcceptingSecrets:    true,
	}
	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)
	reporterAddr := sdk.AccAddress([]byte("reporter"))
	testCases := []struct {
		name        string
		evidence    []byte
		reason      string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty evidence - fails",
			evidence:    []byte{},
			reason:      "early reveal detected",
			expectError: true,
			errorMsg:    "evidence cannot be empty",
		},
		{
			name:        "empty reason - fails",
			evidence:    getValidPublicKey("test_evidence"),
			reason:      "",
			expectError: true,
			errorMsg:    "slash reason cannot be empty",
		},
	}
	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use different guardian (and key — keys are globally unique
			// forever) for each test case
			testGuardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("slash_guardian_%d", i)))
			// Register test guardian
			regMsg := &types.MsgGuardianRegister{
				Guardian:            testGuardianAddr.String(),
				EncryptionPublicKey: getValidPublicKey(fmt.Sprintf("test_enckey_%d", i)),
				AvailableFrom:       0,
				AvailableUntil:      1000,
				Deposit:             &validStake,
				AcceptingSecrets:    true,
			}
			_, err := msgServer.GuardianRegister(f.ctx, regMsg)
			require.NoError(t, err)
			slashMsg := &types.MsgSlashGuardian{
				GuardianAddress: testGuardianAddr.String(),
				ReporterAddress: reporterAddr.String(),
				Reason:          tc.reason,
				Evidence:        tc.evidence,
				SecretId:        types.GenerateValidSecretID(),
			}
			_, err = msgServer.SlashGuardian(f.ctx, slashMsg)
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSlashGuardian_PreventsSelfSlashing(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	// Register guardian
	guardianAddr := sdk.AccAddress([]byte("self_slash_test"))
	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("test_enckey"),
		AvailableFrom:       0,
		AvailableUntil:      1000,
		Deposit:             &validStake,
		AcceptingSecrets:    true,
	}
	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)
	// Try to self-slash (should fail)
	slashMsg := &types.MsgSlashGuardian{
		GuardianAddress: guardianAddr.String(),
		ReporterAddress: guardianAddr.String(), // Same as guardian!
		Reason:          "trying to self-slash for bounty",
		Evidence:        getValidPublicKey("test_evidence"),
		SecretId:        types.GenerateValidSecretID(),
	}
	_, err = msgServer.SlashGuardian(f.ctx, slashMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "guardian cannot slash themselves")
}
