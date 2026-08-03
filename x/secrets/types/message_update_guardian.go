package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	gogotypes "github.com/cosmos/gogoproto/types"
)

const TypeMsgGuardianUpdate = "update_guardian"

var _ sdk.Msg = &MsgGuardianUpdate{}

// NewMsgGuardianUpdate constructs an update message. acceptingSecrets is
// presence-aware: pass nil to leave the guardian's acceptance flag unchanged.
func NewMsgGuardianUpdate(guardian string, availableFrom int64, availableUntil int64, deposit *sdk.Coin, acceptingSecrets *bool) *MsgGuardianUpdate {
	var accepting *gogotypes.BoolValue
	if acceptingSecrets != nil {
		accepting = &gogotypes.BoolValue{Value: *acceptingSecrets}
	}
	return &MsgGuardianUpdate{
		Guardian:         guardian,
		AvailableFrom:    availableFrom,
		AvailableUntil:   availableUntil,
		Deposit:          deposit,
		AcceptingSecrets: accepting,
	}
}

// ValidateBasic does basic validation checks on the message
func (msg *MsgGuardianUpdate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Guardian); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address (%s)", err)
	}

	// At least one field must be updated
	// Note: We allow the message through basic validation and let the server
	// determine if actual changes are being made (especially for AcceptingSecrets)
	// SECURITY: Encryption key updates are forbidden (permanently immutable)
	hasUpdates := msg.AvailableFrom != 0 ||
		msg.AvailableUntil != 0 ||
		(msg.Deposit != nil && !msg.Deposit.IsZero()) ||
		msg.AcceptingSecrets != nil // presence-aware: an explicit true or false counts

	if !hasUpdates {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "at least one field must be updated")
	}

	// Note: Encryption public keys are permanently immutable and not included in this message

	// Validate deposit if provided
	if msg.Deposit != nil {
		if !msg.Deposit.IsValid() {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "deposit must be a valid coin")
		}
		if msg.Deposit.IsZero() {
			return errors.Wrap(sdkerrors.ErrInvalidRequest, "deposit must be non-zero")
		}
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgGuardianUpdate) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgGuardianUpdate) Type() string {
	return TypeMsgGuardianUpdate
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgGuardianUpdate) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}

// GetSigners returns the expected signers for a MsgGuardianUpdate message
func (msg *MsgGuardianUpdate) GetSigners() []sdk.AccAddress {
	guardian, err := sdk.AccAddressFromBech32(msg.Guardian)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{guardian}
}
