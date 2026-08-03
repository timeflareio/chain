package keeper_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// ─────────────────────────────────────────────────────────────────────────────
// Terminal-secret retention (PENDING_TERMINAL_SECRET_RETENTION_PLAN §10.8)
// ─────────────────────────────────────────────────────────────────────────────

// TestRetention_GoldenCanonicalEncoding pins the canonical TerminalSecretRecord
// encoding: a fixed record must marshal to exactly these bytes and this digest,
// so an encoder change can never silently re-digest history. If this test
// fails, the encoder changed — that is a consensus-visible event requiring a
// deliberate migration decision, never a test update in passing.
func TestRetention_GoldenCanonicalEncoding(t *testing.T) {
	record := types.TerminalSecretRecord{
		Secret: types.Secret{
			Id:               "9d2c1c50-0000-5000-8000-000000000001",
			Creator:          "tmflr1golden_creator",
			Threshold:        2,
			MinShares:        3,
			MaxShares:        4,
			State:            types.SECRET_STATUS_REVEALED,
			CreatedAt:        100,
			CommitDeadline:   210,
			RevealStartBlock: 400,
			RevealEndBlock:   500,
			RewardPool:       sdk.NewInt64Coin(types.DefaultDenom, 129_600),
			// A = max_shares × F_accept, escrowed apart from the pool
			AcceptFees: sdk.NewInt64Coin(types.DefaultDenom, 48_000),
			// Per-guardian frozen bonds (dynamic bond economics, July 2026) —
			// the field-20 flat bond is retired from the wire shape
			GuardianBondAmounts: []int64{1_404, 1_404, 1_404},
			Bump:                types.MinBump,
			AcceptedCount:       3,
			RevealedCount:       2,
			TerminalAt:          501,
			SecretCommitment:    []byte("golden_commitment_32_bytes______"),
			SecretPublicKey:     []byte("golden_pk_s_32_bytes____________"),
		},
		Reveals: []types.RevealedShare{
			{GuardianAddress: "tmflr1golden_guardian_a", DecryptedShare: []byte("share_a"), RevealedAtBlock: 410},
			{GuardianAddress: "tmflr1golden_guardian_b", DecryptedShare: []byte("share_b"), RevealedAtBlock: 420},
		},
		PayloadDigest: []byte("golden_payload_digest_32_bytes__"),
	}

	digest, err := keeper.TerminalRecordDigest(record)
	require.NoError(t, err)

	// The pinned golden digest. Recompute ONLY on a deliberate,
	// consensus-coordinated change to the canonical encoding.
	// Recomputed July 2026 (pre-launch, no history to re-digest): the
	// variable-quorum change retired Secret fields 8 (buffered shares) and
	// 18 (requested_shares) and added fields 27/28 (min_shares/max_shares) —
	// see docs/planning/done/DONE_VARIABLE_QUORUM_PLAN.md.
	// Recomputed again July 2026 (still pre-launch): guardian cost recovery
	// added field 29 (accept_fees), the acceptance reimbursement escrowed
	// apart from the pool — see
	// docs/planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md.
	const golden = "0e223fc4d195864ead9feefc74f32f73834be244aabe09ce80564b6da6e24ea9"
	require.Equal(t, golden, hex.EncodeToString(digest),
		"canonical encoding changed — this re-digests history and must be a deliberate migration, not a drive-by")

	// Determinism: marshalling twice yields identical bytes
	bz1, err := record.Marshal()
	require.NoError(t, err)
	bz2, err := record.Marshal()
	require.NoError(t, err)
	require.Equal(t, bz1, bz2)
}

// TestRetention_FullLifecycle drives a secret through the real lifecycle to
// settlement, then asserts both retention stages exactly:
//
//	Stage 1 (at terminal): share + assignment + slash-mark stores empty;
//	  reveal records and payload ciphertext retained; prune entry enqueued.
//	Stage 2 (terminal_at + RetentionBlocks): seven-field tombstone written
//	  with the digest of the canonical record; archival event carries the
//	  full record; zero residual entries in every secret-keyed store.
func TestRetention_FullLifecycle(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, 3)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	guardians := make([]string, 3)
	for i := int64(0); i < 3; i++ {
		guardians[i] = secret.SelectedGuardians[i]
		require.NoError(t, acceptAs(t, f, msgServer, secretId, guardians[i]))
	}
	finaliseCommitDeadline(t, f, secretId)
	secret, _ = f.keeper.GetSecret(f.ctx, secretId)

	setHeight(f, secret.RevealStartBlock)
	for _, g := range guardians {
		require.NoError(t, conformanceReveal(t, f, msgServer, secretId, g))
	}
	setHeight(f, secret.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// ── Stage 1 assertions ────────────────────────────────────────────────
	terminal, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_REVEALED, terminal.State)
	require.Equal(t, secret.RevealEndBlock+1, terminal.TerminalAt)

	rng := collections.NewPrefixedPairRange[string, string](secretId)
	requireEmptyPair := func(m collections.Map[collections.Pair[string, string], types.SecretShareData], what string) {
		count := 0
		require.NoError(t, m.Walk(f.ctx, rng, func(collections.Pair[string, string], types.SecretShareData) (bool, error) {
			count++
			return false, nil
		}))
		require.Zero(t, count, "stage 1 must delete every %s record", what)
	}
	requireEmptyPair(f.keeper.SecretShares, "share")
	assignCount := 0
	require.NoError(t, f.keeper.SecretAssignments.Walk(f.ctx, rng, func(collections.Pair[string, string], types.AssignmentRecord) (bool, error) {
		assignCount++
		return false, nil
	}))
	require.Zero(t, assignCount, "stage 1 must delete every assignment record")

	// Reconstruction inputs deliberately survive stage 1
	require.Len(t, revealsFor(t, f, secretId), 3, "reveal records must survive stage 1")
	hasPayload, err := f.keeper.SecretPayloads.Has(f.ctx, secretId)
	require.NoError(t, err)
	require.True(t, hasPayload, "payload ciphertext must survive stage 1")

	// Prune entry enqueued at terminal_at + RetentionBlocks
	dueHeight := terminal.TerminalAt + types.RetentionBlocksValue()
	hasEntry, err := f.keeper.PruneQueue.Has(f.ctx, collections.Join(dueHeight, secretId))
	require.NoError(t, err)
	require.True(t, hasEntry, "stage 2 prune must be scheduled at terminal_at + RetentionBlocks")

	assertInvariants(t, f)
	assertSolvency(t, f, bank)

	// Capture the canonical record before pruning to independently verify the
	// digest the chain writes
	expectedRecord, err := f.keeper.BuildTerminalRecord(f.ctx, terminal)
	require.NoError(t, err)
	expectedDigest, err := keeper.TerminalRecordDigest(expectedRecord)
	require.NoError(t, err)
	payload, err := f.keeper.SecretPayloads.Get(f.ctx, secretId)
	require.NoError(t, err)
	payloadDigest := sha256.Sum256(payload)
	require.Equal(t, payloadDigest[:], expectedRecord.PayloadDigest)
	require.Len(t, expectedRecord.Reveals, 3)

	// ── Stage 2 ───────────────────────────────────────────────────────────
	// One block early: nothing happens
	setHeight(f, dueHeight-1)
	require.NoError(t, f.keeper.ProcessDuePrunes(f.ctx))
	require.True(t, f.keeper.HasSecret(f.ctx, secretId), "must not prune before the retention window lapses")

	setHeight(f, dueHeight)
	sdk.UnwrapSDKContext(f.ctx).EventManager().Events() // (reset noise: read side only)
	require.NoError(t, f.keeper.ProcessDuePrunes(f.ctx))

	// The seven-field tombstone, exactly as agreed
	tombstone, err := f.keeper.SecretTombstones.Get(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, expectedDigest, tombstone.RecordDigest, "tombstone digest must match the canonical record")
	require.Equal(t, types.SECRET_STATUS_REVEALED, tombstone.FinalState)
	require.Equal(t, terminal.TerminalAt, tombstone.TerminalAt)
	require.Equal(t, dueHeight, tombstone.PrunedAt)
	require.Equal(t, terminal.Creator, tombstone.Creator)
	require.Equal(t, terminal.CreatedAt, tombstone.CreatedAt)
	require.Equal(t, terminal.SecretCommitment, tombstone.SecretCommitment)

	// The archival event carries the full canonical record — the load-bearing
	// recovery path once state is gone. Pin its contents exactly.
	var pruneEvent *sdk.Event
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type == types.EventTypeSecretPruned {
			e := ev
			pruneEvent = &e
		}
	}
	require.NotNil(t, pruneEvent, "stage 2 must emit the archival event")
	attrs := map[string]string{}
	for _, a := range pruneEvent.Attributes {
		attrs[a.Key] = a.Value
	}
	require.Equal(t, secretId, attrs[types.AttributeKeySecretId])
	require.Equal(t, hex.EncodeToString(expectedDigest), attrs["record_digest"])
	require.Equal(t, types.SECRET_STATUS_REVEALED, attrs["final_state"])
	archived, err := base64.StdEncoding.DecodeString(attrs["canonical_record"])
	require.NoError(t, err)
	rehash := sha256.Sum256(archived)
	require.Equal(t, expectedDigest, rehash[:],
		"the archived canonical record must hash to the tombstone digest — the self-authenticating archive property")
	var roundTrip types.TerminalSecretRecord
	require.NoError(t, roundTrip.Unmarshal(archived))
	require.Equal(t, expectedRecord, roundTrip, "archived record must decode to the exact canonical record")

	// Zero residual entries in every secret-keyed store
	assertFullyPruned(t, f, secretId, terminal)

	assertInvariants(t, f)
}

// TestRetention_PruneCapCarryOver proves a same-height burst larger than
// MaxPrunesPerBlock carries over to the next block instead of stretching
// EndBlock.
func TestRetention_PruneCapCarryOver(t *testing.T) {
	f := initFixture(t)
	setHeight(f, 100)

	total := types.MaxPrunesPerBlock + 7
	dueHeight := int64(100_000)
	for i := 0; i < total; i++ {
		secret := types.Secret{
			Id:         types.GenerateValidSecretID(),
			Creator:    sdk.AccAddress([]byte("prune_cap_creator___")).String(),
			State:      types.SECRET_STATUS_CANCELLED,
			CreatedAt:  50,
			TerminalAt: dueHeight - types.RetentionBlocksValue(),
			RewardPool: sdk.NewInt64Coin(types.DefaultDenom, 0),
		}
		require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
		require.NoError(t, f.keeper.IndexSecretCreator(f.ctx, secret))
		require.NoError(t, f.keeper.IndexSecretCreation(f.ctx, secret))
		enqueueForState(t, f, secret)
	}

	setHeight(f, dueHeight)
	require.NoError(t, f.keeper.ProcessDuePrunes(f.ctx))

	remaining := 0
	require.NoError(t, f.keeper.PruneQueue.Walk(f.ctx, nil, func(collections.Pair[int64, string]) (bool, error) {
		remaining++
		return false, nil
	}))
	require.Equal(t, 7, remaining, "the cap must leave the excess enqueued")

	tombstones := 0
	require.NoError(t, f.keeper.SecretTombstones.Walk(f.ctx, nil, func(string, types.SecretTombstone) (bool, error) {
		tombstones++
		return false, nil
	}))
	require.Equal(t, types.MaxPrunesPerBlock, tombstones)

	// Next block drains the carry-over (the <=-drain)
	setHeight(f, dueHeight+1)
	require.NoError(t, f.keeper.ProcessDuePrunes(f.ctx))
	remaining = 0
	require.NoError(t, f.keeper.PruneQueue.Walk(f.ctx, nil, func(collections.Pair[int64, string]) (bool, error) {
		remaining++
		return false, nil
	}))
	require.Zero(t, remaining)
	assertInvariants(t, f)
}

// TestRetention_GenesisRoundTrip proves tombstones survive export/import and
// that a terminal-but-unpruned secret's prune entry is rebuilt (the queue is
// derived state).
func TestRetention_GenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	setHeight(f, 100)

	// A pruned secret's tombstone
	prunedId := types.GenerateValidSecretID()
	tombstone := types.SecretTombstone{
		RecordDigest:     []byte("digest_32_bytes_________________"),
		FinalState:       types.SECRET_STATUS_REVEALED,
		TerminalAt:       500,
		PrunedAt:         500 + types.RetentionBlocksValue(),
		Creator:          sdk.AccAddress([]byte("genesis_creator_____")).String(),
		CreatedAt:        100,
		SecretCommitment: []byte("commitment_32_bytes_____________"),
	}
	require.NoError(t, f.keeper.SecretTombstones.Set(f.ctx, prunedId, tombstone))

	// A terminal-but-unpruned secret
	terminalId := types.GenerateValidSecretID()
	terminalSecret := types.Secret{
		Id:         terminalId,
		Creator:    sdk.AccAddress([]byte("genesis_creator_____")).String(),
		State:      types.SECRET_STATUS_FAILED,
		CreatedAt:  200,
		TerminalAt: 400,
		RewardPool: sdk.NewInt64Coin(types.DefaultDenom, 0),
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, terminalSecret))

	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.SecretTombstones, 1)
	require.Equal(t, prunedId, exported.SecretTombstones[0].SecretId)

	// Import into a fresh fixture
	f2 := initFixture(t)
	setHeight(f2, 100)
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, exported))

	imported, err := f2.keeper.SecretTombstones.Get(f2.ctx, prunedId)
	require.NoError(t, err)
	require.Equal(t, tombstone, imported)

	hasPrune, err := f2.keeper.PruneQueue.Has(f2.ctx,
		collections.Join(terminalSecret.TerminalAt+types.RetentionBlocksValue(), terminalId))
	require.NoError(t, err)
	require.True(t, hasPrune, "the prune entry must be rebuilt on import (derived state)")
}

// TestRetention_QueryTombstone covers the new query: NotFound before pruning
// and for never-existent ids, the tombstone after.
func TestRetention_QueryTombstone(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.SecretTombstone(f.ctx, &types.QuerySecretTombstoneRequest{SecretId: types.GenerateValidSecretID()})
	require.Error(t, err, "never-existent id must be NotFound")

	id := types.GenerateValidSecretID()
	tombstone := types.SecretTombstone{RecordDigest: []byte("d"), FinalState: types.SECRET_STATUS_CANCELLED}
	require.NoError(t, f.keeper.SecretTombstones.Set(f.ctx, id, tombstone))

	resp, err := qs.SecretTombstone(f.ctx, &types.QuerySecretTombstoneRequest{SecretId: id})
	require.NoError(t, err)
	require.Equal(t, tombstone, resp.Tombstone)
}

// assertFullyPruned sweeps every secret-keyed store for residual entries —
// the plan's conformance requirement: pruned secret ⇒ tombstone exists ∧
// nothing else remains anywhere.
func assertFullyPruned(t *testing.T, f *fixture, secretId string, secret types.Secret) {
	t.Helper()

	require.False(t, f.keeper.HasSecret(f.ctx, secretId), "slim record must be gone")

	has, err := f.keeper.SecretTombstones.Has(f.ctx, secretId)
	require.NoError(t, err)
	require.True(t, has, "the tombstone must exist")

	rng := collections.NewPrefixedPairRange[string, string](secretId)
	count := 0
	require.NoError(t, f.keeper.SecretShares.Walk(f.ctx, rng, func(collections.Pair[string, string], types.SecretShareData) (bool, error) {
		count++
		return false, nil
	}))
	require.NoError(t, f.keeper.SecretAssignments.Walk(f.ctx, rng, func(collections.Pair[string, string], types.AssignmentRecord) (bool, error) {
		count++
		return false, nil
	}))
	require.NoError(t, f.keeper.SecretReveals.Walk(f.ctx, rng, func(collections.Pair[string, string], types.RevealedShare) (bool, error) {
		count++
		return false, nil
	}))
	require.Zero(t, count, "no share/assignment/reveal residue")

	hasPayload, err := f.keeper.SecretPayloads.Has(f.ctx, secretId)
	require.NoError(t, err)
	require.False(t, hasPayload, "payload ciphertext must be gone")

	hasCreatorIdx, err := f.keeper.SecretsByCreator.Has(f.ctx, collections.Join(secret.Creator, secretId))
	require.NoError(t, err)
	require.False(t, hasCreatorIdx, "creator-index entry must be gone")

	hasHint, err := f.keeper.HintsByCreation.Has(f.ctx, collections.Join(secret.CreatedAt, secretId))
	require.NoError(t, err)
	require.False(t, hasHint, "hint-feed entry must be gone")

	slashKey := fmt.Sprintf("%s:", secretId)
	require.NoError(t, f.keeper.EarlyRevealSlash.Walk(f.ctx, nil, func(key string, _ bool) (bool, error) {
		require.NotContains(t, key, slashKey, "no slash-mark residue")
		return false, nil
	}))

	require.NoError(t, f.keeper.PruneQueue.Walk(f.ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		require.NotEqual(t, secretId, key.K2(), "no prune-queue residue")
		return false, nil
	}))
	require.NoError(t, f.keeper.CommitQueue.Walk(f.ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		require.NotEqual(t, secretId, key.K2(), "no commit-queue residue")
		return false, nil
	}))
	require.NoError(t, f.keeper.SettlementQueue.Walk(f.ctx, nil, func(key collections.Pair[int64, string]) (bool, error) {
		require.NotEqual(t, secretId, key.K2(), "no settlement-queue residue")
		return false, nil
	}))
}
