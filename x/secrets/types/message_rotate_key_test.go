package types

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func validRotateKeyAddress() string {
	return sdk.AccAddress([]byte("rotate_key_guardian_")).String()
}

func TestMsgGuardianRotateKey_ValidateBasic(t *testing.T) {
	validKey := make([]byte, PublicKeyLength)
	validKey[0] = 0x42

	cases := []struct {
		name    string
		msg     *MsgGuardianRotateKey
		wantErr string
	}{
		{
			name: "valid",
			msg:  NewMsgGuardianRotateKey(validRotateKeyAddress(), validKey),
		},
		{
			name:    "invalid address",
			msg:     NewMsgGuardianRotateKey("not-bech32", validKey),
			wantErr: "invalid guardian address",
		},
		{
			name:    "short key",
			msg:     NewMsgGuardianRotateKey(validRotateKeyAddress(), validKey[:16]),
			wantErr: "exactly 32 bytes",
		},
		{
			name:    "empty key",
			msg:     NewMsgGuardianRotateKey(validRotateKeyAddress(), nil),
			wantErr: "exactly 32 bytes",
		},
		{
			name:    "all-zero key",
			msg:     NewMsgGuardianRotateKey(validRotateKeyAddress(), make([]byte, PublicKeyLength)),
			wantErr: "all zeros",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestKeyRotationFeeDerivation pins the fee to its derivation — one
// guardian-day of the master rate, never a hard-coded product — so a rate
// retune cascades automatically (docs/spec.md "Guardian Key Rotation").
func TestKeyRotationFeeDerivation(t *testing.T) {
	expected := math.NewInt(RatePerGuardianBlock).Mul(math.NewInt(KeyRotationFeeBlocks))
	require.True(t, expected.Equal(KeyRotationFee()))
	require.True(t, KeyRotationFee().IsPositive())

	// The interval is a whole number of fee-days (30 days at 6s blocks) —
	// the two constants share the guardian-day block base.
	require.Equal(t, int64(30), KeyRotationMinIntervalBlocks/KeyRotationFeeBlocks)
}
