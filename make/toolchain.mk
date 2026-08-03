# Toolchain Verification
# `make doctor` checks every tool local development needs and prints install hints.

##@ Getting started

## verify the local development toolchain (go, buf, jq)
doctor:
	@echo "🩺 Timeflare toolchain check"
	@echo "============================"
	@failed=0; \
	check() { \
		name="$$1"; cmd="$$2"; hint="$$3"; \
		if command -v "$$cmd" >/dev/null 2>&1; then \
			version=$$("$$cmd" --version 2>/dev/null | head -1 || echo "installed"); \
			printf "  ✅ %-14s %s\n" "$$name" "$$version"; \
		else \
			printf "  ❌ %-14s missing — %s\n" "$$name" "$$hint"; \
			failed=1; \
		fi; \
	}; \
	if command -v go >/dev/null 2>&1; then \
		printf "  ✅ %-14s %s\n" "go" "$$(go version)"; \
	else \
		printf "  ❌ %-14s missing — install from https://go.dev/dl/\n" "go"; \
		failed=1; \
	fi; \
	check "buf"       buf       "https://buf.build/docs/installation"; \
	check "jq"        jq        "brew install jq"; \
	echo ""; \
	if command -v golangci-lint >/dev/null 2>&1; then \
		printf "  ✅ %-14s %s\n" "golangci-lint" "$$(golangci-lint version 2>/dev/null | head -1)"; \
	else \
		printf "  ℹ️  %-14s not installed (auto-installed on first 'make verify')\n" "golangci-lint"; \
	fi; \
	if command -v govulncheck >/dev/null 2>&1; then \
		printf "  ✅ %-14s installed\n" "govulncheck"; \
	else \
		printf "  ℹ️  %-14s not installed (auto-installed on first 'make deps-verify')\n" "govulncheck"; \
	fi; \
	if [ "$$failed" = "1" ]; then \
		echo ""; echo "❌ Toolchain incomplete — install the tools above and re-run 'make doctor'"; \
		exit 1; \
	fi; \
	echo "✅ Toolchain complete — you're ready to 'make build' and 'make dev-up'"

.PHONY: doctor
