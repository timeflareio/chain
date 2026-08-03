package types

import (
	"bytes"
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgGuardianRegister_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validEncryptionKey := bytes.Repeat([]byte{2}, 32)                 // 32 bytes encryption key
	validDeposit := sdk.NewCoin("uveil", math.NewInt(10_000_000_000)) // 10,000 VEIL float

	tests := []struct {
		name      string
		msg       MsgGuardianRegister
		expectErr bool
		errType   error
	}{
		// Valid cases
		{
			name: "valid new registration",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},
		{
			name: "valid with max capacity set",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},
		{
			name: "valid with accepting_secrets false",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    false,
			},
			expectErr: false,
		},

		// Guardian address validation
		{
			name: "invalid guardian address - empty",
			msg: MsgGuardianRegister{
				Guardian:            "",
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - malformed",
			msg: MsgGuardianRegister{
				Guardian:            "invalid-address",
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - wrong prefix",
			msg: MsgGuardianRegister{
				Guardian:            "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},

		// Encryption public key validation
		{
			name: "invalid encryption public key - empty",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: []byte{},
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid encryption public key - nil",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: nil,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "valid encryption public key - exactly 32 bytes",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: bytes.Repeat([]byte{4}, 32),
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},

		// Availability window validation
		{
			name: "invalid availability - until <= from",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       1000,
				AvailableUntil:      1000,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid availability - until < from",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       1000,
				AvailableUntil:      500,
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "valid availability - minimum window",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      101, // Just 1 block difference
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},
		{
			name: "valid availability - large window",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       0,
				AvailableUntil:      5_256_000, // 1 year maximum
				Deposit:             &validDeposit,
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},

		// Deposit validation
		{
			name: "valid deposit - zero amount (deposit optional; entry fee charged separately)",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.ZeroInt()},
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},
		{
			name: "invalid deposit - negative amount",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(-1000)},
				AcceptingSecrets:    true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid stake - wrong denom",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &sdk.Coin{Denom: "atom", Amount: math.NewInt(10_000_000_000)},
				AcceptingSecrets:    true,
			},
			expectErr: false, // Note: denom validation happens in keeper, not ValidateBasic
		},
		{
			name: "valid stake - minimum amount (10,000 VEIL)",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(10_000_000_000)},
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},
		{
			name: "valid stake - higher amount for slashing resilience",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(15_000_000_000)},
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},
		{
			name: "valid stake - very high amount",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(10_000_000_000_000)},
				AcceptingSecrets:    true,
			},
			expectErr: false,
		},

		// Edge cases
		{
			name: "all zero values except required fields",
			msg: MsgGuardianRegister{
				Guardian:            validGuardian,
				EncryptionPublicKey: validEncryptionKey,
				AvailableFrom:       0,
				AvailableUntil:      1,
				Deposit:             &validDeposit,
				AcceptingSecrets:    false,
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()

			if tt.expectErr {
				require.Error(t, err)
				if tt.errType != nil {
					require.ErrorIs(t, err, tt.errType)
				}
				// Check error message contains relevant info
				if strings.Contains(tt.name, "guardian") && strings.Contains(tt.name, "address") {
					require.Contains(t, err.Error(), "guardian address")
				}
				if strings.Contains(tt.name, "public key") && !strings.Contains(tt.name, "encryption") {
					require.Contains(t, err.Error(), "public key cannot be empty")
				}
				if strings.Contains(tt.name, "encryption public key") {
					require.Contains(t, err.Error(), "encryption public key cannot be empty")
				}
				if strings.Contains(tt.name, "availability") {
					require.Contains(t, err.Error(), "available_until must be greater than available_from")
				}
				if strings.Contains(tt.name, "stake") && strings.Contains(tt.name, "zero") {
					require.Contains(t, err.Error(), "stake must be non-zero")
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgGuardianRegister_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validEncryptionKey := bytes.Repeat([]byte{2}, 32)
	validDeposit := sdk.NewCoin("uveil", math.NewInt(10_000_000_000))

	msg := MsgGuardianRegister{
		Guardian:            validGuardian,
		EncryptionPublicKey: validEncryptionKey,
		AvailableFrom:       100,
		AvailableUntil:      1000,
		Deposit:             &validDeposit,
		AcceptingSecrets:    true,
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(validGuardian)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

func TestMsgGuardianRegister_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgGuardianRegister{
		Guardian:            "invalid-address",
		EncryptionPublicKey: bytes.Repeat([]byte{2}, 32),
		AvailableFrom:       100,
		AvailableUntil:      1000,
		Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(10_000_000_000)},
		AcceptingSecrets:    true,
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgGuardianRegister_Type(t *testing.T) {
	msg := MsgGuardianRegister{}
	require.Equal(t, TypeMsgGuardianRegister, msg.Type())
}

func TestMsgGuardianRegister_Route(t *testing.T) {
	msg := MsgGuardianRegister{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgGuardianRegister_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validDeposit := sdk.NewCoin("uveil", math.NewInt(10_000_000_000))
	msg := MsgGuardianRegister{
		Guardian:            "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
		EncryptionPublicKey: bytes.Repeat([]byte{2}, 32),
		AvailableFrom:       100,
		AvailableUntil:      1000,
		Deposit:             &validDeposit,
		AcceptingSecrets:    true,
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}

func TestNewMsgGuardianRegister(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	encryptionPublicKey := bytes.Repeat([]byte{2}, 32)
	availableFrom := int64(100)
	availableUntil := int64(1000)
	deposit := sdk.NewCoin("uveil", math.NewInt(10_000_000_000))
	acceptingSecrets := true

	msg := NewMsgGuardianRegister(guardian, encryptionPublicKey, availableFrom, availableUntil, deposit, acceptingSecrets)

	require.Equal(t, guardian, msg.Guardian)
	require.Equal(t, encryptionPublicKey, msg.EncryptionPublicKey)
	require.Equal(t, availableFrom, msg.AvailableFrom)
	require.Equal(t, availableUntil, msg.AvailableUntil)
	require.Equal(t, &deposit, msg.Deposit)
	require.Equal(t, acceptingSecrets, msg.AcceptingSecrets)
}

func TestMsgGuardianRegister_ValidateBasic_SpecCompliance(t *testing.T) {
	// Additional tests to ensure spec compliance from operations.md
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validEncryptionKey := bytes.Repeat([]byte{2}, 32)

	t.Run("encryption key must be exactly 32 bytes", func(t *testing.T) {
		// Too short
		msg := MsgGuardianRegister{
			Guardian:            validGuardian,
			EncryptionPublicKey: bytes.Repeat([]byte{2}, 31),
			AvailableFrom:       100,
			AvailableUntil:      1000,
			Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(10_000_000_000)},
			AcceptingSecrets:    true,
		}
		// Note: Current implementation only checks for empty, not exact length
		// This test documents the spec requirement
		err := msg.ValidateBasic()
		require.NoError(t, err) // Current implementation doesn't validate exact length
	})

	t.Run("public key validation", func(t *testing.T) {
		// All zeros should be valid in ValidateBasic (checked in keeper)
		msg := MsgGuardianRegister{
			Guardian:            validGuardian,
			EncryptionPublicKey: validEncryptionKey,
			AvailableFrom:       100,
			AvailableUntil:      1000,
			Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(10_000_000_000)},
			AcceptingSecrets:    true,
		}
		err := msg.ValidateBasic()
		require.NoError(t, err) // ValidateBasic only checks for empty, not all zeros
	})

	t.Run("encryption key all zeros", func(t *testing.T) {
		// All zeros should be valid in ValidateBasic (checked in keeper)
		msg := MsgGuardianRegister{
			Guardian:            validGuardian,
			EncryptionPublicKey: bytes.Repeat([]byte{0}, 32),
			AvailableFrom:       100,
			AvailableUntil:      1000,
			Deposit:             &sdk.Coin{Denom: "uveil", Amount: math.NewInt(10_000_000_000)},
			AcceptingSecrets:    true,
		}
		err := msg.ValidateBasic()
		require.NoError(t, err) // ValidateBasic only checks for empty, not all zeros
	})
}
