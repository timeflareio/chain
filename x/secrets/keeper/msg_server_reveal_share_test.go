package keeper_test

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

func TestGuardianRevealShare_ValidReveal(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Create a test guardian and secret with shares - use unique names per test
	guardian := sdk.AccAddress([]byte("valid_reveal_guardian"))
	creator := sdk.AccAddress([]byte("valid_reveal_creator"))
	secretId, shareDataMap := setupTestSecretWithShare(t, f, msgServer, guardian, creator, "valid_reveal_secret")

	// Get the secret to check its state and reveal timing
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Ensure secret is in pending state
	if secret.State != "pending" {
		t.Fatalf("Expected secret to be in pending state, got %s", secret.State)
	}

	// Mock the reveal window timing
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)

	// Get the first accepted guardian's share data
	accepted := acceptedGuardians(t, f, secretId)
	if len(accepted) == 0 {
		t.Fatal("No accepted guardians found")
	}

	assignedGuardian := accepted[0]
	shareData := shareDataMap[assignedGuardian]

	if shareData == nil {
		t.Fatalf("No share data found for accepted guardian %s", assignedGuardian)
	}

	// Test valid reveal
	revealMsg := &types.MsgGuardianRevealShare{
		Guardian:       assignedGuardian,
		SecretId:       secretId,
		DecryptedShare: shareData.DecryptedShare,
	}

	resp, err := msgServer.GuardianRevealShare(f.ctx, revealMsg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.Accepted)
	require.False(t, resp.ReconstructionComplete) // Only 1 share revealed, need threshold

	// Verify share was stored
	secret, err = f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_PENDING, secret.State)
	require.Equal(t, int64(1), secret.RevealedCount)
	require.True(t, f.keeper.HasGuardianRevealed(f.ctx, secretId, assignedGuardian))

	// Check the guardian's assignment record exists and was accepted
	require.Equal(t, types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED,
		assignmentStatus(t, f, secretId, assignedGuardian))
}

func TestGuardianRevealShare_ReconstructionTriggered(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Use the working setup with unique guardian names
	guardian := sdk.AccAddress([]byte("recon_test_guardian"))
	creator := sdk.AccAddress([]byte("recon_test_creator"))
	secretId, shareDataMap := setupTestSecretWithShare(t, f, msgServer, guardian, creator, "reconstruction_secret")

	// Get the secret to check its state and reveal timing
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Ensure secret is in pending state with accepted guardians
	if secret.State != "pending" {
		t.Fatalf("Expected secret to be in pending state, got %s", secret.State)
	}

	activeGuardians := acceptedGuardians(t, f, secretId)
	if len(activeGuardians) == 0 {
		t.Fatal("No accepted guardians found - secret not properly activated")
	}

	// Mock the reveal window timing
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)

	// Only use accepted guardians for reveal
	if len(activeGuardians) < 3 {
		t.Fatalf("Expected at least 3 accepted guardians, got %d", len(activeGuardians))
	}

	// Reveal shares from active guardians
	revealCount := 0
	for i, guardianAddr := range activeGuardians {
		if i >= 3 {
			break // Only reveal 3 shares to meet threshold
		}

		shareData, exists := shareDataMap[guardianAddr]
		if !exists {
			continue // Skip if no share data for this guardian
		}

		revealMsg := &types.MsgGuardianRevealShare{
			Guardian:       guardianAddr,
			SecretId:       secretId,
			DecryptedShare: shareData.DecryptedShare,
		}

		resp, err := msgServer.GuardianRevealShare(f.ctx, revealMsg)
		require.NoError(t, err)
		require.True(t, resp.Accepted)

		revealCount++

		// Reconstruction should trigger when threshold is met (threshold = 3)
		if revealCount < 3 {
			require.False(t, resp.ReconstructionComplete)
		} else {
			require.True(t, resp.ReconstructionComplete)
			break // Stop after reconstruction is complete
		}
	}

	// Verify secret state changed to reconstructable (threshold met, window still open)
	secret, err = f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_RECONSTRUCTABLE, secret.State)
}

func TestGuardianRevealShare_InvalidCases(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardian := sdk.AccAddress([]byte("invalid_test_guardian"))
	creator := sdk.AccAddress([]byte("invalid_test_creator"))
	secretId, shareDataMap := setupTestSecretWithShare(t, f, msgServer, guardian, creator, "invalid_test_secret")

	// Get the secret to check its state and reveal timing
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Get the first accepted guardian's share data
	accepted := acceptedGuardians(t, f, secretId)
	if len(accepted) == 0 {
		t.Fatal("No accepted guardians found")
	}

	assignedGuardian := accepted[0]
	shareData := shareDataMap[assignedGuardian]

	if shareData == nil {
		t.Fatalf("No share data found for accepted guardian %s", assignedGuardian)
	}

	testCases := []struct {
		name      string
		setupFunc func()
		msg       *types.MsgGuardianRevealShare
		errMsg    string
	}{
		{
			name: "non-existent secret",
			setupFunc: func() {
				f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)
			},
			msg: &types.MsgGuardianRevealShare{
				Guardian:       assignedGuardian,
				SecretId:       types.GenerateValidSecretID(),
				DecryptedShare: shareData.DecryptedShare,
			},
			errMsg: "secret not found",
		},
		{
			name: "before reveal window",
			setupFunc: func() {
				f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock - 10) // Before reveal start
			},
			msg: &types.MsgGuardianRevealShare{
				Guardian:       assignedGuardian,
				SecretId:       secretId,
				DecryptedShare: shareData.DecryptedShare,
			},
			errMsg: "reveal window not yet open",
		},
		{
			name: "after reveal window",
			setupFunc: func() {
				f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealEndBlock + 10) // After reveal end
			},
			msg: &types.MsgGuardianRevealShare{
				Guardian:       assignedGuardian,
				SecretId:       secretId,
				DecryptedShare: shareData.DecryptedShare,
			},
			errMsg: "reveal window closed",
		},
		{
			name: "wrong guardian",
			setupFunc: func() {
				f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)
			},
			msg: &types.MsgGuardianRevealShare{
				Guardian:       sdk.AccAddress([]byte("wrong_guardian")).String(),
				SecretId:       secretId,
				DecryptedShare: shareData.DecryptedShare,
			},
			errMsg: "not assigned to secret",
		},
		{
			name: "invalid share data",
			setupFunc: func() {
				f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)
			},
			msg: &types.MsgGuardianRevealShare{
				Guardian:       assignedGuardian,
				SecretId:       secretId,
				DecryptedShare: []byte("invalid_share_data"), // Wrong data
			},
			errMsg: "HMAC verification failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Save original context
			originalCtx := f.ctx

			// Run setup if provided
			if tc.setupFunc != nil {
				tc.setupFunc()
			}

			// Execute reveal
			_, err := msgServer.GuardianRevealShare(f.ctx, tc.msg)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)

			// Restore context for next test
			f.ctx = originalCtx
		})
	}
}

func TestGuardianRevealShare_DoubleReveal(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardian := sdk.AccAddress([]byte("double_reveal_guardian"))
	creator := sdk.AccAddress([]byte("double_reveal_creator"))
	secretId, shareDataMap := setupTestSecretWithShare(t, f, msgServer, guardian, creator, "double_reveal_secret")

	// Get the secret to check its state and reveal timing
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Ensure secret is in pending state
	if secret.State != "pending" {
		t.Fatalf("Expected secret to be in pending state, got %s", secret.State)
	}

	// Mock the reveal window timing
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)

	// Get the first accepted guardian's share data
	accepted := acceptedGuardians(t, f, secretId)
	if len(accepted) == 0 {
		t.Fatal("No accepted guardians found")
	}

	assignedGuardian := accepted[0]
	shareData := shareDataMap[assignedGuardian]

	if shareData == nil {
		t.Fatalf("No share data found for accepted guardian %s", assignedGuardian)
	}

	// First reveal should succeed
	revealMsg := &types.MsgGuardianRevealShare{
		Guardian:       assignedGuardian,
		SecretId:       secretId,
		DecryptedShare: shareData.DecryptedShare,
	}

	resp, err := msgServer.GuardianRevealShare(f.ctx, revealMsg)
	require.NoError(t, err)
	require.True(t, resp.Accepted)

	// Second reveal with same guardian should fail
	_, err = msgServer.GuardianRevealShare(f.ctx, revealMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already revealed share")
}

func TestGuardianRevealShare_UnacceptedGuardian(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardian := sdk.AccAddress([]byte("unaccepted_guardian"))
	creator := sdk.AccAddress([]byte("unaccepted_creator"))

	// Setup but don't accept the share
	secretId, shareDataMap := setupTestSecretWithShareNoAccept(t, f, msgServer, guardian, creator, "unaccepted_secret")

	// Get the first assigned guardian's share data
	var assignedGuardian string
	var shareData *ShareData
	for guardianAddr, data := range shareDataMap {
		assignedGuardian = guardianAddr
		shareData = data
		break // Use the first one
	}

	if shareData == nil {
		t.Fatal("No share data found for any guardian")
	}

	// Move to reveal window - same as ValidReveal test
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(120)

	// Try to reveal without accepting first
	revealMsg := &types.MsgGuardianRevealShare{
		Guardian:       assignedGuardian,
		SecretId:       secretId,
		DecryptedShare: shareData.DecryptedShare,
	}

	_, err := msgServer.GuardianRevealShare(f.ctx, revealMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret is not in pending or reconstructable state")
}

// Helper struct to hold share data for testing
type ShareData struct {
	DecryptedShare []byte
	EncryptedShare []byte
	ShareHMAC      []byte
}

// Helper function to setup a secret with a single guardian's share
func setupTestSecretWithShare(
	t *testing.T,
	f *fixture,
	msgServer types.MsgServer,
	guardian sdk.AccAddress,
	creator sdk.AccAddress,
	secretIdPrefix string,
) (string, map[string]*ShareData) {
	return setupTestSecretWithShareInternal(t, f, msgServer, guardian, creator, secretIdPrefix, true)
}

// Helper function to setup a secret with a single guardian's share without accepting
func setupTestSecretWithShareNoAccept(
	t *testing.T,
	f *fixture,
	msgServer types.MsgServer,
	guardian sdk.AccAddress,
	creator sdk.AccAddress,
	secretIdPrefix string,
) (string, map[string]*ShareData) {
	return setupTestSecretWithShareInternal(t, f, msgServer, guardian, creator, secretIdPrefix, false)
}

// Internal helper that handles the actual setup
func setupTestSecretWithShareInternal(
	t *testing.T,
	f *fixture,
	msgServer types.MsgServer,
	guardian sdk.AccAddress,
	creator sdk.AccAddress,
	secretIdPrefix string,
	acceptShare bool,
) (string, map[string]*ShareData) {
	// Register extra guardians for threshold=3, redundancy=3 (need 15 guardians, register 20 to be safe)
	for i := 0; i < 20; i++ {
		guardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("%s_guardian_%d", secretIdPrefix, i)))
		registerGuardian(t, f, msgServer, guardianAddr, fmt.Sprintf("%s_g%d", secretIdPrefix, i))
	}

	// Phase 1: Request guardians
	requestMsg := &types.MsgUserRequestGuardians{
		Creator:       creator.String(),
		DetectionHint: testDetectionHint(),
		RevealWindow:  &types.RevealWindow{StartOffset: 300, Duration: types.MinRevealDuration},
		Threshold:     3,
		MinShares:     9,
		MaxShares:     9,
		Bump:          types.MinBump,
	}

	requestResp, err := msgServer.UserRequestGuardians(f.ctx, requestMsg)
	if err != nil {
		t.Fatalf("UserRequestGuardians failed: %v", err)
	}

	secretId := requestResp.SecretId

	// Get the secret to see the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Phase 2: Distribute shares to the protocol-selected guardians
	shareDataMap := make(map[string]*ShareData)
	shares := make([]*types.EncryptedShareData, 0, len(secret.SelectedGuardians))

	for _, guardianAddress := range secret.SelectedGuardians {
		// Create actual share data for this guardian
		decryptedShare := []byte("test_share_data_with_sufficient_length")
		encryptedShare := encryptShareForGuardian(decryptedShare, guardianAddress)
		shareHMAC := computeShareHMAC(f, decryptedShare, secretId, guardianAddress)

		shareData := &ShareData{
			DecryptedShare: decryptedShare,
			EncryptedShare: encryptedShare,
			ShareHMAC:      shareHMAC,
		}
		shareDataMap[guardianAddress] = shareData

		shares = append(shares, &types.EncryptedShareData{
			GuardianAddress: guardianAddress,
			EncryptedShare:  encryptedShare,
			ShareHmac:       shareHMAC,
		})
	}

	secretCommitment := sha256.Sum256([]byte("original_secret"))
	distributeMsg := &types.MsgUserDistributeShares{
		Creator:           creator.String(),
		SecretId:          secretId,
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            shares,
		SecretCommitment:  secretCommitment[:],
	}

	_, err = msgServer.UserDistributeShares(f.ctx, distributeMsg)
	if err != nil {
		t.Fatalf("UserDistributeShares failed: %v", err)
	}

	// Phase 3: Accept shares (if requested)
	if acceptShare {
		// Get updated secret after distribution
		updatedSecret, err := f.keeper.GetSecret(f.ctx, secretId)
		if err != nil {
			t.Fatalf("Failed to get updated secret: %v", err)
		}

		// Confirm shares for all selected guardians
		for _, guardianAddress := range updatedSecret.SelectedGuardians {
			confirmMsg := &types.MsgGuardianConfirmShares{
				Guardian: guardianAddress,
				SecretId: secretId,
				Accept:   true,
			}

			_, err = msgServer.GuardianConfirmShares(f.ctx, confirmMsg)
			if err != nil {
				t.Fatalf("GuardianConfirmShares failed for guardian %s: %v", guardianAddress, err)
			}
		}

		// The roster finalises at the commit deadline — nothing activates
		// mid-window
		finaliseCommitDeadline(t, f, secretId)
	}

	return secretId, shareDataMap
}

// Helper function to register a guardian
func registerGuardian(t *testing.T, f *fixture, msgServer types.MsgServer, guardian sdk.AccAddress, prefix string) {
	// Create unique encryption key for this guardian
	encryptionKey := getValidPublicKey(prefix + "_enc")

	// Register guardian with relative timing
	stake := sdk.NewCoin("uveil", math.NewInt(10000000000)) // 10,000 VEIL
	msg := &types.MsgGuardianRegister{
		Guardian:            guardian.String(),
		EncryptionPublicKey: encryptionKey,
		AvailableFrom:       0,      // Use default (current_block + 1)
		AvailableUntil:      100000, // 100000 blocks from available_from
		Deposit:             &stake,
		AcceptingSecrets:    true, // Accept new secret assignments
	}

	_, err := msgServer.GuardianRegister(f.ctx, msg)
	if err != nil {
		t.Fatalf("GuardianRegister failed: %v", err)
	}

	// Advance block height by 1 to make guardian active (since AvailableFrom = current_block + 1)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	f.ctx = sdkCtx.WithBlockHeight(sdkCtx.BlockHeight() + 1)

	// Verify guardian was registered and is active
	if !f.keeper.IsGuardianActive(f.ctx, guardian.String()) {
		t.Fatalf("Guardian %s is not active after registration", guardian.String())
	}
}

// Mock encryption function for testing
func encryptShareForGuardian(share []byte, guardianAddress string) []byte {
	// In real implementation, this would use the guardian's public key
	// For testing, we just do a simple transform
	encrypted := make([]byte, len(share))
	for i := range share {
		encrypted[i] = share[i] ^ byte(guardianAddress[i%len(guardianAddress)])
	}
	return encrypted
}

// Helper function to compute share HMAC - must match keeper's HMAC computation
func computeShareHMAC(f *fixture, share []byte, secretId string, guardianAddress string) []byte {
	// Use the keeper's test HMAC method - this matches the verification process
	hmac, err := f.keeper.ComputeTestHMAC(secretId, guardianAddress, share)
	if err != nil {
		panic(fmt.Sprintf("HMAC computation failed in test: %v", err))
	}
	return hmac
}

func TestGuardianRevealShare_AfterThresholdMet(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Use unique guardian names for this test
	guardian := sdk.AccAddress([]byte("threshold_test_guardian"))
	creator := sdk.AccAddress([]byte("threshold_test_creator"))
	secretId, shareDataMap := setupTestSecretWithShare(t, f, msgServer, guardian, creator, "threshold_test_secret")

	// Get the secret to check its state and reveal timing
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Ensure secret is in pending state with accepted guardians
	if secret.State != "pending" {
		t.Fatalf("Expected secret to be in pending state, got %s", secret.State)
	}

	activeGuardians := acceptedGuardians(t, f, secretId)
	if len(activeGuardians) < 5 {
		t.Fatalf("Expected at least 5 accepted guardians, got %d", len(activeGuardians))
	}

	activeGuardianCount := len(activeGuardians)
	t.Logf("Secret has %d accepted guardians, threshold = %d", activeGuardianCount, secret.Threshold)

	// Mock the reveal window timing
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)

	// Phase 1: Reveal shares from first 3 guardians (meet threshold)
	revealsCompleted := 0
	for i, guardianAddr := range activeGuardians {
		if i >= 3 {
			break // Only reveal first 3 to meet threshold
		}

		shareData, exists := shareDataMap[guardianAddr]
		if !exists {
			t.Fatalf("No share data for guardian %s", guardianAddr)
		}

		revealMsg := &types.MsgGuardianRevealShare{
			Guardian:       guardianAddr,
			SecretId:       secretId,
			DecryptedShare: shareData.DecryptedShare,
		}

		resp, err := msgServer.GuardianRevealShare(f.ctx, revealMsg)
		require.NoError(t, err)
		require.True(t, resp.Accepted)

		revealsCompleted++

		// Check reconstruction completion
		if revealsCompleted < 3 {
			require.False(t, resp.ReconstructionComplete)
		} else {
			require.True(t, resp.ReconstructionComplete)
		}
	}

	// Verify secret is now in reconstructable state
	secret, err = f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_RECONSTRUCTABLE, secret.State)
	require.Equal(t, int64(3), secret.RevealedCount)
	require.Len(t, revealsFor(t, f, secretId), 3)

	// Phase 2: Reveal shares from remaining guardians (AFTER threshold met)
	// This is the core test - these should succeed even though threshold is met
	for i, guardianAddr := range activeGuardians {
		if i < 3 {
			continue // Skip already revealed guardians
		}

		shareData, exists := shareDataMap[guardianAddr]
		if !exists {
			t.Fatalf("No share data for guardian %s", guardianAddr)
		}

		revealMsg := &types.MsgGuardianRevealShare{
			Guardian:       guardianAddr,
			SecretId:       secretId,
			DecryptedShare: shareData.DecryptedShare,
		}

		// This should succeed even though threshold is already met
		resp, err := msgServer.GuardianRevealShare(f.ctx, revealMsg)
		require.NoError(t, err, "Guardian %s should be able to reveal after threshold met", guardianAddr)
		require.True(t, resp.Accepted, "Guardian %s reveal should be accepted after threshold met", guardianAddr)

		// Reconstruction was already complete, so this should still be true
		require.True(t, resp.ReconstructionComplete, "ReconstructionComplete should remain true after threshold met")
	}

	// Verify all shares were stored
	finalSecret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	require.Equal(t, types.SECRET_STATUS_RECONSTRUCTABLE, finalSecret.State)

	// Should have revealed shares from all accepted guardians
	finalReveals := revealsFor(t, f, secretId)
	expectedRevealedShares := len(activeGuardians)
	require.Equal(t, expectedRevealedShares, len(finalReveals),
		"All %d accepted guardians should have revealed their shares", expectedRevealedShares)
	require.Equal(t, int64(expectedRevealedShares), finalSecret.RevealedCount)

	// Verify all revealed shares are from accepted guardians
	for _, revealedShare := range finalReveals {
		isActive := false
		for _, activeAddr := range activeGuardians {
			if revealedShare.GuardianAddress == activeAddr {
				isActive = true
				break
			}
		}
		require.True(t, isActive, "Revealed share from %s should be from an accepted guardian", revealedShare.GuardianAddress)
	}

	t.Logf("✅ Test passed: All %d accepted guardians successfully revealed shares after threshold was met", len(activeGuardians))
}
