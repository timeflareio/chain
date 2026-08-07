package keeper_test

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Lifecycle fuzzer (test strategy plan §3 tier 2, §9.3): a seeded,
// deterministic random walk over the full message surface — register,
// deposit, withdraw, create, accept, reject, reveal, report, cancel — with
// the EndBlock sweeps and the ENTIRE invariant library (including exact
// solvency) checked after every block.
//
// CI runs the fixed seed set below. Deep/periodic runs:
//
//	FUZZ_BLOCKS=5000 go test ./x/secrets/keeper/ -run TestLifecycleFuzz
//	FUZZ_SEED=12345  go test ./x/secrets/keeper/ -run TestLifecycleFuzz
//
// Every failure message carries the seed and block; replay is exact.

// fuzzProfile weights the action mix. The settlement-biased profile reaches
// window-end settlement often; the chaos profile leans on cancellation,
// leak-reporting and under-capitalised guardians.
type fuzzProfile struct {
	name        string
	bumps       []int64
	pAccept     float64
	pReject     float64
	pReveal     float64
	pCancel     float64
	pReport     float64
	pRegister   float64
	pTopUp      float64
	pWithdraw   float64
	maxGuardian int
	maxLive     int
}

var settlementBiased = fuzzProfile{
	name:    "settlement-biased",
	bumps:   []int64{100, 150, 250},
	pAccept: 0.85, pReject: 0.03, pReveal: 0.65,
	pCancel: 0.02, pReport: 0.02,
	pRegister: 0.30, pTopUp: 0.05, pWithdraw: 0.03,
	maxGuardian: 12, maxLive: 4,
}

var chaos = fuzzProfile{
	name:    "chaos",
	bumps:   []int64{100, 333, 999, 1000},
	pAccept: 0.50, pReject: 0.10, pReveal: 0.35,
	pCancel: 0.10, pReport: 0.15,
	pRegister: 0.40, pTopUp: 0.10, pWithdraw: 0.10,
	maxGuardian: 10, maxLive: 5,
}

// Errors a random walk legitimately provokes; anything else fails the run.
var expectedFuzzErrors = []string{
	"insufficient unlocked float",
	"insufficient guardians",
	"already responded",
	"already revealed",
	"already slashed",
	"slots for secret",
	"awaiting_acceptance state",
	"commit deadline passed",
	"reveal window",
	"can only cancel",
	"cannot cancel secret after reveal window",
	"not currently active",
	"no unlocked float",
	"guardian has no accepted assignment",
	"cannot slash themselves",
	"pending or reconstructable state",
}

func fuzzTolerable(err error) bool {
	if err == nil {
		return true
	}
	for _, s := range expectedFuzzErrors {
		if strings.Contains(err.Error(), s) {
			return true
		}
	}
	return false
}

func TestLifecycleFuzz(t *testing.T) {
	blocks := int64(500)
	if v := os.Getenv("FUZZ_BLOCKS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		require.NoError(t, err, "FUZZ_BLOCKS must be an integer")
		blocks = n
	}

	type run struct {
		seed    int64
		profile fuzzProfile
	}
	runs := []run{
		{1, settlementBiased}, {2, settlementBiased}, {3, settlementBiased},
		{101, chaos}, {102, chaos},
	}
	if v := os.Getenv("FUZZ_SEED"); v != "" {
		seed, err := strconv.ParseInt(v, 10, 64)
		require.NoError(t, err, "FUZZ_SEED must be an integer")
		runs = []run{{seed, settlementBiased}, {seed, chaos}}
	}

	for _, r := range runs {
		r := r
		t.Run(fmt.Sprintf("%s_seed=%d", r.profile.name, r.seed), func(t *testing.T) {
			fuzzOneRun(t, r.seed, blocks, r.profile)
		})
	}
}

func fuzzOneRun(t *testing.T, seed, blocks int64, p fuzzProfile) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	bank := newLedgerBankKeeper()
	f := initFixtureWithBank(t, bank)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	setHeight(f, 10)

	creator := sdk.AccAddress([]byte("fuzz_creator________")).String()
	reporter := sdk.AccAddress([]byte("fuzz_reporter_______")).String()

	guardians := []string{}
	// shares[secretId][guardian] = the plaintext share (reveal + evidence material)
	shares := map[string]map[string][]byte{}

	step := func(desc string, err error) {
		if !fuzzTolerable(err) {
			t.Fatalf("seed=%d height=%d: unexpected error from %s: %v", seed, height(f), desc, err)
		}
	}

	for block := int64(0); block < blocks; block++ {
		setHeight(f, height(f)+1+int64(rng.Intn(2))) // irregular block advance

		// ── Guardian population ──────────────────────────────────────────
		if len(guardians) < p.maxGuardian && rng.Float64() < p.pRegister {
			name := fmt.Sprintf("fz%d_g%03d", seed, len(guardians))
			deposit := testFloatUnit().MulRaw(int64(1 + rng.Intn(8))) // 1–8 bond units
			addr := registerConformanceGuardian(t, f, msgServer, name, deposit)
			guardians = append(guardians, addr)
		}
		if len(guardians) > 0 && rng.Float64() < p.pTopUp {
			g := guardians[rng.Intn(len(guardians))]
			topUp := sdk.NewCoin(types.DefaultDenom, testFloatUnit().MulRaw(int64(1+rng.Intn(3))))
			_, err := msgServer.GuardianUpdate(f.ctx, &types.MsgGuardianUpdate{Guardian: g, Deposit: &topUp})
			step("top-up", err)
		}
		if len(guardians) > 0 && rng.Float64() < p.pWithdraw {
			g := guardians[rng.Intn(len(guardians))]
			_, err := msgServer.GuardianWithdrawStake(f.ctx, &types.MsgGuardianWithdrawStake{Guardian: g})
			step("withdraw", err)
		}

		// ── Secret creation (Phases 1+2) ─────────────────────────────────
		live := 0
		require.NoError(t, f.keeper.Secrets.Walk(f.ctx, nil, func(_ string, s types.Secret) (bool, error) {
			if !s.IsComplete() {
				live++
			}
			return false, nil
		}))
		if live < p.maxLive && len(guardians) >= 3 && rng.Float64() < 0.5 {
			bump := p.bumps[rng.Intn(len(p.bumps))]
			threshold := int64(2)
			nShares := int64(2 + rng.Intn(3)) // 2–4
			startOffset := types.MinRevealStartOffsetTotal + int64(rng.Intn(30))

			secretId, err := fuzzCreateSecret(t, f, msgServer, creator, bump, threshold, nShares, startOffset, shares)
			step("create", err)
			_ = secretId
		}

		// ── React to every live secret based on on-chain state ───────────
		require.NoError(t, f.keeper.Secrets.Walk(f.ctx, nil, func(id string, s types.Secret) (bool, error) {
			switch s.State {
			case types.SECRET_STATUS_AWAITING_ACCEPTANCE:
				for _, g := range s.SelectedGuardians {
					record, err := f.keeper.GetAssignment(f.ctx, id, g)
					if err != nil || record.Status != types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED {
						continue
					}
					roll := rng.Float64()
					if roll < p.pAccept {
						step("accept", acceptAs(t, f, msgServer, id, g))
					} else if roll < p.pAccept+p.pReject {
						_, err := msgServer.GuardianConfirmShares(f.ctx, &types.MsgGuardianConfirmShares{
							Guardian: g, SecretId: id, Accept: false,
						})
						step("reject", err)
					}
				}
			case types.SECRET_STATUS_PENDING, types.SECRET_STATUS_RECONSTRUCTABLE:
				h := height(f)
				active := acceptedGuardians(t, f, id)
				if h < s.RevealStartBlock {
					// Pre-window: maybe cancel, maybe report a leak
					if rng.Float64() < p.pCancel {
						_, err := msgServer.UserCancelSecret(f.ctx, &types.MsgUserCancelSecret{SecretId: id, Creator: creator})
						step("cancel", err)
						return false, nil
					}
					if rng.Float64() < p.pReport && len(active) > 0 {
						leaker := active[rng.Intn(len(active))]
						if ev, ok := shares[id][leaker]; ok {
							_, err := msgServer.SlashGuardian(f.ctx, &types.MsgSlashGuardian{
								GuardianAddress: leaker,
								ReporterAddress: reporter,
								Reason:          "fuzz leak",
								Evidence:        ev,
								SecretId:        id,
							})
							step("report", err)
						}
					}
				} else if h <= s.RevealEndBlock {
					// In-window: each unrevealed accepted guardian may reveal
					for _, g := range active {
						if f.keeper.HasGuardianRevealed(f.ctx, id, g) {
							continue
						}
						if rng.Float64() < p.pReveal {
							if share, ok := shares[id][g]; ok {
								_, err := msgServer.GuardianRevealShare(f.ctx, &types.MsgGuardianRevealShare{
									Guardian: g, SecretId: id, DecryptedShare: share,
								})
								step("reveal", err)
							}
						}
					}
				}
			}
			return false, nil
		}))

		// ── EndBlock sweeps ───────────────────────────────────────────────
		require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx), "seed=%d height=%d", seed, height(f))
		require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx), "seed=%d height=%d", seed, height(f))

		// ── The full invariant library, every block ───────────────────────
		assertInvariants(t, f)
		assertSolvency(t, f, bank)
	}

	// End of run: force-settle everything still live and re-check
	maxDue := int64(0)
	require.NoError(t, f.keeper.Secrets.Walk(f.ctx, nil, func(_ string, s types.Secret) (bool, error) {
		if !s.IsComplete() && s.RevealEndBlock+1 > maxDue {
			maxDue = s.RevealEndBlock + 1
		}
		if !s.IsComplete() && s.CommitDeadline+1 > maxDue {
			maxDue = s.CommitDeadline + 1
		}
		return false, nil
	}))
	if maxDue > height(f) {
		setHeight(f, maxDue)
	}
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))
	require.NoError(t, f.keeper.ProcessExpiredRevealWindows(f.ctx))
	assertInvariants(t, f)
	assertSolvency(t, f, bank)

	// After the drain every secret must be terminal and every bond released
	require.NoError(t, f.keeper.Secrets.Walk(f.ctx, nil, func(id string, s types.Secret) (bool, error) {
		require.True(t, s.IsComplete(), "seed=%d: secret %s stranded in state %s after final drain", seed, id, s.State)
		return false, nil
	}))
}

// fuzzCreateSecret drives Phases 1+2, recording each guardian's plaintext
// share for later reveals and leak evidence.
func fuzzCreateSecret(t *testing.T, f *fixture, msgServer types.MsgServer, creator string, bump, threshold, nShares, startOffset int64, shares map[string]map[string][]byte) (string, error) {
	t.Helper()
	resp, err := msgServer.UserRequestGuardians(f.ctx, &types.MsgUserRequestGuardians{
		Creator:           creator,
		DetectionHint:     testDetectionHint(),
		Threshold:         threshold,
		MinShares:         nShares,
		MaxShares:         nShares,
		RevealStartOffset: startOffset,
		Bump:              bump,
	})
	if err != nil {
		return "", err
	}

	secret, err := f.keeper.GetSecret(f.ctx, resp.SecretId)
	require.NoError(t, err)
	shares[resp.SecretId] = map[string][]byte{}
	shareData := make([]*types.EncryptedShareData, 0, len(secret.SelectedGuardians))
	for _, guardianAddress := range secret.SelectedGuardians {
		// 32B deterministic stand-in — key-share-envelope scale, satisfies the
		// MinEvidenceLength floor and the MaxRevealedKeyShareSize ceiling
		data := testShareBytes(resp.SecretId, guardianAddress)
		shares[resp.SecretId][guardianAddress] = data
		shareData = append(shareData, &types.EncryptedShareData{
			GuardianAddress: guardianAddress,
			EncryptedShare:  data,
			ShareHmac:       generateTestHMAC(resp.SecretId, guardianAddress, data),
		})
	}
	_, err = msgServer.UserDistributeShares(f.ctx, &types.MsgUserDistributeShares{
		Creator:           creator,
		SecretId:          resp.SecretId,
		SecretCommitment:  []byte("fuzz_commitment"),
		PayloadCiphertext: testPayloadCiphertext(),
		SecretPublicKey:   testSecretPublicKey(),
		Shares:            shareData,
	})
	require.NoError(t, err)
	return resp.SecretId, nil
}
