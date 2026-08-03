package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetSigners returns the expected signers for a MsgUserRequestGuardians transaction.
func (msg *MsgUserRequestGuardians) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}
