package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUserRequestGuardians = "request_guardians"

var _ sdk.Msg = &MsgUserRequestGuardians{}

// ValidateBasic performs stateless validation of MsgUserRequestGuardians
func (msg *MsgUserRequestGuardians) ValidateBasic() error {
	// Validate creator address
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	// Validate the detection hint (shape only — the chain cannot and need not
	// verify the hint targets anyone; random bytes are a valid no-discovery hint)
	if err := ValidateDetectionHint(&msg.DetectionHint); err != nil {
		return err
	}

	// Validate reveal window
	if msg.RevealWindow == nil {
		return errorsmod.Wrap(ErrInvalidRequest, "reveal window is required")
	}
	if err := validateRevealWindow(msg.RevealWindow); err != nil {
		return err
	}

	// Validate threshold and the guardian band (ordering + strict gap bound)
	if err := ValidateShareBand(msg.Threshold, msg.MinShares, msg.MaxShares); err != nil {
		return err
	}

	// Validate the security factor (the reward pool itself is protocol-derived
	// from bump, distance and max_shares — the creator does not choose an amount)
	if err := ValidateBump(msg.Bump); err != nil {
		return errorsmod.Wrap(ErrInvalidRequest, err.Error())
	}

	return nil
}

// validateRevealWindow validates the reveal window configuration
func validateRevealWindow(window *RevealWindow) error {
	// The floor is the fixed commit window plus the activation buffer. With the
	// commit window a constant this is fully checkable here, so the stateless
	// and keeper layers agree on it rather than the keeper being stricter.
	if window.StartOffset < MinRevealStartOffsetTotal {
		return errorsmod.Wrapf(ErrInvalidRequest, "reveal start offset too small, minimum %d blocks, got %d", MinRevealStartOffsetTotal, window.StartOffset)
	}

	if window.StartOffset > MaxRevealStartOffset {
		return errorsmod.Wrapf(ErrInvalidRequest, "reveal start offset too large, maximum %d blocks, got %d", MaxRevealStartOffset, window.StartOffset)
	}

	if window.Duration < MinRevealDuration {
		return errorsmod.Wrapf(ErrInvalidRequest, "reveal window duration too short, minimum %d blocks, got %d", MinRevealDuration, window.Duration)
	}

	if window.Duration > MaxRevealDuration {
		return errorsmod.Wrapf(ErrInvalidRequest, "reveal window duration too long, maximum %d blocks, got %d", MaxRevealDuration, window.Duration)
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgUserRequestGuardians) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgUserRequestGuardians) Type() string {
	return TypeMsgUserRequestGuardians
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgUserRequestGuardians) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}
