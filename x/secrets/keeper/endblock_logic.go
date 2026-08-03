package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Settlement error handling (settlement & state-integrity plan, ruled July
// 2026). There are no transient errors on the EndBlock settlement paths:
// settlement is pure deterministic computation over committed state, the bond
// accounting is exact-amount by construction, and node-local failures surface
// as panics — so every error return below is an ASSERTION that a bug
// elsewhere has already corrupted the books. The failure model is therefore
// fail-safe, not fail-open:
//
//   - All-or-nothing per secret: each secret's expiry/settlement runs inside
//     a CacheContext and commits only if every step succeeded — there is
//     never a half-settled secret, and retries are trivially safe.
//   - Quarantine, never halt: on failure the queue entry is retained and
//     retried every block. Deliberately no panic — an EndBlock panic halts
//     every node deterministically, converting any attacker-reachable
//     trigger into a chain-wide DoS. The blast radius is one secret, its
//     funds stay locked (not lost) in module escrow, and queue neighbours
//     are unaffected.
//   - Alarm from the first failure: retry alone would be a silent liveness
//     failure, so every failed attempt emits settlement_stalled, logs at
//     error level and bumps a telemetry counter. When an upgraded binary
//     ships the underlying fix, the pending retry completes and the books
//     balance with zero migration work.

// reportSettlementStall raises the alarm for one failed settlement or
// commit-expiry attempt: event + error log + node-local telemetry counter
// (secrets.settlement_stalled.<operation>). The error text is deterministic
// (same state, same code path on every node), so it is safe in an event.
func (k Keeper) reportSettlementStall(sdkCtx sdk.Context, operation, secretId string, err error) {
	//nolint:staticcheck // OpenTelemetry adoption is deliberately out of scope for the sdk bump; upstream nolints the same package
	telemetry.IncrCounter(1, types.ModuleName, types.EventSettlementStalled, operation)
	sdkCtx.Logger().Error("settlement stalled — partial state discarded, queue entry retained for retry",
		"operation", operation, "secret_id", secretId, "error", err)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventSettlementStalled,
			sdk.NewAttribute(types.AttributeKeySecretId, secretId),
			sdk.NewAttribute("operation", operation),
			sdk.NewAttribute("error", err.Error()),
		),
	)
}

// ProcessExpiredCommits drains the commit-deadline queue: secrets whose
// commit deadline has passed (due = deadline + 1 <= current height) that are
// still in RESERVED or AWAITING_ACCEPTANCE are failed, their locked guardian
// bonds released, and the creator's reward pool refunded.
//
// Queue-driven: only due entries are read — completed secrets are never
// revisited, and an idle block reads at most one key. Each expiry runs inside
// a per-secret cache-commit (processing and dequeue land together or not at
// all); a failure discards the partial state, keeps the entry for retry next
// block, and raises the settlement_stalled alarm.
func (k Keeper) ProcessExpiredCommits(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	due, err := drainDueEntries(ctx, k.CommitQueue, currentHeight)
	if err != nil {
		return err
	}

	for _, key := range due {
		secretId := key.K2()

		secret, err := k.GetSecret(ctx, secretId)
		if err != nil {
			// Secret no longer exists — stale entry, drop it
			if rmErr := k.CommitQueue.Remove(ctx, key); rmErr != nil {
				sdkCtx.Logger().Error("failed to remove stale commit entry", "secret_id", secretId, "error", rmErr)
			}
			continue
		}

		// State guard: the deadline only applies while the commit phase is
		// still open. Anything else (activated, cancelled, already failed)
		// means this entry is obsolete — dequeue without touching the secret.
		if secret.State != types.SECRET_STATUS_RESERVED &&
			secret.State != types.SECRET_STATUS_AWAITING_ACCEPTANCE {
			if rmErr := k.CommitQueue.Remove(ctx, key); rmErr != nil {
				sdkCtx.Logger().Error("failed to remove obsolete commit entry", "secret_id", secretId, "error", rmErr)
			}
			continue
		}

		// Process this expired commit atomically: refund, bond release,
		// transition and dequeue commit together or not at all. On failure
		// the partial state is discarded, the entry is retained for retry
		// next block (the state stays non-terminal), and the alarm fires.
		cacheCtx, writeCache := sdkCtx.CacheContext()
		if err := k.expireCommitAndDequeue(cacheCtx, secret, key); err != nil {
			k.reportSettlementStall(sdkCtx, types.StalledOpCommitExpiry, secretId, err)
			continue
		}
		writeCache()
	}

	return nil
}

// expireCommitAndDequeue is the atomic unit of one commit finalisation
// (activation or failure): the caller runs it inside a CacheContext and
// commits only on success.
func (k Keeper) expireCommitAndDequeue(ctx context.Context, secret types.Secret, key collections.Pair[int64, string]) error {
	if err := k.processExpiredCommit(ctx, secret); err != nil {
		return err
	}
	return k.CommitQueue.Remove(ctx, key)
}

// activateSecretAtDeadline finalises an activating secret: FSM →
// PENDING with exactly the accepted set. The settlement-queue entry stays —
// it is the secret's next (and only remaining) EndBlock touch-point.
func (k Keeper) activateSecretAtDeadline(ctx context.Context, secret types.Secret) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := k.TransitionSecretState(ctx, &secret, EventSufficientAccepted); err != nil {
		return fmt.Errorf("failed to transition secret %s to pending state: %w", secret.Id, err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSecretPending,
			sdk.NewAttribute(types.AttributeKeySecretId, secret.Id),
			sdk.NewAttribute("active_guardians", fmt.Sprintf("%d", secret.AcceptedCount)),
			sdk.NewAttribute("min_shares", fmt.Sprintf("%d", secret.MinShares)),
			sdk.NewAttribute("max_shares", fmt.Sprintf("%d", secret.MaxShares)),
			sdk.NewAttribute("threshold", fmt.Sprintf("%d", secret.Threshold)),
			sdk.NewAttribute("total_assigned", fmt.Sprintf("%d", len(secret.SelectedGuardians))),
		),
	)

	return nil
}

// processExpiredCommit is the roster's single finalisation point, run at the
// commit deadline. A secret in AWAITING_ACCEPTANCE with at least min_shares
// acceptances ACTIVATES with exactly the accepted set (anywhere in
// [min, max]); anything else — still RESERVED, or below the band floor —
// fails with the full no-fault refund. Nothing activates mid-window: clients
// infer lock-in from accepted_count ≥ min_shares.
func (k Keeper) processExpiredCommit(ctx context.Context, secret types.Secret) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if secret.State == types.SECRET_STATUS_AWAITING_ACCEPTANCE && secret.AcceptedCount >= secret.MinShares {
		return k.activateSecretAtDeadline(ctx, secret)
	}

	// Determine appropriate event and phase based on current state
	var event string
	var phase string
	if secret.State == types.SECRET_STATUS_RESERVED {
		event = EventDistributionTimeout
		phase = "1_guardian_selection"
	} else { // AWAITING_ACCEPTANCE
		event = EventAcceptanceTimeout
		phase = "2_share_distribution"
	}

	// Refund the creator's reward pool BEFORE advancing state. If the refund
	// fails we return the error without transitioning: the caller's cache
	// discards any partial state, the secret stays in its current
	// (non-terminal) state, and the sweep retries next block. This guarantees
	// the invariant "a commit that reaches FAILED was refunded" — we never
	// strand a creator's locked reward.
	if err := k.refundRewardPool(ctx, secret.Creator, secret.RewardPool); err != nil {
		return fmt.Errorf("failed to refund reward pool for expired commit %s: %w", secret.Id, err)
	}

	// The pool is refunded in full — no guardian saw the job through — but the
	// acceptance reimbursement is not the pool. Guardians that accepted a
	// secret which then failed to activate did exactly what was asked of them
	// and are paid; the slices of guardians that never accepted go back to the
	// creator with everything else.
	acceptedForFees, err := k.AcceptedGuardians(ctx, secret.Id)
	if err != nil {
		return fmt.Errorf("failed to load accepted guardians for accept-fee settlement on %s: %w", secret.Id, err)
	}
	if err := k.distributeAcceptFees(ctx, secret, acceptedForFees); err != nil {
		return err
	}

	// Commit-timeout is a no-fault exit: unlock the bond of every guardian
	// that accepted before the deadline (fewer than min_shares did, or the
	// secret would have activated above). No slashing on this path. A release
	// failure aborts the expiry — the caller's cache discards the partial
	// unlocks.
	if err := k.ReleaseAllAcceptedBonds(ctx, secret); err != nil {
		return err
	}

	// The secret will never reach its reveal window: drop its settlement entry
	k.dequeueSettlement(ctx, secret)

	// Transition to the terminal FAILED state. The FSM mutates and persists the
	// local secret in one write; once FAILED, IsComplete() makes the sweep skip
	// it permanently.
	if err := k.TransitionSecretState(ctx, &secret, event); err != nil {
		return fmt.Errorf("failed to transition secret to failed state: %w", err)
	}

	// Emit timeout event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"secret_commit_timeout",
			sdk.NewAttribute("secret_id", secret.Id),
			sdk.NewAttribute("creator", secret.Creator),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", secret.CommitDeadline)),
			sdk.NewAttribute("current_height", fmt.Sprintf("%d", sdkCtx.BlockHeight())),
			sdk.NewAttribute("phase", phase),
		),
	)

	sdkCtx.Logger().Info("Processed expired commit",
		"secret_id", secret.Id,
		"creator", secret.Creator,
		"deadline", secret.CommitDeadline,
		"current_height", sdkCtx.BlockHeight(),
		"phase", phase)

	return nil
}

// ProcessExpiredRevealWindows drains the settlement queue and runs the
// window-end settlement (bond return, no-reveal slashing, reward
// distribution) for each due secret.
//
// The reveal window is inclusive of reveal_end_block ([start, end]), so the
// settlement entry falls due at end + 1: reveal transactions in block end are
// still valid, and the EndBlock of block end + 1 settles with the final
// revealer set. Queue-driven: only due entries are read — completed secrets
// are never revisited, and an idle block reads at most one key. Entries are
// dequeued on successful processing (or when the state guard shows settlement
// no longer applies). Each settlement runs inside a per-secret cache-commit;
// a failure discards the partial state, keeps the entry for retry next block,
// and raises the settlement_stalled alarm.
func (k Keeper) ProcessExpiredRevealWindows(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	due, err := drainDueEntries(ctx, k.SettlementQueue, currentHeight)
	if err != nil {
		return err
	}

	// Secrets that settled at this height, in queue order. They divide this
	// height's rebate allowance equally (see CreditRebates), so crediting waits
	// until every settlement has run: the count is not known until then.
	settled := make([]types.Secret, 0, len(due))

	for _, key := range due {
		secretId := key.K2()

		secret, err := k.GetSecret(ctx, secretId)
		if err != nil {
			// Secret no longer exists — stale entry, drop it
			if rmErr := k.SettlementQueue.Remove(ctx, key); rmErr != nil {
				sdkCtx.Logger().Error("failed to remove stale settlement entry", "secret_id", secretId, "error", rmErr)
			}
			continue
		}

		// State guard: settlement only applies to an active reveal phase.
		// Anything else (cancelled, failed at commit-timeout, already settled)
		// means this entry is obsolete — dequeue without touching the secret.
		if secret.State != types.SECRET_STATUS_PENDING &&
			secret.State != types.SECRET_STATUS_RECONSTRUCTABLE {
			if rmErr := k.SettlementQueue.Remove(ctx, key); rmErr != nil {
				sdkCtx.Logger().Error("failed to remove obsolete settlement entry", "secret_id", secretId, "error", rmErr)
			}
			continue
		}

		// Settle atomically: bond returns, slashes, transition, pool split
		// and dequeue commit together or not at all. On failure the partial
		// state is discarded, the entry is retained for retry next block
		// (the state stays non-terminal), and the alarm fires.
		cacheCtx, writeCache := sdkCtx.CacheContext()
		if err := k.settleSecretAndDequeue(cacheCtx, secret, key, currentHeight); err != nil {
			k.reportSettlementStall(sdkCtx, types.StalledOpSettlement, secretId, err)
			continue
		}
		writeCache()

		// A settlement that stalled is not counted: it will settle at a later
		// height and divide that height's allowance instead.
		settled = append(settled, secret)
	}

	// Credit recipient rebates for the height, inside its own cache-commit: a
	// failure here must not undo settlements that have already committed, and
	// the allowance is better left unspent than half-spent. The alarm fires on
	// the same channel as a stalled settlement, since the cause is the same
	// class of state-integrity failure.
	if len(settled) > 0 {
		cacheCtx, writeCache := sdkCtx.CacheContext()
		if err := k.CreditRebates(cacheCtx, settled, currentHeight); err != nil {
			k.reportSettlementStall(sdkCtx, types.StalledOpSettlement, settled[0].Id, err)
		} else {
			writeCache()
		}
	}

	return nil
}

// settleSecretAndDequeue is the atomic unit of one settlement: the caller
// runs it inside a CacheContext and commits only on success.
func (k Keeper) settleSecretAndDequeue(ctx context.Context, secret types.Secret, key collections.Pair[int64, string], currentHeight int64) error {
	if err := k.processExpiredSecret(ctx, secret, currentHeight); err != nil {
		return err
	}
	return k.SettlementQueue.Remove(ctx, key)
}

// processExpiredSecret runs the single window-end settlement for an expired secret.
//
// Settlement is THRESHOLD-INDEPENDENT (see docs/spec.md "Settlement"): whether the
// reveal threshold was met determines only the secret's final state (revealed vs
// failed — a cryptographic outcome), never the payments. Per guardian:
//
//   - revealed correctly (HMAC-verified at submission) → bond returned; included
//     in the equal split of the reward pool P
//   - no-reveal → bond slashed 40% burn / 10% creator / 50% returned; excluded
//   - early-slashed (bond already fully deducted at report time) → excluded
//
// P is refunded to the creator only if NO guardian revealed. Integer-division
// dust from the pool split is burned.
//
// Every error return is an assertion, not expected-failure handling: the
// caller runs this inside a per-secret CacheContext, so returning an error
// discards all partial state and quarantines the secret for retry.
func (k Keeper) processExpiredSecret(ctx context.Context, secret types.Secret, currentHeight int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Partition the active (accepted) guardians by settlement outcome. A load
	// failure aborts the settlement — settling on a partial view would
	// misclassify guardians as no-shows or refund a pool that was earned.
	revealers, noShows, err := k.partitionGuardiansForSettlement(ctx, secret)
	if err != nil {
		return fmt.Errorf("failed to partition guardians for settlement of %s: %w", secret.Id, err)
	}

	// 1. Return each revealer's bond in full (each guardian's own frozen
	// amount) and release its active-bond slot; its k was already stepped
	// down at each accepted reveal
	for _, guardianAddress := range revealers {
		bond, ok := secret.GuardianBondAmount(guardianAddress)
		if !ok {
			return fmt.Errorf("no frozen bond recorded for revealer %s on secret %s — state-integrity violation",
				guardianAddress, secret.Id)
		}
		if err := k.UnlockGuardianFloat(ctx, guardianAddress, bond); err != nil {
			return fmt.Errorf("failed to return revealer bond to %s on secret %s: %w",
				guardianAddress, secret.Id, err)
		}
		if err := k.DecrementActiveBonds(ctx, guardianAddress); err != nil {
			return fmt.Errorf("failed to release active-bond slot for revealer %s on secret %s: %w",
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

	// 2. Slash each no-show's bond by percentage — a failed slash aborts the
	// settlement rather than silently letting the guardian keep a forfeited bond
	for _, guardianAddress := range noShows {
		if err := k.slashNoRevealBond(ctx, guardianAddress, secret); err != nil {
			return fmt.Errorf("failed to slash guardian %s for no reveal on secret %s: %w",
				guardianAddress, secret.Id, err)
		}
	}

	// 3. Update secret state based on reconstruction success (cryptographic
	// outcome only — does not affect the payments above/below)
	if err := k.updateSecretStateAfterExpiry(ctx, &secret); err != nil {
		return err
	}

	// 4. Distribute the pool among revealers, or refund the creator if none.
	// The acceptance reimbursement follows the same test: at settlement, only
	// a guardian that revealed did the job it was paid to do — an acceptor
	// that then no-showed is being slashed, and keeps nothing.
	if err := k.distributeAcceptFees(ctx, secret, revealers); err != nil {
		return fmt.Errorf("failed to settle accept fees for %s: %w", secret.Id, err)
	}
	if len(revealers) == 0 {
		if err := k.refundRewardPool(ctx, secret.Creator, secret.RewardPool); err != nil {
			return fmt.Errorf("failed to refund reward pool for %s (no revealers): %w", secret.Id, err)
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"secret_rewards_refunded",
				sdk.NewAttribute("secret_id", secret.Id),
				sdk.NewAttribute("creator", secret.Creator),
				sdk.NewAttribute("amount", secret.RewardPool.String()),
				sdk.NewAttribute("reason", "no_guardians_revealed"),
			),
		)
		return nil
	}

	if err := k.distributePoolToRevealers(ctx, secret, revealers); err != nil {
		return fmt.Errorf("failed to distribute reward pool for %s: %w", secret.Id, err)
	}

	return nil
}

// partitionGuardiansForSettlement splits the secret's active guardians into
// revealers (paid, bond returned) and no-shows (slashed). Guardians already
// slashed for an early reveal appear in neither set: their bond was fully
// deducted at report time and they are excluded from the pool. A load
// failure propagates — a partial view must never settle.
func (k Keeper) partitionGuardiansForSettlement(ctx context.Context, secret types.Secret) (revealers, noShows []string, err error) {
	// The active set is the accepted set; both walks touch only tiny records
	// (assignment statuses, reveal existence) — never share bytes
	active, err := k.AcceptedGuardians(ctx, secret.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load accepted guardians for settlement of %s: %w", secret.Id, err)
	}

	for _, guardianAddress := range active {
		if k.IsGuardianSlashedForEarlyReveal(ctx, guardianAddress, secret.Id) {
			continue // bond already gone; excluded from P
		}
		if k.HasGuardianRevealed(ctx, secret.Id, guardianAddress) {
			revealers = append(revealers, guardianAddress)
		} else {
			noShows = append(noShows, guardianAddress)
		}
	}
	return revealers, noShows, nil
}

// slashNoRevealBond applies the no-reveal bond distribution to one guardian:
// 40% burned, 10% to the creator, 50% returned to the guardian's unlocked
// float. Percentages are of this guardian's own bond frozen on the secret at
// selection, so the penalty can never exceed the collateral that acceptance
// locked. The slash event also steps the guardian's bond multiplier k up,
// pricing its future acceptances higher.
func (k Keeper) slashNoRevealBond(ctx context.Context, guardianAddress string, secret types.Secret) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bond, ok := secret.GuardianBondAmount(guardianAddress)
	if !ok {
		return fmt.Errorf("no frozen bond recorded for no-show %s on secret %s — state-integrity violation",
			guardianAddress, secret.Id)
	}

	burnAmount, creatorAmount, returnedAmount := types.NoRevealSlashSplit(bond)

	// Remove the slashed portion from the guardian's float (funds stay in the
	// module account until burned/sent below), then unlock the remainder.
	slashedPortion := burnAmount.Add(creatorAmount)
	if err := k.DeductLockedFloat(ctx, guardianAddress, slashedPortion); err != nil {
		return fmt.Errorf("failed to deduct slashed bond portion: %w", err)
	}
	if err := k.UnlockGuardianFloat(ctx, guardianAddress, returnedAmount); err != nil {
		return fmt.Errorf("failed to return unslashed bond portion: %w", err)
	}
	if err := k.DecrementActiveBonds(ctx, guardianAddress); err != nil {
		return fmt.Errorf("failed to release active-bond slot for no-show %s: %w", guardianAddress, err)
	}
	if err := k.AdjustBondKOnSlash(ctx, guardianAddress); err != nil {
		return fmt.Errorf("failed to adjust bond multiplier for no-show %s: %w", guardianAddress, err)
	}

	// Burn the burn share
	if burnAmount.IsPositive() {
		if err := k.BurnSlashedFunds(ctx, sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, burnAmount))); err != nil {
			return fmt.Errorf("failed to burn slashed funds: %w", err)
		}
	}

	// Send the creator share
	if creatorAmount.IsPositive() {
		creatorAddr, err := sdk.AccAddressFromBech32(secret.Creator)
		if err != nil {
			return fmt.Errorf("invalid creator address %s: %w", secret.Creator, err)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creatorAddr,
			sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, creatorAmount))); err != nil {
			return fmt.Errorf("failed to send creator compensation: %w", err)
		}
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventGuardianSlashed,
			sdk.NewAttribute("guardian_address", guardianAddress),
			sdk.NewAttribute("slash_type", types.SlashTypeNoReveal),
			sdk.NewAttribute("secret_id", secret.Id),
			sdk.NewAttribute("secret_creator", secret.Creator),
			sdk.NewAttribute("bond", sdk.NewCoin(types.DefaultDenom, bond).String()),
			sdk.NewAttribute("burned", burnAmount.String()),
			sdk.NewAttribute("to_creator", creatorAmount.String()),
			sdk.NewAttribute("returned", returnedAmount.String()),
			sdk.NewAttribute("automatic", "true"),
		),
	)

	return nil
}

// distributePoolToRevealers splits the reward pool equally among the guardians
// that revealed correctly. A failed guardian's share is NOT refunded to the
// creator — it flows to the revealers (fewer revealers, larger share each),
// which is what makes revealing the dominant strategy. Dust is burned.
func (k Keeper) distributePoolToRevealers(ctx context.Context, secret types.Secret, revealers []string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	rewardPerGuardian := secret.RewardPool.Amount.QuoRaw(int64(len(revealers)))
	rewardCoin := sdk.NewCoin(secret.RewardPool.Denom, rewardPerGuardian)

	distributed := math.ZeroInt()
	for _, guardianAddress := range revealers {
		guardianAddr, err := sdk.AccAddressFromBech32(guardianAddress)
		if err != nil {
			return fmt.Errorf("invalid guardian address %s during reward distribution for %s: %w",
				guardianAddress, secret.Id, err)
		}
		if rewardPerGuardian.IsZero() {
			continue
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, guardianAddr, sdk.NewCoins(rewardCoin)); err != nil {
			return fmt.Errorf("failed to send reward %s to guardian %s for %s: %w",
				rewardCoin.String(), guardianAddress, secret.Id, err)
		}
		distributed = distributed.Add(rewardPerGuardian)

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"guardian_reward_distributed",
				sdk.NewAttribute("secret_id", secret.Id),
				sdk.NewAttribute("guardian", guardianAddress),
				sdk.NewAttribute("amount", rewardCoin.String()),
				sdk.NewAttribute("distribution_type", "window_end_equal_split"),
			),
		)
	}

	// Burn integer-division dust (spec: dust from any split is burned)
	dust := secret.RewardPool.Amount.Sub(distributed)
	if dust.IsPositive() {
		if err := k.BurnSlashedFunds(ctx, sdk.NewCoins(sdk.NewCoin(secret.RewardPool.Denom, dust))); err != nil {
			return fmt.Errorf("failed to burn distribution dust %s for %s: %w", dust.String(), secret.Id, err)
		}
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"secret_rewards_distributed",
			sdk.NewAttribute("secret_id", secret.Id),
			sdk.NewAttribute("total_eligible", fmt.Sprintf("%d", len(revealers))),
			sdk.NewAttribute("reward_per_guardian", rewardCoin.String()),
			sdk.NewAttribute("total_distributed", distributed.String()),
			sdk.NewAttribute("dust_burned", dust.String()),
		),
	)

	return nil
}

// updateSecretStateAfterExpiry updates the secret state based on whether reconstruction was successful
func (k Keeper) updateSecretStateAfterExpiry(ctx context.Context, secret *types.Secret) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Handle state transitions based on current state and threshold status
	if secret.State == types.SECRET_STATUS_RECONSTRUCTABLE {
		// Secret was already reconstructable - transition to final revealed state
		if err := k.TransitionSecretState(ctx, secret, EventWindowClosed); err != nil {
			return fmt.Errorf("failed to transition reconstructable secret to revealed state: %w", err)
		}

		sdkCtx.Logger().Info("Reconstructable secret window closed - transitioned to revealed",
			"secret_id", secret.Id,
			"shares_revealed", secret.RevealedCount,
			"threshold", secret.Threshold,
		)
	} else if secret.State == types.SECRET_STATUS_PENDING {
		// Secret was still pending - check if threshold was met
		if secret.RevealedCount >= secret.Threshold {
			// Sufficient shares - mark as revealed (threshold reached at window end)
			if err := k.TransitionSecretState(ctx, secret, EventThresholdReached); err != nil {
				return fmt.Errorf("failed to transition pending secret to reconstructable state: %w", err)
			}
			// Immediately transition to revealed since window is closed
			if err := k.TransitionSecretState(ctx, secret, EventWindowClosed); err != nil {
				return fmt.Errorf("failed to transition to final revealed state: %w", err)
			}

			sdkCtx.Logger().Info("Secret threshold met at reveal window end",
				"secret_id", secret.Id,
				"shares_revealed", secret.RevealedCount,
				"threshold", secret.Threshold,
			)
		} else {
			// Insufficient shares - mark as failed
			if err := k.TransitionSecretState(ctx, secret, EventRevealTimeout); err != nil {
				return fmt.Errorf("failed to transition secret to failed state: %w", err)
			}

			sdkCtx.Logger().Info("Secret failed - insufficient shares at window end",
				"secret_id", secret.Id,
				"shares_revealed", secret.RevealedCount,
				"threshold", secret.Threshold,
			)
		}
	}

	// Note: Secret state is already updated in storage by TransitionSecretState
	// No need to overwrite it here
	return nil
}

// MarkGuardianSlashedForEarlyReveal tracks that a guardian was slashed for early reveal
// This is used to exclude them from reward distribution at window end
func (k Keeper) MarkGuardianSlashedForEarlyReveal(ctx context.Context, guardianAddress, secretId string) error {
	// Use collections framework with key format: "secretId:guardianAddress"
	key := fmt.Sprintf("%s:%s", secretId, guardianAddress)
	return k.EarlyRevealSlash.Set(ctx, key, true)
}

// IsGuardianSlashedForEarlyReveal checks if a guardian was slashed for early reveal on a specific secret
func (k Keeper) IsGuardianSlashedForEarlyReveal(ctx context.Context, guardianAddress, secretId string) bool {
	key := fmt.Sprintf("%s:%s", secretId, guardianAddress)
	slashed, err := k.EarlyRevealSlash.Get(ctx, key)
	if err != nil {
		return false // Not found means not slashed
	}
	return slashed
}

// distributeAcceptFees settles the secret's escrowed acceptance
// reimbursement at its terminal state: one stored slice to each guardian in
// earned, and every unearned slice back to the creator. Called on EVERY
// terminal path — pass a nil recipient list when no guardian earned anything
// — so no accept fee can be stranded in module escrow.
//
// The slice comes from the secret's STORED accept_fees, never the live
// constant, so a gas retune re-prices future secrets only (the
// immutable-economics ruling, as for ProRataCancellationPayout). It divides
// exactly by max_shares by construction, so there is no dust on this path.
func (k Keeper) distributeAcceptFees(ctx context.Context, secret types.Secret, earned []string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	total := secret.AcceptFees.Amount
	if !total.IsPositive() {
		return nil // pre-work-component secret, or a zero-share band
	}
	perGuardian := types.PerGuardianAcceptFee(total, secret.MaxShares)

	paid := math.ZeroInt()
	if perGuardian.IsPositive() {
		for _, guardianAddress := range earned {
			guardianAddr, err := sdk.AccAddressFromBech32(guardianAddress)
			if err != nil {
				return fmt.Errorf("invalid guardian address %s during accept-fee distribution for %s: %w",
					guardianAddress, secret.Id, err)
			}
			coin := sdk.NewCoin(secret.AcceptFees.Denom, perGuardian)
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, guardianAddr, sdk.NewCoins(coin)); err != nil {
				return fmt.Errorf("failed to send accept fee %s to guardian %s for %s: %w",
					coin.String(), guardianAddress, secret.Id, err)
			}
			paid = paid.Add(perGuardian)

			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"guardian_accept_fee_paid",
					sdk.NewAttribute("secret_id", secret.Id),
					sdk.NewAttribute("guardian", guardianAddress),
					sdk.NewAttribute("amount", coin.String()),
				),
			)
		}
	}

	// Slices nobody earned go back to the creator — the protocol never keeps
	// a reimbursement for work that was not done
	unearned := total.Sub(paid)
	if unearned.IsPositive() {
		if err := k.refundRewardPool(ctx, secret.Creator, sdk.NewCoin(secret.AcceptFees.Denom, unearned)); err != nil {
			return fmt.Errorf("failed to refund unearned accept fees for %s: %w", secret.Id, err)
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"accept_fees_refunded",
				sdk.NewAttribute("secret_id", secret.Id),
				sdk.NewAttribute("creator", secret.Creator),
				sdk.NewAttribute("amount", sdk.NewCoin(secret.AcceptFees.Denom, unearned).String()),
			),
		)
	}
	return nil
}

// refundRewardPool refunds the locked reward pool back to the creator
func (k Keeper) refundRewardPool(ctx context.Context, creator string, rewardPool sdk.Coin) error {
	creatorAddr, err := sdk.AccAddressFromBech32(creator)
	if err != nil {
		return fmt.Errorf("invalid creator address %s: %w", creator, err)
	}

	// Send coins from module back to creator
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creatorAddr, sdk.NewCoins(rewardPool))
}
