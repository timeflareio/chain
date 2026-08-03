# Chain Upgrade Tooling
# Scaffolding and local rehearsal for software upgrades. See docs/upgrades.md.

##@ Chain upgrades

## scaffold a new chain upgrade package (usage: make upgrade-scaffold NAME=v2)
upgrade-scaffold:
	@if [ -z "$(NAME)" ]; then \
		echo "❌ Usage: make upgrade-scaffold NAME=v2"; \
		exit 1; \
	fi
	@if [ -d "app/upgrades/$(NAME)" ]; then \
		echo "❌ app/upgrades/$(NAME) already exists"; \
		exit 1; \
	fi
	@mkdir -p app/upgrades/$(NAME)
	@printf '%s\n' \
		'// Package $(NAME) contains the $(NAME) chain software upgrade.' \
		'//' \
		'// Register this upgrade in the Upgrades list in app/upgrades.go, then' \
		'// follow docs/upgrades.md to schedule it via governance.' \
		'package $(NAME)' \
		'' \
		'import (' \
		'	"context"' \
		'' \
		'	storetypes "cosmossdk.io/store/types"' \
		'	upgradetypes "cosmossdk.io/x/upgrade/types"' \
		'	"github.com/cosmos/cosmos-sdk/types/module"' \
		'' \
		'	"github.com/timeflareio/chain/app/upgrades"' \
		')' \
		'' \
		'// UpgradeName must match the name in the software-upgrade proposal' \
		'const UpgradeName = "$(NAME)"' \
		'' \
		'// Upgrade is registered in app/upgrades.go' \
		'var Upgrade = upgrades.Upgrade{' \
		'	UpgradeName:          UpgradeName,' \
		'	CreateUpgradeHandler: createUpgradeHandler,' \
		'	StoreUpgrades:        storetypes.StoreUpgrades{},' \
		'}' \
		'' \
		'func createUpgradeHandler(mm *module.Manager, configurator module.Configurator) upgradetypes.UpgradeHandler {' \
		'	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {' \
		'		// Custom state migrations for $(NAME) go here, before RunMigrations.' \
		'		return mm.RunMigrations(ctx, configurator, fromVM)' \
		'	}' \
		'}' \
		> app/upgrades/$(NAME)/upgrade.go
	@echo "✅ Scaffolded app/upgrades/$(NAME)/upgrade.go"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Add the upgrade to the Upgrades list in app/upgrades.go:"
	@echo "       $(NAME) \"github.com/timeflareio/chain/app/upgrades/$(NAME)\""
	@echo "       var Upgrades = []upgrades.Upgrade{$(NAME).Upgrade}"
	@echo "  2. Implement migrations in the handler if required"
	@echo "  3. Rehearse locally: make devnet-upgrade-test NAME=$(NAME)"
	@echo "  4. See docs/upgrades.md for the governance proposal flow"

## rehearse a chain upgrade on the local devnet (usage: make devnet-upgrade-test NAME=v2)
devnet-upgrade-test:
	@if [ -z "$(NAME)" ]; then \
		echo "❌ Usage: make devnet-upgrade-test NAME=v2"; \
		exit 1; \
	fi
	@./devnet/chain/upgrade-test.sh "$(NAME)"

.PHONY: upgrade-scaffold devnet-upgrade-test
