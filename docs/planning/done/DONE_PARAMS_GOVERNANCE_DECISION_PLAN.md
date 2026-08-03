# Economic Parameters & Governance — Decision: Immutable (Position A)

*Resolves the spec/implementation contradiction on whether the protocol's
economic constants are governable. Decision direction: they are not — the
constants stay compile-time, the correction path is the already-rehearsed
coordinated software upgrade, and the confidence that makes immutability safe
comes from simulation and multi-testnet calibration before launch, not from a
post-launch adjustment knob.*

> **Status: DONE (July 2026).** Position A (fully immutable) confirmed by the
> owner — including the explicit ruling to **keep the software-upgrade path**
> (`x/upgrade` + `x/gov`, gov scoped solely to upgrade coordination), which
> Position A's correction story stands on. All work items complete:
> **(1)** the spec amendment landed — "governance-tunable" removed, one
> authoritative immutability statement plus the gov-scope sentence in
> spec.md §Economic Parameters; **(2)** the in-flight-repricing hardening
> shipped — `ProRataCancellationPayout` now derives from the secret's
> **stored** pool (`P × elapsed ÷ (distance × requested_shares)`), never the
> live rate constant, with equivalence, degenerate-input and
> simulated-upgrade tests (`economics_cancellation_test.go`,
> `TestCancellation_NoInFlightRepricing`); **(3)** `RetentionBlocks` shipped
> as a compile-time constant
> ([DONE_TERMINAL_SECRET_RETENTION_PLAN.md](DONE_TERMINAL_SECRET_RETENTION_PLAN.md));
> **(4)** the fee-split deviation resolved by wiring
> ([DONE_FEE_BURN_PLAN.md](DONE_FEE_BURN_PLAN.md), PR #70). Item **(5)**, the
> simulation/testnet calibration programme, is this plan's *decision* (that
> programme, not governance, calibrates the constants) and its execution
> lives with
> [PENDING_ECONOMIC_SIMULATION_PLAN.md](../PENDING_ECONOMIC_SIMULATION_PLAN.md) /
> [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) and
> the testnet-launch plan.*

## Contents

1. [The contradiction](#1-the-contradiction)
2. [The decision and its reasoning](#2-the-decision-and-its-reasoning)
3. [Rejected alternatives](#3-rejected-alternatives)
4. [What replaces governance: validation before launch](#4-what-replaces-governance-validation-before-launch)
5. [Work items](#5-work-items)
6. [Open questions](#6-open-questions)

---

## 1. The contradiction

The documentation says three incompatible things:

1. **spec.md, Economic Parameters**: "Base constants (**governance-tunable**):
   `rate`, `bond_multiplier`, `F`, `max_tier`, the per-violation bond
   distribution, and `H`…"
2. **spec.md, Decentralization Strategy**: "**Immutable Economics**: Fee
   splits, burns, and stakes cannot be changed… Hardcoded Economics: Core
   economics embedded in protocol to prevent manipulation."
3. **Implementation**: every economic knob is a compile-time constant in
   `x/secrets/types/constants.go`. There is no `Params` collection, no
   `MsgUpdateParams`, no param subspace — PROTOCOL.md states it plainly:
   "None are governance parameters — changing any requires a chain software
   upgrade."

The implementation has been Position A de facto all along; the decision
below makes it Position A de jure and cleans up the one spec sentence that
disagrees.

## 2. The decision and its reasoning

**Position A — fully immutable.** Four arguments, in order of weight:

1. **Immutability is a product feature here, not just a decentralisation
   posture.** Timeflare's product is long-horizon commitments: a guardian
   accepting a 1-year secret is underwriting it against today's bond and
   reward economics. Economics that a token vote can move undermine exactly
   the predictability that makes a 1-year commitment underwritable — even
   with in-flight obligations snapshotted, expected future income and bond
   requirements would float. "Immutable economics" is what the guardian's
   business case stands on.
2. **Parameter governance would arrive exactly when it is least safe.** Gov
   configuration is currently SDK defaults, and early chain life is when
   token distribution is most concentrated and capture is cheapest. A
   "launch calibration window" (Position C) opens the attack surface at its
   most vulnerable moment. If parameter governance is ever genuinely wanted,
   it should arrive *late* — after distribution decentralises — and the
   software-upgrade path can introduce it then. Choosing A now forecloses
   nothing.
3. **The framing that motivated params was overstated.** "The only correction
   is a hard fork" mischaracterises the path: recalibration under A is a
   coordinated software upgrade via a standard gov proposal — machinery this
   chain already has and rehearses (`make upgrade-scaffold`,
   `make devnet-upgrade-test`, docs/upgrades.md). And the timeline advantage
   of params is small: a param change still needs a proposal plus the full
   voting period; an upgrade adds binary distribution on top. Days versus a
   week or two — not "mutable versus frozen". For economic constants, the
   extra friction (validator supermajority coordination rather than a token
   vote) is arguably the *stronger* safeguard, not a cost.
4. **Simplicity is real and compounding.** No `Params` state, no
   `MsgUpdateParams` proto surface, no per-field validation, no gov-config
   calibration burden as a launch blocker, no param-mutation branches in the
   conformance suites, and the "immutable economics" claim in spec.md stays
   a truthful, auditable statement.

## 3. Rejected alternatives

- **Position B (minimal params, gov-gated)** — rejected: its speed advantage
  over upgrades is marginal (see §2.3); it requires gov-parameter
  calibration (voting period, quorum, deposits) to become launch-critical;
  it opens economics to capture during the concentrated-distribution phase
  (§2.2); and it forfeits the immutability guarantee that the guardian
  business case relies on (§2.1).
- **Position C (time-boxed adjustability)** — rejected: novel mechanism,
  more code, and its premise is inverted — the calibration window it opens
  early is precisely the window in which governance is least trustworthy.
  Calibration belongs *before* launch (§4), not in a mutable grace period
  after it.

## 4. What replaces governance: validation before launch

Immutable-at-launch is only defensible if the provisional values are
validated first. The economics were deliberately designed to make this
tractable — essentially everything derives from two knobs (`rate`,
`bond_multiplier`) plus the creator's `bump` dial. Two layers:

**Simulation** — specified in
[PENDING_ECONOMIC_SIMULATION_PLAN.md](../PENDING_ECONOMIC_SIMULATION_PLAN.md)
(an executable model importing the chain's own pricing functions, projecting
1/2/5/10/25/100-year horizons);
[REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) owns the
values it validates. Model token distribution and flows as a function of
guardian count, user count, secret volume, and validator count. The
questions the simulation must answer:

- **Guardian profitability**: reward income vs. infrastructure cost vs.
  capital locked in floats — does a rational operator stay, at realistic
  secret volumes? Where is the participation-elasticity cliff?
- **Bond calibration**: is `B = 5,000 VEIL × bump` sufficient deterrent for
  the secrets people actually protect, without pricing small guardians out
  of candidacy (the per-secret affordability check makes the float the
  admission ticket)?
- **Sybil economics**: the burned entry fee is the principal mitigation for
  the pre-acceptance leak window (PROTOCOL.md Open Defects #1) — the entry
  fee is a **security parameter**, not merely an economic one, and the
  simulation must price the attack, not just the honest path.
- **Supply dynamics**: burn rate (entry fees + slash burns + dust + the fee
  burn once wired) against the fixed 1B supply; guardian float lockup vs.
  circulating supply; creator affordability over time.

**Dependency**: the 90/10 fee split with burn is **unwired**
(PROTOCOL.md Deviations #1; TOKEN_ECONOMICS implementation-audit item 1).
Any supply simulation run before that deviation is resolved models a
deflation flow that does not exist. **Resolution ruled (owner, July 2026):
wire it — Priority 0, [DONE_FEE_BURN_PLAN.md](DONE_FEE_BURN_PLAN.md)**;
the simulation then models the burn at its wired baseline with the split
ratio sweepable.

**Multi-testnet calibration**: launch testnet-1 with the simulated v1
values, measure real guardian participation and secret demand, recalibrate
via a governance software upgrade, repeat on testnet-2 if the deltas are
large. This doubles as a rehearsal of the exact correction mechanism
Position A relies on for mainnet (the testnet-launch plan already schedules
"an economics-parameter recalibration" chaos drill — under A this is an
upgrade drill).

## 5. Work items

1. **spec.md amendment** — remove "governance-tunable" from §Economic
   Parameters and consolidate the two contradictory sections into one
   authoritative statement: constants are immutable; the correction path is
   a coordinated software upgrade, expected rarely and rehearsed on
   testnets. (Same fix demanded by TOKEN_ECONOMICS implementation-audit
   item 4 — one edit satisfies both plans.)
2. **In-flight repricing hardening** — required under Position A too: a
   software upgrade that changes `rate` must not re-price existing
   obligations any more than a param change may. `reward_pool` and
   `bond_amount` are already snapshotted per secret and settlement reads
   stored values; the one live read is the **cancellation wage**
   (`ProRataCancellationPayout` recomputes from the current
   `RatePerGuardianBlock`). Fix it to derive from stored state — the
   creation-time per-guardian rate is recoverable as
   `P ÷ (distance × requested_shares)`, so this likely needs no new field —
   and add a conformance test that mutates the constant mid-lifecycle
   (simulating an upgrade) and asserts no in-flight secret's settlement or
   cancellation moves by one uveil.
3. **Retention plan alignment** —
   [DONE_TERMINAL_SECRET_RETENTION_PLAN.md](DONE_TERMINAL_SECRET_RETENTION_PLAN.md)
   recommends `RetentionBlocks` "as a module `Params` field … so governance
   can tune it" — under Position A there is no params mechanism for it to
   live in. It becomes a compile-time constant like everything else; update
   that plan's §9 accordingly when it is implemented.
4. **Fee-split deviation resolution** — precedes the simulation (§4
   dependency). **Ruled (owner, July 2026): wire it** — owned by
   [DONE_FEE_BURN_PLAN.md](DONE_FEE_BURN_PLAN.md) (Priority 0),
   tracked here only as an ordering constraint.
5. **Simulation + testnet calibration programme** — the §4 content; the
   simulator is specified in
   [PENDING_ECONOMIC_SIMULATION_PLAN.md](../PENDING_ECONOMIC_SIMULATION_PLAN.md),
   the values in TOKEN_ECONOMICS, the testnet execution in the
   testnet-launch plan. This plan's contribution is the decision that this
   programme, not governance, is the calibration mechanism.

## 6. Open questions (all resolved, July 2026)

1. ~~**Final sign-off**~~ **Ruled: Position A, final** — including the
   explicit decision to keep the software-upgrade path after weighing its
   removal (the risk assessment: removing it would make Position A's
   "correction = coordinated upgrade" story false and leave hard forks as
   the only fix on a chain whose product is multi-year in-flight
   commitments). Spec amendment landed; plan flipped to DONE.
2. ~~**Gov module scope statement**~~ **Landed** in spec.md §Economic
   Parameters: `x/gov` is wired solely to coordinate software upgrades —
   no parameters, no treasury, nothing else to govern. `gov.min_deposit`
   calibration remains flagged in TOKEN_ECONOMICS.
3. ~~**Sequencing of work item 2**~~ **Done ahead of any testnet** — the
   hardened stored-pool derivation shipped with this plan's closure, so the
   first recalibration drill exercises the hardened path by construction.
