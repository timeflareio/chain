package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/timeflareio/chain/x/secrets/types"
)

// feeCollectorAddr is the fee collector module account address — where the
// ante handler deposits every transaction fee during block execution.
var feeCollectorAddr = authtypes.NewModuleAddress(authtypes.FeeCollectorName)

// ProcessFeeSplit is the module's BeginBlock work, ordered BEFORE
// x/distribution's BeginBlocker (app_config.go): it removes and burns
// FeeBurnPercent of the previous block's collected fees, leaving
// FeeValidatorPercent in the fee collector for the distribution module's
// AllocateTokens to credit as withdrawable validator rewards the same
// block. This is the protocol's guaranteed, usage-proportional deflation
// (docs/planning/done/DONE_FEE_BURN_PLAN.md; routing corrected July 2026 —
// DONE_VALIDATOR_REWARD_ROUTING_PLAN.md).
func (k Keeper) ProcessFeeSplit(ctx context.Context) error {
	fees := k.bankKeeper.GetAllBalances(ctx, feeCollectorAddr)
	if fees.IsZero() {
		return nil // empty blocks cost nothing
	}
	return k.DistributeFees(ctx, fees)
}

// DistributeFees splits transaction fees according to the deflationary
// economic model: FeeBurnPercent burned, FeeValidatorPercent left where it
// sits — the fee collector — so x/distribution allocates it with full
// reward bookkeeping exactly as vanilla fee handling would. Per-denom
// arithmetic goes through the parameterised economics core
// (types.SplitFeeAmount), so conservation is exact by construction and the
// simulator sweeps the same code path.
func (k Keeper) DistributeFees(ctx context.Context, totalFees sdk.Coins) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	validatorFees := sdk.NewCoins()
	burnFees := sdk.NewCoins()
	for _, coin := range totalFees {
		validatorAmount, burnAmount := types.SplitFeeAmount(coin.Amount)
		if validatorAmount.IsPositive() {
			validatorFees = validatorFees.Add(sdk.NewCoin(coin.Denom, validatorAmount))
		}
		if burnAmount.IsPositive() {
			burnFees = burnFees.Add(sdk.NewCoin(coin.Denom, burnAmount))
		}
	}

	// Burn fees for deflationary pressure: route through this module's
	// account (which holds the Burner permission), then burn. The validator
	// share is deliberately not moved — x/distribution's BeginBlocker,
	// ordered immediately after this one, allocates the fee collector's
	// remaining balance to validators by bonded voting power.
	if !burnFees.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx, authtypes.FeeCollectorName, types.ModuleName, burnFees,
		); err != nil {
			return err
		}
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, burnFees); err != nil {
			return err
		}
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"fee_distribution",
			sdk.NewAttribute("validator_fees", validatorFees.String()),
			sdk.NewAttribute("burned_fees", burnFees.String()),
		),
	)

	return nil
}

// BurnSlashedFunds burns slashed guardian funds for deflationary pressure
func (k Keeper) BurnSlashedFunds(ctx context.Context, amount sdk.Coins) error {
	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, amount)
}
