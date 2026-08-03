package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Settlement quarantine coverage (settlement & state-integrity plan, ruled
// July 2026): a bank-level fault during EndBlock settlement or commit expiry
// must commit NO partial state (per-secret cache-commit), retain the queue
// entry for retry, raise the settlement_stalled alarm on every attempt, keep
// the invariants intact throughout, and settle cleanly once the fault clears.

// faultInjectingBankKeeper wraps the tracking keeper and, while failing is
// set, rejects every module→account send and burn — modelling the
// "unknown bug" class the quarantine defends against.
type faultInjectingBankKeeper struct {
	*trackingBankKeeper
	failing bool
}

func newFaultInjectingBankKeeper() *faultInjectingBankKeeper {
	return &faultInjectingBankKeeper{trackingBankKeeper: newTrackingBankKeeper()}
}

func (fb *faultInjectingBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	if fb.failing {
		return errors.New("injected bank fault: send")
	}
	return fb.trackingBankKeeper.SendCoinsFromModuleToAccount(ctx, senderModule, recipientAddr, amt)
}

func (fb *faultInjectingBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	if fb.failing {
		return errors.New("injected bank fault: burn")
	}
	return fb.trackingBankKeeper.BurnCoins(ctx, moduleName, amt)
}

// stalledEvents returns the settlement_stalled events emitted so far for one
// secret, filtered by operation.
func stalledEvents(f *fixture, secretId, operation string) []sdk.Event {
	var out []sdk.Event
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type != types.EventSettlementStalled {
			continue
		}
		matchesSecret, matchesOp := false, false
		for _, attr := range ev.Attributes {
			if attr.Key == types.AttributeKeySecretId && attr.Value == secretId {
				matchesSecret = true
			}
			if attr.Key == "operation" && attr.Value == operation {
				matchesOp = true
			}
		}
		if matchesSecret && matchesOp {
			out = append(out, ev)
		}
	}
	return out
}

// eventsOfType returns every event of the given type emitted so far.
func eventsOfType(f *fixture, eventType string) []sdk.Event {
	var out []sdk.Event
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type == eventType {
			out = append(out, ev)
		}
	}
	return out
}

// TestSettlementStall_QuarantineAndRecovery drives a full settlement into an
// injected bank fault and proves the fail-safe model: no partial state, entry
// retained, alarm on every attempt, invariants intact, clean self-recovery.
func TestSettlementStall_QuarantineAndRecovery(t *testing.T) {
	bank := newFaultInjectingBankKeeper()
	f := initFixtureWithBank(t, bank)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_000)

	bond := testFloatUnit()
	pool := testFloatUnit() // any exactly-divisible-by-nothing pool works
	creator := sdk.AccAddress([]byte("stall_creator_______")).String()

	// 3 active guardians, 2 revealed → settlement pays 2, slashes 1
	secret, guardians := settlementSecret(t, f, 3, bond, pool, creator)
	markRevealed(t, f, secret.Id, guardians[0], guardians[1])
	settlementKey := collections.Join(secret.RevealEndBlock+1, secret.Id)

	// ── Fault active: the settlement must stall, not half-commit ─────────
	bank.failing = true
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx),
		"a stalled settlement quarantines the secret; it must never error (or panic) the EndBlock sweep")

	// No partial state: the secret is untouched and every bond is still locked
	stalled, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_PENDING, stalled.State, "no state transition may commit on a failed settlement")
	for _, g := range guardians {
		guardian, found := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, found)
		require.True(t, bond.Equal(guardian.LockedStake.Amount),
			"guardian %s bond must stay locked (not lost) while settlement is stalled", g)
	}

	// The events of the discarded attempt must not leak out of the cache
	require.Empty(t, eventsOfType(f, "guardian_bond_released"),
		"partial-settlement events must be discarded with the partial state")

	// The queue entry is retained and the alarm fired
	has, err := f.keeper.SettlementQueue.Has(f.ctx, settlementKey)
	require.NoError(t, err)
	require.True(t, has, "the settlement entry must be retained for retry")
	require.Len(t, stalledEvents(f, secret.Id, types.StalledOpSettlement), 1,
		"the settlement_stalled alarm must fire on the first failure")

	assertInvariants(t, f)

	// ── Next block, fault persists: retried, stalled again, alarm again ──
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_001)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))
	require.Len(t, stalledEvents(f, secret.Id, types.StalledOpSettlement), 2,
		"the alarm must fire on every failed attempt, not just the first")
	assertInvariants(t, f)

	// ── Fault cleared: the pending retry completes the settlement ────────
	bank.failing = false
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_002)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	settled, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_FAILED, settled.State,
		"2 of 3 revealed below threshold 3: cryptographic outcome is failed")

	// Exact amounts, as if the fault had never happened: each revealer paid
	// P/2 with its bond unlocked; the no-show slashed 40/10/50
	rewardEach := pool.QuoRaw(2)
	require.True(t, rewardEach.Equal(bank.received(guardians[0])))
	require.True(t, rewardEach.Equal(bank.received(guardians[1])))
	require.True(t, bond.MulRaw(types.NoRevealCreatorPercent).QuoRaw(100).Equal(bank.received(creator)),
		"creator receives exactly the 10%% no-show slice")

	has, err = f.keeper.SettlementQueue.Has(f.ctx, settlementKey)
	require.NoError(t, err)
	require.False(t, has, "the completed settlement must dequeue its entry")

	require.Len(t, stalledEvents(f, secret.Id, types.StalledOpSettlement), 2,
		"no alarm on the successful attempt")
	assertInvariants(t, f)
}

// TestCommitExpiryStall_QuarantineAndRecovery proves the same treatment on
// the commit-expiry path: the refund fault stalls the expiry wholesale (the
// secret never reaches FAILED unrefunded), and it completes once cleared.
func TestCommitExpiryStall_QuarantineAndRecovery(t *testing.T) {
	bank := newFaultInjectingBankKeeper()
	f := initFixtureWithBank(t, bank)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_000)

	bond := testFloatUnit()
	creator := sdk.AccAddress([]byte("stallc_creator______")).String()
	guardianAddr := sdk.AccAddress([]byte("stallc_guardian_____")).String()
	setupSlashableGuardian(t, f, guardianAddr, bond) // bond locked from a prior acceptance

	// AWAITING_ACCEPTANCE with an expired commit deadline and one accepted bond
	secret := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           2,
		MinShares:           2,
		MaxShares:           2,
		State:               types.SECRET_STATUS_AWAITING_ACCEPTANCE,
		CreatedAt:           9_000,
		CommitDeadline:      9_999, // due at 10_000
		RevealStartBlock:    11_000,
		RevealEndBlock:      12_000,
		RewardPool:          sdk.NewCoin(types.DefaultDenom, bond.MulRaw(3)),
		GuardianBondAmounts: repeatBond(bond, 1),
		Bump:                types.MinBump,
		SelectedGuardians:   []string{guardianAddr},
		AcceptedCount:       1,
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secret.Id)
	writeShareRecord(t, f, secret.Id, guardianAddr, []byte("encrypted_share_data"), nil)
	writeAssignmentRecord(t, f, secret.Id, guardianAddr, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 9_500)
	enqueueForState(t, f, secret)
	commitKey := collections.Join(secret.CommitDeadline+1, secret.Id)

	// ── Fault active: refund fails, the whole expiry is discarded ────────
	bank.failing = true
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

	stalled, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_AWAITING_ACCEPTANCE, stalled.State,
		"a commit that cannot refund must never reach FAILED")
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.True(t, found)
	require.True(t, bond.Equal(guardian.LockedStake.Amount), "the accepted bond must stay locked while stalled")

	has, err := f.keeper.CommitQueue.Has(f.ctx, commitKey)
	require.NoError(t, err)
	require.True(t, has, "the commit entry must be retained for retry")
	require.Len(t, stalledEvents(f, secret.Id, types.StalledOpCommitExpiry), 1)
	assertInvariants(t, f)

	// ── Fault cleared: the retry completes the no-fault exit ─────────────
	bank.failing = false
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_001)
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

	failed, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_FAILED, failed.State)
	require.True(t, secret.RewardPool.Amount.Equal(bank.received(creator)), "the pool must refund in full")

	guardian, found = f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.True(t, found)
	require.True(t, guardian.LockedStake.Amount.IsZero(), "the accepted bond must unlock in full")

	has, err = f.keeper.CommitQueue.Has(f.ctx, commitKey)
	require.NoError(t, err)
	require.False(t, has)
	require.Len(t, stalledEvents(f, secret.Id, types.StalledOpCommitExpiry), 1)
	assertInvariants(t, f)
}

// TestUserCancelSecret_BankFaultFailsWholeTransaction proves the message-path
// counterpart: a payout fault fails MsgUserCancelSecret outright (atomic abort
// via the transaction cache) instead of half-cancelling.
func TestUserCancelSecret_BankFaultFailsWholeTransaction(t *testing.T) {
	bank := newFaultInjectingBankKeeper()
	f := initFixtureWithBank(t, bank)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_000)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	bond := testFloatUnit()
	creator := sdk.AccAddress([]byte("stallx_creator______")).String()
	guardianAddr := sdk.AccAddress([]byte("stallx_guardian_____")).String()
	setupSlashableGuardian(t, f, guardianAddr, bond)

	secret := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           2,
		MinShares:           1,
		MaxShares:           2,
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           9_000,
		CommitDeadline:      9_500, // elapsed > 0: a pro-rata payout is due
		RevealStartBlock:    11_000,
		RevealEndBlock:      12_000,
		RewardPool:          sdk.NewCoin(types.DefaultDenom, bond.MulRaw(3)),
		GuardianBondAmounts: repeatBond(bond, 1),
		Bump:                types.MinBump,
		SelectedGuardians:   []string{guardianAddr},
		AcceptedCount:       1,
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	writeAssignmentRecord(t, f, secret.Id, guardianAddr, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 9_400)

	bank.failing = true
	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{SecretId: secret.Id, Creator: creator})
	require.Error(t, err, "a payout fault must fail the cancellation transaction wholesale")

	// The secret must not have been cancelled by the failed attempt
	after, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_PENDING, after.State)
}
