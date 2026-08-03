# Chain Upgrade Runbook

*How Timeflare software upgrades are built, rehearsed, and rolled out*

## Overview

Timeflare uses the Cosmos SDK `x/upgrade` module for coordinated, consensus-safe
binary upgrades. An upgrade is scheduled on-chain (via governance); when the
chain reaches the upgrade height, every node without the new upgrade handler
halts deliberately. Validators then install the new binary — whose registered
handler runs the state migrations exactly once — and the chain resumes.

Two distinct kinds of "update" exist, with different processes:

| Kind | Example | Process |
|------|---------|---------|
| **Consensus-breaking** | New message types, state layout changes, parameter semantics | Requires an on-chain software upgrade (this runbook) |
| **Non-breaking** | Bug fixes in queries, CLI improvements, guardian service changes | Plain release; validators update at leisure |

If a change alters how transactions are validated or state is written, treat it
as consensus-breaking.

## Anatomy of an Upgrade

```
app/upgrades/
├── types.go          # Upgrade struct definition
└── v2/               # one package per upgrade
    └── upgrade.go    # UpgradeName + handler + store changes
app/upgrades.go       # the registry: var Upgrades = []upgrades.Upgrade{v2.Upgrade}
```

The upgrade **name** ties everything together: the governance proposal, the
registered handler, and (in a cosmovisor layout) the binary directory. They
must all match exactly.

## Creating an Upgrade

1. **Scaffold the package**:
   ```bash
   make upgrade-scaffold NAME=v2
   ```
2. **Register it** in `app/upgrades.go`:
   ```go
   import v2 "github.com/timeflareio/chain/app/upgrades/v2"

   var Upgrades = []upgrades.Upgrade{v2.Upgrade}
   ```
3. **Implement migrations** in the handler if the upgrade changes state. Module
   version bumps are handled automatically by `RunMigrations`; custom state
   surgery goes before it.

   **Derived state needs a module migration, not handler surgery.** Anything
   `InitGenesis` builds as a side effect — indexes, queues, caches — is absent
   after an in-place upgrade, because an upgrade never runs `InitGenesis`. Bump
   the owning module's `ConsensusVersion` and register the rebuild with
   `cfg.RegisterMigration` in `RegisterServices`: a versioned module migration
   replays correctly for a node syncing from genesis, whereas handler surgery is
   easy to get wrong on replay. Make the rebuild idempotent (derive from scratch
   rather than upsert) so it repairs drift as well as absence, and assert the
   result — `x/secrets` rebuilds its guardian eligibility index this way and
   proves it with invariant 9.
4. **Declare store changes** in `StoreUpgrades` if modules are added, renamed,
   or removed.

Never remove an upgrade the chain has already passed on a live network — nodes
syncing from genesis must still replay it.

## Rehearsing Locally

The devnet is initialised with a **60-second governance voting period**
(production default is 48h), so the full proposal → vote → upgrade flow can be
rehearsed in minutes:

```bash
make dev-up                          # running devnet (dev-reset if initialised pre-tooling)
make devnet-upgrade-test NAME=v2     # submit proposal, vote, wait for the height
```

**Run the rehearsal with the OLD binary** — the upgrade must not yet be
registered in `app/upgrades.go` when the proposal executes. The flow mirrors
production exactly:

1. The chain halts at the upgrade height with `UPGRADE "v2" NEEDED`.
2. Register the upgrade in `app/upgrades.go`, `make install`, `make dev-up`.
3. The handler runs once on restart and the chain resumes. Verify with
   `timeflared query upgrade applied v2`.

**Ordering matters**: `x/upgrade` deliberately fails consensus if a binary that
already registers the handler runs *before* the upgrade height (`BINARY UPDATED
BEFORE TRIGGER!`) — the same protection that stops a production validator from
switching binaries early. The rehearsal script detects this misordering and
prints the recovery steps (unregister, reinstall, restart, halt as intended).

## Production Rollout

1. **Tag and publish** the new release; CI builds `timeflared` binaries.
2. **Submit the software-upgrade proposal** with the target height (allow
   enough lead time for the 48h voting period plus validator preparation):
   ```bash
   timeflared tx gov submit-proposal upgrade-proposal.json --from <key> ...
   ```
   The proposal JSON contains a `MsgSoftwareUpgrade` whose `plan.name` matches
   the registered upgrade name and whose authority is the gov module account.
3. **Validators prepare**: download/build the new binary ahead of the height.
4. **At the upgrade height** every node halts. Validators swap binaries and
   restart; the handler runs once and the chain resumes when +2/3 voting power
   is back online.
5. **Verify**: `timeflared query upgrade applied <name>` and normal block
   production.

**Rollback stance**: once the upgrade height is reached and migrations have
run, rolling back requires a coordinated halt and state export — treat upgrades
as forward-only and rehearse them properly instead.

> **Cosmovisor** (deferred): the registry and this flow are compatible with
> cosmovisor, which automates the halt-and-swap step by supervising the chain
> process and switching to a pre-staged binary at the upgrade height. Adopting
> it later requires no changes to the code in `app/upgrades/` — only operator
> tooling (directory layout plus service configuration).

## Dependency Updates (non-consensus)

- `make update-deps` — updates all four ecosystems (root Go module, guardian Go
  module, Rust crates, npm packages); follow with `make test-all`.
- Dependabot raises weekly PRs per ecosystem (`.github/dependabot.yml`), with
  Cosmos SDK packages grouped so they move together.
- Proto compatibility is guarded in CI by `buf breaking` against `main`.
- **Toolchain note**: a newer local Go toolchain can break older pinned
  dependencies (e.g. `bytedance/sonic` needed upgrading for Go 1.26) — when
  builds fail after a toolchain update, check indirect dependencies first.
