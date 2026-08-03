package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetSigners returns the expected signers for a MsgSlashGuardian transaction.
func (msg *MsgSlashGuardian) GetSigners() []sdk.AccAddress {
	reporter, err := sdk.AccAddressFromBech32(msg.ReporterAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{reporter}
}
