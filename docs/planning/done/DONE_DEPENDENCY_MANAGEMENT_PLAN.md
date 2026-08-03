# Dependency Management — Upgrades, Patches & Validation Across All Ecosystems

*One workflow for keeping every dependency root current: routine upgrades
arrive as grouped, CI-validated PRs; security patches (including cosmos-sdk
advisories) have a fast, rehearsed path from alert to merged fix; and every
dependency change — routine or urgent — passes the same test/validate gate
before it ships.*

> **Status: DONE — implemented (July 2026).** Landed on
> `guardian-improvements-dep-management`: tiered make targets (`deps-verify`,
> `deps-update-routine`, `deps-update-proto`, `deps-pins`; blanket
> `update-deps` removed), Dependabot T1/T2/T3 grouping, CI path-filter fixes
> + audit steps + actionlint + label-gated `e2e-full` job, the weekly
> security-sweep workflow, annotated go.mod pins, and
> docs/guides/DEPENDENCIES.md. Originally compiled
> from the current tooling state; all §7 questions resolved: Dependabot kept
> and extended (no Renovate), a label-triggered `e2e-full` CI job gates T2
> consensus-stack PRs, weekly cadence across all ecosystems, and
> `cargo deny`/Actions SHA-pinning deferred to launch hardening. The repo
> already has partial machinery (Dependabot, a blunt `make update-deps`,
> `go-govulncheck`); this plan completes it, fixes what's unsafe about it, and
> defines the runbooks. Cross-checked against the repo (July 2026): the
> current-state claims are verified; the security-scanning baseline turned
> out weaker than first drafted (§1.4) and the CI path-filter audit results
> are now recorded rather than left as "verify" items (§1.3).

## Contents

1. [Current state](#1-current-state)
2. [Goals](#2-goals)
3. [The upgrade tiers](#3-the-upgrade-tiers)
4. [The validation pipeline](#4-the-validation-pipeline)
5. [Runbooks](#5-runbooks)
6. [Surface area](#6-surface-area)
7. [Open questions](#7-open-questions)

---

## 1. Current state

**Six dependency roots** (five package ecosystems + proto deps):

| Root | Manifest / lock | Update path today | Automated? |
|---|---|---|---|
| Chain (Go) | `go.mod` / `go.sum` | `make update-deps` → `go get -u ./...` | Dependabot weekly, cosmos-grouped |
| Guardian (Go) | `guardian/go.mod` / `go.sum` | same, via `cd guardian` | Dependabot weekly, cosmos-grouped |
| Workspace | `go.work` / `go.work.sum` | manual (`go work sync`) — has previously drifted and needed ad-hoc refresh | ❌ |
| Rust crypto | `rust/Cargo.toml` / `Cargo.lock` | `cargo update` | Dependabot weekly |
| TypeScript SDK | `typescript-sdk/package.json` / `package-lock.json` | `npm update` | Dependabot weekly |
| Proto deps | `buf.lock` (cosmos-proto, cosmos-sdk, gogo, googleapis, ics23, tendermint, wellknowntypes) | nothing — never updated by any target | ❌ (Dependabot has no buf support) |

Plus **GitHub Actions** (Dependabot-covered, weekly).

**Carried pins/patches** — four `replace` directives in `go.mod`, each with a
recorded reason (the pattern to formalise in §5.3):
`cosmos-sdk → v0.53.2` (AutoCLI protobuf fix), `gin → v1.9.1`
(GHSA-h395-qcrw-5vmq), `goleveldb → pre-release` (broken tag),
`nhooyr.io/websocket → coder/websocket` (dead vanity URL). The guardian
module additionally pins cosmos-sdk via its own replace and points at the
chain module by relative path.

**What's wrong with the current machinery:**

1. **`make update-deps` is unsafe by construction**: `go get -u ./...` bumps
   *everything* transitively — it will happily walk cosmos-sdk minor versions
   and consensus-adjacent deps (cometbft, store, iavl) in one undifferentiated
   sweep, fighting the replace pins and mixing risk tiers into one diff.
   `cargo update` and `npm update` have the same shape (everything at once,
   no validation step attached).
2. **No update path at all** for `buf.lock` and `go.work.sum` — the proto
   deps have never been bumped, and workspace-sum drift has already caused
   local breakage once.
3. **Validation isn't attached to updating.** `make update-deps` doesn't run
   anything afterwards; the dep PR pipeline relies on CI's `changes` path
   filters catching manifest/lockfile paths. Audited (July 2026): `go.sum`,
   `guardian/go.sum`, `Cargo.lock` (via `rust/**`), `package-lock.json` (via
   `typescript-sdk/**`) and `buf.lock` all trigger their jobs correctly. Two
   real gaps: **`go.work`/`go.work.sum` match no filter** (a
   workspace-sum-only diff runs zero jobs), and **`.github/workflows/**`
   matches no filter either — Dependabot's GitHub-Actions bumps merge with
   zero CI** (fix in §4; decision logged as §7.5).
4. **Security scanning is manual-only — detection rests on GitHub's
   Dependabot alerts alone.** The Go scanner exists as `make go-govulncheck`
   but is deliberately excluded from the blocking verify set
   (make/go-quality.mk:70), and despite the root Makefile's comment claiming
   it runs in CI (Makefile:131–134 — stale), **no workflow runs it**: neither
   ci.yml nor release.yml has a govulncheck step, so it runs only when typed
   by hand. There is no `cargo audit`/`cargo deny`, no `npm audit` gate, no
   OSV sweep across roots, and nothing scheduled.
5. **No runbook** for the upgrade that actually matters: cosmos-sdk/cometbft,
   where an upgrade is consensus-affecting and must be validated against a
   live devnet, not just unit tests.

## 2. Goals

1. **Routine updates are near-free**: grouped PRs arrive on schedule, CI
   proves them, merging is a review click. Patch/minor noise never interrupts
   protocol work.
2. **Security patches are fast and rehearsed**: alert → branch → targeted
   bump → full validation → merged, with the same muscle memory whether it's
   a Go transitive dep or a cosmos advisory.
3. **Every root is covered** — including the two that currently have no
   update path (buf.lock, go.work.sum).
4. **Risk-tiered, not uniform**: a lodash-style patch bump and a cometbft
   minor are different events with different gates.
5. **Carried patches are managed, not accumulated**: every replace/patch has
   a reason, a tracking reference, and an exit condition.

## 3. The upgrade tiers

| Tier | What | Examples | Gate |
|---|---|---|---|
| **T1 — routine** | Patch/minor of non-consensus, non-crypto deps | zap, testify, jest, serde | Grouped scheduled PR; full CI green ⇒ mergeable on review |
| **T2 — consensus-adjacent** | cosmos-sdk, cosmossdk.io/*, cometbft, iavl, ics23, gogoproto, buf deps | cosmos-sdk v0.53.x → v0.54 | Manual branch per §5.2 runbook: full CI **plus fresh-devnet `make e2e` + `make e2e-scenarios`**, genesis export/import check |
| **T3 — cryptographic** | rust: x25519-dalek, chacha20poly1305, sha2, hmac, rand, rand_chacha; Go: crypto libs; TS: cosmjs crypto | dalek major bump | Changelog review + the Rust vector/round-trip suite + cross-client e2e (a seal on the new version must unseal against a chain/guardian on it) |
| **T0 — security** | Any advisory affecting a shipped path, any tier | GHSA in gin (already carried) | Immediate, targeted, minimal diff (§5.1); severity decides whether it pre-empts in-flight work |

Dependabot grouping is extended to match: the existing `cosmos` group already
*is* the Go-side T2 boundary — its `github.com/cosmos/*` pattern covers
`iavl`, `ics23` and `gogoproto` today, so nothing to add there (the remaining
T2 coverage gap is only the buf deps, which Dependabot cannot see); a second
group batches all T1 noise per ecosystem into one weekly PR; crypto crates
get their own group so T3 review is never buried inside a routine batch.

## 4. The validation pipeline

One target, used by humans and CI identically:

```
make deps-verify
  ├─ go build ./... + make test            (chain + guardian, quality gates included)
  ├─ cargo test + cargo audit              (rust)
  ├─ npm ci + npm test + npm audit         (typescript-sdk, audit: high+)
  ├─ buf build + buf lint                  (proto deps still compile/lint)
  ├─ govulncheck (both Go modules)
  └─ go work sync + git diff --exit-code go.work.sum   (workspace drift check)
```

And the update side becomes tier-shaped instead of blanket:

- `make deps-update-routine` — patch/minor only: `go get -u=patch ./...` (or
  explicit non-T2 module list), `cargo update` (respecting semver ranges),
  `npm update`, then `deps-verify`. **Replaces today's `go get -u ./...`.**
- `make deps-update-proto` — `buf dep update` + `make proto-gen` +
  `deps-verify` (covers the never-updated buf.lock).
- T2/T3 bumps are never batch-updated — always a deliberate
  `go get <module>@<version>` on a branch, per runbook.

**CI changes:**
- Close the two audited path-filter gaps (§1.3): add `go.work`/`go.work.sum`
  to the `go` filter, and give `.github/workflows/**` a `ci` filter feeding a
  minimal workflow-lint job (actionlint), so Actions bumps stop merging with
  zero CI (§7.5). The other lockfile paths are verified working — no change
  needed.
- Add the audit steps (`cargo audit`, `npm audit --audit-level=high`) to
  their ecosystem jobs — cheap, and turns Dependabot alerts from the only
  detection into a merge gate.
- A **scheduled weekly security sweep** workflow: `go-govulncheck` ×2 +
  `cargo audit` + `npm audit` on main, independent of PR traffic — catches
  advisories against deps we *haven't* recently touched. (Until this lands,
  govulncheck runs nowhere automatically — §1.4.)
- (Open question §7.2) an opt-in `e2e-full` job, label-triggered on T2 PRs,
  so the devnet gate can run in CI rather than only on a maintainer machine.

## 5. Runbooks

### 5.1 Security patch (T0) — alert to merged

1. Alert arrives (Dependabot alert / scheduled sweep failure / cosmos
   security advisory channel).
2. Branch; apply the **minimal** bump (`go get module@fixed`,
   `cargo update -p crate`, `npm update pkg` — never the blanket target).
   If no fixed release exists yet: carry a pin/patch per §5.3.
3. `make deps-verify`; T2/T3 packages additionally get their tier's gate.
4. PR labelled `security`; merge on green. The CI breaking-check and
   review conventions apply as normal — speed comes from the diff being
   minimal, not from skipping gates.

### 5.2 Cosmos-sdk / consensus-stack upgrade (T2)

1. Read the release/upgrade notes **and the changelog of every
   cosmossdk.io/* submodule that moves with it**; note store/genesis-format
   changes.
2. Branch. Bump both Go modules **together** (chain + guardian pin the same
   sdk); revisit the `replace` block — specifically whether the AutoCLI-fix
   pin and the client/v2 pseudo-version are still needed at the new version
   (each carried pin is re-justified or dropped on every T2 upgrade, §5.3).
3. `go work sync`; `make proto-gen` if buf deps moved with the sdk.
4. `make deps-verify`, then the devnet gate: fresh `make dev-reset`,
   `make e2e`, `make e2e-scenarios`, plus a genesis round-trip
   (`timeflared export` → import on a clean home) to catch state-format
   drift.
5. Post-launch this runbook grows the `make devnet-upgrade-test` governance
   rehearsal and coordination with `ENFORCE_PROTO_BREAKING`; pre-launch,
   dev-reset absorbs everything.

### 5.3 Carrying local patches (pins, forks, overrides)

Sometimes the fix must be carried before upstream ships it. Mechanisms, per
ecosystem: Go `replace` (already in use ×4), Cargo `[patch.crates-io]`, npm
`overrides` (or `patch-package` for source patches). Rules that keep this
from becoming sediment:

1. Every carried patch has a **comment with the reason** (the existing four
   already do — keep the standard) **and a tracking reference** (upstream
   issue/PR or advisory ID).
2. Every carried patch has an **exit condition** ("until gin ≥1.9.1 is in
   the dependency graph naturally", "until sdk v0.54"). T2 upgrades re-audit
   the whole block (§5.2.2).
3. A `make deps-pins` target prints the current carried set with reasons —
   the review surface for "what are we still dragging along?".
4. Forking a dep (rather than pinning a version) requires a plan-doc note —
   forks are where carried patches go to be forgotten.

## 6. Surface area

- **Makefile**: replace `update-deps`/`deps-update` with the tiered targets
  (`deps-update-routine`, `deps-update-proto`, `deps-verify`, `deps-pins`);
  wire `cargo audit`/`npm audit` installs into `make doctor`.
- **`.github/dependabot.yml`**: extend the cosmos group (iavl/ics23/gogo),
  add the T1 batch groups and the rust-crypto group; align schedules.
- **CI (`ci.yml`)**: path-filter fixes for lockfiles; audit steps; the
  scheduled security-sweep workflow; optionally the label-gated e2e job.
- **Docs**: this plan's §3/§5 tables lifted into a short
  `docs/guides/DEPENDENCIES.md` once refined (runbooks belong where an
  on-call human looks, not only in planning).
- **Not touched**: the dependency versions themselves — this plan builds the
  machinery; the first tier-shaped update runs through it as its own
  validation. Known backlog awaiting it: the Rust crypto crates are several
  majors behind current (`rand` 0.7, `rand_chacha` 0.2, `x25519-dalek` 1.1),
  so the first T3 exercise of §5's gates is effectively pre-booked.

## 7. Open questions — all resolved (July 2026)

1. ~~Renovate vs extending Dependabot~~ **Resolved: keep Dependabot.**
   Extend its grouping per §3; the two roots it cannot cover (`buf.lock`,
   `go.work.sum`) are handled by the §4 make targets and the workspace-drift
   check in `deps-verify`. Revisit Renovate only if PR noise grows.
2. ~~e2e in CI for T2 PRs~~ **Resolved: add the CI job.** A label-triggered
   `e2e-full` job (spins the devnet in the runner, ~10 min) so the T2 gate
   is enforced rather than remembered; the local runbook remains valid for
   iteration.
3. ~~Cadence~~ **Resolved: weekly across all ecosystems** while the
   dependency surface stays small; revisit if T1 batches become noisy.
4. ~~`cargo deny` and Actions SHA-pinning~~ **Resolved: `cargo audit` lands
   now (§4); `cargo deny` (licence/ban policy) and pinning GitHub Actions by
   SHA are deferred to a launch-hardening pass** — record them there when
   that plan exists.
5. ~~Actions-bump PRs run zero CI~~ **Resolved: lint them.** Dependabot's
   github-actions PRs touch only `.github/workflows/**`, which no `changes`
   filter matches — today they merge without a single job running (§1.3).
   Add a `ci` filter and a minimal actionlint job (§4); heavier gating isn't
   warranted for version-pin bumps, and SHA-pinning stays deferred per §7.4.
