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

// Phase 2 (bonded guardian economics) coverage: protocol-derived pricing at
// publication, bond locking at acceptance, the first-num_guardians-to-confirm
// slot cap, and the commit-timeout bond release.
// See docs/spec.md "Secret Economics & Slashing".

// setupBondTestSecret drives a secret through Phases 1+2 with the given bump,
// registering enough guardians (each with a float of 4 bonds at that bump).
func setupBondTestSecret(t *testing.T, f *fixture, msgServer types.MsgServer, bump, threshold, shares int64) string {
	t.Helper()

	// Register guardians with ample float for this bump level
	deposit := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	needed := shares + shares // shares + generous buffer headroom
	for i := int64(0); i < needed+5; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("bond_%s_g%02d____________", t.Name(), i)))
		msg := &types.MsgGuardianRegister{
			Guardian:            addr.String(),
			EncryptionPublicKey: getValidPublicKey(fmt.Sprintf("bondkey_%s_%d", t.Name(), i)),
			AvailableFrom:       0,
			AvailableUntil:      100000,
			Deposit:             &deposit,
			AcceptingSecrets:    true,
		}
		_, err := msgServer.GuardianRegister(f.ctx, msg)
		require.NoError(t, err)
	}

	// Advance one block so the guardians' availability windows (from = height+1)
	// are active at request time
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockHeight(sdkCtx.BlockHeight() + 1)

	creator := sdk.AccAddress([]byte("creator_address"))
	reqMsg := &types.MsgUserRequestGuardians{
		Creator:       creator.String(),
		DetectionHint: testDetectionHint(),
		Threshold:     threshold,
		MinShares:     shares,
		MaxShares:     shares,
		RevealWindow: &types.RevealWindow{
			StartOffset: 400,
			Duration:    testRevealDuration,
		},
		Bump: bump,
	}
	resp, err := msgServer.UserRequestGuardians(f.ctx, reqMsg)
	require.NoError(t, err)

	// Distribute shares so guardians can accept
	secret, err := f.keeper.GetSecret(f.ctx, resp.SecretId)
	require.NoError(t, err)
	shareData := make([]*types.EncryptedShareData, 0, len(secret.SelectedGuardians))
	for _, guardian := range secret.SelectedGuardians {
		data := testShareBytes(resp.SecretId, guardian)
		shareData = append(shareData, &types.EncryptedShareData{
			GuardianAddress: guardian,
			EncryptedShare:  data,
			ShareHmac:       generateTestHMAC(resp.SecretId, guardian, data),
		})
	}
	_, err = msgServer.UserDistributeShares(f.ctx, &types.MsgUserDistributeShares{
		Creator:           creator.String(),
		SecretId:          resp.SecretId,
		SecretCommitment:  []byte("secret_commitment_hash"),
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            shareData,
	})
	require.NoError(t, err)

	return resp.SecretId
}

// TestUserRequestGuardians_DerivesEconomics proves the reward pool and bonds are
// protocol-derived at publication: P = rate × distance × shares × bump and
// B_g = rate × distance × bump × k_g frozen per selected guardian, with
// distance = commit_deadline → settlement.
func TestUserRequestGuardians_DerivesEconomics(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	const bump = int64(250) // 2.50
	secretId := setupBondTestSecret(t, f, msgServer, bump, 3, 5)

	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)

	// distance runs from commit_deadline to SETTLEMENT, which is end + 1
	// (the reveal window is inclusive of reveal_end_block)
	distance := (secret.RevealEndBlock + 1) - secret.CommitDeadline
	require.Positive(t, distance)

	expectedPool := types.RewardPoolAmount(distance, secret.MaxShares, bump)
	require.Equal(t, expectedPool, secret.RewardPool.Amount,
		"reward pool must equal rate × distance × requested_shares × bump")

	// Every selected guardian is freshly registered (k at the floor), so all
	// frozen bonds equal B = rate × distance × bump × k(4.00)
	expectedBond := types.BondAmount(distance, bump, types.InitialBondK)
	require.Len(t, secret.GuardianBondAmounts, len(secret.SelectedGuardians),
		"exactly one frozen bond per selected guardian")
	for i, got := range secret.GuardianBondAmounts {
		require.Equal(t, expectedBond.Int64(), got, "frozen bond for guardian %d", i)
	}
	require.Equal(t, bump, secret.Bump)

	// Sanity-check against hand-computed values: at rate 1, bump 2.50 and
	// k 4.00 the fixed-point factors collapse to 10 uveil per block of distance
	require.Equal(t, math.NewInt(distance*10), expectedBond)

	assertInvariants(t, f)
}

// TestGuardianConfirmShares_LocksBond proves acceptance moves exactly B from the
// guardian's unlocked float into its locked portion.
func TestGuardianConfirmShares_LocksBond(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 3, 5)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)

	guardianAddr := secret.SelectedGuardians[0]
	before, found := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.True(t, found)
	require.True(t, before.LockedStake.IsZero())

	_, err = msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
		Guardian: guardianAddr,
		SecretId: secretId,
		Accept:   true,
	})
	require.NoError(t, err)

	bond, ok := secret.GuardianBondAmount(guardianAddr)
	require.True(t, ok)
	after, _ := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.Equal(t, bond, after.LockedStake.Amount,
		"acceptance must lock exactly the guardian's frozen bond")
	require.Equal(t, before.Stake.Amount, after.Stake.Amount,
		"locking must not change the float total")
	require.Equal(t, int64(1), after.ActiveBondCount,
		"acceptance must count one active bond")

	assertInvariants(t, f)
}

// TestGuardianConfirmShares_RejectsWhenUnlockedFloatInsufficient proves acceptance is
// the hard capital gate: a guardian whose unlocked float cannot cover B is
// rejected outright, with no partial lock.
func TestGuardianConfirmShares_RejectsWhenUnlockedFloatInsufficient(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 3, 5)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)

	// Drain the guardian's unlocked float by locking almost all of it, leaving
	// less than one bond unlocked
	guardianAddr := secret.SelectedGuardians[0]
	g, _ := f.keeper.GetGuardian(f.ctx, guardianAddr)
	bond, ok := secret.GuardianBondAmount(guardianAddr)
	require.True(t, ok)
	leaveUnlocked := bond.SubRaw(1) // one uveil short
	require.NoError(t, f.keeper.LockGuardianFloat(f.ctx, guardianAddr, g.Stake.Amount.Sub(leaveUnlocked)))

	_, err = msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
		Guardian: guardianAddr,
		SecretId: secretId,
		Accept:   true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient unlocked float")

	// No partial lock: the locked amount is unchanged and the assignment still open
	after, _ := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.Equal(t, g.Stake.Amount.Sub(leaveUnlocked), after.LockedStake.Amount)
	require.Equal(t, types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED, assignmentStatus(t, f, secretId, guardianAddr))

	// NOTE: no assertInvariants here — this test fabricates an unbacked lock
	// via LockGuardianFloat as a shortcut for "float consumed elsewhere", which
	// the no-stranded-bonds invariant rightly rejects. The legitimate
	// multi-secret exhaustion path is the B5 conformance scenario.
}

// TestGuardianConfirmShares_WholeBandAccepts proves there is no first-n race: every
// one of the max_shares candidates can accept — none is turned away while the
// deadline is open — each locking its own bond, and the deadline finalisation
// activates the secret with the full accepted set.
func TestGuardianConfirmShares_WholeBandAccepts(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	const shares = int64(3)
	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, shares)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, shares, int64(len(secret.SelectedGuardians)),
		"selection draws exactly max_shares candidates")

	// Every candidate accepts; each acceptance locks that guardian's bond
	for i, g := range secret.SelectedGuardians {
		_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
			Guardian: g,
			SecretId: secretId,
			Accept:   true,
		})
		require.NoError(t, err, "acceptance %d must not be turned away", i+1)

		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		bond, ok := secret.GuardianBondAmount(g)
		require.True(t, ok)
		require.Equal(t, bond, guardian.LockedStake.Amount,
			"each acceptance locks that guardian's own frozen bond")
	}

	mid, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_AWAITING_ACCEPTANCE, mid.State,
		"nothing activates mid-window")
	require.Equal(t, int64(len(secret.SelectedGuardians)), mid.AcceptedCount)

	// The deadline finalises with the whole accepted set
	finaliseCommitDeadline(t, f, secretId)
	activated, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_PENDING, activated.State)

	assertInvariants(t, f)
}

// TestProcessExpiredCommits_ReleasesAcceptedBonds proves the commit-timeout
// no-fault path: guardians who accepted before the deadline get their bonds
// unlocked when the secret fails to fill all slots.
func TestProcessExpiredCommits_ReleasesAcceptedBonds(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 3, 5)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)

	// Two guardians accept (fewer than the 5 required slots)
	accepted := []string{
		secret.SelectedGuardians[0],
		secret.SelectedGuardians[1],
	}
	for _, g := range accepted {
		_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
			Guardian: g, SecretId: secretId, Accept: true,
		})
		require.NoError(t, err)
		bond, ok := secret.GuardianBondAmount(g)
		require.True(t, ok)
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.Equal(t, bond, guardian.LockedStake.Amount)
	}

	// Advance past the commit deadline and run the sweep
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.CommitDeadline + 1)
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

	// The secret failed and every accepted bond was released in full
	failed, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_FAILED, failed.State)
	for _, g := range accepted {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero(),
			"commit-timeout must release guardian %s's bond", g)
	}

	assertInvariants(t, f)
}
