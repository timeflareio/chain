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

func TestGuardianRegister_Success(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	testCases := []struct {
		name             string
		guardian         sdk.AccAddress
		encryptionKey    []byte
		availableFrom    int64
		availableUntil   int64
		stake            sdk.Coin
		acceptingSecrets bool
	}{
		{
			name:             "basic successful registration",
			guardian:         sdk.AccAddress([]byte("test_guardian_1")),
			encryptionKey:    getValidPublicKey("key1"),
			availableFrom:    10,
			availableUntil:   1000,
			stake:            sdk.NewCoin(types.DefaultDenom, testFloatUnit()),
			acceptingSecrets: true,
		},
		{
			name:             "registration with high stake",
			guardian:         sdk.AccAddress([]byte("test_guardian_2")),
			encryptionKey:    getValidPublicKey("key2"),
			availableFrom:    0, // Use default (current + 1)
			availableUntil:   5000,
			stake:            sdk.NewCoin(types.DefaultDenom, math.NewInt(20_000_000_000)), // 20k VEIL
			acceptingSecrets: true,
		},
		{
			name:             "registration with unlimited capacity",
			guardian:         sdk.AccAddress([]byte("test_guardian_3")),
			encryptionKey:    getValidPublicKey("key3"),
			availableFrom:    100,
			availableUntil:   2000,
			stake:            sdk.NewCoin(types.DefaultDenom, testFloatUnit()),
			acceptingSecrets: false,
		},
		{
			name:             "registration far in future",
			guardian:         sdk.AccAddress([]byte("test_guardian_4")),
			encryptionKey:    getValidPublicKey("key4"),
			availableFrom:    types.MaxAvailableFromOffset - 100,                           // Near max allowed
			availableUntil:   types.MaxAvailableFromOffset + types.MinAvailabilityWindow,   // Ensure positive duration
			stake:            sdk.NewCoin(types.DefaultDenom, math.NewInt(15_000_000_000)), // 15k VEIL
			acceptingSecrets: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &types.MsgGuardianRegister{
				Guardian:            tc.guardian.String(),
				EncryptionPublicKey: tc.encryptionKey,
				AvailableFrom:       tc.availableFrom,
				AvailableUntil:      tc.availableUntil,
				Deposit:             &tc.stake,
				AcceptingSecrets:    tc.acceptingSecrets,
			}

			resp, err := msgServer.GuardianRegister(f.ctx, msg)
			require.NoError(t, err)
			require.NotNil(t, resp)

			// Verify guardian was created
			guardian, found := f.keeper.GetGuardian(f.ctx, tc.guardian.String())
			require.True(t, found)
			require.Equal(t, tc.guardian.String(), guardian.Address)
			require.Equal(t, tc.encryptionKey, guardian.EncryptionPublicKey)
			require.Equal(t, tc.stake, *guardian.Stake)
			require.Equal(t, tc.acceptingSecrets, guardian.AcceptingSecrets)
		})
	}
}

func TestGuardianRegister_InvalidAddress(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

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
		{
			name:     "invalid checksum",
			guardian: "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48abc123",
			errorMsg: "invalid guardian address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &types.MsgGuardianRegister{
				Guardian:            tc.guardian,
				EncryptionPublicKey: getValidPublicKey("test_key"),
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validStake,
				AcceptingSecrets:    true,
			}

			_, err := msgServer.GuardianRegister(f.ctx, msg)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errorMsg)
		})
	}
}

func TestGuardianRegister_DuplicateRegistration(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardianAddr := sdk.AccAddress([]byte("duplicate_guardian"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	msg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("test_key"),
		AvailableFrom:       100,
		AvailableUntil:      1000,
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	// First registration should succeed
	resp, err := msgServer.GuardianRegister(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Second registration should fail
	_, err = msgServer.GuardianRegister(f.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "guardian already exists - use MsgGuardianUpdate to modify parameters")
}

func TestGuardianRegister_DepositValidation(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	t.Run("wrong denomination rejected", func(t *testing.T) {
		guardianAddr := sdk.AccAddress([]byte("deposit_test_denom"))
		bad := sdk.Coin{Denom: "uatom", Amount: testFloatUnit()}
		msg := &types.MsgGuardianRegister{
			Guardian:            guardianAddr.String(),
			EncryptionPublicKey: getValidPublicKey("test_key"),
			AvailableFrom:       100,
			AvailableUntil:      1000,
			Deposit:             &bad,
			AcceptingSecrets:    true,
		}

		_, err := msgServer.GuardianRegister(f.ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "deposit must be in uveil denomination")
	})

	// Under the bonded model there is no minimum float: the entry fee
	// gates registration, and per-secret bond checks gate acceptance. Nil and
	// zero deposits are therefore valid registrations.
	t.Run("nil deposit accepted", func(t *testing.T) {
		guardianAddr := sdk.AccAddress([]byte("deposit_test_nil"))
		msg := &types.MsgGuardianRegister{
			Guardian:            guardianAddr.String(),
			EncryptionPublicKey: getValidPublicKey("test_key"),
			AvailableFrom:       100,
			AvailableUntil:      1000,
			Deposit:             nil,
			AcceptingSecrets:    true,
		}

		_, err := msgServer.GuardianRegister(f.ctx, msg)
		require.NoError(t, err)

		guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
		require.True(t, found)
		require.True(t, guardian.Stake.IsZero(), "nil deposit must yield a zero float")
		require.True(t, guardian.LockedStake.IsZero())
	})

	t.Run("zero deposit accepted", func(t *testing.T) {
		guardianAddr := sdk.AccAddress([]byte("deposit_test_zero"))
		zero := sdk.NewCoin(types.DefaultDenom, math.ZeroInt())
		msg := &types.MsgGuardianRegister{
			Guardian:            guardianAddr.String(),
			EncryptionPublicKey: getValidPublicKey("test_key_zero_deposit"),
			AvailableFrom:       100,
			AvailableUntil:      1000,
			Deposit:             &zero,
			AcceptingSecrets:    true,
		}

		_, err := msgServer.GuardianRegister(f.ctx, msg)
		require.NoError(t, err)
	})
}

func TestGuardianRegister_EncryptionKeyValidation(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	testCases := []struct {
		name          string
		encryptionKey []byte
		errorMsg      string
	}{
		{
			name:          "empty key",
			encryptionKey: []byte{},
			errorMsg:      "encryption public key must be exactly 32 bytes",
		},
		{
			name:          "nil key",
			encryptionKey: nil,
			errorMsg:      "encryption public key must be exactly 32 bytes",
		},
		{
			name:          "key too short",
			encryptionKey: make([]byte, 31),
			errorMsg:      "encryption public key must be exactly 32 bytes",
		},
		{
			name:          "key too long",
			encryptionKey: make([]byte, 33),
			errorMsg:      "encryption public key must be exactly 32 bytes",
		},
		{
			name:          "all zeros key",
			encryptionKey: make([]byte, 32), // All zeros — the degenerate X25519 small-order point
			errorMsg:      "encryption public key is not a usable X25519 public key",
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			guardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("enc_key_test_%d", i)))

			msg := &types.MsgGuardianRegister{
				Guardian:            guardianAddr.String(),
				EncryptionPublicKey: tc.encryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validStake,
				AcceptingSecrets:    true,
			}

			_, err := msgServer.GuardianRegister(f.ctx, msg)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errorMsg)
		})
	}
}

func TestGuardianRegister_AvailabilityWindowValidation(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	testCases := []struct {
		name           string
		availableFrom  int64
		availableUntil int64
		errorMsg       string
	}{
		{
			name:           "negative available_from",
			availableFrom:  -1,
			availableUntil: 1000,
			errorMsg:       "available_from must be >= 0",
		},
		{
			name:           "available_from too far in future",
			availableFrom:  types.MaxAvailableFromOffset + 1,
			availableUntil: 1000,
			errorMsg:       "available_from too far in future",
		},
		{
			name:           "window too short",
			availableFrom:  100,
			availableUntil: types.MinAvailabilityWindow - 1,
			errorMsg:       "availability window too short",
		},
		{
			name:           "window too long",
			availableFrom:  100,
			availableUntil: 100 + types.MaxAvailabilityWindow + 1, // Duration > MaxAvailabilityWindow
			errorMsg:       "availability window too long",
		},
		{
			name:           "window too short",
			availableFrom:  0,
			availableUntil: 1, // Too short, below minimum window
			errorMsg:       "availability window too short",
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			guardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("window_test_%d", i)))

			msg := &types.MsgGuardianRegister{
				Guardian:            guardianAddr.String(),
				EncryptionPublicKey: getValidPublicKey("test_key"),
				AvailableFrom:       tc.availableFrom,
				AvailableUntil:      tc.availableUntil,
				Deposit:             &validStake,
				AcceptingSecrets:    true,
			}

			_, err := msgServer.GuardianRegister(f.ctx, msg)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errorMsg)
		})
	}
}

func TestGuardianRegister_SignerValidation(t *testing.T) {
	// f := initFixture(t)
	// msgServer := keeper.NewMsgServerImpl(f.keeper)

	// In our test setup, the GetSigners() returns the guardian address by default,
	// so this test will actually succeed. In production, the actual signer validation
	// happens at the transaction level, not in our keeper tests.
	// We'll skip this test as it requires integration testing setup.
	t.Skip("Signer validation requires integration testing setup with actual transaction signers")
}

func TestGuardianRegister_EventEmission(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardianAddr := sdk.AccAddress([]byte("event_test_guardian"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	// Clear any existing events
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
	f.ctx = sdkCtx

	msg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("test_key"),
		AvailableFrom:       100,
		AvailableUntil:      2000,
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	resp, err := msgServer.GuardianRegister(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Check events
	events := sdk.UnwrapSDKContext(f.ctx).EventManager().Events()
	require.Len(t, events, 1)

	event := events[0]
	require.Equal(t, types.EventGuardianRegistered, event.Type)

	// Check event attributes
	attrs := event.Attributes
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value
	}

	require.Equal(t, guardianAddr.String(), attrMap["guardian"])
	require.Equal(t, "registered", attrMap["action"])
	require.Equal(t, stake.String(), attrMap["float_deposit"])
	require.Equal(t, sdk.NewCoin(types.DefaultDenom, types.EntryFee()).String(), attrMap["entry_fee"])
	require.NotEmpty(t, attrMap["available_from"])
	require.NotEmpty(t, attrMap["available_until"])
}

func TestGuardianRegister_StateChanges(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardianAddr := sdk.AccAddress([]byte("state_test_guardian"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	msg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("test_key"),
		AvailableFrom:       100,
		AvailableUntil:      2000,
		Deposit:             &stake,
		AcceptingSecrets:    false,
	}

	// Verify guardian doesn't exist before
	_, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.False(t, found)

	// Register guardian
	resp, err := msgServer.GuardianRegister(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify guardian exists after registration
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)

	// Verify all fields are stored correctly
	require.Equal(t, guardianAddr.String(), guardian.Address)
	require.Equal(t, msg.EncryptionPublicKey, guardian.EncryptionPublicKey)
	require.Equal(t, currentBlock+100, guardian.AvailableFrom)   // Converted to absolute
	require.Equal(t, currentBlock+2000, guardian.AvailableUntil) // Converted to absolute: current_block + available_until_offset
	require.Equal(t, stake, *guardian.Stake)
	require.Equal(t, false, guardian.AcceptingSecrets)

	// TODO: Verify stake was transferred to module account
	// This would require mocking the bank keeper or using integration tests
}

func TestGuardianRegister_EdgeCases(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	testCases := []struct {
		name             string
		availableFrom    int64
		availableUntil   int64
		acceptingSecrets bool
		description      string
	}{
		{
			name:             "zero available_from uses default",
			availableFrom:    0,
			availableUntil:   1000,
			acceptingSecrets: true,
			description:      "available_from = 0 should use default offset",
		},
		{
			name:             "zero max_capacity means unlimited",
			availableFrom:    100,
			availableUntil:   1000,
			acceptingSecrets: true,
		},
		{
			name:             "minimum valid window",
			availableFrom:    1,
			availableUntil:   types.MinAvailabilityWindow + 1, // Duration = (current+101) - (current+1) = 100 blocks
			acceptingSecrets: false,
			description:      "should accept minimum valid availability window",
		},
		{
			name:             "maximum valid window",
			availableFrom:    1,
			availableUntil:   types.MaxAvailabilityWindow,
			acceptingSecrets: true,
			description:      "should accept maximum valid availability window",
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			guardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("edge_test_%d", i)))
			stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

			msg := &types.MsgGuardianRegister{
				Guardian:            guardianAddr.String(),
				EncryptionPublicKey: getValidPublicKey(fmt.Sprintf("key_%d", i)),
				AvailableFrom:       tc.availableFrom,
				AvailableUntil:      tc.availableUntil,
				Deposit:             &stake,
				AcceptingSecrets:    tc.acceptingSecrets,
			}

			resp, err := msgServer.GuardianRegister(f.ctx, msg)
			require.NoError(t, err, tc.description)
			require.NotNil(t, resp)

			// Verify guardian was created with correct values
			guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
			require.True(t, found)
			require.Equal(t, tc.acceptingSecrets, guardian.AcceptingSecrets)
		})
	}
}

func TestGuardianRegister_InsufficientFunds(t *testing.T) {
	// f := initFixture(t)
	// msgServer := keeper.NewMsgServerImpl(f.keeper)

	// This test would require a mock bank keeper that returns insufficient funds
	// For now, we'll skip this as it requires integration testing setup
	t.Skip("Requires mock bank keeper setup for insufficient funds testing")
}
