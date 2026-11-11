# CI gating — run only what the diff can actually change

| | |
|---|---|
| **Priority** | P2 |
| **Status** | refining |
| **Created** | 4 August 2026 |
| **Updated** | 11 November 2026 |
| **Protocol surface** | none |
| **Repositories** | chain |
| **Depends on** | — |
| **Related** | [DONE_E2E_SCENARIO_DETERMINISM_PLAN.md](done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md) |
| **Open questions** | 4 |

**Why this priority**: CI latency and cost; no protocol impact, but it shapes
every contributor's edit loop and blocks documentation work behind the full
verification, test and e2e suites.
**Status notes**: Four open questions below.
**Origin**: owner request, 4 August 2026: "ensure the various workflows are
gated on things that can actually change the outcome", motivated by
documentation changes paying for full verification and tests.
**Components**:
- `.github/workflows/ci.yml` (all four jobs)
- `.github/workflows/release.yml` (reviewed; deliberately left ungated)
- repository branch-protection settings (required status checks) — not a file
  in this repository, and the reason a job-level gate is chosen over a
  trigger-level one
- no Go, proto, spec or `make` surface changes

## The problem

`ci.yml` has four jobs and every one of them runs on every pull request,
regardless of what changed:

| Job | Cost | Runs today when only `docs/spec.md` changes |
|---|---|---|
| `workflows` (actionlint) | seconds | yes |
| `proto` (buf lint + breaking) | ~30s + buf setup | yes |
| `chain` (`make verify` + `make test`) | minutes | yes |
| `e2e` (devnet lifecycle + scenarios) | around ten minutes by the projection in [DONE_E2E_SCENARIO_DETERMINISM_PLAN.md](done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md); not re-measured for this plan | yes |

A change confined to prose cannot alter the outcome of `proto`, and — with the
exceptions inventoried below — cannot alter `chain` or `e2e` either. `buf lint`
and `buf breaking` read `proto/` and the buf configuration. `make test` compiles
and runs Go tests. `make verify` is `gofmt`, `golangci-lint`, `go vet`,
`verify-boundaries`, `verify-choke-points` and `verify-networks`: the first five
read only `.go` files and the `x/secrets/types` module graph; the sixth reads the
network registry and the devnet tree, and is the one that does not ignore
Markdown.

## What each job's outcome actually depends on

Established by reading the targets and grepping the test tree, not assumed:

- **`make verify`** — `**/*.go`, `.golangci.yml`, `go.mod`/`go.sum` in both
  modules, `make/go-quality.mk`, plus the inputs of `verify-networks`
  (`make/scripts/verify-networks.sh`):
  - `networks.json` — validated structurally;
  - `app/app.go` and `devnet/lib/common-utils.sh` — the registry's address
    prefix and devnet chain id are compared against them;
  - **every file under `devnet/`, whatever its extension** — the script runs a
    recursive `grep` for a hard-coded `TIMEFLARE_BLOCK_TIME` default across the
    whole directory. `devnet/README.md` is inside that sweep, so Markdown under
    `devnet/` can fail `make verify`.
- **`make test`** — the Go inputs above, plus the non-Go inputs that tests read
  from disk:
  - `docs/operations.md` — `cmd/timeflared/cmd/example_drift_test.go` extracts
    every `timeflared …` invocation and asserts the command path, every flag and
    the positional arity resolve against the real command tree, and that at
    least 15 examples were extracted. **This file is code for gating purposes.**
  - `devnet/chain/generate-genesis-keys.sh`, `devnet/docker/init-chain.sh` —
    `app/rebate_pool_test.go` asserts they carry the rebate pool address.
  - `testdata/vectors/*.json` — asserted by the share-band, creation-fee, dial
    and selection-gas vector tests.
  - `x/secrets/types/testdata/vectors/*.json` — `app/wallet_derivation_test.go`
    reads `wallet_derivation.json` from the wire-contract module.
  - `docs/static/**`, `docs/template/**` — embedded into the binary by
    `docs/docs.go` via `//go:embed`, so they compile.
- **`buf lint` / `buf breaking`** — `proto/**`, `buf.yaml`, `buf.lock`.
- **`e2e`** — everything `chain` depends on, plus `devnet/**` (which includes
  `devnet/versions.env` and the e2e driver's `devnet/e2e/package.json` and
  lockfile).
- **`actionlint`** — `.github/workflows/**`.

The residue — every `*.md` except `docs/operations.md` and those under
`devnet/`, plus `LICENSE` — is provably inert.

## Design

A `changes` job classifies the diff and emits a single boolean, `docs_only`.
`proto`, `chain` and `e2e` are skipped when it is true. `workflows` stays
unconditional: it is seconds-fast and it is the deliberate always-runs anchor
for Dependabot's `github-actions` pull requests (the comment at the top of
`ci.yml` explains why it exists at all).

`proto` additionally gates on its own inputs, so a pure-Go pull request stops
paying for buf setup.

| Job | Condition |
|---|---|
| `workflows` | always |
| `changes` | always |
| `proto` | `proto/**`, `buf.yaml` or `buf.lock` touched |
| `chain` | not `docs_only` |
| `e2e` | not `docs_only` |

### Allowlist, not denylist

`docs_only` is computed as *"every path in the diff matches the inert
allowlist"*. Anything unrecognised — a new top-level file, a rename, a config
this plan did not anticipate — falls outside the allowlist and runs the full
suite. The gate therefore fails towards spending CI minutes, never towards
skipping a check that mattered. A denylist ("skip if only `*.md` changed")
inverts that: it silently exempts `docs/operations.md` and `docs/static/`, both
of which are compiled or asserted.

The allowlist is:

```
**/*.md   — except docs/operations.md
LICENSE
```

`docs/operations.md` is called out as an explicit exception in the same place,
with a comment pointing at `example_drift_test.go`, so the next person to widen
the allowlist meets the reason.

As drawn, this list still matches `devnet/README.md`, which the inventory above
shows is not inert. How Markdown under `devnet/` is treated is open question 3.

### Why not `paths-ignore`

`paths-ignore` on the trigger is the obvious one-line change and it is the wrong
one, for two independent reasons:

1. **It leaves required checks pending forever.** When a workflow is skipped by
   path or branch filtering, GitHub does not create the check runs at all, so
   branch protection waits on a check that will never report. A job skipped by a
   job-level `if:` reports a `skipped` conclusion, which satisfies a required
   check. This is documented GitHub behaviour ("handling skipped but required
   checks") and it is the whole reason for the `changes` job.
2. **It cannot express the exception.** `paths-ignore` has no negation. The
   `docs/operations.md` carve-out would have to be smuggled into an ordered
   `paths:` list where the last match wins — unreadable, and a silent
   test-skipping bug the first time someone reorders it.

### The `needs` skip trap

`e2e` currently declares `needs: [chain, proto, workflows]`. A skipped `needs`
job propagates: if `proto` is skipped because no proto changed, `chain` and
`e2e` are skipped too, and a pure-Go pull request would run no tests at all.
The fix is a guard that distinguishes *skipped* from *failed*, because the naive
`if: always()` also throws away the fail-fast property the current
`needs: [proto, workflows]` was added for (its comment: "so a lint failure never
burns the long test run"):

```yaml
if: ${{ !cancelled() && !contains(needs.*.result, 'failure')
        && needs.changes.outputs.docs_only == 'false' }}
```

`!cancelled()` lets a skipped dependency through; the `contains` clause still
stops a failed one. This applies to both `chain` and `e2e`.

### Computing the diff

The `changes` job checks out with `fetch-depth: 0` (as `proto` already does for
`buf breaking`) and diffs:

- pull request — `git diff --name-only <base.sha> <head>`
- push to `main` — `git diff --name-only ${{ github.event.before }} ${{ github.sha }}`

`github.event.before` is all-zeros on a branch's first push and unreliable after
a force push. When the base commit is absent or unresolvable, the job emits
`docs_only=false` and logs why — the fail-safe direction.

## `release.yml` stays ungated

Reviewed and deliberately unchanged. Its `verify` job runs on a `v*` tag push,
where the meaningful diff is not "the last commit" but "the entire tree being
published". A tag whose final commit touched only prose still ships binaries, a
proto tarball and a vector corpus, and the version-policy guard, the
`GITHUB_REF_NAME` stamp assertion and the checksum manifest all have to run for
that tag regardless. Gating a release on the shape of one commit would be
gating on the wrong thing.

## What this does not solve

- **`e2e` depends on artefacts this repository does not hold.** It runs against
  the pinned `guardiand` release and the pinned SDK dist tarball, and its
  `Preconditions` step fails the job when either is unreadable. Gating changes
  how often it runs, not what it needs in order to pass.
- **No caching or suite-splitting.** `chain` still runs `make verify` and the
  full `make test` whenever any Go file changes, including a one-line comment.
  Making *that* proportional is a different concern and would need its own plan.
- **The gate is per-diff, not per-package.** A change under `x/secrets/keeper/`
  runs the `x/secrets/types` tests too. Correct but not minimal.
- **Nothing here compiles a consumer.** Unchanged, and still the standing hazard
  (`PROTOCOL_CHANGE.md`): a green `ci.yml` never meant the guardian or the SDK
  still agree, and it means no more and no less than that after this change.

## Open questions

1. **Are `chain`, `proto` and `e2e` configured as required status checks on
   `main`?** The branch-protection settings cannot be read from the checkout.
   *Recommendation: proceed regardless.* The job-level gate is correct in both
   worlds — a skip reports `skipped` and satisfies a required check, whereas the
   `paths-ignore` alternative would break only in the "yes" case, invisibly. If
   the answer is yes, the first docs-only pull request after this lands should be
   watched to confirm the checks resolve rather than hang.
2. **Should `.github/dependabot.yml` join the inert allowlist?** It cannot change
   any job's outcome. *Recommendation: no.* It is one file that changes rarely,
   and every entry added to the allowlist is a standing assertion someone must
   re-verify. Keep the allowlist to the two entries that carry real traffic.
3. **How does the allowlist treat Markdown under `devnet/`?** `verify-networks`
   greps the whole of `devnet/`, so `devnet/README.md` can fail `make verify` and
   `**/*.md` is not inert there. Either the allowlist carves `devnet/**` out
   beside `docs/operations.md`, or the script's sweep is narrowed to scripts —
   the second is a `make` surface change, which this plan currently rules out.
   *Recommendation: carve `devnet/**/*.md` out of the allowlist.* It costs one
   more exception line, keeps this plan to `ci.yml`, and fails towards running
   the suite.
4. **Does the generated-code gate belong in this plan?** `TODO.md` ("Nothing
   verifies that generated code matches the protos") records that no check
   confirms the committed `.pb.go` files match `proto/`, sketches a
   `make proto-gen && git diff --exit-code` step, notes that it needs a pinned
   generation environment to be honest, and says it "may belong" here. This plan
   reduces what runs; that gate adds a check. *Recommendation: no — give it its
   own plan.* It is a different concern (one plan, one concern) with its own
   unsolved prerequisite, and folding it in would hold a simple gating change
   behind a toolchain-pinning decision.

## Verification

Local (`actionlint` is already installed by the `workflows` job, so run the same
binary against the edited file):

1. `actionlint .github/workflows/ci.yml`

On the execution branch, four pull requests exercising each arm — this is a CI
change, so the only honest verification is observing CI:

2. **Docs-only** — edit `docs/spec.md`. Expect `workflows` and `changes` green;
   `proto`, `chain`, `e2e` skipped; the pull request mergeable.
3. **The carve-out** — edit `docs/operations.md` alone. Expect `chain` to **run**.
   This is the assertion that matters most; if it skips, the gate is unsafe.
4. **Go-only** — touch a comment in `x/secrets/keeper/`. Expect `proto` skipped,
   `chain` and `e2e` to run — i.e. the `needs` skip trap is actually handled.
5. **Proto** — touch `proto/**`. Expect all jobs to run.

No devnet involvement, so `chain.lock` is not needed for any of this.
