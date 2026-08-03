package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetSigners returns the expected signers for a MsgUserCancelSecret transaction.
func (msg *MsgUserCancelSecret) GetSigners() []sdk.AccAddress {
	sender, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{sender}
}
