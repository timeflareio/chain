package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Recipient rebate — the protocol's only distribution mechanism.
//
// A secret that reaches `revealed` credits its recipient a rebate on what the
// creator irrecoverably spent to send it, paid from the keyless rebate pool.
// Two ceilings bound it: RebateRatioPercent of that spend (so manufacturing a
// rebate is a loss at any token price) and an allowance that accrues from the
// pool's own balance (so the drain has a ceiling the protocol cannot exceed).
//
// See docs/spec.md "Recipient Rebate".

// RebatePoolBalance is the keyless rebate pool's spendable balance: what the
// module account holds, less what is already credited to secrets and awaiting
// collection. Reservations are subtracted so a credited rebate is always
// payable — the pool can never promise the same uveil twice.
func (k Keeper) RebatePoolBalance(ctx context.Context, state types.RebateState) math.Int {
	held := k.bankKeeper.GetBalance(ctx, k.accountKeeper.GetModuleAddress(types.RebatePoolName), types.DefaultDenom).Amount
	spendable := held.Sub(math.NewInt(state.Reserved))
	if !spendable.IsPositive() {
		return math.ZeroInt()
	}
	return spendable
}

// GetRebateState reads the accrual record, treating an absent record as the
// zero state. A chain that has never credited a rebate has no record, and the
// genesis of a network with no history writes none.
func (k Keeper) GetRebateState(ctx context.Context) (types.RebateState, error) {
	state, err := k.RebateState.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.RebateState{}, nil
		}
		return types.RebateState{}, fmt.Errorf("failed to read rebate state: %w", err)
	}
	return state, nil
}

// AccrueRebateAllowance advances the allowance to the current height and
// returns the updated state WITHOUT writing it — the caller writes once, after
// crediting, so a settlement that fails leaves no accrual behind.
//
// Accrual is lazy: the gap since the last accrual is applied here rather than
// every block, so an idle block does no work at all. The allowance is capped
// at RebateBurstBlocks of accrual, which is what lets a lone recipient receive
// a full rebate rather than one block's worth, while stopping an idle stretch
// from becoming a drainable lump.
//
// An unset clock (accrued_height 0 — a chain that has never credited a rebate,
// or state imported from before this mechanism) accrues from genesis. That
// cannot over-credit: the burst cap bounds any gap, however long, to one day of
// accrual. An earlier version treated the first touch as clock-setting only and
// accrued nothing, which silently swallowed the FIRST eligible secret's rebate
// on every network — caught by the devnet drill, not by unit tests that primed
// the clock.
func (k Keeper) AccrueRebateAllowance(ctx context.Context, state types.RebateState, currentHeight int64) types.RebateState {
	balance := k.RebatePoolBalance(ctx, state)

	elapsed := int64(0)
	if currentHeight > state.AccruedHeight {
		elapsed = currentHeight - state.AccruedHeight
	}

	allowance := types.AccrueRebateAllowance(math.NewInt(state.Allowance), balance, elapsed)
	state.Allowance = allowance.Int64()
	state.AccruedHeight = currentHeight
	return state
}

// CreditRebates credits each settled secret's recipient rebate in one pass
// over a settlement height, and writes the resulting accrual state.
//
// settled carries every secret that reached a terminal state at this height,
// whichever state that was: they divide the allowance equally, and a secret
// that settled anything other than `revealed` is counted but credited nothing.
// Counting them is deliberate — dividing only among revealed secrets would
// leak, through the amounts, how many secrets at a height failed.
//
// Crediting is deliberately NOT atomic with each secret's own settlement: the
// allowance is one shared quantity, so it is accrued once, divided once, and
// written once. A per-secret failure here is an assertion (the pool cannot
// under-pay a reservation it just sized), and returning an error abandons the
// whole credit pass rather than half-crediting a height.
func (k Keeper) CreditRebates(ctx context.Context, settled []types.Secret, currentHeight int64) error {
	if len(settled) == 0 {
		return nil
	}

	state, err := k.GetRebateState(ctx)
	if err != nil {
		return err
	}
	state = k.AccrueRebateAllowance(ctx, state, currentHeight)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	settlingCount := int64(len(settled))

	for _, secret := range settled {
		// Re-read the secret: the caller's copy predates settlement, so its
		// state is still `pending` or `reconstructable` and its revealed count
		// may be stale. Both the outcome test below and the spend must come
		// from the settled record.
		stored, err := k.GetSecret(ctx, secret.Id)
		if err != nil {
			return fmt.Errorf("failed to re-read secret %s to credit its rebate: %w", secret.Id, err)
		}
		if stored.State != types.SECRET_STATUS_REVEALED {
			continue // failed to reconstruct: counted in the divisor, credited nothing
		}

		amount := types.RebateAmount(
			irrecoverableSpend(stored),
			math.NewInt(state.Allowance),
			settlingCount,
		)
		if !amount.IsPositive() {
			continue // below the dust floor, or no allowance to share
		}
		stored.RebateAmount = amount.Int64()
		if err := k.SetSecret(ctx, stored); err != nil {
			return fmt.Errorf("failed to credit rebate on secret %s: %w", secret.Id, err)
		}
		if err := k.RebateExpiryQueue.Set(ctx, collections.Join(
			types.RebateCollectionDeadline(stored.TerminalAt), stored.Id)); err != nil {
			return fmt.Errorf("failed to enqueue the rebate deadline for %s: %w", secret.Id, err)
		}

		state.Allowance -= amount.Int64()
		state.Reserved += amount.Int64()

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventRebateCredited,
				sdk.NewAttribute("secret_id", secret.Id),
				sdk.NewAttribute("amount", sdk.NewCoin(types.DefaultDenom, amount).String()),
				sdk.NewAttribute("settling_count", fmt.Sprintf("%d", settlingCount)),
			),
		)
	}

	return k.RebateState.Set(ctx, state)
}

// irrecoverableSpend is the creator's unrecoverable outlay on a settled
// secret: the reward pool and the accept slices that have just been paid to
// revealing guardians. Everything refunded to the creator is excluded, and so
// is the creation fee — it is not carried on the secret record, and leaving it
// out understates the spend, which only widens the margin that makes farming a
// loss.
//
// Called after settlement has paid out, so the accept slices are exactly the
// revealers' share of A; a secret reaching `revealed` always has at least one
// revealer, so the pool was distributed in full.
func irrecoverableSpend(secret types.Secret) math.Int {
	spend := secret.RewardPool.Amount
	if secret.RevealedCount <= 0 || secret.MaxShares <= 0 {
		return spend
	}
	acceptSlice := secret.AcceptFees.Amount.QuoRaw(secret.MaxShares)
	return spend.Add(acceptSlice.MulRaw(secret.RevealedCount))
}

// ReleaseRebateReservation returns an uncollected rebate's reservation to the
// pool when its secret is pruned. The retention window is therefore the
// collection window: nothing expires separately, and nothing stays reserved
// against a secret that no longer exists.
func (k Keeper) ReleaseRebateReservation(ctx context.Context, secret types.Secret) error {
	if secret.RebateAmount <= 0 || secret.RebateCollected {
		return nil
	}

	state, err := k.GetRebateState(ctx)
	if err != nil {
		return err
	}
	state.Reserved -= secret.RebateAmount
	if state.Reserved < 0 {
		return fmt.Errorf("rebate reservation underflow releasing %d uveil for secret %s — state-integrity violation",
			secret.RebateAmount, secret.Id)
	}
	if err := k.RebateState.Set(ctx, state); err != nil {
		return fmt.Errorf("failed to release rebate reservation for %s: %w", secret.Id, err)
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventRebateExpired,
			sdk.NewAttribute("secret_id", secret.Id),
			sdk.NewAttribute("amount", sdk.NewCoin(types.DefaultDenom, math.NewInt(secret.RebateAmount)).String()),
		),
	)
	return nil
}

// PayRebate pays a credited rebate to the recipient and marks it collected.
// The caller has already proved recipiency; this is the money movement and the
// bookkeeping, and it must be atomic with that proof.
func (k Keeper) PayRebate(ctx context.Context, secret types.Secret, recipient sdk.AccAddress) (math.Int, error) {
	amount := math.NewInt(secret.RebateAmount)
	coins := sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, amount))

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.RebatePoolName, recipient, coins); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to pay rebate for %s: %w", secret.Id, err)
	}

	secret.RebateCollected = true
	if err := k.SetSecret(ctx, secret); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to mark rebate collected on %s: %w", secret.Id, err)
	}

	state, err := k.GetRebateState(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	state.Reserved -= secret.RebateAmount
	if state.Reserved < 0 {
		return math.ZeroInt(), fmt.Errorf("rebate reservation underflow paying %s uveil for secret %s — state-integrity violation",
			amount, secret.Id)
	}
	if err := k.RebateState.Set(ctx, state); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to release paid rebate reservation for %s: %w", secret.Id, err)
	}

	return amount, nil
}

// ClearRebateCommitments sweeps every commitment left against a secret when it
// is pruned. Losing commitments do not expire on their own — nobody has a
// reason to clean up after a rebate they failed to collect — so pruning is
// where they go.
func (k Keeper) ClearRebateCommitments(ctx context.Context, secretID string) error {
	rng := collections.NewPrefixedPairRange[string, string](secretID)
	var keys []collections.Pair[string, string]
	if err := k.RebateCommitments.Walk(ctx, rng, func(key collections.Pair[string, string], _ types.RebateCommitmentRecord) (bool, error) {
		keys = append(keys, key)
		return false, nil
	}); err != nil {
		return fmt.Errorf("failed to walk rebate commitments for %s: %w", secretID, err)
	}
	for _, key := range keys {
		if err := k.RebateCommitments.Remove(ctx, key); err != nil {
			return fmt.Errorf("failed to clear rebate commitment for %s: %w", secretID, err)
		}
	}
	return nil
}

// ProcessExpiredRebates voids the rebates whose collection window has closed and
// returns their reservations to the pool.
//
// Queue-driven like every other deadline in the module: only due entries are
// read, so an idle block does no work. Voiding zeroes the credited amount, which
// keeps the invariant "reserved == the sum of uncollected credited rebates"
// exactly true — an expired rebate is simply no longer credited.
func (k Keeper) ProcessExpiredRebates(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	due, err := drainDueEntries(ctx, k.RebateExpiryQueue, currentHeight)
	if err != nil {
		return err
	}

	for _, key := range due {
		secretID := key.K2()

		secret, err := k.GetSecret(ctx, secretID)
		if err != nil {
			// Pruned already (possible only under a retention override shorter
			// than the collection window) — the prune path released it.
			if rmErr := k.RebateExpiryQueue.Remove(ctx, key); rmErr != nil {
				sdkCtx.Logger().Error("failed to remove stale rebate expiry entry", "secret_id", secretID, "error", rmErr)
			}
			continue
		}

		cacheCtx, writeCache := sdkCtx.CacheContext()
		if err := k.voidRebate(cacheCtx, secret); err != nil {
			// Retained for retry next block, and alarmed on the same channel as a
			// stalled settlement: the cause is the same class of state-integrity
			// failure, and a stuck reservation suppresses the accrual rate.
			k.reportSettlementStall(sdkCtx, types.StalledOpSettlement, secretID, err)
			continue
		}
		writeCache()

		if rmErr := k.RebateExpiryQueue.Remove(ctx, key); rmErr != nil {
			sdkCtx.Logger().Error("failed to dequeue processed rebate expiry", "secret_id", secretID, "error", rmErr)
		}
	}

	return nil
}

// voidRebate releases an uncollected rebate's reservation and zeroes the credit,
// so nothing on chain still claims funds the pool has taken back.
func (k Keeper) voidRebate(ctx context.Context, secret types.Secret) error {
	if secret.RebateAmount <= 0 || secret.RebateCollected {
		return nil // collected in time, or never credited
	}

	if err := k.ReleaseRebateReservation(ctx, secret); err != nil {
		return err
	}
	if err := k.ClearRebateCommitments(ctx, secret.Id); err != nil {
		return err
	}

	secret.RebateAmount = 0
	if err := k.SetSecret(ctx, secret); err != nil {
		return fmt.Errorf("failed to void the expired rebate on %s: %w", secret.Id, err)
	}
	return nil
}
