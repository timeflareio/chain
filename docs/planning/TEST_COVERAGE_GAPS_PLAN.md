# Test Coverage Gaps Plan

**Status**: Proposed (automated review, July 2026)
**Priority**: P1 — assurance
**Components**: `x/secrets/keeper/query*.go`, `x/secrets/types/genesis.go`, `guardian/guardian/integration_test.go`, `x/secrets/simulation` (new)

## What this plan does

Closes the specific, identified test-coverage gaps that remain after the (genuinely strong) economics conformance and fuzz work: the query layer, genesis round-trips, the skipped guardian integration suite, and standard Cosmos SDK simulation support. Also corrects TODO.md's stale claims about what is untested.

## Why

The economics core is exceptionally well tested (18-scenario conformance suite, seeded lifecycle fuzzer with full invariant re-checks, exact-uveil e2e scenario assertions). The gaps are at the edges:

1. **Query layer: 12 of 13 RPCs have no dedicated tests.** Only `query_hints_test.go` exists. Untested: `Secret`, `Secrets`, `SecretsByCreator`, `PendingSecrets`, `SecretMeta`, `SecretAssignments`, `SecretReveals`, `SecretShare`, `SecretPayload`, `Guardian`, `Guardians` — including their pagination behaviour and the on-demand assembly of the full secret view from side-stores (a regression here breaks every client silently, since consensus doesn't exercise queries).
2. **Genesis: no direct export→import→export round-trip test**, and `GenesisState.Validate()` is barely tested (structural hardening of Validate itself is in SETTLEMENT_AND_STATE_INTEGRITY; this plan tests it).
3. **Guardian integration tests are 100% skipped**: all 9 tests in `guardian/guardian/integration_test.go` (576 lines) start with `t.Skip("Test requires running blockchain…")`, plus 2 more in `service_test.go`. The behaviours they claim to cover — restart recovery, concurrent secret processing, error recovery, reveal-window expiry — are precisely the guardian's slashing-risk surface, currently asserted by dead code.
4. **No SDK simulation support**: `app.go` constructs a `SimulationManager`, but `x/secrets` ships no simulation operations and no `RandomizedGenState` — standard `simd`-style app-level fuzz simulation never exercises secrets messages. The bespoke lifecycle fuzzer partly compensates within the module, but app-level sim additionally shakes out inter-module interactions (bank/auth/gov alongside secrets) and is table stakes for Cosmos audit reviews.
5. ~~No Go↔Rust HMAC cross-compatibility test asserted from the Go side~~ **Resolved (July 2026)**: `crypto/vectors_test.go` asserts the shared `testdata/vectors/` corpus from the Go side (DONE_CONSENSUS_CRYPTO_PURE_GO_PLAN.md Phase 0).
6. **The since-deleted TODO.md's "Missing Test Coverage" section was stale**: it listed `msg_server_reveal_share.go`, `guardian_selection.go`, `msg_server_cancel_secret.go` etc. as untested, but keeper tests for the message handlers exist; it also lists a `MsgReconstructSecret` that is no longer part of the message surface. The real gaps are the ones above.

## How

### Phase 1 — Query-layer test suite

One test file per query handler group, covering: happy path, not-found, pagination (limit/offset/next-key, and the `HintsSince` 1000-cap), the filtered iteration in `PendingSecrets`, and — most valuably — **view-assembly consistency**: create secrets via the real message handlers, then assert the assembled query responses agree with the side-stores at every lifecycle stage (piggyback on the conformance suite's scenario builders rather than hand-rolling state).

### Phase 2 — Genesis round-trip

- Drive the lifecycle fuzzer to a mid-flight state (secrets in every state, queues populated), export genesis, import into a fresh app, assert byte-identical re-export and invariant-library cleanliness, then continue the fuzz run on the imported chain to prove liveness (the F2 conformance test covers past-due import; this generalises it).
- Table-driven `GenesisState.Validate()` tests for each rejection rule (expands as SETTLEMENT_AND_STATE_INTEGRITY adds rules).

### Phase 3 — Resurrect the guardian integration suite

Two options:

- **Option A (recommended)**: convert the skipped tests to run against the devnet harness — a `make test-guardian-integration` target that requires `make dev-up` (mirroring how `make e2e` already works), gated in CI behind the label-triggered job proposed in `DONE_DEPENDENCY_MANAGEMENT_PLAN.md`.
- **Option B**: rewrite them against the mocked `ClientInterface` where possible (restart recovery and cache reconciliation are testable with mocks; true concurrency/performance need Option A).

Delete any test that the guardian-improvements plan's native-gRPC rewrite will invalidate rather than porting it twice — coordinate sequencing with that plan.

### Phase 4 — SDK simulation support

`x/secrets/simulation/` with `RandomizedGenState` and weighted operations for the message surface (reusing the lifecycle fuzzer's action generators where the shapes align), wired into the module's `AppModuleSimulation` implementation; add a CI smoke sim (short determinism + non-halting run).

### Phase 5 — TODO.md correction (resolved)

~~Rewrite the testing-gaps and CLI sections of TODO.md~~ Resolved August 2026: TODO.md was deleted by owner ruling — docs/planning/ is the roadmap (the DOCS_ACCURACY_REFRESH sweep that would have folded it in landed as PR #137).

## Open questions

1. **Guardian integration: Option A, B, or both?** A gives real coverage but ties guardian CI to devnet runtime; B is fast but can't test what matters most. Recommend A for the concurrency/recovery tests and deleting the rest.
2. **Simulation depth**: full weighted-operation sim for all 9 messages is significant work; a minimal viable version (register/request/confirm/reveal only) covers the state-heavy paths. Which bar?
3. **Coverage targets**: should CI enforce a coverage floor for `x/secrets` (the repo has `make test-all` with coverage but no gate)? A floor prevents regression of exactly the kind this plan fixes.
4. **Sequencing with the guardian rewrite**: if the native-gRPC client (guardian-improvements plan) lands soon, Phase 3 should follow it, not precede it. What's the expected order?
