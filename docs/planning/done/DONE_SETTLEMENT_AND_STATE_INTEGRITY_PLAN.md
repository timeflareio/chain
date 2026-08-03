# Settlement Error Handling & State Integrity Plan

**Status**: IMPLEMENTED (July 2026, branch `settlement-state-integrity`) — all design questions ruled (owner, July 2026); see the decision log below. Originally proposed by the automated review, July 2026. Implementation notes: Phase 3 items 3 and 4 were already resolved on main before this branch (the UUID regexp is a package-level `var` in `x/secrets/types/validation.go`, and the module's `BeginBlockers` entry is no longer inert — it carries the fee-burn split shipped in #70), so no change was needed for either. No live upgrade handler exists yet; the first one must call `keeper.CheckStateInvariants` after its migrations (spec.md "State Integrity & Import-Time Validation").
**Priority**: P0 — protocol correctness, pre-testnet
**Components**: `x/secrets/keeper/endblock_logic.go`, `x/secrets/types/genesis.go`, `x/secrets/keeper/msg_server_confirm_shares.go`, `x/secrets/keeper/guardian_selection.go`

## What this plan does

Fixes a cluster of state-integrity gaps found in the consensus path: settlement error-swallowing that can violate the no-stranded-bonds invariant, genesis validation that admits inconsistent state, a silent zero-key fallback that masks invariant violations, and stale dead code in a message handler.

## Why

### 1. Settlement swallows per-guardian errors while still finalising the secret

In `processExpiredSecret` / `distributePoolToRevealers` / `slashNoRevealBond`, a failed `UnlockGuardianFloat`, bank `Send`, or `Burn` for an individual guardian is logged and `continue`d; `processExpiredSecret` then returns `nil`, so the settlement-queue entry is dequeued and the secret is marked terminal. Consequences of a transient failure:

- A locked bond can be **stranded forever** (secret is terminal, never re-examined — the due-height queue design guarantees this).
- A no-reveal **slash can be silently skipped** (guardian keeps a bond it should have forfeited).

This is the inverse of the care taken at the queue-drain level, where a failed entry is deliberately retained for retry next block. The no-stranded-bonds invariant (spec.md Economic Invariants; proven in `no_stranded_bonds_test.go`) holds only because tests never inject bank-level failures.

**Framing note (July 2026)**: there are no *transient* errors on this path — settlement is pure deterministic computation over committed state (node-local failures like disk I/O surface as panics and isolate that node via AppHash divergence; they never reach these error returns). The bond accounting is exact-amount by construction (`BondAmount` frozen on the secret record, locks and unlocks use it verbatim, slash splits are remainder-based and sum to the bond exactly), so these error returns are pure **assertions**: they fire only when a bug elsewhere — a double-settlement, a missed lock, a bad migration, a refactor that recomputes instead of reading stored amounts — has already corrupted the books. Every node then fails identically; consensus enshrines the error rather than catching it. The design question is therefore not how to handle expected failures but how an assertion guarding against **unknown bugs** should fail: today it fails *open* (silent, permanent stranding); this plan makes it fail *safe* (loud, contained, self-recovering once a fix ships).

### 2. Genesis validation admits inconsistent state

`GenesisState.Validate()` checks ID uniqueness, threshold positivity, reveal-block ordering, key lengths, and `locked ≤ total`. It does **not** validate:

- secret `state` is a legal FSM state,
- `threshold ≤ shares`,
- reward pool / bond amounts are valid coins,
- side-store entries (shares, assignments, reveals, payloads) reference existing secrets and guardians,
- counter consistency (`accepted_count` / `revealed_count` vs the side-stores),
- due-queue entries reference live secrets.

A hand-crafted or migration-produced genesis can import state the runtime would treat as impossible. Because the invariant library is **test-only** (`invariants_test.go`), nothing catches it at import time. This matters most at the exact moment genesis files are hand-assembled: testnet launch and chain upgrades.

### 3. Silent zero-key fallback

`prepareGuardianInfoResponse` substitutes a 32-byte zero public key when a selected guardian record is missing ("shouldn't happen"). A creator would then encrypt a share to an all-zero key. This masks an invariant violation with silently-broken cryptography; it should be a hard error.

### 4. Stale dead code

`msg_server_confirm_shares.go:190–215` contains `HandleTimeouts`, a placeholder with `_ = currentHeight` and TODOs referencing a BeginBlocker that will never exist (timeouts are handled by the due-height queues). Also: `ValidateStateTransition` in `secret_state_machine.go` duplicates the FSM edge map and is unused by the transition path; `isValidUUID` recompiles its regexp on every call in a consensus path; `module.go` registers the module in `BeginBlockers` (app config) with no BeginBlock implementation.

## How

### Phase 1 — Settlement atomicity (the substantive fix — ruled July 2026)

**Ruled: Option A, implemented with per-secret atomic commits rather than an idempotence audit.** Each secret's entire settlement runs inside a `CacheContext`, with the writes committed only if every step succeeded:

- **All-or-nothing per secret**: a failure at any step discards the partial state wholesale — there is never a half-settled secret, retries are trivially safe, and no settlement operation needs auditing for re-runnability (the fragile part of Option A as originally drafted, which would have had to stay correct through every future refactor). Option B (per-guardian settlement records) is **rejected**: more state and a proto change buying nothing the cache-commit does not already provide.
- **Quarantine, never halt**: on failure the queue entry is retained (the drain layer's existing semantics) and retried every block. Deliberately no panic — an invariant panic in EndBlock halts every node deterministically, converting any attacker-reachable trigger into a chain-wide DoS. A stalled settlement leaves the funds **locked, not lost**, in module escrow; the blast radius is one secret, and a poisoned entry re-scans each block without blocking its queue neighbours. When an upgraded binary ships the underlying fix, the pending retry completes and the books balance with zero migration work.
- **Alarm from the first failure — the alarm *is* the detection mechanism**: because these failures are deterministic, retry alone would be a silent liveness failure. Emit a `settlement_stalled` event (secret id, failing operation, error) on every failed attempt, log at error level, and expose a stalled-settlement count via telemetry/query.
- **Same treatment for the other fail-open sites**: `ReleaseAllAcceptedBonds` (cancellation and commit-timeout paths) logs-and-skips identically today. In `MsgCancelSecret`, propagating the error gets atomic abort for free (the transaction fails; the creator retries); in commit expiry, apply the same EndBlock cache-commit + retain-and-retry pattern as settlement.

Add a conformance test that injects bank-keeper failures (wrap the bank keeper in a fault-injecting decorator in tests) and asserts: no partial state is committed, the queue entry is retained, the `settlement_stalled` event is emitted, the invariants hold throughout, and settlement completes cleanly once the fault clears.

### Phase 2 — Genesis validation

Extend `GenesisState.Validate()` with the structural checks above, and add an import-time invariant sweep: after `InitGenesis`, run the same assertions the invariant library runs (extract the invariant functions from `invariants_test.go` into a non-test package, e.g. `keeper/invariants.go`, so both tests and genesis import can call them; keep them out of per-block execution). Add a direct export→import→export round-trip equality test.

### Phase 3 — Small fixes

1. Replace the zero-key fallback with an error return.
2. Delete `HandleTimeouts` and the unused `ValidateStateTransition` map (or wire the latter into a test asserting it matches the FSM).
3. Hoist the UUID regexp to a package-level `var`.
4. Remove the module from `BeginBlockers` in `app_config.go` (or document why the inert entry stays).

Each phase must update spec.md where behaviour is clarified (settlement retry semantics belong in the "Settlement trigger — due-height queues" section).

## Decision log (ruled by the owner, July 2026)

1. ~~**Settlement option A vs B**~~ **Ruled: Option A, via per-secret `CacheContext` atomic commits** (Phase 1). The cache-commit removes Option A's idempotence-audit burden — the only reason B existed — so B is rejected as extra state and a proto change buying nothing.
2. ~~**What should a *permanently* failing settlement do?**~~ **Ruled: retry forever, alarm from failure #1 — no N-retry escalation threshold.** Every failure at these call sites is a deterministic bug manifesting, so no retry count distinguishes "permanent" from "transient" (there are no transient errors on this path), and no stronger on-chain action exists to escalate to anyway. Visibility from the first failure (`settlement_stalled` event + error log + stalled count) is the entire mechanism; one stalled settlement on any dashboard or explorer is the signal. Funds stay locked-not-lost until a software upgrade fixes the underlying bug, at which point the pending retry self-recovers.
3. ~~**Runtime invariant assertions**~~ **Ruled: keep the invariant sweep out of mainnet consensus and out of per-block execution entirely.** Run the extracted library at the three moments state is at wholesale risk: after `InitGenesis`, inside upgrade migrations at the upgrade height, and continuously in the devnet/e2e suites. No build tags, no node flags, no per-block canary.
4. ~~**Genesis strictness**~~ **Ruled: hard halt on import-time invariant failure.** Pre-launch there is no legacy state to be gentle with, and a genesis known to be inconsistent must never produce blocks. If migration development ever needs a soft mode, it is an explicit dev-only flag — never the default.

**Net shape of the work**: propagate errors instead of swallowing them, wrap per-secret settlement (and commit expiry) in a cache-commit, emit one new event, extract the invariant functions into a non-test package, and extend genesis validation. No new protocol state, no proto changes, and no behaviour change on any healthy path.
