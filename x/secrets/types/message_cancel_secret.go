package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUserCancelSecret = "cancel_secret"

var _ sdk.Msg = &MsgUserCancelSecret{}

// ValidateBasic performs stateless validation of MsgUserCancelSecret
func (msg *MsgUserCancelSecret) ValidateBasic() error {
	// Validate sender address
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address (%s)", err)
	}

	// Validate secret ID
	if err := ValidateSecretIdRequired(msg.SecretId); err != nil {
		return err
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgUserCancelSecret) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgUserCancelSecret) Type() string {
	return TypeMsgUserCancelSecret
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgUserCancelSecret) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}
