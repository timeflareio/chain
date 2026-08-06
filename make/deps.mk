# Dependency Management — tiered update + validation targets
# (docs/guides/DEPENDENCIES.md holds the tier definitions, runbooks, and the
# cross-tier ordering rule: T0 security → T2 consensus → T3 crypto → T1 routine.)
#
# deps-verify             one gate for every dependency root, used by humans and CI
# deps-update-routine     patch-level Go updates, then verify
# deps-update-proto       buf.lock update + proto regeneration, then verify
# deps-update-consensus   T2 scaffold: bump cosmos-sdk in ALL pinned Go modules (replace
#                         pins included), sync, verify — judgement steps stay manual
# deps-verify-consensus   the T2 devnet gate: fresh devnet → e2e + scenarios →
#                         genesis export/validate round-trip → teardown
# deps-pins               print every carried pin/patch with its recorded reason

##@ Dependencies

## verify every dependency root (build + test + audit + drift checks)
#
# "Every dependency root" means every root IN THIS REPOSITORY. The guardian, the
# SDK and crypto are separate repositories with their own gates, and each runs
# this same verification against its own roots — the chain cannot speak for them
# and should not pretend to. Cross-repo agreement is what the pinned versions,
# `verify-pins` and the vector corpora establish, not this target.
deps-verify:
	@echo "🔍 Verifying all dependency roots"
	@echo "── Chain (go build + test) ──────────────────────────────"
	@go build ./...
	@$(MAKE) test
	@echo "── Leaf modules (go build; tests run via make test) ─────"
	@for m in $(GO_SUBMODULE_DIRS); do \
		(cd $$m && go build ./...) || exit 1; \
	done
	@echo "── Proto deps (buf build + lint) ────────────────────────"
	@buf build && buf lint
	@echo "── Vulnerability scan (govulncheck, all Go modules) ─────"
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	@$(MAKE) govulncheck-gated GOVULN_DIR=.
	@$(MAKE) govulncheck-gated GOVULN_DIR=x/secrets/types
	@echo "── Module metadata drift check ──────────────────────────"
	@# There is no committed go.work to sync any more (the umbrella workspace is
	@# uncommitted and per-developer), so the drift this checks for is per-module:
	@# go.mod/go.sum must already be tidy, or the committed dependency metadata
	@# does not describe what the code imports.
	@go mod tidy
	@for m in $(GO_SUBMODULE_DIRS); do (cd $$m && go mod tidy) || exit 1; done
	@git diff --exit-code go.mod go.sum $(foreach m,$(GO_SUBMODULE_DIRS),$(m)/go.mod $(m)/go.sum) || \
		(echo "❌ module metadata drifted — commit the tidied go.mod/go.sum"; exit 1)
	@echo "✅ All dependency roots verified"

## routine (T1) update: patch-level Go updates, then full verification
deps-update-routine:
	@echo "📦 Routine dependency update (patch/minor only)"
	@$(MAKE) mod-update-patch
	@for m in $(GO_SUBMODULE_DIRS); do \
		echo "📦 Updating $$m to latest patch versions..."; \
		(cd $$m && go get -u=patch ./... && go mod tidy) || exit 1; \
	done
	@# Rust and npm dependencies belong to the repositories that own that code
	@# (crypto and typescript-sdk); each runs its own routine update.
	@$(MAKE) deps-verify

# The mechanical steps are automated; the judgement steps of the T2 runbook
# (changelog review incl. every cosmossdk.io/* submodule, replace-pin
# re-justification) remain yours — see docs/guides/DEPENDENCIES.md.
## T2 consensus scaffold: bump cosmos-sdk in all pinned Go modules, sync, verify (usage: make deps-update-consensus VERSION=v0.54.0)
deps-update-consensus:
	@test -n "$(VERSION)" || \
		(echo "❌ VERSION is required — e.g. make deps-update-consensus VERSION=v0.54.0"; exit 1)
	@echo "⚠️  T2 runbook — judgement steps this target cannot do for you:"
	@echo "   1. Read the cosmos-sdk $(VERSION) release/upgrade notes AND the changelog"
	@echo "      of every cosmossdk.io/* submodule that moves with it (store/genesis-"
	@echo "      format changes especially)."
	@echo "   2. Re-justify or drop every carried pin — review follows below."
	@echo "   3. Mirror the resulting pin block in timeflareio/guardian. It carries a"
	@echo "      copy because Go honours replace directives only in the main module"
	@echo "      being built; its 'make verify-pins' fails until it moves, and until"
	@echo "      then the two daemons build against different SDK versions."
	@echo ""
	@echo "📦 Bumping cosmos-sdk to $(VERSION) in all pinned Go modules (require + replace pin)..."
	@if grep -qE 'github.com/cosmos/cosmos-sdk => github.com/cosmos/cosmos-sdk' go.mod; then \
		go mod edit -replace github.com/cosmos/cosmos-sdk=github.com/cosmos/cosmos-sdk@$(VERSION); \
		echo "   chain: replace pin updated → $(VERSION) (re-check whether the pin is still needed at all)"; \
	fi
	@go get github.com/cosmos/cosmos-sdk@$(VERSION) && go mod tidy
	@if grep -qE 'github.com/cosmos/cosmos-sdk => github.com/cosmos/cosmos-sdk' x/secrets/types/go.mod; then \
		cd x/secrets/types && go mod edit -replace github.com/cosmos/cosmos-sdk=github.com/cosmos/cosmos-sdk@$(VERSION); \
		echo "   x/secrets/types: replace pin updated → $(VERSION)"; \
	fi
	@cd x/secrets/types && go get github.com/cosmos/cosmos-sdk@$(VERSION) && go mod tidy
	@echo ""
	@echo "📌 Carried pins to re-justify on this upgrade:"
	@$(MAKE) deps-pins
	@echo ""
	@echo "ℹ️  If buf deps moved with the sdk: run 'make deps-update-proto' before verifying."
	@$(MAKE) deps-verify
	@echo ""
	@echo "✅ Mechanical bump + deps-verify complete. Now run the T2 devnet gate:"
	@echo "   make deps-verify-consensus"

# Required for every consensus-stack (T2) bump; label the PR 'e2e' so CI runs it too.
## T2 devnet gate: fresh devnet → e2e + scenarios → genesis round-trip (catches state-format drift) → teardown
deps-verify-consensus:
	@echo "🧪 T2 devnet gate: fresh devnet → e2e → scenarios → genesis round-trip"
	@TIMEFLARE_BLOCK_TIME=$${TIMEFLARE_BLOCK_TIME:-$(TEST_BLOCK_TIME)} $(MAKE) dev-reset
	@status=0; \
	$(MAKE) e2e && $(MAKE) e2e-scenarios || status=$$?; \
	$(MAKE) dev-down; \
	if [ $$status -ne 0 ]; then \
		echo "❌ T2 devnet gate failed (exit $$status) — chain state kept in ~/.timeflare"; \
		exit $$status; \
	fi
	@echo "── Genesis export/validate round-trip ───────────────────"
	@tmp=$$(mktemp -d) && \
	timeflared export --home "$$HOME/.timeflare" > "$$tmp/exported-genesis.json" && \
	timeflared init t2-roundtrip --chain-id t2-roundtrip --home "$$tmp/home" > /dev/null 2>&1 && \
	cp "$$tmp/exported-genesis.json" "$$tmp/home/config/genesis.json" && \
	timeflared genesis validate-genesis --home "$$tmp/home" && \
	rm -rf "$$tmp" && \
	echo "✅ Exported post-e2e state re-validates on a clean home" || \
	(echo "❌ Genesis round-trip failed — state-format drift between export and import"; exit 1)
	@echo "✅ T2 devnet gate passed"

## update proto module deps (buf.lock) and regenerate, then verify
deps-update-proto:
	@echo "📦 Updating buf module dependencies..."
	@buf dep update
	@$(MAKE) proto-gen
	@$(MAKE) deps-verify

# internal: govulncheck in GOVULN_DIR, failing only on REACHABLE advisories not
# accepted (with a documented reason) in .govulncheck-accepted. Unreachable
# advisories never fail the gate; unfixable-but-reachable ones must be accepted
# explicitly rather than blocking the gate forever.
govulncheck-gated:
	@found=$$(cd $(GOVULN_DIR) && govulncheck -format json ./... | \
		jq -r 'select(.finding != null and .finding.trace[0].function != null) | .finding.osv' | sort -u); \
	accepted=$$(grep -E '^GO-' .govulncheck-accepted 2>/dev/null || true); \
	remaining=""; \
	for id in $$found; do \
		echo "$$accepted" | grep -qxF "$$id" || remaining="$$remaining $$id"; \
	done; \
	if [ -n "$$remaining" ]; then \
		echo "❌ Reachable vulnerabilities in $(GOVULN_DIR):$$remaining"; \
		echo "   Fix by bumping the affected module (T0 runbook in docs/guides/DEPENDENCIES.md)."; \
		echo "   Only if genuinely unfixable: add to .govulncheck-accepted with reason + exit condition."; \
		echo "   Details:"; \
		cd $(GOVULN_DIR) && govulncheck ./... || true; \
		exit 1; \
	fi; \
	for id in $$found; do \
		echo "  ⚠️  accepted advisory triggered: $$id (see .govulncheck-accepted)"; \
	done; \
	echo "  ✅ govulncheck ($(GOVULN_DIR)): no unaccepted reachable vulnerabilities"

## print the carried dependency pins/patches with their recorded reasons
deps-pins:
	@echo "📌 Carried dependency pins and patches"
	@echo ""
	@echo "── go.mod replace directives ──"
	@awk '/^replace \(/{inblk=1;next} inblk&&/^\)/{inblk=0;next} inblk{print "  "$$0}' go.mod
	@echo ""
	@echo "── x/secrets/types/go.mod replace directives ──"
	@awk '/^replace \(/{inblk=1;next} inblk&&/^\)/{inblk=0;next} inblk{print "  "$$0}' x/secrets/types/go.mod
	@awk '/^replace [^(]/{print "  "$$0}' x/secrets/types/go.mod
	@echo ""

.PHONY: deps-verify deps-update-routine deps-update-proto deps-pins deps-update-consensus deps-verify-consensus govulncheck-gated
