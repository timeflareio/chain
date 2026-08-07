# Protocol-Derived Reveal Window — Plan

*The creator chooses how long guardians have to reveal, anywhere from 10 minutes
to a day. The window is a retry budget against guardian downtime, and what
governs how much is needed is how long the guardian has been silent since it last
proved it was alive — a quantity the protocol knows exactly and the creator does
not. Remove the field and derive the window on chain.*

> **Status: done — 7 August 2026.** Executed on `fixed-reveal-window`, merged as
> [PR #29](https://github.com/timeflareio/chain/pull/29). The full release train
> ran: chain `v0.0.5` + `x/secrets/types/v0.0.4`, guardian `v0.0.5`, SDK `v0.0.6`
> then `v0.0.7`, mobile merged, devnet pins moved, and both suites green against
> those artefacts (`e2e` full lifecycle; `e2e-scenarios` 74 assertions, 0 failed).
> `COMPATIBILITY.md` carries the row.
>
> **Three things the execution found that §6 had not listed:**
>
> 1. **The SDK's `examples/` are wire-facing.** They drive the chain's e2e
>    harness, so `v0.0.6` shipped `src/` correctly and still broke the harness;
>    `v0.0.7` fixed them. `scenario-create.js` took the reveal duration as a
>    *positional* argument, so removing it shifted `bump` and `shares` — the
>    devnet's `create_secret` helper and its seven call sites had to move in the
>    same change.
> 2. **`make sdk-sync` is version-blind** (`make/devnet.mk:100`): it short-circuits
>    when `.devnet/sdk/examples` and `node_modules` exist and never compares
>    `SDK_VERSION`, so moving the pin cannot force a re-fetch. The harness silently
>    kept running the old SDK and reported success — exactly the failure mode
>    `PROTOCOL_CHANGE.md` describes, but in the target rather than the operator.
>    Worked around with `rm -rf .devnet/sdk`; it wants a real fix.
> 3. **`mobile-client` has two SDK pins**, not one: `app/package.json` and
>    `e2e/package.json`. Both are legitimate — they are separate npm workspaces
>    and both consume the SDK, `e2e` driving it directly in its suites — and both
>    sat at `v0.0.3` before this change. Nothing keeps them equal, so moving one
>    is silently a divergence until npm hoists them into conflicting lockfile
>    entries and the type-checker resolves the stale one. The chain has
>    `make verify-pins` for this class of problem; mobile has no equivalent.
>
> Neither 2 nor 3 is in this plan's scope; both are worth their own change.
>
> **Priority**: P2 — protocol surface, pre-testnet. Removing a wire field is
> cheap now and expensive once integrators have pinned it.
>
> **Origin**: design session, 6–7 August 2026. The mobile client hardcodes the
> window at 300 blocks in two places and the devnet compose defaults carry a
> third literal at 100 — a dial no caller varies. Owner rulings:
>
> - **6 August 2026** — the client must not set it, and a governance-tunable
>   module parameter is deliberately not in scope, to be reassessed after testnet
>   launch (§5).
> - **7 August 2026** — the window scales with the hold rather than being a single
>   scalar (§2 records the reasoning that displaced the flat value); the bounds are
>   **5 minutes and 12 hours**; the floor holds to a **1-hour** secret and the
>   ceiling engages from a **30-day** secret and runs to the horizon; the ramp
>   between them is **concave (square-root)**; the `Secret` record **keeps** its
>   stored `reveal_start_block`/`reveal_end_block` rather than deriving on read;
>   and `RevealWindow`, now defining only a start, **collapses to a scalar** on
>   the request message (§4). Recalibration is handled by rebuilding the chain
>   rather than migrating live state (§5).
>
> **Note on "constant"**: the 6 August ruling was for a compile-time value over a
> governance parameter, and that still holds — the derivation in §3 is
> compile-time, immutable, and identical on every node. What changed is that the
> compile-time value is a short function of an existing input rather than a
> scalar.
>
> **Note on the ruled bounds**: they were given in wall-clock and are recorded here
> in **blocks**, per this repository's rule that the protocol is never denominated
> in wall-clock. The block counts are the normative values; the minutes and hours
> describe them at the production 6s cadence only.
>
> **Components**: `proto/timeflare/secrets/v1/secret.proto` (`RevealWindow`),
> `x/secrets/types` (constants, the derivation, `ValidateBasic`),
> `x/secrets/keeper` (`validateRevealWindow`, window derivation),
> `x/secrets/client/cli/tx.go`, `docs/spec.md`, `docs/operations.md`,
> `testdata/vectors/dials.json` and `testdata/vectors/tx_gas.json` plus the
> vendored copies under `typescript-sdk/src/vendor/vectors/`, `typescript-sdk/`
> (`protocol/constants.ts`, `protocol/txclient.ts`, the `DIALS` table),
> `mobile-client/app/` (Timing screen, wizard draft, seal driver, compose state,
> Renew screen), `devnet/` scenarios and `mobile-client/e2e/`.
> `timeflareio/guardian` and `timeflareio/crypto` are **clear** — swept 6 August
> 2026, zero references — but guardian still takes the wire-contract pin bump in
> the release train.
>
> **Related plans**: follows the pattern of
> [done/DONE_FIXED_COMMIT_TIMEOUT_PLAN.md](done/DONE_FIXED_COMMIT_TIMEOUT_PLAN.md),
> which collapsed the sibling dial — though that one resolved to a scalar and
> this one does not, for the reason in §2. Reverses the `revealDuration` dial
> exposed by
> [DONE_ADVANCED_DIALS_PLAN.md](../../../mobile-client/docs/planning/done/DONE_ADVANCED_DIALS_PLAN.md);
> supersedes the §7 Q2 wizard-default ruling in
> [PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md](../../../mobile-client/docs/planning/PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md)
> (§7).

## 1. Why the dial does not earn its field

The reveal window is `reveal_start_block … reveal_end_block`, both inclusive,
with `duration = end − start` supplied by the creator and bounded 100–14,400.

What the window buys is **retry budget**. `guardiand` reveals as soon as the
window opens — event-driven discovery with a 6-second fallback poll
(`internal/config/config.go:186`) and in-flight deduplication — and retries on
failure until the window closes. It does not gate reconstruction: the recipient
can rebuild the moment `threshold` shares are on chain, however long the window
then stays open. It gates only *settlement*, at `reveal_end_block + 1`.

So the question the creator is being asked is: how likely is it that a guardian
is temporarily unable to transact when the window opens, and how long should the
protocol wait for that to clear? Both terms are properties of guardian
operations. The creator knows neither — but, as §2 sets out, the protocol knows
the first one exactly.

The two adjacent risks the creator *can* reason about already have their own
dials, and neither is served by this one:

- **Permanent guardian loss** is real, but a wider window does not resurrect a
  guardian that has gone. That risk is managed by the redundancy band —
  `max_shares` above `min_shares` — which is the instrument for it.
- **Value at stake** is expressed through `bump`, which scales deterrence.

The evidence that the field is inert is that no caller varies it. Mobile's wizard
default is 300 (`wizardState.ts:52`), the Renew path re-declares the same literal
locally (`RenewScreen.tsx:31`), and the devnet compose default is 100
(`compose.ts:59`). Three literals, one of them silently divergent, and no user
journey that moves any of them.

## 2. Why the window scales with the hold

The natural first answer is a single scalar, as `CommitTimeoutBlocks` is. It is
the wrong answer here, and the reason is the same one that makes the reveal
cushion different from the commit cushion in the first place.

**Liveness evidence decays.** A guardian accepting shares was selected minutes
earlier and has just transacted — it is observably alive. A guardian revealing
may have held for up to `H`, about a year, and the protocol has heard nothing
from it since. There is **no heartbeat**: between `MsgGuardianConfirmShares` and
its reveal a guardian is silent on chain. `MsgGuardianUpdate` exists but nothing
obliges a guardian to send one, so it cannot be relied on as a liveness signal.

That gives the exact variable. A guardian's last proof of life is its acceptance,
which lands by `commit_deadline`, so the interval over which its health is
unobserved is

```
hold = reveal_start_block − commit_deadline = start_offset − CommitTimeoutBlocks
```

This is not a proxy for staleness. It *is* the staleness, known exactly at
publication.

**Why staleness changes the answer.** It is tempting to reason that transient
faults — a restart, a wedged endpoint, an expired certificate, a full disk — are
minutes-scale regardless of how long the secret has waited, and therefore that
one cushion fits every hold. That conflates two different quantities. The
*duration* of a fault is indeed hold-independent. The *probability of being in
one when the window opens* is not: a guardian observed transacting 50 blocks ago
is almost certainly healthy, whereas one last seen a year ago has had a year in
which to drift into a recoverable-but-currently-broken state — a migration left
half-done, a credential expired, an operator who moved on. As that probability
rises, more of the fault-clearance distribution falls inside the window, so the
marginal value of each extra block of cushion rises with the hold. A flat value
is therefore either too tight at the top of the range or wastefully long at the
bottom.

**Why keyed on the hold and not on `distance`.** `distance` is measured to
`reveal_end_block + 1`, so a window defined as a function of distance is
self-referential — solvable for an affine function, but the pool price stops
being something the creator can read off their own inputs, in a protocol whose
economics are deliberately exact. The hold excludes the window by construction,
so the derivation is a plain forward computation. It is also the more faithful
variable: distance includes the commit phase and the window, neither of which is
time during which the guardian is unobserved.

**Why "cost is immaterial" does not collapse this to a flat generous value.** The
owner has ruled the token cost of the cushion immaterial (6 August 2026), which
removes the obvious argument for keeping windows short. But tokens are not the
only cost. Settlement occurs at `reveal_end_block + 1`, so the window is a tail
on every secret before guardians are paid, bonds return to `unlocked`, and the
creator learns the outcome. Sizing every secret for the year-long case would put
a twelve-hour settlement tail on a ten-minute secret — disproportionate in
wall-clock terms irrespective of price, and a poor product. That is what the
curve buys, and it survives the cost ruling.

## 3. The derivation

Four corners, all ruled by the owner (7 August 2026):

| Constant | Blocks | At 6s |
|---|---|---|
| `RevealWindowFloor` | 50 | 5 minutes |
| `RevealWindowCeiling` | 7,200 | 12 hours |
| `RevealRampStart` — the floor holds to here | 600 | 1 hour |
| `RevealRampEnd` — the ceiling holds from here | 432,000 | 30 days |

```
hold = start_offset − CommitTimeoutBlocks

hold ≤ RevealRampStart  →  RevealWindowFloor
hold ≥ RevealRampEnd    →  RevealWindowCeiling
otherwise               →  RevealWindowFloor
                           + isqrt((hold − RevealRampStart) × rise² ÷ span)

where rise = RevealWindowCeiling − RevealWindowFloor   (7,150)
      span = RevealRampEnd − RevealRampStart           (431,400)
      isqrt is truncating integer square root
```

Everything is **blocks**, and integer throughout — stated normatively in exactly
that form, with the `k` adjustment helpers as the precedent for "normative in
exactly this integer form". Multiplication precedes division so no precision is
lost to truncation; the largest intermediate is
`431,400 × 7,150² ≈ 2.2 × 10¹³`, comfortably inside `int64`. Both ruled knees are
hit exactly: at `hold = RevealRampEnd` the argument reduces to `rise²`, whose
integer square root is exactly `rise`.

| Hold | Blocks | Window | At 6s |
|---|---|---|---|
| protocol minimum | 50 | 50 (floor) | 5 min |
| 1 hour — ramp starts | 600 | 50 (floor) | 5 min |
| 6 hours | 3,600 | 646 | 1.1 h |
| 1 day | 14,400 | 1,328 | 2.2 h |
| 7 days | 100,800 | 3,495 | 5.8 h |
| 15 days | 216,000 | 5,102 | 8.5 h |
| 30 days — ramp ends | 432,000 | 7,200 (ceiling) | 12 h |
| 1 year — horizon | 5,255,950 | 7,200 (ceiling) | 12 h |

**Why the floor is 50 and not larger.** The staleness argument sets it: a guardian
that confirmed 50 blocks ago is the most recently verified-live guardian in the
system, so it needs the *least* cushion, not the most. Sizing the floor for a
human on-call cycle would contradict the very reasoning that motivates the ramp,
and would put a settlement tail on a ten-minute secret three times longer than its
entire hold. 50 also matches `CommitTimeoutBlocks`, which covers the structurally
identical cushion at the other end of the lifecycle.

**Why the ceiling is 7,200.** At the far end the guardian's last proof of life is
up to a year old, and the failure that matters is one automated retry cannot
clear. Twelve hours covers an incident that begins outside working hours and is
picked up the following morning. It also sits inside today's `MaxRevealDuration`
of 14,400, so nothing becomes permissible that the protocol does not already
permit.

**The interior is concave, and that is the point.** With both knees fixed, the
path between them is what encodes the risk model. If breakage arrives at a roughly
constant rate, the probability that a guardian is in a broken state at window-open
follows `1 − e^(−λ·hold)` — it rises fastest early and saturates. A square-root
interior tracks that shape; a linear one would under-provision exactly where the
probability is climbing quickest, in the first days of a hold. The cost is a
longer tail on medium secrets — a one-day hold draws 2.2 h rather than the 28 min
a linear ramp would give — accepted because the tail is wall-clock on an already
day-long secret, whilst the under-provisioning it avoids is an honest guardian
being slashed.

Note the floor still governs the short end: everything under a 1-hour hold gets
50 blocks regardless of shape, so the concave interior costs the short-secret case
nothing.

**Calibration is judgement, not measurement.** The four corners are reasoned, not
fitted to observed reveal latency, because none exists yet. The *structure* is the
durable part; the numbers should be revisited once testnet produces data, which
means rebuilding the chain rather than migrating anything (§5).

**Both bounds are deleted, not replaced.** `MinRevealDuration` and
`MaxRevealDuration` are read only by the two comparisons in `ValidateBasic` and
the duplicate pair in the keeper's `validateRevealWindow`; all four go, since
there is no longer a creator input to validate.

## 4. What this changes downstream

- **The `Secret` record keeps both absolute heights** — `reveal_start_block` and
  `reveal_end_block` (`secret.proto:124`,`:126`) — computed once at publication
  and frozen, exactly as today. Nothing about the settlement due-height queue
  changes, and no migration is needed. Dropping the stored end and re-deriving it
  on read was considered and **rejected** (owner ruling, 7 August 2026); the
  reasons are below.
- **`RevealWindow` collapses to a scalar** (owner ruling, 7 August 2026). With
  `duration` gone it would define only the start, so the wrapper stops earning
  its place: `MsgUserRequestGuardians` carries a bare `int64 reveal_start_offset`
  and the `RevealWindow` message is deleted outright. It is used in exactly one
  place (`tx.proto:81`) and by nothing else, so nothing else moves — in
  particular the `Secret` record is untouched, because it never used the wrapper.
  Its window has always been the two scalar heights above, and it keeps them.
- **`MaxRevealStartOffset` becomes correct.** It currently equals `H`, which
  overstates the real ceiling — an offset of exactly `H` leaves no room for any
  window at all. Because the ceiling lands inside the curve's saturated region,
  the true maximum is the literal `H − 7,200 = 5,248,800`, at which
  `start_offset + window` is exactly `H`. This is a latent off-by-window bug in
  the current constant, fixed as a side effect.
- **The `distance` bound is unaffected.** Maximum distance is still reached when
  `reveal_end_block = created_at + H`, so `distance ≤ H + 1 − CommitTimeoutBlocks`
  holds verbatim and the maximum-bond figure in the spec (≈ 1,262 VEIL) does not
  move.
- **Minimum pricing falls slightly**, since the shortest secret's window goes from
  a permitted minimum of 100 to a derived 50. Immaterial under the cost ruling;
  noted because it moves `creation_fee` and `dials` vector expectations, and
  because it is a *reduction* — no existing quote gets more expensive at the short
  end.
- **One fewer cross-implementation bound to keep in step** — but one new shared
  computation. The bounds currently appear in `constants.go`, `dials.json`, the
  SDK's `DIALS` table and mobile's `ADVANCED_LIMITS`; all four go. In their place
  the derivation has to exist client-side to quote a price before submission —
  and it should exist **once**, exported from the SDK and called by mobile, not
  reimplemented in both. `testdata/vectors/` pins that single copy against the
  chain's (§6 items 7 and 10).
- **The SDK's dial API breaks, intentionally.** `DialValues.revealDurationBlocks`
  is a required field, so removing it is a compile error at every construction
  site rather than a silent default — which is what you want when a dial stops
  existing. Acceptable because nothing is live (owner, 7 August 2026); the same
  licence covers reusing the proto field number in §6 item 3.

### Why the stored `reveal_end_block` stays

It is now derivable, so keeping it is a deliberate choice rather than an
oversight. The window is **fixed at creation and immutable thereafter** — the
offsets are set when the creator requests guardians and nothing in the lifecycle
moves them — so the derived value can never change for a given secret. That
immutability is what makes the case, in three parts:

1. **Recomputation buys nothing and costs on every read.** The height is read far
   more often than it is written: the settlement queue key, the guardian's window
   checks, every client rendering a countdown. Deriving a value that is by
   construction the same answer every time is pure repeated work — and the
   concave form is not free, carrying a multiplication into the 10¹³ range and an
   integer square root.
2. **It confines the curve to creation time.** Stored, the chain runs the
   derivation once per secret and writes a height; from then on anyone asking when
   the window closes *reads a number*. The curve still has to exist in the SDK and
   mobile, but only to quote a price **before** submission, since a creator wants
   the cost before they sign — a pre-submission concern that ends the moment the
   secret exists. Derived, the curve instead runs on every read, in every consumer
   that shows or acts on a closing height — SDK, mobile, guardian, block
   explorers, any third-party integrator — and each has to reproduce the clamps,
   the knees, the integer square root and its truncation *bit-identically*, or it
   acts on a different closing height than the chain will settle on. That is a
   correctness obligation, not a display detail, and it multiplies precisely the
   drift hazard the vector corpus exists to contain. Mobile is the sharp edge,
   being the component that already reimplements the primitives by hand.
3. **It is what keeps the guardian clear of the curve.** `guardiand` reads the
   stored field directly for its window checks and dashboard countdowns
   (`service.go:588`, `dashboard_source.go:180`,`:216`), and the 6 August sweep
   found zero reveal-duration references in that repository. That property is
   produced by storing the height — it is not automatic. Deriving would introduce
   the curve into the daemon for the first time.

Two smaller consequences worth noting rather than arguing from. The settlement
due height is a composite key `(RevealEndBlock + 1, secret_id)` written at
publication (`due_queues.go:32`) and reconstructed later to *delete* the entry
(`dequeueSettlement`, `:78`, called on cancellation and commit-timeout failure);
reading it from the record means the key deleted is always the key written, with
no dependence on the derivation being re-executed identically. And invariant 4
(`invariants.go:391`) cross-checks the queue key against the record — a real
consistency check between two independently written values, which would become
vacuous if one were derived from the other.

The counter-argument — that derived state cannot drift from the rule — is handled
by validation rather than by derivation: import-time checks assert the window is
ordered and within `RevealWindowFloor … RevealWindowCeiling`.

## 5. Not solved

- **Governance tunability is out of scope**, by owner ruling (6 August 2026), and
  **recalibration does not migrate live state** (owner, 7 August 2026): while in
  testnet, changing the four corners means rebuilding the chain or shipping a new
  one, not upgrading over existing secrets. So this plan carries no migration
  path, and none of §4's reasoning depends on one. If that ever ceases to hold —
  a live network with secrets in flight — the stored height is already the thing
  that would make an in-place change safe, since a secret's window is frozen at
  creation and cannot be re-derived out from under it. That is a property worth
  having, not a plan for using it.
- **The calibration is unmeasured** (§3). The structure is the durable part; the
  four corners are a first estimate and should be treated as such.
- **The hold is a whole-roster approximation.** The derivation uses one interval
  for the secret, but staleness is really per-guardian: a roster's members all
  accepted within the same commit window, so they share a hold to within 50
  blocks, which is why the approximation holds. It would stop holding if
  acceptance were ever spread over a longer period.
- **A heartbeat would be strictly better and is not proposed.** If guardians ever
  signalled liveness on chain, the window could key on time since *that* signal
  rather than since acceptance, and would shrink for any guardian still visibly
  healthy. That is a new protocol surface with its own costs and needs its own
  argued case; it is noted here only so the choice of variable is understood as
  the best available one, not the ideal one.
- **This does not improve guardian reveal reliability**, only how much tolerance
  the protocol grants. Reveal-side liveness — alerting, retry behaviour, operator
  runbooks — stays in the guardian repository.
- **The cushion remains inside `distance`**, so the creator continues to pay for
  it at `rate` per guardian per block. Moving it outside would hand the creator
  free locked guardian capital for the window and break the paid-hold invariant.
  Settled, not open.

## 6. Work items

Spec first, per the spec-first rule — this changes protocol behaviour.

1. `docs/spec.md`: state the reveal window as protocol-derived, with the
   derivation, the worked table from §3 and the reasoning from §2 (the staleness
   argument is the justification a reader will look for, and it is not obvious);
   remove `duration` from the `MsgUserRequestGuardians` documentation and the
   `RevealWindow` struct comment; restate the horizon rule in §Timing Constraints
   as a bound on `start_offset` alone; drop the `Min/MaxRevealDuration` rows from
   the three parameter tables (§Economic Parameters, §Timing Constraints, the
   constants summary) and add the derivation alongside `CommitTimeoutBlocks`;
   correct `MaxRevealStartOffset` per §4. In §State Integrity & Import-Time
   Validation, state the window check as **bounds and ordering** — the window is
   frozen at creation, so import validates that what was stored is well-formed,
   not that it matches a fresh derivation.
2. `docs/operations.md`: drop the `duration` field row and restate
   `reveal_end_block` as derived.
3. `proto/`: delete the `RevealWindow` message from `secret.proto:10-16` — after
   this change it is used nowhere, its only reference being `tx.proto:81`. In
   `MsgUserRequestGuardians`, replace `RevealWindow reveal_window = 3` with
   `int64 reveal_start_offset = 3`, reusing the number. The wire type changes from
   length-delimited to varint — breaking, and accepted: nothing is live (owner,
   7 August 2026). Regenerate.
4. `x/secrets/types`: delete `MinRevealDuration`/`MaxRevealDuration`; add the four
   corner constants and the derivation as an exported normative function beside
   the `k` adjustment helpers, which are the precedent for "normative in exactly
   this integer form" — chain, tests and tooling must all call the same code. The
   concave interior needs a **truncating integer square root**; write it
   explicitly (Newton or bit-wise) rather than going via `math.Sqrt`, whose
   float64 rounding is not guaranteed identical across architectures and would be
   a consensus hazard. Correct `MaxRevealStartOffset` to `H − 7,200`; delete the
   two range checks in `message_request_guardians.go:62-67`; update fixtures built
   from the bounds.
5. `x/secrets/keeper`: derive `revealEndBlock` via the new helper
   (`msg_server_request_guardians.go:70`) and keep storing it on the record;
   delete the duplicate range checks at `:313-319` and restate the horizon check
   at `:327`. Leave invariant 4 (`invariants.go:391`) reading the stored field —
   rewriting it to re-derive would make it compare the derivation against itself
   and stop it catching anything (§4).
6. `x/secrets/client/cli/tx.go`: remove the reveal-duration flag and its default.
7. `testdata/vectors/dials.json`: remove the `reveal_duration_blocks` bound entry
   and the field from every case; restate the horizon rule
   (`start_offset + duration <= 5256000`) as a bound on `start_offset`; **delete
   the cases that exist only to exercise the removed bounds** — `maximum reveal
   duration` and its minimum counterpart — and re-anchor any case whose name or
   reason cites the range. **Add cases pinning the derivation**: both ruled knees
   exactly (`hold = 600 → 50`, `hold = 432,000 → 7,200`), one hold either side of
   each knee to catch an off-by-one in the comparison, at least two interior
   points, and the horizon. This is the corpus that stops the SDK's and mobile's
   reimplementations from drifting (§4), so it is a deliverable in its own right
   rather than a fixture refresh. Update the corpus description.
8. `testdata/vectors/tx_gas.json`: fix the `phase_1_measurement` note, which cites
   `duration 100` as a sweep input. No re-measurement needed — the corpus declares
   a fitted model with ~25 % headroom and `gas-model.test.ts` only fails when the
   declared model falls *below* a measured point. Dropping a field shrinks the
   transaction, so measured gas moves down, away from the failing direction.
9. Re-sync the vendored corpus under `typescript-sdk/src/vendor/vectors/`.
10. **`typescript-sdk`** — the SDK is where the derivation lands for clients, and
    it must be the *only* client-side copy:
    - `protocol/constants.ts`: drop `REVEAL_DURATION_MIN_BLOCKS`/
      `REVEAL_DURATION_MAX_BLOCKS` (`:120-121`); add the four corner constants and
      **export the derivation** — `revealWindowBlocks(startOffsetBlocks)` — plus a
      `distanceBlocks(startOffsetBlocks)` helper, since callers currently assemble
      distance by hand and would otherwise each re-derive the window to do it.
      Assert both against the new vector cases. A second hand-rolled copy
      downstream is exactly the drift §4 exists to prevent.
    - `DIALS`: delete the `revealDuration` descriptor and `'revealDuration'` from
      the `DialId` union; **remove `revealDurationBlocks` from `DialValues`** —
      it is a required field, so every construction site breaks loudly, which is
      the intent. `revealStartOffset.max` stops depending on it and becomes the
      constant `MAX_REVEAL_HORIZON_BLOCKS − 7,200`; `isPinned` simplifies with it;
      drop the dial's branches from `dialError`/`dialValue`.
    - `protocol/txclient.ts`: `requestGuardians` takes a `revealStartOffset`
      instead of a `revealWindow` object, and the `dialErrors` call at `:357`
      loses `revealDurationBlocks`.
    - Tests: `dials.test.ts`, the pinned-constant suite, tx-validation, and
      `creation-quote.test.ts` — its `distanceBlocks` fixtures (`:28-29`) are
      built on a creator-chosen duration.
11. **`mobile-client/app`** — every duration reference goes, and the price
    estimate starts calling the SDK rather than adding a draft field:
    - `wizardState.ts`: remove `DEFAULT_REVEAL_DURATION_BLOCKS` (`:52`), the
      `revealDurationBlocks` draft field and its `set-reveal-duration` action.
      **`ADVANCED_LIMITS` holds only this one entry (`:56-60`), so the whole
      structure goes.** `openCeilingMs` (`:133`) and `checkOpenBounds` lose their
      duration parameter and the ceiling becomes a constant. The estimate's
      `distanceBlocks` (`:522`) must call the SDK's `distanceBlocks` helper.
    - `TimingScreen.tsx`: delete the reveal-duration `Stepper` (`:279-298`). The
      Advanced disclosure **survives** — it still carries the exact-blocks offset
      stepper — but its header copy ("Advanced ▾ · open window", `:274`) and the
      explanatory `Body` at `:313-317` are both written about the duration and
      need rewriting for a derived window.
    - `RenewScreen.tsx:31`: drop the local literal; the renewed seal's window
      follows from its offset.
    - `compose.ts:59` (devnet default) and `sealDriver.ts:95`,`:110`
      (pass-through and bounds-assertion argument).
    - Show the derived window read-only on the Timing screen. This is a
      compose-time display point only — while the creator is still picking an open
      date, the estimate they are watching moves with it, because the window is
      part of the paid distance. Once `MsgUserRequestGuardians` is submitted the
      window is fixed and nothing about it changes again.
    - Tests: delete `wizardState.test.ts:304` outright — it asserts a 14,300-block
      ceiling difference between a short and a long window, and with one derived
      window there is no such comparison to repair. Also `:497` (the 300 default),
      `sealDriver.test.ts:173`,`:200`, and any estimate expectation that moves
      because distance now comes from the curve.
12. Repack the vendored SDK tarball (`pack-sdk.sh`) and sync the mobile lockfile
    integrity hash — without this the mobile e2e run keeps testing the old bounds.
13. `devnet/` scenarios and `mobile-client/e2e`: drop the argument wherever a
    reveal window is constructed. Suite *runtime* is not at risk — the derived
    floor of 50 is half the old minimum of 100, so short-secret scenarios get
    faster, not slower. The risk runs the other way: a scenario that asserts a
    specific `reveal_end_block`, or that races a reveal against a window it
    assumed was 100+ blocks wide, now has less room. Check the reveal-timing
    assertions rather than the wall-clock budget.
14. Cross-component grep for `duration` / `revealDuration` / `RevealDuration` /
    `REVEAL_DURATION` / `MinRevealDuration` / `MaxRevealDuration`, confirming each
    listed component is either changed or confirmed clear. Guardian and crypto are
    expected clear — confirm, do not assume.
15. Run the release train in `PROTOCOL_CHANGE.md`: this touches `proto/`,
    `x/secrets/types` validation bounds *and* the chain-semantics vectors, so all
    seven steps apply, both tag namespaces move, and the `COMPATIBILITY.md` row is
    added only after `make e2e && make e2e-scenarios` pass against the pinned
    artefacts.

## 7. Plans this collides with

- [PENDING_E2E_SCENARIO_DETERMINISM_PLAN.md](PENDING_E2E_SCENARIO_DETERMINISM_PLAN.md)
  — its §4 constants table lists `MinRevealDuration` and `MaxRevealDuration`, and
  its shrink analysis concludes "**duration**: no scope. Already at
  `MinRevealDuration`". Both statements are retired by this change, and the
  conclusion moves in that plan's favour: at the offset floor of 100 the derived
  window is 50 blocks, half the old minimum, so every scenario it measures gets
  *shorter* rather than longer. **Not edited here** — it is being executed on the
  `e2e-scenario-determinism` branch, so touching it from this worktree would
  collide. Whichever lands second re-measures against the other.

- [PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md](../../../mobile-client/docs/planning/PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md)
  — §7 Q2 rules the wizard's `revealDuration` Advanced dial and its 300-block
  default, and its §2 pricing table teaches the wizard to include the window span
  in `distance`. The dial ruling is superseded; the pricing point survives and in
  fact becomes more important, since the span now moves with the offset the
  creator is adjusting. Sweep both.
- [PENDING_CONSTANTS_SYNC_PLAN.md](../../../mobile-client/docs/planning/PENDING_CONSTANTS_SYNC_PLAN.md)
  — owns how mobile stays in step with chain constants. A derived window is the
  first case where mobile must mirror a chain *computation* rather than a value;
  confirm that plan's mechanism covers it, or record the gap there.
- [DONE_ADVANCED_DIALS_PLAN.md](../../../mobile-client/docs/planning/done/DONE_ADVANCED_DIALS_PLAN.md)
  — records the dial's `affects: ['reliability', 'cost']` descriptor. Completed,
  so not edited; noted because it is where the dial's rationale lives.
- [done/DONE_FIXED_COMMIT_TIMEOUT_PLAN.md](done/DONE_FIXED_COMMIT_TIMEOUT_PLAN.md)
  — the precedent for removing a timing dial. Its §6 item 8 re-anchored a
  `dials.json` case named `maximum commit window and reveal duration`; that case
  is touched again here.
