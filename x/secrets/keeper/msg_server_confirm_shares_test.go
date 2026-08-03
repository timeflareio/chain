package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

func TestMsgGuardianConfirmShares_AcceptSuccess(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Set block height to a non-zero value
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	// Setup: Create secret in awaiting_acceptance state
	secretId := setupSecretWithShares(t, f, msgServer)

	// Get the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Test guardian accepting their assignment
	guardianAddress := secret.SelectedGuardians[0]
	msg := &types.MsgGuardianConfirmShares{
		Guardian: guardianAddress,
		SecretId: secretId,
		Accept:   true,
	}

	_, err = msgServer.GuardianConfirmShares(f.ctx, msg)
	if err != nil {
		t.Fatalf("GuardianConfirmShares failed: %v", err)
	}

	// Verify the guardian's assignment record was updated
	record, err := f.keeper.GetAssignment(f.ctx, secretId, guardianAddress)
	if err != nil {
		t.Fatalf("Guardian assignment record not found: %v", err)
	}

	if record.Status != types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
		t.Errorf("Expected assignment status ACCEPTED, got %v", record.Status)
	}

	if record.RespondedAtBlock == 0 {
		t.Error("Expected RespondedAtBlock to be set")
	}

	// Verify the denormalised accepted counter was incremented
	updatedSecret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}
	if updatedSecret.AcceptedCount != 1 {
		t.Errorf("Expected accepted count 1, got %d", updatedSecret.AcceptedCount)
	}
}

func TestMsgGuardianConfirmShares_RejectAssignment(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create secret in awaiting_acceptance state
	secretId := setupSecretWithShares(t, f, msgServer)

	// Get the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Test guardian rejecting their assignment
	guardianAddress := secret.SelectedGuardians[0]
	msg := &types.MsgGuardianConfirmShares{
		Guardian: guardianAddress,
		SecretId: secretId,
		Accept:   false, // Reject
	}

	_, err = msgServer.GuardianConfirmShares(f.ctx, msg)
	if err != nil {
		t.Fatalf("GuardianConfirmShares failed: %v", err)
	}

	// Verify the guardian's assignment record was marked as rejected
	if status := assignmentStatus(t, f, secretId, guardianAddress); status != types.AssignmentStatus_ASSIGNMENT_STATUS_REJECTED {
		t.Errorf("Expected assignment status REJECTED, got %v", status)
	}

	// Verify encrypted share and HMAC are preserved for audit purposes
	shareData, err := f.keeper.GetShareData(f.ctx, secretId, guardianAddress)
	if err != nil {
		t.Fatalf("Expected share record to be preserved after rejection for audit purposes: %v", err)
	}
	if len(shareData.EncryptedShare) == 0 {
		t.Error("Expected encrypted share to be preserved after rejection for audit purposes")
	}
	if len(shareData.ShareHmac) == 0 {
		t.Error("Expected share HMAC to be preserved after rejection for audit purposes")
	}
}

func TestMsgGuardianConfirmShares_AllAcceptedThenDeadlineFinalises(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create secret in awaiting_acceptance state
	secretId := setupSecretWithShares(t, f, msgServer)

	// Get the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Every selected candidate accepts — there is no first-n gate, and no
	// acceptance flips the state mid-window
	for _, guardianAddress := range secret.SelectedGuardians {
		msg := &types.MsgGuardianConfirmShares{
			Guardian: guardianAddress,
			SecretId: secretId,
			Accept:   true,
		}

		if _, err = msgServer.GuardianConfirmShares(f.ctx, msg); err != nil {
			t.Fatalf("GuardianConfirmShares failed for guardian %s: %v", guardianAddress, err)
		}

		currentSecret, _ := f.keeper.GetSecret(f.ctx, secretId)
		if currentSecret.State != "awaiting_acceptance" {
			t.Fatalf("Secret must stay in awaiting_acceptance until the deadline, got %s", currentSecret.State)
		}
	}

	fullSecret, _ := f.keeper.GetSecret(f.ctx, secretId)
	if fullSecret.AcceptedCount != int64(len(secret.SelectedGuardians)) {
		t.Errorf("Expected all %d candidates accepted, got %d", len(secret.SelectedGuardians), fullSecret.AcceptedCount)
	}

	// The roster finalises at the commit deadline
	finaliseCommitDeadline(t, f, secretId)

	updatedSecret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}
	if updatedSecret.State != "pending" {
		t.Errorf("Expected state 'pending', got %s", updatedSecret.State)
	}
}

func TestMsgGuardianConfirmShares_LockInReportedAtMinShares(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create secret in awaiting_acceptance state
	secretId := setupSecretWithShares(t, f, msgServer)

	// Get the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Accept one by one: the response reports lock-in exactly from the
	// min_shares-th acceptance (no event, no state change)
	for i, guardianAddress := range secret.SelectedGuardians {
		msg := &types.MsgGuardianConfirmShares{
			Guardian: guardianAddress,
			SecretId: secretId,
			Accept:   true,
		}

		resp, err := msgServer.GuardianConfirmShares(f.ctx, msg)
		if err != nil {
			t.Fatalf("GuardianConfirmShares failed for guardian %s: %v", guardianAddress, err)
		}

		wantLockedIn := int64(i+1) >= secret.MinShares
		if resp.LockedIn != wantLockedIn {
			t.Errorf("acceptance %d: LockedIn = %v, want %v (min_shares %d)", i+1, resp.LockedIn, wantLockedIn, secret.MinShares)
		}

		currentSecret, _ := f.keeper.GetSecret(f.ctx, secretId)
		if currentSecret.State != "awaiting_acceptance" {
			t.Fatalf("lock-in must not change state, got %s", currentSecret.State)
		}
	}

	// Finalisation at the deadline activates with the accepted set
	finaliseCommitDeadline(t, f, secretId)

	updatedSecret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}
	if updatedSecret.State != "pending" {
		t.Errorf("Expected state 'pending', got %s", updatedSecret.State)
	}
}

func TestMsgGuardianConfirmShares_InvalidParameters(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create secret in awaiting_acceptance state
	secretId := setupSecretWithShares(t, f, msgServer)

	// Get the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	validGuardianAddress := secret.SelectedGuardians[0]

	testCases := []struct {
		name        string
		msg         *types.MsgGuardianConfirmShares
		expectError string
	}{
		{
			name: "invalid guardian address",
			msg: &types.MsgGuardianConfirmShares{
				Guardian: "invalid_address",
				SecretId: secretId,
			},
			expectError: "invalid guardian address",
		},
		{
			name: "non-existent secret",
			msg: &types.MsgGuardianConfirmShares{
				Guardian: validGuardianAddress,
				SecretId: types.GenerateValidSecretID(),
				Accept:   true,
			},
			expectError: "secret not found",
		},
		{
			name: "guardian not assigned",
			msg: &types.MsgGuardianConfirmShares{
				Guardian: sdk.AccAddress([]byte("unassigned_guardian_test")).String(),
				SecretId: secretId,
				Accept:   true,
			},
			expectError: "guardian not assigned",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgServer.GuardianConfirmShares(f.ctx, tc.msg)
			if err == nil {
				t.Fatalf("Expected error containing '%s', got nil", tc.expectError)
			}
			if !contains(err.Error(), tc.expectError) {
				t.Errorf("Expected error containing '%s', got '%s'", tc.expectError, err.Error())
			}
		})
	}
}

func TestMsgGuardianConfirmShares_WrongSecretState(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create secret but change its state
	secretId := setupSecretWithShares(t, f, msgServer)

	// Change secret state to something other than awaiting_acceptance
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	secret.State = "pending" // Wrong state
	err = f.keeper.SetSecret(f.ctx, secret)
	if err != nil {
		t.Fatalf("Failed to update secret state: %v", err)
	}

	validGuardianAddress := secret.SelectedGuardians[0]
	msg := &types.MsgGuardianConfirmShares{
		Guardian: validGuardianAddress,
		SecretId: secretId,
		Accept:   true,
	}

	_, err = msgServer.GuardianConfirmShares(f.ctx, msg)
	if err == nil {
		t.Fatal("Expected error for wrong secret state")
	}

	if !contains(err.Error(), "can only accept assignments in awaiting_acceptance state") {
		t.Errorf("Expected 'can only accept assignments in awaiting_acceptance state' error, got: %v", err)
	}
}

func TestMsgGuardianConfirmShares_AlreadyConfirmed(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Setup: Create secret in awaiting_acceptance state
	secretId := setupSecretWithShares(t, f, msgServer)

	// Get the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	guardianAddress := secret.SelectedGuardians[0]
	msg := &types.MsgGuardianConfirmShares{
		Guardian: guardianAddress,
		SecretId: secretId,
		Accept:   true,
	}

	// First confirmation should succeed
	_, err = msgServer.GuardianConfirmShares(f.ctx, msg)
	if err != nil {
		t.Fatalf("First GuardianConfirmShares failed: %v", err)
	}

	// Second confirmation should fail
	_, err = msgServer.GuardianConfirmShares(f.ctx, msg)
	if err == nil {
		t.Fatal("Expected error for double confirmation")
	}

	if !contains(err.Error(), "guardian already responded") {
		t.Errorf("Expected 'guardian already responded' error, got: %v", err)
	}
}

// Helper function to setup a secret with shares in awaiting_acceptance state
func setupSecretWithShares(t *testing.T, f *fixture, msgServer types.MsgServer) string {
	// Note: Don't register guardians here - createTestSecret already does it

	// Create secret in reserved state
	secretId := createTestSecret(t, f, msgServer)

	// Get the secret to see the selected guardians
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Create and distribute shares
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

	distributeMsg := &types.MsgUserDistributeShares{
		Creator:           creator.String(),
		SecretId:          secretId,
		SecretCommitment:  []byte("secret_commitment_hash"),
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            shares,
	}

	_, err = msgServer.UserDistributeShares(f.ctx, distributeMsg)
	if err != nil {
		t.Fatalf("UserDistributeShares failed: %v", err)
	}

	return secretId
}
