package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
	"github.com/timeflareio/crypto/go"
)

// Recipient rebate (docs/spec.md "Recipient Rebate"). The arithmetic is pinned
// in x/secrets/types; what is asserted here is the keeper's side: accrual from
// the pool balance, crediting at settlement, reservation accounting, the
// recipiency proof, and the release of an uncollected rebate at prune.

const (
	uveilPerVeil  = int64(1_000_000)
	testPoolStart = int64(700_000_000) * uveilPerVeil // the genesis rebate pool
)

// rebateBank is a bank mock with a settable rebate-pool balance that records
// what the module pays out, so a test can assert the money moved and where.
type rebateBank struct {
	mockBankKeeper
	poolBalance int64
	payments    []rebatePayment
}

type rebatePayment struct {
	module    string
	recipient string
	amount    int64
}

func (b *rebateBank) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	if addr.Equals(authModuleAddress(types.RebatePoolName)) {
		return sdk.NewCoin(denom, math.NewInt(b.poolBalance))
	}
	return b.mockBankKeeper.GetBalance(ctx, addr, denom)
}

func (b *rebateBank) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	if senderModule == types.RebatePoolName {
		b.payments = append(b.payments, rebatePayment{
			module:    senderModule,
			recipient: recipientAddr.String(),
			amount:    amt.AmountOf(types.DefaultDenom).Int64(),
		})
		b.poolBalance -= amt.AmountOf(types.DefaultDenom).Int64()
	}
	return nil
}

func authModuleAddress(name string) sdk.AccAddress {
	return mockAuthKeeper{}.GetModuleAddress(name)
}

// revealedSecret builds a settled, revealed secret carrying the fixture's real
// detection hint, alongside the recipiency proof its recipient would compute.
func revealedSecret(t *testing.T, rewardPool, acceptFees int64, revealedCount, maxShares int64) (types.Secret, []byte) {
	t.Helper()

	hint := testDetectionHint()
	secret := types.Secret{
		Id:            types.GenerateValidSecretID(),
		Creator:       sdk.AccAddress([]byte("rebate_test_creator__")).String(),
		DetectionHint: hint,
		State:         types.SECRET_STATUS_REVEALED,
		Threshold:     2,
		MinShares:     2,
		MaxShares:     maxShares,
		RevealedCount: revealedCount,
		RewardPool:    sdk.NewCoin(types.DefaultDenom, math.NewInt(rewardPool)),
		AcceptFees:    sdk.NewCoin(types.DefaultDenom, math.NewInt(acceptFees)),
	}
	return secret, recipiencyProofFor(t, secret)
}

func rebateFixture(t *testing.T, poolBalance int64) (*fixture, *rebateBank) {
	t.Helper()
	bank := &rebateBank{poolBalance: poolBalance}
	return initFixtureWithBank(t, bank), bank
}

// ── accrual ─────────────────────────────────────────────────────────────────

// An unset clock accrues from genesis, bounded by the burst cap. Treating the
// first touch as clock-setting only — which an earlier version did — swallowed
// the first eligible secret's rebate on every network, because the settlement
// that would credit it was itself the first touch.
func TestAccrueRebateAllowance_UnsetClockAccruesFromGenesis(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)
	perBlock := types.RebateAccrualPerBlock(math.NewInt(testPoolStart)).Int64()

	state := f.keeper.AccrueRebateAllowance(f.ctx, types.RebateState{}, 1_000)
	require.Equal(t, 1_000*perBlock, state.Allowance, "an unset clock accrues from genesis")
	require.Equal(t, int64(1_000), state.AccruedHeight)

	// However long the gap, the burst cap bounds it — so accruing from genesis
	// can never hand out more than a day's worth.
	deep := f.keeper.AccrueRebateAllowance(f.ctx, types.RebateState{}, 10_000_000)
	require.Equal(t, types.RebateAllowanceCap(math.NewInt(testPoolStart)).Int64(), deep.Allowance)
}

func TestAccrueRebateAllowance_LazyAcrossIdleBlocks(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)
	perBlock := types.RebateAccrualPerBlock(math.NewInt(testPoolStart)).Int64()

	state := f.keeper.AccrueRebateAllowance(f.ctx, types.RebateState{AccruedHeight: 100}, 110)
	require.Equal(t, 10*perBlock, state.Allowance, "ten idle blocks accrue ten blocks' allowance")
	require.Equal(t, int64(110), state.AccruedHeight)

	// A month of idle blocks is still capped at one day.
	state = f.keeper.AccrueRebateAllowance(f.ctx, types.RebateState{AccruedHeight: 1}, 1+types.RebateBurstBlocks*30)
	require.Equal(t, types.RebateAllowanceCap(math.NewInt(testPoolStart)).Int64(), state.Allowance)
}

// Reservations are not spendable: the accrual rate must follow what the pool
// can actually pay, or a credited-but-uncollected rebate would be counted
// twice.
func TestRebatePoolBalance_NetsReservations(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	reserved := int64(50_000) * uveilPerVeil
	balance := f.keeper.RebatePoolBalance(f.ctx, types.RebateState{Reserved: reserved})
	require.Equal(t, testPoolStart-reserved, balance.Int64())

	// Reservations exceeding the balance never yield a negative rate.
	balance = f.keeper.RebatePoolBalance(f.ctx, types.RebateState{Reserved: testPoolStart * 2})
	require.True(t, balance.IsZero())
}

// ── crediting at settlement ─────────────────────────────────────────────────

func TestCreditRebates_CreditsRevealedSecretAtTheRatio(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	// A day of allowance is available, so the 30% ratio is the binding term.
	primed := types.RebateState{
		Allowance:     types.RebateAllowanceCap(math.NewInt(testPoolStart)).Int64(),
		AccruedHeight: 500,
	}
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, primed))

	secret, _ := revealedSecret(t, 1_000*uveilPerVeil, 100*uveilPerVeil, 2, 4)
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))

	require.NoError(t, f.keeper.CreditRebates(f.ctx, []types.Secret{secret}, 501))

	// Spend = pool + the two accept slices actually paid: 1000 + 2×25 = 1050.
	// 30% of that is 315 VEIL.
	stored, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, int64(315)*uveilPerVeil, stored.RebateAmount)
	require.False(t, stored.RebateCollected)

	state, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Equal(t, stored.RebateAmount, state.Reserved, "a credited rebate must be reserved")
	require.Equal(t, primed.Allowance-stored.RebateAmount, state.Allowance)
}

func TestCreditRebates_SkipsSecretsThatDidNotReveal(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{
		Allowance:     types.RebateAllowanceCap(math.NewInt(testPoolStart)).Int64(),
		AccruedHeight: 500,
	}))

	for _, state := range []string{types.SECRET_STATUS_FAILED, types.SECRET_STATUS_CANCELLED} {
		secret, _ := revealedSecret(t, 1_000*uveilPerVeil, 0, 0, 4)
		secret.State = state
		require.NoError(t, f.keeper.SetSecret(f.ctx, secret))

		require.NoError(t, f.keeper.CreditRebates(f.ctx, []types.Secret{secret}, 501))

		stored, err := f.keeper.GetSecret(f.ctx, secret.Id)
		require.NoError(t, err)
		require.Zero(t, stored.RebateAmount, "a %s secret must not be credited", state)
	}

	rebateState, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, rebateState.Reserved)
}

// A height's secrets divide the allowance equally, and a failed secret is
// counted in the divisor: dividing only among revealed secrets would leak,
// through the amounts, how many secrets at that height failed.
func TestCreditRebates_FailedSecretsStillCountInTheDivisor(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	// Accrued to the credit height, so no accrual lands mid-test and the
	// division is the only thing under assertion.
	allowance := int64(400) * uveilPerVeil
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Allowance: allowance, AccruedHeight: 501}))

	// Four settle; only one reveals. Its share is allowance ÷ 4, not the whole
	// allowance — and 100 VEIL is below the ratio ceiling, so the share binds.
	revealed, _ := revealedSecret(t, 10_000*uveilPerVeil, 0, 2, 4)
	settled := []types.Secret{revealed}
	require.NoError(t, f.keeper.SetSecret(f.ctx, revealed))
	for i := 0; i < 3; i++ {
		failed, _ := revealedSecret(t, 10_000*uveilPerVeil, 0, 0, 4)
		failed.State = types.SECRET_STATUS_FAILED
		require.NoError(t, f.keeper.SetSecret(f.ctx, failed))
		settled = append(settled, failed)
	}

	require.NoError(t, f.keeper.CreditRebates(f.ctx, settled, 501))

	stored, err := f.keeper.GetSecret(f.ctx, revealed.Id)
	require.NoError(t, err)
	require.Equal(t, allowance/4, stored.RebateAmount)
}

// The per-height bound: whatever the cluster, the credits at one height can
// never exceed the allowance that height had.
func TestCreditRebates_HeightNeverExceedsTheAllowance(t *testing.T) {
	for _, clusterSize := range []int{1, 2, 5, 40} {
		f, _ := rebateFixture(t, testPoolStart)
		allowance := int64(1_000) * uveilPerVeil
		require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Allowance: allowance, AccruedHeight: 501}))

		settled := make([]types.Secret, 0, clusterSize)
		for i := 0; i < clusterSize; i++ {
			// Expensive secrets, so the allowance share is always the binding term.
			secret, _ := revealedSecret(t, 1_000_000*uveilPerVeil, 0, 2, 4)
			require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
			settled = append(settled, secret)
		}

		require.NoError(t, f.keeper.CreditRebates(f.ctx, settled, 501))

		state, err := f.keeper.GetRebateState(f.ctx)
		require.NoError(t, err)
		require.LessOrEqual(t, state.Reserved, allowance,
			"a cluster of %d credited %d against an allowance of %d", clusterSize, state.Reserved, allowance)
		require.Equal(t, allowance-state.Reserved, state.Allowance, "allowance and reservations must conserve")
	}
}

func TestCreditRebates_DustSharesAreNotCredited(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	// Four settling secrets against an allowance of two dust floors: each
	// share is half a floor, so nothing is credited at all.
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{
		Allowance:     types.RebateDustFloor * 2,
		AccruedHeight: 501,
	}))

	settled := make([]types.Secret, 0, 4)
	for i := 0; i < 4; i++ {
		secret, _ := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
		require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
		settled = append(settled, secret)
	}

	require.NoError(t, f.keeper.CreditRebates(f.ctx, settled, 501))

	state, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, state.Reserved, "sub-dust shares must credit nothing")
	require.Equal(t, types.RebateDustFloor*2, state.Allowance, "and must leave the allowance untouched")
}

// Re-running the same credit pass on the same state must produce the same
// amounts: consensus requires it, and a validator replaying a block must not
// diverge.
func TestCreditRebates_IsDeterministic(t *testing.T) {
	credit := func() int64 {
		f, _ := rebateFixture(t, testPoolStart)
		require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{
			Allowance:     int64(700) * uveilPerVeil,
			AccruedHeight: 900,
		}))
		secret, _ := revealedSecret(t, 3_000*uveilPerVeil, 40*uveilPerVeil, 3, 5)
		require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
		require.NoError(t, f.keeper.CreditRebates(f.ctx, []types.Secret{secret}, 1_000))
		stored, err := f.keeper.GetSecret(f.ctx, secret.Id)
		require.NoError(t, err)
		return stored.RebateAmount
	}
	first := credit()
	require.Positive(t, first)
	for i := 0; i < 3; i++ {
		require.Equal(t, first, credit(), "the same inputs must credit the same amount")
	}
}

// ── collecting ──────────────────────────────────────────────────────────────

// commitFor records the commitment a collector must publish before revealing,
// at the height given, and returns the message that reveals it.
func commitFor(t *testing.T, f *fixture, ms types.MsgServer, secretID string, collector sdk.AccAddress, proof []byte, atHeight int64) *types.MsgRecipientCollectRebate {
	t.Helper()
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(atHeight)
	_, err := ms.RecipientCommitRebate(f.ctx, &types.MsgRecipientCommitRebate{
		Recipient:  collector.String(),
		SecretId:   secretID,
		Commitment: crypto.RebateCommitment(proof, collector.Bytes()),
	})
	require.NoError(t, err)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(atHeight + 1)
	return &types.MsgRecipientCollectRebate{
		Recipient: collector.String(),
		SecretId:  secretID,
		Z:         proof,
	}
}

func TestCollectRebate_PaysTheRecipientProvingRecipiency(t *testing.T) {
	f, bank := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	secret, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: secret.RebateAmount}))

	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))
	reveal := commitFor(t, f, msgServer, secret.Id, recipient, proof, 100)
	resp, err := msgServer.RecipientCollectRebate(f.ctx, reveal)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(300*uveilPerVeil).String(), resp.Amount)

	require.Len(t, bank.payments, 1)
	require.Equal(t, types.RebatePoolName, bank.payments[0].module)
	require.Equal(t, recipient.String(), bank.payments[0].recipient)
	require.Equal(t, 300*uveilPerVeil, bank.payments[0].amount)

	stored, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.True(t, stored.RebateCollected)

	state, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, state.Reserved, "paying a rebate must release its reservation")
}

func TestCollectRebate_Refusals(t *testing.T) {
	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))

	tests := []struct {
		name    string
		prepare func(secret *types.Secret)
		proofOf func(valid []byte) []byte
		wantErr error
	}{
		{
			name:    "a proof that does not derive the hint tag",
			prepare: func(s *types.Secret) { s.RebateAmount = 300 * uveilPerVeil },
			proofOf: func(valid []byte) []byte {
				wrong := append([]byte(nil), valid...)
				wrong[0] ^= 0x01
				return wrong
			},
			wantErr: types.ErrInvalidRecipiencyProof,
		},
		{
			name:    "a secret with no credited rebate",
			prepare: func(s *types.Secret) { s.RebateAmount = 0 },
			wantErr: types.ErrNoRebate,
		},
		{
			name: "a rebate already collected",
			prepare: func(s *types.Secret) {
				s.RebateAmount = 300 * uveilPerVeil
				s.RebateCollected = true
			},
			wantErr: types.ErrRebateAlreadyCollected,
		},
		{
			name: "a secret that never revealed",
			prepare: func(s *types.Secret) {
				s.State = types.SECRET_STATUS_PENDING
				s.RebateAmount = 0
			},
			wantErr: types.ErrNoRebate,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, bank := rebateFixture(t, testPoolStart)
			msgServer := keeper.NewMsgServerImpl(f.keeper)

			secret, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
			tc.prepare(&secret)
			require.NoError(t, f.keeper.SetSecret(f.ctx, secret))

			// Commit with the VALID proof first where the secret allows it, so
			// each case fails on the condition it names rather than on a
			// missing commitment.
			if secret.RebateAmount > 0 && !secret.RebateCollected {
				commitFor(t, f, msgServer, secret.Id, recipient, proof, 100)
			}
			if tc.proofOf != nil {
				proof = tc.proofOf(proof)
			}

			_, err := msgServer.RecipientCollectRebate(f.ctx, &types.MsgRecipientCollectRebate{
				Recipient: recipient.String(),
				SecretId:  secret.Id,
				Z:         proof,
			})
			require.ErrorIs(t, err, tc.wantErr)
			require.Empty(t, bank.payments, "a refused collection must move no funds")
		})
	}
}

// Collecting twice must pay once: the second attempt is refused after the
// first marked the rebate collected.
func TestCollectRebate_CannotBeCollectedTwice(t *testing.T) {
	f, bank := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	secret, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: secret.RebateAmount}))

	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))
	msg := commitFor(t, f, msgServer, secret.Id, recipient, proof, 100)

	_, err := msgServer.RecipientCollectRebate(f.ctx, msg)
	require.NoError(t, err)

	_, err = msgServer.RecipientCollectRebate(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrRebateAlreadyCollected)
	require.Len(t, bank.payments, 1, "the rebate must be paid exactly once")
}

// The reason commit–reveal exists: a stolen proof is worthless without a
// commitment made before the proof was public. This is the front-running
// attack, and it must fail.
func TestCollectRebate_StolenProofCannotBeFrontRun(t *testing.T) {
	f, bank := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	secret, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: secret.RebateAmount}))

	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))
	reveal := commitFor(t, f, msgServer, secret.Id, recipient, proof, 100)

	// A validator watching the mempool copies the proof into its own message.
	thief := sdk.AccAddress([]byte("rebate_frontrunner___"))
	_, err := msgServer.RecipientCollectRebate(f.ctx, &types.MsgRecipientCollectRebate{
		Recipient: thief.String(),
		SecretId:  secret.Id,
		Z:         proof,
	})
	require.ErrorIs(t, err, types.ErrNoRebateCommitment)
	require.Empty(t, bank.payments, "a stolen proof must move no funds")

	// Committing now does not save the thief either: the commitment cannot
	// predate the block it is made in, and this reveal lands in the same one.
	_, err = msgServer.RecipientCommitRebate(f.ctx, &types.MsgRecipientCommitRebate{
		Recipient:  thief.String(),
		SecretId:   secret.Id,
		Commitment: crypto.RebateCommitment(proof, thief.Bytes()),
	})
	require.NoError(t, err)
	_, err = msgServer.RecipientCollectRebate(f.ctx, &types.MsgRecipientCollectRebate{
		Recipient: thief.String(),
		SecretId:  secret.Id,
		Z:         proof,
	})
	require.ErrorIs(t, err, types.ErrCommitmentTooRecent)
	require.Empty(t, bank.payments)

	// The rightful collector, who committed a block earlier, still succeeds.
	_, err = msgServer.RecipientCollectRebate(f.ctx, reveal)
	require.NoError(t, err)
	require.Len(t, bank.payments, 1)
	require.Equal(t, recipient.String(), bank.payments[0].recipient)
}

// A commitment that does not correspond to the revealed proof is refused: an
// attacker cannot pre-commit to a guess and substitute a proof later.
func TestCollectRebate_CommitmentMustMatchTheProof(t *testing.T) {
	f, bank := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	secret, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: secret.RebateAmount}))

	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)
	_, err := msgServer.RecipientCommitRebate(f.ctx, &types.MsgRecipientCommitRebate{
		Recipient:  recipient.String(),
		SecretId:   secret.Id,
		Commitment: crypto.RebateCommitment([]byte("a wrong proof, thirty-two bytes.."), recipient.Bytes()),
	})
	require.NoError(t, err)

	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(101)
	_, err = msgServer.RecipientCollectRebate(f.ctx, &types.MsgRecipientCollectRebate{
		Recipient: recipient.String(),
		SecretId:  secret.Id,
		Z:         proof,
	})
	require.ErrorIs(t, err, types.ErrCommitmentMismatch)
	require.Empty(t, bank.payments)
}

// A commitment binds to ONE address: the same proof committed by A cannot be
// revealed by B, even a block later.
func TestCollectRebate_CommitmentBindsToOneAddress(t *testing.T) {
	f, bank := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	secret, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: secret.RebateAmount}))

	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))
	other := sdk.AccAddress([]byte("rebate_other_address_"))

	// The other address commits the RECIPIENT's commitment value — which does
	// not bind to it, because the address is hashed in.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)
	_, err := msgServer.RecipientCommitRebate(f.ctx, &types.MsgRecipientCommitRebate{
		Recipient:  other.String(),
		SecretId:   secret.Id,
		Commitment: crypto.RebateCommitment(proof, recipient.Bytes()),
	})
	require.NoError(t, err)

	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(101)
	_, err = msgServer.RecipientCollectRebate(f.ctx, &types.MsgRecipientCollectRebate{
		Recipient: other.String(),
		SecretId:  secret.Id,
		Z:         proof,
	})
	require.ErrorIs(t, err, types.ErrCommitmentMismatch)
	require.Empty(t, bank.payments)
}

// Committing requires something to collect: no rebate, or one already
// collected, and the commitment is refused outright.
func TestCommitRebate_Refusals(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))

	noRebate, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	require.NoError(t, f.keeper.SetSecret(f.ctx, noRebate))
	_, err := msgServer.RecipientCommitRebate(f.ctx, &types.MsgRecipientCommitRebate{
		Recipient:  recipient.String(),
		SecretId:   noRebate.Id,
		Commitment: crypto.RebateCommitment(proof, recipient.Bytes()),
	})
	require.ErrorIs(t, err, types.ErrNoRebate)

	collected, proof2 := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	collected.RebateAmount = 300 * uveilPerVeil
	collected.RebateCollected = true
	require.NoError(t, f.keeper.SetSecret(f.ctx, collected))
	_, err = msgServer.RecipientCommitRebate(f.ctx, &types.MsgRecipientCommitRebate{
		Recipient:  recipient.String(),
		SecretId:   collected.Id,
		Commitment: crypto.RebateCommitment(proof2, recipient.Bytes()),
	})
	require.ErrorIs(t, err, types.ErrRebateAlreadyCollected)
}

// ── expiry at prune ─────────────────────────────────────────────────────────

func TestReleaseRebateReservation(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	reserved := int64(300) * uveilPerVeil
	secret, _ := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = reserved
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: reserved}))

	require.NoError(t, f.keeper.ReleaseRebateReservation(f.ctx, secret))

	state, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, state.Reserved, "an uncollected rebate's reservation returns to the pool")
}

func TestReleaseRebateReservation_CollectedRebateIsNotReleasedTwice(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	secret, _ := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	secret.RebateCollected = true

	// PayRebate already released the reservation, so the pool holds none.
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: 0}))
	require.NoError(t, f.keeper.ReleaseRebateReservation(f.ctx, secret))

	state, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, state.Reserved, "a collected rebate must not be released again")
}

// ── wiring ──────────────────────────────────────────────────────────────────

// The full path: a real secret revealed through the lifecycle, settled by
// EndBlock, credited without any test poking the rebate state, then collected.
// Everything above tests a piece; this proves the pieces are connected.
func TestRebate_EndToEndThroughSettlement(t *testing.T) {
	f, bank := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardian := sdk.AccAddress([]byte("rebate_e2e_guardian__"))
	creator := sdk.AccAddress([]byte("rebate_e2e_creator___"))
	secretId, shareDataMap := setupTestSecretWithShare(t, f, msgServer, guardian, creator, "rebate_e2e")

	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)

	// Reveal enough shares to reconstruct.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)
	for i, guardianAddr := range acceptedGuardians(t, f, secretId) {
		if i >= int(secret.Threshold) {
			break
		}
		shareData, ok := shareDataMap[guardianAddr]
		require.True(t, ok)
		_, err := msgServer.GuardianRevealShare(f.ctx, &types.MsgGuardianRevealShare{
			Guardian:       guardianAddr,
			SecretId:       secretId,
			DecryptedShare: shareData.DecryptedShare,
		})
		require.NoError(t, err)
	}

	// Deliberately NO priming of the rebate state: this is a virgin chain, and
	// the settlement below is the first touch of the accrual clock. Priming it
	// here is exactly what hid the bug the devnet drill found — the first
	// eligible secret on every network was credited nothing.

	// The fixture's secret is devnet-scale: its protocol-derived pool is a few
	// thousand uveil, so 30% of it is dust and no rebate would be credited.
	// Raise the stored pool to a realistic year-long secret's invoice — the
	// only test-only adjustment here, and it changes nothing about the path
	// under test.
	priced, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	priced.RewardPool = sdk.NewCoin(types.DefaultDenom, math.NewInt(26*uveilPerVeil))
	require.NoError(t, f.keeper.SetSecret(f.ctx, priced))

	// Settle at reveal_end_block + 1, exactly as EndBlock does.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock + 1)
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))

	settled, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_REVEALED, settled.State)
	require.Positive(t, settled.RebateAmount, "settlement must credit the recipient a rebate")
	require.False(t, settled.RebateCollected)

	// The collection deadline is derived from terminal_at, so a zero there would
	// void the rebate on the very next block. Assert the stamp and the queue
	// entry that depends on it.
	require.Positive(t, settled.TerminalAt, "settlement must stamp terminal_at")
	has, err := f.keeper.RebateExpiryQueue.Has(f.ctx,
		collections.Join(types.RebateCollectionDeadline(settled.TerminalAt), secretId))
	require.NoError(t, err)
	require.True(t, has, "the credited rebate must be enqueued at its collection deadline")

	// The credited amount respects the ratio ceiling on the spend that
	// settlement actually paid out.
	require.LessOrEqual(t, settled.RebateAmount,
		types.RebateRatioOf(settled.RewardPool.Amount.Add(settled.AcceptFees.Amount)).Int64())

	rebateState, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Equal(t, settled.RebateAmount, rebateState.Reserved)

	// And the recipient can collect it. The hint on a lifecycle-built secret is
	// the fixture's, so recompute the proof the same way the recipient would.
	proof := recipiencyProofFor(t, settled)
	recipient := sdk.AccAddress([]byte("rebate_e2e_recipient_"))
	reveal := commitFor(t, f, msgServer, secretId, recipient, proof,
		sdk.UnwrapSDKContext(f.ctx).BlockHeight())
	resp, err := msgServer.RecipientCollectRebate(f.ctx, reveal)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(settled.RebateAmount).String(), resp.Amount)
	require.Len(t, bank.payments, 1)
	require.Equal(t, settled.RebateAmount, bank.payments[0].amount)

	collected, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.True(t, collected.RebateCollected)
}

// ── collection window ───────────────────────────────────────────────────────

// The window is explicit — three months from settlement — and it must always
// close before pruning takes the detection hint the proof is checked against.
func TestRebateCollectionWindowStaysInsideRetention(t *testing.T) {
	require.Less(t, types.RebateCollectionBlocks, types.RetentionBlocks,
		"a collection window outliving retention would promise a rebate the chain can no longer verify")
	require.Equal(t, int64(1_296_000), types.RebateCollectionBlocks, "three months at ~6s blocks")

	// And the clamp holds it inside a shorter retention override, so a devnet
	// cannot promise more than it can honour.
	t.Setenv(types.RetentionBlocksEnvVar, "60")
	require.Equal(t, int64(60), types.RebateCollectionWindow())
	require.Equal(t, int64(160), types.RebateCollectionDeadline(100))
}

func TestCollectRebate_RefusedAfterTheWindowCloses(t *testing.T) {
	f, bank := rebateFixture(t, testPoolStart)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	secret, proof := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	secret.TerminalAt = 1_000
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: secret.RebateAmount}))

	recipient := sdk.AccAddress([]byte("rebate_collector_addr"))
	deadline := types.RebateCollectionDeadline(secret.TerminalAt)

	// Committing after the deadline is refused outright.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(deadline + 1)
	_, err := msgServer.RecipientCommitRebate(f.ctx, &types.MsgRecipientCommitRebate{
		Recipient:  recipient.String(),
		SecretId:   secret.Id,
		Commitment: crypto.RebateCommitment(proof, recipient.Bytes()),
	})
	require.ErrorIs(t, err, types.ErrRebateExpired)

	// And so is revealing against a commitment made in time.
	reveal := commitFor(t, f, msgServer, secret.Id, recipient, proof, deadline-1)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(deadline + 1)
	_, err = msgServer.RecipientCollectRebate(f.ctx, reveal)
	require.ErrorIs(t, err, types.ErrRebateExpired)
	require.Empty(t, bank.payments)

	// The last block of the window still pays.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(deadline)
	_, err = msgServer.RecipientCollectRebate(f.ctx, reveal)
	require.NoError(t, err)
	require.Len(t, bank.payments, 1)
}

// At the deadline the rebate is voided and its reservation returns to the pool,
// so unclaimed adoption funding funds the next newcomer instead of sitting
// reserved until pruning.
func TestProcessExpiredRebates_VoidsAndReleases(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	secret, _ := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	secret.TerminalAt = 500
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: secret.RebateAmount}))
	deadline := types.RebateCollectionDeadline(secret.TerminalAt)
	require.NoError(t, f.keeper.RebateExpiryQueue.Set(f.ctx, collections.Join(deadline, secret.Id)))

	// A block before the deadline: nothing happens.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(deadline - 1)
	require.NoError(t, f.keeper.ProcessExpiredRebates(f.ctx))
	held, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, 300*uveilPerVeil, held.RebateAmount)

	// At the deadline it is voided.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(deadline)
	require.NoError(t, f.keeper.ProcessExpiredRebates(f.ctx))

	voided, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Zero(t, voided.RebateAmount, "an expired rebate is no longer credited")
	require.False(t, voided.RebateCollected, "and was never collected")

	state, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, state.Reserved, "its reservation returns to the pool")

	// Idempotent: a second pass finds nothing due and changes nothing.
	require.NoError(t, f.keeper.ProcessExpiredRebates(f.ctx))
	state, err = f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, state.Reserved)
}

// A collected rebate must not be voided or double-released when its deadline
// passes: the reservation is already gone.
func TestProcessExpiredRebates_LeavesCollectedRebatesAlone(t *testing.T) {
	f, _ := rebateFixture(t, testPoolStart)

	secret, _ := revealedSecret(t, 1_000*uveilPerVeil, 0, 2, 4)
	secret.RebateAmount = 300 * uveilPerVeil
	secret.RebateCollected = true
	secret.TerminalAt = 500
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.RebateState.Set(f.ctx, types.RebateState{Reserved: 0}))
	deadline := types.RebateCollectionDeadline(secret.TerminalAt)
	require.NoError(t, f.keeper.RebateExpiryQueue.Set(f.ctx, collections.Join(deadline, secret.Id)))

	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(deadline)
	require.NoError(t, f.keeper.ProcessExpiredRebates(f.ctx))

	kept, err := f.keeper.GetSecret(f.ctx, secret.Id)
	require.NoError(t, err)
	require.Equal(t, 300*uveilPerVeil, kept.RebateAmount, "a collected rebate keeps its record")
	state, err := f.keeper.GetRebateState(f.ctx)
	require.NoError(t, err)
	require.Zero(t, state.Reserved)
}
