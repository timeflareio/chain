# Recipient Rebate — Plan

*When a secret is revealed, the protocol credits its recipient a rebate on what
the creator irrecoverably spent to send it. The recipient collects it by proving
recipiency against the detection hint already on chain. This is how a new wallet
tops up a wallet on every network, once it can transact at all.*

> A wallet needs one incoming payment before it can send anything — Cosmos writes
> an account record only on first receipt — so the rebate is not itself a cold
> start. Getting a new address its first VEIL is owned by
> [DONE_WALLET_BOOTSTRAPPING_PLAN.md](DONE_WALLET_BOOTSTRAPPING_PLAN.md).

> **Status: done — 30 July 2026**, branch `worktree-recipient-rebate`, PR #117.
> Spec, constants, proto, keeper, **commit–reveal collection**, genesis
> restructure, devnet tooling, AutoCLI, cross-implementation primitives
> (Go/Rust/mobile pinned to one vector), unit tests, the devnet e2e scenario and
> the mobile surface are landed, with the collection window ruled explicit
> (three months, §6). `make verify`, the full Go suite, `cargo test` and the
> 340-test mobile suite are green.
>
> **Proven end to end on a live devnet (30 July 2026)**: a band-7:9 secret
> settled `revealed`, its recipient was credited **95,067 uveil** — exactly 30%
> of the 316,890 uveil irrecoverable spend — an uncommitted reveal was refused
> with "commit first, then reveal in a later block", and a commit followed a
> block later by a reveal paid the collector while the **keyless pool's balance
> fell by exactly 95,067**. A second collection was refused. The run also caught
> a real defect first (§10).
>
> **Priority**: P2 — the only funding path for a new wallet on a public network,
> and the mobile app's Funding screen currently has a dead CTA.
>
> **Origin**: owner design session, 29 July 2026. Rulings: one mechanism for
> testnets and mainnet; distribution **piggybacks the secret flow** rather than
> gating strangers at a faucet; the **recipient** is the beneficiary and the
> creator's spend is the authorisation; the drain is **bounded above** by a
> rate proportional to the remaining pool (the actual pace is unknowable — it
> depends on how many secrets settle, which may be none); and the rebate is
> **protocol state, not a hosted service** — no indexer, no signer, no custody,
> and no key over the pool.
>
> **Components**: `proto/timeflare/secrets/v1/` (new Msg, secret fields) ·
> `x/secrets/types` (constants, message + structural validation) ·
> `app/` (keyless rebate module account) · `devnet/chain/` genesis setup and
> `devnet/fund.sh` (two pools replacing three) ·
> `x/secrets/keeper` (settlement crediting, collection handler, pool
> accounting, pruning) · `x/secrets/module/autocli.go` (+ `TestTxCommandParity`)
> · `docs/spec.md` (new section, economics tables) · `mobile-client/app`
> (surface the rebate on a revealed inbound secret) · `devnet/` (e2e scenario) ·
> docs sweep. Replaces the faucet item in
> [`../automated/TESTNET_LAUNCH_PLAN.md`](../automated/TESTNET_LAUNCH_PLAN.md) §2 and
> discharges the Wave-4 dependency in
> [`../client-app/PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md`](../client-app/PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md)
> §5.1. **No new component** — this extends `x/secrets`.

## 1. Problem

Nothing funds a new wallet beyond genesis. `devnet/fund.sh` is developer
tooling; the app's Funding screen raises "Faucet not available yet". A faucet
cannot serve mainnet, because it decides who deserves funds from evidence a
stranger supplies — captcha scores, addresses, IP reputation — all purchasable,
so its exposure is unbounded once the token has value.

## 2. Mechanism

**Trigger.** At `reveal_end_block + 1` the settlement queue already drains every
secret due at that height. For each that reaches `revealed`, the keeper credits
a rebate:

```
rebate = min(0.3 × S, allowance ÷ n)
```

- `S` — the creator's **irrecoverable** spend on that secret: the reward pool
  `P` and the accept slices actually paid to revealing guardians, both known in
  this block because this is where they are paid. The creation fee is
  **excluded**: it is not carried on the secret record, and adding a field for
  it would cost state on every secret to raise `S` by a few percent.
  Understating `S` only widens the margin that makes farming a loss.
- `0.3` — the rebate ratio, the ceiling that makes farming a loss (§3).
- `allowance` — the accrued distribution allowance (§4), shared equally by the
  `n` secrets settling at this height and decremented by what they take.

`revealed` is the trigger because it is the first point the spend cannot be
recovered: cancelling one block after activation refunds essentially the whole
pool, so crediting at creation or activation would be farmable for the price of
a creation fee.

**Claim.** `MsgClaimRebate{secret_id, z}` — one message per secret; no
batching in v1. The keeper requires
`SHA256("timeflare/detect/v1" ‖ z)[:8] == secret.detection_hint.tag`, marks the
rebate claimed, and pays **the signer**. Only the holder of the recipient's
private key can produce `z = X25519(a, R)` for the stored ephemeral key `R`, so
recipiency is proved without an account, an identity, or a credential — and the
transaction signature is itself the proof that the destination is controlled by
the collector.

`ValidateBasic` checks structure only (`z` is 32 bytes, `secret_id` well
formed); the hint arithmetic is keeper work, since the types module carries no
crypto.

**Unclaimed rebates** expire with the secret: terminal secrets are pruned to
tombstones after the retention window, and pruning returns the unclaimed
reservation to the pool, where it is available for distribution again. The
collection window is therefore the retention window — no separate expiry to reason about.

## 3. Why it cannot be farmed

Self-dealing — send yourself a secret, wait out the time-lock, collect — costs `S`
and returns at most `0.3 × S`: a loss of `0.7 × S` **at any token price**, which
is what lets one mechanism serve testnets and mainnet. The only recapture path
is being a selected guardian, so breaking even would mean capturing ~70% of the
spend back through sortition-allocated assignments — bought with entry fees,
floats and slashing exposure, and far beyond any realistic share of the
guardian set.

Consequence: a rebate is smaller than the invoice that produced it, so it funds
a *minimal* first secret — short window, small share band, `bump` at its floor —
not a copy of the one received.

## 4. Bounding the drain

**Genesis is restructured into two pools.** The three key-controlled incentive
pools collapse — validator and guardian launch distribution were never
meaningfully different, and both are bootstrapping by another name:

| Pool | Amount | Control | Purpose |
|---|---|---|---|
| **Rebate pool** | 700,000,000 (70%) | **keyless module account** | Spendable only by the formula below |
| **Bootstrapping pool** | 300,000,000 (30%) | key-controlled | Launch funding for any of the three actors — validators, guardians, users |

The rebate pool has no key, so it can only ever be spent by the formula, and it
can never be topped up. The bootstrapping pool is where every hand-made grant
comes from, which is also what makes cold start work (§9).

**Accrual.** The distribution allowance fills at

```
pool ÷ 50,000,000  per block   (14.00 VEIL/block at 700M)
```

read from the live module balance, so the rate falls as the pool does. Fully
claimed, that is **10% of the remaining pool per year** — the worst case the
mechanism can produce, at 5,256,000 blocks a year:

| Year | Pool | Accrual | Per day | Sustains (at 300/rebate) |
|---|---|---|---|---|
| 0 | 700.0M | 14.00/blk | 201,600 | 672 rebates/day |
| 1 | 630.0M | 12.60/blk | 181,440 | 605/day |
| 5 | 413.3M | 8.27/blk | 119,043 | 397/day |
| 10 | 244.1M | 4.88/blk | 70,294 | 234/day |
| 20 | 85.1M | 1.70/blk | 24,510 | 82/day |
| 30 | 29.7M | 0.59/blk | 8,546 | 28/day |

Because the rate is proportional to what remains, the pool decays geometrically
and never empties: distribution slows asymptotically rather than stopping at a
cliff, and nothing is permanently stranded.

**Accumulation, and why it is needed.** Unclaimed allowance accumulates, capped
at `burst` — one day's accrual (14,400 blocks; 201,600 VEIL at 700M). Without
accumulation the
allowance for a lone settling secret would be a single block's 14 VEIL, far
below a useful rebate, and the mechanism would fail at its purpose. With it, a
recipient receives the full `0.3 × S` at any realistic volume, the accrual binds
only above roughly 670 settlements a day, and the `burst` cap absorbs an
ordinary cluster of simultaneous settlements without letting an idle month
become a drainable lump.

**The dust floor.** A share below **0.05 VEIL** (`50,000 uveil`) is not credited
at all — five times the ~0.01 VEIL gas of the collecting transaction. Sized
against what the protocol actually charges rather than a round number: at
`RatePerGuardianBlock`, a month-long five-share secret's pool is ~2.16 VEIL, so
its rebate is ~0.65 VEIL, and a 1 VEIL floor would have excluded every secret
shorter than about four months. This floor admits any secret whose irrecoverable
spend exceeds ~0.17 VEIL — about eleven days at five shares — and leaves
collection comfortably worth doing.

**Worst case is worst case.** Full claim means every block pays out — 14,400
settled secrets a day, each having paid a full invoice. At realistic volumes the
`0.3 × S` term binds instead, and most blocks settle nothing, so the pool
declines far more slowly than 10% a year.

## 5. Protocol surface

A consensus change, approved 29 July 2026, and sequenced spec-first per
CLAUDE.md.

- **proto**: `MsgClaimRebate` (+ response); on the secret record, the rebate
  amount and claimed flag.
- **`x/secrets/types`**: four compile-time constants — accrual divisor
  (`50,000,000`), rebate ratio (`30%`), `burst` (`14,400` blocks of accrual) and
  `dust` (`50,000 uveil`). There is no parameter governance on this chain, so
  they are immutable and retunable only by coordinated software upgrade — and
  shared by every network, since it is one binary.
- **genesis / `app`**: the three incentive pools become two — a keyless rebate
  module account holding 700M and a key-controlled bootstrapping pool holding
  300M. Touches `docs/spec.md`'s Genesis Pool Allocations table, module-account
  permissions, `devnet/chain/` genesis generation, and `devnet/fund.sh`, whose
  `user | guardian | validator` pool argument collapses to the single
  bootstrapping key.
- **keeper**: allowance accrual from the module balance; credit-at-settlement
  inside the existing due-height drain; collection handler; reservation accounting;
  release on prune. Failure of any payout
  fails the transaction atomically, as elsewhere in settlement.
- **AutoCLI**: an `RpcCommandOptions` entry, so collection gets a CLI verb by
  construction (`TestTxCommandParity` enforces it).
- **`docs/spec.md`**: a new section under the economics model, plus the worked
  examples and flow-of-funds tables.

## 6. Risks

- **Recipient-privacy regression (accepted, owner-ruled).** Publishing `z`
  proves recipiency to everyone, permanently: it links the claiming address to
  that secret, and a wallet claiming on several secrets links those secrets to
  one another. This cuts against the detection-hint property that no observer
  can enumerate a recipient's secrets or link two to the same recipient
  (`docs/spec.md` "Recipient Discovery"). It is opt-in — claiming is voluntary —
  and a wallet can collect to a fresh address, though consolidation re-links.
  Closing it properly needs a zero-knowledge proof of hint knowledge, which is
  out of proportion here. The spec section must state this plainly so no
  integrator assumes claiming is private.
- **Clustered settlements dilute genuine recipients.** Reveal windows are
  creator-chosen and coincide on round dates, so `n` is lumpy with no adversary
  involved. Sizing `D` to cover an ordinary cluster at full `r × S` is the
  mitigation, and it is a real tension: more cluster headroom means a looser
  worst-case bound.
- **No-discovery secrets dilute the block and their share evaporates.** Random
  hint bytes are indistinguishable from real hints by design, so such secrets
  count in `n` and nobody can collect their share.
- **Dilution can be aimed.** `reveal_end_block` is public, so an adversary can
  settle secrets at a target's height to shrink their rebate — at the cost of a
  full settled invoice per secret, orders of magnitude more than the rebate
  suppressed. A nuisance, not a strategy.
- **Front-running: closed by commit–reveal** (owner-ruled, 30 July 2026). `z`
  is public once revealed, and the rebate pays the signer, so collection is two
  transactions: `MsgRecipientCommitRebate` publishes
  `SHA256("timeflare/rebate-commit/v1" ‖ z ‖ collector address bytes)`, and the
  reveal must land in a **strictly later block** and reproduce that commitment.
  An observer who lifts `z` from the mempool has no commitment for it and cannot
  backdate one. Pinned by tests that assert the stolen-proof attack fails, that a
  same-block commit cannot rescue it, and that a commitment binds to exactly one
  address and one proof. Residual cost: one extra transaction and one block of
  latency per collection.
- **Collection expires after three months** (owner-ruled, 30 July 2026):
  `RebateCollectionBlocks` = 1,296,000 blocks from settlement, explicit rather
  than inherited from pruning. At the deadline the rebate is voided and its
  reservation returns to the pool, so unclaimed adoption funding funds the next
  newcomer rather than sitting reserved for another three months. A recipient who
  never scans loses it — which is the cost of a mechanism nobody has to
  administer.

  The window **must always close before pruning** (`RetentionBlocks` =
  2,592,000, ~6 months): the proof is verified against the detection hint, and
  pruning takes the hint with it. Enforced twice — a test asserting the
  constants' relationship, and a clamp to the live retention value so a devnet
  running a short override cannot promise a window it cannot honour.
- **Keyless is irreversible.** No key means no top-up, no correction, and no
  recovery: 65% of supply is committed to this formula for good, and getting the
  accrual divisor, ratio or `burst` wrong is a coordinated software upgrade to
  fix. In-flight secrets keep the values they were credited with.
- **The bootstrapping pool is now the only key-controlled supply**, so every
  launch grant to every actor competes for the same 300M. That is a budgeting
  problem rather than a protocol one, but it is a real narrowing: there is no
  second key to fall back on.
- **Legal.** Distributing a market-valued token, even as a usage rebate, is a
  distribution event with UK/EU financial-promotion and MiCA-type exposure, and
  giving value implies sanctions screening. Needs an opinion before mainnet, not
  before testnet.

## 7. Work items

Landed on `worktree-recipient-rebate` (30 July 2026) unless marked otherwise.

1. ✅ **Spec** — `docs/spec.md` "Recipient Rebate", the rewritten Genesis Pool
   Allocations, and the privacy warning §6 requires.
2. ✅ **Genesis restructure** — keyless rebate module account (no permissions,
   blocked from receiving), one bootstrapping key replacing three incentive
   keys, across `devnet/chain/`, `devnet/docker/`, `devnet/fund.sh`,
   `devnet/guardians.sh`, `devnet/users/`, `devnet/e2e-scenarios.sh`. The pool
   address is a literal in the genesis scripts, guarded by
   `app/rebate_pool_test.go`.
3. ✅ **Proto + types** — `MsgRecipientCollectRebate`, `rebate_amount` /
   `rebate_collected` on the secret, `RebateState` in genesis, four constants,
   three errors, structural `ValidateBasic`.
4. ✅ **Keeper** — lazy accrual from the pool balance, credit-at-settlement in
   the existing due-height drain, reservation accounting, the collect handler,
   release on prune, genesis import/export and validation.
5. ✅ **Crypto** — `crypto.DetectionTag` / `DetectionTagMatches`, the Go home
   the Rust implementation already referenced, pinned to
   `testdata/vectors/detection_hint.json`.
6. ✅ **Tests** — arithmetic table tests, keeper tests (accrual, division, dust,
   determinism, reservation, refusals), an end-to-end pass through settlement
   and collection, and four genesis guard tests. `make verify` green.
7. ✅ **Client primitives** — `recipiency_proof` and `rebate_commitment` in
   `rust/` (WASM), the SDK's `CryptoBackend` and `TimeflareTxClient`
   (`commitRebate` / `collectRebate`), and `mobile-client/app/src/state/rebate.ts`
   — all four implementations pinned to
   `testdata/vectors/rebate_commitment.json`.
8. ✅ **Mobile** — the rebate on a revealed inbound secret with its two-tap
   commit→reveal flow (`RebateCard`), rebate fields carried through the inbox
   record, and the Funding screen's dead faucet CTA replaced with honest copy.
   The card counts the window down ("Expires in 3 days" / "1 hour" /
   "12 minutes", fully spelled — a deadline that costs money is not the place
   for "in 3 h"), and past the deadline shows the rebate as expired rather than
   offering a button the chain would refuse. Vendored SDK repacked and the
   lockfile integrity synced.
9. ✅ **Devnet e2e** — scenario S10 in `devnet/e2e-scenarios.sh`. Run against a
   live 1s-block devnet (30 July 2026): the rest of the suite passed 48
   assertions on the two-pool genesis, and the keyless pool held exactly
   700,000,000 VEIL with supply at 999,997,600 (the 2,400 difference being the
   10% burn on 24 guardian entry fees).

   The scenario carries two shapes, because **the dust floor is calibrated for
   production economics**: at devnet scales (1s blocks, minute-long windows) 30%
   of a secret's irrecoverable spend lands under the 0.05 VEIL floor, so the
   default shape asserts the suppression — correct behaviour, worth pinning —
   and `REBATE_COLLECTION_DRILL=1` runs a band-7:9, bump-10×, ~1,020-block
   secret whose 30% clears the floor for every allowed activation outcome, then
   asserts the full path: credit ≤ the ratio ceiling, an uncommitted reveal
   collecting nothing, commit, collect, and the keyless pool paying exactly the
   rebate.

   A first attempt used a 15-wide **zero-width** band to reach the floor with a
   short window; it failed to activate, because every selected guardian must
   then accept before the deadline and the suite deliberately damages guardians
   before this point (S1 kills a daemon, S3 slashes another). Recorded because
   the shortcut is tempting and the failure mode is not obvious.

   **What the run caught that unit tests did not**: the allowance accrued
   nothing on its first touch, so the FIRST eligible secret on every network was
   credited zero. The keeper end-to-end test had primed the accrual clock to give
   the allowance somewhere to accrue from, and that priming was exactly the
   condition hiding the defect. Fixed by accruing from genesis — safe because the
   burst cap bounds any gap — and the test now runs the virgin-chain path.

   S10 additionally asserts the *reason* an uncommitted reveal is refused (the
   chain's "commit first" text), not merely that nothing was collected: a
   transaction failing for an unrelated cause would otherwise masquerade as the
   front-running defence working, which is precisely the false positive the
   throwaway drill script produced before it was tightened.
10. ✅ **Pre-existing e2e fixes, incidental to this plan** — the scenario
    manifest never carried `acceptFees` (so S2's accept-fee assertion aborted
    the suite under `set -u`, on `main` as well as here), and
    `scenario-create.js` had no way to vary the guardian band. Both fixed:
    without the first the suite cannot reach the rebate scenario at all, and
    without the second a devnet rebate can only ever be dust-suppressed.
11. ✅ **Docs sweep** — `TESTING_COMMANDS.md`, `devnet/README.md`, the testnet
   plan's superseded faucet item, the marketing site's superseded claim service.

## 8. Open questions

1. **Accumulation** (§4) — confirm the allowance accrues rather than being a
   standalone per-block figure. *Recommendation*: it must, or a lone recipient's
   rebate is 13 VEIL. The 10% worst-case bound is unaffected either way, because
   nothing can be paid faster than the accrual.
2. **`burst` cap** — *Recommendation*: one day of accrual (14,400 blocks;
   187,200 VEIL at 650M, ~600 rebates). Big enough for any genuine cluster,
   small enough that an idle stretch never becomes a lump.
3. **Legal review before mainnet.** Distributing a market-valued token, even as
   a usage rebate, is a distribution event with UK/EU financial-promotion and
   MiCA-type exposure, and giving value implies sanctions screening.
   *Recommendation*: one opinion before mainnet, not before testnet.

## 9. Not solved

- **Pre-launch funding.** Before anything has settled there is nothing to
  rebate; the first wallets on a new network are funded by an operator from a
  pool key with the existing CLI.
- **A funded wallet on demand.** A rebate buys a first minimal secret; repeated
  mainnet use means acquiring VEIL.
- **Private claiming** (§6) — would need a zero-knowledge proof.
- **Claiming to an address other than the signer's**, and multi-address
  recipients. Out of scope for the MVP: the collector states their address by
  signing, which is the whole proof of control. Revisit when wallets carry
  several addresses.
- **Guardian and validator funding** — orders of magnitude larger, and out of
  band.
