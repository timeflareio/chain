package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/types"
)

// Due-height queue plumbing.
//
// Instead of scanning every secret each EndBlock, secrets are enqueued at
// creation under the height their next lifecycle deadline falls due:
//
//	commit queue:     due = commit_deadline + 1   (acceptance valid while height <= deadline)
//	settlement queue: due = reveal_end_block + 1  (reveals valid while height <= end)
//
// EndBlock drains entries with due <= current height. Because CometBFT
// executes every height sequentially this fires at exactly the due height in
// practice (the == case); the <= form is the self-healing safety net for
// state that predates its due height (genesis import, migrations). Entries
// are removed on processing, on activation (commit queue), and on
// cancellation — so terminal secrets are never revisited.

// EnqueueSecretDeadlines registers a freshly created secret in both queues.
func (k Keeper) EnqueueSecretDeadlines(ctx context.Context, secret types.Secret) error {
	if err := k.CommitQueue.Set(ctx, collections.Join(secret.CommitDeadline+1, secret.Id)); err != nil {
		return err
	}
	return k.SettlementQueue.Set(ctx, collections.Join(secret.RevealEndBlock+1, secret.Id))
}

// RestoreSecretDeadlines re-registers an imported (genesis) secret with
// exactly the queue entries its state warrants: both entries pre-activation,
// settlement-only once activated, a prune entry when terminal. Blindly
// enqueueing both would leave an activated import with a bogus commit entry
// that outlives settlement (found by the F2 conformance scenario).
func (k Keeper) RestoreSecretDeadlines(ctx context.Context, secret types.Secret) error {
	switch secret.State {
	case types.SECRET_STATUS_RESERVED, types.SECRET_STATUS_AWAITING_ACCEPTANCE:
		return k.EnqueueSecretDeadlines(ctx, secret)
	case types.SECRET_STATUS_PENDING, types.SECRET_STATUS_RECONSTRUCTABLE:
		return k.SettlementQueue.Set(ctx, collections.Join(secret.RevealEndBlock+1, secret.Id))
	default:
		// Terminal but not yet pruned: restore the Stage 2 prune entry (the
		// prune queue is derived state, never exported). A record predating
		// the terminal_at stamp (terminal_at == 0, pre-storage-split
		// exports) gets a full retention window from import height rather
		// than retroactive pruning — the <=-drain would otherwise prune it
		// on the first block.
		terminalAt := secret.TerminalAt
		if terminalAt == 0 {
			terminalAt = sdkHeight(ctx)
		}
		// An uncollected rebate's collection deadline is derived state too, and
		// restored on the same clock as the prune entry — otherwise an imported
		// chain would hold its reservation until pruning.
		if secret.RebateAmount > 0 && !secret.RebateCollected {
			if err := k.RebateExpiryQueue.Set(ctx, collections.Join(
				types.RebateCollectionDeadline(terminalAt), secret.Id)); err != nil {
				return err
			}
		}
		return k.PruneQueue.Set(ctx, collections.Join(terminalAt+types.RetentionBlocksValue(), secret.Id))
	}
}

// sdkHeight is a tiny helper for the genesis-import fallback above.
func sdkHeight(ctx context.Context) int64 {
	return sdk.UnwrapSDKContext(ctx).BlockHeight()
}

// dequeueSettlement removes a secret's settlement entry (settlement,
// cancellation, or commit-timeout failure).
func (k Keeper) dequeueSettlement(ctx context.Context, secret types.Secret) {
	if err := k.SettlementQueue.Remove(ctx, collections.Join(secret.RevealEndBlock+1, secret.Id)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("failed to dequeue settlement entry",
			"secret_id", secret.Id, "error", err)
	}
}

// drainDueEntries collects every queue entry with due height <= currentHeight.
// Keys are collected before processing so callers can mutate the queue while
// handling them. Iteration is key-ordered (heights ascend), so it stops at the
// first future entry — an idle block reads at most one key.
func drainDueEntries(ctx context.Context, queue collections.KeySet[collections.Pair[int64, string]], currentHeight int64) ([]collections.Pair[int64, string], error) {
	var due []collections.Pair[int64, string]
	err := queue.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		if key.K1() > currentHeight {
			return true, nil // heights ascend: nothing further is due
		}
		due = append(due, key)
		return false, nil
	})
	return due, err
}
