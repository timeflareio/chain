package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/types"
)

// Guardian float accounting primitives.
//
// A guardian's float is held in module escrow and partitioned into locked
// (per-secret bonds) and unlocked (withdrawable working capital):
//
//	total    = guardian.Stake
//	locked   = guardian.LockedStake
//	unlocked = total − locked
//
// Accepting a secret locks the bond; settlement releases it back to unlocked
// (return), removes it from the float entirely (slash), or a mix of both.
// All mutations MUST go through these helpers so the invariants hold:
//
//	0 ≤ locked ≤ total, always.

// floatAmounts extracts the (total, locked) amounts, treating nil coins as zero.
func floatAmounts(guardian *types.Guardian) (total, locked math.Int) {
	total, locked = math.ZeroInt(), math.ZeroInt()
	if guardian.Stake != nil {
		total = guardian.Stake.Amount
	}
	if guardian.LockedStake != nil {
		locked = guardian.LockedStake.Amount
	}
	return total, locked
}

// UnlockedFloat returns the guardian's unlocked float (total − locked).
func UnlockedFloat(guardian *types.Guardian) math.Int {
	total, locked := floatAmounts(guardian)
	return total.Sub(locked)
}

// LockGuardianFloat moves amount from the guardian's unlocked float into the
// locked portion (a bond lock) and increments the active-bond counter. Fails
// if the unlocked float is insufficient OR the guardian is at the concurrency
// cap — the caller must treat either as a rejected acceptance, never a
// partial lock. The cap re-check here (not just at selection) is normative:
// a guardian can be in flight on several selections at once, and the cap
// gates the moment a bond actually locks (docs/spec.md "Guardian Acceptance").
func (k Keeper) LockGuardianFloat(ctx context.Context, guardianAddress string, amount math.Int) error {
	guardian, found := k.GetGuardian(ctx, guardianAddress)
	if !found {
		return fmt.Errorf("guardian not found: %s", guardianAddress)
	}

	if guardian.ActiveBondCount >= types.MaxActiveBondsPerGuardian {
		return fmt.Errorf("guardian %s is at the concurrency cap (%d active bonds)",
			guardianAddress, types.MaxActiveBondsPerGuardian)
	}

	unlocked := UnlockedFloat(&guardian)
	if unlocked.LT(amount) {
		return fmt.Errorf("insufficient unlocked float: guardian %s has %s unlocked, bond requires %s",
			guardianAddress, unlocked.String(), amount.String())
	}

	_, locked := floatAmounts(&guardian)
	newLocked := locked.Add(amount)
	guardian.LockedStake = &sdk.Coin{Denom: types.DefaultDenom, Amount: newLocked}
	guardian.ActiveBondCount++
	return k.SetGuardian(ctx, guardian)
}

// UnlockGuardianFloat releases amount from the locked portion back to unlocked
// (a bond return). The float total is unchanged.
func (k Keeper) UnlockGuardianFloat(ctx context.Context, guardianAddress string, amount math.Int) error {
	guardian, found := k.GetGuardian(ctx, guardianAddress)
	if !found {
		return fmt.Errorf("guardian not found: %s", guardianAddress)
	}

	_, locked := floatAmounts(&guardian)
	if locked.LT(amount) {
		return fmt.Errorf("cannot unlock %s: guardian %s has only %s locked",
			amount.String(), guardianAddress, locked.String())
	}

	newLocked := locked.Sub(amount)
	guardian.LockedStake = &sdk.Coin{Denom: types.DefaultDenom, Amount: newLocked}
	return k.SetGuardian(ctx, guardian)
}

// ReleaseAllAcceptedBonds unlocks the secret's bond for every guardian that
// ACCEPTED an assignment on it — the no-fault paths (commit-timeout,
// cancellation) where every honest bond is returned in full. Guardians
// already slashed for an early reveal are skipped: their bond was fully
// deducted at report time, so there is nothing to release. A failure on any
// guardian aborts the whole release: these errors are assertions (a bug has
// already corrupted the bond accounting), and both callers are atomic — the
// commit-expiry sweep runs inside a per-secret cache-commit that discards the
// partial unlocks and retries next block, and MsgUserCancelSecret fails the
// transaction wholesale.
func (k Keeper) ReleaseAllAcceptedBonds(ctx context.Context, secret types.Secret) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	accepted, err := k.AcceptedGuardians(ctx, secret.Id)
	if err != nil {
		return fmt.Errorf("failed to load accepted guardians for bond release on secret %s: %w", secret.Id, err)
	}
	for _, guardianAddress := range accepted {
		if k.IsGuardianSlashedForEarlyReveal(ctx, guardianAddress, secret.Id) {
			continue // bond already fully deducted (and counter released) at report time
		}
		bond, ok := secret.GuardianBondAmount(guardianAddress)
		if !ok {
			return fmt.Errorf("no frozen bond recorded for accepted guardian %s on secret %s — state-integrity violation",
				guardianAddress, secret.Id)
		}
		if err := k.UnlockGuardianFloat(ctx, guardianAddress, bond); err != nil {
			return fmt.Errorf("failed to release bond %s for guardian %s on secret %s: %w",
				bond.String(), guardianAddress, secret.Id, err)
		}
		if err := k.DecrementActiveBonds(ctx, guardianAddress); err != nil {
			return fmt.Errorf("failed to release active-bond slot for guardian %s on secret %s: %w",
				guardianAddress, secret.Id, err)
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"guardian_bond_released",
				sdk.NewAttribute("secret_id", secret.Id),
				sdk.NewAttribute("guardian", guardianAddress),
				sdk.NewAttribute("bond", sdk.NewCoin(types.DefaultDenom, bond).String()),
			),
		)
	}
	return nil
}

// DecrementActiveBonds releases one active-bond slot when a guardian's bond
// on a secret is finally disposed (settlement return, no-reveal slash,
// early-reveal slash, cancellation or commit-expiry release). A zero counter
// here is an assertion — the books are already wrong.
func (k Keeper) DecrementActiveBonds(ctx context.Context, guardianAddress string) error {
	guardian, found := k.GetGuardian(ctx, guardianAddress)
	if !found {
		return fmt.Errorf("guardian not found: %s", guardianAddress)
	}
	if guardian.ActiveBondCount <= 0 {
		return fmt.Errorf("cannot release active-bond slot: guardian %s has none in flight", guardianAddress)
	}
	guardian.ActiveBondCount--
	return k.SetGuardian(ctx, guardian)
}

// AdjustBondKOnSlash steps the guardian's bond multiplier up after a slash
// (either violation): k′ = min(MaxBondK, k × 126 ÷ 100). Applied at the
// event — no-reveal settlement or early-reveal report — and affects only
// future selections; frozen bonds never move (Position A).
func (k Keeper) AdjustBondKOnSlash(ctx context.Context, guardianAddress string) error {
	return k.adjustBondK(ctx, guardianAddress, types.NextBondKAfterSlash, "slash")
}

// AdjustBondKOnReveal steps the guardian's bond multiplier down after a
// correct on-chain reveal: k′ = max(MinBondK, k × 963 ÷ 1000) — recovery is
// deliberately ~6× slower than the climb.
func (k Keeper) AdjustBondKOnReveal(ctx context.Context, guardianAddress string) error {
	return k.adjustBondK(ctx, guardianAddress, types.NextBondKAfterReveal, "reveal")
}

func (k Keeper) adjustBondK(ctx context.Context, guardianAddress string, next func(int64) int64, cause string) error {
	guardian, found := k.GetGuardian(ctx, guardianAddress)
	if !found {
		return fmt.Errorf("guardian not found: %s", guardianAddress)
	}
	previous := types.ClampBondK(guardian.BondK)
	guardian.BondK = next(guardian.BondK)
	if err := k.SetGuardian(ctx, guardian); err != nil {
		return err
	}
	if guardian.BondK != previous {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			sdk.NewEvent(
				"guardian_bond_k_adjusted",
				sdk.NewAttribute("guardian", guardianAddress),
				sdk.NewAttribute("cause", cause),
				sdk.NewAttribute("previous_k", fmt.Sprintf("%d", previous)),
				sdk.NewAttribute("new_k", fmt.Sprintf("%d", guardian.BondK)),
			),
		)
	}
	return nil
}

// DeductLockedFloat removes amount from BOTH the locked portion and the float
// total (a slash: the funds leave the guardian's float; the caller is
// responsible for distributing/burning the corresponding module-held coins).
func (k Keeper) DeductLockedFloat(ctx context.Context, guardianAddress string, amount math.Int) error {
	guardian, found := k.GetGuardian(ctx, guardianAddress)
	if !found {
		return fmt.Errorf("guardian not found: %s", guardianAddress)
	}

	total, locked := floatAmounts(&guardian)
	if locked.LT(amount) {
		return fmt.Errorf("cannot deduct %s: guardian %s has only %s locked",
			amount.String(), guardianAddress, locked.String())
	}

	guardian.Stake = &sdk.Coin{Denom: types.DefaultDenom, Amount: total.Sub(amount)}
	guardian.LockedStake = &sdk.Coin{Denom: types.DefaultDenom, Amount: locked.Sub(amount)}
	return k.SetGuardian(ctx, guardian)
}
