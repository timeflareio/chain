package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GuardianSelector handles protocol-controlled guardian selection
type GuardianSelector struct {
	keeper *Keeper
}

// NewGuardianSelector creates a new guardian selector
func NewGuardianSelector(k *Keeper) *GuardianSelector {
	return &GuardianSelector{
		keeper: k,
	}
}

// SelectGuardians selects guardians for a secret via hash sortition, with
// reveal-window, concurrency-cap and per-candidate bond-affordability
// filtering. It returns the selected guardian addresses (in ascending-ticket
// order), each selected guardian's bond amount frozen from its live k
// (aligned index-for-index with the addresses), plus the seed the sortition
// ran on — the seed inputs are emitted on the reservation event so anyone can
// confirm from public data that the seed was honest (none of its inputs are
// the creator's; see docs/spec.md "Guardian Selection").
//
// The gate is strict: if fewer than maxShares guardians are eligible, the
// whole transaction fails — there is no reduced-band fallback. There is no
// protocol-side over-selection constant: the acceptance margin is the
// creator's chosen band max_shares − min_shares.
func (gs *GuardianSelector) SelectGuardians(
	ctx context.Context,
	maxShares int64,
	revealEndBlock int64,
	distance int64,
	bump int64,
	secretCounter uint64,
) ([]string, []int64, []byte, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get all eligible guardians (availability window covering the reveal
	// window, accepting, under the concurrency cap, and able to afford their
	// own bond for this secret), with each candidate's bond priced by its k.
	// Read from the eligibility index, so the cost tracks the eligible set
	// rather than every registration ever made — see guardian_eligibility.go.
	candidates, rejected, err := gs.keeper.EligibleCandidatesFor(ctx, revealEndBlock, distance, bump)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get eligible guardians: %w", err)
	}

	if int64(len(candidates)) < maxShares {
		// The strict gate. Report WHY rather than just the shortfall: the
		// dominant cause is availability windows that do not reach
		// reveal_end_block, which a bare count gives a creator no way to guess.
		return nil, nil, nil, &InsufficientGuardiansError{
			Needed:         maxShares,
			Found:          len(candidates),
			RevealEndBlock: revealEndBlock,
			FurthestWindow: gs.keeper.furthestAvailability(ctx),
			Rejections:     rejected,
		}
	}

	// The selection seed is built entirely from consensus data — the creator
	// contributes nothing to it (the counter is protocol-assigned in
	// transaction order and differentiates secrets within a block)
	seed := generateSelectionSeed(sdkCtx, secretCounter)

	winners := selectBySortition(candidates, seed, maxShares)

	// Freeze each winner's bond in selection order — these stored amounts are
	// what acceptance locks and settlement releases/slashes verbatim; a
	// guardian's k moving after this height never re-prices this selection.
	// The bond travels with the candidate, so no second lookup is needed.
	selected := make([]string, len(winners))
	selectedBonds := make([]int64, len(winners))
	for i, winner := range winners {
		selected[i] = winner.Address
		selectedBonds[i] = winner.Bond
	}

	return selected, selectedBonds, seed, nil
}

// generateSelectionSeed builds the sortition seed from consensus data only.
// Every input is either fixed (chainID), consensus-agreed header state
// (height, the PREVIOUS block's hash — the current block's hash is unknown
// during execution), or protocol-assigned in transaction order (the secret
// counter). The creator supplies none of them; their only residual influence
// is submission timing.
func generateSelectionSeed(sdkCtx sdk.Context, secretCounter uint64) []byte {
	blockHeader := sdkCtx.BlockHeader()
	return ComputeSelectionSeed(blockHeader.ChainID, sdkCtx.BlockHeight(), blockHeader.LastBlockId.Hash, secretCounter)
}

// ComputeSelectionSeed is the normative seed derivation (docs/spec.md
// "Guardian Selection"):
//
//	seed = SHA256(chainID ‖ uint64_be(height) ‖ lastBlockHash ‖ uint64_be(counter))
//
// It is a pure function so off-chain verifiers can recompute the seed from
// the reservation event's attributes and confirm it was honest.
func ComputeSelectionSeed(chainID string, height int64, lastBlockHash []byte, secretCounter uint64) []byte {
	hasher := sha256.New()
	hasher.Write([]byte(chainID))
	hasher.Write(beUint64(uint64(height)))
	hasher.Write(lastBlockHash)
	hasher.Write(beUint64(secretCounter))
	return hasher.Sum(nil)
}

// selectBySortition picks the totalNeeded guardians with the lowest tickets,
// where each guardian's ticket depends only on the seed and its own address:
//
//	ticket(g) = SHA256(seed ‖ guardian_address_bech32_bytes)
//
// Tickets are compared as 256-bit big-endian integers; the astronomically
// unlikely tie is broken by guardian address ascending (byte-wise on the
// bech32 string). Sortition is order-independent — the outcome is the same
// regardless of candidate enumeration order — and stable under pool changes:
// adding or removing one guardian changes only whether THAT guardian wins a
// slot. Because the seed differs per secret, every eligible guardian draws a
// fresh uniform ticket each time, so selection probability is k/n regardless
// of address.
func selectBySortition(candidates []EligibleCandidate, seed []byte, totalNeeded int64) []EligibleCandidate {
	type scored struct {
		candidate EligibleCandidate
		ticket    []byte
	}

	tickets := make([]scored, len(candidates))
	for i, candidate := range candidates {
		hasher := sha256.New()
		hasher.Write(seed)
		hasher.Write([]byte(candidate.Address))
		tickets[i] = scored{candidate: candidate, ticket: hasher.Sum(nil)}
	}

	sort.Slice(tickets, func(i, j int) bool {
		if c := bytes.Compare(tickets[i].ticket, tickets[j].ticket); c != 0 {
			return c < 0
		}
		return tickets[i].candidate.Address < tickets[j].candidate.Address
	})

	selected := make([]EligibleCandidate, totalNeeded)
	for i := range selected {
		selected[i] = tickets[i].candidate
	}

	return selected
}

// beUint64 returns the 8-byte big-endian encoding of v (the fixed encoding
// pinned in spec.md for the seed inputs).
func beUint64(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}
