# Fixed Commit Timeout — Plan

*The creator chooses how long guardians have to accept, anywhere from 2 to 20
minutes. Nothing they could weigh justifies the field, and it varies the one window
in which a secret cannot be cancelled. Replace it with a single protocol value.*

> **Status: done — 1 August 2026.** Executed on
> `worktree-fixed-commit-timeout`, merged as
> [PR #128](https://github.com/leedavis81/timeflare/pull/128). Every work item
> in §6 landed; the guardian daemon needed no change, confirmed by the sweep
> rather than assumed. §4's open runtime question is answered: guardian
> acceptance completes well inside the 50-block window at
> `TIMEFLARE_BLOCK_TIME=1s`, so the suites' block time did not need loosening.
>
> **Priority**: P2 — protocol surface, pre-testnet. Removing a wire field is
> cheap now and expensive later.
>
> **Origin**: constants sweep, 31 July 2026. Owner ruling: collapse
> `MinCommitTimeout`/`MaxCommitTimeout` to a single value of **5 minutes**.
> Prompted by the cancellation blind spot found while explaining the secret state
> machine — cancellation requires `pending`, which is only reached at
> `commit_deadline`, so the commit window is time in which a creator has no exit.
> Retry latency — the one counter-use the spec records for a short timeout — was
> ruled not a valid concern (owner, 31 July 2026); see §1.
>
> **Components**: `proto/timeflare/secrets/v1/tx.proto`, `x/secrets/types`
> (constants, `ValidateBasic`), `x/secrets/keeper` (window calculation),
> `x/secrets/client/cli/tx.go` (the hand-written `--commit-timeout` flag),
> `docs/spec.md`, `docs/PROTOCOL.md`, `docs/operations.md`,
> `testdata/vectors/dials.json` (the pinned cross-implementation corpus) and its
> vendored copy under `typescript-sdk/src/vendor/vectors/`, `typescript-sdk/`
> (message construction, the `DIALS` descriptor table, gas/pricing helpers),
> `mobile-client/app/` (the Timing screen, the wizard draft, the commit-session
> countdown), `devnet/` scenario scripts and `mobile-client/e2e/`.
>
> **Related plans**: reverses the `commit_timeout` dial exposed by
> [DONE_ADVANCED_DIALS_PLAN.md](client-app/DONE_ADVANCED_DIALS_PLAN.md); lands
> ahead of [PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md](client-app/PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md)
> and [PENDING_MOBILE_OUTCOME_SURFACES_PLAN.md](client-app/PENDING_MOBILE_OUTCOME_SURFACES_PLAN.md),
> both of which still encode the 20–200 range (§7).

## 1. Why the dial does not earn its field

The commit timeout sets `commit_deadline = height + commit_timeout`: the window in
which selected guardians accept, finalised in `EndBlock` at the deadline.

The creator is offered 20–200 blocks. What would they trade?

- **Longer** supposedly improves the odds of filling the band. But guardian
  acceptance is automated — the daemon polls, decrypts, verifies and submits
  within seconds. Minutes of extra runway buy nothing a few seconds has not
  already secured.
- **Shorter** arms the secret sooner. But nothing is waiting on it: the secret
  cannot open until `reveal_start_block`, which is separately chosen and at least
  50 blocks out.
- **Shorter also shortens a retry.** A secret that fails to fill its band refunds
  only at `commit_deadline`, so a creator who must start over waits out the
  window first — the one trade the spec actually records (`docs/spec.md`,
  Retry semantics). It does not earn a protocol field. Acceptance is automated
  and lands within seconds, so an unfilled band is the rare path, and on that
  path the creator is blocked on nothing but their own decision to try again.
  Buying three minutes back on an exceptional case does not justify a
  creator-facing dial on every request. The *reason* the wait has no early exit
  stands and is worth keeping: it is what prices abandoned selection draws.

So the range varies the deadline tenfold while changing neither the outcome nor
anything the creator can perceive. The mobile client's own default (60 blocks) has
never needed to be anything else, which is the tell.

**What it does vary is the blind spot.** Cancellation is `pending`-only, and
`pending` begins at the commit deadline — so from submission until then, a creator
has no exit. Today a creator can pick 200 blocks and be committed for twenty
minutes without being told that is the trade they made. The creator's real levers
on guardian participation are the band (`min_shares`, `max_shares`) and the
`bump`, both of which change the economics; this one only changes the clock.

## 2. The value

**50 blocks — 5 minutes at the production 6s cadence** (owner ruling, 31 July
2026).

The constant is denominated in **blocks**, so the wall-clock term scales with the
chain's actual cadence, exactly as `MinRevealStartOffset` and the retention
windows already do. "5 minutes" describes it at 6s and is not a promise the
protocol can keep at another cadence.

Replaces both bounds with one `CommitTimeoutBlocks`. No *interval* is derived
from the pair, so there is no range arithmetic to unpick — the bounds themselves
are read only by the two comparisons in `ValidateBasic`, the equivalent pair in
the keeper, and the test fixtures built from `MinRevealStartOffset +
MaxCommitTimeout`, which collapse to a single term.

What *is* derived from the chosen value is the reveal-offset floor,
`commit_timeout + MinRevealStartOffset`, and it is expressed in four independent
places that must agree: the keeper's `validateRevealWindow`, the
`reveal_start_offset_blocks` rule in `testdata/vectors/dials.json`, the SDK's
`revealStartOffset.min` in the `DIALS` table, and the mobile e2e context. With
the timeout constant each of those becomes a literal — the relationship stops
being a computation and the four copies stop being able to disagree.

## 3. What this simplifies downstream

- **`ValidateBasic`** loses a range check and the `commit_timeout` field with it.
- **Pricing becomes more predictable.** `distance = (reveal_end_block + 1) −
  commit_deadline` feeds both the reward pool and every bond. With the deadline
  fixed relative to the creation height, distance is a function of the reveal
  schedule alone.
- **The start-offset floor loses a variable.** `start_offset` must clear
  `MinRevealStartOffset + commit_timeout`; with a constant, the minimum legal
  offset is a constant too, and can be stated in the client rather than computed.
- **The mobile Timing screen loses a control** and the wizard draft a field.

## 4. Risks and what execution must confirm

**A short deadline on a fast devnet.** The constant is blocks, so a devnet at
`TIMEFLARE_BLOCK_TIME=1s` gets 50 *seconds*, not five minutes. Execution must
confirm that the guardian daemon's poll-and-accept cycle reliably completes
inside 50 blocks at 1s — and if it does not, the honest fix is the suites' block
time, not a looser protocol.

**The suites converge on one window, and one of them slows down.** Today the
three test paths each pick their own: the devnet scenarios take the CLI default
of 110 blocks, the mobile lifecycle suite passes 60, and
`mobile-client/e2e/test/abandoned-commit.test.ts` deliberately passes the
minimum 20 so its deadline expires quickly. A fixed 50 shortens the first two and
**lengthens the third** — that suite exists to wait out a commit deadline, so its
wait grows from 20 blocks to 50 (~50s at 1s). That cost is accepted and takes no
test-only escape hatch (owner ruling, 31 July 2026): an override of the constant
for the benefit of a suite is the production-code-for-tests concession CLAUDE.md
rules out, and ~50s is tolerable. One window across every suite is the gain.

**The blind spot narrows at the top and widens at the bottom.** Against today's
200-block ceiling five minutes is a clear improvement, but against the 20-block
floor it is a regression: a creator who picks the minimum today is committed for
two minutes, and after this change for five. Fixing the value trades a
creator-tunable exposure for a uniform one, which is the point — it is not a
strict reduction, and should not be described as one. The gap itself belongs to
the `pending`-only cancellation rule, which this plan does not touch; if it should
be closed rather than bounded, that is its own concern.

**Field removal needs a grep-driven sweep.** `commit_timeout` crosses proto, the
types module, the SDK, the mobile draft, the devnet scenario scripts and the
mobile e2e suite. Per CLAUDE.md's ShareIndex lesson, execution confirms which
components are clear, not only which ones change.

## 5. Not solved

- **The `pending`-only cancellation rule.** Narrowing the window is not the same
  as giving a creator an exit during it.
- **`MinRevealDuration`/`MaxRevealDuration`**, the other range the sweep found
  worth collapsing. Same argument, different field, and deliberately a separate
  plan: it feeds `distance` directly, so it changes pricing in a way the commit
  timeout does not. That plan is not yet authored; this one does not depend on it,
  and the two touch the same `dials.json` cases, so whichever lands second
  re-anchors them.
- **Re-anchoring the deadline.** The anchor does not change: `commit_deadline`
  stays the height at which the request lands plus the constant, exactly as today
  (owner ruling, 31 July 2026). Anchoring it to the reveal schedule instead would
  make `distance` constant rather than merely predictable, but it would move every
  bond calculation — a far larger blast radius than removing a field, and not this
  plan's concern. This plan changes only the *term*, never what it is added to.

## 6. Work items

Spec first, per the spec-first rule — this changes protocol behaviour.

1. `docs/spec.md`: the commit phase is a fixed 50-block window; remove
   `commit_timeout` from the message documentation, the `Secret` struct comment
   and the parameter tables. In Retry semantics, drop the advice to choose a short
   timeout and keep the reason the wait has no early exit (§1). In the `distance`
   derivation, `distance ≤ H + 1 − commit_timeout` becomes a fixed bound.
2. `docs/PROTOCOL.md`: the timeout line in the dial summary and the
   `MinCommitTimeout`/`MaxCommitTimeout` row in the parameter table.
3. `docs/operations.md`: drop the `commit_timeout` field row, restate
   `commit_deadline`, and **fix the pre-existing error** in the
   `reveal_window.start_offset` row — it states the floor as
   `commit_timeout + 100`, where the code and PROTOCOL.md say
   `commit_timeout + 50`. The corrected floor is the constant 100.
4. `proto/`: remove `commit_timeout` from `MsgUserRequestGuardians`; regenerate.
5. `x/secrets/types`: replace `MinCommitTimeout`/`MaxCommitTimeout` with
   `CommitTimeoutBlocks = 50`; drop the range check from `ValidateBasic`; update
   fixtures built from `MinRevealStartOffset + MaxCommitTimeout`.
6. `x/secrets/keeper`: derive `commit_deadline` from the constant; drop the
   duplicate range check and the `commitTimeout` parameter from
   `validateRevealWindow`, whose floor becomes a literal.
7. `x/secrets/client/cli/tx.go`: remove the `--commit-timeout` flag, its
   `defaultCommitTimeout`, and the flag's line in the command help text.
8. `testdata/vectors/dials.json`: remove the `commit_timeout_blocks` bound entry
   and the field from every case; restate the `reveal_start_offset_blocks` rule as
   a constant floor; **delete the two cases that exist only to test the removed
   bounds** (`commit timeout too short`, `commit timeout too long`) and re-anchor
   the cases whose names or reasons cite the range (`maximum commit window and
   reveal duration`, `offset exactly at commit_timeout + buffer`). Update the
   corpus description, which documents the keeper's stricter rule in terms of
   `commit_timeout`. Re-sync the vendored copy under
   `typescript-sdk/src/vendor/vectors/`.
9. `typescript-sdk`: drop the parameter from message construction and the quote
   helpers; remove the `commitTimeout` entry from the `DIALS` descriptor table and
   the `'commitTimeout'` member of its id union; make `revealStartOffset.min` a
   constant; drop `COMMIT_TIMEOUT_MIN_BLOCKS`/`COMMIT_TIMEOUT_MAX_BLOCKS`; update
   the pinned-constant, dials, session and tx-validation tests.
10. `mobile-client/app`: remove the Timing control and the draft field; sweep the
    commit-session countdown, compose state and seal driver; the minimum legal
    `start_offset` becomes a constant.
11. Repack the vendored SDK tarball (`pack-sdk.sh`) and sync the mobile lockfile
    integrity hash — without this the mobile e2e run keeps testing the old bounds.
12. `devnet/` scenarios and `mobile-client/e2e`: drop the argument, retire the
    `abandoned-commit` suite's minimum-timeout comment, and confirm the 50-block
    deadline holds at the suites' block time (§4).
13. Cross-component grep for `commit_timeout` / `commitTimeout` /
    `CommitTimeout` / `COMMIT_TIMEOUT`, confirming every listed component is
    either changed or clear.
14. Sweep the two pending mobile plans (§7) so neither still specifies a dial the
    protocol no longer has.

## 7. Plans this collides with

The dial being removed was deliberately built, so the reversal is not confined to
code:

- **[DONE_ADVANCED_DIALS_PLAN.md](client-app/DONE_ADVANCED_DIALS_PLAN.md)**
  exposed `commit_timeout` as an advanced dial (step 10 over 20–200,
  `affects: ['reliability']`) and introduced the `DIALS` descriptor table that
  encodes it. A completed plan is a decision log and is not rewritten; this plan
  supersedes that one dial and leaves the descriptor pattern — which is what made
  the reveal-offset floor executable rather than prose — intact.
- **[PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md](client-app/PENDING_MOBILE_UX_PROTOCOL_ALIGNMENT_PLAN.md)**
  specifies a `commitTimeout` Advanced dial with a 60-block default and copy
  computed *from the chosen timeout* (its §5.1.5 and the wizard defaults). Those
  items dissolve rather than change: with a constant the timing floor is fixed
  copy, not a computation.
- **[PENDING_MOBILE_OUTCOME_SURFACES_PLAN.md](client-app/PENDING_MOBILE_OUTCOME_SURFACES_PLAN.md)**
  cites `COMMIT_TIMEOUT_MAX_BLOCKS` = 200 in its countdown reasoning.

This plan lands **first**. It is protocol surface, it deletes work from both
pending plans rather than adding any, and executing either one first would build
a control this plan then removes.
