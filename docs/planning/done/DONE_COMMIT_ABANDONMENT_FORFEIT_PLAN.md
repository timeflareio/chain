# Commit Abandonment — Request-Time Pricing & Cancellation Restriction

**Status**: DONE (July 2026) — this plan's own scope shipped in PR #59 (`feat(secrets)!: restrict cancellation to pending`): the cancellation restriction, its unit/conformance tests (C1/C2, C5), the S4 e2e scenario, and the same-session doc updates. The one residual — the non-refundable **creation fee** `max(draw_fee_unit, pool_fee_percent × P)` — is owned by [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) §5.4b/Phase 2 (simulation-gated); PROTOCOL.md Security Observation §1 closes fully when it ships. **Restructured July 2026 by ruling: the exit-time forfeit is dropped.** Abandonment is priced by the non-refundable **creation fee** charged at `MsgRequestGuardians`, owned by [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) §5.4/Phase 2, whose fee takes this plan's `max(flat floor, percentage of P)` shape. This plan's own scope — the **pre-activation cancellation restriction** plus its tests and docs — is unblocked and implementable now; the fee rides token-economics Phase 2 (simulation-gated, see Sequencing). The original exit-time-forfeit framing is preserved in git history.
**Origin**: created July 2026 from PROTOCOL.md Security Observations §1 ("the highest-value open protocol fix"), spun out of [DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md](DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md) by ruling
**Priority**: P0/P1 — protocol economics, pre-testnet
**Components**: `x/secrets/keeper/msg_server.go` (`CancelSecret`), `x/secrets/keeper/conformance_test.go`, `docs/spec.md`, `docs/operations.md`, `docs/PROTOCOL.md`. The creation fee itself lands with the token-economics plan's Phase 2 components (`economics.go`, `constants.go`, `msg_server_request_guardians.go`).

## What this plan does (as restructured)

1. **Restricts cancellation to `pending`** (ruled July 2026): cancellation is
   a mechanic reserved for *after* guardian confirmation — `MsgCancelSecret`
   becomes valid only from `pending`. The paid pro-rata exit exists to
   release *bonded* guardians; before activation there is no committed
   guardian set to release.
2. **Contributes the fee shape** to the token-economics creation fee:
   `creation_fee = max(draw_fee_unit, pool_fee_percent × P)`, collected at
   `MsgRequestGuardians`, non-refundable on every path. This makes every
   Phase-1 selection draw **financially binding at request time**, closing
   the abandon-and-refund grinding loop without touching any exit path.

Together these close PROTOCOL.md Security Observations §1.

## Why

### The attack (verified behaviour before this plan)

1. Submit `MsgRequestGuardians` (minimum 20-block commit timeout) and read the
   selected guardian set from the response.
2. If the set doesn't contain enough colluding/sybil guardians, walk away.
   Either exit path refunds the pool **in full**:
   - **Commit-timeout**: `processExpiredCommit` refunds `P` entirely — no
     forfeit, no draw fee.
   - **Immediate cancellation**: `MsgCancelSecret` during the commit phase has
     `elapsed = 0`, so the pro-rata arithmetic refunds everything — and it is
     *faster* than waiting for the deadline.
3. Repeat, in parallel across many pending secrets (each request draws its own
   seed via the per-request counter), keeping only draws where the attacker's
   guardians hold ≥ threshold of the assignments.

Cost per rejected draw: one transaction fee. The selection-hardening plan
removes the creator's ability to *bias* a draw, but explicitly does not make
draws *costly* — its D2/D3 analysis records this residual and defers it here.
Successful grinding defeats protocol-controlled selection and skews guardian
workload toward the attacker's guardians.

### Why the two-step commit cannot simply be collapsed

Analysed and settled in PROTOCOL.md Oddity #2: the client needs the guardians'
encryption keys before it can encrypt shares, and any single-transaction design
requires predicting the selection — every predictable-seed scheme reintroduces
cheap re-rolls. Two interactions, where **the first is financially binding**,
is the minimum structure for grinding resistance. The protocol has the right
shape; pricing the first interaction is what this plan (via the creation fee)
delivers.

## Design (as restructured)

### D1 — Request-time pricing beats exit-time forfeiture (ruled July 2026)

The earlier draft charged a forfeit at exit (commit-timeout, and originally
pre-activation cancellation too). The ruling replaces this with a fee
**collected at request**, which is strictly better on every axis:

- **Exit-agnostic.** An exit-time forfeit must enumerate the exits — the
  cancellation bypass, the garbage-shares path — and every future exit is a
  new hole to remember. A fee collected before any exit exists cannot be
  dodged by any exit, present or future. The entire exit-enumeration class
  of analysis disappears.
- **The implementation collapses.** No forfeit arithmetic in
  `processExpiredCommit`, no changes to any refund path, and `P` stays
  **fully refundable** on commit-timeout — the existing conformance
  assertions remain true.
- **The spec story gets cleaner, not messier**: "the pool is always refunded
  in full if the commit fails; the fee is the price of the draw" — no
  retitling of the no-fault-refund sections.
- **Deterrence is equivalent when sized equally**: the attacker's per-draw
  cost is identical whether collected at entry or exit, and the grind is
  necessarily on the attacker's own real secret, so a percentage of `P`
  scales with the value being attacked.

What is deliberately given up: "honest creators never pay". That property
was already surrendered when the token-economics rulings adopted a
non-refundable pool fee at request time — completers were paying it anyway;
this ruling just stops abandoners paying *twice* under two mechanisms.

### D2 — The fee shape: `max(draw_fee_unit, pool_fee_percent × P)`

Both terms are needed, exactly as in the original forfeit design:

- **`pool_fee_percent`** (proposed **10 %** — token plan Q1) scales the cost
  of a draw with the secret's own parameters: grinding a large, long,
  high-`bump` secret (the kind worth attacking) costs proportionally more
  per re-roll.
- **`draw_fee_unit`** floors the cost for minimal secrets, where 10 % of a
  tiny `P` would make grinding nearly free. **Derived, never absolute**:
  `draw_fee_unit = rate × draw_fee_multiplier`, with
  `draw_fee_multiplier = 14,400` proposed — i.e. the floor is **one
  guardian-day of wages** (1.44 VEIL at v1 values), a memorable anchor in
  the token plan's §5.1 ladder terms. The floor binds below
  `P = 14.4 VEIL` (≈ 10 guardian-days).

Values are **merged into the token plan's Q1** and confirmed against the
economic-simulation report like every other number.

### D3 — Destination: validators (supersedes the burn)

The original forfeit burned, for two reasons: never route abandonment value
to the assigned guardians (in the grinding scenario some are the attacker's
sybils — that rejection **stands**), and burning kept the self-dealing
invariant trivially airtight. Routing to **validators** (fee-collector →
distribution module, the same flow as the rest of the creation fee) is
equally safe: an attacker-validator recoups only a negligible pro-rata slice
of its own fee, and consensus security is what the fee buys. One flow, no
split logic, and it strengthens the security budget the token plan
identifies as the genuine problem. The deflation story loses only its
smallest projected sink.

### D4 — Cancellation restricted to `pending` (ruled July 2026)

`MsgCancelSecret` is valid **only from `pending`**. Pre-activation, a secret
now has exactly one way to end: the commit-timeout, which refunds `P` in
full (the draw was already paid for at request). Consequences:

- The cancel-instead-of-timeout bypass is closed **structurally**: every
  abandoned draw sits out the full commit timeout (≥ 20 blocks), throttling
  parallel grinding loops on top of pricing them.
- The `awaiting_acceptance` garbage-shares path (distribute threshold-many
  junk shares so the secret can never activate, then ride the timeout to a
  refund) still refunds `P` in full — correctly, because the draw fee was
  already collected; there is nothing left to protect at exit.
- Honest costs, both accepted: a creator whose secret genuinely fails to
  staff paid the same draw fee as everyone else (uniform price, no exit
  adjudication); a creator who changes their mind pre-activation waits out
  the commit timeout — bounded at 200 blocks (~20 minutes), their own
  Phase-1 choice.
- Post-activation cancellation from `pending` is untouched (pro-rata
  settlement, available from the moment of activation).

### D5 — What this plan does not do

- **No proto changes.** The cancellation restriction is message validation;
  the fee is charged from values already computed at request time.
- **No claim to eliminate grinding.** It prices each draw (fee + gas) and
  throttles it (the timeout wait); an attacker can still buy re-rolls at a
  per-draw cost that scales with the target. The threshold and sybil
  economics (burned entry fees, per-secret floats) remain the structural
  defences.

## How

1. **Cancellation restriction** (`CancelSecret`): reject
   `reserved`/`awaiting_acceptance` — valid state becomes `pending` only.
   Message-validation change; consensus-breaking; no proto change.
2. **The creation fee**: implemented entirely in token-economics Phase 2
   (`PoolFeePercent` + `DrawFeeMultiplierBlocks` constants, a
   `CreationFee(P)` helper in `economics.go`, charged in
   `MsgRequestGuardians` funding, routed to the fee collector in the same
   flow). This plan adds **no fee code of its own**.
3. **Tests**: conformance C1 (commit-phase cancel) flips from asserting a
   full refund to asserting **rejection**; F1 (commit-timeout) continues to
   assert the full `P` refund — now correct by design, the fee never enters
   escrow — plus a new assertion that the creation fee was collected at
   request and not refunded; a `pending`-state cancel immediately after
   activation still succeeds (the mechanic that replaces pre-activation
   cancel); `make e2e-scenarios` gains a rejected-commit-phase-cancel
   assertion (the abandoned-draw fee assertion lands with token Phase 2).
4. **Cross-component check** (the ShareIndex lesson, CLAUDE.md): grep the
   TypeScript SDK and the mobile-app plan for client-side cancellation state
   validation or "cancel any time before reveal" copy — align them with
   `pending`-only in the same session rather than relying on chain rejection
   (the guardian daemon never cancels; no impact there).
5. **Docs, same session**: spec.md — "Secret Cancellation" state validation
   becomes `pending` only (current text permits "any non-final state");
   "Cancellation and No-Fault Refunds" keeps its title (commit-timeout
   remains a full-`P` refund) with the creation fee documented under Secret
   Pricing; operations.md — `MsgCancelSecret` constraints; PROTOCOL.md —
   the state-machine diagram loses the `reserved`/`awaiting_acceptance` →
   `cancelled` edges, `MsgCancelSecret` preconditions updated, funds-flow
   table gains the creation fee row, Security Observations §1 closed.

## Decisions & merged questions

1. ~~Forfeit values~~ **Merged into token-economics Q1** (July 2026):
   `pool_fee_percent` (10 % proposed) and `draw_fee_unit`
   (`rate × 14,400` = 1.44 VEIL proposed), confirmed against the
   economic-simulation report.
2. ~~Fold into the pool fee vs keep both~~ **Resolved (July 2026): folded.**
   One request-time mechanism prices both consensus security and the draw;
   the earlier "keep both" leaning is superseded — two stacked charges
   bought no additional structure once the fee moved to request time.
3. ~~Honest-failure sympathy (waive on demonstrated distribution)~~
   **Resolved (July 2026): moot.** Nothing is charged at exit, so there is
   nothing to waive; the fee is a uniform draw price paid identically by
   every creator, and no exit-time adjudication of intent exists to be
   gamed.

## Sequencing

- **The cancellation restriction is independent and small** — land it
  pre-testnet whenever convenient (consensus-breaking; free pre-launch).
- **The fee rides token-economics Phase 2**, which is gated on Q1 /
  the economic-simulation report (containerisation → simulation → values).
  If abandonment pricing is wanted before that report lands, the flat floor
  (`draw_fee_unit`) can ship first as a standalone request fee and the
  percentage term follows with Phase 2 — one constant now, one later.
- Together with the selection-hardening plan (bias removed) and this pricing
  (re-rolls costly + throttled), the grinding story is complete; closes
  PROTOCOL.md Security Observations §1 and feeds the
  SECURITY_AUDIT_READINESS threat model (malicious-creator section).
