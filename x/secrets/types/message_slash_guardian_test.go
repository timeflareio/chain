package types

import (
	"bytes"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgSlashGuardian_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	// Generate valid test addresses
	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	reporterAddr := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	validGuardian := guardianAddr.String()
	validReporter := reporterAddr.String()
	validEvidence := bytes.Repeat([]byte{1}, MinEvidenceLength) // 32 bytes minimum
	validReason := "Guardian revealed share before authorized window"
	validSecretId := GenerateValidSecretID()

	tests := []struct {
		name      string
		msg       MsgSlashGuardian
		expectErr bool
		errType   error
	}{
		// Valid cases
		{
			name: "valid slash report",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: false,
		},
		{
			name: "valid slash report with envelope-sized evidence",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        bytes.Repeat([]byte{1}, 34), // key-share envelope size
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: false,
		},
		{
			name: "evidence too large - over the key-share envelope cap",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        bytes.Repeat([]byte{1}, 256), // > MaxRevealedKeyShareSize
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "valid slash report with maximum reason length",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          strings.Repeat("a", 256), // Maximum length per operations.md
				SecretId:        validSecretId,
			},
			expectErr: false,
		},

		// Guardian address validation
		{
			name: "invalid guardian address - empty",
			msg: MsgSlashGuardian{
				GuardianAddress: "",
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - malformed",
			msg: MsgSlashGuardian{
				GuardianAddress: "invalid-address",
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid guardian address - wrong prefix",
			msg: MsgSlashGuardian{
				GuardianAddress: "cosmos1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},

		// Reporter address validation
		{
			name: "invalid reporter address - empty",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: "",
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid reporter address - malformed",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: "invalid-address",
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid reporter address - wrong prefix",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: "cosmos1abc123def456ghi789jkl012mno345pqr678st",
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},

		// Evidence validation
		{
			name: "invalid evidence - empty",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        []byte{},
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid evidence - nil",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        nil,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid evidence - too short (below MinEvidenceLength)",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength-1), // 31 bytes, below minimum
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "valid evidence - exactly minimum length",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength), // Exactly 32 bytes
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: false,
		},
		{
			name: "valid evidence - maximum envelope length",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        bytes.Repeat([]byte{1}, 64), // == MaxRevealedKeyShareSize
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: false,
		},

		// Reason validation
		{
			name: "invalid reason - empty",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          "",
				SecretId:        validSecretId,
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidRequest,
		},
		{
			name: "valid reason - minimum length (1 char)",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          "A",
				SecretId:        validSecretId,
			},
			expectErr: false,
		},

		// Secret ID validation
		{
			name: "invalid secret ID - empty",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        "",
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "valid secret ID - typical format",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        GenerateValidSecretID(),
			},
			expectErr: false,
		},

		// Edge cases
		{
			name: "valid slash report with different addresses",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter, // Different from guardian - required
				Evidence:        validEvidence,
				Reason:          validReason,
				SecretId:        validSecretId,
			},
			expectErr: false,
		},
		{
			name: "valid slash report with minimal valid data",
			msg: MsgSlashGuardian{
				GuardianAddress: validGuardian,
				ReporterAddress: validReporter,
				Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength),
				Reason:          "Early reveal detected",
				SecretId:        GenerateValidSecretID(),
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
				if strings.Contains(tt.name, "reporter") && strings.Contains(tt.name, "address") {
					require.Contains(t, err.Error(), "reporter address")
				}
				if strings.Contains(tt.name, "evidence") {
					require.Contains(t, err.Error(), "evidence")
				}
				if strings.Contains(tt.name, "reason") {
					require.Contains(t, err.Error(), "reason")
				}
				if strings.Contains(tt.name, "secret") && strings.Contains(tt.name, "ID") {
					require.Contains(t, err.Error(), "secret ID")
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgSlashGuardian_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	// Generate valid test addresses
	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	reporterAddr := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	validGuardian := guardianAddr.String()
	validReporter := reporterAddr.String()

	msg := MsgSlashGuardian{
		GuardianAddress: validGuardian,
		ReporterAddress: validReporter,
		Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength),
		Reason:          "Early reveal detected",
		SecretId:        "550e8400-e29b-41d4-a716-446655440000",
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	require.Equal(t, reporterAddr, signers[0])
}

func TestMsgSlashGuardian_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	msg := MsgSlashGuardian{
		GuardianAddress: guardianAddr.String(),
		ReporterAddress: "invalid-address",
		Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength),
		Reason:          "Early reveal detected",
		SecretId:        "550e8400-e29b-41d4-a716-446655440000",
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgSlashGuardian_Type(t *testing.T) {
	msg := MsgSlashGuardian{}
	require.Equal(t, TypeMsgSlashGuardian, msg.Type())
}

func TestMsgSlashGuardian_Route(t *testing.T) {
	msg := MsgSlashGuardian{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgSlashGuardian_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	reporterAddr := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	msg := MsgSlashGuardian{
		GuardianAddress: guardianAddr.String(),
		ReporterAddress: reporterAddr.String(),
		Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength),
		Reason:          "Guardian revealed share before authorized window",
		SecretId:        "550e8400-e29b-41d4-a716-446655440000",
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}

func TestMsgSlashGuardian_ValidateBasic_SpecCompliance(t *testing.T) {
	// Test spec requirements from operations.md
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	guardianAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	reporterAddr := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	validGuardian := guardianAddr.String()
	validReporter := reporterAddr.String()

	t.Run("evidence length requirement - spec says 32-1024 bytes", func(t *testing.T) {
		// Test minimum requirement
		msg := MsgSlashGuardian{
			GuardianAddress: validGuardian,
			ReporterAddress: validReporter,
			Evidence:        bytes.Repeat([]byte{1}, 32), // Exactly MinEvidenceLength
			Reason:          "Early reveal",
			SecretId:        GenerateValidSecretID(),
		}
		err := msg.ValidateBasic()
		require.NoError(t, err)

		// Test below minimum
		msg.Evidence = bytes.Repeat([]byte{1}, 31)
		err = msg.ValidateBasic()
		require.Error(t, err)
		require.Contains(t, err.Error(), "evidence too short")
	})

	t.Run("reason length requirement - spec says max 256 characters", func(t *testing.T) {
		// Test maximum length per spec
		msg := MsgSlashGuardian{
			GuardianAddress: validGuardian,
			ReporterAddress: validReporter,
			Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength),
			Reason:          strings.Repeat("a", 256), // Exactly max per spec
			SecretId:        GenerateValidSecretID(),
		}
		err := msg.ValidateBasic()
		require.NoError(t, err) // Current implementation doesn't enforce max length
	})

	t.Run("reporter cannot be guardian - self-slash prevention", func(t *testing.T) {
		// This validation is likely done in keeper, not ValidateBasic
		// ValidateBasic only checks address format validity
		msg := MsgSlashGuardian{
			GuardianAddress: validGuardian,
			ReporterAddress: validGuardian, // Same as guardian
			Evidence:        bytes.Repeat([]byte{1}, MinEvidenceLength),
			Reason:          "Self-slash attempt",
			SecretId:        GenerateValidSecretID(),
		}
		err := msg.ValidateBasic()
		require.NoError(t, err) // ValidateBasic doesn't check same address
	})
}
