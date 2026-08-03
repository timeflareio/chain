package keeper

import (
	"context"
	"fmt"

	coserr "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
)

// GuardianRotateKey rotates the guardian's share-encryption key forward: the
// new key becomes the next epoch of the append-only history and takes effect
// for selections from the NEXT block; every existing assignment stays bound
// to the epoch key it was created under (forward-only — the chain holds only
// ciphertext and can re-encrypt nothing, so replacement semantics are
// impossible by construction). Charges the flat burned rotation fee and
// enforces the minimum rotation interval. See docs/spec.md "Guardian Key
// Rotation".
func (ms msgServer) GuardianRotateKey(ctx context.Context, msg *types.MsgGuardianRotateKey) (*types.MsgGuardianRotateKeyResponse, error) {
	addr, err := ms.validateGuardianAddress(msg.Guardian)
	if err != nil {
		return nil, err
	}

	// CRITICAL: Validate transaction signer matches guardian address
	if err := ms.validateSignerAuthorization(msg, addr); err != nil {
		return nil, err
	}

	guardian, found := ms.k.GetGuardian(ctx, msg.Guardian)
	if !found {
		return nil, coserr.Wrap(types.ErrGuardianNotFound, "cannot rotate key for unregistered guardian")
	}

	// Validate the new key's format and validity
	if err := ms.validateEncryptionPublicKey(msg.NewKey); err != nil {
		return nil, err
	}

	// Global, permanent uniqueness: the new key must never have been
	// registered by any guardian at any epoch — a retired key stays reserved
	// forever, since share material encrypted to it may still exist.
	if taken, holder := ms.k.KeyEverRegistered(ctx, msg.NewKey); taken {
		return nil, coserr.Wrapf(types.ErrKeyAlreadyRegistered, "key held by guardian %s", holder)
	}

	// Minimum interval: the newest epoch's effective height must be at least
	// KeyRotationMinIntervalBlocks old (epoch 0's, set at registration,
	// starts the clock). A guardian whose current key is compromised inside
	// the window sets accepting_secrets = false immediately — identical
	// forward protection — and rotates when the window opens.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()
	newest, err := ms.k.GuardianKeyHistory.Get(ctx, guardianHistoryKey(guardian))
	if err != nil {
		return nil, coserr.Wrapf(err,
			"guardian %s has no key-history entry at its current epoch %d — state-integrity violation",
			guardian.Address, guardian.CurrentKeyEpoch)
	}
	minInterval := types.KeyRotationMinIntervalValue()
	if elapsed := blockHeight - newest.EffectiveFromHeight; elapsed < minInterval {
		return nil, coserr.Wrapf(types.ErrRotationTooSoon,
			"%d blocks since the current epoch took effect (minimum %d); earliest rotation at height %d",
			elapsed, minInterval, newest.EffectiveFromHeight+minInterval)
	}

	// Charge the flat rotation fee and burn it — anti-spam pricing of the
	// permanent history entry, not economics.
	fee := sdk.NewCoin(types.DefaultDenom, types.KeyRotationFee())
	if err := ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, types.ModuleName, sdk.NewCoins(fee)); err != nil {
		return nil, coserr.Wrap(sdkerr.ErrInsufficientFunds, "failed to charge rotation fee")
	}
	if err := ms.k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(fee)); err != nil {
		return nil, coserr.Wrap(err, "failed to burn rotation fee")
	}

	// Append the next epoch, effective from the next block: a same-block
	// selection still hands the creator the pre-rotation key (the selection
	// read is effective-height-aware — see GuardianKeyInForce).
	newEpoch := guardian.CurrentKeyEpoch + 1
	effectiveFrom := blockHeight + 1
	if err := ms.k.AppendGuardianKeyEpoch(ctx, guardian.Address, newEpoch, types.KeyHistoryEntry{
		PublicKey:           msg.NewKey,
		EffectiveFromHeight: effectiveFrom,
	}); err != nil {
		return nil, coserr.Wrap(err, "failed to append key epoch")
	}

	// Advance the record's current-epoch pointer and its convenience copy of
	// the current key (the O(1) selection read)
	guardian.EncryptionPublicKey = msg.NewKey
	guardian.CurrentKeyEpoch = newEpoch
	if err := ms.k.SetGuardian(ctx, guardian); err != nil {
		return nil, coserr.Wrap(err, "failed to update guardian")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventGuardianKeyRotated,
			sdk.NewAttribute("guardian", msg.Guardian),
			sdk.NewAttribute("new_epoch", fmt.Sprintf("%d", newEpoch)),
			sdk.NewAttribute("effective_from_height", fmt.Sprintf("%d", effectiveFrom)),
			sdk.NewAttribute("fee_burned", fee.String()),
		),
	)

	return &types.MsgGuardianRotateKeyResponse{
		NewEpoch:            newEpoch,
		EffectiveFromHeight: effectiveFrom,
	}, nil
}
