package keeper

import (
	"context"
	"fmt"

	coserr "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/timeflareio/chain/x/secrets/types"
)

func (ms msgServer) GuardianRegister(ctx context.Context, msg *types.MsgGuardianRegister) (*types.MsgGuardianRegisterResponse, error) {
	// OPTIMIZATION: Check duplicate guardian FIRST to save gas on common failures
	_, found := ms.k.GetGuardian(ctx, msg.Guardian)
	if found {
		return nil, coserr.Wrap(sdkerr.ErrInvalidRequest, "guardian already exists - use MsgGuardianUpdate to modify parameters")
	}

	// Validate basic message fields including the optional float deposit
	addr, err := ms.validateGuardianRegisterMessage(ctx, msg)
	if err != nil {
		return nil, err
	}

	// CRITICAL: Validate transaction signer matches guardian address
	if err := ms.validateSignerAuthorization(msg, addr); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Validate relative availability window timing for new registration
	if err := ms.validateRegistrationAvailabilityWindow(msg.AvailableFrom, msg.AvailableUntil, blockHeight); err != nil {
		return nil, err
	}

	// Global, permanent key uniqueness: the key must never have been
	// registered by any guardian at any epoch — retired keys stay reserved
	// forever (docs/spec.md "Guardian Key Rotation")
	if taken, holder := ms.k.KeyEverRegistered(ctx, msg.EncryptionPublicKey); taken {
		return nil, coserr.Wrapf(types.ErrKeyAlreadyRegistered, "key held by guardian %s", holder)
	}

	// Handle new registration
	return ms.handleNewRegistration(ctx, msg, addr, blockHeight)
}

// handleNewRegistration processes new guardian registrations.
//
// Economics: registration charges the protocol entry fee F from the
// guardian's account into the fee collector, where it rides the next
// block's 90/10 fee split like every validator-bound flow — 90% allocated
// to validator rewards, 10% burned (ruled July 2026: one pipe, no
// exemptions). The fee is never returned. Any deposit in the message is
// additionally moved into module escrow as the guardian's initial float
// (working capital for per-secret bonds).
func (ms msgServer) handleNewRegistration(ctx context.Context, msg *types.MsgGuardianRegister, addr sdk.AccAddress, blockHeight int64) (*types.MsgGuardianRegisterResponse, error) {

	// Charge the entry fee into the fee collector — it joins the next
	// block's 90/10 split (ProcessFeeSplit, then x/distribution allocation)
	entryFee := sdk.NewCoin(types.DefaultDenom, types.EntryFee())
	if err := ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, authtypes.FeeCollectorName, sdk.NewCoins(entryFee)); err != nil {
		return nil, coserr.Wrap(sdkerr.ErrInsufficientFunds, "failed to charge entry fee")
	}

	// Move the optional initial float deposit into module escrow
	deposit := sdk.NewCoin(types.DefaultDenom, math.ZeroInt())
	if msg.Deposit != nil && !msg.Deposit.IsZero() {
		deposit = *msg.Deposit
		if err := ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, types.ModuleName, sdk.NewCoins(deposit)); err != nil {
			return nil, coserr.Wrap(sdkerr.ErrInsufficientFunds, "failed to deposit float")
		}
	}

	// Convert relative availability values to absolute block heights
	availableFromAbs, availableUntilAbs := ms.convertRegistrationTimesToAbsolute(msg.AvailableFrom, msg.AvailableUntil, blockHeight)

	// Create new guardian with an empty locked portion, the bond multiplier
	// at its floor (every registrant starts at k = 4.00 — the minimum any
	// guardian can hold, so fresh addresses gain no advantage), and no
	// active bonds
	locked := sdk.NewCoin(types.DefaultDenom, math.ZeroInt())
	guardian := types.Guardian{
		Address:             msg.Guardian,
		EncryptionPublicKey: msg.EncryptionPublicKey,
		AvailableFrom:       availableFromAbs,
		AvailableUntil:      availableUntilAbs,
		Stake:               &deposit, // float total
		LockedStake:         &locked,  // nothing bonded yet
		AcceptingSecrets:    msg.AcceptingSecrets,
		BondK:               types.InitialBondK,
		ActiveBondCount:     0,
	}

	// Save new guardian
	if err := ms.k.SetGuardian(ctx, guardian); err != nil {
		return nil, coserr.Wrap(err, "failed to store guardian")
	}

	// The registration key is epoch 0 of the guardian's append-only key
	// history (current_key_epoch defaults to 0 on the record). Effective from
	// the next block, matching the rotation rule shape — the guardian cannot
	// be selected before then anyway (availability starts at height + 1) —
	// and starting the rotation-interval clock.
	if err := ms.k.AppendGuardianKeyEpoch(ctx, guardian.Address, 0, types.KeyHistoryEntry{
		PublicKey:           msg.EncryptionPublicKey,
		EffectiveFromHeight: blockHeight + 1,
	}); err != nil {
		return nil, coserr.Wrap(err, "failed to record key epoch 0")
	}

	// Emit registration event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventGuardianRegistered,
			sdk.NewAttribute("guardian", msg.Guardian),
			sdk.NewAttribute("action", "registered"),
			sdk.NewAttribute("entry_fee", entryFee.String()),
			sdk.NewAttribute("float_deposit", deposit.String()),
			sdk.NewAttribute("bond_k", fmt.Sprintf("%d", guardian.BondK)),
			sdk.NewAttribute("available_from", fmt.Sprintf("%d", availableFromAbs)),
			sdk.NewAttribute("available_until", fmt.Sprintf("%d", availableUntilAbs)),
		),
	)

	return &types.MsgGuardianRegisterResponse{}, nil
}
