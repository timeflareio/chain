package types

import (
	errorsmod "cosmossdk.io/errors"
)

// x/secrets module sentinel errors
var (
	ErrSecretNotFound            = errorsmod.Register(ModuleName, 1, "secret not found")
	ErrSecretAlreadyExists       = errorsmod.Register(ModuleName, 2, "secret already exists")
	ErrSecretNotPending          = errorsmod.Register(ModuleName, 3, "secret is not in pending state")
	ErrThresholdNotMet           = errorsmod.Register(ModuleName, 4, "threshold not met for reconstruction")
	ErrRevealTimeNotReached      = errorsmod.Register(ModuleName, 5, "reveal time has not been reached")
	ErrCancelDeadlinePassed      = errorsmod.Register(ModuleName, 6, "cancel deadline has passed")
	ErrInvalidSecretID           = errorsmod.Register(ModuleName, 7, "invalid secret ID")
	ErrInvalidThreshold          = errorsmod.Register(ModuleName, 8, "invalid threshold value")
	ErrInvalidRevealBlock        = errorsmod.Register(ModuleName, 9, "invalid reveal block")
	ErrInsufficientReward        = errorsmod.Register(ModuleName, 10, "insufficient reward amount")
	ErrInvalidRecipient          = errorsmod.Register(ModuleName, 11, "invalid recipient data")
	ErrTooManyGuardians          = errorsmod.Register(ModuleName, 12, "too many guardians assigned")
	ErrInvalidGuardianAssignment = errorsmod.Register(ModuleName, 13, "invalid guardian assignment")
	ErrInvalidSecretStatus       = errorsmod.Register(ModuleName, 14, "invalid secret status")
	ErrRevealBlockPassed         = errorsmod.Register(ModuleName, 15, "reveal block has passed")
	ErrInvalidGuardian           = errorsmod.Register(ModuleName, 16, "invalid guardian")
	ErrRevealBlockNotReached     = errorsmod.Register(ModuleName, 17, "reveal block not reached")
	ErrInsufficientShares        = errorsmod.Register(ModuleName, 18, "insufficient shares")
	ErrInvalidShareHMAC          = errorsmod.Register(ModuleName, 19, "invalid share HMAC")
	ErrSecretExists              = errorsmod.Register(ModuleName, 20, "secret already exists")
	ErrInvalidRequest            = errorsmod.Register(ModuleName, 21, "invalid request")

	// New errors for three-phase protocol
	ErrAcceptanceWindowClosed  = errorsmod.Register(ModuleName, 22, "acceptance window has closed")
	ErrGuardianNotAssigned     = errorsmod.Register(ModuleName, 23, "guardian not assigned to this secret")
	ErrAlreadyResponded        = errorsmod.Register(ModuleName, 24, "guardian already responded to assignment")
	ErrGuardianNotActive       = errorsmod.Register(ModuleName, 25, "guardian is not currently active")
	ErrInsufficientAcceptances = errorsmod.Register(ModuleName, 26, "insufficient guardian acceptances")
	ErrTimeoutPassed           = errorsmod.Register(ModuleName, 27, "timeout deadline has passed")

	// Guardian-related errors
	ErrInvalidSigner         = errorsmod.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrGuardianNotFound      = errorsmod.Register(ModuleName, 1101, "guardian not found")
	ErrGuardianAlreadyExists = errorsmod.Register(ModuleName, 1102, "guardian already exists")
	ErrInvalidStakeAmount    = errorsmod.Register(ModuleName, 1103, "invalid stake amount")
	ErrInvalidAvailability   = errorsmod.Register(ModuleName, 1104, "invalid availability window")
	ErrGuardianSlashed       = errorsmod.Register(ModuleName, 1105, "guardian has been slashed")
	ErrActiveSecrets         = errorsmod.Register(ModuleName, 1106, "guardian has active secret assignments")
	ErrAlreadySlashed        = errorsmod.Register(ModuleName, 1107, "guardian already slashed for this secret")
	ErrInsufficientBond      = errorsmod.Register(ModuleName, 1108, "insufficient unlocked float to lock the bond")
	ErrKeyAlreadyRegistered  = errorsmod.Register(ModuleName, 1109, "encryption key already registered — every key ever registered by any guardian stays reserved forever")
	ErrRotationTooSoon       = errorsmod.Register(ModuleName, 1110, "key rotation minimum interval not met")

	// Recipient rebate (docs/spec.md "Recipient Rebate")
	ErrNoRebate               = errorsmod.Register(ModuleName, 1111, "secret has no credited rebate")
	ErrRebateAlreadyCollected = errorsmod.Register(ModuleName, 1112, "rebate already collected")
	ErrInvalidRecipiencyProof = errorsmod.Register(ModuleName, 1113, "recipiency proof does not match the secret's detection hint")
	ErrNoRebateCommitment     = errorsmod.Register(ModuleName, 1114, "no rebate commitment for this address — commit first, then reveal in a later block")
	ErrCommitmentTooRecent    = errorsmod.Register(ModuleName, 1115, "rebate commitment was made in this block — reveal must land in a later one")
	ErrCommitmentMismatch     = errorsmod.Register(ModuleName, 1116, "recipiency proof does not reproduce the committed value for this address")
	ErrRebateExpired          = errorsmod.Register(ModuleName, 1117, "the rebate collection window has closed")
)

// Note: Constants moved to constants.go to avoid duplication
