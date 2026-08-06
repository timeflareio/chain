# Timeflare Chain Mechanics

*How the chain is built inside — state layout, genesis behaviour, measured
costs — and the protocol's judgement ledger: costs consciously accepted,
deliberate oddities, known documentation drift, open concerns and defects.*

> **Scope**: [operations.md](operations.md) is the operation-level truth — what
> every transaction and block-driven operation does, its validation, side
> effects and token flows. [spec.md](spec.md) is the protocol authority — the
> rationale, economics and security model. [ECONOMICS.md](ECONOMICS.md)
> catalogues the fees themselves. This document holds what none of those
> should: the implementation's internals, its measured performance, and the
> running ledger of judgements about it. Open design work lives in
> [docs/planning/](planning/) and is referenced per item below.

## Table of Contents

1. [State & Storage](#state--storage)
2. [Performance & Scaling](#performance--scaling)
3. [The Judgement Ledger](#the-judgement-ledger)
   - [Design decisions — the questions readers ask](#design-decisions--the-questions-readers-ask)
   - [Accepted Trade-offs](#accepted-trade-offs)
   - [Load-Bearing Oddities](#load-bearing-oddities--deliberate-decisions-worth-revisiting)
   - [Deviations From Documented Intent](#deviations-from-documented-intent)
   - [Inefficiencies — Optimisation Candidates](#inefficiencies--optimisation-candidates)
   - [Security Observations](#security-observations)
   - [Open Defects & Cleanup Notes](#open-defects--cleanup-notes)

---

## State & Storage


All state lives in the unified `secrets` module using the Collections framework:
a slim metadata record per secret plus keyed side-stores, so that a hot-path
write touches one small key rather than rewriting the whole record.

| Collection | Key | Value | Notes |
|---|---|---|---|
| `Secrets` | secret ID (UUID string) | slim `Secret` record | Identity, timing, band (`threshold`, `min_shares`, `max_shares`), economics (`reward_pool`, `accept_fees`, `bump`, `guardian_bond_amounts` — one frozen bond per selected guardian, aligned with `selected_guardians`), commitment, state, the recipient `detection_hint` (the recipient's key is never stored), the per-secret public key `secret_public_key` (pk_s, set at distribution), denormalised `revealed_count`, `terminal_at`, and the rebate pair `rebate_amount` / `rebate_collected` (set at settlement / collection). Retired fields are proto-`reserved`: the embedded per-guardian data (9, 13–15), the pre-band share counts (8, 18) and the single flat `bond_amount` (20) |
| `SecretAcceptedCounts` | secret ID | int64 | **Hot**: the acceptance tally, split out of the record so an acceptance writes ~10 bytes instead of rewriting a roster that carries one address and one frozen bond per selected guardian (a 32-guardian accept burned ~94,000 gas on that rewrite alone — more than the protocol reimburses for the whole transaction). `GetSecret`/`SetSecret` join and split it transparently, so callers still read `Secret.accepted_count` |
| `SecretShares` | (secret ID, guardian addr) | `SecretShareData` | **Cold**: encrypted key share + HMAC, written once at distribution, never rewritten |
| `SecretAssignments` | (secret ID, guardian addr) | `AssignmentRecord` | **Hot**: status (`PROPOSED`/`ACCEPTED`/`REJECTED`) + `responded_at_block`, tiny record flipped once |
| `SecretReveals` | (secret ID, guardian addr) | `RevealedShare` | One write per reveal |
| `SecretPayloads` | secret ID | `StoredPayload` | **Cold**: the payload ciphertext `C`, written exactly once at distribution — the only copy, independent of guardian count; served by `Query/SecretPayload` |
| `Guardians` | bech32 address | `Guardian` record | Float (`Stake` = total, `LockedStake` = bonded), current-epoch encryption key + `current_key_epoch`, availability window, accepting flag |
| `GuardianKeyHistory` | (guardian addr, epoch) | `KeyHistoryEntry` | **Append-only** key epochs: `{public_key, effective_from_height}`; epoch 0 = registration (effective height + 1), +1 per rotation (effective next block); never overwritten or deleted |
| `GuardianKeyIndex` | public key bytes | holding guardian address | Global key-uniqueness index over **every key ever registered, all epochs, forever** — never shrinks; derived state, rebuilt on import from the histories |
| `GuardianEligibility` | (`available_until`, guardian addr) | `GuardianEligibility` projection | Selection's candidate source. Membership *is* `accepting_secrets`; the value carries `available_from`, `active_bond_count`, `bond_k` and `unlocked`, so the per-secret filter judges a candidate without reading its record. Ordering by `available_until` lets phase 1 range-read from `reveal_end_block` and never touch registrations that cannot serve the window. Maintained only through `SetGuardian` — the single write choke point, enforced by `make verify` — and asserted by invariant 9; derived state, rebuilt on import and by the 1→2 migration, not exported. The price is write churn: because the projection carries `unlocked`, **every** float movement rewrites the entry, and each write also re-reads the record (Inefficiencies §2) |
| `EarlyRevealSlash` | `"secretId:guardianAddr"` (colon-joined string, not a Pair) | bool | Guardians slashed for early reveal — excluded from settlement rewards *and* bond return |
| `CommitQueue` | (due height, secret ID) | — | Due-height queue; `due = commit_deadline + 1` |
| `SettlementQueue` | (due height, secret ID) | — | Due-height queue; `due = reveal_end_block + 1` |
| `SecretsByCreator` | (creator, secret ID) | — | Creator index, written once at creation; deleted at Stage 2 pruning |
| `HintsByCreation` | (created_at, secret ID) | `DetectionHint` | Discovery-scan feed, written once at creation; served in creation order by `Query/HintsSince` so recipients resume from a height cursor; deleted at Stage 2 pruning (discovery is bounded by the retention window) |
| `SecretCounter` | — | sequence | Monotonic per-request counter: assigns secret IDs and differentiates selection seeds; genesis-exported, never re-derived from the secret set |
| `SecretTombstones` | secret ID | `SecretTombstone` | **Permanent** (~180B each): the seven-field anchor written at Stage 2 pruning — digest of the canonical final record + final state + heights + creator + commitment; served by `Query/SecretTombstone` |
| `PruneQueue` | (due height, secret ID) | — | Retention due-height queue; `due = terminal_at + RetentionBlocks`; drained by EndBlock capped at `MaxPrunesPerBlock` |
| `RebateState` | — | `RebateState {allowance, accrued_height, reserved}` | The rebate mechanism's accrual bookkeeping: the unclaimed allowance, the height it was last accrued to, and the total credited-but-uncollected reservations. Consensus state — genesis-exported |
| `RebateCommitments` | (secret ID, collector addr) | `RebateCommitmentRecord {commitment, committed_at}` | Commit–reveal priority claims for rebate collection. A re-commit overwrites; the spent one is deleted at payment, the rest swept at Stage 2 pruning. Not exported — a collector re-commits after a restart-from-export |
| `RebateExpiryQueue` | (due height, secret ID) | — | Due-height queue; `due = terminal_at + RebateCollectionWindow()`; voids uncollected rebates |

One module account (`secrets`, `Burner` permission only) holds: guardian
floats (bonds are an accounting lock within the float, not a separate
balance), locked reward pools, slashed amounts in transit to
burn/creator/reporter, and the fee-split burn share in transit to burn
(entry fees go directly to the fee collector and never touch this account).
A second, **keyless** module account (`rebate_pool`, no permissions at all)
holds the recipient-rebate pool — 70% of supply at genesis, spendable only by
the rebate formula (crediting at settlement, payout at
`MsgRecipientCollectRebate` — see [operations.md](operations.md#recipient-rebate));
`RebateState.reserved` tracks
the portion already credited and awaiting collection, so the pool can never
promise the same uveil twice.

**Genesis** exports `Secrets` (with the acceptance tally joined back onto each
record, so the split is invisible across an export/import cycle),
`Guardians`, the four side-stores (shares,
assignments, reveals, payloads), `EarlyRevealSlash`, `SecretTombstones`
(permanent state), `GuardianKeyHistory` (a guardian with no exported entries
gets epoch 0 synthesised from its record on import — the pre-rotation
migration path), and the `SecretCounter` sequence (consensus state — a
chain restarted from export continues the ID/seed sequence rather than
reissuing IDs), and the `RebateState` accrual record (consensus state — above
all its `reserved` total, or credited rebates would be promised twice); the
queues (including the prune queue, rebuilt from terminal secrets'
`terminal_at + RetentionBlocks`, and the rebate expiry queue, rebuilt from
uncollected credited rebates on the same clock), creator index, key-uniqueness
index and eligibility index are derived and rebuilt on import. Rebate
commitments are **not** exported — a collector simply re-commits after a
restart-from-export. `GenesisState.Validate()` enforces
structural consistency (legal FSM states, `threshold ≤ min_shares ≤ max_shares`,
well-formed coins, side-store referential integrity, counter/side-store
agreement, tombstone/live-record exclusivity, key-history shape — contiguous
epochs, increasing effective heights, global key uniqueness, record/current
agreement), and `InitGenesis` finishes with the full state-integrity sweep
(`keeper.CheckStateInvariants` — nine invariants, the library it shares with the
test suites), **hard-halting** on any violation: an inconsistent genesis must
never produce blocks. `InitGenesis` is its **only** production call site: an
in-place chain upgrade does not run it, so derived state an upgrade introduces
needs its own module migration (§`ConsensusVersion` 1→2 rebuilt the eligibility
index for exactly this reason) rather than relying on this sweep to notice.

**Queries** serve both the slim record and an assembled `SecretView` whose field
numbers 1–20 deliberately match the pre-split `Secret`, so existing clients decode
responses unchanged (`query.proto`).


### State machine internals

Every secret transition — including `reconstructable` — is driven through the
FSM (`secret_state_machine.go`), and each transition is a **single write**:
`TransitionSecretState` mutates the caller's in-memory record, stamps
`terminal_at` on terminal entry, and persists exactly once (no read-back). The
state diagram and per-state semantics live in
[operations.md](operations.md#state-values). Terminal secrets are dequeued from
the commit/settlement queues and never revisited by them; **retention runs in
two stages**
([planning/done/DONE_TERMINAL_SECRET_RETENTION_PLAN.md](planning/done/DONE_TERMINAL_SECRET_RETENTION_PLAN.md)
carries the design rationale): at the terminal transition, Stage 1 deletes the
encrypted shares, assignment records and slash marks and schedules the prune;
at `terminal_at + RetentionBlocks` (~6 months), Stage 2 deletes everything else
— slim record, reveal records, payload ciphertext, creator-index and hint
entries, plus any leftover rebate commitments (releasing an uncollected
rebate's reservation) — replaced by a permanent `SecretTombstone` and an
archival event carrying the full canonical record.

### Message entry points

Every handler first verifies the transaction signer equals the actor address
named in the message. Behaviour is documented per operation in
[operations.md](operations.md); the code lives here:

| Operation | Entry (`x/secrets/keeper/`) |
|---|---|
| `MsgUserRequestGuardians` | `msg_server_request_guardians.go` |
| `MsgUserDistributeShares` | `msg_server_distribute_shares.go` |
| `MsgGuardianConfirmShares` | `msg_server_confirm_shares.go` |
| `MsgGuardianRevealShare` | `msg_server_reveal_share.go` |
| `MsgUserCancelSecret` | `msg_server.go` |
| `MsgGuardianRegister` | `msg_server_register_guardian.go` |
| `MsgGuardianUpdate` | `msg_server_update_guardian.go` |
| `MsgGuardianRotateKey` | `msg_server_rotate_key.go` |
| `MsgGuardianWithdrawStake` | `msg_server_withdraw_stake.go` |
| `MsgSlashGuardian` | `msg_server_slash_guardian.go` |
| `MsgRecipientCommitRebate` | `msg_server_commit_rebate.go` |
| `MsgRecipientCollectRebate` | `msg_server_collect_rebate.go` |
| EndBlock queues | `module.go` → `endblock_logic.go`, `rebate.go`, `retention.go` |
| BeginBlock fee split | `fee_distribution.go` |

---

## Performance & Scaling

*What each operation costs, what grows without bound, and what is metered. The
numbers here are measured, not estimated; where something has not been measured
this document says so rather than guessing.*

### 1. Metered and unmetered work — the distinction that matters

Cosmos meters **store access**. Reads and writes are charged to whoever's
transaction performs them (`ReadCostFlat` 1,000, `ReadCostPerByte` 3,
`WriteCostFlat` 2,000, `WriteCostPerByte` 30, `IterNextCostFlat` 30). Everything
else — hashing, sorting, arithmetic, encoding — is **free to the transaction and
real to the validator**.

Two consequences run through this whole document:

- **A growing term inside a transaction handler is a liveness boundary, not a
  tax.** Clients declare a gas limit from a fitted model
  (`typescript-sdk/src/protocol/constants.ts`). When metered work grows past the
  model's margin, the transaction aborts out of gas — and the fee is still
  deducted, because the ante handler charges `gas_limit × gas_price` with no
  refund on failure. This is why per-registration cost in phase 1 was a defect
  rather than an inefficiency (§3).
- **`EndBlock` work is unmetered entirely.** Nobody's transaction pays for
  settlement, commit expiry or pruning. That is a block-time question with no
  fee brake — the consensus block gas limit bounds transactions, never
  BeginBlock/EndBlock — see
  [Security Observation §1](#1-endblock-settlement-work-is-unmetered-and-uncapped),
  which owns the residual.

### 2. State that grows without bound

| Store | Grows with | Pruned? |
|---|---|---|
| `Guardians` | Every registration, forever | **No** — registration is permanent; there is no deregistration, only stake withdrawal |
| `GuardianEligibility` | One entry per *accepting* guardian | No, but lapsed entries are never **read** (§3) |
| `GuardianKeyHistory` | One entry per key epoch per guardian | **No** — append-only by design; every key ever used stays reserved, because share material encrypted to it may still exist |
| `GuardianKeyIndex` | Every key ever registered | **No** — same reason |
| `SecretTombstones` | One ~180 B record per pruned secret, forever | **No** — the permanent anchor that makes an archived copy self-authenticating |
| `HintsByCreation` | One entry per live-or-retained secret | **Yes** — the hint entry is deleted at Stage 2 retention pruning, so the discovery feed is bounded by the retention window (~6 months) |
| `Secrets` + side stores | Live secrets | **Yes** — staged retention pruning ([DONE_TERMINAL_SECRET_RETENTION_PLAN.md](planning/done/DONE_TERMINAL_SECRET_RETENTION_PLAN.md)) |
| `SecretCounter` | Monotonic, never reset | n/a — 8 bytes |

**Storage per secret**: the slim record plus side stores, plus a payload capped
at **4,216 B**. The record was deliberately slimmed (share envelopes, acceptance
tally and reveals live in their own keys) so that acceptance and reveal write one
small key instead of rewriting the whole record.

The unbounded stores are a **disk** cost, not a gas cost, provided nothing on a
hot path enumerates them. Keeping that true is the point of §3.

### 3. Guardian selection — O(eligible), and why it cannot be less

Candidate enumeration range-reads the `GuardianEligibility` index
(`(available_until, guardian_address)` → projection) from
`available_until >= reveal_end_block`, which is the binding clause of the
eligibility predicate. Registrations too short-dated for the secret — lapsed ones
included — sort below the range start and are never touched.

Measured phase-1 handler gas, sweeping the registry at a fixed band
(`selection_gas_test.go`, `selection_index_test.go`):

| | Gas per registered guardian | 400 lapsed registrations cost |
|---|---|---|
| Walk + a duplicate read per guardian (before) | **2,092** | ~836,000 gas |
| Walk, read once | **564** | ~226,000 gas |
| **Eligibility index (current)** | **360** | **0** |

At 36 registered guardians — the devnet the SDK's gas model was fitted on — the
old walk was already **53% of phase-1 handler gas**, and against the model
declared then (`250,000 + 5,500 × max_shares`) creation would have begun failing
out of gas for every creator at **62–79 registrations**, narrowest band first.

**Enumeration cannot stop early.** Sortition takes the `max_shares` *lowest*
tickets across all candidates, and `ticket(g) = SHA256(seed ‖ address)`, so every
candidate's ticket must be computed. Returning the first `max_shares` found would
select by store order — bech32 address ascending — and guardian addresses are
self-chosen, so that would be offline-grindable by exactly the route
`creatorAddress` was removed from the seed to close
([Trade-off §5](#accepted-trade-offs)). The index narrows *what* is
read, never how much of it.

So selection is **O(eligible)** and stays so. The index solves
dead-registration accumulation, which is the problem a permanent registry
actually has; it does not make a large *healthy* set cheap. Going below
O(eligible) means sampling a subset of candidates, which changes the byte-exact
eligibility predicate and the equal-probability claim resting on it — a protocol
change, deliberately out of scope.

#### The declared-gas boundary moved; it did not disappear

This is the part worth reading twice, because the improvement is easy to
overstate.

Clients declare phase-1 gas from a fitted constant model, re-measured on a live
40-guardian devnet on 29 July 2026:

```
measured   133,402 + 4,336 × max_shares
declared   167,000 + 5,400 × max_shares      (~25% margin)
```

The base still contains **360 gas per eligible guardian**, and the model still
treats it as constant. So the margin is finite headroom, and phase 1 begins
aborting out of gas once the eligible set passes roughly:

| `max_shares` | Eligible guardians the margin covers | Same band, before the index (registered) | Improvement |
|---|---|---|---|
| 2 | ~139 | ~62 | 2.2× |
| 7 | ~154 | ~65 | 2.4× |
| 32 | ~228 | ~79 | 2.9× |

So the boundary moved **2.2–2.9× band for band** — and the honest reading of that
is that it is *less* than the drop in per-guardian cost, which was 5.8×
(2,092 → 360). The reason is that the same live re-measurement lowered the
declared base by a third (250,000 → 167,000), so the absolute headroom shrank
even as the slope fell. Quoting a per-guardian ratio, or comparing the old
narrowest band against the new widest, would both flatter this.

The **change of driver** is the larger win and does not show up in that ratio: the
boundary is now reached by healthy participation rather than by dead
registrations accumulating forever, and a lapsed registration no longer moves it
at all. It is **not** a closed problem.

This residual is tracked as an open concern in
[Security Observation §3](#3-phase-1-gas-grows-with-the-eligible-guardian-set-and-the-creator-declares-it),
narrowed rather than closed.

**Widening the margin is the wrong fix.** Cosmos charges the declared limit, so
a fatter margin taxes every creator on every secret to defer a boundary;
`gas-model.test.ts` enforces that in both directions (under 1.1× a measured
point the transaction aborts, over 1.6× the creator is being overcharged). The
real fix is for the declaration to scale with the eligible count. `Query/Guardians`
already returns every field the predicate needs, but there is **no eligible-count
query and no server-side filter**, so a client must page the registry and apply
the predicate itself — the same O(registered) walk, moved off-chain where it is
unmetered and cacheable. A client-model change needing its own plan.

**Unmetered CPU in selection** (`selection_bench_test.go`, Apple M-series,
`-benchtime 20x`):

| Eligible candidates | Wall clock per selection |
|---|---|
| 1,000 | 1.11 ms |
| 10,000 | 13.8 ms |

Roughly linear with a sort's log factor on top. None of it is charged to the
creator, so at 10,000 eligible guardians a burst of concurrent creations is the
constraint, not the fee.

### 4. Per-operation cost

**Message handlers.** All figures are *handler* gas (store access inside the
keeper); a real transaction also pays ante costs — signature verification, tx
size, fee deduction — of roughly 20,000.

| Handler | Shape | Notes |
|---|---|---|
| `MsgUserRequestGuardians` | O(eligible) + O(max_shares) | §3. The eligible term is metered against the creator |
| `MsgUserDistributeShares` | O(max_shares) + O(payload bytes) | One share envelope per selected guardian, payload stored once |
| `MsgGuardianConfirmShares` | **O(1)** | 48,144 gas at the band ceiling, against the 120,000 the creator reimburses. Deliberately flat: the acceptance tally lives in its own key, so acceptance does not rewrite the roster. While it did, accept gas grew ~4,200 per guardian and broke the flat reimbursement at about fifteen |
| `MsgGuardianRevealShare` | **O(1)** | One reveal record plus the guardian's own record; does not walk the assignment set |
| `MsgUserCancelSecret` | O(active guardians) | ~2.07 M gas at 32 guardians, measured on a live chain. Sends coins per active guardian, so it is genuinely per-guardian work |
| `MsgGuardianRegister` / `Update` / `RotateKey` / `WithdrawStake` | O(1) | Each writes the guardian record, so each also rewrites one eligibility-index entry |
| `MsgSlashGuardian` | O(1) | |

**`EndBlock`** — all unmetered.

| Pass | Shape | Notes |
|---|---|---|
| `ProcessExpiredCommits` | O(due) | Due-height queue; an idle block reads at most one key |
| `ProcessExpiredRevealWindows` | O(due) × O(guardians per secret) | Settlement pays out per guardian and writes each guardian's record, so it also rewrites index entries |
| `ProcessExpiredRebates` | O(due) | Due-height queue; voids uncollected rebates at their collection deadline |
| `ProcessDuePrunes` | O(backlog) per block, capped at 50 *processed* | The cap bounds the work, **not the scan** — a burst of `N` same-height terminal secrets costs O(N²/50) key reads. Open, [Inefficiency §1](#inefficiencies--optimisation-candidates) |
| `ProcessFeeSplit` | O(1) | BeginBlock, ordered before `x/distribution` |

**Queries.** Paginated via the collections helpers. `SecretsByCreator` and
`HintsSince` read purpose-built indexes rather than scanning the secret store;
`HintsSince` is a range read from a height cursor, which is what makes
incremental recipient polling viable.

### 5. Index maintenance — the write side

The eligibility index is maintained solely by `SetGuardian`, the module's single
guardian-write choke point. Two costs follow, both deliberate:

- **One read of the previous record per guardian write**, needed to retire a
  stale key when `available_until` moves (it is part of the key). Paying it at the
  choke point buys an index that cannot drift; maintained at the eight call sites
  instead, it would drift the first time a ninth writer appeared — and a stale
  eligibility entry is a consensus fault, not a slow read.
- **One index write per guardian write** (~5,060 gas). It lands hottest on
  acceptance, which has 48,144 of a 100,000 budget spare, and on settlement,
  which is unmetered.

`CheckStateInvariants` proves the index agrees with the guardian records
(invariant 9) rather than assuming it. It runs in exactly one place today —
after `InitGenesis` (`genesis.go:126`), where an inconsistent genesis halts
rather than producing blocks — plus continuously in the conformance and fuzz
suites. It is **not** wired into per-block execution, and **not** into upgrade
migrations, because no upgrade exists yet (`app/upgrades.go` registers none).
Two consequences worth stating plainly:

- **A writer that bypassed `SetGuardian` mid-life would not be detected until
  the next genesis export/import.** Registering the sweep as a real SDK
  invariant (`x/secrets/module/module.go` currently has `RegisterInvariants` as
  an empty stub) would let `x/crisis` catch it on a schedule instead.
- **The index is derived state.** It has a migration; see §5.1.

#### 5.1 The index and chain upgrades

`SetGuardian` builds the index, so `InitGenesis` populates it as a side effect of
importing guardians. **An in-place upgrade does not run `InitGenesis`** — it runs
module migrations. Without one, adopting this code on a chain whose store
predates the index would leave it **empty**, and every
`MsgUserRequestGuardians` would fail `insufficient guardians: need N, have 0` —
complete, and invisible in a diff.

So `x/secrets` is **`ConsensusVersion` 2**, with a **1 → 2 migration**
registered in `RegisterServices` that calls `RebuildEligibilityIndex`. It clears
the index and derives it again from the guardian records, which makes it
idempotent and able to repair drift as well as absence — an upsert would leave
behind entries whose guardian has since moved its `available_until` or stopped
accepting.

Why a versioned module migration rather than surgery in the upgrade handler: it
must replay correctly for a node syncing from genesis, and `RunMigrations` drives
it from the on-chain version map so it executes once, in consensus, at the
upgrade height — the same determinism `InitGenesis` has.

No `StoreUpgrades` entry is required: the index prefix lives inside the existing
`secrets` store, not a new module store.

`TestRebuildEligibilityIndex` runs the rebuild against an empty index, an orphan
entry, a stale projection and a key left at a superseded window, and requires
byte-identical recovery plus a clean invariant sweep in every case.

### 6. Regression guards

Performance claims rot silently, so the ones that matter are pinned by tests
that fail rather than by prose:

- `TestPhase1GasSlopePerRegisteredGuardian` — measures at two registry sizes and
  asserts the **slope**, not an absolute, so it survives unrelated handler
  changes and still catches a reintroduced per-guardian read. Its absence is why
  a term worth 53% of the budget shipped unnoticed.
- `TestEligibilityIndexIgnoresIneligibleRegistrations` — 400 lapsed
  registrations must add no gas.
- `TestEligibilityIndexMatchesWalk` — the index yields exactly the candidate set
  a naive walk would, over a table of every way the predicate can fail,
  boundaries included.
- `TestGuardianAcceptGasFitsReimbursement` / `...RevealGas...` — the guardian
  legs stay inside what the creator reimburses, swept across the whole band.
- `gas-model.test.ts` + `testdata/vectors/tx_gas.json` — the SDK's declared model
  must never fall below a measured point.

### 7. Not yet measured

Stated plainly so nobody reads silence as coverage:

- **Block-time impact of a burst of concurrent creations.** §3 gives per-call
  wall clock; the aggregate effect on block production has not been measured.
- **`EndBlock` settlement at scale** — cost per block with a large due cohort.
- **Query latency under a large secret store**, including whether
  `PendingSecrets`' filtered paginate over-scans.
- **IAVL growth in absolute terms** — §2 says what grows, not how many gigabytes
  a year at a given load.
- **Phase-2 and cancel figures since the index landed.** Neither is affected by
  the change — their corpus points date from 27 July 2026 on a 36-guardian
  devnet — but they have not been re-measured, so the two halves of
  `tx_gas.json` were taken on different chains.
- **Whether 360 gas per eligible guardian holds at scale.** It was measured at
  40 eligible; the per-entry cost is a fixed store read and should not vary, but
  a sweep at 1,000+ eligible has not been run and the boundary table above is
  arithmetic from a single slope.

---

## The Judgement Ledger

One ledger, six statuses — an item moves between sections as rulings happen,
never between documents. **Accepted Trade-offs** are ruled: the cost is known
and consciously taken. **Load-Bearing Oddities** are deliberate structure that
looks wrong but carries no accepted cost. **Deviations From Documented Intent**
are places where prose and code disagree, awaiting alignment. **Inefficiencies**
are optimisation candidates. **Security Observations** are open concerns with
no ruling yet — when one is ruled acceptable it moves to Accepted Trade-offs.
**Open Defects** are things we intend to fix; a fixed defect is deleted (git
history keeps it).

### Design decisions — the questions readers ask

The ledger's deliberate decisions, as the questions a creator, guardian
operator or auditor actually asks. These are **not known issues**: each looks
wrong at first glance, solves a real problem, and was chosen with the
alternative understood. Every answer links to the entry that owns the
argument — this index summarises, it never becomes a second authority. Every
entry cites an owner ruling: the index records decisions, it does not make
them.

- **Why does creating a secret take two transactions?** The financially
  binding first step *is* the anti-grinding mechanism — any single-transaction
  design lets an attacker buy selection re-rolls at transaction-fee cost.
  Never collapse it. ([Oddity #1](#load-bearing-oddities--deliberate-decisions-worth-revisiting))
- **Why can anyone compute a share's HMAC key?** Deliberately public inputs,
  so the chain verifies slashing evidence keylessly; HMAC proves binding,
  never honesty. ([Trade-off §2](#2-hmac-keys-derive-entirely-from-public-inputs))
- **Why does a slashed guardian's "reveal" come from their accuser?** The
  auto-submitted evidence salvages the leaked share for reconstruction and
  prevents double punishment for one share.
  ([Oddity #2](#load-bearing-oddities--deliberate-decisions-worth-revisiting))
- **Why can't I report an early leak once the reveal window opens?**
  Possession stops proving a pre-window leak the moment reveals are legal —
  keeping reports open would turn every honest reveal into evidence against
  its author. ([Trade-off §3](#3-late-discovered-pre-window-leaks-are-unpunishable))
- **Why are guardians paid only at window close, even when the threshold was
  met on day one?** One race-free settlement point with complete
  information — paying early races the slashing regime.
  ([Trade-off §11](#11-all-settlement-waits-for-window-close))
- **Why must a guardian's availability cover the whole reveal window, and why
  is the reveal horizon capped at a year?** The cap is permanent: share/bond
  transfer is ruled out forever, because possession-based evidence cannot
  distinguish a transferor's retained copy from the transferee's leak.
  ([Trade-off §13](#13-availability-must-cover-the-whole-reveal-window--no-handoff-ever);
  spec.md Common Attack Vectors #6)
- **Why doesn't the creator get a partial refund for unfilled slots or
  no-shows?** The pool prices the band ceiling; forfeited slices reward the
  guardians who did the work — the same rule that makes revealing the
  dominant strategy.
  ([Trade-off §6](#6-creators-pay-the-band-ceiling-regardless-of-outcomes))
- **Why doesn't the chain check that my shares or commitment are valid?** The
  chain guarantees reconstruction integrity, never content validity — it
  sees only ciphertext.
  ([Trade-off §1](#1-the-chain-guarantees-reconstruction-integrity-never-content-validity);
  spec.md Common Attack Vectors #5)
- **My secret failed to activate — can I resume it?** Never. A retry is a
  full restart with a fresh seal (new ID, draw, keys, split, hint), so the
  failed attempt's shares can neither reconstruct nor link to the new one.
  ([Trade-off §4](#4-a-failed-attempts-ciphertext-stays-exposed-to-its-coalition))
- **Why must an abandoned draw wait out the full commit timeout, and why does
  every draw cost a fee?** The non-refundable creation fee prices the draw
  and the sit-out is deliberate anti-grinding structure — there is no early
  exit. ([Trade-off §5](#5-selection-grinding-is-priced-not-eliminated))
- **Who can collect a rebate, and why does collecting link my address to the
  secret?** Recipiency is proved by a bearer secret (`z`), and publishing it
  is a deliberate, opt-in exception to recipient privacy — collect to a
  single-use address, or not at all, if unlinkability matters.
  (spec.md "Recipient Rebate";
  [operations.md](operations.md#recipient-rebate))

## Accepted Trade-offs

*Consciously accepted costs: places where the protocol knowingly gave
something up to get something more valuable. Unlike the Load-Bearing Oddities
below (decisions that look wrong but aren't) or the Open Defects (things we
intend to fix), nothing here is a bug or a surprise — each entry records what
we chose, what it costs, and where the decision was made, so the reasoning
survives the conversations it was made in. Revisiting one means paying its
cost differently, not discovering it.*

*Each entry carries its own reasoning — the *why*, not just the *what* — so
the decision survives without a second document open. Entries link out for the
normative rule (spec.md), the mechanism (the keeper), and the decision log (the
plan), never for the argument itself. When a new ruling accepts a residual
cost, add it here in the same session.*

The costs themselves — what each fee is, how it is derived and where the
VEIL goes — are catalogued in [ECONOMICS.md](ECONOMICS.md).

---

### Test-harness determinism

#### 1. The scenario suite constrains selection, so S8 no longer samples a real pool

**Chose**: before reserving its post-rotation secret, S8 clears
`accepting_secrets` on every guardian outside the pre-rotation secret's selected
set, so selection has one possible outcome and the rotator is drawn with
certainty. Nothing about sortition changes — it runs honestly over the candidates
it is given, and pausing this way is a mechanism guardians genuinely have.
**Gave up**: S8's draw exercises selection over five candidates instead of
twenty-four, so it contributes nothing to selection coverage. Read as a
key-rotation test it loses nothing; read as evidence that selection works over a
realistic pool it would mislead, and it must not be.
**Also gave up**: on-chain state that outlives the process. A suite killed between
park and restore leaves the fleet unable to reserve anything until something
restores it or the chain is reset — an `EXIT` trap covers every path the suite
controls, and cannot cover `SIGKILL`.
**Where decided**: owner ruling, 6 August 2026;
`done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md`. Selection's own coverage is
`x/secrets/keeper/guardian_selection_test.go` and the sortition vectors.

#### 2. Devnet guardian identities are published, and deliberately so

**Chose**: every devnet guardian's signing key derives from the canonical BIP39
all-zeros test mnemonic at HD account index N, so `guardian-07` holds the same
address on every run. A ticket is `SHA256(seed ‖ address)`, so without stable
addresses no run's guardian set can resemble another's, and a log from yesterday
describes guardians that no longer exist.
**Gave up**: any secrecy for those keys — anyone can derive them. Confined to the
devnet by a chain-ID assertion that reads the devnet's ID from `networks.json` and
fails closed if it cannot; the keys cannot be created against any other chain.
**Where decided**: `done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md`. The mechanism is
`devnet/guardians.sh`.

### Security model boundaries

#### 1. The chain guarantees reconstruction integrity, never content validity

**Chose**: the chain stores and reveals ciphertext it cannot read; the
`secret_commitment` is stored but never verified on-chain, and recipients
verify reconstruction client-side.
**Gave up**: any on-chain guarantee that a secret's shares reconstruct to
something meaningful — a creator can distribute garbage against a valid
commitment and guardians will faithfully store and reveal it.
**Where decided**: spec.md "Invalid Share Submission" (accepted risk);
[Deviation #2](#deviations-from-documented-intent).

#### 2. HMAC keys derive entirely from public inputs

**Chose**: `SHA256("secrets" ‖ secretID ‖ guardianAddr ‖ "hmac_salt")` — a
salted hash, not a keyed MAC — so the chain verifies slashing evidence and
reveals with no secret key material. This is what makes early-reveal evidence
*self-verifying*: a reporter presents the plaintext share and the chain
recomputes the binding share ↔ guardian ↔ secret from data it already holds,
with no key custody and no trusted verifier anywhere in the loop.
**Gave up**: HMAC proves *binding*, never honesty — anyone (including the
creator) can compute a valid HMAC over arbitrary data, so a matching HMAC
says "this is the share that was issued to this guardian for this secret",
never "this guardian behaved". Pre-window share secrecy therefore rests
entirely on the share's own entropy — exactly 256 bits under the key-share
architecture, since the share is a point on the per-secret private key's
polynomial rather than a fragment of the payload.
**Where decided**: spec.md "Early-Reveal Reporting"; derivation in
[`crypto/hmac.go`](../crypto/hmac.go) (pure Go — chain and guardian) and
[`rust/src/utils.rs`](../rust/src/utils.rs) (WASM/TS SDK), pinned
byte-for-byte by `testdata/vectors/`; entropy analysis in
[planning/done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](planning/done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md) §8.

#### 3. Late-discovered pre-window leaks are unpunishable

**Chose**: early-reveal reports close the moment the reveal window opens
(`height < reveal_start_block`). This is forced, not arbitrary: HMAC evidence
proves *possession* of the share, and once reveals are legal and public on
chain, possession no longer distinguishes a pre-window leak from a replay of
somebody's legitimate reveal. Keeping the window open would turn every honest
reveal into slashable "evidence" against its own author.
**Gave up**: a leak discovered after the window opens can never be slashed;
reporting promptly is the reporter's responsibility, and a late report is
simply a failed reward claim, exactly like missing any other bounty window.
**Where decided**: ruling July 2026; enforced in
`x/secrets/keeper/msg_server_slash_guardian.go`, spec.md "Early-Reveal
Reporting".

#### 4. A failed attempt's ciphertext stays exposed to its coalition

**Chose**: the client-side retry convention — a failed secret is never
resumed; a retry re-seals with entirely fresh material (new secret ID and
selection draw, new inner encryption, new per-secret keypair, new split, new
hint), so the failed attempt's shares can neither reconstruct nor link to any
later attempt. The convention is advisory: documented client-side, not
chain-enforced. It costs nothing to honour because the seal path already
generates fresh material on every call (`rust/src/seal.rs`), which is why the
ruling was documentation-only.

The underlying exposure is structural. Phase 2 hands a decryptable share to
**every** one of the `max_shares` selected candidates, and it must:
acceptance *means* "I decrypted my share and verified its HMAC", so material
precedes the bond. `MsgSlashGuardian` correctly refuses reports against a
guardian with no accepted assignment — there is no bond to slash — so a
candidate that receives a share and never accepts holds live material with
nothing at stake. The gap bound `max_shares − min_shares < threshold` makes
the never-confirmed set sub-threshold on any secret that **activates**; the
bound is conditional on activation, and a secret failing below `min_shares`
leaves up to `max_shares` never-bonded holders.

**Gave up**: any attempt that reached Phase 2 left its outer ciphertext `C`
in transaction history permanently, so a `≥ threshold` coalition of that
attempt's holders can always unlock that ciphertext — recipient-only, since
it decrypts to `C_r`. The content's effective time-lock is the minimum over
all distributed attempts. Bounded by protocol-random selection, the sunk
1,000 VEIL entry fee per colluding registration, and early-reveal slashing
once bonds lock.

**Structural fixes ruled out**: *accept-then-distribute* (a fourth
interaction in substance, and it forces a "was a share actually delivered?"
gate onto the no-reveal slash); *bond-at-selection* (seizes float for
assignments never confirmed); *an on-chain `pk_s` uniqueness check* (proto and
state surface for a failure mode the SDK cannot produce).
**Where decided**:
[planning/done/DONE_RETRY_CONVENTION_PLAN.md](planning/done/DONE_RETRY_CONVENTION_PLAN.md)
(ruled July 2026; documentation-only); authority
[spec.md, Common Attack Vectors #7](spec.md#common-attack-vectors--mitigations)
and its Client-Side Integration Guide "Retry semantics" point.

#### 5. Selection grinding is priced, not eliminated

**Chose**: a consensus-only seed —
`SHA256(chainID ‖ uint64_be(height) ‖ lastBlockHash ‖ uint64_be(counter))` —
with a non-refundable creation fee on top. No input is the creator's:
`clientEntropy` and the creator-chosen `secret_id` were both removed from the
message surface, and `creatorAddress` was removed from the seed because it was
an *offline-parallelisable* grind lever (generate many candidate addresses,
compute each resulting selection offline, fund and submit from the winner — no
block-waiting at all). The seed's inputs being public is deliberate: it is what
lets anyone verify from public data that the seed was honest, which is the
verifiability property the protocol actually claims.

**Gave up**: the creator's residual influence is **submission timing**. Because
every seed input is public before submission, a creator can compute the draw it
would get at the next height and choose not to submit — one whole-set re-roll
per block, at no cost for the rolls it discards. Ruled acceptable, and not
defended against:

- It gives **whole-set re-rolls only, never surgical placement**, which is no
  cheaper than the Sybil route the bond economics already price.
- Only a secret's own **creator** can grind its draw, and that creator already
  holds the plaintext — biasing it endangers only their own material, the same
  category as choosing `threshold = 2`.
- Every draw a creator actually *uses* is paid for at full price (the creation
  fee, 90% to validators and 10% burned), so steering cannot be done for free —
  only the unused rolls are free, and those change nothing.
- A creator wanting its own secret to fail has strictly cheaper routes anyway:
  never distribute, or distribute below `min_shares`, and let the commit
  deadline take it.

Commit–reveal seeding is **held in reserve** — it does not close retry-grinding
without also making commit abandonment costly, and it adds a round-trip of
latency to every secret. An on-chain randomness beacon (drand/VRF) is recorded
as the strongest long-term fix and future infrastructure, not a launch
requirement.
**Where decided**: D2, D3 and D6 in
[planning/done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md](planning/done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md)
(July 2026); the fee itself in
[planning/done/DONE_CREATION_FEE_PLAN.md](planning/done/DONE_CREATION_FEE_PLAN.md)
(per-draw cost table in its decision history).

### Economics

#### 6. Creators pay the band ceiling regardless of outcomes

**Chose**: the pool is fixed at `P(max_shares)` and splits equally among
*actual* revealers, so any slot that never ends in a reveal — left unfilled at
activation **or** a no-show at reveal time — raises the per-revealer payout.
The creator pays `P(max_shares)` either way. Uniformity is the point: one rule
covers both cases, and it is the same rule that makes revealing the dominant
strategy no matter what the rest of the roster does.
**Gave up**: the creator is compensated for a no-show only via the 10% bond
slice, and for an unfilled slot not at all; a wide band buys redundancy, not
a discount.
**Where decided**: variable-quorum design in
[planning/done/DONE_VARIABLE_QUORUM_PLAN.md](planning/done/DONE_VARIABLE_QUORUM_PLAN.md);
settlement rules in spec.md "Settlement".

#### 7. Settlement is threshold-independent — service is paid, not outcome

**Chose**: revealers are paid and bonds returned whether or not the secret
reached threshold; the threshold decides only the final state.
**Gave up**: a creator whose secret fails one reveal short of threshold
still pays every guardian who did reveal.
**Where decided**: spec.md "Secret Economics & Slashing" (ruled with the
bonded guardian economics, July 2026 —
[planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md)).

#### 8. Fixed prices, zero governance (Position A)

**Chose**: no `Params` state, no `MsgUpdateParams`, no vote can move an
economic constant; a guardian underwriting a year-long secret underwrites it
against economics that cannot float beneath it. The only retune path is a
coordinated software upgrade.
**Gave up**: no on-chain response to token-price swings, congestion, or
mispriced constants — every adjustment costs an upgrade cycle.
**Where decided**:
[planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md);
spec.md "Economic Parameters".

#### 9. Guardian registration is permanent and the entry fee sunk

**Chose**: no deregistration or refund path — a leaving guardian drains
their float and simply stops qualifying for selection; the 1,000 VEIL entry
fee is spent at registration (90% to validators, 10% burned).
**Gave up**: no clean exit or fee recovery for honest leavers; the fee is a
pure Sybil cost by design.
**Where decided**: [operations.md `MsgGuardianWithdrawStake`](operations.md#msgguardianwithdrawstake);
fee routing ruled in
[planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md).

#### 10. Integer dust is tolerated, not engineered away

**Chose**: truncating integer arithmetic everywhere, with dust bounded and
asserted rather than eliminated — the creation-fee curve can dip by up to
one basis point of `P` between adjacent distances, and the community pool
accrues each reward withdrawal's sub-uveil truncation remainder (SDK
design).
**Gave up**: literal exactness claims ("monotone", "exactly zero") — the
suites assert bounds (dust ≤ `P ÷ 10,000`; pool growth < 1 uveil per run)
instead.
**Where decided**: found in execution, July 2026 —
[planning/done/DONE_CREATION_FEE_PLAN.md](planning/done/DONE_CREATION_FEE_PLAN.md) §2
and
[planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md) §4.

### Liveness & capital

#### 11. All settlement waits for window close

**Chose**: one race-free settlement point at `reveal_end_block + 1` with
complete information — no payout can race an early-reveal report, and the
eligible set is final before anyone is paid. Paying at threshold instead would
settle against a set that a later report can still change, and would have to
decide what to do about a guardian already paid when its bond is
retrospectively forfeit.
**Gave up**: guardians' bonds stay locked up to a full window past their
work, even when threshold was met on the window's first block, and settlement
rides EndBlock rather than a user transaction (nobody can hurry it).
**Where decided**: the settlement queue in
`x/secrets/keeper/endblock_logic.go`; spec.md "Settlement".

#### 12. Settlement failures quarantine, never halt

**Chose**: a failed settlement retries every block behind a per-secret
`CacheContext`, with a `settlement_stalled` alarm from the first failure —
funds are locked-not-lost with a one-secret blast radius, self-recovering
when an upgraded binary fixes the bug.
**Gave up**: no automatic escalation or unlock — a stalled secret's funds
wait for a software upgrade; deliberately no panic (a deterministic EndBlock
panic is a chain-wide DoS).
**Where decided**: spec.md "Settlement failure handling" (July 2026);
[operations.md Time-Based Operations](operations.md#time-based-operations-beginblockendblock).

#### 13. Availability must cover the whole reveal window — no handoff, ever

**Chose**: selection requires `available_until ≥ reveal_end_block`, so a
guardian is only ever assigned a secret it has committed to outlive. The
reveal horizon `H` is pinned equal to `MaxAvailabilityWindow` for the same
reason — validation must never promise a window that selection cannot staff.

Share/bond transfer between guardians (guardian A hands an in-flight share and
its bond obligation to B, compensation forming part of the transaction) was
explored as the escape hatch and ruled **permanently off the table**.
Mechanically it is deceptively easy — the HMAC key derives from public inputs
(§2), so B's reveal would verify keylessly against A's original stored HMAC —
but it is unsound under possession-based evidence. Nothing can make A forget
the share: post-transfer A holds the plaintext with no collateral at stake and
can profitably report B for an "early reveal" through a Sybil reporter
address. The evidence verifies, because it is the genuine share and possession
cannot distinguish "B leaked" from "A retained", so A nets the transfer payment
plus 50% of B's bond while a provably innocent B is fully slashed. No defence
survives: reporter restrictions are Sybil-bypassable, closing reports for
transferred shares removes deterrence exactly where it is needed, and slashing
both parties punishes the innocent one. Since accepting a transfer hands the
transferor a call option on half one's bond, no rational guardian would accept
— the market is self-defeating before it is even unsafe. **One share, one
holder, forever** is therefore load-bearing for the whole evidence model, not
incidental. Transfer would not have addressed key loss in any case: the
transferor must be able to decrypt in order to hand off. The supported
key-lifecycle mechanic is forward-only **rotation**, which moves no share
material anywhere, so the invariant holds trivially (§14).

**Gave up**: long-dated secrets select only from correspondingly
long-registered guardians, shrinking the eligible set exactly where
redundancy matters most (a one-year secret needs guardians registered
≥ one year ahead). The cap is permanent, not awaiting handoff mechanics;
extending horizons is cancel-and-recreate territory, and operator exit is
`accepting_secrets = false` plus serving out existing commitments.
**Where decided**: ruled July 2026 —
[spec.md, Common Attack Vectors #6](spec.md#common-attack-vectors--mitigations)
("Share Ownership Transfer") and spec.md "Timing Constraints".

#### 14. Key rotation is hygiene, not recovery

**Chose**: forward-only key epochs — rotation bounds the blast radius of a
key leak to one epoch for *future* assignments, while every old epoch stays
permanently bound to the assignments made under it.
**Gave up**: there is deliberately no key-recovery mechanic — a guardian who
loses key material eats the no-show slashes on every in-flight secret
encrypted to the lost epochs.
**Where decided**: spec.md "Guardian Key Rotation";
[planning/done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md](planning/done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md).

#### 15. Short secrets cost the creator disproportionately

**Chose**: the creator funds the guardians' gas — an `F_accept` slice
escrowed apart from the pool, and an `F_reveal` slice inside it — so that
completing the job can never cost a guardian money, at any distance the
protocol permits.
**Gave up**: price flatness at the short end. A one-minute five-guardian
secret costs the creator ~0.234 VEIL, of which the guardians' actual wage is
0.0001 VEIL — the rest is gas reimbursement, the anti-grinding floor and the
creator's own gas. The work of guarding a secret has a fixed cost that no
amount of brevity avoids. The same property is the short-secret spam defence.
**Where decided**:
[planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md](planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md)
(ruled July 2026).

#### 16. Guardians carry their own gas until the secret ends

**Chose**: nothing is disbursed before the secret reaches a terminal state,
so a guardian is reimbursed for its accept transaction at settlement,
cancellation or commit expiry — not when it sends it.
**Gave up**: immediacy. On a year-long secret the guardian floats ~0.011 VEIL
of gas for a year. The alternative — paying at acceptance — would let the
module's escrow drift from the amounts stored on the secret, forcing either a
second stored component or a payout recomputed from live constants, which the
immutable-economics ruling forbids. The carry is small beside the bond already
locked for the same period (~21 VEIL on a year-long secret).
**Where decided**:
[planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md](planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md)
(ruled July 2026).

#### 17. Declared gas always exceeds gas consumed, and the excess is spent

**Chose**: every actor declares a gas limit with headroom over what its
transaction actually consumes, and Cosmos charges the declared limit rather
than the consumption — unused gas is never refunded.
**Gave up**: exact pricing of work. A declaration must clear worst-case
consumption or the transaction aborts, so a margin always exists and is always
paid. Two places it shows:

- **Creators** declare a measured model — a base plus a per-share and a
  per-byte term for phase 2 — rather than a flat figure. That bounds the waste
  (the old flat 1,000,000-gas phase 2 declared roughly three times what a
  seven-guardian secret consumed) without removing it. Determinism is the
  reason it stays a model rather than a simulation: a wallet must quote the
  exact fee before the user signs, and a simulated figure cannot be quoted.
- **Guardians** declare the amount the protocol reimburses whenever that
  exceeds simulation, so spend equals reimbursement exactly and no
  `gas_adjustment` setting can erode coverage. Where a handler outgrows the
  reimbursement the daemon declares the larger simulated figure instead: being
  under-reimbursed is bad, but aborting is worse — an aborted reveal is a
  no-show slash.

**Also accepted**: a guardian operator who raises `gas_price` above the
consensus floor pays the difference themselves. The creator's escrow is
denominated at the floor, so buying mempool priority is the guardian's
purchase, not a protocol cost. Overriding the operator's own price would be
worse — it could leave a guardian unable to push a reveal through a congested
chain. The daemon logs the per-leg shortfall once at startup so the choice is
informed rather than discovered in a shrinking balance.
**Where decided**:
[planning/done/DONE_CREATION_COST_INTEGRITY_PLAN.md](planning/done/DONE_CREATION_COST_INTEGRITY_PLAN.md)
§6 (ruled 27 July 2026).

## Load-Bearing Oddities — Deliberate Decisions Worth Revisiting

These look wrong at first glance but each solves a real problem. Revisiting any of
them means solving the underlying problem another way.

Decisions whose cost has been consciously accepted live in
[Accepted Trade-offs](#accepted-trade-offs) with their full reasoning:
public-input HMAC keys (§2), the report window
closing at `reveal_start_block` (§3), pre-acceptance share exposure and the
retry convention (§4), the fixed band-ceiling pool and where forfeited slices
flow (§6), settlement waiting for window close (§11), the availability cap and
the permanent ruling against share/bond transfer (§13), and forward-only key
rotation (§14). Only the two decisions below carry no accepted cost — they are
structure, not trade.

1. **Three-phase commit with one deadline.** Guardians are selected and locked
   before any share exists because the client needs their encryption keys before
   it can encrypt, and selection must be protocol-controlled (anti-bias). A single
   `commit_deadline` spans all phases. **Do not collapse this to a single
   transaction.** Analysis (July 2026) showed the two-step shape is itself the
   anti-bias mechanism: any single-transaction design requires the client to
   *predict* the selection before committing funds, and every predictable-seed
   scheme lets an attacker buy selection re-rolls at roughly transaction-fee cost.
   Two interactions, where the **first is financially binding**, is the minimum
   structure for grinding resistance. The non-refundable creation fee (July
   2026) completes the shape: the financially binding first step now carries a
   real per-draw price, not just gas. The seed's inputs are all public, so a
   creator can compute a draw before submitting and re-roll by choosing a later
   block — a whole-set re-roll, never surgical placement, and every *realised*
   draw is paid for at full price. Ruled acceptable and out of scope
   ([Trade-off §5](#accepted-trade-offs)).
2. **Early-reveal slashing auto-submits the evidence as the guardian's reveal.**
   Odd — the punished guardian "reveals" via their accuser — but it salvages the
   leaked share for reconstruction and prevents double-punishment (early *and*
   no-show) for one share. The internal path deliberately skips the `k`
   step-down that a voluntary reveal earns, so the salvage never softens the
   slash.


## Deviations From Documented Intent

1. **Progressive reveal windows / anti-coordination measures do not exist
   on-chain.** The reveal-window check is pure `[start, end]` bounds; every
   guardian may reveal at the first block. Client-side anti-coordination reveal
   scheduling is implemented in the guardian daemon
   ([planning/done/DONE_GUARDIAN_IMPROVEMENTS_PLAN.md](planning/done/DONE_GUARDIAN_IMPROVEMENTS_PLAN.md)
   §7.3 — `reveal_offset_blocks`, a deterministic per-(secret, guardian) offset,
   **default 0/off**) — but it is opt-in per guardian and nothing protocol-side
   enforces it.
   **Documented intent to align**: spec.md "Revolutionary Capabilities" lists
   "progressive reveal windows" under Multi-layer Security, and CLAUDE.md's
   project overview says "progressive time-locks" — both read as
   protocol-enforced. Alignment = either add a protocol-side stagger or
   soften both claims to describe the daemon's opt-in scheduling.
2. **`secret_commitment` is stored, never verified on-chain.** Verification is
   wholly client-side — deliberate (the GIGO decision: the chain guarantees
   reconstruction integrity, not content validity; spec.md "Common Attack
   Vectors" #5, Invalid Share Submission, records it as an accepted risk;
   [Trade-off §1](#accepted-trade-offs)).
   **Documented intent to align**: spec.md "Share Distribution (Phase 2)" —
   the "Distribution Requirements" / "Security Measures" paragraphs
   ("*Encryption validation* ensures shares are properly encrypted…",
   "*Share validation* prevents malformed data…") read as chain-side checks
   the chain cannot perform (it sees only ciphertext). Alignment = reword
   those paragraphs as client/guardian responsibilities.
3. **The same spec.md paragraph describes a retired share model.** "Distribution
   Requirements" (spec.md "Share Distribution (Phase 2)") states that creators
   must "distribute shares to all assigned guardians from Phase 1" and that
   "each guardian must receive exactly one share at their assigned share index
   (1 to shares)". Neither holds: explicit share indices were removed in
   December 2024 (SSS carries an intrinsic ID — see CLAUDE.md's ShareIndex
   lesson), and distributing to *fewer* than `max_shares` is legal down to
   `threshold` (§2). Alignment = rewrite the paragraph against the band model;
   this is a spec correction, so it needs a ruling before it is made.


## Inefficiencies — Optimisation Candidates

1. **The prune queue's per-block cap bounds the work, not the scan.**
   `ProcessDuePrunes` drains via `drainDueEntries`, which collects **every** due
   entry into a slice and only then truncates to `MaxPrunesPerBlock`. The
   backlog does drain — 50 per block — but while it exists every block re-walks
   and re-allocates the whole of it, so a burst of `N` same-height terminal
   secrets costs `O(N²/50)` key reads spread over `N/50` blocks instead of
   `O(N)`. Same-height terminal transitions are cheap to produce (§1 of Security
   Observations), and the fix is to stop the walk at the cap rather than after
   it.

2. **Every guardian write re-reads the record it was just handed, to maintain
   the eligibility index.** `SetGuardian` calls `setGuardianEligibility` first,
   which does its own `GetGuardian` to compare the stored `available_until`
   against the incoming one and retire a stale key if the window moved. But
   `available_until` changes on exactly two paths — registration and
   `MsgGuardianUpdate` — while the five float mutations in `guardian_float.go`
   (bond lock, bond release, bond-count decrement, `k` adjustment, locked-float
   deduction) each read the guardian, mutate a copy, and hand it straight to
   `SetGuardian`. On those the comparison is guaranteed equal, so the read is
   pure overhead: **~1,534 gas** (`ReadCostFlat` 1,000 + 3 × a ~178-byte
   record), twice-read where once would do. This is the same shape as the
   duplicate read removed from selection above, reintroduced on the write side.

   Where it lands: **metered** on guardian acceptance and reveal — affordable,
   since accept measures 44,871–57,918 handler gas against a 100,000 budget
   (`guardian_gas_test.go`), so it is ~3% and not a boundary — and **unmetered**
   in EndBlock settlement, once per accepted guardian per secret, where it is
   block time with no fee brake (§1 of Security Observations).

   The index `Set` is also unconditional. A float movement genuinely changes the
   projection (`unlocked` moves), so most writes are necessary — but
   `MsgGuardianRotateKey` touches no projection field and still rewrites the
   entry.

   The fix is cheap and needs no new state: let `SetGuardian` accept the
   previously-read record from the caller that already holds it, and skip the
   `Set` when the projection is unchanged — `eligibilityEqual` already exists in
   `guardian_eligibility.go` for invariant 9. Not yet done, and deliberately
   listed rather than fixed silently: it is a cost the selection work added while
   removing a larger one.


## Security Observations

Every entry below is a concern that is **open** — reachable, unresolved, and not
covered by an existing ruling. Anything ruled acceptable moves to
[Accepted Trade-offs](#accepted-trade-offs); anything fixed is deleted (git
history keeps it).

### 1. EndBlock settlement work is unmetered and uncapped

Genesis sets `consensus.params.block.max_gas = 75,000,000` (August 2026 —
[DONE_CONSENSUS_BLOCK_GAS_LIMIT_PLAN.md](planning/done/DONE_CONSENSUS_BLOCK_GAS_LIMIT_PLAN.md)),
so gas now bounds what one block of *transactions* may execute. EndBlock sits
outside that bound: BeginBlock/EndBlock run with an infinite gas meter by SDK
design. `ProcessDuePrunes` caps itself at `MaxPrunesPerBlock`;
`ProcessExpiredCommits` and `ProcessExpiredRevealWindows` have no equivalent
cap, and every secret due at a height settles in that height's EndBlock, each
walking its accepted set doing bank sends, bond releases and possibly
slashes. That the retention path *is* capped and the settlement path is not
is an inconsistency inside one file, not a considered asymmetry.

**Why it is reachable rather than theoretical**: `reveal_end_block` is
creator-chosen, so settlement heights can be made to converge deliberately.
The block gas limit bounds the *creation* burst per block (~400 narrowest-shape
creations), but creations spread across many blocks can still share one
`reveal_end_block`, so the settlement concentration survives the gas limit.
The unrecoverable outlay is only the creation fee plus gas — ~0.10 VEIL per
secret at the narrowest band, since the pool and accept fees come back
(refunded on failure, or paid to the actor's own guardians). Measured
1 August 2026: a commit-expiry sweep of 470 secrets in a single EndBlock cost
less than the ~10 ms block-interval measurement noise on a development
workstation, so today's exposure sits far from the block interval — the gap
is structural, not currently painful.

Correctness is not at risk: the per-secret `CacheContext` keeps every
settlement all-or-nothing, and the failure model is already quarantine-not-halt
([Trade-off §12](#accepted-trade-offs)). The exposure is block
time, and therefore liveness.

**The fix is not free.** A per-block settlement cap means deferring a
settlement past its due height, which delays guardian payouts and extends
bond locks — so it needs a fairness rule (oldest-due first) and a ruling on
how long a guardian can be asked to wait beyond `reveal_end_block + 1`. An
owner call.

### 2. Assignment responsiveness is neither measured nor priced

A selected candidate that simply never responds pays nothing: no transaction,
no bond locked, no slash, and — critically — **no `k` movement**. The bond
multiplier moves only on a slash (up) or a correct reveal (down), so a guardian
that ignores nine assignments out of ten holds exactly the standing of one that
accepts every assignment it is offered. Selection probability is unaffected
too, by design — see the sortition row in Hard-Coded Protocol Values: `k` prices
bonds, never candidacy.

The consequence is that **reliability is invisible**. Creators cannot select
for it, the protocol cannot reward it, and a guardian faces no cost for
treating assignments as optional — while still collecting on the ones it
chooses to take. This is not primarily an attack surface: a Sybil adversary
would sink 1,000 VEIL per address to impose roughly a creation fee per forced
failure, which is economically absurd. The realistic case is ordinary
free-riding and ordinary downtime, which are indistinguishable from each other
and from malice.

What it costs the creator: if non-responders exceed `max_shares − min_shares`,
the secret fails at the commit deadline. Pool and accept fees are refunded in
full, but the **creation fee is not** — so every attempt costs the creator the
draw price plus gas, and costs a silent candidate nothing. The gap bound caps
the slack a creator can buy: with `max − min < threshold`, a 2-of-5 shape
tolerates only one silent candidate.

The variable-quorum design treats this as the creator's dial and documents the
robustness range that follows
([planning/done/DONE_VARIABLE_QUORUM_PLAN.md](planning/done/DONE_VARIABLE_QUORUM_PLAN.md)
— "`min` close to `threshold` is doubly fragile"), and a high `threshold` with a
wide band genuinely mitigates it (16-of-32 tolerates 15 silent candidates). What
that plan does not address is the *persistence*: it models non-response as a
random per-secret failure, whereas it is a stable per-guardian property the
protocol declines to record. Pricing it directly would need bond-at-selection,
which is ruled out ([Trade-off §4](#accepted-trade-offs)); a
response-rate statistic that creators could read, or that fed `k`, is the
cheaper direction and is new mechanism requiring a ruling.

### 3. Phase-1 gas grows with the ELIGIBLE guardian set, and the creator declares it

Candidate enumeration is metered inside the *creator's*
`MsgUserRequestGuardians` gas, and the SDK declares a fitted **constant** model
(`typescript-sdk/src/protocol/constants.ts`, pinned by `gas-model.test.ts`
against `testdata/vectors/tx_gas.json`). Any term in that cost which grows with
the network eats the model's margin until phase 1 aborts out of gas — for
**every** creator, on a purely organic trigger, with the fee still deducted
because the ante handler charges `gas_limit × gas_price` and does not refund.

Enumeration range-reads the eligibility index keyed
`(available_until, guardian_address)` from the reveal end, so a registration that
cannot serve the window costs **nothing at all** — 400 lapsed registrations add
exactly zero — and each eligible candidate costs **360 gas**. The growth term
therefore tracks the *eligible* set: it is reached by healthy participation, and
no longer moves when a guardian's window lapses.

**It is narrowed, not closed.** Against the declared model
(`167,000 + 5,400 × max_shares`, ~25% margin over a 40-eligible measurement), the
margin is exhausted at roughly **139 eligible guardians at `max_shares` 2, 154 at
7, and 228 at 32**. A narrow band therefore aborts at 140 eligible guardians,
which a testnet reaches. [Performance & Scaling §3](#3-guardian-selection--oeligible-and-why-it-cannot-be-less) above holds the measured
complexity and the derivation of those figures.

**Widening the margin is not the fix.** Cosmos charges the declared limit, so a
fatter margin taxes every creator on every secret to defer the boundary, and
`gas-model.test.ts` rejects it (`MIN_MARGIN` 1.1, `MAX_MARGIN` 1.6 — bounded above
as well as below, and the model currently sits at 1.25). The fix is a declaration
that scales with the eligible count. The inputs are already on the wire —
`Query/Guardians` returns every field the predicate needs — but there is **no
eligible-count query and no server-side filter**, so a client must page the whole
registry and apply the predicate itself — the O(registered) walk the index keeps
out of consensus, run off-chain where it is unmetered and cacheable rather than
charged to every creator. A client-model change, unwritten, and the reason this
stays on this list.

Selection is O(eligible) by construction and cannot be less: sortition takes the
`max_shares` lowest tickets, so every candidate's ticket must be computed. Going
below that means sampling candidates, which changes the byte-exact eligibility
predicate and the equal-probability claim resting on it.


## Open Defects & Cleanup Notes

*No known defect is open.* Likely bugs and unfinished edges are recorded here,
each needing a reproducing test or a ruling before it is fixed. Concerns that are
reachable but unresolved live under Security Observations above; costs
consciously accepted live in [Accepted Trade-offs](#accepted-trade-offs).

### Closed: three defects in the scenario suite itself

Recorded rather than dropped, because each was invisible for the same reason —
the suite asserts the chain and nothing asserts the suite — and that reason has
not gone away.

#### 1. S8's rotator was drawn by lottery, and lost one run in four

The key-rotation scenario needed a *named* guardian to be selected for its
post-rotation secret, and got there by redrawing until sortition obliged: six
attempts, five of twenty-four per draw, so `(19/24)^6 ≈ 25%` of runs exhausted
the budget and failed for no protocol reason. It went unnoticed because S8 skips
in CI whenever the rotation-interval override is unset, which was always — the
scenario the job could not afford to run was also the one that would have failed
weekly. It was observed failing exactly this way while measuring the cadence floor
(6 August 2026).

**Fixed** by constraining the candidate pool instead of resampling it: park every
guardian outside the pre-rotation secret's selected set, reserve the second
secret, restore. Selection then has exactly one possible outcome. See
`done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md`.

#### 2. The rebate drill's share band never reached the chain

`create_secret` is documented `manifest offset duration bump` and forwarded four
arguments; the rebate collection drill called it with five, the fifth being the
`7:9` band its entire dust-floor calibration was derived from. The band was
silently dropped and the drill ran the suite's ordinary zero-width 5, so the
calibration recorded in its comment had never once been exercised.

**Fixed** by forwarding the argument. The lesson is the shape: bash discards extra
arguments without complaint, so a helper's contract lives only in its comment.

#### 3. Three block-event reads took the first event in the block

S1's `guardian_slashed` and `secret_rewards_distributed`, and S3's
`secret_rewards_distributed`, read `.[0]` of a height rather than selecting on
`secret_id`. Correct only while exactly one secret can settle per block, which is
true of a sequential suite and silently false of anything else — and the failure
would present as another secret's economics, not as a missing event.

**Fixed** by selecting on `secret_id`, as S4, S5, S8 and S10b already did.
