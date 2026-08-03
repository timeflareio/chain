package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

func TestMsgUserDistributeShares_ValidateBasic(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validCreator := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"
	validGuardian := "tmflr1va6kzunyd9skuv2lv9jxgun9wde47h6lgservz"
	validGuardian2 := "tmflr1va6kzunyd9skuvjlv9jxgun9wde47h6lt5jyd8"
	validGuardian3 := "tmflr1va6kzunyd9skuv6lv9jxgun9wde47h6l20v74y"
	validSecretId := GenerateValidSecretID()
	validCommitment := make([]byte, 32)   // 32 bytes
	validPayload := make([]byte, 256)     // payload ciphertext C (opaque to the chain)
	validSecretPubKey := make([]byte, 32) // pk_s
	validEncryptedShare := make([]byte, 64)
	validHMAC := make([]byte, 32) // SHA256 HMAC

	validShare := &EncryptedShareData{
		GuardianAddress: validGuardian,
		EncryptedShare:  validEncryptedShare,
		ShareHmac:       validHMAC,
	}

	tests := []struct {
		name      string
		msg       MsgUserDistributeShares
		expectErr bool
		errType   error
	}{
		{
			name: "valid message",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: false,
		},
		{
			name: "invalid creator address - empty",
			msg: MsgUserDistributeShares{
				Creator:           "",
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid creator address - malformed",
			msg: MsgUserDistributeShares{
				Creator:           "invalid-address",
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid secret ID - not UUID format",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          "not-a-uuid-format",
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "invalid secret ID - wrong length",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          strings.Repeat("a", 65), // 65 characters
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidSecretID,
		},
		{
			name: "valid secret ID - proper UUID format",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          "f47ac10b-58cc-4372-a567-0e02b2c3d479",
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: false,
		},
		{
			name: "invalid secret commitment - empty",
			msg: MsgUserDistributeShares{
				Creator:          validCreator,
				SecretId:         validSecretId,
				SecretCommitment: []byte{},
				Shares:           []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid secret commitment - nil",
			msg: MsgUserDistributeShares{
				Creator:          validCreator,
				SecretId:         validSecretId,
				SecretCommitment: nil,
				Shares:           []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid secret commitment - too large",
			msg: MsgUserDistributeShares{
				Creator:          validCreator,
				SecretId:         validSecretId,
				SecretCommitment: make([]byte, 1025), // > 1024 bytes
				Shares:           []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "valid secret commitment - maximum size (1024 bytes)",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  make([]byte, 1024),
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: false,
		},
		{
			name: "invalid payload ciphertext - empty",
			msg: MsgUserDistributeShares{
				Creator:          validCreator,
				SecretId:         validSecretId,
				SecretCommitment: validCommitment,
				SecretPublicKey:  validSecretPubKey,
				Shares:           []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid payload ciphertext - over MaxPayloadSize",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: make([]byte, MaxPayloadSize+1),
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid secret public key - wrong length",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   make([]byte, 31),
				Shares:            []*EncryptedShareData{validShare},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid shares - empty array",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid shares - nil array",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            nil,
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid shares - nil share in array",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares:            []*EncryptedShareData{nil},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid share - empty guardian address",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: "",

						EncryptedShare: validEncryptedShare,
						ShareHmac:      validHMAC,
					},
				},
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid share - malformed guardian address",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: "invalid-address",

						EncryptedShare: validEncryptedShare,
						ShareHmac:      validHMAC,
					},
				},
			},
			expectErr: true,
			errType:   sdkerrors.ErrInvalidAddress,
		},
		{
			name: "valid share - all required fields present",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,
						EncryptedShare:  validEncryptedShare,
						ShareHmac:       validHMAC,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "invalid share - empty encrypted share",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: []byte{},
						ShareHmac:      validHMAC,
					},
				},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid share - nil encrypted share",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: nil,
						ShareHmac:      validHMAC,
					},
				},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid share - encrypted share too large",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: make([]byte, MaxKeyShareSize+1), // > MaxKeyShareSize bytes
						ShareHmac:      validHMAC,
					},
				},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "valid share - maximum encrypted share size",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: make([]byte, MaxKeyShareSize),
						ShareHmac:      validHMAC,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "invalid share - empty HMAC",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      []byte{},
					},
				},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid share - nil HMAC",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      nil,
					},
				},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid share - HMAC wrong length (too short)",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      make([]byte, 16), // < 32 bytes
					},
				},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "invalid share - HMAC wrong length (too long)",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      make([]byte, 64), // > 32 bytes
					},
				},
			},
			expectErr: true,
			errType:   ErrInvalidRequest,
		},
		{
			name: "valid shares - different guardians with matching assignments",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      validHMAC,
					},
					{
						GuardianAddress: validGuardian2,
						EncryptedShare:  validEncryptedShare,
						ShareHmac:       validHMAC,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "valid shares - multiple valid shares",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: []*EncryptedShareData{
					{
						GuardianAddress: validGuardian,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      validHMAC,
					},
					{
						GuardianAddress: validGuardian2,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      validHMAC,
					},
					{
						GuardianAddress: validGuardian3,

						EncryptedShare: validEncryptedShare,
						ShareHmac:      validHMAC,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "valid shares - too many",
			msg: MsgUserDistributeShares{
				Creator:           validCreator,
				SecretId:          validSecretId,
				SecretCommitment:  validCommitment,
				PayloadCiphertext: validPayload,
				SecretPublicKey:   validSecretPubKey,
				Shares: func() []*EncryptedShareData {
					// Create 1001 shares (> 1000 max)
					shares := make([]*EncryptedShareData, 1001)
					for i := range shares {
						shares[i] = &EncryptedShareData{
							GuardianAddress: validGuardian,

							EncryptedShare: validEncryptedShare,
							ShareHmac:      validHMAC,
						}
					}
					return shares
				}(),
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

func TestMsgUserDistributeShares_GetSigners(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	validCreator := "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz"

	msg := MsgUserDistributeShares{
		Creator: validCreator,
	}

	signers := msg.GetSigners()
	require.Len(t, signers, 1)

	expectedAddr, err := sdk.AccAddressFromBech32(validCreator)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, signers[0])
}

func TestMsgUserDistributeShares_GetSigners_InvalidAddress(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgUserDistributeShares{
		Creator: "invalid-address",
	}

	// Should panic with invalid address
	require.Panics(t, func() {
		msg.GetSigners()
	})
}

func TestMsgUserDistributeShares_Type(t *testing.T) {
	msg := MsgUserDistributeShares{}
	require.Equal(t, TypeMsgUserDistributeShares, msg.Type())
}

func TestMsgUserDistributeShares_Route(t *testing.T) {
	msg := MsgUserDistributeShares{}
	require.Equal(t, ModuleName, msg.Route())
}

func TestMsgUserDistributeShares_GetSignBytes(t *testing.T) {
	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")

	msg := MsgUserDistributeShares{
		Creator:          "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
		SecretId:         GenerateValidSecretID(),
		SecretCommitment: make([]byte, 32),
		Shares: []*EncryptedShareData{
			{
				GuardianAddress: "tmflr1va6kzunyd9skuv2lv9jxgun9wde47h6lgservz",
				EncryptedShare:  make([]byte, 64),
				ShareHmac:       make([]byte, 32),
			},
		},
	}

	signBytes := msg.GetSignBytes()
	require.NotEmpty(t, signBytes)

	// Should be valid JSON
	require.NotPanics(t, func() {
		sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg)) //nolint:staticcheck // pins the legacy sign-bytes encoding
	})
}
