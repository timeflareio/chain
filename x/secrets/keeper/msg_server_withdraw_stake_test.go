package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// registerWithdrawTestGuardian registers a guardian with a float of 4 fixture units.
func registerWithdrawTestGuardian(t *testing.T, f *fixture, msgServer types.MsgServer, name string) (sdk.AccAddress, sdk.Coin) {
	t.Helper()
	guardianAddr := sdk.AccAddress([]byte(name))
	deposit := sdk.NewCoin(types.DefaultDenom, testFloatUnit().MulRaw(4))

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey(name + "_enckey"),
		AvailableFrom:       5,
		AvailableUntil:      1005,
		Deposit:             &deposit,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)
	return guardianAddr, deposit
}

func TestGuardianWithdrawStake_Success(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardianAddr, deposit := registerWithdrawTestGuardian(t, f, msgServer, "withdraw_success_test")

	// Verify guardian exists with the full float unlocked
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, deposit, *guardian.Stake)

	// Withdraw the unlocked float — allowed at any time in the bonded model,
	// even during the availability window (bonds, not availability, protect
	// in-flight secrets)
	withdrawMsg := &types.MsgGuardianWithdrawStake{
		Guardian: guardianAddr.String(),
	}

	_, err := msgServer.GuardianWithdrawStake(f.ctx, withdrawMsg)
	require.NoError(t, err)

	// The guardian record PERSISTS (registration is permanent; the entry fee
	// was burned) with a zero float
	guardian, found = f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found, "guardian record must persist after withdrawal")
	require.True(t, guardian.Stake.IsZero(), "float total must be zero after full withdrawal")
	require.True(t, guardian.LockedStake.IsZero())
}

func TestGuardianWithdrawStake_OnlyUnlockedFloatReturned(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardianAddr, deposit := registerWithdrawTestGuardian(t, f, msgServer, "withdraw_locked_test")

	// Lock one fixture unit as if a secret had been accepted
	bond := testFloatUnit()
	require.NoError(t, f.keeper.LockGuardianFloat(f.ctx, guardianAddr.String(), bond))

	// Withdraw — must return only the unlocked portion
	withdrawMsg := &types.MsgGuardianWithdrawStake{Guardian: guardianAddr.String()}
	_, err := msgServer.GuardianWithdrawStake(f.ctx, withdrawMsg)
	require.NoError(t, err)

	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, bond, guardian.Stake.Amount, "float total must shrink to exactly the locked bond")
	require.Equal(t, bond, guardian.LockedStake.Amount, "locked bond must remain in escrow")
	_ = deposit

	// A second withdrawal has nothing unlocked to return
	_, err = msgServer.GuardianWithdrawStake(f.ctx, withdrawMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no unlocked float")
}

func TestGuardianWithdrawStake_GuardianNotFound(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Try to withdraw stake for non-existent guardian
	nonExistentAddr := sdk.AccAddress([]byte("non_existent_guardian"))
	withdrawMsg := &types.MsgGuardianWithdrawStake{
		Guardian: nonExistentAddr.String(),
	}

	_, err := msgServer.GuardianWithdrawStake(f.ctx, withdrawMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "guardian not found")
}

func TestGuardianWithdrawStake_InvalidAddress(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	testCases := []struct {
		name     string
		guardian string
		errorMsg string
	}{
		{
			name:     "empty address",
			guardian: "",
			errorMsg: "invalid guardian address",
		},
		{
			name:     "malformed address",
			guardian: "invalid_address_format",
			errorMsg: "invalid guardian address",
		},
		{
			name:     "wrong prefix",
			guardian: "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48abc123",
			errorMsg: "invalid guardian address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			withdrawMsg := &types.MsgGuardianWithdrawStake{
				Guardian: tc.guardian,
			}

			_, err := msgServer.GuardianWithdrawStake(f.ctx, withdrawMsg)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errorMsg)
		})
	}
}

func TestGuardianWithdrawStake_EventEmission(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardianAddr, deposit := registerWithdrawTestGuardian(t, f, msgServer, "event_test_guardian")

	// Clear any existing events
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithEventManager(sdk.NewEventManager())

	// Withdraw stake
	withdrawMsg := &types.MsgGuardianWithdrawStake{
		Guardian: guardianAddr.String(),
	}

	_, err := msgServer.GuardianWithdrawStake(f.ctx, withdrawMsg)
	require.NoError(t, err)

	// Check events
	events := sdk.UnwrapSDKContext(f.ctx).EventManager().Events()
	require.Len(t, events, 1)

	event := events[0]
	require.Equal(t, types.EventGuardianUpdated, event.Type)

	// Check event attributes
	attrs := event.Attributes
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value
	}

	require.Equal(t, guardianAddr.String(), attrMap["guardian"])
	require.Equal(t, "float_withdrawn", attrMap["action"])
	require.Equal(t, deposit.String(), attrMap["withdrawn"])
}
