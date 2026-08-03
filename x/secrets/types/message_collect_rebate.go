package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgRecipientCollectRebate = "collect_rebate"

var _ sdk.Msg = &MsgRecipientCollectRebate{}

// ValidateBasic performs stateless validation of MsgRecipientCollectRebate.
//
// Structural only, per the module-boundary rule: `z` is checked for length,
// never for meaning. Whether it derives the secret's hint tag is a
// cryptographic computation against stored state, and that is keeper work.
func (msg *MsgRecipientCollectRebate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Recipient); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address (%s)", err)
	}

	if err := ValidateSecretIdRequired(msg.SecretId); err != nil {
		return err
	}

	if len(msg.Z) != PublicKeyLength {
		return errorsmod.Wrapf(ErrInvalidRequest,
			"recipiency proof must be %d bytes, got %d", PublicKeyLength, len(msg.Z))
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgRecipientCollectRebate) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgRecipientCollectRebate) Type() string {
	return TypeMsgRecipientCollectRebate
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgRecipientCollectRebate) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}

// GetSigners returns the collecting recipient as the only signer: the rebate
// is paid to the signer, so signer and payee are the same account by
// construction.
func (msg *MsgRecipientCollectRebate) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}
