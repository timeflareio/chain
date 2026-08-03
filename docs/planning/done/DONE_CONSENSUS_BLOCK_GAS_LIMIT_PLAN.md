# Consensus block gas limit

**Priority**: P1 — a genesis consensus parameter with no current bound; it is
the enabler behind one client-side and one daemon-side defect already recorded
in two sweeps, and genesis is the cheapest moment to set it.
**Status**: done (1 August 2026 — [PR #136](https://github.com/leedavis81/timeflare/pull/136) merged, CI green). The measurement leg ran and the owner ruled the same day: **75,000,000, set at
genesis only** (see "The ruled value").
**Origin**: surfaced twice independently — the mobile pre-testnet sweep
([DONE_MOBILE_PRE_TESTNET_SWEEP.md](DONE_MOBILE_PRE_TESTNET_SWEEP.md)
finding 31, where it is what makes a permanent discovery wedge affordable) and
the guardian pre-testnet sweep
([guardian/PENDING_GUARDIAN_PRE_TESTNET_SWEEP.md](guardian/PENDING_GUARDIAN_PRE_TESTNET_SWEEP.md)
finding 20, where it is why a forged gas simulation is bounded only by the
account balance — now broken out as
[guardian/PRIORITY_GUARDIAN_TX_PATH_RESILIENCE_PLAN.md](guardian/PRIORITY_GUARDIAN_TX_PATH_RESILIENCE_PLAN.md)
§3, which owns the *daemon-side* ceiling). Neither sweep nor that plan owns
this: they are client and daemon documents, and this is a chain-side consensus
parameter.
**Components**: `devnet/chain/apply-genesis-economics.sh` (the shared
genesis-knobs script both the native `setup-chain.sh` and compose
`docker/init-chain.sh` paths run — one edit covers both, grep-verified),
`docs/spec.md` ("Network Configuration" / "Economic Constants"),
`docs/CHAIN_MECHANICS.md` (security observation §1 asserts the unlimited state;
narrowed to its still-open EndBlock half), `testdata/vectors/tx_gas.json`
(its prose asserted the unlimited state), and the e2e suites that must keep
passing under a real limit. No `app/` upgrade handler: the limit is set at
genesis, not retrofitted onto a live chain.

---

## The issue

Timeflare's genesis never sets a consensus block gas limit, so CometBFT's
default of `-1` — **unlimited** — applies. Verified: no `max_gas` assignment
exists anywhere in the repository outside generated OpenAPI schema and one
prose reference; `devnet/chain/setup-chain.sh` authors genesis and sets
economics, denominations and the mempool price knob but never touches
`consensus.params.block`. The consequence is already written down as a known
fact in the test corpus:

> "A 32-guardian cancel needs ~2.07M gas (0.207 VEIL at the floor price);
> **block max_gas is unlimited on this chain**, so it is includable."
> — `testdata/vectors/tx_gas.json:8`

That sentence is load-bearing in the corpus's own reasoning: the largest
legitimate transaction the protocol can produce is includable *because*
nothing bounds a block. Setting a limit is therefore not a free change — it
interacts with the protocol's own worst-case transaction sizes, which is
exactly why it deserves a plan rather than a one-line genesis edit.

`max_gas` is the only per-block execution bound the chain has. `MinGasPrice`
(`x/secrets/types/constants.go:364-365`, consensus-enforced at 0.1 uveil/gas)
prices gas but does not cap how much of it one block may contain, and
`max_bytes` bounds serialised size, which is a poor proxy: the protocol's
expensive handlers are expensive in *execution*, not bytes (a ~180k-gas
`MsgUserRequestGuardians` is a few hundred bytes on the wire).

## Why it matters

**1. No bound on block execution time.** With `max_gas = -1`, the only limit
on the work one block imposes on every validator is what proposers happen to
include and what the mempool happens to hold. Block time is a protocol-wide
assumption — the economics are denominated in blocks throughout
(`rate` per guardian per block, `distance`, every window and deadline), and
`docs/spec.md` "Network Configuration" states ~6 seconds as the expected
interval. Nothing enforces that a block's contents can be executed inside it.

**2. It makes a permanent discovery wedge cheap** (mobile sweep finding 31).
Recipient discovery pages the hint feed at 1,000 records; the SDK's page-boundary
handling rewinds its cursor to the last record's creation height, so if
≥ 1,000 hints share a single height the cursor cannot advance and discovery
wedges for every recipient whose cursor sits behind that block. The client-side
fix (paginate by `pagination.key`) is owned by the mobile sweep and is being
implemented — but the *reachability* of a 1,000-creation block is this plan's
concern. Costed at the ruled constants:

| Component | Per creation | × 1,000 | Recoverable? |
|---|---|---|---|
| Creation fee floor (`CreationFeeFloorGas` 600,000 × 0.1 uveil) | 0.06 VEIL | 60 VEIL | **No** — never enters escrow |
| Phase-1 gas (`167,000 + 5,400 × max_shares`, at `max_shares = 2`) | ~0.0178 VEIL | ~17.8 VEIL | **No** |
| Reward pool `P` + accept fees `A` (minimum shape) | ~0.05 VEIL | ~50 VEIL | Yes — refunded in full at commit-timeout |

So roughly **78 VEIL sunk** buys a permanent, network-wide wedge of every
scanning client. A block gas limit does not eliminate the attack — the
attacker spreads creations across blocks instead — but it removes the
*single-height concentration* that the wedge specifically requires, turning a
permanent break into ordinary spam that the fee floor already prices.

**3. It is why an unbounded gas simulation is unbounded** (guardian sweep
finding 20). The guardian declares `max(simulated, reimbursed)` gas with no
ceiling; a hostile or MITM'd node returning a huge `gas_used` gets it signed
as the declared limit, and the ante handler deducts the declared fee up front.
That sweep records the only bound as the account balance — and the reason no
consensus bound applies is this parameter. A `max_gas` would cap the damage of
a single forged simulation at the limit rather than the balance, independently
of whether the daemon-side ceiling lands.

## What this plan does not solve

- It does not close the discovery wedge on its own; the SDK pagination fix is
  the actual remedy and is owned by the mobile sweep. This plan removes the
  cheap concentration vector.
- It does not remove the need for the guardian's own gas ceiling (guardian
  sweep finding 20). Defence in depth: the daemon should not sign an absurd
  limit even where consensus would reject the block.
- It says nothing about mempool policy (`minimum-gas-prices` in `app.toml`
  remains the per-node knob, at or above the consensus floor) or about
  `max_bytes`.
- It does not introduce economic parameter governance. `max_gas` is a
  CometBFT consensus parameter, not one of the protocol's immutable economic
  constants (CHAIN_MECHANICS.md Trade-off §8, Position A), so setting it neither
  contradicts the no-governance stance nor creates a precedent for moving
  `rate`, `F` or the bond formula.

## Constraints any value must satisfy

1. **The largest legitimate single transaction must remain includable, with
   headroom.** The known ceiling is a 32-guardian cancel at ~2.07M gas
   (`testdata/vectors/tx_gas.json`). A limit below that bricks a legitimate
   protocol operation permanently — the worst possible outcome, since a
   creator who cannot cancel a 32-guardian secret has no exit.
2. **A block must hold a realistic burst of concurrent activity.** Guardian
   accept and reveal transactions arrive in clusters (one per selected
   guardian per secret, up to 32), at 120,000/130,000 declared gas each. A
   secret activating with a full roster is ~4M gas of confirmations that
   should not be forced to straddle many blocks — the commit window is only
   50 blocks (`CommitTimeoutBlocks`) and reveal windows have hard ends, so
   artificial queuing converts a throughput limit into missed deadlines and
   no-reveal slashes.
3. **The limit must be justifiable as executable within the block interval**
   on the reference validator hardware, or it is decoration.
4. **The e2e suites must pass under it** — including `e2e-scenarios` and the
   mobile lifecycle suites, which exercise multi-guardian settlement.

## Measurement (1 August 2026)

One devnet run filling blocks to the candidate limit with real protocol
transactions, so the chosen figure is defended by an observation rather than
by another chain's convention.

**Method.** Fresh native devnet: single validator on an Apple-Silicon
workstation, 2-second blocks (`timeout_commit = 2s`), 36 registered guardians
with the daemons stopped so the traffic under test is purely creator-side.
Load was real `MsgUserRequestGuardians` transactions (2-share shape,
~142k gas consumed each), pre-signed offline with explicit sequences and
broadcast into a single block window in chunked RPC batches. Gas was declared
tightly (160,000 against ~142k consumed) because `max_gas` bounds *declared*
gas — a tight declaration makes the measured blocks emulate the worst case
the limit actually admits, a block whose declared gas is nearly all consumed.
Execution time was read from block-header timestamp deltas against the
empty-block baseline: 2,031 ms median over 940 empty blocks, noise ≈ ±10 ms.
A block's execution cost appears in the *following* block's interval, and
that is where it was read.

**Results.** Zero failed transactions across the run.

| Block contents | Declared gas | Consumed gas | Next-block interval |
|---|---|---|---|
| 70 creations | 11.2M | 9.9M | baseline (no measurable inflation) |
| 210 creations across 3 blocks | 11.5M/12.3M/9.8M | ~29.7M total | baseline |
| **470 creations, one block** | **75.2M** | **66.5M** | **2,028 ms — below the ±10 ms noise floor** |
| 400 bank sends, one block | 36.0M | 27.0M | baseline |

The headline observation: a single block carrying **75.2M declared /
66.5M consumed gas of real creation traffic executed in under 10 ms** —
implying at least ~6.5 Ggas/s of execution throughput on this hardware, four
orders of magnitude inside the ~6-second production interval. The 400-send
bank block cross-checks that this is not a quirk of one handler's gas
pricing. Two subsidiary observations:

- **The commit-expiry sweep is also cheap at this scale.** The 470 secrets
  reserved in the 75M block all hit their commit deadline 50 blocks later
  (`CommitTimeoutBlocks`), and the EndBlock sweep that failed and refunded
  all 470 in one block was likewise below the noise floor. EndBlock work is
  not gas-metered — `max_gas` cannot bound it — so it is worth knowing the
  protocol's own worst concentration of it costs nothing measurable today.
- **The unlimited default is very real.** Nothing pushed back on 470
  transactions in one block; the mempool accepted the batch in ~1 second and
  the proposer included it whole.

**Reading.** Constraint 3 is satisfied at 75M with enormous margin: even
derating the workstation figure by 100× for conservative reference validator
hardware, a full 75M block executes in well under one second. Execution time
therefore does not discriminate between the candidate values — the ruling
rests on the economic bounds alone (single-transaction ceiling, burst
headroom, the forged-simulation cap). The tighter 30M alternative buys no
execution-time safety that 75M lacks, while giving up burst headroom.

**Caveat.** Developer workstation, single validator, warm state, no state
sync or disk pressure — the figure should be sanity-checked on reference
validator hardware at testnet, but the conclusion survives orders-of-magnitude
derating.

## The ruled value

**`consensus.params.block.max_gas = 75,000,000`, set at genesis** (owner
ruling, 1 August 2026).

The value is roughly the Cosmos Hub's long-standing choice and ~36× the
largest known legitimate transaction, which satisfies constraints 1 and 2
with generous headroom while still bounding a block to a few hundred protocol
transactions rather than unlimited. It caps a single forged-simulation drain
at 7.5 VEIL at the floor price (75M × 0.1 uveil) instead of an entire
balance. The measurement above shows a 75M block of real protocol traffic
executes in under 10 ms on a development workstation, so constraint 3 holds
with orders of magnitude to spare. The tighter 30,000,000 alternative
(≈14× the cancel ceiling, ~230 accept transactions per block) is strictly
dominated for this protocol: the measurement shows it buys no execution-time
safety, and it starts to bind on legitimate bursts.

Genesis is the whole of the delivery: setting the parameter there costs
nothing and needs no migration, where retrofitting a live chain does. Any
later change is an ordinary consensus-params update through the existing
`x/gov`-coordinated upgrade machinery (the same path `docs/upgrades.md`
already describes) — if testnet measurement on reference validator hardware
shows the value wrong, that path exists.

## Implementation phases

Ordered spec-first per the planning rules, and deliberately small — the
measurement leg was the only substantial work.

1. **Measure** — done, 1 August 2026; method, results and reading are
   recorded in the Measurement section above. One deliberate deviation from
   the original intent: the load was creation traffic
   (`MsgUserRequestGuardians`) plus a bank-send cross-check rather than
   `MsgGuardianConfirmShares`, because a confirm flood requires the full
   distribute phase per secret and the measured quantity — execution time
   per unit of consumed gas across the module's keeper machinery — does not
   depend on which handler burns the gas (the bank tier exists to check
   exactly that).
2. **Spec** — done, 1 August 2026. `docs/spec.md` "Network Configuration"
   states the block gas limit alongside the block time, with the rationale
   (execution bound, not a price); "Economic Constants" notes it is a
   consensus parameter and explicitly *not* one of the immutable economic
   constants. The cross-component sweep surfaced one further edge:
   `docs/CHAIN_MECHANICS.md` security observation §1 reasoned from the unlimited
   state — narrowed to its still-open half (EndBlock settlement work is
   unmetered and uncapped, which `max_gas` cannot bound; this plan never
   claimed to close it).
3. **Genesis** — done, 1 August 2026. `consensus.params.block.max_gas =
   75000000` set in `devnet/chain/apply-genesis-economics.sh` rather than
   inline in `setup-chain.sh`: the shared script is the declared single home
   for genesis knobs, and both the native and compose genesis paths run it
   (grep-verified, per the cross-component rule), so one edit covers both.
4. **Corpus prose** — done, 1 August 2026. `testdata/vectors/tx_gas.json`'s
   `cancel_measurement` note now reasons from the 75M limit (~36× headroom
   on the 32-guardian cancel). The vector *values* did not change; the
   justification did.
5. **Verify** — done, 1 August 2026, on a fresh devnet built from the
   execution branch with the limit live (confirmed via `/consensus_params`):
   `make e2e` passed; `make e2e-scenarios` passed (53 assertions, 0 failed);
   the mobile lifecycle suites passed; a deliberate 80M-gas transaction was
   rejected at CheckTx (`tx gas limit 80000000 exceeds block max gas
   75000000`, code 41) rather than silently truncated; and an exactly-75M
   transaction was accepted and committed, so the boundary itself is
   includable.

## Cross-links

- [DONE_MOBILE_PRE_TESTNET_SWEEP.md](DONE_MOBILE_PRE_TESTNET_SWEEP.md)
  finding 31 — the discovery wedge; owns the SDK pagination remedy, and cites
  this plan as the owner of the concentration vector.
- [guardian/PRIORITY_GUARDIAN_TX_PATH_RESILIENCE_PLAN.md](guardian/PRIORITY_GUARDIAN_TX_PATH_RESILIENCE_PLAN.md)
  §3 — the unbounded *declared* gas limit, and the daemon-side ceiling that
  refuses to sign an absurd simulated figure. That plan reasons from the
  absence of a consensus `max_gas`; this one supplies it. The two are
  complementary and neither substitutes for the other: a consensus limit caps
  the damage of a simulation the daemon wrongly trusts, and a daemon ceiling
  holds even on a network whose limit is generous.
- [guardian/PENDING_GUARDIAN_TRANSPORT_SECURITY_PLAN.md](guardian/PENDING_GUARDIAN_TRANSPORT_SECURITY_PLAN.md)
  — the plaintext channel that makes a forged simulation reachable in the
  first place.
- [CHAIN_MECHANICS.md](../../CHAIN_MECHANICS.md) §8 (fixed prices, zero
  governance) and §17 (declared gas exceeds consumed) — the stances this plan
  must not contradict.
