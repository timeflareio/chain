# Local Devnet Driver
# Single-command local development environment: chain + guardians + funded users.
#
# Runtime state (PIDs, logs) lives in .devnet/ which is gitignored.
# Chain data lives in ~/.timeflare as before.

# Default guardian count: the protocol assigns shares + 30% buffer distinct
# guardians, so the default e2e config (5 shares) needs at least 7 registered.
# The default is set well above that floor so selection has a real candidate
# pool (bond affordability filtering, concurrency caps) to work against.
GUARDIAN_COUNT ?= 24
DEVNET_DIR     := .devnet
DEVNET_BIN     := $(DEVNET_DIR)/bin
CHAIN_PID_FILE := $(DEVNET_DIR)/chain.pid
CHAIN_LOG_FILE := $(DEVNET_DIR)/chain.log

# Pinned component versions (devnet/versions.env). The guardian is no longer
# built from a sibling directory — the devnet runs the released binary, which is
# what an operator would run. GUARDIAND_BIN overrides with a local build for
# cross-repo work.
include devnet/versions.env
GUARDIAND_BIN  ?=
GUARDIAND      := $(DEVNET_BIN)/guardiand
CHAIN_RPC      ?= http://localhost:26657

# Orchestration scripts (thin wrappers around timeflared / guardiand)
# PATH is scoped to the synced binary so the devnet cannot silently pick up a
# guardiand installed globally by another checkout — the failure that would
# cause is a devnet quietly testing the wrong build.
#
# It belongs on every script that shells out to guardiand, not just the manager:
# the scenario suite restarts daemons and probes their health, so it calls the
# binary too. Leaving it off there cost a CI run — the calls became "command not
# found", which a probe reading only an exit code cannot tell apart from
# "unhealthy".
DEVNET_PATH       := PATH="$(CURDIR)/$(DEVNET_BIN):$$PATH"
GUARDIAN_MANAGER  := $(DEVNET_PATH) ./devnet/guardians.sh
CHAIN_IDENTITY    := ./devnet/verify-chain-identity.sh
USER_SETUP        := ./devnet/users/setup-test-users.sh
# The e2e harness is driven by the TypeScript SDK's examples. The SDK lives in
# its own repository now, so SDK_DIR points at whatever provides them: a working
# tree today, and a directory unpacked from the pinned SDK release once one
# exists (see sdk-sync).
SDK_DIR           ?= $(DEVNET_DIR)/sdk
E2E_TEST           = $(SDK_DIR)/examples/secret-lifecycle.js
E2E_KEYGEN         = $(SDK_DIR)/examples/generate-keypair.js
RECIPIENT_KEYPAIR := $(DEVNET_DIR)/recipient-keypair.json

##@ Dev loop

# Fetches the EXAMPLES bundle specifically. The SDK publishes two tarballs, and
# the other one — timeflare-sdk-<tag>.tgz — is dist-only and byte-deterministic
# for consumers that vendor the package. It carries no examples/, so a '*.tgz'
# glob would match it and leave the harness with nothing to run.
# The examples are JavaScript with real runtime dependencies (cosmjs, bech32,
# @noble/hashes). The release bundle ships source, build output and WASM — not
# node_modules, which would be large and platform-specific — so they have to be
# installed before anything can run.
#
# `npm ci` when a lockfile is present, which is deterministic and what a release
# bundle should carry. SDK v0.0.1 does not include one, so `npm install` is the
# fallback; it resolves the same declared ranges, just without the exact-tree
# guarantee. An SDK release that bundles its lockfile removes the fallback.
sdk-install:
	@set -e; \
	if [ -f "$(SDK_DIR)/package-lock.json" ]; then \
		echo "📦 Installing SDK dependencies (npm ci — deterministic)"; \
		( cd "$(SDK_DIR)" && npm ci --omit=dev --silent ); \
	else \
		echo "📦 Installing SDK dependencies (npm install — the bundle carries no lockfile)"; \
		( cd "$(SDK_DIR)" && npm install --omit=dev --silent --no-audit --no-fund ); \
	fi; \
	node -e "require('$(CURDIR)/$(SDK_DIR)/node_modules/bech32')" 2>/dev/null || { \
		echo "❌ dependencies did not install — the examples will not run"; exit 1; }; \
	echo "✅ SDK dependencies installed"

## make the SDK examples available to the e2e harness (SDK_DIR=<path> for a working tree)
sdk-sync:
	@set -e; \
	if [ -d "$(SDK_DIR)/examples" ] && [ -d "$(SDK_DIR)/node_modules" ]; then \
		echo "📦 Using SDK at $(SDK_DIR)"; exit 0; \
	fi; \
	if [ -d "$(SDK_DIR)/examples" ] && [ ! -d "$(SDK_DIR)/node_modules" ]; then \
		echo "📦 SDK present at $(SDK_DIR) but dependencies are not installed"; \
		$(MAKE) --no-print-directory sdk-install SDK_DIR="$(SDK_DIR)"; \
		exit 0; \
	fi; \
	if [ -z "$(SDK_VERSION)" ]; then \
		echo "❌ No SDK available for the e2e harness."; \
		echo ""; \
		echo "   SDK_VERSION is unset in devnet/versions.env because the TypeScript"; \
		echo "   SDK has not been lifted into its own repository yet (migration"; \
		echo "   phase 4), so there is no release to pin."; \
		echo ""; \
		echo "   Point at a working tree meanwhile:"; \
		echo "     make e2e SDK_DIR=/path/to/typescript-sdk"; \
		exit 1; \
	fi; \
	echo "📦 Fetching SDK $(SDK_VERSION) from timeflareio/typescript-sdk"; \
	mkdir -p "$(SDK_DIR)"; \
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	gh release download "$(SDK_VERSION)" --repo timeflareio/typescript-sdk \
		--pattern 'timeflare-sdk-examples-*.tar.gz' --dir "$$tmp"; \
	tar -xzf "$$tmp"/timeflare-sdk-examples-*.tar.gz -C "$(SDK_DIR)" --strip-components=1; \
	test -f "$(SDK_DIR)/examples/secret-lifecycle.js" || { \
		echo "❌ the bundle did not contain examples/ — wrong asset?"; exit 1; }; \
	$(MAKE) --no-print-directory sdk-install SDK_DIR="$(SDK_DIR)"; \
	echo "✅ SDK $(SDK_VERSION) ready at $(SDK_DIR)"

## fetch the pinned guardiand release binary (or use GUARDIAND_BIN=<path>)
guardiand-sync:
	@set -e; \
	mkdir -p $(DEVNET_BIN); \
	if [ -n "$(GUARDIAND_BIN)" ]; then \
		if [ ! -x "$(GUARDIAND_BIN)" ]; then \
			echo "❌ GUARDIAND_BIN=$(GUARDIAND_BIN) is not an executable"; exit 1; \
		fi; \
		cp "$(GUARDIAND_BIN)" "$(GUARDIAND)"; \
		echo "👮 Using local guardiand: $(GUARDIAND_BIN) ($$("$(GUARDIAND)" version | awk '/^Version:/{print $$2}'))"; \
		exit 0; \
	fi; \
	if [ -z "$(GUARDIAN_VERSION)" ]; then \
		echo "❌ GUARDIAN_VERSION is unset in devnet/versions.env"; exit 1; \
	fi; \
	have=""; \
	if [ -x "$(GUARDIAND)" ]; then \
		have=$$("$(GUARDIAND)" version 2>/dev/null | awk '/^Version:/{print $$2}'); \
	fi; \
	if [ "$$have" = "$(GUARDIAN_VERSION)" ]; then \
		echo "👮 guardiand $(GUARDIAN_VERSION) already present"; exit 0; \
	fi; \
	os=$$(uname -s | tr 'A-Z' 'a-z'); \
	arch=$$(uname -m); \
	case "$$arch" in x86_64) arch=amd64;; aarch64) arch=arm64;; esac; \
	asset="guardiand-$(GUARDIAN_VERSION)-$$os-$$arch"; \
	echo "👮 Fetching $$asset from timeflareio/guardian@$(GUARDIAN_VERSION)"; \
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	if ! gh release download "$(GUARDIAN_VERSION)" --repo timeflareio/guardian \
		--pattern "$$asset" --pattern "*checksums.txt" --dir "$$tmp" 2>/dev/null; then \
		if gh repo view timeflareio/guardian >/dev/null 2>&1; then \
			echo "❌ $(GUARDIAN_VERSION) has no asset $$asset."; \
			echo "   The repository is readable, so this is a missing release or a"; \
			echo "   host/arch with no published binary."; \
		else \
			echo "❌ Cannot read timeflareio/guardian."; \
			echo ""; \
			echo "   That repository is private, and GitHub answers 404 rather than 403"; \
			echo "   for a token that cannot see it — so 'not found' here usually means"; \
			echo "   'not permitted', not 'not released'."; \
			echo ""; \
			echo "   In CI the default GITHUB_TOKEN is scoped to this repository only."; \
			echo "   Fix by making timeflareio/guardian public, or by providing a token"; \
			echo "   with read access as GH_TOKEN."; \
			echo ""; \
			echo "   Locally: check 'gh auth status'."; \
		fi; \
		exit 1; \
	fi; \
	( cd "$$tmp" && grep " $$asset$$" *checksums.txt | shasum -a 256 -c - ) || { \
		echo "❌ checksum mismatch for $$asset"; exit 1; }; \
	install -m 0755 "$$tmp/$$asset" "$(GUARDIAND)"; \
	got=$$("$(GUARDIAND)" version | awk '/^Version:/{print $$2}'); \
	if [ "$$got" != "$(GUARDIAN_VERSION)" ]; then \
		echo "❌ downloaded binary reports $$got, expected $(GUARDIAN_VERSION)"; exit 1; \
	fi; \
	echo "✅ guardiand $(GUARDIAN_VERSION) ready"


## start the full local devnet (chain + $(GUARDIAN_COUNT) guardians + funded test user)
dev-up: go-install guardiand-sync
	@mkdir -p $(DEVNET_DIR)
	@if [ ! -d "$$HOME/.timeflare/config" ]; then \
		echo "⛓️  No chain state found — initialising test chain..."; \
		$(MAKE) init-test-chain; \
	fi
	@if curl -s --max-time 2 $(CHAIN_RPC)/status >/dev/null 2>&1; then \
		echo "⛓️  Chain already running at $(CHAIN_RPC)"; \
		$(CHAIN_IDENTITY) || exit 1; \
	else \
		echo "⛓️  Starting chain (log: $(CHAIN_LOG_FILE))..."; \
		nohup timeflared start $${TIMEFLARE_RETENTION_BLOCKS:+--unsafe-dev-overrides} $${TIMEFLARE_KEY_ROTATION_MIN_INTERVAL:+--unsafe-dev-overrides} >> $(CHAIN_LOG_FILE) 2>&1 & echo $$! > $(CHAIN_PID_FILE); \
		i=0; until curl -s --max-time 2 $(CHAIN_RPC)/status 2>/dev/null | jq -e '.result.sync_info.latest_block_height | tonumber >= 1' >/dev/null 2>&1; do \
			i=$$((i+1)); \
			if [ $$i -gt 60 ]; then echo "❌ Chain failed to produce a block — see $(CHAIN_LOG_FILE)"; exit 1; fi; \
			sleep 1; \
		done; \
		i=0; until timeflared query bank total >/dev/null 2>&1; do \
			i=$$((i+1)); \
			if [ $$i -gt 30 ]; then echo "❌ Chain app queries not ready — see $(CHAIN_LOG_FILE)"; exit 1; fi; \
			sleep 1; \
		done; \
		echo "✅ Chain producing blocks and serving queries at $(CHAIN_RPC)"; \
	fi
	@echo "👮 Registering $(GUARDIAN_COUNT) guardians (idempotent)..."
	@$(GUARDIAN_MANAGER) register $(GUARDIAN_COUNT)
	@echo "👮 Starting $(GUARDIAN_COUNT) guardians..."
	@$(GUARDIAN_MANAGER) start $(GUARDIAN_COUNT)
	@echo "👤 Funding test user..."
	@$(USER_SETUP) fund
	@echo ""
	@echo "🚀 Devnet is up. Useful commands:"
	@echo "   make dev-status   — chain and guardian overview"
	@echo "   make e2e          — run the full secret lifecycle test"
	@echo "   make dev-down     — stop everything"

## stop the local devnet (guardians + chain)
dev-down:
	@echo "👮 Stopping guardians..."
	@$(GUARDIAN_MANAGER) stop || true
	@if [ -f $(CHAIN_PID_FILE) ]; then \
		pid=$$(cat $(CHAIN_PID_FILE)); \
		if kill -0 $$pid 2>/dev/null; then \
			echo "⛓️  Stopping chain (pid $$pid)..."; \
			kill $$pid; \
		fi; \
		rm -f $(CHAIN_PID_FILE); \
	elif pgrep -f "timeflared start" >/dev/null 2>&1; then \
		echo "⚠️  Chain is running but was not started by 'make dev-up' — stop it yourself"; \
	fi
	@echo "✅ Devnet stopped"

## wipe all devnet state and bring a fresh environment up
dev-reset: dev-down
	@# ~/.timeflare is shared by every checkout and worktree, so the wipe below
	@# invalidates ANY running chain or guardian regardless of who started it.
	@# dev-down only stops processes recorded in this checkout's pid files —
	@# take down strays from other sessions too, or dev-up will see the old
	@# chain on the RPC port, skip the restart, and fund from a genesis keyring
	@# the old chain has never heard of ("account not found: key not found").
	@if pgrep -f "timeflared start" >/dev/null 2>&1; then \
		echo "⚠️  Stopping stray chain process(es) from another session..."; \
		pkill -f "timeflared start" || true; \
	fi
	@if pgrep -f "guardiand start --config-path $$HOME/.timeflare/guardian" >/dev/null 2>&1; then \
		echo "⚠️  Stopping stray guardian process(es) from another session..."; \
		pkill -f "guardiand start --config-path $$HOME/.timeflare/guardian" || true; \
	fi
	@i=0; while pgrep -f "timeflared start" >/dev/null 2>&1 || curl -s --max-time 1 $(CHAIN_RPC)/status >/dev/null 2>&1; do \
		i=$$((i+1)); \
		if [ $$i -gt 15 ]; then \
			echo "❌ Something still answers at $(CHAIN_RPC) (a Docker devnet? 'make docker-reset' owns that) — stop it, then re-run"; \
			exit 1; \
		fi; \
		sleep 1; \
	done
	@echo "♻️  Removing chain state (~/.timeflare) and devnet runtime files..."
	@rm -rf "$$HOME/.timeflare"
	@# Preserve $(DEVNET_DIR)/docker — that is the COMPOSE devnet's host-side
	@# state (exported keyring, compose.yaml); wiping it while its Docker
	@# volumes live on strands the containerised stack. `make docker-reset`
	@# owns that lifecycle.
	@if [ -d $(DEVNET_DIR) ]; then \
		find $(DEVNET_DIR) -mindepth 1 -maxdepth 1 ! -name docker -exec rm -rf {} +; \
	fi
	@$(MAKE) dev-up

## show devnet status (chain height, guardians)
dev-status:
	@echo "⛓️  Chain"
	@echo "  ─────"
	@if curl -s --max-time 2 $(CHAIN_RPC)/status >/dev/null 2>&1; then \
		if command -v jq >/dev/null 2>&1; then \
			curl -s $(CHAIN_RPC)/status | jq -r '.result.sync_info | "  height: \(.latest_block_height)  block time: \(.latest_block_time)"'; \
		else \
			echo "  running at $(CHAIN_RPC) (install jq for details)"; \
		fi; \
	else \
		echo "  not running — start with 'make dev-up'"; \
	fi
	@echo ""
	@echo "👮 Guardians"
	@echo "  ─────────"
	@$(GUARDIAN_MANAGER) status || true

## fund any address from the bootstrapping pool (thin preset over devnet/fund.sh): make faucet ADDR=tmflr1… [AMOUNT=<VEIL>] [NODE=<host:port>]
#
# AMOUNT is in VEIL — the unit a person thinks in — and is converted here.
# devnet/fund.sh keeps its uveil contract: it is the shared primitive with
# documented exit codes, and changing its units would silently rescale any
# consumer by a million.
faucet:
	@if [ -z "$(ADDR)" ]; then \
		echo "usage: make faucet ADDR=tmflr1… [AMOUNT=<VEIL>] [NODE=<host:port>]"; \
		echo "  AMOUNT is VEIL (default 1000). 0.5 is fine; more than 6 decimals is not."; \
		exit 2; \
	fi
	@veil="$(or $(AMOUNT),1000)"; \
	uveil=$$(awk -v v="$$veil" 'BEGIN { \
		if (v !~ /^[0-9]+(\.[0-9]+)?$$/) { print "NaN"; exit } \
		u = v * 1000000; \
		if (u != int(u)) { print "FRAC"; exit } \
		printf "%d", u \
	}'); \
	case "$$uveil" in \
		NaN) echo "AMOUNT must be a positive number of VEIL, got '$$veil'"; exit 2 ;; \
		FRAC) echo "AMOUNT '$$veil' VEIL is finer than 1 uveil (6 decimal places)"; exit 2 ;; \
		0) echo "AMOUNT must be more than zero"; exit 2 ;; \
	esac; \
	echo "==> Funding $(ADDR) with $$veil VEIL ($$uveil uveil)"; \
	./devnet/fund.sh "$(ADDR)" "$$uveil" --node "$(or $(NODE),localhost:26657)"

##@ Testing & E2E

## run the end-to-end secret lifecycle test against the running devnet
e2e: sdk-sync
	@if ! curl -s --max-time 2 $(CHAIN_RPC)/status >/dev/null 2>&1; then \
		echo "❌ Chain is not running — start the devnet first with 'make dev-up'"; \
		exit 1; \
	fi
	@$(USER_SETUP) fund
	@if [ ! -f $(RECIPIENT_KEYPAIR) ]; then \
		echo "🔑 Generating recipient keypair..."; \
		node $(E2E_KEYGEN) $(RECIPIENT_KEYPAIR); \
	fi
	@echo "🧪 Running secret lifecycle end-to-end test..."
	@# RECIPIENT_KEYPAIR is exported explicitly: the SDK examples fall back to a
	@# path relative to their own tree (<sdk>/../../.devnet), which was this
	@# repository inside the monorepo and is not any more.
	@RECIPIENT_KEYPAIR=$(CURDIR)/$(RECIPIENT_KEYPAIR) node $(E2E_TEST)

## run the failure-path scenario suite against the running devnet (no-show slash, mid-hold cancel, early-reveal report)
e2e-scenarios: sdk-sync
	@if ! curl -s --max-time 2 $(CHAIN_RPC)/status >/dev/null 2>&1; then \
		echo "❌ Chain is not running — start the devnet first with 'make dev-up'"; \
		exit 1; \
	fi
	@$(USER_SETUP) fund
	@if [ ! -f $(RECIPIENT_KEYPAIR) ]; then \
		echo "🔑 Generating recipient keypair..."; \
		node $(E2E_KEYGEN) $(RECIPIENT_KEYPAIR); \
	fi
	@echo "🧪 Running failure-path scenario suite..."
	@$(DEVNET_PATH) RECIPIENT_KEYPAIR=$(CURDIR)/$(RECIPIENT_KEYPAIR) SDK_DIR=$(SDK_DIR) ./devnet/e2e-scenarios.sh

## full E2E on the NATIVE devnet: fresh fast-block chain → lifecycle → teardown (fallback when Docker is unavailable; `make e2e-full` targets the compose stack)
e2e-full-native:
	@echo "🧪 Full end-to-end run: fresh native devnet → lifecycle → teardown"
	@TIMEFLARE_BLOCK_TIME=$${TIMEFLARE_BLOCK_TIME:-2s} $(MAKE) dev-reset
	@status=0; $(MAKE) e2e || status=$$?; \
	echo "🧹 Tearing down devnet..."; \
	$(MAKE) dev-down; \
	if [ $$status -eq 0 ]; then echo "✅ Full E2E passed and environment cleaned up"; \
	else echo "❌ Full E2E failed (exit $$status) — chain state kept in ~/.timeflare for inspection"; fi; \
	exit $$status

.PHONY: guardiand-sync sdk-sync sdk-install dev-up dev-down dev-reset dev-status faucet e2e e2e-scenarios e2e-full-native
