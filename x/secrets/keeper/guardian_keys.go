package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/timeflareio/chain/x/secrets/types"
)

// Guardian key-epoch helpers (docs/spec.md "Guardian Key Rotation").
//
// A guardian's keys form an append-only history (address, epoch) →
// {public_key, effective_from_height}, epoch 0 being the registration key.
// The epoch in force at height h is the newest entry with
// effective_from_height <= h — a pure function of the history, never stored
// per-secret. Every key ever registered stays globally reserved forever via
// the uniqueness index.

// guardianHistoryKey is the history key of a guardian record's CURRENT epoch.
func guardianHistoryKey(guardian types.Guardian) collections.Pair[string, uint64] {
	return collections.Join(guardian.Address, guardian.CurrentKeyEpoch)
}

// AppendGuardianKeyEpoch writes one epoch of a guardian's key history and
// reserves the key in the global uniqueness index. It refuses to overwrite an
// existing epoch (the history is append-only) or to reserve a key any
// guardian has ever registered.
func (k Keeper) AppendGuardianKeyEpoch(ctx context.Context, guardianAddress string, epoch uint64, entry types.KeyHistoryEntry) error {
	key := collections.Join(guardianAddress, epoch)
	if has, err := k.GuardianKeyHistory.Has(ctx, key); err != nil {
		return err
	} else if has {
		return fmt.Errorf("key history for guardian %s already holds epoch %d — the history is append-only", guardianAddress, epoch)
	}

	if holder, err := k.GuardianKeyIndex.Get(ctx, entry.PublicKey); err == nil {
		return fmt.Errorf("encryption key already registered by guardian %s — every key ever registered stays reserved forever", holder)
	}

	if err := k.GuardianKeyHistory.Set(ctx, key, entry); err != nil {
		return err
	}
	return k.GuardianKeyIndex.Set(ctx, entry.PublicKey, guardianAddress)
}

// KeyEverRegistered reports whether any guardian has ever registered the
// given encryption key at any epoch, and by whom.
func (k Keeper) KeyEverRegistered(ctx context.Context, publicKey []byte) (bool, string) {
	holder, err := k.GuardianKeyIndex.Get(ctx, publicKey)
	if err != nil {
		return false, ""
	}
	return true, holder
}

// GuardianKeyInForce resolves the encryption key in force for the guardian at
// the given height — the key selection must hand the creator. This differs
// from the record's current key in exactly one case: a rotation that landed
// earlier in the SAME block wrote the new key to the record, but the rotation
// is effective from the next block, so the previous epoch's key is still the
// one in force (docs/spec.md "Guardian Key Rotation": a same-block rotation
// and selection select the pre-rotation key, whatever the transaction order).
//
// A guardian without a history entry (possible only in unit fixtures that
// store records directly) resolves to the record key.
func (k Keeper) GuardianKeyInForce(ctx context.Context, guardian types.Guardian, height int64) ([]byte, error) {
	newest, err := k.GuardianKeyHistory.Get(ctx, collections.Join(guardian.Address, guardian.CurrentKeyEpoch))
	if err != nil {
		return guardian.EncryptionPublicKey, nil
	}
	if newest.EffectiveFromHeight <= height || guardian.CurrentKeyEpoch == 0 {
		return guardian.EncryptionPublicKey, nil
	}
	// The newest epoch is not yet in force; the rotation interval makes a
	// second not-yet-effective epoch impossible, so one step back suffices.
	previous, err := k.GuardianKeyHistory.Get(ctx, collections.Join(guardian.Address, guardian.CurrentKeyEpoch-1))
	if err != nil {
		return nil, fmt.Errorf("guardian %s current epoch %d is not yet in force at height %d and epoch %d is missing — state-integrity violation: %w",
			guardian.Address, guardian.CurrentKeyEpoch, height, guardian.CurrentKeyEpoch-1, err)
	}
	return previous.PublicKey, nil
}

// GuardianEpochInForce derives the key epoch in force for a guardian at the
// given height: the newest history entry with effective_from_height <= h.
// This is the derivation the guardian daemon mirrors to resolve which key an
// assignment was encrypted to (from the secret's creation height).
func (k Keeper) GuardianEpochInForce(ctx context.Context, guardianAddress string, height int64) (uint64, types.KeyHistoryEntry, error) {
	guardian, found := k.GetGuardian(ctx, guardianAddress)
	if !found {
		return 0, types.KeyHistoryEntry{}, fmt.Errorf("guardian %s not found", guardianAddress)
	}
	for epoch := guardian.CurrentKeyEpoch; ; epoch-- {
		entry, err := k.GuardianKeyHistory.Get(ctx, collections.Join(guardianAddress, epoch))
		if err != nil {
			return 0, types.KeyHistoryEntry{}, fmt.Errorf("guardian %s has no key epoch in force at height %d (history missing at epoch %d): %w",
				guardianAddress, height, epoch, err)
		}
		if entry.EffectiveFromHeight <= height {
			return epoch, entry, nil
		}
		if epoch == 0 {
			return 0, types.KeyHistoryEntry{}, fmt.Errorf("guardian %s has no key epoch in force at height %d (epoch 0 effective from %d)",
				guardianAddress, height, entry.EffectiveFromHeight)
		}
	}
}

// WalkGuardianKeyHistory iterates one guardian's key history in epoch order.
func (k Keeper) WalkGuardianKeyHistory(ctx context.Context, guardianAddress string, fn func(epoch uint64, entry types.KeyHistoryEntry) (stop bool, err error)) error {
	rng := collections.NewPrefixedPairRange[string, uint64](guardianAddress)
	return k.GuardianKeyHistory.Walk(ctx, rng, func(key collections.Pair[string, uint64], entry types.KeyHistoryEntry) (bool, error) {
		return fn(key.K2(), entry)
	})
}
