package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// TestNoStrandedBonds proves the spec invariant (docs/spec.md "Economic
// Invariants"): every path a secret can take terminates in a settlement that
// returns, slashes, or refunds every locked bond — no bond is left locked
// indefinitely. Each subtest drives one lifecycle path end-to-end through the
// real message handlers and EndBlock sweeps, then asserts every guardian's
// locked float is zero.
func TestNoStrandedBonds(t *testing.T) {
	const (
		bump      = types.MinBump
		threshold = int64(2)
		shares    = int64(3)
	)

	// buildCommittedSecret drives a secret through Phases 1-3 plus the
	// deadline finalisation (all candidates accepted, state pending) and
	// returns its id and the bonded guardians.
	buildCommittedSecret := func(t *testing.T, f *fixture, msgServer types.MsgServer) (string, []string) {
		t.Helper()
		secretId := setupBondTestSecret(t, f, msgServer, bump, threshold, shares)
		secret, err := f.keeper.GetSecret(f.ctx, secretId)
		require.NoError(t, err)

		bonded := make([]string, 0, shares)
		for i := int64(0); i < shares; i++ {
			g := secret.SelectedGuardians[i]
			_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
				Guardian: g, SecretId: secretId, Accept: true,
			})
			require.NoError(t, err)
			bonded = append(bonded, g)
		}
		finaliseCommitDeadline(t, f, secretId)

		committed, err := f.keeper.GetSecret(f.ctx, secretId)
		require.NoError(t, err)
		require.Equal(t, types.SECRET_STATUS_PENDING, committed.State)
		return secretId, bonded
	}

	// assertNoLockedFloat asserts no guardian in the fixture has anything
	// locked — the invariant itself.
	assertNoLockedFloat := func(t *testing.T, f *fixture, path string) {
		t.Helper()
		require.NoError(t, f.keeper.Guardians.Walk(f.ctx, nil, func(addr string, g types.Guardian) (bool, error) {
			locked := math.ZeroInt()
			if g.LockedStake != nil {
				locked = g.LockedStake.Amount
			}
			require.True(t, locked.IsZero(),
				"path %q stranded %s locked in guardian %s", path, locked, addr)
			return false, nil
		}))
	}

	// terminalAndSwept asserts the secret is terminal, that a further sweep
	// pass changes nothing (no re-processing loops), and that BOTH due-height
	// queues are empty — a terminal secret must never be looked at again by
	// EndBlock (the queues are the mechanism that guarantees O(due) work).
	terminalAndSwept := func(t *testing.T, f *fixture, secretId, wantState string) {
		t.Helper()
		secret, err := f.keeper.GetSecret(f.ctx, secretId)
		require.NoError(t, err)
		require.Equal(t, wantState, secret.State)
		require.True(t, secret.IsComplete(), "state %s must be terminal", wantState)

		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1)
		require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))
		require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))
		after, err := f.keeper.GetSecret(f.ctx, secretId)
		require.NoError(t, err)
		require.Equal(t, wantState, after.State, "terminal secret must not be re-processed")

		// Queue hygiene: no entry for any secret may survive its terminal state
		commitEntries := 0
		require.NoError(t, f.keeper.CommitQueue.Walk(f.ctx, nil, func(_ collections.Pair[int64, string]) (bool, error) {
			commitEntries++
			return false, nil
		}))
		settlementEntries := 0
		require.NoError(t, f.keeper.SettlementQueue.Walk(f.ctx, nil, func(_ collections.Pair[int64, string]) (bool, error) {
			settlementEntries++
			return false, nil
		}))
		require.Zero(t, commitEntries, "commit queue must be empty once every secret is terminal")
		require.Zero(t, settlementEntries, "settlement queue must be empty once every secret is terminal")

		// Full state-side invariant library (stranded bonds + queue hygiene)
		assertInvariants(t, f)
	}

	revealAs := func(t *testing.T, f *fixture, msgServer types.MsgServer, secretId, guardian string) {
		t.Helper()
		secret, err := f.keeper.GetSecret(f.ctx, secretId)
		require.NoError(t, err)
		require.Contains(t, secret.SelectedGuardians, guardian, "revealer must hold a distributed share")
		share := testShareBytes(secretId, guardian) // matches setup HMAC input
		_, err = msgServer.GuardianRevealShare(f.ctx, &types.MsgGuardianRevealShare{
			Guardian: guardian, SecretId: secretId, DecryptedShare: share,
		})
		require.NoError(t, err)
	}

	t.Run("success: all reveal, threshold met", func(t *testing.T) {
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

		secretId, bonded := buildCommittedSecret(t, f, msgServer)
		secret, _ := f.keeper.GetSecret(f.ctx, secretId)

		// Open the reveal window and have everyone reveal
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)
		for _, g := range bonded {
			revealAs(t, f, msgServer, secretId, g)
		}

		// Settle at window end
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock + 1)
		require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

		assertNoLockedFloat(t, f, "success")
		terminalAndSwept(t, f, secretId, types.SECRET_STATUS_REVEALED)
	})

	t.Run("threshold-fail: one reveals, others slashed", func(t *testing.T) {
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

		secretId, bonded := buildCommittedSecret(t, f, msgServer)
		secret, _ := f.keeper.GetSecret(f.ctx, secretId)

		// Only one guardian reveals (below the threshold of 2)
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)
		revealAs(t, f, msgServer, secretId, bonded[0])

		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock + 1)
		require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

		assertNoLockedFloat(t, f, "threshold-fail")
		terminalAndSwept(t, f, secretId, types.SECRET_STATUS_FAILED)
	})

	t.Run("reveal-timeout: nobody reveals", func(t *testing.T) {
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

		secretId, _ := buildCommittedSecret(t, f, msgServer)
		secret, _ := f.keeper.GetSecret(f.ctx, secretId)

		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock + 1)
		require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

		assertNoLockedFloat(t, f, "reveal-timeout")
		terminalAndSwept(t, f, secretId, types.SECRET_STATUS_FAILED)
	})

	t.Run("cancel: paid exit mid-hold", func(t *testing.T) {
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

		secretId, _ := buildCommittedSecret(t, f, msgServer)
		secret, _ := f.keeper.GetSecret(f.ctx, secretId)

		// Cancel between commit deadline and reveal start
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.CommitDeadline + 50)
		_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
			SecretId: secretId, Creator: secret.Creator,
		})
		require.NoError(t, err)

		assertNoLockedFloat(t, f, "cancel")
		terminalAndSwept(t, f, secretId, types.SECRET_STATUS_CANCELLED)
	})

	t.Run("commit-timeout: partial acceptance", func(t *testing.T) {
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

		secretId := setupBondTestSecret(t, f, msgServer, bump, threshold, shares)
		secret, _ := f.keeper.GetSecret(f.ctx, secretId)

		// Only one guardian accepts (fewer than the required 3 slots)
		_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
			Guardian: secret.SelectedGuardians[0],
			SecretId: secretId,
			Accept:   true,
		})
		require.NoError(t, err)

		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.CommitDeadline + 1)
		require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

		assertNoLockedFloat(t, f, "commit-timeout")
		terminalAndSwept(t, f, secretId, types.SECRET_STATUS_FAILED)
	})

	t.Run("early-reveal report then settlement", func(t *testing.T) {
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.keeper)
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

		secretId, bonded := buildCommittedSecret(t, f, msgServer)
		secret, _ := f.keeper.GetSecret(f.ctx, secretId)

		// Report the leaker BEFORE the window opens; the leaked share is the
		// exact plaintext the setup HMAC committed to
		leaker := bonded[2]
		evidence := testShareBytes(secretId, leaker)
		_, err := msgServer.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
			GuardianAddress: leaker,
			ReporterAddress: sdk.AccAddress([]byte("invariant_reporter__")).String(),
			Reason:          "early reveal",
			Evidence:        evidence,
			SecretId:        secretId,
		})
		require.NoError(t, err)

		// The remaining two reveal in the window; settle at window end
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)
		revealAs(t, f, msgServer, secretId, bonded[0])
		revealAs(t, f, msgServer, secretId, bonded[1])
		f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock + 1)
		require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

		assertNoLockedFloat(t, f, "early-reveal")
		terminalAndSwept(t, f, secretId, types.SECRET_STATUS_REVEALED)
	})
}
