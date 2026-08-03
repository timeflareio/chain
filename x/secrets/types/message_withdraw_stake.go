package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgGuardianWithdrawStake = "withdraw_stake"

var _ sdk.Msg = &MsgGuardianWithdrawStake{}

// ValidateBasic does basic validation checks on the message
func (msg *MsgGuardianWithdrawStake) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Guardian); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address (%s)", err)
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgGuardianWithdrawStake) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgGuardianWithdrawStake) Type() string {
	return TypeMsgGuardianWithdrawStake
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgGuardianWithdrawStake) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}
