package keeper

import (
	"context"

	coserr "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerr "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/timeflareio/chain/x/secrets/types"
	"github.com/timeflareio/crypto/go"
)

// Common validation functions

// AvailabilityWindowParams struct removed - now using WindowCalculator
// TimeConversionParams struct removed - now using WindowCalculator
// GuardianAvailabilityState enum moved to window_calculator.go

// validateGuardianAddress validates and parses a guardian address
func (ms msgServer) validateGuardianAddress(address string) (sdk.AccAddress, error) {
	addr, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return nil, coserr.Wrapf(sdkerr.ErrInvalidAddress,
			"invalid guardian address '%s': %s", address, err)
	}
	return addr, nil
}

// SignerProvider interface for messages that can provide signers
type SignerProvider interface {
	GetSigners() []sdk.AccAddress
}

// validateSignerAuthorization validates that the message is signed by the expected address
func (ms msgServer) validateSignerAuthorization(msg SignerProvider, expectedAddr sdk.AccAddress) error {
	signers := msg.GetSigners()
	if len(signers) != 1 {
		return coserr.Wrap(sdkerr.ErrUnauthorized, "invalid number of signers")
	}
	if !signers[0].Equals(expectedAddr) {
		return coserr.Wrap(sdkerr.ErrUnauthorized, "transaction must be signed by the guardian")
	}
	return nil
}

// validateEncryptionPublicKey validates a guardian share-encryption public key
// at registration and at rotation. Length is checked first so the error names
// the actual problem; usability is delegated to the crypto module.
func (ms msgServer) validateEncryptionPublicKey(key []byte) error {
	if len(key) != types.PublicKeyLength {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"encryption public key must be exactly %d bytes, got %d bytes",
			types.PublicKeyLength, len(key))
	}
	if err := crypto.ValidateX25519PublicKey(key); err != nil {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"encryption public key is not a usable X25519 public key: %s", err)
	}
	return nil
}

// validateSecretPublicKey validates pk_s, the per-secret public key supplied at
// share distribution. A small-order pk_s would make the payload ciphertext C
// decryptable by any observer. That harm is self-inflicted — the creator already
// holds the plaintext — but the check is applied uniformly rather than carved
// out: an argued asymmetry costs more to document and audit than it saves
// (docs/spec.md "Common Attack Vectors", Small-Order Key Registration).
func (ms msgServer) validateSecretPublicKey(key []byte) error {
	if err := crypto.ValidateX25519PublicKey(key); err != nil {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"secret public key (pk_s) is not a usable X25519 public key: %s", err)
	}
	return nil
}

// validateDetectionHintKey validates the detection hint's ephemeral key R.
//
// This is a shape check, not content verification: the chain still cannot and
// does not inspect who a hint targets. But against a small-order R every
// recipient computes the SAME all-zero shared value, so one hint would match
// every recipient and the 2^-64 scan false-positive bound would become 1. A
// creator wanting no discovery supplies random bytes, which are small-order with
// probability ~2^-250, so the no-discovery path is unaffected.
func (ms msgServer) validateDetectionHintKey(ephemeralPub []byte) error {
	if err := crypto.ValidateX25519PublicKey(ephemeralPub); err != nil {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"detection hint ephemeral key is not a usable X25519 public key: %s", err)
	}
	return nil
}

// hasUpdateFields checks if a MsgGuardianUpdate has any fields to update
func hasUpdateFields(msg *types.MsgGuardianUpdate) bool {
	return msg.AvailableFrom != 0 ||
		msg.AvailableUntil != 0 ||
		(msg.Deposit != nil && !msg.Deposit.IsZero()) ||
		msg.AcceptingSecrets != nil // presence-aware: an explicit true or false counts
	// Note: EncryptionPublicKey field removed - permanently immutable
}

// validateGuardianRegisterMessage validates the basic fields of a MsgGuardianRegister
func (ms msgServer) validateGuardianRegisterMessage(ctx context.Context, msg *types.MsgGuardianRegister) (sdk.AccAddress, error) {
	// Validate guardian address
	addr, err := ms.validateGuardianAddress(msg.Guardian)
	if err != nil {
		return nil, err
	}

	// Validate that the account is not a vesting account
	if err := ms.validateNotVestingAccount(ctx, addr); err != nil {
		return nil, err
	}

	// Validate encryption public key format and validity
	if err := ms.validateEncryptionPublicKey(msg.EncryptionPublicKey); err != nil {
		return nil, err
	}

	// Validate the optional initial float deposit. There is no minimum — the
	// entry fee gates registration, and per-secret bond checks gate
	// acceptance — but a provided deposit must be a valid coin in the chain denom.
	if msg.Deposit != nil && !msg.Deposit.IsZero() {
		if msg.Deposit.Denom != types.DefaultDenom {
			return nil, coserr.Wrapf(sdkerr.ErrInvalidRequest,
				"deposit must be in %s denomination, got %s", types.DefaultDenom, msg.Deposit.Denom)
		}
		if msg.Deposit.Amount.IsNegative() {
			return nil, coserr.Wrap(sdkerr.ErrInvalidRequest, "deposit cannot be negative")
		}
	}

	return addr, nil
}

// isVestingAccount checks if the given account is a vesting account type
func (ms msgServer) isVestingAccount(ctx context.Context, addr sdk.AccAddress) (bool, error) {
	account := ms.k.accountKeeper.GetAccount(ctx, addr)
	if account == nil {
		return false, coserr.Wrapf(sdkerr.ErrUnknownAddress, "account %s does not exist", addr.String())
	}

	// Check if the account has vesting-related methods (duck typing approach)
	// Vesting accounts implement methods like GetVestedCoins and GetVestingCoins
	if vestingAccount, hasVestingMethods := account.(interface {
		GetVestedCoins(ctx sdk.Context, blockTime int64) sdk.Coins
		GetVestingCoins(blockTime int64) sdk.Coins
	}); hasVestingMethods && vestingAccount != nil {
		return true, nil
	}

	return false, nil
}

// validateNotVestingAccount ensures the account is not a vesting account
func (ms msgServer) validateNotVestingAccount(ctx context.Context, addr sdk.AccAddress) error {
	isVesting, err := ms.isVestingAccount(ctx, addr)
	if err != nil {
		return err
	}

	if isVesting {
		return coserr.Wrapf(sdkerr.ErrUnauthorized,
			"vesting accounts cannot register as guardians - guardian registration is restricted to accounts with fully liquid tokens to prevent unvested token slashing")
	}

	return nil
}

// validateRegistrationAvailabilityWindow validates availability window for new guardian registrations
func (ms msgServer) validateRegistrationAvailabilityWindow(availableFromOffset, availableUntilOffset, blockHeight int64) error {
	calc := NewWindowCalculator(blockHeight)
	_, err := calc.Calculate(availableFromOffset, availableUntilOffset)
	return err
}

// validateUpdateAvailabilityWindow validates availability window for guardian updates
func (ms msgServer) validateUpdateAvailabilityWindow(availableFromOffset, availableUntilOffset, blockHeight int64, existingGuardian *types.Guardian) error {
	calc := NewUpdateWindowCalculator(blockHeight, existingGuardian)
	_, err := calc.Calculate(availableFromOffset, availableUntilOffset)
	return err
}

// convertRegistrationTimesToAbsolute converts relative times to absolute for new registrations
func (ms msgServer) convertRegistrationTimesToAbsolute(availableFromOffset, availableUntilOffset, blockHeight int64) (int64, int64) {
	calc := NewWindowCalculator(blockHeight)
	window, err := calc.Calculate(availableFromOffset, availableUntilOffset)
	if err != nil {
		// This should not happen as validation is done before conversion
		// Return sensible defaults in case of error
		return blockHeight + 1, blockHeight + types.MinAvailabilityWindow
	}
	return window.From, window.Until
}

// convertUpdateTimesToAbsolute converts relative times to absolute for guardian updates
func (ms msgServer) convertUpdateTimesToAbsolute(availableFromOffset, availableUntilOffset, blockHeight int64, existingGuardian *types.Guardian) (int64, int64) {
	calc := NewUpdateWindowCalculator(blockHeight, existingGuardian)
	window, err := calc.Calculate(availableFromOffset, availableUntilOffset)
	if err != nil {
		// This should not happen as validation is done before conversion
		// Return existing values as fallback
		return existingGuardian.AvailableFrom, existingGuardian.AvailableUntil
	}
	return window.From, window.Until
}

// validateSlashGuardianMessage validates the basic fields of a MsgSlashGuardian
func (ms msgServer) validateSlashGuardianMessage(msg *types.MsgSlashGuardian) error {
	// Validate required fields for early reveal reporting
	if len(msg.SecretId) == 0 {
		return coserr.Wrap(sdkerr.ErrInvalidRequest, "secret_id is required for early reveal reporting")
	}

	// Validate evidence
	if len(msg.Evidence) == 0 {
		return coserr.Wrap(sdkerr.ErrInvalidRequest, "evidence cannot be empty")
	}

	if len(msg.Evidence) < types.MinEvidenceLength {
		return coserr.Wrapf(sdkerr.ErrInvalidRequest,
			"evidence too short - minimum %d bytes required, got %d bytes",
			types.MinEvidenceLength, len(msg.Evidence))
	}

	if len(msg.Reason) == 0 {
		return coserr.Wrap(sdkerr.ErrInvalidRequest, "slash reason cannot be empty")
	}

	return nil
}

// validateGuardianWithdrawStakeMessage validates the basic fields of a MsgGuardianWithdrawStake
func (ms msgServer) validateGuardianWithdrawStakeMessage(msg *types.MsgGuardianWithdrawStake) (sdk.AccAddress, error) {
	// Validate guardian address
	return ms.validateGuardianAddress(msg.Guardian)
}
