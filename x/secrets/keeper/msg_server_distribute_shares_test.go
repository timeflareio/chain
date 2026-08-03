package keeper_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

func TestMsgUserDistributeShares_Success(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Register guardians and create a secret in reserved state
	registerTestGuardians(t, f, msgServer, 5)
	secretId := createTestSecret(t, f, msgServer)

	// Get the secret to see the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Create encrypted shares for each selected guardian
	shares := make([]*types.EncryptedShareData, 0, len(secret.SelectedGuardians))
	for _, guardianAddress := range secret.SelectedGuardians {
		// Generate proper HMAC using the same logic as production
		shareData := testShareBytes(secretId, guardianAddress)
		hmac := generateTestHMAC(secretId, guardianAddress, shareData)

		shares = append(shares, &types.EncryptedShareData{
			GuardianAddress: guardianAddress,
			EncryptedShare:  shareData,
			ShareHmac:       hmac,
		})
	}

	creator := sdk.AccAddress([]byte("creator_address"))
	msg := &types.MsgUserDistributeShares{
		Creator:           creator.String(),
		SecretId:          secretId,
		SecretCommitment:  []byte("secret_commitment_hash"),
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            shares,
	}

	_, err = msgServer.UserDistributeShares(f.ctx, msg)
	if err != nil {
		t.Fatalf("UserDistributeShares failed: %v", err)
	}

	// Verify secret state changed to awaiting_acceptance
	updatedSecret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}

	if updatedSecret.State != "awaiting_acceptance" {
		t.Errorf("Expected state 'awaiting_acceptance', got %s", updatedSecret.State)
	}

	// Verify shares were stored in the side-store, each with a PROPOSED
	// assignment record
	for i, guardianAddress := range updatedSecret.SelectedGuardians {
		expectedData := testShareBytes(secretId, guardianAddress)
		// Generate proper HMAC for comparison
		expectedHmac := generateTestHMAC(secretId, guardianAddress, expectedData)

		shareData, err := f.keeper.GetShareData(f.ctx, secretId, guardianAddress)
		if err != nil {
			t.Fatalf("Guardian %d: failed to get share data for guardian %s: %v",
				i, guardianAddress, err)
		}

		if string(shareData.EncryptedShare) != string(expectedData) {
			t.Errorf("Guardian %d: expected encrypted data %s, got %s",
				i, string(expectedData), string(shareData.EncryptedShare))
		}
		if string(shareData.ShareHmac) != string(expectedHmac) {
			t.Errorf("Guardian %d: HMAC mismatch for guardian %s",
				i, guardianAddress)
		}

		if status := assignmentStatus(t, f, secretId, guardianAddress); status != types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED {
			t.Errorf("Guardian %d: expected assignment status PROPOSED, got %v", i, status)
		}
	}

	// Verify commitment was stored
	if string(updatedSecret.SecretCommitment) != string(msg.SecretCommitment) {
		t.Errorf("Expected commitment %s, got %s", string(msg.SecretCommitment), string(updatedSecret.SecretCommitment))
	}
}

func TestMsgUserDistributeShares_InvalidParameters(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Register guardians and create a secret
	registerTestGuardians(t, f, msgServer, 5)
	secretId := createTestSecret(t, f, msgServer)

	creator := sdk.AccAddress([]byte("creator_address"))
	validShares := createValidShares(t, f, secretId)

	testCases := []struct {
		name        string
		msg         *types.MsgUserDistributeShares
		expectError string
	}{
		{
			name: "invalid creator address",
			msg: &types.MsgUserDistributeShares{
				Creator: "invalid_address",
			},
			expectError: "invalid creator address",
		},
		{
			name: "non-existent secret",
			msg: &types.MsgUserDistributeShares{
				Creator:           creator.String(),
				SecretId:          types.GenerateValidSecretID(),
				SecretCommitment:  []byte("commitment"),
				PayloadCiphertext: testPayloadCiphertext(),
				SecretPublicKey:   testSecretPublicKey(),
				Shares:            validShares,
			},
			expectError: "secret not found",
		},
		{
			name: "empty commitment",
			msg: &types.MsgUserDistributeShares{
				Creator:          creator.String(),
				SecretId:         secretId,
				SecretCommitment: []byte{}, // Empty commitment
				Shares:           validShares,
			},
			expectError: "commitment cannot be empty",
		},
		{
			name: "empty shares",
			msg: &types.MsgUserDistributeShares{
				Creator:           creator.String(),
				SecretId:          secretId,
				SecretCommitment:  []byte("commitment"),
				PayloadCiphertext: testPayloadCiphertext(),
				SecretPublicKey:   testSecretPublicKey(),
				Shares:            []*types.EncryptedShareData{}, // Empty shares
			},
			expectError: "shares array cannot be empty",
		},
		{
			name: "mismatched share count",
			msg: &types.MsgUserDistributeShares{
				Creator:           creator.String(),
				SecretId:          secretId,
				SecretCommitment:  []byte("commitment"),
				PayloadCiphertext: testPayloadCiphertext(),
				SecretPublicKey:   testSecretPublicKey(),
				Shares: []*types.EncryptedShareData{
					{
						GuardianAddress: "guardian1",
						EncryptedShare:  []byte("data"),
						ShareHmac:       make([]byte, 32), // Proper 32-byte HMAC
					},
				}, // Only 1 share, but should have 5
			},
			expectError: "invalid guardian address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgServer.UserDistributeShares(f.ctx, tc.msg)
			if err == nil {
				t.Fatalf("Expected error containing '%s', got nil", tc.expectError)
			}
			if !contains(err.Error(), tc.expectError) {
				t.Errorf("Expected error containing '%s', got '%s'", tc.expectError, err.Error())
			}
		})
	}
}

func TestMsgUserDistributeShares_WrongSecretState(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Register guardians and create a secret
	registerTestGuardians(t, f, msgServer, 5)
	secretId := createTestSecret(t, f, msgServer)

	// Manually change secret state to something other than "reserved"
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	secret.State = "pending" // Change to wrong state
	err = f.keeper.SetSecret(f.ctx, secret)
	if err != nil {
		t.Fatalf("Failed to update secret state: %v", err)
	}

	creator := sdk.AccAddress([]byte("creator_address"))
	validShares := createValidShares(t, f, secretId)

	msg := &types.MsgUserDistributeShares{
		Creator:           creator.String(),
		SecretId:          secretId,
		SecretCommitment:  []byte("commitment"),
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            validShares,
	}

	_, err = msgServer.UserDistributeShares(f.ctx, msg)
	if err == nil {
		t.Fatal("Expected error for wrong secret state")
	}

	if !contains(err.Error(), "secret must be in RESERVED state") {
		t.Errorf("Expected 'secret must be in RESERVED state' error, got: %v", err)
	}
}

func TestMsgUserDistributeShares_UnauthorizedCreator(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Register guardians and create a secret
	registerTestGuardians(t, f, msgServer, 5)
	secretId := createTestSecret(t, f, msgServer)

	// Try to distribute shares with different creator
	differentCreator := sdk.AccAddress([]byte("different_creator"))
	validShares := createValidShares(t, f, secretId)

	msg := &types.MsgUserDistributeShares{
		Creator:           differentCreator.String(), // Different from original creator
		SecretId:          secretId,
		SecretCommitment:  []byte("commitment"),
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            validShares,
	}

	_, err := msgServer.UserDistributeShares(f.ctx, msg)
	if err == nil {
		t.Fatal("Expected unauthorized creator error")
	}

	if !contains(err.Error(), "only creator can finalize secret") {
		t.Errorf("Expected 'only creator can finalize secret' error, got: %v", err)
	}
}

func TestMsgUserDistributeShares_InvalidShareData(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Register guardians and create a secret
	registerTestGuardians(t, f, msgServer, 5)
	secretId := createTestSecret(t, f, msgServer)

	creator := sdk.AccAddress([]byte("creator_address"))

	testCases := []struct {
		name        string
		shareModify func(*types.EncryptedShareData)
		expectError string
	}{
		{
			name: "empty encrypted data",
			shareModify: func(share *types.EncryptedShareData) {
				share.EncryptedShare = []byte{}
			},
			expectError: "encrypted share at index 0 cannot be empty",
		},
		{
			name: "empty HMAC",
			shareModify: func(share *types.EncryptedShareData) {
				share.ShareHmac = []byte{}
			},
			expectError: "share HMAC at index 0 cannot be empty",
		},
		{
			name: "invalid guardian address",
			shareModify: func(share *types.EncryptedShareData) {
				share.GuardianAddress = "non_assigned_guardian"
			},
			expectError: "invalid guardian address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shares := createValidShares(t, f, secretId)
			tc.shareModify(shares[0]) // Modify first share

			msg := &types.MsgUserDistributeShares{
				Creator:           creator.String(),
				SecretId:          secretId,
				SecretCommitment:  []byte("commitment"),
				PayloadCiphertext: testPayloadCiphertext(),
				SecretPublicKey:   testSecretPublicKey(),
				Shares:            shares,
			}

			_, err := msgServer.UserDistributeShares(f.ctx, msg)
			if err == nil {
				t.Fatalf("Expected error containing '%s', got nil", tc.expectError)
			}
			if !contains(err.Error(), tc.expectError) {
				t.Errorf("Expected error containing '%s', got '%s'", tc.expectError, err.Error())
			}
		})
	}
}

// Helper function to create a test secret in reserved state
func createTestSecret(t *testing.T, f *fixture, msgServer types.MsgServer) string {
	// Register guardians first (use unique prefix to avoid conflicts)
	registerTestGuardiansWithPrefix(t, f, msgServer, 21, "distribute_"+t.Name()) // Need 15 shares guardians (register extra to be safe)

	creator := sdk.AccAddress([]byte("creator_address"))

	msg := &types.MsgUserRequestGuardians{
		Creator:       creator.String(),
		DetectionHint: testDetectionHint(),
		Threshold:     3,
		MinShares:     15,
		MaxShares:     15,
		RevealWindow: &types.RevealWindow{
			StartOffset: 400,
			Duration:    testRevealDuration,
		},
		Bump: types.MinBump,
	}

	resp, err := msgServer.UserRequestGuardians(f.ctx, msg)
	if err != nil {
		t.Fatalf("Failed to create test secret: %v", err)
	}

	return resp.SecretId
}

// generateTestHMAC creates a proper HMAC for testing using the same logic as production
// This function replicates the exact HMAC generation logic from the keeper
func generateTestHMAC(secretID, guardianAddress string, shareData []byte) []byte {
	// Use the same key generation logic as the keeper
	h := sha256.New()
	h.Write([]byte(types.ModuleName)) // "secrets"
	h.Write([]byte(secretID))
	h.Write([]byte(guardianAddress))
	h.Write([]byte("hmac_salt"))
	hmacKey := h.Sum(nil)

	// Generate HMAC with the same parameters as production
	hmacGen := hmac.New(sha256.New, hmacKey)
	hmacGen.Write(shareData)
	hmacGen.Write([]byte(guardianAddress))
	hmacGen.Write([]byte(secretID))
	return hmacGen.Sum(nil)
}

// Helper function to create valid shares for a secret
func createValidShares(t *testing.T, f *fixture, secretId string) []*types.EncryptedShareData {
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret for share creation: %v", err)
	}

	shares := make([]*types.EncryptedShareData, 0, len(secret.SelectedGuardians))
	for _, guardianAddress := range secret.SelectedGuardians {
		// Generate proper HMAC using the same logic as production
		shareData := testShareBytes(secretId, guardianAddress)
		hmac := generateTestHMAC(secretId, guardianAddress, shareData)

		shares = append(shares, &types.EncryptedShareData{
			GuardianAddress: guardianAddress,
			EncryptedShare:  shareData,
			ShareHmac:       hmac,
		})
	}

	return shares
}
