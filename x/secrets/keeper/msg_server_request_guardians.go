package keeper

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/timeflareio/chain/x/secrets/types"
)

// UserRequestGuardians handles Phase 1 of the three-phase commit protocol
// This assigns guardians and returns their public keys for client-side encryption
func (ms msgServer) UserRequestGuardians(ctx context.Context, msg *types.MsgUserRequestGuardians) (*types.MsgUserRequestGuardiansResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Step 1: Validate basic message parameters
	if err := ms.validateUserRequestGuardiansMessage(msg); err != nil {
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

	// Step 2: Assign the protocol-derived secret ID. The monotonic counter is
	// read in consensus transaction order (deterministic across validators),
	// never repeats, and is outside the creator's control — it also
	// differentiates the selection seeds of secrets in the same block.
	secretId, secretCounter, err := ms.k.NextSecretId(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to assign secret ID")
	}

	// Step 3: Invariant guard — a strictly increasing counter cannot produce a
	// duplicate ID unless state was imported inconsistently
	if ms.k.HasSecret(ctx, secretId) {
		return nil, errorsmod.Wrapf(types.ErrSecretExists, "secret with ID %s already exists", secretId)
	}

	// Step 4: Validate reveal window timing
	if err := ms.validateRevealStartOffset(msg.RevealStartOffset); err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidRevealBlock, "invalid reveal window: %s", err)
	}

	// Step 5: Validate threshold and the guardian band (ordering + strict
	// gap bound) — the same authoritative rule ValidateBasic enforces
	if err := types.ValidateShareBand(msg.Threshold, msg.MinShares, msg.MaxShares); err != nil {
		return nil, err
	}

	// Step 6: Calculate reveal timing before guardian selection. The window's
	// length is derived from the hold the offset implies, not chosen — both
	// heights are absolute from here on, stored on the record and never
	// recomputed (docs/spec.md "The Reveal Window").
	revealStartBlock := sdkCtx.BlockHeight() + msg.RevealStartOffset
	revealEndBlock := revealStartBlock + types.RevealWindowForStartOffset(msg.RevealStartOffset)
	commitDeadline := sdkCtx.BlockHeight() + types.CommitTimeoutBlocks

	// Step 7: Derive the economics for this secret — the reward pool is priced
	// by protocol formula (P = rate × distance × max_shares × bump) on the
	// band ceiling and FIXED (unfilled slots are never refunded); each
	// selected guardian's bond (B_g = rate × distance × bump × k_g, priced by
	// its own live bond multiplier) is frozen on the secret at selection so
	// settlement releases exactly what acceptance locked.
	// distance runs from commit_deadline to settlement. The reveal window is
	// inclusive of reveal_end_block, so settlement (and bond release) happens
	// at end + 1 — every block a bond stays locked is a block paid for.
	distance := (revealEndBlock + 1) - commitDeadline
	rewardPool := sdk.NewCoin(types.DefaultDenom, types.RewardPoolAmount(distance, msg.MaxShares, msg.Bump))
	// The acceptance reimbursement is escrowed alongside the pool but held
	// apart from it: the two are earned by different acts and settle on
	// different rules (docs/spec.md "Secret Economics & Slashing").
	acceptFees := sdk.NewCoin(types.DefaultDenom, types.AcceptFeesAmount(msg.MaxShares))

	// Step 8: Protocol-controlled guardian selection (with reveal window,
	// concurrency cap and per-candidate bond affordability consideration);
	// freezes each winner's bond amount alongside the selection
	selectedGuardians, guardianBonds, selectionSeed, err := ms.selectGuardiansForRequest(ctx, msg, revealEndBlock, distance, secretCounter)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidGuardian, "guardian selection failed: %s", err)
	}

	// Step 9: Lock the derived reward pool and the acceptance reimbursement
	// from the creator. Both are module escrow; both are released only at the
	// secret's terminal state.
	if err := ms.lockRewardPool(ctx, msg.Creator, rewardPool.Add(acceptFees)); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "failed to lock reward pool: %s", err)
	}

	// Step 9b: Charge the non-refundable creation fee — the price of this
	// selection draw and the recurring validator budget (docs/spec.md
	// "Creation Fee"). It goes straight to the fee collector (never module
	// escrow, so no refund path can touch it) and rides the next block's
	// 90/10 split. Atomic with the rest of the handler: a failed request
	// charges nothing.
	// Priced on the pool's time component only — never on the gas
	// reimbursements the creator also funds, which are a pass-through and not
	// part of what a selection draw is worth.
	timeComponent := types.TimeComponentAmount(distance, msg.MaxShares, msg.Bump)
	creationFee := sdk.NewCoin(types.DefaultDenom, types.CreationFee(timeComponent, distance))
	if err := ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, creator, authtypes.FeeCollectorName, sdk.NewCoins(creationFee)); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "failed to charge creation fee %s: %s", creationFee, err)
	}

	// Step 10: Create and store secret record in RESERVED state
	secret := ms.createRequestedSecretRecord(secretId, msg, selectedGuardians, guardianBonds, sdkCtx.BlockHeight(), revealStartBlock, revealEndBlock, rewardPool, acceptFees)
	if err := ms.k.SetSecret(ctx, secret); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store secret")
	}
	if err := ms.k.IndexSecretCreator(ctx, secret); err != nil {
		return nil, errorsmod.Wrap(err, "failed to index secret creator")
	}
	if err := ms.k.IndexSecretCreation(ctx, secret); err != nil {
		return nil, errorsmod.Wrap(err, "failed to index secret creation hint")
	}

	// Register the secret's deadlines in the due-height queues so EndBlock
	// only ever touches secrets that are actually due
	if err := ms.k.EnqueueSecretDeadlines(ctx, secret); err != nil {
		return nil, errorsmod.Wrap(err, "failed to enqueue secret deadlines")
	}

	// Step 11: Log secret creation (secret is already in RESERVED state)
	sdkCtx.Logger().Info("Secret created and guardians assigned",
		"secret_id", secretId,
		"state", types.SECRET_STATUS_RESERVED,
		"guardian_count", len(selectedGuardians),
		"reward_pool", rewardPool.String(),
		"guardian_bond_amounts", formatBondAmounts(guardianBonds),
	)

	// Step 12: Emit reservation event (using pre-calculated reveal timing)
	ms.emitSecretRequestedEvent(sdkCtx, secretId, msg, revealStartBlock, revealEndBlock, int64(len(selectedGuardians)), rewardPool, acceptFees, creationFee, distance, guardianBonds, selectionSeed, secretCounter)

	// Step 13: Prepare guardian info for client (hard error on a missing
	// guardian record — the transaction aborts atomically)
	guardianInfos, err := ms.prepareGuardianInfoResponse(ctx, selectedGuardians)
	if err != nil {
		return nil, err
	}

	return &types.MsgUserRequestGuardiansResponse{
		SecretId:            secretId,
		GuardianAssignments: guardianInfos,
	}, nil
}

// validateUserRequestGuardiansMessage performs basic validation on the MsgUserRequestGuardians
func (ms msgServer) validateUserRequestGuardiansMessage(msg *types.MsgUserRequestGuardians) error {
	// Validate creator address
	if _, err := sdk.AccAddressFromBech32(msg.Creator); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address: %s", err)
	}

	// No secret commitment validation in Phase 1 - commitment comes in Phase 2

	// Validate the detection hint shape (content is deliberately unverifiable —
	// random bytes are a valid no-discovery hint)
	if err := types.ValidateDetectionHint(&msg.DetectionHint); err != nil {
		return err
	}
	// The hint's ephemeral key must still be a usable X25519 point: against a
	// small-order R every recipient derives the same all-zero shared value, so
	// one hint would match everyone. Shape, not content — see
	// validateDetectionHintKey.
	if err := ms.validateDetectionHintKey(msg.DetectionHint.EphemeralPub); err != nil {
		return err
	}

	// Validate the security factor (the reward pool is protocol-derived, not
	// creator-chosen — bump, distance and shares fully determine what is paid)
	if err := types.ValidateBump(msg.Bump); err != nil {
		return errorsmod.Wrap(types.ErrInvalidRequest, err.Error())
	}

	return nil
}

// selectGuardiansForRequest performs protocol-controlled guardian selection
// for Phase 1. Selects exactly max_shares candidates (the band ceiling) with
// reveal window, concurrency cap and per-candidate bond affordability
// consideration — hard-failing if fewer are eligible. Returns the selected
// addresses, their frozen bond amounts (aligned), and the selection seed (its
// inputs are emitted for off-chain seed verification).
func (ms msgServer) selectGuardiansForRequest(ctx context.Context, msg *types.MsgUserRequestGuardians, revealEndBlock, distance int64, secretCounter uint64) ([]string, []int64, []byte, error) {
	// Initialize guardian selector with unified keeper
	guardianSelector := NewGuardianSelector(&ms.k)

	// Select guardians (with reveal window and bond affordability consideration)
	selected, selectedBonds, seed, err := guardianSelector.SelectGuardians(
		ctx,
		msg.MaxShares,
		revealEndBlock,
		distance,
		msg.Bump,
		secretCounter,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	// Invariant guard: the sortition must return exactly the band ceiling
	if selectedCount := int64(len(selected)); selectedCount != msg.MaxShares {
		return nil, nil, nil, errorsmod.Wrapf(types.ErrInvalidGuardian,
			"guardian selection produced %d assignments, expected exactly max_shares=%d",
			selectedCount, msg.MaxShares)
	}

	return selected, selectedBonds, seed, nil
}

// createRequestedSecretRecord constructs the Secret object to be stored in RESERVED state
func (ms msgServer) createRequestedSecretRecord(secretId string, msg *types.MsgUserRequestGuardians,
	selectedGuardians []string, guardianBonds []int64, blockHeight, revealStartBlock, revealEndBlock int64,
	rewardPool, acceptFees sdk.Coin) types.Secret {

	return types.Secret{
		Id:                  secretId,
		Creator:             msg.Creator,
		RevealStartBlock:    revealStartBlock,
		RevealEndBlock:      revealEndBlock,
		Threshold:           msg.Threshold,
		MinShares:           msg.MinShares,                           // Band floor: acceptances needed to activate at the deadline
		MaxShares:           msg.MaxShares,                           // Band ceiling: candidates selected, SSS total
		State:               types.SECRET_STATUS_RESERVED,            // Phase 1: reserved
		CommitDeadline:      blockHeight + types.CommitTimeoutBlocks, // Single deadline for the complete 3-phase commit
		SelectedGuardians:   selectedGuardians,
		CreatedAt:           blockHeight,
		DetectionHint:       msg.DetectionHint, // Recipient discovery hint; the recipient's key is never stored
		RewardPool:          rewardPool,        // Derived: P = max_shares × F_reveal + rate × distance × max_shares × bump, fixed
		AcceptFees:          acceptFees,        // A = max_shares × F_accept, escrowed apart from the pool
		Bump:                msg.Bump,
		GuardianBondAmounts: guardianBonds, // B_g = rate × distance × bump × k_g, frozen at selection, aligned with SelectedGuardians
		SecretCommitment:    []byte{},      // Set during Phase 2
	}
}

// formatBondAmounts renders the per-guardian frozen bond amounts (uveil) as a
// comma-separated list aligned with the selection order, for logs and events.
func formatBondAmounts(bonds []int64) string {
	parts := make([]string, len(bonds))
	for i, b := range bonds {
		parts[i] = strconv.FormatInt(b, 10)
	}
	return strings.Join(parts, ",")
}

// prepareGuardianInfoResponse converts the selected guardians to client-friendly
// format. A selected guardian without a registration record is a state-integrity
// violation (selection reads the guardian store), so it hard-fails the request
// rather than substituting a placeholder key — a zero public key would have the
// creator encrypt a share to silently-broken cryptography.
//
// The key handed to the creator is the key IN FORCE at this height, not the
// record's current key: a rotation ordered earlier in the same block wrote
// its new key to the record, but rotations are effective from the next block,
// so a same-block selection still encrypts to the pre-rotation key — keeping
// the handed key consistent with the epoch derivation the guardian daemon
// runs from the secret's creation height (docs/spec.md "Guardian Key
// Rotation").
func (ms msgServer) prepareGuardianInfoResponse(ctx context.Context, selectedGuardians []string) ([]*types.GuardianInfo, error) {
	guardianInfos := make([]*types.GuardianInfo, len(selectedGuardians))
	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()

	for i, address := range selectedGuardians {
		guardian, found := ms.k.GetGuardian(ctx, address)
		if !found {
			return nil, errorsmod.Wrapf(types.ErrGuardianNotFound,
				"selected guardian %s has no registration record — state-integrity violation", address)
		}

		keyInForce, err := ms.k.GuardianKeyInForce(ctx, guardian, blockHeight)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to resolve the key in force")
		}

		guardianInfos[i] = &types.GuardianInfo{
			Address:   address,
			PublicKey: keyInForce, // the epoch key the creator must encrypt to
		}
	}

	return guardianInfos, nil
}

// validateRevealStartOffset validates the reveal timing. The offset is the only
// timing value the creator supplies; the window's length is derived from it, so
// the reveal horizon reduces to a bound on the offset alone.
func (ms msgServer) validateRevealStartOffset(startOffset int64) error {
	// The floor is a constant: the fixed commit window plus the reveal buffer.
	if startOffset < types.MinRevealStartOffsetTotal {
		return fmt.Errorf("reveal start offset too small: %d blocks (minimum %d blocks = %d commit + %d buffer)",
			startOffset, types.MinRevealStartOffsetTotal, types.CommitTimeoutBlocks, types.MinRevealStartOffset)
	}

	// The ceiling is H less the longest window the derivation can return, so
	// reveal_end_block lands inside the horizon for every offset that passes.
	// H equals MaxAvailabilityWindow, meaning any such window can be covered by a
	// freshly registered guardian, and the priced distance (commit_deadline ->
	// settlement) is bounded as a consequence: distance ≤ H + 1 −
	// CommitTimeoutBlocks. See docs/spec.md "Timing Constraints".
	if startOffset > types.MaxRevealStartOffset {
		return fmt.Errorf("reveal start offset too large: %d blocks (maximum %d blocks, which is the ~1 year guardian availability cap less the longest reveal window)",
			startOffset, types.MaxRevealStartOffset)
	}

	return nil
}

// lockRewardPool locks the reward tokens from creator
func (ms msgServer) lockRewardPool(ctx context.Context, creator string, reward sdk.Coin) error {
	creatorAddr, err := sdk.AccAddressFromBech32(creator)
	if err != nil {
		return err
	}
	return ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(reward))
}

// emitSecretRequestedEvent emits the event for successful secret request
func (ms msgServer) emitSecretRequestedEvent(sdkCtx sdk.Context, secretId string, msg *types.MsgUserRequestGuardians, revealStartBlock, revealEndBlock int64, totalShares int64, rewardPool, acceptFees, creationFee sdk.Coin, distance int64, guardianBonds []int64, selectionSeed []byte, secretCounter uint64) {
	creationFeeRegime := "percent"
	if types.CreationFeeIsFloorPriced(types.TimeComponentAmount(distance, msg.MaxShares, msg.Bump), distance) {
		creationFeeRegime = "floor"
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSecretReserved,
			sdk.NewAttribute(types.AttributeKeySecretId, secretId),
			sdk.NewAttribute(types.AttributeKeyCreator, msg.Creator),
			sdk.NewAttribute("reveal_start_block", strconv.FormatInt(revealStartBlock, 10)),
			sdk.NewAttribute("reveal_end_block", strconv.FormatInt(revealEndBlock, 10)),
			sdk.NewAttribute("threshold", strconv.FormatInt(msg.Threshold, 10)),
			sdk.NewAttribute("min_shares", strconv.FormatInt(msg.MinShares, 10)),
			sdk.NewAttribute("max_shares", strconv.FormatInt(msg.MaxShares, 10)),
			sdk.NewAttribute("total_guardians", strconv.FormatInt(totalShares, 10)),
			sdk.NewAttribute("bump", strconv.FormatInt(msg.Bump, 10)),
			sdk.NewAttribute("reward_pool", rewardPool.String()),
			// Escrowed apart from the pool and released at the terminal state
			// to the guardians that did the job asked of them
			sdk.NewAttribute("accept_fees", acceptFees.String()),
			// The non-refundable draw price and which side of the max()
			// priced it (docs/spec.md "Creation Fee")
			sdk.NewAttribute("creation_fee", creationFee.String()),
			sdk.NewAttribute("creation_fee_regime", creationFeeRegime),
			// Per-guardian frozen bonds (uveil), aligned with the selection
			// order — bonds are priced by each guardian's own k, so there is
			// no single per-guardian amount any more
			sdk.NewAttribute("guardian_bond_amounts", formatBondAmounts(guardianBonds)),
			// Seed inputs for off-chain verification (Claim A): anyone can
			// confirm the seed was SHA256(chainID ‖ uint64_be(height) ‖
			// lastBlockHash ‖ uint64_be(counter)) — none of the inputs are the
			// creator's, so the selection could not have been biased. The
			// candidate set itself is deliberately not emitted.
			sdk.NewAttribute("selection_height", strconv.FormatInt(sdkCtx.BlockHeight(), 10)),
			sdk.NewAttribute("selection_last_block_hash", hex.EncodeToString(sdkCtx.BlockHeader().LastBlockId.Hash)),
			sdk.NewAttribute("secret_counter", strconv.FormatUint(secretCounter, 10)),
			sdk.NewAttribute("selection_seed", hex.EncodeToString(selectionSeed)),
			sdk.NewAttribute("phase", "1_reserved"),
		),
	)
}
