package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetSigners returns the expected signers for a MsgGuardianRegister transaction.
func (msg *MsgGuardianRegister) GetSigners() []sdk.AccAddress {
	guardian, err := sdk.AccAddressFromBech32(msg.Guardian)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{guardian}
}
