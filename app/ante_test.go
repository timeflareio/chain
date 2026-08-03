package app

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	secretstypes "github.com/timeflareio/chain/x/secrets/types"
)

// stubFeeTx implements sdk.FeeTx with just the fields the floor reads.
type stubFeeTx struct {
	gas uint64
	fee sdk.Coins
}

func (s stubFeeTx) GetMsgs() []sdk.Msg                    { return nil }
func (s stubFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (s stubFeeTx) GetGas() uint64                        { return s.gas }
func (s stubFeeTx) GetFee() sdk.Coins                     { return s.fee }
func (s stubFeeTx) FeePayer() []byte                      { return nil }
func (s stubFeeTx) FeeGranter() []byte                    { return nil }

// nonFeeTx implements sdk.Tx but NOT sdk.FeeTx.
type nonFeeTx struct{}

func (nonFeeTx) GetMsgs() []sdk.Msg                    { return nil }
func (nonFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

// runFloor drives the wrapped handler and reports whether the inner (stock)
// chain was reached.
func runFloor(t *testing.T, ctx sdk.Context, tx sdk.Tx, simulate bool) (reachedInner bool, err error) {
	t.Helper()
	inner := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		reachedInner = true
		return ctx, nil
	}
	_, err = feeFloorAnteHandler(inner)(ctx, tx, simulate)
	return reachedInner, err
}

func uveil(amount int64) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(secretstypes.DefaultDenom, math.NewInt(amount)))
}

// TestFeeFloor_ExactBoundary pins the ceiling arithmetic at the reference
// shape: 200,000 gas × 1/10 = 20,000 uveil required, exactly.
func TestFeeFloor_ExactBoundary(t *testing.T) {
	ctx := sdk.Context{}.WithBlockHeight(10)

	reached, err := runFloor(t, ctx, stubFeeTx{gas: 200_000, fee: uveil(20_000)}, false)
	require.NoError(t, err, "paying exactly the floor must pass")
	require.True(t, reached, "a compliant tx must reach the stock chain")

	reached, err = runFloor(t, ctx, stubFeeTx{gas: 200_000, fee: uveil(19_999)}, false)
	require.Error(t, err, "one uveil below the floor must be rejected")
	require.False(t, reached, "a rejected tx must never reach the stock chain")
}

// TestFeeFloor_CeilingDivision: gas that does not divide evenly rounds UP —
// no gas limit prices to zero.
func TestFeeFloor_CeilingDivision(t *testing.T) {
	ctx := sdk.Context{}.WithBlockHeight(10)

	// 9 gas × 1/10 = 0.9 → required 1
	_, err := runFloor(t, ctx, stubFeeTx{gas: 9, fee: sdk.NewCoins()}, false)
	require.Error(t, err, "even a 9-gas tx must pay at least 1 uveil")

	reached, err := runFloor(t, ctx, stubFeeTx{gas: 9, fee: uveil(1)}, false)
	require.NoError(t, err)
	require.True(t, reached)

	// 200,001 gas → ⌈20,000.1⌉ = 20,001
	_, err = runFloor(t, ctx, stubFeeTx{gas: 200_001, fee: uveil(20_000)}, false)
	require.Error(t, err, "ceiling division: 200,001 gas requires 20,001 uveil")
}

// TestFeeFloor_WrongDenomDoesNotCount: only uveil satisfies the floor.
func TestFeeFloor_WrongDenomDoesNotCount(t *testing.T) {
	ctx := sdk.Context{}.WithBlockHeight(10)
	fee := sdk.NewCoins(sdk.NewCoin("other", math.NewInt(1_000_000)))
	_, err := runFloor(t, ctx, stubFeeTx{gas: 200_000, fee: fee}, false)
	require.Error(t, err, "a fee paid entirely in another denom must be rejected")
}

// TestFeeFloor_GenesisExemption: gentxs execute through the ante chain at
// height 0 with no fee — the chain could not initialise without this.
func TestFeeFloor_GenesisExemption(t *testing.T) {
	ctx := sdk.Context{}.WithBlockHeight(0)
	reached, err := runFloor(t, ctx, stubFeeTx{gas: 200_000, fee: sdk.NewCoins()}, false)
	require.NoError(t, err, "zero-fee gentx must pass at genesis")
	require.True(t, reached, "genesis txs still run the stock chain")
}

// TestFeeFloor_SimulateExemption: fee-less gas estimation must keep working.
func TestFeeFloor_SimulateExemption(t *testing.T) {
	ctx := sdk.Context{}.WithBlockHeight(10)
	reached, err := runFloor(t, ctx, stubFeeTx{gas: 200_000, fee: sdk.NewCoins()}, true)
	require.NoError(t, err, "zero-fee simulation must pass")
	require.True(t, reached)
}

// TestFeeFloor_NonFeeTxRejected: a tx that cannot declare a fee cannot be
// priced, so it is rejected outright (matching the stock chain's stance).
func TestFeeFloor_NonFeeTxRejected(t *testing.T) {
	ctx := sdk.Context{}.WithBlockHeight(10)
	_, err := runFloor(t, ctx, nonFeeTx{}, false)
	require.Error(t, err)
}
