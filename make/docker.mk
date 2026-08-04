# Compose Devnet Driver (docs/planning/PENDING_CONTAINERISATION_PLAN.md)
# Opt-in parallel to the native devnet (make dev-up stays the default loop):
# isolated networking, zero host toolchain for bring-up, multi-validator
# topology. The e2e harness remains host-side and drives the stack through
# its published ports.

VALIDATOR_COUNT ?= 1
DOCKER_DEVNET_DIR   := .devnet/docker
DOCKER_COMPOSE_FILE := $(DOCKER_DEVNET_DIR)/compose.yaml
DOCKER_HOME         := $(DOCKER_DEVNET_DIR)/home
DOCKER_IMAGE_TAG    ?= dev
DOCKER_RPC          ?= http://localhost:26657

# Version stamping mirrors make/common.mk so `timeflared version` inside the
# image reports the build it came from. DOCKER_BUILD_EXTRA_ARGS lets CI add
# buildx cache flags (--cache-from/--cache-to type=gha --load) without the
# local flow knowing about them.
DOCKER_BUILD_EXTRA_ARGS ?=
DOCKER_BUILD_ARGS := --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) $(DOCKER_BUILD_EXTRA_ARGS)

# DOCKER_PREBUILT=1 — the CI fast path: compile both static binaries on the
# HOST (where actions/setup-go's build cache persists across runs) and COPY
# them into the runtime images via Dockerfile.prebuilt. The hermetic
# Dockerfiles' BuildKit cache mounts are not exported by --cache-to type=gha,
# so in CI they recompile the entire module graph every run (~6-7 minutes);
# this path drops the three image builds to seconds. GOOS is forced to linux
# so it also works from a macOS host; GOARCH follows the host, matching the
# local Docker platform. Devnet/CI only — release images keep the hermetic
# in-container build with its pinned toolchain.
DOCKER_PREBUILT ?= 0
DOCKER_PREBUILT_DIR        := $(DOCKER_DEVNET_DIR)/prebuilt
DOCKER_PREBUILT_DOCKERFILE := devnet/docker/Dockerfile.prebuilt

##@ Containers

# The guardian image and binary come from timeflareio/guardian at the version
# pinned in devnet/versions.env. Override either for cross-repo development:
#
#   make docker-build GUARDIAN_IMAGE=timeflare/guardiand:dev
#
# Defined outside the DOCKER_PREBUILT branches because BOTH consume it — the
# default path pulls this image, the prebuilt path fetches the matching binary.
# It used to sit inside the prebuilt branch, which left the default path pulling
# an empty image name: "docker pull requires 1 argument".
GUARDIAN_IMAGE ?= ghcr.io/timeflareio/guardiand:$(GUARDIAN_VERSION)

ifeq ($(DOCKER_PREBUILT),1)
# Fetch the linux guardiand release binary to $(OUT). Used by the prebuilt image
# path, which needs a linux binary regardless of the host.
#
# The architecture follows `go env GOARCH`, matching the timeflared build beside
# it (`GOOS=linux go build` with no GOARCH takes the host's). Hardcoding amd64
# was invisible on an amd64 CI runner and put an amd64 guardiand next to an arm64
# timeflared in the same stack on any Apple Silicon machine.
guardiand-fetch-linux:
	@set -e; \
	if [ -z "$(GUARDIAN_VERSION)" ]; then echo "❌ GUARDIAN_VERSION unset"; exit 1; fi; \
	asset="guardiand-$(GUARDIAN_VERSION)-linux-$$(go env GOARCH)"; \
	echo "👮 Fetching $$asset from timeflareio/guardian@$(GUARDIAN_VERSION)"; \
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	gh release download "$(GUARDIAN_VERSION)" --repo timeflareio/guardian \
		--pattern "$$asset" --pattern "*checksums.txt" --dir "$$tmp"; \
	( cd "$$tmp" && grep " $$asset$$" *checksums.txt | shasum -a 256 -c - ); \
	install -m 0755 "$$tmp/$$asset" "$(OUT)"

## build the images with DOCKER_PREBUILT=1: binaries compiled/fetched on the host, then copied in
docker-build:
	@echo "🐳 Compiling timeflared + guardiand on the host (DOCKER_PREBUILT=1)..."
	@mkdir -p $(DOCKER_PREBUILT_DIR)
	@# No -trimpath, matching go-install's BUILD_FLAGS: the CI jobs run
	@# `make go-install` first (host CLI for the e2e harness), and identical
	@# compile flags let these builds reuse that warm build cache instead of
	@# recompiling the graph. -trimpath stays a release/hermetic-image concern.
	@CGO_ENABLED=0 GOOS=linux go build \
		-ldflags "-X github.com/cosmos/cosmos-sdk/version.Name=timeflare \
		          -X github.com/cosmos/cosmos-sdk/version.AppName=timeflare \
		          -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
		          -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)" \
		-o $(DOCKER_PREBUILT_DIR)/timeflared ./cmd/timeflared
	@# guardiand is no longer compiled here — there is no guardian source in this
	@# repository. The linux/amd64 release binary is fetched instead, which is
	@# also what the compose stack should be running: the artefact an operator
	@# would deploy, not a local rebuild of it.
	@$(MAKE) --no-print-directory guardiand-fetch-linux \
		OUT=$(CURDIR)/$(DOCKER_PREBUILT_DIR)/guardiand
	@echo "🐳 Building timeflare/timeflared:$(DOCKER_IMAGE_TAG) (prebuilt)..."
	@docker build --target timeflared -t timeflare/timeflared:$(DOCKER_IMAGE_TAG) -f $(DOCKER_PREBUILT_DOCKERFILE) $(DOCKER_BUILD_ARGS) $(DOCKER_PREBUILT_DIR)
	@echo "🐳 Building timeflare/guardiand:$(DOCKER_IMAGE_TAG) (prebuilt)..."
	@docker build --target guardiand -t timeflare/guardiand:$(DOCKER_IMAGE_TAG) -f $(DOCKER_PREBUILT_DOCKERFILE) $(DOCKER_BUILD_ARGS) $(DOCKER_PREBUILT_DIR)
	@echo "🐳 Building timeflare/tools:$(DOCKER_IMAGE_TAG) (prebuilt)..."
	@docker build --target tools -t timeflare/tools:$(DOCKER_IMAGE_TAG) -f $(DOCKER_PREBUILT_DOCKERFILE) $(DOCKER_BUILD_EXTRA_ARGS) $(DOCKER_PREBUILT_DIR)
	@docker images --format 'table {{.Repository}}:{{.Tag}}\t{{.Size}}' | grep '^timeflare/'
else
## build the timeflared, guardiand, and devnet tools images (timeflared built in Docker, guardiand pulled)
docker-build:
	@echo "🐳 Building timeflare/timeflared:$(DOCKER_IMAGE_TAG)..."
	@docker build -t timeflare/timeflared:$(DOCKER_IMAGE_TAG) $(DOCKER_BUILD_ARGS) .
	@# Pulled, not built: the guardian lives in its own repository and publishes
	@# this image per release. GUARDIAN_IMAGE overrides for local guardian work.
	@echo "🐳 Pulling $(GUARDIAN_IMAGE) -> timeflare/guardiand:$(DOCKER_IMAGE_TAG)..."
	@docker pull -q $(GUARDIAN_IMAGE)
	@docker tag $(GUARDIAN_IMAGE) timeflare/guardiand:$(DOCKER_IMAGE_TAG)
	@# The tools image takes guardiand from the released image rather than
	@# compiling it — there is no guardian source here. GUARDIAN_IMAGE is passed
	@# so the pinned version is stated once, in devnet/versions.env.
	@echo "🐳 Building timeflare/tools:$(DOCKER_IMAGE_TAG)..."
	@docker build -t timeflare/tools:$(DOCKER_IMAGE_TAG) -f devnet/docker/Dockerfile.tools \
		--build-arg GUARDIAN_IMAGE=$(GUARDIAN_IMAGE) $(DOCKER_BUILD_EXTRA_ARGS) .
	@docker images --format 'table {{.Repository}}:{{.Tag}}\t{{.Size}}' | grep '^timeflare/'
endif

## start the compose devnet ($(VALIDATOR_COUNT) validators + $(GUARDIAN_COUNT) guardians in containers)
docker-up: docker-build
	@mkdir -p $(DOCKER_HOME)
	@VALIDATOR_COUNT=$(VALIDATOR_COUNT) GUARDIAN_COUNT=$(GUARDIAN_COUNT) \
		./devnet/docker/generate-compose.sh $(DOCKER_COMPOSE_FILE)
	@docker compose -f $(DOCKER_COMPOSE_FILE) up -d --wait --wait-timeout 300
	@echo ""
	@echo "🐳 Compose devnet is up ($(VALIDATOR_COUNT) validators, $(GUARDIAN_COUNT) guardians)."
	@echo "   make docker-status  — stack overview"
	@echo "   make docker-e2e     — host-side lifecycle test against the stack"
	@echo "   make docker-down    — stop everything (state kept in volumes)"

# Teardown falls back to the compose PROJECT NAME when the generated file is
# gone (e.g. a stray `rm -rf .devnet`) — containers/volumes must never be
# orphaned just because their host-side definition was deleted.
## stop the compose devnet (volumes kept)
docker-down:
	@if [ -f $(DOCKER_COMPOSE_FILE) ]; then docker compose -f $(DOCKER_COMPOSE_FILE) down; \
	else docker compose -p timeflare down --remove-orphans 2>/dev/null || true; fi
	@echo "🐳 Compose devnet stopped"

## wipe the compose devnet (containers, volumes, exported keyring) and start fresh
docker-reset:
	@if [ -f $(DOCKER_COMPOSE_FILE) ]; then docker compose -f $(DOCKER_COMPOSE_FILE) down -v; \
	else docker compose -p timeflare down -v --remove-orphans 2>/dev/null || true; fi
	@rm -rf $(DOCKER_DEVNET_DIR)
	@$(MAKE) docker-up

## tail logs from the compose devnet (SERVICE=validator-0 for one service)
docker-logs:
	@docker compose -f $(DOCKER_COMPOSE_FILE) logs -f $(SERVICE)

## show compose devnet status
docker-status:
	@docker compose -f $(DOCKER_COMPOSE_FILE) ps
	@echo ""
	@curl -s --max-time 2 $(DOCKER_RPC)/status | jq -r '.result.sync_info | "chain height: \(.latest_block_height)  block time: \(.latest_block_time)"' \
		|| echo "chain RPC not reachable at $(DOCKER_RPC)"

# Host-side e2e against the compose stack: same suites as the native devnet,
# pointed at the stack's published ports and the exported genesis keyring.
# TIMEFLARE_HOME isolates george + genesis keyring from any native devnet.
#
# SDK_DIR is passed explicitly. The suites are driven by the SDK's examples,
# which come from the pinned release now rather than a sibling directory, and the
# scenario script defaults to .devnet/sdk only when nothing tells it otherwise —
# a default is not the same as the value this stack actually synced.
DOCKER_E2E_ENV := TIMEFLARE_HOME=$(CURDIR)/$(DOCKER_HOME) \
	RECIPIENT_KEYPAIR=$(CURDIR)/$(DOCKER_DEVNET_DIR)/recipient-keypair.json \
	SDK_DIR=$(SDK_DIR) \
	GUARDIAN_CONTROL=docker

## run the full lifecycle e2e (host-side) against the compose devnet
docker-e2e: sdk-sync
	@if ! curl -s --max-time 2 $(DOCKER_RPC)/status >/dev/null 2>&1; then \
		echo "❌ Compose devnet not reachable — start it with 'make docker-up'"; exit 1; fi
	@$(DOCKER_E2E_ENV) $(USER_SETUP) fund
	@if [ ! -f $(DOCKER_DEVNET_DIR)/recipient-keypair.json ]; then \
		echo "🔑 Generating recipient keypair..."; \
		node $(E2E_KEYGEN) $(DOCKER_DEVNET_DIR)/recipient-keypair.json; fi
	@echo "🧪 Running secret lifecycle e2e against the compose stack..."
	@$(DOCKER_E2E_ENV) node $(E2E_TEST)

# The default verification gate (ruled July 2026, amending the plan's §8.2/8.3):
# every full-E2E pass runs against a fresh multi-validator compose stack, so it
# doubles as an app-hash determinism check — three validators independently
# execute every block and must agree. Native iteration (`make dev-up`) is
# unchanged; `make e2e-full-native` remains the Docker-free fallback.
E2E_FULL_VALIDATORS ?= 3

## full E2E with cleanup: fresh $(E2E_FULL_VALIDATORS)-validator compose stack → lifecycle → app-hash determinism check → teardown
e2e-full: go-install
	@echo "🧪 Full end-to-end run: fresh $(E2E_FULL_VALIDATORS)-validator compose stack → lifecycle → teardown"
	@TIMEFLARE_BLOCK_TIME=$${TIMEFLARE_BLOCK_TIME:-2s} VALIDATOR_COUNT=$(E2E_FULL_VALIDATORS) $(MAKE) docker-reset
	@status=0; $(MAKE) docker-e2e || status=$$?; \
	if [ $$status -eq 0 ]; then \
		./devnet/docker/check-app-hash.sh $(E2E_FULL_VALIDATORS) || status=$$?; \
	fi; \
	echo "🧹 Tearing down compose devnet..."; \
	docker compose -f $(DOCKER_COMPOSE_FILE) down -v >/dev/null 2>&1 || true; \
	rm -rf $(DOCKER_DEVNET_DIR); \
	if [ $$status -eq 0 ]; then echo "✅ Full E2E passed (incl. app-hash agreement) and environment cleaned up"; \
	else echo "❌ Full E2E failed (exit $$status)"; fi; \
	exit $$status

## run the failure-path scenario suite (host-side) against the compose devnet
docker-e2e-scenarios: sdk-sync
	@if ! curl -s --max-time 2 $(DOCKER_RPC)/status >/dev/null 2>&1; then \
		echo "❌ Compose devnet not reachable — start it with 'make docker-up'"; exit 1; fi
	@$(DOCKER_E2E_ENV) $(USER_SETUP) fund
	@if [ ! -f $(DOCKER_DEVNET_DIR)/recipient-keypair.json ]; then \
		echo "🔑 Generating recipient keypair..."; \
		node $(E2E_KEYGEN) $(DOCKER_DEVNET_DIR)/recipient-keypair.json; fi
	@echo "🧪 Running failure-path scenario suite against the compose stack..."
	@$(DOCKER_E2E_ENV) ./devnet/e2e-scenarios.sh

.PHONY: guardiand-fetch-linux docker-build docker-up docker-down docker-reset docker-logs docker-status docker-e2e docker-e2e-scenarios e2e-full
