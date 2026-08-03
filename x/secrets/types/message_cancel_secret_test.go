package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgUserCancelSecret_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validCreator := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validSecretId := GenerateValidSecretID()

	tests := []struct {
		name      string
		msg       MsgUserCancelSecret
		expectErr bool
		errType   error
	}{
		{
			name: "valid message",
			msg: MsgUserCancelSecret{
				SecretId: validSecretId,
				Creator:  validCreator,
			},
			expectErr: false,
		},
		{
			name: "invalid sender address - empty",
			msg: MsgUserCancelSecret{
				SecretId: validSecretId,
				Creator:  "",
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid sender address - malformed",
			msg: MsgUserCancelSecret{
				SecretId: validSecretId,
				Creator:  "invalid-address",
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid sender address - wrong prefix",
			msg: MsgUserCancelSecret{
				SecretId: validSecretId,
				Creator:  "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid secret ID - too short",
			msg: MsgUserCancelSecret{
				SecretId: "short",
				Creator:  validCreator,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "invalid secret ID - wrong length",
			msg: MsgUserCancelSecret{
				SecretId: strings.Repeat("a", 65), // 65 characters
				Creator:  validCreator,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "invalid secret ID - empty",
			msg: MsgUserCancelSecret{
				SecretId: "",
				Creator:  validCreator,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "valid secret ID - proper UUID format",
			msg: MsgUserCancelSecret{
				SecretId: GenerateValidSecretID(),
				Creator:  validCreator,
			},
			expectErr: false,
		},
		{
			name: "valid message - typical UUID format",
			msg: MsgUserCancelSecret{
				SecretId: GenerateValidSecretID(),
				Creator:  validCreator,
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

func TestMsgUserCancelSecret_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validCreator := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"

	msg := MsgUserCancelSecret{
		SecretId: GenerateValidSecretID(),
		Creator:  validCreator,
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(validCreator)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

func TestMsgUserCancelSecret_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgUserCancelSecret{
		SecretId: GenerateValidSecretID(),
		Creator:  "invalid-address",
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgUserCancelSecret_Type(t *testing.T) {
	msg := MsgUserCancelSecret{}
	require.Equal(t, TypeMsgUserCancelSecret, msg.Type())
}

func TestMsgUserCancelSecret_Route(t *testing.T) {
	msg := MsgUserCancelSecret{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgUserCancelSecret_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgUserCancelSecret{
		SecretId: GenerateValidSecretID(),
		Creator:  "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}
