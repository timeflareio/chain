# Fee Burn — Wiring the 90/10 Split

*Make the protocol's guaranteed, usage-proportional deflation real: 90 % of
every transaction fee to validators, 10 % permanently burned — implemented,
invariant-tested, and exposed to the economic simulation as a sweepable
lever.*

> **Status: DONE (July 2026) — shipped in PR #70 (`fee-burn-wiring`)**:
> `FeeValidatorPercent`/`FeeBurnPercent` constants with sum guard, the
> parameterised split in the economics core, the secrets module's
> `BeginBlock` (`ProcessFeeSplit`, ordered before x/distribution in
> `app_config.go`), unit/conformance/invariant tests plus the e2e burn
> assertion, and the same-session doc updates. Was PRIORITY 0 (owner
> ruling, July 2026). The 90/10 fee
> burn is **confirmed protocol design**: the protocol wants a degree of
> *guaranteed* deflation tied to usage, and the fee burn is that mechanism —
> the mechanical burns (entry fees, slashing, dust) are scenario-dependent,
> not guaranteed. Validators are compensated through the **creation fee**
> (`max(floor, pool_fee_percent × P)` →
> [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) §5.4b),
> which also prices Sybil/grinding attacks — not by keeping the burned share.
>
> This ruling **reverses the July 14 "abolition" note** in the
> token-economics plan, which git archaeology traced to a bulk planning-sweep
> commit (`269cf63`, "plan updates") with no decision record behind it — the
> plan's original position (burn dormant, to be wired; defect #1 of the
> implementation audit) is restored and now executed.
>
> **Simulation requirement (ruled with this plan)**: the split ratio is a
> first-class **sweepable input** to the economic simulation
> ([PENDING_ECONOMIC_SIMULATION_PLAN.md](../PENDING_ECONOMIC_SIMULATION_PLAN.md)),
> via the parameterised economics core (that plan's §2/§5 resolution,
> option (a)). 10 % is the wired baseline; the simulator plays with it.

## 1. Current state

- `Keeper.DistributeFees` (`x/secrets/keeper/fee_distribution.go`) is
  **complete and tested but has zero callers**: it splits an input amount
  via the decimal calculator, sends the validator share
  `fee_collector → distribution` module, routes the burn share through the
  secrets module account (which holds the `Burner` permission) and burns
  it, and emits a `fee_distribution` event.
- The ratios live as string constants (`"0.90"`/`"0.10"`) inside the keeper
  file — not in `constants.go` with every other economic constant, and not
  reachable by the simulator.
- Today 100 % of fees follow the vanilla SDK path: ante handler deducts
  into `fee_collector`; `x/distribution`'s BeginBlocker allocates the
  previous block's balance to validators. **No fee has ever been burned.**
- PROTOCOL.md records this as **Deviation #1**; spec.md and CLAUDE.md
  describe the split as live. Wiring it makes those documents true.

## 2. Design: burn in BeginBlock, ordered before distribution

`x/distribution` allocates whatever sits in `fee_collector` at the start of
each block. The split therefore runs as a **new `BeginBlock` on `x/secrets`,
ordered immediately before `distribution`** in `app_config.go`'s
`BeginBlockers` (the module currently has no BeginBlock; its EndBlock
ordering is untouched):

1. Read the full `fee_collector` balance (previous block's collected fees).
2. If zero: no-op (empty blocks cost nothing).
3. Call `DistributeFees` with it: 90 % moves to `distribution` (which then
   allocates it to validators exactly as today), 10 % is burned.

> **Correction (July 2026)**: step 3's "moves to `distribution`" was a bare
> `SendCoinsFromModuleToModule` into the distribution module **account**,
> which `AllocateTokens` never sees — it only allocates from the fee
> collector, which this sweep had just emptied. The 90 % share was parked
> unaccounted and validators could withdraw none of it; the suites asserted
> the send, never a withdrawable reward. Fixed by
> [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md):
> the split now burns the 10 % and **leaves the 90 % in the fee collector**
> for x/distribution's own BeginBlocker to allocate with full bookkeeping.

Why BeginBlock rather than an ante/post handler: one deterministic burn
point per block covering every fee flow, no per-transaction gas overhead,
and it composes with the existing `mint → distribution` ordering the same
way real fee-burn chains do. The distribution module sees a pre-shrunk pot
and needs no changes.

Ordering note: `secrets` currently sits **after** `distribution` in
`BeginBlockers` (and has no begin-block work). It moves before
`distribution` for the burn; `EndBlockers` order is unchanged. The begin-
and end-block orders are independent lists — no other module is affected.

## 3. Constants — one home, simulator-reachable

The string ratios move to `x/secrets/types/constants.go` as integer
percentages in the house style of the slash splits, with a compile-time
sum guard:

```go
// Transaction-fee split (per block, at BeginBlock): the protocol's
// guaranteed usage-proportional deflation. Shares must total 100.
FeeValidatorPercent = int64(90)
FeeBurnPercent      = int64(10)
```

They are consumed by the keeper through the **parameterised economics
core** (the economic-simulation plan's ruled §2/§5 resolution: pure
functions taking the constants as arguments, wrapped by constant-bound
versions the chain calls) — which is exactly what lets the simulator sweep
the split without forking the arithmetic.

## 4. What this plan deliberately does not decide

- **Consensus-enforced fee floor** (TOKEN_ECONOMICS §5.7 proposal,
  0.5 uveil/gas): the burn is proportional to fees actually paid, so
  "guaranteed" deflation is only as strong as the fee floor under it.
  Companion work, separately ruled — this plan neither implements nor
  blocks on it.
- **`community_tax` zeroing** (SDK default 2 % skims the validator share
  into an accidental treasury — TOKEN_ECONOMICS audit item 2): same
  pipeline, separate decision.
- **The other July 14 banner rulings** (entry fee 10,000 VEIL, creation-fee
  sizing, bootstrap workflow): unaffected by this reversal; they stand or
  fall on their own re-confirmation.

## 5. Tests

1. **Unit**: exact 90/10 amounts for known inputs incl. odd values (the
   decimal-calculator precision guard); zero-fee no-op; event attributes.
2. **Conformance**: drive a block containing fee-paying transactions
   through the app; assert the exact `fee_collector → distribution` and
   burn movements, and that **total supply shrank by exactly the burn**.
3. **Invariant**: extend the supply accounting used by the burn invariants —
   cumulative supply reduction = Σ(entry fees + slash burns + dust +
   **fee burns**).
4. **E2E**: the scenario suite gains an assertion that a transaction
   block's `fee_distribution` event reports `burned_fees` = 10 % of that
   block's fees, and node-level supply queries reflect it (runs on both the
   native and compose devnets; the 3-validator compose stack also proves
   the burn is app-hash-deterministic across validators).
5. **Simulation smoke** (once the sim lands): the CI golden run pins the
   burn flow at the wired baseline.

## 6. Documentation, same session

- **PROTOCOL.md**: Deviation #1 closed; BeginBlock section gains the split
  ("There is no BeginBlock logic" becomes false); funds-flow table updated.
- **spec.md**: the fee-distribution claims become accurate — verify the
  numbers as stated (90/10, burn address/mechanism) match the wired
  behaviour exactly.
- **TOKEN_ECONOMICS plan**: abolition bullet replaced by the ruling above;
  implementation-audit item 1 resolution = *wired* (this plan).
- **PARAMS_GOVERNANCE plan** work item 4: resolution = wired, owned here.
- **ECONOMIC_SIMULATION plan**: models the burn at baseline; split ratio
  joins `rate`/`bond_multiplier`/`entry_fee` in the §5 sensitivity sweeps.

## 7. Implementation checklist

1. Constants: `FeeValidatorPercent`/`FeeBurnPercent` in `constants.go`
   (+ sum guard); delete the keeper-local string ratios.
2. Parameterised split arithmetic in the economics core; constant-bound
   wrapper used by `DistributeFees`.
3. `BeginBlock` on the secrets module: read `fee_collector`, call
   `DistributeFees`; register `HasBeginBlocker`.
4. `app_config.go`: move `secrets` before `distribution` in
   `BeginBlockers` only.
5. Tests per §5 (unit, conformance, invariant, e2e assertion).
6. Docs per §6.
7. Devnet verification: `make e2e-full` (compose, 3 validators — burn must
   be deterministic across the set) and `make e2e-scenarios`.
