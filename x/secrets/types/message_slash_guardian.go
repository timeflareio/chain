package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgSlashGuardian = "slash_guardian"

var _ sdk.Msg = &MsgSlashGuardian{}

// ValidateBasic does basic validation checks on the message
func (msg *MsgSlashGuardian) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.GuardianAddress); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address (%s)", err)
	}

	if _, err := sdk.AccAddressFromBech32(msg.ReporterAddress); err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid reporter address (%s)", err)
	}

	if len(msg.Evidence) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "evidence cannot be empty")
	}

	if len(msg.Evidence) < MinEvidenceLength {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"evidence too short - minimum %d bytes required, got %d bytes",
			MinEvidenceLength, len(msg.Evidence))
	}

	// Evidence is a plaintext key-share envelope (34B today) — bound it like
	// a reveal submission
	if int64(len(msg.Evidence)) > MaxRevealedKeyShareSize {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest,
			"evidence too large - maximum %d bytes, got %d bytes",
			MaxRevealedKeyShareSize, len(msg.Evidence))
	}

	if len(msg.Reason) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "slash reason cannot be empty")
	}

	if err := ValidateSecretIdRequired(msg.SecretId); err != nil {
		return err
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgSlashGuardian) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgSlashGuardian) Type() string {
	return TypeMsgSlashGuardian
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgSlashGuardian) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}
