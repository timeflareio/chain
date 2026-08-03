# Dynamic Bond Economics — Duration-Anchored Bond, Self-Adjusting `k`, and Related Changes

*Replaces the flat, duration-independent guardian bond
(`B = rate × bond_multiplier × bump`) with a bond anchored to what the
secret actually costs (`B = rate × distance × bump × k`), where `k` is a
live, per-guardian value that rises on slashing and falls (more slowly) on
successful reveals — plus three related changes surfaced by the same
investigation: routing the guardian entry fee to validators, a per-guardian
cap on concurrent secrets, and a lower `rate`.*

> **Status: COMPLETE (all phases, July 2026).** Phase 6 landed 25 July 2026:
> `tools/econsim` and its plan (PENDING_ECONOMIC_SIMULATION_PLAN.md) are
> deleted — links to either below are historical. The open design questions
> in the original draft were put to the owner in a review session on
> 21 July 2026 and ruled (each ruling is recorded inline in §2 below, marked
> **Ruled**). The evidence base remains the informal exploration conducted
> against the [PENDING_ECONOMIC_SIMULATION_PLAN.md](PENDING_ECONOMIC_SIMULATION_PLAN.md)
> engine (`tools/econsim`), logged there as open questions §8.8–§8.13.
> Because the owner has fixed the values directly, the calibration sweeps
> originally gating this plan (§4) are superseded — see the note at the top
> of §4.

## Contents

1. [What this solves](#1-what-this-solves)
2. [The six changes](#2-the-six-changes)
3. [Position A implications — narrower than initially framed](#3-position-a-implications--narrower-than-initially-framed)
4. [Validation approach — superseded by owner rulings](#4-validation-approach--superseded-by-owner-rulings)
5. [DONE_ECONOMICS_TEST_STRATEGY_PLAN.md — what's still needed, what isn't](#5-done_economics_test_strategy_planmd--whats-still-needed-what-isnt)
6. [Implementation phases](#6-implementation-phases)
7. [Open questions](#7-open-questions)

---

## 1. What this solves

Today's bond (`B = rate × bond_multiplier × bump`) ignores a secret's own
duration entirely. That has two proven consequences (informal exploration,
July 2026, against the Phase 1–3 `tools/econsim` engine):

- **A hard, duration-independent bond caps the whole system's throughput.**
  With a flat 5,000 VEIL bond (`bump = 1.00`) and a fixed 1B VEIL supply, the
  absolute maximum concurrent secrets — even if 100% of all VEIL in
  existence were guardian float — is `1,000,000,000 ÷ 5,000 ÷ shares ≈
  40,000`. No population growth crosses this ceiling; only a smaller
  per-secret bond does.
- **The collusion-cost-to-pool ratio swings wildly and unintentionally with
  duration** — from ~3,472× at a 1-day secret down to ~9.5× at the maximum
  1-year secret — because the bond never adjusts for how long the secret
  actually runs. This is an artifact of the formula, not a deliberate
  security choice.

Anchoring the bond to what the creator is actually paying
(`rate × distance × bump`) fixes both: it removes the duration-independent
ceiling, and it makes the collusion ratio a deliberate, duration-independent
constant (`threshold × k`) instead of an accident of the formula.

## 2. The six changes

### 2.1 Bond formula: remove `bond_multiplier`, adopt `B = rate × distance × bump × k`

```
B (per-guardian bond) = rate × distance × bump × k
```

Replaces `B = rate × bond_multiplier × bump`. `bond_multiplier` is retired
entirely.

**Ruled — no ceiling, no floor on `B`.** Every prior sweep retained today's
flat bond as a ceiling; the owner's rulings make one unnecessary by
arithmetic: with `rate = 1` (§2.5), the worst possible bond — maximum
distance (`MaxRevealHorizon ≈ 5,256,000` blocks), maximum `bump` (10.00),
maximum `k` (24.00) — is `1 × 5,256,001 × 1000 × 2400 ÷ 10,000 ≈ 1.26 ×
10⁹ uveil ≈ 1,262 VEIL`, a quarter of today's flat 5,000 VEIL bond at
`bump = 1.00`. The formula's own bounds are the ceiling. A floor on `B` was
also considered (short secrets now carry proportionally tiny bonds) and
declined — the owner's ruling is that scale takes priority and the constant
collusion ratio (`threshold × k`) is the security property; a short secret
whose *external* value is high leans on `bump` (up to 10×), and the spec
records this as an intentional property.

### 2.2 `k` as a live per-guardian property, adjusted on slash/reveal events

**Ruled — event-triggered.** `k` adjusts on each individual slash and each
individual successful reveal; it is not keyed off a measured slash *rate*.
In the owner's framing, `k` **is itself the rate** — a live reputation value
whose level encodes the guardian's recent history. (The original draft's
"rate-triggered" framing in §4.2 and the slash-rate-threshold floors bullet
are withdrawn — they contradicted this ruling.)

- **Range: 4.00–24.00**, stored in hundredths (400–2400). **Ruled: newly
  registered guardians start at the floor, `k = 4.00`.**
- **On a slash event** (no-reveal or early-reveal, either one):
  `k′ = min(2400, k × 126 ÷ 100)` — truncating integer division on the
  hundredths representation, clamped at the *ceiling*. From the floor, 8
  consecutive slashes reach the ceiling:
  `4.00 → 5.04 → 6.35 → 8.00 → 10.08 → 12.70 → 16.00 → 20.16 → 24.00`.
- **On a successful reveal**: `k′ = max(400, k × 963 ÷ 1000)` — truncating
  integer division, clamped at the *floor*. From the ceiling, full recovery
  takes ~48 consecutive successful reveals.
- **Get the `min`/`max` direction right — this was caught and corrected
  during design.** The clamp on the *rising* (slash) side must be `min`
  (caps growth at the ceiling); the clamp on the *falling* (reveal) side
  must be `max` (floors the decline at the minimum). Written the other way
  round, `k` snaps instantly to the ceiling on the first slash and instantly
  to the floor on the first reveal — collapsing the gradual curve into a
  binary switch.
- **Precision: hundredths (2 decimal places), matching `BumpScale = 100`
  already used for `bump`** — a reused fixed-point scale, not a new one.
  The exact integer operations above (multiply, truncating divide, clamp)
  are the normative definition; they live in one shared function in
  `x/secrets/types` so the chain, tests, and simulator can never drift.
  The owner has ruled that ±0.01 drift between an idealised real-valued
  trace and the truncating integer trace is immaterial — the hard 4.00–24.00
  clamps bound any accumulation.
- Both multipliers were solved to land on the requested step counts (the
  precise mathematical values are `6^(1/8) ≈ 1.2510` for the climb and
  `6^(-1/48) ≈ 0.9634` for the recovery; 1.26 sits mid-range
  (`[1.2510, 1.2917)`) rather than at a boundary, so it is robust to
  fixed-point rounding differences).
- **Expected equilibrium behaviour — understood and accepted.** Under
  event-triggering, one ×1.26 slash step takes about six ×0.963 reveal
  steps to unwind, so any guardian whose per-assignment slash probability
  is below ~14% trends toward the floor between incidents. At the modelled
  baseline (~1.1% per assignment) `k` therefore sits at or near 4.00 for
  most guardians most of the time, spiking on each slash and decaying over
  the next handful of reveals. This is the intended dynamic: a
  recent-history reputation signal that makes bonds temporarily expensive
  for guardians who have just failed, not a permanent tiering of guardians
  by long-run failure rate. The 24.00 ceiling is reachable only through
  consecutive or near-consecutive slash streaks — which is precisely the
  behaviour that should price a guardian out.
- **Per-guardian, not network-wide.** A guardian's own `k` responds only to
  their own slash/reveal history — this is what prevents a cheap,
  adversarially-triggerable signal (deliberately slashing on throwaway
  secrets) from raising *other* guardians' bond requirements, and what
  avoids collectively punishing honest guardians for a different guardian's
  failures.
- **Known risks, tracked in §7 — reputation farming and re-registration
  whitewashing.** A guardian could behave impeccably to hold `k` at the
  floor, then defect on one high-value secret at the cheapest possible bond
  (mitigation candidates: an asymmetric reset toward the ceiling on any
  slash — not adopted, flat ×1.26 ruled for now). Separately, because new
  registrants start at 4.00 and guardians can withdraw and re-register, the
  entry fee `F` (1,000 VEIL) is now the effective price of a full
  reputation reset from 24.00 back to 4.00 — see §7.2.

### 2.3 Guardian entry fee routed to validators

Today `F` (the guardian registration fee, 1,000 VEIL) is burned
(`x/secrets/keeper/msg_server_register_guardian.go`). Route it instead to
validator income via the same `SendCoinsFromModuleToModule` →
`x/distribution` path gas fees already use (proportional to bonded stake,
not a flat per-validator split).

**Ruled — 100% to validators, no burn component.** The entry fee does not
pass through the 90/10 transaction-fee split; only transaction fees are
90/10. This makes it the only fee in the system with no burn component —
a deliberate, documented choice, not an oversight.

> **Correction (July 2026)**: both halves of this section were later
> amended. The routing was defective — the `SendCoinsFromModuleToModule`
> path parked the fee unaccounted in the distribution module account, where
> validators could withdraw none of it — and the no-burn ruling was
> reversed by the one-pipe ruling: **every** validator-bound flow rides the
> fee collector's 90/10 split (per registration: 900 VEIL allocated to
> validators, 100 VEIL burned). See
> [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md).

`tools/econsim` finding: fixes the §4a validator-count health threshold at
years 1–2 only — entry-fee income is tied to *new* registrations, which are
front-loaded during the growth phase, so the effect fades by year 10. Zero
measured cost to any guardian-side metric. Lowest-risk item in this plan.

### 2.4 Per-guardian cap on concurrent secrets

**Ruled — a constant, 100 concurrent secrets per guardian**, regardless of
float size or cohort, and a **pure count cap** (the VEIL-denominated
`MaxBondPerGuardian` + validator-funded expansion fee explored informally
is *not* adopted). This sits below what a Large-float guardian could
otherwise reach under the duration-anchored bond at low `k`
(capital-derived capacity in the hundreds to low thousands per guardian in
prior sweeps), so it binds whales without being the first constraint a
typical guardian hits.

**Mechanism — a hard eligibility gate at two points:**

- **Selection filter**: a guardian whose active-bond count has reached the
  cap is not a selection candidate, exactly like the existing
  unlocked-float affordability filter.
- **Confirmation re-check**: because a guardian can be in flight on several
  selections at once, the count is enforced again at `MsgConfirmShares` —
  the moment the bond actually locks. A guardian at the cap simply fails to
  confirm (no partial state written) and does not become one of the
  first-`n` acceptors.

The count is a denormalised per-guardian counter (incremented when a bond
locks at acceptance, decremented when that bond is finally disposed —
settlement, cancellation, commit-expiry release, or early-reveal slash),
asserted against the assignment store by the invariant suite. Bond size per
secret stays a clean, predictable function of `(distance, bump, k)` for
every guardian — no secret gets a smaller bond because the assigned
guardian happened to be near their cap.

### 2.5 `rate` lowered to 1 uveil (from today's 100)

**Ruled — decided.** Not contingent on further sweeps.

`tools/econsim` finding: at `k=0` (today's flat formula), dropping `rate` to
1 uveil *improved* validator count and affordability at nearly every
horizon — counter to an initial (wrong) prediction that fixed-VEIL costs
(`GuardianInfraMonthly`, `ValidatorOpexMonthly`) would swamp a 100×-smaller
reward. The real mechanism: the bond *also* shrinks 100× at `k=0` (since
`bond_multiplier` is a block count, not a VEIL amount), and two validator
income sources (the creation-fee floor, genesis-pool disbursements) don't
scale with `rate` at all, so they become relatively more important as
reward income shrinks.

Two caveats carried forward with the decision: it also raised *absolute*
unstaffed-secret counts at most horizons (price elasticity pulls in more
demand than the capacity gain matches), and stacked with `k ≥ 1` on the
duration-anchored curve it produced the worst affordability regression in
the investigation — read against the §4.4 caveat that the affordability
metric itself is unreliable at extreme horizons.

### 2.6 Bond amounts frozen per-guardian at selection (Option A)

**Ruled.** With per-guardian `k`, the single `bond_amount` recorded on the
secret becomes a per-guardian amount, and the moment it is fixed matters
because `k` can move between selection and confirmation. The ruling:

- **At selection (inside the `MsgRequestGuardians` transaction)**, each
  candidate's bond `B_i = rate × distance × bump × k_i` is computed from
  their `k` at that block; the affordability filter compares each
  candidate's *own* `B_i` against their unlocked float; and the selected
  guardians' bond amounts are frozen on the secret record alongside the
  selection list.
- **Confirmation stays a pure lock step**, exactly as today: it locks the
  stored amount and computes nothing. Selection's affordability check and
  the locked amount are the same number by construction, so a selected
  guardian can never fail to confirm because their bond moved underneath
  them.
- The stale window this admits — a guardian slashed between selection and
  confirmation still gets their cheaper pre-slash bond for that one secret —
  is bounded by the commit deadline (≤ `MaxCommitTimeout` = 200 blocks) and
  accepted.

The rejected alternative (compute at confirmation) would couple the locked
amount to live `k`, letting a selected-as-affordable guardian fail
confirmation through no fault of the creator and leaving the secret short
of `n` acceptors.

## 3. Position A implications — narrower than initially framed

[DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md)
ruled the economic constants **fully immutable at launch** — no `Params`
state, no `MsgUpdateParams`, retunable only via a rare, coordinated software
upgrade that never re-prices in-flight secrets. The reasoning given there:
*"a guardian underwriting a year-long secret is underwriting it against
economics that cannot float beneath it."*

**This plan does not violate that guarantee.** `k` is fixed for a given
secret **at selection time** (§2.6) — a guardian selected for a secret has
that bond amount frozen for the life of the commitment; nothing about it
moves while they're underwriting it, exactly as Position A requires. What's
dynamic is only the `k` a guardian's *next* selection will use. And the
mechanism that produces `k` — the 4.00–24.00 range, the ×1.26/×0.963
adjustment rule — is itself a fixed, immutable protocol constant, no
different in kind from `distance`, `bump`, or `n`, which already vary
per-secret today without anyone treating that as a governance concern.
Per-guardian `k` is a new *input* to an unchanged, immutable formula, not a
new governance mechanism.

**One genuinely new property, documented in spec.md as intentional:** two
guardians selected for an *otherwise identical* secret (same distance,
bump, `n`) at the same moment can now be required to post different bond
amounts, based on their own individual slash/reveal history. That cannot
happen today — bond depends only on the secret's own parameters, never on
which guardian is assigned. Not a problem, but new, and the spec calls it
out as a design property rather than letting it surface as a surprise.

## 4. Validation approach — superseded by owner rulings

> **Status note (21 July 2026): superseded as a gate.** The owner has fixed
> the values directly (§2 rulings) rather than deriving them from
> calibration sweeps, and implementation proceeds on that basis. The
> material below is retained as a record of what a future calibration pass
> would need, and because items 3–5 document known limits of the simulator
> that any future reading of its output must respect. `tools/econsim` is
> kept compiling against the new economics (its replay and golden tests
> re-anchor to the new amounts in the same PR), but the per-cohort
> `k`-tracking extension and the §2.2 curve sweeps are no longer
> prerequisites for the chain implementation.

The original draft required, before values were decided:

1. **Per-cohort (minimum) slash/reveal history tracking** in the simulator —
   the sim models guardians as three float-size cohorts, not individuals,
   so there is currently no per-guardian reliability signal to key `k` off.
2. **The dynamic-`k` mechanism itself**, swept against the 4–24 range and
   the 1.26×/0.963× multipliers.
3. **Pull the existing `share_validators` divergence-detector metric**
   (§4a of PENDING_ECONOMIC_SIMULATION_PLAN.md) for every configuration.
   Caveat: `AcctValidators` is a pure accumulator in the current sim
   (credit-only, no outflow in any code path), so this metric is likely to
   *also* diverge given long enough a horizon — treat it as a
   medium-horizon cross-check, not a clean long-horizon verdict.
4. **The §4a affordability metric's validity at extreme scale is an open
   question** (logged as §8.12 in the simulation plan) — figures in the
   billions of a percent most likely indicate the metric's denominator
   (mean modelled user balance) has been driven to near-zero by a model
   that gives users no way to earn VEIL back. Building a circulation model
   was considered and explicitly declined by the owner as too speculative.
   Affordability numbers at extreme horizons are read with that caveat,
   not taken at face value.
5. **Year-25/100 validator collapse is unfixed by every configuration
   tested** (12+ combinations) — confirmed as the validator-count model's
   own no-persistence issue rather than something any change in this plan
   reaches. Any claim that this plan "solves" long-run validator
   sustainability should be checked against this finding first.

## 5. `DONE_ECONOMICS_TEST_STRATEGY_PLAN.md` — what's still needed, what isn't

Checked this document against the changes above. **Recommendation: extend
it, don't delete it — it is not the same thing as `tools/econsim`, and most
of what it covers still applies.**

**Why they're different things.**
[DONE_ECONOMICS_TEST_STRATEGY_PLAN.md](done/DONE_ECONOMICS_TEST_STRATEGY_PLAN.md)
is a three-tier test suite (`x/secrets/keeper/conformance_test.go`,
`lifecycle_fuzz_test.go`, `make e2e-scenarios`) that proves the **chain's
actual code** correctly implements exact settlement amounts, slash splits,
cancellation payouts, and four conservation invariants (solvency,
conservation, no-stranded-bonds, queue hygiene) — for *whatever* the wired
economics are. `tools/econsim` is a completely separate tool: a long-horizon
(1–100 year) Monte Carlo projection used to decide *what the constants
should be*, per
[PENDING_ECONOMIC_SIMULATION_PLAN.md](PENDING_ECONOMIC_SIMULATION_PLAN.md).
Neither replaces the other. **Confirmed with the owner**: "remove all the
economic simulation code" refers to `tools/econsim` specifically — **not**
the conformance/fuzz/e2e suite, which is chain-correctness infrastructure
with two genuine defects already found and fixed by it (F2, F5). Recorded
here explicitly since deleting the wrong one would remove the safety net
that catches exactly the kind of defect a bond-formula rewrite is likely to
introduce.

**What's still needed from it, unchanged**: all four invariants (§4 of that
plan) still apply verbatim under the new bond formula — solvency,
conservation, no-stranded-bonds, and queue hygiene don't depend on how `B`
is computed, only on the bookkeeping being exact. The existing scenario
catalogue (registration/float, selection/acceptance, cancellation,
reveal/settlement, early-reveal reporting, timeouts) all still exercise
real mechanics this plan doesn't remove.

**What needs extending** (new scenarios, not a new plan): a guardian's `k`
correctly increasing on a slash and decreasing (more slowly) on a
successful reveal; per-guardian bond amounts frozen at selection and locked
verbatim at confirmation; the eligibility gate at the per-guardian secret
cap (§2.4) correctly rejecting at both selection and confirmation with no
partial state written; the entry fee (§2.3) crediting the validator
distribution path instead of burning; and the removal of `bond_multiplier`
from every place the conformance suite currently derives expected amounts
from it (per that plan's own rule: "exact amounts... derived from the base
constants... never hard-coded").

**Recommendation for `tools/econsim` itself**: keep it compiling through
this plan's implementation (its golden test exists precisely to surface
pricing changes as reviewable diffs in the same PR). Retire it per
`PENDING_ECONOMIC_SIMULATION_PLAN.md`'s own framing (a pre-launch
calibration mechanism, not a permanent feature) as the *last* step, once
the implementation has landed and the devnet suites pass.

## 6. Implementation phases

1. ~~Extend `tools/econsim` (per-cohort k-tracking, curve sweeps)~~ —
   **superseded** by the owner's direct rulings (§4 status note).
2. ~~Sweep and calibrate~~ — **superseded**, same basis.
3. ✅ **Spec-first chain design** — `docs/spec.md` updated for the new bond
   formula, per-guardian `k` state and its adjustment rule, the selection-
   time bond freeze (§2.6), the entry-fee destination change, and the
   secret cap, ahead of the keeper changes. Per-guardian bond variability
   (§3) documented as an intentional property.
4. ✅ **Chain implementation** — `x/secrets/types` (`bond_multiplier`
   retired, new bond formula, `k`-adjustment functions, new constants),
   proto additions (`Guardian.bond_k` + `active_bond_count`;
   `Secret.bond_amount` field 20 reserved, `guardian_bond_amounts` field 26
   added), keeper changes (per-candidate selection filtering and bond
   freezing, confirmation-time cap re-check, `k` updates on slash/reveal
   events, entry-fee redirect, cap enforcement). Invariant 3 extended to
   per-guardian frozen bonds plus the active-bond counter.
5. ✅ **Extended the conformance suite and e2e scenarios** (§5) — new G1–G4
   scenarios (entry-fee destination, `k` on slash/reveal, per-guardian bond
   variability, the cap gate at both points) added to the existing
   catalogue; `devnet/e2e-scenarios.sh` reads each guardian's own frozen
   bond. `tools/econsim` kept compiling, its replay amounts and goldens
   re-anchored so the pricing change is a reviewable diff. The canonical
   `TerminalSecretRecord` golden was recomputed (pre-launch, deliberate —
   the wire shape changed).
6. ✅ **Decommission `tools/econsim`** — landed 25 July 2026 as its own
   change: the tool, its planning doc
   (PENDING_ECONOMIC_SIMULATION_PLAN.md), and every build/CI hook removed
   (`go.work`, `GO_SUBMODULE_DIRS`, `verify-boundaries`, `deps-*` targets,
   CI path filter, security-sweep scan, Dependabot entry). The calibration
   report (docs/reports/econsim-calibration-v1/) is retained as the
   historical evidence base for the §2 rulings.

## 7. Open questions

Resolved this session (rulings recorded inline in §2): ~~ceiling on `B`~~
(§2.1 — none needed, bounded by arithmetic), ~~`k`-adjustment shape~~
(§2.2 — flat ×1.26/×0.963 event-triggered ruled; asymmetric reset not
adopted), ~~cap value and form~~ (§2.4 — constant 100, pure count cap),
~~bond determination timing~~ (§2.6 — frozen at selection),
~~spec.md wording for per-guardian bond variability~~ (§3 — documented as
intentional).

Still open:

1. **Affordability metric**: redefine it, or keep it and discount extreme
   values as a known artifact? Not resolved by this plan; tracked in
   `PENDING_ECONOMIC_SIMULATION_PLAN.md` §8.12. Only matters if
   `tools/econsim` output is consulted again before decommissioning.
2. **Re-registration whitewashing**: a guardian slashed to `k = 24.00` can
   withdraw their float, re-register under the same or a fresh key, and
   start again at `k = 4.00` for the price of the entry fee `F`
   (1,000 VEIL, which under §2.3 now pays validators rather than burning).
   On long-duration secrets the 24.00 → 4.00 difference is a 6× bond, so
   for a large-float guardian the reset can pay for itself. Options if this
   is judged a problem: price re-registration higher, carry `k` across
   re-registration for a known address (weak — fresh keys are free), or
   accept and document `F` as the whitewashing cost. **Not blocking
   implementation** — the mechanics land as ruled; this is a pricing/spec
   question on `F`.
3. **Reputation farming** (§2.2): behave impeccably at the floor, defect
   once at the cheapest bond. The flat multiplier is ruled for now; an
   asymmetric slash reset remains the candidate mitigation if live
   behaviour warrants it. Post-launch retuning is a software upgrade
   (Position A).
