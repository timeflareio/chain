# E2E scenarios — deterministic outcomes, concurrent execution

- **Priority** — P2 — suite latency and reliability. No protocol impact if the
  seed override (open question 1) is declined; a protocol-surface change if it
  is accepted. The suite is the only gate that exercises the chain, the
  guardian daemon and the SDK together, and at ~19 minutes it is the reason
  two of its own scenarios are switched off in CI.
- **Status** — `refining` (4 August 2026). Three open questions below; the
  first is a protocol-surface ruling and blocks phase 4 only.
- **Origin** — owner request, 4 August 2026: shorten the E2E devnet lifecycle
  job, combining scenarios into one chain cycle where safe, followed by
  "ensure all scenario tests are deterministic, if guardian selection prevents
  that, we need a mechanism to resolve it".
- **Components** —
  - `devnet/e2e-scenarios.sh` — the whole suite: classification, launch
    ordering, role assignment, the shrunk block distances
  - `devnet/guardians.sh` — per-victim restart (`guardians_restart`),
    deterministic guardian key derivation
  - `make/devnet.mk` — `GUARDIAN_COUNT` default, `e2e-scenarios` invocation
  - `.github/workflows/ci.yml` — the `e2e` job: enable S5 and S8 via the
    existing dev overrides; still one job (owner ruling, 4 August 2026)
  - `docs/CHAIN_MECHANICS.md` — two ledger entries (the S8 draw lottery, the
    non-reproducibility of devnet guardian identities)
  - `docs/spec.md` and `x/secrets/types/constants.go` — **only** if open
    question 1 is ruled for the seed override
  - `typescript-sdk` — `examples/secret-lifecycle.js` hardcodes a 150-block
    reveal offset; a separate release-train item, see "Not in scope"
  - No change to `proto/`, the wire contract, or any protocol timing constant

## The problem

The suite is already one chain cycle. There is no per-scenario refresh to
remove: `devnet/e2e-scenarios.sh` is a single process driving S1–S11 against a
running chain, and both `make e2e` and `make e2e-scenarios` refuse to start one
(`make/devnet.mk`). The only reset is the single `dev-reset` in
`e2e-full-native`, and CI does one `dev-up` for both suites.

The cost is that **the scenarios wait serially on independent block-height
deadlines**. Each `create_secret "$M" 150 100 100` puts reveal 150 blocks out
with a 100-block window, so ~250 blocks pass before its assertions can fire —
and the next scenario does not begin until they have. Roughly, at CI's 1s
blocks:

| | blocks |
|---|---|
| `make e2e` (secret-lifecycle.js, 150+100) | ~250 |
| S1 no-show slash | ~250 |
| S2 mid-hold cancel (deadline + 15) | ~65 |
| S3 early-reveal report | ~250 |
| S4 commit timeout (deadline + 2) | ~52 |
| S6 fee burn / S10b / S11 | ~20 |
| S10 rebate | ~250 |
| **total** | **~1,140 ≈ 19 min** |

S5 and S8 — the two most expensive scenarios — are absent from that table
because **they skip in CI today**. Both gate on a dev override
(`TIMEFLARE_RETENTION_BLOCKS`, `TIMEFLARE_KEY_ROTATION_MIN_INTERVAL`) and
`ci.yml` sets neither. S8 alone is a 400+100-block secret. Enabling them as-is
adds ~560 blocks and takes the job to ~28 minutes, which is why they are off —
the suite's cost is already deciding its own coverage.

Separately, the suite is **not deterministic**. Guardian selection is hash
sortition, and three scenarios need a *particular* guardian to play a role:
S1's no-show victim, S3's leaker, S8's rotator. S8's is the acute case — it
redraws a secret until a pre-chosen guardian is selected, with a **~25%**
chance of exhausting its six attempts (5 of 24 per draw, `(19/24)^6 ≈ 0.25`).
That has gone unnoticed only because S8 skips in CI. Any move to concurrency
makes this worse rather than better: overlapping secrets must not share the
guardian that one of them is about to kill.

So the two asks are one piece of work. Both change the same waits and the same
role assignment, and doing either alone would have to be substantially redone
by the other.

## What the timing constants actually allow

Every scheduling value is an immutable protocol constant in
`x/secrets/types/constants.go` (Position A — retuning is a software upgrade,
never governance):

| Constant | Value | Role | Scope |
|---|---|---|---|
| `CommitTimeoutBlocks` | 50 | `commit_deadline = creation + 50` | none — protocol-fixed, not creator-chosen |
| `MinRevealStartOffset` | 50 | buffer, commit deadline → reveal start | none |
| `MinRevealStartOffsetTotal` | **100** | floor on `startOffset` | none — **but the scenarios pass 150** |
| `MinRevealDuration` | **100** | floor on `duration` | none — **the scenarios already pass 100** |
| `MaxRevealDuration` | 14,400 | ceiling | not binding |
| `MaxRevealHorizon` | 5,256,000 | `offset + duration` ceiling | not binding |
| `RetentionBlocks` | 2,592,000 | prune delay after terminal | dev override; S5 uses 60 |
| `KeyRotationMinIntervalBlocks` | 432,000 | minimum rotation spacing | dev override; S8 uses 30 |
| `MinAvailabilityWindow` | 100 | guardian availability floor | devnet sets 5,256,000 — not binding |

The decisive detail is that **`startOffset` is validated as a pure relative
offset**. `validateRevealWindow` (`x/secrets/keeper/msg_server_request_guardians.go`)
and `ValidateBasic` (`x/secrets/types/message_request_guardians.go`) compare it
against the constant and never against the height the transaction lands at;
`reveal_start_block` is then `execution_height + offset`. So `startOffset: 100`
is valid however late the transaction lands — the 150 carries no timing slack
that shrinking it would spend. S4 already passes 100.

That bounds the shrink exactly:

- **offset 150 → 100** on S1, S3, S10: 50 blocks each.
- **duration**: no scope. Already at `MinRevealDuration`.
- **S8's offset 400 → 200**: ~200 blocks. The only constraint is that secret A
  is still in flight when B settles. B is created ~25 blocks after A (rotation,
  restart, draws); at the floor B ends at `A+225`, so A needs
  `offset + 100 > 225`. An offset of 200 ends A at `A+300` — a 75-block margin.
  The 30-block rotation-interval override is satisfied trivially, since epoch 0
  is set at registration.

`CommitTimeoutBlocks` and `MinRevealStartOffset` are deliberately left alone.
Both are load-bearing semantics rather than padding — the 50-block commit
window is precisely what S4 asserts, and the 50-block buffer is the guarantee
that a secret reaches `pending` before its reveal opens. A dev override has
precedent, but these two sit in the consensus path of every secret rather than
in a background sweep, so overriding them would have the devnet testing a
different protocol from the one we ship.

Serially the shrink is worth only ~13% (~1,140 → ~990 blocks). Its value is
that it lowers the *ceiling*: once scenarios overlap, wall clock is the longest
single scenario, so 252 → 202 is a direct cut on the whole suite, and S8 is
what sets that ceiling whenever it is enabled.

## Determinism: two different properties

The ask separates into two properties that are worth naming apart, because they
have different costs and only one of them is required.

**Outcome determinism** — the suite never fails for a reason unrelated to
protocol behaviour. No sortition lottery anywhere in the pass/fail path. This
is the property CI needs, and phases 1–3 deliver it without touching the
protocol.

**Cast reproducibility** — the same guardians draw the same secrets on every
run. Strictly stronger, and useful for debugging rather than for correctness.
It requires two independent things, and today neither holds:

1. *Stable guardian identities.* `devnet/guardians.sh` creates each guardian
   with `timeflared keys add` against a passphrase from `openssl rand`, so
   every `dev-reset` produces 24 **new addresses**. Since a ticket is
   `SHA256(seed ‖ address)`, the winning set cannot repeat across runs even if
   the seed were fixed. Fixing this is devnet-only (phase 2).
2. *A reproducible seed.* `ComputeSelectionSeed` is
   `SHA256(chainID ‖ height ‖ lastBlockHash ‖ counter)`
   (`x/secrets/keeper/guardian_selection.go`). `lastBlockHash` is
   unreproducible by construction and `height` varies with transaction timing,
   so the seed differs every run. Changing this is a protocol-surface change —
   **open question 1**.

The plan therefore delivers outcome determinism unconditionally, and treats
cast reproducibility as a ruling to be taken rather than assumed.

### Making outcomes deterministic without touching selection

Sortition cannot be steered, but it can be *read*. Every role is assigned from
observed on-chain state after selection, never chosen in advance:

- **S1's victim** — from S1's own `selected_guardians`, minus the union of
  every other in-flight secret's `selected_guardians`.
- **S3's leaker** — the same rule. S1 and S3 additionally cannot overlap each
  other at all (below).
- **S8's rotator** — the hard case. The rotation must happen *before* secret B
  is created, because B's Phase-1 response is what hands the creator the new
  epoch key. So the rotator is fixed before B's draw exists and cannot be
  chosen from the intersection. Two mechanisms fix it, and the plan takes both:
  rotate **every** guardian in A's set rather than one, which turns "a specific
  guardian is drawn" (20.8% per draw) into "any of A's five is drawn" (72.6%),
  and raise the draw budget from 6 to 20. Each abandoned draw is two
  transactions and no wait, so 20 draws cost ~60 blocks in the worst case and
  nothing in the common one. Residual failure probability: `0.274^20`, i.e.
  below 1 in 10^11. The scenario still asserts what it asserted before — the
  retired-epoch pipeline serves A while the new-epoch pipeline serves B — and
  now asserts it for whichever of A's guardians B happened to draw.

`selected_guardians` is fixed at selection and never extended or substituted
(commit 443c770), so a set read once stays valid for the secret's life, and the
disjointness computed at launch cannot decay underneath the suite.

**The pool must be big enough for disjointness to be reliable.** With
`GUARDIAN_COUNT=24` and 5 selected, the chance that at least one of S1's five
is absent from five other in-flight sets is only ~84% — a flaky suite. The
plan raises the default to 48, which takes it to ~99%, and phase 3 asserts the
disjointness explicitly so a shortfall fails at the launcher with a named
cause instead of two scenarios later as a protocol-shaped error. This is the
one respect in which a larger guardian pool genuinely buys something: the
existing `GUARDIAN_COUNT` comment in `make/devnet.mk` is careful to say a
larger pool adds no *acceptance* redundancy, and that remains true — what it
adds here is room for disjoint role assignment.

If open question 1 is ruled for the seed override, the disjointness becomes
computable ahead of the run rather than probabilistically available, and the
`GUARDIAN_COUNT` increase can be reverted. That is the main reason to want it.

## Classification: what can overlap

| | Class | Why |
|---|---|---|
| S2 mid-hold cancel | **parallel** | acts only on its own secret |
| S4 commit timeout | **parallel** | never reaches guardians beyond selection |
| S10 rebate | **parallel** | own secret; S10b needs the fix below |
| S11 funded claim kit | **parallel** | bank-level, own addresses |
| S6 fee burn | **parallel** | the ratio assertion is already explicitly independent of what shares the block, and the known-fee check is `>=` |
| S1 no-show slash | **serial** | kills a daemon *and* asserts that guardian's global float |
| S3 early-reveal report | **serial** | slashes a guardian and asserts pool exclusion |
| S8 key rotation | **serial** | rotates and restarts a daemon |
| S6b withdraw drill | **serial** | asserts an exact balance delta on `bootstrapping` |
| S5 retention pruning | **after** | consumes S1's and S2's terminal records |
| S7 liveness, S9 no treasury | **end of run** | whole-chain scans, already delta-tolerant |

Four conflicts have to be closed for the parallel column to be true. These are
the reasons the file is sequential today, not oversights:

1. **S1 and S3 cannot overlap each other.** Both perturb the fleet *and* make
   guardian-scoped assertions — S1 asserts "victim float shrank by exactly the
   slashed portion", which fails if S3's leaker is the same guardian. Neither
   can overlap S8. Three serial slots are therefore structural.
2. **`guardians_restart` restarts the whole native fleet** by design — a
   hardcoded count once left a victim dead for the rest of a run, and a dead
   guardian is still selectable. Under overlap a fleet restart takes down every
   concurrent secret's guardians. It needs the per-victim behaviour the docker
   path already has, and `assert_fleet_intact` needs a definition that holds
   when a *different* scenario is deliberately running one daemon short.
3. **S10b already documents its dependency**: `S10_FROM_HEIGHT` carries the
   comment "everything a guardian confirmed from here on belongs to this
   secret, *because the suite is sequential*". Filtering the accept count by
   secret ID removes it.
4. **S6b's withdraw drill conflicts with S11 and with user funding.** It
   asserts `BAL_AFTER - BAL_BEFORE == WITHDRAWN - fee` on the `bootstrapping`
   address, and `fund.sh` — which S11 and `setup-test-users.sh fund` both use —
   draws from that same pool.

## Execution

Ordered so that each phase is independently verifiable and the risky
restructure lands last.

### Phase 1 — shrink the distances (no structural change)

1. S1, S3, S10 offsets 150 → 100; S8A 400 → 200. Duration stays at 100
   throughout (already the floor).
2. Leave the `REBATE_COLLECTION_DRILL` shape alone. Its 900-block window and
   10× bump are already calibrated as "the cheapest shape whose 30% clears the
   dust floor for every allowed activation outcome" — the floor is met by the
   nine-share gas cost, not by the window, and re-deriving it is a separate
   concern.
3. Verify: `make e2e-scenarios` with both dev overrides set, whole suite green.

Expected: ~1,700 → ~1,450 blocks with S5 and S8 enabled.

### Phase 2 — determinism groundwork

1. `devnet/guardians.sh`: derive each guardian's signing key from a fixed,
   index-derived mnemonic instead of `timeflared keys add` with a random
   passphrase, so `guardian-07` is the same address on every run. Devnet-only;
   no protocol surface. Document plainly in the script that these are throwaway
   test identities with published derivation and must never be reachable from a
   non-devnet chain ID.
2. S8: rotate all of A's selected guardians; raise the draw budget to 20; fail
   with the observed intersection in the message.
3. S10b: filter the acceptance audit by secret ID; drop the reliance on
   `S10_FROM_HEIGHT` meaning "the suite is sequential".
4. `guardians_restart`: restart only the named victim in native mode, matching
   the docker path. Redefine `assert_fleet_intact` as "every daemon except
   those the suite currently knows to be down", tracked explicitly.
5. Add the two `docs/CHAIN_MECHANICS.md` ledger entries: the S8 draw lottery
   (an open defect, fixed here) and the non-reproducibility of devnet guardian
   identities (an accepted trade-off until phase 2, then closed).
6. Verify: the suite green, and S8 green across at least five consecutive runs
   — the point of the change is that its failure mode was probabilistic, so one
   green run is not evidence.

### Phase 3 — the launcher and the classification

1. Declare the classification as a table at the top of the file that the
   launcher *reads*, so a scenario's class is executable rather than a comment
   a human has to keep honest.
2. Keep every scenario's numeric identity: `S1` stays `S1`. It is the name in
   every log line, CI artefact, commit message and cross-reference in the file
   (S5's own comment names S1 and S2 by number), and renumbering to a `P`/`S`
   prefix would break that archaeology for no functional gain. The class rides
   as a separate tag — `S1 [serial]`, `S2 [parallel]` — printed in the banner
   and sourced from the table.
3. Restructure into: launch all parallel secrets → assert each as it matures,
   in height order → the three serial slots (S1, S3, S8) → S5 → S6b → the
   end-of-run scans (S7, S9).
4. Assert disjointness at the launcher: after the parallel batch is created,
   compute each serial scenario's available victim set and fail immediately,
   naming the sets, if any is empty.
5. `GUARDIAN_COUNT` default 24 → 48.
6. Revisit S9's dust bound. It tolerates "at most withdrawal dust", and under
   concurrency the withdraw drill that produces the dust no longer sits at a
   known point in the run.
7. Verify: five consecutive green runs, and one deliberate-failure run per
   serial scenario (kill the wrong daemon, skip a rotation) confirming the
   failure is still reported where it happens rather than two scenarios later.

Expected: the critical path becomes the longest single scenario — ~302 blocks
with S8, ~202 without.

### Phase 4 — CI

1. Enable S5 and S8 in the `e2e` job by setting `TIMEFLARE_RETENTION_BLOCKS=60`
   and `TIMEFLARE_KEY_ROTATION_MIN_INTERVAL=30` on both the `dev-up` and the
   suite steps. `dev-up` already forwards `--unsafe-dev-overrides` when either
   is set. Note the coupling: `RebateCollectionWindow()` clamps to the live
   retention value, so 60 also shortens S10's rebate collection window — that
   is the existing calibration and must not be pushed lower without
   re-deriving the drill.
2. One job, no sharding (owner ruling, 4 August 2026). Sharding would multiply
   runner minutes, re-pay `dev-up` per shard, and do nothing for local runs,
   where the devnet is machine-global.
3. Block time stays at 1s (owner ruling, 4 August 2026).

Expected end state: ~19 min → ~5–6 min *with* S5 and S8 now running, versus
~28 min if they were enabled without this work.

## What this does not solve

- **Cast reproducibility is not delivered** unless open question 1 is ruled for
  the override. Phase 2 makes guardian *addresses* stable, which makes failures
  legible — the same run twice will still draw different guardians. Phase 3's
  disjointness remains probabilistic (~99% at 48 guardians), asserted rather
  than guaranteed. The plan bounds this rather than closing it, and says so at
  the point of assertion.
- **The three serial slots stay serial.** S1, S3 and S8 make guardian-global
  assertions and perturb the fleet; no amount of scheduling makes them
  overlappable. The floor on the suite is the longest of the three plus the
  parallel batch's tail.
- **`make e2e` keeps its 250 blocks.** `secret-lifecycle.js` hardcodes
  `revealWindow: { startOffset: 150, duration: 100 }` in the SDK, so shrinking
  it to 100 needs an SDK release and a `SDK_VERSION` bump in
  `devnet/versions.env` — a release-train item under `PROTOCOL_CHANGE.md`, not
  something this plan can land. `scenario-create.js`'s *default* offset of 150
  is likewise the SDK's, but every scenario passes the offset explicitly, so
  phase 1 is unaffected.
- **The rebate collection drill stays off by default.** ~1,000 blocks, and its
  shape is already minimal for the dust floor.
- **Nothing here compiles a consumer.** The suite exercises released guardian
  and SDK artefacts at pinned versions, so a green run says nothing about
  unreleased consumer changes (workspace root `CLAUDE.md`, "Changing anything
  that crosses a boundary").

## Related plans

- `PENDING_CI_GATING_PLAN.md` — decides *when* the `e2e` job runs; this plan
  decides how long it takes. Orthogonal, but both edit `ci.yml`'s `e2e` job, so
  whichever lands second rebases onto the other.
- `done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md`,
  `done/DONE_GUARDIAN_SELECTION_SCALABILITY_PLAN.md` — the selection design and
  seed derivation open question 1 would amend.
- `done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md` — the rotation mechanics S8 covers,
  and the source of the `KeyRotationMinIntervalBlocks` override.
- `done/DONE_TERMINAL_SECRET_RETENTION_PLAN.md` — S5's subject and the
  `RetentionBlocks` override.
- `done/DONE_DEVNET_PARALLEL_GUARDIAN_REGISTRATION_PLAN.md` — prior art for
  the `GUARDIAN_COUNT` increase's effect on `dev-up`.

## Open questions

**1. Should the selection seed take a devnet-only override, to make sortition
reproducible?**

The mechanism would mirror the two that already exist — an environment variable
read through a `…Value()` accessor in `x/secrets/types`, refused by
`timeflared start` unless `--unsafe-dev-overrides` is passed, and marked
consensus-critical. Under the override the seed would drop `height` and
`lastBlockHash`, leaving `SHA256(chainID ‖ counter)`: still varying per secret
(so different secrets draw different sets, which the scenarios need), but
reproducible across runs. Combined with phase 2's stable addresses, secret *N*
would always draw the same guardians, disjointness would be computable before
the run, and `GUARDIAN_COUNT` could go back to 24.

A constant seed would be wrong, for the record: tickets depend only on the seed
and the address, so a fixed seed makes *every* secret select the identical five
guardians — which defeats disjointness rather than enabling it.

The argument against is not cost but blast radius. The two existing overrides
change *when* background work fires; this one changes *who is selected*, the
mechanism the protocol's fairness rests on, and it lives on the phase-1 hot
path of every secret. It would need a `docs/spec.md` amendment in the same
change ("Guardian Selection" and the normative seed derivation), and it puts a
branch inside the function whose whole purpose is that its inputs are publicly
verifiable.

**Recommendation: decline for now, and revisit only if phase 3's disjointness
proves flaky in practice.** Phases 1–3 deliver outcome determinism without it,
which is the property CI needs; reproducibility is a debugging convenience, and
phase 2 delivers most of that benefit by making addresses stable. A cheaper
non-protocol fallback also exists if reproducibility later turns out to matter:
`ComputeSelectionSeed` is already exported and pure, and the seed inputs are
emitted on the reservation event, so the harness can *verify* which set was
drawn and why — it simply cannot choose it in advance. Needs an owner ruling
either way, because it is a protocol-surface change and the plan must not
assume one.

**2. Should S1 and S3 overlap each other via a bond-and-float partition
instead of staying serial?**

They conflict only through guardian-global assertions, not through the fleet:
S1 kills a daemon, S3 merely reports one. If each read only its own victim's
float and the two victims were disjoint, they could overlap and the suite's
critical path would drop by another ~200 blocks. The reason to hesitate is that
a slash moves the guardian's `k`, which prices *future* bonds — assertions read
frozen per-secret bonds so they hold, but a reviewer can no longer confirm that
by inspection, and the two scenarios are precisely the ones whose failures have
historically been misdiagnosed as protocol defects.

**Recommendation: keep them serial in this plan.** The saving is real but it
trades away the property that makes the suite trustworthy — that a
fleet-perturbing scenario owns the fleet while it runs. Revisit as its own plan
if the critical path is still the binding constraint after phase 4.

**3. Is 48 the right `GUARDIAN_COUNT` default, given `dev-up` pays for it?**

48 puts disjointness at ~99% and 96 at ~99.9%, against a `dev-up` that
registers and starts every daemon. The registration path is already
parallelised (`DONE_DEVNET_PARALLEL_GUARDIAN_REGISTRATION_PLAN.md`), but 48
live daemons on a CI runner is a resource question this plan has not measured,
and `guardians.sh start` with no count now runs at that scale on every S1
restart.

**Recommendation: measure `dev-up` and steady-state resource use at 24, 48 and
96 during phase 3, and set the default from that rather than from the
probability alone.** If 48 is too heavy for the runner, the fallback is to keep
24 and accept ~84% disjointness with an explicit retry of the launch batch,
which costs a few blocks and stays honest about what it is doing.
