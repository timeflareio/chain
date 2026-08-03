# Guardian In-Flight Submissions — Plan

*The daemon re-broadcasts a transaction it has already sent, because it treats
"not yet on chain" as "not yet done". Every accepted assignment costs a wasted
transaction fee.*

> **Status: done — 31 July 2026**, branch `worktree-guardian-inflight`, PR #126.
> Every open question ruled; the expiry window is fixed at five blocks (§3). All
> five work items landed, with the guardian suite green under `-race` and
> `make verify` clean both locally and in CI. Proven on the native devnet: the
> same fleet that broadcast two acceptances per secret broadcast exactly one per
> guardian after the change, with no rejected duplicates.
>
> **The S10b assertion has never executed.** The devnet scenario suite is opt-in
> in CI — it runs only on a pull request carrying the `e2e` label (`ci.yml`) —
> and the local devnet was in use, so the scenario added by work item 5 is backed
> only by a dry-run of its logic against a live settled secret. The FIX is
> evidenced by the devnet before/after and by the unit suite; the regression
> guard that would catch a recurrence is not yet proven in the environment that
> will enforce it. Running `make e2e-scenarios` once on a fresh devnet, or
> labelling a later PR `e2e`, closes this.
>
> **Priority**: P2 — a systematic, unreimbursed cost on every guardian for every
> secret. Not a correctness fault (the chain rejects the duplicate), but it
> silently inverts the economics the accept-fee curve was designed to balance.
>
> **Origin**: found 31 July 2026 while testing the owner's claim that guardian
> gas costs are covered by the accept/reveal reimbursements. They are — exactly.
> The deficit came from somewhere else, and the chain ledger identified it.
>
> **Components**: `guardian/guardian/` (the submission path and the active-secret
> cache), `guardian/guardian/*_test.go`, `devnet/e2e-scenarios.sh`. No chain,
> proto or `docs/spec.md` change — the protocol behaves correctly; the client
> sends a transaction it should not.

## 1. Evidence

Guardian-01 on the native devnet, after two settled secrets, sat 43,476 uveil
below its post-registration baseline. Its complete transaction history accounts
for every uveil:

| Height | Transaction | Fee paid | Reimbursed | Net |
|---|---|---|---|---|
| 5 | register | 18,763 | — (one-off, by design) | −18,763 |
| 462 | accept | 12,000 | 12,000 | **0** |
| 462 | accept — **failed, code 24** | 12,000 | 0 | **−12,000** |
| 600 | reveal | 13,747 | 13,401 | −346 |
| 889 | accept | 12,000 | 12,000 | **0** |
| 889 | accept — **failed, code 24** | 12,000 | 0 | **−12,000** |
| 1048 | reveal | 13,747 | 13,381 | −366 |

The failures carry the chain's own diagnosis:

```
guardian already responded with status: ASSIGNMENT_STATUS_ACCEPTED
```

**The reimbursement curve works.** `GuardianAcceptGas` (120,000) at the floor
price is 12,000 uveil, and `accept_fees ÷ max_shares` is 12,000 uveil — exact.
The covered legs break even to the uveil, and the deficit is one-off registration
gas plus 24,000 uveil of duplicates.

Every secret produced exactly one duplicate. This is systematic, not a race that
occasionally bites.

## 2. Why it happens

`acceptAssignment` documents the design assumption, and it is half true:

> *broadcast success only means the tx passed CheckTx … The next cache refresh
> reconciles against on-chain state, so a failed acceptance self-corrects.*

Correct for a submission that **failed**. Wrong for one still **in flight**:

1. `UpdateFromBlockchain` reads chain state — the assignment is `PENDING`, so it
   stays in `awaitingConfirmation`.
2. `processConfirmations` decrypts the share, verifies the HMAC, broadcasts the
   accept. Broadcast returns after CheckTx; the transaction is not in a block.
3. The next poll cycle runs. Chain state still says `PENDING`, because nothing
   has been included yet.
4. The same assignment is processed again, and a second accept is broadcast.

Both land in the same block — which is exactly what the ledger shows. There is no
in-flight record anywhere in `cache.go` or `reveal.go`: nothing distinguishes
"never submitted" from "submitted, awaiting inclusion".

It therefore fires whenever a poll cycle completes within one block time.
`PollingInterval` and `BlockTime` both default to six seconds
(`guardian/config/config.go`) and the cycle does no waiting, so that is the
normal case, not an edge one.

The duplicate is also the expensive thing to repeat: the full decrypt and HMAC
verification runs before the broadcast, so it burns the guardian's CPU as well as
its fee.

**Reveals are partly covered already, by a different mechanism.** A successful
`ProcessReveal` calls `MarkRevealed`, and `shouldEvict` treats `StateRevealed` as
evictable, so the secret leaves the cache; the next `UpdateFromBlockchain` re-adds
it only if the chain still shows no reveal from this guardian. That costs a
duplicate reveal roughly two poll cycles rather than one — which is the whole of
why reveals are "less likely" — and it is also what makes a dropped reveal
self-correct today. Both facts constrain the guard below.

## 3. The guard

Record each broadcast submission locally, keyed by secret and kind (accept /
reject / reveal), and skip a secret while one of its submissions is in flight.
That fills the gap between "broadcast" and "reconciled" without changing what
either step means.

Three properties make it a fix rather than merely fewer duplicates:

- **It expires after five blocks.** A permanent "already sent" flag would strand
  a guardian whose transaction was dropped from the mempool and never included —
  it would wait forever for a reconcile that cannot come, and miss its window.
  Entries carry the height they were broadcast at and lapse five blocks later.
  Five is chosen against both bounds: inclusion normally takes one block, so it
  absorbs ordinary congestion, while leaving fifteen of the twenty-block
  `MinCommitTimeout` for a genuine retry. It must also stay under the reveal
  path's existing evict-and-re-add recovery (§2), or the guard would convert a
  case that self-corrects today into a missed window — and a missed reveal costs
  50% of the bond, far more than the duplicate fee this plan exists to save.
- **It does not suppress a genuine retry.** A submission that fails in DeliverTx
  must be retryable while the window is open; that self-correction is what the
  existing comment describes and it has to keep working. The guard covers the
  *unknown* interval only.
- **It is per (secret, kind).** An accept in flight must not block that secret's
  later reveal, which is a different transaction at a different time.

**Reveals get the same guard.** The reveal window is long enough that overlapping
cycles are less likely, but "less likely" is not a guarantee and a wasted reveal
costs 13,747 uveil — more than a wasted accept. Whether duplicates are actually
observed on reveals is a question for execution to answer with the ledger, not to
assume either way. The guard sits alongside `MarkRevealed` rather than replacing
it: that flag drives cache eviction, which is a separate concern from whether a
transaction is outstanding.

### 3.1 Three properties of the implementation

- **In-memory and per-process**, cleared on restart — the stance
  `observations.go` already documents for the daemon's other local records. A
  restart mid-flight costs one duplicate, which is rare and self-corrects on the
  following cycle. Persisting it would buy a fraction of one fee at the price of
  a durable store the daemon does not otherwise need.
- **Not built on `Observations`.** That buffer already records every broadcast
  keyed by kind and secret, so it looks like the natural home, but it is an
  explicitly best-effort 256-entry ring whose contract is that recording "must
  never block or fail the work it describes". A correctness-bearing guard cannot
  sit on a lossy structure. The registry is a small separate type in the same
  package — no new component, and the two stay independent.
- **Reserve and report in one locked operation.** Reveals fire from both the poll
  loop and `onNewHeight`, and `processReveals` runs workers in parallel, so a
  "consult, then submit" sequence is a time-of-check race that would reintroduce
  exactly the duplicate being fixed. The check and the record are atomic.

## 4. Not solved

- **The reveal reimbursement shortfall.** `GuardianRevealGas` is 130,000, so the
  basis is 13,000 uveil, but the daemon declares 137,466 and pays 13,747 — the
  guardian eats roughly 350 uveil per reveal. That is a constant that has drifted
  from what the client actually declares, not a duplicate-submission fault, and
  it needs its own plan. Recorded here because the same investigation found it
  and it would otherwise be lost.
- **Registration gas** (18,763 uveil) is deliberately unreimbursed: a one-off
  cost of entering the market, alongside the entry fee.
- **Nothing checks that the gas constants still match the daemon.** The comment on
  `GuardianAcceptGas`/`GuardianRevealGas` says they are measurements of the
  protocol's own code path, but no test asserts the daemon's declared limits
  against them, which is how the reveal figure drifted unnoticed.

## 5. Work items

1. An in-flight registry on the submission path: reserve-and-record atomically at
   broadcast, keyed by (secret, kind) with the broadcast height; clear on
   reconcile to a terminal assignment status. Per §3.1 — in-package, in-memory,
   independent of `Observations`.
2. Expiry five blocks after the recorded height, so a dropped transaction is
   retried rather than stranded.
3. Apply it to reveals as well as accepts and rejects, alongside the existing
   `MarkRevealed` eviction rather than in place of it.
4. Unit tests in `guardian/guardian/`: a second submission inside the in-flight
   window is suppressed; one after expiry is not; a DeliverTx failure still
   retries; an accept in flight does not block that secret's reveal; and
   concurrent submissions of the same (secret, kind) yield exactly one broadcast,
   which is the property the atomic reserve exists for.
5. A devnet assertion that a settled secret produces **exactly one** accept
   transaction per participating guardian, and no `code 24` failures — counted
   per guardian address on an existing settled-secret scenario rather than in a
   new one. This is the check that would have caught it: the existing suites
   assert the protocol outcome and never look at what the daemon spent getting
   there.
