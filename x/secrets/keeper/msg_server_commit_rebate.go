package keeper

import (
	"context"

	"cosmossdk.io/collections"
	coserr "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
)

// RecipientCommitRebate records the commitment that binds a recipiency proof to
// the address collecting with it — step 1 of 2 (docs/spec.md
// "Recipient Rebate").
//
// The commitment is opaque: the chain cannot tell a real one from a random
// 32-byte value, and does not need to. Its only job is to exist BEFORE the
// proof becomes public, so that an observer who later sees the proof in a
// reveal transaction cannot claim the rebate with it — they would need a
// commitment of their own, made before they had the proof.
//
// Re-committing overwrites: a collector who mistyped simply commits again and
// waits another block. Front-running is unaffected, because the attacker's
// problem is producing a matching commitment at all.
func (ms msgServer) RecipientCommitRebate(ctx context.Context, msg *types.MsgRecipientCommitRebate) (*types.MsgRecipientCommitRebateResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	recipientAddr, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, coserr.Wrapf(sdkerr.ErrInvalidAddress, "invalid recipient address (%s)", err)
	}
	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, coserr.Wrap(sdkerr.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(recipientAddr) {
		return nil, coserr.Wrap(sdkerr.ErrUnauthorized, "transaction must be signed by the committing address")
	}

	// Commitments only exist against a collectable rebate, which bounds how
	// many can ever be written and keeps them sweepable at prune.
	secret, err := ms.k.GetSecret(ctx, msg.SecretId)
	if err != nil {
		return nil, coserr.Wrapf(sdkerr.ErrNotFound, "secret %s not found", msg.SecretId)
	}
	if secret.RebateAmount <= 0 {
		return nil, coserr.Wrapf(types.ErrNoRebate,
			"secret %s has no rebate to collect (state %s)", msg.SecretId, secret.State)
	}
	if secret.RebateCollected {
		return nil, coserr.Wrapf(types.ErrRebateAlreadyCollected,
			"the rebate on secret %s has already been collected", msg.SecretId)
	}
	if deadline := types.RebateCollectionDeadline(secret.TerminalAt); sdkCtx.BlockHeight() > deadline {
		return nil, coserr.Wrapf(types.ErrRebateExpired,
			"the rebate on secret %s was collectable until height %d", msg.SecretId, deadline)
	}

	height := sdkCtx.BlockHeight()
	record := types.RebateCommitmentRecord{Commitment: msg.Commitment, CommittedAt: height}
	if err := ms.k.RebateCommitments.Set(ctx,
		collections.Join(msg.SecretId, msg.Recipient), record); err != nil {
		return nil, coserr.Wrapf(err, "failed to record rebate commitment for %s", msg.SecretId)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventRebateCommitted,
			sdk.NewAttribute("secret_id", msg.SecretId),
			sdk.NewAttribute("recipient", msg.Recipient),
		),
	)

	return &types.MsgRecipientCommitRebateResponse{CommittedAt: height}, nil
}
