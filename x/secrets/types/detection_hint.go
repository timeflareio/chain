package types

import (
	errorsmod "cosmossdk.io/errors"
)

const (
	// DetectionHintVersion is the only hint format currently defined:
	// single-key X25519 (tag = SHA256("timeflare/detect/v1" || shared)[:8]).
	// The version field exists so a dual-key scheme can be added without a
	// proto re-cut.
	DetectionHintVersion = 1
	// DetectionTagLength is the tag size in bytes. 8 bytes puts scan false
	// positives at 2^-64.
	DetectionTagLength = 8
)

// ValidateDetectionHint checks the hint's shape. The chain deliberately
// verifies nothing about the hint's content: only the holder of a recipient
// private key can relate a hint to anyone, and a creator wanting no discovery
// supplies random bytes — indistinguishable from a real hint by design.
func ValidateDetectionHint(hint *DetectionHint) error {
	if hint == nil {
		return errorsmod.Wrap(ErrInvalidRequest, "detection hint is required")
	}
	if hint.Version != DetectionHintVersion {
		return errorsmod.Wrapf(ErrInvalidRequest, "unsupported detection hint version %d (expected %d)", hint.Version, DetectionHintVersion)
	}
	if len(hint.EphemeralPub) != PublicKeyLength {
		return errorsmod.Wrapf(ErrInvalidRequest, "detection hint ephemeral key must be %d bytes, got %d", PublicKeyLength, len(hint.EphemeralPub))
	}
	if len(hint.Tag) != DetectionTagLength {
		return errorsmod.Wrapf(ErrInvalidRequest, "detection hint tag must be %d bytes, got %d", DetectionTagLength, len(hint.Tag))
	}
	return nil
}
