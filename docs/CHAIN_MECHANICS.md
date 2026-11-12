# Timeflare Chain Mechanics

*How the chain is built inside — state layout, genesis behaviour and measured
costs.*

> **Scope**: [operations.md](operations.md) is the operation-level truth — what
> every transaction and block-driven operation does, its validation, side
> effects and token flows. [spec.md](spec.md) is the protocol authority — the
> rationale, economics and security model. [ECONOMICS.md](ECONOMICS.md)
> catalogues the fees themselves. [KNOWN_RISKS.md](KNOWN_RISKS.md) is the
> register of judgements about the protocol: accepted trade-offs, open
> concerns and defects. This document holds what none of those should: the
> implementation's internals and its measured performance.

## Table of Contents

1. [State & Storage](#state--storage)
2. [Performance & Scaling](#performance--scaling)

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
  [Security Observation §1](KNOWN_RISKS.md#1-endblock-settlement-work-is-unmetered-and-uncapped),
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
([Trade-off §5](KNOWN_RISKS.md#5-selection-grinding-is-priced-not-eliminated)). The index narrows *what* is
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
[Security Observation §3](KNOWN_RISKS.md#3-phase-1-gas-grows-with-the-eligible-guardian-set-and-the-creator-declares-it),
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
| `ProcessDuePrunes` | O(backlog) per block, capped at 50 *processed* | The cap bounds the work, **not the scan** — a burst of `N` same-height terminal secrets costs O(N²/50) key reads. Open, [Inefficiency §1](KNOWN_RISKS.md#inefficiencies--optimisation-candidates) |
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
