package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	secretstypes "github.com/timeflareio/chain/x/secrets/types"
)

// The rebate pool is funded at genesis by address, because it is a module
// account with no key to look up (docs/spec.md "Genesis Pool Allocations").
// The genesis scripts therefore carry that address as a literal, and a literal
// derived from a module name is exactly the kind of thing that silently rots.
// These tests make it fail loudly instead.

const rebatePoolBech32 = "tmflr1g6ct2qh5jtrew322yuumdehgwnk9pcexzzz3d2"

func TestRebatePoolAddress_MatchesTheModuleName(t *testing.T) {
	// The package's init has already sealed the SDK config with the tmflr
	// prefix, which is exactly the configuration genesis runs under.
	require.Equal(t, rebatePoolBech32,
		authtypes.NewModuleAddress(secretstypes.RebatePoolName).String(),
		"the rebate pool's derived address changed — every genesis script carrying it must be updated")
}

// The pool must be permissionless: a minter could inflate a fixed supply and a
// burner could destroy adoption funding. Neither belongs on an account whose
// only job is to be spent by formula.
func TestRebatePool_HasNoPermissions(t *testing.T) {
	var found bool
	for _, entry := range moduleAccPerms {
		if entry.Account == secretstypes.RebatePoolName {
			found = true
			require.Empty(t, entry.Permissions, "the rebate pool must hold no permissions")
		}
	}
	require.True(t, found, "the rebate pool must be a registered module account")
}

// Inbound sends are blocked, which is what makes "it can never be topped up" a
// property of the chain rather than a convention.
func TestRebatePool_IsBlockedFromReceiving(t *testing.T) {
	require.Contains(t, blockAccAddrs, secretstypes.RebatePoolName)
}

// Every genesis path must fund the pool at the same literal, and none may hand
// it a key.
func TestGenesisScripts_CarryTheRebatePoolAddress(t *testing.T) {
	scripts := []string{
		"../devnet/chain/generate-genesis-keys.sh",
		"../devnet/docker/init-chain.sh",
	}
	for _, path := range scripts {
		t.Run(filepath.Base(path), func(t *testing.T) {
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			content := string(body)

			require.Contains(t, content, rebatePoolBech32,
				"%s must fund the rebate pool at its derived module address", path)
			require.NotContains(t, content, "user-incentives",
				"%s still references a retired incentive pool", path)
			require.NotContains(t, content, "rebate-pool",
				"%s appears to create a KEY for the rebate pool; it must have none", path)
			require.True(t, strings.Contains(content, "700000000000000"),
				"%s must allocate 700M VEIL (70%%) to the rebate pool", path)
			require.True(t, strings.Contains(content, "300000000000000"),
				"%s must allocate 300M VEIL (30%%) to bootstrapping", path)
		})
	}
}
