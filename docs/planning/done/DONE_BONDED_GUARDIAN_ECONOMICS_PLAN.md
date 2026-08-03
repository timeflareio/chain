# Bonded Guardian Economics — Design & Implementation Plan

*A replacement for the guardian collateral and slashing model: per-obligation **bonds**
instead of a single shared stake, with **creator-priced security**. Guardians post
returnable collateral per secret they accept; creators pay a formula-derived reward and
tune security with one dial.*

> **Status: IMPLEMENTED (July 2026).** All five phases of the
> [implementation plan](#11-implementation-plan-and-remaining-checks) have landed in
> `x/secrets/`: the specification was lifted into
> [spec.md "Secret Economics & Slashing"](../../spec.md#secret-economics--slashing) (now the
> protocol authority), the guardian float/entry-fee registration, protocol-derived pricing,
> per-secret bond escrow, threshold-independent settlement, percentage slashing, and
> pro-rata cancellation are live, and the no-stranded-bonds invariant is covered by
> `x/secrets/keeper/no_stranded_bonds_test.go`. This document remains as the design
> rationale and decision log.
>
> **Fixes:** [PROTOCOL.md](../../CHAIN_MECHANICS.md) Suspected Defects **#2** (impunity hole) and
> **#3** (stranded guardian) are dissolved by this model — both now closed.

## Contents

1. [The problem](#1-the-problem)
2. [The solution in one page](#2-the-solution-in-one-page)
3. [Variables and parameters](#3-variables-and-parameters)
4. [How it works](#4-how-it-works)
5. [Worked examples](#5-worked-examples)
6. [Security boundary — what this does not solve](#6-security-boundary--what-this-does-not-solve)
7. [Specification: Secret Economics (liftable)](#7-specification-secret-economics-liftable)
8. [Interactions with earlier protocol decisions](#8-interactions-with-earlier-protocol-decisions)
9. [Decision log](#9-decision-log)
10. [Design rationale — paths considered and rejected](#10-design-rationale--paths-considered-and-rejected)
11. [Implementation plan and remaining checks](#11-implementation-plan-and-remaining-checks)

---

## 1. The problem

The current protocol gives every guardian a **single shared stake** (a fixed 10,000 VEIL)
and deducts **fixed penalties** from it (5,000 VEIL for a missed reveal, 10,000 VEIL for
an early reveal). Three problems all trace back to one root cause — **accountability is
pooled, not per-obligation**:

1. **Impunity hole** (PROTOCOL.md Suspected Defect #2). A guardian whose stake has been
   drawn below a penalty escapes no-reveal slashing entirely — `executeSlashing` errors
   instead of slashing — so it can then no-show on its remaining assignments for free.
2. **Stranded guardian** (PROTOCOL.md Suspected Defect #3). A guardian slashed below the
   10,000 minimum keeps its existing assignments but can no longer be meaningfully
   punished on them, and its only route to recover is income from the very assignments it
   may be failing.
3. **Specification gap.** `spec.md` insists penalties are fixed "regardless of a
   guardian's total stake", but never says what happens when the stake is *below* the
   penalty — the silence that produced the clamp-versus-error inconsistency in the first
   place.

A **per-obligation collateral model removes all three by construction**, not by patching
each: there is no shared pool to drain and no global floor to fall below. Every accepted
secret carries its own bond, sized so that slashing is always a percentage of collateral
that is guaranteed to be present.

---

## 2. The solution in one page

**Guardians** stop posting one big stake. Instead:

- They pay a one-off **entry fee `F`** (burned) to register as a guardian.
- They keep a **deposited float** of VEIL. Each secret they accept **locks a bond `B`**
  from that float; the bond is **returned** if they behave and **slashed** if they do not.

**Creators** price each secret with a single security dial, `bump`:

- They fund a **reward pool `P`** up front, computed by protocol formula from how long the
  secret is held, how many guardians it needs, and `bump`.
- A higher `bump` costs more (bigger `P`) **and** makes each guardian lock more (bigger
  `B`) — so buying security raises both the carrot and the stick together.

**Deterrence** comes primarily from the reward, not the bond: a misbehaving guardian
**forfeits its share of `P`** *and* has its bond slashed. Because the reward the guardian
would forfeit is exactly what the creator paid, deterrence scales tightly with price.

**Settlement** happens once, at the reveal window's end, and is **threshold-independent**:
guardians who revealed correctly get their bonds back and split the pool; no-shows are
slashed; the cryptographic question of whether enough shares arrived to reconstruct the
secret does not change any payment.

Everything a secret needs (`B`, `P`) is **derived** via `bump` from `rate` — the single master
price level — so revaluing the economy is one knob, and nothing downstream is a stale
hard-coded value.

---

## 3. Variables and parameters

Values are of three kinds. ⚠️ **Derived values must never be hard-coded**: they are
computed from the base constants, so that changing a base value cascades everywhere.

### Base constants

Hand-set; protocol-level for now, with the intent that the economy or governance may tune
them later.

| Symbol | Name | Units | Meaning |
|--------|------|-------|---------|
| `rate` | base reward rate | VEIL / guardian / block | Standby reward: what one guardian earns per block before tier scaling. **The master price level** (a flow); everything else scales with it. |
| `bond_multiplier` | bond horizon | blocks | Reference lock-horizon that sizes the base bond: *the base bond equals `bond_multiplier` blocks of one guardian's standby reward.* Tunes the bond-to-reward ratio. Not a yield — a plain scaling factor. |
| `F` | entry fee | VEIL | One-off, **burned**, paid to register. The right to operate. Independent of the rest. |
| `max_tier` | max security factor | — | Ceiling on `bump`: a creator picks any `bump ∈ [1, max_tier]`. |
| bond distribution | slash split | % of bond | Per-violation split of a slashed bond over {burn, creator, reporter, returned} — see [§4.5](#45-slashing). |
| `H` | max reveal distance | blocks | Upper bound on `distance`. |

### Creator inputs (chosen per secret)

| Symbol | Name | Units | Meaning |
|--------|------|-------|---------|
| `bump` | security factor | — | Any value in `[1, max_tier]` (fixed-point, 2 decimal places); default `1`. Scales reward and bond together. |
| `distance` | hold length | blocks | Blocks from `CommitDeadline` to settlement (≈ `reveal_end_block`); must be ≤ `H`. |
| `num_guardians` | guardians | count | Number of guardians the secret needs (equals `shares`). |
| threshold | reconstruction threshold | count | Shares required to reconstruct (existing SSS parameter). |

### Derived values (compute — never hard-code)

| Symbol | Formula | Value (v1) | Meaning |
|--------|---------|-----------|---------|
| `bond_unit` | `rate × bond_multiplier` | ≈ 5,000 VEIL | The base bond (the bond at `bump = 1`). A named intermediate. |
| `B` | `bond_unit × bump` | — | Bond a guardian locks per accepted secret; also the float it must have unlocked to accept it. |
| `P` | `rate × distance × num_guardians × bump` | — | Reward pool; the creator funds it up front. |

**One master knob, one ratio knob.** `rate` is the master price level: change it and both the
reward `P` and the bond `B` move together — so revaluing the whole economy is a single edit and
the reward and collateral can never drift apart. `bond_multiplier` is the second knob: it sets
the bond-to-reward *ratio* (how much collateral a unit of reward price buys) without touching
rewards. The bond is still *not* a return on capital — the guardian's income is `P`; the bond
is a returnable deposit, sized here as a fixed horizon of standby reward. `bump` is the
per-secret dial that scales both reward and bond together. `max_tier`, `H`, `F`, and the bond
distribution are independent.

### Provisional v1 values

| Constant | Value |
|----------|-------|
| `rate` | 0.0001 VEIL / guardian / block |
| `bond_multiplier` | 50,000,000 blocks (≈ 9.5 years of standby reward) |
| `F` | 1,000 VEIL |
| `max_tier` | 10 (so `bump ∈ [1, 10]`) |
| no-reveal bond split | 40 % burn / 10 % creator / 50 % returned |
| early-reveal bond split | 40 % burn / 10 % creator / 50 % reporter / 0 % returned |
| `H` | 52,560,000 blocks (≈ 10 years) |
| **Derived** `bond_unit` | `rate × bond_multiplier` ≈ 5,000 VEIL |

> All block↔time conversions in this document assume ~6-second blocks (so `H` ≈ 10 years and
> `bond_multiplier` ≈ 9.5 years).

---

## 4. How it works

### 4.1 Registration, the float, and eligibility

To become a guardian, an address pays the one-off **entry fee `F`** (burned). This is the
"price to play": paying it is what grants guardian status. It is a burned commitment and a
deflationary sink, not a per-secret cost, and it is never returned. (The real economic barrier
to spinning up guardians is the working float they must post to accept secrets, not `F`.)

A guardian then maintains a **deposited float** of VEIL. Accepting a secret **locks** a
bond `B` from the *unlocked* portion of the float. If the unlocked balance is
insufficient, the acceptance is **rejected** — a guardian cannot take on a secret it
cannot collateralise. Bonds unlock again at settlement, so the float recycles. A guardian
may withdraw only the *unlocked* portion of its float.

**Eligibility is per secret.** Acceptance is the hard gate: `MsgConfirmShares` locks `B` and
**fails if `unlocked < B`**, so a guardian can never take on a secret it cannot collateralise.
Selection uses the *same* per-secret test — a guardian is a candidate for a given secret only
while its unlocked float covers **that secret's bond `B`** — so cheap secrets (`bump = 1`,
`B = bond_unit`) stay open to small guardians, and only high-`bump` secrets demand a large
float. There is no universal floor. This self-throttles concurrency naturally: each accepted
secret locks its `B`, lowering the unlocked float, so a guardian can hold roughly `float ÷ B`
concurrent secrets before it stops qualifying for more. Because selection is only advisory
(acceptance is the real gate), the existing acceptance buffer still absorbs the occasional
guardian that declines or is out-raced to its float by another secret.

`F` gates *registration*; a secret's own bond `B` gates *acceptance* of that secret.

### 4.2 The reward pool `P`

The creator funds a reward pool up front:

```
P = rate × distance × num_guardians × bump
```

`P` is split at settlement among the guardians who revealed. It is the **primary security
lever**: a misbehaving guardian forfeits its share, so a bigger pool is a bigger payday to
lose.

**Why `num_guardians` is a factor** (rather than a fixed pool divided later): each guardian
is a *separately-paid resource* — it locks the same bond and does the same work regardless
of how many others there are — so each earns the same wage (`rate × distance × bump`,
independent of the count). The pool therefore scales with the count, which correctly makes
*more redundancy cost the creator more*. Note the
pool is **sized** on `num_guardians` but **split** among the *revealers*, so the two counts
differ and do not cancel — that is what gives revealers a larger share when others fail.

### 4.3 The bond `B`

Each guardian that accepts locks a bond:

```
B = bond_unit × bump          where   bond_unit = rate × bond_multiplier
```

The base bond `bond_unit` is pegged to the reward price level: it equals `bond_multiplier`
blocks of one guardian's standby reward. This keeps `rate` as the single master knob — raise
it and both what the creator pays (`P`) and what each guardian locks (`B`) rise together —
while `bond_multiplier` sets the bond-to-reward ratio.

The bond is *not* a return on capital: the guardian's income is the reward `P`, and the bond
is a returnable security deposit that sits alongside purely to be slashable. `bond_multiplier`
is a plain scaling factor (a reference lock-horizon), **not** a yield — and because VEIL is
deflationary (every slash burns), simply locking VEIL is already mildly advantageous, so no
return needs to be manufactured on the bond.

The bond correlates with what the creator pays in two ways:

- **Via `bump`:** the same dial that scales the reward scales the bond, so buying more
  *security* raises both.
- **Via the forfeited reward:** a guardian's real stake-at-risk is `bond + the reward it
  would forfeit` on misbehaviour, and that forfeited reward *is exactly what the creator
  pays* — a 1:1, automatic coupling.

The bond is **deliberately independent of `distance` and `num_guardians`**. It moves only with
the price level (`rate`) and the *security* bought (`bump`), not the hold length: the
deterrence a secret needs does not grow with how long it is held, and locking more collateral
for longer against the same fee would simply make long secrets unattractive to guardians (see
[§10](#10-design-rationale--paths-considered-and-rejected)). Headcount is excluded because
each guardian's obligation is independent of the others.

### 4.4 The reveal-distance cap `H`

Reveal distance is **bounded** by `H` (≈ 10 years). This bounds the worst-case capital
lock, makes guardian capacity knowable, and lets bonds recycle predictably — so
under-capitalisation is a rare edge case rather than the norm. Ultra-long ("someday")
secrets beyond `H` are out of scope; there is no bond roll-over.

`H` is what makes the matching problem tractable:

- Most guardians have the credit to accept most secrets; under-capitalisation is the
  exception.
- The existing **30 % acceptance buffer** absorbs the residual — a guardian that declines
  or is rejected for lack of bond credit is simply another rejection the buffer already
  tolerates, so no willingness-advertising market is needed for v1.
- An extreme-`bump` secret that exhausts even the buffer is **allowed to fail to clear**:
  the creator gets that feedback and lowers `bump` or waits for deeper capital. This is a
  self-correcting market signal, not a failure mode to engineer around.

### 4.5 Slashing

Penalties are a **percentage of the posted bond**, never a fixed VEIL amount. This removes
the impunity hole by construction: a percentage of the bond can never exceed the bond, so
"insufficient stake to slash" cannot occur. Under any misbehaviour, **both** pools move for
that guardian — its bond is slashed *and* its reward share is forfeited.

There is deliberately **no eviction or deregistration**. A repeat offender simply keeps
burning its own bonds and forfeiting rewards, which is a sufficient consequence without
extra lifecycle machinery.

Two pots move at settlement, and they are independent: the guardian's **bond** and its share
of the creator's **reward pool `P`**. The bond is distributed as a single per-violation split
(each row sums to 100 % of `B`); the reward share follows separately.

| Outcome | Bond → burn | Bond → creator | Bond → reporter | Bond → returned | Its `P` share |
|---------|-------------|----------------|-----------------|-----------------|---------------|
| **Revealed** | — | — | — | 100 % | **paid** (splits `P` with the other revealers) |
| **No reveal** (operational) | 40 % | 10 % | — | 50 % | **forfeited** → to the revealers |
| **Early reveal** (malicious, proven) | 40 % | 10 % | 50 % | 0 % | **forfeited** → to the revealers |

Early reveal is strictly harsher than no reveal on two counts: none of the bond comes back
(0 % vs 50 %), and half of it funds a reporter bounty. One honesty note: because anyone can be
the reporter, a leaking guardian can self-report through a second address and recapture the
50 % bounty on its own bond — so the *guaranteed* early-reveal loss is the 40 % burn, the 10 %
creator slice, and the forfeited reward share, not the full bond. This is inherent to any
reporter-bounty design and is accepted.

**The creator's fee is not refunded on a guardian's failure** — a failed guardian's `P` share
goes to the guardians who *did* reveal, not back to the creator; the creator's only recompense
for that failure is its 10 % slice of the slashed bond. `P` returns to the creator in full only
on commit-timeout or when nobody reveals; cancellation refunds the *unearned remainder* after
paying guardians pro-rata (§4.7). The creator's slash slice stays below 100 %, which is what
defends the [self-dealing invariant](#48-the-self-dealing-invariant).

### 4.6 Settlement

Settlement is **threshold-independent**. Whether the reveal threshold was met determines
only whether the *recipient can reconstruct the secret* — a cryptographic outcome — and
does **not** branch the payments:

- The pool `P` is **split among the guardians who revealed correctly** (no-shows and
  early-slashed guardians are excluded). Fewer revealers means a **larger share each** — a
  guardian is paid for revealing regardless of what the others did, making revealing the
  dominant strategy.
- `P` is **refunded to the creator in full** only if *nobody* revealed. (Cancellation refunds
  just the unearned remainder — see §4.7.)

Consequently there is **no refund-on-failure** for the creator: if a secret fails because
too few revealed, the pool is still paid to whoever did reveal. The creator's downside
protection is the slashed no-show bonds, and the creator bears the risk of choosing too
high a threshold or too few reliable guardians. *(This is a deliberate change from
`spec.md`'s stated creator-protection.)*

**Timing.** Reward distribution, honest-bond return, and no-reveal slashing all happen at
**one window-end settlement event**, folded into the existing `ProcessExpiredRevealWindows`
EndBlock sweep. **Early-reveal is the sole exception**: on a valid report the bond is
slashed and the reporter paid *immediately* (a window can be years away, so deferring the
bounty would gut the monitoring incentive); only that guardian's reward-pool *exclusion*
waits for settlement.

### 4.7 Bond return, cancellation, and no-fault refunds

Every guardian that revealed correctly gets its bond back; every guardian that no-showed or
early-revealed is slashed — independent of whether the secret hit its threshold. Two paths end
a secret before its reveal window, and both return every bond in full with no slashing:

- **Commit-timeout** (no fault) — fewer than `num_guardians` confirm before the commit
  deadline, so the secret is never committed: all accepted bonds returned, pool refunded to
  the creator in full. (`threshold` plays no part here — it is only the reconstruction
  minimum; the commit phase must fill the full requested `num_guardians`.)
- **Creator cancellation** (paid exit) — a key product feature: the creator may cancel at any
  point before the reveal window opens. All bonds are returned in full, and the pool settles
  **pro-rata by distance travelled** — each guardian is paid for the blocks it actually
  guarded, and the creator is refunded the unearned remainder:

  ```
  elapsed             = cancel_block − CommitDeadline      (floor 0)
  per-guardian payout = rate × elapsed × bump              (= (P ÷ num_guardians) × elapsed ÷ distance)
  creator refund      = P − num_guardians × per-guardian payout
  ```

  Cancelling during the commit phase (`elapsed = 0`) refunds everything; cancelling one block
  before the window opens pays the guardians almost the full pool. **Cancellation is never a
  free way to lock guardian capital.** Once the reveal window opens, cancellation is no longer
  possible — the secret proceeds to normal settlement.

This is genuinely new per-`(guardian, secret)` escrow accounting, which reverses `spec.md`'s
current "simple removal without state management overhead" principle. It is a deliberate
reversal, justified by the accountability gains, and must be stated as such in the spec.

### 4.8 The self-dealing invariant

The guard against a creator acting as its own guardian to manufacture a payout:

> **The creator's share of any slash must be < 100 %.**

With the v1 splits (the creator receives 10 % of a slashed bond in either case), a self-dealing
creator/guardian always nets a loss on an engineered failure — it recovers only the returned
portion plus that 10 % creator share, never the whole bond. The reward pool cannot be a profit
source either: paying yourself as your own revealing guardian merely round-trips your own money.

---

## 5. Worked examples

All examples use the [provisional v1 values](#provisional-v1-values): `rate = 0.0001`,
`bond_multiplier = 50,000,000`, so `bond_unit = rate × bond_multiplier = 5,000 VEIL`.

### 5.1 A single secret, end to end

**Alice** creates a secret: `bump = 5`, `distance = 1,000,000` blocks (≈ 70 days),
`num_guardians = 9`, `threshold = 3`.

Derived:

- **Reward pool** `P = rate × distance × num_guardians × bump = 0.0001 × 1,000,000 × 9 × 5
  = ` **4,500 VEIL** (Alice funds this up front; ≈ 500 per guardian).
- **Bond** `B = bond_unit × bump = 5,000 × 5 = ` **25,000 VEIL** (each accepting guardian
  locks this from its float, and gets it back on revealing).

Settlement outcomes for one of Alice's guardians (bond 25,000, reward share ≈ 500), keyed
to what the guardian did — not to whether the secret reconstructed:

| What this guardian did | Its bond | Its reward | Alice |
|------------------------|----------|-----------|-------|
| Revealed correctly | 25,000 returned | share of the 4,500 pool (≈ 500 if all 9 reveal; **more** if others failed) | pays 4,500 to the revealers |
| No-reveal | slash 12,500 → 10,000 burn / **2,500 to Alice**; 12,500 returned | forfeited → to revealers | + 2,500 |
| Early-reveal (proven) | slash 25,000 → 10,000 burn / 12,500 reporter / **2,500 to Alice** | excluded | + 2,500 (reporter + 12,500) |

**If Alice cancels instead** at 400,000 blocks past `CommitDeadline` (40 % of the distance):
every bond is returned in full, each guardian is paid `0.0001 × 400,000 × 5 = 200 VEIL` for the
blocks it guarded (1,800 total), and Alice is refunded the unearned **2,700 VEIL**.

### 5.2 The bond tracks only `bump`; the reward tracks everything

Per-guardian figures for three secrets spanning the full range. The **reward `P`** responds
to all three creator inputs (`bump`, `distance`, `num_guardians`); the **bond `B`** responds
to `bump` alone — so it ranges only from 5,000 (`bump = 1`) to 50,000 (`bump = 10`, the
`max_tier` ceiling), regardless of how long or how large the secret is:

| Secret | `bump` | `distance` | guardians | Reward pool `P` | Bond `B` (each) |
|--------|--------|-----------|-----------|-----------------|-----------------|
| Short, low security | 1 | 100,000 (≈ 7 d) | 5 | 50 VEIL | 5,000 |
| Alice (medium) | 5 | 1,000,000 (≈ 70 d) | 9 | 4,500 VEIL | 25,000 |
| Long, high security | 10 | 5,256,000 (≈ 1 y) | 15 | 78,840 VEIL | 50,000 |

### 5.3 One knob revalues everything; a second tunes the ratio

Halving `rate` (0.0001 → 0.00005) halves both the reward pool `P` *and* the bond `B` at once —
because `bond_unit = rate × bond_multiplier` moves with `rate` too. That is the single master
revaluation knob: the reward and the collateral cannot drift apart. If instead you want bonds
to bite harder *relative* to rewards, raise `bond_multiplier` alone — that lifts `B` while
leaving `P` untouched. Nothing is hard-coded downstream, so both knobs cascade cleanly.

---

## 6. Security boundary — what this does not solve

The protocol does **not** try to price out collusion — a sufficiently valuable secret can
always out-bid its guardians, and that is not a game the chain can win. What it does is give
the creator the knobs to raise the cost and the count an attacker must overcome: `bump` raises
each guardian's stake-at-risk (bond + forfeited reward), and `threshold` sets how many
guardians must be subverted. The **threshold remains the primary time-lock guarantee**; `bump`
just raises the ante. Neither is a promise of unbreakability, and the protocol makes no such
claim.

Early-reveal deterrence additionally depends on **detection**: it requires a reporter with HMAC
evidence (as today). An undetected off-chain leak forfeits nothing. The specification must
state this plainly so that no one mistakes a large bond for an absolute guarantee.

---

## 7. Specification: Secret Economics (liftable)

*This section is written to be lifted, largely verbatim, into `spec.md`. It is
self-contained and declarative.*

### 7.1 Actors and quantities

- **Guardian** — an address that has paid the entry fee `F` and may store shares and
  reveal them. Maintains a deposited **float** of VEIL, partitioned into `locked` and
  `unlocked`.
- **Creator** — an address that publishes a secret and funds its reward pool.
- **Reporter** — any address that submits valid evidence of an early reveal.

Three distinct VEIL quantities, with different owners and fates:

| Quantity | Owner | Lifetime | Fate |
|----------|-------|----------|------|
| Entry fee `F` | Guardian | Paid once at registration | **Burned** |
| Bond `B` | Guardian | Locked at acceptance, released at settlement | **Returned** if honest; **slashed** if not |
| Reward pool `P` | Creator | Funded at publication, distributed at settlement or cancellation | Split to revealers at settlement; on cancellation paid pro-rata for blocks guarded (remainder refunded); refunded in full on commit-timeout or if no one reveals |

### 7.2 Parameters

Base constants (governance-tunable): `rate` (master reward price), `bond_multiplier` (bond
horizon, in blocks), `F`, `max_tier` (the `bump` ceiling), the per-violation bond distribution,
and `H`. `rate` is the master knob — reward and bond both scale with it; `bond_multiplier` sets
the bond-to-reward ratio. `bump` is fixed-point with 2 decimal places, `bump ∈ [1, max_tier]`.
Provisional values are in [§3](#provisional-v1-values).

Derived quantities (computed, never stored as literals):

```
bond_unit = rate × bond_multiplier
B         = bond_unit × bump
P         = rate × distance × num_guardians × bump
```

### 7.3 Guardian lifecycle and the float

1. **Register.** Pay `F` (burned). The address becomes a guardian.
2. **Fund.** Deposit VEIL into the float. Withdrawals are permitted only from `unlocked`.
3. **Be selected.** For a given secret a guardian is a candidate only while
   `unlocked ≥ B` for that secret. Selection is otherwise protocol-controlled and
   verifiably random (unchanged), and is advisory — acceptance (step 4) is the hard gate.
4. **Accept** (`MsgConfirmShares`). Locks `B` from `unlocked`. Acceptance **fails** if
   `unlocked < B`. Acceptance is **first-`num_guardians`-to-confirm**: the first
   `num_guardians` valid confirmations win the slots and lock their bonds; later confirmations
   are rejected without locking. Ordering is deterministic (block height, then in-block
   transaction order).
5. **Settle.** At settlement, `B` is returned, slashed, or refunded per §7.5–7.6, and the
   corresponding amount moves from `locked` back to `unlocked` (or out of the float).

### 7.4 Secret lifecycle and money movement

Timeline (all offsets fixed at publication; `CreatedAt < CommitDeadline <
RevealStartBlock < RevealEndBlock`):

1. **Publish** (Phase 1). The creator funds `P` up front. `distance` is measured from
   `CommitDeadline` to settlement (≈ `RevealEndBlock`); `distance ≤ H`.
2. **Distribute & accept** (Phases 2–3). Guardians accept and lock bonds up to
   `CommitDeadline`.
3. **Hold**. Between `CommitDeadline` and `RevealStartBlock`, bonds remain locked. The
   creator may cancel at any point before `RevealStartBlock` (§7.7).
4. **Reveal window** (`RevealStartBlock` … `RevealEndBlock`). Guardians reveal shares.
   Cancellation is no longer possible.
5. **Settlement** (one EndBlock event just after `RevealEndBlock`). See §7.5.

### 7.5 Settlement (threshold-independent)

A guardian has **revealed correctly** if and only if its revealed share verifies against the
HMAC commitment recorded at distribution; anything else — an invalid share or no submission —
is a no-reveal.

At settlement, for each guardian (bond fate first; the reward fate is the closing clause):

- **Revealed correctly** → bond `B` returned to `unlocked`; guardian included in the split
  of `P`.
- **No-reveal** → of the bond `B`: 40 % burned, 10 % to the creator, 50 % returned; guardian
  excluded from `P`.
- **Early-reveal, proven** → handled immediately on report (see §7.6), and excluded from
  `P` here.

The reward pool `P` is split equally among the included (revealing) guardians. If **no**
guardian revealed, `P` is refunded to the creator. `P` is never partially refunded on a
per-guardian failure — a failed guardian's share goes to the revealers. Integer-division dust
from any split (pool or bond) is **burned**.

### 7.6 Early-reveal reporting

On a valid `MsgSlashGuardian` (HMAC evidence proving a pre-window reveal), **immediately**
slash the full bond `B` — 40 % burned, 10 % to the creator, 50 % to the reporter, 0 %
returned — and mark the guardian for exclusion from `P` at settlement.

Reports are accepted **only before `RevealStartBlock`**. Once the reveal window opens the
shares are due to be public, so evidence of an "early" reveal is moot — late reports are
rejected. (By construction, a bond can therefore never be slashed after it has been released.)

### 7.7 Cancellation and no-fault refunds

Two paths end a secret before its reveal window; both return every bond in full:

- **Commit-timeout**: fewer than `num_guardians` confirmations by `CommitDeadline` (the full
  requested count must be met to commit; `threshold` is only the reconstruction minimum).
  Bonds returned in full; `P` refunded in full.
- **Creator cancellation** (permitted only before `RevealStartBlock`): bonds returned in
  full; the pool settles pro-rata by distance travelled, with the remainder refunded to the
  creator:

  ```
  elapsed             = cancel_block − CommitDeadline   (floor 0)
  per-guardian payout = rate × elapsed × bump
  creator refund      = P − num_guardians × per-guardian payout
  ```

### 7.8 Invariants

- **Self-dealing:** the creator's share of any slash is < 100 %.
- **No stranded bonds:** every path a secret can take terminates in a settlement that
  returns, slashes, or refunds every locked bond. No bond is left locked indefinitely.
- **Percentage slashing:** every penalty is a fraction of the posted bond, so slashing can
  never fail for insufficient collateral.
- **Paid hold:** once a secret commits, every block a bond stays locked is a block its
  guardian is earning for — via the pool at settlement, or pro-rata on cancellation. A
  creator can never lock guardian capital for free.

---

## 8. Interactions with earlier protocol decisions

- **Reinstates capacity limiting, the good way.** `max_capacity` was removed as arbitrary
  overhead; per-secret bonds bring back a **capital-based, self-adjusting** concurrency limit —
  a guardian can hold roughly `float ÷ B` concurrent secrets (exactly: until its unlocked float
  can no longer cover the next secret's `B`) — which is more elegant than a hand-set number.
- **Two burn sinks.** The entry fee and the burn portion of every slash both reinforce the
  1-billion fixed-supply deflationary model.
- **A philosophical shift in slashing.** Penalties move from **fixed and predictable** to
  **variable, percentage-based and market-priced**. This is a rewrite of the Slashing
  section's premise, not an amendment — better suited to secrets of widely varying value.
- **"Arbitrary future reveal times" becomes "bounded but generous"** (≈ 10 years, via `H`).
  Real time-locks are months to years, so little of practical value is lost — but a stated
  promise is narrowed and the spec should say so.

---

## 9. Decision log

Settled decisions (July 2026). Numbers are stable references.

1. **Entry fee `F`** — the guardian's one-off price to play, burned at registration; not
   per secret, not returnable.
2. **`bump` is the single creator security dial** — a continuous factor `bump ∈ [1, max_tier]`
   (`max_tier = 10`), fixed-point with 2 decimal places, default `1`. It scales both the reward
   pool and the bond. Supersedes the earlier discrete tier set `{1, 2, 5, 10, 20}`
   (simplification C).
3. **Reward pool `P = rate × distance × num_guardians × bump`** — protocol-priced, no
   guardian-quoted pricing; funded up front, split among revealers. The reward is the
   primary deterrent (misbehaviour forfeits the share).
4. **Bond `B = bond_unit × bump`** with **`bond_unit = rate × bond_multiplier`**, keyed to
   `bump` and independent of `distance` and `num_guardians`. `bond_multiplier` is a base
   constant (a reference lock-horizon, in blocks), so the bond pegs to the reward price level:
   `rate` is the single master knob (reward and bond scale together), and `bond_multiplier`
   tunes the bond-to-reward ratio. The bond is *not* a return on capital — it is a returnable
   deposit; `bond_multiplier` is a plain scaling factor, **not** a yield. Supersedes the
   earlier `base × M` / `base = 1 VEIL` construction, the "~10× collateral ratio" heuristic,
   the interim `bond_unit = rate ÷ yield` derivation (dropped as a false yield frame), and the
   brief independent-base-constant `bond_unit` (dropped so `rate` remains the single
   revaluation knob).
5. **Slashing is a percentage of the posted bond**, expressed as one per-violation split of the
   bond over {burn, creator, reporter, returned}. No-reveal returns half the bond; early-reveal
   returns none. (Simplification B: collapses the old two-step "slash fraction × distribution"
   into a single table — same numbers, fewer parameters.)
6. **Bond distribution** — no-reveal: 40 % burn / 10 % creator / 50 % returned; early-reveal:
   40 % burn / 10 % creator / 50 % reporter / 0 % returned. The creator share stays < 100 %, so
   the self-dealing invariant holds. A failed guardian's **reward** share is separate: forfeited
   to the revealers, never refunded to the creator except in the no-fault cases.
7. **No eviction or deregistration** on any violation — a repeat offender keeps burning
   bonds and forfeiting rewards. Early reveal is harsher via the bond slice and the reporter
   bounty.
8. **Settlement is a single window-end event** and **threshold-independent** — revealers
   split the pool and get bonds back; no-shows are slashed; whether the secret reconstructed
   is irrelevant. This removes the creator's refund-on-failure (`P` refunded in full only if
   nobody reveals or on commit-timeout; pro-rata remainder on cancellation) and, as a side
   effect, dissolves the honest-but-unpaid case.
9. **Max reveal distance `H` ≈ 52,560,000 blocks (~10 years)** — extend the existing cap and
   remove the redundant shorter constant; ultra-long secrets are out of scope (hard cap, no
   roll-over).
10. **Acceptance buffer stays at 30 % for v1** — acknowledged as not ideal, but out of scope
    for this plan and not a blocker.
11. **Bonds stay denominated in fixed VEIL** — appreciation over long holds is accepted as
    extra incentive to take long bonds.
12. **Clean-cut migration** (nothing live): registration, `MsgUpdateGuardian`,
    `MsgConfirmShares`, withdrawal, and both slashing paths all change shape.
13. **`rate` is the master base constant; `bond_multiplier` is the ratio constant**
    (both governance-tunable later). `bond_unit = rate × bond_multiplier`, and `B` and `P`
    derive from `rate` (via `bond_unit` for the bond) — none may be hard-coded downstream.
    Changing `rate` revalues the whole economy in one edit; changing `bond_multiplier` re-tunes
    the bond-to-reward ratio without touching rewards.
14. **Provisional v1 values are set** (adjustable) — see [§3](#provisional-v1-values).
15. **Float custody = per-guardian module escrow.** State tracks each guardian's `total` and
    `locked` (`unlocked = total − locked`); withdrawal capped at `unlocked`; acceptance locks
    a bond or is rejected. Replaces the old registration-stake + withdrawal flow.
16. **Eligibility is per secret: `unlocked ≥ B` for that secret** — there is no universal float
    floor. Acceptance (`MsgConfirmShares`) is the hard gate and rejects when `unlocked < B`;
    selection uses the same test but is advisory. Cheap secrets stay open to small guardians;
    concurrency self-throttles at roughly `float ÷ B`. Supersedes the earlier universal
    `bond_unit × max_tier` selection floor (dropped as an unnecessary barrier to entry —
    simplification A).
17. **Acceptance is first-`num_guardians`-to-confirm** — the first `num_guardians` valid
    confirmations lock bonds and win the slots; later ones rejected without locking;
    deterministic by block height then in-block tx order.
18. **The reward `P` is exact** — fully derived from the creator's dials; no tipping, no
    arbitrary top-ups.
19. **`distance` = `CommitDeadline` → settlement** (≈ `RevealEndBlock`), both fixed at
    Phase 1 so `P` is known and funded up front. `CommitDeadline` is the acceptance timeout,
    the natural fixed reference — not each guardian's actual accept block. The ≤ 200-block
    commit window is negligible against reveal distances.
20. **Early-reveal slashing stays immediate** — bond slashed and reporter paid at once; only
    the reward-pool exclusion waits for settlement. Immediate because ~10-year windows would
    otherwise defer the bounty for years and gut the monitoring incentive.
21. **Commit-timeout → everyone refunded** — if fewer than the full requested `num_guardians`
    confirm by `CommitDeadline` the secret is never committed: accepted bonds returned, pool
    refunded (no fault). `threshold` is not consulted here — it is only the reconstruction
    minimum. `P` is financed on the requested `num_guardians` (the count that wins slots and is
    paid), not on the selection buffer.
22. **Reporter bounty is percentage-based** (50 % of the slashed bond), scaling with the
    creator's parameters rather than a fixed amount.
23. **Cancellation is a paid exit, kept as a key product feature** — permitted at any point
    before `RevealStartBlock`; all bonds returned in full; the pool settles pro-rata by
    distance travelled (`rate × elapsed × bump` per guardian, `elapsed = cancel_block −
    CommitDeadline`, floor 0) with the unearned remainder refunded to the creator. Supersedes
    the earlier full-refund cancellation, which let a creator lock guardian capital for free
    (a griefing vector).
24. **Early-reveal reports are accepted only before `RevealStartBlock`** — once the window
    opens the shares are due to be public, so "early" evidence is moot; late reports are
    rejected. By construction, a bond can never be slashed after release.
25. **"Revealed correctly" is defined** — the revealed share verifies against the HMAC
    commitment recorded at distribution; an invalid share or silence is a no-reveal.
26. **Self-reporting is accepted** — a leaking guardian can recapture the 50 % reporter bounty
    via a second address, so the guaranteed early-reveal loss is the 40 % burn + 10 % creator
    slice + forfeited reward. Inherent to reporter-bounty designs; stated honestly rather than
    engineered around.
27. **Precision and dust** — `bump` is fixed-point with 2 decimal places; integer-division
    dust from any split (pool or bond) is burned.
28. **Early-slashed guardians are excluded from cancellation** (July 2026, found as
    scenario C6 of the test-strategy exercise) — a guardian already slashed for an early
    reveal earns no pro-rata cancellation wage and has no bond to release; its would-be
    wage slice flows to the **creator** via the existing unearned-remainder arithmetic.
    Reporter rejected (already compensated by the report-time bounty); burn rejected (the
    pool is the creator's money — burning it punishes the leak's victim). Consistency:
    each path's forfeited value follows that path's default remainder flow — settlement →
    revealers, cancellation → creator. The self-dealing bound holds (creator-as-reporter
    nets ≤ 60 % of the bond plus its own money back; the 40 % burn always stands).

---

## 10. Design rationale — paths considered and rejected

- **Bond keyed to the reward (`bond = k × per-guardian reward`).** Rejected. Because the
  reward scales with `distance`, so would the bond — and the bond is locked for that same
  `distance` — so a long secret would lock far more collateral for the same per-block fee as
  a short one, for no extra deterrence value. Guardians would shun long secrets. **Fix:** key
  the bond to `bump` only; the reward keeps its `distance` term, the bond does not.
- **Deriving the bond from `rate` via a target `yield` (`bond_unit = rate ÷ yield`).** Tried,
  then dropped — but the *label*, not the coupling, was the problem. "Yield" framed the bond
  as invested capital earning a return, which it is not: the guardian's income is the reward
  `P` (a service fee); the bond is a returnable deposit. **Fix:** keep the derivation but
  rename the constant. `bond_unit = rate × bond_multiplier`, where `bond_multiplier` is an
  honest reference lock-horizon (in blocks — turning the `rate` *flow* into a bond *stock*
  unavoidably needs a time factor), *not* a yield.
- **Making `bond_unit` its own independent base constant.** Considered and dropped. It cleanly
  separated reward from collateral, but revaluing the economy then meant editing *two*
  constants in sync (`rate` and `bond_unit`), which can drift. **Fix:** peg the bond to `rate`
  via `bond_multiplier`, so `rate` is the single master revaluation knob and the ratio stays a
  separate, deliberate choice — same knob count, no drift.
- **Bond as the primary deterrent.** Reframed. With a modest bond, per-secret *slashing* is
  not the main deterrent — the **forfeited reward share** is, and it applies to both
  no-reveal (auto-excluded) and early-reveal (excluded on report). Higher `bump` raises
  both, so security still scales with the single dial.
- **Creator refund-on-failure retained.** Rejected in favour of threshold-independent
  settlement, which pays revealers regardless of reconstruction. This makes revealing a
  dominant strategy and removes the "honest but unpaid" case, at the cost of the creator's
  refund when too few reveal (their protection becomes the slashed no-show bonds).
- **Deferring early-reveal slashing to settlement.** Rejected. With ~10-year windows it
  would defer the reporter bounty for years and gut monitoring; early-reveal slashing and
  bounty stay immediate.
- **Free cancellation (full refund at any time before the reveal window).** Rejected as a
  griefing vector: a creator could lock guardians' bonds for years and cancel at the last
  block, uncompensated, at no cost beyond gas. **Fix:** cancellation stays (it is a key
  product feature) but becomes a *paid exit* — guardians are paid pro-rata for the distance
  travelled and only the unearned remainder is refunded.

---

## 11. Implementation plan and remaining checks

**No open design decisions remain** — the work below is build and verification only. It is
substantial, so it is split into **five sequenced phases**. Each phase is independently
reviewable, leaves the tree in a coherent state, and lands with reproducing tests (the
prove-it-then-fix approach used for Suspected Defect #1). Phases are strictly ordered by
dependency: 0 → 1 → 2 → 3 → 4.

### Open checks (not decisions), routed to their phase

- **[6a]** Extend and reconcile the reveal-distance constants → **Phase 0**.
- **[NEW-2]** No-stranded-bonds invariant → verified in **Phase 4** (tested incrementally
  from Phase 3).
- **[12]** Willingness advertising — **resolved by simplification A.** Protocol-set pricing
  plus reject-on-insufficient-funds means the only "willingness" left is capital availability,
  and the per-secret `unlocked ≥ B` candidacy check (§7.3 step 3) already excludes
  under-funded guardians at selection time. No advertising market for v1.

### Phase 0 — Spec and constants groundwork *(no behaviour change)*

The authority-first prerequisite; nothing economic changes yet.

- Lift [§7](#7-specification-secret-economics-liftable) into `spec.md` as the new
  Slashing / Secret Economics section — spec is the authority the code then follows.
- **[6a]** Raise the reveal cap to `H ≈ 52,560,000 blocks (~10 years)`. The code currently
  has both `MaxRevealStartOffset` (~1 year) and a shorter, overlapping `MaxRevealDelayBlocks`
  (~7 days) that likely caps reveals sooner than intended — extend the former, delete the
  latter, and confirm `H` bounds `reveal_end_block`, not `reveal_start_block`.
- **Exit:** spec merged; constants reconciled; existing suite green.

### Phase 1 — Guardian float and registration *(foundation)*

Replaces the single-stake model with the deposited float. Independently shippable — no
secret touches a bond yet.

- Proto and state for the float: per-guardian `total` / `locked` (`unlocked = total −
  locked`).
- Registration change: pay-and-**burn** `F`; deposit/withdraw with withdrawal capped at
  `unlocked`.
- Derived helper: `bond_unit = rate × bond_multiplier` (computed, never a literal —
  Decision 13).
- No universal float floor (simplification A); the per-secret `unlocked ≥ B` gate lands with
  acceptance in Phase 2.
- **Exit:** guardians register, fund, and withdraw against the float; selection unchanged
  (the per-secret gate lands in Phase 2); slashing/settlement still stubbed to old behaviour.

### Phase 2 — Per-secret pricing and bond escrow

Introduces the two per-secret money movements: the creator's up-front pool and the
guardian's locked bond.

- Up-front reward-pool funding at publication (Phase 1 of the secret): `P = rate × distance
  × num_guardians × bump`, funded from the creator.
- Bond lock at `MsgConfirmShares`: lock `B = bond_unit × bump`; **reject if `unlocked < B`**;
  first-`num_guardians`-to-confirm win slots (Decision 17); per-secret `unlocked ≥ B`
  candidacy filter at selection (Decision 16).
- Per-`(guardian, secret)` bond escrow accounting (the new state that makes settlement
  possible).
- **Exit:** money is correctly *held* on both sides for a live secret; release/slash logic
  still lands in Phase 3.

### Phase 3 — Settlement and slashing rewrite

The behavioural core: one threshold-independent settlement plus the two slash paths.

- Combined settlement in `ProcessExpiredRevealWindows`: return honest bonds → slash
  no-shows (no-reveal bond split, §4.5) → split `P` among revealers (refund creator only if
  none revealed).
- Percentage slashing in both paths, no eviction (Decisions 5–7).
- Immediate early-reveal slash + reporter bounty, with reports rejected once the reveal
  window opens (Decisions 20, 24); reward-pool exclusion deferred to settlement.
- Exit paths: commit-timeout (full refund, Decision 21) and pro-rata cancellation — bonds
  returned, guardians paid `rate × elapsed × bump`, remainder refunded (Decision 23).
- **Exit:** full lifecycle pays out correctly end to end; the Alice worked example
  ([§5.1](#51-a-single-secret-end-to-end)) reproduces in a keeper test.

### Phase 4 — Invariant verification and documentation

- **[NEW-2]** Prove the no-stranded-bonds invariant across **every** path a secret can take
  — success, threshold-fail, cancel, commit-timeout, reveal-timeout — each releasing or
  slashing every locked bond, with no dead-ends and no way for a third party to freeze a
  guardian's float without its consent (same class as Suspected Defect #1).
- Update `PROTOCOL.md` to describe the behaviour *as implemented*, and close Suspected
  Defects #2 and #3.
- **Exit:** invariants tested; docs synchronised; defects closed.
