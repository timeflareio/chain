package types

import (
	"encoding/json"
	"os"
	"testing"
)

// The shared cross-implementation corpus lives in testdata/vectors/ at the
// repo root; this module sits at x/secrets/types.
const dialsVectorsPath = "../../../testdata/vectors/dials.json"

type revealWindowVector struct {
	HoldBlocks   int64  `json:"hold_blocks"`
	WindowBlocks int64  `json:"window_blocks"`
	Reason       string `json:"reason,omitempty"`
}

type revealWindowCorpus struct {
	RevealWindowDerivation struct {
		Constants struct {
			WindowFloor   int64 `json:"window_floor"`
			WindowCeiling int64 `json:"window_ceiling"`
			RampStart     int64 `json:"ramp_start"`
			RampEnd       int64 `json:"ramp_end"`
		} `json:"constants"`
		Cases []revealWindowVector `json:"cases"`
	} `json:"reveal_window_derivation"`
}

func loadRevealWindowCorpus(t *testing.T) revealWindowCorpus {
	t.Helper()
	data, err := os.ReadFile(dialsVectorsPath)
	if err != nil {
		t.Fatalf("reading dials vectors: %v", err)
	}
	var corpus revealWindowCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parsing dials vectors: %v", err)
	}
	if len(corpus.RevealWindowDerivation.Cases) == 0 {
		t.Fatal("dials corpus must carry reveal-window derivation cases")
	}
	return corpus
}

// TestRevealWindowBlocks_Vectors pins the derivation to the shared corpus, so
// the chain and every client that quotes a price before submission compute the
// same window for the same hold.
func TestRevealWindowBlocks_Vectors(t *testing.T) {
	corpus := loadRevealWindowCorpus(t)

	c := corpus.RevealWindowDerivation.Constants
	if c.WindowFloor != RevealWindowFloor || c.WindowCeiling != RevealWindowCeiling ||
		c.RampStart != RevealRampStart || c.RampEnd != RevealRampEnd {
		t.Fatalf("corpus constants drifted from the module: corpus floor=%d ceiling=%d rampStart=%d rampEnd=%d, module floor=%d ceiling=%d rampStart=%d rampEnd=%d",
			c.WindowFloor, c.WindowCeiling, c.RampStart, c.RampEnd,
			RevealWindowFloor, RevealWindowCeiling, RevealRampStart, RevealRampEnd)
	}

	for _, v := range corpus.RevealWindowDerivation.Cases {
		if got := RevealWindowBlocks(v.HoldBlocks); got != v.WindowBlocks {
			t.Errorf("RevealWindowBlocks(%d) = %d, corpus says %d (%s)",
				v.HoldBlocks, got, v.WindowBlocks, v.Reason)
		}
	}
}

// TestRevealWindowBlocks_Monotonic guards the property the clamps and the ramp
// exist to provide: a longer hold never earns a shorter window, and no hold
// escapes the bounds. A curve that dipped would let a creator buy more cushion
// by opening sooner.
func TestRevealWindowBlocks_Monotonic(t *testing.T) {
	// Dense either side of both knees, sparse across the interior.
	holds := []int64{0, 1, 50, 599, 600, 601, 602}
	for h := int64(1_000); h < RevealRampEnd; h += 3_571 {
		holds = append(holds, h)
	}
	holds = append(holds, RevealRampEnd-1, RevealRampEnd, RevealRampEnd+1, MaxRevealHorizon)

	prev := int64(-1)
	for _, h := range holds {
		w := RevealWindowBlocks(h)
		if w < RevealWindowFloor || w > RevealWindowCeiling {
			t.Fatalf("RevealWindowBlocks(%d) = %d, outside [%d, %d]", h, w, RevealWindowFloor, RevealWindowCeiling)
		}
		if w < prev {
			t.Fatalf("RevealWindowBlocks(%d) = %d, below the window for a shorter hold (%d)", h, w, prev)
		}
		prev = w
	}
}

// TestRevealWindowForStartOffset_MaxOffsetClosesOnHorizon is the bound the
// offset ceiling exists to guarantee: the furthest legal opening still closes
// exactly on the horizon, so a secret's whole life fits inside H.
func TestRevealWindowForStartOffset_MaxOffsetClosesOnHorizon(t *testing.T) {
	end := MaxRevealStartOffset + RevealWindowForStartOffset(MaxRevealStartOffset)
	if end != MaxRevealHorizon {
		t.Fatalf("offset %d + window %d = %d, want exactly the horizon %d",
			MaxRevealStartOffset, RevealWindowForStartOffset(MaxRevealStartOffset), end, MaxRevealHorizon)
	}
}

// TestIsqrt checks the integer square root is truncating and exact, including
// at the perfect square the upper knee relies on.
func TestIsqrt(t *testing.T) {
	rise := RevealWindowCeiling - RevealWindowFloor
	cases := []struct{ in, want int64 }{
		{-1, 0}, {0, 0}, {1, 1}, {2, 1}, {3, 1}, {4, 2}, {8, 2}, {9, 3},
		{99, 9}, {100, 10}, {101, 10},
		{rise * rise, rise}, {rise*rise - 1, rise - 1},
	}
	for _, c := range cases {
		if got := isqrt(c.in); got != c.want {
			t.Errorf("isqrt(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
