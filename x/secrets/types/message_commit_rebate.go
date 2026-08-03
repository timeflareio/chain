package types

import (
	"crypto/sha256"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgRecipientCommitRebate = "commit_rebate"

var _ sdk.Msg = &MsgRecipientCommitRebate{}

// ValidateBasic performs stateless validation of MsgRecipientCommitRebate.
// Structural only: whether the commitment corresponds to a real recipiency
// proof is unknowable until the reveal, which is the entire point of it.
func (msg *MsgRecipientCommitRebate) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Recipient); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address (%s)", err)
	}

	if err := ValidateSecretIdRequired(msg.SecretId); err != nil {
		return err
	}

	if len(msg.Commitment) != sha256.Size {
		return errorsmod.Wrapf(ErrInvalidRequest,
			"rebate commitment must be %d bytes, got %d", sha256.Size, len(msg.Commitment))
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgRecipientCommitRebate) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgRecipientCommitRebate) Type() string {
	return TypeMsgRecipientCommitRebate
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgRecipientCommitRebate) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}

// GetSigners returns the committing address as the only signer: the commitment
// binds to it, and only it can reveal against it.
func (msg *MsgRecipientCommitRebate) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}
