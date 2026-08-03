package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/timeflareio/chain/x/secrets/types"
)

// SlashGuardian handles early-reveal reports. On valid HMAC evidence the
// guardian's FULL bond for the secret is slashed immediately — 40% burned,
// 10% to the creator, 50% to the reporter, nothing returned — and the
// guardian is marked for exclusion from the reward pool at settlement.
//
// Reports are accepted only before the reveal window opens: once shares are
// due to be public, "early" evidence is moot. By construction a bond can
// therefore never be slashed after it has been released.
// See docs/spec.md "Early-Reveal Reporting".
func (ms msgServer) SlashGuardian(goCtx context.Context, msg *types.MsgSlashGuardian) (*types.MsgSlashGuardianResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	// Validate reporter address
	reporter, err := sdk.AccAddressFromBech32(msg.ReporterAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid reporter address: %s", err)
	}

	// CRITICAL: Validate transaction signer matches reporter address
	signers := msg.GetSigners()
	if len(signers) != 1 {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(reporter) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "transaction must be signed by the reporter")
	}

	// Validate message fields
	if err := ms.validateSlashGuardianMessage(msg); err != nil {
		return nil, err
	}

	// Prevent trivial same-address self-slashing for bounty farming. (Self-
	// reporting via a second address remains possible and is acknowledged in
	// the spec — the guaranteed loss is the burn + creator slices.)
	if reporter.String() == msg.GuardianAddress {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "guardian cannot slash themselves")
	}

	// Get guardian to slash
	if _, found := ms.k.GetGuardian(sdkCtx, msg.GuardianAddress); !found {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "guardian not found: %s", msg.GuardianAddress)
	}

	// Get the secret to check timing and reveal status
	secret, err := ms.k.GetSecret(sdkCtx, msg.SecretId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "secret not found: %s", err)
	}

	// Reports are valid only BEFORE the reveal window opens
	if sdkCtx.BlockHeight() >= secret.RevealStartBlock {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"early-reveal reports are only accepted before the reveal window opens (window opened at block %d, current: %d)",
			secret.RevealStartBlock, sdkCtx.BlockHeight())
	}

	// Prevent duplicate slashing for the same guardian and secret
	if ms.k.IsGuardianSlashedForEarlyReveal(goCtx, msg.GuardianAddress, msg.SecretId) {
		return nil, errorsmod.Wrapf(types.ErrAlreadySlashed,
			"guardian %s already slashed for early reveal on secret %s", msg.GuardianAddress, msg.SecretId)
	}

	// A guardian that already revealed on-chain cannot be reported
	if ms.k.HasGuardianRevealed(goCtx, msg.SecretId, msg.GuardianAddress) {
		return nil, errorsmod.Wrapf(types.ErrAlreadySlashed,
			"guardian %s already revealed for secret %s - cannot slash for early reveal", msg.GuardianAddress, msg.SecretId)
	}

	// The bond is locked at acceptance: only an ACCEPTED assignment has
	// collateral to slash. A pre-acceptance leak has nothing at stake (and the
	// guardian would forfeit its slot's reward by never accepting).
	record, err := ms.k.GetAssignment(goCtx, msg.SecretId, msg.GuardianAddress)
	if err != nil || record.Status != types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"guardian %s has no accepted assignment (and therefore no locked bond) on secret %s",
			msg.GuardianAddress, msg.SecretId)
	}

	// CRITICAL: Verify HMAC evidence for early reveal
	// This prevents false accusations by requiring cryptographic proof —
	// a single point-read of the guardian's share record
	share, err := ms.k.GetShareData(goCtx, msg.SecretId, msg.GuardianAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "failed to get guardian share data: %s", err)
	}
	if !ms.k.VerifyShareHMAC(msg.SecretId, msg.GuardianAddress, msg.Evidence, share.ShareHmac) {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "invalid evidence - HMAC verification failed")
	}

	// Slash the FULL bond — this guardian's own amount frozen at selection:
	// remove it from the guardian's float entirely (the coins stay in module
	// escrow until distributed below), release its active-bond slot, and step
	// its bond multiplier k up
	bond, ok := secret.GuardianBondAmount(msg.GuardianAddress)
	if !ok {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"no frozen bond recorded for guardian %s on secret %s", msg.GuardianAddress, msg.SecretId)
	}
	if err := ms.k.DeductLockedFloat(goCtx, msg.GuardianAddress, bond); err != nil {
		return nil, errorsmod.Wrap(err, "failed to deduct guardian bond")
	}
	if err := ms.k.DecrementActiveBonds(goCtx, msg.GuardianAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to release active-bond slot")
	}
	if err := ms.k.AdjustBondKOnSlash(goCtx, msg.GuardianAddress); err != nil {
		return nil, errorsmod.Wrap(err, "failed to adjust bond multiplier")
	}

	// Distribute per the early-reveal bond split: 40% burn / 10% creator / 50% reporter
	burnAmount, creatorAmount, reporterAmount := types.EarlyRevealSlashSplit(bond)

	if reporterAmount.IsPositive() {
		if err := ms.k.bankKeeper.SendCoinsFromModuleToAccount(goCtx, types.ModuleName, reporter,
			sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, reporterAmount))); err != nil {
			return nil, errorsmod.Wrap(err, "failed to send bounty to reporter")
		}
	}

	if creatorAmount.IsPositive() {
		creatorAddr, err := sdk.AccAddressFromBech32(secret.Creator)
		if err != nil {
			// Unresolvable creator: burn their slice instead (never strand funds)
			burnAmount = burnAmount.Add(creatorAmount)
		} else if err := ms.k.bankKeeper.SendCoinsFromModuleToAccount(goCtx, types.ModuleName, creatorAddr,
			sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, creatorAmount))); err != nil {
			return nil, errorsmod.Wrap(err, "failed to send compensation to secret creator")
		}
	}

	if burnAmount.IsPositive() {
		if err := ms.k.BurnSlashedFunds(goCtx, sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, burnAmount))); err != nil {
			return nil, errorsmod.Wrap(err, "failed to burn slashed funds")
		}
	}

	// Mark the guardian for exclusion from the reward pool at settlement
	if err := ms.k.MarkGuardianSlashedForEarlyReveal(goCtx, msg.GuardianAddress, msg.SecretId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to mark guardian as slashed for early reveal")
	}

	// The leaked share is public anyway: record it on-chain so it counts toward
	// reconstruction for the recipient. This does NOT restore the guardian's
	// standing — settlement excludes early-slashed guardians from bond return
	// and reward via the marker above.
	if err := ms.k.AutoGuardianRevealShare(goCtx, msg.GuardianAddress, msg.SecretId, msg.Evidence); err != nil {
		ms.k.Logger().Info("automatic reveal after slashing failed",
			"error", err,
			"guardian", msg.GuardianAddress,
			"secret_id", msg.SecretId)
	}

	// Emit events for early reveal slashing
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventGuardianSlashed,
			sdk.NewAttribute("guardian_address", msg.GuardianAddress),
			sdk.NewAttribute("reporter_address", msg.ReporterAddress),
			sdk.NewAttribute("slash_type", types.SlashTypeEarlyReveal),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("secret_id", msg.SecretId),
			sdk.NewAttribute("bond_slashed", sdk.NewCoin(types.DefaultDenom, bond).String()),
			sdk.NewAttribute("burned", burnAmount.String()),
			sdk.NewAttribute("reporter_bounty", reporterAmount.String()),
			sdk.NewAttribute("creator_compensation", creatorAmount.String()),
		),
	)

	return &types.MsgSlashGuardianResponse{}, nil
}
