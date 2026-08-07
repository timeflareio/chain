package keeper

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// The chain half of the shared dial corpus (testdata/vectors/dials.json). The
// TypeScript SDK asserts the same file through its DIALS descriptor table, so
// a client cannot offer a value this chain rejects — nor refuse one it accepts
// — without one of the two suites failing.
//
// This lives in the keeper package because the corpus is asserted against BOTH
// validation layers a real request passes. The two agree on every bound today —
// the commit window and the reveal window are both protocol constants, so the
// offset floor and ceiling are constants that ValidateBasic can check
// statelessly — and running the handler's copy as well is what keeps them from
// drifting apart. A corpus asserted against only one layer would pass drafts the
// other rejects, which is the client-side defect the corpus exists to prevent.

type dialCase struct {
	Name                    string `json:"name"`
	Threshold               int64  `json:"threshold"`
	MinShares               int64  `json:"min_shares"`
	MaxShares               int64  `json:"max_shares"`
	BumpHundredths          int64  `json:"bump_hundredths"`
	RevealStartOffsetBlocks int64  `json:"reveal_start_offset_blocks"`
	Valid                   bool   `json:"valid"`
	Reason                  string `json:"reason"`
}

type dialCorpus struct {
	Bounds map[string]json.RawMessage `json:"bounds"`
	Cases  []dialCase                 `json:"cases"`
}

func loadDialCorpus(t *testing.T) dialCorpus {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "vectors", "dials.json")
	raw, err := os.ReadFile(path) //nolint:gosec // fixed test-data path
	require.NoError(t, err, "dial corpus missing at %s", path)
	var corpus dialCorpus
	require.NoError(t, json.Unmarshal(raw, &corpus))
	require.NotEmpty(t, corpus.Cases, "corpus carries no cases")
	return corpus
}

// validateDialCase runs a corpus case through BOTH validation layers a real
// request passes: the stateless ValidateBasic, then the handler's
// context-dependent reveal-window check.
func validateDialCase(c dialCase) error {
	msg := &types.MsgUserRequestGuardians{
		// A well-formed creator and hint keep the case focused on the dials;
		// both have their own validation and corpora.
		// Encoded with whatever bech32 prefix this binary has configured, so
		// the case exercises the dials rather than the address codec.
		Creator: sdk.AccAddress(bytes.Repeat([]byte{0x01}, 20)).String(),
		DetectionHint: types.DetectionHint{
			Version:      types.DetectionHintVersion,
			EphemeralPub: bytes.Repeat([]byte{0x42}, 32),
			Tag:          bytes.Repeat([]byte{0x07}, 8),
		},
		RevealStartOffset: c.RevealStartOffsetBlocks,
		Threshold:         c.Threshold,
		MinShares:         c.MinShares,
		MaxShares:         c.MaxShares,
		Bump:              c.BumpHundredths,
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	return (msgServer{}).validateRevealStartOffset(msg.RevealStartOffset)
}

func TestDialVectors(t *testing.T) {
	corpus := loadDialCorpus(t)

	var valid, invalid int
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			err := validateDialCase(c)
			if c.Valid {
				require.NoError(t, err, "corpus says legal, chain rejected it")
			} else {
				require.Error(t, err, "corpus says illegal (%s), chain accepted it", c.Reason)
			}
		})
		if c.Valid {
			valid++
		} else {
			invalid++
		}
	}

	// A corpus that drifted to all-valid or all-invalid would still pass every
	// case above while testing nothing.
	require.Positive(t, valid, "corpus carries no legal cases")
	require.Positive(t, invalid, "corpus carries no illegal cases")
}

// TestDialCorpusBoundsMatchConstants keeps the corpus's declared bounds block
// honest: it is documentation for client authors, so it must not drift from
// the constants the cases are actually validated against.
func TestDialCorpusBoundsMatchConstants(t *testing.T) {
	corpus := loadDialCorpus(t)

	type minMax struct {
		Min int64 `json:"min"`
		Max int64 `json:"max"`
	}
	var threshold, minShares, bump, offset minMax
	require.NoError(t, json.Unmarshal(corpus.Bounds["threshold"], &threshold))
	require.NoError(t, json.Unmarshal(corpus.Bounds["min_shares"], &minShares))
	require.NoError(t, json.Unmarshal(corpus.Bounds["bump_hundredths"], &bump))
	require.NoError(t, json.Unmarshal(corpus.Bounds["reveal_start_offset_blocks"], &offset))

	require.Equal(t, types.MinThreshold, threshold.Min)
	require.Equal(t, types.MaxThreshold, threshold.Max)
	require.Equal(t, types.MinShares, minShares.Min)
	require.Equal(t, types.MaxTotalShares, minShares.Max)
	require.Equal(t, types.MinBump, bump.Min)
	require.Equal(t, types.MaxBump, bump.Max)
	require.Equal(t, types.MinRevealStartOffsetTotal, offset.Min)
	require.Equal(t, types.MaxRevealStartOffset, offset.Max)
}
