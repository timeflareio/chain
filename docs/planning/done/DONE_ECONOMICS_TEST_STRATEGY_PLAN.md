# Secret Economics — Test Strategy & Conformance Plan

*A systematic programme to prove every expected outcome of the bonded guardian
economics — rewards, slashing, leak reporting, unmet thresholds, timeouts,
cancellation — with deliberate focus on edge cases, error modalities, and
compound interactions rather than the happy path.*

> **Status: COMPLETE (July 2026) — all phases implemented.**
> The three-tier architecture, scenario catalogue, and all open questions
> (§9) are resolved. Two genuine defects were found and fixed along the way
> (**C6** during cataloguing, **F2** by the conformance suite); the **F5**
> finding (`H` vs the guardian availability cap) was ruled on and fixed —
> `H` is now the reveal *horizon*, equal to the availability cap (§6).

## Contents

1. [Goal and philosophy](#1-goal-and-philosophy)
2. [Why not just the Cosmos simulation framework](#2-why-not-just-the-cosmos-simulation-framework)
3. [The three tiers](#3-the-three-tiers)
4. [The four invariants](#4-the-four-invariants)
5. [Scenario catalogue](#5-scenario-catalogue)
6. [Rulings produced by this exercise](#6-rulings-produced-by-this-exercise)
7. [Implementation phases](#7-implementation-phases)
8. [How it runs](#8-how-it-runs)
9. [Open questions for refinement](#9-open-questions-for-refinement)

---

## 1. Goal and philosophy

The happy path is already proven (unit suites + live devnet verification, PR #8).
What is *not* systematically proven is everything else: partial failures,
boundary blocks, rounding residues, compound interactions (a slash on one
secret while another is mid-hold), and error-path retries. These are where
funds are lost and invariants break.

Three principles:

1. **Every scenario is also a conservation proof.** Each test asserts its
   specific outcome (who got paid what, who was slashed how much) *and* runs
   the global invariants at the end. A scenario that pays the right people but
   leaks a uveil from escrow must fail.
2. **Exact amounts, never `>= 0`.** All assertions are to the uveil, derived
   from the base constants (`rate`, `bond_multiplier`) — never hard-coded, per
   the spec's cascade rule.
3. **Drive the real machinery.** Scenarios go through the actual message
   handlers and EndBlock sweeps at controlled block heights — no keeper-method
   shortcuts that could diverge from what a transaction actually does.

## 2. Why not just the Cosmos simulation framework

Honest assessment of the SDK's simulation mechanic for this purpose:

| Property | SDK simulation | What we need |
|---|---|---|
| Random weighted msg sequences over many blocks | ✅ its core strength | Useful (tier 2) |
| Global invariant checks each block | ✅ | Essential (tier 2) |
| Proving *specific* outcomes ("this guardian was slashed exactly 40/10/50") | ❌ — sims assert properties true in *every* state, not expectations for *a* scenario | Tier 1's job |
| Current state in this repo | `x/secrets/simulation` operations were stubs — they ran `ValidateBasic` and returned without delivering anything. **Removed entirely (July 2026)** along with `make sim` and the skipped app-level sim tests: everything they nominally checked is covered by tiers 1–3, which drive the real handlers | Real operations would need funded accounts, valid HMAC share material, multi-block window advancement |
| On-chain invariant module | Crisis module removed in SDK 0.53 — invariants live in tests/sim regardless | — |

**Conclusion:** the sim framework answers only the fuzzing tier, and a custom
keeper-level fuzzer answers it more cheaply (same property-checking power, no
app plumbing, runs in `go test`, failures replay from a seed). The SDK sim
becomes worthwhile later for *cross-module* fuzzing (bank/auth/staking
interplay); the invariant functions built here transplant directly into real
sim operations at that point. Recorded as a deferred follow-up, not part of
this plan.

## 3. The three tiers

### Tier 1 — Conformance suite (the bulk)

Table-driven keeper tests (`x/secrets/keeper/conformance_test.go`) covering the
full catalogue in §5. Infrastructure already exists from the economics work:

- `trackingBankKeeper` — records every module→account send and every burn, so
  tests assert exactly where each uveil went; extended with a **ledger check**
  that computes escrow-in vs escrow-out per scenario
- `setupBondTestSecret` / `setupSlashableGuardian` — full-pipeline and
  direct-state fixtures
- controlled `WithBlockHeight` + direct `ProcessExpiredCommits` /
  `ProcessExpiredRevealWindows` calls for deterministic timing

Each scenario ends with `assertInvariants(t, f)` (§4).

### Tier 2 — Property fuzzer

`x/secrets/keeper/lifecycle_fuzz_test.go`: a seeded, deterministic random
walk —

- N guardians (random floats), M concurrent secrets (random `bump`,
  `threshold`, `shares`, window geometry)
- per block: random subset of actions (register, deposit, withdraw, request,
  distribute, accept, reject, reveal, report-leak, cancel), then both EndBlock
  sweeps, then **all four invariants**
- failure output = the seed; replay with `-run TestLifecycleFuzz -seed N`
- default CI budget ~200 blocks; deep mode via env (`FUZZ_BLOCKS=5000`) for
  periodic long runs — this is the "run them periodically" instinct, baked in

### Tier 3 — E2E devnet scenarios

Cross-component only (daemon behaviour, CLI, real bank module) — the logic is
proven in tiers 1–2, so keep this thin:

- happy path (exists: `make e2e`)
- no-show settlement (observed live during PR #8 verification — script it)
- early-reveal report via CLI + settlement exclusion
- cancellation mid-hold with pro-rata payout

Implemented as `make e2e-scenarios` (`devnet/e2e-scenarios.sh` +
`typescript-sdk/examples/scenario-create.js`), runnable against a fresh
devnet.

## 4. The four invariants

Checked at the end of every tier-1 scenario and after every tier-2 block:

1. **Solvency** — module escrow balance `==` Σ(active secrets' reward pools)
   + Σ(all guardians' locked floats). Exact equality, not `>=`.
2. **Conservation** — total supply delta `==` Σ(burns): entry fees, slash burn
   slices, distribution dust. No other value creation or destruction.
3. **No stranded bonds** — every locked uveil in any guardian float maps to an
   ACCEPTED assignment on a non-terminal secret; terminal secrets hold no
   locks anywhere.
4. **Queue hygiene** — every non-terminal secret has exactly its due entries
   (commit entry iff pre-activation, settlement entry iff pre-settlement);
   terminal secrets have none.

## 5. Scenario catalogue

Status: ✅ covered today · ⬜ gap to fill · 🔴 defect found by cataloguing.
Tier 1 unless marked otherwise.

### A. Registration & float

| # | Scenario | Expected outcome | Status |
|---|---|---|---|
| A1 | Register with balance < entry fee | Rejected; zero state written | ✅ partial |
| A2 | Register with nil/zero deposit | Valid; zero float; fee burned | ✅ |
| A3 | Duplicate registration | Rejected | ✅ |
| A4 | Withdraw with nothing locked | Full float returned; record persists | ✅ |
| A5 | Withdraw with a bond locked | Only unlocked returned; repeat fails | ✅ |
| A6 | Deposit top-up mid-hold, then withdraw | Accounting exact across lock boundary | ✅ |

### B. Selection & acceptance

| # | Scenario | Expected outcome | Status |
|---|---|---|---|
| B1 | High-bump secret vs small-float guardians | Excluded from candidacy (unlocked < B) | ✅ |
| B2 | Too few eligible guardians at Phase 1 | Fails cleanly; **nothing escrowed** (selection precedes pool lock) | ✅ |
| B3 | Acceptance one uveil short of B | Hard reject; no partial lock | ✅ |
| B4 | First-`num_guardians`-to-confirm; N+1th confirmation | Rejected without locking | ✅ |
| B5 | One guardian accepting multiple secrets until float exhausted | Later acceptance rejected; earlier bonds intact | ✅ |
| B6 | Acceptance after commit deadline | Rejected | ✅ |
| B7 | Withdraw unlocked float *after* accepting | Bond survives; settlement returns it correctly | ✅ |

### C. Cancellation

| # | Scenario | Expected outcome | Status |
|---|---|---|---|
| C1 | Cancel during commit phase (elapsed = 0) | Full pool refund; all bonds released | ✅ |
| C2 | Cancel mid-hold | Pro-rata `rate × elapsed × bump` each; remainder to creator | ✅ |
| C3 | Cancel at `reveal_start − 1` | Guardians paid the full pre-window hold; the window span is ALWAYS refunded (spec wording tightened — "almost the full pool" only held when duration ≪ distance) | ✅ |
| C4 | Cancel at/after `reveal_start` | Rejected | ✅ |
| C5 | Wrong sender / double cancel | Rejected | ✅ |
| C6 | **Cancel after an early-reveal slash** | Leaker excluded from the pro-rata wage (slice flows to the creator via the remainder arithmetic) and from bond release (already deducted at report time). Ruling in §6 | ✅ fixed |

### D. Reveal & settlement (parametrise threshold `t`, shares `n`, revealers `r`)

| # | Scenario | Expected outcome | Status |
|---|---|---|---|
| D1 | `r = n` | All paid `P/n`; dust burned; state `revealed` | ✅ |
| D2 | Full outcome grid: 35 combos over `n ∈ {2,5,24}` × threshold corners+interior × `r ∈ {0,1,t−1,t,n}` | Payments never branch on the threshold; only the terminal state does | ✅ |
| D3 | `0 < r < t` | `failed`; revealers still paid; no pool refund | ✅ |
| D4 | `r = 0` | `failed`; pool refunded; all slashed | ✅ |
| D5 | Boundary reveals: at `start`, at `end` (inclusive — paid), at `end+1` (rejected, settled same block) | Per spec window `[start, end]` | ✅ |
| D6 | Duplicate reveal / non-accepted assignee / bad HMAC / cancelled secret | All rejected, no penalty at rejection | ✅ partial |
| D7 | Rounding: odd bumps (101, 333, 999) | burn + creator + returned `== B` exactly; pool splits conserve | ✅ |
| D8 | Commit-refund retry: transient bank failure | Entry retained, state non-terminal; next block retries to success exactly once | ✅ |

### E. Early-reveal reporting

| # | Scenario | Expected outcome | Status |
|---|---|---|---|
| E1 | Valid report pre-window | Full bond 40/10/50; excluded from `P` | ✅ |
| E2 | Report at exactly `reveal_start_block` | Rejected (boundary) | ✅ |
| E3 | Duplicate / already-revealed / non-accepted / self-report / nonexistent | Rejected | ✅ |
| E4 | Reporter *is* the creator | Allowed; nets exactly 60 %; the 40 % burn stands | ✅ |
| E5 | **All** active guardians early-slashed | Zero eligible revealers → pool refunded; no double slash; terminal state REVEALED (the auto-revealed evidence reconstructs) — every incentive lands correctly | ✅ |
| E6 | Leak evidence auto-reveal | Counts toward reconstruction threshold — reconstructable before the window even opens | ✅ |

### F. Timeouts, queues, multi-secret

| # | Scenario | Expected outcome | Status |
|---|---|---|---|
| F1 | Commit-timeout: zero and partial acceptances | Bonds released; pool refunded in full | ✅ |
| F2 | Genesis import with past-due secrets | Settled on the first block (the `≤` drain). 🔴→✅ **Defect found & fixed**: InitGenesis blindly enqueued both entries, leaving imported activated secrets with a bogus commit entry that outlived settlement — caught by invariant 4; fixed with state-aware `RestoreSecretDeadlines` | ✅ |
| F3 | Multiple secrets due in the same block + a stale entry for a terminal secret | All processed; the stale entry self-heals (dequeued without touching the secret) | ✅ |
| F4 | Same guardian on two secrets; slashed on A mid-hold of B | B's bond intact; reveals and settles B normally; float shows exactly one lost bond | ✅ |
| F5 | Horizon exactly `H` / `H + 1` | Validation passes at `H` **and** selection succeeds (a max-availability guardian reaches exactly `reveal_end_block`); `H + 1` rejected. 🔎→✅ **Finding resolved (ruling in §6)**: `H` was ~10 y while availability caps at ~1 y, making long distances unsatisfiable; `H` is now the reveal *horizon* (`reveal_end ≤ created + H`), set equal to `MaxAvailabilityWindow` | ✅ fixed |
| F6 | Terminal secrets never re-processed; queues empty | Sweep no-ops | ✅ |

### G. Tier-2 fuzzer targets (properties, not scenarios)

- Random interleavings of A–F actions across concurrent secrets never violate
  the four invariants
- No sequence of valid messages can make `LockGuardianFloat`/`Unlock`/`Deduct`
  arithmetic go negative or leave residue
- No sequence strands a queue entry or re-processes a terminal secret

## 6. Rulings produced by this exercise

1. **C6 — cancellation after early-reveal slash (CONFIRMED and FIXED, July
   2026):** the slashed leaker is **excluded** from the pro-rata cancellation
   payout and from bond release (its bond is already gone). Its would-be wage
   slice flows to the **creator** via the existing `refund = P − Σ payouts`
   arithmetic — no new destination or constant. Implemented in `CancelSecret`
   and `ReleaseAllAcceptedBonds` (the latter also covers commit-timeout after
   a report); specified in spec.md "Cancellation and No-Fault Refunds"; proven
   by `TestCancellation_ExcludesEarlySlashedLeaker` (full-pipeline scenario
   with per-uveil conservation of both the pool and the slashed bond).
   - *Reporter rejected*: already compensated by the 50 % bond bounty at
     report time; performed none of the holding service the wage pays for.
   - *Burn rejected*: the pool is the creator's money; burning it punishes the
     victim of the leak.
   - *Consistency*: each path's forfeited value follows that path's default
     remainder flow — settlement → revealers (reveal-dominance incentive),
     cancellation → creator (unearned-remainder principle). Same exclusion
     rule, same shape. Self-dealing bound holds: creator-as-reporter nets
     10 % + 50 % of the bond + their own unearned wage back; the 40 % burn
     stands, so no engineered-profit path exists.
   - Requires: spec.md cancellation section update + code fix + tests (C6
     scenario and E4/E5 companions).

2. **F5 — reveal cap vs guardian availability (RULED and FIXED, July
   2026):** `H` is **lowered to the guardian availability cap** and
   redefined as the reveal *horizon*: `reveal_end_block ≤ created_at + H`
   with `H = MaxAvailabilityWindow = 5,256,000` blocks (≈ 1 year). The
   horizon — not the priced distance — is what selection can actually
   staff, so validating it directly makes the cap exactly reachable: a
   freshly registered max-window guardian reaches `reveal_end_block` with
   zero slack (proven by the reworked F5 conformance test, which asserts
   *success* at exactly `H`). The priced distance is bounded as a
   consequence (`distance ≤ H + 1 − commit_timeout`).
   - *Ten-year `H` rejected*: validation must never promise a window
     selection cannot staff; a decade-long guardian commitment is
     unrealistic to demand up front.
   - *Far-future reveals*: achieved by cancel-and-recreate cycles — the
     paid pro-rata cancellation makes this a first-class pattern and the
     intended basis for a future dead-man's-handle feature.
   - *Deferred*: raising `H` beyond the availability cap awaits guardian
     handoff/bond-transfer mechanics (explicitly out of scope for now).
   - Also fixed in the same pass: the guardian daemon double-converted
     availability offsets to absolute heights before submitting
     (`registration.go`) — invisible on a low-height devnet, badly wrong at
     real heights; it now passes relative offsets through as the message
     expects, defaulting to the full availability window.

## 7. Implementation phases

- **Phase 0 — C6 fix (spec-first).** ✅ **DONE** — spec.md updated,
  `CancelSecret` and `ReleaseAllAcceptedBonds` exclude early-slashed
  guardians, `TestCancellation_ExcludesEarlySlashedLeaker` proves it.
- **Phase 1 — Invariant library.** ✅ **DONE** — `invariants_test.go`:
  `assertInvariants` (stranded bonds + queue hygiene) wired into every
  economics suite; `ledgerBankKeeper` + `assertSolvency` for the exact
  escrow identity on full-pipeline scenarios.
- **Phase 2 — Conformance suite.** ✅ **DONE** — every catalogue gap filled
  in `conformance_test.go` (35-combination D2 grid included). Produced one
  fixed defect (F2 genesis enqueue) and one finding (F5: `H` vs the
  guardian availability cap — since ruled on and fixed, §6 ruling 2).
- **Phase 3 — Lifecycle fuzzer.** ✅ **DONE** — `lifecycle_fuzz_test.go`:
  settlement-biased + chaos profiles, five fixed CI seeds at 500 blocks
  (~0.7 s), `FUZZ_BLOCKS` / `FUZZ_SEED` env overrides, full invariant
  library (incl. exact solvency) after EVERY block, end-of-run drain
  asserting every secret terminal. Exit met: 5 × 5,000-block deep run
  clean (~36 s) plus rotating spot seeds.
- **Phase 4 — E2E scenario scripts.** ✅ **DONE** — `make e2e-scenarios`
  (`devnet/e2e-scenarios.sh`): S1 no-show slash (daemon killed
  post-acceptance), S2 mid-hold cancellation, S3 early-reveal report with
  real share evidence (creator-as-reporter). All amounts asserted to the
  uveil via `block_results`/`tx` events; `scenario-create.js` keeps each
  guardian's plaintext share in a manifest as valid leak evidence. Exit met:
  26/26 assertions green in a single pass against a fresh 2-second-block
  devnet. One operational nuance surfaced: `MsgCancelSecret` emits two
  `secret_cancelled` events per tx (the FSM's bare state-transition event
  plus the economics event) — consumers must select by attribute, not
  position.
- **Deferred** — real SDK simulation operations for cross-module fuzzing
  (transplants tier-2 invariants); revisit after the guardian code review.
  The old no-op sim stubs (`x/secrets/simulation`, `make sim`, the skipped
  app-level sim tests, and the module's `AppModuleSimulation` wiring) were
  **removed** in July 2026 — they never delivered messages and are no head
  start for the real thing, which would be written from scratch against the
  invariant library.

## 8. How it runs

| Cadence | What | Where |
|---|---|---|
| Every `make test` | Conformance suite + fuzzer (CI budget, fixed seed set) | `x/secrets/keeper` |
| Periodic / pre-release | Deep fuzz (`FUZZ_BLOCKS=5000`, rotating seeds) + `make e2e-scenarios` | manual or scheduled CI |
| Per protocol change | The relevant catalogue group extended *first* (spec-first, then test, then code) | this document updated |

## 9. Open questions — all resolved (July 2026)

1. **C6 ruling** — confirmed and fixed (§6).
2. **D2 grid size** — corners + one interior point per `n ∈ {2, 5, 24}`
   (full sweep rejected as slow with no added discrimination).
3. **Fuzzer action weights** — biased toward accept/reveal so runs reach
   settlement often, plus a "chaos" profile (uniform weights, higher
   cancel/report rates) in the rotating seed set.
4. **Tier-3 assertions** — scripts assert on-chain events and amounts via
   `block_results` + `jq` (lifted from the PR #8 manual verification
   commands), not just exit codes.
5. **D8 fault injection** — a test-only failing bank keeper following the
   `trackingBankKeeper` pattern; no production code changes.
