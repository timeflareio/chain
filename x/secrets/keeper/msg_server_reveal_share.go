package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
)

func (k msgServer) GuardianRevealShare(goCtx context.Context, msg *types.MsgGuardianRevealShare) (*types.MsgGuardianRevealShareResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Validate guardian address
	guardianAddr, err := sdk.AccAddressFromBech32(msg.Guardian)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address: %s", err)
	}

	// CRITICAL: Validate transaction signer matches guardian address
	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(guardianAddr) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "transaction must be signed by the guardian")
	}

	// Basic validation of decrypted share format: a plaintext key-share
	// envelope, not a payload fragment
	if len(msg.DecryptedShare) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "decrypted share cannot be empty")
	}
	if int64(len(msg.DecryptedShare)) > types.MaxRevealedKeyShareSize {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"decrypted share exceeds maximum size: %d bytes (limit: %d bytes)",
			len(msg.DecryptedShare), types.MaxRevealedKeyShareSize)
	}

	// Get the secret
	secret, err := k.k.GetSecret(ctx, msg.SecretId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "secret not found: %s", msg.SecretId)
	}

	// Check if secret has been cancelled - no reveals allowed after cancellation
	if secret.State == types.SECRET_STATUS_CANCELLED {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "cannot reveal shares for cancelled secret")
	}

	// Check secret state - must be pending or reconstructable (acceptance complete, reveal window open)
	if secret.State != types.SECRET_STATUS_PENDING && secret.State != types.SECRET_STATUS_RECONSTRUCTABLE {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "secret is not in pending or reconstructable state (reveal window must be open): %s", secret.State)
	}

	// Validate reveal window (point-reads only — no share bytes)
	revealValidator := &RevealWindowValidator{}
	if err := revealValidator.CanGuardianRevealShare(goCtx, k.k, secret, msg.Guardian, ctx.BlockHeight()); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
	}

	// Find the guardian's assignment record
	record, err := k.k.GetAssignment(goCtx, msg.SecretId, msg.Guardian)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound,
			"guardian assignment not found for guardian %s", msg.Guardian)
	}

	// Verify guardian has accepted this assignment (double-check for consistency)
	if record.Status != types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"guardian has not accepted this assignment, current status: %s", record.Status.String())
	}

	// Verify HMAC to ensure share authenticity (for slashing protection) —
	// a single point-read of this guardian's share record
	share, err := k.k.GetShareData(goCtx, msg.SecretId, msg.Guardian)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound,
			"share data not found for guardian %s", msg.Guardian)
	}
	if !k.k.VerifyShareHMAC(msg.SecretId, msg.Guardian, msg.DecryptedShare, share.ShareHmac) {
		// HMAC verification failed - reject the transaction (no penalty)
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "HMAC verification failed - invalid share data")
	}

	// Use shared logic for adding the revealed share
	// All rewards are distributed at reveal_window.end_block by ProcessExpiredRevealWindows
	// This ensures fair distribution with complete slashing and eligibility information
	// and prevents race conditions between early reconstruction and window expiry
	updatedSecret, reconstructionComplete, err := k.k.addRevealedShare(goCtx, secret, msg.Guardian, msg.DecryptedShare)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to add revealed share")
	}
	secret = updatedSecret

	// A correct, in-window, HMAC-verified reveal is the reveal event that
	// steps the guardian's bond multiplier k down (× 0.963, floor-clamped),
	// cheapening its future acceptances. Applied here — not at settlement —
	// so the adjustment is per event, exactly as specified.
	if err := k.k.AdjustBondKOnReveal(goCtx, msg.Guardian); err != nil {
		return nil, errorsmod.Wrap(err, "failed to adjust bond multiplier after reveal")
	}

	// Emit events
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"share_revealed",
			sdk.NewAttribute("secret_id", msg.SecretId),
			sdk.NewAttribute("guardian", msg.Guardian),
			// ShareIndex no longer tracked - SSS handles intrinsic IDs
			sdk.NewAttribute("block_height", fmt.Sprintf("%d", ctx.BlockHeight())),
			sdk.NewAttribute("reconstruction_complete", fmt.Sprintf("%t", reconstructionComplete)),
		),
	)

	if reconstructionComplete {
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"secret_reconstructed",
				sdk.NewAttribute("secret_id", msg.SecretId),
				sdk.NewAttribute("total_shares", fmt.Sprintf("%d", secret.RevealedCount)),
				sdk.NewAttribute("threshold", fmt.Sprintf("%d", secret.Threshold)),
			),
		)
	}

	return &types.MsgGuardianRevealShareResponse{
		Accepted:               true,
		ReconstructionComplete: reconstructionComplete,
	}, nil
}

// NOTE: The calculateGuardianReward function has been removed.
// Rewards are distributed fairly at reveal_window.end_block by ProcessExpiredRevealWindows.
// This ensures equal treatment with complete slashing information and prevents race conditions.
