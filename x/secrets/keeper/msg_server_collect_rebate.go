package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	coserr "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
	"github.com/timeflareio/crypto/go"
)

// RecipientCollectRebate reveals the recipiency proof and pays the rebate —
// step 2 of 2 (docs/spec.md "Recipient Rebate").
//
// The proof is `z`, the X25519 shared value the recipient's private key
// computes against the hint's stored ephemeral key: only its holder can produce
// a value that hashes to the stored tag. Two conditions must hold, and the
// second is what makes the first safe to make public:
//
//  1. `z` reproduces the secret's detection-hint tag — this is recipiency.
//  2. `z` reproduces the commitment this signer published in an EARLIER block —
//     this is priority. `z` is public from the moment this transaction enters a
//     mempool, so without a prior commitment any observer, a validator most
//     easily of all, could copy it into their own transaction and take the
//     rebate. A front-runner has no commitment for a proof it has just seen and
//     cannot backdate one.
func (ms msgServer) RecipientCollectRebate(ctx context.Context, msg *types.MsgRecipientCollectRebate) (*types.MsgRecipientCollectRebateResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	recipientAddr, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, coserr.Wrapf(sdkerr.ErrInvalidAddress, "invalid recipient address (%s)", err)
	}

	// The rebate is paid to the signer: a mismatch here would pay one account
	// on another account's proof.
	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, coserr.Wrap(sdkerr.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(recipientAddr) {
		return nil, coserr.Wrap(sdkerr.ErrUnauthorized, "transaction must be signed by the collecting recipient")
	}

	secret, err := ms.k.GetSecret(ctx, msg.SecretId)
	if err != nil {
		return nil, coserr.Wrapf(sdkerr.ErrNotFound, "secret %s not found", msg.SecretId)
	}

	// A rebate exists only on a revealed secret, and only above the dust
	// floor. Refusals name the reason so a client can tell "nothing was ever
	// credited" from "someone already collected it".
	if secret.RebateAmount <= 0 {
		return nil, coserr.Wrapf(types.ErrNoRebate,
			"secret %s has no rebate (state %s)", msg.SecretId, secret.State)
	}
	if secret.RebateCollected {
		return nil, coserr.Wrapf(types.ErrRebateAlreadyCollected,
			"the rebate on secret %s has already been collected", msg.SecretId)
	}
	if deadline := types.RebateCollectionDeadline(secret.TerminalAt); sdkCtx.BlockHeight() > deadline {
		return nil, coserr.Wrapf(types.ErrRebateExpired,
			"the rebate on secret %s was collectable until height %d", msg.SecretId, deadline)
	}

	// Recipiency: recompute the hint tag from the submitted shared value.
	// Constant-time, and it reveals nothing about the stored tag on failure.
	if !crypto.DetectionTagMatches(msg.Z, secret.DetectionHint.Tag) {
		return nil, coserr.Wrapf(types.ErrInvalidRecipiencyProof,
			"proof does not match the detection hint of secret %s", msg.SecretId)
	}

	// Priority: this signer must have committed to this proof in an earlier
	// block. Both halves matter — the commitment must match the proof, and it
	// must predate this block.
	commitKey := collections.Join(msg.SecretId, msg.Recipient)
	committed, err := ms.k.RebateCommitments.Get(ctx, commitKey)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, coserr.Wrapf(types.ErrNoRebateCommitment,
				"address %s has no commitment on secret %s", msg.Recipient, msg.SecretId)
		}
		return nil, coserr.Wrapf(err, "failed to read rebate commitment for %s", msg.SecretId)
	}
	if committed.CommittedAt >= sdkCtx.BlockHeight() {
		return nil, coserr.Wrapf(types.ErrCommitmentTooRecent,
			"commitment on secret %s was recorded at height %d; reveal must land later",
			msg.SecretId, committed.CommittedAt)
	}
	if !crypto.RebateCommitmentMatches(msg.Z, recipientAddr.Bytes(), committed.Commitment) {
		return nil, coserr.Wrapf(types.ErrCommitmentMismatch,
			"proof does not reproduce %s's commitment on secret %s", msg.Recipient, msg.SecretId)
	}

	amount, err := ms.k.PayRebate(ctx, secret, recipientAddr)
	if err != nil {
		return nil, err
	}
	if err := ms.k.RebateCommitments.Remove(ctx, commitKey); err != nil {
		return nil, coserr.Wrapf(err, "failed to clear the spent commitment on %s", msg.SecretId)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventRebateCollected,
			sdk.NewAttribute("secret_id", secret.Id),
			sdk.NewAttribute("recipient", msg.Recipient),
			sdk.NewAttribute("amount", sdk.NewCoin(types.DefaultDenom, amount).String()),
		),
	)

	return &types.MsgRecipientCollectRebateResponse{Amount: amount.String()}, nil
}
