# Release Engineering & CI Completion Plan

| | |
|---|---|
| **Priority** | P2 |
| **Status** | refining |
| **Created** | July 2026 |
| **Updated** | 11 November 2026 |
| **Protocol surface** | none |
| **Repositories** | chain |
| **Depends on** | — |
| **Related** | [PENDING_RELEASE_STRATEGY_PLAN.md](PENDING_RELEASE_STRATEGY_PLAN.md); [DONE_CONTAINERISATION_PLAN.md](done/DONE_CONTAINERISATION_PLAN.md); [DONE_DEPENDENCY_MANAGEMENT_PLAN.md](done/DONE_DEPENDENCY_MANAGEMENT_PLAN.md); `timeflareio/guardian` — `PENDING_RELEASE_STRATEGY_PLAN.md`; `timeflareio/network` — `PENDING_TESTNET_LAUNCH_PLAN.md` |
| **Open questions** | 6 |

**Why this priority**: required before asking third parties to run binaries
**Status notes**: six open questions below; not executable until they are ruled.
**Origin**: automated review, July 2026.
**Components**: `.github/workflows/release.yml`, `.github/workflows/ci.yml`, `Dockerfile`, `CHANGELOG.md` (new), `docs/upgrades.md`

**Already landed**: the release workflow itself. `.github/workflows/release.yml` (migration phase 2b, commit `4f72605`, 3 August 2026) triggers on a `v*` tag only, enforces the 0.x version policy, gates on `make verify` and `make test`, cross-compiles `timeflared` for darwin and linux on arm64 and amd64 with `CGO_ENABLED=0 -trimpath` and ldflags stamped from the tag, asserts the binary reports the tag, publishes a checksums file, the proto and chain-vector tarballs and generated release notes, and pushes a multi-arch distroless image to `ghcr.io/timeflareio/timeflared`. Nothing in the release path depends on Ignite. The repositories are public, so release assets and GHCR images are readable without credentials.

## What this plan does

Completes the release pipeline to the standard a validator should expect of consensus software: signed and verifiable artefacts, a changelog and release-candidate convention, demonstrated reproducibility, hardened container images, upgrade-artefact packaging, a smoke test that exercises more than the version string, and a defined criterion for the proto-breaking launch switch.

It covers `timeflared` only. Releasing `guardiand` and `guardianctl` is owned by `timeflareio/guardian` — `PENDING_RELEASE_STRATEGY_PLAN.md`.

## Why

The release workflow publishes binaries, an image and the protocol surface, and each of the following is still absent from it:

- **No signatures.** A checksums file protects against corruption, not substitution. Validators are expected to run consensus software with no way to confirm who built it.
- **No changelog or release-candidate convention.** Release notes are composed by the workflow from a fixed template; nothing records what changed between two tags, and there is no `-rc.N` convention for a testnet rehearsal.
- **Reproducibility is assumed, not shown.** The build is pure Go with a toolchain taken from `go.mod`, `-trimpath` and stamped ldflags, so a byte-identical rebuild is realistic — but no recipe is published and no one has compared two builds.
- **Container images are not hardened.** `Dockerfile` names its base images by tag (`golang:1.26.5`, `gcr.io/distroless/static-debian12:nonroot`), not by digest, and no SBOM is attached to the published image.
- **No upgrade-artefact packaging.** `docs/upgrades.md` defers cosmovisor; a release is not laid out in the cosmovisor directory convention and its notes carry no upgrade-name mapping.
- **The smoke test is one line deep.** The workflow asserts `timeflared version` equals the tag. A binary that reports its version and cannot validate a genesis would still ship.
- **The `ENFORCE_PROTO_BREAKING` launch switch has no agreed flip criterion** (open question 3).

## How

### Phase 1 — Changelog and release-candidate convention

1. `CHANGELOG.md` (new; Keep-a-Changelog format), enforced by a CI check on release pull requests; release notes generated from it in place of the fixed template.
2. Tag protocol: annotated tags only (every tag to date is annotated, by practice rather than by check), releases cut from `main`, prereleases (`-rc.N`) for testnet rehearsal.

### Phase 2 — Signing and reproducibility

1. Sign the checksums file and the image (mechanism: open question 1) and document the verification steps a validator runs.
2. Publish the build recipe so third parties can byte-compare, and prove it: rebuild a tagged release independently and compare against the published checksums.

### Phase 3 — Container image hardening

Digest-pin both base images in `Dockerfile`, and attach an SBOM attestation (syft/cosign attach) to the image the `image` job pushes.

### Phase 4 — Upgrade-artefact packaging

Attach per-upgrade bundles in the cosmovisor directory convention, or a manifest mapping release → upgrade name, and state the upgrade name in the release notes. Whether this phase belongs here is open question 4.

### Phase 5 — CI hardening remainder

1. Extend the release smoke test: after the version assertion, run a genesis-validation invocation against the built binary, so a broken artefact cannot ship silently.
2. Define the `ENFORCE_PROTO_BREAKING` flip criterion and state it in one place (open question 3).

## Open questions

1. **Signing mechanism**: cosign/OIDC (no key custody, GitHub-trust-rooted) vs. project GPG key (validator-familiar, key-custody burden)? Cosmos validator culture still leans GPG/SHA256SUMS. [PENDING_RELEASE_STRATEGY_PLAN.md](PENDING_RELEASE_STRATEGY_PLAN.md) and `timeflareio/guardian` — `PENDING_RELEASE_STRATEGY_PLAN.md` both raise the same requirement and neither owns the mechanism. *Recommendation: cosign keyless for both the checksums file and the image, decided once for every repository that ships a binary.*
2. **Does this plan fold into [PENDING_RELEASE_STRATEGY_PLAN.md](PENDING_RELEASE_STRATEGY_PLAN.md)?** The workflow this plan set out to build exists; what remains here overlaps that plan's mechanics and open questions (signing, reproducibility, the release gate). *Recommendation: fold the policy items (signing requirement, the flip criterion) into the strategy plan and keep this one as the mechanical residue — changelog, digest pins, SBOM, smoke test — or retire it once those are absorbed.*
3. **When does `ENFORCE_PROTO_BREAKING` flip?** Three statements disagree: the comment in `.github/workflows/ci.yml` says mainnet launch; [PENDING_RELEASE_STRATEGY_PLAN.md](PENDING_RELEASE_STRATEGY_PLAN.md) ties it to `v1.0.0` at mainnet genesis; the automated review behind this plan proposed first public testnet genesis, on the argument that testnet operators also deserve non-broken protos. *Recommendation: first public testnet genesis, recorded in the strategy plan and the `ci.yml` comment together.*
4. **Who owns cosmovisor?** Phase 4 packages releases for it; `timeflareio/network` — `PENDING_TESTNET_LAUNCH_PLAN.md` claims its adoption (Phase 10) and `docs/upgrades.md` defers it. *Recommendation: the network plan owns adoption and names the layout it needs; this plan only produces the artefact, after that plan rules.*
5. **What replaces the CI jobs this plan assumed?** [DONE_DEPENDENCY_MANAGEMENT_PLAN.md](done/DONE_DEPENDENCY_MANAGEMENT_PLAN.md) delivered a label-gated `e2e-full` job, audit steps and a scheduled security sweep in the monorepo's CI. This repository's `.github/workflows/` holds only `ci.yml` and `release.yml`: neither a scheduled sweep nor a label-gated job exists, and the lifecycle e2e runs on every pull request instead. *Recommendation: reinstate the scheduled `govulncheck` sweep here as a small change of its own; treat the multi-validator `e2e-full` run as part of the release-gate question in the strategy plan.*
6. **Is SHA-pinning of GitHub Actions still wanted?** `.github/dependabot.yml` records a ruling of 31 July 2026: actions float on major tags by design, Dependabot proposes majors only, and SHA-pinning is "the answer" only if supply-chain pinning is ever wanted. The automated review behind this plan lists unpinned actions as a gap in the pipeline that produces consensus binaries. *Recommendation: SHA-pin the actions used by `release.yml` only, decided together with question 1, and leave `ci.yml` on major tags as ruled.*
