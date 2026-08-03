# Guardian Selection Scalability — Plan

*Remove the full-registry walk from `MsgUserRequestGuardians`, which is metered
inside the **creator's** declared gas and already breaks secret creation at
fewer than a hundred registered guardians. Replace it with an eligibility index
so the cost tracks the *eligible* set rather than every registration ever made,
and state the resulting complexity in a new `PERFORMANCE.md`.*

> **Status: done — merged 29 July 2026 as PR #116 (`worktree-selection-index`).**
> Refined and ruled the same day; no open questions. Scope confirmed by the
> owner: the index carries a filter payload (§4), dead registrations are handled
> without any pruning mechanism (§4), no target guardian count is fixed (§1), and
> the whole plan lands as **one PR** with the phases as ordered commits (§9).
> Execution measured three things the plan had predicted wrongly — all recorded
> in place below (§1 the residual boundary, §4 dead-registration handling and the
> state migration, §6 what the gas-model test actually guards).
>
> **Priority**: **P1 — creator-facing liveness, pre-testnet.** Not the P2
> "scale readiness" it was filed as. Measurement on 29 July 2026 put the
> failure boundary at **~62–79 registered guardians**; the devnet already runs
> 24. This gates testnet, where open guardian registration is the whole point.
>
> **Origin**: automated review, July 2026. Re-scoped 29 July 2026 after
> validating [`PROTOCOL.md` Security Observations §3](../../CHAIN_MECHANICS.md#3-phase-1-gas-grows-with-the-eligible-guardian-set-and-the-creator-declares-it)
> by measurement, which contradicted this plan's own premise (§1) and answered
> two of its open questions.
>
> **Components**:
> - `x/secrets/keeper/guardian_selection.go` — candidate enumeration
> - `x/secrets/keeper/keeper.go` — `GetActiveGuardians`, `SetGuardian` (the
>   index's single write choke point), collection declarations
> - `x/secrets/keeper/genesis.go` — index built on import, exported as derived
>   state or not at all
> - `x/secrets/module/module.go` — `ConsensusVersion` bump and the module
>   migration that rebuilds the index on an in-place upgrade (§4); genesis alone
>   is not enough
> - `x/secrets/types/` — new store prefix and the index value type
> - **`typescript-sdk/src/protocol/constants.ts`** — `REQUEST_GUARDIANS_GAS_BASE`
>   and the per-share term are fitted to the *old* cost and must be re-fitted;
>   `gas-model.test.ts` and `testdata/vectors/tx_gas.json` re-measured. **This is
>   the component the plan previously omitted, and it is the one that decides
>   whether creators can transact.**
> - `mobile-client/` — consumes the SDK model through the vendored tarball; no
>   code change, but a repack is required or the app ships the stale model
> - `docs/PROTOCOL.md` — Inefficiencies §1 closes; Security Observations §3 is
>   NARROWED and stays open, its growth term changing from registered to eligible
>   guardians rather than disappearing
> - `docs/spec.md` — **no normative change** (§4 argues why); the
>   enumeration-order-independence claim becomes load-bearing and gets a
>   cross-reference
> - `docs/PERFORMANCE.md` — new
> - Tests — keeper (set equality, gas slope regression, genesis), SDK gas model

## 1. Why — corrected by measurement

This plan was filed on the belief that the walk was **unpriced**: *"gas does not
currently price this walk per-guardian, so the cost is borne by every validator
regardless of what the creator pays — a mild griefing asymmetry."*

**That is wrong, and it inverts the severity.** The walk runs inside
`MsgUserRequestGuardians` (`msg_server_request_guardians.go:205`), there is no
`InfiniteGasMeter` anywhere in `x/secrets`, so every store read is metered
against the creator's transaction. It is not a diffuse tax on validators. It is
a **hard failure boundary for creators**, and the creator pays the full declared
fee for the transaction that fails, because the ante handler deducts
`gas_limit × gas_price` with no refund.

Measured on 29 July 2026 through the existing `measureGas` harness, sweeping the
registry at a fixed band:

| Registered guardians | Phase-1 handler gas |
|---|---|
| 36 | 141,711 |
| 100 | 275,599 |
| 200 | 484,799 |
| 400 | 903,199 |

**2,092 gas per registered guardian, exactly linear** (the slope is 2092.0 at
every interval). At the 36-guardian devnet the model was fitted on, the walk is
75,312 gas — **53% of phase-1 handler gas is already the registry scan**.

The SDK declares `250,000 + 5,500 × max_shares` against a fit of
`196,949 + 4,381 × max_shares`, so headroom is `53,051 + 1,119 × max_shares`.
Creation therefore begins failing out-of-gas at:

```
N > 36 + (53,051 + 1,119 × max_shares) / 2,092
```

**~62 guardians at the narrowest band, ~65 at a typical 7, ~79 at the ceiling of
32.** Conservative in two ways: the slope is pure store gas with no bank work,
so the mock-bank under-read that `tx_gas.json` warns about does not apply to it;
and real records carry a populated `monitor_name`, which adds ~6 gas per byte to
the slope and moves the boundary *earlier*.

Registration is permanent — there is no deregistration, only stake withdrawal —
so the walked set never shrinks. Dead guardians are charged to every future
creator forever.

**No target guardian count is fixed**, deliberately. Urgency is now measured
rather than estimated, so the remaining question would be one of capacity, and
committing to a figure invites designing to it.

What Phase 0 plus the index actually buys is **hundreds of ELIGIBLE guardians,
not thousands**: measured after execution, the declared model's margin is
exhausted at roughly 139 eligible at `max_shares` 2, 154 at 7 and 228 at 32,
because the base still carries 360 gas per eligible guardian. Band for band that
is a 2.2–2.9× improvement on the 62–79 *registered* boundary it replaced — less
than the 5.8× drop in per-guardian cost, because the same re-measurement lowered
the declared base by a third — and a change of driver
— healthy participation rather than dead registrations accumulating forever — but
it bounds the problem rather than closing it (§8). `PERFORMANCE.md` §3 states the
measured complexity and lets an operator draw their own line.

**Two supporting facts survive unchanged**: the due-height queues already solved
the settlement-scan equivalent, so "index what consensus reads" is the house
pattern rather than a novelty; and there is still no `PERFORMANCE.md`, so none
of this is stated anywhere a node operator or auditor would look.

## 2. Phase 0 — remove the duplicate read, as the first commit

`GetActiveGuardians` walks the collection and then, for each entry the walk has
**already decoded**, calls `IsGuardianActive(address)`, which re-reads and
re-decodes the same record to compare two integers (`keeper.go:544-549`,
`keeper.go:509-525`).

Measured at 200 registered guardians: **2,095 gas per guardian as it stands, 564
walking once — the duplicate read is 73% of the cost.** `PROTOCOL.md`
Inefficiencies §1 says this "halves the constant factor"; it is a **3.7×**
reduction.

Because it also lowers the base the SDK's fit was taken at, removing it moves
the failure boundary from ~65 to roughly **~240 registered guardians** against
the declared model unchanged.

It is a two-line inline comparison with no behaviour change and no state change,
so it needs no migration and no spec involvement. It leads the branch as its own
commit for two reasons: it is the change a bisect should be able to isolate, and
it means the index's differential test (§4) compares against a walk that is
already correct rather than one being fixed in the same diff.

`IsGuardianActive` itself stays — other call sites hold only an address. It is
this call site that is wasteful.

## 3. Phase 1 — what is measured, and what still is not

**Gas is now measured; this phase no longer sets urgency.** The original phase
proposed benchmarking at 100/1k/10k/100k registered guardians to decide whether
the plan was urgent. Its *lowest* rung, 100, is already past the point where
every creator's phase 1 fails, so that ladder cannot calibrate anything — it
starts beyond the cliff.

What remains genuinely unmeasured is **wall-clock and block-time**, a different
question from gas and not answered by the table in §1:

- `SelectGuardians` execution time at 1k / 10k / 100k eligible candidates,
  including the SHA-256 ticket per candidate and the full sort — none of which
  is metered, so gas says nothing about it.
- Block-time impact of a burst of concurrent creations, which is where unmetered
  CPU becomes a liveness question rather than a fee question, and where this
  plan touches [Security Observations §1](../../CHAIN_MECHANICS.md#1-endblock-settlement-work-is-unmetered-and-uncapped),
  the absent per-block gas ceiling.

Deliverable: a Go benchmark alongside the gas regression test of §6, with the
numbers landing in `PERFORMANCE.md`.

## 4. Phase 2 — the eligibility index

### Why this is not a protocol change

`spec.md` pins the eligibility predicate and the sortition as
"consensus-critical and byte-exact". The index must therefore reproduce the
**same candidate set** — and the licence to reorder enumeration is already in
the spec: *"the outcome is independent of candidate enumeration order and stable
under pool changes"*, because `ticket(g) = SHA256(seed ‖ address)` depends on
nothing but the seed and the guardian's own address.

So the correctness property is **set equality, not ordering**. The original
phase's worry about iterating "in key order into the candidate list before
shuffling" was aimed at the wrong invariant: an index that yields the same set
in any order yields a byte-identical selection. That spec property is
load-bearing from here on and should be cross-referenced as such.

This is not a fresh argument: the hardening plan adopted sortition partly *for*
this, recording that order-independence "is what lets the
`GUARDIAN_SELECTION_SCALABILITY` plan later change candidate *enumeration* (an
eligibility index) without changing selection *outcomes*"
([`DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md`](DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md)).
The groundwork for this index was laid deliberately.

No normative change follows. The index is derived state: `InitGenesis` builds it
as a side effect of writing guardians, so genesis needs no import loop of its
own.

**An in-place upgrade does need one**, which the plan originally missed. An
upgrade never runs `InitGenesis`, so without a migration a chain whose store
predated the index would resume with it EMPTY and fail every
`MsgUserRequestGuardians` with `insufficient guardians: need N, have 0` — total,
and invisible in a diff. The module therefore bumps to `ConsensusVersion` 2 and
registers a 1 → 2 migration that clears and re-derives the index. Clearing rather
than upserting makes it idempotent and lets it repair drift as well as absence.
No `StoreUpgrades` entry: the prefix lives inside the existing `secrets` store.

### The range bound

Index on `available_until`, and range-read from **`reveal_end_block`, not
`now`** — the binding clause of the predicate is
`available_until ≥ reveal_end_block`, which is strictly tighter than
`available_until ≥ now`. A one-year secret then enumerates only guardians whose
window actually reaches a year out, which is both the case the strict gate fails
on today and the case operators find most confusing.

Membership also carries `accepting_secrets = true`, mutated rarely.

### Shape: a `Map` carrying the filter fields

An index is not unconditionally cheaper, and the arithmetic decides the shape.
From the measured constants (`ReadCostFlat 1000`, `ReadCostPerByte 3`,
`IterNextCostFlat 30`, and a guardian record of ~178 bytes key+value, which
reproduces the measured 564 and 2,095 exactly):

| Shape | Cost per entry enumerated | Scales with |
|---|---|---|
| Phase 0 only — walk, no second read | **564** | *registered* |
| `KeySet` index + a record `Get` per hit | **~1,750** | *eligible* |
| **`Map` index carrying the filter fields** | **~336** (measured 360) | *eligible* |

A `KeySet` index would be **a bet on the exclusion ratio**: at ~1,750 per
candidate against 564 per registration, it must exclude **~68% of registrations**
merely to break even, so on a healthy network where most guardians are accepting
and long-dated it would be *slower than doing nothing*. That is the failure mode
this plan would otherwise have shipped, and it is why the shape is ruled rather
than left to implementation taste.

**The index is therefore a `Map` whose value carries `available_from`,
`active_bond_count`, `bond_k` and the float** — everything the per-secret filter
needs, so a candidate is judged without reading its record at all. That is
strictly better than the walk even at zero exclusion (~336 against 564), and it
is the only shape whose cost tracks the eligible set unconditionally.

The price is write churn: the value must be rewritten whenever the float moves.
That is affordable, with numbers rather than assumptions — an index `Set` is
~5,060 gas, while acceptance measures 48,144 of a 100,000 handler budget at the
band ceiling (`guardian_gas_test.go`), so there is ample room. Settlement writes
float in `EndBlock`, which is unmetered: real work, no fee consequence, and one
more entry for `PERFORMANCE.md`.

### One write choke point

Every guardian record write in the module funnels through `SetGuardian`
(`keeper.go:490`) — genesis, registration, update, rotation, withdrawal and all
five float mutations in `guardian_float.go`. The index is maintained **there**,
not at the eight call sites.

This costs a read of the previous record inside `SetGuardian` to retire a stale
index key when `available_until` changes. It is worth paying: an index
maintained at the choke point cannot drift, whereas one maintained at call sites
drifts the first time someone adds a ninth writer — and a drifted eligibility
index is a consensus fault, not a performance bug.

### Dead registrations: never read, never pruned

A lapsed guardian's entry sorts below `reveal_end_block` and is therefore never
visited: measured, 400 lapsed registrations add **exactly zero** gas. No proto
change, no new state, no operator action.

**There is no pruning step, and there must not be one.** The range read starts at
`reveal_end_block`, so lapsed entries are never enumerated and so cannot be
deleted on the way past — and entries below the range start must not be deleted
anyway, since a window too short for *this* secret is still perfectly valid for a
shorter-dated one. Deleting on a read path would also mean **reads mutate state**:
harmless while the only caller is a message handler, and a consensus divergence
the moment such a helper were reused from a gRPC query, which runs per node
outside consensus.

The cost is storage: one entry per accepting guardian, forever, mirroring a
`Guardians` collection that also never shrinks. Disk, not gas.

An explicit "dormant" flag with a re-activation flow was considered and rejected:
it is protocol-visible surface, and a new operator-facing state, for a problem
the range bound already solves invisibly.

### What this does not close

**The index bounds the walk by the eligible set; it does not make selection
constant-time.** Sortition must see every candidate's ticket to find the lowest
`max_shares`, so a network with 10,000 healthy, accepting, long-dated guardians
still enumerates 10,000 entries for a 3-guardian secret. The index solves
**dead-registration accumulation**, which is the problem the protocol actually
has (permanent registration, no deregistration); it does not solve **large
healthy set size**.

Bounding enumeration below the eligible set means sampling a subset of
candidates, which changes the byte-exact eligibility predicate and the
equal-probability claim that rests on it. That is a protocol change and out of
scope here: this plan indexes selection on eligible guardians and stops there.
Noted so the boundary is explicit, not as work in waiting.

**Enumeration cannot terminate early**, which is why the index narrows *what* is
read rather than *how much*. Sortition takes the lowest `max_shares` tickets
across all candidates, so every ticket must be computed; stopping at the first
`max_shares` found would select by store order, which is bech32 address
ascending — and guardian addresses are self-chosen, so that is offline-grindable
by exactly the route `creatorAddress` was removed from the seed to close
([ACCEPTED_TRADEOFFS.md §5](../../CHAIN_MECHANICS.md)).

## 5. Phase 3 — bound the remaining walks

Audit every `Walk` over `Guardians` and `Secrets` outside query paths: convert
to index reads or document why the call site is genuinely cold. Query-path walks
get pagination — most already have it via the collections helpers; verify
`PendingSecrets`' filtered paginate does not over-scan.

The audit starts from `GetActiveGuardians` alone.
`GetGuardiansWithMinStake` and `GetGuardiansByAvailabilityWindow` had the same
shape and were **resolved by deletion on 29 July 2026**: neither had a caller
anywhere in the repo, tests included, and `GetGuardiansWithMinStake` encoded the
retired minimum-stake model — the bonded design has no stake floor, only
per-secret bond affordability. Deleting beat bounding, because there was nothing
to bound and leaving them invited a future caller to reintroduce exactly the walk
this plan removes.

`genesis.go:154` walks the whole guardian set on export. That one is correct and
stays: export is not consensus-hot and must be exhaustive by definition.

## 6. Phase 4 — re-fit the SDK gas model, and pin it

**The plan is not done when the chain gets faster.** Creators declare gas from
the SDK's fitted model, so until it is re-measured they keep paying the old
figure. `gas-model.test.ts` does bound the margin on both sides (1.1× and 1.6×
of a measured point), so it flags waste as well as shortfall — but only against
the corpus it is given, so it cannot fire until the corpus itself is
re-measured.

- Re-measure `testdata/vectors/tx_gas.json` against a live devnet, recording
  `registered_guardians` as the conditions block already requires.
- Re-fit `REQUEST_GUARDIANS_GAS_BASE` and the per-share term, keeping the ~25%
  margin discipline and the existing instruction not to widen the margin to
  absorb a regression.
- Repack the vendored SDK tarball, or the mobile client ships the stale model.

**Add the regression test that was missing.** Nothing in the Go suite pinned gas
against registry size, which is why a growth term worth 53% of the budget
reached a shipped default unnoticed. A keeper test that measures phase-1 gas at
two registry sizes and asserts the **slope** — not the absolute — is the guard
that generalises, because it fails on any future call site that reintroduces
per-guardian work regardless of how the base moves.

## 7. Phase 5 — PERFORMANCE.md

Per-operation complexity table (message handlers, EndBlock, queries) with the
state-growth story: what grows forever (guardians, tombstoned secrets after the
retention plan, the hint feed), what is pruned, and the measured numbers from §1
and §3. Include the storage-per-secret figure (slim record + side-stores +
payload ≤ 4,216 B) so node operators can capacity-plan, and state which
operations are **unmetered** — `EndBlock` settlement work and the sortition CPU
— since that is precisely what a gas table hides. Audit-package material
(`SECURITY_AUDIT_READINESS` Phase 3).

## 8. What this plan does not solve

- **Large healthy-set enumeration** — §4 states the bound. Selection stays
  O(eligible), and going below that is a protocol change with its own plan.
- **Detecting index drift at runtime** — the sweep runs at genesis and the
  migration rebuilds at an upgrade; nothing watches the span between. Judged not
  worth solving: drift needs a bug that bypasses the write choke point, escapes
  CI and the invariant sweep, and ships. Prevention is cheaper than detection, so
  `make verify` now fails if `Guardians.Set` appears outside `SetGuardian`, which
  closes the one real hole — invariant 8 runs only in tests that call the sweep
  and at genesis, so a new writer with its own test would have shipped. The
  runtime-detection design was written out and rejected the same day; the
  error-modality catalogue and the argument against halting on derived-state
  faults are kept in
  [`done/REJECTED_DERIVED_STATE_REPAIR_PLAN.md`](REJECTED_DERIVED_STATE_REPAIR_PLAN.md).
- **The declared-gas boundary** — measured at roughly 139–228 *eligible*
  guardians depending on band (§1). Widening the margin is the wrong response:
  Cosmos charges the declared limit, so it taxes every creator to defer a
  boundary. The fix is a declaration that scales with the eligible count. `Query/Guardians`
  returns every field the predicate needs, but there is no eligible-count query
  and no server-side filter, so the client pages the registry and filters itself — a client-model change needing its own plan, and the
  successor this work hands off to.
- **Unmetered CPU and the absent per-block gas ceiling** — `PERFORMANCE.md`
  documents it; [Security Observations §1](../../CHAIN_MECHANICS.md#1-endblock-settlement-work-is-unmetered-and-uncapped)
  owns it.
- **Bespoke gas pricing for candidates scanned** — the original plan asked
  whether phase 1 should charge per candidate. It already does, implicitly and
  fully, via metered store reads; that was the discovery of §1. Adding a bespoke
  charge on top would double-price the same work, so the question is **closed,
  not deferred**.
- **Hint-feed growth** — `HintsByCreation` grows one entry per secret forever
  and every recipient poller scans it from a cursor. A real unbounded-growth
  concern, and **not this concern** (one plan, one concern): different
  collection, consumer and mutation point, and it interacts with the retention
  plan's pruning rather than with selection. It needs its own plan, or an
  addition to the retention work that already owns tombstoning.

## 9. Delivery

**One PR from one worktree**, with the phases as ordered commits: Phase 0
first (§2), then the index, then the remaining walks, the SDK re-fit and
`PERFORMANCE.md`. Phase 0 is separable by bisect without being separable by
review, which is the property that matters — the creator-facing break and the
state-shape change land together, so there is no window in which the
re-fitted gas model describes a chain that only half exists.

Run `make verify` and the keeper suites locally before raising it, and hold the
PR open until CI is green (planning README, Execution).

[`DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md`](DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md)
**is complete**, so the sequencing constraint this plan carried — "do hardening
first, it changes the shuffle" — is discharged. Both plans rewrite the same
function; the shuffle is settled and this plan changes only how candidates are
enumerated into it.
