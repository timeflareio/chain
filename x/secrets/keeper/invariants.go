package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"github.com/timeflareio/chain/x/secrets/types"
	"github.com/timeflareio/crypto/go"
)

// State-integrity invariant library (settlement & state-integrity plan,
// ruled July 2026). Extracted from the test suite so both the tests and the
// wholesale-risk moments can run the same assertions:
//
//   - after InitGenesis (hard halt — a genesis known to be inconsistent must
//     never produce blocks),
//   - inside upgrade migrations at the upgrade height — an OBLIGATION on
//     future handlers, not existing wiring: app/upgrades.go registers no
//     upgrades yet, so nothing calls this from a migration today,
//   - continuously in the conformance and fuzz suites (after every scenario
//     and every fuzzer block).
//
// Deliberately NOT wired into per-block execution: settlement failures are
// already contained by the per-secret cache-commit (endblock_logic.go), and a
// per-block sweep would turn any attacker-reachable inconsistency into a
// chain-wide halt. The numbering (3, 4, 4b, 5, 6) matches the economics test
// strategy plan; invariants 1 and 2 (exact escrow solvency) need a ledgered
// bank model and remain test-only (invariants_test.go).

// CheckStateInvariants runs the full state-side invariant sweep over the
// module's store and returns the first violation found (nil when the state is
// consistent). Iteration is Collections-walk ordered throughout, so every
// node reports the same violation for the same state.
func (k Keeper) CheckStateInvariants(ctx context.Context) error {
	// Collect all secrets once, preserving walk (key) order for the
	// per-secret checks below.
	secrets := map[string]types.Secret{}
	var secretIds []string
	if err := k.Secrets.Walk(ctx, nil, func(id string, s types.Secret) (bool, error) {
		// Join the side-stored acceptance tally: invariant 5 exists precisely
		// to prove it agrees with the assignment records, so it must compare
		// the real value and not the zero the raw blob now carries.
		joined, err := k.withAcceptedCount(ctx, s)
		if err != nil {
			return true, err
		}
		secrets[id] = joined
		secretIds = append(secretIds, id)
		return false, nil
	}); err != nil {
		return fmt.Errorf("invariant sweep: walking secrets: %w", err)
	}

	if err := k.checkNoStrandedBonds(ctx, secrets, secretIds); err != nil {
		return err
	}
	if err := k.checkQueueHygiene(ctx, secrets, secretIds); err != nil {
		return err
	}
	if err := k.checkPruneQueueHygiene(ctx, secrets, secretIds); err != nil {
		return err
	}
	if err := k.checkCounterConsistency(ctx, secrets, secretIds); err != nil {
		return err
	}
	if err := k.checkPayloadPresence(ctx, secrets, secretIds); err != nil {
		return err
	}
	if err := k.checkKeyHistoryIntegrity(ctx, secrets, secretIds); err != nil {
		return err
	}
	if err := k.checkStoredKeysAreUsable(ctx, secrets, secretIds); err != nil {
		return err
	}
	return k.checkEligibilityIndexIntegrity(ctx)
}

// checkStoredKeysAreUsable asserts invariant 8 (key usability): every X25519
// public key held in state is one the message handlers would accept — exactly 32
// bytes and not a small-order point. The handlers reject such keys at
// registration, rotation, share distribution and hint submission, so a live
// chain cannot produce a violation; this sweep is what closes the GENESIS path,
// where state is assembled outside the message path entirely. It runs at
// InitGenesis behind a hard halt, so a genesis carrying an unusable key can
// never produce blocks (docs/spec.md "Common Attack Vectors", Small-Order Key
// Registration).
func (k Keeper) checkStoredKeysAreUsable(ctx context.Context, secrets map[string]types.Secret, secretIds []string) error {
	// Guardian records and every epoch of their key histories. A retired epoch
	// key still matters: share material encrypted to it may be in flight.
	if err := k.Guardians.Walk(ctx, nil, func(address string, g types.Guardian) (bool, error) {
		if err := crypto.ValidateX25519PublicKey(g.EncryptionPublicKey); err != nil {
			return true, fmt.Errorf("invariant 8: guardian %s holds an unusable encryption key: %w", address, err)
		}
		return false, nil
	}); err != nil {
		return err
	}
	if err := k.GuardianKeyHistory.Walk(ctx, nil, func(key collections.Pair[string, uint64], entry types.KeyHistoryEntry) (bool, error) {
		if err := crypto.ValidateX25519PublicKey(entry.PublicKey); err != nil {
			return true, fmt.Errorf("invariant 8: guardian %s epoch %d holds an unusable key: %w",
				key.K1(), key.K2(), err)
		}
		return false, nil
	}); err != nil {
		return err
	}

	// Detection hints on live secrets. A small-order R makes one hint match
	// every recipient, so it is a state-integrity violation, not just a client
	// annoyance.
	//
	// Scoped to hints that are actually PRESENT. This invariant is about key
	// usability, and hint presence is a separate property that nothing else
	// asserts: the message handlers require a 32-byte R (ValidateDetectionHint),
	// but GenesisState.Validate does not check hint shape, and an absent hint is
	// harmless — it means no recipient can discover the secret, which is the
	// no-discovery choice taken to its limit. Asserting presence here would be a
	// different invariant smuggled in under this one.
	for _, id := range secretIds {
		s := secrets[id]
		if len(s.DetectionHint.EphemeralPub) == 0 {
			continue
		}
		if err := crypto.ValidateX25519PublicKey(s.DetectionHint.EphemeralPub); err != nil {
			return fmt.Errorf("invariant 8: secret %s carries an unusable detection hint key: %w", id, err)
		}
	}
	return nil
}

// checkEligibilityIndexIntegrity asserts invariant 9: the selection eligibility
// index agrees exactly with the guardian records it projects.
//
// The index decides who can be selected, so drift here is not a performance
// bug — a stale entry would let selection consider a guardian under an
// availability window, float or bond count it no longer has, and that is a
// consensus fault. SetGuardian is the module's only guardian writer and
// maintains the index, so this proves that property rather than assuming it, and
// catches any future writer that bypasses the choke point.
//
// Both directions are checked: every accepting guardian appears exactly once
// with a matching projection, and no entry survives whose guardian has stopped
// accepting or never existed.
func (k Keeper) checkEligibilityIndexIntegrity(ctx context.Context) error {
	// Keyed by address, not by the index key: collections.Pair holds POINTERS to
	// its parts, so two Pairs built from equal values are distinct Go map keys and
	// every lookup would miss. The address is unique per guardian anyway, and
	// available_until is then checked explicitly, which is what makes a stale key
	// reportable rather than merely absent.
	type expectation struct {
		availableUntil int64
		projection     types.GuardianEligibility
	}
	expected := map[string]expectation{}
	if err := k.Guardians.Walk(ctx, nil, func(address string, guardian types.Guardian) (bool, error) {
		if guardian.AcceptingSecrets {
			expected[address] = expectation{
				availableUntil: guardian.AvailableUntil,
				projection:     GuardianEligibilityOf(guardian),
			}
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("invariant 9 (eligibility index): walking guardians: %w", err)
	}

	seen := 0
	if err := k.GuardianEligibility.Walk(ctx, nil, func(key collections.Pair[int64, string], entry types.GuardianEligibility) (bool, error) {
		address := key.K2()
		want, ok := expected[address]
		if !ok {
			// Name the divergence: an orphan or a guardian that stopped accepting
			// implicate different writers, so guessing wastes the signal.
			reason := "no guardian record exists at that address"
			if _, found := k.GetGuardian(ctx, address); found {
				reason = "the guardian has accepting_secrets = false and should not be indexed at all"
			}
			return true, fmt.Errorf(
				"invariant 9 (eligibility index): entry (available_until %d, %s) has no accepting guardian behind it: %s — "+
					"a guardian record was written or removed without going through SetGuardian",
				key.K1(), address, reason)
		}
		if want.availableUntil != key.K1() {
			return true, fmt.Errorf(
				"invariant 9 (eligibility index): entry for %s is keyed at available_until %d but the record now says %d — "+
					"the previous key was not retired when the window moved, so selection can still see the old one",
				address, key.K1(), want.availableUntil)
		}
		if !eligibilityEqual(entry, want.projection) {
			return true, fmt.Errorf(
				"invariant 9 (eligibility index): entry for %s is stale (indexed %v, record projects %v) — "+
					"the index was not rewritten when the record changed",
				address, entry, want.projection)
		}
		seen++
		return false, nil
	}); err != nil {
		return err
	}

	if seen != len(expected) {
		return fmt.Errorf(
			"invariant 9 (eligibility index): %d accepting guardians but %d index entries — "+
				"guardians are missing from the index and cannot be selected",
			len(expected), seen)
	}
	return nil
}

// checkKeyHistoryIntegrity asserts invariant 7 (key rotation): every
// guardian's key history is contiguous from epoch 0 to its current_key_epoch
// with strictly increasing effective heights; the record's key is the current
// epoch's key; every history key is reserved in the uniqueness index for this
// guardian (and the index holds nothing else); and the derived epoch for
// every in-flight assignment exists in its guardian's history.
func (k Keeper) checkKeyHistoryIntegrity(ctx context.Context, secrets map[string]types.Secret, secretIds []string) error {
	historyKeys := map[string]string{} // key bytes -> guardian address
	if err := k.Guardians.Walk(ctx, nil, func(addr string, g types.Guardian) (bool, error) {
		lastEffective := int64(-1)
		for epoch := uint64(0); epoch <= g.CurrentKeyEpoch; epoch++ {
			entry, err := k.GuardianKeyHistory.Get(ctx, collections.Join(addr, epoch))
			if err != nil {
				return true, fmt.Errorf("invariant 7 (key history): guardian %s is missing key epoch %d (current %d)", addr, epoch, g.CurrentKeyEpoch)
			}
			if entry.EffectiveFromHeight <= lastEffective {
				return true, fmt.Errorf("invariant 7: guardian %s key epoch %d effective height %d does not increase (previous %d)",
					addr, epoch, entry.EffectiveFromHeight, lastEffective)
			}
			lastEffective = entry.EffectiveFromHeight
			if holder, err := k.GuardianKeyIndex.Get(ctx, entry.PublicKey); err != nil {
				return true, fmt.Errorf("invariant 7: guardian %s key epoch %d is not reserved in the uniqueness index", addr, epoch)
			} else if holder != addr {
				return true, fmt.Errorf("invariant 7: guardian %s key epoch %d is indexed to %s", addr, epoch, holder)
			}
			if prev, dup := historyKeys[string(entry.PublicKey)]; dup {
				return true, fmt.Errorf("invariant 7: guardians %s and %s hold the same key — keys are globally unique forever", prev, addr)
			}
			historyKeys[string(entry.PublicKey)] = addr
			if epoch == g.CurrentKeyEpoch && !bytes.Equal(entry.PublicKey, g.EncryptionPublicKey) {
				return true, fmt.Errorf("invariant 7: guardian %s record key does not match its current key epoch %d", addr, epoch)
			}
		}
		// Epochs above the pointer would be unreachable state
		if _, err := k.GuardianKeyHistory.Get(ctx, collections.Join(addr, g.CurrentKeyEpoch+1)); err == nil {
			return true, fmt.Errorf("invariant 7: guardian %s holds a key epoch above its current_key_epoch %d", addr, g.CurrentKeyEpoch)
		}
		return false, nil
	}); err != nil {
		return err
	}

	// The index must hold exactly the history's keys — an orphaned
	// reservation would block a legitimate registration forever.
	if err := k.GuardianKeyIndex.Walk(ctx, nil, func(key []byte, holder string) (bool, error) {
		if historyKeys[string(key)] != holder {
			return true, fmt.Errorf("invariant 7: uniqueness index holds a key for %s with no matching history entry", holder)
		}
		return false, nil
	}); err != nil {
		return err
	}

	// Every in-flight assignment's epoch must be derivable from its
	// guardian's history at the secret's creation (= selection) height.
	for _, id := range secretIds {
		s := secrets[id]
		if s.IsComplete() {
			continue
		}
		for _, addr := range s.SelectedGuardians {
			if _, _, err := k.GuardianEpochInForce(ctx, addr, s.CreatedAt); err != nil {
				return fmt.Errorf("invariant 7: secret %s guardian %s has no derivable key epoch at creation height %d: %w",
					id, addr, s.CreatedAt, err)
			}
		}
	}
	return nil
}

// checkNoStrandedBonds asserts invariant 3: every guardian's locked float
// equals exactly the sum of its OWN frozen bonds for its ACCEPTED,
// not-early-slashed assignments on non-terminal secrets, and its
// active-bond counter matches the count of those assignments. Terminal
// secrets contribute nothing.
func (k Keeper) checkNoStrandedBonds(ctx context.Context, secrets map[string]types.Secret, secretIds []string) error {
	expectedLocked := map[string]math.Int{}
	expectedActive := map[string]int64{}
	for _, id := range secretIds {
		s := secrets[id]
		if s.IsComplete() {
			continue
		}
		if err := k.WalkAssignments(ctx, s.Id, func(guardianAddress string, record types.AssignmentRecord) (bool, error) {
			if record.Status != types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
				return false, nil
			}
			if k.IsGuardianSlashedForEarlyReveal(ctx, guardianAddress, s.Id) {
				return false, nil // bond fully deducted at report time
			}
			bond, hasBond := s.GuardianBondAmount(guardianAddress)
			if !hasBond {
				return true, fmt.Errorf("invariant 3: accepted guardian %s on secret %s has no frozen bond recorded", guardianAddress, s.Id)
			}
			cur, ok := expectedLocked[guardianAddress]
			if !ok {
				cur = math.ZeroInt()
			}
			expectedLocked[guardianAddress] = cur.Add(bond)
			expectedActive[guardianAddress]++
			return false, nil
		}); err != nil {
			return fmt.Errorf("invariant 3: walking assignments for %s: %w", id, err)
		}
	}

	return k.Guardians.Walk(ctx, nil, func(addr string, g types.Guardian) (bool, error) {
		locked := math.ZeroInt()
		if g.LockedStake != nil {
			locked = g.LockedStake.Amount
		}
		want, ok := expectedLocked[addr]
		if !ok {
			want = math.ZeroInt()
		}
		if !locked.Equal(want) {
			return true, fmt.Errorf("invariant 3 (no stranded bonds): guardian %s has %s locked but live accepted assignments account for %s",
				addr, locked, want)
		}

		total := math.ZeroInt()
		if g.Stake != nil {
			total = g.Stake.Amount
		}
		if locked.GT(total) {
			return true, fmt.Errorf("invariant 3: guardian %s locked %s exceeds float total %s", addr, locked, total)
		}

		if g.ActiveBondCount != expectedActive[addr] {
			return true, fmt.Errorf("invariant 3 (active-bond counter): guardian %s counter reads %d but live accepted assignments account for %d",
				addr, g.ActiveBondCount, expectedActive[addr])
		}
		return false, nil
	})
}

// checkQueueHygiene asserts invariant 4: reserved/awaiting secrets hold
// exactly a commit entry (due deadline+1) and a settlement entry (due end+1);
// pending and reconstructable secrets hold only the settlement entry;
// terminal secrets hold none; and no queue entry points at a missing secret.
func (k Keeper) checkQueueHygiene(ctx context.Context, secrets map[string]types.Secret, secretIds []string) error {
	commitEntries := map[string]int64{} // secretId -> due height
	if err := k.CommitQueue.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		if _, ok := secrets[key.K2()]; !ok {
			return true, fmt.Errorf("invariant 4: commit entry for nonexistent secret %s", key.K2())
		}
		commitEntries[key.K2()] = key.K1()
		return false, nil
	}); err != nil {
		return err
	}
	settlementEntries := map[string]int64{}
	if err := k.SettlementQueue.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		if _, ok := secrets[key.K2()]; !ok {
			return true, fmt.Errorf("invariant 4: settlement entry for nonexistent secret %s", key.K2())
		}
		settlementEntries[key.K2()] = key.K1()
		return false, nil
	}); err != nil {
		return err
	}

	for _, id := range secretIds {
		s := secrets[id]
		switch s.State {
		case types.SECRET_STATUS_RESERVED, types.SECRET_STATUS_AWAITING_ACCEPTANCE:
			due, ok := commitEntries[id]
			if !ok {
				return fmt.Errorf("invariant 4: pre-activation secret %s (%s) missing its commit entry", id, s.State)
			}
			if due != s.CommitDeadline+1 {
				return fmt.Errorf("invariant 4: commit entry for %s at wrong due height %d (want %d)", id, due, s.CommitDeadline+1)
			}
			due, ok = settlementEntries[id]
			if !ok {
				return fmt.Errorf("invariant 4: pre-activation secret %s missing its settlement entry", id)
			}
			if due != s.RevealEndBlock+1 {
				return fmt.Errorf("invariant 4: settlement entry for %s at wrong due height %d (want %d)", id, due, s.RevealEndBlock+1)
			}
		case types.SECRET_STATUS_PENDING, types.SECRET_STATUS_RECONSTRUCTABLE:
			if _, ok := commitEntries[id]; ok {
				return fmt.Errorf("invariant 4: activated secret %s still holds a commit entry", id)
			}
			due, ok := settlementEntries[id]
			if !ok {
				return fmt.Errorf("invariant 4: active secret %s (%s) missing its settlement entry", id, s.State)
			}
			if due != s.RevealEndBlock+1 {
				return fmt.Errorf("invariant 4: settlement entry for %s at wrong due height %d (want %d)", id, due, s.RevealEndBlock+1)
			}
		default: // terminal
			if _, ok := commitEntries[id]; ok {
				return fmt.Errorf("invariant 4: terminal secret %s (%s) still holds a commit entry", id, s.State)
			}
			if _, ok := settlementEntries[id]; ok {
				return fmt.Errorf("invariant 4: terminal secret %s (%s) still holds a settlement entry", id, s.State)
			}
		}
	}
	return nil
}

// checkPruneQueueHygiene asserts invariant 4b (retention): every
// terminal-but-unpruned secret holds exactly one prune entry; every prune
// entry references an existing terminal secret (pruned secrets are dequeued
// in the same step that deletes their record).
func (k Keeper) checkPruneQueueHygiene(ctx context.Context, secrets map[string]types.Secret, secretIds []string) error {
	pruneEntries := map[string]int64{}
	if err := k.PruneQueue.Walk(ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		if _, ok := secrets[key.K2()]; !ok {
			return true, fmt.Errorf("invariant 4b: prune entry for nonexistent secret %s", key.K2())
		}
		pruneEntries[key.K2()] = key.K1()
		return false, nil
	}); err != nil {
		return err
	}
	for _, id := range secretIds {
		s := secrets[id]
		_, ok := pruneEntries[id]
		if s.IsComplete() && !ok {
			return fmt.Errorf("invariant 4b: terminal secret %s (%s) missing its prune entry", id, s.State)
		}
		if !s.IsComplete() && ok {
			return fmt.Errorf("invariant 4b: non-terminal secret %s (%s) holds a prune entry", id, s.State)
		}
	}
	return nil
}

// checkCounterConsistency asserts invariant 5: the denormalised counters are
// protocol state (slot cap, activation and threshold checks read them) — they
// must never drift from the side-stores. Terminal secrets are the exception
// BY DESIGN: Stage 1 retention deletes their share and assignment records at
// the terminal transition, so for them the invariant flips — those stores
// must be EMPTY, while the reveal records (reconstruction inputs) survive
// until Stage 2 and stay checked.
func (k Keeper) checkCounterConsistency(ctx context.Context, secrets map[string]types.Secret, secretIds []string) error {
	for _, id := range secretIds {
		s := secrets[id]

		revealCount, err := k.countReveals(ctx, id)
		if err != nil {
			return fmt.Errorf("invariant 5: counting reveals for %s: %w", id, err)
		}

		if s.IsComplete() {
			if err := k.WalkAssignments(ctx, id, func(guardianAddress string, _ types.AssignmentRecord) (bool, error) {
				return true, fmt.Errorf("invariant 5 (retention stage 1): terminal secret %s (%s) still has an assignment record for %s",
					id, s.State, guardianAddress)
			}); err != nil {
				return err
			}
			if err := k.SecretShares.Walk(ctx, collections.NewPrefixedPairRange[string, string](id),
				func(key collections.Pair[string, string], _ types.SecretShareData) (bool, error) {
					return true, fmt.Errorf("invariant 5 (retention stage 1): terminal secret %s (%s) still has a share record for %s",
						id, s.State, key.K2())
				}); err != nil {
				return err
			}
			if s.RevealedCount != revealCount {
				return fmt.Errorf("invariant 5: terminal secret %s revealed_count=%d does not match its reveal store (%d)",
					id, s.RevealedCount, revealCount)
			}
			continue
		}

		selected := map[string]bool{}
		for _, addr := range s.SelectedGuardians {
			selected[addr] = true
		}

		acceptedCount := int64(0)
		if err := k.WalkAssignments(ctx, id, func(guardianAddress string, record types.AssignmentRecord) (bool, error) {
			if !selected[guardianAddress] {
				return true, fmt.Errorf("invariant 5: assignment record for %s on secret %s does not belong to a selected guardian",
					guardianAddress, id)
			}
			hasShare, err := k.SecretShares.Has(ctx, collections.Join(id, guardianAddress))
			if err != nil {
				return true, err
			}
			if !hasShare {
				return true, fmt.Errorf("invariant 5: assignment record for %s on secret %s has no matching share record",
					guardianAddress, id)
			}
			if record.Status == types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
				acceptedCount++
			}
			return false, nil
		}); err != nil {
			return err
		}
		if s.AcceptedCount != acceptedCount {
			return fmt.Errorf("invariant 5: secret %s accepted_count=%d but assignment store holds %d ACCEPTED records",
				id, s.AcceptedCount, acceptedCount)
		}
		if s.RevealedCount != revealCount {
			return fmt.Errorf("invariant 5: secret %s revealed_count=%d does not match its reveal store (%d)",
				id, s.RevealedCount, revealCount)
		}
	}
	return nil
}

// countReveals counts a secret's revealed shares without loading share bytes
// into a slice the caller does not need.
func (k Keeper) countReveals(ctx context.Context, secretID string) (int64, error) {
	count := int64(0)
	rng := collections.NewPrefixedPairRange[string, string](secretID)
	err := k.SecretReveals.Walk(ctx, rng, func(_ collections.Pair[string, string], _ types.RevealedShare) (bool, error) {
		count++
		return false, nil
	})
	return count, err
}

// checkPayloadPresence asserts invariant 6: the payload ciphertext is written
// exactly once, at distribution — absent while RESERVED, present (and capped)
// from AWAITING_ACCEPTANCE through the reveal phase, alongside a 32B
// per-secret public key. Terminal states are not asserted (retention prunes
// them).
func (k Keeper) checkPayloadPresence(ctx context.Context, secrets map[string]types.Secret, secretIds []string) error {
	for _, id := range secretIds {
		s := secrets[id]
		hasPayload, err := k.SecretPayloads.Has(ctx, id)
		if err != nil {
			return fmt.Errorf("invariant 6: checking payload for %s: %w", id, err)
		}
		switch s.State {
		case types.SECRET_STATUS_RESERVED:
			if hasPayload {
				return fmt.Errorf("invariant 6: reserved secret %s already has a payload ciphertext", id)
			}
		case types.SECRET_STATUS_AWAITING_ACCEPTANCE, types.SECRET_STATUS_PENDING, types.SECRET_STATUS_RECONSTRUCTABLE:
			if !hasPayload {
				return fmt.Errorf("invariant 6: distributed secret %s (%s) has no payload ciphertext", id, s.State)
			}
			payload, err := k.SecretPayloads.Get(ctx, id)
			if err != nil {
				return fmt.Errorf("invariant 6: reading payload for %s: %w", id, err)
			}
			if len(payload) == 0 {
				return fmt.Errorf("invariant 6: secret %s payload is empty", id)
			}
			if int64(len(payload)) > types.MaxPayloadSize {
				return fmt.Errorf("invariant 6: secret %s payload exceeds MaxPayloadSize (%d > %d)",
					id, len(payload), types.MaxPayloadSize)
			}
			if len(s.SecretPublicKey) != types.SecretPublicKeySize {
				return fmt.Errorf("invariant 6: secret %s has no per-secret public key", id)
			}
			if err := crypto.ValidateX25519PublicKey(s.SecretPublicKey); err != nil {
				return fmt.Errorf("invariant 6: secret %s has an unusable per-secret public key: %w", id, err)
			}
		}
	}
	return nil
}
