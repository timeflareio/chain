package keeper

// Internal test for prepareGuardianInfoResponse: the missing-guardian branch
// is unreachable through the public API (selection only ever returns
// addresses read from the guardian store), so the state-integrity assertion
// is exercised white-box.

import (
	"bytes"
	"testing"

	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"

	"github.com/timeflareio/chain/x/secrets/types"
)

// TestPrepareGuardianInfoResponse_MissingGuardianHardFails pins the removal
// of the silent zero-key fallback: a selected guardian without a registration
// record is a hard error, never a placeholder key a creator would encrypt a
// share to.
func TestPrepareGuardianInfoResponse_MissingGuardianHardFails(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	encCfg := moduletestutil.MakeTestEncodingConfig()
	k := NewKeeper(encCfg.Codec, addresscodec.NewBech32Codec("tmflr"), runtime.NewKVStoreService(storeKey), nil, nil)
	ms := msgServer{k: k}

	registered := types.Guardian{
		Address:             "tmflr1registered",
		EncryptionPublicKey: bytes.Repeat([]byte{0x11}, types.PublicKeyLength),
		AvailableFrom:       0,
		AvailableUntil:      1000,
	}
	require.NoError(t, k.SetGuardian(ctx, registered))

	// Healthy path: the guardian's actual encryption key comes back
	infos, err := ms.prepareGuardianInfoResponse(ctx, []string{registered.Address})
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Equal(t, registered.EncryptionPublicKey, infos[0].PublicKey)

	// A selected guardian with no record hard-fails the request — and no
	// zero-key placeholder is ever produced
	infos, err = ms.prepareGuardianInfoResponse(ctx, []string{registered.Address, "tmflr1missing"})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrGuardianNotFound)
	require.Nil(t, infos)
}
