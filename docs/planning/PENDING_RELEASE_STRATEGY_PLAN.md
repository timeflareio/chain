# Release Strategy — `timeflareio/chain` — Plan

| | |
|---|---|
| **Priority** | P2 |
| **Status** | refining |
| **Created** | August 2026 |
| **Updated** | 11 November 2026 |
| **Protocol surface** | none — it versions the artefacts that carry the surface, and changes none of them |
| **Repositories** | chain |
| **Depends on** | — |
| **Related** | [PENDING_RELEASE_ENGINEERING_PLAN.md](PENDING_RELEASE_ENGINEERING_PLAN.md); `timeflareio/guardian` — `PENDING_RELEASE_STRATEGY_PLAN.md` |
| **Open questions** | 6 (§6) |

*Settles how this repository versions and releases. It is the hardest of the
per-repository release plans because it publishes the most: two Go modules on
separate tag namespaces, node binaries, a container image, and the
protocol-surface artefacts every other component pins.*

**Status notes**: §6 is unresolved; this plan is not executable until those
questions are ruled and folded into the body.
**Why this priority**: the `v0.0.x` tags cut during the migration serve for now,
but this must land before a testnet, because a chain release is the thing
validators run.
**Origin**: multi-repo migration plan (monorepo history), which delegated
release strategy to a per-repo plan.
**Components**: `.github/workflows/release.yml`, `Makefile`,
`COMPATIBILITY.md`, `PROTOCOL_CHANGE.md`, `docs/KNOWN_RISKS.md`, and the consumer-facing
contract with the guardian, the TypeScript SDK and the mobile client.

**Already landed**: the release mechanics. `.github/workflows/release.yml`
(migration phase 2b, commit `4f72605`, 3 August 2026) triggers on a `v*` tag
only, enforces the 0.x version policy, runs `make verify` and `make test` on the
tagged commit, builds `timeflared` pure Go with `-trimpath` for four platforms,
and publishes the binaries, a checksums file, the proto and chain-vector
tarballs and the GHCR image. The devnet, the e2e suites and `make e2e-full`
arrived in the same phase. Releases `v0.0.1`–`v0.0.5` and
`x/secrets/types/v0.0.1`–`v0.0.4` were cut this way.

## 1. What this repository publishes

More than any other component, and on **two tag namespaces**:

| Artefact | Tag | Consumed by |
|---|---|---|
| Root Go module `…/chain` | `vX.Y.Z` | nothing today — it is the node, not a library |
| `x/secrets/types` module | `x/secrets/types/vX.Y.Z` | the guardian, and future Go integrators |
| `timeflared` binaries | `vX.Y.Z` | validators, node operators |
| `timeflared` container image (GHCR) | `vX.Y.Z` | the compose devnet, operators |
| proto tarball | `vX.Y.Z` | no consumer today — it carries `proto/` alone, and the TypeScript SDK's `proto-sync` regenerates from the tagged source archive instead; published for integrators generating their own bindings |
| chain-semantics vector tarball + manifest | `vX.Y.Z` | the SDK and the mobile client — the guardian's two files travel inside the `x/secrets/types` module it already requires |

The two namespaces are forced, not chosen: Go requires a nested module's tags to
carry its subdirectory prefix. That is the cosmos convention and it is why an
integrator can pin the wire contract at `x/secrets/types/v1.4.0` without
inheriting the node.

**The two namespaces move independently.** A keeper-only or dependency-only
change moves the chain tag without moving the wire contract, and a types tag is
cut only when the types module actually changed; `PROTOCOL_CHANGE.md` states the
rule and the release workflow handles the chain tag alone. The numbers therefore
diverge, and `COMPATIBILITY.md` is the only place the pairing is recorded.

## 2. What a version number means

Three classes of change with very different blast radii:

- **Node-internal** — keeper refactor, performance, logging. Affects operators
  only, who upgrade at their convenience.
- **Wire-contract change** — proto, `x/secrets/types`, spec. Every consumer must
  roll. This is the release train (`PROTOCOL_CHANGE.md`).
- **Consensus-breaking** — state machine behaviour changes such that nodes on
  different versions disagree. Requires a coordinated upgrade at a height, not
  merely a new release. `app/upgrades/` exists for exactly this.

The third class is the one semver alone cannot express, because "breaking" for a
library means "recompile" whereas for a chain it means "the network halts unless
everyone upgrades together at an agreed height". §6 asks how to signal it.

## 3. Release mechanics

- **Trigger**: pushing a `vX.Y.Z` tag. Nothing releases on merge.
- **Preconditions in the workflow**: the 0.x version guard, then `make verify`
  and `make test` green on the tagged commit. The e2e suites are not part of the
  gate; whether they should be is §6.
- **Reproducible binaries.** Validators must be able to verify that a published
  binary matches its source. The build is pure Go with no cgo, `-trimpath` and a
  toolchain taken from `go.mod`, so this is achievable, but it needs proving
  rather than assuming — the proof is
  [PENDING_RELEASE_ENGINEERING_PLAN.md](PENDING_RELEASE_ENGINEERING_PLAN.md)
  Phase 2.
- **Nested module tag** is pushed separately, and only when the types module
  actually changed (§1).

## 4. Independence and the compatibility matrix

`COMPATIBILITY.md` lives here and is the join table: one row per chain release
naming the compatible guardian, SDK, crypto and mobile versions, plus both
corpus versions. This is the answer to "which guardian do I run against testnet
vX?" and the input to the integration pipeline's pin set.

Note the asymmetry with crypto: that repository versions **independently** and a
release there neither waits for nor implies a release here. This repository is
the opposite — a wire-contract change here obliges every consumer to move, so
its releases are the clock the others follow.

## 5. Carried dependency pins

Both modules here carry an identical `replace` block, and the guardian must
mirror it exactly or the two daemons build against different cosmos-sdk
versions. Go only honours `replace` in the main module being built, so the
mirroring cannot be inherited — it must be checked. `make verify-pins` in the
guardian repository fetches this repository's `go.mod` at the pinned types tag
and diffs the blocks. A release here that changes the pin block therefore
obliges a guardian release, and the "Carried pins" comment heading each block
(in `go.mod`, `x/secrets/types/go.mod` and the guardian's `go.mod`) is what
names the obligation to whoever edits one.

## 6. Open questions

1. **How is a consensus-breaking release signalled?** A major bump is necessary
   but insufficient — operators need to know the upgrade height and that
   coordination is required, which a version string cannot carry.
   *Recommendation*: reserve MAJOR for consensus-breaking, and require every
   such release to ship an `app/upgrades/vN` handler plus a release note naming
   the upgrade name that governance will submit. The version signals "you cannot
   upgrade casually"; the handler and note carry the how.

2. **When does `v0.x` become `v1.0.0`?** For a chain this is not a library
   maturity statement but a network one. The release workflow already refuses
   any tag outside `v0.*` and points here for the decision. *Recommendation*:
   `v1.0.0` at mainnet genesis and not before; testnets run `v0.x` however long
   they need. The `ENFORCE_PROTO_BREAKING` launch switch in CI is a related
   decision expressed in a different place; when it flips is disputed between
   this recommendation, the `ci.yml` comment and
   [PENDING_RELEASE_ENGINEERING_PLAN.md](PENDING_RELEASE_ENGINEERING_PLAN.md)
   (its open question 3), and the two should be ruled together.

3. **Where is release signing owned, and how do validators verify a binary?**
   Checksums alone protect against corruption, not substitution, and a validator
   running a substituted binary is a catastrophic failure rather than an
   inconvenience. This plan, `timeflareio/guardian` —
   `PENDING_RELEASE_STRATEGY_PLAN.md` and `timeflareio/crypto` —
   `docs/planning/done/DONE_RELEASE_STRATEGY_PLAN.md` each defer signing to a
   project-wide supply-chain plan, and no such plan exists in any repository.
   The only place the mechanism is discussed is
   [PENDING_RELEASE_ENGINEERING_PLAN.md](PENDING_RELEASE_ENGINEERING_PLAN.md)
   (its open question 1). *Recommendation*: name one owner — the release
   engineering plan here, extended to state the requirement for every repository
   that ships a binary — and settle it before any public network.

4. **Does a release gate on the e2e suites?** `docs/KNOWN_RISKS.md` (Gaps With
   No Owner §2, "A release can publish from a commit whose E2E is unknown")
   records that a tag pushed while `main`'s
   e2e is red or still running publishes anyway, and that this is worked around
   by judgement. It lists three options: require green `main` in the
   `PROTOCOL_CHANGE.md` runbook; add e2e to the release gate and accept its
   duration on every release; or state plainly that a release asserts
   `verify && test` and nothing more. A fourth is the multi-validator
   `make e2e-full`, which also checks app-hash agreement. `timeflareio/guardian`
   — `PENDING_RELEASE_STRATEGY_PLAN.md` (its open question 4) asks the mirror
   question for a guardian release. *Recommendation*: gate in the workflow —
   a chain release that cannot produce blocks is worthless — and choose between
   the lifecycle suites and `e2e-full` on measured cost.

5. **Should anything mechanise the `COMPATIBILITY.md` row?** Independent
   namespaces make that file the only record of the pairing, and a stale or
   missing row is silently wrong. A row is a tested claim added only after the
   consumers have moved and the e2e suites pass against the pinned artefacts, so
   it cannot exist when the chain tag is pushed and the workflow cannot require
   it for the release being published. *Recommendation*: have the release
   workflow refuse to publish while the *previous* chain release has no row,
   which enforces that each train finishes before the next starts.

6. **Is the proto tarball kept?** It has no consumer: it carries `proto/` alone,
   and buf needs `buf.yaml` and `buf.lock` from the repository root to generate,
   so the SDK regenerates from the tagged source archive. The choice is between
   making the tarball self-sufficient and dropping it.
   *Recommendation*: make it self-sufficient by adding the two buf files — the
   definitions are the protocol surface, and an integrator generating their own
   bindings should be able to pin them without cloning the node.

## 7. What this plan does not solve

- **Chain upgrade rehearsal** — how an upgrade is tested before a network runs
  it — is `docs/upgrades.md` and the upgrades runbook, not release mechanics.
- **Registry publication for the protos.** Whether they should also be pushed
  to the Buf Schema Registry is a separate question with its own trade-offs.
