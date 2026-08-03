# Token Economics — Calibration & Evolution Plan

*A complete inventory of every value that makes VEIL move, an audit of what is actually
implemented versus claimed, a relative-scale analysis anchoring every cost to the price
of a one-day secret, proposed values for what remains open, a minimal-genesis
distribution design, and a framework for how the economics evolve — without a treasury,
without ongoing governance cycles.*

> **Status: REJECTED as a live plan — owner ruling, July 2026.** Retained as
> the economics analysis record (the inventory, cost ladder, money map and
> distribution design below remain the reference). Everything with
> pre-testnet urgency was carved into its own plans (links below); the
> launch/TGE surface this plan uniquely held — the distribution/bootstrap
> design (§7), mainnet genesis configuration (§10 Phase 4) and the launch
> decisions (§9 Q3 bulk distribution, Q4 credit scoring, Q5 fiat pricing
> corridor) — is **deliberately unowned** and will be re-planned fresh when
> token-generation planning begins.
>
> *(Original status, for the record: PENDING — refreshed 2026-07-25 against
> the implemented economics.)*
> The guardian economics this plan once proposed to calibrate are now **settled,
> ruled and implemented**: the bonded model
> ([DONE_BONDED_GUARDIAN_ECONOMICS_PLAN.md](DONE_BONDED_GUARDIAN_ECONOMICS_PLAN.md)),
> the duration-anchored dynamic bonds, `rate = 1`, the validator-routed entry fee
> and the concurrency cap
> ([DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md)
> — constants fixed by owner ruling against the calibration evidence in
> [../reports/econsim-calibration-v1/ECONSIM_CALIBRATION_V1.md](../../reports/econsim-calibration-v1/ECONSIM_CALIBRATION_V1.md)),
> and the wired 90/10 fee burn
> ([DONE_FEE_BURN_PLAN.md](DONE_FEE_BURN_PLAN.md)). The near-term protocol work has
> since been **carved into its own plans**:
> [DONE_CREATION_FEE_PLAN.md](../DONE_CREATION_FEE_PLAN.md),
> [DONE_CONSENSUS_FEE_FLOOR_PLAN.md](../DONE_CONSENSUS_FEE_FLOOR_PLAN.md)
> and [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md)
> (which owns `community_tax = 0` and the July 2026 reward-routing defect).
> What remains for THIS plan is the launch surface: mainnet genesis
> configuration, the distribution/bootstrap design, and the fiat pricing
> corridor that anchors every VEIL amount.

> **Design stance (settled with the project owner, July 2026):**
> - **No treasury.** No accumulating, discretionary-spend fund of any kind. Protocol
>   fees may exist only as *flows* that are distributed or burned in the same breath.
>   `community_tax = 0` is **decided**; implementation is owned by
>   [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md).
> - **No governance cycles.** Economic constants stay compile-time; the only retuning
>   path is a rare, coordinated software upgrade. No parameter votes, no spending votes.
> - **Minimal genesis holdings.** Genesis pools exist solely to onboard the three roles
>   (validators, guardians, creators); they are small, time-boxed, mechanically
>   disbursed, and their unspent remainder is **burned** at sunset. After bootstrap,
>   the token economics simply play out.

> **Standing rulings this plan builds on (all implemented):**
> - **Guardian pricing is duration-anchored and reputation-priced**: per-guardian
>   bond `B = rate × distance × bump × k ÷ 10,000`, with the live per-guardian
>   multiplier `k` ∈ [4.00, 24.00] (×1.26 per slash, ×0.963 per correct reveal,
>   newcomers at the floor). No ceiling or floor on `B` — the formula's own bounds
>   are the ceiling (worst case ≈ 1,262 VEIL), and `threshold × k` is the constant
>   collusion-cost property.
> - **`rate = 1 uveil`/guardian/block** — the master price level every fee, pool,
>   bond and wage derives from. The fiat level of the whole ladder is set by the
>   pricing corridor at token generation (§9 Q5); `rate` is the single-constant
>   correction if launch pricing lands outside it.
> - **Entry fee `F` = 1,000 VEIL, routed 100 % to validators** (no burn component —
>   the only fee in the system without one, a deliberate documented choice). It is
>   Sybil resistance, the price of a full `k`-reset via re-registration, and
>   front-loaded validator income in the growth years.
> - **The 90/10 gas-fee split is wired** (`ProcessFeeSplit`, BeginBlock): 90 % of
>   every block's fees to validators, 10 % burned — the protocol's guaranteed,
>   usage-proportional deflation. The ratio is a compile-time constant, sweepable
>   in the (now-retired) simulator's economics core.
> - **The creation fee is the agreed structural addition**
>   (`max(floor, pool_fee_percent × P)`, non-refundable at request, 100 % to
>   validators, never pooled) — it is simultaneously the consensus security budget
>   and the per-draw price that completes the selection-grinding fix
>   (PROTOCOL.md Security Observations §1;
>   [DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md](DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md)
>   restructured its forfeit into this fee). Its two values are §9 Q1 — the plan's
>   most consequential open number.
> - **Bootstrap acquisition** runs through the off-chain signup/credit workflow
>   (§7.4) — the front door for the bootstrap cohorts, explicitly **not** the
>   bulk-distribution mechanism.

> **Implementation order**: the economics-calibration chain
> (containerisation → simulation → value decisions) has completed its first two
> steps — the simulator ran, produced
> [ECONSIM_CALIBRATION_V1.md](../../reports/econsim-calibration-v1/ECONSIM_CALIBRATION_V1.md),
> and was decommissioned with the guardian-side values ruled directly from it.
> The remaining numeric decisions (§9 Q1 creation fee, Q2 gas floor, Q5 pricing
> corridor) read from the same report's sweeps. There is no governance path to
> retune post-launch (Position A — and none is wanted), so nothing ships until
> its number is confirmed. §10 Phase 0 (documentation truth reconciliation)
> carries no numeric content and may land at any time.

## Contents

1. [The goal](#1-the-goal)
2. [Inventory — every value that moves the token](#2-inventory--every-value-that-moves-the-token)
3. [Implementation audit — what is real today](#3-implementation-audit--what-is-real-today)
4. [The money map — sources, flows and sinks](#4-the-money-map--sources-flows-and-sinks)
5. [Calibration analysis, lever by lever](#5-calibration-analysis-lever-by-lever)
6. [Proposed values (what remains open)](#6-proposed-values-what-remains-open)
7. [VEIL distribution — bootstrap, not endowment](#7-veil-distribution--bootstrap-not-endowment)
8. [Evolution over time](#8-evolution-over-time)
9. [Open decisions (user input required)](#9-open-decisions-user-input-required)
10. [Implementation phases (after approval)](#10-implementation-phases-after-approval)

---

## 1. The goal

Set a defensible starting point for every economic parameter, and know in advance which
dial to turn, in which direction, when reality diverges from the model. Concretely:

- **Creators** should find secrets affordable at low security and expensive-but-fair at
  high security, with pricing they can predict exactly before publishing.
- **Guardians** should earn a competitive return on *locked* capital — enough to attract
  and retain a deep, decentralised float, not so much that creators are overcharged.
  (Settled and implemented — §5.3 records the shape the rulings produced.)
- **Validators** must have a real security budget. Consensus security cannot run on
  goodwill; today it runs on gas plus front-loaded entry fees (see §5.4 — the creation
  fee is the structural fix, and it is this plan's main remaining deliverable).
- **Slashing** must make honest revelation the dominant strategy at every `bump` tier.
- **Deflation** should be a genuine, measurable force tied to usage, not a marketing
  claim. The 10 % fee burn is wired (guaranteed, usage-proportional); the
  scenario-dependent sinks (slash burns, dust, bootstrap sunset) ride alongside it.
  §5.6 sizes all of it honestly — at launch volumes it is modest, and the plan says so.
- **Distribution** must onboard all three participant classes and then get out of the
  way: no long-lived pools, no discretionary programmes, no large genesis holdings.

A constraint inherited from the protocol's design philosophy: the economics are
**usage-driven** — no inflation, no block rewards, no treasury. Every reward is paid by
someone who received a service. This plan works entirely within that constraint.

---

## 2. Inventory — every value that moves the token

Everything below can move VEIL. Values marked **hardcoded** are compile-time Go
constants (changeable only via chain upgrade); **genesis** values are set once at
network launch; **node-config** values are per-operator and *not* consensus-enforced;
**inherited** values are Cosmos SDK defaults we have never explicitly set.

### 2.1 Secrets-module economics (hardcoded — `x/secrets/types/constants.go`, `economics.go`)

| Parameter | Value | What it drives |
|---|---|---|
| `rate` (RatePerGuardianBlock) | 1 uveil/guardian/block | Master price level — every reward pool, bond, fee and cancellation wage scales with it |
| Bond multiplier `k` (`MinBondK`/`MaxBondK`, `InitialBondK`) | 4.00–24.00 (hundredths, 400–2400), newcomers at 4.00; ×1.26 per slash, ×0.963 per correct reveal (truncating, clamped) | Per-guardian reputation pricing of collateral; `threshold × k` is the constant collusion-cost property |
| Entry fee `F` (EntryFeeAmount) | 1,000 VEIL — routed 100 % to validators, never returned | Sybil resistance; the price of a `k`-reset via re-registration; front-loaded validator income |
| `bump` (`MinBump`/`MaxBump`, `MaxTier = 10`) | 1.00–10.00 (hundredths) | The creator's single security dial; scales `P` and `B` together |
| `MaxActiveBondsPerGuardian` | 100 concurrent secrets | Hard per-guardian concurrency cap (selection filter + re-check at bond lock) |
| No-reveal bond split | 40 % burn / 10 % creator / 50 % returned | Griefing deterrence; creator compensation; burn sink |
| Early-reveal bond split | 40 % burn / 10 % creator / 50 % reporter / 0 returned | Leak deterrence; reporter bounty economics |
| Max reveal horizon `H` | 5,256,000 blocks (≈ 1 year) | Bounds `distance`, hence the maximum single-secret price and lock duration |
| Derived: reward pool `P` | `rate × distance × max_shares × bump ÷ 100` | What the creator pays; priced on the band ceiling, fixed (unfilled slots never refunded) |
| Derived: bond `B` | `rate × distance × bump × k ÷ 10,000`, frozen per-guardian at selection | Per-secret guardian collateral (worst case ≈ 1,262 VEIL; typical bonds are small — §5.3) |
| Derived: cancellation wage | `rate × elapsed × bump ÷ 100` per guardian, from the stored pool | Pro-rata paid exit |

### 2.2 Fee handling (hardcoded — **live**, `ProcessFeeSplit` at BeginBlock)

| Parameter | Value | Location |
|---|---|---|
| Validator fee share | 90 % | `FeeValidatorPercent`, `constants.go` |
| Burn fee share | 10 % | `FeeBurnPercent`, `constants.go` |

Applied to the previous block's collected fees every block, ordered before
`x/distribution` so allocation sees the post-burn pot. The guardian entry fee does
**not** pass through this split — it is forwarded whole to the distribution module.

### 2.3 Genesis allocations and launch configuration

| Parameter | Value | Location |
|---|---|---|
| Total supply | 1,000,000,000 VEIL, fixed | genesis (sum of pools) |
| Validator incentives pool | 50M VEIL (5 %) | `devnet/chain/genesis-addresses.conf` |
| Guardian incentives pool | 300M VEIL (30 %) | `devnet/chain/genesis-addresses.conf` |
| User incentives pool | 650M VEIL (65 %) | `devnet/chain/genesis-addresses.conf` |
| Inflation | 0 (mint params zeroed) | `devnet/chain/setup-chain.sh` |
| Devnet validator self-delegation | 10,000 VEIL | `genesis-addresses.conf` |

⚠️ These pools currently mean **100 % of supply sits in three key-controlled accounts
at genesis** — the opposite of the minimal-holdings stance. They are devnet
scaffolding; §7 replaces them for mainnet.

### 2.4 Node configuration (per-operator — **not** consensus-enforced)

| Parameter | Value | Location |
|---|---|---|
| Minimum gas price | 0.1 uveil (default; env-overridable) | `devnet/chain/setup-chain.sh` |
| Block time | ~6 s (assumed throughout the economics) | consensus config |

⚠️ `minimum-gas-prices` lives in each validator's `app.toml`. Any validator can accept
zero-fee transactions; there is no chain-wide fee floor. Since the 10 % burn is now
live, an advisory floor means the deflation sink can be competed toward zero — the
floor must become consensus state (§5.7, Phase 3).

### 2.5 Inherited Cosmos SDK defaults (never explicitly set — silent levers)

| Parameter | SDK default | Why it matters |
|---|---|---|
| `distribution.community_tax` | **2 %** | Skims 2 % of all validator fees into a community pool — an accidental treasury, directly contradicting the no-treasury stance. Ruled zero; not yet set (§10 Phase 1) |
| `staking.max_validators` | 100 | Consensus set size |
| `staking.unbonding_time` | 21 days | Validator capital exit friction |
| `gov.min_deposit` | 10 VEIL | Trivially cheap governance spam at mainnet — gov remains needed for software-upgrade coordination even with immutable economics |
| `gov.voting_period` | devnet-only 30 s override | Needs a real mainnet value |
| `slashing` module (validator) params | SDK defaults (downtime jail, double-sign slash 5 %) | Validator-side slashing — entirely separate from guardian slashing, and currently uncalibrated |

### 2.6 Non-price constants with economic side-effects

These are validation bounds, but they shape the market:

- The band `threshold ≤ min_shares ≤ max_shares ≤ 32` with the gap bound
  `max_shares − min_shares < threshold`, and `MinThreshold`/`MaxThreshold` (2/16) —
  bound the per-secret price range (`P` prices `max_shares`) and the capital a single
  secret can lock. Over-selection is the creator's explicit band; there is no
  protocol-side buffer constant.
- `MaxRevealDuration` (14,400 blocks ≈ 1 day) and `MinRevealDuration` (100 blocks) —
  the paid window span.
- `MaxAvailabilityWindow` = `H` — guardian commitment horizon.

---

## 3. Implementation audit — what is real today

Reading the code against the spec's economic claims. Two of the original four gaps
have been closed since this plan was first drafted; two remain, and both are this
plan's to fix.

1. ~~The 10 % fee burn does not happen.~~ **Resolved (July 2026)** — `ProcessFeeSplit`
   runs every block at BeginBlock, ordered before `x/distribution`, ratios in
   `constants.go` ([DONE_FEE_BURN_PLAN.md](DONE_FEE_BURN_PLAN.md)). The burn is
   the protocol's guaranteed usage-proportional deflation; slash burns and dust are
   the scenario-dependent sinks alongside it.
2. **An accidental treasury exists — OPEN**, and worse, **validator-bound fees
   are stranded unallocated** (July 2026 finding: the 90 % share and entry fees
   sit in the distribution module account with zero withdrawable rewards). Both
   are owned by
   [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md).
3. **The fee floor is advisory — OPEN.** 0.1 uveil is node config, not protocol state,
   so both validator gas revenue and the 10 % burn can be bypassed by any validator
   willing to accept cheaper transactions — owned by
   [DONE_CONSENSUS_FEE_FLOOR_PLAN.md](../DONE_CONSENSUS_FEE_FLOOR_PLAN.md).
4. ~~Spec self-contradiction on mutability.~~ Resolved by the design stance: immutable
   wins — constants stay compile-time; retuning is a coordinated software upgrade,
   expected rarely (§8.1). Verify spec.md carries no residual "governance-tunable"
   wording during Phase 0.

---

## 4. The money map — sources, flows and sinks

Every VEIL movement in the system. Fixed supply means the only dynamics are
*circulation* (who holds it, locked vs liquid) and *destruction* (burns).

```
                          ┌────────────────────────────────────────────┐
                          │              1,000,000,000 VEIL            │
                          │  ≥98% in holders' hands at block 1 (§7):   │
                          │    pre-genesis sale + operator earn-in     │
                          │  ≤1.5% time-boxed bootstrap rebates —      │
                          │      unspent remainder 🔥 BURNED at sunset  │
                          └────────────────────────────────────────────┘
                                             │
                                             ▼
   CREATORS ──── reward pool P ────► escrow ────► revealing GUARDIANS (split P equally)
      │              │                  │
      │              │ cancellation     └──► creator refund (unearned remainder,
      │              ▼                       commit-timeout, or nobody revealed)
      │       pro-rata guardian wage
      │
      ├──── creation fee (§5.4: max(floor, 10% of P), non-refundable —
      │        prices consensus security AND the selection draw) ──100%──► VALIDATORS  [PROPOSED — §9 Q1]
      │
      ├──── gas fees ──► fee collector ──90%──► VALIDATORS (distribution module)
      │                                └─10%──► 🔥 BURN                 [live]
      │
   GUARDIANS ── entry fee F (1,000 VEIL) ──100%──► VALIDATORS          [live]
      │
      ├──── float deposit ──► per-guardian escrow (locked/unlocked)    [returnable]
      │         │ bond B locked per accepted secret (B = rate × distance × bump × k ÷ 10,000)
      │         ▼
      │     slashing:  no-reveal:   40% 🔥 / 10% creator / 50% returned  [live]
      │                early-reveal: 40% 🔥 / 10% creator / 50% reporter [live]
      │
      └──── split dust (pool & bond integer remainders) ──► 🔥 BURN    [live]

   VALIDATORS ── consensus stake (staking module, uveil) ── standard delegation
                 └── validator slashing (double-sign/downtime) — SDK defaults
```

Note there is deliberately **no box where money accumulates awaiting a decision**: the
distribution module is a pass-through (rewards accrue to stakers continuously by
formula, non-discretionary), burns are instant, and the bootstrap allocations are
mechanical and self-extinguishing.

**Sinks (deflation)**: the 10 % gas-fee burn (guaranteed, scales with transaction
volume) + slash burns (40 % of duration-anchored bonds, hopefully rare) + split dust
+ the one-off bootstrap sunset burn. **Sources of validator income**: 90 % of gas,
100 % of entry fees (front-loaded to the growth years), and — once Q1 is ruled —
100 % of creation fees (scales with value-at-risk). ⚠️ The first two are
currently stranded unallocated (the reward-routing defect —
[DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md)). §5.6 sizes the sinks honestly:
at launch volumes deflation is modest and the plan does not pretend otherwise.

---

## 5. Calibration analysis, lever by lever

### 5.1 The cost ladder — every fee relative to a one-day secret

To judge whether the constants are *mutually* sensible, anchor everything to two
reference quantities (at the implemented `rate = 1 uveil`/guardian/block):

```
w  = one guardian-day of wages at bump 1 = rate × 14,400 = 0.0144 VEIL
S₁ = the starter secret: 1 day, 3 guardians, bump 1 = 3 × w = 0.0432 VEIL
```

| Rung | VEIL | × S₁ | In guardian-days (w) |
|---|---|---|---|
| Creation-fee floor on S₁ (proposed: 1 × w — §9 Q1) | 0.0144 | 1/3 | 1 |
| Gas fee (200k gas @ 0.1 uveil) | 0.02 | ~1/2 | 1.4 |
| **Anchor: 1-day secret `S₁`** | **0.0432** | **1** | **3** |
| Base bond `B` (1-day, bump 1, k = 4.00) | 0.0576 | 1.3 | 4 |
| 7-day announcement (5g, bump 1) | 0.504 | ~12 | 35 |
| Bond, 30-day secret (bump 1, k = 4.00) | 1.73 | 40 | 120 |
| 30-day dead-man's handle (5g, bump 2) | 4.32 | 100 | 300 |
| 70-day escrow, medium security (9g, bump 5) | 45 | ~1,040 | 3,125 |
| Entry fee `F` | 1,000 | ~23,150 | 69,444 (≈ 190 years) |
| Max possible bond (1y, bump 10, k = 24.00) | ~1,262 | ~29,200 | — |
| Max secret pool `P` (1y, 32g, bump 10) | ~1,682 | ~38,900 | — |
| Per-guardian payout, max secret | ~52.6 | ~1,200 | — |

Reading the ladder:

- **The middle and top of the ladder are healthy**: price scales linearly with real
  resource consumption (guardian-blocks), `bump` scales wage and collateral together,
  the entry fee towers over any single engagement (no single payday recovers it —
  the professional-posture property survives `rate = 1` unchanged), and the maximum
  bond and maximum pool sit within 2× of each other.
- ⚠️ **The bottom of the ladder is compressed — flag for Q1/Q5.** At `rate = 1` with
  the 0.1 uveil gas floor, **gas (~0.02 VEIL) is roughly half of `S₁` itself**, and
  the proposed one-guardian-day creation-fee floor (0.0144 VEIL) sits *below* gas.
  Two consequences to decide with open eyes: the entry-level secret's cost is
  gas-dominated (an accessibility question for Q5's fiat corridor — if `S₁` should
  cost cents, gas is fine; if less, it isn't), and the anti-grinding floor as
  proposed adds less than one gas fee per draw (the calibration report's
  `creation_fee_floor_days` sweep showed k2 = 1 and k2 = 4 both healthy — k2 = 4
  would put the floor at ~3× gas; §9 Q1 should choose with this table in view).
- **Ratios worth enshrining as design intent**: revealing returns `100 ÷ k` % of the
  posted bond as wage — **25 % at the floor `k = 4.00`, ~4 % at the ceiling** — so a
  guardian's return on locked capital is set by their own reputation and nothing
  else; `threshold × k` is the constant collusion-cost property (ruled); and every
  rung is `rate × something`, so the single expected retune (`rate`, §5.2) rescales
  the whole ladder without disturbing any relative incentive.

### 5.2 Creator pricing — `rate`, and what a secret costs

`P = rate × distance × max_shares × bump ÷ 100` (priced on the band ceiling, fixed —
unfilled slots enlarge the revealers' split, never refund). At ~6 s blocks
(14,400/day):

| Use case | Band ceiling | Distance | Bump | Pool `P` | Per guardian (if all reveal) |
|---|---|---|---|---|---|
| 1-day sealed bid (the anchor `S₁`) | 3 | 14,400 | 1.00 | 0.0432 VEIL | 0.0144 VEIL |
| 7-day announcement | 5 | 100,800 | 1.00 | 0.504 VEIL | 0.10 VEIL |
| 30-day dead-man's handle (refresh monthly) | 5 | 432,000 | 2.00 | 4.32 VEIL | 0.86 VEIL |
| 70-day escrow, medium security | 9 | 1,000,000 | 5.00 | 45 VEIL | 5 VEIL |
| 1-year inheritance, max security | 32 | 5,256,000 | 10.00 | ~1,682 VEIL | ~52.6 VEIL |

Two observations:

- **The shape is right.** Price scales linearly with real resource consumption
  (guardian-blocks of locked capital) and quadratically with paranoia (`bump` raises
  both wage and collateral). Nothing to restructure.
- **The level is unknowable until VEIL has a price.** `S₁` is a twentieth of a cent
  at $0.10/VEIL and ~4 cents at $1/VEIL. `rate` is therefore *the* parameter most
  likely to need retuning post-launch. The plan's position: **keep `rate = 1 uveil`**
  as the launch value, define the acceptable fiat corridor at token generation
  (§9 Q5), and if launch pricing lands outside it, correct `rate` once via
  coordinated upgrade — every fee, bond and wage rescales in lockstep because
  everything is `rate × ratio`.

### 5.3 Guardian returns — settled by the dynamic-bond rulings; recorded here

The implemented model
([DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md))
replaced the old flat `bond_unit` with duration-anchored, reputation-priced
collateral. The economics that fall out:

- **Return on locked capital is `100 ÷ k` % per secret**, independent of duration,
  `rate` and `bump` (all cancel): a floor-`k` guardian earns 25 % of each posted
  bond as wage; a ceiling-`k` guardian ~4 %. Reputation *is* the yield curve —
  honest guardians are structurally more capital-efficient, which is the intended
  incentive.
- **Bonds are small; the entry fee dominates guardian capital.** A 30-day bump-1
  bond at the floor is 1.73 VEIL; even 100 concurrent such secrets (the
  `MaxActiveBondsPerGuardian` cap) lock only ~173 VEIL of float against the sunk
  1,000 VEIL fee. The de-facto minimum viable guardian is therefore **the entry fee
  plus a working float measured in hundreds of VEIL** — amortisation runs through
  wages: a guardian rolling ~100 concurrent 30-day bump-1 secrets earns
  ~43 VEIL/month, recovering the fee in roughly two years. Steeper utilisation or
  higher-bump assignments shorten that; the earn-in slots (§7.2) are sized so
  bootstrap-cohort guardians never carry that bridge unaided.
- **Concurrency self-throttle**: capacity = min(100, unlocked float ÷ ΣB). Network
  capacity in guardian-blocks is `Σ unlocked floats ÷ per-block bond cost` — worth
  exposing as a metric (§8.3) because it is the supply side of the whole market.
- The calibration report's guardian-side sweeps (registrations, affordability,
  unstaffed secrets) are the evidence base behind these constants; guardian-count
  health is the §8.3 metric that would justify revisiting `F` at an upgrade.

### 5.4 Validator economics — the genuine problem, and the fee-vs-treasury distinction

Validators earn 90 % of gas, plus 100 % of entry fees. Size it honestly: at the
0.1 uveil floor a typical 200k-gas transaction pays 0.02 VEIL, so one *million*
transactions a year is ~18,000 VEIL of validator gas revenue across the whole set;
entry fees add 1,000 VEIL per registration but are **front-loaded** — tied to *new*
registrations, fading after the growth years (this was the measured finding behind
routing them to validators: it fixes validator-count health in years 1–2 only).
Meanwhile consensus secures escrows and floats that scale with usage. **Gas scales
with transaction count, not value-at-risk; entry fees scale with guardian growth,
not usage. Nothing in the live system pays validators proportionally to what they
secure.** The bootstrap allocation (§7) only rents a bridge.

Three structural options:

- **(a) Raise gas prices.** Helps at the margin, but gas revenue scales with tx
  *count*, not value-at-risk — and at `rate = 1`, gas is already ~half the price of
  the smallest secret (§5.1), so there is little headroom before gas distorts the
  entry-level use case. Not sufficient alone.
- **(b) The creation fee** *(recommended — the plan's one structural addition)*: a
  non-refundable `creation_fee = max(floor, pool_fee_percent × P)` charged at
  `MsgUserRequestGuardians`, **the entire fee flowing to validators**
  (fee-collector → distribution module in the same flow; never pooled; it does not
  pass through the 90/10 split). It does **double duty** (ruled July 2026): it is
  the consensus security budget *and* the price of a selection draw — collecting it
  at request time absorbs the commit-abandonment forfeit mechanism while leaving
  every refund path untouched, since the fee never enters escrow. It is the pending
  half of PROTOCOL.md Security Observations §1, which closes fully when this ships.
  Proposed values (§9 Q1): `pool_fee_percent = 10 %`; floor = `k2` guardian-days of
  wages (`rate × k2 × 14,400`), with the §5.1 compression flag in view — at
  `rate = 1`, k2 = 1 gives 0.0144 VEIL (below one gas fee), k2 = 4 gives
  0.0576 VEIL (~3× gas). The calibration report swept both knobs:
  `creation_fee_percent` healthy at 5–10 (and the **no-fee row measurably worse** —
  validators 1 vs 4); the floor healthy at k2 = 1 and k2 = 4.

  **The creator's full Phase-1 invoice** — everything charged at
  `MsgUserRequestGuardians`, derived from `rate`:

  ```
  1. Reward pool   P  = rate × distance × max_shares × bump ÷ 100
                        → ESCROWED, refundable per lifecycle rules
  2. Creation fee     = max(floor, 10 % × P)          [proposed — §9 Q1]
                        → NON-REFUNDABLE, 100 % to validators
  3. Gas              = gas_used × gas price (§5.7 floor)
                        → non-refundable, 90 % validators / 10 % burned

  total debit = P + creation fee + gas
  ```

  | Secret (§5.2 examples) | `P` | Creation fee (k2 = 1) | Gas (~200k) | Total debit |
  |---|---|---|---|---|
  | 1-day sealed bid `S₁` (3g, bump 1) | 0.0432 | **0.0144** (floor) | 0.02 | 0.078 |
  | 7-day announcement (5g, bump 1) | 0.504 | 0.050 (10 %) | 0.02 | 0.574 |
  | 30-day dead-man's handle (5g, bump 2) | 4.32 | 0.432 (10 %) | 0.02 | 4.77 |
  | 70-day escrow (9g, bump 5) | 45 | 4.5 (10 %) | 0.02 | 49.5 |
  | 1-year max (32g, bump 10) | ~1,682 | ~168 (10 %) | 0.02 | ~1,850 |

  Refundability at a glance: `P` comes back per the lifecycle (in full on
  commit-timeout or zero-reveal; pro-rata remainder on post-activation
  cancellation; to revealers at settlement); the creation fee and gas never come
  back. The guardian bond `B` appears nowhere here — it is the guardian's
  collateral, never a creator cost.
- **(c) Consensus-enforced fee floor** (§5.7, Phase 3). Needed regardless of (a)/(b)
  so validator gas revenue and the 10 % burn cannot be competed to zero; on its own
  it does not fix the budget.

**Sizing the correction.** At a year-one scenario of 50,000 secrets averaging
5 guardians / 30 days / bump 1.5 (`P` ≈ 3.24 VEIL each, ΣP ≈ 162,000 VEIL), a 10 %
creation fee is ~16,200 VEIL/year of validator revenue — comparable to gas at a
million transactions, but scaling with exactly the thing validators secure (value
and duration of escrowed commitments) rather than message count. In VEIL terms all
validator income at `rate = 1` is small; **whether it pays for real infrastructure
is entirely a fiat question — Q5 (the pricing corridor) and Q1 must be decided
together**, and this coupling is the refreshed plan's most important honesty note.

**This fee is not a treasury, and must not become one.** The distinction that
matters is *accumulation + discretion*: a treasury is a pot that fills up and is
later spent by somebody's decision. The creation fee has neither property: 100 %
flows into the distribution module in the same transaction flow and accrues to
stakers continuously by formula. Nothing pools, nothing is spendable, no vote ever
happens. It is a *price*, not a *fund*.

### 5.5 Slashing — deterrence arithmetic

Keep the percentage structure (it is the settled model); what changed under the
dynamic-bond rulings is that deterrence now **scales with the secret and with the
guardian's own history**:

- **No-reveal**: guaranteed loss = 50 % of the posted bond (40 burn + 10 creator)
  plus the forfeited `P` share, plus a ×1.26 step on `k` that raises the price of
  every future acceptance. Both the bond and the forfeited wage scale with
  `distance × bump`, so the penalty is always proportionate to the commitment
  broken — and since wage = `100 ÷ k` % of bond, skipping a trivial reveal always
  costs multiples of what revealing would have earned. Sound at every tier.
- **Early-reveal**: worst case loses the entire bond (40 burn / 10 creator /
  50 reporter) plus the wage plus the `k` step. The spec is already honest that the
  threshold, not the bond, is the primary guarantee on high-value secrets; the
  constant `threshold × k` collusion-cost property is the ruled framing. §8.3 keeps
  reporter-activity monitoring — the bounty only deters if leaks are *detectable*,
  and zero reporter traffic is itself a signal.
- **Self-dealing bound** (creator as reporter nets ≤ 60 % of the bond while 40 %
  burns) holds at all values; preserved by construction as long as burn > 0.
- **Validator-side slashing** (double-sign, downtime) is running on SDK defaults —
  needs explicit mainnet values in genesis (§10 Phase 4), aligned so that validator
  misbehaviour is never cheaper than guardian misbehaviour relative to stake at risk.

### 5.6 Deflation — sizing the sinks honestly

Three live sinks plus a one-off:

| Sink | Year-one scenario (50k secrets as §5.4; 1M tx; 200 registrations; 2 % no-reveal) |
|---|---|
| 10 % gas-fee burn (guaranteed, usage-proportional) | ~2,000 VEIL |
| No-reveal slash burns (5,000 offences × 40 % × ~2.6 VEIL avg bond) | ~5,200 VEIL |
| Split dust | negligible |
| Bootstrap sunset burn (§7 — unspent remainder) | one-off, at month 24 |
| **Total** | **≈ 7,000 VEIL — ~0.0007 % of supply** |

The honest story: **launch-scale deflation is symbolic, and the plan does not claim
otherwise.** What the design guarantees is the *mechanism* — every transaction burns,
every slash burns, and both compound with usage; the calibration report's
long-horizon runs show the burn becoming material at scale (its burned-supply column
reaches hundreds of millions of VEIL over the modelled horizon at simulated volumes).
Publish a burn dashboard rather than promising a rate (§8.3). Note the entry fee is
**not** a sink — it is validator income by ruling (§2.3 of the dynamic-bond plan),
the one fee in the system with no burn component.

### 5.7 The fee floor — a launch value, not a test value

The 0.1 uveil minimum gas price is node configuration — advisory, not consensus.
The launch floor must balance three forces: **spam cost** (filling blocks must be
expensive), **guardian margin** (guardians pay gas on confirm + reveal out of their
wage), and **creator accessibility** (three transactions per secret must stay small
against `P` — already strained at the entry level, §5.1).

| Floor (uveil/gas) | Typical 200k-gas tx | vs `S₁` (0.0432 VEIL) | Guardian confirm+reveal (~120k gas) vs 1-day wage (0.0144) | Cost to saturate the chain for a day |
|---|---|---|---|---|
| **0.1 (current default — proposed to enforce)** | **0.02 VEIL** | **~46 %** | **~83 %** | **~43,000 VEIL** |
| 0.5 | 0.1 VEIL | 2.3× | 4.2× | ~215,000 VEIL |

**Proposal (§9 Q2): consensus-enforce the floor at the current 0.1 uveil** — the
structural change (floor as protocol state, ante-handler enforced) is the point;
raising the *value* is not currently affordable, because at `rate = 1` gas is already
half the smallest secret and most of the smallest wage. The calibration report's
gas-fee sweep showed low sensitivity across its whole range (health flat), so the
value is not load-bearing; the accessibility arithmetic above is what binds. If Q5's
fiat corridor later implies a `rate` retune, revisit the floor's relation to `rate`
in the same upgrade. Guardian-registration spam is separately gated by the 1,000 VEIL
entry fee, so gas need not defend that path.

### 5.8 Share and threshold bounds — validation of the sliders

Owner stance (July 2026): the band bounds are *deliberate user-facing trade-off
sliders*, not economic dials — this section only validates that the values are right
and correctly related. The implemented chain:

```
MaxTotalShares = 32        ← the anchor: a transaction/state budget (distribute tx ≈
                             32 × ~94 B encrypted key shares + payload; NOT an SSS
                             limit — GF(256) supports 255 shares)
band                       ← threshold ≤ min_shares ≤ max_shares ≤ 32, with the gap
                             bound max_shares − min_shares < threshold (never-confirmed
                             candidates are sub-threshold on any activated secret)
MinShares      = 2         ← SSS minimum for any split
MinThreshold   = 2         ← a SECURITY floor, not a slider: 1-of-n would let any
                             single guardian collapse the time-lock alone
```

Verdict: the chain is sound and every relationship is enforced in validation and
covered by the conformance suite. Raising the band ceiling is really a decision
about `MaxTotalShares` (transaction size, state per secret, settlement work — all
linear in it), not about economics.

**One flag**: `MaxThreshold = 16` is the only bound with **no derivation** — the old
"SSS implementation limit" rationale is untrue at GF(256). Either document it as a
deliberate product ceiling or raise it toward the band ceiling (a 20-of-24 secret is
currently impossible while 16-of-16 is allowed — an inconsistency, not a
protection). Economically inert either way. Decision: §9 Q6.

---

## 6. Proposed values (what remains open)

The guardian-side constants (`rate`, `k` range and dynamics, entry fee, concurrency
cap, slash splits, bump range) are **ruled and implemented** — they appear in §2.1
as facts, not proposals. What this plan still proposes:

| Parameter | Current | Proposed | Rationale |
|---|---|---|---|
| Creation fee (`pool_fee_percent` + floor) | — (new) | **max(k2 × w, 10 % of `P`), non-refundable at request, 100 % to validators, never pooled; k2 = 1 or 4 guardian-days — see the §5.1 compression flag** | Security budget *and* selection-draw pricing in one flow (§5.4b); completes PROTOCOL.md Security Observations §1. §9 Q1 |
| Min gas price | 0.1 uveil, node-config | **0.1 uveil, consensus-enforced** | §5.7 — the structural change is the point; the value has no headroom at `rate = 1`. §9 Q2 |
| `community_tax` | 2 % (inherited) | **0 % (decided)** | No accidental treasury; set explicitly in genesis. Phase 1 |
| `MaxThreshold` | 16 (underived) | **document or raise toward the band ceiling** | §5.8; economically inert. §9 Q6 |
| `gov.min_deposit` | ~10 VEIL (inherited) | **materially higher (e.g. ≥ 10,000 VEIL)** | Gov exists only for upgrade coordination; make spamming it expensive |
| `gov.voting_period` | devnet 30 s | **e.g. 5 days mainnet** | Placeholder — set with launch coordination design |
| Validator slashing params | SDK defaults | **explicit genesis values** | Stop inheriting silently; calibrate vs guardian penalties |
| `staking.max_validators` / `unbonding_time` | 100 / 21 d | **keep, set explicitly** | Defaults fine; silence is not |
| Genesis pools (5/30/65 endowment) | devnet scaffolding | **replace with the §7 bootstrap model** | Minimal-holdings stance |

---

## 7. VEIL distribution — bootstrap, not endowment

The current 5/30/65 pools put **the entire supply behind three keys with no schedule
and no sunset** — an endowment model. Replace it with a bootstrap model built on three
rules:

1. **Size bottom-up from onboarding arithmetic** (in §5.1 anchor units), not from
   round percentages of supply.
2. **Time-boxed with a hard sunset**: every post-genesis allocation expires at month
   24, and the unspent remainder is **burned** — the governance-free way to end a
   programme (no vote on what to do with leftovers; deflation gets them).
3. **Mechanical disbursement only**: rebates and subsidies triggered by on-chain
   actions against published criteria — no discretionary grants, no committee.

### 7.1 Day-one tokens — the acquisition problem

Rebates and subsidies share a chicken-and-egg flaw: **they refund tokens the
participant must already have**. A guardian needs the 1,000 VEIL entry fee plus a
working float *before* it can do anything rebatable; a validator needs stake; and the
network needs both roles live at block 1, before any market exists to buy from.
Reimbursement is a de-risking layer, not an acquisition channel.

The resolution is that **a genesis file is distribution without custody**: any
allocation decided *before* launch is simply a balance at block 1 — nobody holds it,
transfers it, or administers it afterwards. So move acquisition before genesis:

1. **Operator earn-in (incentivised testnet)** — administered through the §7.4
   signup/credit workflow. Guardians and validators earn their genesis allocation by
   demonstrably performing the role on the final testnet (registering, accepting,
   revealing on time, maintaining availability — all measurable on-chain; scoring
   criteria published in advance). Each allocation is **purpose-sized**: a guardian
   slot = entry fee + working float ≈ 1,500 VEIL; a validator slot = the launch
   self-stake. The people who need working capital at block 1 are exactly those who
   proved they will deploy it — and each allocation is individually too small to
   matter as sell pressure if they defect.
2. **Public sale / LBP, settled into genesis.** The broad ≥ 97 % distribution is
   conducted before launch and buyers' balances are encoded directly in the genesis
   file. A liquid market therefore exists from block 1 — which is what makes the
   remaining acquisition paths work.
3. **Creators buy on market — theirs is the cheap role.** The anchor secret `S₁`
   costs 0.0432 VEIL; no pre-funding beyond pocket change is needed, and the usage
   rebates (§7.2) make early usage approximately free *after the fact*.

Guardians joining after the earn-in cohort face the ~1,500 VEIL entry cost with a
live market — a normal capital decision, softened by the continuing entry-fee rebate
while the bootstrap window lasts.

*Considered and rejected: protocol-level fee deferral* ("earn-in registration" — the
entry fee withheld from future wages instead of paid up front). It solves acquisition
with no allocation at all, but adds a debt state to the guardian lifecycle; not worth
the complexity while the testnet route exists.

### 7.2 Sizing the bootstrap (bottom-up)

Re-derived at the implemented constants; the totals are proposals to confirm with
Q5's fiat corridor (the validator lines in particular are sized against real-world
infrastructure costs, which are fiat-denominated).

**Pre-genesis — earn-in allocations, baked into the genesis file (zero custody):**

| Allocation | Arithmetic | Cost |
|---|---|---|
| Guardian slots (entry fee 1,000 + working float ~500) | 200 × 1,500 | 0.3M VEIL |
| Validator slots (launch self-stake) | 30 × 50,000 | 1.5M VEIL |
| **Pre-genesis subtotal** | | **1.8M VEIL (0.18 %)** |

**Post-genesis — mechanical rebates and subsidies (multisig, 24-month sunset,
remainder burned):**

| Mechanism | Arithmetic | Cost |
|---|---|---|
| Entry-fee rebates for post-earn-in guardians (paid after 6 months of maintained availability) | ~300 × 1,000 | 0.3M VEIL |
| Wage bridge (top up guardian wages while utilisation is low, ≤ 18 months) | scenario-sized | ≤ 0.2M VEIL |
| Validator uptime subsidies, years 1–2, decaying | 30 × fiat-anchored (Q5) | placeholder ~2.3M VEIL |
| Creator usage rebates (fee/pool refunds on completed lifecycles — §7.4 abuse caps) | ~100k secrets × small avg | ≤ 0.5M VEIL |
| **Post-genesis cap** | | **≈ 3.3M, capped at 5M VEIL (0.5 %)** |

**Total bootstrap: ≤ ~7M VEIL (< 0.7 % of supply)** — 1.8M distributed at genesis to
proven operators, ≤ 5M disbursed mechanically then sunset-burned. The remaining
**≥ 99 % is in holders' hands at block 1** via the bulk-distribution mechanism
(§9 Q3 — still open, and the biggest decision in this plan). Compare: the current
devnet design holds 100 % behind three keys indefinitely; this holds < 0.7 %,
briefly, for published mechanical purposes only.

### 7.3 Custody and accounting

Only the post-genesis slice (≤ 5M, 0.5 %) needs custody at all: a k-of-n multisig
for the 24-month window, publishing a simple monthly statement. Circulating-supply
accounting from day one:
`circulating = 1B − unspent bootstrap − burned − guardian-locked − validator-bonded`.
This number, not total supply, is what the market prices — and under this design it
starts above 99 % and only ever rises (until burns bite), which is the honest story.

### 7.4 The signup/credit workflow — off-chain bootstrapping, on-chain evidence

**Stance (July 2026): adopted as the acquisition front door for the bootstrap
cohorts; explicitly rejected as the bulk-distribution mechanism.**

The mechanism — a generalisation of the operator earn-in to all three roles:

1. **Signup** (off-chain): a lightweight account (email/OAuth, optional social or
   proof-of-personhood attestation later if abuse demands it) tied to a testnet
   address. This is a web product, not protocol surface.
2. **Credit accrual**: credits are earned for **on-chain, measurable, costly-to-fake
   actions** — the anti-abuse core. Guardian credits: registration, availability
   uptime, acceptances, on-time reveals across the incentivised testnet. Validator
   credits: uptime and participation. Creator credits: completed secret lifecycles.
   Off-chain actions (referrals, content) may exist but are capped to noise.
3. **Settlement**: pre-launch cohort credits settle as **genesis balances** (zero
   custody — §7.1); post-launch tranches settle as periodic **merkle-claim drops**
   from the multisig rebate allocation, on published conversion criteria.
4. **Caps everywhere**: per-account allocations are purpose-sized (a guardian slot,
   a validator stake, a creator's first secrets) and individually too small to farm
   profitably. The programme total lives inside the §7.2 caps and sunset-burns.

**Why this resists abuse well enough**: the scoring source is chain activity that
costs real time and real testnet operation (a farmed guardian account still has to
run a guardian through the scoring window); per-account caps bound the payoff of any
single sybil; and the whole slice is < 1 % of supply, so even leaked allocations are
bootstrap noise, not distribution damage. Accept the leakage; do not build a
KYC-grade identity system for a sub-percent slice.

**Why it must not be the bulk distribution**: credits are custodial IOUs until
settled — a credit ledger covering most of the supply recreates the endowment
problem (§7 preamble) with added liability and no market. The bulk mechanism —
public sale / LBP, or alternatively a long-running usage-mining programme (slower,
more Sybil-exposed, but sale-free) — remains the plan's biggest open decision
(§9 Q3).

**Usage-rebate abuse note** (the "refund transactions from the pool over time"
mechanism): a creator who is also a guardian can farm rebates by paying themselves —
the burns and gas make each cycle lossy, but the rebate can exceed the loss if
uncapped. Controls: rebates capped per account and per tranche, only on secrets that
complete a full lifecycle, and funded per-use from the capped allocation so the worst
case is bounded by design rather than by detection.

---

## 8. Evolution over time

### 8.1 Mutability: immutable constants, upgrade-only retuning

Settled by the design stance: the economic constants stay compile-time. There are no
parameter votes and no treasury to administer, so the protocol has **no recurring
governance surface at all** — x/gov exists solely to coordinate software upgrades (the
`make upgrade-scaffold` / `make devnet-upgrade-test` machinery already rehearses this).

What makes this stance *affordable* is the derivation discipline: every fee, bond and
wage is `ratio × rate`, so the only economically plausible correction — the fiat
level is wrong at launch — is a **single-constant change** (`rate`) that rescales the
entire ladder without disturbing any relative incentive. The expected number of
economic upgrades is zero or one. The ratios themselves (the `k` range and dynamics,
slash splits, `100 ÷ k` wage-to-bond, the fee percentages once ruled) are design
intent and should be treated as fork-worthy to change, exactly as spec.md's
"Fork-Based Evolution" principle already states.

### 8.2 Phases

**Phase A — Launch (months 0–6): subsidised bootstrap.** Bootstrap allocations carry
both sides of the market mechanically. Success = ≥ 100 active guardians, healthy
aggregate float, organic secrets at every bump tier, zero invariant violations.

**Phase B — Growth (months 6–24): subsidy decay, market discovery.** Wage bridge and
uptime subsidies decay on their published schedule; the creation fee becomes the
dominant recurring validator revenue as entry-fee income fades with registration
growth. If the §5.2 pricing corridor is breached, the one-time `rate` correction
ships as a coordinated upgrade.

**Phase C — Maturity (24+ months): nothing left to administer.** Bootstrap sunset
burns the remainder; all rewards organic; the economics are exactly the compiled
ratios playing out. The burn dashboard is the deflation story.

### 8.3 Metrics and retuning triggers

With no parameter governance, most "triggers" during Phases A–B pull *subsidy* levers
(which are ours to pull, mechanically) rather than protocol constants; only persistent
structural failure justifies an upgrade.

| Metric | Healthy | Trigger and response |
|---|---|---|
| Float utilisation (Σ locked ÷ Σ float) | 30–70 % | > 80 % sustained → guardian capital scarce: wage bridge (Phase A/B); persistent post-bootstrap → upgrade candidate on the wage side. < 15 % → oversupplied: let subsidies decay faster |
| Secrets failing to staff, by bump tier | ~0 at bump ≤ 5 | High-tier failures persistent → capital/availability depth problem: wage bridge, or accept as the market signal the spec already endorses |
| Commit-timeout rate | < 5 % | Rising → acceptance friction, or grinding (check against PROTOCOL.md §1 once the creation fee ships) |
| Mean `k` across active guardians | near 4.00 | Drifting up network-wide → systemic reliability problem, not a pricing problem — investigate before touching constants |
| Validator fee revenue ÷ value-at-risk in escrow | trending up | Flat at ~0 → creation fee too low; upgrade candidate |
| Burn rate | published, rising with usage | No target — dashboard honesty |
| Reporter reports filed | > 0 occasionally | Perpetual zero → either no leaks (good) or no monitoring (bad); a creator-side rebate can fund watchtower usage during bootstrap |
| Guardian registrations | net positive | Net decline → entry-fee or wage problem |
| Bootstrap spend vs published schedule | on curve | Deviation → publish and correct; sunset burn is the backstop |

---

## 9. Open decisions (user input required)

Settled and implemented (no longer open): the guardian-side economics in full —
`rate = 1`, dynamic `k` bonds, the 1,000 VEIL validator-routed entry fee, the
concurrency cap, slash splits — plus the wired 90/10 fee burn, `community_tax = 0`
(ruled; implementation is Phase 1), and the signup/credit workflow as the bootstrap
front door.

Still requiring explicit decisions. The numeric ones (Q1, Q2, Q5) read from the
calibration report
([ECONSIM_CALIBRATION_V1.md](../../reports/econsim-calibration-v1/ECONSIM_CALIBRATION_V1.md));
Q3, Q4 and Q6 are product/design decisions the calibration does not gate:

1. **Creation fee values**: now owned by
   [DONE_CREATION_FEE_PLAN.md](../DONE_CREATION_FEE_PLAN.md) (its §6 Q1,
   carrying the same recommendation: 10 % + a k2 = 4 guardian-day floor). The
   coupling stands: Q1 and Q5 should be ruled together — the VEIL amounts only
   mean something at a fiat level.
2. **Gas floor**: now owned by
   [DONE_CONSENSUS_FEE_FLOOR_PLAN.md](../DONE_CONSENSUS_FEE_FLOOR_PLAN.md)
   (its §4 Q1, carrying the same recommendation: enforce at the current
   0.1 uveil).
3. **Bulk distribution mechanism** (the big one): public sale / LBP settling into
   the genesis file (fast, clean float, regulatory surface), or a long-running
   usage-mining programme (sale-free, slower, more Sybil-exposed), or a
   combination? ~99 % of supply rides on this; §7.4 deliberately does not cover it.
4. **Credit-workflow scoring bar**: which actions earn credits at what weights,
   the per-account caps per cohort, and whether any personhood attestation is
   required at signup or only escalated on detected abuse. Needed before the final
   incentivised testnet.
5. **Pricing corridor**: what should the anchor secret `S₁` (1-day, 3-guardian,
   bump 1 = 0.0432 VEIL) cost in fiat at launch? This anchors `rate` and the TGE
   valuation together — and it decides whether Q1's validator budget and the §5.1
   gas-compression flag are problems or noise. The plan's most leveraged number.
6. **`MaxThreshold`**: document 16 as a deliberate product ceiling, or raise it
   toward the band ceiling (32) for slider consistency (§5.8)? Economically inert;
   validation and docs only.

## 10. Implementation phases (after approval)

Each phase lands independently with tests and spec.md updates in the same session
(CLAUDE.md documentation-synchronisation rule). Phases 0–1 carry no open numeric
content and may land at any time; Phase 2 waits on Q1 (and pairs naturally with a
Q5 ruling); Phase 3 on Q2; Phase 4 additionally waits on Q3/Q4 for the genesis
balances; Phase 5 is unblocked and its metrics are worth landing early.

- **Phase 0 — Truth reconciliation** *(docs only)*: sweep spec.md for residual
  pre-ruling economics wording — the mutability hedge ("governance-tunable" vs the
  immutable stance), any surviving old-model pricing prose, and replace the
  §Genesis Pool Allocations section with the bootstrap model (§7). (The fee-burn
  and bonded-economics sections are already accurate — most of the original
  Phase 0 list has been overtaken by the landed plans.)
- **Phase 1 — `community_tax = 0` + reward routing**: carved out into
  [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md)
  (the tax and the routing defect must land together).
- **Phase 2 — Creation fee**: carved out into
  [DONE_CREATION_FEE_PLAN.md](../DONE_CREATION_FEE_PLAN.md), which owns the
  design, the Q1 value ruling and the implementation — and records the
  validator reward-routing defect (July 2026 finding) its routing depends on.
  This plan retains the validator-budget analysis (§5.4) and the Q5 coupling.
- **Phase 3 — Consensus fee floor**: carved out into
  [DONE_CONSENSUS_FEE_FLOOR_PLAN.md](../DONE_CONSENSUS_FEE_FLOOR_PLAN.md).
- **Phase 4 — Genesis & launch config**: explicit mainnet genesis for gov, staking
  and validator-slashing params (§6 table); replace the three endowment pools with
  genesis-file balances from the bulk mechanism (Q3) + operator earn-in (§7.1) plus
  the single ≤ 5M multisig rebate allocation (§7.2); stand up the signup/credit
  service (§7.4) ahead of the final incentivised testnet; publish
  `docs/tokenomics.md` (public-facing) with the cost ladder, circulating-supply
  definition, credit/earn-in criteria, rebate schedule and sunset-burn commitment.
- **Phase 5 — Observability**: burn/utilisation/mean-`k`/validator-revenue metrics
  exposed via queries or telemetry to feed the §8.3 dashboard.
