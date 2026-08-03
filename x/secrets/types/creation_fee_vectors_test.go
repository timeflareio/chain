package types

import (
	"encoding/json"
	"os"
	"testing"

	"cosmossdk.io/math"
)

// The shared cross-implementation corpus lives in testdata/vectors/ at the
// repo root; this module sits at x/secrets/types.
const creationFeeVectorsPath = "../../../testdata/vectors/creation_fee.json"

type creationFeeVector struct {
	Name           string `json:"name"`
	PoolUveil      string `json:"pool_uveil"`
	DistanceBlocks int64  `json:"distance_blocks"`
	FeeUveil       string `json:"fee_uveil"`
	Regime         string `json:"regime"`
}

type creationFeeCorpus struct {
	Vectors []creationFeeVector `json:"vectors"`
}

// TestCreationFee_Vectors pins CreationFee — the fee formula the client
// cost estimators re-implement — to the shared vector matrix, so the chain
// and every client derive identical fees from identical inputs.
func TestCreationFee_Vectors(t *testing.T) {
	data, err := os.ReadFile(creationFeeVectorsPath)
	if err != nil {
		t.Fatalf("reading creation fee vectors: %v", err)
	}
	var corpus creationFeeCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parsing creation fee vectors: %v", err)
	}
	if len(corpus.Vectors) == 0 {
		t.Fatal("creation fee corpus must not be empty")
	}

	for _, v := range corpus.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			pool, ok := math.NewIntFromString(v.PoolUveil)
			if !ok {
				t.Fatalf("bad pool_uveil %q", v.PoolUveil)
			}
			want, ok := math.NewIntFromString(v.FeeUveil)
			if !ok {
				t.Fatalf("bad fee_uveil %q", v.FeeUveil)
			}
			got := CreationFee(pool, v.DistanceBlocks)
			if !got.Equal(want) {
				t.Fatalf("CreationFee(%s, %d) = %s, want %s", v.PoolUveil, v.DistanceBlocks, got, want)
			}
			wantFloor := v.Regime == "floor"
			if CreationFeeIsFloorPriced(pool, v.DistanceBlocks) != wantFloor {
				t.Fatalf("regime mismatch: want %s", v.Regime)
			}
		})
	}
}
