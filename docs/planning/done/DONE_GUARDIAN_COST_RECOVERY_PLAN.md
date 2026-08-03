# Guardian Cost Recovery — Pricing Work as Well as Time — Plan

*The reward pool prices duration and nothing else: `P = rate × distance ×
max_shares × bump`. A guardian's cost has two parts — the time it holds the
share, and the fixed work of two transactions — so on any short secret the
work swamps the pay and the guardian completes the job at a loss. This plan
adds a **work component** to the pool: a gas-denominated reimbursement,
derived through the same `MinRequiredFee()` device the creation-fee floor
already uses, paid by the creator to the guardians who actually do the work.
It introduces no tunable parameter and no second dial for creators; `bump`
remains the only thing they choose.*

> **Status: done — 27 July 2026**, executed on `worktree-guardian-cost-recovery`.
> Ruled by the owner, 27 July 2026: the pool prices work as well as time; the
> work component is never collateral; **only performed work is paid**, with an
> explicit rejection unpaid; **nothing is disbursed before the secret reaches a
> terminal state**, which keeps the escrow and the secret's stored amounts
> identical at every height and leaves settlement's equal split and dust burn
> untouched; the commit-expiry refund is reduced by the accept legs of
> guardians who did accept; and the creation fee is charged on the time
> component only.
>
> **Settled during execution**: the acceptance reimbursement is escrowed in its
> own field (`Secret.accept_fees`, proto field 29) rather than blended into the
> pool. `ProRataCancellationPayout` derives the cancellation wage from stored
> economics and never from live constants — the in-flight repricing guarantee
> of the immutable-economics ruling — and a single blended pool could not have
> honoured that, since cancellation must tell the work and time halves apart.
> A separate bucket keeps every terminal-state payout derivable from stored
> state alone. The pool's reveal leg accrues over the hold exactly as the wage
> does; a no-show's reveal leg flows to the revealers under the existing
> dominant-strategy rule, and only the accept slice returns to the creator.
> **Priority**: P1 — economics correctness, pre-testnet. Every secret on the
> chain today shorter than ~38 hours of distance is completed at a loss by
> its guardians, and nothing in the protocol or the daemon prevents it.
> **Origin**: measurement of the live devnet, 26–27 July 2026 — settled
> secret `c56f1d4b` pays each guardian 439 uveil against 22,931 uveil of gas.
> Extends the fee-economics family:
> [DONE_CREATION_FEE_PLAN.md](done/DONE_CREATION_FEE_PLAN.md) (whose
> gas-denominated floor is the precedent this plan reuses),
> [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md)
> (the one-pipe ruling), and
> [DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md)
> (the pricing core this modifies).
> **Components**: `x/secrets/types` (`constants.go` gas figures,
> `economics.go` pool derivation, economics-core tests);
> `x/secrets/keeper` (`msg_server_request_guardians.go` escrow sizing,
> `msg_server_confirm_shares.go` accept-leg payout, settlement and
> cancellation distribution in `endblock_logic.go`, event attributes);
> `docs/spec.md` (Secret Economics & Slashing, Secret Pricing **including its
> worked-example table of `P` and creation fee per secret**, the money map,
> cancellation) + `docs/operations.md` + `docs/PROTOCOL.md` (funds-flow
> table); `docs/ACCEPTED_TRADEOFFS.md` (the short-secret cost increase and the
> guardian's gas carry);
> conformance, no-stranded-bonds and `devnet/e2e-scenarios.sh` exact-amount
> assertions; TypeScript SDK and mobile-client cost estimators (no proto
> change — the pool is computed chain-side from existing fields).
> **Explicitly unaffected**, confirmed rather than assumed: `guardian/` — the
> daemon neither prices nor claims anything, so no `guardiand` change is
> required; and the bond path, `k` curve and selection filter, which the work
> component never enters.

## 1. Why

Revenue and cost scale differently, and nothing reconciles them.

- **Revenue** per guardian is `rate × distance × bump` — linear in distance.
- **Cost** per guardian is two transactions — `MsgGuardianConfirmShares` and
  `MsgGuardianRevealShare` — whose gas does not vary with distance at all.

Measured on the devnet at the 0.1 uveil/gas consensus floor: accept costs
11,032 uveil (110,314 gas), reveal 11,899 uveil (118,984 gas), **22,931
uveil per guardian per secret**. Break-even therefore sits at `distance ×
bump ≈ 22,931` blocks — about 38 hours at six-second blocks, at bump 1.00.

Below that line the guardian pays to work. Secret `c56f1d4b` (distance 439)
paid each of seven guardians 439 uveil at settlement — 5% of what the reveal
transaction alone cost them. The protocol permits distances far shorter
still: `MinRevealStartOffset` is 50 blocks and `MinRevealDuration` 100, so a
valid secret can pay ~150 uveil against ~23,000 uveil of guardian gas.

Two consequences follow, both visible on the live chain:

1. **Guardians subsidise validators.** The fee pipe has two payers — the
   creator's creation fee and gas, and every guardian's own gas (160,517
   uveil per secret across seven). On a one-hour secret, validators receive
   **156% of the creator's entire outlay**; the guardians fund the other 56
   points out of float.
2. **Short secrets are an attack shaped like ordinary usage.** Minting cheap
   short-horizon secrets consumes guardian float, concurrency slots and
   attention while paying less than the gas it forces them to spend.

Neither the protocol nor `guardiand` guards the loss region: validation bounds
distance in time, never in economics, and the daemon accepts every assignment
whose bond it can afford. `docs/ACCEPTED_TRADEOFFS.md` records no position on
this, so it is an omission rather than a settled trade-off.

## 2. Design

### The pool prices work and time

```
P = max_shares × ( F_work + rate × distance × bump )
                  └ work ┘  └────── time ──────┘
```

Additive, not `max(floor, curve)`: no regime switch, no branch in the
pricing core, and the guardian's net is strictly positive at every distance
including one block. A floor would pin net to exactly zero across the whole
floor-priced range, which is protection without an incentive.

`F_work` is the guardian's round trip. It is named in two legs, because the
terminal state decides which of them a guardian has earned:

```
F_accept = MinRequiredFee(GuardianAcceptGas)     // 120,000 gas → 12,000 uveil
F_reveal = MinRequiredFee(GuardianRevealGas)     // 130,000 gas → 13,000 uveil
F_work   = F_accept + F_reveal                   //            → 25,000 uveil
```

**This is not a new parameter.** `MinRequiredFee()` is the existing device
that prices `CreationFeeFloor()` from `CreationFeeFloorGas`; the gas figures
are observations of the protocol's own code path (the measured limits, 110,314
and 118,984, rounded up for headroom), not policy choices. Both move
automatically with any retune of the consensus gas floor, and neither is
exposed to creators — `bump` remains their only dial.

**`bump` multiplies the time component only.** Gas does not scale with a
creator's security choice, so neither does its reimbursement. The bond is
untouched: `B = rate × distance × bump × k` continues to price collateral by
what is at stake, and the work component never enters it.

**The creation fee is charged on the time component only.** The percentage
curve prices the selection draw, and a draw does not become more expensive
because the guardians' gas is being reimbursed — charging `bps(d)` on a
pass-through would route roughly 8,750 uveil of every guardian's gas
reimbursement to validators and the burn. So `creation_fee = max(floor,
P_time × bps(d) ÷ 10,000)` where `P_time = rate × distance × max_shares ×
bump`. The floor is unchanged. One visible consequence: the floor-to-percent
crossover stays where it is today (~96,500 blocks, 6.7 days) instead of moving
to ~68,000, so the regime reported in the reservation event is unaffected.

### Nothing is disbursed before a terminal state

The whole pool stays in escrow from `MsgUserRequestGuardians` until the secret
reaches a terminal state, and is then distributed according to what each
guardian actually did. **No payment is made at acceptance.** That single rule
is what keeps the accounting honest: `secret.RewardPool` and the escrow the
module actually holds are the same number at every moment of the secret's
life, so every downstream calculation — the equal split, the dust remainder,
the refund paths — reads a real balance and needs no adjustment.

| Terminal state | Each guardian receives | Creator refunded |
|---|---|---|
| **revealed** (≥ 1 revealer) | revealers split the pool equally — unchanged code | nothing |
| **failed** — commit expired below `min_shares` | `F_accept` to each guardian that accepted | pool less those accept legs |
| **failed** — settled with no revealers | nothing (every acceptor no-showed and is slashed) | the pool in full, as today |
| **cancelled** by the creator | `F_accept` + pro-rata time, to each accepted guardian | the remainder, including every reveal leg |

Two asymmetries in that table are deliberate. A guardian that **accepts and
then no-shows gets nothing at all**, its accept leg included: it is being
penalised for non-performance, and the existing rule that a failed guardian's
share flows to the revealers is what makes revealing the dominant strategy. A
guardian that accepted a secret which **never activated** is in the opposite
position — it did everything asked of it and the roster simply did not fill,
which is not its fault — so it keeps its accept leg and the creator's refund
is reduced accordingly.

**The work component is never collateral.** It is a payment, not a deposit:
it lands in the guardian's spendable balance, never in the float, never
locked, never slashable, and it plays no part in bond accounting or the `k`
curve.

**The guardian fronts its own gas until the terminal state**, which on a
year-long secret is a year. That is working capital rather than a loss, and
it is small beside the bond already locked for the same period — 0.011 VEIL of
gas against roughly 21 VEIL of collateral on a year-long secret. Accepted
deliberately (§5) in exchange for an escrow that can never drift from its
stored value.

### Worked figures (max_shares 7, bump 1.00, six-second blocks)

| Distance | Pool now | Net/guardian now | Pool with this plan | Net/guardian | Creator pays |
|---|---|---|---|---|---|
| 10 blk (1 min) | 70 | −22,921 | 175,070 | **+2,079** | 0.2101 → 0.3851 |
| 150 blk (15 min) | 1,050 | −22,781 | 176,050 | +2,219 | 0.2110 → 0.3861 |
| 14,400 (1 day) | 100,800 | −8,531 | 275,800 | +16,469 | 0.3108 → 0.4858 |
| 432,000 (30 days) | 3,024,000 | +409,069 | 3,199,000 | +434,069 | 3.3252 → 3.5002 |
| 5,256,000 (1 year) | 36,792,000 | +5,233,069 | 36,967,000 | +5,258,069 | 38.7816 → 38.9566 |

All figures uveil unless marked VEIL. Net is positive at every distance and
monotonically increasing; at one year the work component is 0.47% of the
pool — invisible where it does not matter, decisive where it does.

The 156% anomaly resolves as a side effect: at ten blocks validators drop to
86.6% of the creator's outlay, because the creator now funds the gas their own
secret causes rather than the guardians absorbing it.

## 3. Lifecycle interactions

- **Rejection and silence.** A guardian that does not want an assignment
  sends nothing: unresponded assignments simply do not count, the roster
  finalises from the accepted set, and no gas is spent. **Rejecting is
  therefore already free.** An explicit `MsgGuardianConfirmShares` rejection
  is a courtesy that costs its sender gas and — because selection is final
  and no replacement guardian can be drawn — buys the creator nothing
  mechanical today. It is therefore unpaid — see §7 for the reasoning and the
  condition under which paying for it would become worth its cost.
- **Commit expiry below `min_shares`.** The one path this plan changes
  materially. `processExpiredCommit` currently refunds `secret.RewardPool` in
  full, on the stated invariant that "a commit that reaches FAILED was
  refunded". It now pays `F_accept` to each guardian that accepted and
  refunds the remainder. Guardians who accepted a secret that never activated
  did nothing wrong, and this is the only path on which they would otherwise
  be left out of pocket. The invariant's wording changes; its purpose — no
  stranded escrow — does not.
- **Cancellation.** Accepted guardians are paid `F_accept` plus the pro-rata
  time component; the creator is refunded the remainder, every reveal leg
  included, since no reveal occurred. This strengthens the paid-hold
  invariant: a cancelled secret can no longer leave a guardian out of pocket
  for its acceptance.
- **Failed secrets with revealers** (settled below threshold). Revealers
  split the pool exactly as today — threshold-independent settlement is
  unchanged, and the work component simply rides along inside the pool.
- **Failed secrets with no revealers.** Every acceptor no-showed and is
  slashed accordingly; the pool refunds to the creator in full, as today.
- **No-shows.** A guardian that accepts and never reveals receives nothing —
  accept leg included — on top of the existing partial bond slash. Its whole
  share flows to the revealers under the established rule that makes
  revealing the dominant strategy. This is the deliberate counterpart to the
  commit-expiry case above: there the guardian is blameless, here it is not.
- **Early-reveal slash.** Unchanged — the full bond is forfeit and the
  guardian is excluded from settlement, so it receives no work component
  either.
- **Unfilled slots.** The pool is priced on `max_shares` with no
  activation-time refund, so when fewer guardians accept than were selected,
  the revealers split a larger pool — work component included. That is the
  existing fixed-pool rule applied unchanged, and it is what allows
  settlement to remain a single equal division with no component
  reconstruction.

## 4. Properties this must preserve

Each of these is a test, not a claim:

- **A guardian that completes the job is never out of pocket**, at any
  distance ≥ 1 block, at bump 1.00, paying the consensus floor gas price.
- **Reject-farming is impossible** — an unpaid rejection cannot be profitable.
- **Ghost-farming is impossible** — a selected guardian that never responds
  receives nothing, so passive registration earns nothing.
- **Self-dealing is unprofitable** — a creator running guardians cannot steer
  protocol-controlled selection, and pays the creation fee (0.06 VEIL) plus
  their own gas (0.15 VEIL) per attempt against reimbursements that only ever
  equal real gas.
- **Sloppy operators absorb their own waste** — a guardian setting extravagant
  gas limits, or paying above the floor price, eats the difference. The
  reimbursement is denominated in protocol-defined gas, not in what the
  guardian chose to pay.
- **No stranded funds** — every work slice is either paid to a guardian or
  returned, on every lifecycle path, proven by the existing
  `no_stranded_bonds_test.go` pattern extended to the work component.
- **Escrow never drifts from the stored pool** — because nothing is disbursed
  before a terminal state, `secret.RewardPool` equals the module's escrow for
  that secret at every height. The equal split
  (`distributePoolToRevealers`) and its dust remainder therefore keep reading
  a real balance, and neither needs to know how the pool was composed.
- **No component is ever reconstructed at settlement** — settlement remains a
  single equal division of whatever is escrowed, so a later retune of the gas
  constants cannot re-price a live secret.
- **No disposition can exceed its secret's escrow** — a secret's escrow is
  fixed at creation while the gas constants are not, so the cancellation and
  commit-expiry paths, which compute work legs from the constants in force
  when they run, are bounded by `RewardPool`. A guard that should never bind:
  it needs a retune roughly doubling `F_accept` inside one secret's lifetime.

## 5. What this plan does not solve

- **It does not lower the minimum reveal distance.** `MinRevealStartOffset`
  (50) and `MinRevealDuration` (100) are unchanged. This plan makes a shorter
  minimum *safe* — a ten-block secret pays its guardians properly — but
  actually lowering it is a separate decision with its own reveal-window and
  liveness questions, and wants its own plan.
- **It does not address entry-fee amortisation.** The 1,000 VEIL entry fee
  remains a sunk cost recovered over a guardian's working life
  (`ACCEPTED_TRADEOFFS.md` §9).
- **It does not retune `RatePerGuardianBlock`.** The master price level is
  untouched; this plan changes the *shape* of the pool, not its level.
- **It does not protect against gas-price volatility.** Reimbursement tracks
  the consensus floor. If validators price gas well above it, guardians are
  under-reimbursed — a validator-market question, not a pricing-core one.
- **It does not change who pays the guardians.** Creators do, as today.
- **It does not reimburse gas at the moment it is spent.** A guardian carries
  its own accept and reveal gas until the secret reaches a terminal state —
  on a year-long secret, a year. Accepted deliberately: the alternative is
  paying at acceptance, which would make the escrow diverge from
  `secret.RewardPool` and force either a stored component or a recomputed
  split at settlement. The carry is 0.011 VEIL against a bond of roughly 21
  VEIL locked over the same period.

## 6. Implementation

Spec-first: the protocol document leads, code follows.

1. **Spec** — `docs/spec.md` "Secret Economics & Slashing" and the pricing
   section: the two-component pool, the terminal-state disposition table, the
   creation fee charged on the time component, the commit-expiry and
   cancellation changes, and the money map. `docs/operations.md`
   message-level effects; `PROTOCOL.md` funds-flow table.
2. **Pricing core** — `GuardianAcceptGas` / `GuardianRevealGas` in
   `constants.go`; `WorkComponent()`, `AcceptLeg()`, `TimeComponent()` and the
   revised `RewardPoolAmount()` in `economics.go`, with parameterised `…With`
   variants matching the house style; `CreationFee()` re-based on the time
   component; economics-core table tests including the one-block case.
3. **Escrow** — `msg_server_request_guardians.go` escrows the larger pool.
   Nothing else changes at request or acceptance time: `confirm_shares` pays
   nothing, so no new funds movement enters the hot path.
4. **Terminal-state disposition** — `endblock_logic.go`:
   `processExpiredCommit` pays `F_accept` to each acceptor and refunds the
   remainder (replacing the full refund); settlement's equal split and dust
   burn are untouched; cancellation adds `F_accept` to each accepted
   guardian's pro-rata payout, bounded by `RewardPool`. Extend the
   no-stranded-funds invariant to every terminal path, and assert
   escrow-equals-stored-pool as a property.

   **No migration.** Nothing is live, so this lands by chain reset rather than
   by an upgrade handler: every secret on the resulting chain is priced under
   the new rules from genesis, and no code needs to reason about a secret
   escrowed under the old pool.
5. **Suites and clients** — `devnet/e2e-scenarios.sh` exact-amount
   assertions; conformance vectors; SDK and mobile-client cost estimators;
   `TESTING_COMMANDS.md`.
6. **Documentation of the economics** — the duration/cost model published as
   the reference visual, showing current-versus-proposed with the x-axis
   opening at ten blocks.

## 7. Why only performed work is paid

Ruled 27 July 2026. The principle decides who is owed anything when the
secret reaches its terminal state — and, since nothing is paid before then,
the terminal state is the only moment at which the question arises.

**An explicit rejection is unpaid**, because silence is already a free
rejection: declining costs a guardian nothing as long as it sends no
transaction. With selection final and no replacement guardian drawable, an
early "no" buys the creator nothing that a late one does not, so paying for
it would purchase a courtesy with no mechanical value — and would create a
gradient towards rejecting, since a reimbursement sized with rounding
headroom necessarily exceeds a careful operator's actual cost.

**Payment at selection was rejected outright.** It would pay a registered,
available, permanently silent guardian on every draw: passive income for the
one behaviour that hurts creators most, because silence burns the whole
commit window where a rejection at least resolves the record.

**Work nobody performed is not paid for.** On a secret that never activated,
the accept legs of guardians who did accept are paid and the rest of the pool
returns to the creator — not to the fee collector's 90/10 split as draw
pricing, since the creation fee already prices the draw and routing work
slices there would charge the same draw twice. On a secret that settled, a
no-show's share is not returned at all: it flows to the revealers under the
existing dominant-strategy rule. The distinction is blame, not bookkeeping —
an acceptor on a stillborn secret is blameless, a no-show is not.

The condition that would reopen this: **if the protocol ever gains
re-selection** — drawing a replacement when a guardian declines — an early
rejection becomes mechanically valuable, and paying for it earns its cost.
Worth revisiting then, not before.

**Dust.** Settlement divides the escrowed pool by the number of revealers and
burns the integer remainder, at most `revealers − 1` uveil. Because the pool
is still whole at that point, the existing calculation is already correct and
needs no change — the remainder is genuine rounding, never a component that
left early.
