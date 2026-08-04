# Release Engineering & CI Completion Plan

**Status**: Proposed (automated review, July 2026)
**Priority**: P2 — required before asking third parties to run binaries
**Components**: `.github/workflows/release.yml`, `.github/workflows/ci.yml`, `make/common.mk`, coordinates `DONE_CONTAINERISATION_PLAN.md` and `DONE_DEPENDENCY_MANAGEMENT_PLAN.md`

## What this plan does

Replaces the stock Ignite-CLI release boilerplate with a real release pipeline — versioned, checksummed, signed, complete (both binaries), and reproducible enough for validators to trust — and closes the CI gaps that the dependency-management plan doesn't already claim.

## Why

The current `release.yml` is uncustomised Ignite boilerplate: any `v*` tag (or push to the default branch) builds `timeflared` for three platforms via `ignite/cli` actions and publishes a GitHub prerelease. Problems:

- **`guardiand` is not released at all** — guardian operators, the network's second citizen, have no artefact.
- **No checksums or signatures** — validators are expected to run consensus software with no integrity story. (SHA256SUMS + signing is table stakes; validators on other chains routinely verify.)
- **No changelog or release notes discipline**; version injection exists (`make/common.mk` ldflags) but nothing governs what a tag means.
- ~~**cgo complicates everything**~~ **Resolved (July 2026)**: `DONE_CONSENSUS_CRYPTO_PURE_GO_PLAN.md` landed — both released binaries are pure Go (the Rust FFI is deleted; Rust remains WASM-only for the SDK). Cross-compilation, reproducible builds, and cosmovisor packaging should all assume the pure-Go world.
- **No container images published** — the Docker plan creates Dockerfiles for dev; production images (pinned, versioned, published to a registry) are a release concern and belong here.
- **No upgrade artefact packaging**: `docs/upgrades.md`'s runbook and cosmovisor adoption (TESTNET_LAUNCH) need releases laid out in the cosmovisor directory convention with the upgrade-name mapping stated in release notes.
- CI gaps *not* already claimed by the dependency plan (which owns: path-filter fixes, `cargo audit`/`npm audit` gates, scheduled security sweep, label-gated e2e-full job): SHA-pinning of GitHub Actions (currently deferred "to launch hardening" — a supply-chain gap in the pipeline that produces consensus binaries), and the proto-breaking `ENFORCE_PROTO_BREAKING` launch switch having a defined flip criterion.

## How

### Phase 1 — Version & changelog discipline

1. Adopt semver with a stated pre-1.0 policy (breaking changes allowed, minor-bump signalled — matching the project's pre-launch convention).
2. `CHANGELOG.md` (Keep-a-Changelog format), enforced by a CI check on release PRs; release notes generated from it.
3. Tag protocol: annotated tags only, releases cut from `main`, prereleases (`-rc.N`) for testnet rehearsal.

### Phase 2 — The release workflow (replace `release.yml`)

1. Triggered on tag only (drop the on-push-to-default prerelease behaviour).
2. Build matrix: `timeflared` and `guardiand`, linux/amd64 + linux/arm64 + darwin/arm64 (darwin/amd64 if there's demand) — pure Go, no per-target native libraries.
3. Artefacts: tarballs + `SHA256SUMS` + signature (cosign keyless via GitHub OIDC is the low-ceremony modern option; a project GPG key is the traditional one).
4. Reproducibility: pinned Go toolchain, `-trimpath`, stamped ldflags from the tag; publish the build recipe so third parties can byte-compare. With cgo gone, full bit-for-bit reproducibility is a realistic target.
5. Cosmovisor layout: attach per-upgrade bundles or a manifest mapping release → upgrade-name.

### Phase 3 — Container image publishing

Now the Docker plan's Dockerfiles exist (rewritten against the pure-Go binaries as DONE_CONTAINERISATION_PLAN.md, July 2026), add a `docker-publish` job to the release workflow: multi-arch images for `timeflared` and `guardiand` to GHCR, tagged by version + digest-pinned base images, with SBOM attestation (syft/cosign attach) as a cheap add.

### Phase 4 — CI hardening remainder

1. SHA-pin all GitHub Actions (dependabot already tracks actions and will bump pins).
2. Define the `ENFORCE_PROTO_BREAKING` flip criterion (proposal: flip at first public testnet genesis, not mainnet — testnet operators also deserve non-broken protos; pre-testnet breaking stays free, matching the project's working style).
3. Release-workflow smoke test: install the built artefacts and run `timeflared version` / `guardiand version` + a genesis-validation invocation, so a broken artefact can't ship silently.

## Open questions

1. **Signing mechanism**: cosign/OIDC (no key custody, GitHub-trust-rooted) vs. project GPG key (validator-familiar, key-custody burden)? Cosmos validator culture still leans GPG/SHA256SUMS.
2. ~~Sequencing with CONSENSUS_CRYPTO_PURE_GO~~ **Resolved (July 2026)**: pure-Go landed first, as recommended — build the simpler pipeline.
3. **Ignite dependency**: keep Ignite for scaffolding but remove it from the release path entirely (proposed), or drop the dependency altogether?
4. **Private-repo constraint**: is the repo public by testnet time? Release links, GHCR visibility, and chain-registry entries all assume public artefacts.
