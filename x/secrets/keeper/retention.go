package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/types"
)

// Terminal-secret retention (docs/planning/PENDING_TERMINAL_SECRET_RETENTION_PLAN.md).
//
// Live state scales with active secrets, not every secret ever created.
// Retention runs in two stages because the data has two lifetimes:
//
//	Stage 1 — at the terminal transition: delete the encrypted share records
//	  (including rejected assignments'), the assignment statuses, and the
//	  early-reveal slash marks. Settlement provably reads none of these after
//	  the terminal transition; the reveal records and payload ciphertext C —
//	  the reconstruction inputs — deliberately survive.
//	Stage 2 — at terminal_at + RetentionBlocks: build the canonical
//	  TerminalSecretRecord from state, hash it, write the permanent
//	  SecretTombstone, emit the archival event carrying the full canonical
//	  record, then delete the slim record, the reveal records, the payload
//	  ciphertext, the creator-index entry, and the hint-feed entry.
//
// The recipient has the full retention window to fetch the reveal records and
// C and reconstruct; after Stage 2 only archives can help — the tombstone's
// digest is what makes any archived copy self-authenticating.

// onSecretTerminal runs Stage 1 retention for a secret that has just reached
// a terminal state (revealed/cancelled/failed) and enqueues its Stage 2
// prune. Called exactly once per secret, from TransitionSecretState, after
// the terminal record has been persisted. Idempotent: deletes of absent keys
// and re-enqueues are no-ops, so a retried caller cannot double-fire.
func (k Keeper) onSecretTerminal(ctx context.Context, secret types.Secret) error {
	// Collect the guardian set from the assignment records before deleting
	// them — the early-reveal slash marks are keyed by guardian address.
	rng := collections.NewPrefixedPairRange[string, string](secret.Id)
	var guardians []string
	if err := k.SecretAssignments.Walk(ctx, rng, func(key collections.Pair[string, string], _ types.AssignmentRecord) (bool, error) {
		guardians = append(guardians, key.K2())
		return false, nil
	}); err != nil {
		return fmt.Errorf("stage 1: walking assignments for %s: %w", secret.Id, err)
	}

	// Encrypted shares (incl. rejected assignments' — formerly kept "for
	// audit", now bounded by the terminal transition) and assignment statuses
	if err := k.SecretShares.Clear(ctx, rng); err != nil {
		return fmt.Errorf("stage 1: clearing shares for %s: %w", secret.Id, err)
	}
	if err := k.SecretAssignments.Clear(ctx, rng); err != nil {
		return fmt.Errorf("stage 1: clearing assignments for %s: %w", secret.Id, err)
	}

	// Early-reveal slash marks — consumed during settlement/cancellation,
	// dead weight afterwards
	for _, guardian := range guardians {
		key := fmt.Sprintf("%s:%s", secret.Id, guardian)
		if err := k.EarlyRevealSlash.Remove(ctx, key); err != nil {
			return fmt.Errorf("stage 1: removing slash mark %s: %w", key, err)
		}
	}

	// Schedule Stage 2
	return k.PruneQueue.Set(ctx, collections.Join(secret.TerminalAt+types.RetentionBlocksValue(), secret.Id))
}

// BuildTerminalRecord assembles the canonical final record of a terminal
// secret from state — never from a re-assembled or query-derived view. The
// reveals are sorted by guardian address (bech32 string, byte-wise
// ascending); prefix iteration already yields that order, and the explicit
// sort pins the canonical definition rather than an iteration detail.
func (k Keeper) BuildTerminalRecord(ctx context.Context, secret types.Secret) (types.TerminalSecretRecord, error) {
	record := types.TerminalSecretRecord{Secret: secret}

	rng := collections.NewPrefixedPairRange[string, string](secret.Id)
	if err := k.SecretReveals.Walk(ctx, rng, func(_ collections.Pair[string, string], reveal types.RevealedShare) (bool, error) {
		record.Reveals = append(record.Reveals, reveal)
		return false, nil
	}); err != nil {
		return record, fmt.Errorf("walking reveals for %s: %w", secret.Id, err)
	}
	sort.Slice(record.Reveals, func(i, j int) bool {
		return record.Reveals[i].GuardianAddress < record.Reveals[j].GuardianAddress
	})

	// payload_digest = SHA256(C), hashed from the stored entry at prune time.
	// A terminal secret that never reached distribution (commit-timeout from
	// reserved) has no payload — the digest is empty by definition.
	payload, err := k.SecretPayloads.Get(ctx, secret.Id)
	if err == nil {
		digest := sha256.Sum256(payload)
		record.PayloadDigest = digest[:]
	} else if !errors.Is(err, collections.ErrNotFound) {
		return record, fmt.Errorf("reading payload for %s: %w", secret.Id, err)
	}

	return record, nil
}

// TerminalRecordDigest returns SHA256 over the deterministic proto marshal of
// the canonical record. The message has no map fields, so gogoproto's
// Marshal is deterministic; the golden test in retention_test.go pins the
// exact bytes so an encoder change can never silently re-digest history.
func TerminalRecordDigest(record types.TerminalSecretRecord) ([]byte, error) {
	bz, err := record.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshalling canonical record: %w", err)
	}
	digest := sha256.Sum256(bz)
	return digest[:], nil
}

// ProcessDuePrunes drains the prune queue (Stage 2), capped at
// MaxPrunesPerBlock per block — a burst of same-height expiries carries over
// to the next block via the <=-drain. Per due secret: build the canonical
// record, write the tombstone, emit the archival event, delete everything
// else. A processing error keeps the entry so the next block retries.
func (k Keeper) ProcessDuePrunes(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	due, err := drainDueEntries(ctx, k.PruneQueue, currentHeight)
	if err != nil {
		return err
	}
	if len(due) > types.MaxPrunesPerBlock {
		due = due[:types.MaxPrunesPerBlock]
	}

	for _, key := range due {
		secretId := key.K2()

		secret, err := k.GetSecret(ctx, secretId)
		if err != nil {
			// Already gone — stale entry, drop it
			if rmErr := k.PruneQueue.Remove(ctx, key); rmErr != nil {
				sdkCtx.Logger().Error("failed to remove stale prune entry", "secret_id", secretId, "error", rmErr)
			}
			continue
		}

		// Guard: only terminal secrets are ever enqueued, and terminal states
		// are terminal — anything else is a corrupt entry worth logging.
		if !secret.IsComplete() {
			sdkCtx.Logger().Error("prune entry for non-terminal secret — dropping", "secret_id", secretId, "state", secret.State)
			if rmErr := k.PruneQueue.Remove(ctx, key); rmErr != nil {
				sdkCtx.Logger().Error("failed to remove bad prune entry", "secret_id", secretId, "error", rmErr)
			}
			continue
		}

		if err := k.pruneTerminalSecret(ctx, secret, currentHeight); err != nil {
			sdkCtx.Logger().Error("failed to prune terminal secret", "secret_id", secretId, "error", err)
			continue // entry retained: retry next block
		}

		if rmErr := k.PruneQueue.Remove(ctx, key); rmErr != nil {
			sdkCtx.Logger().Error("failed to dequeue processed prune entry", "secret_id", secretId, "error", rmErr)
		}
	}

	return nil
}

// pruneTerminalSecret executes Stage 2 for one secret: tombstone first (from
// state that is about to be deleted), archival event, then the deletes.
func (k Keeper) pruneTerminalSecret(ctx context.Context, secret types.Secret, currentHeight int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// An uncollected rebate's reservation returns to the pool: the retention
	// window IS the collection window, so pruning is where an unclaimed rebate
	// expires. Released before the record is built so the tombstone is written
	// from state that no longer promises the funds.
	if err := k.ReleaseRebateReservation(ctx, secret); err != nil {
		return err
	}
	if err := k.ClearRebateCommitments(ctx, secret.Id); err != nil {
		return err
	}

	record, err := k.BuildTerminalRecord(ctx, secret)
	if err != nil {
		return err
	}
	recordBytes, err := record.Marshal()
	if err != nil {
		return fmt.Errorf("marshalling canonical record for %s: %w", secret.Id, err)
	}
	digest := sha256.Sum256(recordBytes)

	tombstone := types.SecretTombstone{
		RecordDigest:     digest[:],
		FinalState:       secret.State,
		TerminalAt:       secret.TerminalAt,
		PrunedAt:         currentHeight,
		Creator:          secret.Creator,
		CreatedAt:        secret.CreatedAt,
		SecretCommitment: secret.SecretCommitment,
	}
	if err := k.SecretTombstones.Set(ctx, secret.Id, tombstone); err != nil {
		return fmt.Errorf("writing tombstone for %s: %w", secret.Id, err)
	}

	// The archival hook: indexers that retain events get a complete,
	// self-verifying archive at zero state cost. This event is the
	// load-bearing recovery path once the deletes below land.
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeSecretPruned,
			sdk.NewAttribute(types.AttributeKeySecretId, secret.Id),
			sdk.NewAttribute("record_digest", hex.EncodeToString(digest[:])),
			sdk.NewAttribute("final_state", secret.State),
			sdk.NewAttribute("terminal_at", fmt.Sprintf("%d", secret.TerminalAt)),
			sdk.NewAttribute("pruned_at", fmt.Sprintf("%d", currentHeight)),
			sdk.NewAttribute("canonical_record", base64.StdEncoding.EncodeToString(recordBytes)),
		),
	)

	// Stage 2 deletes: slim record, reveal records, payload ciphertext,
	// creator-index entry, hint-feed entry. (Shares, assignments and slash
	// marks died at Stage 1.)
	if err := k.Secrets.Remove(ctx, secret.Id); err != nil {
		return fmt.Errorf("removing slim record for %s: %w", secret.Id, err)
	}
	if err := k.SecretAcceptedCounts.Remove(ctx, secret.Id); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("removing acceptance tally for %s: %w", secret.Id, err)
	}
	rng := collections.NewPrefixedPairRange[string, string](secret.Id)
	if err := k.SecretReveals.Clear(ctx, rng); err != nil {
		return fmt.Errorf("clearing reveals for %s: %w", secret.Id, err)
	}
	if err := k.SecretPayloads.Remove(ctx, secret.Id); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("removing payload for %s: %w", secret.Id, err)
	}
	if err := k.SecretsByCreator.Remove(ctx, collections.Join(secret.Creator, secret.Id)); err != nil {
		return fmt.Errorf("removing creator index for %s: %w", secret.Id, err)
	}
	if err := k.HintsByCreation.Remove(ctx, collections.Join(secret.CreatedAt, secret.Id)); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("removing hint entry for %s: %w", secret.Id, err)
	}

	return nil
}
