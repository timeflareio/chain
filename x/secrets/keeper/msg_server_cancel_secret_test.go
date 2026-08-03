package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

func TestMsgUserCancelSecret(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Test data - use SDK AccAddress format
	creatorAddr := sdk.AccAddress([]byte("creator_address")).String()
	guardianAddr1 := sdk.AccAddress([]byte("guardian1_address")).String()
	guardianAddr2 := sdk.AccAddress([]byte("guardian2_address")).String()
	rewardAmount := sdk.NewInt64Coin("uveil", 1500000000) // 1500 VEIL

	// Each case uses its own secret id: the accepted set now lives in the
	// per-guardian assignment side-store, so a shared id would leak records
	// between cases.
	tests := []struct {
		name        string
		setupSecret func(t *testing.T) types.Secret
		expectError bool
		errorMsg    string
	}{
		{
			// Cancellation is a post-activation mechanic (ruled July 2026):
			// pre-activation secrets exit via commit-timeout only.
			name: "Error: Cancel rejected in reserved state",
			setupSecret: func(t *testing.T) types.Secret {
				return types.Secret{
					Id:               types.GenerateValidSecretID(),
					Creator:          creatorAddr,
					State:            types.SECRET_STATUS_RESERVED,
					RewardPool:       rewardAmount,
					RevealStartBlock: sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1000,
					CreatedAt:        sdk.UnwrapSDKContext(f.ctx).BlockHeight(),
					// No guardians selected or accepted in reserved state
				}
			},
			expectError: true,
			errorMsg:    "can only cancel secrets in pending state",
		},
		{
			name: "Error: Cancel rejected in awaiting_acceptance state",
			setupSecret: func(t *testing.T) types.Secret {
				return types.Secret{
					Id:               types.GenerateValidSecretID(),
					Creator:          creatorAddr,
					State:            types.SECRET_STATUS_AWAITING_ACCEPTANCE,
					RewardPool:       rewardAmount,
					RevealStartBlock: sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1000,
					CreatedAt:        sdk.UnwrapSDKContext(f.ctx).BlockHeight(),
					// No acceptances yet: accepted set is empty until pending state
				}
			},
			expectError: true,
			errorMsg:    "can only cancel secrets in pending state",
		},
		{
			name: "Successfully cancel secret in pending state with time-based compensation",
			setupSecret: func(t *testing.T) types.Secret {
				acceptanceBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 500
				revealStartBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 500 // 1000 blocks total commitment

				// Bond release now hard-fails on a missing guardian record or an
				// unlockable bond, so the guardians must exist with the bond locked
				bond := testFloatUnit()
				setupSlashableGuardian(t, f, guardianAddr1, bond)
				setupSlashableGuardian(t, f, guardianAddr2, bond)

				secret := types.Secret{
					Id:                  types.GenerateValidSecretID(),
					Creator:             creatorAddr,
					State:               types.SECRET_STATUS_PENDING,
					RewardPool:          rewardAmount,
					GuardianBondAmounts: repeatBond(bond, 2),
					RevealStartBlock:    revealStartBlock,
					SelectedGuardians:   []string{guardianAddr1, guardianAddr2},
					AcceptedCount:       2,
					CreatedAt:           sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 600,
				}
				writeAssignmentRecord(t, f, secret.Id, guardianAddr1,
					types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, acceptanceBlock)
				writeAssignmentRecord(t, f, secret.Id, guardianAddr2,
					types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, acceptanceBlock)
				return secret
			},
			expectError: false,
		},
		{
			name: "Error: Cancel after reveal window starts",
			setupSecret: func(t *testing.T) types.Secret {
				secret := types.Secret{
					Id:                types.GenerateValidSecretID(),
					Creator:           creatorAddr,
					State:             types.SECRET_STATUS_PENDING,
					RewardPool:        rewardAmount,
					RevealStartBlock:  sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 10, // Already started
					SelectedGuardians: []string{guardianAddr1},
					AcceptedCount:     1,
					CreatedAt:         sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 100,
				}
				writeAssignmentRecord(t, f, secret.Id, guardianAddr1,
					types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, sdk.UnwrapSDKContext(f.ctx).BlockHeight()-50)
				return secret
			},
			expectError: true,
			errorMsg:    "cannot cancel secret after reveal window starts",
		},
		{
			// Bonded economics: cancellation is a PAID exit permitted at any
			// point before reveal_start_block — even one block before. Pro-rata
			// guardian pay makes late cancellation non-abusive, so the old
			// MinCancelBlocks buffer is gone.
			name: "Success: Cancel one block before reveal window (paid exit)",
			setupSecret: func(t *testing.T) types.Secret {
				setupSlashableGuardian(t, f, guardianAddr1, testFloatUnit())
				secret := types.Secret{
					Id:                  types.GenerateValidSecretID(),
					Creator:             creatorAddr,
					State:               types.SECRET_STATUS_PENDING,
					RewardPool:          rewardAmount,
					CommitDeadline:      sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 50,
					RevealStartBlock:    sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1, // one block away
					RevealEndBlock:      sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1000,
					Bump:                types.MinBump,
					GuardianBondAmounts: repeatBond(testFloatUnit(), 1),
					SelectedGuardians:   []string{guardianAddr1},
					AcceptedCount:       1,
					CreatedAt:           sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 100,
				}
				writeAssignmentRecord(t, f, secret.Id, guardianAddr1,
					types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, sdk.UnwrapSDKContext(f.ctx).BlockHeight()-50)
				return secret
			},
			expectError: false,
		},
		{
			name: "Error: Wrong creator",
			setupSecret: func(t *testing.T) types.Secret {
				return types.Secret{
					Id:               types.GenerateValidSecretID(),
					Creator:          sdk.AccAddress([]byte("different_creator")).String(),
					State:            types.SECRET_STATUS_RESERVED,
					RewardPool:       rewardAmount,
					RevealStartBlock: sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1000,
					CreatedAt:        sdk.UnwrapSDKContext(f.ctx).BlockHeight(),
				}
			},
			expectError: true,
			errorMsg:    "only creator can cancel secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup secret (and any per-guardian assignment records)
			secret := tt.setupSecret(t)
			err := f.keeper.SetSecret(f.ctx, secret)
			require.NoError(t, err)

			// Attempt cancellation
			msg := &types.MsgUserCancelSecret{
				SecretId: secret.Id,
				Creator:  creatorAddr,
			}

			response, err := msgServer.UserCancelSecret(f.ctx, msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
				require.Nil(t, response)
				return
			}

			// Verify successful cancellation
			require.NoError(t, err)
			require.NotNil(t, response)

			// Verify secret state updated
			updatedSecret, err := f.keeper.GetSecret(f.ctx, secret.Id)
			require.NoError(t, err)
			require.Equal(t, types.SECRET_STATUS_CANCELLED, updatedSecret.State)

			// Note: Bank operations are mocked in this test setup.
			// The important thing is that the cancellation logic executes without errors.
			// In a full integration test, we would verify actual token transfers.
		})
	}
}

func TestUserCancelSecretLogic(t *testing.T) {
	f := initFixture(t)

	t.Run("Cancellation executes compensation logic without errors", func(t *testing.T) {
		// Test that the compensation calculation doesn't cause panics or errors
		guardianAddr1 := sdk.AccAddress([]byte("guardian1_address")).String()
		guardianAddr2 := sdk.AccAddress([]byte("guardian2_address")).String()
		acceptanceBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 250 // 50% elapsed
		bond := testFloatUnit()
		setupSlashableGuardian(t, f, guardianAddr1, bond)
		setupSlashableGuardian(t, f, guardianAddr2, bond)
		secret := types.Secret{
			Id:                  types.GenerateValidSecretID(),
			Creator:             sdk.AccAddress([]byte("creator_address")).String(),
			State:               types.SECRET_STATUS_PENDING,
			RewardPool:          sdk.NewInt64Coin("uveil", 1500000000),
			GuardianBondAmounts: repeatBond(bond, 2),
			RevealStartBlock:    sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 500,
			SelectedGuardians:   []string{guardianAddr1, guardianAddr2},
			AcceptedCount:       2,
			CreatedAt:           sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 300,
		}

		err := f.keeper.SetSecret(f.ctx, secret)
		require.NoError(t, err)
		writeAssignmentRecord(t, f, secret.Id, guardianAddr1,
			types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, acceptanceBlock)
		writeAssignmentRecord(t, f, secret.Id, guardianAddr2,
			types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, acceptanceBlock)

		msg := &types.MsgUserCancelSecret{
			SecretId: secret.Id,
			Creator:  secret.Creator,
		}

		msgServer := keeper.NewMsgServerImpl(f.keeper)
		response, err := msgServer.UserCancelSecret(f.ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, response)

		// Verify secret is cancelled
		updatedSecret, err := f.keeper.GetSecret(f.ctx, secret.Id)
		require.NoError(t, err)
		require.Equal(t, types.SECRET_STATUS_CANCELLED, updatedSecret.State)
	})

	t.Run("Zero reward pool handles gracefully", func(t *testing.T) {
		secret := types.Secret{
			Id:               types.GenerateValidSecretID(),
			Creator:          sdk.AccAddress([]byte("creator_address")).String(),
			State:            types.SECRET_STATUS_PENDING, // cancellation is pending-only
			RewardPool:       sdk.NewInt64Coin("uveil", 0),
			CommitDeadline:   sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 10,
			RevealStartBlock: sdk.UnwrapSDKContext(f.ctx).BlockHeight() + 1000,
			CreatedAt:        sdk.UnwrapSDKContext(f.ctx).BlockHeight() - 20,
		}

		err := f.keeper.SetSecret(f.ctx, secret)
		require.NoError(t, err)

		msg := &types.MsgUserCancelSecret{
			SecretId: secret.Id,
			Creator:  secret.Creator,
		}

		msgServer := keeper.NewMsgServerImpl(f.keeper)
		response, err := msgServer.UserCancelSecret(f.ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, response)

		// Verify secret is cancelled
		updatedSecret, err := f.keeper.GetSecret(f.ctx, secret.Id)
		require.NoError(t, err)
		require.Equal(t, types.SECRET_STATUS_CANCELLED, updatedSecret.State)
	})
}
