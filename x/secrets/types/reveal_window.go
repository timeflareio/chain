package types

// The reveal window is derived, never chosen by the creator. It is a retry
// budget: the daemon reveals as soon as the window opens and retries until it
// closes, so what the window absorbs is a guardian being briefly unable to
// transact. How much is needed follows how long that guardian has been
// unobserved — see docs/spec.md "The Reveal Window".
//
// The functions here are normative in exactly this integer form: multiplication
// before division, truncating throughout. A floating-point square root is not an
// acceptable substitute, because its rounding is not guaranteed identical across
// architectures and this value fixes a settlement height.

// RevealHold returns the interval over which a secret's guardians are
// unobserved: from commit_deadline, where their acceptance is the last thing
// the chain hears from them, to the moment the window opens.
func RevealHold(revealStartOffset int64) int64 {
	return revealStartOffset - CommitTimeoutBlocks
}

// RevealWindowBlocks returns the derived window length for a hold:
//
//	hold ≤ RevealRampStart  →  RevealWindowFloor
//	hold ≥ RevealRampEnd    →  RevealWindowCeiling
//	otherwise               →  floor + isqrt((hold − rampStart) × rise² ÷ span)
//
// Concave by construction. With breakage arriving at a roughly constant rate the
// probability of being broken at window-open rises fastest early and saturates,
// so the cushion has to grow quickest in the first days of a hold and then level
// off. Both knees are exact: at RevealRampEnd the argument reduces to rise², and
// its integer square root is exactly rise.
func RevealWindowBlocks(hold int64) int64 {
	if hold <= RevealRampStart {
		return RevealWindowFloor
	}
	if hold >= RevealRampEnd {
		return RevealWindowCeiling
	}
	rise := RevealWindowCeiling - RevealWindowFloor
	span := RevealRampEnd - RevealRampStart
	// (hold − rampStart) ≤ span ≤ 431,400 and rise² = 51,122,500, so the product
	// peaks near 2.2e13 — three orders inside the int64 ceiling.
	return RevealWindowFloor + isqrt((hold-RevealRampStart)*rise*rise/span)
}

// RevealWindowForStartOffset is RevealWindowBlocks over the hold a creator's
// offset implies. This is the form callers want: the offset is the only timing
// value on the wire.
func RevealWindowForStartOffset(revealStartOffset int64) int64 {
	return RevealWindowBlocks(RevealHold(revealStartOffset))
}

// isqrt returns the truncating integer square root of n by Newton's method:
// the largest x with x² ≤ n. Returns 0 for n ≤ 0.
func isqrt(n int64) int64 {
	if n <= 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}
