package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"github.com/timeflareio/chain/x/secrets/types"
)

// InitGenesis initializes the secrets module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// Initialize secrets. Non-terminal secrets are re-registered in the
	// due-height queues with exactly the entries their state warrants (the
	// queues and the creator index are derived state and are not exported):
	// EndBlock's <=-drain catches any entry whose due height has already
	// passed, so imported past-due secrets settle on the first block rather
	// than being stranded.
	for _, secret := range genState.Secrets {
		if err := k.SetSecret(ctx, *secret); err != nil {
			return err
		}
		if err := k.IndexSecretCreator(ctx, *secret); err != nil {
			return err
		}
		if err := k.IndexSecretCreation(ctx, *secret); err != nil {
			return err
		}
		if err := k.RestoreSecretDeadlines(ctx, *secret); err != nil {
			return err
		}
	}

	// Initialize guardians. SetGuardian maintains the selection eligibility
	// index, so that index is rebuilt here as a side effect rather than needing
	// an import loop of its own — it is derived state and is not exported. The
	// invariant sweep at the end of this function proves the rebuild happened
	// (invariant 9), which is why there is no separate genesis assertion for it.
	for _, guardian := range genState.Guardians {
		if err := k.SetGuardian(ctx, *guardian); err != nil {
			return err
		}
	}

	// Key histories. The uniqueness index is derived state and is rebuilt
	// here (AppendGuardianKeyEpoch reserves each key as it writes; a
	// collision aborts the import). A guardian with no exported entries gets
	// epoch 0 synthesised from its record — the migration path for state
	// created before key rotation existed (docs/spec.md "Guardian Key
	// Rotation"); effective height 0 keeps the derivation total and starts
	// the rotation-interval clock at genesis.
	guardiansWithHistory := make(map[string]bool)
	for _, entry := range genState.GuardianKeyHistories {
		if err := k.AppendGuardianKeyEpoch(ctx, entry.GuardianAddress, entry.Epoch, entry.Entry); err != nil {
			return err
		}
		guardiansWithHistory[entry.GuardianAddress] = true
	}
	for _, guardian := range genState.Guardians {
		if guardiansWithHistory[guardian.Address] {
			continue
		}
		if err := k.AppendGuardianKeyEpoch(ctx, guardian.Address, 0, types.KeyHistoryEntry{
			PublicKey:           guardian.EncryptionPublicKey,
			EffectiveFromHeight: 0,
		}); err != nil {
			return err
		}
	}

	// Side-stores: encrypted shares, assignment statuses, reveals
	for _, entry := range genState.SecretShares {
		if err := k.SecretShares.Set(ctx, collections.Join(entry.SecretId, entry.GuardianAddress), entry.Data); err != nil {
			return err
		}
	}
	for _, entry := range genState.SecretAssignments {
		if err := k.SetAssignment(ctx, entry.SecretId, entry.GuardianAddress, entry.Record); err != nil {
			return err
		}
	}
	for _, entry := range genState.SecretReveals {
		if err := k.SecretReveals.Set(ctx, collections.Join(entry.SecretId, entry.Reveal.GuardianAddress), entry.Reveal); err != nil {
			return err
		}
	}

	// Early-reveal slash marks (consumed at settlement/cancellation to exclude
	// the slashed guardian from payouts — genuine state, not derivable)
	for _, entry := range genState.EarlyRevealSlashes {
		if err := k.MarkGuardianSlashedForEarlyReveal(ctx, entry.GuardianAddress, entry.SecretId); err != nil {
			return err
		}
	}

	// Payload ciphertexts (the only copy of each secret's material)
	for _, entry := range genState.SecretPayloads {
		if err := k.SecretPayloads.Set(ctx, entry.SecretId, entry.PayloadCiphertext); err != nil {
			return err
		}
	}

	// The secret counter is consensus state: a chain restarted from exported
	// genesis must continue the sequence, never reset it (IDs would reissue
	// and selection seeds would repeat).
	if err := k.SecretCounter.Set(ctx, genState.SecretCounter); err != nil {
		return err
	}

	// The rebate accrual record is consensus state: a chain restarted from
	// exported genesis must keep its allowance, its accrual height and — above
	// all — its reservations, or credited rebates would be promised twice.
	// A zero record is the correct state for a chain that has never credited
	// one, and writing it is harmless.
	if err := k.RebateState.Set(ctx, genState.RebateState); err != nil {
		return err
	}

	// Tombstones of pruned secrets — permanent state, exported as-is. The
	// prune queue is derived and was rebuilt above by RestoreSecretDeadlines.
	for _, entry := range genState.SecretTombstones {
		if err := k.SecretTombstones.Set(ctx, entry.SecretId, entry.Tombstone); err != nil {
			return err
		}
	}

	// Import-time state-integrity sweep (hard halt — ruled July 2026): run
	// the full invariant library over the imported state. GenesisState
	// .Validate() checks each record's shape and cross-references; this sweep
	// additionally proves the assembled store is one the runtime could have
	// produced (bond accounting, queue hygiene, counters, payloads).
	// Pre-launch there is no legacy state to be gentle with — a genesis known
	// to be inconsistent must never produce blocks.
	if err := k.CheckStateInvariants(ctx); err != nil {
		return fmt.Errorf("genesis import failed the state-integrity sweep: %w", err)
	}

	return nil
}

// ExportGenesis returns the secrets module's exported genesis state.
func (k Keeper) ExportGenesis(ctx context.Context) (types.GenesisState, error) {
	var genState types.GenesisState

	// Export all secrets
	var secrets []*types.Secret
	err := k.Secrets.Walk(ctx, &collections.Range[string]{}, func(key string, value types.Secret) (bool, error) {
		// The acceptance tally lives in its own record; join it so the export
		// is a complete secret and the genesis consistency check (which
		// cross-checks the tally against the assignment records) sees the
		// real value rather than a zero.
		joined, err := k.withAcceptedCount(ctx, value)
		if err != nil {
			return true, err
		}
		secrets = append(secrets, &joined)
		return false, nil
	})
	if err != nil {
		return genState, err
	}
	genState.Secrets = secrets

	// Export all guardians
	var guardians []*types.Guardian
	err = k.Guardians.Walk(ctx, &collections.Range[string]{}, func(key string, value types.Guardian) (bool, error) {
		guardians = append(guardians, &value)
		return false, nil
	})
	if err != nil {
		return genState, err
	}
	genState.Guardians = guardians

	// Export the side-stores
	err = k.SecretShares.Walk(ctx, nil, func(key collections.Pair[string, string], value types.SecretShareData) (bool, error) {
		genState.SecretShares = append(genState.SecretShares, types.StoredShare{
			SecretId:        key.K1(),
			GuardianAddress: key.K2(),
			Data:            value,
		})
		return false, nil
	})
	if err != nil {
		return genState, err
	}

	err = k.SecretAssignments.Walk(ctx, nil, func(key collections.Pair[string, string], value types.AssignmentRecord) (bool, error) {
		genState.SecretAssignments = append(genState.SecretAssignments, types.StoredAssignment{
			SecretId:        key.K1(),
			GuardianAddress: key.K2(),
			Record:          value,
		})
		return false, nil
	})
	if err != nil {
		return genState, err
	}

	err = k.SecretReveals.Walk(ctx, nil, func(key collections.Pair[string, string], value types.RevealedShare) (bool, error) {
		genState.SecretReveals = append(genState.SecretReveals, types.StoredReveal{
			SecretId: key.K1(),
			Reveal:   value,
		})
		return false, nil
	})
	if err != nil {
		return genState, err
	}

	err = k.SecretPayloads.Walk(ctx, nil, func(secretID string, payload []byte) (bool, error) {
		genState.SecretPayloads = append(genState.SecretPayloads, types.StoredPayload{
			SecretId:          secretID,
			PayloadCiphertext: payload,
		})
		return false, nil
	})
	if err != nil {
		return genState, err
	}

	// Export early-reveal slash marks (stored keyed "secretId:guardianAddress")
	err = k.EarlyRevealSlash.Walk(ctx, nil, func(key string, value bool) (bool, error) {
		if !value {
			return false, nil
		}
		secretID, guardianAddress, ok := strings.Cut(key, ":")
		if !ok {
			return true, fmt.Errorf("malformed early reveal slash key: %s", key)
		}
		genState.EarlyRevealSlashes = append(genState.EarlyRevealSlashes, types.EarlyRevealSlashEntry{
			SecretId:        secretID,
			GuardianAddress: guardianAddress,
		})
		return false, nil
	})
	if err != nil {
		return genState, err
	}

	// Export the secret counter (the next value to assign)
	counter, err := k.SecretCounter.Peek(ctx)
	if err != nil {
		return genState, err
	}
	genState.SecretCounter = counter

	// Export the rebate accrual record (allowance, accrual height, reserved)
	rebateState, err := k.GetRebateState(ctx)
	if err != nil {
		return genState, err
	}
	genState.RebateState = rebateState

	// Export the tombstones of pruned secrets
	err = k.SecretTombstones.Walk(ctx, nil, func(secretID string, tombstone types.SecretTombstone) (bool, error) {
		genState.SecretTombstones = append(genState.SecretTombstones, types.StoredTombstone{
			SecretId:  secretID,
			Tombstone: tombstone,
		})
		return false, nil
	})
	if err != nil {
		return genState, err
	}

	// Export the key histories (the uniqueness index is derived state and is
	// rebuilt on import)
	err = k.GuardianKeyHistory.Walk(ctx, nil, func(key collections.Pair[string, uint64], entry types.KeyHistoryEntry) (bool, error) {
		genState.GuardianKeyHistories = append(genState.GuardianKeyHistories, types.StoredKeyHistoryEntry{
			GuardianAddress: key.K1(),
			Epoch:           key.K2(),
			Entry:           entry,
		})
		return false, nil
	})
	if err != nil {
		return genState, err
	}

	return genState, nil
}
