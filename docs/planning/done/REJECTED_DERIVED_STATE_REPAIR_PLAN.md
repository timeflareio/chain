# Derived-State Integrity & Repair — Plan *(rejected)*

*Detect and repair drift in the module's derived stores — indexes, queues, feeds
— without halting the chain. Drift is caused by code, not by attackers, so the
right response is a report and a rebuild, never a liveness failure for everyone.*

> **Status: REJECTED — 29 July 2026, the day it was proposed.** Retained for the
> error-modality catalogue (§1) and the argument against halting on derived-state
> faults, both of which stay true; the *work* was not warranted.
>
> **Why rejected.** It preemptively built machinery for a fault with no instance.
> Drift needs a bug that bypasses the write choke point, escapes CI and the
> invariant sweep, and ships — a compound improbability — and the plan answered it
> with rebuilders for six stores, a query endpoint, dashboard surfacing, a
> Prometheus gauge and a runbook. It also contradicted a rule applied in the same
> work it came from: `GetActiveGuardians` was deleted for being unused code that
> invites reintroducing what it does, and this plan proposed five rebuilders
> nothing would call.
>
> The plan's own conclusion undercut it. Having argued that repair belongs in the
> upgrade path rather than a halt, what remained was detection — and detection is
> the wrong layer. **Drift is caused by code, so it is cheapest to prevent at
> review time**, which is what shipped instead:
>
> - **`verify-choke-points`** in `make verify` fails if `Guardians.Set` appears
>   outside `SetGuardian`, in the mould of `verify-boundaries`. That closes the one
>   real hole — invariant 8 runs only in tests that call the sweep and at genesis,
>   so a new writer with its own test would have shipped.
> - **A strict-gate error that explains itself**, so an empty or under-populated
>   index reports the cause rather than a bare `insufficient guardians`. Worth
>   having regardless of drift.
> - **`docs/upgrades.md`** already carries the rule for new derived state, and the
>   `x/secrets` 1 → 2 migration already rebuilds the eligibility index.
>
> Both landed in
> [`DONE_GUARDIAN_SELECTION_SCALABILITY_PLAN.md`](DONE_GUARDIAN_SELECTION_SCALABILITY_PLAN.md)'s
> execution (PR #116). Nothing here is orphaned.
>
> **If this is ever revived**, the trigger is evidence rather than caution: an
> actual drift incident, or a derived store gaining a writer that cannot go
> through a choke point. §5's open questions are the right starting point, and Q1
> (whether detection should be a query at all, given it exposes an
> `O(registered)` walk) remains the sharpest.
>
> **Priority**: was P2. **Origin**: owner ruling, 29 July 2026 —
> *"I want to favour graceful self-heal almost always over halts"* — arising from
> the guardian eligibility index. The anti-halt principle was accepted and is
> reflected in what shipped; the plan built to serve it was not.
>
> **Components**: see the original list below; none were touched.

## 1. The error modality

**What can drift.** Every store the module maintains *alongside* its authoritative
records, and therefore has to keep in step by hand:

| Store | Derived from | Drift symptom |
|---|---|---|
| `GuardianEligibility` | `Guardians` | **Silent and total** if empty: every `MsgUserRequestGuardians` fails `insufficient guardians: need N, have 0`. **Silent and partial** if stale: selection considers a guardian under an availability window, float or bond count it no longer has — a consensus-relevant difference in *who can be selected* |
| `GuardianKeyIndex` | `GuardianKeyHistory` | A retired key stops being reserved, so a key could be re-registered; or a live key looks taken |
| `CommitQueue` / `SettlementQueue` | `Secrets` | A missing entry means a secret never finalises or never settles — bonds stay locked, the creator is never refunded. Silent until someone notices funds are stuck |
| `PruneQueue` | terminal `Secrets` | Retention stops; state grows. Benign, slow |
| `SecretsByCreator`, `HintsByCreation` | `Secrets` | A creator's secret disappears from their list; a recipient's scan misses a secret it should have found |

**What causes drift — and what does not.** In every case the authoritative record
is correct and the projection is not. That requires either a **writer that
bypassed the choke point** (the class that already occurred twice in test
fixtures during the eligibility work, caught by invariant 8) or an **incomplete
migration** (derived state absent after an in-place upgrade, since an upgrade
never runs `InitGenesis`).

Neither is attacker-reachable. There is no message a user can send that
desynchronises a projection from its record; it takes a code change to do that.
**That is the fact this plan turns on.**

**Why halting is the wrong response.** `x/crisis`-style invariant enforcement
converts a bookkeeping fault into a chain-wide liveness failure. Two reasons that
is a bad trade here:

- **There is no attack to stop.** A halt's justification is preventing an
  attacker from profiting from inconsistent state. Drift is not attacker-caused,
  so the halt buys nothing and costs everyone their liveness — guardians included,
  who are slashable for reveals they cannot submit against a halted chain.
- **Detection is O(registered).** The sweep walks every guardian and every index
  entry. Running it often enough to bound the exposure window would reintroduce
  exactly the per-registration cost the eligibility index was built to remove,
  unmetered, in `EndBlock`.

`x/crisis` is also **not currently wired into the app**, so `RegisterInvariants`
is a no-op stub; enforcement-by-halt would mean adding a module, not flipping a
switch.

## 2. What already exists

- **`CheckStateInvariants`** covers invariants 3–8 and runs after `InitGenesis`,
  where an inconsistent genesis halts rather than producing blocks. That halt is
  right: nothing is live yet, so there is no liveness to lose.
- **`RebuildEligibilityIndex`** — clear and re-derive, idempotent, proven against
  four drift shapes (empty, orphan entry, stale projection, key at a superseded
  window) plus a clean invariant sweep after each.
- **The `x/secrets` 1 → 2 migration** invokes it, so an upgrade rebuilds.
- **`docs/upgrades.md`** states the general rule for new derived state.

**The gap**: nothing detects drift between one genesis and the next, and nothing
repairs it without a binary upgrade.

## 3. The shape

**Detection is off-chain and read-only.** The invariant sweep already returns the
first violation with a message naming which writer is implicated. Exposing it as a
**query** — not a consensus check — means a node operator, the guardian dashboard
or a monitoring probe can run it on whatever schedule they like, at their own
cost, with no block-time impact and no possibility of a halt. A read-only query
cannot diverge because it writes nothing.

The guardian dashboard already polls per-guardian JSON endpoints and has an
"unavailable says why" convention, so a detected fault has somewhere honest to
appear.

**Repair happens through the upgrade path, deliberately.** This is the part worth
arguing rather than assuming, because a governance-gated "repair now" message
looks attractive and is, on inspection, mostly surface:

> Drift is caused by a code bug. A code bug needs a new binary. **A repair channel
> that avoids an upgrade is therefore only useful for the window between noticing
> the fault and shipping the fix it already requires** — and in that window the
> rebuild would be undone by the same bug that caused the drift.

So the pathway is: **detect off-chain → keep producing blocks → fix the code →
the upgrade's migration rebuilds.** No halt, no standing authority to rewrite
state, no new message type.

What that does need is for rebuilds to be **cheap enough to run on every
upgrade** rather than only when a version gap demands it — so an upgrade shipped
for an unrelated reason also repairs anything that has drifted. Rebuilds are
`O(records)` at a single height on a chain that is already halted for the
upgrade, which is the one moment that cost is free.

**Canonical rebuilders per store**, all in the shape already established:
idempotent, clear-then-re-derive (never upsert — an upsert repairs absence but
leaves stale entries, which is most of the drift), and asserted by the invariant
sweep at the end.

## 4. Implementation phases

1. **Rebuilders** — one per derived store in the table (§1), each idempotent and
   each tested against the drift shapes that store can exhibit, following
   `TestRebuildEligibilityIndex`.
2. **A `RepairDerivedState` keeper entry point** that runs every rebuilder then
   `CheckStateInvariants`, so an upgrade handler has one call to make and cannot
   forget a store.
3. **Detection query** — surface the sweep read-only (§5 Q1), with the violation
   message as its payload.
4. **Operator surfacing** — the guardian dashboard shows a detected fault; a
   Prometheus gauge makes it alertable.
5. **Runbook** — `docs/upgrades.md` gains "call `RepairDerivedState` from every
   upgrade handler"; a short operator note covers what to do when the query
   reports a fault (report it, keep running, do not restart into anything).

## 5. Open questions

**Q1 — Detection surface: a gRPC query, or a CLI-only debug command?**
*Recommendation: a query.* The dashboard and monitoring already speak that
protocol, and the check is read-only so it cannot affect consensus. The
counter-argument is that it exposes an O(registered) walk to anyone who can reach
the node's query port, which is a cheap denial-of-service lever; that argues for
CLI-only, or for the query being disabled by default. Worth a ruling because it
is a public-surface decision, not an implementation detail.

**Q2 — Should every upgrade call `RepairDerivedState`, or only upgrades that
change a derived store?** *Recommendation: every upgrade.* It is free at that
height, it makes repair automatic rather than remembered, and "only when needed"
is a judgement someone will get wrong. The cost is that an upgrade's diff no
longer tells you whether state was rewritten — mitigated by the rebuild being a
provable no-op when nothing has drifted.

**Q3 — Scope now, or the eligibility index only?** *Recommendation: all of the
stores in §1.* They share one mechanism, and the queues are the ones whose drift
strands funds. But it is a bigger change than the index alone, so the owner may
prefer to land the index's rebuilder (already done) plus the queues, and defer the
feeds.

**Q4 — Does a detected-but-unrepaired fault need to restrict anything?** For
example, refusing new secret creation while the eligibility index is known bad,
rather than failing every creation with a confusing "insufficient guardians".
*Recommendation: no protocol restriction, but a better error.* Restricting is a
halt in miniature; a clear message costs nothing.

## 6. What this plan does not solve

- **Drift caused by state corruption below the module** (disk, IAVL) — that is a
  node-operations problem, addressed by resync from peers, not by rebuilding a
  projection.
- **Detecting drift the invariants do not model.** The sweep proves the
  properties it encodes and nothing else; a projection with no invariant is
  unprotected, which is why a new derived store must arrive with one.
- **Halting as a policy question elsewhere.** This plan argues against halts for
  *derived-state* faults specifically, on the grounds that they are not
  attacker-reachable. Faults that are attacker-reachable are a different
  argument, and [`PROTOCOL.md` Security Observations §1](../../CHAIN_MECHANICS.md#1-endblock-settlement-work-is-unmetered-and-uncapped)
  owns the closest one.
- **`x/crisis`.** Not wired in, and not proposed here. If it ever arrives for
  other reasons, registering the sweep becomes a two-line addition — and the halt
  semantics it brings would need the ruling this plan declines to make.
