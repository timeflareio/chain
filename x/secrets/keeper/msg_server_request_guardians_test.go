package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

func TestMsgUserRequestGuardians_Success(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register test guardians first (need max_shares = 17 candidates)
	registerTestGuardians(t, f, msgServer, 25) // Register extra to be safe

	creator := sdk.AccAddress([]byte("creator_address"))

	msg := &types.MsgUserRequestGuardians{
		Creator:           creator.String(),
		DetectionHint:     testDetectionHint(),
		Threshold:         3,
		MinShares:         15,
		MaxShares:         17, // band: gap 2 < threshold 3
		RevealStartOffset: types.MinRevealStartOffsetTotal,
		Bump:              types.MinBump,
	}

	resp, err := msgServer.UserRequestGuardians(f.ctx, msg)
	if err != nil {
		t.Fatalf("UserRequestGuardians failed: %v", err)
	}

	if resp.SecretId == "" {
		t.Fatal("Expected secret ID to be generated")
	}

	// Verify secret was stored with correct state
	secret, err := f.keeper.GetSecret(f.ctx, resp.SecretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	if secret.Creator != creator.String() {
		t.Errorf("Expected creator %s, got %s", creator.String(), secret.Creator)
	}
	if secret.State != "reserved" {
		t.Errorf("Expected state 'reserved', got %s", secret.State)
	}
	if secret.Threshold != 3 {
		t.Errorf("Expected threshold 3, got %d", secret.Threshold)
	}
	if secret.MinShares != 15 {
		t.Errorf("Expected min_shares 15, got %d", secret.MinShares)
	}
	if secret.MaxShares != 17 {
		t.Errorf("Expected max_shares 17, got %d", secret.MaxShares)
	}
	if len(secret.SelectedGuardians) != 17 { // exactly max_shares candidates
		t.Errorf("Expected 17 selected guardians (exactly max_shares), got %d", len(secret.SelectedGuardians))
	}

	// No per-guardian records exist at request time — UserDistributeShares creates them
	if _, err := f.keeper.GetAssignment(f.ctx, resp.SecretId, secret.SelectedGuardians[0]); err == nil {
		t.Error("Expected no assignment records at request time (created at UserDistributeShares)")
	}
	if secret.AcceptedCount != 0 {
		t.Errorf("Expected accepted count 0 at request time, got %d", secret.AcceptedCount)
	}
}

func TestMsgUserRequestGuardians_InvalidParameters(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register test guardians (need enough for 15 shares + buffer = 20 guardians)
	registerTestGuardians(t, f, msgServer, 25)

	creator := sdk.AccAddress([]byte("creator_address"))

	testCases := []struct {
		name        string
		msg         *types.MsgUserRequestGuardians
		expectError string
	}{
		{
			name: "invalid creator address",
			msg: &types.MsgUserRequestGuardians{
				Creator:       "invalid_address",
				DetectionHint: testDetectionHint(),
			},
			expectError: "invalid creator address",
		},
		{
			name: "invalid threshold - zero",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         0,
				MinShares:         15,
				MaxShares:         15,
				RevealStartOffset: types.MinRevealStartOffsetTotal,
				Bump:              types.MinBump,
			},
			expectError: "threshold must be between",
		},
		{
			name: "invalid shares - too low",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         3,
				MinShares:         2, // Must be >= threshold,
				MaxShares:         2, // Must be >= threshold,
				RevealStartOffset: types.MinRevealStartOffsetTotal,
				Bump:              types.MinBump,
			},
			expectError: "min_shares (2) must be >= threshold (3)",
		},
		{
			name: "threshold exceeds shares",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         16,
				MinShares:         15, // threshold > shares,
				MaxShares:         15, // threshold > shares,
				RevealStartOffset: types.MinRevealStartOffsetTotal,
				Bump:              types.MinBump,
			},
			expectError: "min_shares (15) must be >= threshold (16)",
		},
		{
			name: "invalid reveal window - start in past",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         3,
				MinShares:         15,
				MaxShares:         15,
				RevealStartOffset: -1,
				Bump:              types.MinBump,
			},
			expectError: "reveal start offset too small",
		},
		{
			name: "invalid reveal window - offset above the ceiling",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         3,
				MinShares:         15,
				MaxShares:         15,
				RevealStartOffset: types.MaxRevealStartOffset + 1,
				Bump:              types.MinBump,
			},
			expectError: "reveal start offset too large",
		},
		{
			name: "invalid reveal window - min interval too small",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         3,
				MinShares:         15,
				MaxShares:         15,
				RevealStartOffset: types.MinRevealStartOffsetTotal - 1,
				Bump:              types.MinBump,
			},
			expectError: "reveal start offset too small",
		},
		{
			name: "insufficient reward",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         3,
				MinShares:         15,
				MaxShares:         15,
				RevealStartOffset: types.MinRevealStartOffsetTotal,
				Bump:              types.MinBump - 1, // Below 1.00
			},
			expectError: "bump must be between",
		},
		{
			name: "bump above max tier",
			msg: &types.MsgUserRequestGuardians{
				Creator:           creator.String(),
				DetectionHint:     testDetectionHint(),
				Threshold:         3,
				MinShares:         15,
				MaxShares:         15,
				RevealStartOffset: types.MinRevealStartOffsetTotal,
				Bump:              types.MaxBump + 1, // Above max tier
			},
			expectError: "bump must be between",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgServer.UserRequestGuardians(f.ctx, tc.msg)
			if err == nil {
				t.Fatalf("Expected error containing '%s', got nil", tc.expectError)
			}
			if !contains(err.Error(), tc.expectError) {
				t.Errorf("Expected error containing '%s', got '%s'", tc.expectError, err.Error())
			}
		})
	}
}

func TestMsgUserRequestGuardians_InsufficientGuardians(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register only 2 guardians, but request 5
	registerTestGuardians(t, f, msgServer, 2)

	creator := sdk.AccAddress([]byte("creator_address"))

	msg := &types.MsgUserRequestGuardians{
		Creator:           creator.String(),
		DetectionHint:     testDetectionHint(),
		Threshold:         3,
		MinShares:         15, // Need 5 guardians but only have 2,
		MaxShares:         15, // Need 5 guardians but only have 2,
		RevealStartOffset: types.MinRevealStartOffsetTotal,
		Bump:              types.MinBump,
	}

	_, err := msgServer.UserRequestGuardians(f.ctx, msg)
	if err == nil {
		t.Fatal("Expected insufficient guardians error")
	}

	if !contains(err.Error(), "insufficient guardians") {
		t.Errorf("Expected 'insufficient guardians' error, got: %v", err)
	}
}

func TestMsgUserRequestGuardians_GuardianSelection(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register more guardians than needed to test selection (need 15 shares guardians)
	registerTestGuardians(t, f, msgServer, 25)

	creator := sdk.AccAddress([]byte("creator_address"))

	msg := &types.MsgUserRequestGuardians{
		Creator:           creator.String(),
		DetectionHint:     testDetectionHint(),
		Threshold:         3,
		MinShares:         15,
		MaxShares:         15,
		RevealStartOffset: types.MinRevealStartOffsetTotal,
		Bump:              types.MinBump,
	}

	resp, err := msgServer.UserRequestGuardians(f.ctx, msg)
	if err != nil {
		t.Fatalf("UserRequestGuardians failed: %v", err)
	}

	// Verify secret was stored
	secret, err := f.keeper.GetSecret(f.ctx, resp.SecretId)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Check that exactly max_shares guardians were selected
	expectedTotalSelected := int(msg.MaxShares)
	if len(secret.SelectedGuardians) != expectedTotalSelected {
		t.Errorf("Expected %d selected guardians (exactly max_shares), got %d",
			expectedTotalSelected, len(secret.SelectedGuardians))
	}

	// Check that all selected guardians are unique
	guardianMap := make(map[string]bool)
	for _, address := range secret.SelectedGuardians {
		if guardianMap[address] {
			t.Errorf("Duplicate guardian selection: %s", address)
		}
		guardianMap[address] = true
	}

	// Verify selected guardian count (exactly the band ceiling)
	expectedMinSelected := int(secret.MaxShares)
	expectedMaxSelected := int(secret.MaxShares)
	actualSelected := len(secret.SelectedGuardians)

	if actualSelected < expectedMinSelected {
		t.Errorf("Too few selected guardians: got %d, expected at least %d",
			actualSelected, expectedMinSelected)
	}
	if actualSelected > expectedMaxSelected {
		t.Errorf("Too many selected guardians: got %d, expected at most %d",
			actualSelected, expectedMaxSelected)
	}
}

// Helper function to register test guardians
func registerTestGuardians(t *testing.T, f *fixture, msgServer types.MsgServer, count int) {
	registerTestGuardiansWithPrefix(t, f, msgServer, count, t.Name())
}

// Helper function to register test guardians with a prefix for uniqueness
func registerTestGuardiansWithPrefix(t *testing.T, f *fixture, msgServer types.MsgServer, count int, prefix string) {
	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	// No need for current height since we use relative values

	for i := 0; i < count; i++ {
		guardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("%s_guardian%d_____________________%d", prefix, i, i))) // Make unique addresses

		msg := &types.MsgGuardianRegister{
			Guardian:            guardianAddr.String(),
			EncryptionPublicKey: getValidPublicKey(fmt.Sprintf("enckey_%s_%d", prefix, i)),
			AvailableFrom:       0,      // Use default (current_block + 1)
			AvailableUntil:      100000, // 100000 blocks from available_from
			Deposit:             &validStake,
			AcceptingSecrets:    true, // Accept new secret assignments
		}

		_, err := msgServer.GuardianRegister(f.ctx, msg)
		if err != nil {
			t.Fatalf("Failed to register guardian %d: %v", i, err)
		}

		// Advance block height by 1 to make guardian active (since AvailableFrom = current_block + 1)
		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		f.ctx = sdkCtx.WithBlockHeight(sdkCtx.BlockHeight() + 1)

		// Verify guardian was registered and is active
		if !f.keeper.IsGuardianActive(f.ctx, guardianAddr.String()) {
			t.Fatalf("Guardian %d is not active after registration", i)
		}
	}
}

// contains function is defined in keeper_test.go
