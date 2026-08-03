package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/timeflareio/chain/x/secrets/types"
)

// UserDistributeShares handles Phase 2 of the three-phase commit protocol
// This receives the encrypted shares and moves the secret to AWAITING_ACCEPTANCE state
func (ms msgServer) UserDistributeShares(ctx context.Context, msg *types.MsgUserDistributeShares) (*types.MsgUserDistributeSharesResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Step 1: Validate basic message parameters
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if err := ms.validateUserDistributeSharesMessage(msg); err != nil {
		return nil, err
	}

	// CRITICAL: Validate transaction signer matches creator address
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address: %s", err)
	}

	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(creator) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "transaction must be signed by the creator")
	}

	// Step 2: Get the reserved secret
	secret, err := ms.k.GetSecret(ctx, msg.SecretId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrSecretNotFound, "secret %s not found", msg.SecretId)
	}

	// Step 3: Check if commit timeout has passed
	if sdkCtx.BlockHeight() > secret.CommitDeadline {
		return nil, errorsmod.Wrapf(types.ErrTimeoutPassed,
			"commit deadline passed at block %d, current block: %d",
			secret.CommitDeadline, sdkCtx.BlockHeight())
	}

	// Step 4: Validate secret is in RESERVED state
	if secret.State != types.SECRET_STATUS_RESERVED {
		return nil, errorsmod.Wrapf(types.ErrInvalidSecretStatus,
			"secret must be in RESERVED state for finalization, current state: %s", secret.State)
	}

	// Step 5: Validate creator authorization
	if secret.Creator != msg.Creator {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "only creator can finalize secret")
	}

	// Step 5: Validate shares match the Phase-1 guardian selection
	if err := ms.validateShareAssignments(secret, msg.Shares); err != nil {
		return nil, err
	}

	// Step 5b: pk_s must be a usable X25519 key — a small-order value would make
	// the payload ciphertext C decryptable by any observer
	if err := ms.validateSecretPublicKey(msg.SecretPublicKey); err != nil {
		return nil, err
	}

	// Step 6: Secret commitment verification happens client-side
	// The protocol stores the client's commitment for client-side reconstruction verification

	// Step 7: Write each guardian's encrypted share (cold, immutable) and its
	// PROPOSED assignment record (hot, tiny) into the side-stores. Guardians
	// selected in Phase 1 that received no share get no records — they can
	// never accept, exactly as before.
	for _, share := range msg.Shares {
		if err := ms.k.SecretShares.Set(ctx, collections.Join(msg.SecretId, share.GuardianAddress), types.SecretShareData{
			EncryptedShare: share.EncryptedShare,
			ShareHmac:      share.ShareHmac,
		}); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store encrypted share")
		}
		if err := ms.k.SetAssignment(ctx, msg.SecretId, share.GuardianAddress, types.AssignmentRecord{
			Status:           types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED,
			RespondedAtBlock: 0,
		}); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store assignment record")
		}
	}

	// Step 8: Store the payload ciphertext C exactly once — the only copy of
	// the (doubly encrypted) secret material on chain. Its pre-window
	// confidentiality rests entirely on the key shares (see spec.md).
	if err := ms.k.SecretPayloads.Set(ctx, msg.SecretId, msg.PayloadCiphertext); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store payload ciphertext")
	}

	// Step 9: Update secret commitment and record pk_s for fault attribution
	secret.SecretCommitment = msg.SecretCommitment
	secret.SecretPublicKey = msg.SecretPublicKey

	// Note: CommitDeadline was already set in Phase 1 for the entire 3-phase process

	// Step 10: Transition to awaiting acceptance — the FSM write persists the
	// updates above in the same single store write
	if err := ms.k.TransitionSecretState(ctx, &secret, EventSharesDistributed); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transition secret to awaiting acceptance state")
	}

	// Step 11: Emit finalization event
	ms.emitSecretDistributedEvent(sdkCtx, msg.SecretId, msg)

	return &types.MsgUserDistributeSharesResponse{}, nil
}

// validateUserDistributeSharesMessage performs basic validation on the MsgUserDistributeShares
func (ms msgServer) validateUserDistributeSharesMessage(msg *types.MsgUserDistributeShares) error {
	// Validate creator address
	if _, err := sdk.AccAddressFromBech32(msg.Creator); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address: %s", err)
	}

	// Validate secret ID
	if err := types.ValidateSecretIdRequired(msg.SecretId); err != nil {
		return err
	}

	// Validate we have shares
	if len(msg.Shares) == 0 {
		return errorsmod.Wrapf(types.ErrInvalidRequest, "no encrypted shares provided")
	}

	// Validate secret commitment (client-provided SHA256)
	if len(msg.SecretCommitment) == 0 {
		return errorsmod.Wrapf(types.ErrInvalidRequest, "secret commitment cannot be empty")
	}

	// SSS shares require only secret commitment for verification

	// Note: Timeout validation no longer needed - acceptance deadline was set in Phase 1

	// Validate each share
	for i, share := range msg.Shares {
		if err := ms.validateEncryptedShareData(share, i); err != nil {
			return errorsmod.Wrapf(types.ErrInvalidRequest, "invalid share at index %d: %s", i, err)
		}
	}

	return nil
}

// validateEncryptedShareData validates a single encrypted share
func (ms msgServer) validateEncryptedShareData(share *types.EncryptedShareData, index int) error {
	// Validate guardian address
	if _, err := sdk.AccAddressFromBech32(share.GuardianAddress); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address: %s", err)
	}

	// Share index no longer tracked - SSS handles intrinsic IDs

	// Validate encrypted share is not empty
	if len(share.EncryptedShare) == 0 {
		return errorsmod.Wrapf(types.ErrInvalidRequest, "encrypted share cannot be empty")
	}

	// Validate encrypted share size does not exceed maximum limit
	if int64(len(share.EncryptedShare)) > types.MaxKeyShareSize {
		return errorsmod.Wrapf(types.ErrInvalidRequest,
			"encrypted key share exceeds maximum size: %d bytes (limit: %d bytes)",
			len(share.EncryptedShare), types.MaxKeyShareSize)
	}

	// Validate HMAC is not empty (required for slashing detection)
	if len(share.ShareHmac) == 0 {
		return errorsmod.Wrapf(types.ErrInvalidRequest, "share HMAC cannot be empty")
	}

	return nil
}

// validateShareAssignments ensures provided shares meet minimum requirements and use valid guardians
func (ms msgServer) validateShareAssignments(secret types.Secret, shares []*types.EncryptedShareData) error {
	// Create map of valid guardian addresses from Phase 1 selection
	validGuardians := make(map[string]bool)
	for _, address := range secret.SelectedGuardians {
		validGuardians[address] = true
	}

	// Track guardian usage (each guardian should only appear once)
	guardianUsage := make(map[string]bool) // guardian_address -> used

	// Validate each provided share
	for _, share := range shares {
		// Verify guardian was selected in Phase 1
		if !validGuardians[share.GuardianAddress] {
			return errorsmod.Wrapf(types.ErrInvalidGuardian,
				"guardian %s was not selected for this secret", share.GuardianAddress)
		}

		// Share index no longer tracked - SSS handles intrinsic IDs

		// Check for duplicate guardian usage
		if guardianUsage[share.GuardianAddress] {
			return errorsmod.Wrapf(types.ErrInvalidRequest,
				"guardian %s appears multiple times", share.GuardianAddress)
		}
		guardianUsage[share.GuardianAddress] = true

		// No need to track share distribution - SSS handles that
	}

	// Just ensure we have at least the threshold number of guardians
	if len(shares) < int(secret.Threshold) {
		return errorsmod.Wrapf(types.ErrInvalidRequest,
			"insufficient shares: provided %d, threshold requires %d",
			len(shares), secret.Threshold)
	}

	return nil
}

// emitSecretDistributedEvent emits the event for successful secret distribution (AWAITING_ACCEPTANCE state)
func (ms msgServer) emitSecretDistributedEvent(sdkCtx sdk.Context, secretId string, msg *types.MsgUserDistributeShares) {
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSecretAwaitingAcceptance,
			sdk.NewAttribute(types.AttributeKeySecretId, secretId),
			sdk.NewAttribute(types.AttributeKeyCreator, msg.Creator),
			sdk.NewAttribute("shares_count", strconv.Itoa(len(msg.Shares))),
			sdk.NewAttribute("phase", "2_awaiting_acceptance"),
		),
	)
}
