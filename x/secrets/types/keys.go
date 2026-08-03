package types

const (
	// ModuleName defines the module name
	ModuleName = "secrets"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_secrets"

	// QuerierRoute defines the module's query routing key used by the abci.Query RPC service
	QuerierRoute = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

var (
	// SecretPrefix defines the key prefix for secrets in collections
	SecretPrefix = []byte("secrets")

	// GuardianPrefix defines the key prefix for guardians in collections
	GuardianPrefix = []byte("guardians")

	// CommitQueuePrefix keys the commit-deadline queue: (due_height, secret_id)
	// entries drained by EndBlock when due_height <= current height
	CommitQueuePrefix = []byte("commit_queue")

	// SecretsByCreatorPrefix keys the creator index: (creator, secret_id).
	// Maintained at secret creation so creator-scoped queries walk only the
	// creator's own entries instead of the whole secret store. (Deliberately
	// not "secrets_by_creator": no collections prefix may be a byte-prefix of
	// another, and "secrets" already exists.)
	SecretsByCreatorPrefix = []byte("creator_index")

	// SecretSharesPrefix keys the COLD share store: (secret_id, guardian_address)
	// -> SecretShareData. Written once at UserDistributeShares, immutable, read one
	// guardian at a time.
	SecretSharesPrefix = []byte("secret_shares")

	// SecretAssignmentsPrefix keys the HOT assignment-status store:
	// (secret_id, guardian_address) -> AssignmentRecord. Tiny records so status
	// flips and accepted-set walks never touch share bytes.
	SecretAssignmentsPrefix = []byte("secret_assignments")

	// SecretRevealsPrefix keys the reveal store: (secret_id, guardian_address)
	// -> RevealedShare. One write per reveal.
	SecretRevealsPrefix = []byte("secret_reveals")

	// SecretAcceptedCountsPrefix keys each secret's acceptance tally:
	// secret_id -> int64.
	//
	// Held apart from the secret record because it is the ONLY field an
	// acceptance mutates, and the record it used to live in grows with the
	// band — it carries one address and one frozen bond per selected guardian.
	// Rewriting all of that to increment one counter made a guardian's accept
	// gas scale with max_shares (~4,200 per guardian) while the protocol
	// reimburses a flat amount, so wide-band secrets were worked at a loss.
	// The tally is derivable from the assignment records and both the genesis
	// and keeper invariants already prove the two agree; this store just makes
	// reading it O(1) instead of a walk.
	SecretAcceptedCountsPrefix = []byte("secret_accepted_counts")

	// SecretPayloadsPrefix keys the payload store: secret_id -> payload
	// ciphertext C. Written once at UserDistributeShares, immutable; the only copy
	// of the secret material on chain (key-share architecture).
	SecretPayloadsPrefix = []byte("secret_payloads")

	// SettlementQueuePrefix keys the settlement queue: (due_height, secret_id)
	// entries drained by EndBlock when due_height <= current height
	SettlementQueuePrefix = []byte("settlement_queue")

	// SecretCounterPrefix keys the monotonic sequence that assigns secret IDs
	// and differentiates selection seeds. Persisted independently of the
	// secret set (pruning must never reset it) and exported in genesis.
	SecretCounterPrefix = []byte("secret_counter")

	// HintsByCreationPrefix keys the discovery-scan feed: (created_at,
	// secret_id) -> DetectionHint. Written once at creation, served in
	// creation order by Query/HintsSince so recipients resume scanning from a
	// height cursor. Derived state — rebuilt on genesis import, not exported.
	HintsByCreationPrefix = []byte("hints_by_creation")

	// SecretTombstonesPrefix keys the permanent tombstones of pruned secrets:
	// secret_id -> SecretTombstone (~180B each, forever). Exported in genesis.
	SecretTombstonesPrefix = []byte("secret_tombstones")

	// PruneQueuePrefix keys the retention due-height queue: (terminal_at +
	// RetentionBlocks, secret_id), drained by EndBlock like the other queues.
	// Derived state — rebuilt on genesis import from non-pruned terminal
	// secrets, not exported.
	PruneQueuePrefix = []byte("prune_queue")

	// GuardianKeyHistoryPrefix keys the append-only key-epoch history:
	// (guardian_address, epoch) -> KeyHistoryEntry. Written at registration
	// (epoch 0) and each rotation; entries are never overwritten or deleted.
	// Exported in genesis. (No collections prefix may be a byte-prefix of
	// another: "guardians" and this differ at byte 9, 's' vs '_'.)
	GuardianKeyHistoryPrefix = []byte("guardian_key_history")

	// GuardianKeyIndexPrefix keys the global key-uniqueness index:
	// public_key bytes -> holding guardian address. Covers every key ever
	// registered by any guardian, all epochs included, and never shrinks —
	// a retired key stays reserved forever. Derived state — rebuilt on
	// genesis import from the key histories, not exported.
	GuardianKeyIndexPrefix = []byte("guardian_key_index")

	// GuardianEligibilityPrefix keys the selection eligibility index:
	// (available_until, guardian_address) -> GuardianEligibility. Membership is
	// "accepting_secrets = true"; the value carries everything the per-secret
	// filter needs, so selection judges a candidate without reading its record.
	//
	// Selection range-reads from available_until >= reveal_end_block, which is
	// the binding clause of the eligibility predicate. Registrations whose window
	// ends before that sort below the range start and are never read — which is
	// how a permanent registry stops charging creators for guardians that lapsed
	// years ago.
	//
	// Derived state — rebuilt on genesis import via SetGuardian, not exported.
	// (No collections prefix may be a byte-prefix of another: this differs from
	// "guardians" at byte 8 ('_' vs 's') and from the two "guardian_key_*"
	// prefixes at byte 9 ('e' vs 'k').)
	GuardianEligibilityPrefix = []byte("guardian_eligibility")

	// RebateCommitmentsPrefix keys the rebate collection commitments:
	// (secret_id, collector_address) -> commit height. Written by step 1 of
	// commit–reveal collection, read and deleted by step 2, and swept when the
	// secret is pruned. Deliberately NOT exported at genesis: a commitment is
	// short-lived user state, and an export/import that loses one costs a
	// re-commit and one block, nothing more.
	//
	// (No collections prefix may be a byte-prefix of another: this differs from
	// "rebate_state" at byte 8 — 'c' vs 's'.)
	RebateCommitmentsPrefix = []byte("rebate_commitments")

	// RebateExpiryQueuePrefix keys the rebate collection deadline queue:
	// (terminal_at + RebateCollectionWindow, secret_id), drained by EndBlock like
	// the other due-height queues. Voiding an uncollected rebate here — rather
	// than waiting for pruning — returns its reservation to the pool while the
	// pool can still redistribute it.
	//
	// Derived state: rebuilt on genesis import from secrets carrying an
	// uncollected rebate, not exported. (Differs from "rebate_state" and
	// "rebate_commitments" at byte 8 — 'e' vs 's'/'c'.)
	RebateExpiryQueuePrefix = []byte("rebate_expiry_queue")

	// RebateStatePrefix keys the single recipient-rebate accrual record
	// (allowance, accrued height, reserved). Consensus state, exported at
	// genesis. See docs/spec.md "Recipient Rebate".
	RebateStatePrefix = []byte("rebate_state")
)
