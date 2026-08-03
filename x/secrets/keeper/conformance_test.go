package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Conformance suite — fills the scenario catalogue of
// docs/planning/done/DONE_ECONOMICS_TEST_STRATEGY_PLAN.md §5. Every scenario drives the
// real message handlers and EndBlock sweeps, asserts exact amounts derived
// from the base constants, and ends with the invariant library.

// registerConformanceGuardian registers a guardian with an exact float
// deposit through the real message handler.
func registerConformanceGuardian(t *testing.T, f *fixture, msgServer types.MsgServer, name string, deposit math.Int) string {
	t.Helper()
	addr := sdk.AccAddress([]byte(fmt.Sprintf("%-20s", name))).String()
	dep := sdk.NewCoin(types.DefaultDenom, deposit)
	_, err := msgServer.GuardianRegister(f.ctx, &types.MsgGuardianRegister{
		Guardian:            addr,
		EncryptionPublicKey: getValidPublicKey(name + "_enckey"),
		AvailableFrom:       0,
		AvailableUntil:      100000,
		Deposit:             &dep,
		AcceptingSecrets:    true,
	})
	require.NoError(t, err)
	return addr
}

// requestConformanceSecret drives Phases 1+2 with explicit parameters.
func requestConformanceSecret(t *testing.T, f *fixture, msgServer types.MsgServer, bump, threshold, minShares, maxShares, startOffset, duration int64) (string, error) {
	t.Helper()
	creator := sdk.AccAddress([]byte("creator_address")).String()
	resp, err := msgServer.UserRequestGuardians(f.ctx, &types.MsgUserRequestGuardians{
		Creator:       creator,
		DetectionHint: testDetectionHint(),
		Threshold:     threshold,
		MinShares:     minShares,
		MaxShares:     maxShares,
		RevealWindow:  &types.RevealWindow{StartOffset: startOffset, Duration: duration},
		Bump:          bump,
	})
	if err != nil {
		return "", err
	}

	// Distribute shares so guardians can accept/reveal
	secret, err := f.keeper.GetSecret(f.ctx, resp.SecretId)
	require.NoError(t, err)
	shareData := make([]*types.EncryptedShareData, 0, len(secret.SelectedGuardians))
	for _, guardian := range secret.SelectedGuardians {
		data := testShareBytes(resp.SecretId, guardian)
		shareData = append(shareData, &types.EncryptedShareData{
			GuardianAddress: guardian,
			EncryptedShare:  data,
			ShareHmac:       generateTestHMAC(resp.SecretId, guardian, data),
		})
	}
	_, err = msgServer.UserDistributeShares(f.ctx, &types.MsgUserDistributeShares{
		Creator:           creator,
		SecretId:          resp.SecretId,
		SecretCommitment:  []byte("secret_commitment_hash"),
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            shareData,
	})
	require.NoError(t, err)
	return resp.SecretId, nil
}

// conformanceBond is the frozen bond a freshly registered guardian (k at the
// registration floor) posts on a conformance secret of the given shape:
// distance = start_offset + duration + 1 − CommitTimeoutBlocks, B = rate ×
// distance × bump × k.
func conformanceBond(bump, startOffset, duration int64) math.Int {
	distance := startOffset + duration + 1 - types.CommitTimeoutBlocks
	return types.BondAmount(distance, bump, types.InitialBondK)
}

func acceptAs(t *testing.T, f *fixture, msgServer types.MsgServer, secretId, guardian string) error {
	t.Helper()
	_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
		Guardian: guardian, SecretId: secretId, Accept: true,
	})
	return err
}

func conformanceReveal(t *testing.T, f *fixture, msgServer types.MsgServer, secretId, guardian string) error {
	t.Helper()
	_, err := msgServer.GuardianRevealShare(f.ctx, &types.MsgGuardianRevealShare{
		Guardian:       guardian,
		SecretId:       secretId,
		DecryptedShare: testShareBytes(secretId, guardian),
	})
	return err
}

func setHeight(f *fixture, h int64) {
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(h)
}

func height(f *fixture) int64 { return sdk.UnwrapSDKContext(f.ctx).BlockHeight() }

// ─────────────────────────────────────────────────────────────────────────────
// A. Registration & float
// ─────────────────────────────────────────────────────────────────────────────

// A6: deposit top-up mid-hold, then withdraw — accounting exact across the
// lock boundary, proven against the full escrow identity.
func TestConformance_A6_TopUpMidHoldThenWithdraw(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	bond := conformanceBond(types.MinBump, 400, testRevealDuration)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, 3)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	g := secret.SelectedGuardians[0]
	require.NoError(t, acceptAs(t, f, msgServer, secretId, g)) // locks B

	before, _ := f.keeper.GetGuardian(f.ctx, g)
	initialTotal := before.Stake.Amount // 4×B from the fixture

	// Top up mid-hold
	topUp := sdk.NewCoin(types.DefaultDenom, bond.MulRaw(2))
	_, err := msgServer.GuardianUpdate(f.ctx, &types.MsgGuardianUpdate{
		Guardian: g,
		Deposit:  &topUp,
	})
	require.NoError(t, err)

	mid, _ := f.keeper.GetGuardian(f.ctx, g)
	require.True(t, initialTotal.Add(topUp.Amount).Equal(mid.Stake.Amount), "top-up must add to the float total")
	require.True(t, bond.Equal(mid.LockedStake.Amount), "top-up must not touch the locked portion")

	// Withdraw: everything except the locked bond comes back
	_, err = msgServer.GuardianWithdrawStake(f.ctx, &types.MsgGuardianWithdrawStake{Guardian: g})
	require.NoError(t, err)

	after, _ := f.keeper.GetGuardian(f.ctx, g)
	require.True(t, bond.Equal(after.Stake.Amount), "float must shrink to exactly the locked bond")
	require.True(t, bond.Equal(after.LockedStake.Amount))
	expectedWithdrawal := initialTotal.Add(topUp.Amount).Sub(bond)
	require.True(t, expectedWithdrawal.Equal(bank.received(g)), "withdrawal must return exactly total − locked")

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// ─────────────────────────────────────────────────────────────────────────────
// B. Selection & acceptance
// ─────────────────────────────────────────────────────────────────────────────

// B1: a high-bump secret excludes guardians whose unlocked float cannot cover
// its bond — candidacy is per secret.
func TestConformance_B1_HighBumpExcludesSmallFloats(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	const bump = types.MaxBump // 10.00 — the priciest bond this shape can demand
	bigBond := conformanceBond(bump, 400, 150)

	big := map[string]bool{}
	for i := 0; i < 3; i++ {
		addr := registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("b1_big_%02d", i), bigBond.MulRaw(2))
		big[addr] = true
	}
	for i := 0; i < 5; i++ {
		registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("b1_small_%02d", i), bigBond.QuoRaw(2)) // half a bond — far short
	}
	setHeight(f, height(f)+1) // availability windows begin

	secretId, err := requestConformanceSecret(t, f, msgServer, bump, 2, 2, 2, 400, 150)
	require.NoError(t, err)

	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.NotEmpty(t, secret.SelectedGuardians)
	for _, addr := range secret.SelectedGuardians {
		require.True(t, big[addr],
			"guardian %s was assigned but its unlocked float cannot cover the %s bond", addr, bigBond)
	}

	assertInvariants(t, f)
}

// B2: too few eligible guardians — Phase 1 fails cleanly with NOTHING
// escrowed and nothing enqueued (selection precedes the pool lock).
func TestConformance_B2_InsufficientGuardiansEscrowsNothing(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	registerConformanceGuardian(t, f, msgServer, "b2_lonely_guardian", testFloatUnit().MulRaw(4))
	setHeight(f, height(f)+1)
	escrowedBefore := *bank.escrowedIn

	_, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 5, 5, 400, 150) // needs max_shares 5, has 1
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient guardians")

	// Nothing was escrowed, stored, or enqueued
	require.True(t, escrowedBefore.Equal(*bank.escrowedIn), "a failed Phase 1 must not escrow the pool")
	count := 0
	require.NoError(t, f.keeper.Secrets.Walk(f.ctx, nil, func(string, types.Secret) (bool, error) {
		count++
		return false, nil
	}))
	require.Zero(t, count, "a failed Phase 1 must not store a secret")

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// B5: one guardian accepting concurrent secrets until its float is exhausted —
// the acceptance-time gate rejects the over-commitment; the legitimate bond
// stays intact and the starved secret commit-times-out cleanly.
func TestConformance_B5_FloatExhaustionAcrossSecrets(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	bond := conformanceBond(types.MinBump, 400, 150)

	// Exactly one bond of float each — every guardian can carry ONE secret
	guardians := make([]string, 3)
	for i := range guardians {
		guardians[i] = registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("b5_guardian_%02d", i), bond)
	}
	setHeight(f, height(f)+1)

	// Both secrets select all three candidates (band [min 2, max 3]) while
	// everyone's unlocked float still covers the bond
	secretA, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 2, 3, 400, 150)
	require.NoError(t, err)
	secretB, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 2, 3, 400, 150)
	require.NoError(t, err)

	// Guardians 0 and 1 fill secret A (locks their only bond)
	require.NoError(t, acceptAs(t, f, msgServer, secretA, guardians[0]))
	require.NoError(t, acceptAs(t, f, msgServer, secretA, guardians[1]))

	// Their acceptances of secret B must now fail the capital gate
	err = acceptAs(t, f, msgServer, secretB, guardians[0])
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient unlocked float")
	err = acceptAs(t, f, msgServer, secretB, guardians[1])
	require.Error(t, err)

	// Guardian 2 accepts B (its float is free) but B cannot reach min_shares
	require.NoError(t, acceptAs(t, f, msgServer, secretB, guardians[2]))

	// The shared deadline finalises both: A activates with its 2 acceptances
	// (= min_shares), B fails below the floor — bond released, pool refunded
	secB, _ := f.keeper.GetSecret(f.ctx, secretB)
	setHeight(f, secB.CommitDeadline+1)
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

	activatedA, _ := f.keeper.GetSecret(f.ctx, secretA)
	require.Equal(t, types.SECRET_STATUS_PENDING, activatedA.State)
	failed, _ := f.keeper.GetSecret(f.ctx, secretB)
	require.Equal(t, types.SECRET_STATUS_FAILED, failed.State)
	g2, _ := f.keeper.GetGuardian(f.ctx, guardians[2])
	require.True(t, g2.LockedStake.IsZero(), "the starved secret must release its accepted bond")

	// Secret A's bonds are untouched by any of it
	for _, g := range guardians[:2] {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, bond.Equal(guardian.LockedStake.Amount), "secret A's bond must survive B's failure")
	}

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// B6: acceptance after the commit deadline is rejected.
func TestConformance_B6_AcceptanceAfterDeadlineRejected(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, 3)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)

	setHeight(f, secret.CommitDeadline+1)
	err := acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[0])
	require.Error(t, err)
	require.Contains(t, err.Error(), "commit deadline passed")

	// The sweep then fails it cleanly
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))
	failed, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_FAILED, failed.State)

	assertInvariants(t, f)
}

// B7: a guardian that withdraws its unlocked float mid-hold keeps its bond
// obligation; settlement still returns the bond, which is then withdrawable.
func TestConformance_B7_WithdrawMidHoldThenSettle(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	bond := conformanceBond(types.MinBump, 400, testRevealDuration)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, 3)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	guardians := make([]string, 3)
	for i := int64(0); i < 3; i++ {
		guardians[i] = secret.SelectedGuardians[i]
		require.NoError(t, acceptAs(t, f, msgServer, secretId, guardians[i]))
	}
	finaliseCommitDeadline(t, f, secretId)

	// Guardian 0 withdraws mid-hold: unlocked comes back, the bond stays
	_, err := msgServer.GuardianWithdrawStake(f.ctx, &types.MsgGuardianWithdrawStake{Guardian: guardians[0]})
	require.NoError(t, err)
	mid, _ := f.keeper.GetGuardian(f.ctx, guardians[0])
	require.True(t, bond.Equal(mid.Stake.Amount), "only the bond may remain in escrow")
	require.True(t, bond.Equal(mid.LockedStake.Amount))

	// Everyone reveals; settle at end + 1
	secret, _ = f.keeper.GetSecret(f.ctx, secretId)
	setHeight(f, secret.RevealStartBlock)
	for _, g := range guardians {
		require.NoError(t, conformanceReveal(t, f, msgServer, secretId, g))
	}
	setHeight(f, secret.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// The bond came back unlocked and is withdrawable
	settled, _ := f.keeper.GetGuardian(f.ctx, guardians[0])
	require.True(t, bond.Equal(settled.Stake.Amount))
	require.True(t, settled.LockedStake.IsZero())
	_, err = msgServer.GuardianWithdrawStake(f.ctx, &types.MsgGuardianWithdrawStake{Guardian: guardians[0]})
	require.NoError(t, err)
	empty, _ := f.keeper.GetGuardian(f.ctx, guardians[0])
	require.True(t, empty.Stake.IsZero(), "post-settlement withdrawal must empty the float")

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// ─────────────────────────────────────────────────────────────────────────────
// C. Cancellation
// ─────────────────────────────────────────────────────────────────────────────

// C1: pre-activation cancellation is rejected outright (ruled July 2026 —
// cancellation is a post-activation mechanic; pre-activation secrets exit via
// commit-timeout only). Locked bonds are untouched by the rejected attempt
// and are released by the commit-timeout, which refunds the pool in full.
func TestConformance_C1_CommitPhaseCancelRejected(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, 3)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)

	// Partial acceptance (2 of 3 requested): bonds locked, still awaiting_acceptance
	for i := 0; i < 2; i++ {
		require.NoError(t, acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[i]))
	}
	mid, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_AWAITING_ACCEPTANCE, mid.State)

	// Cancellation is rejected pre-activation; nothing moves
	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
		SecretId: secretId, Creator: secret.Creator,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "can only cancel secrets in pending state")
	for i := 0; i < 2; i++ {
		bond, ok := secret.GuardianBondAmount(secret.SelectedGuardians[i])
		require.True(t, ok)
		g, _ := f.keeper.GetGuardian(f.ctx, secret.SelectedGuardians[i])
		require.True(t, bond.Equal(g.LockedStake.Amount),
			"rejected cancel must leave the bond locked")
	}
	require.True(t, bank.received(secret.Creator).IsZero(), "rejected cancel must move no funds")

	// The only pre-activation exit: commit-timeout — full pool refund, bonds
	// released. The acceptance reimbursement is NOT the pool: the two
	// guardians that accepted did the job asked of them and are paid, while
	// the third slice — nobody's work — goes back with the pool.
	setHeight(f, secret.CommitDeadline+1)
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))
	failed, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_FAILED, failed.State)

	acceptFee := types.PerGuardianAcceptFee(secret.AcceptFees.Amount, secret.MaxShares)
	for i := 0; i < 2; i++ {
		require.True(t, acceptFee.Equal(bank.received(secret.SelectedGuardians[i])),
			"a guardian that accepted a secret which never activated is reimbursed for accepting")
	}
	require.True(t, bank.received(secret.SelectedGuardians[2]).IsZero(),
		"a guardian that never accepted earns nothing")
	require.True(t, secret.RewardPool.Amount.Add(acceptFee).Equal(bank.received(secret.Creator)),
		"commit-timeout must refund the pool in full, plus the accept slice nobody earned")
	for i := 0; i < 2; i++ {
		g, _ := f.keeper.GetGuardian(f.ctx, secret.SelectedGuardians[i])
		require.True(t, g.LockedStake.IsZero(), "commit-timeout must release every accepted bond")
	}

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// C2: cancelling immediately after activation (still inside the commit phase,
// so elapsed clamps to 0) succeeds, refunds the full pool, pays no wages, and
// releases every bond — the mechanic that replaces pre-activation cancel.
func TestConformance_C2_CancelImmediatelyAfterActivation(t *testing.T) {
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
	// The roster finalises at the deadline; activation never happens mid-window
	mid, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_AWAITING_ACCEPTANCE, mid.State)
	finaliseCommitDeadline(t, f, secretId)
	activated, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_PENDING, activated.State)

	// Cancel at the first possible moment (the finalisation height,
	// deadline + 1): each guardian has earned exactly one block's wage
	elapsed := height(f) - activated.CommitDeadline
	require.Equal(t, int64(1), elapsed, "earliest cancel is one block past the deadline")
	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
		SecretId: secretId, Creator: secret.Creator,
	})
	require.NoError(t, err)

	wage := types.ProRataCancellationPayout(secret.RewardPool.Amount,
		(secret.RevealEndBlock+1)-secret.CommitDeadline, secret.MaxShares, elapsed)
	refund := secret.RewardPool.Amount.Sub(wage.MulRaw(3))
	require.True(t, refund.Equal(bank.received(secret.Creator)),
		"earliest cancel must refund everything except one block's wages")
	// Acceptance is reimbursed in full even at the earliest possible cancel:
	// the wage accrues with the hold, the reimbursement does not — it was
	// earned outright by accepting
	acceptFee := types.PerGuardianAcceptFee(secret.AcceptFees.Amount, secret.MaxShares)
	for _, g := range guardians {
		require.True(t, wage.Add(acceptFee).Equal(bank.received(g)),
			"one block of wage accrues past the deadline, on top of the full acceptance reimbursement")
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero(), "cancel must release every bond")
	}

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// C3: cancelling one block before the reveal window opens pays the guardians
// almost the full pool and the creator only the sliver that remains.
func TestConformance_C3_CancelAtLastPossibleBlock(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	const bump = types.MinBump
	secretId := setupBondTestSecret(t, f, msgServer, bump, 2, 3)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	guardians := make([]string, 3)
	for i := int64(0); i < 3; i++ {
		guardians[i] = secret.SelectedGuardians[i]
		require.NoError(t, acceptAs(t, f, msgServer, secretId, guardians[i]))
	}
	finaliseCommitDeadline(t, f, secretId)
	secret, _ = f.keeper.GetSecret(f.ctx, secretId)

	cancelHeight := secret.RevealStartBlock - 1
	setHeight(f, cancelHeight)
	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{
		SecretId: secretId, Creator: secret.Creator,
	})
	require.NoError(t, err)

	payout := types.ProRataCancellationPayout(secret.RewardPool.Amount,
		(secret.RevealEndBlock+1)-secret.CommitDeadline, secret.MaxShares,
		cancelHeight-secret.CommitDeadline)
	acceptFee := types.PerGuardianAcceptFee(secret.AcceptFees.Amount, secret.MaxShares)
	for _, g := range guardians {
		require.True(t, payout.Add(acceptFee).Equal(bank.received(g)),
			"guardian %s must earn the near-full wage plus its acceptance reimbursement", g)
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero())
	}
	refund := secret.RewardPool.Amount.Sub(payout.MulRaw(3))
	require.True(t, refund.Equal(bank.received(secret.Creator)))
	// Cross-check the refund from first principles: what remains unearned at
	// start − 1 is exactly the window span (start − 1 → end + 1) per guardian.
	// "One block before the window" is the LAST cancellable moment, but the
	// guardians have only earned the pre-window hold — the window itself is
	// always refunded, so the creator's refund is never a mere sliver.
	unearnedPerGuardian := types.ProRataCancellationPayout(secret.RewardPool.Amount,
		(secret.RevealEndBlock+1)-secret.CommitDeadline, secret.MaxShares,
		secret.RevealEndBlock+1-cancelHeight)
	// The two expressions agree up to integer-division dust: each floors a
	// per-guardian slice of a pool that no longer divides evenly (it carries
	// the reveal legs as well as the wage), so the gap is bounded by two uveil
	// per guardian and can never be negative — the creator is never short.
	crossCheckGap := refund.Sub(unearnedPerGuardian.MulRaw(3))
	require.False(t, crossCheckGap.IsNegative(),
		"refund must never fall below the unearned window span × guardians")
	require.True(t, crossCheckGap.LTE(math.NewInt(6)),
		"refund must equal the unearned window span × guardians, to within truncation dust (got a gap of %s)",
		crossCheckGap)

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// C5: a cancelled secret cannot be cancelled again.
func TestConformance_C5_DoubleCancelRejected(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 2, 3)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	// Cancellation is pending-only: accept and finalise at the deadline first
	for i := 0; i < 3; i++ {
		require.NoError(t, acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[i]))
	}
	finaliseCommitDeadline(t, f, secretId)

	_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{SecretId: secretId, Creator: secret.Creator})
	require.NoError(t, err)

	_, err = msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{SecretId: secretId, Creator: secret.Creator})
	require.Error(t, err)
	require.Contains(t, err.Error(), "can only cancel")

	assertInvariants(t, f)
}

// failingBankKeeper fails the next N module→account sends, then behaves
// normally — the D8 fault-injection hook (test-only, per plan §9.5).
type failingBankKeeper struct {
	mockBankKeeper
	failuresRemaining *int
}

func (fb *failingBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	if *fb.failuresRemaining > 0 {
		*fb.failuresRemaining--
		return fmt.Errorf("injected transient bank failure")
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// D. Reveal & settlement
// ─────────────────────────────────────────────────────────────────────────────

// conformanceSettlementSecret is settlementSecret with a parametrised
// threshold, for the D2 grid.
var conformanceSecretSeq int

func conformanceSettlementSecret(t *testing.T, f *fixture, n int, threshold int64, bond, pool math.Int, creator string) (types.Secret, []string) {
	t.Helper()
	currentHeight := height(f)
	conformanceSecretSeq++ // unique guardian sets per fixture call

	guardians := make([]string, 0, n)
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("cs%04d_g%03d_________", conformanceSecretSeq, i))).String()
		guardians = append(guardians, addr)
		setupSlashableGuardian(t, f, addr, bond)
	}

	secret := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           threshold,
		MinShares:           int64(n),
		MaxShares:           int64(n),
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           1,
		CommitDeadline:      50,
		RevealStartBlock:    currentHeight - 100,
		RevealEndBlock:      currentHeight - 1, // settlement due at end+1 = now
		RewardPool:          sdk.NewCoin(types.DefaultDenom, pool),
		GuardianBondAmounts: repeatBond(bond, n),
		Bump:                types.MinBump,
		SelectedGuardians:   guardians,
		AcceptedCount:       int64(n),
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secret.Id)
	for _, addr := range guardians {
		writeShareRecord(t, f, secret.Id, addr, []byte("encrypted_share_data"), nil)
		writeAssignmentRecord(t, f, secret.Id, addr, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	}
	enqueueForState(t, f, secret)
	return secret, guardians
}

// D2: the settlement outcome grid — corners plus one interior point per
// n ∈ {2, 5, 24} (plan §9.2). For every (threshold, revealers) combination the
// payments follow the same threshold-independent rules; only the terminal
// state differs.
func TestConformance_D2_SettlementGrid(t *testing.T) {
	pool := math.NewInt(1_000_000_007) // indivisible by most r → exercises dust

	type combo struct{ n, t, r int }
	grid := []combo{}
	for _, n := range []int{2, 5, 24} {
		tMax := n
		if tMax > 16 {
			tMax = 16
		}
		thresholds := []int{2, tMax}
		if n == 5 {
			thresholds = append(thresholds, 3) // interior point
		}
		for _, th := range thresholds {
			for _, r := range []int{0, 1, th - 1, th, n} {
				if r < 0 || r > n {
					continue
				}
				grid = append(grid, combo{n, th, r})
			}
		}
	}

	for _, c := range grid {
		c := c
		t.Run(fmt.Sprintf("n=%d_t=%d_r=%d", c.n, c.t, c.r), func(t *testing.T) {
			bank := newTrackingBankKeeper()
			f := initFixtureWithBank(t, bank)
			setHeight(f, 10_000)

			bond := testFloatUnit()
			creator := sdk.AccAddress([]byte("d2_grid_creator_____")).String()
			secret, guardians := conformanceSettlementSecret(t, f, c.n, int64(c.t), bond, pool, creator)
			if c.r > 0 {
				markRevealed(t, f, secret.Id, guardians[:c.r]...)
			}

			require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

			// Terminal state is the ONLY thing the threshold decides
			settled, _ := f.keeper.GetSecret(f.ctx, secret.Id)
			if c.r >= c.t {
				require.Equal(t, types.SECRET_STATUS_REVEALED, settled.State)
			} else {
				require.Equal(t, types.SECRET_STATUS_FAILED, settled.State)
			}

			// Payments never branch on it
			burnShare := bond.MulRaw(types.NoRevealBurnPercent).QuoRaw(100)
			creatorShare := bond.MulRaw(types.NoRevealCreatorPercent).QuoRaw(100)
			noShows := int64(c.n - c.r)

			if c.r > 0 {
				per := pool.QuoRaw(int64(c.r))
				for _, g := range guardians[:c.r] {
					require.True(t, per.Equal(bank.received(g)), "revealer must get pool/r")
					guardian, _ := f.keeper.GetGuardian(f.ctx, g)
					require.True(t, guardian.LockedStake.IsZero())
					require.True(t, bond.MulRaw(2).Equal(guardian.Stake.Amount), "revealer float untouched")
				}
				dust := pool.Sub(per.MulRaw(int64(c.r)))
				require.True(t, bank.burnt.Equal(burnShare.MulRaw(noShows).Add(dust)),
					"burn must be no-show burn slices + pool dust")
				require.True(t, bank.received(creator).Equal(creatorShare.MulRaw(noShows)),
					"creator gets only slash slices when anyone revealed")
			} else {
				// Sole refund case
				require.True(t, bank.received(creator).Equal(pool.Add(creatorShare.MulRaw(noShows))),
					"with zero revealers the creator gets the pool + slash slices")
				require.True(t, bank.burnt.Equal(burnShare.MulRaw(noShows)))
			}
			for _, g := range guardians[c.r:] {
				guardian, _ := f.keeper.GetGuardian(f.ctx, g)
				require.True(t, guardian.LockedStake.IsZero())
				require.True(t, bond.MulRaw(2).Sub(bond.MulRaw(50).QuoRaw(100)).Equal(guardian.Stake.Amount),
					"no-show keeps exactly the returned 50%%")
			}

			assertInvariants(t, f)
		})
	}
}

// D7: odd bumps (fixed-point hundredths) — every slash split must conserve the
// bond exactly, and pool dust must be burned, for values that do not divide
// cleanly.
func TestConformance_D7_RoundingConservation(t *testing.T) {
	for _, bump := range []int64{101, 333, 999} {
		bump := bump
		t.Run(fmt.Sprintf("bump=%d", bump), func(t *testing.T) {
			bank := newTrackingBankKeeper()
			f := initFixtureWithBank(t, bank)
			setHeight(f, 10_000)

			bond := types.BondAmount(351, bump, types.InitialBondK) // arbitrary fixture distance
			pool := math.NewInt(1_000_000_000)                      // ÷3 leaves remainder 1 → dust
			creator := sdk.AccAddress([]byte("d7_round_creator____")).String()
			secret, guardians := conformanceSettlementSecret(t, f, 4, 2, bond, pool, creator)
			secret.Bump = bump
			require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
			markRevealed(t, f, secret.Id, guardians[:3]...) // 3 reveal, 1 no-show

			require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

			// Bond conservation for the no-show: burn + creator + returned == B
			burnShare := bond.MulRaw(types.NoRevealBurnPercent).QuoRaw(100)
			creatorShare := bond.MulRaw(types.NoRevealCreatorPercent).QuoRaw(100)
			returned := bond.Sub(burnShare).Sub(creatorShare)
			require.True(t, burnShare.Add(creatorShare).Add(returned).Equal(bond),
				"slash split must conserve the bond to the uveil")
			noShow, _ := f.keeper.GetGuardian(f.ctx, guardians[3])
			require.True(t, bond.MulRaw(2).Sub(burnShare).Sub(creatorShare).Equal(noShow.Stake.Amount))

			// Pool conservation: paid + dust == P
			per := pool.QuoRaw(3)
			dust := pool.Sub(per.MulRaw(3))
			require.True(t, dust.IsPositive(), "combo must exercise dust")
			require.True(t, bank.burnt.Equal(burnShare.Add(dust)), "burn = slash burn + pool dust")

			assertInvariants(t, f)
		})
	}
}

// D8: a transient failure during the commit-timeout refund keeps the queue
// entry and the non-terminal state, and the next block's sweep retries to
// success — no double refund, no stranded state.
func TestConformance_D8_CommitRefundRetriesAfterTransientFailure(t *testing.T) {
	failures := 1
	f := initFixtureWithBank(t, &failingBankKeeper{failuresRemaining: &failures})
	setHeight(f, 100)

	creator := sdk.AccAddress([]byte("d8_retry_creator____")).String()
	secret := types.Secret{
		Id:             types.GenerateValidSecretID(),
		Creator:        creator,
		Threshold:      2,
		State:          types.SECRET_STATUS_RESERVED,
		CreatedAt:      90,
		CommitDeadline: 150,
		RevealEndBlock: 1_000,
		RewardPool:     sdk.NewCoin(types.DefaultDenom, math.NewInt(75_000)),
		Bump:           types.MinBump,
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	enqueueForState(t, f, secret)

	// First sweep: the injected failure aborts the refund BEFORE any state
	// transition — the secret stays reserved and the entry stays queued
	setHeight(f, secret.CommitDeadline+1)
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))
	still, _ := f.keeper.GetSecret(f.ctx, secret.Id)
	require.Equal(t, types.SECRET_STATUS_RESERVED, still.State, "a failed refund must not fail the secret")

	// Next block: the retry succeeds and the secret terminates exactly once
	setHeight(f, secret.CommitDeadline+2)
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))
	failed, _ := f.keeper.GetSecret(f.ctx, secret.Id)
	require.Equal(t, types.SECRET_STATUS_FAILED, failed.State)
	require.Zero(t, failures, "exactly one injected failure must have been consumed")

	// A third sweep is a no-op (terminal + dequeued)
	setHeight(f, secret.CommitDeadline+3)
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))

	assertInvariants(t, f)
}

// ─────────────────────────────────────────────────────────────────────────────
// E. Early-reveal reporting
// ─────────────────────────────────────────────────────────────────────────────

// E4: the creator may act as the reporter. It nets its 10% compensation plus
// the 50% bounty — but the 40% burn always stands (the self-dealing bound).
func TestConformance_E4_CreatorAsReporter(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	srv := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	guardianAddr := sdk.AccAddress([]byte("e4_guardian_________")).String()
	creatorAddr := sdk.AccAddress([]byte("e4_creator__________")).String()
	bond := testFloatUnit()
	evidence := []byte("decrypted_share_data_minimum_32_bytes_required_for_evidence")

	setupSlashableGuardian(t, f, guardianAddr, bond)
	secretId := setupEarlyRevealSecret(t, f, guardianAddr, creatorAddr, bond, evidence)

	_, err := srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: guardianAddr,
		ReporterAddress: creatorAddr, // the creator reports its own guardian
		Reason:          "early reveal",
		Evidence:        evidence,
		SecretId:        secretId,
	})
	require.NoError(t, err, "creator-as-reporter is allowed")

	// Creator nets 10% + 50% = 60% of the bond — never more, because...
	require.True(t, bond.MulRaw(60).QuoRaw(100).Equal(bank.received(creatorAddr)))
	// ...the 40% burn is unconditional: no self-dealing path recovers it
	require.True(t, bond.MulRaw(40).QuoRaw(100).Equal(*bank.burnt),
		"the burn share must stand even when creator and reporter are the same party")

	assertInvariants(t, f)
}

// multiGuardianPendingSecret builds a pending pre-window secret with n bonded
// guardians whose HMACs match per-guardian evidence.
func multiGuardianPendingSecret(t *testing.T, f *fixture, n int, threshold int64, creator string, bond math.Int) (string, []string, [][]byte) {
	t.Helper()
	secretId := types.GenerateValidSecretID()
	currentHeight := height(f)

	guardians := make([]string, n)
	evidence := make([][]byte, n)
	for i := 0; i < n; i++ {
		guardians[i] = sdk.AccAddress([]byte(fmt.Sprintf("e5_guardian_%02d______", i))).String()
		setupSlashableGuardian(t, f, guardians[i], bond)
		evidence[i] = []byte(fmt.Sprintf("leaked_share_%02d_padded_to_minimum_32_bytes____", i))
	}

	secret := types.Secret{
		Id:                  secretId,
		Creator:             creator,
		Threshold:           threshold,
		MinShares:           int64(n),
		MaxShares:           int64(n),
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           currentHeight - 10,
		CommitDeadline:      currentHeight - 5,
		RevealStartBlock:    currentHeight + 1_000, // window NOT yet open
		RevealEndBlock:      currentHeight + 2_000,
		RewardPool:          sdk.NewCoin(types.DefaultDenom, math.NewInt(900_000_000)),
		GuardianBondAmounts: repeatBond(bond, n),
		Bump:                types.MinBump,
		SelectedGuardians:   guardians,
		AcceptedCount:       int64(n),
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	seedSealFields(t, f, secret.Id)
	for i := 0; i < n; i++ {
		writeShareRecord(t, f, secretId, guardians[i], []byte("encrypted_share_data"),
			computeEvidenceHMAC(secretId, guardians[i], evidence[i]))
		writeAssignmentRecord(t, f, secretId, guardians[i], types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	}
	enqueueForState(t, f, secret)
	return secretId, guardians, evidence
}

// E5: ALL active guardians are early-slashed — the compound case. Settlement
// sees zero eligible revealers, refunds the pool to the creator, and slashes
// nobody twice. The auto-revealed evidence still made the secret
// reconstructable, so the terminal state is REVEALED — the creator gets both
// the leaked-early secret's reconstruction AND the pool back, and the leakers
// lose their entire bonds. Every party's incentive lands correctly.
func TestConformance_E5_AllGuardiansEarlySlashed(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	srv := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	bond := testFloatUnit()
	creator := sdk.AccAddress([]byte("e5_creator__________")).String()
	reporter := sdk.AccAddress([]byte("e5_reporter_________")).String()
	secretId, guardians, evidence := multiGuardianPendingSecret(t, f, 3, 2, creator, bond)

	for i := range guardians {
		_, err := srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
			GuardianAddress: guardians[i],
			ReporterAddress: reporter,
			Reason:          "early reveal",
			Evidence:        evidence[i],
			SecretId:        secretId,
		})
		require.NoError(t, err, "report %d must succeed", i)
	}

	// The auto-revealed evidence crossed the threshold pre-window
	preSettle, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_RECONSTRUCTABLE, preSettle.State)

	// Settle at end + 1
	setHeight(f, preSettle.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	settled, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_REVEALED, settled.State,
		"the leaked evidence reconstructs the secret — a cryptographic outcome")

	// Zero eligible revealers → the pool goes back to the creator, plus the
	// 3 × 10% report-time slices
	expectedCreator := preSettle.RewardPool.Amount.Add(bond.MulRaw(10).QuoRaw(100).MulRaw(3))
	require.True(t, expectedCreator.Equal(bank.received(creator)),
		"creator must get the pool back (nobody eligible) plus slash slices")

	// Each leaker lost its whole bond at report time, nothing more at settlement
	for _, g := range guardians {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero())
		require.True(t, bond.Equal(guardian.Stake.Amount), "exactly one bond lost, no double slash")
		require.True(t, bank.received(g).IsZero(), "a leaker earns nothing")
	}

	// Burn = 3 × 40% of bond; reporter = 3 × 50%
	require.True(t, bond.MulRaw(40).QuoRaw(100).MulRaw(3).Equal(*bank.burnt))
	require.True(t, bond.MulRaw(50).QuoRaw(100).MulRaw(3).Equal(bank.received(reporter)))

	assertInvariants(t, f)
}

// E6: auto-revealed leak evidence counts toward the reconstruction threshold —
// the recipient can reconstruct before the window even opens.
func TestConformance_E6_EvidenceCountsTowardThreshold(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	bond := testFloatUnit()
	creator := sdk.AccAddress([]byte("e6_creator__________")).String()
	secretId, guardians, evidence := multiGuardianPendingSecret(t, f, 3, 2, creator, bond)

	for i := 0; i < 2; i++ { // exactly threshold-many reports
		_, err := srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
			GuardianAddress: guardians[i],
			ReporterAddress: sdk.AccAddress([]byte("e6_reporter_________")).String(),
			Reason:          "early reveal",
			Evidence:        evidence[i],
			SecretId:        secretId,
		})
		require.NoError(t, err)
	}

	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_RECONSTRUCTABLE, secret.State,
		"threshold-many auto-revealed leaks make the secret reconstructable pre-window")
	require.Equal(t, int64(2), secret.RevealedCount)
	require.Len(t, revealsFor(t, f, secretId), 2)

	assertInvariants(t, f)
}

// ─────────────────────────────────────────────────────────────────────────────
// F. Timeouts, queues, multi-secret
// ─────────────────────────────────────────────────────────────────────────────

// F2: genesis import re-registers non-terminal secrets in the due-height
// queues, and the <=-drain settles already-past-due imports on the first
// sweep instead of stranding them.
func TestConformance_F2_GenesisImportSettlesPastDue(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	setHeight(f, 10_000)

	bond := testFloatUnit()
	creator := sdk.AccAddress([]byte("f2_creator__________")).String()
	guardianAddr := sdk.AccAddress([]byte("f2_guardian_________")).String()

	total := sdk.NewCoin(types.DefaultDenom, bond.MulRaw(2))
	locked := sdk.NewCoin(types.DefaultDenom, bond)
	pastDue := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           2,
		MinShares:           1,
		MaxShares:           1,
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           100,
		CommitDeadline:      150,
		RevealStartBlock:    200,
		RevealEndBlock:      300, // due at 301 — thousands of blocks ago
		RewardPool:          sdk.NewCoin(types.DefaultDenom, math.NewInt(45_000)),
		GuardianBondAmounts: []int64{bond.Int64()},
		Bump:                types.MinBump,
		SelectedGuardians:   []string{guardianAddr},
		AcceptedCount:       1,
		SecretPublicKey:     testSecretPublicKey(),
	}

	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{
		Secrets: []*types.Secret{&pastDue},
		Guardians: []*types.Guardian{{
			Address:             guardianAddr,
			EncryptionPublicKey: getValidPublicKey("f2_enckey"),
			AvailableFrom:       1,
			AvailableUntil:      100_000,
			Stake:               &total,
			LockedStake:         &locked,
			AcceptingSecrets:    true,
			BondK:               types.InitialBondK,
			ActiveBondCount:     1,
		}},
		SecretShares: []types.StoredShare{{
			SecretId:        pastDue.Id,
			GuardianAddress: guardianAddr,
			Data:            types.SecretShareData{EncryptedShare: []byte("encrypted_share_data")},
		}},
		SecretAssignments: []types.StoredAssignment{{
			SecretId:        pastDue.Id,
			GuardianAddress: guardianAddr,
			Record:          types.AssignmentRecord{Status: types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED},
		}},
		SecretPayloads: []types.StoredPayload{{
			SecretId:          pastDue.Id,
			PayloadCiphertext: testPayloadCiphertext(),
		}},
	}))

	// The first sweep after import settles it (no-show slash: nobody revealed)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	settled, err := f.keeper.GetSecret(f.ctx, pastDue.Id)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_FAILED, settled.State)
	require.True(t, bank.received(creator).Equal(
		pastDue.RewardPool.Amount.Add(bond.MulRaw(10).QuoRaw(100))),
		"imported past-due secret must settle on the first block after import")

	assertInvariants(t, f)
}

// F3: multiple secrets due in the same block are all processed, and a stale
// entry for an already-terminal secret self-heals (dequeued without touching
// the secret).
func TestConformance_F3_MultipleDueSameBlockPlusStaleEntry(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	setHeight(f, 10_000)

	bond := testFloatUnit()
	creator := sdk.AccAddress([]byte("f3_creator__________")).String()

	// Two secrets due at exactly the same height
	secretA, guardiansA := conformanceSettlementSecret(t, f, 2, 2, bond, math.NewInt(100_000), creator)
	secretB, guardiansB := conformanceSettlementSecret(t, f, 2, 2, bond, math.NewInt(200_000), creator)
	markRevealed(t, f, secretA.Id, guardiansA...)
	markRevealed(t, f, secretB.Id, guardiansB[:1]...)

	// Plus a stale settlement entry for a terminal secret (simulates any
	// historical dequeue miss — the sweep must self-heal)
	stale := types.Secret{
		Id:             types.GenerateValidSecretID(),
		Creator:        creator,
		Threshold:      2,
		State:          types.SECRET_STATUS_CANCELLED,
		CreatedAt:      1,
		RevealEndBlock: height(f) - 1,
		RewardPool:     sdk.NewCoin(types.DefaultDenom, math.ZeroInt()),
		Bump:           types.MinBump,
		TerminalAt:     height(f) - 1, // the stamp the FSM leaves at cancellation
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, stale))
	require.NoError(t, f.keeper.SettlementQueue.Set(f.ctx, collections.Join(stale.RevealEndBlock+1, stale.Id)))
	// Terminal fixtures carry the prune entry production would have left
	require.NoError(t, f.keeper.PruneQueue.Set(f.ctx, collections.Join(stale.TerminalAt+types.RetentionBlocksValue(), stale.Id)))

	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// Both due secrets settled in the same sweep
	a, _ := f.keeper.GetSecret(f.ctx, secretA.Id)
	require.Equal(t, types.SECRET_STATUS_REVEALED, a.State)
	b, _ := f.keeper.GetSecret(f.ctx, secretB.Id)
	require.Equal(t, types.SECRET_STATUS_FAILED, b.State)
	// A's revealers each got half its pool; B's sole revealer got all of B's
	require.True(t, bank.received(guardiansA[0]).Equal(math.NewInt(50_000)))
	require.True(t, bank.received(guardiansB[0]).Equal(math.NewInt(200_000)))

	// The stale entry is gone and the terminal secret untouched
	still, _ := f.keeper.GetSecret(f.ctx, stale.Id)
	require.Equal(t, types.SECRET_STATUS_CANCELLED, still.State)

	assertInvariants(t, f) // also proves the stale entry was dequeued
}

// F4: cross-secret isolation — a guardian early-slashed on secret A keeps its
// separate bond on secret B, reveals B normally, and every float movement is
// exact.
func TestConformance_F4_CrossSecretSlashIsolation(t *testing.T) {
	bank := newTrackingBankKeeper()
	f := initFixtureWithBank(t, bank)
	srv := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	bond := testFloatUnit()
	creator := sdk.AccAddress([]byte("f4_creator__________")).String()

	// Secret A: the shared guardian + a partner, pre-window (reportable)
	secretAId, guardiansA, evidenceA := multiGuardianPendingSecret(t, f, 2, 2, creator, bond)
	shared := guardiansA[0]

	// Secret B: the SAME shared guardian + a fresh partner. Give the shared
	// guardian a second bond's lock and stake to carry both secrets.
	sharedGuardian, _ := f.keeper.GetGuardian(f.ctx, shared)
	sharedGuardian.Stake = &sdk.Coin{Denom: types.DefaultDenom, Amount: bond.MulRaw(3)} // 2 locked + 1 spare
	sharedGuardian.LockedStake = &sdk.Coin{Denom: types.DefaultDenom, Amount: bond.MulRaw(2)}
	sharedGuardian.ActiveBondCount = 2 // one active bond per secret carried
	require.NoError(t, f.keeper.SetGuardian(f.ctx, sharedGuardian))

	partnerB := sdk.AccAddress([]byte("f4_partner_b________")).String()
	setupSlashableGuardian(t, f, partnerB, bond)
	secretB := types.Secret{
		Id:                  types.GenerateValidSecretID(),
		Creator:             creator,
		Threshold:           2,
		MinShares:           2,
		MaxShares:           2,
		State:               types.SECRET_STATUS_PENDING,
		CreatedAt:           height(f) - 10,
		CommitDeadline:      height(f) - 5,
		RevealStartBlock:    height(f) + 50,
		RevealEndBlock:      height(f) + 150,
		RewardPool:          sdk.NewCoin(types.DefaultDenom, math.NewInt(300_000)),
		GuardianBondAmounts: repeatBond(bond, 2),
		Bump:                types.MinBump,
		SelectedGuardians:   []string{shared, partnerB},
		AcceptedCount:       2,
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secretB))
	// Real HMAC so the shared guardian can genuinely reveal on B
	shareB := []byte("secret_b_share_for_shared_guardian_32bytes____")
	writeShareRecord(t, f, secretB.Id, shared, []byte("encrypted_share_data"),
		generateTestHMAC(secretB.Id, shared, shareB))
	writeAssignmentRecord(t, f, secretB.Id, shared, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	writeShareRecord(t, f, secretB.Id, partnerB, []byte("encrypted_share_data"), nil)
	writeAssignmentRecord(t, f, secretB.Id, partnerB, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED, 0)
	enqueueForState(t, f, secretB)

	// Early-slash the shared guardian on A (full bond deducted)
	_, err := srv.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
		GuardianAddress: shared,
		ReporterAddress: sdk.AccAddress([]byte("f4_reporter_________")).String(),
		Reason:          "early reveal on A",
		Evidence:        evidenceA[0],
		SecretId:        secretAId,
	})
	require.NoError(t, err)

	afterSlash, _ := f.keeper.GetGuardian(f.ctx, shared)
	require.True(t, bond.MulRaw(2).Equal(afterSlash.Stake.Amount), "only A's bond may leave the float")
	require.True(t, bond.Equal(afterSlash.LockedStake.Amount), "B's bond must remain locked")

	// The shared guardian reveals B in B's window and is paid normally
	setHeight(f, secretB.RevealStartBlock)
	_, err = srv.GuardianRevealShare(f.ctx, &types.MsgGuardianRevealShare{
		Guardian: shared, SecretId: secretB.Id, DecryptedShare: shareB,
	})
	require.NoError(t, err, "the A-slash must not impair revealing on B")

	setHeight(f, secretB.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// B settles: shared guardian gets B's bond back + the full pool (partner
	// no-showed); its final float shows exactly one lost bond (A's)
	final, _ := f.keeper.GetGuardian(f.ctx, shared)
	require.True(t, final.LockedStake.IsZero())
	require.True(t, bond.MulRaw(2).Equal(final.Stake.Amount))
	require.True(t, bank.received(shared).Equal(secretB.RewardPool.Amount),
		"the shared guardian takes B's whole pool as its only revealer")

	// Settle A too (its window is later): the shared guardian is excluded,
	// A's partner no-showed → pool refunded
	secretA, _ := f.keeper.GetSecret(f.ctx, secretAId)
	setHeight(f, secretA.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	assertInvariants(t, f)
}

// F5: the reveal-horizon cap H (= MaxAvailabilityWindow). A window ending
// exactly H blocks out validates AND is staffable — a guardian registered one
// block earlier with the maximum availability window reaches precisely that
// far, so the cap is exactly reachable with zero slack. H + 1 is rejected by
// validation itself.
//
// RESOLVED (July 2026 ruling): H was 10 years while guardian availability is
// capped at 1 year, so any distance beyond ~1 year was unsatisfiable. H is
// now the availability cap; far-future reveals use cancel/recreate cycles
// (dead-man's handle) until guardian handoff/bond-transfer mechanics exist.
func TestConformance_F5_HorizonCapBoundary(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	// Register with the maximum window: from = 101, until = 101 + MaxAvailabilityWindow
	// — exactly the reach a horizon-H secret created at 101 needs.
	for _, name := range []string{"f5_guardian_a", "f5_guardian_b", "f5_guardian_c"} {
		dep := sdk.NewCoin(types.DefaultDenom, testFloatUnit().MulRaw(4))
		_, err := msgServer.GuardianRegister(f.ctx, &types.MsgGuardianRegister{
			Guardian:            sdk.AccAddress([]byte(fmt.Sprintf("%-20s", name))).String(),
			EncryptionPublicKey: getValidPublicKey(name + "_enckey"),
			AvailableFrom:       0,
			AvailableUntil:      types.MaxAvailabilityWindow + 1,
			Deposit:             &dep,
			AcceptingSecrets:    true,
		})
		require.NoError(t, err)
	}
	setHeight(f, height(f)+1)

	// horizon = startOffset + duration (reveal_end_block offset from creation)
	duration := types.MaxRevealDuration

	// Exactly H: validates and selection succeeds — the freshly registered
	// guardians' availability reaches reveal_end_block with zero slack
	offsetAtH := types.MaxRevealHorizon - duration
	_, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 2, 2, offsetAtH, duration)
	require.NoError(t, err,
		"a horizon of exactly H must validate and be staffable by a max-availability guardian")

	// H + 1: rejected by the horizon validation itself
	_, err = requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 2, 2, offsetAtH+1, duration)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reveal window ends too far in the future")

	assertInvariants(t, f)
}

// ─────────────────────────────────────────────────────────────────────────────
// G. Dynamic bond economics (DONE_DYNAMIC_BOND_ECONOMICS_PLAN §5)
// ─────────────────────────────────────────────────────────────────────────────

// G1: the entry fee is charged straight into the fee collector — the one
// validator payment pipe (it rides the next block's 90/10 split; ruled July
// 2026) — never bare-sent to the distribution module account, and it never
// joins the float.
func TestConformance_G1_EntryFeeRoutedToValidators(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	deposit := testFloatUnit()
	addr := registerConformanceGuardian(t, f, msgServer, "g1_fee_guardian", deposit)

	require.True(t, types.EntryFee().Equal(bank.receivedByModule(authtypes.FeeCollectorName)),
		"the whole entry fee must reach the fee collector")
	require.True(t, bank.forwardedToModule(distrtypes.ModuleName).IsZero(),
		"nothing may be bare-sent to the distribution module account (unaccounted = unwithdrawable)")
	require.True(t, bank.burnt.IsZero(),
		"registration itself burns nothing — the fee's 10%% burn happens at the next block's split")

	// The fee is not part of the float: the guardian's float is the deposit alone
	g, found := f.keeper.GetGuardian(f.ctx, addr)
	require.True(t, found)
	require.True(t, deposit.Equal(g.Stake.Amount), "the entry fee must never join the float")
	require.Equal(t, types.InitialBondK, g.BondK, "a new registrant starts at the k floor")
	require.Zero(t, g.ActiveBondCount)

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// G2: k rises on a no-reveal slash at settlement and falls (more slowly) on a
// correct reveal — the event-triggered curve, applied through the real
// handlers rather than the pure function.
func TestConformance_G2_BondKMovesOnSlashAndReveal(t *testing.T) {
	f := initFixture(t)
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

	// Two reveal, one no-shows. Every guardian starts at the floor, so the
	// revealers' k cannot fall further — it stays clamped at the floor.
	setHeight(f, secret.RevealStartBlock)
	for _, g := range guardians[:2] {
		require.NoError(t, conformanceReveal(t, f, msgServer, secretId, g))
		after, _ := f.keeper.GetGuardian(f.ctx, g)
		require.Equal(t, types.MinBondK, after.BondK,
			"a reveal at the floor must clamp, never dip below it")
	}

	setHeight(f, secret.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	// The no-show's k climbed one step; the revealers' stayed at the floor
	noShow, _ := f.keeper.GetGuardian(f.ctx, guardians[2])
	require.Equal(t, types.NextBondKAfterSlash(types.MinBondK), noShow.BondK,
		"a no-reveal slash must step k up by exactly one multiplier")
	require.Greater(t, noShow.BondK, types.MinBondK)
	for _, g := range guardians[:2] {
		revealer, _ := f.keeper.GetGuardian(f.ctx, g)
		require.Equal(t, types.MinBondK, revealer.BondK)
	}

	// Every bond slot was released, whichever way the guardian settled
	for _, g := range guardians {
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.Zero(t, guardian.ActiveBondCount, "settlement must release the active-bond slot")
	}

	assertInvariants(t, f)
}

// G3: a slashed guardian's HIGHER k prices its next secret's bond above a
// clean guardian's — the per-guardian bond variability spec.md documents as
// an intentional property.
func TestConformance_G3_HigherKPricesABiggerBond(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	clean := registerConformanceGuardian(t, f, msgServer, "g3_clean_guardian", testFloatUnit())
	punished := registerConformanceGuardian(t, f, msgServer, "g3_punished_guardian", testFloatUnit())
	for i := 0; i < 3; i++ {
		registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("g3_filler_%02d", i), testFloatUnit())
	}
	setHeight(f, height(f)+1)

	// Two slashes lift the punished guardian's k well clear of the floor
	require.NoError(t, f.keeper.AdjustBondKOnSlash(f.ctx, punished))
	require.NoError(t, f.keeper.AdjustBondKOnSlash(f.ctx, punished))
	punishedK := types.NextBondKAfterSlash(types.NextBondKAfterSlash(types.MinBondK))

	secretId, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 3, 3, 400, 150)
	require.NoError(t, err)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	distance := (secret.RevealEndBlock + 1) - secret.CommitDeadline

	cleanBond, ok := secret.GuardianBondAmount(clean)
	require.True(t, ok, "the clean guardian must have been selected")
	punishedBond, ok := secret.GuardianBondAmount(punished)
	require.True(t, ok, "the punished guardian must have been selected")

	require.Equal(t, types.BondAmount(distance, types.MinBump, types.MinBondK), cleanBond)
	require.Equal(t, types.BondAmount(distance, types.MinBump, punishedK), punishedBond)
	require.True(t, punishedBond.GT(cleanBond),
		"two guardians on the SAME secret owe different bonds — priced by their own k")

	// Acceptance locks each guardian's own frozen amount, not a shared one
	require.NoError(t, acceptAs(t, f, msgServer, secretId, punished))
	locked, _ := f.keeper.GetGuardian(f.ctx, punished)
	require.Equal(t, punishedBond, locked.LockedStake.Amount)

	assertInvariants(t, f)
}

// G4: the per-guardian concurrency cap is a hard eligibility gate at BOTH
// points — a guardian at the cap is not a selection candidate, and if it
// somehow reaches confirmation it is rejected there with no partial state.
func TestConformance_G4_ConcurrencyCapGate(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	// One saturated guardian plus exactly enough clean ones to staff a secret
	saturated := registerConformanceGuardian(t, f, msgServer, "g4_saturated", testFloatUnit())
	clean := map[string]bool{}
	for i := 0; i < 4; i++ {
		clean[registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("g4_clean_%02d", i), testFloatUnit())] = true
	}
	setHeight(f, height(f)+1)

	g, _ := f.keeper.GetGuardian(f.ctx, saturated)
	g.ActiveBondCount = types.MaxActiveBondsPerGuardian
	require.NoError(t, f.keeper.SetGuardian(f.ctx, g))

	// Selection gate: the saturated guardian is not a candidate
	secretId, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 3, 3, 400, 150)
	require.NoError(t, err)
	secret, _ := f.keeper.GetSecret(f.ctx, secretId)
	for _, addr := range secret.SelectedGuardians {
		require.True(t, clean[addr],
			"a guardian at the concurrency cap must not be selected (got %s)", addr)
	}

	// Confirmation gate: force the saturated guardian onto the secret as if it
	// had been selected while below the cap, then hit the cap before it
	// confirms — the moment the bond would actually lock.
	racer := secret.SelectedGuardians[0]
	r, _ := f.keeper.GetGuardian(f.ctx, racer)
	r.ActiveBondCount = types.MaxActiveBondsPerGuardian
	require.NoError(t, f.keeper.SetGuardian(f.ctx, r))

	err = acceptAs(t, f, msgServer, secretId, racer)
	require.Error(t, err, "confirmation at the cap must be rejected")
	require.Contains(t, err.Error(), "concurrency cap")

	// No partial state: nothing locked, the assignment still open, no slot taken
	after, _ := f.keeper.GetGuardian(f.ctx, racer)
	require.True(t, after.LockedStake.IsZero(), "a rejected confirmation must lock nothing")
	require.Equal(t, types.MaxActiveBondsPerGuardian, after.ActiveBondCount,
		"a rejected confirmation must not consume a slot")
	require.Equal(t, types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED,
		assignmentStatus(t, f, secretId, racer))
	unchanged, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Zero(t, unchanged.AcceptedCount)
}

// ─────────────────────────────────────────────────────────────────────────────
// H. Variable quorum — the [min, max] band (docs/spec.md "The [min_shares,
// max_shares] band"; docs/planning/PENDING_VARIABLE_QUORUM_PLAN.md)
// ─────────────────────────────────────────────────────────────────────────────

// hBandSecret registers count fresh guardians and requests a band secret over
// them, returning the secret and its selected candidates.
func hBandSecret(t *testing.T, f *fixture, msgServer types.MsgServer, prefix string, count int, threshold, minShares, maxShares int64) (string, types.Secret) {
	t.Helper()
	for i := 0; i < count; i++ {
		registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("%s_g%02d", prefix, i), testFloatUnit())
	}
	setHeight(f, height(f)+1)
	secretId, err := requestConformanceSecret(t, f, msgServer, types.MinBump, threshold, minShares, maxShares, 400, 150)
	require.NoError(t, err)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, int(maxShares), len(secret.SelectedGuardians),
		"selection must draw exactly max_shares candidates")
	return secretId, secret
}

// H1: activation at the band floor — exactly min_shares accept, the deadline
// activates with that exact set, and the never-confirmed candidate never
// locks a bond.
func TestConformance_H1_ActivationAtMin(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId, secret := hBandSecret(t, f, msgServer, "h1", 4, 2, 2, 3)

	require.NoError(t, acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[0]))
	require.NoError(t, acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[1]))
	finaliseCommitDeadline(t, f, secretId)

	activated, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_PENDING, activated.State)
	require.Equal(t, int64(2), activated.AcceptedCount, "the accepted set IS the active set")

	// The idle candidate never bonded and stays PROPOSED forever
	idle := secret.SelectedGuardians[2]
	idleGuardian, _ := f.keeper.GetGuardian(f.ctx, idle)
	require.True(t, idleGuardian.LockedStake.IsZero(), "a never-confirming candidate never locks a bond")
	require.Equal(t, types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED, assignmentStatus(t, f, secretId, idle))

	assertInvariants(t, f)
}

// H2: confirmation stays open after lock-in — candidates keep joining up to
// max_shares until the deadline, and the roster finalises with everyone.
func TestConformance_H2_ConfirmationsAfterLockIn(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId, secret := hBandSecret(t, f, msgServer, "h2", 4, 2, 2, 3)

	// min_shares accept: locked in, but confirmation stays open
	require.NoError(t, acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[0]))
	require.NoError(t, acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[1]))
	lockedIn, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_AWAITING_ACCEPTANCE, lockedIn.State,
		"lock-in is inferred, never a state change")

	// The third candidate joins after lock-in — no race, nobody turned away
	require.NoError(t, acceptAs(t, f, msgServer, secretId, secret.SelectedGuardians[2]))
	finaliseCommitDeadline(t, f, secretId)

	activated, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_PENDING, activated.State)
	require.Equal(t, int64(3), activated.AcceptedCount, "everyone in the band who confirmed is active")

	assertInvariants(t, f)
}

// H3: failure below the band floor — fewer than min_shares by the deadline
// fails the secret, releases every accepted bond, and refunds the pool in
// full (the no-fault exit, unchanged in kind from the old commit-timeout).
func TestConformance_H3_FailureBelowMin(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId, secret := hBandSecret(t, f, msgServer, "h3", 5, 3, 3, 4)

	// Only one acceptance — below min_shares = 3
	accepted := secret.SelectedGuardians[0]
	require.NoError(t, acceptAs(t, f, msgServer, secretId, accepted))
	finaliseCommitDeadline(t, f, secretId)

	failed, _ := f.keeper.GetSecret(f.ctx, secretId)
	require.Equal(t, types.SECRET_STATUS_FAILED, failed.State)

	// The pool refunds in full — nobody saw the job through — but the single
	// guardian that accepted is reimbursed for accepting, and only the slices
	// nobody earned travel back with the pool
	acceptFee := types.PerGuardianAcceptFee(secret.AcceptFees.Amount, secret.MaxShares)
	require.True(t, acceptFee.Equal(bank.received(accepted)),
		"the one guardian that accepted is reimbursed even though the secret never activated")
	unearnedFees := acceptFee.MulRaw(secret.MaxShares - 1)
	require.True(t, secret.RewardPool.Amount.Add(unearnedFees).Equal(bank.received(secret.Creator)),
		"failure below min_shares must refund the pool in full, plus every accept slice nobody earned")
	acceptedGuardian, _ := f.keeper.GetGuardian(f.ctx, accepted)
	require.True(t, acceptedGuardian.LockedStake.IsZero(), "the accepted bond is released, no fault")

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// H4: the fixed pool splits among actual revealers — a secret priced on
// max_shares that activates below the ceiling pays each revealer MORE than
// the per-slot slice; the creator is never refunded the unfilled slots.
func TestConformance_H4_FixedPoolSplitUnderVariableAcceptance(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	secretId, secret := hBandSecret(t, f, msgServer, "h4", 4, 2, 2, 3)
	require.True(t, types.RewardPoolAmount((secret.RevealEndBlock+1)-secret.CommitDeadline, 3, types.MinBump).
		Equal(secret.RewardPool.Amount), "the pool prices the band ceiling")

	// Two of three accept; the roster finalises at 2
	active := []string{secret.SelectedGuardians[0], secret.SelectedGuardians[1]}
	for _, g := range active {
		require.NoError(t, acceptAs(t, f, msgServer, secretId, g))
	}
	finaliseCommitDeadline(t, f, secretId)

	// Both reveal; settlement splits the FIXED max-priced pool between the two
	setHeight(f, secret.RevealStartBlock)
	for _, g := range active {
		require.NoError(t, conformanceReveal(t, f, msgServer, secretId, g))
	}
	setHeight(f, secret.RevealEndBlock+1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	perRevealer := secret.RewardPool.Amount.QuoRaw(2)
	perSlot := secret.RewardPool.Amount.QuoRaw(3)
	require.True(t, perRevealer.GT(perSlot), "an unfilled slot enriches the revealers")
	// The acceptance reimbursement follows the same test as the pool: earned
	// by the guardians that revealed, and only theirs — the unfilled slot's
	// slice goes back to the creator, since nobody accepted against it
	acceptFee := types.PerGuardianAcceptFee(secret.AcceptFees.Amount, secret.MaxShares)
	for _, g := range active {
		require.True(t, perRevealer.Add(acceptFee).Equal(bank.received(g)),
			"each revealer takes an equal split of the FIXED pool (no unfilled-slot refund), plus its acceptance reimbursement")
		guardian, _ := f.keeper.GetGuardian(f.ctx, g)
		require.True(t, guardian.LockedStake.IsZero(), "settlement releases the bond")
	}
	// The fixed-pool rule is unchanged: no part of the POOL comes back for an
	// unfilled slot. The acceptance reimbursement is not the pool — it pays
	// for a transaction that, in the unfilled slot's case, nobody sent — so
	// that one slice, and only that, returns.
	require.True(t, acceptFee.Equal(bank.received(secret.Creator)),
		"the creator is refunded the unfilled slot's accept slice, and no part of the pool")

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// H5: the gap bound is enforced on the message path — a band as wide as the
// threshold is rejected before any state is touched.
func TestConformance_H5_GapBoundRejectedAtHandler(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	for i := 0; i < 5; i++ {
		registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("h5_g%02d", i), testFloatUnit())
	}
	setHeight(f, height(f)+1)

	_, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 2, 4, 400, 150)
	require.Error(t, err, "gap 2 with threshold 2 violates the strict bound")
	require.Contains(t, err.Error(), "strictly below threshold")

	assertInvariants(t, f)
}

// ─────────────────────────────────────────────────────────────────────────────
// I. Creation fee (DONE_CREATION_FEE_PLAN §5)
// ─────────────────────────────────────────────────────────────────────────────

// findEvent returns the LAST event of the given type as an attribute map —
// scenarios emit one reservation event per created secret.
func findEvent(t *testing.T, f *fixture, eventType string) map[string]string {
	t.Helper()
	var attrs map[string]string
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type != eventType {
			continue
		}
		attrs = map[string]string{}
		for _, a := range ev.Attributes {
			attrs[a.Key] = a.Value
		}
	}
	require.NotNil(t, attrs, "no %s event emitted", eventType)
	return attrs
}

// I1: a floor-priced secret (small pool) charges exactly the gas-denominated
// floor into the fee collector — never module escrow — and the reservation
// event reports the fee and the floor regime.
func TestConformance_I1_CreationFeeFloorRegime(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	for i := range 3 {
		registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("i1_g%02d", i), testFloatUnit())
	}
	setHeight(f, height(f)+1)
	feeCollectorBefore := bank.receivedByModule(authtypes.FeeCollectorName)

	// Small shape: distance ≈ 551 blocks, 3 guardians, bump 1 — the curve
	// fee is tiny, so the 60,000 uveil floor prices the draw.
	secretId, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 2, 3, 400, 150)
	require.NoError(t, err)

	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	distance := (secret.RevealEndBlock + 1) - secret.CommitDeadline
	// The curve prices the pool's TIME component only — never the gas
	// reimbursements the creator also funds
	wantFee := types.CreationFee(types.TimeComponentAmount(distance, secret.MaxShares, secret.Bump), distance)
	require.True(t, types.CreationFeeFloor().Equal(wantFee), "this shape must be floor-priced")

	charged := bank.receivedByModule(authtypes.FeeCollectorName).Sub(feeCollectorBefore)
	require.True(t, wantFee.Equal(charged),
		"the exact creation fee must reach the fee collector (want %s, got %s)", wantFee, charged)

	ev := findEvent(t, f, types.EventTypeSecretReserved)
	require.Equal(t, wantFee.String()+types.DefaultDenom, ev["creation_fee"])
	require.Equal(t, "floor", ev["creation_fee_regime"])

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// I2: a long secret prices on the percentage curve — the event reports the
// percent regime and the exact curve amount is charged.
func TestConformance_I2_CreationFeePercentRegime(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)

	// Deep floats so the long secret's bonds are affordable
	for i := range 3 {
		registerConformanceGuardian(t, f, msgServer, fmt.Sprintf("i2_g%02d", i), testFloatUnit().MulRaw(1_000))
	}
	setHeight(f, height(f)+1)
	feeCollectorBefore := bank.receivedByModule(authtypes.FeeCollectorName)

	// Big shape inside the helper guardians' availability window: ~6 days
	// of distance at bump 10 — the 5.5%-ish curve fee is ~4× the floor.
	secretId, err := requestConformanceSecret(t, f, msgServer, types.MaxBump, 2, 2, 3, 90_000, 1_000)
	require.NoError(t, err)

	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	distance := (secret.RevealEndBlock + 1) - secret.CommitDeadline
	// The curve prices the pool's TIME component only — never the gas
	// reimbursements the creator also funds
	wantFee := types.CreationFee(types.TimeComponentAmount(distance, secret.MaxShares, secret.Bump), distance)
	require.True(t, wantFee.GT(types.CreationFeeFloor()), "this shape must be curve-priced")

	charged := bank.receivedByModule(authtypes.FeeCollectorName).Sub(feeCollectorBefore)
	require.True(t, wantFee.Equal(charged),
		"the exact creation fee must reach the fee collector (want %s, got %s)", wantFee, charged)

	ev := findEvent(t, f, types.EventTypeSecretReserved)
	require.Equal(t, "percent", ev["creation_fee_regime"])

	assertInvariants(t, f)
	assertSolvency(t, f, bank)
}

// I3: a failed request charges nothing — selection precedes the fee charge,
// so a draw that cannot fill leaves the fee collector untouched.
func TestConformance_I3_FailedRequestChargesNoFee(t *testing.T) {
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 100)
	// No guardians registered at all: selection must fail.

	_, err := requestConformanceSecret(t, f, msgServer, types.MinBump, 2, 2, 3, 400, 150)
	require.Error(t, err, "selection must fail with no registered guardians")
	require.True(t, bank.receivedByModule(authtypes.FeeCollectorName).IsZero(),
		"a failed request must charge no creation fee")
	require.True(t, bank.escrowedIn.IsZero(), "a failed request must lock no pool")

	assertInvariants(t, f)
}
