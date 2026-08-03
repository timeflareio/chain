# timeflare chain — Makefile
#
# The chain: app/, cmd/timeflared/, x/secrets/ and the protocol surface
# (proto/, the x/secrets/types wire contract, and the chain-semantics vector
# corpus). Two Go modules live here — the root module and the nested
# x/secrets/types leaf, which is tagged and consumed independently so an
# integrator can pin the wire contract without inheriting chain internals.
#
# Cryptographic primitives come from github.com/timeflareio/crypto at a pinned
# tag, imported as .../crypto/go. Nothing in this repository builds them.
#
# Devnet, container and upgrade targets arrive with phase 2b of the multi-repo
# migration; this Makefile covers modules, build, quality and tests.

# Load common foundation
include make/common.mk

# Nested leaf library modules owned by this Makefile (see common.mk note) —
# quality/test/deps targets iterate these in addition to $(TEST_PACKAGES).
GO_SUBMODULE_DIRS := x/secrets/types

# Load shared capabilities
include make/go-testing.mk
include make/go-quality.mk
include make/go-build.mk
include make/toolchain.mk
include make/cleanup.mk
include make/devnet.mk
include make/deps.mk

# Project configuration
APPNAME := timeflare
CMD_PATH := ./cmd/timeflared

# Default target - show help
.DEFAULT_GOAL := help

# Section order for the grouped `make help` output (see make/common.mk)
HELP_SECTION_ORDER := Getting started|Testing|Build|Code quality|Dependencies|Misc

###############################################################################
###                              Protobuf                                  ###
###############################################################################

##@ Build

## generate Go protobuf files from proto/
proto-gen:
	@echo "Generating Go protobuf files..."
	@buf generate --template proto/buf.gen.gogo.yaml
	@# buf writes source-relative paths (x/secrets/types/timeflare/secrets/v1/);
	@# the live package is flat under x/secrets/types — move and tidy
	@if [ -d x/secrets/types/timeflare ]; then \
		mv x/secrets/types/timeflare/secrets/v1/*.go x/secrets/types/ 2>/dev/null; \
		rm -rf x/secrets/types/timeflare; \
	fi
	@./make/scripts/export-buf-deps.sh

###############################################################################
###                              Testing                                   ###
###############################################################################

##@ Testing

## run all unit tests, including the nested types module
test: go-test-unit
	@echo "✅ All tests passed!"

###############################################################################
###                              Build                                     ###
###############################################################################

##@ Build

## regenerate protobufs and install the binary
build: proto-gen go-install
	@echo "🔨 Build completed successfully!"

## install binary to GOPATH/bin
install: go-install

## build binary without installing
build-binary: go-build-binary

# Legacy alias
all: build

###############################################################################
###                           Code Quality                                 ###
###############################################################################

##@ Code quality

## format and lint all code (with fixes)
format: go-format go-lint

# NB: go-govulncheck is intentionally NOT part of the blocking verify set.
# Vulnerability findings depend on upstream advisories outside our control
# (including fixes that don't yet exist), so it gates dependency changes
# (make deps-verify) and runs on the weekly security-sweep workflow rather
# than blocking every merge.
## verify all code quality standards (read-only checks)
verify: go-format-check go-lint-check go-vet verify-boundaries verify-choke-points

###############################################################################
###                           Cleanup                                      ###
###############################################################################

## clean and format all code, removing build artefacts and temporary files
clean: format clean-all

## clean temporary files and build artefacts
clean-all: clean-temps clean-coverage clean-build

###############################################################################
###                           Dependencies                                 ###
###############################################################################

# Removed: blanket `go get -u ./...` walked consensus-stack minors in one
# undifferentiated sweep. Use the tiered targets in make/deps.mk instead.
update-deps:
	@echo "⛔ 'make update-deps' was removed — it blanket-updated every root at once."
	@echo "   Use the tiered targets instead:"
	@echo "     make deps-update-routine   patch/minor updates + full verification"
	@echo "     make deps-update-proto     buf.lock update + proto regen + verification"
	@echo "     make deps-verify           validation only (no updates)"
	@echo "     make deps-pins             list carried pins/patches with reasons"
	@echo "   Consensus (T2) and crypto (T3) bumps are never batch-updated."
	@exit 1

##@ Misc

## clean and verify Go modules (download, tidy, verify)
tidy: mod-clean

## download Go modules
download: mod-clean

## show module information
info: mod-info

.PHONY: proto-gen test build install build-binary all
.PHONY: format verify clean clean-all update-deps tidy download info
