package types

import (
	"bytes"
	"fmt"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Secrets:   []*Secret{},
		Guardians: []*Guardian{},
	}
}

// validSecretStates is the closed set of legal FSM states a stored secret can
// occupy (secret_state_machine.go). Genesis must never admit a state the
// runtime would treat as impossible.
var validSecretStates = map[string]bool{
	SECRET_STATUS_RESERVED:            true,
	SECRET_STATUS_AWAITING_ACCEPTANCE: true,
	SECRET_STATUS_PENDING:             true,
	SECRET_STATUS_RECONSTRUCTABLE:     true,
	SECRET_STATUS_REVEALED:            true,
	SECRET_STATUS_CANCELLED:           true,
	SECRET_STATUS_FAILED:              true,
}

// IsValidSecretState reports whether state is a legal FSM state.
func IsValidSecretState(state string) bool {
	return validSecretStates[state]
}

// Validate performs structural genesis state validation returning an error
// upon any failure: per-record shape (legal FSM state, threshold ≤ shares,
// well-formed coins), referential integrity of every side-store entry, and
// consistency of the denormalised counters with the side-stores. The
// deeper store-level assertions (bond accounting, queue hygiene) run in the
// keeper's import-time state-integrity sweep after InitGenesis.
func (gs GenesisState) Validate() error {
	// Rebate accrual record: negative bookkeeping is nonsense, and a
	// reservation must be backed by rebates actually credited to secrets -
	// cross-checked against the secret set at the end of this function.
	if gs.RebateState.Allowance < 0 {
		return fmt.Errorf("rebate allowance cannot be negative: %d", gs.RebateState.Allowance)
	}
	if gs.RebateState.AccruedHeight < 0 {
		return fmt.Errorf("rebate accrued height cannot be negative: %d", gs.RebateState.AccruedHeight)
	}
	if gs.RebateState.Reserved < 0 {
		return fmt.Errorf("rebate reservation cannot be negative: %d", gs.RebateState.Reserved)
	}
	uncollectedRebates := int64(0)

	// Validate secrets
	secretsById := make(map[string]*Secret)
	for _, secret := range gs.Secrets {
		if secret == nil {
			return fmt.Errorf("secret cannot be nil")
		}

		if secret.Id == "" {
			return fmt.Errorf("secret ID cannot be empty")
		}

		if secretsById[secret.Id] != nil {
			return fmt.Errorf("duplicate secret ID: %s", secret.Id)
		}
		secretsById[secret.Id] = secret

		if secret.Creator == "" {
			return fmt.Errorf("secret creator cannot be empty")
		}

		if !IsValidSecretState(secret.State) {
			return fmt.Errorf("secret %s has invalid state %q", secret.Id, secret.State)
		}

		// Rebates exist only on revealed secrets, and only a credited rebate
		// can be marked collected.
		if secret.RebateAmount < 0 {
			return fmt.Errorf("secret %s has negative rebate amount %d", secret.Id, secret.RebateAmount)
		}
		if secret.RebateAmount > 0 && secret.State != SECRET_STATUS_REVEALED {
			return fmt.Errorf("secret %s carries a rebate in state %q - rebates are credited only on revealed secrets",
				secret.Id, secret.State)
		}
		if secret.RebateCollected && secret.RebateAmount == 0 {
			return fmt.Errorf("secret %s is marked rebate-collected with no rebate credited", secret.Id)
		}
		if secret.RebateAmount > 0 && !secret.RebateCollected {
			uncollectedRebates += secret.RebateAmount
		}

		if secret.Threshold <= 0 {
			return fmt.Errorf("secret threshold must be positive")
		}

		if secret.MinShares < secret.Threshold {
			return fmt.Errorf("secret %s min_shares (%d) below threshold (%d)", secret.Id, secret.MinShares, secret.Threshold)
		}

		if secret.MaxShares < secret.MinShares {
			return fmt.Errorf("secret %s max_shares (%d) below min_shares (%d)", secret.Id, secret.MaxShares, secret.MinShares)
		}

		if secret.RevealStartBlock <= 0 {
			return fmt.Errorf("secret reveal start block must be positive")
		}

		if secret.RevealEndBlock <= secret.RevealStartBlock {
			return fmt.Errorf("secret reveal end block must be after start block")
		}

		// The economics fields are frozen at publication and read verbatim at
		// settlement — malformed coins here would poison every payment path.
		if !secret.RewardPool.IsValid() {
			return fmt.Errorf("secret %s reward pool must be a valid coin", secret.Id)
		}
		if len(secret.GuardianBondAmounts) != len(secret.SelectedGuardians) {
			return fmt.Errorf("secret %s must carry exactly one frozen bond per selected guardian (%d bonds, %d guardians)",
				secret.Id, len(secret.GuardianBondAmounts), len(secret.SelectedGuardians))
		}
		for i, bond := range secret.GuardianBondAmounts {
			if bond <= 0 {
				return fmt.Errorf("secret %s frozen bond for guardian %s must be positive, got %d",
					secret.Id, secret.SelectedGuardians[i], bond)
			}
		}
	}

	// Validate guardians
	guardianAddresses := make(map[string]bool)
	for _, guardian := range gs.Guardians {
		if guardian == nil {
			return fmt.Errorf("guardian cannot be nil")
		}

		if guardian.Address == "" {
			return fmt.Errorf("guardian address cannot be empty")
		}

		if guardianAddresses[guardian.Address] {
			return fmt.Errorf("duplicate guardian address: %s", guardian.Address)
		}
		guardianAddresses[guardian.Address] = true

		if len(guardian.EncryptionPublicKey) != PublicKeyLength {
			return fmt.Errorf("guardian encryption public key must be %d bytes", PublicKeyLength)
		}

		if guardian.AvailableUntil <= guardian.AvailableFrom {
			return fmt.Errorf("guardian available_until must be greater than available_from")
		}

		// The bond multiplier must sit inside its hard range (a zero from
		// pre-k state would silently read as the floor on-chain; genesis is
		// stricter and rejects it), and the active-bond counter can never be
		// negative or above the concurrency cap.
		if guardian.BondK < MinBondK || guardian.BondK > MaxBondK {
			return fmt.Errorf("guardian %s bond multiplier %d outside [%d, %d]",
				guardian.Address, guardian.BondK, MinBondK, MaxBondK)
		}
		if guardian.ActiveBondCount < 0 || guardian.ActiveBondCount > MaxActiveBondsPerGuardian {
			return fmt.Errorf("guardian %s active-bond count %d outside [0, %d]",
				guardian.Address, guardian.ActiveBondCount, MaxActiveBondsPerGuardian)
		}

		// Bonded model: the float (Stake = total, LockedStake = bonded portion)
		// may legitimately be zero — the entry fee gates registration.
		// Validate only internal consistency: coins well-formed, locked ≤ total.
		if guardian.Stake != nil && !guardian.Stake.IsValid() {
			return fmt.Errorf("guardian float total must be a valid coin")
		}
		if guardian.LockedStake != nil && !guardian.LockedStake.IsValid() {
			return fmt.Errorf("guardian locked float must be a valid coin")
		}
		if guardian.LockedStake != nil && !guardian.LockedStake.IsZero() {
			if guardian.Stake == nil || guardian.Stake.Amount.LT(guardian.LockedStake.Amount) {
				return fmt.Errorf("guardian locked float cannot exceed float total")
			}
		}
	}

	// Key histories: every entry must reference a registered guardian; per
	// guardian the epochs must be contiguous from 0 with strictly increasing
	// effective heights, the max epoch must match the record's
	// current_key_epoch, and the record's key must be the current epoch's
	// key; every key (historical or current) must be globally unique — a
	// retired key stays reserved forever. A guardian with NO entries is the
	// migration path (epoch 0 is synthesised from its record on import) and
	// must sit at current_key_epoch = 0.
	guardiansByAddr := make(map[string]*Guardian)
	for _, guardian := range gs.Guardians {
		guardiansByAddr[guardian.Address] = guardian
	}
	historyByGuardian := make(map[string]map[uint64]*KeyHistoryEntry)
	seenKeys := make(map[string]string) // hex-ish string of key -> holder
	for i := range gs.GuardianKeyHistories {
		entry := &gs.GuardianKeyHistories[i]
		if guardiansByAddr[entry.GuardianAddress] == nil {
			return fmt.Errorf("key history entry references nonexistent guardian %s", entry.GuardianAddress)
		}
		if len(entry.Entry.PublicKey) != PublicKeyLength {
			return fmt.Errorf("guardian %s key epoch %d public key must be %d bytes", entry.GuardianAddress, entry.Epoch, PublicKeyLength)
		}
		if entry.Entry.EffectiveFromHeight < 0 {
			return fmt.Errorf("guardian %s key epoch %d effective height cannot be negative", entry.GuardianAddress, entry.Epoch)
		}
		epochs := historyByGuardian[entry.GuardianAddress]
		if epochs == nil {
			epochs = make(map[uint64]*KeyHistoryEntry)
			historyByGuardian[entry.GuardianAddress] = epochs
		}
		if epochs[entry.Epoch] != nil {
			return fmt.Errorf("duplicate key history entry for guardian %s epoch %d", entry.GuardianAddress, entry.Epoch)
		}
		epochs[entry.Epoch] = &entry.Entry
		if holder, dup := seenKeys[string(entry.Entry.PublicKey)]; dup {
			return fmt.Errorf("guardian %s key epoch %d reuses a key already registered by %s — keys are globally unique forever",
				entry.GuardianAddress, entry.Epoch, holder)
		}
		seenKeys[string(entry.Entry.PublicKey)] = entry.GuardianAddress
	}
	for _, guardian := range gs.Guardians {
		epochs := historyByGuardian[guardian.Address]
		if epochs == nil {
			if guardian.CurrentKeyEpoch != 0 {
				return fmt.Errorf("guardian %s has current_key_epoch %d but no key history entries", guardian.Address, guardian.CurrentKeyEpoch)
			}
			// Epoch 0 is synthesised from the record on import; its key still
			// participates in global uniqueness.
			if holder, dup := seenKeys[string(guardian.EncryptionPublicKey)]; dup {
				return fmt.Errorf("guardian %s record key collides with a key registered by %s", guardian.Address, holder)
			}
			seenKeys[string(guardian.EncryptionPublicKey)] = guardian.Address
			continue
		}
		lastEffective := int64(-1)
		for epoch := uint64(0); epoch <= guardian.CurrentKeyEpoch; epoch++ {
			entry := epochs[epoch]
			if entry == nil {
				return fmt.Errorf("guardian %s key history is missing epoch %d (epochs must be contiguous from 0)", guardian.Address, epoch)
			}
			if entry.EffectiveFromHeight <= lastEffective {
				return fmt.Errorf("guardian %s key epoch %d effective height %d does not increase (previous %d)",
					guardian.Address, epoch, entry.EffectiveFromHeight, lastEffective)
			}
			lastEffective = entry.EffectiveFromHeight
		}
		if uint64(len(epochs)) != guardian.CurrentKeyEpoch+1 {
			return fmt.Errorf("guardian %s key history holds %d entries but current_key_epoch is %d",
				guardian.Address, len(epochs), guardian.CurrentKeyEpoch)
		}
		if !bytes.Equal(epochs[guardian.CurrentKeyEpoch].PublicKey, guardian.EncryptionPublicKey) {
			return fmt.Errorf("guardian %s record key does not match its current key epoch %d", guardian.Address, guardian.CurrentKeyEpoch)
		}
	}

	// Side-store referential integrity: every entry must reference a stored
	// secret and a registered guardian (guardian records are permanent), and
	// no (secret, guardian) key may appear twice — a duplicate would silently
	// overwrite on import.
	shareKeys := make(map[string]bool)
	sharesBySecret := make(map[string]bool)
	for _, entry := range gs.SecretShares {
		if secretsById[entry.SecretId] == nil {
			return fmt.Errorf("share record references nonexistent secret %s", entry.SecretId)
		}
		if !guardianAddresses[entry.GuardianAddress] {
			return fmt.Errorf("share record for secret %s references nonexistent guardian %s", entry.SecretId, entry.GuardianAddress)
		}
		key := entry.SecretId + "/" + entry.GuardianAddress
		if shareKeys[key] {
			return fmt.Errorf("duplicate share record for secret %s guardian %s", entry.SecretId, entry.GuardianAddress)
		}
		shareKeys[key] = true
		sharesBySecret[entry.SecretId] = true
	}

	assignmentKeys := make(map[string]bool)
	acceptedCounts := make(map[string]int64) // secretId -> ACCEPTED assignment records
	assignmentsBySecret := make(map[string]bool)
	for _, entry := range gs.SecretAssignments {
		if secretsById[entry.SecretId] == nil {
			return fmt.Errorf("assignment record references nonexistent secret %s", entry.SecretId)
		}
		if !guardianAddresses[entry.GuardianAddress] {
			return fmt.Errorf("assignment record for secret %s references nonexistent guardian %s", entry.SecretId, entry.GuardianAddress)
		}
		key := entry.SecretId + "/" + entry.GuardianAddress
		if assignmentKeys[key] {
			return fmt.Errorf("duplicate assignment record for secret %s guardian %s", entry.SecretId, entry.GuardianAddress)
		}
		assignmentKeys[key] = true
		assignmentsBySecret[entry.SecretId] = true
		if entry.Record.Status == AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
			acceptedCounts[entry.SecretId]++
		}
	}

	revealKeys := make(map[string]bool)
	revealCounts := make(map[string]int64) // secretId -> reveal records
	for _, entry := range gs.SecretReveals {
		if secretsById[entry.SecretId] == nil {
			return fmt.Errorf("reveal record references nonexistent secret %s", entry.SecretId)
		}
		if !guardianAddresses[entry.Reveal.GuardianAddress] {
			return fmt.Errorf("reveal record for secret %s references nonexistent guardian %s", entry.SecretId, entry.Reveal.GuardianAddress)
		}
		key := entry.SecretId + "/" + entry.Reveal.GuardianAddress
		if revealKeys[key] {
			return fmt.Errorf("duplicate reveal record for secret %s guardian %s", entry.SecretId, entry.Reveal.GuardianAddress)
		}
		revealKeys[key] = true
		revealCounts[entry.SecretId]++
	}

	payloadSecrets := make(map[string]bool)
	for _, entry := range gs.SecretPayloads {
		if secretsById[entry.SecretId] == nil {
			return fmt.Errorf("payload record references nonexistent secret %s", entry.SecretId)
		}
		if payloadSecrets[entry.SecretId] {
			return fmt.Errorf("duplicate payload record for secret %s", entry.SecretId)
		}
		payloadSecrets[entry.SecretId] = true
	}

	slashKeys := make(map[string]bool)
	for _, entry := range gs.EarlyRevealSlashes {
		if secretsById[entry.SecretId] == nil {
			return fmt.Errorf("early-reveal slash mark references nonexistent secret %s", entry.SecretId)
		}
		if !guardianAddresses[entry.GuardianAddress] {
			return fmt.Errorf("early-reveal slash mark for secret %s references nonexistent guardian %s", entry.SecretId, entry.GuardianAddress)
		}
		key := entry.SecretId + "/" + entry.GuardianAddress
		if slashKeys[key] {
			return fmt.Errorf("duplicate early-reveal slash mark for secret %s guardian %s", entry.SecretId, entry.GuardianAddress)
		}
		slashKeys[key] = true
	}

	// Tombstones anchor PRUNED secrets: their live record was deleted at
	// Stage 2, so a tombstone colliding with a stored secret is inconsistent.
	tombstoneSecrets := make(map[string]bool)
	for _, entry := range gs.SecretTombstones {
		if secretsById[entry.SecretId] != nil {
			return fmt.Errorf("tombstone for secret %s collides with a live secret record", entry.SecretId)
		}
		if tombstoneSecrets[entry.SecretId] {
			return fmt.Errorf("duplicate tombstone for secret %s", entry.SecretId)
		}
		tombstoneSecrets[entry.SecretId] = true
	}

	// Counter consistency: the denormalised accepted_count/revealed_count are
	// protocol state and must match the side-stores exactly. Terminal secrets
	// flip the assignment/share rule — Stage 1 retention deleted those
	// records at the terminal transition, so none may be present.
	for _, secret := range gs.Secrets {
		if secret.IsComplete() {
			if assignmentsBySecret[secret.Id] {
				return fmt.Errorf("terminal secret %s (%s) still has assignment records (deleted at the terminal transition)", secret.Id, secret.State)
			}
			if sharesBySecret[secret.Id] {
				return fmt.Errorf("terminal secret %s (%s) still has share records (deleted at the terminal transition)", secret.Id, secret.State)
			}
		} else if secret.AcceptedCount != acceptedCounts[secret.Id] {
			return fmt.Errorf("secret %s accepted_count=%d but genesis holds %d ACCEPTED assignment records",
				secret.Id, secret.AcceptedCount, acceptedCounts[secret.Id])
		}
		if secret.RevealedCount != revealCounts[secret.Id] {
			return fmt.Errorf("secret %s revealed_count=%d but genesis holds %d reveal records",
				secret.Id, secret.RevealedCount, revealCounts[secret.Id])
		}
	}

	// Every stored secret consumed exactly one counter value (pruned or failed
	// secrets consumed values too, so the counter may legitimately exceed the
	// secret count — but it can never be below it).
	if gs.SecretCounter < uint64(len(gs.Secrets)) {
		return fmt.Errorf("secret counter (%d) below stored secret count (%d) — the counter must never be re-derived or reset", gs.SecretCounter, len(gs.Secrets))
	}

	// The reservation must equal the rebates credited and not yet collected:
	// a surplus would have the pool holding back funds no secret can claim,
	// and a shortfall would let a collection underflow the reservation.
	if gs.RebateState.Reserved != uncollectedRebates {
		return fmt.Errorf("rebate reservation (%d) does not match uncollected credited rebates (%d)",
			gs.RebateState.Reserved, uncollectedRebates)
	}

	return nil
}
