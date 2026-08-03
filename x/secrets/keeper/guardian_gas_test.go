package keeper_test

import (
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// The regression guard behind the guardian cost-recovery model.
//
// The creator escrows a guardian's two transactions at exactly
// MinRequiredFee(GuardianAcceptGas) and MinRequiredFee(GuardianRevealGas), and
// the daemon declares those same limits (guardian/blockchain/signer.go). The
// arrangement holds only while the handlers fit inside them. Nothing enforced
// that before: ordinary growth in either code path would have started charging
// guardians to do the job, silently, with no test failing.
//
// What this measures is HANDLER gas — store reads and writes inside the keeper.
// A real transaction also pays ante-handler costs (signature verification, tx
// size), which is why the budget below is a fraction of the reimbursed constant
// rather than the whole of it. The relationship was calibrated against the
// devnet, where a confirm consumed 89,514 of its 120,000 declared and a reveal
// 95,534 of 130,000 — leaving the handler roughly two thirds of the envelope
// once ante overhead is paid.
//
// If this trips, the fix is NOT to widen the allowance. It is either to trim
// the handler or to re-price the reimbursement constants — the latter being a
// protocol change that re-prices every future secret, which is exactly the
// decision the failure should force someone to make consciously.
//
// anteGasAllowance is what a real transaction spends OUTSIDE the handler:
// signature verification, tx-size gas, sequence and fee deduction. Derived by
// subtraction against the devnet, where a 7-guardian confirm consumed 89,514
// end to end against 70,282 of handler measured here. Rounded up.
const anteGasAllowance = 20_000

func handlerGasBudget(reimbursed uint64) uint64 {
	return reimbursed - anteGasAllowance
}

// measureGas runs fn against a fresh metered context and reports what it burned.
//
// ⚠️ Valid only for handlers that do no BANK work. The fixture injects a mock
// bank keeper, so coin transfers cost almost nothing here while the real one
// reads and writes an account per transfer. The accept and reveal legs below
// move no coins — acceptance locks float on the guardian record, revelation
// writes a reveal record — so the measurement holds for them. It does NOT hold
// for anything that pays out: a cancel, which sends coins per active guardian,
// measured ~45% under its real cost this way (1,113,210 here against 2,067,532
// on a real chain at the band ceiling) and shipped an out-of-gas defect twice
// before that was understood. Measure bank-heavy paths against a real chain.
func measureGas(t *testing.T, f *fixture, fn func(ctx sdk.Context)) uint64 {
	t.Helper()
	metered := sdk.UnwrapSDKContext(f.ctx).WithGasMeter(storetypes.NewGasMeter(50_000_000))
	before := metered.GasMeter().GasConsumed()
	fn(metered)
	return metered.GasMeter().GasConsumed() - before
}

// TestGuardianAcceptGasFitsReimbursement sweeps the share band, because the
// reimbursement is a FLAT constant: a per-share term creeping into either
// handler would break it at the ceiling while every small-band test stayed
// green.
//
// The sweep runs to the protocol ceiling, and that is only affordable because
// the acceptance tally was moved out of the secret record. While an acceptance
// rewrote the whole record — one address and one frozen bond per selected
// guardian — accept gas grew by ~4,200 per guardian and reached 177,148 at the
// ceiling, so a flat 120,000 reimbursement stopped covering it at about fifteen
// guardians: below the shipped Maximum preset. With the tally in its own key
// the slope is ~430 per guardian and the ceiling costs 48,144.
//
// Keep the ceiling case. It is the one that failed before, and the per-share
// term it guards against is exactly what would creep back in if some future
// handler started rewriting the roster again.
func TestGuardianAcceptGasFitsReimbursement(t *testing.T) {
	bands := []struct {
		name                 string
		threshold, maxShares int64
	}{
		{"narrowest band", 2, 2},
		{"standard tier", 3, 7},
		{"high tier", 5, 12},
		{"maximum tier", 9, 20},
		{"protocol ceiling", 16, 32},
	}

	budget := handlerGasBudget(types.GuardianAcceptGas)

	for _, band := range bands {
		t.Run(band.name, func(t *testing.T) {
			f := initFixture(t)
			msgServer := keeper.NewMsgServerImpl(f.keeper)
			f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

			secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, band.threshold, band.maxShares)
			secret, err := f.keeper.GetSecret(f.ctx, secretId)
			require.NoError(t, err)

			var confirmErr error
			used := measureGas(t, f, func(ctx sdk.Context) {
				_, confirmErr = msgServer.GuardianConfirmShares(ctx, &types.MsgGuardianConfirmShares{
					Guardian: secret.SelectedGuardians[0],
					SecretId: secretId,
					Accept:   true,
				})
			})
			require.NoError(t, confirmErr)

			t.Logf("accept handler gas at max_shares=%d: %d (budget %d of %d reimbursed)",
				band.maxShares, used, budget, types.GuardianAcceptGas)
			require.LessOrEqual(t, used, budget,
				"the accept handler no longer fits the gas the creator reimburses; "+
					"trim the handler or re-price GuardianAcceptGas — do not raise the fraction")
		})
	}
}

// TestGuardianRevealGasFitsReimbursement is the reveal-leg counterpart. The
// reveal is the leg that must never fail: a guardian that cannot get it through
// is slashed for a no-show, so its envelope needs the same guard.
//
// No band sweep here, unlike the accept leg. A reveal writes one guardian's own
// record and reads the secret; it does not walk the assignment set, and the
// devnet bears that out — every observed reveal declared an identical limit
// regardless of the secret's band. The accept path is the one that touches the
// roster, so that is where a per-share term would appear first.
func TestGuardianRevealGasFitsReimbursement(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	guardian := sdk.AccAddress([]byte("gas_reveal_guardian_"))
	creator := sdk.AccAddress([]byte("gas_reveal_creator__"))
	secretId, shareDataMap := setupTestSecretWithShare(t, f, msgServer, guardian, creator, "gas_reveal")

	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(secret.RevealStartBlock)

	accepted := acceptedGuardians(t, f, secretId)
	require.NotEmpty(t, accepted, "no accepted guardians to reveal with")
	revealer := accepted[0]
	share := shareDataMap[revealer]
	require.NotNil(t, share, "no share recorded for the revealing guardian")

	budget := handlerGasBudget(types.GuardianRevealGas)
	var revealErr error
	used := measureGas(t, f, func(ctx sdk.Context) {
		_, revealErr = msgServer.GuardianRevealShare(ctx, &types.MsgGuardianRevealShare{
			Guardian:       revealer,
			SecretId:       secretId,
			DecryptedShare: share.DecryptedShare,
		})
	})
	require.NoError(t, revealErr)

	t.Logf("reveal handler gas: %d (budget %d of %d reimbursed)",
		used, budget, types.GuardianRevealGas)
	require.LessOrEqual(t, used, budget,
		"the reveal handler no longer fits the gas the creator reimburses; "+
			"trim the handler or re-price GuardianRevealGas — do not raise the fraction")
}

// TestAcceptedCountSurvivesEveryQueryPath guards the join the tally split
// requires.
//
// Moving the acceptance tally into its own record means anything that reads a
// secret STRAIGHT FROM THE COLLECTION sees a zero. GetSecret joins it, but the
// paginated queries hand the collection value to the view assembler directly —
// and that path shipped returning acceptedCount 0 while the single-secret
// query returned the right number. The invariants did not catch it because
// they walk state, not queries.
func TestAcceptedCountSurvivesEveryQueryPath(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	qs := keeper.NewQueryServerImpl(f.keeper)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(100)

	secretId := setupBondTestSecret(t, f, msgServer, types.MinBump, 3, 5)
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)

	const accepting = 3
	for i := 0; i < accepting; i++ {
		_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
			Guardian: secret.SelectedGuardians[i],
			SecretId: secretId,
			Accept:   true,
		})
		require.NoError(t, err)
	}

	t.Run("single-secret query", func(t *testing.T) {
		got, err := qs.Secret(f.ctx, &types.QuerySecretRequest{SecretId: secretId})
		require.NoError(t, err)
		require.Equal(t, int64(accepting), got.Secret.AcceptedCount)
	})

	t.Run("paginated list query", func(t *testing.T) {
		got, err := qs.Secrets(f.ctx, &types.QuerySecretsRequest{})
		require.NoError(t, err)
		require.NotEmpty(t, got.Secrets)
		for _, v := range got.Secrets {
			if v.Id == secretId {
				require.Equal(t, int64(accepting), v.AcceptedCount,
					"the paginated path reads the collection directly and must still join the tally")
				return
			}
		}
		t.Fatalf("secret %s missing from the paginated listing", secretId)
	})

	t.Run("creator-scoped query", func(t *testing.T) {
		got, err := qs.SecretsByCreator(f.ctx, &types.QuerySecretsByCreatorRequest{
			Creator: secret.Creator,
		})
		require.NoError(t, err)
		require.NotEmpty(t, got.Secrets)
		require.Equal(t, int64(accepting), got.Secrets[0].AcceptedCount)
	})
}
