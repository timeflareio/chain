# CI gating — run only what the diff can actually change

- **Priority** — P2 — CI latency and cost; no protocol impact, but it shapes
  every contributor's edit loop and blocks documentation work behind a
  twenty-minute suite.
- **Status** — `refining` (4 August 2026). Two open questions below.
- **Origin** — owner request, 4 August 2026: "ensure the various workflows are
  gated on things that can actually change the outcome", motivated by
  documentation changes paying for full verification and tests.
- **Components** —
  - `.github/workflows/ci.yml` (all four jobs)
  - `.github/workflows/release.yml` (reviewed; deliberately left ungated)
  - repository branch-protection settings (required status checks) — not a file
    in this repository, and the reason design B is chosen over design A
  - no Go, proto, spec or `make` surface changes

## The problem

`ci.yml` has four jobs and every one of them runs on every pull request,
regardless of what changed:

| Job | Cost | Runs today when only `docs/spec.md` changes |
|---|---|---|
| `workflows` (actionlint) | seconds | yes |
| `proto` (buf lint + breaking) | ~30s + buf setup | yes |
| `chain` (`make verify` + `make test`) | minutes | yes |
| `e2e` (devnet lifecycle) | tens of minutes | yes |

A change confined to prose cannot alter the outcome of any of the last three.
`make verify` is `gofmt`/`golangci-lint`/`go vet`/`verify-boundaries`/
`verify-choke-points` — all four read only `.go` files and the `x/secrets/types`
module graph. `make test` compiles and runs Go tests. `buf lint` and
`buf breaking` read `proto/` and the buf configuration. None of them opens a
Markdown file, and no script under `make/scripts/` scans one.

## What each job's outcome actually depends on

Established by reading the targets and grepping the test tree, not assumed:

- **`make verify`** — `**/*.go`, `.golangci.yml`, `go.mod`/`go.sum` in both
  modules, `make/go-quality.mk`.
- **`make test`** — all of the above, plus three non-Go inputs that tests read
  from disk:
  - `docs/operations.md` — `cmd/timeflared/cmd/example_drift_test.go` extracts
    every `timeflared …` invocation and asserts the command path, every flag and
    the positional arity resolve against the real command tree, and that at
    least 15 examples were extracted. **This file is code for gating purposes.**
  - `devnet/chain/generate-genesis-keys.sh`, `devnet/docker/init-chain.sh` —
    `app/rebate_pool_test.go` asserts they carry the rebate pool address.
  - `testdata/vectors/*.json` — asserted by the share-band, creation-fee, dial
    and selection-gas vector tests.
  - `docs/static/**`, `docs/template/**` — embedded into the binary by
    `docs/docs.go` via `//go:embed`, so they compile.
- **`buf lint` / `buf breaking`** — `proto/**`, `buf.yaml`, `buf.lock`.
- **`e2e`** — everything `chain` depends on, plus `devnet/**` and
  `devnet/versions.env`.
- **`actionlint`** — `.github/workflows/**`.

The residue — every `*.md` except `docs/operations.md`, plus `LICENSE` — is
provably inert. That set is the gate.

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

- **`e2e` cannot pass yet.** It is blocked on the private `timeflareio/guardian`
  repository and on the TypeScript SDK having no release to pin (both documented
  in the job itself). Gating changes how often it runs, not whether it passes.
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
   `main`?** I cannot read the branch-protection settings from the checkout.
   *Recommendation: proceed regardless.* Design B is correct in both worlds — a
   job-level skip reports `skipped` and satisfies a required check, whereas the
   `paths-ignore` alternative would break only in the "yes" case, invisibly. If
   the answer is yes, the first docs-only pull request after this lands should be
   watched to confirm the checks resolve rather than hang.
2. **Should `.github/dependabot.yml` join the inert allowlist?** It cannot change
   any job's outcome. *Recommendation: no.* It is one file that changes rarely,
   and every entry added to the allowlist is a standing assertion someone must
   re-verify. Keep the allowlist to the two entries that carry real traffic.

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
