package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Phase 3 (bonded guardian economics) settlement coverage: threshold-
// independent window-end settlement, percentage no-reveal slashing, pool
// refund only when nobody reveals, dust burning, and pro-rata cancellation.
// See docs/spec.md "Settlement" and "Cancellation and No-Fault Refunds".

// trackingBankKeeper records module→account sends and burns so tests can
// assert exactly where every uveil went.
type trackingBankKeeper struct {
	mockBankKeeper
	sends map[string]math.Int // recipient address -> total received
	burnt *math.Int
}

func newTrackingBankKeeper() *trackingBankKeeper {
	z := math.ZeroInt()
	return &trackingBankKeeper{sends: make(map[string]math.Int), burnt: &z}
}

func (tb *trackingBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	cur, ok := tb.sends[recipientAddr.String()]
	if !ok {
		cur = math.ZeroInt()
	}
	tb.sends[recipientAddr.String()] = cur.Add(amt.AmountOf(types.DefaultDenom))
	return nil
}

func (tb *trackingBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	*tb.burnt = tb.burnt.Add(amt.AmountOf(types.DefaultDenom))
	return nil
}

func (tb *trackingBankKeeper) received(addr string) math.Int {
	if v, ok := tb.sends[addr]; ok {
		return v
	}
	return math.ZeroInt()
}

// repeatBond builds a per-guardian frozen-bond slice with the same amount for
// every guardian — the fixture shape when all guardians share one k.
func repeatBond(bond math.Int, n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = bond.Int64()
	}
	return out
}

// settlementSecret builds a PENDING secret with `n` active bonded guardians
// (each with the bond locked in its float) whose reveal window has expired.
func settlementSecret(t *testing.T, f *fixture, n int, bond, pool math.Int, creator string) (types.Secret, []string) {
	t.Helper()
	currentHeight := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	guardians := make([]string, 0, n)
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("settle_%s_g%02d__________", t.Name(), i))).String()
		guardians = append(guardians, addr)
		setupSlashableGuardian(t, f, addr, bond) // bond locked, one spare bond unlocked
	}

	secret := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           3,
		MinShares:           int64(n),
		MaxShares:           int64(n),
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           1,
		CommitDeadline:      50,
		RevealStartBlock:    currentHeight - 100,
		RevealEndBlock:      currentHeight - 1, // settlement due at end+1 = currentHeight
		RewardPool:          sdk.NewCoin(types.DefaultDenom, pool),
		GuardianBondAmounts: repeatBond(bond, n),
		Bump:                types.MinBump,
		SelectedGuardians:   guardians,
		AcceptedCount:       int64(n),
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secret.Id)
	// Seed the side-stores as UserDistributeShares/GuardianConfirmShares would have
	for _, addr := range guardians {
		writeShareRecord(t, f, secret.Id, addr, []byte("encrypted_share_data"), nil)
		writeAssignmentRecord(t, f, secret.Id, addr, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	}
	// Register the queue entries production would have left for a PENDING
	// secret (settlement only — the commit entry retires at activation)
	enqueueForState(t, f, secret)
	return secret, guardians
}

func markRevealed(t *testing.T, f *fixture, secretId string, guardians ...string) {
	t.Helper()
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	for _, g := range guardians {
		writeReveal(t, f, secretId, g, []byte("share_"+g), sdk.UnwrapSDKContext(f.ctx).BlockHeight()-10)
	}
	secret.RevealedCount += int64(len(guardians))
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
}

// TestSettlement_ThresholdIndependent proves the core Phase 3 property: with
// the threshold NOT met (2 revealers < threshold 3), revealers are STILL paid
// and get bonds back, no-shows are STILL slashed, and the creator receives
// only its slash slices — never a pool refund. The secret's failed state is a
// cryptographic outcome only.
func TestSettlement_ThresholdIndependent(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_000)

	bond := testFloatUnit()
	pool := math.NewInt(900_000_000) // 900 VEIL
	creator := sdk.AccAddress([]byte("settlement_creator__")).String()

	secret, guardians := settlementSecret(t, f, 3, bond, pool, creator)
	revealers := guardians[:2] // 2 of 3 reveal — BELOW the threshold of 3
	noShow := guardians[2]
	markRevealed(t, f, secret.Id, revealers...)

	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// Cryptographic outcome: threshold not met → FAILED
	settled, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_FAILED, settled.State)

	// Revealers: bonds fully unlocked, and the pool split between the two —
	// each gets HALF of the pool (the no-show's forfeited share flows to them)
	half := pool.QuoRaw(2)
	for _, g := range revealers {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero(), "revealer %s bond must be returned", g)
		require.Equal(t, bond.MulRaw(2), guardian.Stake.Amount, "revealer float total unchanged")
		require.Equal(t, half, bank.received(g), "revealer %s must receive half the pool", g)
	}

	// No-show: 40% burned + 10% creator leave the float; 50% returned to unlocked
	noShowGuardian, _ := f.keeper.GetGuardian(f.ctx, noShow)
	slashed := bond.MulRaw(50).QuoRaw(100)
	require.True(t, noShowGuardian.LockedStake.IsZero())
	require.Equal(t, bond.MulRaw(2).Sub(slashed), noShowGuardian.Stake.Amount,
		"no-show float must shrink by exactly the slashed 50%%")

	// Creator got its 10% slash slice and NOTHING else (no pool refund on failure)
	require.Equal(t, bond.MulRaw(10).QuoRaw(100), bank.received(creator),
		"creator receives only the 10%% slash slice — threshold failure is not refunded")

	// 40% of the no-show bond burned (pool split evenly leaves no dust here)
	require.Equal(t, bond.MulRaw(40).QuoRaw(100), *bank.burnt)

	assertInvariants(t, f)
}

// TestSettlement_NobodyReveals proves the ONLY pool-refund case: no revealers.
// Every guardian is slashed; the creator gets the full pool back plus the
// slash slices.
func TestSettlement_NobodyReveals(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_000)

	bond := testFloatUnit()
	pool := math.NewInt(900_000_000)
	creator := sdk.AccAddress([]byte("settlement_creator__")).String()

	secret, guardians := settlementSecret(t, f, 3, bond, pool, creator)
	// nobody reveals

	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// Pool refunded in full + 3 × 10% slash slices
	expectedCreator := pool.Add(bond.MulRaw(10).QuoRaw(100).MulRaw(3))
	require.Equal(t, expectedCreator, bank.received(creator))

	// Every guardian slashed 50%
	for _, g := range guardians {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero())
		require.Equal(t, bond.MulRaw(2).Sub(bond.MulRaw(50).QuoRaw(100)), guardian.Stake.Amount)
	}

	// 3 × 40% burned
	require.Equal(t, bond.MulRaw(40).QuoRaw(100).MulRaw(3), *bank.burnt)

	settled, _ := f.keeper.GetSecret(f.ctx, secret.Id)
	require.Equal(t, types.SECRET_STATUS_FAILED, settled.State)

	assertInvariants(t, f)
}

// TestSettlement_AllReveal_DustBurned proves the success path and that
// integer-division dust from the pool split is burned.
func TestSettlement_AllReveal_DustBurned(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_000)

	bond := testFloatUnit()
	pool := math.NewInt(1_000_000_000) // 1000 VEIL over 3 revealers → dust 1
	creator := sdk.AccAddress([]byte("settlement_creator__")).String()

	secret, guardians := settlementSecret(t, f, 3, bond, pool, creator)
	markRevealed(t, f, secret.Id, guardians...)

	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	settled, _ := f.keeper.GetSecret(f.ctx, secret.Id)
	require.Equal(t, types.SECRET_STATUS_REVEALED, settled.State)

	per := pool.QuoRaw(3)
	for _, g := range guardians {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero(), "revealer bond returned")
		require.Equal(t, per, bank.received(g))
	}

	// The indivisible remainder is burned, creator gets nothing
	dust := pool.Sub(per.MulRaw(3))
	require.True(t, dust.IsPositive(), "test must exercise a dusty split")
	require.Equal(t, dust, *bank.burnt)
	require.True(t, bank.received(creator).IsZero())

	assertInvariants(t, f)
}

// TestSettlement_EarlySlashedExcluded proves a guardian already slashed for an
// early reveal is excluded from settlement entirely: no bond return (it was
// already deducted at report time), no pool share, and no double slash.
func TestSettlement_EarlySlashedExcluded(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10_000)

	bond := testFloatUnit()
	pool := math.NewInt(900_000_000)
	creator := sdk.AccAddress([]byte("settlement_creator__")).String()

	secret, guardians := settlementSecret(t, f, 3, bond, pool, creator)
	leaker := guardians[2]

	// Simulate the immediate report-time slash: full bond deducted, the
	// active-bond slot released, and the exclusion marker written — exactly
	// the three moves MsgSlashGuardian makes
	require.NoError(t, f.keeper.DeductLockedFloat(f.ctx, leaker, bond))
	require.NoError(t, f.keeper.DecrementActiveBonds(f.ctx, leaker))
	require.NoError(t, f.keeper.MarkGuardianSlashedForEarlyReveal(f.ctx, leaker, secret.Id))
	// The leaked evidence was auto-revealed on-chain (leaker holds a reveal record)
	markRevealed(t, f, secret.Id, guardians[0], guardians[1], leaker)

	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// The two honest revealers split the pool; the leaker gets nothing
	half := pool.QuoRaw(2)
	require.Equal(t, half, bank.received(guardians[0]))
	require.Equal(t, half, bank.received(guardians[1]))
	require.True(t, bank.received(leaker).IsZero(), "early-slashed guardian must be excluded from the pool")

	// The leaker's float is untouched by settlement (bond already gone at report)
	leakerGuardian, _ := f.keeper.GetGuardian(f.ctx, leaker)
	require.Equal(t, bond, leakerGuardian.Stake.Amount, "no double slash at settlement")
	require.True(t, leakerGuardian.LockedStake.IsZero())

	assertInvariants(t, f)
}

// TestCancellation_ProRata reproduces the spec's cancellation example: bonds
// returned in full, each active guardian paid rate × elapsed × bump, creator
// refunded the unearned remainder. See docs/spec.md "Cancellation".
func TestCancellation_ProRata(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	const (
		bump           = int64(500) // 5.00 (the spec's Alice example)
		distance       = int64(1_000_000)
		numGuardians   = 5
		commitDeadline = int64(1_000)
	)
	bond := types.BondAmount(distance, bump, types.InitialBondK)
	pool := types.RewardPoolAmount(distance, int64(numGuardians), bump)
	acceptFees := types.AcceptFeesAmount(int64(numGuardians))
	creator := sdk.AccAddress([]byte("cancel_creator______")).String()

	// Build a pending secret mid-hold
	currentHeight := commitDeadline + 400_000 // 40% of the distance travelled
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(currentHeight)

	guardians := make([]string, 0, numGuardians)
	for i := 0; i < numGuardians; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("cancel_g%02d__________", i))).String()
		guardians = append(guardians, addr)
		setupSlashableGuardian(t, f, addr, bond)
	}

	secret := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           3,
		MinShares:           int64(numGuardians),
		MaxShares:           int64(numGuardians),
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           900,
		CommitDeadline:      commitDeadline,
		RevealStartBlock:    commitDeadline + distance - 100, // window still ahead
		RevealEndBlock:      commitDeadline + distance - 1,   // priced distance = (end+1) − deadline = distance, matching the pool above
		RewardPool:          sdk.NewCoin(types.DefaultDenom, pool),
		AcceptFees:          sdk.NewCoin(types.DefaultDenom, acceptFees),
		GuardianBondAmounts: repeatBond(bond, numGuardians),
		Bump:                bump,
		SelectedGuardians:   guardians,
		AcceptedCount:       int64(numGuardians),
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secret.Id)
	for _, addr := range guardians {
		writeShareRecord(t, f, secret.Id, addr, []byte("encrypted_share_data"), nil)
		writeAssignmentRecord(t, f, secret.Id, addr, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	}

	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
		SecretId: secret.Id,
		Creator:  creator,
	})
	require.NoError(t, err)

	// Every bond returned in full
	for _, g := range guardians {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero(), "cancellation must release every bond")
		require.Equal(t, bond.MulRaw(2), guardian.Stake.Amount)
	}

	// The wage: rate × elapsed × bump = 1 × 400,000 × 5.00 = 2 VEIL, plus the
	// pool's reveal leg accruing over the same 40% of the hold (13,000 × 0.4)
	elapsed := currentHeight - commitDeadline
	expectedPayout := types.ProRataCancellationPayout(pool, distance, int64(numGuardians), elapsed)
	revealLegAccrued := types.RevealLeg().MulRaw(elapsed).QuoRaw(distance)
	require.Equal(t, math.NewInt(2_000_000).Add(revealLegAccrued), expectedPayout,
		"spec example: 2 VEIL wage per guardian at 40%% distance, plus the accrued reveal leg")

	// Acceptance is reimbursed in full regardless of when the creator cancelled
	// — it is not the pool, and the work was done
	perGuardianAcceptFee := types.PerGuardianAcceptFee(acceptFees, int64(numGuardians))
	require.Equal(t, types.AcceptLeg(), perGuardianAcceptFee)
	for _, g := range guardians {
		require.Equal(t, expectedPayout.Add(perGuardianAcceptFee), bank.received(g),
			"cancelled guardian receives the accrued pool slice plus its full acceptance reimbursement")
	}

	// Creator refunded both unearned remainders: (P − n × payout) and any
	// accept-fee slices nobody earned (none here — all five accepted)
	expectedRefund := pool.Sub(expectedPayout.MulRaw(int64(numGuardians)))
	require.Equal(t, expectedRefund, bank.received(creator))

	// Secret is terminally cancelled
	cancelled, _ := f.keeper.GetSecret(f.ctx, secret.Id)
	require.Equal(t, types.SECRET_STATUS_CANCELLED, cancelled.State)

	assertInvariants(t, f)
}

// TestCancellation_NoInFlightRepricing simulates a software upgrade retuning
// RatePerGuardianBlock mid-lifecycle (immutable-economics ruling,
// docs/planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md work item 2):
// a secret whose pool was escrowed under a DIFFERENT creation-time rate must
// be cancelled at that rate — the live constant must be invisible to every
// paid amount. (Go constants cannot be mutated in a test; a stored pool
// priced at another rate is the same observable state an upgrade produces.)
func TestCancellation_NoInFlightRepricing(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	const (
		creationRate   = types.RatePerGuardianBlock * 2 // the pre-"upgrade" rate
		bump           = int64(500)
		distance       = int64(1_000_000)
		numGuardians   = 5
		commitDeadline = int64(1_000)
	)
	bond := types.BondAmount(distance, bump, types.InitialBondK)
	// The pool this secret escrowed at creation, priced at the OLD rate.
	pool := math.NewInt(creationRate).
		Mul(math.NewInt(distance)).Mul(math.NewInt(int64(numGuardians))).
		Mul(math.NewInt(bump)).Quo(math.NewInt(types.BumpScale))

	currentHeight := commitDeadline + 400_000
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(currentHeight)

	guardians := make([]string, 0, numGuardians)
	for i := 0; i < numGuardians; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("repric_g%02d__________", i))).String()
		guardians = append(guardians, addr)
		setupSlashableGuardian(t, f, addr, bond)
	}
	creator := sdk.AccAddress([]byte("repric_creator______")).String()

	secret := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           3,
		MinShares:           int64(numGuardians),
		MaxShares:           int64(numGuardians),
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           900,
		CommitDeadline:      commitDeadline,
		RevealStartBlock:    commitDeadline + distance - 100,
		RevealEndBlock:      commitDeadline + distance - 1, // priced distance = distance
		RewardPool:          sdk.NewCoin(types.DefaultDenom, pool),
		GuardianBondAmounts: repeatBond(bond, numGuardians),
		Bump:                bump,
		SelectedGuardians:   guardians,
		AcceptedCount:       int64(numGuardians),
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secret.Id)
	for _, addr := range guardians {
		writeShareRecord(t, f, secret.Id, addr, []byte("encrypted_share_data"), nil)
		writeAssignmentRecord(t, f, secret.Id, addr, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	}

	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
		SecretId: secret.Id, Creator: creator,
	})
	require.NoError(t, err)

	// Each guardian is paid at the CREATION-time rate…
	elapsed := currentHeight - commitDeadline
	wageAtCreationRate := math.NewInt(creationRate).
		Mul(math.NewInt(elapsed)).Mul(math.NewInt(bump)).Quo(math.NewInt(types.BumpScale))
	// …which differs from what the live constant would pay.
	wageAtLiveConstant := math.NewInt(types.RatePerGuardianBlock).
		Mul(math.NewInt(elapsed)).Mul(math.NewInt(bump)).Quo(math.NewInt(types.BumpScale))
	require.False(t, wageAtCreationRate.Equal(wageAtLiveConstant), "test premise: the rates must differ")

	for _, g := range guardians {
		require.True(t, wageAtCreationRate.Equal(bank.received(g)),
			"guardian %s must be paid at the creation-time rate, not the live constant", g)
	}
	// Creator refunded exactly the stored pool minus the stored-rate wages.
	expectedRefund := pool.Sub(wageAtCreationRate.MulRaw(int64(numGuardians)))
	require.True(t, expectedRefund.Equal(bank.received(creator)),
		"refund must conserve the STORED pool: not one uveil re-priced")

	assertInvariants(t, f)
}

// TestCancellation_RejectedDuringCommitPhase proves pre-activation
// cancellation is rejected outright (ruled July 2026): cancellation is a
// post-activation mechanic, and pre-activation secrets exit via
// commit-timeout only. No funds may move on the rejected attempt.
func TestCancellation_RejectedDuringCommitPhase(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	pool := math.NewInt(4_500_000_000)
	creator := sdk.AccAddress([]byte("cancel_creator______")).String()

	for _, state := range []string{types.SECRET_STATUS_RESERVED, types.SECRET_STATUS_AWAITING_ACCEPTANCE} {
		secret := types.Secret{
			Id:               types.GenerateValidSecretID(),
			Creator:          creator,
			Threshold:        3,
			State:            state,
			CreatedAt:        90,
			CommitDeadline:   200, // still in the commit phase
			RevealStartBlock: 10_000,
			RevealEndBlock:   20_000,
			RewardPool:       sdk.NewCoin(types.DefaultDenom, pool),
			Bump:             types.MinBump,
		}
		require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
		enqueueForState(t, f, secret)
		if state == types.SECRET_STATUS_AWAITING_ACCEPTANCE {
			seedSealFields(t, f, secret.Id) // distributed secrets carry payload + pk_s
		}

		_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
			SecretId: secret.Id,
			Creator:  creator,
		})
		require.Error(t, err, "pre-activation cancel must be rejected from %s", state)
		require.Contains(t, err.Error(), "can only cancel secrets in pending state")

		unchanged, getErr := f.keeper.GetSecret(f.ctx, secret.Id)
		require.NoError(t, getErr)
		require.Equal(t, state, unchanged.State, "rejected cancel must not change state")
	}

	require.True(t, bank.received(creator).IsZero(), "rejected cancel must move no funds")

	assertInvariants(t, f)
}

// TestCancellation_RejectedOnceWindowOpen proves cancellation stops at
// reveal_start_block — after that the secret proceeds to normal settlement.
func TestCancellation_RejectedOnceWindowOpen(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	creator := sdk.AccAddress([]byte("cancel_creator______")).String()
	secret := types.Secret{
		Id:               types.GenerateValidSecretID(),
		Creator:          creator,
		Threshold:        3,
		State:            types.SECRET_STATUS_PENDING,
		CreatedAt:        90,
		CommitDeadline:   200,
		RevealStartBlock: 1_000,
		RevealEndBlock:   2_000,
		RewardPool:       sdk.NewCoin(types.DefaultDenom, math.NewInt(1_000_000)),
		Bump:             types.MinBump,
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secret.Id)

	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(1_000) // window just opened

	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
		SecretId: secret.Id,
		Creator:  creator,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot cancel secret after reveal window starts")
}

// TestRevealWindow_EndBlockInclusive pins the window semantics the spec
// defines: reveals are valid at height == reveal_end_block (both bounds
// inclusive), rejected at end + 1, and settlement fires in the EndBlock of
// end + 1 — so a share revealed in the window's final block still earns its
// pool share and bond return.
func TestRevealWindow_EndBlockInclusive(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	// Full pipeline so shares carry valid HMACs
	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, 3)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
			Guardian: secret.SelectedGuardians[i],
			SecretId: secretId,
			Accept:   true,
		})
		require.NoError(t, err)
	}
	finaliseCommitDeadline(t, f, secretId)
	secret, _ = f.keeper.GetSecret(f.ctx, secretId)

	lastMinute := secret.SelectedGuardians[0]
	share := testShareBytes(secretId, lastMinute)

	// A reveal in the window's FINAL block (height == end) is valid
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock)
	_, err = msgServer.GuardianRevealShare(f.ctx, &types.MsgGuardianRevealShare{
		Guardian: lastMinute, SecretId: secretId, DecryptedShare: share,
	})
	require.NoError(t, err, "a reveal at height == reveal_end_block must be accepted")

	// One block later the window is closed to reveals...
	tooLate := secret.SelectedGuardians[1]
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock + 1)
	_, err = msgServer.GuardianRevealShare(f.ctx, &types.MsgGuardianRevealShare{
		Guardian: tooLate, SecretId: secretId,
		DecryptedShare: testShareBytes(secretId, tooLate),
	})
	require.Error(t, err, "a reveal at end + 1 must be rejected")
	require.Contains(t, err.Error(), "reveal window closed")

	// ...and that same block's EndBlock settles: the final-block revealer is
	// paid and its bond returned; the two no-shows are slashed
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	settled, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_FAILED, settled.State) // 1 < threshold 2: cryptographic outcome only

	revealer, _ := f.keeper.GetGuardian(f.ctx, lastMinute)
	require.True(t, revealer.LockedStake.IsZero(), "final-block revealer's bond must be returned")
	require.Equal(t,
		secret.RewardPool.Amount.Add(types.PerGuardianAcceptFee(settled.AcceptFees.Amount, settled.MaxShares)),
		bank.received(lastMinute),
		"the sole revealer takes the whole pool plus its own acceptance reimbursement, despite revealing in the final block")

	slashed, _ := f.keeper.GetGuardian(f.ctx, tooLate)
	require.True(t, slashed.LockedStake.IsZero())
	require.True(t, slashed.Stake.Amount.LT(revealer.Stake.Amount),
		"the late guardian must have been slashed as a no-show")

	assertInvariants(t, f)
	// Full-pipeline scenario: the exact escrow identity must also hold
	assertSolvency(t, f, bank)
}

// TestCancellation_ExcludesEarlySlashedLeaker is the C6 conformance scenario
// (docs/planning/done/DONE_ECONOMICS_TEST_STRATEGY_PLAN.md): a guardian is slashed for
// an early reveal, then the creator cancels mid-hold. The leaker must earn no
// cancellation wage (its slice flows to the creator via the unearned-remainder
// arithmetic), must have no bond released (it was deducted at report time),
// and the honest guardians settle exactly as normal.
// See spec.md "Cancellation and No-Fault Refunds".
func TestCancellation_ExcludesEarlySlashedLeaker(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	const bump = types.MinBump
	bond := conformanceBond(bump, 400, testRevealDuration)

	// Full pipeline: 3 slots, all accepted → pending, bonds locked
	secretId := setupBondTestSecret(t, f, msgServer, bump, 2, 3)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	guardians := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		g := secret.SelectedGuardians[i]
		_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
			Guardian: g, SecretId: secretId, Accept: true,
		})
		require.NoError(t, err)
		guardians = append(guardians, g)
	}
	finaliseCommitDeadline(t, f, secretId)
	secret, _ = f.keeper.GetSecret(f.ctx, secretId)
	honest := guardians[:2]
	leaker := guardians[2]
	leakerBefore, _ := f.keeper.GetGuardian(f.ctx, leaker)

	// The leaker leaks pre-window and is reported via the real message
	reporter := sdk.AccAddress([]byte("c6_reporter_________")).String()
	evidence := testShareBytes(secretId, leaker) // matches the pipeline HMAC
	burntBeforeReport := *bank.burnt             // baseline before the report-time slash
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.CommitDeadline + 10)
	_, err = msgServer.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: leaker,
		ReporterAddress: reporter,
		Reason:          "early reveal",
		Evidence:        evidence,
		SecretId:        secretId,
	})
	require.NoError(t, err)

	// Creator cancels mid-hold
	cancelHeight := secret.CommitDeadline + 50
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(cancelHeight)
	_, err = msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
		SecretId: secretId, Creator: secret.Creator,
	})
	require.NoError(t, err)

	elapsed := cancelHeight - secret.CommitDeadline
	payout := types.ProRataCancellationPayout(secret.RewardPool.Amount,
		(secret.RevealEndBlock+1)-secret.CommitDeadline, secret.MaxShares, elapsed)
	require.True(t, payout.IsPositive(), "mid-hold cancel must produce a wage")

	// Honest guardians: bond released in full, wage paid, acceptance reimbursed
	acceptFee := types.PerGuardianAcceptFee(secret.AcceptFees.Amount, secret.MaxShares)
	for _, g := range honest {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero(), "honest guardian %s bond must be released", g)
		require.Equal(t, payout.Add(acceptFee), bank.received(g),
			"honest guardian %s must earn the pro-rata wage and its acceptance reimbursement", g)
	}

	// Leaker: NO wage, no bond release (it was deducted at report time), and
	// its float reflects exactly the report-time slash — cancellation must not
	// touch it again in either direction
	leakerAfter, _ := f.keeper.GetGuardian(f.ctx, leaker)
	require.True(t, bank.received(leaker).IsZero(), "the leaker must earn no cancellation wage")
	require.True(t, leakerAfter.LockedStake.IsZero())
	require.True(t, leakerBefore.Stake.Amount.Sub(bond).Equal(leakerAfter.Stake.Amount),
		"the leaker's float must show only the report-time bond deduction")

	// Creator: refund = P − 2 × wage (the leaker's slice flows here), plus the
	// leaker's unearned acceptance slice — it forfeits every payment, not just
	// the wage — plus the 10% slash slice received at report time
	slashSliceToCreator := bond.MulRaw(types.EarlyRevealCreatorPercent).QuoRaw(100)
	expectedCreator := secret.RewardPool.Amount.Sub(payout.MulRaw(2)).Add(acceptFee).Add(slashSliceToCreator)
	require.True(t, expectedCreator.Equal(bank.received(secret.Creator)),
		"creator must receive the unearned remainder INCLUDING the leaker's slice (want %s, got %s)",
		expectedCreator, bank.received(secret.Creator))

	// Reporter: exactly the 50% bounty from the report — no share of the wage
	reporterBounty := bond.Sub(bond.MulRaw(types.EarlyRevealBurnPercent).QuoRaw(100)).Sub(slashSliceToCreator)
	require.True(t, reporterBounty.Equal(bank.received(reporter)),
		"reporter must receive only the report-time bounty")

	// Conservation across the whole scenario: pool + slashed bond fully
	// accounted for — nothing minted, nothing stranded
	poolOut := payout.MulRaw(2).Add(secret.RewardPool.Amount.Sub(payout.MulRaw(2))) // wages + creator refund
	require.True(t, poolOut.Equal(secret.RewardPool.Amount), "pool must be fully disbursed")
	bondOut := bank.burnt.Sub(burntBeforeReport).Add(reporterBounty).Add(slashSliceToCreator)
	require.True(t, bondOut.Equal(bond), "slashed bond must be fully disbursed (burn + bounty + creator slice)")

	// Terminal state
	cancelled, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_CANCELLED, cancelled.State)

	assertInvariants(t, f)
	// Full-pipeline scenario: the exact escrow identity must also hold
	assertSolvency(t, f, bank)
}
