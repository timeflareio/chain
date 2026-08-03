package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/types"
)

// The selection eligibility index.
//
// Phase-1 candidate enumeration used to walk the entire guardian collection on
// every MsgUserRequestGuardians. That walk is metered inside the CREATOR's gas,
// and registration is permanent (there is no deregistration), so its cost grew
// monotonically with every guardian who ever registered — including guardians
// who lapsed years ago. Measured, it reached 53% of phase-1 gas at 36
// registrations and would have begun aborting creation out of gas at a few
// hundred.
//
// The index holds every guardian with accepting_secrets = true, keyed
// (available_until, address). Selection range-reads from
// available_until >= reveal_end_block — the binding clause of the eligibility
// predicate — so registrations too short-dated for the secret, lapsed ones
// included, sort below the range start and are never read at all.
//
// This is an enumeration change, not a protocol change. The eligibility
// predicate and the sortition are unchanged: because ticket(g) =
// SHA256(seed ‖ address) depends on nothing but the seed and the guardian's own
// address, selection is independent of the order candidates are enumerated in
// (docs/spec.md, "Guardian Selection (Normative)"). The index therefore has one
// correctness obligation — yield the SAME SET the walk would have — and set
// equality is what the tests assert.
//
// DERIVED STATE, WITH A MIGRATION. The index is built by SetGuardian, so
// InitGenesis populates it as a side effect of importing guardians. An in-place
// chain upgrade does NOT run InitGenesis — it runs module migrations — so
// x/secrets is ConsensusVersion 2 and registers a 1 → 2 migration
// (module.go) that calls RebuildEligibilityIndex below.
//
// Without it, adopting this code on a chain whose store predates the index would
// leave the index EMPTY and fail every MsgUserRequestGuardians with
// "insufficient guardians: need N, have 0" — total, and invisible to a reader of
// the diff. No StoreUpgrades entry is needed: the prefix lives inside the
// existing secrets store rather than a new module store.

// eligibilityKey is the index key for a guardian: its availability upper bound
// first, so a range read can bound on it, then the address to make it unique.
func eligibilityKey(guardian types.Guardian) collections.Pair[int64, string] {
	return collections.Join(guardian.AvailableUntil, guardian.Address)
}

// GuardianEligibilityOf projects the record onto what the per-secret filter
// needs. The single place the projection is derived, so the index cannot come
// to mean something subtly different from the record it mirrors.
func GuardianEligibilityOf(guardian types.Guardian) types.GuardianEligibility {
	return types.GuardianEligibility{
		AvailableFrom:   guardian.AvailableFrom,
		ActiveBondCount: guardian.ActiveBondCount,
		BondK:           guardian.BondK,
		Unlocked:        sdk.NewCoin(types.DefaultDenom, UnlockedFloat(&guardian)),
	}
}

// setGuardianEligibility brings the index into line with a guardian record that
// is about to be written. Called only by SetGuardian.
//
// Maintaining it at the choke point rather than at the eight call sites that
// write guardians (registration, update, rotation, withdrawal and the five float
// mutations) costs one read of the previous record — needed to retire a stale
// key when available_until moves, since that field is part of the key. It is
// worth paying: an index maintained here cannot drift, whereas one maintained at
// call sites drifts the first time someone adds a ninth writer, and a stale
// eligibility entry is not a performance bug — it would let selection consider a
// guardian under an availability window it no longer has, which is a consensus
// fault.
//
// The read is affordable where it lands hottest: acceptance measures 48,144 gas
// of the 100,000 the creator reimburses.
func (k Keeper) setGuardianEligibility(ctx context.Context, guardian types.Guardian) error {
	// Retire the previous entry when the key itself has moved. Overwriting the
	// new key would otherwise leave the old one behind, still claiming a window
	// the guardian has given up.
	if previous, found := k.GetGuardian(ctx, guardian.Address); found && previous.AvailableUntil != guardian.AvailableUntil {
		if err := k.GuardianEligibility.Remove(ctx, eligibilityKey(previous)); err != nil {
			return fmt.Errorf("failed to retire stale eligibility entry for %s: %w", guardian.Address, err)
		}
	}

	// Membership is exactly "accepting new assignments". A guardian that has
	// stopped accepting is removed rather than filtered at read time, so the
	// range read never pays for it.
	if !guardian.AcceptingSecrets {
		if err := k.GuardianEligibility.Remove(ctx, eligibilityKey(guardian)); err != nil {
			return fmt.Errorf("failed to remove eligibility entry for %s: %w", guardian.Address, err)
		}
		return nil
	}

	if err := k.GuardianEligibility.Set(ctx, eligibilityKey(guardian), GuardianEligibilityOf(guardian)); err != nil {
		return fmt.Errorf("failed to index eligibility for %s: %w", guardian.Address, err)
	}
	return nil
}

// eligibilityEqual compares two projections field by field.
//
// Written out rather than generated: the point of the comparison is that every
// field the selection filter reads is in step with the record, so a field added
// to GuardianEligibility must be added here too. A generated Equal would keep
// compiling and silently stop proving that.
func eligibilityEqual(a, b types.GuardianEligibility) bool {
	return a.AvailableFrom == b.AvailableFrom &&
		a.ActiveBondCount == b.ActiveBondCount &&
		a.BondK == b.BondK &&
		a.Unlocked.Equal(b.Unlocked)
}

// RebuildEligibilityIndex discards the index and derives it again from the
// guardian records. Idempotent, so it is safe to run against a populated index,
// an empty one, or a partially drifted one.
//
// This is the migration path for the index being derived state. InitGenesis
// populates it as a side effect of SetGuardian, but an in-place chain upgrade
// does not run InitGenesis — it runs module migrations, and without this a chain
// whose store predates the index would come up with an EMPTY one, failing every
// MsgUserRequestGuardians with "insufficient guardians: need N, have 0".
//
// Registered as the x/secrets 1 → 2 migration (module.go), so it executes once,
// in consensus, at the upgrade height: every node runs it over identical state
// and writes identical entries, which is the same determinism guarantee
// InitGenesis has.
//
// Clearing first rather than upserting is deliberate. An upsert would leave
// behind any entry whose guardian has since changed its available_until or
// stopped accepting — precisely the drift a rebuild exists to remove — so the
// only honest rebuild starts from nothing.
func (k Keeper) RebuildEligibilityIndex(ctx context.Context) error {
	if err := k.GuardianEligibility.Clear(ctx, nil); err != nil {
		return fmt.Errorf("failed to clear the eligibility index: %w", err)
	}

	indexed := 0
	if err := k.Guardians.Walk(ctx, nil, func(_ string, guardian types.Guardian) (bool, error) {
		if !guardian.AcceptingSecrets {
			return false, nil
		}
		if err := k.GuardianEligibility.Set(ctx, eligibilityKey(guardian), GuardianEligibilityOf(guardian)); err != nil {
			return true, fmt.Errorf("failed to index eligibility for %s: %w", guardian.Address, err)
		}
		indexed++
		return false, nil
	}); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).Logger().Info(
		"rebuilt guardian eligibility index", "accepting_guardians", indexed)
	return nil
}

// EligibleCandidate is one entry of the eligibility index, resolved for a
// specific secret: the address, and the bond that guardian would post.
type EligibleCandidate struct {
	Address string
	Bond    int64
}

// CandidateRejections counts why in-range guardians failed the per-secret
// filter. Free to collect — the range read visits these entries anyway — and it
// turns the strict gate's failure from "insufficient guardians" into something
// an operator can act on.
type CandidateRejections struct {
	NotYetActive     int
	AtConcurrency    int
	CannotAffordBond int
}

// InsufficientGuardiansError explains a failed strict gate.
//
// The bare count is the least useful thing to report. A creator who asks for a
// long-dated secret and is told "need 13, have 0" cannot tell whether nobody is
// available that far out, whether guardians are saturated, or whether they are
// too poor to bond — and the first of those is by far the most common, because
// selection requires available_until >= reveal_end_block and guardians register
// for windows shorter than the protocol's one-year horizon.
//
// Guardians whose window ends before reveal_end_block are never enumerated (they
// sort below the range start), so their count is not available for free. The
// furthest window in the network is, though: one descending read of the index.
// That single fact is what distinguishes "ask for less time" from "wait for
// capacity", so it is worth the read on a path that has already failed.
type InsufficientGuardiansError struct {
	Needed         int64
	Found          int
	RevealEndBlock int64
	// FurthestWindow is the largest available_until among accepting guardians, or
	// 0 when the index holds nothing at all.
	FurthestWindow int64
	Rejections     CandidateRejections
}

func (e *InsufficientGuardiansError) Error() string {
	msg := fmt.Sprintf("insufficient guardians: need %d, have %d", e.Needed, e.Found)

	switch {
	case e.FurthestWindow == 0:
		return msg + " — no guardians are registered as accepting new assignments"
	case e.FurthestWindow < e.RevealEndBlock:
		return fmt.Sprintf("%s — this secret needs guardians available through block %d, "+
			"but the furthest availability window in the network ends at block %d; "+
			"shorten the reveal window or wait for guardians to extend their availability",
			msg, e.RevealEndBlock, e.FurthestWindow)
	}

	// Guardians exist that are available long enough, so the loss is in the
	// per-secret filters. Name whichever actually bit.
	reasons := make([]string, 0, 3)
	if e.Rejections.CannotAffordBond > 0 {
		reasons = append(reasons, fmt.Sprintf("%d cannot afford this secret's bond", e.Rejections.CannotAffordBond))
	}
	if e.Rejections.AtConcurrency > 0 {
		reasons = append(reasons, fmt.Sprintf("%d are at the concurrency cap of %d",
			e.Rejections.AtConcurrency, types.MaxActiveBondsPerGuardian))
	}
	if e.Rejections.NotYetActive > 0 {
		reasons = append(reasons, fmt.Sprintf("%d are not active yet", e.Rejections.NotYetActive))
	}
	if len(reasons) == 0 {
		return msg + " — too few guardians are available through the reveal window"
	}
	return msg + " — of those available through the reveal window, " + strings.Join(reasons, ", ")
}

// furthestAvailability returns the largest available_until among accepting
// guardians, or 0 if the index is empty. One descending iterator step.
func (k Keeper) furthestAvailability(ctx context.Context) int64 {
	var furthest int64
	rng := new(collections.Range[collections.Pair[int64, string]]).Descending()
	_ = k.GuardianEligibility.Walk(ctx, rng, func(key collections.Pair[int64, string], _ types.GuardianEligibility) (bool, error) {
		furthest = key.K1()
		return true, nil // stop: descending, so the first key is the largest
	})
	return furthest
}

// EligibleCandidatesFor enumerates the guardians eligible for a secret whose
// reveal window ends at revealEndBlock, applying the full predicate from
// docs/spec.md: accepting (index membership), available_until >= reveal_end_block
// (the range bound), available_from <= now, active-bond count below the
// concurrency cap, and unlocked float covering this candidate's OWN bond for
// this secret, priced by its own live k.
//
// Enumeration cannot stop early. Sortition takes the max_shares LOWEST tickets
// across all candidates, so every candidate's ticket must be computed; returning
// the first max_shares found would select by store order, which is bech32
// address ascending — and guardian addresses are self-chosen, so that would be
// offline-grindable by exactly the route creatorAddress was removed from the
// seed to close (docs/CHAIN_MECHANICS.md Trade-off §5). The index narrows WHAT is read,
// never how much of it.
func (k Keeper) EligibleCandidatesFor(ctx context.Context, revealEndBlock, distance, bump int64) ([]EligibleCandidate, CandidateRejections, error) {
	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	var rejected CandidateRejections

	// Range from reveal_end_block upwards. Everything below it fails
	// available_until >= reveal_end_block and is never touched: entries there may
	// still be perfectly good candidates for shorter-dated secrets, so they are
	// skipped rather than pruned.
	rng := new(collections.Range[collections.Pair[int64, string]]).
		StartInclusive(collections.PairPrefix[int64, string](revealEndBlock))

	var candidates []EligibleCandidate
	err := k.GuardianEligibility.Walk(ctx, rng, func(key collections.Pair[int64, string], entry types.GuardianEligibility) (bool, error) {
		if currentHeight < entry.AvailableFrom {
			rejected.NotYetActive++
			return false, nil
		}
		if entry.ActiveBondCount >= types.MaxActiveBondsPerGuardian {
			rejected.AtConcurrency++
			return false, nil
		}

		bond := types.BondAmount(distance, bump, types.ClampBondK(entry.BondK))
		if entry.Unlocked.Amount.LT(bond) {
			rejected.CannotAffordBond++
			return false, nil
		}

		candidates = append(candidates, EligibleCandidate{Address: key.K2(), Bond: bond.Int64()})
		return false, nil
	})
	if err != nil {
		return nil, rejected, err
	}

	return candidates, rejected, nil
}
