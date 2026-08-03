package keeper_test

import (
	"testing"

	gogotypes "github.com/cosmos/gogoproto/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

func TestGuardianUpdate_Success(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian first
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	initialStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("initial_key"),
		AvailableFrom:       100,  // Future block
		AvailableUntil:      2000, // 1900 blocks duration
		Deposit:             &initialStake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Test successful update with multiple fields
	additionalStake := sdk.NewCoin(types.DefaultDenom, math.NewInt(5_000_000_000)) // 5000 VEIL
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:         guardianAddr.String(),
		AvailableFrom:    0,    // Preserve existing
		AvailableUntil:   3000, // Extend to 3000 blocks
		Deposit:          &additionalStake,
		AcceptingSecrets: &gogotypes.BoolValue{Value: false},
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify the update
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, int64(100), guardian.AvailableFrom)   // Preserved
	require.Equal(t, int64(3000), guardian.AvailableUntil) // Updated: current_block(1) + available_until(3000-1)
	require.False(t, guardian.AcceptingSecrets)

	// Verify stake was increased
	expectedStake := initialStake.Amount.Add(additionalStake.Amount)
	require.Equal(t, expectedStake, guardian.Stake.Amount)
}

func TestGuardianUpdate_EncryptionKeyUpdate_FieldRemoved(t *testing.T) {
	// Note: Encryption keys are now permanently immutable and the field has been completely
	// removed from MsgGuardianUpdate. This test verifies the field is no longer present.

	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian first
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	initialKey := getValidPublicKey("initial_key")

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: initialKey,
		AvailableFrom:       1000, // Future block (we're at block 1)
		AvailableUntil:      2000,
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Update other fields - should work fine since encryption key is not included
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableUntil: 2500, // Extend availability window
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify the encryption key remains unchanged (from original registration)
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, initialKey, guardian.EncryptionPublicKey)
}

// TestGuardianUpdate_EncryptionKeyUpdate_DuringActiveWindow_ShouldFail removed
// Encryption keys are now permanently immutable, not just during active windows

func TestGuardianUpdate_AvailabilityWindowUpdate_DuringActiveWindow(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian that's currently active
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("initial_key"),
		AvailableFrom:       1,   // Start soon (relative to current block)
		AvailableUntil:      111, // Duration of 110 blocks
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Advance block height to make guardian active (past AvailableFrom)
	newBlockHeight := currentBlock + 5 // Guardian becomes active at currentBlock + 1
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(newBlockHeight)

	// Should be able to extend availability window even during active period
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  0,   // Preserve existing
		AvailableUntil: 500, // Extend further
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify the extension
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, currentBlock+1, guardian.AvailableFrom)      // Preserved (original absolute value)
	require.Equal(t, newBlockHeight+500, guardian.AvailableUntil) // Extended: current_block + available_until_offset
}

func TestGuardianUpdate_AvailabilityWindowUpdate_IgnoreFromChangesDuringActive(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian that's currently active
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("initial_key"),
		AvailableFrom:       1,   // Start soon (relative to current block)
		AvailableUntil:      111, // Duration of 110 blocks
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Advance block height to make guardian active (past AvailableFrom)
	newBlockHeight := currentBlock + 5 // Guardian becomes active at currentBlock + 1
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(newBlockHeight)

	// Should ignore available_from changes during active window but allow extensions
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  50,  // Try to change available_from - should be ignored
		AvailableUntil: 500, // Extend duration - should work
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify available_from was preserved (ignored) and available_until was extended
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, currentBlock+1, guardian.AvailableFrom)      // Original value preserved
	require.Equal(t, newBlockHeight+500, guardian.AvailableUntil) // Extended: current_block + available_until_offset
}

func TestGuardianUpdate_StakeIncrease(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian first
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	initialStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("initial_key"),
		AvailableFrom:       100,
		AvailableUntil:      2000,
		Deposit:             &initialStake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Test stake increase
	increment := sdk.NewCoin(types.DefaultDenom, math.NewInt(7_500_000_000)) // 7500 VEIL
	updateMsg := &types.MsgGuardianUpdate{
		Guardian: guardianAddr.String(),
		Deposit:  &increment,
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify stake increased
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	expectedStake := initialStake.Amount.Add(increment.Amount)
	require.Equal(t, expectedStake, guardian.Stake.Amount)
}

func TestGuardianUpdate_NonExistentGuardian_ShouldFail(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Try to update a guardian that doesn't exist
	guardianAddr := sdk.AccAddress([]byte("nonexistent_guardian"))
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableUntil: 2000, // A valid updatable field so validation reaches the lookup
	}

	_, err := msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "guardian not found")
}

func TestGuardianUpdate_NoFieldsSpecified_ShouldFail(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian first
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("initial_key"),
		AvailableFrom:       100,
		AvailableUntil:      2000,
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Try to update with no fields specified (should fail validation)
	updateMsg := &types.MsgGuardianUpdate{
		Guardian: guardianAddr.String(),
		// No fields specified for update
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one field must be updated")
}

// TestGuardianUpdate_SameEncryptionKey_ShouldFail removed
// All encryption key updates are now permanently forbidden

func TestGuardianUpdate_AcceptingSecretsToggle(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian accepting secrets
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("initial_key"),
		AvailableFrom:       100,
		AvailableUntil:      2000,
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Update to stop accepting secrets
	// NOTE: accepting_secrets alone does not count as an update in ValidateBasic
	// (proto3 bool has no "unset"), so another updatable field must accompany it
	// Presence-aware toggle: an explicit false alone is now a valid update
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:         guardianAddr.String(),
		AcceptingSecrets: &gogotypes.BoolValue{Value: false},
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify the change
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.False(t, guardian.AcceptingSecrets)
}

func TestGuardianUpdate_EventEmission(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian first
	guardianAddr := sdk.AccAddress([]byte("test_guardian_addr"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("initial_key"),
		AvailableFrom:       100,
		AvailableUntil:      2000,
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Clear events from registration
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithEventManager(sdk.NewEventManager())
	f.ctx = sdkCtx

	// Update guardian
	additionalStake := sdk.NewCoin(types.DefaultDenom, math.NewInt(2_500_000_000))
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:         guardianAddr.String(),
		AvailableUntil:   3000,
		Deposit:          &additionalStake,
		AcceptingSecrets: &gogotypes.BoolValue{Value: false},
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Check events
	events := sdk.UnwrapSDKContext(f.ctx).EventManager().Events()
	require.Len(t, events, 1)

	event := events[0]
	require.Equal(t, types.EventGuardianUpdated, event.Type)

	// Check event attributes
	attrs := event.Attributes
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value
	}

	require.Equal(t, guardianAddr.String(), attrMap["guardian"])
	require.Equal(t, "guardian_updated", attrMap["action"])
	require.Equal(t, "true", attrMap["availability_updated"])
	require.Equal(t, additionalStake.String(), attrMap["float_deposited"])
	require.Equal(t, "false", attrMap["accepting_secrets_updated"])
}

// Additional GuardianUpdate test moved from keeper_test.go

func TestGuardianUpdate_UpdateWithoutStakeChange(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardianAddr := sdk.AccAddress([]byte("no_stake_change_guardian"))
	initialStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	// Register guardian initially
	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("test_enckey"),
		AvailableFrom:       100,
		AvailableUntil:      1000,
		Deposit:             &initialStake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Update without changing stake using MsgGuardianUpdate
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  0,    // 0 = preserve existing available_from for updates
		AvailableUntil: 2000, // 2000 blocks from preserved available_from (extend)
		// AcceptingSecrets omitted (nil) — presence-aware, preserves existing value
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify stake remained the same but other fields updated
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, initialStake, *guardian.Stake)

	// Since AvailableFrom=0 preserves existing, it should remain the original absolute value
	// Original: available_from = current_block + 100
	// With new behavior: available_until = current_block + 2000
	require.Equal(t, currentBlock+100, guardian.AvailableFrom, "Expected available_from to be preserved")
	require.Equal(t, currentBlock+2000, guardian.AvailableUntil, "Expected available_until to be current_block + offset")

	// REGRESSION GUARD: before presence-aware accepting_secrets, an update that
	// did not mention the flag would silently flip an accepting guardian to
	// false (proto3 bool default compared against stored value). Omission must
	// now preserve the existing value.
	require.True(t, guardian.AcceptingSecrets, "accepting_secrets must be preserved when omitted from the update")
}

// New comprehensive tests for availability period restrictions

func TestGuardianUpdate_WithinPeriod_IgnoreFromChanges(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian
	guardianAddr := sdk.AccAddress([]byte("test_guardian_within"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("within_key"),
		AvailableFrom:       1,   // Start at currentBlock + 1
		AvailableUntil:      500, // Duration of 499 blocks
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Advance to be within the availability period
	newBlockHeight := currentBlock + 10 // Guardian is active (currentBlock + 1 to currentBlock + 500)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(newBlockHeight)

	// Try to change available_from during within period - should be ignored
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  100, // Try to change - should be ignored
		AvailableUntil: 800, // Extend - should work
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify available_from was preserved and available_until was extended
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, currentBlock+1, guardian.AvailableFrom)      // Original preserved
	require.Equal(t, newBlockHeight+800, guardian.AvailableUntil) // Extended: current_block + available_until_offset
}

func TestGuardianUpdate_PrecedesPeriod_IgnoreFromChanges(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian with future availability
	guardianAddr := sdk.AccAddress([]byte("test_guardian_precedes"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("precedes_key"),
		AvailableFrom:       100, // Start at currentBlock + 100 (future)
		AvailableUntil:      500, // Duration of 400 blocks
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Current block is before availability period (precedes state)
	// currentBlock < currentBlock + 100, so guardian precedes availability

	// Try to change available_from during precedes period - should be ignored
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  200, // Try to change - should be ignored
		AvailableUntil: 700, // Extend - should work
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify available_from was preserved and available_until was extended
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, currentBlock+100, guardian.AvailableFrom)  // Original preserved
	require.Equal(t, currentBlock+700, guardian.AvailableUntil) // Extended: current_block + available_until_offset
}

func TestGuardianUpdate_PassedPeriod_AllowFullUpdates(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian with past availability
	guardianAddr := sdk.AccAddress([]byte("test_guardian_passed"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("passed_key"),
		AvailableFrom:       1,   // Start at currentBlock + 1
		AvailableUntil:      101, // End at currentBlock + 101 (minimum period = 100 blocks)
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Advance past the availability period
	newBlockHeight := currentBlock + 150 // Past currentBlock + 101 (guardian has passed)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(newBlockHeight)

	// Should allow both available_from and available_until changes
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  50,  // Change start - should work
		AvailableUntil: 300, // Change duration - should work
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.NoError(t, err)

	// Verify both fields were updated
	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, newBlockHeight+50, guardian.AvailableFrom)   // New start time
	require.Equal(t, newBlockHeight+300, guardian.AvailableUntil) // New: current_block + available_until_offset
}

func TestGuardianUpdate_ExtensionValidation_FailsIfNotExtension(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian
	guardianAddr := sdk.AccAddress([]byte("test_guardian_validation"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("validation_key"),
		AvailableFrom:       1,   // Start at currentBlock + 1
		AvailableUntil:      500, // End at currentBlock + 501
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Advance to be within the availability period
	newBlockHeight := currentBlock + 10
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(newBlockHeight)

	// Try to set available_until to a non-extension (shorter duration) - should fail
	updateMsg := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableUntil: 100, // Shorter than existing 500 - should fail
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only extensions allowed")
}

func TestGuardianUpdate_StateTransitions_Behavior(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Register a guardian with specific timing
	guardianAddr := sdk.AccAddress([]byte("test_guardian_transitions"))
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	registerMsg := &types.MsgGuardianRegister{
		Guardian:            guardianAddr.String(),
		EncryptionPublicKey: getValidPublicKey("transitions_key"),
		AvailableFrom:       10,  // Start at currentBlock + 10
		AvailableUntil:      150, // End at currentBlock + 160 (duration of 140)
		Deposit:             &stake,
		AcceptingSecrets:    true,
	}

	_, err := msgServer.GuardianRegister(f.ctx, registerMsg)
	require.NoError(t, err)

	// Test 1: Precedes state (currentBlock < available_from)
	// Should ignore available_from changes, allow extensions
	updateMsg1 := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  20,  // Try to change - should be ignored
		AvailableUntil: 200, // Extend - should work (longer than existing 150)
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg1)
	require.NoError(t, err)

	guardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, currentBlock+10, guardian.AvailableFrom)   // Preserved
	require.Equal(t, currentBlock+200, guardian.AvailableUntil) // Extended: current_block + available_until_offset

	// Test 2: Within state (available_from <= currentBlock <= available_until)
	newBlockHeight := currentBlock + 15 // Within period
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(newBlockHeight)

	updateMsg2 := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  5,   // Try to change - should be ignored
		AvailableUntil: 300, // Extend further - should work (longer than current 200)
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg2)
	require.NoError(t, err)

	guardian, found = f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, currentBlock+10, guardian.AvailableFrom)     // Still preserved
	require.Equal(t, newBlockHeight+300, guardian.AvailableUntil) // Extended: current_block + available_until_offset

	// Test 3: Passed state (currentBlock > available_until)
	newBlockHeight = currentBlock + 400 // Past the end (guardian ends at currentBlock + 315)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(newBlockHeight)

	updateMsg3 := &types.MsgGuardianUpdate{
		Guardian:       guardianAddr.String(),
		AvailableFrom:  50,  // Change start - should work now
		AvailableUntil: 300, // Change duration - should work
	}

	_, err = msgServer.GuardianUpdate(f.ctx, updateMsg3)
	require.NoError(t, err)

	guardian, found = f.keeper.GetGuardian(f.ctx, guardianAddr.String())
	require.True(t, found)
	require.Equal(t, newBlockHeight+50, guardian.AvailableFrom)   // Changed
	require.Equal(t, newBlockHeight+300, guardian.AvailableUntil) // Changed: current_block + available_until_offset
}
