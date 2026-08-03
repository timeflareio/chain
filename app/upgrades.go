package app

import (
	"fmt"

	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/timeflareio/chain/app/upgrades"
)

// Upgrades is the registry of all chain software upgrades, oldest first.
// Add new upgrades here after scaffolding with `make upgrade-scaffold NAME=vN`.
// Entries for heights the chain has already passed must never be removed on a
// live network — nodes syncing from genesis still need them.
var Upgrades = []upgrades.Upgrade{}

// setupUpgradeHandlers registers every upgrade handler and, when a node
// restarts into a pending upgrade, configures the store loader with that
// upgrade's store changes. Must be called before app.Load.
func (app *App) setupUpgradeHandlers() {
	for _, upgrade := range Upgrades {
		app.UpgradeKeeper.SetUpgradeHandler(
			upgrade.UpgradeName,
			upgrade.CreateUpgradeHandler(app.ModuleManager, app.Configurator()),
		)
	}

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Errorf("failed to read upgrade info from disk: %w", err))
	}

	if upgradeInfo.Name == "" || app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return
	}

	for _, upgrade := range Upgrades {
		if upgrade.UpgradeName == upgradeInfo.Name {
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &upgrade.StoreUpgrades))
			break
		}
	}
}
