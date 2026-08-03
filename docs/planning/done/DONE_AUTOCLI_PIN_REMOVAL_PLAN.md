# Removing the cosmos-sdk AutoCLI Replace Pin — Plan

*Retires the carried `replace github.com/cosmos/cosmos-sdk => … v0.53.2`
pin from all three Go modules. The pin's documented exit condition — "when
the module graph naturally resolves ≥ v0.53.2" — is met: the graph now
requires v0.53.4 naturally, so the pin actively **downgrades** the built
sdk. Removal is a T2 consensus-adjacent change (built sdk moves
0.53.2 → 0.53.4) and follows the T2 runbook gates.*

> **Status: done — executed July 2026** on branch
> `worktree-autocli-pin-removal` (PR #102). All T2 gates passed locally
> (unit + verify, fresh-devnet e2e + scenarios, genesis round-trip).
> **Priority**: P3 — hygiene; no urgency, but cheap and shrinks the
> carried-pin surface ahead of the multi-repo split
> (PENDING_MULTIREPO_MIGRATION_PLAN.md §4.3 mirrors this block across
> repos, so every retired pin is one less thing to keep in sync).
> **Origin**: pin audit during the multi-repo migration planning session,
> July 2026. Pin history: introduced at v0.50.13 to dodge an upstream
> AutoCLI bug (cosmos-sdk PR #24335), moved to v0.53.2 in June 2025
> (commit 42b52aad) when the fix shipped; v0.53.4 carries that fix.
> **Components**: `go.mod`/`go.sum` in the chain root, `x/secrets/types`,
> and `guardian`; `docs/guides/DEPENDENCIES.md`; `CLAUDE.md`.

## Change

1. **Branch** (dedicated, per working convention): delete the three-line
   pin entry — the two comment lines and the
   `github.com/cosmos/cosmos-sdk => github.com/cosmos/cosmos-sdk v0.53.2`
   replace line — from each of:
   - `go.mod` (chain root)
   - `x/secrets/types/go.mod`
   - `guardian/go.mod`

   The other three carried pins (gin, goleveldb, nhooyr.io) are
   untouched — their exit conditions are not met.
2. `go mod tidy` in each of the three modules; confirm
   `go list -m github.com/cosmos/cosmos-sdk` reports v0.53.4 in the chain
   root **and** in `guardian` (the "mirror exactly" invariant now holds by
   both graphs resolving the same version naturally, rather than by
   matching pins).
3. **Docs in the same PR**:
   - `docs/guides/DEPENDENCIES.md` — remove/adjust any prose using this
     pin as the worked example of a carried pin; note in the T2 section
     that the sdk version now floats on the natural graph resolution.
   - `CLAUDE.md` Key Configuration — "Cosmos SDK: v0.53.2 (via replace
     directive)" becomes the naturally-resolved version without the
     replace clause.
   - `make deps-pins` output should list one fewer pin (verify it derives
     from go.mod and needs no separate edit).

## Gates (T2 runbook, docs/guides/DEPENDENCIES.md)

- `make test` and `make verify` (root-command AutoCLI guard
  `TestRootCmdSmoke` is part of the unit suite).
- Fresh-devnet `make e2e` + `make e2e-scenarios` (label the PR `e2e` to
  run the devnet gate in CI).
- Genesis export/import round-trip check.

## Verification already done (2026-07-25, local)

With the pin deleted from all three modules: `go mod tidy` resolved
v0.53.4 cleanly everywhere; `TestRootCmdSmoke` (builds the full root
command through autocli's `EnhanceRootCommand`, which panics on any
mismatch) passed; the full chain build, `x/secrets` keeper tests, the
entire guardian suite, and the types module tests passed; and the
AutoCLI-generated `query secrets` tree rendered all 13 query verbs
correctly. The devnet e2e gates were **not** run — that is what execution
adds.

## Risk / rollback

Low: a patch-level sdk move (0.53.2 → 0.53.4) already required by the
graph, guarded by the AutoCLI smoke test and the devnet gate. Rollback is
reinstating the three-line pin entry and re-tidying. Note the removal does
**not** unlock `go install …@version` for our binaries — the three
remaining pins still block it (multi-repo plan §8.4 unchanged).
