package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/timeflareio/chain/x/secrets/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns an implementation of the MsgServer interface for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

func (ms msgServer) UserCancelSecret(ctx context.Context, msg *types.MsgUserCancelSecret) (*types.MsgUserCancelSecretResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// CRITICAL: Validate transaction signer matches sender address
	sender, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address: %s", err)
	}

	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(sender) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "transaction must be signed by the sender")
	}

	// Get the secret
	secret, err := ms.k.GetSecret(ctx, msg.SecretId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrSecretNotFound, "secret %s not found", msg.SecretId)
	}

	// Only creator can abort
	if secret.Creator != msg.Creator {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "only creator can cancel secret")
	}

	// Cancellation is a post-activation mechanic: it exists to release bonded
	// guardians via the paid pro-rata exit, and before activation there is no
	// committed guardian set to release. Pre-activation (reserved,
	// awaiting_acceptance) the only exit is the commit-timeout — this also
	// closes the cancel-instead-of-timeout bypass of the selection-draw
	// pricing (spec.md "Secret Cancellation").
	if secret.State != types.SECRET_STATUS_PENDING {
		return nil, errorsmod.Wrapf(types.ErrInvalidSecretStatus, "can only cancel secrets in pending state (pre-activation secrets exit via commit-timeout; reconstructable and terminal secrets cannot be cancelled), current status: %s", secret.State)
	}

	// Cancellation is permitted at any point BEFORE the reveal window opens.
	// Once it opens the secret proceeds to normal settlement.
	if sdkCtx.BlockHeight() >= secret.RevealStartBlock {
		return nil, errorsmod.Wrapf(types.ErrRevealBlockPassed, "cannot cancel secret after reveal window starts")
	}

	creatorAddr, err := sdk.AccAddressFromBech32(secret.Creator)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address: %s", err)
	}

	// Cancellation is a PAID exit (spec "Cancellation and no-fault refunds"):
	// every honest locked bond is returned in full, and the pool settles
	// pro-rata by distance travelled — each honest active guardian is paid
	// rate × elapsed × bump for the blocks it actually guarded past the commit
	// deadline, and the creator is refunded the unearned remainder. Cancelling
	// during the commit phase (elapsed = 0) refunds everything. A creator can
	// never lock guardian capital for free ("paid hold" invariant).
	//
	// Guardians already slashed for an early reveal are EXCLUDED: their bond
	// is already gone (deducted at report time) and their guarding was
	// breached, so their would-be wage slice falls into the unearned remainder
	// and flows back to the creator below — mirroring settlement's exclusion,
	// with each path's forfeited value following that path's default remainder
	// flow. See spec.md "Cancellation and No-Fault Refunds".
	//
	// Any failure below propagates and fails the whole transaction — the
	// message cache gives atomic abort for free (no partial payout state can
	// commit), and the creator simply retries.
	if err := ms.k.ReleaseAllAcceptedBonds(ctx, secret); err != nil {
		return nil, errorsmod.Wrap(err, "failed to release guardian bonds")
	}

	// The wage derives from the secret's STORED economics (pool, distance,
	// max_shares) — never the live rate constant — so a software upgrade
	// retuning the rate can never re-price an in-flight secret's cancellation
	// (immutable-economics ruling, work item 2). The max_shares denominator
	// keeps the per-guardian wage constant regardless of how many accepted;
	// unfilled band slots therefore refund to the creator in the remainder.
	elapsed := sdkCtx.BlockHeight() - secret.CommitDeadline
	distance := (secret.RevealEndBlock + 1) - secret.CommitDeadline
	perGuardianPayout := types.ProRataCancellationPayout(
		secret.RewardPool.Amount, distance, secret.MaxShares, elapsed)

	// The active set is the accepted set (the roster finalised at the commit
	// deadline) — a walk over the tiny assignment records
	activeGuardians, err := ms.k.AcceptedGuardians(ctx, secret.Id)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to load accepted guardians")
	}

	totalPaid := math.ZeroInt()
	if perGuardianPayout.IsPositive() {
		payoutCoin := sdk.NewCoin(secret.RewardPool.Denom, perGuardianPayout)
		for _, activeGuardianAddr := range activeGuardians {
			if ms.k.IsGuardianSlashedForEarlyReveal(ctx, activeGuardianAddr, secret.Id) {
				sdkCtx.Logger().Info("Excluding early-slashed guardian from cancellation payout",
					"guardian", activeGuardianAddr, "secret_id", secret.Id)
				continue // its slice stays in the remainder → creator refund
			}
			guardianAddr, err := sdk.AccAddressFromBech32(activeGuardianAddr)
			if err != nil {
				return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress,
					"invalid guardian address %s during cancellation payout: %s", activeGuardianAddr, err)
			}
			if err := ms.k.bankKeeper.SendCoinsFromModuleToAccount(
				ctx, types.ModuleName, guardianAddr, sdk.NewCoins(payoutCoin),
			); err != nil {
				return nil, errorsmod.Wrapf(err,
					"failed to send cancellation payout to guardian %s", activeGuardianAddr)
			}
			totalPaid = totalPaid.Add(perGuardianPayout)

			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"guardian_cancellation_payout",
					sdk.NewAttribute(types.AttributeKeySecretId, msg.SecretId),
					sdk.NewAttribute("guardian", activeGuardianAddr),
					sdk.NewAttribute("amount", payoutCoin.String()),
					sdk.NewAttribute("elapsed_blocks", fmt.Sprintf("%d", elapsed)),
				),
			)
		}
	}

	// Cancellation is a terminal state, so the acceptance reimbursement
	// settles here too: every guardian that accepted did the job asked of it
	// and is made whole for that transaction, whatever the creator later
	// decided. Early-slashed guardians are excluded on the same basis as the
	// wage above — their slice stays in the creator's refund.
	feeEarners := make([]string, 0, len(activeGuardians))
	for _, activeGuardianAddr := range activeGuardians {
		if ms.k.IsGuardianSlashedForEarlyReveal(ctx, activeGuardianAddr, secret.Id) {
			continue
		}
		feeEarners = append(feeEarners, activeGuardianAddr)
	}
	if err := ms.k.distributeAcceptFees(ctx, secret, feeEarners); err != nil {
		return nil, errorsmod.Wrap(err, "failed to settle accept fees on cancellation")
	}

	// Refund the unearned remainder of the pool to the creator
	creatorRefund := secret.RewardPool.Amount.Sub(totalPaid)
	if creatorRefund.IsPositive() {
		refundCoin := sdk.NewCoin(secret.RewardPool.Denom, creatorRefund)
		if err := ms.k.bankKeeper.SendCoinsFromModuleToAccount(
			ctx, types.ModuleName, creatorAddr, sdk.NewCoins(refundCoin),
		); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund creator")
		}
	}

	// Use state machine to transition to cancelled state
	if err := ms.k.TransitionSecretState(ctx, &secret, EventSecretCancelled); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transition secret to cancelled state")
	}

	// The secret is terminal: retire the settlement entry so EndBlock never
	// revisits it. The commit entry is already gone — cancellation is
	// pending-only, and pending is only ever reached by the deadline
	// finalisation that consumed that entry.
	ms.k.dequeueSettlement(ctx, secret)

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSecretCancelled,
			sdk.NewAttribute(types.AttributeKeySecretId, msg.SecretId),
			sdk.NewAttribute(types.AttributeKeyCreator, msg.Creator),
			sdk.NewAttribute("guardians_paid", fmt.Sprintf("%d", len(activeGuardians))),
			sdk.NewAttribute("per_guardian_payout", perGuardianPayout.String()),
			sdk.NewAttribute("creator_refund", creatorRefund.String()),
			sdk.NewAttribute("elapsed_blocks", fmt.Sprintf("%d", elapsed)),
		),
	)

	return &types.MsgUserCancelSecretResponse{}, nil
}
