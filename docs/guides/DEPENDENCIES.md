# Dependency Management

How dependencies are updated, validated, and patched across every ecosystem
in this repository. The design rationale lives in
[docs/planning/done/DONE_DEPENDENCY_MANAGEMENT_PLAN.md](../planning/done/DONE_DEPENDENCY_MANAGEMENT_PLAN.md);
this guide is the operational reference.

## Dependency roots

| Root | Manifest / lock | Update path | Automated |
|---|---|---|---|
| Chain (Go) | `go.mod` / `go.sum` | `make deps-update-routine` | Dependabot weekly |
| Guardian (Go) | `guardian/go.mod` / `go.sum` | same | Dependabot weekly |
| Types module (Go) | `x/secrets/types/go.mod` / `go.sum` | same | Dependabot weekly |
| Crypto module (Go) | `crypto/go.mod` / `go.sum` | same | Dependabot weekly |
| Workspace | `go.work` / `go.work.sum` | `go work sync` (drift-checked by `deps-verify`) | via the make targets |
| Rust crypto | `rust/Cargo.toml` / `Cargo.lock` | `make deps-update-routine` | Dependabot weekly |
| TypeScript SDK | `typescript-sdk/package.json` / lock | `make deps-update-routine` | Dependabot weekly |
| Proto deps | `buf.lock` | `make deps-update-proto` | via the make target (no Dependabot support) |
| GitHub Actions | `.github/workflows/*` | Dependabot weekly (actionlint-gated) | Dependabot weekly |

## The tiers

| Tier | What | Examples | Gate |
|---|---|---|---|
| **T1 — routine** | Patch/minor of non-consensus, non-crypto deps | zap, testify, jest, serde | Grouped scheduled PR; full CI green ⇒ mergeable on review |
| **T2 — consensus-adjacent** | cosmos-sdk, cosmossdk.io/\*, cometbft, iavl, ics23, gogoproto, buf deps | cosmos-sdk v0.53.x → v0.55 (August 2026) | Manual branch per the T2 runbook below: full CI **plus fresh-devnet `make e2e` + `make e2e-scenarios`** (label the PR `e2e` to run the devnet gate in CI), genesis export/import check |
| **T3 — cryptographic** | rust: x25519-dalek, chacha20poly1305, sha2, hmac, rand, rand_chacha; Go crypto libs; TS cosmjs crypto | dalek major bump | Changelog review + the Rust vector/round-trip suite + cross-client e2e (a seal on the new version must unseal against a chain/guardian on it) |
| **T0 — security** | Any advisory affecting a shipped path, any tier | GHSA in gin (already carried) | Immediate, targeted, minimal diff (runbook below); severity decides whether it pre-empts in-flight work |

## Ordering when multiple tiers are pending

When updates across several tiers are queued (a Dependabot backlog, a
post-holiday sweep), land them **key dependencies first**:

```
T0 security  →  T2 consensus stack  →  T3 cryptographic  →  T1 routine
```

Rationale:

1. **T2 moves the ground under everything else.** A cosmos-sdk bump drags the
   grouped `cosmossdk.io/*` submodules and a swathe of transitive T1-level
   dependencies with it, and re-audits the entire `replace` block. T1 PRs
   raised against the old graph are often obsoleted (Dependabot auto-closes
   them) or need re-verification — merging them first means verifying twice.
2. **T3 sits on the T2 result.** The crypto crates' dependency graph
   (rand_core, digest versions) should be settled against the final Go/WASM
   toolchain state, not re-churned afterwards.
3. **T1 last is free.** Routine bumps are grouped, CI-gated and conflict only
   on lockfiles — Dependabot rebases them onto whatever landed above.

A tier that is *blocked* does not hold the queue: if the T2/T3 bump needs its
deliberate branch and nobody is doing that work now, defer it with a comment
on the PR (state the tier, the blocker, and the runbook) and let T1 proceed —
the ordering rule is about sequencing work that is actually happening, not
about freezing routine updates behind a stalled major.

## Make targets

- `make deps-verify` — the single validation gate: builds and tests all four
  Go modules (chain, guardian, and the leaf library modules `crypto` and
  `x/secrets/types`), runs `cargo test` + `cargo audit`, the SDK tests +
  `npm audit`, `buf build`/`buf lint`, `govulncheck` on all four modules, and
  the workspace-drift check (`go work sync` must leave `go.work.sum` unchanged).
  The govulncheck step fails only on **reachable** advisories that are not
  listed in `.govulncheck-accepted` — a documented allowlist for genuinely
  unfixable findings (e.g. `x/crypto/openpgp`, unmaintained by design with no
  fixed version ever coming). Every accepted entry carries a reason and an
  exit condition, and the file is re-audited on every T2 upgrade; if a fixed
  release exists, bump the module instead of accepting.
- `make deps-update-routine` — patch-level Go updates (`go get -u=patch`),
  semver-range `cargo update` / `npm update`, then `deps-verify`.
- `make deps-update-proto` — `buf dep update` + `make proto-gen` +
  `deps-verify`.
- `make deps-update-consensus VERSION=vX.Y.Z` — the T2 scaffold: bumps
  cosmos-sdk in **every** sdk-bearing Go module (chain, guardian,
  `x/secrets/types`; if a module carries an sdk `replace` pin it is updated
  too — none is carried today, the sdk version floats on the natural graph
  resolution), `go work sync`, prints the carried pins for
  re-justification, then runs `deps-verify`. The judgement steps of the T2
  runbook (changelog review of every moving `cosmossdk.io/*` submodule,
  deciding whether each pin is still needed) are printed as a checklist, not
  automated.
- `make deps-verify-consensus` — the T2 devnet gate: fresh `dev-reset` →
  `make e2e` → `make e2e-scenarios` → genesis export/validate round-trip on a
  clean home (catches state-format drift) → teardown. Run after
  `deps-update-consensus` compiles and unit-verifies; label the PR `e2e` so CI
  runs the devnet gate too.
- `make deps-pins` — prints every carried pin/patch with its recorded reason.

T2/T3 bumps are **never batch-updated** — always a deliberate branch, per
runbook. The `deps-update-consensus` scaffold automates the T2 runbook's
mechanical steps only; it is not a batch tool.

## Runbook: security patch (T0)

1. Alert arrives (Dependabot alert / security-sweep workflow failure / cosmos
   security advisory channel).
2. Branch; apply the **minimal** bump (`go get module@fixed`,
   `cargo update -p crate`, `npm update pkg` — never the blanket target).
   If no fixed release exists yet: carry a pin/patch (see below).
3. `make deps-verify`; T2/T3 packages additionally get their tier's gate.
4. PR labelled `security`; merge on green. Speed comes from the diff being
   minimal, not from skipping gates.

## Runbook: cosmos-sdk / consensus-stack upgrade (T2)

1. Read the release/upgrade notes **and the changelog of every
   cosmossdk.io/\* submodule that moves with it**; note store/genesis-format
   changes. (Judgement — no tooling can do this for you.)
2. Branch. Run `make deps-update-consensus VERSION=vX.Y.Z` — it bumps every
   sdk-bearing Go module **together** (chain + guardian + `x/secrets/types`
   must resolve the same sdk; today all three do so naturally, with no sdk
   replace pin — the scaffold updates a pin only where one is carried),
   syncs the workspace, prints the carried pins, and runs `deps-verify`.
   Then actually re-justify or drop each printed pin — the scaffold reminds
   you; it cannot decide.
3. `make deps-update-proto` if buf deps moved with the sdk.
4. `make deps-verify-consensus` — the devnet gate: fresh `dev-reset`,
   `make e2e`, `make e2e-scenarios`, plus the genesis export/validate
   round-trip on a clean home to catch state-format drift. Label the PR
   `e2e` to also run the devnet gate in CI.
5. Post-launch this runbook grows the `make devnet-upgrade-test` governance
   rehearsal and coordination with `ENFORCE_PROTO_BREAKING`; pre-launch,
   dev-reset absorbs everything.

## Carrying local patches (pins, forks, overrides)

Mechanisms per ecosystem: Go `replace`, Cargo `[patch.crates-io]`, npm
`overrides` (or `patch-package` for source patches). Rules:

1. Every carried patch has a **comment with the reason** and a **tracking
   reference** (upstream issue/PR or advisory ID).
2. Every carried patch has an **exit condition** ("until gin ≥ 1.9.1 is in
   the dependency graph naturally"). T2 upgrades re-audit the whole block.
3. `make deps-pins` prints the current carried set — the review surface for
   "what are we still dragging along?".
4. Forking a dep (rather than pinning a version) requires a plan-doc note —
   forks are where carried patches go to be forgotten.

## Scheduled scanning

The `security-sweep` workflow runs weekly (Monday 06:00 UTC) on main:
`govulncheck` on all four Go modules, `cargo audit`, and `npm audit` — catching
advisories against dependencies no recent PR has touched. PR-path audits
(`cargo audit`, `npm audit` in their CI jobs) gate dependency changes as they
arrive.
