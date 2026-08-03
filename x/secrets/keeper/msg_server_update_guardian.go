package keeper

import (
	"context"
	"fmt"

	coserr "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
)

func (ms msgServer) GuardianUpdate(ctx context.Context, msg *types.MsgGuardianUpdate) (*types.MsgGuardianUpdateResponse, error) {
	// Validate basic message fields
	addr, err := ms.validateGuardianUpdateMessage(msg)
	if err != nil {
		return nil, err
	}

	// CRITICAL: Validate transaction signer matches guardian address
	if err := ms.validateSignerAuthorization(msg, addr); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Get existing guardian (must exist for updates)
	existingGuardian, found := ms.k.GetGuardian(ctx, msg.Guardian)
	if !found {
		return nil, coserr.Wrap(sdkerr.ErrNotFound, "guardian not found - cannot update non-existent guardian")
	}

	// Start with existing guardian data
	updatedGuardian := existingGuardian

	// Note: Encryption public keys are permanently immutable and not included in this message
	// Guardians must withdraw and re-register to change encryption keys

	// Handle availability window update
	if msg.AvailableFrom != 0 || msg.AvailableUntil != 0 {
		if err := ms.validateUpdateAvailabilityWindow(msg.AvailableFrom, msg.AvailableUntil, blockHeight, &existingGuardian); err != nil {
			return nil, err
		}

		// Convert to absolute values
		availableFromAbs, availableUntilAbs := ms.convertUpdateTimesToAbsolute(msg.AvailableFrom, msg.AvailableUntil, blockHeight, &existingGuardian)
		updatedGuardian.AvailableFrom = availableFromAbs
		updatedGuardian.AvailableUntil = availableUntilAbs
	}

	// Handle float deposit top-up
	if msg.Deposit != nil && !msg.Deposit.IsZero() {
		if err := ms.handleFloatDeposit(ctx, msg.Deposit, &updatedGuardian, addr); err != nil {
			return nil, err
		}
	}

	// Handle accepting_secrets update — presence-aware (BoolValue): nil means
	// "no change", an explicit wrapper value (true or false) is applied
	if msg.AcceptingSecrets != nil {
		updatedGuardian.AcceptingSecrets = msg.AcceptingSecrets.Value
	}

	// Save updated guardian
	if err := ms.k.SetGuardian(ctx, updatedGuardian); err != nil {
		return nil, coserr.Wrap(err, "failed to update guardian")
	}

	// Emit update event
	eventAttrs := []sdk.Attribute{
		sdk.NewAttribute("guardian", msg.Guardian),
		sdk.NewAttribute("action", "guardian_updated"),
	}

	// Add specific update information
	// Note: Encryption key updates removed for security - keys are permanently immutable
	if msg.AvailableFrom != 0 || msg.AvailableUntil != 0 {
		eventAttrs = append(eventAttrs,
			sdk.NewAttribute("availability_updated", "true"),
			sdk.NewAttribute("available_from", fmt.Sprintf("%d", updatedGuardian.AvailableFrom)),
			sdk.NewAttribute("available_until", fmt.Sprintf("%d", updatedGuardian.AvailableUntil)),
		)
	}
	if msg.Deposit != nil && !msg.Deposit.IsZero() {
		eventAttrs = append(eventAttrs,
			sdk.NewAttribute("float_deposited", msg.Deposit.String()),
			sdk.NewAttribute("new_float_total", updatedGuardian.Stake.String()),
		)
	}
	if msg.AcceptingSecrets != nil {
		eventAttrs = append(eventAttrs, sdk.NewAttribute("accepting_secrets_updated", fmt.Sprintf("%t", msg.AcceptingSecrets.Value)))
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventGuardianUpdated, // Use correct event type for updates
			eventAttrs...,
		),
	)

	return &types.MsgGuardianUpdateResponse{}, nil
}

// validateGuardianUpdateMessage validates the basic fields of a MsgGuardianUpdate
func (ms msgServer) validateGuardianUpdateMessage(msg *types.MsgGuardianUpdate) (sdk.AccAddress, error) {
	// Validate guardian address
	addr, err := ms.validateGuardianAddress(msg.Guardian)
	if err != nil {
		return nil, err
	}

	// Validate that at least one field is being updated
	if !hasUpdateFields(msg) {
		return nil, coserr.Wrap(sdkerr.ErrInvalidRequest, "at least one field must be updated")
	}

	// Note: Encryption public key field removed - permanently immutable

	return addr, nil
}

// validateEncryptionKeyUpdate removed - encryption keys are permanently immutable
// Guardians must withdraw and re-register to change encryption keys

// handleFloatDeposit moves an additional deposit into module escrow and adds it
// to the guardian's float total. There is no cap: the float is working capital
// for per-secret bonds, and larger floats simply allow more concurrent secrets.
func (ms msgServer) handleFloatDeposit(ctx context.Context, deposit *sdk.Coin, guardian *types.Guardian, addr sdk.AccAddress) error {
	// Validate denomination
	if deposit.Denom != types.DefaultDenom {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"deposit must be in %s denomination, got %s", types.DefaultDenom, deposit.Denom)
	}

	// Validate deposit is positive
	if !deposit.Amount.IsPositive() {
		return coserr.Wrap(sdkerr.ErrInvalidRequest, "deposit must be positive")
	}

	// Transfer the deposit into module escrow
	if err := ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, types.ModuleName, sdk.NewCoins(*deposit)); err != nil {
		return coserr.Wrap(sdkerr.ErrInsufficientFunds, "failed to transfer deposit")
	}

	// Add to the float total
	newTotal := math.ZeroInt()
	if guardian.Stake != nil {
		newTotal = guardian.Stake.Amount
	}
	newTotal = newTotal.Add(deposit.Amount)
	guardian.Stake = &sdk.Coin{Denom: types.DefaultDenom, Amount: newTotal}

	return nil
}
