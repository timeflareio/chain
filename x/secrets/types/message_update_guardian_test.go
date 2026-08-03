package types

import (
	gogotypes "github.com/cosmos/gogoproto/types"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgGuardianUpdate_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validDeposit := sdk.NewCoin("uveil", math.NewInt(5_000_000_000)) // 5000 VEIL

	tests := []struct {
		name      string
		msg       MsgGuardianUpdate
		expectErr bool
	}{
		{
			name: "valid update with all fields (except encryption key)",
			msg: MsgGuardianUpdate{
				Guardian: validGuardian,
				// EncryptionPublicKey removed - permanently immutable
				AvailableFrom:    100,
				AvailableUntil:   1000,
				Deposit:          &validDeposit,
				AcceptingSecrets: &gogotypes.BoolValue{Value: false},
			},
			expectErr: false,
		},
		{
			name: "valid update with only availability window",
			msg: MsgGuardianUpdate{
				Guardian:       validGuardian,
				AvailableFrom:  0, // Preserve existing
				AvailableUntil: 500,
			},
			expectErr: false,
		},
		{
			name: "valid update with only deposit top-up",
			msg: MsgGuardianUpdate{
				Guardian: validGuardian,
				Deposit:  &validDeposit,
			},
			expectErr: false,
		},
		// Note: Encryption key updates removed - keys are permanently immutable
		// Note: capacity updates removed with the max_capacity field (July 2026)

		// Guardian address validation
		{
			name: "invalid guardian address - empty",
			msg: MsgGuardianUpdate{
				Guardian: "",
			},
			expectErr: true,
		},
		{
			name: "invalid guardian address - malformed",
			msg: MsgGuardianUpdate{
				Guardian: "invalid_address",
			},
			expectErr: true,
		},
		{
			name: "invalid guardian address - wrong prefix",
			msg: MsgGuardianUpdate{
				Guardian: "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48abc123",
			},
			expectErr: true,
		},

		// No fields to update
		{
			name: "no fields specified for update",
			msg: MsgGuardianUpdate{
				Guardian: validGuardian,
				// No fields specified
			},
			expectErr: true,
		},

		// Note: Encryption key validation removed - field no longer exists

		// Stake validation
		{
			name: "invalid deposit - invalid coin",
			msg: MsgGuardianUpdate{
				Guardian: validGuardian,
				Deposit:  &sdk.Coin{Denom: "invalid!", Amount: math.NewInt(1000)}, // Invalid denom
			},
			expectErr: true,
		},
		{
			name: "invalid deposit - zero amount",
			msg: MsgGuardianUpdate{
				Guardian: validGuardian,
				Deposit:  &sdk.Coin{Denom: "uveil", Amount: math.ZeroInt()},
			},
			expectErr: true,
		},
		{
			name: "invalid deposit - negative amount",
			msg: MsgGuardianUpdate{
				Guardian: validGuardian,
				Deposit:  &sdk.Coin{Denom: "uveil", Amount: math.NewInt(-1000)},
			},
			expectErr: true,
		},
		{
			name: "invalid deposit - invalid denom",
			msg: MsgGuardianUpdate{
				Guardian: validGuardian,
				Deposit:  &sdk.Coin{Denom: "", Amount: math.NewInt(1000)},
			},
			expectErr: true,
		},

		// Availability window validation - basic format checks
		{
			name: "invalid available_from - negative",
			msg: MsgGuardianUpdate{
				Guardian:       validGuardian,
				AvailableFrom:  -1,
				AvailableUntil: 1000,
			},
			expectErr: false, // Basic validation doesn't check this - it's handled in server logic
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err, "Expected error for test case: %s", tc.name)
			} else {
				require.NoError(t, err, "Expected no error for test case: %s", tc.name)
			}
		})
	}
}

func TestMsgGuardianUpdate_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	msg := MsgGuardianUpdate{
		Guardian: guardian,
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(guardian)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

func TestMsgGuardianUpdate_GetSigners_InvalidAddress(t *testing.T) {
	msg := MsgGuardianUpdate{
		Guardian: "invalid_address",
	}

	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgGuardianUpdate_Type(t *testing.T) {
	msg := MsgGuardianUpdate{}
	require.Equal(t, TypeMsgGuardianUpdate, msg.Type())
}

func TestMsgGuardianUpdate_Route(t *testing.T) {
	msg := MsgGuardianUpdate{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgGuardianUpdate_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validDeposit := sdk.NewCoin("uveil", math.NewInt(10_000_000_000))
	msg := MsgGuardianUpdate{
		Guardian: "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
		// EncryptionPublicKey removed - permanently immutable
		AvailableFrom:    100,
		AvailableUntil:   1000,
		Deposit:          &validDeposit,
		AcceptingSecrets: &gogotypes.BoolValue{Value: true},
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}

func TestNewMsgGuardianUpdate(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	availableFrom := int64(100)
	availableUntil := int64(1000)
	deposit := sdk.NewCoin("uveil", math.NewInt(10_000_000_000))
	acceptingSecrets := true

	msg := NewMsgGuardianUpdate(guardian, availableFrom, availableUntil, &deposit, &acceptingSecrets)

	require.Equal(t, guardian, msg.Guardian)
	require.Equal(t, availableFrom, msg.AvailableFrom)
	require.Equal(t, availableUntil, msg.AvailableUntil)
	require.Equal(t, &deposit, msg.Deposit)
	require.Equal(t, acceptingSecrets, msg.AcceptingSecrets.Value)
}

func TestMsgGuardianUpdate_ValidateBasic_FieldDetection(t *testing.T) {
	// Test that the validation correctly detects when fields are provided vs not provided
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"

	// Test that zero values are properly handled
	tests := []struct {
		name        string
		msg         MsgGuardianUpdate
		expectErr   bool
		description string
	}{
		{
			name: "availability update alone should be valid",
			msg: MsgGuardianUpdate{
				Guardian:       validGuardian,
				AvailableUntil: 1000,
			},
			expectErr:   false,
			description: "a single updatable field should be a valid update",
		},
		{
			name: "AcceptingSecrets explicit false alone should be valid update",
			msg: MsgGuardianUpdate{
				Guardian:         validGuardian,
				AcceptingSecrets: &gogotypes.BoolValue{Value: false}, // Explicit false IS a valid update on its own
			},
			expectErr:   false,
			description: "presence-aware toggle to false alone should be a valid update",
		},
		{
			name: "AvailableFrom zero should be valid (preserve existing)",
			msg: MsgGuardianUpdate{
				Guardian:       validGuardian,
				AvailableFrom:  0,    // Zero means preserve existing
				AvailableUntil: 1000, // Must provide until if providing from
			},
			expectErr:   false,
			description: "AvailableFrom 0 should be valid (preserve existing)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err, tc.description)
			} else {
				require.NoError(t, err, tc.description)
			}
		})
	}
}
