package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	secretstypes "github.com/timeflareio/chain/x/secrets/types"
)

// The shared corpus pins the mnemonic → address pairing at the chain's HD
// path (docs/spec.md "Network Configuration"; CLIENT_CONVENTIONS.md §9)
// across this keyring, the TypeScript SDK and any future client.
type walletDerivationCorpus struct {
	HdPath               string `json:"hd_path"`
	WrongHdPathCosmoshub string `json:"wrong_hd_path_cosmoshub"`
	Vectors              []struct {
		Name                  string `json:"name"`
		Mnemonic              string `json:"mnemonic"`
		Address               string `json:"address"`
		WrongAddressCosmoshub string `json:"wrong_address_cosmoshub"`
	} `json:"vectors"`
}

func loadWalletDerivationCorpus(t *testing.T) walletDerivationCorpus {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(
		"..", "x", "secrets", "types", "testdata", "vectors", "wallet_derivation.json"))
	require.NoError(t, err)
	var corpus walletDerivationCorpus
	require.NoError(t, json.Unmarshal(raw, &corpus))
	require.NotEmpty(t, corpus.Vectors)
	return corpus
}

func newVectorKeyring(t *testing.T) keyring.Keyring {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	return keyring.NewInMemory(codec.NewProtoCodec(registry))
}

func deriveVectorAddress(t *testing.T, kr keyring.Keyring, name, mnemonic, hdPath string) string {
	t.Helper()
	record, err := kr.NewAccount(name, mnemonic, "", hdPath, hd.Secp256k1)
	require.NoError(t, err)
	addr, err := record.GetAddress()
	require.NoError(t, err)
	bech, err := sdk.Bech32ifyAddressBytes(AccountAddressPrefix, addr)
	require.NoError(t, err)
	return bech
}

// TestWalletDerivationVectors asserts the chain's keyring derives every
// corpus mnemonic to the pinned address at m/44'/ChainCoinType'/0'/0/0.
func TestWalletDerivationVectors(t *testing.T) {
	corpus := loadWalletDerivationCorpus(t)

	chainPath := hd.CreateHDPath(secretstypes.ChainCoinType, 0, 0).String()
	require.Equal(t, corpus.HdPath, chainPath,
		"corpus hd_path and ChainCoinType (x/secrets/types) have drifted")

	kr := newVectorKeyring(t)
	for _, v := range corpus.Vectors {
		got := deriveVectorAddress(t, kr, v.Name, v.Mnemonic, chainPath)
		require.NotEqual(t, v.WrongAddressCosmoshub, got,
			"%s: derivation regressed to the Cosmos Hub default coin type 118 — wallet keys derive with ChainCoinType (9733)", v.Name)
		require.Equal(t, v.Address, got, "%s: address mismatch at %s", v.Name, chainPath)
	}
}

// TestWalletDerivationNegativeVectors pins the corpus's recorded 118-derived
// addresses, keeping the regression assertion above meaningful.
func TestWalletDerivationNegativeVectors(t *testing.T) {
	corpus := loadWalletDerivationCorpus(t)

	hubPath := hd.CreateHDPath(118, 0, 0).String()
	require.Equal(t, corpus.WrongHdPathCosmoshub, hubPath)

	kr := newVectorKeyring(t)
	for _, v := range corpus.Vectors {
		got := deriveVectorAddress(t, kr, v.Name+"-hub", v.Mnemonic, hubPath)
		require.Equal(t, v.WrongAddressCosmoshub, got, "%s: 118-derived address mismatch", v.Name)
		require.NotEqual(t, v.Address, got, "%s: the two paths must not collide", v.Name)
	}
}
