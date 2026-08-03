# Timeflare Economics — the token map

Every movement of VEIL the protocol can cause, what determines its size, and
why it exists. One page, so that "who pays what, to whom, and when" never has
to be reassembled from six documents and a keeper.

> **This document is derivation and rationale, not protocol law.**
> [spec.md](spec.md) is the single source of truth for behaviour, and
> [`x/secrets/types/constants.go`](../x/secrets/types/constants.go) is the
> source of truth for values — every figure below is computed from those
> constants and cross-referenced back. Where this document and the spec
> disagree, the spec wins and this page is the bug. Consciously accepted costs
> live in [CHAIN_MECHANICS.md](CHAIN_MECHANICS.md#accepted-trade-offs); the funds-flow
> summary table in [operations.md](operations.md#funds-flow-summary) is the same
> map in one-line-per-trigger form.

## 1. The shape of the system

**Fixed supply, no inflation, no treasury.** 1,000,000,000 VEIL exists at
genesis and no mechanism ever mints another unit — the mint module is wired to
zero. There is no community pool (`community_tax = 0`) and no protocol
treasury, so every VEIL a participant spends goes to another participant or is
destroyed. The supply curve only ever points down.

Two independent staking systems sit on top of it:

| | Validators | Guardians |
|---|---|---|
| Secures | consensus | secrets |
| Stake | delegated VEIL (standard SDK staking) | a deposited **float**, drawn on per secret as **bonds** |
| Paid by | transaction fees (90% of the fee pipe) | secret creators, directly |
| Inflation | none — fees only | none — fees only |

Nothing a guardian does affects consensus, and nothing a validator does earns
guardian revenue. They share exactly one thing: the fee pipe.

## 2. The fee pipe, and the only ways VEIL is destroyed

Every fee-like payment in the protocol lands in the **fee collector** and is
split in the next block's `BeginBlock`
([`fee_distribution.go`](../x/secrets/keeper/fee_distribution.go)):

```
90% → validators (via the distribution module)
10% → burned, permanently
```

`FeeValidatorPercent` / `FeeBurnPercent`. Integer-division dust joins the burn
(deflation-biased, per the house rule that split dust is always burned). This
is the **one-pipe ruling** (July 2026): *no validator-bound flow is exempt* —
gas, the creation fee and the guardian entry fee all ride the same split, so
there is exactly one place to reason about validator revenue.

VEIL is destroyed in four places, and nowhere else:

| Sink | Amount |
|---|---|
| The fee split | 10% of every fee that reaches the collector |
| Bond slashing | 40% of every slashed bond, both violation types |
| Key rotation | 100% of the rotation fee |
| Dust | integer-division remainders from the fee split and the pool split |

**Minimum gas price is consensus law, not node etiquette.** The ante chain
rejects any transaction paying below `⌈gas_limit × 1 ÷ 10⌉` uveil — 0.1 uveil
per gas — in both `CheckTx` and `DeliverTx` (`MinGasPriceUveilNum` /
`MinGasPriceUveilDen`). `app.toml`'s `minimum-gas-prices` is a per-node
mempool knob that may only sit *at or above* this floor. Several protocol
prices are denominated in gas against this floor, so they retune themselves if
it ever moves.

**Declared gas is a model, not a flat figure.** Both creator phases scale with
the band — phase 1 walks the candidate set and freezes one bond per selected
guardian, phase 2 carries one share envelope each plus the payload stored once —
so a single number would over-declare small bands and under-declare large ones.
The declared models, fitted to the measured sweep in
[`testdata/vectors/tx_gas.json`](../testdata/vectors/tx_gas.json) with ~25%
headroom and pinned by `gas-model.test.ts`:

| Transaction | Declared gas | Cost at the floor |
|---|---|---|
| `UserRequestGuardians` | `167,000 + 5,400 × max_shares` | 0.0194 VEIL at `max_shares` 5 |
| `UserDistributeShares` | `95,000 + 21,500 × max_shares + 50 × ciphertext_bytes` | 0.0210 VEIL at 5 shares, 152 B |
| `UserCancelSecret` | `120,000 + 80,000 × active_guardians` | 0.0520 VEIL at 5 active |
| `GuardianConfirmShares` | 120,000 (`GuardianAcceptGas`) | 0.0120 VEIL |
| `GuardianRevealShare` | 130,000 (`GuardianRevealGas`) | 0.0130 VEIL |

The two guardian legs are declared at exactly the amount the protocol
reimburses, so spend equals reimbursement to the uveil and no `gas_adjustment`
setting can erode coverage (CHAIN_MECHANICS.md Trade-off §17). Cancel is the one handler
whose cost is dominated by its per-guardian term rather than its base: a
32-guardian cancel needs ~2.07M gas where a 2-guardian one needs ~214k.

## 3. What a creator pays

Five charges, in two groups. The distinction that matters is **escrowed**
(may come back, per the terminal state) versus **spent** (never returns).

| # | Charge | Amount | Destination | Refundable |
|---|---|---|---|---|
| 1a | Pool — **time component** | `rate × distance × max_shares × bump` | guardians | escrowed |
| 1b | Pool — **reveal legs** | `max_shares × F_reveal` | guardians | escrowed |
| 2 | **Accept fees `A`** | `max_shares × F_accept` | guardians | escrowed separately |
| 3 | **Creation fee** | `max(floor, P_time × bps(d) ÷ 10,000)` | fee pipe (90/10) | never |
| 4 | Gas — phase 1 | 0.0278 VEIL at 5 shares (band-scaled) | fee pipe (90/10) | never |
| 5 | Gas — phase 2 | 0.0210 VEIL at 5 shares, 152 B (band- and payload-scaled) | fee pipe (90/10) | never |

Phase 3 costs the creator nothing: guardians send their own acceptances.

**What each is for**

- **1a, the time component** buys the thing being purchased — a share held for
  `distance` blocks. It is also the security anchor: the bond is defined
  against it, so paying more for time raises collateral proportionally.
- **1b + 2, the gas reimbursements** make the guardian whole for the two
  transactions the protocol obliges it to send. Without them a guardian's
  revenue scales with distance while its cost does not, and every short secret
  is worked at a loss — see §7.
- **3, the creation fee** does two jobs: it is the recurring **validator
  security budget**, scaling with the value consensus is protecting, and it
  **prices every selection draw**, which is what closed the
  abandon-and-retry grinding hole. Gas alone is under 0.1 VEIL at any band and
  is independent of what is at stake, so without this fee validators would
  secure a 788-VEIL pool for a few hundredths of a VEIL.
- **4, 5, gas** pays for block space and execution.

**The two escrowed amounts are stored separately, and that is the minimum.**
The reveal leg needs no field of its own: its disposition coincides with the
pool's in every terminal state, so it rides inside `reward_pool`. The accept
fee diverges from the pool in two states — it is *paid* where the pool is
*refunded* (commit expiry) and paid *in full* where the pool accrues
*pro-rata* (cancellation) — so it gets exactly one field, `accept_fees`.
Merging them would force one rule on both: pay it, and the creator funds a
reveal that never happened; refund it, and an acceptor on a stillborn secret
eats its own gas.

## 4. The creation-fee curve

```
bps(d)       = 1,000 − (1,000 − 500) × min(d, 432,000) ÷ 432,000
creation_fee = max(CreationFeeFloor, P_time × bps(d) ÷ 10,000)
```

Truncating integer division throughout. The percentage falls linearly from
**10% at zero distance to 5% at 30 days**, then stays flat:

| distance | bps |
|---|---|
| 0 | 10.00% |
| 14,400 (1 day) | 9.84% |
| 100,800 (7 days) | 8.84% |
| 432,000 (30 days) | 5.00% |
| 5,256,000 (1 year) | 5.00% |

Because `P_time` grows linearly with distance while the rate falls by at most
half, the absolute fee is non-decreasing in distance — no window shape makes a
longer secret cheaper.

**The floor is gas-denominated, deliberately.** `CreationFeeFloor =
MinRequiredFee(600,000 gas)` = **0.06 VEIL** — three reference 200k-gas
transactions at the consensus floor price. At minimal distances even 10% of
`P_time` is far below a single gas fee, so only a flat gas-anchored floor
prices a grinding draw; and being gas-denominated, it tracks any future
gas-floor retune automatically instead of silently becoming cheap.

Where the floor gives way to the curve depends on the band, because `P_time`
scales with `max_shares` while the floor does not:

| band (`max_shares`, bump 1.00) | floor → percent crossover |
|---|---|
| 2 | 600,000 blocks (~42 days) |
| 5 | 143,885 blocks (~10 days) |
| 15 | 42,017 blocks (~2.9 days) |
| 32 | 19,172 blocks (~1.3 days) |

At the narrowest band the floor binds for the first six weeks of distance, and
past `CreationFeeCurveEndBlocks` the crossover is simply where `d ÷ 10` reaches
the floor. No band escapes the curve entirely within the one-year horizon.

**The fee is charged on `P_time` only** — never on the gas reimbursements the
creator also funds. A draw does not become more valuable because a
pass-through was added, and taxing one would route part of every guardian's
reimbursement to validators and the burn.

## 5. What a guardian pays

| Charge | Amount | Destination | When |
|---|---|---|---|
| **Entry fee `F`** | 1,000 VEIL | fee pipe → 900 validators / 100 burned | once, at registration |
| **Float deposit** | operator's choice | module escrow | at registration / top-up |
| **Bond `B`** | `rate × distance × bump × k` | locked *within* the float | per accepted secret |
| Gas — accept | 0.0120 VEIL (= `F_accept`) | fee pipe (90/10) | per accepted secret |
| Gas — reveal | 0.0130 VEIL (= `F_reveal`) | fee pipe (90/10) | per revealed secret |
| **Key rotation fee** | `rate × 14,400` = 0.0144 VEIL | **burned in full** | per rotation |

The **entry fee is sunk** — permanent registration, no exit refund
(CHAIN_MECHANICS.md Trade-off §9). The **float is not spent**: it is working capital,
withdrawable when unlocked. The **bond is not a cost** either — it is
collateral, returned in full to an honest guardian; its cost is the
opportunity cost of capital locked for the secret's duration. The **rotation
fee** is anti-spam pricing for a permanent, forever-reserved history entry,
not economics — it is the only charge burned in its entirety.

## 6. The bond, and the reputation multiplier `k`

```
B (guardian g) = rate × distance × bump × k_g
```

frozen per-guardian at selection and stored on the secret, so a later retune
of any constant cannot re-price a live obligation.

The bond is deliberately **`k` times that guardian's own reward slice**
(`P_time ÷ max_shares = rate × distance × bump`), which makes the
collusion-cost-to-reward ratio a duration-independent constant — at least
`threshold × k` — rather than an accident of hold length. There is no floor
and no ceiling on `B` itself; the formula's own bounds cap it. At the extreme
(one year, bump 10.00, `k` 24.00) the maximum possible bond is **1,261.44
VEIL**; the same secret at the `k` floor bonds **210.24 VEIL**.

**`k` is a live per-guardian reputation value**, hundredths fixed point,
clamped to `[4.00, 24.00]`, starting every new registrant at the floor:

| Event | Effect |
|---|---|
| Any slash (either type) | `k′ = min(24.00, k × 1.26)` |
| Correct on-chain reveal | `k′ = max(4.00, k × 0.963)` |

The climb from floor to ceiling is eight slashes:

```
4.00 → 5.04 → 6.35 → 8.00 → 10.08 → 12.70 → 16.00 → 20.16 → 24.00
```

Recovery is deliberately ~6× slower than the climb: **one slash takes 6 clean
reveals to unwind**, and a full descent from the ceiling takes **47**. `k`
prices a guardian's bonds, never its selection probability — a poor history
makes work more expensive to take on, it does not exclude anyone.

**Concurrency cap**: 100 active bonds per guardian
(`MaxActiveBondsPerGuardian`), checked at selection and re-checked at
acceptance. It bounds a single guardian's blast radius and its capital
exposure independently of float size.

## 7. What a guardian earns, and why it is never negative

Revenue for seeing a secret through:

```
per guardian = A ÷ max_shares  +  P ÷ revealers
             = F_accept  +  (F_reveal + rate × distance × bump)      [all reveal]
```

**The problem this structure solves.** Revenue scales with distance; the cost
of two transactions does not. Under duration-only pricing the break-even sat
at ~22,931 blocks — about 38 hours — and every shorter secret was completed at
a loss. Measured on the devnet: a settled secret paid each guardian **439
uveil against 22,931 uveil of gas**, and because guardians were funding the
fee pipe out of float, validators collected **156% of the creator's entire
outlay**.

Reimbursing the two transactions removes the crossover entirely — net is
positive from the first block and monotonically increasing, at every band size
(pinned by `TestGuardianIsNeverOutOfPocket`).

**Why the creation fee cannot do this job instead.** It is a *percentage of
the wage*; gas is a *constant*. In the percent regime the fee per guardian is
`distance × bump × bps ÷ 10⁴` — 5–10% of that guardian's time wage — so
covering a fixed 25,000 uveil needs a wage of ~500,000 uveil, i.e. ~17 days at
bump 1.00. Per-guardian surplus (+) or shortfall (−) against 25,000 uveil of
gas, if the creation fee had to fund it:

| distance | n=2 | n=5 | n=7 | n=15 | n=32 |
|---|---|---|---|---|---|
| 1 min | +5,000 | −13,000 | −16,429 | −21,000 | −23,125 |
| 1 day | +5,000 | −13,000 | −16,429 | −21,000 | −23,125 |
| 7 days | +5,000 | −13,000 | −16,090 | −16,090 | −16,090 |
| 30 days | +5,000 | −3,400 | −3,400 | −3,400 | −3,400 |
| 1 year | +237,800 | +237,800 | +237,800 | +237,800 | +237,800 |

It covers the cost precisely where guardians do not need it — long secrets,
where the wage already dwarfs gas — and fails where they do. A percentage can
never cover a constant at the short end; that is the same defect in a
different hat.

## 8. Slashing

Both violations are priced as a **percentage of the posted bond**, never a
flat amount, so the penalty scales with what was at stake.

| | Burn | Creator | Returned | Reporter |
|---|---|---|---|---|
| **No-reveal** (at settlement) | 40% | 10% | 50% | — |
| **Early reveal** (on proof, immediate) | 40% | 10% | 0% | 50% |

On a 21.024 VEIL bond that is 8.4096 burned and 2.1024 to the creator in both
cases; the remaining 10.512 either returns to the guardian (no-reveal) or pays
the reporter (early reveal). Integer-division dust from a slash goes to the
**third party** — the reporter for early reveals, the slashed guardian for
no-shows — so conservation is exact by construction.

The asymmetry is the point. A no-show has failed to perform; an early revealer
has destroyed the product, so it forfeits everything and the bounty makes
reporting profitable for anyone holding the evidence. Both step `k` up, making
every future acceptance more expensive.

The creator's 10% is downside protection, not a refund: **there is no
refund-on-failure**. If a secret fails because too few revealed, the pool is
still paid to whoever did reveal.

## 9. Where the VEIL goes at the end

Nothing is disbursed before the secret reaches a terminal state, which keeps
the stored amounts equal to the escrow actually held at every height.

| Terminal state | Each guardian receives | Creator refunded |
|---|---|---|
| **revealed** (≥ 1 revealer) | revealers split `P` equally, one slice of `A` each | unearned `A` slices |
| **failed** — commit expired below `min_shares` | one slice of `A` to each acceptor | `P` in full, plus unearned `A` |
| **failed** — settled, no revealers | nothing | `P` and `A` in full |
| **cancelled** | one slice of `A`, plus the pro-rata wage | the unearned remainder |

Two rules explain every cell:

- **Only performed work is paid.** An explicit rejection earns nothing —
  declining is already free, because it needs no transaction. Slices whose
  work never happened go back to the creator.
- **A failed guardian's share flows to the revealers, not the creator.** This
  is what makes revealing the dominant strategy regardless of what anyone else
  does, and it is why unfilled band slots enlarge the split rather than
  producing a refund (the pool is fixed at `P(max_shares)`).

The difference between the two `failed` rows is **blame**: a guardian that
accepted a secret which never activated did what was asked and the roster
simply did not fill; a no-show is at fault.

**Cancellation is a paid exit**, valid only from `pending` and only before the
reveal window opens. Bonds return in full, the wage accrues pro-rata by
distance travelled, and acceptance is reimbursed whenever the cancellation
lands — so a creator can never lock guardian capital for free, and can never
leave a guardian out of pocket for accepting.

## 10. Worked invoices

`rate = 1 uveil`, gas at the consensus floor, a 152-byte payload ciphertext
(the small-payload reference point in the measured corpus), all figures VEIL.

| Shape | P_time | reveal legs | **P** | **A** | creation fee | gas | **creator total** |
|---|---|---|---|---|---|---|---|
| 1 min (5g, ×1.00) | 0.0001 | 0.0650 | 0.0651 | 0.060 | 0.0600 | 0.0488 | **0.2338** |
| 1 day (5g, ×1.00) | 0.0720 | 0.0650 | 0.1370 | 0.060 | 0.0600 | 0.0488 | **0.3058** |
| 30 days (5g, ×2.00) | 4.3200 | 0.0650 | 4.3850 | 0.060 | 0.2160 | 0.0488 | **4.7098** |
| 1 year (15g, ×10.00) | 788.4000 | 0.1950 | 788.5950 | 0.180 | 39.4200 | 0.0758 | **828.2708** |

At the maximum payload (4,216 B) add ~0.020 VEIL of phase-2 gas to each row.

The composition shifts sharply across the range. On a one-minute secret the
guardians receive 54% of the bill and the fee pipe takes 46% — and nearly all
of the guardians' share is gas reimbursement rather than wage, so the whole
invoice is fixed costs. On a one-year secret the guardians take 95%, the
creation fee 4.8%, and gas rounds to nothing. Short secrets are
disproportionately expensive by design: the work of guarding a secret has a
fixed cost that brevity cannot avoid, and that same property is the
short-secret spam defence (CHAIN_MECHANICS.md Trade-off §15).

## 11. Invariants

Properties the economics guarantee, each mechanically tested rather than
asserted:

- **No inflation, ever.** No code path mints VEIL. Supply is monotonically
  non-increasing.
- **Solvency.** The module's escrow balance equals the sum of every live
  secret's `reward_pool` and `accept_fees` plus every guardian's float, at
  every height (`assertSolvency`, driven by the lifecycle fuzz suite).
- **No stranded funds.** Every escrowed amount is either paid out or refunded
  on every lifecycle path (`no_stranded_bonds_test.go`).
- **A guardian that completes the job is never out of pocket**, at any
  distance the protocol permits (`TestGuardianIsNeverOutOfPocket`).
- **Conservation across slashes.** `burn + creator + remainder == bond`,
  exactly, by construction.
- **No in-flight repricing.** Every obligation is snapshotted on the secret at
  creation and settlement reads only stored values, so a software upgrade
  retuning any constant re-prices *future* secrets only.

## 12. Where the values live, and how they stay honest

Every constant is compile-time immutable in
[`constants.go`](../x/secrets/types/constants.go); every derived quantity is
computed in [`economics.go`](../x/secrets/types/economics.go) and is never
hard-coded at a call site.

**There is no parameter governance.** No `Params` state, no
`MsgUpdateParams`, no vote can move an economic constant. Immutability is a
product feature: a guardian underwriting a year-long secret is underwriting it
against economics that cannot float beneath it. The only retuning path is a
coordinated software upgrade ([upgrades.md](upgrades.md)); `x/gov` exists
solely to coordinate those.

Client-side mirrors of these values (the TypeScript SDK's
`protocol/constants.ts`, and the mobile client through it) are kept from
drifting by the shared vector corpora in
[`testdata/vectors/`](../testdata/vectors/), asserted by both the chain's Go
tests and the clients' test suites.

**Decision records** for each piece:
[DONE_DYNAMIC_BOND_ECONOMICS_PLAN](planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md)
(bonds, `k`, pool),
[DONE_CREATION_FEE_PLAN](planning/done/DONE_CREATION_FEE_PLAN.md) (the curve
and its floor),
[DONE_VALIDATOR_REWARD_ROUTING_PLAN](planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md)
(the one-pipe split),
[DONE_GUARDIAN_COST_RECOVERY_PLAN](planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md)
(the gas reimbursements),
[DONE_PARAMS_GOVERNANCE_DECISION_PLAN](planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md)
(immutability).
