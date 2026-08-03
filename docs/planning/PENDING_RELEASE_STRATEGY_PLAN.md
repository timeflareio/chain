# Release Strategy — `timeflareio/chain` — Plan

*Settles how this repository versions and releases. It is the hardest of the
five because it publishes the most: two Go modules on separate tag namespaces,
node binaries, a container image, and the protocol-surface artefacts every other
component pins. Deliberately a draft — authored now so the questions are
visible, refined once the repository can be exercised end to end (phase 2b
brings the devnet and e2e suites).*

> **Status: refining** — created August 2026 with the phase-2a lift. §6 is
> unresolved; this plan is not executable until those questions are ruled and
> folded into the body.
> **Priority**: P2 — the migration's per-phase `v0.0.x` tags serve for now, but
> this must land before a testnet, because a chain release is the thing
> validators run.
> **Components**: `.github/workflows/` (a new `release.yml`), `Makefile`,
> `COMPATIBILITY.md`, `PROTOCOL_CHANGE.md`, and the consumer-facing contract
> with the guardian, the TypeScript SDK and the mobile client.

## 1. What this repository publishes

More than any other component, and on **two tag namespaces**:

| Artefact | Tag | Consumed by |
|---|---|---|
| Root Go module `…/chain` | `vX.Y.Z` | nothing today — it is the node, not a library |
| `x/secrets/types` module | `x/secrets/types/vX.Y.Z` | the guardian, and future Go integrators |
| `timeflared` binaries | `vX.Y.Z` | validators, node operators |
| `timeflared` container image (GHCR) | `vX.Y.Z` | the compose devnet, operators |
| proto tarball | `vX.Y.Z` | the TypeScript SDK (`proto-sync`) |
| chain-semantics vector tarball + manifest | `vX.Y.Z` | the SDK and the guardian |

The two namespaces are forced, not chosen: Go requires a nested module's tags to
carry its subdirectory prefix. That is the cosmos convention and it is why an
integrator can pin the wire contract at `x/secrets/types/v1.4.0` without
inheriting the node.

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

## 3. Release mechanics (proposed)

- **Trigger**: pushing a `vX.Y.Z` tag. Nothing releases on merge.
- **Preconditions in the workflow**: `make verify` and `make test` green on the
  tagged commit, plus — once phase 2b lands — `make e2e-full` against the
  multi-validator compose stack, since a chain release that cannot produce
  blocks is worthless.
- **Reproducible binaries.** Validators must be able to verify that a published
  binary matches its source. The build is pure Go with no cgo, so this is
  achievable with `-trimpath` and pinned toolchain, but it needs proving rather
  than assuming.
- **Nested module tag** is pushed alongside, and only when the types module
  actually changed (§6).

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
obliges a guardian release, and the pin-set comment (`# pin-set: YYYY-MM-x`) is
what makes the mismatch legible.

## 6. Open questions

1. **How is a consensus-breaking release signalled?** A major bump is necessary
   but insufficient — operators need to know the upgrade height and that
   coordination is required, which a version string cannot carry.
   *Recommendation*: reserve MAJOR for consensus-breaking, and require every
   such release to ship an `app/upgrades/vN` handler plus a release note naming
   the upgrade name that governance will submit. The version signals "you cannot
   upgrade casually"; the handler and note carry the how.

2. **Do the two tag namespaces move together or independently?**
   Independently is more honest (a keeper-only change should not bump the wire
   contract, and a types-only change should not force operators to think about a
   node upgrade), but it makes `COMPATIBILITY.md` the only place the pairing is
   recorded, and a stale row becomes silently wrong. *Recommendation*:
   independent, with the release workflow refusing to publish a chain release
   whose `COMPATIBILITY.md` row is missing — mechanising the one thing that
   makes independence safe.

3. **When does `v0.x` become `v1.0.0`?** For a chain this is not a library
   maturity statement but a network one. *Recommendation*: `v1.0.0` at mainnet
   genesis and not before; testnets run `v0.x` however long they need. Note the
   `ENFORCE_PROTO_BREAKING` launch switch in CI is the same decision expressed
   in a different place, and the two must flip together.

4. **Are release binaries signed, and how do validators verify them?** Checksums
   alone protect against corruption, not substitution, and a validator running a
   substituted binary is a catastrophic failure rather than an inconvenience.
   *Recommendation*: defer the mechanism but not the requirement — this must be
   settled before any public network, and it belongs to a project-wide
   supply-chain plan rather than this one, since the guardian has the identical
   problem.

## 7. What this plan does not solve

- **Chain upgrade rehearsal** — how an upgrade is tested before a network runs
  it — is `docs/upgrades.md` and the upgrades runbook, not release mechanics.
- **The e2e gate** cannot be wired until phase 2b brings the devnet and compose
  stack. Until then §3's preconditions are `verify` + `test` only, which is
  weaker than a chain release deserves and should not be mistaken for
  sufficient.
- **Registry publication for the proto tarball.** The SDK consumes it as a
  release asset; whether protos should also be pushed to the Buf Schema Registry
  is a separate question with its own trade-offs.
