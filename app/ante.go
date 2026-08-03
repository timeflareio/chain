package app

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	secretstypes "github.com/timeflareio/chain/x/secrets/types"
)

// feeFloorAnteHandler wraps the runtime-assembled ante chain with the
// consensus-enforced fee floor (DONE_CONSENSUS_FEE_FLOOR_PLAN.md): every
// transaction must pay at least
//
//	⌈gas_limit × MinGasPriceUveilNum ÷ MinGasPriceUveilDen⌉ uveil
//
// in BOTH CheckTx and DeliverTx. The stock DeductFeeDecorator checks each
// node's app.toml `minimum-gas-prices` in CheckTx only, so block execution
// would otherwise accept zero-fee transactions from any proposer — competing
// both validator gas revenue and the guaranteed 10% burn to zero.
//
// Wrapping (rather than reassembling the stock decorator list) makes
// dropping a stock decorator impossible by construction: the inner handler
// is exactly what the SDK's tx module installed at build time.
//
// Exactly two execution modes are exempt, and no real transaction executes
// in either:
//   - genesis (block height 0): gentxs run through the ante chain at chain
//     initialisation and carry no fee — without this the chain cannot start;
//   - simulation: fee-less gas estimation.
func feeFloorAnteHandler(next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if !simulate && ctx.BlockHeight() > 0 {
			feeTx, ok := tx.(sdk.FeeTx)
			if !ok {
				return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "tx must implement FeeTx")
			}
			required := secretstypes.MinRequiredFee(feeTx.GetGas())
			paid := feeTx.GetFee().AmountOf(secretstypes.DefaultDenom)
			if paid.LT(required) {
				return ctx, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee,
					"consensus-enforced fee floor: got %s%s, required %s%s (gas %d at %d/%d uveil per gas)",
					paid, secretstypes.DefaultDenom, required, secretstypes.DefaultDenom,
					feeTx.GetGas(), secretstypes.MinGasPriceUveilNum, secretstypes.MinGasPriceUveilDen)
			}
		}
		return next(ctx, tx, simulate)
	}
}
