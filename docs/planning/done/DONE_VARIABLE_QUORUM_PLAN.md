# Variable Quorum — Client-Chosen `[min, max]` Guardian Range

*Replaces the fixed 30% over-selection buffer (PROTOCOL.md Oddity #1) and the
first-`n`-to-confirm race (Oddity #8) with a creator-chosen guardian **range**
`[min_shares, max_shares]`, its width governed with reference to `threshold`. The
protocol selects `max` candidates and distributes a share to each up front,
exactly as today. Once `min` guardians confirm the secret is **locked in**
(guaranteed to proceed), but confirmation stays open to the rest up to `max`
until the timeout block, where the roster finalises. There is no first-`n` gate,
no blind constant, and no reordering of the existing three-phase commit — the 30%
buffer simply becomes an explicit, validated band the creator tunes.*

> **Status: `done` (25 July 2026, branch `worktree-variable-quorum`).**
> Originated from a design session on PROTOCOL.md Oddity #1 ("the 30%
> additional guardian feature — solves a real problem, but not cleanly"). The
> design is settled, including the client preset (§5, resolved July 2026).
> Executed across all six phases; verified by `make verify`/`make test`, the
> full-lifecycle devnet e2e, and the failure-path scenario suite (34
> assertions green).
>
> **Scope note.** This plan deliberately does **not** touch when share material
> is delivered: shares are still distributed to all `max` candidates in Phase 2,
> exactly as today. It therefore does **not** close Open Defect #1 (the
> collateral-free pre-acceptance leak); the gap constraint (§2.1) *bounds* the
> never-confirmed portion of that surface but the pre-bond window remains. A
> separate accept-then-distribute change would be the full fix, and is out of
> scope here.

**Origin**: PROTOCOL.md Load-Bearing Oddity #1 (30% buffer) and Oddity #8
(first-to-confirm).
**Priority**: P1 — protocol structure, pre-testnet.
**Components**: `x/secrets/types` (`constants.go`, selection/economics helpers,
message `ValidateBasic`), proto (`Msg`/`Secret`/`query`), `x/secrets/keeper`
(`guardian_selection.go`, `msg_server_request_guardians.go`,
`msg_server_confirm_shares.go`, `endblock_logic.go`, `secret_state_machine.go`),
`docs/spec.md`, `docs/operations.md`, `docs/PROTOCOL.md`, the TypeScript SDK, the
mobile client (`mobile-client/`), and the conformance/e2e suites.

## Contents

1. [What this solves](#1-what-this-solves)
2. [Design](#2-design)
3. [Protocol deltas](#3-protocol-deltas)
4. [Interaction with the load-bearing oddities and security observations](#4-interaction-with-the-load-bearing-oddities-and-security-observations)
5. [Open questions](#5-open-questions)
6. [Implementation phases](#6-implementation-phases)

---

## 1. What this solves

Today the buffer bakes two decisions into one blind constant (PROTOCOL.md
Oddity #1): **how many extra guardians to select** for acceptance-failure
tolerance, and **who ultimately staffs the secret** (the first `requested_shares`
to accept). Both are taken out of the creator's hands and fixed at 30%. The
costs, as documented:

- **Blind sizing.** 30% is a fixed constant, independent of the network's real
  acceptance rate — simultaneously too tight on a flaky network and pure waste on
  a reliable one. The creator, who alone knows their robustness needs, has no
  lever.
- **Burned guardian fees + latency bias (Oddity #8).** Because only the first
  `requested_shares` acceptances win, buffer guardians who lose the race pay a
  transaction fee purely to learn they lost, and the race systematically favours
  low-latency guardians.

This plan replaces the constant with a **creator-chosen band**:

- The creator picks `threshold`, then `[min_shares, max_shares]` (with the
  relational validation in §2.1, whose width is governed with reference to
  `threshold`).
- Over-selection becomes `max − min`, chosen by the creator, not a fixed 30%.
- The race disappears: everyone who confirms within the band by the deadline is a
  real, activated participant — there is no first-`n` gate to lose.

What it deliberately does **not** fully close: the over-distribution of live share
material to non-confirming candidates (Open Defect #1). Shares still go to all
`max` candidates up front, because acceptance still means "I decrypted my share
and verified its HMAC". On any secret that *activates*, the gap constraint
(§2.1) keeps the *never-confirmed* set below `threshold`, so that set can never
reconstruct on its own — a real bound — but the pre-bond window (all `max`
recipients hold live shares before any bond locks) is unchanged from today, and
a secret that *fails* below `min` leaves all `max` recipients holding shares
with no bond ever posted, exactly as today's commit-timeout failure. See §4.

## 2. Design

### 2.1 The range model

The creator specifies a **range** `[min_shares, max_shares]` instead of a single
`requested_shares`, plus the reconstruction `threshold` as today.

- **Selection** draws exactly `max_shares` candidates by the existing hash
  sortition (unchanged seed, eligibility, tie-break). There is **no separate
  buffer constant** — the margin *is* `max − min`, chosen by the creator.
  `calculateBuffer` is deleted. If `max` eligible candidates cannot be selected
  on the request call, the transaction **fails**, exactly as today (§3.3).
- **Distribution** is unchanged: Phase 2 stores an encrypted share for each of
  the `max` selected candidates (same message, same flow — see §3.2).
- **Lock-in** is the moment `accepted_count` first reaches `min_shares`: the
  secret is then guaranteed to proceed (acceptances are never revoked, so the
  count can only hold or grow). This is **inferred by clients** from
  `accepted_count ≥ min_shares` — there is **no on-chain event** and **no state
  transition**; the secret stays in `awaiting_acceptance`.
- **Confirmation stays open** after lock-in: further candidates may confirm up to
  `max_shares` until the timeout block. There is no race — everyone who confirms
  by the deadline is in.
- **Finalisation** happens at the timeout block: if `accepted_count ≥ min_shares`
  → `pending` with *exactly the accepted set*; otherwise → `failed`, bonds
  released, pool refunded to the creator (identical to today's commit-timeout).
- **Relational validation (hard).**
  `MinThreshold ≤ threshold ≤ min_shares ≤ max_shares ≤ MaxTotalShares (32)`,
  **and** `max_shares − min_shares < threshold`.
  - `threshold ≤ min` is required: a `min`-sized activation must still be able to
    reach `threshold` reveals.
  - `max − min < threshold` (strict) is the buffer-safety bound: on any secret
    that activates, at least `min` guardians have confirmed, so the
    never-confirmed set is at most `max − min`; keeping it strictly below
    `threshold` ensures the guardians who receive a part but never bond can
    never be a reconstruction-capable set on their own. (The bound is
    conditional on activation — a secret that fails below `min` leaves up to
    `max` never-bonded share holders, as today; see §4.)
  - `min == max` (a zero-width band, no tolerance) is permitted — a legitimate
    "exactly this many" request.

**Enforcement is on-chain-authoritative and mirrored, pinned against drift.** The
full validation (ordering **and** the strict gap bound) is enforced by
`ValidateBasic` in `x/secrets/types` — the authoritative wire contract — and
re-implemented client-side in **both** the TypeScript SDK and the mobile client
for pre-submission UX. This is exactly the surface that has drifted before (two
TS clients diverging on a chain-enforced bound — see the client-consolidation
plan), so the `(threshold, min, max) → valid/invalid` matrix must be pinned by a
shared `testdata/vectors/` corpus that the chain and every client assert against,
rather than each re-deriving the rule.

**A low threshold forces a narrow band — by design.** Combined with `threshold ≤
min`, the gap bound means a low threshold permits almost no over-selection
(`threshold = 2` allows only `max ≤ min + 1`). This is unavoidable, not a defect:
if any 2 shares reconstruct, any 2 idle recipients are dangerous, so a
low-threshold secret simply cannot carry much safe redundancy. Permissible
over-selection scales with `threshold`, which is a self-regulating property
spec.md should state explicitly.

**Client ergonomics.** The client exposes this as "target + margin" and keeps
**30% as the default preset**, *clamped to the gap bound*:
`max = min(ceil(min × 1.3), min + threshold − 1)`. Current behaviour is the
default; only advanced creators tighten or widen the band. A tight band is
cheaper in guardians actually staffed but carries higher activation-failure risk;
a wide band is more robust but a larger accepted set shares the fixed pool (§3.4).

**Variable redundancy is an intentional, documented semantic change.** With
`threshold = 5`, a `max = 9` activation tolerates 4 reveal-time no-shows
(5-of-9); a `min = 6` activation tolerates only 1 (5-of-6). The creator's
robustness guarantee is therefore a *range*, and **`min` close to `threshold` is
doubly fragile** — fragile both to activation failure and to reveal-time griefing
(fewer malicious no-shows needed to push below `threshold`). spec.md documents
this so it does not surface as a surprise; the client should visualise the
low-end slack, not just the launch probability.

**No new collusion surface.** A colluder reconstructs by *holding* `≥ threshold`
shares, which is governed by the protocol-random draw over `max` candidates — not
by the accepted-set denominator. Declining removes only the colluder's own
shares, never honest ones, so it cannot help an attacker. Selecting `max` (a
larger draw) for the same `threshold` marginally *raises* the bar to control
`threshold` slots.

## 3. Protocol deltas

### 3.1 Proto / message changes

- `MsgRequestGuardians`: replace `requested_shares` with `min_shares` and
  `max_shares` (keep `threshold`). *(Field-number handling: reserve the old
  field, add two new ones; the `SecretView` field-number-stability contract in
  `query.proto` must be preserved — new fields append.)*
- `Secret` (slim record): store `min_shares`, `max_shares` (replacing `shares` /
  `requested_shares`); `accepted_count` semantics unchanged; the finalisation
  predicate reads `accepted_count ≥ min_shares`.
- No new messages and no split of `MsgDistributeShares` — the distribution flow
  is untouched.
- No new event type — lock-in is inferred by clients from `accepted_count ≥
  min_shares` (§2.1).

### 3.2 FSM

**Unchanged states**: `reserved → awaiting_acceptance → pending`. The single
`commit_deadline` is retained; no new state and no second deadline.

Only the **finalisation predicate** at the deadline changes:

- Current: the first `requested_shares` acceptances win and flip the secret to
  `pending` immediately; the rest are turned away; timeout with fewer accepted →
  `failed`.
- Proposed: acceptances accumulate through `awaiting_acceptance` up to `max`;
  reaching `min` makes the secret **locked-in** (guaranteed to finalise to
  `pending`), inferable by clients from `accepted_count ≥ min_shares` — no state
  change and no event (this keeps `pending` meaning "roster frozen", keeps all
  confirmations in one state, and keeps `MsgCancelSecret` `pending`-only so it
  cannot fire mid-window). At the deadline, `ProcessExpiredCommits` finalises:
  `accepted_count ≥ min_shares` → `pending` with the accepted set, else
  `failed`, bonds released, pool refunded.

`ProcessExpiredCommits` (`endblock_logic.go`) is the touch-point: its
"insufficient acceptances" branch changes from a `requested_shares` comparison to
a `min_shares` comparison, and it no longer needs to discard above-`requested`
acceptances (there is no ceiling below `max`, and `max` is already the candidate
count).

### 3.3 Selection and eligibility

- Strict gate moves from `shares + buffer` to **`max_shares`** eligible
  candidates; `calculateBuffer` is deleted. As today, if fewer than `max`
  eligible guardians can be selected on the request call, the whole transaction
  **fails** — no reduced-band fallback.
- `MaxShares`' `24` cap existed solely to leave room for `shares + 30% ≤ 32`;
  with the buffer folded into the range, `max_shares` may range up to
  `MaxTotalShares (32)` directly. `MinShares` becomes the floor of the *range*
  (still `≥ threshold`).
- Sortition, seed, eligibility predicate, and per-candidate bond freeze are
  otherwise unchanged.

### 3.4 Pricing and escrow — no refunds

- The reward pool `P` (`P = rate × distance × max_shares × bump ÷ 100`) is
  established on **`max_shares`** at request time and is **fixed** — there is no
  activation-time refund of unfilled slots. `rate` is the per-share-per-block
  price, `distance` the reveal time-distance, `bump` the creator's tuning
  multiplier.
- If the secret does not attain `max` acceptances, the fixed pool simply splits
  among the guardians that did accept (`≥ min`). Fewer acceptances therefore mean
  a larger per-guardian payout for the same creator cost; the creator always pays
  `P(max)`, and the trade for a wide band is redundancy, not price.
- Per-guardian bond `B_g` is unchanged (independent of count); more acceptances =
  more total bonded collateral, fewer = less, consistent with existing
  economics. `B_g` is the frozen per-secret bond locked from a guardian's float
  at acceptance.
- The pro-rata cancellation payout keeps its shape but its per-guardian
  denominator moves from `requested_shares` (deleted) to **`max_shares`**:
  `per-guardian payout = P × elapsed ÷ (distance × max_shares)`. This keeps the
  per-guardian-per-block wage rate constant regardless of how many accepted, and
  means the unearned remainder refunded to the creator includes the unfilled
  slots' portion. (Deliberate asymmetry with settlement, where unfilled slots
  enrich revealers — cancellation is a refund path, settlement a reward path.)
  The distance definition is unchanged.

### 3.5 Settlement

- Threshold-independent payout is unchanged; the payee set is now the variable
  accepted set. The **fixed** `max`-priced pool splits equally among actual
  revealers; dust burned. Unfilled activation slots and reveal-time no-shows both
  simply leave a smaller set to divide the same pool (§4, Oddity #9).
- No-show slashing is **unchanged in kind**: acceptance still means the guardian
  decrypted and verified its share in Phase 3, so an accepted-but-did-not-reveal
  guardian is culpable exactly as today. (This is why accept-then-distribute is
  out of scope — moving material after the bond is what would force a "was a share
  actually delivered?" gate on the slash. It is not needed here.)

### 3.6 Constants

- Delete `calculateBuffer` and the 30% buffer logic from `guardian_selection.go`;
  drop the buffer references in `constants.go` comments.
- Relax `MaxShares` toward `MaxTotalShares`; keep `MinThreshold`/`MaxThreshold`
  and `MinShares` (as the range floor).
- No new constants required.

## 4. Interaction with the load-bearing oddities and security observations

- **Oddity #1 (30% buffer)** — resolved as a *sizing* decision. Over-selection
  becomes a creator-tunable band rather than a blind constant. **Over-distribution
  is not eliminated** — shares still go to all `max` candidates — but its size is
  now the creator's explicit choice, bounded by `max − min < threshold`.
- **Oddity #8 (first-to-confirm)** — resolved. No race; everyone in `[min, max]`
  who confirms by the deadline participates. The latency bias and
  fee-to-learn-you-lost both disappear.
- **Open Defect #1 (collateral-free pre-acceptance leak)** — **bounded, not
  closed.** Phase 2 still hands a decryptable share to every `max` candidate, and
  bonds still lock only at acceptance. On any secret that activates, the gap
  bound `max − min < threshold` guarantees the *never-confirmed* set is
  sub-threshold, so idle recipients can never reconstruct on their own — a real
  improvement over the unconstrained 30% buffer. The bound is conditional on
  activation: a secret that fails below `min` leaves all `max` recipients
  holding live shares with no bond ever posted (unchanged from today's
  commit-timeout failure), and during the confirmation window, before any bond
  locks, all `max` recipients hold live shares; an attacker who gets
  `≥ threshold` of their own guardians *selected* can reconstruct at
  distribution time regardless of `min`/`max`. The residual mitigations there are unchanged from today
  (protocol-random selection needing a large population fraction, plus the sunk
  1,000 VEIL entry fee). spec.md and PROTOCOL.md must state plainly that this plan
  bounds but does not close the surface; accept-then-distribute remains the only
  full fix and is out of scope.
- **Oddity #9 (no-shows enrich revealers)** — **generalised.** The pool is fixed
  at `P(max)` and always divides among actual revealers, so *any* slot that never
  ends in a reveal — unfilled at activation **or** a no-show at reveal — raises
  the per-revealer payout. The creator pays `P(max)` regardless. This is a
  deliberate, uniform extension of the existing reveal-time behaviour, documented
  as such.
- **Security §1 (selection grinding)** — unaffected and still requires the
  request-time creation fee ([done/DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md](done/DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md),
  owned by [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md)).
  Selection stays protocol-controlled and the flow stays multi-interaction with
  the first binding.
- **Security §3 (one share, one holder, forever)** — preserved. Winners hold
  their share forever and are bonded; non-confirming candidates never accept and
  never bond. No transfer mechanic is introduced.
- **Oddity #2 (three-phase, one deadline)** — preserved. Still three phases, one
  `commit_deadline`, first interaction financially binding. Only the finalisation
  arithmetic at the deadline changes; the anti-grinding shape is untouched.

## 5. Open questions

None — all decisions are settled.

1. **Default `min` relative to `threshold` — RESOLVED (July 2026).** `min` is
   the creator's explicit guardian-count input (playing the role today's
   `shares` field plays); the client imposes no default cushion above
   `threshold`. The client derives only `max`, keeping the 30% spread capped by
   the gap bound: `max = min + min(ceil(0.3 × min), threshold − 1)` —
   equivalently `max = min(ceil(min × 1.3), min + threshold − 1)`.

## 6. Implementation phases

1. **Spec-first** — `docs/spec.md` (range fields and validation including the
   `max − min < threshold` bound and the low-threshold-forces-narrow-band
   property, lock-in-as-client-inference (no event, no state change),
   finalisation-at-deadline, the fixed no-refund pool,
   and an explicit statement that Open Defect #1 is bounded not closed),
   `docs/operations.md` (changed `MsgRequestGuardians` fields), `docs/PROTOCOL.md`
   (retire Oddity #1 as a sizing decision and Oddity #8; restate Open Defect #1
   with the new bound; generalise Oddity #9 to the fixed pool) ahead of code.
   **Sweep every reference to the 30% over-selection buffer and replace it with
   the `[min, max]` model** — this is a documentation change in its own right, not
   only a code one. Known locations: PROTOCOL.md Oddity #1, Oddity #8, the
   Configuration Parameters / constants table (the `MinShares`/`MaxShares`,
   `MaxTotalShares`, and `calculateBuffer` rows all cite "shares + 30% buffer"),
   Open Defect #1, and the guardian-selection description; plus the corresponding
   selection and pricing sections in spec.md and operations.md. A grep for
   `30%`, `buffer`, and `calculateBuffer` across `docs/` gates completeness.
2. **Types & proto** — `min_shares`/`max_shares` fields (reserve
   `requested_shares`), `Secret` range fields, authoritative `ValidateBasic` for
   the relational constraints (ordering + strict gap bound + `min == max`
   allowed), a shared `testdata/vectors/` validation matrix, deleted buffer
   constant, relaxed `MaxShares`. No new event type. Sweep every `shares` /
   `requested_shares` reader (grep-driven, per the ShareIndex-removal lesson in
   CLAUDE.md): the rename touches `x/secrets` (types, keeper, economics, query)
   and both clients; the guardian module is confirmed clear (it reads its own
   assignment, not the request-time share count). (`tools/econsim`, which
   consumed the pricing core, has since been decommissioned — see
   done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md §6 — so no simulator update is
   needed.)
3. **Keeper** — selection gate on `max` (hard-fail on `< max`); `calculateBuffer`
   removed; acceptance without the first-`n` gate, accumulating to `max` in
   `awaiting_acceptance`; lock-in inferable from `accepted_count ≥ min` (no event,
   no state change); finalisation predicate at the deadline (`≥ min` → `pending`
   with the accepted set, else `failed`); fixed pool split among revealers at
   settlement (no refund path).
4. **Conformance + e2e** — new scenarios: range activation at `min`, at `max`, and
   at an intermediate count; more guardians confirm after `min` is reached, before
   timeout; failure below `min` (full refund, bonds released); gap-bound validation
   rejections; fixed-pool split arithmetic under variable accepted `n`; invariants
   (solvency, conservation, no-stranded-bonds, queue hygiene) hold.
5. **Clients (TypeScript SDK + mobile)** — create flow emits `[min, max]`
   (default preset `max = min(ceil(min × 1.3), min + threshold − 1)`);
   client-side validation of the ordering + gap bound in both clients, asserted
   against the shared vector matrix (Phase 2) so the two cannot drift from the
   chain; distribute and reconstruct flows unchanged; recompute the
   `TerminalSecretRecord` golden (wire shape changed — pre-launch, deliberate).
