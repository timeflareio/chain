# cosmos-sdk v0.53.4 → v0.55.0 (T0 security + consensus-stack upgrade) — Plan

*Close the transaction-processing security gap between the sdk version the
chain runs and the releases that fix it, by taking the consensus-stack
upgrade to v0.55.0 in one hop — sdk, cometbft, `cosmossdk.io/api`, Go
toolchain and ibc-go together. Pre-launch, the state break is absorbed by
`dev-reset`; there is no upgrade handler, store migration, or coordinated
upgrade to write.*

> **Status: done — 1 August 2026.** All rulings folded (target version,
> module dispositions). Executed on `worktree-sdk-v055-upgrade` and merged
> in PR [#133](https://github.com/leedavis81/timeflare/pull/133)
> (`cd394047`): all gates green locally (deps-verify,
> deps-verify-consensus, e2e-full with cross-validator app-hash agreement)
> and in CI, §6 verification items resolved, Dependabot #121/#122/#124
> closed as superseded.
>
> **Priority**: P0 — T0 security, and T0 heads the dependency queue
> (`docs/guides/DEPENDENCIES.md` §Ordering). The urgency is *sequencing and
> cost*, not live exposure: nothing is deployed, so nothing is presently
> exploitable, and both the security patches and the v0.55 line are **state
> breaking** upstream, which pre-launch `dev-reset` absorbs for free and a
> launched chain would need a coordinated governance upgrade to take. This is
> the cheapest moment this upgrade will ever be available.
>
> **Origin**: dependency triage, 31 July 2026. Dependabot raised
> [#121](https://github.com/leedavis81/timeflare/pull/121) and
> [#122](https://github.com/leedavis81/timeflare/pull/122) proposing sdk
> bumps per-module; both are structurally unmergeable (one module each — the
> three sdk-bearing modules must move together). Owner ruled 1 August 2026:
> supersede the interim v0.53.8 patch target and take v0.53.4 → v0.55.0
> directly, with the module dispositions in §3. All eleven v0.53.8 security
> fixes were verified present in v0.55.0 before the ruling (§2).
>
> **Components**:
> - `go.work` (Go directive 1.25.8 → 1.26.5) and the three sdk-bearing
>   modules: `go.mod`/`go.sum` (chain root), `guardian/`, `x/secrets/types/`
> - `go.work.sum` — workspace drift (`go work sync`)
> - `app/` — `app.go`, `app_config.go`, `export.go`, `ibc.go`, `upgrades.go`,
>   `app/upgrades/types.go` (wiring changes, import-path migrations, module
>   removals)
> - `cmd/timeflared/` — `root.go` (SIGN_MODE_TEXTUAL removal),
>   `testnet_multi_node.go` (`ExportGenesisFileWithTime` signature),
>   `commands.go`; `TestTxCommandParity` / `TestRootCmdSmoke`
> - `x/secrets/keeper/` — `cosmossdk.io/store` / `cosmossdk.io/log` import
>   moves
> - `proto/` + generated code — `cosmossdk.io/api` v0.9.2 → v1.1.0; buf deps
>   and descriptors re-verified via `make deps-update-proto`
> - `typescript-sdk/` + mobile vendored tarball — regenerate and repack
>   **only if** generated proto output changes (not expected: timeflare's own
>   protos do not change; verify, don't assume)
> - `.govulncheck-accepted` — re-audit the single carried entry
> - The carried `replace` block in `go.mod` (gin / goleveldb / nhooyr.io) —
>   re-justify per §Carrying local patches
> - `docs/guides/DEPENDENCIES.md` — record the bump
> - Possibly `docs/spec.md` §Fee Distribution and the e2e community-pool
>   assertion — see §6, V1
> - Dependabot PRs #121, #122, #124 — closed as superseded once the
>   execution PR is up
> - `crypto/` — **not touched**; it depends on stdlib + `golang.org/x/crypto`
>   only and carries no sdk edge

## 1. Why v0.55.0 in one hop

The alternative was the minimal T0 patch (v0.53.8, same-minor, lockfile-level
diff) with the consensus-stack move deferred. That sequencing pays the
migration cost twice: the project must reach the maintained sdk line before
launch regardless, and post-launch the same move needs a coordinated
governance upgrade. Taking v0.55.0 now lands the identical security content
(§2) plus the v0.54/v0.55 hardening, in the only window where the state break
is free. Upstream explicitly supports skipping v0.54: "one binary swap, one
upgrade handler, one halt height" — and pre-launch, even the upgrade handler
disappears (`dev-reset`, fresh genesis).

What the hop requires, against what the workspace runs today:

| | current | v0.55.0 requires |
|---|---|---|
| Go directive | 1.25.8 (`go.work`) | 1.26.5 (local toolchain is go1.26.5; CI uses `go-version-file` — no workflow edits) |
| cometbft | 0.38.21 | 0.40.0 (two minors) |
| `cosmossdk.io/api` | v0.9.2 | v1.1.0 (major) |
| `cosmossdk.io/core` | v0.11.3 | v1.1.0 |
| `cosmossdk.io/store` | v1.1.2 (separate module) | folded into the sdk repo (`store/v2` import path) |
| `cosmossdk.io/log` | v1.x | `cosmossdk.io/log/v2` |
| `cosmossdk.io/client/v2` | v2.0.0-beta.8 pin | v2.11.0 |
| ibc-go | v10.7.0 | v11.2.0 |

ibc-go compatibility was the one potential hard blocker: no ibc-go release is
CI-tested against sdk v0.55.0 (v11.2.0 pins v0.54.0, and ibc-go `main` sits on
v0.54.3 as of 1 August 2026). Verified empirically on 1 August 2026: a module
resolving ibc-go v11.2.0 + sdk v0.55.0 + cometbft v0.40.0 together, importing
the core, transfer and ICA-controller keepers, compiles cleanly. ibc-go v11
has also migrated off `x/params`, which unblocks that module's removal (§3).
The combination being upstream-untested is the plan's principal residual risk
(§5).

## 2. Security content — verified present in v0.55.0

All eleven fixes from the v0.53.7/v0.53.8 security releases are in v0.55.0.
The v0.55.0 changelog is incomplete (it omits the v0.53.7/v0.53.8 and
v0.54.3/v0.54.4 sections), so presence was verified fix-by-fix on 1 August
2026 rather than trusted:

**Untrusted-input hardening — reachable by any submitted transaction.** These
are the reason this is T0. Timeflare's ante chain sits directly downstream of
all of them, and it already carries a custom consensus-enforced minimum-gas
decorator, so the tx-decode path is load-bearing here. All appear verbatim in
the v0.55.0 changelog except where noted:

- bound multisig signature and pubkey indexing by slice lengths (#26515)
- reject txs with mismatched signer-info and signature counts (#26517)
- reject txs with extra `SignerInfos` in `SetPubKeyDecorator` (#26573)
- bound compact-bit-array index by elems length (#26509) — absent from the
  v0.55.0 changelog but verified by diffing
  `crypto/types/compact_bit_array.go` between the v0.53.8 and v0.55.0 tags:
  byte-identical apart from one comment
- avoid nil-pointer panic in `GetSigningTxData` (#26527), and again for
  multisig `ModeInfo` with a nil `Multi` or nil `Bitarray` (#26571)
- validate secp256k1 pubkey SEC1 tag byte (#26529)

**Fixes on paths this repository documents:**

- `store/cachemulti` `traceContext` isolation (#25841) — fixed **by removal**:
  the v0.54+ store restructure eliminates `traceContext` from branched stores
  entirely, so the racy structure no longer exists. The settlement design's
  per-secret cache contexts (`docs/spec.md` §Settlement failure handling) sit
  on the restructured store — see §6, V2
- `x/distribution`: fallback behaviour when withdrawing delegator rewards to
  blocked addresses (#26406), and errors for missing historical rewards
  (#26518) — see §6, V1 for the spec.md dust claim
- `x/staking`: handle redelegation when an unbonded source is removed (#26408)

**Additional hardening v0.53.8 does not have**: nested-`Any` recursion depth
cap lowered from 10,000 to 64 in unknown-field validation (#26587,
CPU-amplification DoS on tx decode), capped authz prune work per block
(#26588), and the feegrant pagination fix (#26596).

## 3. Module dispositions (owner rulings, 1 August 2026)

v0.54 consolidated most `cosmossdk.io/x/*` vanity modules into the sdk repo
and re-homed or dropped several modules. Timeflare's wiring is affected as
follows:

- **`x/circuit` — kept, via `github.com/cosmos/cosmos-sdk/contrib/x/circuit`.**
  The module is functionally live here: providing `CircuitBreakerKeeper` to
  depinject (`app/app.go`) makes the sdk's tx module insert the
  circuit-breaker ante decorator, giving the governance authority an
  emergency per-message-type kill switch — trip a broken message type without
  halting the chain. v0.55 offers no replacement for this capability;
  `contrib/` means Cosmos Labs stopped feature work, not that the sdk
  superseded it. `contrib/x/circuit` has no separate `go.mod` — it ships
  inside the sdk module — so keeping it *removes* the `cosmossdk.io/x/circuit`
  pin and moots Dependabot #124 (whose v0.2.0 bump failed `TestRootCmdSmoke`
  against `api` v0.9.2; the `api` v1.1.0 mismatch dissolves with the pin).
- **`x/nft` — dropped.** Pure scaffold: wired in `app_config.go` (module
  config, module account, blocked-address entry, InitGenesis) with zero
  protocol usage — no keeper reference in timeflare code, no mention in
  `docs/spec.md` or the guides. A secrets protocol has no NFT surface;
  architectural minimalism says shed the unmaintained module rather than
  migrate it to `contrib/`.
- **`x/group` — dropped (forced).** The module does not exist in v0.55.0 —
  it moved to the licensed Cosmos Enterprise offering. Timeflare wires it in
  `app_config.go` (EndBlockers, InitGenesis, module config) but has zero
  protocol usage, so the removal is clean. A licensed dependency would be
  incompatible with the project's fully-decentralised model even if the
  module were used.
- **`x/params` — dropped (forced).** Removed entirely upstream. Timeflare
  carried `ParamsKeeper` and `GetSubspace` solely for ibc-go v10 ("TODO:
  Remove once IBC modules migrate away"); ibc-go v11 has migrated, so the
  keeper, the accessor, and the store mount all go.
- **`cosmossdk.io/x/{evidence,feegrant,upgrade,tx}` — import paths migrate**
  to `github.com/cosmos/cosmos-sdk/x/*` (used in `app/` and transitively);
  `cosmossdk.io/log` → `cosmossdk.io/log/v2`; `cosmossdk.io/store` imports
  (in `x/secrets/keeper/`, `app/`, `cmd/`) → the sdk's `store/v2` path.

## 4. Required wiring changes (beyond dependency bumps)

From the v0.54 and v0.55 upgrade guides, mapped to timeflare's files:

1. **`app/app_config.go`**: `x/bank` first in `EndBlockers` (BlockSTM
   lifecycle registration — required whether or not BlockSTM is enabled);
   add `stakingtypes.KeyRotationFeePoolName` with `authtypes.Burner` to
   `moduleAccPerms` (staking keeper panics at construction without it);
   remove nft/group/params entries per §3.
2. **`app/app.go`**: remove `ParamsKeeper` field and `GetSubspace`; circuit
   import path → `contrib/`; drop removed-module keepers from depinject
   outputs. Gov keeper wiring is depinject-managed and falls back to the
   staking keeper for vote calculation — expected no change; verify at
   compile.
3. **`cmd/timeflared/cmd/root.go`**: delete the
   `TextualCoinMetadataQueryFn` tx-config setup (SIGN_MODE_TEXTUAL is removed
   upstream; enum value reserved).
4. **`cmd/timeflared/cmd/testnet_multi_node.go`**:
   `ExportGenesisFileWithTime` now takes a caller-built `AppGenesis` (the new
   form preserves consensus params — previously they were rebuilt from
   defaults, so check whether the multi-node testnet genesis relied on that).
5. **AutoCLI / client/v2 v2.11.0**: re-verify the two descriptor exception
   categories in `x/secrets/module/autocli.go` and the hand-written commands
   in `x/secrets/client/cli/tx.go`. `TestTxCommandParity` (both directions)
   and `TestRootCmdSmoke` are the tripwires. If client/v2 v2.11.0 can now
   bind Coin flags for non-pulsar modules, removing the hand-written
   `guardian-register`/`guardian-update` commands becomes possible — that is
   a **follow-up plan**, not this one.
6. **Validator consensus key rotation is active by default on v0.55** once
   the maccPerm is in place (`MsgRotateConsPubKey`, default
   `key_rotation_fee` of 1,000,000 bond denom). Stock validator-ops surface,
   not a timeflare protocol change; accepted with upstream defaults. No
   spec.md change — the spec does not document validator operations.

## 5. Risks and how they are bounded

- **Freshness.** sdk v0.55.0 and cometbft v0.40.0 were both released on
  27 July 2026 — `.0` releases with minimal ecosystem mileage, and no ibc-go
  release is upstream-tested against them (compile-verified only, §1). This
  is the price of the one-hop ruling, accepted pre-launch. Bounded by the
  full devnet gate: `deps-verify-consensus` plus `make e2e-full`'s
  cross-validator app-hash determinism check — the strongest local signal
  available for a consensus-stack move.
- **State breakage.** The entire hop is state breaking upstream. Pre-launch
  this is absorbed by `dev-reset` (fresh genesis); there is no migration to
  write. Post-launch the same move would need a coordinated upgrade — the
  argument for taking it now.
- **The guardian is the component that gets missed** (CLAUDE.md). Separate Go
  module importing `x/secrets/types` + `crypto` only; an sdk move that
  compiles on the chain can still break its client paths. Mitigated by
  bumping all three modules in one branch and by `make test` / `deps-verify`
  iterating the submodules — `go test ./...` from the root does not descend
  into them.
- **Workspace drift.** The failure mode that broke #121 (`go.work` behind a
  module's `go` directive). This hop moves the directive deliberately
  (1.25.8 → 1.26.5); `deps-verify`'s drift check (`go work sync` must leave
  `go.work.sum` unchanged) is the mechanical guard. Local note: the Go 1.26
  toolchain move can break prebuilt Go tools — reinstall via `go install` if
  lint/buf misbehave.
- **Generated-code drift.** `cosmossdk.io/api` v1.1.0 is a major; timeflare's
  own protos do not change, so its generated output should be stable — but
  `make deps-update-proto` verifies rather than assumes. If the TypeScript
  SDK's generated output changes, the mobile vendored tarball must be
  repacked (`pack-sdk.sh` + lockfile integrity sync) or mobile CI runs a
  stale SDK.
- **A silent behavioural change on the distribution path.** The genuine
  protocol-adjacent residual; see §6, V1. The e2e suite's community-pool
  assertion is the tripwire, and it runs in the gate.

## 6. Verification items (resolved 1 August 2026, on the execution branch)

**V1 — Do the `x/distribution` fixes change the dust behaviour
`docs/spec.md` documents?** Resolved: no. #26406 refactors the truncation
into `sendCoinsToDestination` with identical semantics on the normal path —
truncate, pay the delegator, remainder to the community pool; the new
fallback paths engage only when the *destination* address is blocked, which
never occurs for delegator withdrawals here (timeflare blocks only module
accounts). The spec.md §Fee Distribution dust claim stands unchanged, and
the e2e community-pool assertion (scenario S9) passed on v0.55.0.

**V2 — Does the restructured store preserve the per-secret settlement
cache-context semantics?** Resolved: yes. The settlement and commit-expiry
conformance tests and the full scenario suite pass unchanged on v0.55.0; no
write-semantics difference was observed. The one gate failure investigated
under this heading turned out to be a defect in scenario S10b itself (added
31 July 2026, never previously executed — the E2E CI job was skipped on both
PRs that carried it): it read guardian acceptance from assignment records
that Stage 1 retention deletes at the terminal transition on *both* sdk
versions. Fixed in-branch to read the reveal records, which survive until
Stage 2 by design.

**V3 — Genesis fidelity of the new `ExportGenesisFileWithTime`.** Resolved:
the genesis export/validate round-trip is green and the 3-validator compose
stack runs the full lifecycle to app-hash agreement on the exported genesis
(`make e2e-full`, height 186).

## 7. Execution

Per the T2 consensus runbook (`docs/guides/DEPENDENCIES.md`), worktree forked
from `main`; `main` is never worked on directly.

1. **Branch** in a worktree. Do not reuse the Dependabot branches.
2. **Toolchain first**: `go.work` and the three module `go` directives to
   1.26.5; reinstall prebuilt tools if they misbehave against Go 1.26.
3. **Bump all three sdk-bearing modules together**:
   `make deps-update-consensus VERSION=v0.55.0`, then the moves the scaffold
   does not know about: cometbft v0.40.0, `cosmossdk.io/api` v1.1.0, core
   v1.1.0, client/v2 v2.11.0, log/v2, ibc-go v11.2.0, and dropping the
   `cosmossdk.io/x/circuit` and `cosmossdk.io/x/nft` pins. `go work sync`
   after.
4. **Import-path sweep** (§3) and **wiring changes** (§4), including the nft /
   group / params removals. Grep-driven: confirm each removed symbol is clear
   across *all* components (chain, guardian, types module, TS SDK, docs),
   not just the ones changed.
5. **`make deps-update-proto`** — expected no descriptor change for
   timeflare's own protos; a change here cascades to the TS SDK and mobile
   tarball (§5) and must be handled, not ignored.
6. **Re-justify the carried patches** (`make deps-pins`): gin
   (GHSA-h395-qcrw-5vmq), goleveldb, nhooyr.io/websocket — exit conditions
   re-checked against the new graph; drop any upstream has resolved.
7. **Re-audit `.govulncheck-accepted`.** One entry (`GO-2026-5932`,
   `x/crypto/openpgp` via `x/upgrade` → `hashicorp/go-getter`). The
   `x/upgrade` re-homing may have moved the edge; if a fixed `go-getter` is
   now in the graph, remove the entry.
8. **Verify.** `make verify` + `make test` (all four modules), then
   `make deps-verify`, then the devnet gate: `make deps-verify-consensus`
   and `make e2e-full` (cross-validator determinism), with
   `TIMEFLARE_BLOCK_TIME=1s`. Resolve §6 V1–V3 in writing; land any spec.md
   correction in the same branch.
9. **PR** labelled `security` **and** `e2e`; observe CI to completion.
10. **Close Dependabot #121, #122, #124** as superseded, linking the PR.

## 8. What this plan does not do

- **Does not adopt the new v0.54/v0.55 features**: BlockSTM, LibP2P,
  AdaptiveSync, app-side mempool, ML-DSA-65 keys, OpenTelemetry — all stay at
  stock-off defaults. Consensus key rotation is the one default-on feature
  and is accepted as stock validator ops (§4).
- **Does not remove the hand-written Coin-flag tx commands** even if
  client/v2 v2.11.0 makes it possible — follow-up plan (§4).
- **Does not change any timeflare protocol behaviour.** No proto change, no
  message change, no economics change. If §6 finds an upstream fix alters
  behaviour `docs/spec.md` asserts, the resolution is a documentation
  correction, not a protocol change.
- **Does not track ibc-go beyond v11.2.0.** When an ibc-go release lands that
  is upstream-tested against sdk v0.55, taking it is a routine T1/T2 bump.

## 9. Acceptance

- All three sdk-bearing modules resolve `github.com/cosmos/cosmos-sdk
  v0.55.0`; `crypto/` untouched
- `cosmossdk.io/x/circuit`, `cosmossdk.io/x/nft` pins gone; circuit wired via
  `contrib/`, nft and group fully removed, `x/params` wiring deleted
- `make verify`, `make test`, `make deps-verify` green (four modules, audits,
  govulncheck, drift check) on Go 1.26.5
- `make deps-verify-consensus` green — fresh devnet `make e2e` +
  `make e2e-scenarios` + genesis round-trip — and `make e2e-full` green
  (cross-validator app-hash determinism)
- Carried `replace` block and `.govulncheck-accepted` each re-justified, with
  any dropped entry noted in the PR
- §6 V1–V3 resolved in writing; any `docs/spec.md` correction landed in the
  same branch
- CI green on the PR, `security` and `e2e` labels applied; #121, #122, #124
  closed as superseded with links
