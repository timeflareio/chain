package keeper

import (
	"context"

	coserr "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
)

// GuardianWithdrawStake returns the guardian's entire UNLOCKED float. Bonds for
// in-flight secrets stay locked in escrow until their settlements release
// them, and the guardian record persists — registration is permanent (the
// entry fee is already spent), so a guardian can top up and resume at any time.
func (ms msgServer) GuardianWithdrawStake(ctx context.Context, msg *types.MsgGuardianWithdrawStake) (*types.MsgGuardianWithdrawStakeResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate message fields
	guardianAddr, err := ms.validateGuardianWithdrawStakeMessage(msg)
	if err != nil {
		return nil, err
	}

	// CRITICAL: Validate transaction signer matches guardian address
	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, coserr.Wrap(sdkerr.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(guardianAddr) {
		return nil, coserr.Wrap(sdkerr.ErrUnauthorized, "transaction must be signed by the guardian")
	}

	// Get existing guardian
	guardian, found := ms.k.GetGuardian(ctx, msg.Guardian)
	if !found {
		return nil, coserr.Wrap(sdkerr.ErrNotFound, "guardian not found")
	}

	// Withdrawals are capped at the unlocked float (total − locked)
	unlocked := UnlockedFloat(&guardian)
	if !unlocked.IsPositive() {
		return nil, coserr.Wrap(sdkerr.ErrInvalidRequest,
			"guardian has no unlocked float to withdraw - bonds for in-flight secrets remain locked until settlement")
	}

	// Return the unlocked float to the guardian
	withdrawal := sdk.NewCoin(types.DefaultDenom, unlocked)
	if err := ms.k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, guardianAddr, sdk.NewCoins(withdrawal)); err != nil {
		return nil, coserr.Wrap(err, "failed to return unlocked float")
	}

	// The float total shrinks to exactly the locked portion; the guardian
	// record persists (no deregistration in the bonded model).
	_, locked := floatAmounts(&guardian)
	guardian.Stake = &sdk.Coin{Denom: types.DefaultDenom, Amount: locked}
	guardian.LockedStake = &sdk.Coin{Denom: types.DefaultDenom, Amount: locked}
	if err := ms.k.SetGuardian(ctx, guardian); err != nil {
		return nil, coserr.Wrap(err, "failed to update guardian float")
	}

	// Emit withdrawal event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventGuardianUpdated,
			sdk.NewAttribute("guardian", msg.Guardian),
			sdk.NewAttribute("action", "float_withdrawn"),
			sdk.NewAttribute("withdrawn", withdrawal.String()),
			sdk.NewAttribute("still_locked", sdk.NewCoin(types.DefaultDenom, locked).String()),
		),
	)

	return &types.MsgGuardianWithdrawStakeResponse{}, nil
}
