package cmd_test

import (
	"io"
	"strings"
	"testing"

	clienthelpers "cosmossdk.io/client/v2/helpers"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"github.com/timeflareio/chain/x/secrets/types"

	"github.com/timeflareio/chain/cmd/timeflared/cmd"
)

// TestRootCmdSmoke builds the full root command (depinject wiring plus
// autocli's EnhanceRootCommand) and executes `version`, exactly as main does.
// This is the only test that constructs the CLI: an autocli mismatch — such as
// a module declaring a positional field the client/v2 flag builder cannot
// resolve — panics on every timeflared invocation yet is invisible to keeper
// and module tests.
func TestRootCmdSmoke(t *testing.T) {
	rootCmd := cmd.NewRootCmd()
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	rootCmd.SetArgs([]string{"version", "--home", t.TempDir()})

	if err := svrcmd.Execute(rootCmd, clienthelpers.EnvPrefix, t.TempDir()); err != nil {
		t.Fatalf("executing 'timeflared version': %v", err)
	}
}

// TestStartRefusesRetentionOverrideUnacknowledged pins the dev-override
// guard: with TIMEFLARE_RETENTION_BLOCKS set, `start` must refuse to boot
// unless --unsafe-dev-overrides explicitly acknowledges the consensus-
// critical override. The PreRunE is exercised directly — actually starting
// a node is the devnet suite's job.
func TestStartRefusesRetentionOverrideUnacknowledged(t *testing.T) {
	t.Setenv(types.RetentionBlocksEnvVar, "60")

	rootCmd := cmd.NewRootCmd()
	startCmd, _, err := rootCmd.Find([]string{"start"})
	if err != nil {
		t.Fatalf("finding start command: %v", err)
	}
	startCmd.SetOut(io.Discard)
	startCmd.SetErr(io.Discard)

	err = startCmd.PreRunE(startCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "unsafe-dev-overrides") {
		t.Fatalf("start with %s set and no acknowledgement: want refusal naming the flag, got %v",
			types.RetentionBlocksEnvVar, err)
	}

	if err := startCmd.Flags().Set("unsafe-dev-overrides", "true"); err != nil {
		t.Fatalf("setting flag: %v", err)
	}
	if err := startCmd.PreRunE(startCmd, nil); err != nil {
		t.Fatalf("acknowledged override must pass the guard, got %v", err)
	}
}

// TestStartGuardIgnoresUnsetOverride proves the guard is inert when the
// variable is absent — the production path.
func TestStartGuardIgnoresUnsetOverride(t *testing.T) {
	t.Setenv(types.RetentionBlocksEnvVar, "")

	rootCmd := cmd.NewRootCmd()
	startCmd, _, err := rootCmd.Find([]string{"start"})
	if err != nil {
		t.Fatalf("finding start command: %v", err)
	}
	if err := startCmd.PreRunE(startCmd, nil); err != nil {
		t.Fatalf("start guard must be inert with %s unset, got %v", types.RetentionBlocksEnvVar, err)
	}
}
