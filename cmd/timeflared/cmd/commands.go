package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"cosmossdk.io/log/v2"
	confixcmd "cosmossdk.io/tools/confix/cmd"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"github.com/timeflareio/chain/app"
	"github.com/timeflareio/chain/x/secrets/types"
)

func initRootCmd(
	rootCmd *cobra.Command,
	txConfig client.TxConfig,
	basicManager module.BasicManager,
) {
	rootCmd.AddCommand(
		genutilcli.InitCmd(basicManager, app.DefaultNodeHome),
		NewInPlaceTestnetCmd(),
		NewTestnetMultiNodeCmd(basicManager, banktypes.GenesisBalancesIterator{}),
		debug.Cmd(),
		confixcmd.ConfigCommand(),
		pruning.Cmd(newApp, app.DefaultNodeHome),
		snapshot.Cmd(newApp),
	)

	server.AddCommandsWithStartCmdOptions(rootCmd, app.DefaultNodeHome, newApp, appExport, server.StartCmdOptions{
		AddFlags: func(cmd *cobra.Command) {
			cmd.Flags().Bool(flagUnsafeDevOverrides, false,
				"acknowledge consensus-critical dev/test env overrides ("+types.RetentionBlocksEnvVar+", "+types.KeyRotationMinIntervalEnvVar+"); never use on a production node")
		},
	})
	guardDevOverrides(rootCmd)

	// add keybase, auxiliary RPC, query, genesis, and tx child commands
	rootCmd.AddCommand(
		server.StatusCommand(),
		genutilcli.Commands(txConfig, basicManager, app.DefaultNodeHome),
		queryCommand(),
		txCommand(),
		keys.Commands(),
	)
}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		rpc.WaitTxCmd(),
		rpc.ValidatorCommand(),
		server.QueryBlockCmd(),
		authcmd.QueryTxsByEventsCmd(),
		server.QueryBlocksCmd(),
		authcmd.QueryTxCmd(),
		server.QueryBlockResultsCmd(),
		// GetSecretsQueryCmd(), // Removed temporarily - CLI commands need to be updated for two-phase commit
	)

	return cmd
}

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		flags.LineBreak,
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		authcmd.GetSimulateCmd(),
		flags.LineBreak,
		// GetSecretsTxCmd(), // Removed temporarily - CLI commands need to be updated for two-phase commit
	)

	return cmd
}

// newApp creates the application
func newApp(
	logger log.Logger,
	db dbm.DB,
	appOpts servertypes.AppOptions,
) servertypes.Application {
	baseappOptions := server.DefaultBaseappOptions(appOpts)

	return app.New(
		logger, db, true,
		appOpts,
		baseappOptions...,
	)
}

// appExport creates a new app (optionally at a given height) and exports state.
func appExport(
	logger log.Logger,
	db dbm.DB,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	var bApp *app.App

	// this check is necessary as we use the flag in x/upgrade.
	// we can exit more gracefully by checking the flag here.
	homePath, ok := appOpts.Get(flags.FlagHome).(string)
	if !ok || homePath == "" {
		return servertypes.ExportedApp{}, errors.New("application home not set")
	}

	viperAppOpts, ok := appOpts.(*viper.Viper)
	if !ok {
		return servertypes.ExportedApp{}, errors.New("appOpts is not viper.Viper")
	}

	appOpts = viperAppOpts
	if height != -1 {
		bApp = app.New(logger, db, false, appOpts)
		if err := bApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else {
		bApp = app.New(logger, db, true, appOpts)
	}

	return bApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
}

// flagUnsafeDevOverrides acknowledges consensus-critical dev/test
// environment overrides. TIMEFLARE_RETENTION_BLOCKS exists so devnet e2e
// scenarios can exercise retention pruning in minutes instead of months —
// every node in a network must agree on the value, so a production node
// must never run with it. The start command refuses to boot while the
// variable is set unless this flag is passed explicitly.
const flagUnsafeDevOverrides = "unsafe-dev-overrides"

// guardDevOverrides chains a PreRunE onto the start command that enforces
// the acknowledgement above, and makes an acknowledged override loud.
func guardDevOverrides(rootCmd *cobra.Command) {
	startCmd, _, err := rootCmd.Find([]string{"start"})
	if err != nil {
		panic(fmt.Errorf("guardDevOverrides: no start command: %w", err))
	}

	// Every consensus-critical dev/test env override the acknowledgement
	// covers — extend this list when a new long-timescale constant grows a
	// devnet override.
	guardedEnvVars := []string{
		types.RetentionBlocksEnvVar,
		types.KeyRotationMinIntervalEnvVar,
	}

	prev := startCmd.PreRunE
	startCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		for _, envVar := range guardedEnvVars {
			v := os.Getenv(envVar)
			if v == "" {
				continue
			}
			acknowledged, err := cmd.Flags().GetBool(flagUnsafeDevOverrides)
			if err != nil {
				return err
			}
			if !acknowledged {
				return fmt.Errorf(
					"%s=%s is set — a consensus-critical dev/test override that must never reach a production node; "+
						"unset it, or pass --%s on a throwaway dev chain",
					envVar, v, flagUnsafeDevOverrides,
				)
			}
			cmd.PrintErrf("⚠️  %s=%s — dev/test override active; every node must agree on this value\n",
				envVar, v)
		}
		if prev != nil {
			return prev(cmd, args)
		}
		return nil
	}
}
