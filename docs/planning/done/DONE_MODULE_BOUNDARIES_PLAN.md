# Module Boundaries — Shared Library Modules & a Client-Shaped Guardian

*Restructures the Go module layout so the guardian depends on what it actually
uses — the wire contract and the crypto primitives — instead of the whole
chain. Four modules, one repo, strict one-way dependency flow, mechanically
enforced. This is the "share libs, and that's it" boundary made literal, and
the preparation that makes any future repo decomposition a directory move
rather than a surgery.*

> **Status: DONE — implemented July 2026** (Phases 1–3; Phase 4 stays
> deferred by design until the first out-of-workspace consumer appears).
> Originally proposed July 2026. Motivated by the repo-shape
> discussion recorded in
> [DONE_CONTAINERISATION_PLAN.md](DONE_CONTAINERISATION_PLAN.md) §6:
> the monorepo stays (same-PR atomicity for protocol changes is load-bearing
> pre-launch), but the module graph inside it should reflect the real
> boundaries. Measured starting point: the guardian imports **exactly two**
> packages from the chain module — `crypto/` and `x/secrets/types` — yet its
> `go.mod` depends on the entire chain (app wiring, keeper, ibc-go) via
> `replace => ../`. The code boundary is already clean; the module boundary
> is what misrepresents it. (Re-verified against the repo, 18 July 2026 —
> the import scan result is unchanged.)
>
> **Open questions 1–3 ruled (owner ruling, July 2026)**: the types module
> is modularised **in place** as a nested module at `x/secrets/types` —
> the cosmos `x/` submodule convention (cf. `cosmossdk.io/x/tx`), revised
> from the earlier `api/` ruling (§7.1); `ValidateBasic` and message
> helpers stay with the types (§7.2); `economics.go` stays in the types
> module (§7.3). Only §7.4 (timing) remains open — and was answered in
> practice: implemented July 2026, ahead of `tools/econsim/`.

## Contents

1. [Current state](#1-current-state)
2. [Target shape](#2-target-shape)
3. [The dependency rule and its enforcement](#3-the-dependency-rule-and-its-enforcement)
4. [Migration phases](#4-migration-phases)
5. [Tagging & consumption lifecycle](#5-tagging--consumption-lifecycle)
6. [Coordination with other plans](#6-coordination-with-other-plans)
7. [Open questions](#7-open-questions)

---

## 1. Current state

Two Go modules, one real dependency edge, badly routed:

- **Root module** (`github.com/leedavis81/timeflare`): the chain — `app/`,
  `x/secrets/` (keeper, cli, module wiring), `cmd/timeflared` — plus two
  packages that are not chain-specific at all: `crypto/` (pure-Go HMAC +
  encryption, vector-tested) and `x/secrets/types` (generated proto types,
  gRPC clients, constants, errors, message types, codec registration).
- **Guardian module** (`…/guardian`): imports only
  `…/crypto` and `…/x/secrets/types` from the root — verified by import
  scan, July 2026 — but must `require` the whole root module
  (`replace => ../`) to get them.

Consequences of the misrouted edge:

1. The guardian's module graph carries the app stack (ibc-go, app deps,
   keeper machinery) it never compiles against — slower resolution, noisier
   `go.sum`, and version coupling to chain-only dependencies.
2. Nothing prevents a future `import ".../x/secrets/keeper"` in the guardian
   — the boundary exists by convention only.
3. The guardian's Docker build context must be the repo root
   (containerisation plan §3), and any future repo split would first have to
   invent the very modules this plan extracts.

Also relevant: `go.work` already exists (the workspace mechanism is in
place), and `x/secrets/types` contains one chain-side stowaway —
`expected_keepers.go` (keeper interface declarations) — that doesn't belong
in a wire-contract module. Everything else in the package is genuine
wire-contract material and stays in the extracted module — including two
additions since this plan was first drafted: the parameterised fee-split
core in `economics.go` (`SplitFeeAmountWith`, added for
[PENDING_ECONOMIC_SIMULATION_PLAN.md](../PENDING_ECONOMIC_SIMULATION_PLAN.md))
and the detection-hint helpers (`detection_hint.go`). (Import scan
re-verified 18 July 2026: the types package imports only cosmos-sdk types,
gogoproto/grpc libraries, and itself — no root-module packages, in tests
included.)

## 2. Target shape

Four modules, one repo, tied together by `go.work` for development:

```
timeflare/
├── go.work                    # dev-time workspace (exists today)
├── crypto/                    # NEW MODULE …/timeflare/crypto
│                              # hmac.go, encryption.go, utils.go,
│                              # crypto_test.go, vectors_test.go
│                              # (vectors_test.go asserts testdata/vectors/)
├── (root module)              # the chain, path unchanged:
│   ├── app/  cmd/timeflared/
│   └── x/secrets/             # keeper/ (gains expected_keepers.go),
│       │                      # client/cli/, module/
│       └── types/             # NEW NESTED MODULE …/timeflare/x/secrets/types
│                              # (in place — cosmos x/ submodule convention):
│                              # generated proto (tx/query/types) + constants,
│                              # errors, message types + ValidateBasic, codec
│                              # RegisterInterfaces, genesis types, economics
│                              # (pricing core — §7.3), detection-hint helpers
├── guardian/                  # module — imports x/secrets/types + crypto ONLY
├── rust/  typescript-sdk/     # client implementations (unchanged)
├── testdata/vectors/          # shared corpus (unchanged, still repo-root)
└── devnet/  make/  docs/      # shared infra (unchanged)
```

The in-place nested module keeps every existing import path valid — there
is **no import rewrite, no `go_package` change, no buf pipeline change,
and no CI path-filter change** (the `x/**` and `crypto/**` filters already
cover both new module roots). The cost is a module whose directory sits
inside the chain's source tree; the cosmos-sdk itself uses exactly this
shape for its `x/` submodules.

What deliberately does **not** change:

- The chain stays the root module at its current import path. Moving it
  under `chain/` would touch every import in the largest module for purely
  cosmetic symmetry — deferred unless/until an actual repo split happens
  (containerisation plan §6).
- `testdata/vectors/`, the devnet scripts, the make infrastructure, and the
  docs remain repo-level: they are exactly the things the monorepo exists to
  keep atomic.
- The TS SDK generates its own proto bindings; it is unaffected by the Go
  module layout.

## 3. The dependency rule and its enforcement

```
        ┌──────────────────┐   ┌──────────┐
        │ x/secrets/types  │   │  crypto  │    (leaves — depend only on
        └────▲──────▲──────┘   └─▲────▲───┘     cosmos-sdk types / x/crypto)
             │      │            │    │
      ┌──────┘      └──────┐     │    │
      │                    │     │    │
┌─────┴────┐          ┌────┴─────┴┐   │
│  chain   │          │ guardian  │───┘
└──────────┘          └───────────┘
      guardian → chain : FORBIDDEN
```

- `x/secrets/types` may depend on cosmos-sdk **types** (the generated code
  embeds `sdk.Coin` etc.) but never on keeper/app packages — **nor on the
  sibling leaf `crypto/`**. That edge would be acyclic and is forbidden for
  coupling reasons rather than structural ones: the two leaves are consumed and
  tagged independently (Phase 4), so `types → crypto` would make every
  `crypto/` release force a `types` release and hand every external client
  pinning the wire contract a transitive dependency on the
  `testdata/vectors/`-pinned crypto — plus it keeps `ValidateBasic` a purely
  structural check, with cryptographic computation staying keeper work.
- `crypto` depends on the stdlib and `golang.org/x/crypto` only.
- `guardian` depends on `x/secrets/types`, `crypto`, and cosmos-sdk client
  libraries (keyring, tx factory) — never the chain module.

**Enforcement is mechanical, not conventional** — a `make verify-boundaries`
target wired into `verify` (and thus CI):

```make
verify-boundaries:
	@# guardian may import the types module but no other chain package
	@bad=$$(cd guardian && go list -deps ./... | \
	  grep -E 'timeflare/(x/|app|cmd)' | \
	  grep -v 'timeflare/x/secrets/types$$' || true); \
	if [ -n "$$bad" ]; then echo "❌ guardian imports chain internals:"; \
	  echo "$$bad"; exit 1; fi
	@# the types module must not depend on chain internals or guardian
	@bad=$$(cd x/secrets/types && go list -deps ./... | \
	  grep -E 'timeflare/(x/secrets/(keeper|client|module)|app|cmd|guardian)' || true); \
	  …
```

(Equivalent `depguard` rules in `.golangci.yml` are an acceptable
alternative; the `go list` form has zero new dependencies. Either way the
check runs in the existing chain/guardian CI jobs.)

## 4. Migration phases

### Phase 1 — `crypto/` becomes a module (near-free)

The package already lives at the path its module name implies, so **no
import path changes anywhere**:

1. Add `crypto/go.mod` (module `…/timeflare/crypto`; deps: stdlib +
   `golang.org/x/crypto`). `vectors_test.go` moves with it and keeps
   asserting `../testdata/vectors/`.
2. Add to `go.work`; root and guardian modules `require` it (workspace
   resolves it locally; see §5 for the replace-directive lifecycle).
3. `go mod tidy` ×3; guardian's graph sheds nothing yet (it still requires
   the root for `x/secrets/types`) — this phase is scaffolding.

### Phase 2 — `x/secrets/types` becomes a nested module (the real move)

1. Move `expected_keepers.go` → `x/secrets/keeper/` (package `keeper`) —
   its interfaces are consumed by the keeper and the module wiring, both
   chain-side; references rewrite `types.AuthKeeper` → `keeper.AuthKeeper`.
2. Add `x/secrets/types/go.mod` (module `…/timeflare/x/secrets/types`) —
   the package leaves the root module in place, keeping every import path
   valid. The module mirrors the chain's carried-pin `replace` block
   (both modules must resolve the same cosmos-sdk).
3. The buf pipeline is untouched: `buf.gen.gogo.yaml`'s output path,
   the Makefile flatten step, and the protos' `go_package` all still
   point at `x/secrets/types`, which is still where the code lives.
4. Root and guardian `go.mod`s `require` the new module; guardian's
   drops the root-module requirement entirely — its `replace` directives
   now name `../x/secrets/types` and `../crypto` only.
5. Exit criteria: `go list -m all` in `guardian/` shows **no ibc-go and no
   root module**; full `make test` + devnet `make e2e` green (behavioural
   no-op — no source file changes its content or import path except the
   `expected_keepers.go` move).

### Phase 3 — enforcement + docs

1. `verify-boundaries` (or depguard) wired into `make verify` and CI.
2. Root-module test/quality/deps targets learn about the two new module
   roots: `go test ./...` from the repo root no longer descends into
   nested modules, so `make test`, `make verify`, `deps-verify`,
   `govulncheck`, and the workspace drift check must iterate
   `x/secrets/types` and `crypto` explicitly; Dependabot gains the two
   `gomod` directories.
3. CLAUDE.md architecture section updated. The CI path filters need **no
   changes**: `x/**` and `crypto/**` already cover both new module roots
   in `ci.yml`, and `mobile-client.yml`'s protocol-surface filter keys
   off `x/secrets/`, which remains the types' home.

### Phase 4 — (deferred) tags and context narrowing

When the first out-of-workspace consumer appears (repo split, published
guardian source, or external SDK-in-Go), tag `x/secrets/types/vX.Y.Z` and
`crypto/vX.Y.Z` (Go multi-module repo tagging), drop the guardian's replace
directives, and narrow `guardian/Dockerfile`'s build context to `guardian/`
(containerisation plan §3). Not before — see §5.

## 5. Tagging & consumption lifecycle

Three consumption modes, in order of appearance:

1. **Workspace (now)**: `go.work` resolves `x/secrets/types`/`crypto`
   locally for every developer build and test run. The
   `replace ../x/secrets/types` / `replace ../crypto` directives in
   consumer `go.mod`s exist **only** for builds that run outside the
   workspace (Docker builds, `go install` from a checkout) and are
   redundant within it.
2. **Tagged, same repo**: `x/secrets/types/v0.x.y` + `crypto/v0.x.y` tags
   let out-of-repo consumers (and narrowed Docker contexts) resolve real
   versions. Start this only when such a consumer exists — premature tagging
   is pure ceremony while everything lives in one PR stream.
3. **Split repos (only if ever)**: the modules move with their history; the
   tags keep resolving; the containerisation plan §6 heuristic (split along
   published-artefact boundaries, never atomic ones) governs *whether*.

## 6. Coordination with other plans

- **DONE_CONTAINERISATION_PLAN.md**: §3's root-context caveat for
  `guardian/Dockerfile` is resolved by Phase 4 here; its §6 names this plan
  as the "preparation, not separation" step.
- **automated/RELEASE_ENGINEERING_PLAN.md**: release tagging must account
  for multi-module tags (`vX.Y.Z` for the chain, `x/secrets/types/vX.Y.Z`,
  `crypto/vX.Y.Z`) once Phase 4 lands.
- **DONE_DEPENDENCY_MANAGEMENT_PLAN.md**: `deps-verify` and the workspace
  drift check gain two modules (`go build`/`govulncheck`/tidy across four
  module roots instead of two); Dependabot gets two more `gomod` entries.
- **[PENDING_ECONOMIC_SIMULATION_PLAN.md](../PENDING_ECONOMIC_SIMULATION_PLAN.md)**:
  its §2/§7 name this plan as the simulator's import-surface decision.
  The in-place ruling (§7.1) makes that surface permanent:
  `…/timeflare/x/secrets/types` is now a lean standalone module, so the
  simulator imports it directly — no rewrite ever needed, and the §7.3
  ruling keeps `economics.go` (the parameterised pricing core) inside it.
- **CLAUDE.md / DOCS**: architecture description updates in Phase 3.

## 7. Open questions

1. ~~**Module naming/location**~~ **Ruled (July 2026, revised): in-place
   nested module at `x/secrets/types`.** An initial ruling chose a new
   top-level `api/` module; the owner revised it the same month to keep
   the cosmos `x/` convention and modularise the package where it stands
   (the cosmos-sdk's own `x/` submodule pattern). This also collapses the
   migration cost: no import rewrite, no `go_package`/buf changes, no CI
   filter changes. Future modules' types would follow the same pattern
   (`x/<name>/types` as a nested module) if they ever exist.
2. ~~**Where message validation lives**~~ **Ruled (July 2026): as
   recommended** — `ValidateBasic` and the message helpers stay with the
   types module (clients get the same stateless validation the chain
   runs — the guardian already benefits).
3. ~~**Does `economics.go` belong in the types module?**~~ **Ruled
   (July 2026): yes.** Pricing (`P`, `B`) is protocol surface that clients
   legitimately compute (the guardian's candidacy maths, SDK fee
   previews), and the parameterised fee-split core (`SplitFeeAmountWith`)
   added for the economic simulation plan makes the shared types module
   its natural home. Anything keeper-private stays behind — starting with
   `expected_keepers.go`, which moves to `x/secrets/keeper/`.
4. **Timing** — i.e. *when to schedule the Phase 2 cut*: the repo-wide
   import rewrite has no hard external deadline and is the kind of churn
   best landed in a quiet window between feature branches. One real
   forcing input now exists: the economic simulation plan builds
   `tools/econsim/` on this plan's import surface (§6), so landing Phase 2
   before that work starts avoids a later rewrite in the simulator.
