package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgGuardianRegister = "register_guardian"

var _ sdk.Msg = &MsgGuardianRegister{}

func NewMsgGuardianRegister(guardian string, encryptionPublicKey []byte, availableFrom int64, availableUntil int64, deposit sdk.Coin, acceptingSecrets bool) *MsgGuardianRegister {
	return &MsgGuardianRegister{
		Guardian:            guardian,
		EncryptionPublicKey: encryptionPublicKey,
		AvailableFrom:       availableFrom,
		AvailableUntil:      availableUntil,
		Deposit:             &deposit,
		AcceptingSecrets:    acceptingSecrets,
	}
}

// ValidateBasic does basic validation checks on the message
func (msg *MsgGuardianRegister) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Guardian); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address (%s)", err)
	}

	if len(msg.EncryptionPublicKey) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "encryption public key cannot be empty")
	}

	if msg.AvailableUntil <= msg.AvailableFrom {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "available_until must be greater than available_from")
	}

	// The initial float deposit is optional (the entry fee is charged
	// separately by the handler), but if provided it must be a valid coin.
	if msg.Deposit != nil && !msg.Deposit.IsValid() {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "deposit must be a valid coin")
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgGuardianRegister) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgGuardianRegister) Type() string {
	return TypeMsgGuardianRegister
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgGuardianRegister) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}
