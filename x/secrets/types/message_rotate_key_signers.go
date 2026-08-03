package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetSigners returns the expected signers for a MsgGuardianRotateKey transaction.
func (msg *MsgGuardianRotateKey) GetSigners() []sdk.AccAddress {
	guardian, err := sdk.AccAddressFromBech32(msg.Guardian)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{guardian}
}
