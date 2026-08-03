package keeper

import (
	"context"
	"slices"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
)

// GuardianConfirmShares handles Phase 3 of the three-phase commit protocol
func (ms msgServer) GuardianConfirmShares(ctx context.Context, msg *types.MsgGuardianConfirmShares) (*types.MsgGuardianConfirmSharesResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// CRITICAL: Validate transaction signer matches guardian address
	guardian, err := sdk.AccAddressFromBech32(msg.Guardian)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address: %s", err)
	}

	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(guardian) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "transaction must be signed by the guardian")
	}

	// Get the secret
	secret, err := ms.k.GetSecret(ctx, msg.SecretId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrSecretNotFound, "secret %s not found", msg.SecretId)
	}

	// Can only accept/reject assignments in awaiting_acceptance state
	if secret.State != types.SECRET_STATUS_AWAITING_ACCEPTANCE {
		return nil, errorsmod.Wrapf(types.ErrInvalidSecretStatus,
			"can only accept assignments in awaiting_acceptance state, current status: %s", secret.State)
	}

	// Check if commit deadline has not passed
	if sdkCtx.BlockHeight() > secret.CommitDeadline {
		return nil, errorsmod.Wrapf(types.ErrAcceptanceWindowClosed,
			"commit deadline passed at block %d, current block: %d",
			secret.CommitDeadline, sdkCtx.BlockHeight())
	}

	// Find the guardian's assignment record. Records exist only for guardians
	// that were distributed a share, so a miss means either "never selected"
	// or "selected but given no share" — distinguish via the selection list.
	record, err := ms.k.GetAssignment(ctx, msg.SecretId, msg.Guardian)
	if err != nil {
		if slices.Contains(secret.SelectedGuardians, msg.Guardian) {
			return nil, errorsmod.Wrapf(types.ErrGuardianNotAssigned,
				"guardian %s was not given a share for secret %s (no encrypted share data)",
				msg.Guardian, msg.SecretId)
		}
		return nil, errorsmod.Wrapf(types.ErrGuardianNotAssigned,
			"guardian %s not assigned to secret %s",
			msg.Guardian, msg.SecretId)
	}

	// Check if guardian already responded
	if record.Status != types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED {
		return nil, errorsmod.Wrapf(types.ErrAlreadyResponded,
			"guardian already responded with status: %s", record.Status.String())
	}

	record.RespondedAtBlock = sdkCtx.BlockHeight()

	// If accepting, verify the guardian can decrypt and validate HMAC
	if msg.Accept {
		// Verify guardian is still active and eligible
		isActive := ms.k.IsGuardianActive(ctx, msg.Guardian)
		if !isActive {
			return nil, errorsmod.Wrapf(types.ErrGuardianNotActive,
				"guardian %s is not currently active", msg.Guardian)
		}

		// There is no first-n gate: every assigned candidate can accept until
		// the deadline (the flip-once assignment record above already caps
		// acceptances at the distributed count ≤ max_shares). accepted_count
		// is the denormalised counter maintained right here — the invariant
		// suite asserts it always matches the assignment store.

		// Lock the bond: this guardian's own B_g (frozen on the secret at
		// selection, priced by its k at that height) moves from the guardian's
		// unlocked float to its locked portion. Acceptance is the hard capital
		// gate — insufficient unlocked float, or a guardian meanwhile at the
		// concurrency cap, rejects the acceptance outright; there is no
		// partial lock.
		bond, ok := secret.GuardianBondAmount(msg.Guardian)
		if !ok {
			return nil, errorsmod.Wrapf(types.ErrInsufficientBond,
				"no frozen bond recorded for guardian %s on secret %s — state-integrity violation",
				msg.Guardian, msg.SecretId)
		}
		if err := ms.k.LockGuardianFloat(ctx, msg.Guardian, bond); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInsufficientBond,
				"cannot accept secret %s: %s", msg.SecretId, err)
		}

		record.Status = types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED

		// Emit acceptance event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeAssignmentAccepted,
				sdk.NewAttribute(types.AttributeKeySecretId, msg.SecretId),
				sdk.NewAttribute(types.AttributeKeyGuardianAddress, msg.Guardian),
				sdk.NewAttribute("bond_locked", sdk.NewCoin(types.DefaultDenom, bond).String()),
			),
		)
	} else {
		record.Status = types.AssignmentStatus_ASSIGNMENT_STATUS_REJECTED
		// Note: the encrypted share record is retained for audit/debugging
		// purposes (retention-plan Stage 1 will retire it)

		// Emit rejection event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeAssignmentRejected,
				sdk.NewAttribute(types.AttributeKeySecretId, msg.SecretId),
				sdk.NewAttribute(types.AttributeKeyGuardianAddress, msg.Guardian),
			),
		)
	}

	// Persist the guardian's response — a ~tiny record, not the secret blob
	if err := ms.k.SetAssignment(ctx, msg.SecretId, msg.Guardian, record); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update assignment record")
	}

	// Record the acceptance against its own tiny key rather than rewriting the
	// secret. The secret record carries one address and one frozen bond per
	// selected guardian, so rewriting it here made a guardian's accept gas
	// scale with the band while the protocol reimburses a flat amount — wide
	// bands were worked at a loss. Nothing else about the secret changes on an
	// acceptance, so nothing else needs writing.
	//
	// The secret does NOT activate mid-window: acceptances accumulate through
	// awaiting_acceptance and ProcessExpiredCommits finalises the roster at
	// the commit deadline (≥ min_shares → pending with exactly the accepted
	// set, else failed). Reaching min_shares makes the secret locked-in —
	// guaranteed to finalise, since acceptances are never revoked. That fact
	// is reported in the response and inferable from any Secret query; by
	// design there is no lock-in event and no state change.
	acceptedCount := secret.AcceptedCount
	if msg.Accept {
		acceptedCount, err = ms.k.IncrementAcceptedCount(ctx, msg.SecretId)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to record the acceptance")
		}
	}

	return &types.MsgGuardianConfirmSharesResponse{
		LockedIn: acceptedCount >= secret.MinShares,
	}, nil
}
