# Test Coverage Gaps Plan

| | |
|---|---|
| **Priority** | P1 |
| **Status** | refining |
| **Created** | July 2026 |
| **Updated** | 11 November 2026 |
| **Protocol surface** | none |
| **Repositories** | chain, guardian |
| **Depends on** | — |
| **Related** | [DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md](done/DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md); [DONE_GUARDIAN_IMPROVEMENTS_PLAN.md](done/DONE_GUARDIAN_IMPROVEMENTS_PLAN.md); [DONE_DEPENDENCY_MANAGEMENT_PLAN.md](done/DONE_DEPENDENCY_MANAGEMENT_PLAN.md) |
| **Open questions** | 4 |

**Why this priority**: assurance
**Status notes**: no phase has started; four open questions below.
**Origin**: automated review, July 2026.
**Components**: `x/secrets/keeper/query.go`, `x/secrets/keeper/genesis_test.go`, `x/secrets/simulation/` (new), `.github/workflows/ci.yml`; in `timeflareio/guardian`, `internal/guardian/integration_test.go`

**Already landed**: the direct genesis tests. `x/secrets/keeper/genesis_test.go` holds `TestGenesis_RoundTripEquality` (export → import → export is the identity across three lifecycle stages, with the invariant sweep), `TestGenesisState_Validate_Structural` (table-driven rejection rules) and `TestGenesis_ImportSweepHaltsOnInconsistentState`. The Go-side assertion of the shared primitive vectors is `go/vectors_test.go` in `timeflareio/crypto`, against its `vectors/` corpus. The guardian's native-gRPC client has landed ([DONE_GUARDIAN_IMPROVEMENTS_PLAN.md](done/DONE_GUARDIAN_IMPROVEMENTS_PLAN.md)), so nothing in Phase 3 waits on it.

## What this plan does

Closes the specific, identified test-coverage gaps that remain after the (genuinely strong) economics conformance and fuzz work: the query layer, a fuzz-driven genesis round-trip, the skipped guardian integration suite, and standard Cosmos SDK simulation support.

## Why

The economics core is exceptionally well tested (a conformance suite of 32 tests in `x/secrets/keeper/conformance_test.go`, a seeded lifecycle fuzzer with full invariant re-checks, exact-uveil e2e scenario assertions). The gaps are at the edges:

1. **Query layer: 9 of 14 RPCs are never called by a Go test.** Only `HintsSince` has a dedicated file (`query_hints_test.go`); `Secret`, `Secrets`, `SecretsByCreator` and `SecretTombstone` are touched incidentally by the gas and retention tests. Untested: `PendingSecrets`, `SecretMeta`, `SecretAssignments`, `SecretReveals`, `SecretShare`, `SecretPayload`, `Guardian`, `Guardians`, `GuardianKeyHistory` — including their pagination behaviour and the on-demand assembly of the full secret view from side-stores (a regression here breaks every client silently, since consensus doesn't exercise queries).
2. **Genesis: the round-trip is proven only from a hand-built state.** The existing test drives three secrets to three chosen stages. Nothing exports from an arbitrary mid-flight state, and nothing shows an imported chain stays live afterwards.
3. **Guardian integration tests are 100% skipped**: all 9 tests in `timeflareio/guardian` — `internal/guardian/integration_test.go` (540 lines) start with `t.Skip("Test requires running blockchain for CLI transactions")`. The behaviours they claim to cover — restart recovery, concurrent secret processing, error recovery, reveal-window expiry — are precisely the guardian's slashing-risk surface, currently asserted by dead code. The skip reason names a CLI transaction path the daemon does not have: it talks to the chain over native gRPC.
4. **No SDK simulation support**: `app/app.go` constructs a `SimulationManager`, but `x/secrets` ships no simulation operations and no `RandomizedGenState` — standard `simd`-style app-level fuzz simulation never exercises secrets messages. The bespoke lifecycle fuzzer partly compensates within the module, but app-level sim additionally shakes out inter-module interactions (bank/auth/gov alongside secrets) and is table stakes for Cosmos audit reviews.

## How

### Phase 1 — Query-layer test suite

One test file per query handler group, covering: happy path, not-found, pagination (limit/offset/next-key, and the `HintsSince` 1000-cap), the filtered iteration in `PendingSecrets`, and — most valuably — **view-assembly consistency**: create secrets via the real message handlers, then assert the assembled query responses agree with the side-stores at every lifecycle stage (piggyback on the conformance suite's scenario builders rather than hand-rolling state).

### Phase 2 — Fuzz-driven genesis round-trip

Drive the lifecycle fuzzer to a mid-flight state (secrets in every state, queues populated), export genesis, import into a fresh app, assert byte-identical re-export and invariant-library cleanliness, then continue the fuzz run on the imported chain to prove liveness (the F2 conformance test covers past-due import, and `TestGenesis_RoundTripEquality` covers a fixed three-secret state; this generalises both).

### Phase 3 — Resurrect the guardian integration suite

The tests live in `timeflareio/guardian`; the devnet they would run against lives here. Where this phase is owned is open question 1. Two options for the tests themselves:

- **Option A**: convert the skipped tests to run against the devnet harness. The devnet targets (`make dev-up`, `make e2e`) are in this repository and consume a *released* `guardiand`, so a guardian-side target has no devnet of its own to require, and the label-triggered CI job that [DONE_DEPENDENCY_MANAGEMENT_PLAN.md](done/DONE_DEPENDENCY_MANAGEMENT_PLAN.md) delivered in the monorepo does not exist in either repository's workflows.
- **Option B**: rewrite them against the mocked `ClientInterface` (`timeflareio/guardian` — `internal/chain/interface.go`) where possible (restart recovery and cache reconciliation are testable with mocks; true concurrency/performance need Option A).

Delete any test whose premise the native-gRPC client has invalidated rather than porting it.

### Phase 4 — SDK simulation support

`x/secrets/simulation/` (new) with `RandomizedGenState` and weighted operations for the message surface (reusing the lifecycle fuzzer's action generators where the shapes align), wired into the module's `AppModuleSimulation` implementation; add a CI smoke sim (short determinism + non-halting run).

## Open questions

1. **Where does the guardian integration work live?** The nine skipped tests are in `timeflareio/guardian`, which has no pending plan covering them, and the harness they need is this repository's devnet. *Recommendation: move Phase 3 into a plan in `timeflareio/guardian`, which owns the tests, and keep this plan to the chain's own gaps; the chain's part is at most a devnet hook that plan asks for.*
2. **Guardian integration: Option A, B, or both?** A gives real coverage but ties guardian tests to the chain's devnet across a repository boundary — against a working tree that means the `GUARDIAND_BIN` escape hatch, against a release it means the tests run after the fact; B is fast but can't test what matters most. *Recommendation: A for the concurrency and recovery tests, deleting the rest — subject to question 1 settling which repository drives the devnet for them.*
3. **Simulation depth**: full weighted-operation sim for all 12 messages is significant work; a minimal viable version (register/request/confirm/reveal only) covers the state-heavy paths. Which bar? *Recommendation: the minimal four first, with the rest added as the fuzzer's generators are reused.*
4. **Coverage targets**: should CI enforce a coverage floor for `x/secrets`? `make test` already writes a coverage profile (`make/go-testing.mk`) and nothing gates on it. A floor prevents regression of exactly the kind this plan fixes. *Recommendation: yes, set at the measured figure once Phases 1 and 2 land, so the floor records an achievement rather than a wish.*
