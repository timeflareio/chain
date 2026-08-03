package types

// Secret helper methods for cleaner state checking and lifecycle management

// IsComplete returns true if the secret is in any final state and should not be processed further
// This includes successful completion (revealed), user cancellation, and various failure states
func (s *Secret) IsComplete() bool {
	return s.State == SECRET_STATUS_REVEALED ||
		s.State == SECRET_STATUS_CANCELLED ||
		s.State == SECRET_STATUS_FAILED
}

// IsActive returns true if the secret is in any active state and may need processing
// This is the inverse of IsComplete() for convenience
func (s *Secret) IsActive() bool {
	return !s.IsComplete()
}

// IsSuccessful returns true if the secret completed successfully (was revealed)
func (s *Secret) IsSuccessful() bool {
	return s.State == SECRET_STATUS_REVEALED
}

// WasCancelled returns true if the secret was cancelled by the creator
func (s *Secret) WasCancelled() bool {
	return s.State == SECRET_STATUS_CANCELLED
}

// HasFailed returns true if the secret failed due to timeouts or insufficient participation
func (s *Secret) HasFailed() bool {
	return s.State == SECRET_STATUS_FAILED
}

// CanBeProcessedByEndBlock returns true if the secret is in a state that EndBlock might need to process
// This helps prevent processing completed secrets and reduces computational overhead
func (s *Secret) CanBeProcessedByEndBlock() bool {
	// Only active states can be processed by EndBlock
	// Completed states should be skipped entirely
	return s.IsActive()
}

// GetCompletionType returns a string describing how the secret completed
// Returns empty string if the secret is not yet complete
func (s *Secret) GetCompletionType() string {
	switch s.State {
	case SECRET_STATUS_REVEALED:
		return "successful_reveal"
	case SECRET_STATUS_CANCELLED:
		return "user_cancelled"
	case SECRET_STATUS_FAILED:
		return "protocol_failure"
	default:
		return "" // Not complete
	}
}
