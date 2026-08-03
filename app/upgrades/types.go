// Package upgrades defines the registry pattern for chain software upgrades.
//
// Each upgrade lives in its own versioned subpackage (e.g. upgrades/v2) and
// exposes an Upgrade value that is registered in app/upgrades.go. The upgrade
// name must match the name submitted in the software-upgrade governance
// proposal: when the chain reaches the upgrade height every node halts, and
// on restart with the new binary the registered handler runs exactly once to
// perform state migrations before normal block processing resumes.
//
// Use `make upgrade-scaffold NAME=v2` to generate a new upgrade package, and
// see docs/upgrades.md for the full runbook.
package upgrades

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// Upgrade defines a single chain software upgrade.
type Upgrade struct {
	// UpgradeName is the on-chain upgrade identifier. It must match the name
	// in the software-upgrade governance proposal.
	UpgradeName string

	// CreateUpgradeHandler returns the handler executed once at the upgrade
	// height. Handlers must run module migrations and any custom state
	// changes required by the new binary.
	CreateUpgradeHandler func(*module.Manager, module.Configurator) upgradetypes.UpgradeHandler

	// StoreUpgrades lists module stores added, renamed, or deleted by this
	// upgrade. Applied via the store loader before the handler runs.
	StoreUpgrades storetypes.StoreUpgrades
}
