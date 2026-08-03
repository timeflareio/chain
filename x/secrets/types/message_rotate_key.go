package types

import (
	"bytes"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgGuardianRotateKey = "rotate_key"

var _ sdk.Msg = &MsgGuardianRotateKey{}

func NewMsgGuardianRotateKey(guardian string, newKey []byte) *MsgGuardianRotateKey {
	return &MsgGuardianRotateKey{
		Guardian: guardian,
		NewKey:   newKey,
	}
}

// ValidateBasic does basic validation checks on the message. Stateful rules —
// global key uniqueness, the minimum rotation interval, and the burned fee —
// are enforced by the msg server.
func (msg *MsgGuardianRotateKey) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Guardian); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address (%s)", err)
	}

	if len(msg.NewKey) != PublicKeyLength {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"new key must be exactly %d bytes, got %d", PublicKeyLength, len(msg.NewKey))
	}

	if bytes.Equal(msg.NewKey, make([]byte, PublicKeyLength)) {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "new key cannot be all zeros")
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgGuardianRotateKey) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgGuardianRotateKey) Type() string {
	return TypeMsgGuardianRotateKey
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgGuardianRotateKey) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}
