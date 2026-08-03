# Creation Cost Integrity — Quote, Validate, Size — Plan

*Closes the gap between what a creator is told a secret costs and what the
chain actually debits. The chain's pricing is sound and needs no change: it
derives every charge itself, escrows them atomically, and enforces the gas
floor in both CheckTx and DeliverTx. The defects are all on the client side
of that boundary — the mobile quote omits the creation fee entirely, the
review breakdown does not sum to its own total, the SDK ships validators and
cost functions it never calls on the transaction path, and the SDK's flat gas
table is both wasteful at the bottom of the guardian band and unproven at the
top. Also pins the guardian's own gas spend to the amount the protocol
reimburses, so no daemon configuration can quietly put a guardian out of
pocket.*

> **Priority**: P1 — a creator is quoted a number they do not pay, and the
> shortfall pre-flight is computed from that same wrong number. Pre-testnet,
> so no migration burden; the cost of leaving it is user funds lost to a
> non-refundable fee they were never shown.
>
> **Status**: done — 27 July 2026, branch `worktree-creation-cost-integrity`.
>
> **Origin**: post-merge audit of PR #109 (guardian cost recovery), 27 July
> 2026 — the owner asked for confirmation that the clients price the new
> economics correctly, that the chain rejects under-priced transactions, that
> the SDK validates costings, and that creation costs are fully broken down.
> Three of the four came back negative. Evidence in §2 is measured against
> the running devnet, not derived from the constants.
>
> **Components**:
> - `mobile-client/app/src/screens/create/wizardState.ts` — `estimatePrice`
> - `mobile-client/app/src/screens/create/ReviewScreen.tsx` — breakdown,
>   shortfall pre-flight, "How the price is set" sheet
> - `mobile-client/app/src/screens/status/RenewScreen.tsx` — same omission on
>   the renewal quote
> - `typescript-sdk/src/protocol/txclient.ts` — `requestGuardians` validation,
>   the `FEES` table
> - `typescript-sdk/src/protocol/constants.ts` — a single total-debit helper
> - `guardian/blockchain/signer.go`, `guardian/config/config.go` — declared
>   gas for the two reimbursed messages
> - `x/secrets/keeper/` tests — gas regression guard
> - `docs/guides/CLIENT_CONVENTIONS.md` — the quoting obligation as a binding
>   convention; `docs/ECONOMICS.md` — cross-reference;
>   `docs/ACCEPTED_TRADEOFFS.md` — the residual gas overpayment (§6.4)
>
> No proto changes. No chain behaviour changes. `docs/spec.md` is unaffected:
> every amount involved is already specified correctly and the chain already
> implements it — this plan brings the clients up to the spec, it does not
> move the spec.

## 1. What is already correct

Stated first because the plan must not imply otherwise, and because it bounds
the work.

- **A creator cannot under-price a secret.** `MsgUserRequestGuardians` carries
  no amount field. The handler derives the reward pool, the accept fees and
  the creation fee itself and debits all three atomically
  (`x/secrets/keeper/msg_server_request_guardians.go`). An underfunded creator
  gets the whole transaction rolled back, never a cheap secret.
- **The gas floor is consensus law, not node etiquette.** `app/ante.go` wraps
  the runtime ante chain and rejects anything below
  `⌈gas × MinGasPriceUveilNum ÷ MinGasPriceUveilDen⌉` in *both* CheckTx and
  DeliverTx, so a friendly proposer cannot include a zero-fee transaction.
- **Guardians are not currently out of pocket.** Measured on the devnet (§2.4):
  every accept and reveal transaction is reimbursed with margin to spare.

The chain half of the owner's question is therefore answered in the
affirmative, and §3–§6 touch none of it.

## 2. The defects

### 2.1 The creation fee is absent from every client quote

`estimatePrice` returns `pool + acceptFees + gas`. The creator pays
`pool + acceptFees + creationFee + gas`. `creationFeeUveil` exists in the SDK,
is correct, and is pinned by `creation-fee.test.ts` against the shared vector
corpus — and is called by no application code in either client.

| Secret shape | Quoted | Actually debited | Short by |
|---|---|---|---|
| 1 hour, 7 guardians, ×1.0 | 0.3309 | 0.3909 | 0.0600 (**15.3%**) |
| Standard tier, 30 days | 3.3507 | 3.5020 | 0.1513 (4.3%) |
| High tier, 1 year, 12 guardians | 63.5129 | 66.6660 | 3.1531 (4.7%) |

All figures VEIL, at the current constants. The proportional error is worst on
short secrets, where the gas-denominated floor (60,000 uveil) sets the fee and
the pool is small.

This is the exact failure mode the alignment plan's §7 Q4 ruling names — *"the
user pays a number they never saw"* — arriving by omission rather than by the
staleness that ruling anticipated. See
[PENDING_CONSTANTS_SYNC_PLAN.md](../client-app/PENDING_CONSTANTS_SYNC_PLAN.md),
whose node-version sealing gate is the defence against the staleness variant
and does nothing about this one.

### 2.2 The shortfall pre-flight inherits the same error

`ReviewScreen.tsx` computes `shortfall = price.totalVeil − balanceVeil` from
the total in §2.1. A creator holding just over the quoted total clears the
pre-flight and fails on chain.

The severe path is worse than a failed transaction. If they clear Phase 1 —
paying pool, accept fees and **the non-refundable creation fee** — and then
cannot fund Phase 2, the commit expires, pool and accept fees refund, and the
creation fee is gone. A user loses real money because the client quoted low.

### 2.3 The review breakdown does not sum to its own total

The card lists three lines — Guardian pool, Network fee · reserve, Network fee
· distribute — then a Total. Accept fees are inside `totalVeil` but have no
line of their own, so on Standard tier the visible lines fall 0.084 VEIL short
of the printed total. The creation fee appears in neither.

The "How the price is set" sheet also still states
`P = rate × distance × max guardians × bump`, which is now only the *time
component*; it predates the two-component pool shipped in PR #108/#109 and
never mentions the accept fees or the creation fee.

`RenewScreen.tsx` computes its fresh-seal quote as `pool + acceptFees` with the
same creation-fee omission. Renewal is not wired to the chain yet
([PENDING_RENEW_ORCHESTRATION_PLAN.md](../client-app/PENDING_RENEW_ORCHESTRATION_PLAN.md)
owns that), so this is a latent rather than live defect — fixed here because
the arithmetic lives in the same place.

### 2.4 The SDK validates nothing on the transaction path

`requestGuardians()` builds the message and broadcasts. It does not call
`shareBandError()`, nor check bump, commit timeout or the reveal window —
every one of which the chain will reject. `send()` validates its amount is
positive; the protocol messages validate nothing. The validators exist, are
correct, and are vector-pinned; the mobile client is their only caller.

The cost of a rejected transaction is the creator's gas, so the SDK currently
lets a caller spend money to discover a bound it already knows.

### 2.5 The gas table is loose at the bottom and unproven at the top

`FEES` declares flat gas per message type. Measured against the devnet:

| Message | Declared | Used (observed) | Overpay |
|---|---|---|---|
| `MsgUserRequestGuardians` | 500,000 | ~200,000–213,000 | ~2.4× |
| `MsgUserDistributeShares` | 1,000,000 | 178,000–343,000 | ~2.9–5.6× |

Two consequences.

**Waste.** The creator burns ~0.094 VEIL per secret in gas that no execution
justifies — more than the entire reward pool on a short secret.

**Risk at the top of the band.** Phase-2 gas scales with the share count
(5 shares → ~178k, 7 → ~192k, 12 → ~278k, with a 343k outlier driven by
payload size). No secret at `max_shares = 32` has ever been measured. If the
flat 1,000,000 proves insufficient there, Phase 2 fails out of gas — and by
§2.2's mechanism the creator forfeits the creation fee.

### 2.6 Guardian coverage holds, but nothing guards it

Measured on the devnet at the current daemon defaults:

| Leg | Declared gas | Used | Paid | Reimbursed | Margin |
|---|---|---|---|---|---|
| `MsgGuardianConfirmShares` | 110,314 | ~89,500 | 11,032 | 12,000 | +968 (8.8%) |
| `MsgGuardianRevealShare` | 118,984 | ~95,500 | 11,899 | 13,000 | +1,101 (9.3%) |

The reimbursement is a fixed protocol constant; what a guardian actually pays
is `declared_gas × its own configured gas price`, where `declared_gas` is the
*simulated* gas scaled by the daemon's `gas_adjustment` (default 1.5). Coverage
therefore rests on two values in a per-operator config file and on the
simulator's output, and it is asserted by no test. It inverts silently if an
operator raises `gas_adjustment` or `gas_price`, or if ordinary code growth
pushes simulated gas past the constant.

The margin is real but thin, and its thinness is invisible today.

## 3. Fix — one total, computed once, used everywhere

The root cause of §2.1–§2.3 is that the creator's total debit is assembled ad
hoc at each call site. It gets computed once, in the SDK, and every surface
consumes it.

Add to `typescript-sdk/src/protocol/constants.ts`:

```
creationQuote({ distanceBlocks, maxShares, bumpHundredths }) → {
  timeComponentUveil,   // the wage base — what the creation fee is charged on
  revealLegsUveil,      // maxShares × F_reveal, inside the pool
  poolUveil,            // timeComponent + revealLegs — escrowed, refundable
  acceptFeesUveil,      // maxShares × F_accept — escrowed, refundable
  creationFeeUveil,     // non-refundable, rides the 90/10 split
  creationFeeRegime,    // 'floor' | 'percent' — which side of the max() priced it
  gasPhase1Uveil,
  gasPhase2Uveil,
  totalUveil,           // the sum, and the only number a shortfall check may use
}
```

Every field is derived from the existing pinned functions; the helper adds no
new economics, it removes the opportunity to forget one. `creationFeeRegime`
is surfaced because a floor-priced fee is the case where the user most needs
telling *why* a one-hour secret costs what it does — the chain already emits
the same distinction in the reservation event.

The mobile `PriceEstimate` becomes a thin presentation wrapper over
`creationQuote`, keeping its block/date estimation but owning no arithmetic.

## 4. Fix — show the whole bill

`ReviewScreen` lists every charge, and the lines sum to the total:

| Line | Refundable? |
|---|---|
| Guardian pool | Yes — pro-rata on cancel, in full if nobody reveals |
| Guardian gas cover | Yes — unearned slices refund at terminal state |
| Creation fee | **No** |
| Network fee · reserve | No (gas) |
| Network fee · distribute | No (gas) |
| **Total** | |

The non-refundable rows carry that fact in the UI, because §2.2's severe path
is precisely a user who did not know the creation fee was gone. The
refundability column is the honest framing of the pool and accept fees too:
they are escrow, not a charge, and a creator comparing Timeflare to a fee
should see which of these they might get back.

The "How the price is set" sheet is rewritten to the two-component pool
(`P = max_shares × F_reveal + rate × distance × max_shares × bump`), states
that the creation fee is charged on the time component only and never on the
gas pass-through, and names the floor when the floor is what priced it.
[docs/ECONOMICS.md](../../ECONOMICS.md) §3 and §4 are the source; the sheet is its
plain-language projection and must not restate the derivation.

An arithmetic test asserts the displayed lines sum to the displayed total —
the defect in §2.3 was invisible precisely because nothing checked it.

## 5. Fix — validate before spending gas

`requestGuardians()` validates its inputs against the pinned bounds and throws
before broadcasting: the share band via the existing `shareBandError()`, plus
bump, commit timeout, reveal duration, reveal start offset and the reveal
horizon. `distributeShares()` validates the share count matches the assignment
count and the payload ciphertext cap.

These are the chain's own bounds, mirrored — the SDK is not inventing policy,
and where the chain's rule is authoritative the mirror stays pinned to
`testdata/vectors/` as `shareBandError` already is. Any bound not currently in
the vector corpus gets added there in the same change, so the mirror cannot
drift from the chain silently.

**Validation throws; it does not return.** We do not attempt transactions we
know will fail (ruled 27 July 2026). A rejected transaction costs the caller
gas, so an SDK that broadcasts input it has already recognised as out of range
charges the user to learn something it knew for free. This breaks any caller
currently relying on the chain to do the rejecting — acceptable, since nothing
is live and the behaviour being broken is paying for rejections. The rejected
alternative, a `validateRequest()` the caller may skip, reproduces today's
defect exactly: correct validators sitting uncalled.

## 6. Fix — size gas to the work, pin the guardian's spend

### 6.1 Creator-side: measured, deterministic, share-aware

The `FEES` table's determinism is deliberate and is kept: its comment states
that a wallet UI must quote the fee before signing, and §3's whole purpose is
an exact number shown up front. Simulation would forfeit that.

So the flat constants become a measured linear model — a base, a per-share
term for Phase 2, and a per-byte term for the payload — with generous but
bounded headroom over the measured worst case. The coefficients are derived
from a measurement sweep across the full band (`max_shares` 2…32) at the
maximum payload, and pinned by a test that fails when real gas approaches the
declared limit.

The sweep must run at `max_shares = 32`, which no measurement has yet covered
(§2.5). If it shows Phase 2 needs more than the current flat 1,000,000, that
is a live bug today, not merely waste.

### 6.2 Guardian-side: declare exactly what the protocol reimburses

For `MsgGuardianConfirmShares` and `MsgGuardianRevealShare` the daemon stops
simulating and declares the protocol constant — `GuardianAcceptGas` and
`GuardianRevealGas` — at the consensus floor price (ruled 27 July 2026).
Reimbursement then equals spend exactly, by construction, and no
`gas_adjustment` value can change that.

The question this settles is whether "is anyone left out of pocket?" has a
structural answer or a configuration-dependent one. Before this change,
coverage held only because two values in a per-operator config file happened
to sit where they do; afterwards it holds because the daemon spends exactly
what the protocol pays back.

This is safe on the measured numbers: 120,000 declared against ~89,500 used is
34% headroom, wider than the 1.5× adjustment currently provides over
simulation. It is also the right failure mode. Today, growth past the constant
silently transfers cost to guardians; afterwards it fails the transaction
visibly, and §6.3 catches it in CI long before that.

If an operator raises `gas_price` above the floor to buy mempool priority they
will pay more than they are reimbursed. That is a deliberate purchase, not a
protocol defect — the daemon logs the shortfall per transaction once at
startup so the choice is informed rather than discovered, and §6.4 records it
as an accepted residual rather than leaving it implicit.

### 6.3 A regression guard

A keeper test executes both guardian handlers and asserts consumed gas stays
below a stated fraction of the reimbursement constant, so ordinary code growth
trips CI while there is still headroom rather than when a guardian starts
losing money. The same test documents the measured figures, which are
currently recorded only in prose in `constants.go` and this plan's §2.6.

Run across the share band, since the constants are flat and a per-share gas
term in either handler would break them at the ceiling.

### 6.4 The residual, written down

§6.1 and §6.2 bound gas overpayment; neither removes it, and the honest record
of what remains belongs in
[ACCEPTED_TRADEOFFS.md](../../CHAIN_MECHANICS.md) as a new entry rather than in
this plan's history. Two residuals:

- **Declared gas exceeds consumed gas, and Cosmos never refunds the
  difference.** Every actor overpays somewhat by construction. §6.1 sizes the
  creator's declaration to measured work instead of a flat guess, and §6.2
  makes the guardian's declaration exactly what it is reimbursed — but a
  declaration must exceed worst-case consumption or the transaction fails, so
  a margin always exists and is always spent.
- **An operator who raises `gas_price` above the consensus floor pays the
  difference themselves.** The reimbursement is denominated at the floor, so
  buying mempool priority is a purchase the guardian makes, not a cost the
  protocol reimburses. Making the daemon override the operator's own price
  would be worse: it would silently deny a guardian the ability to get a reveal
  through a congested chain, and a missed reveal is a slash.

## 7. What this plan does not solve

- **It does not re-price anything.** No constant, curve, split or formula
  moves. If the measured Phase-2 sweep (§6.1) shows the guardian band's top end
  is economically unattractive, that is an input to a future economics plan,
  not this one.
- **It does not address which dials a client exposes.** `max_shares` being
  hardcoded client-side, and the coarse step granularity on bump, commit
  timeout and reveal duration, are the subject of
  [DONE_ADVANCED_DIALS_PLAN.md](../client-app/DONE_ADVANCED_DIALS_PLAN.md).
  The two plans touch `wizardState.ts` together and should be sequenced, not
  merged: this one corrects arithmetic that is wrong today, that one widens a
  surface that is merely narrow.
- **It does not close the stale-constants risk.** A client pinning constants an
  upgrade has moved still mis-quotes, correctly summed. That is
  [PENDING_CONSTANTS_SYNC_PLAN.md](../client-app/PENDING_CONSTANTS_SYNC_PLAN.md)'s
  node-version sealing gate, still outstanding.
- **It does not make gas exact.** Declared gas exceeds consumed gas by design
  in Cosmos, and unused gas is not refunded. Both clients will keep overpaying
  somewhat; §6.1 bounds the overpayment, it does not remove it.

## 8. Settled during execution

Two things the plan did not anticipate, both found by measuring rather than by
reading the code. Recorded here because each changed the design.

**The accept leg outgrew its reimbursement, and the cause was bookkeeping.**
The §6.3 band sweep — written to guard against future growth — found existing
growth: accept-handler gas climbed ~4,200 per selected guardian and reached
177,148 at the ceiling against a flat 120,000 reimbursement, so coverage ran
out at about fifteen guardians, below the shipped Maximum preset. The cause was
that an acceptance rewrote the entire secret record (one address and one frozen
bond per guardian) to increment a single counter. The owner ruled to fix the
cause rather than price around it: the acceptance tally moved to its own key,
which flattened the slope to ~430 per guardian and brought the ceiling to
48,144 — inside the existing constant, with no proto change and no re-pricing.
The tally was already redundant state, cross-checked against the assignment
records by both the genesis check and keeper invariant 5, and those two checks
are what found every read path that needed the join.

**The gas model's shape was wrong in both directions.** The sweep (§6.1)
established that phase 1 is *not* band-independent — selection walks the
candidate set and freezes one bond per guardian, so it climbs ~4,381 each — and
that phase 2's old flat 1,000,000 was ADEQUATE at the ceiling (789,900
measured), not insufficient as §2.5 feared. The real phase-2 defect was waste,
not failure. Both provisional figures written before measurement were wrong: a
flat phase 1 was insufficient above fifteen guardians, and a first-pass phase-2
model fell short at the ceiling by ~23,000 gas. The declared model now carries
~25% over a measured fit pinned in `testdata/vectors/tx_gas.json`.

One residual follows from the first: the phase-1 gas base covers work
proportional to the REGISTERED guardian set, so it grows with network size. The
margin absorbs moderate growth; a sustained climb means re-measuring rather
than widening the margin, and the corpus says so.

## 9. Execution order

1. §3 — the `creationQuote` helper and its tests (SDK). Everything else
   consumes it.
2. §4 — the mobile breakdown, shortfall pre-flight, explainer sheet, and the
   lines-sum-to-total test. Fixes the user-visible money defect.
3. §5 — SDK validation on the transaction path, throwing.
4. §6.1 — the gas measurement sweep, then the model. The sweep is also the
   verification that `max_shares = 32` works at all today.
5. §6.2/§6.3 — guardian signer and the keeper gas guard.
6. §6.4 — the `ACCEPTED_TRADEOFFS.md` entry, written once §6.1's measurements
   fix the real numbers.
7. `CLIENT_CONVENTIONS.md` — the quoting obligation written up as a binding
   convention (§7 "Reserved / not yet pinned" is its natural home): a client
   quoting a creation price must include every non-refundable charge, and must
   not compute a sufficiency check from a subtotal.

Verification is `make test` plus the mobile jest suite, and an end-to-end seal
on the devnet at both band extremes with the quoted total reconciled against
the creator's actual balance delta — the only check that proves the quote and
the debit agree.
