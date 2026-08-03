package keeper

// Internal tests for the sortition primitives: the normative seed derivation,
// ticket ordering, order-independence, pool stability, the buffer arithmetic,
// and — the acceptance bar from the selection-hardening plan — provable
// uniformity of selection across eligible guardians.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func sortitionGuardians(n int) []EligibleCandidate {
	guardians := make([]EligibleCandidate, n)
	for i := range guardians {
		guardians[i] = EligibleCandidate{Address: fmt.Sprintf("tmflr1sortition%04d", i)}
	}
	return guardians
}

// addressesOf flattens winners to addresses — the sortition rule is about
// addresses alone, so the assertions below read on those.
func addressesOf(candidates []EligibleCandidate) []string {
	addresses := make([]string, len(candidates))
	for i, candidate := range candidates {
		addresses[i] = candidate.Address
	}
	return addresses
}

// TestComputeSelectionSeed_NormativeVector pins the byte-exact seed formula:
// SHA256(chainID ‖ uint64_be(height) ‖ lastBlockHash ‖ uint64_be(counter)).
// If this test breaks, the change is consensus-breaking and spec.md must move
// with it.
func TestComputeSelectionSeed_NormativeVector(t *testing.T) {
	chainID := "timeflare-test-1"
	height := int64(123456)
	lastBlockHash := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	counter := uint64(42)

	var expected []byte
	{
		var heightBE, counterBE [8]byte
		binary.BigEndian.PutUint64(heightBE[:], uint64(height))
		binary.BigEndian.PutUint64(counterBE[:], counter)

		hasher := sha256.New()
		hasher.Write([]byte(chainID))
		hasher.Write(heightBE[:])
		hasher.Write(lastBlockHash)
		hasher.Write(counterBE[:])
		expected = hasher.Sum(nil)
	}

	require.Equal(t, expected, ComputeSelectionSeed(chainID, height, lastBlockHash, counter))

	// Every input perturbs the seed
	require.NotEqual(t, expected, ComputeSelectionSeed(chainID+"x", height, lastBlockHash, counter))
	require.NotEqual(t, expected, ComputeSelectionSeed(chainID, height+1, lastBlockHash, counter))
	require.NotEqual(t, expected, ComputeSelectionSeed(chainID, height, []byte("fedcba9876543210fedcba9876543210"), counter))
	require.NotEqual(t, expected, ComputeSelectionSeed(chainID, height, lastBlockHash, counter+1))
}

// TestSelectBySortition_NormativeOrdering pins the selection rule: ticket(g) =
// SHA256(seed ‖ address), lowest k win, compared as big-endian 256-bit
// integers (bytes.Compare), output in ascending-ticket order.
func TestSelectBySortition_NormativeOrdering(t *testing.T) {
	guardians := sortitionGuardians(16)
	seed := ComputeSelectionSeed("chain", 1, []byte("hash"), 1)

	selected := addressesOf(selectBySortition(guardians, seed, 5))
	require.Len(t, selected, 5)

	ticket := func(address string) []byte {
		hasher := sha256.New()
		hasher.Write(seed)
		hasher.Write([]byte(address))
		return hasher.Sum(nil)
	}

	// Selected addresses come out in strictly ascending ticket order...
	for i := 1; i < len(selected); i++ {
		require.Negative(t, bytes.Compare(ticket(selected[i-1]), ticket(selected[i])),
			"selection output must be in ascending ticket order")
	}

	// ...and every unselected guardian's ticket is above the winners' cut
	cut := ticket(selected[len(selected)-1])
	selectedSet := make(map[string]bool, len(selected))
	for _, address := range selected {
		selectedSet[address] = true
	}
	for _, guardian := range guardians {
		if !selectedSet[guardian.Address] {
			require.Positive(t, bytes.Compare(ticket(guardian.Address), cut),
				"unselected guardian %s has a lower ticket than a winner", guardian.Address)
		}
	}
}

// TestSelectBySortition_OrderIndependence proves the property that makes a
// future eligibility index safe: the outcome does not depend on candidate
// enumeration order.
func TestSelectBySortition_OrderIndependence(t *testing.T) {
	guardians := sortitionGuardians(24)
	seed := ComputeSelectionSeed("chain", 99, []byte("hash"), 7)

	baseline := selectBySortition(guardians, seed, 9)

	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 20; trial++ {
		shuffled := make([]EligibleCandidate, len(guardians))
		copy(shuffled, guardians)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		require.Equal(t, baseline, selectBySortition(shuffled, seed, 9),
			"selection must be identical regardless of candidate enumeration order")
	}
}

// TestSelectBySortition_PoolStability proves that adding one guardian changes
// only whether THAT guardian wins a slot — everyone else's ticket, and hence
// the rest of the selection, is unchanged (a Fisher–Yates shuffle has no such
// property: one insertion cascades through the swap sequence).
func TestSelectBySortition_PoolStability(t *testing.T) {
	guardians := sortitionGuardians(20)
	seed := ComputeSelectionSeed("chain", 5, []byte("hash"), 3)

	before := addressesOf(selectBySortition(guardians, seed, 8))

	newcomer := EligibleCandidate{Address: "tmflr1sortitionnewcomer"}
	after := addressesOf(selectBySortition(append(guardians, newcomer), seed, 8))

	beforeSet := make(map[string]bool, len(before))
	for _, address := range before {
		beforeSet[address] = true
	}

	dropped := 0
	for _, address := range after {
		if address == newcomer.Address {
			continue
		}
		require.True(t, beforeSet[address],
			"guardian %s entered the selection without being the newcomer", address)
	}
	for _, address := range before {
		found := false
		for _, a := range after {
			if a == address {
				found = true
				break
			}
		}
		if !found {
			dropped++
		}
	}
	require.LessOrEqual(t, dropped, 1,
		"adding one guardian may displace at most one previous winner")
}

// TestSelectBySortition_Uniformity is the plan's acceptance bar (D8): over
// many independent seeds, every eligible guardian must be selected equally
// often — a biased tie-break or hash quirk would starve some guardians of
// work. Chi-squared goodness-of-fit against the uniform expectation; the
// test is fully deterministic (fixed seeds), so the threshold cannot flake.
func TestSelectBySortition_Uniformity(t *testing.T) {
	const (
		nGuardians = 20
		kSelected  = 8
		trials     = 5000
	)

	guardians := sortitionGuardians(nGuardians)
	counts := make(map[string]int, nGuardians)

	for trial := 0; trial < trials; trial++ {
		// Independent seeds exactly as production produces them: the counter
		// differs per secret
		seed := ComputeSelectionSeed("timeflare-fairness", 1000, []byte("fixed_last_block_hash_32_bytes__"), uint64(trial))
		for _, address := range addressesOf(selectBySortition(guardians, seed, kSelected)) {
			counts[address]++
		}
	}

	expected := float64(kSelected*trials) / float64(nGuardians)
	chiSquared := 0.0
	for _, guardian := range guardians {
		observed := float64(counts[guardian.Address])
		diff := observed - expected
		chiSquared += diff * diff / expected
	}

	// 19 degrees of freedom; the p=0.001 critical value is 43.8. A correct
	// implementation lands well below it (deterministically, given the fixed
	// seeds); a systematic bias lands far above.
	require.Less(t, chiSquared, 43.8,
		"selection is not uniform across guardians (chi-squared %.2f over %d guardians, expected %.1f each)",
		chiSquared, nGuardians, expected)

	// And no guardian is starved outright
	for _, guardian := range guardians {
		require.Greater(t, counts[guardian.Address], 0, "guardian %s was never selected", guardian.Address)
	}
}
