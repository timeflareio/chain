package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// Invariant library (docs/planning/done/DONE_ECONOMICS_TEST_STRATEGY_PLAN.md §4).
//
// assertInvariants runs the state-side invariants (no stranded bonds, queue
// hygiene) against the fixture's full store — call it at the end of every
// conformance scenario and after every fuzzer block. assertSolvency adds the
// exact escrow identity and requires a ledgerBankKeeper plus a full-pipeline
// scenario (all funds entering through real messages).

// ledgerBankKeeper extends the tracking keeper with a model of the module
// escrow account, so solvency can be asserted exactly:
//
//	moduleBalance = escrowedIn − paidOut − forwarded − burnt
type ledgerBankKeeper struct {
	trackingBankKeeper
	escrowedIn  *math.Int // account → secrets-module transfers (deposits, pools)
	paidOut     *math.Int // module → account transfers (withdrawals, wages, refunds, bounties)
	forwarded   *math.Int // module → module transfers OUT of escrow
	forwardedTo map[string]math.Int
	receivedBy  map[string]math.Int // account → module transfers by recipient (entry fees to the fee collector)
}

func newLedgerBankKeeper() *ledgerBankKeeper {
	in, out, fwd := math.ZeroInt(), math.ZeroInt(), math.ZeroInt()
	return &ledgerBankKeeper{
		trackingBankKeeper: *newTrackingBankKeeper(),
		escrowedIn:         &in,
		paidOut:            &out,
		forwarded:          &fwd,
		forwardedTo:        make(map[string]math.Int),
		receivedBy:         make(map[string]math.Int),
	}
}

func (lb *ledgerBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	// Only transfers into the secrets module are escrow; the entry fee goes
	// straight to the fee collector and never touches module escrow.
	if recipientModule == types.ModuleName {
		*lb.escrowedIn = lb.escrowedIn.Add(amt.AmountOf(types.DefaultDenom))
	}
	cur, ok := lb.receivedBy[recipientModule]
	if !ok {
		cur = math.ZeroInt()
	}
	lb.receivedBy[recipientModule] = cur.Add(amt.AmountOf(types.DefaultDenom))
	return nil
}

// receivedByModule reports how much accounts have sent directly to a module
// account — the entry fee's route into the fee collector.
func (lb *ledgerBankKeeper) receivedByModule(module string) math.Int {
	if v, ok := lb.receivedBy[module]; ok {
		return v
	}
	return math.ZeroInt()
}

func (lb *ledgerBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	*lb.paidOut = lb.paidOut.Add(amt.AmountOf(types.DefaultDenom))
	return lb.trackingBankKeeper.SendCoinsFromModuleToAccount(ctx, senderModule, recipientAddr, amt)
}

func (lb *ledgerBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	if senderModule == types.ModuleName {
		*lb.forwarded = lb.forwarded.Add(amt.AmountOf(types.DefaultDenom))
		cur, ok := lb.forwardedTo[recipientModule]
		if !ok {
			cur = math.ZeroInt()
		}
		lb.forwardedTo[recipientModule] = cur.Add(amt.AmountOf(types.DefaultDenom))
	}
	return nil
}

// forwardedToModule reports how much this module has forwarded to another
// module account — the entry fee's route to validator rewards.
func (lb *ledgerBankKeeper) forwardedToModule(module string) math.Int {
	if v, ok := lb.forwardedTo[module]; ok {
		return v
	}
	return math.ZeroInt()
}

func (lb *ledgerBankKeeper) moduleBalance() math.Int {
	return lb.escrowedIn.Sub(*lb.paidOut).Sub(*lb.forwarded).Sub(*lb.burnt)
}

// assertInvariants runs the extracted state-side invariant library
// (keeper.CheckStateInvariants — invariants 3, 4, 4b, 5 and 6: no stranded
// bonds, commit/settlement queue hygiene, prune-queue hygiene, counter
// consistency and payload presence) against the fixture's full store. The
// same sweep runs after InitGenesis and inside upgrade migrations, so the
// tests and the wholesale-risk moments can never drift apart.
func assertInvariants(t *testing.T, f *fixture) {
	t.Helper()
	require.NoError(t, f.keeper.CheckStateInvariants(f.ctx))
}

// assertSolvency checks the exact escrow identity (invariants 1 + 2): the
// module account balance equals the sum of every non-terminal secret's reward
// pool AND escrowed accept fees, plus every guardian's float total. Both
// per-secret buckets are held until the secret's terminal state, so both are
// live obligations for exactly as long as the secret is. Requires a
// full-pipeline scenario — all funds must have entered escrow through real
// messages against a ledgerBankKeeper.
func assertSolvency(t *testing.T, f *fixture, bank *ledgerBankKeeper) {
	t.Helper()

	obligations := math.ZeroInt()
	require.NoError(t, f.keeper.Secrets.Walk(f.ctx, nil, func(id string, s types.Secret) (bool, error) {
		if !s.IsComplete() {
			obligations = obligations.Add(s.RewardPool.Amount).Add(s.AcceptFees.Amount)
		}
		return false, nil
	}))
	require.NoError(t, f.keeper.Guardians.Walk(f.ctx, nil, func(addr string, g types.Guardian) (bool, error) {
		if g.Stake != nil {
			obligations = obligations.Add(g.Stake.Amount)
		}
		return false, nil
	}))

	require.True(t, bank.moduleBalance().Equal(obligations),
		"invariant 1 (solvency): module escrow holds %s but protocol obligations total %s (pools of live secrets + guardian floats)",
		bank.moduleBalance(), obligations)
	require.False(t, bank.moduleBalance().IsNegative(), "invariant 2: module balance can never be negative")
}

// enqueueForState registers a directly-written test secret in the due-height
// queues exactly as production would have left them for its state: both
// entries pre-activation, settlement-only once active, none when terminal.
// Direct-state fixtures must call this to satisfy invariant 4.
func enqueueForState(t *testing.T, f *fixture, secret types.Secret) {
	t.Helper()
	switch secret.State {
	case types.SECRET_STATUS_RESERVED, types.SECRET_STATUS_AWAITING_ACCEPTANCE:
		require.NoError(t, f.keeper.EnqueueSecretDeadlines(f.ctx, secret))
	case types.SECRET_STATUS_PENDING, types.SECRET_STATUS_RECONSTRUCTABLE:
		require.NoError(t, f.keeper.SettlementQueue.Set(f.ctx, collections.Join(secret.RevealEndBlock+1, secret.Id)))
	default: // terminal: the Stage 2 prune entry (invariant 4b)
		terminalAt := secret.TerminalAt
		if terminalAt == 0 {
			terminalAt = sdk.UnwrapSDKContext(f.ctx).BlockHeight()
		}
		require.NoError(t, f.keeper.PruneQueue.Set(f.ctx, collections.Join(terminalAt+types.RetentionBlocksValue(), secret.Id)))
	}
}
