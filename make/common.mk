# Common Makefile Infrastructure
# Shared variables, shell setup, colors, and utilities used across all Makefiles

# Git and version information
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT := $(shell git log -1 --format='%H')

# Determine version from git tags or branch/commit
ifeq (,$(VERSION))
  VERSION := $(shell git describe --exact-match 2>/dev/null)
  # if VERSION is empty, then populate it with branch name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

# Build flags for Go applications
# Note: APPNAME should be set by the including Makefile
ldflags = -X github.com/cosmos/cosmos-sdk/version.Name=$(APPNAME) \
	-X github.com/cosmos/cosmos-sdk/version.AppName=$(APPNAME) \
	-X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
	-X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

BUILD_FLAGS := -ldflags '$(ldflags)'

# Shell configuration
SHELL := /bin/bash

# Terminal colors for consistent output
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
BLUE   := $(shell tput -Txterm setaf 4)
RED    := $(shell tput -Txterm setaf 1)
WHITE  := $(shell tput -Txterm bold)$(shell tput -Txterm setaf 7)
RESET  := $(shell tput -Txterm sgr0)

# The cadence every test path runs at, and the only place it is written.
#
# The deployment cadence lives in networks.json, which is what a devnet brought
# up for interactive work uses. Tests override it because the suites wait on
# block-denominated deadlines: at the deployment cadence they take about six
# times as long and cover nothing extra. A target that needs the test cadence
# takes it from here rather than carrying its own default — see
# docs/planning/done/DONE_BLOCK_TIME_CONFIGURATION_PLAN.md.
# 200ms, measured rather than chosen: seven consecutive full runs of the scenario
# suite at this value, all eleven scenarios, no failures. 100ms is intermittent and
# 50ms fails outright, and in both cases what fails is the harness rather than the
# chain — a suite step that takes seconds overruns a window measured in blocks.
# timeout_commit is a post-commit delay, so the real cadence lands ~30ms above this
# (the suites measure and report it).
TEST_BLOCK_TIME ?= 200ms

# Help system configuration
TARGET_MAX_CHAR_NUM=22

# Grouped help: a '##@ Section' comment assigns every following '## described'
# target (in that file) to the named section; targets in the same section merge
# across files. HELP_SECTION_ORDER ('|'-separated) fixes the section order;
# unlisted sections print afterwards in first-seen order, and targets with no
# section land under 'Targets' — so Makefiles without markers keep a flat list.
#
# The '## description' line applies to the next target, and plain '#' rationale
# comments may sit between the two: a target's description belongs at the top of
# its comment block, above the paragraphs explaining why it does what it does.
# Anything else — a blank line, a recipe, another target — ends the pairing.
HELP_SECTION_ORDER ?=

# Universal help target that works with any Makefile
##@ Misc

## show available targets and their descriptions
help:
	@echo ''
	@echo 'Usage:'
	@echo '  ${YELLOW}make${RESET} ${GREEN}<target>${RESET}'
	@awk 'FNR == 1 { section = ""; doc = "" } \
	/^##@ / { section = substr($$0, 5); doc = ""; next } \
	/^## / { doc = substr($$0, 4); next } \
	/^#/ { next } \
	/^[a-zA-Z\-\_0-9%]+:/ { \
		if (doc != "") { \
			helpCommand = substr($$1, 1, index($$1, ":")-1); \
			s = (section == "" ? "Targets" : section); \
			if (!(s in bodies)) { seen[++n] = s } \
			bodies[s] = bodies[s] sprintf("  ${YELLOW}%-$(TARGET_MAX_CHAR_NUM)s${RESET} ${GREEN}%s${RESET}\n", helpCommand, doc); \
		} \
		doc = ""; next \
	} \
	{ doc = "" } \
	END { \
		nPref = split("$(HELP_SECTION_ORDER)", pref, "|"); \
		for (i = 1; i <= nPref; i++) emit(pref[i]); \
		for (i = 1; i <= n; i++) emit(seen[i]); \
	} \
	function emit(s) { \
		if (s in bodies) { \
			printf "\n${WHITE}%s${RESET}\n%s", s, bodies[s]; \
			delete bodies[s]; \
		} \
	}' $(MAKEFILE_LIST)

# Common file path patterns for exclusions
VENDOR_PATHS := -not -path './vendor/*' -not -path './third_party/*'
VENDOR_PATHS_GUARDIAN := -not -path './vendor/*'

# Default timeout configurations (can be overridden)
TEST_TIMEOUT ?= 5m
LINT_TIMEOUT ?= 15m

# Default test packages (can be overridden)
TEST_PACKAGES ?= ./...

# Nested leaf library modules owned by the root Makefile — set there. This repo
# has one: x/secrets/types, the wire contract. `go <cmd> ./...` from the repo
# root does NOT descend into a nested module, so every quality/test/deps target
# must iterate them explicitly. Each component repository keeps its own copies
# of these fragments, so this default is not shared with anything.
GO_SUBMODULE_DIRS ?=

# Coverage file configuration
COVERAGE_FILE ?= coverage.out
COVERAGE_HTML_FILE ?= coverage.html

.PHONY: help