package keeper

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log/v2"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"

	"github.com/timeflareio/chain/x/secrets/types"
	"github.com/timeflareio/crypto/go"
)

type Keeper struct {
	cdc          codec.BinaryCodec
	addressCodec address.Codec
	logger       log.Logger

	// Dependencies
	accountKeeper AuthKeeper
	bankKeeper    BankKeeper

	// Cryptographic operations - now handled by package-level functions

	// State management. Secrets holds the slim metadata record; the bulky,
	// per-guardian data lives in the side-stores below, keyed
	// (secret_id, guardian_address), so each operation reads and writes only
	// what it touches.
	Schema  collections.Schema
	Secrets collections.Map[string, types.Secret]

	// COLD: encrypted shares + HMACs. Written once at UserDistributeShares.
	SecretShares collections.Map[collections.Pair[string, string], types.SecretShareData]
	// HOT: assignment status. Created at UserDistributeShares, flipped once on response.
	SecretAssignments collections.Map[collections.Pair[string, string], types.AssignmentRecord]
	// Reveals. One write per revealed share.
	SecretReveals collections.Map[collections.Pair[string, string], types.RevealedShare]
	// COLD: the payload ciphertext C, written once at UserDistributeShares — the
	// only copy of the (doubly encrypted) secret material on chain.
	SecretPayloads collections.Map[string, []byte]
	// HOT: acceptance tally, one small write per acceptance. Kept out of the
	// secret record so incrementing it does not rewrite the whole roster —
	// see types.SecretAcceptedCountsPrefix. GetSecret/SetSecret join and split
	// it transparently, so callers still see Secret.AcceptedCount.
	SecretAcceptedCounts collections.Map[string, int64]

	// Guardian state management
	Guardians        collections.Map[string, types.Guardian]
	EarlyRevealSlash collections.Map[string, bool] // "secretId:guardianAddress" -> slashed_for_early_reveal

	// Append-only key-epoch history: (guardian_address, epoch) ->
	// KeyHistoryEntry. Epoch 0 is the registration key; MsgGuardianRotateKey
	// appends. The epoch in force at height h is the newest entry with
	// effective_from_height <= h (see GuardianEpochInForce) — never stored
	// per-secret. Exported in genesis.
	GuardianKeyHistory collections.Map[collections.Pair[string, uint64], types.KeyHistoryEntry]

	// Global key-uniqueness index: public_key -> holding guardian address,
	// covering every key ever registered at any epoch, forever (retired keys
	// stay reserved — share material encrypted to them may still exist).
	// Derived state — rebuilt on genesis import from the histories.
	GuardianKeyIndex collections.Map[[]byte, string]

	// Due-height queues: EndBlock drains entries with due_height <= current
	// height instead of scanning the whole secret store, so completed secrets
	// are never revisited (O(due) work per block). Entries are keyed
	// (due_height, secret_id) and removed on processing, activation (commit
	// queue) or cancellation. The <=-drain is the self-healing form of an
	// exact == trigger: with sequential block execution entries fire at
	// exactly their due height; anything older (genesis import, migration)
	// is caught on the next block rather than stranded.
	CommitQueue     collections.KeySet[collections.Pair[int64, string]]
	SettlementQueue collections.KeySet[collections.Pair[int64, string]]

	// Creator index: (creator, secret_id), written once at secret creation.
	// Creator-scoped queries prefix-walk this instead of the whole secret store.
	SecretsByCreator collections.KeySet[collections.Pair[string, string]]

	// Discovery-scan feed: (created_at, secret_id) -> DetectionHint, written
	// once at secret creation. Query/HintsSince range-walks this in creation
	// order so recipients scan incrementally from a height cursor instead of
	// re-reading the whole secret store (UUID-keyed pagination has no cursor).
	HintsByCreation collections.Map[collections.Pair[int64, string], types.DetectionHint]

	// Monotonic per-request counter. Consumed at MsgUserRequestGuardians to
	// derive the protocol-assigned secret ID and to differentiate selection
	// seeds within a block — the creator has no control over the value it
	// receives. It only ever climbs and is persisted independently of the
	// secret set (never re-derive it from stored secrets: pruned secrets
	// consumed values too, and IDs must never reissue).
	SecretCounter collections.Sequence

	// Permanent tombstones of pruned secrets: secret_id -> SecretTombstone
	// (~180B each, forever). Written at Stage 2 pruning as the last on-chain
	// anchor: record_digest makes any archived copy of the canonical final
	// record self-authenticating. Exported in genesis.
	SecretTombstones collections.Map[string, types.SecretTombstone]

	// Retention due-height queue: (terminal_at + RetentionBlocks, secret_id).
	// Enqueued at the terminal transition, drained by EndBlock (capped at
	// MaxPrunesPerBlock per block). Derived state — rebuilt on genesis import.
	PruneQueue collections.KeySet[collections.Pair[int64, string]]

	// Selection eligibility index: (available_until, guardian_address) ->
	// GuardianEligibility, holding every guardian with accepting_secrets = true.
	// Phase-1 candidate enumeration range-reads this from
	// available_until >= reveal_end_block instead of walking the whole guardian
	// collection, so its cost tracks the ELIGIBLE set rather than every
	// registration ever made. That walk is metered inside the creator's gas, and
	// registration is permanent, so the walk it replaces grew forever.
	//
	// Maintained solely by SetGuardian (the module's single guardian-write choke
	// point) — see setGuardianEligibility for why it is not maintained at the
	// call sites. Derived state — rebuilt on genesis import.
	GuardianEligibility collections.Map[collections.Pair[int64, string], types.GuardianEligibility]

	// RebateState is the recipient-rebate accrual record: the allowance
	// available to credit, the height it was accrued to, and the total
	// credited but not yet collected. One key, read and written only by
	// settlements that credit a rebate and by collections that pay one out —
	// an idle block never touches it. See docs/spec.md "Recipient Rebate".
	RebateState collections.Item[types.RebateState]

	// RebateCommitments holds one commitment per (secret, collector): the hash
	// binding a recipiency proof to the address that will reveal it, and the
	// height it was recorded at. The reveal must land strictly later, which is
	// what stops an observer front-running a public proof.
	RebateCommitments collections.Map[collections.Pair[string, string], types.RebateCommitmentRecord]

	// RebateExpiryQueue holds each credited rebate's collection deadline, so an
	// uncollected one is voided and its reservation returned on time instead of
	// lingering until the secret is pruned.
	RebateExpiryQueue collections.KeySet[collections.Pair[int64, string]]
}

func NewKeeper(
	cdc codec.BinaryCodec,
	addressCodec address.Codec,
	storeService store.KVStoreService,
	ak AuthKeeper,
	bk BankKeeper,
) Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:           cdc,
		addressCodec:  addressCodec,
		accountKeeper: ak,
		bankKeeper:    bk,
		Secrets:       collections.NewMap[string, types.Secret](sb, types.SecretPrefix, "secrets", collections.StringKey, codec.CollValue[types.Secret](cdc)),

		SecretAcceptedCounts: collections.NewMap(
			sb,
			collections.NewPrefix(string(types.SecretAcceptedCountsPrefix)),
			"secret_accepted_counts",
			collections.StringKey,
			collections.Int64Value,
		),

		SecretShares: collections.NewMap(
			sb,
			types.SecretSharesPrefix,
			"secret_shares",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.SecretShareData](cdc),
		),

		SecretAssignments: collections.NewMap(
			sb,
			types.SecretAssignmentsPrefix,
			"secret_assignments",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.AssignmentRecord](cdc),
		),

		SecretReveals: collections.NewMap(
			sb,
			types.SecretRevealsPrefix,
			"secret_reveals",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.RevealedShare](cdc),
		),

		SecretPayloads: collections.NewMap(
			sb,
			types.SecretPayloadsPrefix,
			"secret_payloads",
			collections.StringKey,
			collections.BytesValue,
		),

		Guardians: collections.NewMap[string, types.Guardian](
			sb,
			types.GuardianPrefix,
			"guardians",
			collections.StringKey,
			codec.CollValue[types.Guardian](cdc),
		),

		EarlyRevealSlash: collections.NewMap[string, bool](
			sb,
			collections.NewPrefix("early_reveal_slash"),
			"early_reveal_slash",
			collections.StringKey,
			collections.BoolValue,
		),

		GuardianKeyHistory: collections.NewMap(
			sb,
			types.GuardianKeyHistoryPrefix,
			"guardian_key_history",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
			codec.CollValue[types.KeyHistoryEntry](cdc),
		),

		GuardianKeyIndex: collections.NewMap(
			sb,
			types.GuardianKeyIndexPrefix,
			"guardian_key_index",
			collections.BytesKey,
			collections.StringValue,
		),

		CommitQueue: collections.NewKeySet(
			sb,
			types.CommitQueuePrefix,
			"commit_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),

		SettlementQueue: collections.NewKeySet(
			sb,
			types.SettlementQueuePrefix,
			"settlement_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),

		SecretsByCreator: collections.NewKeySet(
			sb,
			types.SecretsByCreatorPrefix,
			"secrets_by_creator",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),

		HintsByCreation: collections.NewMap(
			sb,
			types.HintsByCreationPrefix,
			"hints_by_creation",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
			codec.CollValue[types.DetectionHint](cdc),
		),

		SecretCounter: collections.NewSequence(
			sb,
			types.SecretCounterPrefix,
			"secret_counter",
		),

		SecretTombstones: collections.NewMap[string, types.SecretTombstone](
			sb,
			types.SecretTombstonesPrefix,
			"secret_tombstones",
			collections.StringKey,
			codec.CollValue[types.SecretTombstone](cdc),
		),

		PruneQueue: collections.NewKeySet(
			sb,
			types.PruneQueuePrefix,
			"prune_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),

		GuardianEligibility: collections.NewMap(
			sb,
			types.GuardianEligibilityPrefix,
			"guardian_eligibility",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
			codec.CollValue[types.GuardianEligibility](cdc),
		),

		RebateState: collections.NewItem(
			sb,
			types.RebateStatePrefix,
			"rebate_state",
			codec.CollValue[types.RebateState](cdc),
		),

		RebateExpiryQueue: collections.NewKeySet(
			sb,
			types.RebateExpiryQueuePrefix,
			"rebate_expiry_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),

		RebateCommitments: collections.NewMap(
			sb,
			types.RebateCommitmentsPrefix,
			"rebate_commitments",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.RebateCommitmentRecord](cdc),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

func (k Keeper) Logger() log.Logger {
	if k.logger == nil {
		// Return a no-op logger for tests
		return log.NewNopLogger()
	}
	return k.logger.With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetSecret retrieves a secret by ID
func (k Keeper) GetSecret(ctx context.Context, secretId string) (types.Secret, error) {
	secret, err := k.Secrets.Get(ctx, secretId)
	if err != nil {
		return secret, err
	}
	return k.withAcceptedCount(ctx, secret)
}

// withAcceptedCount joins the side-stored acceptance tally onto a secret read
// straight from the store, so every caller sees a complete record regardless of
// which read path it took. A missing entry is zero, not an error: secrets are
// created before anyone accepts.
func (k Keeper) withAcceptedCount(ctx context.Context, secret types.Secret) (types.Secret, error) {
	count, err := k.SecretAcceptedCounts.Get(ctx, secret.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			secret.AcceptedCount = 0
			return secret, nil
		}
		return secret, err
	}
	secret.AcceptedCount = count
	return secret, nil
}

// SetSecret stores a secret, splitting the acceptance tally into its own record.
//
// The tally is zeroed in the stored blob so there is exactly one source of
// truth for it; GetSecret joins it back. Writing both keeps ordinary
// read-modify-write callers correct without them knowing about the split —
// only the acceptance hot path (IncrementAcceptedCount) skips the blob.
func (k Keeper) SetSecret(ctx context.Context, secret types.Secret) error {
	count := secret.AcceptedCount
	secret.AcceptedCount = 0
	if err := k.Secrets.Set(ctx, secret.Id, secret); err != nil {
		return err
	}
	return k.SecretAcceptedCounts.Set(ctx, secret.Id, count)
}

// IncrementAcceptedCount records one acceptance and returns the new tally.
//
// This is the whole point of the split: an acceptance writes ~10 bytes instead
// of rewriting a secret record that carries one address and one frozen bond per
// selected guardian. Before, a 32-guardian accept burned ~94,000 gas on that
// rewrite alone — more than the protocol reimburses for the entire transaction.
func (k Keeper) IncrementAcceptedCount(ctx context.Context, secretId string) (int64, error) {
	count, err := k.SecretAcceptedCounts.Get(ctx, secretId)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return 0, err
	}
	count++
	if err := k.SecretAcceptedCounts.Set(ctx, secretId, count); err != nil {
		return 0, err
	}
	return count, nil
}

// HasSecret checks if a secret with the given ID exists
func (k Keeper) HasSecret(ctx context.Context, secretId string) bool {
	has, err := k.Secrets.Has(ctx, secretId)
	if err != nil {
		return false
	}
	return has
}

// IndexSecretCreation writes the discovery-scan feed entry for a secret.
// Called once at secret creation (and genesis import) — the hint is immutable
// so the entry is never touched again.
func (k Keeper) IndexSecretCreation(ctx context.Context, secret types.Secret) error {
	return k.HintsByCreation.Set(ctx, collections.Join(secret.CreatedAt, secret.Id), secret.DetectionHint)
}

// IndexSecretCreator records a secret in the creator index. Called once at
// secret creation (and genesis import) — never on updates, so ordinary state
// changes pay nothing for the index.
func (k Keeper) IndexSecretCreator(ctx context.Context, secret types.Secret) error {
	return k.SecretsByCreator.Set(ctx, collections.Join(secret.Creator, secret.Id))
}

// GetSecretsByUser retrieves all secrets published by a user via the creator
// index — cost scales with the caller's own secrets, not the whole store.
func (k Keeper) GetSecretsByUser(ctx context.Context, creator string) ([]types.Secret, error) {
	var secrets []types.Secret

	rng := collections.NewPrefixedPairRange[string, string](creator)
	err := k.SecretsByCreator.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		secret, err := k.Secrets.Get(ctx, key.K2())
		if err != nil {
			return true, fmt.Errorf("creator index references missing secret %s: %w", key.K2(), err)
		}
		secret, err = k.withAcceptedCount(ctx, secret)
		if err != nil {
			return true, fmt.Errorf("failed to read acceptance tally for %s: %w", key.K2(), err)
		}
		secrets = append(secrets, secret)
		return false, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk secrets by user %s: %w", creator, err)
	}

	return secrets, nil
}

// secretIdNamespace is the fixed UUIDv5 namespace for protocol-assigned
// secret IDs (itself a v5 UUID over a protocol-constant name, so it is the
// same on every node and never changes).
var secretIdNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("secrets.timeflare"))

// NextSecretId consumes the next value of the monotonic secret counter and
// derives the protocol-assigned secret ID from it. The counter is read in
// consensus transaction order, so every validator assigns the same value to
// the same request, the value is unique for the chain's lifetime, and the
// creator has no control over it. The user-facing ID keeps the UUID string
// shape via UUIDv5(namespace, chainID ‖ counter) — chainID folds in global
// uniqueness and the hash avoids leaking a clean running total.
func (k Keeper) NextSecretId(ctx context.Context) (string, uint64, error) {
	counter, err := k.SecretCounter.Next(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to advance secret counter: %w", err)
	}
	chainID := sdk.UnwrapSDKContext(ctx).ChainID()
	return DeriveSecretId(chainID, counter), counter, nil
}

// DeriveSecretId maps (chainID, counter) to the canonical secret ID:
// UUIDv5(secretIdNamespace, chainID ‖ uint64_be(counter)).
func DeriveSecretId(chainID string, counter uint64) string {
	name := make([]byte, 0, len(chainID)+8)
	name = append(name, []byte(chainID)...)
	name = binary.BigEndian.AppendUint64(name, counter)
	return uuid.NewSHA1(secretIdNamespace, name).String()
}

// GetSecretCreator returns the creator address for a given secret ID
// This is used by the guardians module to identify the secret creator for slashing distribution
func (k Keeper) GetSecretCreator(ctx context.Context, secretID string) (sdk.AccAddress, error) {
	secret, err := k.GetSecret(ctx, secretID)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", secretID, err)
	}

	creator, err := k.addressCodec.StringToBytes(secret.Creator)
	if err != nil {
		return nil, fmt.Errorf("failed to decode creator address %s: %w", secret.Creator, err)
	}

	return creator, nil
}

// GetShareData returns one guardian's encrypted share record for a secret —
// the single point-read used for HMAC verification at reveal and slash time.
func (k Keeper) GetShareData(ctx context.Context, secretID, guardianAddress string) (types.SecretShareData, error) {
	share, err := k.SecretShares.Get(ctx, collections.Join(secretID, guardianAddress))
	if err != nil {
		return types.SecretShareData{}, fmt.Errorf("guardian %s not assigned to secret %s", guardianAddress, secretID)
	}
	return share, nil
}

// GetAssignment returns one guardian's assignment status record for a secret.
func (k Keeper) GetAssignment(ctx context.Context, secretID, guardianAddress string) (types.AssignmentRecord, error) {
	return k.SecretAssignments.Get(ctx, collections.Join(secretID, guardianAddress))
}

// SetAssignment stores one guardian's assignment status record.
func (k Keeper) SetAssignment(ctx context.Context, secretID, guardianAddress string, record types.AssignmentRecord) error {
	return k.SecretAssignments.Set(ctx, collections.Join(secretID, guardianAddress), record)
}

// WalkAssignments iterates a secret's assignment status records (tiny; no
// share bytes) in guardian-address order.
func (k Keeper) WalkAssignments(ctx context.Context, secretID string, fn func(guardianAddress string, record types.AssignmentRecord) (stop bool, err error)) error {
	rng := collections.NewPrefixedPairRange[string, string](secretID)
	return k.SecretAssignments.Walk(ctx, rng, func(key collections.Pair[string, string], record types.AssignmentRecord) (bool, error) {
		return fn(key.K2(), record)
	})
}

// AcceptedGuardians returns the addresses with an ACCEPTED assignment on the
// secret, in deterministic (address) order. The roster finalises at the
// commit deadline with exactly the accepted set, so on any activated secret
// this is also the active guardian set.
func (k Keeper) AcceptedGuardians(ctx context.Context, secretID string) ([]string, error) {
	var accepted []string
	err := k.WalkAssignments(ctx, secretID, func(guardianAddress string, record types.AssignmentRecord) (bool, error) {
		if record.Status == types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
			accepted = append(accepted, guardianAddress)
		}
		return false, nil
	})
	return accepted, err
}

// RevealsForSecret returns a secret's revealed shares in deterministic
// (guardian address) order.
func (k Keeper) RevealsForSecret(ctx context.Context, secretID string) ([]types.RevealedShare, error) {
	var reveals []types.RevealedShare
	rng := collections.NewPrefixedPairRange[string, string](secretID)
	err := k.SecretReveals.Walk(ctx, rng, func(key collections.Pair[string, string], reveal types.RevealedShare) (bool, error) {
		reveals = append(reveals, reveal)
		return false, nil
	})
	return reveals, err
}

// Guardian management methods

// GetGuardian retrieves a guardian by address
func (k Keeper) GetGuardian(ctx context.Context, address string) (types.Guardian, bool) {
	guardian, err := k.Guardians.Get(ctx, address)
	if err != nil {
		return types.Guardian{}, false
	}
	return guardian, true
}

// SetGuardian stores a guardian and keeps the selection eligibility index in
// step. Every guardian write in the module funnels through here — that is what
// makes the index safe to rely on (see setGuardianEligibility).
func (k Keeper) SetGuardian(ctx context.Context, guardian types.Guardian) error {
	// Index first: it reads the PREVIOUS record to retire a stale key, so it has
	// to run before the record is overwritten.
	if err := k.setGuardianEligibility(ctx, guardian); err != nil {
		return err
	}
	return k.Guardians.Set(ctx, guardian.Address, guardian)
}

// HasGuardianRevealed checks if a specific guardian has already revealed their share for a secret
func (k Keeper) HasGuardianRevealed(ctx context.Context, secretID, guardianAddress string) bool {
	has, err := k.SecretReveals.Has(ctx, collections.Join(secretID, guardianAddress))
	if err != nil {
		return false
	}
	return has
}

// IsGuardianActive checks if a guardian is currently active and available.
//
// There is no float floor here: capital eligibility is per secret (a guardian
// is a selection candidate only while its UNLOCKED float covers that secret's
// bond, and MsgGuardianConfirmShares hard-rejects when it does not). Activity is purely
// registration + availability window.
//
// For callers that already hold the record, use guardianActiveAt instead — this
// one reads it back from the store, which is pure waste when the caller was
// handed it (see GetActiveGuardians).
func (k Keeper) IsGuardianActive(ctx context.Context, address string) bool {
	// Early validation - empty address check
	if address == "" {
		return false
	}

	guardian, err := k.Guardians.Get(ctx, address)
	if err != nil {
		return false
	}

	return guardianActiveAt(guardian, sdk.UnwrapSDKContext(ctx).BlockHeight())
}

// guardianActiveAt is the availability-window predicate on a record already in
// hand: registered, and height within [available_from, available_until].
func guardianActiveAt(guardian types.Guardian, height int64) bool {
	return height >= guardian.AvailableFrom && height <= guardian.AvailableUntil
}

// VerifyShareHMAC verifies an HMAC for a secret share using the crypto package
func (k Keeper) VerifyShareHMAC(secretID, guardianAddress string, revealedShare []byte, storedHMAC []byte) bool {
	return crypto.VerifyHMAC(secretID, guardianAddress, revealedShare, storedHMAC)
}

// ComputeTestHMAC computes an HMAC for testing purposes using the crypto package.
// This ensures tests use the same algorithm as production verification.
func (k Keeper) ComputeTestHMAC(secretID, guardianAddress string, revealedShare []byte) ([]byte, error) {
	return crypto.GenerateHMAC(secretID, guardianAddress, revealedShare)
}

// GetActiveGuardians is deliberately absent. It walked the whole guardian
// collection to build the candidate list, and candidate enumeration now range-
// reads the eligibility index instead (EligibleCandidatesFor). With no callers
// left it was deleted rather than kept: an unused full-registry walk is an
// invitation to reintroduce the cost this module went to some trouble to remove,
// exactly as GetGuardiansWithMinStake and GetGuardiansByAvailabilityWindow were
// deleted before it. Anything needing every guardian should say why it is not
// consensus-hot — genesis export and the invariant sweep both do.

// addRevealedShare handles the core logic of recording a revealed share and
// checking for the reconstruction threshold. This shared logic is used by both
// GuardianRevealShare (user transactions) and AutoGuardianRevealShare (internal slashing).
// The reveal is one write to the reveal store; the slim secret carries only
// the denormalised counter (and the state, on the threshold-crossing reveal).
func (k Keeper) addRevealedShare(ctx context.Context, secret types.Secret, guardianAddress string, decryptedShare []byte) (types.Secret, bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if this guardian has already revealed their share
	if k.HasGuardianRevealed(ctx, secret.Id, guardianAddress) {
		return secret, false, fmt.Errorf("guardian %s already revealed share for secret %s", guardianAddress, secret.Id)
	}

	revealedShare := types.RevealedShare{
		GuardianAddress: guardianAddress,
		DecryptedShare:  decryptedShare,
		RevealedAtBlock: sdkCtx.BlockHeight(),
	}
	if err := k.SecretReveals.Set(ctx, collections.Join(secret.Id, guardianAddress), revealedShare); err != nil {
		return secret, false, fmt.Errorf("failed to record revealed share: %w", err)
	}

	secret.RevealedCount++

	// Check if we have enough shares for reconstruction
	reconstructionComplete := secret.RevealedCount >= secret.Threshold
	if reconstructionComplete && secret.State == types.SECRET_STATUS_PENDING {
		// Threshold met - transition to reconstructable state (reveal window
		// still open); the FSM write persists the counter update too
		if err := k.TransitionSecretState(ctx, &secret, EventThresholdReached); err != nil {
			return secret, false, fmt.Errorf("failed to transition secret to reconstructable state: %w", err)
		}

		sdkCtx.Logger().Info("Secret reconstruction threshold met",
			"secret_id", secret.Id,
			"shares_revealed", secret.RevealedCount,
			"threshold", secret.Threshold,
		)
	} else {
		if err := k.SetSecret(ctx, secret); err != nil {
			return secret, false, fmt.Errorf("failed to update secret: %w", err)
		}
	}

	return secret, reconstructionComplete, nil
}

// AutoGuardianRevealShare handles automatic share revelation during slashing without signer validation
func (k Keeper) AutoGuardianRevealShare(ctx context.Context, guardianAddress, secretID string, decryptedShare []byte) error {
	// Get the secret
	secret, err := k.GetSecret(ctx, secretID)
	if err != nil {
		return fmt.Errorf("secret not found: %s", secretID)
	}

	// Check if secret has been cancelled - no reveals allowed after cancellation
	if secret.State == types.SECRET_STATUS_CANCELLED {
		return fmt.Errorf("cannot reveal shares for cancelled secret")
	}

	// Check secret state - must be pending or reconstructable (acceptance complete, reveal window open)
	if secret.State != types.SECRET_STATUS_PENDING && secret.State != types.SECRET_STATUS_RECONSTRUCTABLE {
		return fmt.Errorf("secret is not in pending or reconstructable state: %s", secret.State)
	}

	// Basic validation of decrypted share format (a key-share envelope)
	if len(decryptedShare) == 0 {
		return fmt.Errorf("decrypted share cannot be empty")
	}
	if int64(len(decryptedShare)) > types.MaxRevealedKeyShareSize {
		return fmt.Errorf("decrypted share exceeds maximum size: %d bytes (limit: %d)",
			len(decryptedShare), types.MaxRevealedKeyShareSize)
	}

	// Check if guardian is assigned to this secret (single point-read)
	share, err := k.GetShareData(ctx, secretID, guardianAddress)
	if err != nil {
		return fmt.Errorf("guardian assignment not found for guardian %s", guardianAddress)
	}

	// Verify HMAC to ensure share authenticity (for slashing protection)
	if !k.VerifyShareHMAC(secretID, guardianAddress, decryptedShare, share.ShareHmac) {
		// HMAC verification failed - reject the operation
		return fmt.Errorf("HMAC verification failed - invalid share data")
	}

	// Use shared logic for adding the revealed share
	_, _, err = k.addRevealedShare(ctx, secret, guardianAddress, decryptedShare)
	return err
}
