package types

import (
	"regexp"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"github.com/google/uuid"
)

// UUID regex pattern for validation (accepts versions 1-5)
var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ValidateSecretId validates a secret ID according to protocol requirements.
// Secret IDs must be either:
// 1. Empty string (will be auto-generated)
// 2. Valid UUID format (36 characters: xxxxxxxx-xxxx-Vxxx-Yxxx-xxxxxxxxxxxx where V=1-5, Y=8,9,a,b)
// This enforces correctness over backward compatibility as specified.
func ValidateSecretId(secretId string) error {
	// Empty string is allowed (will be auto-generated)
	if secretId == "" {
		return nil
	}

	// Check length matches UUID format exactly
	if len(secretId) != SecretIdLength {
		return errorsmod.Wrapf(ErrInvalidSecretID, "secret ID must be valid UUID format (%d characters) or empty for auto-generation, got %d characters", SecretIdLength, len(secretId))
	}

	// Validate UUID format (accepts versions 1-5)
	if !uuidRegex.MatchString(strings.ToLower(secretId)) {
		return errorsmod.Wrapf(ErrInvalidSecretID, "secret ID must be valid UUID format (xxxxxxxx-xxxx-Vxxx-Yxxx-xxxxxxxxxxxx where V=1-5, Y=8,9,a,b), got %s", secretId)
	}

	return nil
}

// ValidateSecretIdRequired validates a secret ID that must not be empty.
// This is used for operations that reference existing secrets.
func ValidateSecretIdRequired(secretId string) error {
	// Check if empty
	if secretId == "" {
		return errorsmod.Wrap(ErrInvalidSecretID, "secret ID is required")
	}

	// Use standard validation
	return ValidateSecretId(secretId)
}

// GenerateValidSecretID generates a valid UUID format secret ID
func GenerateValidSecretID() string {
	return uuid.New().String()
}

// ValidateShareBand is the authoritative relational validation of the
// creator-chosen guardian band (docs/spec.md "The [min_shares, max_shares]
// band"):
//
//	MinThreshold ≤ threshold ≤ min_shares ≤ max_shares ≤ MaxTotalShares (32)
//	max_shares − min_shares < threshold                            (strict)
//
// The strict gap bound guarantees that, on any secret that activates, the
// candidates who received a share but never bonded are a sub-threshold set.
// min_shares == max_shares (a zero-width band) is a legitimate "exactly this
// many" request. Clients re-implement this rule for pre-submission UX; the
// (threshold, min, max) matrix in testdata/vectors/share_band.json pins the
// implementations against drift.
func ValidateShareBand(threshold, minShares, maxShares int64) error {
	if threshold < MinThreshold || threshold > MaxThreshold {
		return errorsmod.Wrapf(ErrInvalidRequest, "threshold must be between %d and %d, got %d", MinThreshold, MaxThreshold, threshold)
	}
	if minShares < MinShares {
		return errorsmod.Wrapf(ErrInvalidRequest, "min_shares must be at least %d, got %d", MinShares, minShares)
	}
	if minShares < threshold {
		return errorsmod.Wrapf(ErrInvalidRequest, "min_shares (%d) must be >= threshold (%d)", minShares, threshold)
	}
	if maxShares < minShares {
		return errorsmod.Wrapf(ErrInvalidRequest, "max_shares (%d) must be >= min_shares (%d)", maxShares, minShares)
	}
	if maxShares > MaxTotalShares {
		return errorsmod.Wrapf(ErrInvalidRequest, "max_shares must not exceed %d, got %d", MaxTotalShares, maxShares)
	}
	if maxShares-minShares >= threshold {
		return errorsmod.Wrapf(ErrInvalidRequest, "band width max_shares − min_shares (%d) must be strictly below threshold (%d): never-confirmed candidates must stay a sub-threshold set", maxShares-minShares, threshold)
	}
	return nil
}
