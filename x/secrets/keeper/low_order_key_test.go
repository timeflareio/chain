package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Small-order key rejection (docs/planning/done/DONE_LOW_ORDER_KEY_VALIDATION_PLAN.md).
//
// An X25519 exchange against a small-order point yields an all-zero shared
// secret, so anything encrypted to such a public key is encrypted under a
// publicly computable key. Before this was enforced, six of the seven table
// entries below registered cleanly — only all-zeros was caught — and any share
// the WASM client encrypted to one was readable by any observer.
//
// smallOrderKeys is libsodium's has_small_order table: five canonical
// u-encodings plus two non-canonical ones that reduce to small-order points.
// The last two are why the chain delegates to curve25519 instead of comparing
// against a table of its own.
var smallOrderKeys = [][]byte{
	make([]byte, 32),
	append([]byte{1}, make([]byte, 31)...),
	{0xe0, 0xeb, 0x7a, 0x7c, 0x3b, 0x41, 0xb8, 0xae, 0x16, 0x56, 0xe3, 0xfa, 0xf1, 0x9f, 0xc4, 0x6a,
		0xda, 0x09, 0x8d, 0xeb, 0x9c, 0x32, 0xb1, 0xfd, 0x86, 0x62, 0x05, 0x16, 0x5f, 0x49, 0xb8, 0x00},
	{0x5f, 0x9c, 0x95, 0xbc, 0xa3, 0x50, 0x8c, 0x24, 0xb1, 0xd0, 0xb1, 0x55, 0x9c, 0x83, 0xef, 0x5b,
		0x04, 0x44, 0x5c, 0xc4, 0x58, 0x1c, 0x8e, 0x86, 0xd8, 0x22, 0x4e, 0xdd, 0xd0, 0x9f, 0x11, 0x57},
	{0xec, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f},
	{0xed, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f},
	{0xee, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f},
}

func TestGuardianRegister_RejectsSmallOrderEncryptionKeys(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	for i, key := range smallOrderKeys {
		t.Run(fmt.Sprintf("point_%d", i), func(t *testing.T) {
			guardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("lowordr_reg_%d", i)))
			_, err := msgServer.GuardianRegister(f.ctx, &types.MsgGuardianRegister{
				Guardian:            guardianAddr.String(),
				EncryptionPublicKey: key,
				AvailableFrom:       100,
				AvailableUntil:      1000,
				Deposit:             &validStake,
				AcceptingSecrets:    true,
			})
			require.Error(t, err, "small-order key %x… must not register", key[:4])
			require.Contains(t, err.Error(), "not a usable X25519 public key")

			_, found := f.keeper.GetGuardian(f.ctx, guardianAddr.String())
			require.False(t, found, "no guardian record may be created for a rejected key")
		})
	}
}

func TestGuardianRotateKey_RejectsSmallOrderKeys(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	// Rotation is the one place a guardian could otherwise downgrade itself to
	// an unusable key after passing registration.
	g := registerRotationGuardian(t, f, msgServer, "rot_lowordr", testFloatUnit())
	setHeight(f, firstRotationHeight(100))

	for i, key := range smallOrderKeys {
		_, err := rotateAs(f, msgServer, g, key)
		require.Error(t, err, "small-order key %d must not be rotatable to", i)
		require.Contains(t, err.Error(), "not a usable X25519 public key")
	}
}

func TestValidKeysStillRegister(t *testing.T) {
	// The guard against over-rejection: keys the protocol itself generates must
	// continue to register. A check that rejected honest guardians would be a
	// worse defect than the one it closes.
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	validStake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())

	for i := 0; i < 10; i++ {
		guardianAddr := sdk.AccAddress([]byte(fmt.Sprintf("lowordr_ok_%d", i)))
		_, err := msgServer.GuardianRegister(f.ctx, &types.MsgGuardianRegister{
			Guardian:            guardianAddr.String(),
			EncryptionPublicKey: getValidPublicKey(fmt.Sprintf("lowordr_ok_key_%d", i)),
			AvailableFrom:       100,
			AvailableUntil:      1000,
			Deposit:             &validStake,
			AcceptingSecrets:    true,
		})
		require.NoError(t, err, "a freshly generated X25519 key must register")
	}
}

func TestUserRequestGuardians_RejectsSmallOrderDetectionHint(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	registerTestGuardians(t, f, msgServer, 25)

	creator := sdk.AccAddress([]byte("creator_address"))

	for i, key := range smallOrderKeys {
		hint := testDetectionHint()
		hint.EphemeralPub = key

		_, err := msgServer.UserRequestGuardians(f.ctx, &types.MsgUserRequestGuardians{
			Creator:           creator.String(),
			DetectionHint:     hint,
			Threshold:         3,
			MinShares:         15,
			MaxShares:         17,
			RevealStartOffset: types.MinRevealStartOffsetTotal,
			Bump:              types.MinBump,
		})
		require.Error(t, err, "small-order hint key %d would match every recipient", i)
		require.Contains(t, err.Error(), "detection hint ephemeral key is not a usable X25519 public key")
	}
}

func TestGenesis_ImportSweepRejectsSmallOrderGuardianKey(t *testing.T) {
	// The genesis path: state is assembled outside the message handlers, so the
	// handlers' rejection is not enough on its own. InitGenesis runs the
	// state-integrity sweep behind a hard halt — a genesis carrying an unusable
	// key must never produce blocks.
	f := initFixture(t)

	total := sdk.NewCoin(types.DefaultDenom, math.NewInt(1_000_000))
	zero := sdk.NewCoin(types.DefaultDenom, math.ZeroInt())
	err := f.keeper.InitGenesis(f.ctx, types.GenesisState{
		Guardians: []*types.Guardian{{
			Address:             "tmflr1lowordergenesis",
			EncryptionPublicKey: smallOrderKeys[1],
			AvailableFrom:       0,
			AvailableUntil:      1000,
			Stake:               &total,
			LockedStake:         &zero,
			BondK:               types.InitialBondK,
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "state-integrity sweep")
	require.Contains(t, err.Error(), "invariant 8")
	require.Contains(t, err.Error(), "unusable encryption key")
}

func TestUserDistributeShares_RejectsSmallOrderSecretPublicKey(t *testing.T) {
	// pk_s: a small-order value would make the payload ciphertext C decryptable
	// by any observer. Self-inflicted, but rejected uniformly rather than carved
	// out as an argued exception.
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	secretId := createTestSecret(t, f, msgServer)
	shares := createValidShares(t, f, secretId)
	creator := sdk.AccAddress([]byte("creator_address"))

	for i, key := range smallOrderKeys {
		_, err := msgServer.UserDistributeShares(f.ctx, &types.MsgUserDistributeShares{
			Creator:           creator.String(),
			SecretId:          secretId,
			Shares:            shares,
			SecretCommitment:  []byte("commitment"),
			PayloadCiphertext: []byte("ciphertext"),
			SecretPublicKey:   key,
		})
		require.Error(t, err, "small-order pk_s %d must be rejected", i)
		require.Contains(t, err.Error(), "pk_s")
	}
}
