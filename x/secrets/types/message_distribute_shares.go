package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUserDistributeShares = "distribute_shares"

var _ sdk.Msg = &MsgUserDistributeShares{}

// ValidateBasic performs stateless validation of MsgUserDistributeShares
func (msg *MsgUserDistributeShares) ValidateBasic() error {
	// Validate creator address
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	// Validate secret ID
	if err := ValidateSecretIdRequired(msg.SecretId); err != nil {
		return err
	}

	// Validate secret commitment
	if len(msg.SecretCommitment) == 0 {
		return errorsmod.Wrap(ErrInvalidRequest, "secret commitment cannot be empty")
	}
	if len(msg.SecretCommitment) > 1024 {
		return errorsmod.Wrapf(ErrInvalidRequest, "secret commitment too large, maximum 1024 bytes, got %d", len(msg.SecretCommitment))
	}

	// Validate the payload ciphertext (stored once; the reconstruction source)
	if len(msg.PayloadCiphertext) == 0 {
		return errorsmod.Wrap(ErrInvalidRequest, "payload ciphertext cannot be empty")
	}
	if len(msg.PayloadCiphertext) > int(MaxPayloadSize) {
		return errorsmod.Wrapf(ErrInvalidRequest, "payload ciphertext too large, maximum %d bytes, got %d", MaxPayloadSize, len(msg.PayloadCiphertext))
	}

	// Validate the per-secret public key
	if len(msg.SecretPublicKey) != SecretPublicKeySize {
		return errorsmod.Wrapf(ErrInvalidRequest, "secret public key must be exactly %d bytes, got %d", SecretPublicKeySize, len(msg.SecretPublicKey))
	}

	// Validate shares array
	if len(msg.Shares) == 0 {
		return errorsmod.Wrap(ErrInvalidRequest, "shares array cannot be empty")
	}

	// Check for reasonable maximum number of shares to prevent spam
	if len(msg.Shares) > 1000 {
		return errorsmod.Wrapf(ErrInvalidRequest, "too many shares, maximum 1000 allowed, got %d", len(msg.Shares))
	}

	// Validate each share and check for duplicate guardians
	guardians := make(map[string]bool)

	for i, share := range msg.Shares {
		if err := validateEncryptedShareData(share, i); err != nil {
			return err
		}

		// Check for duplicate guardians (each guardian can only have one assignment)
		if guardians[share.GuardianAddress] {
			return errorsmod.Wrapf(ErrInvalidRequest, "duplicate guardian %s", share.GuardianAddress)
		}
		guardians[share.GuardianAddress] = true
	}

	return nil
}

// validateEncryptedShareData validates an individual EncryptedShareData
func validateEncryptedShareData(share *EncryptedShareData, index int) error {
	if share == nil {
		return errorsmod.Wrapf(ErrInvalidRequest, "share at index %d cannot be nil", index)
	}

	// Validate guardian address
	_, err := sdk.AccAddressFromBech32(share.GuardianAddress)
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid guardian address at index %d (%s)", index, err)
	}

	// Share index no longer tracked - SSS handles intrinsic IDs

	// Validate encrypted share data
	if len(share.EncryptedShare) == 0 {
		return errorsmod.Wrapf(ErrInvalidRequest, "encrypted share at index %d cannot be empty", index)
	}
	if len(share.EncryptedShare) > int(MaxKeyShareSize) {
		return errorsmod.Wrapf(ErrInvalidRequest, "encrypted share at index %d too large, maximum %d bytes, got %d", index, MaxKeyShareSize, len(share.EncryptedShare))
	}

	// Validate HMAC
	if len(share.ShareHmac) == 0 {
		return errorsmod.Wrapf(ErrInvalidRequest, "share HMAC at index %d cannot be empty", index)
	}
	if len(share.ShareHmac) != 32 {
		return errorsmod.Wrapf(ErrInvalidRequest, "share HMAC at index %d must be 32 bytes (SHA256), got %d", index, len(share.ShareHmac))
	}

	return nil
}

// Route returns the message route for routing purposes
func (msg *MsgUserDistributeShares) Route() string {
	return ModuleName
}

// Type returns the message type for routing purposes
func (msg *MsgUserDistributeShares) Type() string {
	return TypeMsgUserDistributeShares
}

// GetSignBytes returns the canonical byte representation of the message for signing
func (msg *MsgUserDistributeShares) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg)) //nolint:staticcheck // legacy amino sign-bytes kept for API stability; the sdk signs via the tx config now
}
