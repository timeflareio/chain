package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgGuardianRevealShare_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validSecretId := GenerateValidSecretID()
	validDecryptedShare := []byte("valid_share_data")

	tests := []struct {
		name      string
		msg       MsgGuardianRevealShare
		expectErr bool
		errType   error
	}{
		{
			name: "valid message",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       validSecretId,
				DecryptedShare: validDecryptedShare,
			},
			expectErr: false,
		},
		{
			name: "invalid guardian address - empty",
			msg: MsgGuardianRevealShare{
				Guardian:       "",
				SecretId:       validSecretId,
				DecryptedShare: validDecryptedShare,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - malformed",
			msg: MsgGuardianRevealShare{
				Guardian:       "invalid-address",
				SecretId:       validSecretId,
				DecryptedShare: validDecryptedShare,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - wrong prefix",
			msg: MsgGuardianRevealShare{
				Guardian:       "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
				SecretId:       validSecretId,
				DecryptedShare: validDecryptedShare,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid secret ID - not UUID format",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       "not-a-uuid-format",
				DecryptedShare: validDecryptedShare,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "invalid secret ID - wrong length",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       strings.Repeat("a", 65), // 65 characters
				DecryptedShare: validDecryptedShare,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "invalid secret ID - empty",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       "",
				DecryptedShare: validDecryptedShare,
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "valid secret ID - proper UUID format",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       "f47ac10b-58cc-4372-a567-0e02b2c3d479",
				DecryptedShare: validDecryptedShare,
			},
			expectErr: false,
		},
		// ShareIndex field removed - SSS handles intrinsic IDs
		{
			name: "invalid decrypted share - empty",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       validSecretId,
				DecryptedShare: []byte{},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid decrypted share - nil",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       validSecretId,
				DecryptedShare: nil,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "valid decrypted share - minimum size (1 byte)",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       validSecretId,
				DecryptedShare: []byte{1},
			},
			expectErr: false,
		},
		{
			name: "valid decrypted share - maximum size (1024 bytes)",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       validSecretId,
				DecryptedShare: make([]byte, 1024),
			},
			expectErr: false,
		},
		{
			name: "valid decrypted share - typical SSS share",
			msg: MsgGuardianRevealShare{
				Guardian:       validGuardian,
				SecretId:       validSecretId,
				DecryptedShare: []byte("test_share_data"),
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

func TestMsgGuardianRevealShare_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validGuardian := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"

	msg := MsgGuardianRevealShare{
		Guardian:       validGuardian,
		SecretId:       GenerateValidSecretID(),
		DecryptedShare: []byte("test-share"),
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(validGuardian)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

func TestMsgGuardianRevealShare_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgGuardianRevealShare{
		Guardian: "invalid-address",
		SecretId: GenerateValidSecretID(),
		// ShareIndex removed
		DecryptedShare: []byte("test-share"),
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgGuardianRevealShare_Type(t *testing.T) {
	msg := MsgGuardianRevealShare{}
	require.Equal(t, TypeMsgGuardianRevealShare, msg.Type())
}

func TestMsgGuardianRevealShare_Route(t *testing.T) {
	msg := MsgGuardianRevealShare{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgGuardianRevealShare_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgGuardianRevealShare{
		Guardian: "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
		SecretId: GenerateValidSecretID(),
		// ShareIndex removed
		DecryptedShare: []byte("test-share"),
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}
