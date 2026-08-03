package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// countingBankKeeper wraps the standard mock and records how many times the module
// refunds a creator, so a test can assert a refund happens exactly once per secret.
type countingBankKeeper struct {
	mockBankKeeper
	moduleToAccountCalls *int
}

func (c countingBankKeeper) SendCoinsFromModuleToAccount(
	ctx context.Context,
	senderModule string,
	recipientAddr sdk.AccAddress,
	amt sdk.Coins,
) error {
	*c.moduleToAccountCalls++
	return c.mockBankKeeper.SendCoinsFromModuleToAccount(ctx, senderModule, recipientAddr, amt)
}

// TestProcessExpiredCommit_RefundsOnceAndTerminates is a regression guard for the
// commit-expiry double-refund bug: processExpiredCommit transitioned the secret to
// FAILED via the state machine, then wrote a STALE pre-transition copy back over it,
// reverting the state. Because the secret never actually reached a terminal state,
// ProcessExpiredCommits re-processed it every block and refunded the creator again
// and again. The fix removes the stale write; this test proves the secret ends up
// FAILED after one pass and that a second pass is a no-op (no additional refund).
func TestProcessExpiredCommit_RefundsOnceAndTerminates(t *testing.T) {
	states := []string{
		types.SECRET_STATUS_RESERVED,
		types.SECRET_STATUS_AWAITING_ACCEPTANCE,
	}

	for _, startState := range states {
		t.Run(startState, func(t *testing.T) {
			refundCalls := 0
			f := initFixtureWithBank(t, countingBankKeeper{moduleToAccountCalls: &refundCalls})

			creator := sdk.AccAddress([]byte("commit_expiry_creator"))
			secretID := types.GenerateValidSecretID()

			// A deadline that the current height has already passed.
			const createdAt = int64(100)
			const commitDeadline = int64(150)
			const currentHeight = int64(100_000)

			reward := sdk.NewCoin(types.DefaultDenom, math.NewInt(1_000_000))
			secret := types.Secret{
				Id:             secretID,
				Creator:        creator.String(),
				Threshold:      3,
				MinShares:      3,
				MaxShares:      3,
				State:          startState,
				RewardPool:     reward,
				CreatedAt:      createdAt,
				CommitDeadline: commitDeadline,
				RevealEndBlock: commitDeadline + 1000,
			}
			require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
			// Register the deadlines as UserRequestGuardians would (the sweep is queue-driven)
			require.NoError(t, f.keeper.EnqueueSecretDeadlines(f.ctx, secret))

			f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(currentHeight)

			// First sweep: the secret is expired and unaccepted → fail + refund once.
			require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

			got, err := f.keeper.GetSecret(f.ctx, secretID)
			require.NoError(t, err)
			require.Equal(t, types.SECRET_STATUS_FAILED, got.State,
				"expired commit must end in FAILED, not revert to %s", startState)
			require.True(t, got.IsComplete(), "FAILED secret must be terminal")
			require.Equal(t, 1, refundCalls, "creator must be refunded exactly once")

			// Second sweep (next block): the secret is terminal and must be skipped
			// entirely — no state change, no additional refund.
			f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(currentHeight + 1)
			require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

			got, err = f.keeper.GetSecret(f.ctx, secretID)
			require.NoError(t, err)
			require.Equal(t, types.SECRET_STATUS_FAILED, got.State)
			require.Equal(t, 1, refundCalls, "a terminal secret must never be refunded again")

			assertInvariants(t, f)
		})
	}
}
