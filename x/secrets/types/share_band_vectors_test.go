package types

import (
	"encoding/json"
	"os"
	"testing"
)

// The shared cross-implementation corpus lives in testdata/vectors/ at the
// repo root; this module sits at x/secrets/types.
const shareBandVectorsPath = "../../../testdata/vectors/share_band.json"

type shareBandVector struct {
	Name      string `json:"name"`
	Threshold int64  `json:"threshold"`
	MinShares int64  `json:"min_shares"`
	MaxShares int64  `json:"max_shares"`
	Reason    string `json:"reason,omitempty"`
}

type shareBandCorpus struct {
	Valid   []shareBandVector `json:"valid"`
	Invalid []shareBandVector `json:"invalid"`
}

// TestValidateShareBand_Vectors pins ValidateShareBand — the authoritative
// band rule the clients re-implement — to the shared vector matrix, so the
// chain and every client assert identical verdicts on identical inputs.
func TestValidateShareBand_Vectors(t *testing.T) {
	data, err := os.ReadFile(shareBandVectorsPath)
	if err != nil {
		t.Fatalf("reading share band vectors: %v", err)
	}
	var corpus shareBandCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parsing share band vectors: %v", err)
	}
	if len(corpus.Valid) == 0 || len(corpus.Invalid) == 0 {
		t.Fatal("share band corpus must contain both valid and invalid vectors")
	}

	for _, v := range corpus.Valid {
		if err := ValidateShareBand(v.Threshold, v.MinShares, v.MaxShares); err != nil {
			t.Errorf("valid vector %q (t=%d min=%d max=%d) rejected: %v",
				v.Name, v.Threshold, v.MinShares, v.MaxShares, err)
		}
	}
	for _, v := range corpus.Invalid {
		if err := ValidateShareBand(v.Threshold, v.MinShares, v.MaxShares); err == nil {
			t.Errorf("invalid vector %q (t=%d min=%d max=%d) accepted — expected rejection (%s)",
				v.Name, v.Threshold, v.MinShares, v.MaxShares, v.Reason)
		}
	}
}
