package types

import (
	"bytes"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgGuardianWithdrawStake_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	// Generate valid test address
	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	validGuardian := guardianAddr.String()

	tests := []struct {
		name      string
		msg       MsgGuardianWithdrawStake
		expectErr bool
		errType   error
	}{
		// Valid cases
		{
			name: "valid withdraw stake request",
			msg: MsgGuardianWithdrawStake{
				Guardian: validGuardian,
			},
			expectErr: false,
		},

		// Guardian address validation
		{
			name: "invalid guardian address - empty",
			msg: MsgGuardianWithdrawStake{
				Guardian: "",
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - malformed",
			msg: MsgGuardianWithdrawStake{
				Guardian: "invalid-address",
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - wrong prefix",
			msg: MsgGuardianWithdrawStake{
				Guardian: "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - invalid checksum",
			msg: MsgGuardianWithdrawStake{
				Guardian: "tmflr1invalidchecksum",
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},

		// Edge cases
		{
			name: "valid guardian address - different format",
			msg: MsgGuardianWithdrawStake{
				Guardian: sdk.AccAddress(bytes.Repeat([]byte{255}, 20)).String(),
			},
			expectErr: false,
		},
		{
			name: "valid guardian address - typical format",
			msg: MsgGuardianWithdrawStake{
				Guardian: validGuardian,
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
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgGuardianWithdrawStake_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	validGuardian := guardianAddr.String()

	msg := MsgGuardianWithdrawStake{
		Guardian: validGuardian,
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)
	require.Equal(t, guardianAddr, signers[0])
}

func TestMsgGuardianWithdrawStake_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgGuardianWithdrawStake{
		Guardian: "invalid-address",
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgGuardianWithdrawStake_Type(t *testing.T) {
	msg := MsgGuardianWithdrawStake{}
	require.Equal(t, TypeMsgGuardianWithdrawStake, msg.Type())
}

func TestMsgGuardianWithdrawStake_Route(t *testing.T) {
	msg := MsgGuardianWithdrawStake{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgGuardianWithdrawStake_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	msg := MsgGuardianWithdrawStake{
		Guardian: guardianAddr.String(),
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}

func TestMsgGuardianWithdrawStake_ValidateBasic_SpecCompliance(t *testing.T) {
	// Test spec requirements from operations.md
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	validGuardian := guardianAddr.String()

	t.Run("withdrawal conditions - spec requirements", func(t *testing.T) {
		// ValidateBasic only checks address format
		// Actual withdrawal conditions (expired availability, no active commitments, clean exit)
		// are checked in the keeper, not ValidateBasic
		msg := MsgGuardianWithdrawStake{
			Guardian: validGuardian,
		}
		err := msg.ValidateBasic()
		require.NoError(t, err)
	})

	t.Run("guardian address must be valid bech32 with tmflr prefix", func(t *testing.T) {
		// Test correct prefix requirement
		msg := MsgGuardianWithdrawStake{
			Guardian: validGuardian,
		}
		err := msg.ValidateBasic()
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(validGuardian, "tmflr1"))

		// Test wrong prefix fails
		msg.Guardian = "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
		err = msg.ValidateBasic()
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid guardian address")
	})

	t.Run("only guardian can withdraw their own stake", func(t *testing.T) {
		// The signer must be the guardian address (checked by GetSigners)
		msg := MsgGuardianWithdrawStake{
			Guardian: validGuardian,
		}
		signers := msg.GetSigners()
		require.Len(t, signers, 1)
		require.Equal(t, guardianAddr, signers[0])

		// This ensures only the guardian can sign their own withdrawal
		expectedAddr, err := sdk.AccAddressFromBech32(validGuardian)
		require.NoError(t, err)
		require.Equal(t, expectedAddr, signers[0])
	})
}
