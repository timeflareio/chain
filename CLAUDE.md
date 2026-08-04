# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository.

**It is not the whole picture.** The project-wide rules — the working agreement,
the writing conventions (British English, VEIL is a token, never name the owner),
the plan-first mandate, specification authority, and how a change crosses a
repository boundary — are stated once in the workspace root `CLAUDE.md`, at
`~/dev/timeflareio/CLAUDE.md`, which loads alongside this file. Read it if you
are in a checkout that cannot see it.

## Project Overview

**timeflare chain** is a Cosmos SDK-based blockchain implementing a time-locked
secret reveal protocol, built around one module:

- **Secrets Module** (`x/secrets/`): handles both guardian infrastructure
  (registration, staking, slashing) and secret lifecycle management
  (publication, progressive time-locks, protocol-controlled guardian selection,
  and reconstruction using enhanced Shamir Secret Sharing).

This repository owns **the protocol surface**: `proto/`, `docs/spec.md`, the
`x/secrets/types` wire contract, and the chain-semantics vector corpus. Every
other timeflare component consumes that surface at a pinned version.

## Economic Model

**Fixed supply, deflationary**: 1 billion VEIL, no inflation ever; fee-driven
rewards with permanent token burning; no treasury; dual staking (validators for
consensus, guardians for secrets).

Fee distribution per transaction: **90%** to validators, **10%** burned.
Guardians earn directly from secret creators as a service-based reward.

## Essential Commands

- `make test` — all unit tests, including the nested `x/secrets/types` module
- `make verify` — read-only quality gate: gofmt, golangci-lint, go vet,
  `verify-boundaries`, `verify-choke-points`
- `make format` — apply formatting and lint fixes
- `make proto-gen` — regenerate Go protobufs from `proto/`
- `make build` — regenerate protobufs and install `timeflared`
- `make doctor` — verify the local toolchain (go, buf, jq)
- `make help` — grouped target list

`go test ./...` from the root does **not** descend into `x/secrets/types` — use
`make test`, which iterates it (`GO_SUBMODULE_DIRS`).

Devnet, container and chain-upgrade targets arrive with phase 2b of the
multi-repo migration. Until then `devnet/` is present as a test fixture (unit
tests assert the genesis scripts carry the rebate pool address) but is not yet
wired to make targets.

## Architecture

### Go module layout

Two modules live here, with a strict one-way dependency flow:

- **Root module** (`github.com/timeflareio/chain`) — the chain: `app/`,
  `cmd/timeflared/`, `x/secrets/` (keeper, cli, module wiring), `docs/` (the
  embedded API-docs package).
- **`x/secrets/types`** (`github.com/timeflareio/chain/x/secrets/types`) —
  nested leaf module following the cosmos `x/` submodule convention. This is
  **the wire contract**: generated proto types, constants, errors, message
  types + `ValidateBasic`, economics pricing core, detection hints. It must
  never import chain internals, and never import `timeflareio/crypto` — the
  wire contract stays independently consumable so an integrator pinning it
  inherits no crypto dependency. `ValidateBasic` therefore stays a purely
  structural check; anything needing cryptographic computation is keeper work.

Cryptographic primitives come from **`github.com/timeflareio/crypto`** at a
pinned tag, imported as `github.com/timeflareio/crypto/go`. Nothing here builds
them, and a byte-level change to them is a protocol change owned by that
repository.

The boundary is enforced mechanically by `make verify-boundaries`, which now
checks the one edge still internal to this repository (types must not reach into
chain internals or into crypto). The edges that left with their components —
guardian must not import chain internals, crypto must depend on nothing of ours
— are enforced in those repositories.

`make verify-choke-points` enforces that guardian records are only ever written
through `keeper.SetGuardian`, which maintains the eligibility index. A writer
that goes straight to `Guardians.Set(` silently drifts that index.

### CLI/Query and CLI/Tx parity

- **The gRPC services are the source of truth; the CLI must cover them 1:1.**
- **Enforced by construction**: both query and tx commands are generated from
  the service descriptors via AutoCLI (`x/secrets/module/autocli.go`). Adding an
  RPC to `query.proto` or `tx.proto` requires adding its `RpcCommandOptions`
  entry there. Never hand-write query commands in `client/cli`.
- **Tx exceptions are explicit**: a Msg RPC may opt out only via `Skip: true`,
  backed by a hand-written command of the same kebab-case name in
  `x/secrets/client/cli/tx.go`. Two reasons qualify: genuine client-side logic
  (`user-request-guardians`, `user-distribute-shares`) and Coin-bearing messages
  (`guardian-register`, `guardian-update`). `TestTxCommandParity` enforces both
  directions; `TestRootCmdSmoke` catches descriptors the flag builder cannot
  resolve.

## Development Workflow

### Adding new features

1. **Update protobuf definitions** in `proto/timeflare/secrets/v1/`
2. **Implement keeper methods** in `x/secrets/keeper/` following existing
   patterns
3. **Add comprehensive tests** and update `docs/guides/TESTING_COMMANDS.md`
4. **Update `docs/spec.md`** — see below

**Protocol-surface changes land as one unit**: `proto/` + the
`x/secrets/types` module (regenerated code, message helpers, constants) +
`docs/spec.md` together in the same PR — the types module is the shared wire
contract for the guardian and future Go clients, so its content changes are
protocol changes.

### 🚨 CRITICAL REQUIREMENT: Documentation Synchronisation

**ALWAYS update `docs/spec.md` when making code changes:**

- **New messages/operations** → update "Core Operations" with the detailed flow
- **Configuration changes** → update "Configuration Parameters" tables
- **Protocol modifications** → update the relevant architecture sections
- **Security enhancements** → update "Security Model"
- **Economic changes** → update "Economic Model"

**Pattern**: every code change MUST have a corresponding spec.md update in the
same session. **Rationale**: spec.md is the authoritative protocol
documentation and must stay synchronised with implementation.

## 📋 Specification Authority

**`docs/spec.md` is owned here**, and it is the protocol authority for every
timeflare repository — they link it at a pinned tag and never copy it. That
places the duty on this repository: a spec change lands with the code it
describes, in the same PR, because a spec that trails the code misleads every
consumer at once.

The rules for consulting and disputing it are project-wide — see "Specification
authority" in the workspace root `CLAUDE.md`.

## 🚨 Wire-field renames and removals

**The component that gets missed lives in another repository now.** The
`guardian` daemon is tightly coupled to the protocol message shapes but consumes
them as a pinned Go module, so a field removal that compiles cleanly here and in
the TypeScript SDK can still leave the guardian's tests, mocks and service
implementations broken. That happened once inside the monorepo (the December
2024 `shareIndex` removal); across repositories it is the *default* failure mode
rather than an accident, because nothing in this repository's CI compiles the
guardian at all.

Before removing or renaming any wire field or shared symbol: grep the field name
across every consuming repository, and confirm which components are clear — not
just which ones you changed. A rename is not done when this repository is green;
it is done when the release train has landed everywhere.

## Specific to this repository

- **NEVER change core proto files or models without explicit confirmation.**
  `proto/` and the `x/secrets/types` wire contract are the surface every other
  component pins; see `PROTOCOL_CHANGE.md` before touching either.
- Plans live in `docs/planning/`, per its `README.md`. The devnet is
  machine-global, so check for a running `timeflared` before driving it — the
  hazard is described in that README.
