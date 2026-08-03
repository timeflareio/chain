package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgGuardianConfirmShares_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validSecretId := GenerateValidSecretID()

	tests := []struct {
		name      string
		msg       MsgGuardianConfirmShares
		expectErr bool
		errType   error
	}{
		{
			name: "valid message - accept true",
			msg: MsgGuardianConfirmShares{
				Guardian: validGuardian,
				SecretId: validSecretId,
				Accept:   true,
			},
			expectErr: false,
		},
		{
			name: "valid message - accept false",
			msg: MsgGuardianConfirmShares{
				Guardian: validGuardian,
				SecretId: validSecretId,
				Accept:   false,
			},
			expectErr: false,
		},
		{
			name: "invalid guardian address - empty",
			msg: MsgGuardianConfirmShares{
				Guardian: "",
				SecretId: validSecretId,
				Accept:   true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - malformed",
			msg: MsgGuardianConfirmShares{
				Guardian: "invalid-address",
				SecretId: validSecretId,
				Accept:   true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - wrong prefix",
			msg: MsgGuardianConfirmShares{
				Guardian: "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
				SecretId: validSecretId,
				Accept:   true,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid secret ID - too short",
			msg: MsgGuardianConfirmShares{
				Guardian: validGuardian,
				SecretId: "short",
				Accept:   true,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "invalid secret ID - wrong length",
			msg: MsgGuardianConfirmShares{
				Guardian: validGuardian,
				SecretId: strings.Repeat("a", 65), // 65 characters
				Accept:   true,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "invalid secret ID - empty",
			msg: MsgGuardianConfirmShares{
				Guardian: validGuardian,
				SecretId: "",
				Accept:   true,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "valid secret ID - proper UUID format",
			msg: MsgGuardianConfirmShares{
				Guardian: validGuardian,
				SecretId: GenerateValidSecretID(),
				Accept:   true,
			},
			expectErr: false,
		},
		// ShareIndex field removed - SSS handles intrinsic IDs
		{
			name: "valid message - typical UUID format secret ID",
			msg: MsgGuardianConfirmShares{
				Guardian: validGuardian,
				SecretId: GenerateValidSecretID(),
				Accept:   true,
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
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgGuardianConfirmShares_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"

	msg := MsgGuardianConfirmShares{
		Guardian: validGuardian,
		SecretId: GenerateValidSecretID(),
		// ShareIndex removed
		Accept: true,
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(validGuardian)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

func TestMsgGuardianConfirmShares_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgGuardianConfirmShares{
		Guardian: "invalid-address",
		SecretId: GenerateValidSecretID(),
		// ShareIndex removed
		Accept: true,
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgGuardianConfirmShares_Type(t *testing.T) {
	msg := MsgGuardianConfirmShares{}
	require.Equal(t, TypeMsgGuardianConfirmShares, msg.Type())
}

func TestMsgGuardianConfirmShares_Route(t *testing.T) {
	msg := MsgGuardianConfirmShares{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgGuardianConfirmShares_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgGuardianConfirmShares{
		Guardian: "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
		SecretId: "550e8400-e29b-41d4-a716-446655440000",
		// ShareIndex removed
		Accept: true,
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}
