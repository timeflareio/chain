package keeper_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/module"
	"github.com/timeflareio/chain/x/secrets/types"
)

// Test constants
const testRevealDuration = int64(150)

// testFloatUnit is the fixture float-sizing unit: 5,000 VEIL in uveil — the
// retired flat bond amount, kept as a capital scale so existing fixture
// relationships stay recognisable. Actual bonds are duration-anchored now
// (B = rate × distance × bump × k) and far smaller than this at devnet-scale
// distances, so a float of one unit comfortably covers many bonds.
func testFloatUnit() math.Int { return math.NewInt(5_000_000_000) }

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
}

func initFixture(t *testing.T) *fixture {
	t.Helper()
	return initFixtureWithBank(t, &mockBankKeeper{})
}

// initFixtureWithBank builds a fixture with a caller-supplied bank keeper, so tests
// that need to observe bank interactions (e.g. counting refund calls) can inject an
// instrumented implementation.
func initFixtureWithBank(t *testing.T, bank keeper.BankKeeper) *fixture {
	t.Helper()

	// Configure SDK to use tmflr prefix for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("tmflr", "tmflrpub")
	config.SetBech32PrefixForValidator("tmflrvaloper", "tmflrvaloperpub")
	config.SetBech32PrefixForConsensusNode("tmflrvalcons", "tmflrvalconspub")
	// Note: config.Seal() may fail if already sealed, that's okay

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec("tmflr")
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	k := keeper.NewKeeper(
		encCfg.Codec,
		addressCodec,
		storeService,
		&mockAuthKeeper{},
		bank,
	)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
	}
}

type mockBankKeeper struct{}

func (mockBankKeeper) SendCoinsFromAccountToModule(
	ctx context.Context,
	fromAddr sdk.AccAddress,
	toModule string,
	amt sdk.Coins,
) error {
	return nil
}

func (mockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}

func (mockBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	return nil
}

func (mockBankKeeper) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(
		sdk.NewCoin(types.DefaultDenom, math.NewInt(1_000_000_000_000)),
	)
}

func (mockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(
		sdk.NewCoin(types.DefaultDenom, math.NewInt(1_000_000_000_000)),
	)
}

func (mockBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}

func (mockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, math.NewInt(1_000_000_000_000))
}

func (mockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}

type mockAuthKeeper struct{}

func (mockAuthKeeper) AddressCodec() address.Codec {
	return addresscodec.NewBech32Codec("tmflr")
}

func (mockAuthKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	// Return a base account for testing
	return authtypes.NewBaseAccountWithAddress(addr)
}

func (mockAuthKeeper) GetModuleAddress(moduleName string) sdk.AccAddress {
	return authtypes.NewModuleAddress(moduleName)
}

func TestKeeper_SecretManagement(t *testing.T) {
	f := initFixture(t)

	secretID := types.GenerateValidSecretID()

	// Test HasSecret for non-existent secret
	require.False(t, f.keeper.HasSecret(f.ctx, secretID))

	// Test GetSecret for non-existent secret
	_, err := f.keeper.GetSecret(f.ctx, secretID)
	require.Error(t, err)

	// Create and set a secret
	secret := types.Secret{
		Id:        secretID,
		Creator:   "tmflr1creator",
		Threshold: 3,
		State:     types.SECRET_STATUS_PENDING,
		RewardPool: sdk.Coin{
			Denom:  types.DefaultDenom,
			Amount: math.NewInt(1000_000_000),
		},
	}

	err = f.keeper.SetSecret(f.ctx, secret)
	require.NoError(t, err)

	// Test HasSecret for existing secret
	require.True(t, f.keeper.HasSecret(f.ctx, secretID))

	// Test GetSecret for existing secret
	retrievedSecret, err := f.keeper.GetSecret(f.ctx, secretID)
	require.NoError(t, err)
	require.Equal(t, secret.Id, retrievedSecret.Id)
	require.Equal(t, secret.Creator, retrievedSecret.Creator)
	require.Equal(t, secret.Threshold, retrievedSecret.Threshold)
}

func TestKeeper_GuardianManagement(t *testing.T) {
	f := initFixture(t)

	guardianAddr := "tmflr1guardian"

	// Test GetGuardian for non-existent guardian
	_, found := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.False(t, found)

	// Create and set a guardian
	stake := sdk.NewCoin(types.DefaultDenom, testFloatUnit())
	guardian := types.Guardian{
		Address:             guardianAddr,
		EncryptionPublicKey: getValidPublicKey("test_key"),
		AvailableFrom:       100,
		AvailableUntil:      1000,
		Stake:               &stake,
		AcceptingSecrets:    true,
	}

	err := f.keeper.SetGuardian(f.ctx, guardian)
	require.NoError(t, err)

	// Test GetGuardian for existing guardian
	retrievedGuardian, found := f.keeper.GetGuardian(f.ctx, guardianAddr)
	require.True(t, found)
	require.Equal(t, guardian.Address, retrievedGuardian.Address)
	require.Equal(t, guardian.EncryptionPublicKey, retrievedGuardian.EncryptionPublicKey)
	require.Equal(t, guardian.Stake, retrievedGuardian.Stake)
}

func TestKeeper_IsGuardianActive(t *testing.T) {
	f := initFixture(t)
	currentBlock := sdk.UnwrapSDKContext(f.ctx).BlockHeight()

	testCases := []struct {
		name        string
		guardian    types.Guardian
		expected    bool
		description string
	}{
		{
			name: "active guardian",
			guardian: types.Guardian{
				Address:        "tmflr1active",
				AvailableFrom:  currentBlock - 10,
				AvailableUntil: currentBlock + 100,
				Stake:          &sdk.Coin{Denom: types.DefaultDenom, Amount: testFloatUnit()},
			},
			expected:    true,
			description: "Guardian in availability window",
		},
		{
			name: "not yet available",
			guardian: types.Guardian{
				Address:        "tmflr1future",
				AvailableFrom:  currentBlock + 10,
				AvailableUntil: currentBlock + 100,
				Stake:          &sdk.Coin{Denom: types.DefaultDenom, Amount: testFloatUnit()},
			},
			expected:    false,
			description: "Guardian not yet available",
		},
		{
			name: "availability expired",
			guardian: types.Guardian{
				Address:        "tmflr1expired",
				AvailableFrom:  currentBlock - 100,
				AvailableUntil: currentBlock - 10,
				Stake:          &sdk.Coin{Denom: types.DefaultDenom, Amount: testFloatUnit()},
			},
			expected:    false,
			description: "Guardian availability expired",
		},
		{
			name: "empty float is still active (capital checks are per secret, not here)",
			guardian: types.Guardian{
				Address:        "tmflr1nostake",
				AvailableFrom:  currentBlock - 10,
				AvailableUntil: currentBlock + 100,
				Stake:          nil,
			},
			expected:    true,
			description: "Bonded model: activity is registration + availability only; float is checked per secret",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set guardian in store
			err := f.keeper.SetGuardian(f.ctx, tc.guardian)
			require.NoError(t, err)

			// Test IsGuardianActive
			result := f.keeper.IsGuardianActive(f.ctx, tc.guardian.Address)
			require.Equal(t, tc.expected, result, tc.description)
		})
	}

	// Test empty address
	require.False(t, f.keeper.IsGuardianActive(f.ctx, ""))
}

func TestKeeper_HasGuardianRevealed(t *testing.T) {
	guardianAddr := "tmflr1guardian"

	f := initFixture(t)

	// Secret with no reveals
	noReveals := types.GenerateValidSecretID()
	require.False(t, f.keeper.HasGuardianRevealed(f.ctx, noReveals, guardianAddr))

	// Secret with this guardian's reveal recorded
	withReveal := types.GenerateValidSecretID()
	writeReveal(t, f, withReveal, guardianAddr, []byte("revealed_data"), 100)
	require.True(t, f.keeper.HasGuardianRevealed(f.ctx, withReveal, guardianAddr))

	// Secret where only another guardian revealed
	otherReveal := types.GenerateValidSecretID()
	writeReveal(t, f, otherReveal, "tmflr1other", []byte("revealed_data"), 100)
	require.False(t, f.keeper.HasGuardianRevealed(f.ctx, otherReveal, guardianAddr))
}

func TestKeeper_NextSecretId(t *testing.T) {
	f := initFixture(t)

	// IDs are protocol-assigned from the monotonic counter: UUIDv5-shaped
	// (36 chars), strictly sequential counters, and unique
	uuidV5Pattern := `^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`

	id1, counter1, err := f.keeper.NextSecretId(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 36, len(id1))
	require.Regexp(t, uuidV5Pattern, id1)

	id2, counter2, err := f.keeper.NextSecretId(f.ctx)
	require.NoError(t, err)
	require.Regexp(t, uuidV5Pattern, id2)
	require.Equal(t, counter1+1, counter2)
	require.NotEqual(t, id1, id2)

	// Derivation is deterministic: same (chainID, counter) -> same ID
	chainID := sdk.UnwrapSDKContext(f.ctx).ChainID()
	require.Equal(t, id1, keeper.DeriveSecretId(chainID, counter1))
	require.Equal(t, id2, keeper.DeriveSecretId(chainID, counter2))

	// ...and chain-scoped: a different chainID yields a different ID
	require.NotEqual(t, id1, keeper.DeriveSecretId(chainID+"-other", counter1))

	// Every assigned ID passes the protocol's own secret-ID validation
	require.NoError(t, types.ValidateSecretIdRequired(id1))
	require.NoError(t, types.ValidateSecretIdRequired(id2))
}

// Helper function for string containment checks
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// getValidPublicKey generates a deterministic 32-byte key for testing.
// The seed is hashed so distinct seeds always yield distinct keys — the
// chain enforces global key uniqueness across all guardians and epochs, so
// a prefix-truncation scheme (where long seeds sharing 32 leading bytes
// collide) would break every multi-guardian fixture.
func getValidPublicKey(seed string) []byte {
	key := sha256.Sum256([]byte(seed))
	return key[:]
}

// finaliseCommitDeadline advances the context past the secret's commit
// deadline and runs ProcessExpiredCommits — the roster's single finalisation
// point under the [min, max] band: ≥ min_shares accepted → pending with
// exactly the accepted set, fewer → failed with the full no-fault refund.
// Nothing activates mid-window, so every test that needs a pending secret
// funnels through here after its acceptances.
func finaliseCommitDeadline(t *testing.T, f *fixture, secretId string) {
	t.Helper()
	secret, err := f.keeper.GetSecret(f.ctx, secretId)
	require.NoError(t, err)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	if sdkCtx.BlockHeight() <= secret.CommitDeadline {
		f.ctx = sdkCtx.WithBlockHeight(secret.CommitDeadline + 1)
	}
	require.NoError(t, f.keeper.ProcessExpiredCommits(f.ctx))
}
