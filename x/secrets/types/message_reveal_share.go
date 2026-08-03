package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgGuardianRevealShare = "reveal_share"

var _ sdk.Msg = &MsgGuardianRevealShare{}

// GetSigners returns the address of the guardian who must sign the transaction
func (msg *MsgGuardianRevealShare) GetSigners() []sdk.AccAddress {
	guardian, err := sdk.AccAddressFromBech32(msg.Guardian)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{guardian}
}

// Type returns the message type for routing
func (msg *MsgGuardianRevealShare) Type() string {
	return TypeMsgGuardianRevealShare
}

// Route returns the message route for routing
func (msg *MsgGuardianRevealShare) Route() string {
	return ModuleName
}

// GetSignBytes returns the bytes to sign for the message
func (msg *MsgGuardianRevealShare) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}

// ValidateBasic performs basic validation of the message
func (msg *MsgGuardianRevealShare) ValidateBasic() error {
	// Validate guardian address
	_, err := sdk.AccAddressFromBech32(msg.Guardian)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address (%s)", err)
	}

	// Validate secret ID
	if err := ValidateSecretIdRequired(msg.SecretId); err != nil {
		return err
	}

	// Share index no longer tracked - SSS handles intrinsic IDs

	// Validate decrypted share
	if len(msg.DecryptedShare) == 0 {
		return errorsmod.Wrap(ErrInvalidRequest, "decrypted share cannot be empty")
	}

	return nil
}
