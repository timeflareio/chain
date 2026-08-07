package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

// Test constants

func TestMsgUserRequestGuardians_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validCreator := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validHint := DetectionHint{
		Version:      DetectionHintVersion,
		EphemeralPub: make([]byte, PublicKeyLength),
		Tag:          make([]byte, DetectionTagLength),
	}

	tests := []struct {
		name      string
		msg       MsgUserRequestGuardians
		expectErr bool
		errType   error
	}{
		{
			name: "valid message",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: false,
		},
		{
			name: "invalid creator address - empty",
			msg: MsgUserRequestGuardians{
				Creator:           "",
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid creator address - malformed",
			msg: MsgUserRequestGuardians{
				Creator:           "invalid-address",
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid detection hint - unsupported version",
			msg: MsgUserRequestGuardians{
				Creator: validCreator,
				DetectionHint: DetectionHint{
					Version:      2,
					EphemeralPub: make([]byte, PublicKeyLength),
					Tag:          make([]byte, DetectionTagLength),
				},
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid detection hint - ephemeral key wrong length",
			msg: MsgUserRequestGuardians{
				Creator: validCreator,
				DetectionHint: DetectionHint{
					Version:      DetectionHintVersion,
					EphemeralPub: make([]byte, 16),
					Tag:          make([]byte, DetectionTagLength),
				},
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid detection hint - tag wrong length",
			msg: MsgUserRequestGuardians{
				Creator: validCreator,
				DetectionHint: DetectionHint{
					Version:      DetectionHintVersion,
					EphemeralPub: make([]byte, PublicKeyLength),
					Tag:          make([]byte, 4),
				},
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid detection hint - empty",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     DetectionHint{},
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid reveal window - start offset too small",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal - 1,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid reveal window - start offset too large",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MaxRevealStartOffset + 1,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid threshold - too small",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         MinThreshold - 1,
				MinShares:         5,
				MaxShares:         5,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid threshold - too large",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         MaxThreshold + 1,
				MinShares:         17,
				MaxShares:         17,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid band - min below shares floor",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         MinShares - 1,
				MaxShares:         3,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid band - max above the 32 cap",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         31,
				MaxShares:         MaxTotalShares + 1,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid band - min below threshold",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         5,
				MinShares:         4,
				MaxShares:         6,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid band - inverted (max below min)",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         5,
				MinShares:         6,
				MaxShares:         5,
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "valid band - max at the 32 cap",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         16,
				MinShares:         17,
				MaxShares:         MaxTotalShares,
				Bump:              MinBump,
			},
			expectErr: false,
		},
		{
			name: "valid band - zero width (exactly this many)",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         2,
				MinShares:         2,
				MaxShares:         2,
				Bump:              MinBump,
			},
			expectErr: false,
		},
		{
			name: "invalid band - gap equal to threshold (strict bound)",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         8, // gap 3 == threshold — must be strictly below
				Bump:              MinBump,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid bump - below minimum (1.00)",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MinBump - 1,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid bump - zero (unset)",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              0,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid bump - above max tier (10.00)",
			msg: MsgUserRequestGuardians{
				Creator:           validCreator,
				DetectionHint:     validHint,
				RevealStartOffset: MinRevealStartOffsetTotal,
				Threshold:         3,
				MinShares:         5,
				MaxShares:         7,
				Bump:              MaxBump + 1,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
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

func TestMsgUserRequestGuardians_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validCreator := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"

	msg := MsgUserRequestGuardians{
		Creator: validCreator,
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(validCreator)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

func TestMsgUserRequestGuardians_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgUserRequestGuardians{
		Creator: "invalid-address",
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgUserRequestGuardians_Type(t *testing.T) {
	msg := MsgUserRequestGuardians{}
	require.Equal(t, TypeMsgUserRequestGuardians, msg.Type())
}

func TestMsgUserRequestGuardians_Route(t *testing.T) {
	msg := MsgUserRequestGuardians{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgUserRequestGuardians_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgUserRequestGuardians{
		Creator:       "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
		DetectionHint: DetectionHint{Version: DetectionHintVersion, EphemeralPub: make([]byte, PublicKeyLength), Tag: make([]byte, DetectionTagLength)},
		Threshold:     3,
		MinShares:     15,
		MaxShares:     17,
		Bump:          MinBump,
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}
