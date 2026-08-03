package keeper

import (
	coserr "cosmossdk.io/errors"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/timeflareio/chain/x/secrets/types"
)

// GuardianAvailabilityState represents the state of a guardian relative to their availability period
type GuardianAvailabilityState int

const (
	// Guardian's availability period is in the future
	GuardianStatePrecedesAvailability GuardianAvailabilityState = iota
	// Guardian is currently within their availability period
	GuardianStateWithinAvailability
	// Guardian's availability period has ended
	GuardianStatePassedAvailability
)

// AvailabilityWindow represents an absolute availability window
type AvailabilityWindow struct {
	From  int64
	Until int64
}

// Duration returns the window duration in blocks
func (w AvailabilityWindow) Duration() int64 {
	return w.Until - w.From
}

// WindowCalculator handles availability window calculations and validation
type WindowCalculator struct {
	blockHeight      int64
	existingGuardian *types.Guardian // nil for new registrations
}

// NewWindowCalculator creates a calculator for new registrations
func NewWindowCalculator(blockHeight int64) WindowCalculator {
	return WindowCalculator{
		blockHeight:      blockHeight,
		existingGuardian: nil,
	}
}

// NewUpdateWindowCalculator creates a calculator for guardian updates
func NewUpdateWindowCalculator(blockHeight int64, guardian *types.Guardian) WindowCalculator {
	return WindowCalculator{
		blockHeight:      blockHeight,
		existingGuardian: guardian,
	}
}

// Calculate computes and validates an availability window from offsets
func (wc WindowCalculator) Calculate(fromOffset, untilOffset int64) (AvailabilityWindow, error) {
	// Basic validation
	if fromOffset < 0 {
		return AvailabilityWindow{}, coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"available_from must be >= 0 (got %d)", fromOffset)
	}

	// Get the policy for this operation
	policy := wc.getPolicy()

	// Calculate absolute from value
	fromAbs, err := wc.calculateFrom(fromOffset, policy)
	if err != nil {
		return AvailabilityWindow{}, err
	}

	// Calculate absolute until value, relative to current block
	untilAbs := wc.blockHeight + untilOffset

	window := AvailabilityWindow{From: fromAbs, Until: untilAbs}

	// Validate the window
	if err := wc.validateWindow(window, policy); err != nil {
		return AvailabilityWindow{}, err
	}

	return window, nil
}

// UpdatePolicy defines what updates are allowed
type UpdatePolicy struct {
	AllowFromChange bool
	AllowExtension  bool
	MaxUntilFromNow int64
}

// getPolicy returns the update policy based on guardian state
func (wc WindowCalculator) getPolicy() UpdatePolicy {
	// New registrations have no restrictions
	if wc.existingGuardian == nil {
		return UpdatePolicy{
			AllowFromChange: true,
			AllowExtension:  false,
			MaxUntilFromNow: 0, // no special limit
		}
	}

	// Determine guardian state
	state := wc.getGuardianState()

	// Active or pending guardians: restricted updates
	if state != GuardianStatePassedAvailability {
		return UpdatePolicy{
			AllowFromChange: false,
			AllowExtension:  true,
			MaxUntilFromNow: types.MaxAvailabilityWindow, // 1 year from now for active guardians
		}
	}

	// Expired guardians: full flexibility
	return UpdatePolicy{
		AllowFromChange: true,
		AllowExtension:  false,
		MaxUntilFromNow: 0, // no special limit
	}
}

// getGuardianState determines the guardian's availability state
func (wc WindowCalculator) getGuardianState() GuardianAvailabilityState {
	if wc.existingGuardian == nil {
		return GuardianStatePassedAvailability // default for new registrations
	}

	if wc.blockHeight < wc.existingGuardian.AvailableFrom {
		return GuardianStatePrecedesAvailability
	}
	if wc.blockHeight <= wc.existingGuardian.AvailableUntil {
		return GuardianStateWithinAvailability
	}
	return GuardianStatePassedAvailability
}

// calculateFrom determines the absolute from value based on policy
func (wc WindowCalculator) calculateFrom(fromOffset int64, policy UpdatePolicy) (int64, error) {
	// For updates with restricted from changes, always preserve existing
	if wc.existingGuardian != nil && !policy.AllowFromChange {
		return wc.existingGuardian.AvailableFrom, nil
	}

	// For updates with fromOffset=0, preserve existing
	if wc.existingGuardian != nil && fromOffset == 0 {
		return wc.existingGuardian.AvailableFrom, nil
	}

	// Offset 0 is the "from now" sentinel, and "now" is the next block: the
	// current height is already being executed, so availability cannot begin
	// within it. This is the definition of the sentinel, not a tunable floor —
	// no value below it is rejected, because 1 is the smallest offset that
	// means anything.
	effectiveOffset := fromOffset
	if effectiveOffset == 0 {
		effectiveOffset = 1
	}

	// Validate offset isn't too far in future
	if effectiveOffset > types.MaxAvailableFromOffset {
		return 0, coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"available_from too far in future: %d blocks (maximum %d blocks / %d days)",
			effectiveOffset, types.MaxAvailableFromOffset, types.MaxAvailableFromOffset/14400)
	}

	return wc.blockHeight + effectiveOffset, nil
}

// validateWindow validates the calculated window against constraints
func (wc WindowCalculator) validateWindow(window AvailabilityWindow, policy UpdatePolicy) error {
	// Window must be in the future
	if window.Until <= wc.blockHeight {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"availability_until would be in the past: %d (current block: %d)",
			window.Until, wc.blockHeight)
	}

	// Check duration constraints
	duration := window.Duration()
	if duration < types.MinAvailabilityWindow {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"availability window too short: %d blocks (minimum %d blocks / %d minutes)",
			duration, types.MinAvailabilityWindow, types.MinAvailabilityWindow/10)
	}

	// For updates, check extension requirement
	if policy.AllowExtension && window.Until <= wc.existingGuardian.AvailableUntil {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"guardian is within or precedes availability period - only extensions allowed (new until: %d, existing until: %d)",
			window.Until, wc.existingGuardian.AvailableUntil)
	}

	// Check maximum until constraint for active guardians
	if policy.MaxUntilFromNow > 0 && window.Until > wc.blockHeight+policy.MaxUntilFromNow {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"available_until too far in future during active window: block %d (maximum %d blocks / %d days from current block %d)",
			window.Until, wc.blockHeight+policy.MaxUntilFromNow, policy.MaxUntilFromNow/14400, wc.blockHeight)
	}

	// Check maximum duration (unless it's an active guardian with special rules)
	if policy.MaxUntilFromNow == 0 && duration > types.MaxAvailabilityWindow {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"availability window too long: %d blocks (maximum %d blocks / %d days)",
			duration, types.MaxAvailabilityWindow, types.MaxAvailabilityWindow/14400)
	}

	return nil
}
