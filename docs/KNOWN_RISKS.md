# Timeflare Known Risks

*The register of what is known to be risky, unfinished or knowingly given up in
the chain: open security concerns, open defects, gaps with no owner, places
where prose and code disagree, optimisation candidates, and the costs the
protocol has consciously accepted.*

> **Scope**: [spec.md](spec.md) is the protocol authority, and states the
> rules. [operations.md](operations.md) is the operation-level truth.
> [CHAIN_MECHANICS.md](CHAIN_MECHANICS.md) describes how the chain is built
> inside and what it measures. This document holds the judgements about all of
> it. Open design work lives in [docs/planning/](planning/) and is referenced
> per item.

## Table of Contents

1. [How this register works](#how-this-register-works)
2. [Security Observations](#security-observations) — open, not yet ruled
3. [Open Defects & Cleanup Notes](#open-defects--cleanup-notes)
4. [Gaps With No Owner](#gaps-with-no-owner)
5. [Deviations From Documented Intent](#deviations-from-documented-intent)
6. [Inefficiencies — Optimisation Candidates](#inefficiencies--optimisation-candidates)
7. [Accepted Trade-offs](#accepted-trade-offs) — ruled
8. [Load-Bearing Oddities](#load-bearing-oddities--deliberate-decisions-worth-revisiting)
9. [Design decisions — the questions readers ask](#design-decisions--the-questions-readers-ask)
10. [Risks stated in the specification](#risks-stated-in-the-specification)

---

## How this register works

One register, seven statuses — an item moves between sections as rulings
happen, never between documents.

- **Security Observations** are open concerns with no ruling yet. When one is
  ruled acceptable it moves to Accepted Trade-offs.
- **Open Defects** are things we intend to fix. A fixed defect is deleted.
- **Gaps With No Owner** are known gaps where the fix is a decision rather
  than a mechanical change, and no plan has taken them. One that gains a plan
  is deleted here and lives in [docs/planning/](planning/).
- **Deviations From Documented Intent** are places where prose and code
  disagree, awaiting alignment.
- **Inefficiencies** are optimisation candidates.
- **Accepted Trade-offs** are ruled: the cost is known and consciously taken.
- **Load-Bearing Oddities** are deliberate structure that looks wrong but
  carries no accepted cost.

Entry numbers are stable: other documents cite "Trade-off §13" or "Security
Observation §1", so an entry keeps its number for as long as it exists.

Two things this register does not hold. The **normative** statement of a risk
— what the protocol does about an attack — stays in [spec.md](spec.md), and
[the last section](#risks-stated-in-the-specification) indexes those entries
without repeating them. And risks in the **guardian daemon** are recorded in
that repository (`timeflareio/guardian` —
`docs/planning/PENDING_GUARDIAN_PRE_TESTNET_SWEEP_PLAN.md`).

## Security Observations

Every entry below is a concern that is **open** — reachable, unresolved, and not
covered by an existing ruling. Anything ruled acceptable moves to
[Accepted Trade-offs](#accepted-trade-offs); anything fixed is deleted (git
history keeps it).

### 1. EndBlock settlement work is unmetered and uncapped

Genesis sets `consensus.params.block.max_gas = 75,000,000` (August 2026 —
[DONE_CONSENSUS_BLOCK_GAS_LIMIT_PLAN.md](planning/done/DONE_CONSENSUS_BLOCK_GAS_LIMIT_PLAN.md)),
so gas now bounds what one block of *transactions* may execute. EndBlock sits
outside that bound: BeginBlock/EndBlock run with an infinite gas meter by SDK
design. `ProcessDuePrunes` caps itself at `MaxPrunesPerBlock`;
`ProcessExpiredCommits` and `ProcessExpiredRevealWindows` have no equivalent
cap, and every secret due at a height settles in that height's EndBlock, each
walking its accepted set doing bank sends, bond releases and possibly
slashes. That the retention path *is* capped and the settlement path is not
is an inconsistency inside one file, not a considered asymmetry.

**Why it is reachable rather than theoretical**: `reveal_end_block` is
creator-chosen, so settlement heights can be made to converge deliberately.
The block gas limit bounds the *creation* burst per block (~400 narrowest-shape
creations), but creations spread across many blocks can still share one
`reveal_end_block`, so the settlement concentration survives the gas limit.
The unrecoverable outlay is only the creation fee plus gas — ~0.10 VEIL per
secret at the narrowest band, since the pool and accept fees come back
(refunded on failure, or paid to the actor's own guardians). Measured
1 August 2026: a commit-expiry sweep of 470 secrets in a single EndBlock cost
less than the ~10 ms block-interval measurement noise on a development
workstation, so today's exposure sits far from the block interval — the gap
is structural, not currently painful.

Correctness is not at risk: the per-secret `CacheContext` keeps every
settlement all-or-nothing, and the failure model is already quarantine-not-halt
([Trade-off §12](#accepted-trade-offs)). The exposure is block
time, and therefore liveness.

**The fix is not free.** A per-block settlement cap means deferring a
settlement past its due height, which delays guardian payouts and extends
bond locks — so it needs a fairness rule (oldest-due first) and a ruling on
how long a guardian can be asked to wait beyond `reveal_end_block + 1`. An
owner call.

### 2. Assignment responsiveness is neither measured nor priced

A selected candidate that simply never responds pays nothing: no transaction,
no bond locked, no slash, and — critically — **no `k` movement**. The bond
multiplier moves only on a slash (up) or a correct reveal (down), so a guardian
that ignores nine assignments out of ten holds exactly the standing of one that
accepts every assignment it is offered. Selection probability is unaffected
too, by design — see the sortition row in
[operations.md, Hard-Coded Protocol Values](operations.md#hard-coded-protocol-values): `k` prices
bonds, never candidacy.

The consequence is that **reliability is invisible**. Creators cannot select
for it, the protocol cannot reward it, and a guardian faces no cost for
treating assignments as optional — while still collecting on the ones it
chooses to take. This is not primarily an attack surface: a Sybil adversary
would sink 1,000 VEIL per address to impose roughly a creation fee per forced
failure, which is economically absurd. The realistic case is ordinary
free-riding and ordinary downtime, which are indistinguishable from each other
and from malice.

What it costs the creator: if non-responders exceed `max_shares − min_shares`,
the secret fails at the commit deadline. Pool and accept fees are refunded in
full, but the **creation fee is not** — so every attempt costs the creator the
draw price plus gas, and costs a silent candidate nothing. The gap bound caps
the slack a creator can buy: with `max − min < threshold`, a 2-of-5 shape
tolerates only one silent candidate.

The variable-quorum design treats this as the creator's dial and documents the
robustness range that follows
([planning/done/DONE_VARIABLE_QUORUM_PLAN.md](planning/done/DONE_VARIABLE_QUORUM_PLAN.md)
— "`min` close to `threshold` is doubly fragile"), and a high `threshold` with a
wide band genuinely mitigates it (16-of-32 tolerates 15 silent candidates). What
that plan does not address is the *persistence*: it models non-response as a
random per-secret failure, whereas it is a stable per-guardian property the
protocol declines to record. Pricing it directly would need bond-at-selection,
which is ruled out ([Trade-off §4](#accepted-trade-offs)); a
response-rate statistic that creators could read, or that fed `k`, is the
cheaper direction and is new mechanism requiring a ruling.

### 3. Phase-1 gas grows with the ELIGIBLE guardian set, and the creator declares it

Candidate enumeration is metered inside the *creator's*
`MsgUserRequestGuardians` gas, and the SDK declares a fitted **constant** model
(`typescript-sdk/src/protocol/constants.ts`, pinned by `gas-model.test.ts`
against `testdata/vectors/tx_gas.json`). Any term in that cost which grows with
the network eats the model's margin until phase 1 aborts out of gas — for
**every** creator, on a purely organic trigger, with the fee still deducted
because the ante handler charges `gas_limit × gas_price` and does not refund.

Enumeration range-reads the eligibility index keyed
`(available_until, guardian_address)` from the reveal end, so a registration that
cannot serve the window costs **nothing at all** — 400 lapsed registrations add
exactly zero — and each eligible candidate costs **360 gas**. The growth term
therefore tracks the *eligible* set: it is reached by healthy participation, and
no longer moves when a guardian's window lapses.

**It is narrowed, not closed.** Against the declared model
(`167,000 + 5,400 × max_shares`, ~25% margin over a 40-eligible measurement), the
margin is exhausted at roughly **139 eligible guardians at `max_shares` 2, 154 at
7, and 228 at 32**. A narrow band therefore aborts at 140 eligible guardians,
which a testnet reaches.
[CHAIN_MECHANICS.md, Performance & Scaling §3](CHAIN_MECHANICS.md#3-guardian-selection--oeligible-and-why-it-cannot-be-less)
holds the measured
complexity and the derivation of those figures.

**Widening the margin is not the fix.** Cosmos charges the declared limit, so a
fatter margin taxes every creator on every secret to defer the boundary, and
`gas-model.test.ts` rejects it (`MIN_MARGIN` 1.1, `MAX_MARGIN` 1.6 — bounded above
as well as below, and the model currently sits at 1.25). The fix is a declaration
that scales with the eligible count. The inputs are already on the wire —
`Query/Guardians` returns every field the predicate needs — but there is **no
eligible-count query and no server-side filter**, so a client must page the whole
registry and apply the predicate itself — the O(registered) walk the index keeps
out of consensus, run off-chain where it is unmetered and cacheable rather than
charged to every creator. A client-model change, unwritten, and the reason this
stays on this list.

Selection is O(eligible) by construction and cannot be less: sortition takes the
`max_shares` lowest tickets, so every candidate's ticket must be computed. Going
below that means sampling candidates, which changes the byte-exact eligibility
predicate and the equal-probability claim resting on it.

### 4. A secret's creator can slash its guardians with evidence it already holds

`MsgSlashGuardian` accepts a report when the submitted bytes match the
guardian's stored share HMAC. The creator of a secret builds every guardian's
share envelope and computes every stored HMAC, so it holds valid "evidence"
against each guardian from the moment of distribution. Once a guardian has
accepted — and its bond is therefore locked — the creator can report it for an
early reveal it never made, and the report verifies.

**What the handler checks** (`x/secrets/keeper/msg_server_slash_guardian.go`):
the reporter signed the transaction and is not the guardian's own address; the
reveal window has not opened; the guardian holds an accepted assignment; it has
not already been slashed or revealed for this secret; the HMAC matches. Nothing
excludes the creator, and a rule that did would be one second address away from
meaningless — the same reasoning that sinks every reporter restriction in
[Trade-off §13](#13-availability-must-cover-the-whole-reveal-window--no-handoff-ever).

**What it costs the guardian**: the full bond for that secret, a step up in its
bond multiplier `k`, and exclusion from the reward pool at settlement. The
split is 40% burned, 10% to the creator and 50% to the reporter, so a creator
reporting through its own address collects 60% of each bond it destroys. Every
accepted guardian on a secret is exposed from acceptance until
`reveal_start_block`.

**What bounds it**: the take is at most 60% of the bonds on the creator's own
secret, against a creation fee that is not refunded and a reward pool it
funded. Whether that nets positive across the permitted shapes has not been
worked out. Each report also publishes the share it carries — the evidence is
recorded as the guardian's reveal
([Oddity #2](#load-bearing-oddities--deliberate-decisions-worth-revisiting)) —
so slashing `threshold` guardians unlocks the creator's own secret early, which
a creator bent on this does not mind.

**Where the reasoning stops short**: spec.md "Common Attack Vectors" #6 and
[Trade-off §13](#13-availability-must-cover-the-whole-reveal-window--no-handoff-ever)
rest possession-based evidence on *one share, one holder, forever*. That holds
between guardians. It does not hold for the creator, who dealt every share, and
neither document considers the creator as a holder.

Found 11 November 2026 by reading the handler; not exercised on a devnet. It
needs a ruling: accept and record it, as guardian self-reporting is accepted;
remove the creator's profit (for instance its 10% slice of an early-reveal
slash) so that framing costs the creator rather than pays; or change what
counts as evidence, which is a protocol change with a far larger blast radius.


## Open Defects & Cleanup Notes

Likely bugs and unfinished edges are recorded here, each needing a reproducing
test or a ruling before it is fixed. Concerns that are reachable but unresolved
live under [Security Observations](#security-observations); costs consciously
accepted live in [Accepted Trade-offs](#accepted-trade-offs). A fixed defect is
deleted — git history keeps it.

#### 1. A settlement left a bond locked on a slashed guardian — once, unreproduced

Observed 9 August 2026 while verifying the e2e driver relocation: S3's final
assertion — "nothing stranded on the leaker" — failed once, with 592 uveil
still locked on the leaker after settlement. The run had `make e2e` preceding
the suite on the same chain, a 1-second cadence, and the retention and rotation
overrides set. Three full re-runs the same day — with the relocated driver and
with the released-bundle driver, with and without the preceding lifecycle run —
passed 74/74, each ending with a clean lock scan across all 24 guardians. The
trigger is therefore roster- or sequence-dependent: the devnet test key is
regenerated at every reset, so guardian selection re-rolls on every run. The
stranded amount matched no single frozen bond in that run's manifests (S1–S3
bonds were 404/509/604/761 uveil), so it is not simply "one release skipped" of
a known bond. Needs a reproducing test before any fix — and note defect 2,
which sits exactly where this would have been caught at its source.

#### 2. S2's bond-release check iterates a null roster and passes vacuously

`active_assignments` on a cancelled secret comes back null, so S2's
per-guardian release loop (`for g in $(… .secret.active_assignments[])`)
iterates nothing, emits a `jq` error on stderr, and prints its ✓
unconditionally — the check passes on every run whether or not the five bonds
were actually released. The suite
asserts the chain, and nothing asserts the suite (defect 3). The roster is available in
the scenario manifest (`selectedGuardians`), which does not depend on
post-cancellation state.

#### 3. Nothing asserts the scenario suite itself

`devnet/e2e-scenarios.sh` asserts the chain, and nothing asserts the suite. A
check that cannot fail looks exactly like a check that passes, so a defect in
the suite stays invisible until something unrelated exposes it. Defect 2 is one
such; three earlier ones were found and fixed the same way — a key-rotation
scenario that redrew guardians by lottery and failed one run in four, a helper
that silently dropped a share-band argument so a calibration was never
exercised, and block-event reads that took the first event in a block rather
than selecting on `secret_id`. No test confirms that a scenario's assertions
fail when the behaviour they describe is absent.


## Gaps With No Owner

Each was found by doing something real — a failing run, a release, a walked
runbook — and each is recorded rather than fixed because the fix is a decision.

### 1. Nothing verifies that generated code matches the protos

`make proto-gen` output is committed, and no check confirms the commit matches the
`proto/` it came from. CI's protobuf job runs `buf lint` and `buf breaking`, which
compare proto files to proto files.

Two failure directions, and the second is the serious one:

- **Stale generated code.** Regenerating with no proto change rewrites all six
  `.pb.go` files — around 400 lines, entirely inside the gzipped
  `FileDescriptorProto` blobs, zero semantic difference. Those blobs' gzip
  encoding is not stable across Go toolchains. The tree was re-baselined in
  August 2026 and will drift again the next time the toolchain moves.
- **A proto edited without regenerating.** Nothing catches this at all. It leaves
  `x/secrets/types` — the module every integrator pins — describing something
  other than its own `proto/`. Silent, and discovered by a consumer.

A gate would be a `make proto-gen && git diff --exit-code` step, which fails on
either. The complication is the first direction: with encoding unstable across
toolchains, that gate fails for anyone whose Go differs from CI's, so it needs a
pinned generation environment to be honest rather than annoying.

Whether it joins the CI gating work is an open question in
[planning/PENDING_CI_GATING_PLAN.md](planning/PENDING_CI_GATING_PLAN.md).

### 2. A release can publish from a commit whose E2E is unknown

`release.yml`'s `verify` job gates publication on `make verify && make test`. It
does not run the E2E suite, and `PROTOCOL_CHANGE.md` step 2 says "tag and publish"
without requiring `main`'s CI to be green first.

So a tag pushed while `main`'s E2E is red, or still running, publishes anyway:
binaries, a GHCR image and the protocol surface, at a version number that cannot
be unpublished. Nothing is wrong with the artefacts — `verify` and `test` did
pass — but the claim a version number makes is broader than what was checked.

Worked around by judgement today — checking the merged PR's E2E was green before
tagging — which is exactly the kind of step a checklist exists to not depend on.

Options: require green `main` in the runbook; add E2E to the release gate and
accept its duration on every release; or state plainly that a release asserts
`verify && test` and nothing more. The choice is an open question in
[planning/PENDING_RELEASE_STRATEGY_PLAN.md](planning/PENDING_RELEASE_STRATEGY_PLAN.md).

## Deviations From Documented Intent

1. **Progressive reveal windows / anti-coordination measures do not exist
   on-chain.** The reveal-window check is pure `[start, end]` bounds; every
   guardian may reveal at the first block. Nothing staggers reveals on the
   client side either: the guardian daemon reveals as soon as the window
   opens.
   **Documented intent to align**: spec.md "Revolutionary Capabilities" lists
   "progressive reveal windows" under Multi-layer Security, and CLAUDE.md's
   project overview says "progressive time-locks" — both read as
   protocol-enforced. Alignment = either add a protocol-side stagger or
   remove both claims.
2. **`secret_commitment` is stored, never verified on-chain.** Verification is
   wholly client-side — deliberate (the GIGO decision: the chain guarantees
   reconstruction integrity, not content validity; spec.md "Common Attack
   Vectors" #5, Invalid Share Submission, records it as an accepted risk;
   [Trade-off §1](#accepted-trade-offs)).
   **Documented intent to align**: spec.md "Share Distribution (Phase 2)" —
   the "Distribution Requirements" / "Security Measures" paragraphs
   ("*Encryption validation* ensures shares are properly encrypted…",
   "*Share validation* prevents malformed data…") read as chain-side checks
   the chain cannot perform (it sees only ciphertext). Alignment = reword
   those paragraphs as client/guardian responsibilities.
3. **The same spec.md paragraph describes a retired share model.** "Distribution
   Requirements" (spec.md "Share Distribution (Phase 2)") states that creators
   must "distribute shares to all assigned guardians from Phase 1" and that
   "each guardian must receive exactly one share at their assigned share index
   (1 to shares)". Neither holds: explicit share indices were removed in
   December 2024 (SSS carries an intrinsic ID — see CLAUDE.md's ShareIndex
   lesson), and distributing to *fewer* than `max_shares` is legal down to
   `threshold` (§2). Alignment = rewrite the paragraph against the band model;
   this is a spec correction, so it needs a ruling before it is made.

## Inefficiencies — Optimisation Candidates

1. **The prune queue's per-block cap bounds the work, not the scan.**
   `ProcessDuePrunes` drains via `drainDueEntries`, which collects **every** due
   entry into a slice and only then truncates to `MaxPrunesPerBlock`. The
   backlog does drain — 50 per block — but while it exists every block re-walks
   and re-allocates the whole of it, so a burst of `N` same-height terminal
   secrets costs `O(N²/50)` key reads spread over `N/50` blocks instead of
   `O(N)`. Same-height terminal transitions are cheap to produce (§1 of Security
   Observations), and the fix is to stop the walk at the cap rather than after
   it.

2. **Every guardian write re-reads the record it was just handed, to maintain
   the eligibility index.** `SetGuardian` calls `setGuardianEligibility` first,
   which does its own `GetGuardian` to compare the stored `available_until`
   against the incoming one and retire a stale key if the window moved. But
   `available_until` changes on exactly two paths — registration and
   `MsgGuardianUpdate` — while the five float mutations in `guardian_float.go`
   (bond lock, bond release, bond-count decrement, `k` adjustment, locked-float
   deduction) each read the guardian, mutate a copy, and hand it straight to
   `SetGuardian`. On those the comparison is guaranteed equal, so the read is
   pure overhead: **~1,534 gas** (`ReadCostFlat` 1,000 + 3 × a ~178-byte
   record), twice-read where once would do. This is the same shape as the
   duplicate read removed from selection
   ([CHAIN_MECHANICS.md, Performance & Scaling §3](CHAIN_MECHANICS.md#3-guardian-selection--oeligible-and-why-it-cannot-be-less)),
   reintroduced on the write side.

   Where it lands: **metered** on guardian acceptance and reveal — affordable,
   since accept measures 44,871–57,918 handler gas against a 100,000 budget
   (`guardian_gas_test.go`), so it is ~3% and not a boundary — and **unmetered**
   in EndBlock settlement, once per accepted guardian per secret, where it is
   block time with no fee brake (§1 of Security Observations).

   The index `Set` is also unconditional. A float movement genuinely changes the
   projection (`unlocked` moves), so most writes are necessary — but
   `MsgGuardianRotateKey` touches no projection field and still rewrites the
   entry.

   The fix is cheap and needs no new state: let `SetGuardian` accept the
   previously-read record from the caller that already holds it, and skip the
   `Set` when the projection is unchanged — `eligibilityEqual` already exists in
   `guardian_eligibility.go` for invariant 9. Not yet done, and deliberately
   listed rather than fixed silently: it is a cost the selection work added while
   removing a larger one.

## Accepted Trade-offs

*Consciously accepted costs: places where the protocol knowingly gave
something up to get something more valuable. Unlike the Load-Bearing Oddities
below (decisions that look wrong but aren't) or the Open Defects (things we
intend to fix), nothing here is a bug or a surprise — each entry records what
we chose, what it costs, and where the decision was made, so the reasoning
survives the conversations it was made in. Revisiting one means paying its
cost differently, not discovering it.*

*Each entry carries its own reasoning — the *why*, not just the *what* — so
the decision survives without a second document open. Entries link out for the
normative rule (spec.md), the mechanism (the keeper), and the decision log (the
plan), never for the argument itself. When a new ruling accepts a residual
cost, add it here in the same session.*

The costs themselves — what each fee is, how it is derived and where the
VEIL goes — are catalogued in [ECONOMICS.md](ECONOMICS.md).

---

### Test-harness determinism

#### 1. The scenario suite constrains selection, so S8 no longer samples a real pool

**Chose**: before reserving its post-rotation secret, S8 clears
`accepting_secrets` on every guardian outside the pre-rotation secret's selected
set, so selection has one possible outcome and the rotator is drawn with
certainty. Nothing about sortition changes — it runs honestly over the candidates
it is given, and pausing this way is a mechanism guardians genuinely have.
**Gave up**: S8's draw exercises selection over five candidates instead of
twenty-four, so it contributes nothing to selection coverage. Read as a
key-rotation test it loses nothing; read as evidence that selection works over a
realistic pool it would mislead, and it must not be.
**Also gave up**: on-chain state that outlives the process. A suite killed between
park and restore leaves the fleet unable to reserve anything until something
restores it or the chain is reset — an `EXIT` trap covers every path the suite
controls, and cannot cover `SIGKILL`.
**Where decided**: owner ruling, 6 August 2026;
`done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md`. Selection's own coverage is
`x/secrets/keeper/guardian_selection_test.go` and the sortition vectors.

#### 2. Devnet guardian identities are published, and deliberately so

**Chose**: every devnet guardian's signing key derives from the canonical BIP39
all-zeros test mnemonic at HD account index N, so `guardian-07` holds the same
address on every run. A ticket is `SHA256(seed ‖ address)`, so without stable
addresses no run's guardian set can resemble another's, and a log from yesterday
describes guardians that no longer exist.
**Gave up**: any secrecy for those keys — anyone can derive them. Confined to the
devnet by a chain-ID assertion that reads the devnet's ID from `networks.json` and
fails closed if it cannot; the keys cannot be created against any other chain.
**Where decided**: `done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md`. The mechanism is
`devnet/guardians.sh`.

### Security model boundaries

#### 1. The chain guarantees reconstruction integrity, never content validity

**Chose**: the chain stores and reveals ciphertext it cannot read; the
`secret_commitment` is stored but never verified on-chain, and recipients
verify reconstruction client-side.
**Gave up**: any on-chain guarantee that a secret's shares reconstruct to
something meaningful — a creator can distribute garbage against a valid
commitment and guardians will faithfully store and reveal it.
**Where decided**: spec.md "Invalid Share Submission" (accepted risk);
[Deviation #2](#deviations-from-documented-intent).

#### 2. HMAC keys derive entirely from public inputs

**Chose**: `SHA256("secrets" ‖ secretID ‖ guardianAddr ‖ "hmac_salt")` — a
salted hash, not a keyed MAC — so the chain verifies slashing evidence and
reveals with no secret key material. This is what makes early-reveal evidence
*self-verifying*: a reporter presents the plaintext share and the chain
recomputes the binding share ↔ guardian ↔ secret from data it already holds,
with no key custody and no trusted verifier anywhere in the loop.
**Gave up**: HMAC proves *binding*, never honesty — anyone (including the
creator) can compute a valid HMAC over arbitrary data, so a matching HMAC
says "this is the share that was issued to this guardian for this secret",
never "this guardian behaved". Pre-window share secrecy therefore rests
entirely on the share's own entropy — exactly 256 bits under the key-share
architecture, since the share is a point on the per-secret private key's
polynomial rather than a fragment of the payload.
**Where decided**: spec.md "Early-Reveal Reporting"; derivation in
`timeflareio/crypto` — `go/hmac.go` (pure Go — chain and guardian) and
`rust/src/utils.rs` (WASM/TS SDK), pinned byte-for-byte by that repository's
`vectors/` corpus; entropy analysis in
[planning/done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](planning/done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md) §8.

#### 3. Late-discovered pre-window leaks are unpunishable

**Chose**: early-reveal reports close the moment the reveal window opens
(`height < reveal_start_block`). This is forced, not arbitrary: HMAC evidence
proves *possession* of the share, and once reveals are legal and public on
chain, possession no longer distinguishes a pre-window leak from a replay of
somebody's legitimate reveal. Keeping the window open would turn every honest
reveal into slashable "evidence" against its own author.
**Gave up**: a leak discovered after the window opens can never be slashed;
reporting promptly is the reporter's responsibility, and a late report is
simply a failed reward claim, exactly like missing any other bounty window.
**Where decided**: ruling July 2026; enforced in
`x/secrets/keeper/msg_server_slash_guardian.go`, spec.md "Early-Reveal
Reporting".

#### 4. A failed attempt's ciphertext stays exposed to its coalition

**Chose**: the client-side retry convention — a failed secret is never
resumed; a retry re-seals with entirely fresh material (new secret ID and
selection draw, new inner encryption, new per-secret keypair, new split, new
hint), so the failed attempt's shares can neither reconstruct nor link to any
later attempt. The convention is advisory: documented client-side, not
chain-enforced. It costs nothing to honour because the seal path already
generates fresh material on every call (`timeflareio/crypto` —
`rust/src/seal.rs`), which is why the
ruling was documentation-only.

The underlying exposure is structural. Phase 2 hands a decryptable share to
**every** one of the `max_shares` selected candidates, and it must:
acceptance *means* "I decrypted my share and verified its HMAC", so material
precedes the bond. `MsgSlashGuardian` correctly refuses reports against a
guardian with no accepted assignment — there is no bond to slash — so a
candidate that receives a share and never accepts holds live material with
nothing at stake. The gap bound `max_shares − min_shares < threshold` makes
the never-confirmed set sub-threshold on any secret that **activates**; the
bound is conditional on activation, and a secret failing below `min_shares`
leaves up to `max_shares` never-bonded holders.

**Gave up**: any attempt that reached Phase 2 left its outer ciphertext `C`
in transaction history permanently, so a `≥ threshold` coalition of that
attempt's holders can always unlock that ciphertext — recipient-only, since
it decrypts to `C_r`. The content's effective time-lock is the minimum over
all distributed attempts. Bounded by protocol-random selection, the sunk
1,000 VEIL entry fee per colluding registration, and early-reveal slashing
once bonds lock.

**Structural fixes ruled out**: *accept-then-distribute* (a fourth
interaction in substance, and it forces a "was a share actually delivered?"
gate onto the no-reveal slash); *bond-at-selection* (seizes float for
assignments never confirmed); *an on-chain `pk_s` uniqueness check* (proto and
state surface for a failure mode the SDK cannot produce).
**Where decided**:
[planning/done/DONE_RETRY_CONVENTION_PLAN.md](planning/done/DONE_RETRY_CONVENTION_PLAN.md)
(ruled July 2026; documentation-only); authority
[spec.md, Common Attack Vectors #7](spec.md#common-attack-vectors--mitigations)
and its Client-Side Integration Guide "Retry semantics" point.

#### 5. Selection grinding is priced, not eliminated

**Chose**: a consensus-only seed —
`SHA256(chainID ‖ uint64_be(height) ‖ lastBlockHash ‖ uint64_be(counter))` —
with a non-refundable creation fee on top. No input is the creator's:
`clientEntropy` and the creator-chosen `secret_id` were both removed from the
message surface, and `creatorAddress` was removed from the seed because it was
an *offline-parallelisable* grind lever (generate many candidate addresses,
compute each resulting selection offline, fund and submit from the winner — no
block-waiting at all). The seed's inputs being public is deliberate: it is what
lets anyone verify from public data that the seed was honest, which is the
verifiability property the protocol actually claims.

**Gave up**: the creator's residual influence is **submission timing**. Because
every seed input is public before submission, a creator can compute the draw it
would get at the next height and choose not to submit — one whole-set re-roll
per block, at no cost for the rolls it discards. Ruled acceptable, and not
defended against:

- It gives **whole-set re-rolls only, never surgical placement**, which is no
  cheaper than the Sybil route the bond economics already price.
- Only a secret's own **creator** can grind its draw, and that creator already
  holds the plaintext — biasing it endangers only their own material, the same
  category as choosing `threshold = 2`.
- Every draw a creator actually *uses* is paid for at full price (the creation
  fee, 90% to validators and 10% burned), so steering cannot be done for free —
  only the unused rolls are free, and those change nothing.
- A creator wanting its own secret to fail has strictly cheaper routes anyway:
  never distribute, or distribute below `min_shares`, and let the commit
  deadline take it.

Commit–reveal seeding is **held in reserve** — it does not close retry-grinding
without also making commit abandonment costly, and it adds a round-trip of
latency to every secret. An on-chain randomness beacon (drand/VRF) is recorded
as the strongest long-term fix and future infrastructure, not a launch
requirement.
**Where decided**: D2, D3 and D6 in
[planning/done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md](planning/done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md)
(July 2026); the fee itself in
[planning/done/DONE_CREATION_FEE_PLAN.md](planning/done/DONE_CREATION_FEE_PLAN.md)
(per-draw cost table in its decision history).

### Economics

#### 6. Creators pay the band ceiling regardless of outcomes

**Chose**: the pool is fixed at `P(max_shares)` and splits equally among
*actual* revealers, so any slot that never ends in a reveal — left unfilled at
activation **or** a no-show at reveal time — raises the per-revealer payout.
The creator pays `P(max_shares)` either way. Uniformity is the point: one rule
covers both cases, and it is the same rule that makes revealing the dominant
strategy no matter what the rest of the roster does.
**Gave up**: the creator is compensated for a no-show only via the 10% bond
slice, and for an unfilled slot not at all; a wide band buys redundancy, not
a discount.
**Where decided**: variable-quorum design in
[planning/done/DONE_VARIABLE_QUORUM_PLAN.md](planning/done/DONE_VARIABLE_QUORUM_PLAN.md);
settlement rules in spec.md "Settlement".

#### 7. Settlement is threshold-independent — service is paid, not outcome

**Chose**: revealers are paid and bonds returned whether or not the secret
reached threshold; the threshold decides only the final state.
**Gave up**: a creator whose secret fails one reveal short of threshold
still pays every guardian who did reveal.
**Where decided**: spec.md "Secret Economics & Slashing" (ruled with the
bonded guardian economics, July 2026 —
[planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md)).

#### 8. Fixed prices, zero governance (Position A)

**Chose**: no `Params` state, no `MsgUpdateParams`, no vote can move an
economic constant; a guardian underwriting a year-long secret underwrites it
against economics that cannot float beneath it. The only retune path is a
coordinated software upgrade.
**Gave up**: no on-chain response to token-price swings, congestion, or
mispriced constants — every adjustment costs an upgrade cycle.
**Where decided**:
[planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md);
spec.md "Economic Parameters".

#### 9. Guardian registration is permanent and the entry fee sunk

**Chose**: no deregistration or refund path — a leaving guardian drains
their float and simply stops qualifying for selection; the 1,000 VEIL entry
fee is spent at registration (90% to validators, 10% burned).
**Gave up**: no clean exit or fee recovery for honest leavers; the fee is a
pure Sybil cost by design.
**Where decided**: [operations.md `MsgGuardianWithdrawStake`](operations.md#msgguardianwithdrawstake);
fee routing ruled in
[planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md).

#### 10. Integer dust is tolerated, not engineered away

**Chose**: truncating integer arithmetic everywhere, with dust bounded and
asserted rather than eliminated — the creation-fee curve can dip by up to
one basis point of `P` between adjacent distances, and the community pool
accrues each reward withdrawal's sub-uveil truncation remainder (SDK
design).
**Gave up**: literal exactness claims ("monotone", "exactly zero") — the
suites assert bounds (dust ≤ `P ÷ 10,000`; pool growth < 1 uveil per run)
instead.
**Where decided**: found in execution, July 2026 —
[planning/done/DONE_CREATION_FEE_PLAN.md](planning/done/DONE_CREATION_FEE_PLAN.md) §2
and
[planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md) §4.

### Liveness & capital

#### 11. All settlement waits for window close

**Chose**: one race-free settlement point at `reveal_end_block + 1` with
complete information — no payout can race an early-reveal report, and the
eligible set is final before anyone is paid. Paying at threshold instead would
settle against a set that a later report can still change, and would have to
decide what to do about a guardian already paid when its bond is
retrospectively forfeit.
**Gave up**: guardians' bonds stay locked up to a full window past their
work, even when threshold was met on the window's first block, and settlement
rides EndBlock rather than a user transaction (nobody can hurry it).
**Where decided**: the settlement queue in
`x/secrets/keeper/endblock_logic.go`; spec.md "Settlement".

#### 12. Settlement failures quarantine, never halt

**Chose**: a failed settlement retries every block behind a per-secret
`CacheContext`, with a `settlement_stalled` alarm from the first failure —
tokens are locked-not-lost with a one-secret blast radius, self-recovering
when an upgraded binary fixes the bug.
**Gave up**: no automatic escalation or unlock — a stalled secret's tokens
wait for a software upgrade; deliberately no panic (a deterministic EndBlock
panic is a chain-wide DoS).
**Where decided**: spec.md "Settlement failure handling" (July 2026);
[operations.md Time-Based Operations](operations.md#time-based-operations-beginblockendblock).

#### 13. Availability must cover the whole reveal window — no handoff, ever

**Chose**: selection requires `available_until ≥ reveal_end_block`, so a
guardian is only ever assigned a secret it has committed to outlive. The
reveal horizon `H` is pinned equal to `MaxAvailabilityWindow` for the same
reason — validation must never promise a window that selection cannot staff.

Share/bond transfer between guardians (guardian A hands an in-flight share and
its bond obligation to B, compensation forming part of the transaction) was
explored as the escape hatch and ruled **permanently off the table**.
Mechanically it is deceptively easy — the HMAC key derives from public inputs
(§2), so B's reveal would verify keylessly against A's original stored HMAC —
but it is unsound under possession-based evidence. Nothing can make A forget
the share: post-transfer A holds the plaintext with no collateral at stake and
can profitably report B for an "early reveal" through a Sybil reporter
address. The evidence verifies, because it is the genuine share and possession
cannot distinguish "B leaked" from "A retained", so A nets the transfer compensation
plus 50% of B's bond while a provably innocent B is fully slashed. No defence
survives: reporter restrictions are Sybil-bypassable, closing reports for
transferred shares removes deterrence exactly where it is needed, and slashing
both parties punishes the innocent one. Since accepting a transfer hands the
transferor a call option on half one's bond, no rational guardian would accept
— the market is self-defeating before it is even unsafe. **One share, one
holder, forever** is therefore load-bearing for the whole evidence model, not
incidental. Transfer would not have addressed key loss in any case: the
transferor must be able to decrypt in order to hand off. The supported
key-lifecycle mechanic is forward-only **rotation**, which moves no share
material anywhere, so the invariant holds trivially (§14).

**Gave up**: long-dated secrets select only from correspondingly
long-registered guardians, shrinking the eligible set exactly where
redundancy matters most (a one-year secret needs guardians registered
≥ one year ahead). The cap is permanent, not awaiting handoff mechanics;
extending horizons is cancel-and-recreate territory, and operator exit is
`accepting_secrets = false` plus serving out existing commitments.
**Where decided**: ruled July 2026 —
[spec.md, Common Attack Vectors #6](spec.md#common-attack-vectors--mitigations)
("Share Ownership Transfer") and spec.md "Timing Constraints".

#### 14. Key rotation is hygiene, not recovery

**Chose**: forward-only key epochs — rotation bounds the blast radius of a
key leak to one epoch for *future* assignments, while every old epoch stays
permanently bound to the assignments made under it.
**Gave up**: there is deliberately no key-recovery mechanic — a guardian who
loses key material eats the no-show slashes on every in-flight secret
encrypted to the lost epochs.
**Where decided**: spec.md "Guardian Key Rotation";
[planning/done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md](planning/done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md).

#### 15. Short secrets cost the creator disproportionately

**Chose**: the creator pays for the guardians' gas — an `F_accept` slice
escrowed apart from the pool, and an `F_reveal` slice inside it — so that
completing the job can never cost a guardian VEIL, at any distance the
protocol permits.
**Gave up**: price flatness at the short end. A one-minute five-guardian
secret costs the creator ~0.234 VEIL, of which the guardians' actual wage is
0.0001 VEIL — the rest is gas reimbursement, the anti-grinding floor and the
creator's own gas. The work of guarding a secret has a fixed cost that no
amount of brevity avoids. The same property is the short-secret spam defence.
**Where decided**:
[planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md](planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md)
(ruled July 2026).

#### 16. Guardians carry their own gas until the secret ends

**Chose**: nothing is disbursed before the secret reaches a terminal state,
so a guardian is reimbursed for its accept transaction at settlement,
cancellation or commit expiry — not when it sends it.
**Gave up**: immediacy. On a year-long secret the guardian floats ~0.011 VEIL
of gas for a year. The alternative — paying at acceptance — would let the
module's escrow drift from the amounts stored on the secret, forcing either a
second stored component or a payout recomputed from live constants, which the
immutable-economics ruling forbids. The carry is small beside the bond already
locked for the same period (~21 VEIL on a year-long secret).
**Where decided**:
[planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md](planning/done/DONE_GUARDIAN_COST_RECOVERY_PLAN.md)
(ruled July 2026).

#### 17. Declared gas always exceeds gas consumed, and the excess is spent

**Chose**: every actor declares a gas limit with headroom over what its
transaction actually consumes, and Cosmos charges the declared limit rather
than the consumption — unused gas is never refunded.
**Gave up**: exact pricing of work. A declaration must clear worst-case
consumption or the transaction aborts, so a margin always exists and is always
paid. Two places it shows:

- **Creators** declare a measured model — a base plus a per-share and a
  per-byte term for phase 2 — rather than a flat figure. That bounds the waste
  (the old flat 1,000,000-gas phase 2 declared roughly three times what a
  seven-guardian secret consumed) without removing it. Determinism is the
  reason it stays a model rather than a simulation: a wallet must quote the
  exact fee before the user signs, and a simulated figure cannot be quoted.
- **Guardians** declare the amount the protocol reimburses whenever that
  exceeds simulation, so spend equals reimbursement exactly and no
  `gas_adjustment` setting can erode coverage. Where a handler outgrows the
  reimbursement the daemon declares the larger simulated figure instead: being
  under-reimbursed is bad, but aborting is worse — an aborted reveal is a
  no-show slash.

**Also accepted**: a guardian operator who raises `gas_price` above the
consensus floor pays the difference themselves. The creator's escrow is
denominated at the floor, so buying mempool priority is the guardian's
purchase, not a protocol cost. Overriding the operator's own price would be
worse — it could leave a guardian unable to push a reveal through a congested
chain. The daemon logs the per-leg shortfall once at startup so the choice is
informed rather than discovered in a shrinking balance.
**Where decided**:
[planning/done/DONE_CREATION_COST_INTEGRITY_PLAN.md](planning/done/DONE_CREATION_COST_INTEGRITY_PLAN.md)
§6 (ruled 27 July 2026).

### Client hand-off

#### 18. A shared claim link carries the claim kit in its URL

**Chose**: the mobile client's universal links, planned and not yet built,
share a claim kit as `https://link.timeflare.io/claim#timeflare:claim/…` —
the whole kit in the URL fragment. One tap opens the app with the kit loaded,
and a phone without the app reaches an install page from the same link.
Browsers never send the fragment, so the link host, its CDN and messenger
link-preview bots do not receive the kit.
**Gave up**: the kit is a PRIVATE key, and on a phone that opens the link
without the app it is exposed on the device: it lands in browser history and
whatever that history syncs to, a compromised link host, CDN or domain could
serve a page that reads it, and an in-app web view or a browser extension can
see the full URL. A claim link carrying nothing, with the kit pasted in
separately, would close all three; it was declined for the extra step it puts
on a first-contact claimant.
**Where decided**: owner, 11 November 2026; `timeflareio/mobile-client` —
`docs/planning/PENDING_UNIVERSAL_LINKS_PLAN.md` §2 and §8.

## Load-Bearing Oddities — Deliberate Decisions Worth Revisiting

These look wrong at first glance but each solves a real problem. Revisiting any of
them means solving the underlying problem another way.

Decisions whose cost has been consciously accepted live in
[Accepted Trade-offs](#accepted-trade-offs) with their full reasoning:
public-input HMAC keys (§2), the report window
closing at `reveal_start_block` (§3), pre-acceptance share exposure and the
retry convention (§4), the fixed band-ceiling pool and where forfeited slices
flow (§6), settlement waiting for window close (§11), the availability cap and
the permanent ruling against share/bond transfer (§13), and forward-only key
rotation (§14). Only the two decisions below carry no accepted cost — they are
structure, not trade.

1. **Three-phase commit with one deadline.** Guardians are selected and locked
   before any share exists because the client needs their encryption keys before
   it can encrypt, and selection must be protocol-controlled (anti-bias). A single
   `commit_deadline` spans all phases. **Do not collapse this to a single
   transaction.** Analysis (July 2026) showed the two-step shape is itself the
   anti-bias mechanism: any single-transaction design requires the client to
   *predict* the selection before committing tokens, and every predictable-seed
   scheme lets an attacker buy selection re-rolls at roughly transaction-fee cost.
   Two interactions, where the **first is financially binding**, is the minimum
   structure for grinding resistance. The non-refundable creation fee (July
   2026) completes the shape: the financially binding first step now carries a
   real per-draw price, not just gas. The seed's inputs are all public, so a
   creator can compute a draw before submitting and re-roll by choosing a later
   block — a whole-set re-roll, never surgical placement, and every *realised*
   draw is paid for at full price. Ruled acceptable and out of scope
   ([Trade-off §5](#accepted-trade-offs)).
2. **Early-reveal slashing auto-submits the evidence as the guardian's reveal.**
   Odd — the punished guardian "reveals" via their accuser — but it salvages the
   leaked share for reconstruction and prevents double-punishment (early *and*
   no-show) for one share. The internal path deliberately skips the `k`
   step-down that a voluntary reveal earns, so the salvage never softens the
   slash.

## Design decisions — the questions readers ask

The register's deliberate decisions, as the questions a creator, guardian
operator or auditor actually asks. These are **not known issues**: each looks
wrong at first glance, solves a real problem, and was chosen with the
alternative understood. Every answer links to the entry that owns the
argument — this index summarises, it never becomes a second authority. Every
entry cites an owner ruling: the index records decisions, it does not make
them.

- **Why does creating a secret take two transactions?** The financially
  binding first step *is* the anti-grinding mechanism — any single-transaction
  design lets an attacker buy selection re-rolls at transaction-fee cost.
  Never collapse it. ([Oddity #1](#load-bearing-oddities--deliberate-decisions-worth-revisiting))
- **Why can anyone compute a share's HMAC key?** Deliberately public inputs,
  so the chain verifies slashing evidence keylessly; HMAC proves binding,
  never honesty. ([Trade-off §2](#2-hmac-keys-derive-entirely-from-public-inputs))
- **Why does a slashed guardian's "reveal" come from their accuser?** The
  auto-submitted evidence salvages the leaked share for reconstruction and
  prevents double punishment for one share.
  ([Oddity #2](#load-bearing-oddities--deliberate-decisions-worth-revisiting))
- **Why can't I report an early leak once the reveal window opens?**
  Possession stops proving a pre-window leak the moment reveals are legal —
  keeping reports open would turn every honest reveal into evidence against
  its author. ([Trade-off §3](#3-late-discovered-pre-window-leaks-are-unpunishable))
- **Why are guardians paid only at window close, even when the threshold was
  met on day one?** One race-free settlement point with complete
  information — paying early races the slashing regime.
  ([Trade-off §11](#11-all-settlement-waits-for-window-close))
- **Why must a guardian's availability cover the whole reveal window, and why
  is the reveal horizon capped at a year?** The cap is permanent: share/bond
  transfer is ruled out forever, because possession-based evidence cannot
  distinguish a transferor's retained copy from the transferee's leak.
  ([Trade-off §13](#13-availability-must-cover-the-whole-reveal-window--no-handoff-ever);
  spec.md Common Attack Vectors #6)
- **Why doesn't the creator get a partial refund for unfilled slots or
  no-shows?** The pool prices the band ceiling; forfeited slices reward the
  guardians who did the work — the same rule that makes revealing the
  dominant strategy.
  ([Trade-off §6](#6-creators-pay-the-band-ceiling-regardless-of-outcomes))
- **Why doesn't the chain check that my shares or commitment are valid?** The
  chain guarantees reconstruction integrity, never content validity — it
  sees only ciphertext.
  ([Trade-off §1](#1-the-chain-guarantees-reconstruction-integrity-never-content-validity);
  spec.md Common Attack Vectors #5)
- **My secret failed to activate — can I resume it?** Never. A retry is a
  full restart with a fresh seal (new ID, draw, keys, split, hint), so the
  failed attempt's shares can neither reconstruct nor link to the new one.
  ([Trade-off §4](#4-a-failed-attempts-ciphertext-stays-exposed-to-its-coalition))
- **Why must an abandoned draw wait out the full commit timeout, and why does
  every draw cost a fee?** The non-refundable creation fee prices the draw
  and the sit-out is deliberate anti-grinding structure — there is no early
  exit. ([Trade-off §5](#5-selection-grinding-is-priced-not-eliminated))
- **Who can collect a rebate, and why does collecting link my address to the
  secret?** Recipiency is proved by a bearer secret (`z`), and publishing it
  is a deliberate, opt-in exception to recipient privacy — collect to a
  single-use address, or not at all, if unlinkability matters.
  (spec.md "Recipient Rebate";
  [operations.md](operations.md#recipient-rebate))

## Risks stated in the specification

[spec.md](spec.md) states these and is their authority. They are listed so that
the whole surface can be read from one place; the argument is in the spec, or
in the entry named, and is not repeated here.

| Risk | Where the spec states it | Standing | Reasoning held here |
|---|---|---|---|
| Guardian collusion to reveal early | Common Attack Vectors #1 | mitigated, not priced out — the threshold is the primary guarantee | — |
| Sybil registration | Common Attack Vectors #2 | mitigated by the entry fee and float | [Trade-off §9](#9-guardian-registration-is-permanent-and-the-entry-fee-sunk) |
| A guardian accepts and never reveals | Common Attack Vectors #3 | mitigated by the no-reveal slash and redundancy | [Trade-off §7](#7-settlement-is-threshold-independent--service-is-paid-not-outcome) |
| Validators manipulate block times | Common Attack Vectors #4 | consensus-level protection | — |
| A creator distributes shares that reconstruct to nothing useful | Common Attack Vectors #5 | accepted risk | [Trade-off §1](#1-the-chain-guarantees-reconstruction-integrity-never-content-validity) |
| Share or bond transfer between guardians | Common Attack Vectors #6 | ruled out permanently | [Trade-off §13](#13-availability-must-cover-the-whole-reveal-window--no-handoff-ever) |
| Candidates hold a share before any bond locks | Common Attack Vectors #7 | accepted risk, mitigated by the retry convention | [Trade-off §4](#4-a-failed-attempts-ciphertext-stays-exposed-to-its-coalition) |
| Small-order keys | Common Attack Vectors #8 | closed, 29 July 2026 | — |
| A leaking guardian reports itself and recaptures the bounty | Early-Reveal Reporting, "Honesty note" | accepted | — |
| An undetected off-chain leak forfeits nothing | Early-Reveal Reporting, "Honesty note" | accepted | [Trade-off §3](#3-late-discovered-pre-window-leaks-are-unpunishable) |
