package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// feeBankKeeper models exactly what the BeginBlock fee split touches: the
// fee collector balance, module-to-module transfers, and burns.
type feeBankKeeper struct {
	mockBankKeeper
	collectorBalance sdk.Coins
	moduleTransfers  map[string]sdk.Coins // "sender->recipient" -> total
	burnt            sdk.Coins
}

func newFeeBankKeeper(balance sdk.Coins) *feeBankKeeper {
	return &feeBankKeeper{
		collectorBalance: balance,
		moduleTransfers:  map[string]sdk.Coins{},
	}
}

func (fb *feeBankKeeper) GetAllBalances(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	if addr.Equals(authtypes.NewModuleAddress(authtypes.FeeCollectorName)) {
		return fb.collectorBalance
	}
	return sdk.NewCoins()
}

func (fb *feeBankKeeper) SendCoinsFromModuleToModule(_ context.Context, sender, recipient string, amt sdk.Coins) error {
	key := sender + "->" + recipient
	fb.moduleTransfers[key] = fb.moduleTransfers[key].Add(amt...)
	fb.collectorBalance = fb.collectorBalance.Sub(amt...)
	return nil
}

func (fb *feeBankKeeper) BurnCoins(_ context.Context, _ string, amt sdk.Coins) error {
	fb.burnt = fb.burnt.Add(amt...)
	return nil
}

// TestProcessFeeSplit_SplitsAndBurns pins the BeginBlock flow: 10% of the
// fee collector's balance routes through the secrets module account and is
// burned, the 90% validator share is LEFT IN the fee collector for
// x/distribution's AllocateTokens (the withdrawable-reward pathway — the
// assertion the original suites lacked), and the event reports both
// amounts.
func TestProcessFeeSplit_SplitsAndBurns(t *testing.T) {
	fees := sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, math.NewInt(1000)))
	bank := newFeeBankKeeper(fees)
	f := initFixtureWithBank(t, bank)

	require.NoError(t, f.keeper.ProcessFeeSplit(f.ctx))

	require.Equal(t, int64(900), bank.collectorBalance.AmountOf(types.DefaultDenom).Int64(),
		"the 90%% validator share must stay in the fee collector for AllocateTokens")
	toDistribution := bank.moduleTransfers[authtypes.FeeCollectorName+"->"+distrtypes.ModuleName]
	require.True(t, toDistribution.IsZero(),
		"nothing may be bare-sent to the distribution module account (unaccounted = unwithdrawable)")
	require.Equal(t, int64(100), bank.burnt.AmountOf(types.DefaultDenom).Int64(),
		"10%% must be burned")
	toSecrets := bank.moduleTransfers[authtypes.FeeCollectorName+"->"+types.ModuleName]
	require.Equal(t, int64(100), toSecrets.AmountOf(types.DefaultDenom).Int64(),
		"the burn share routes through the module account (Burner permission)")

	var event *sdk.Event
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		if ev.Type == "fee_distribution" {
			e := ev
			event = &e
		}
	}
	require.NotNil(t, event, "the split must emit fee_distribution")
	attrs := map[string]string{}
	for _, a := range event.Attributes {
		attrs[a.Key] = a.Value
	}
	require.Equal(t, "900"+types.DefaultDenom, attrs["validator_fees"])
	require.Equal(t, "100"+types.DefaultDenom, attrs["burned_fees"])
}

// TestProcessFeeSplit_DustJoinsTheBurn: at awkward totals the validator
// share is floored and the remainder burns (deflation-biased).
func TestProcessFeeSplit_DustJoinsTheBurn(t *testing.T) {
	bank := newFeeBankKeeper(sdk.NewCoins(sdk.NewCoin(types.DefaultDenom, math.NewInt(15))))
	f := initFixtureWithBank(t, bank)

	require.NoError(t, f.keeper.ProcessFeeSplit(f.ctx))

	require.Equal(t, int64(13), bank.collectorBalance.AmountOf(types.DefaultDenom).Int64(),
		"the floored validator share stays in the fee collector")
	require.Equal(t, int64(2), bank.burnt.AmountOf(types.DefaultDenom).Int64())
}

// TestProcessFeeSplit_EmptyBlockNoOp: an empty fee collector produces no
// transfers, no burns, and no event.
func TestProcessFeeSplit_EmptyBlockNoOp(t *testing.T) {
	bank := newFeeBankKeeper(sdk.NewCoins())
	f := initFixtureWithBank(t, bank)

	require.NoError(t, f.keeper.ProcessFeeSplit(f.ctx))

	require.Empty(t, bank.moduleTransfers)
	require.True(t, bank.burnt.IsZero())
	for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
		require.NotEqual(t, "fee_distribution", ev.Type, "empty blocks must not emit the split event")
	}
}
