# E2E scenarios — deterministic outcomes, faster cadence

- **Priority** — P2 — suite latency and coverage. **No protocol impact**: no
  proto, wire-contract, spec or timing-constant change, and no SDK release. Two
  of the suite's eleven scenarios never run in CI because the job cannot afford
  them. Measured end state: **all eleven at ~5 minutes of block time**, against
  ~19 minutes for nine of them today.
- **Status** — `ready` (6 August 2026). All open questions ruled by the owner,
  4–6 August 2026; the rulings are folded into the body below.
- **Origin** — owner request, 4 August 2026: shorten the E2E devnet lifecycle
  job, combining scenarios into one chain cycle where safe, followed by
  "ensure all scenario tests are deterministic, if guardian selection prevents
  that, we need a mechanism to resolve it".
- **Components** —
  - `devnet/e2e-scenarios.sh` — shrunk block distances, the dropped share-band
    argument, candidate-pool parking, per-secret filtering of block events, the
    fleet-restore trap
  - `devnet/guardians.sh` — deterministic guardian key derivation, the
    `park`/`restore` helpers, per-victim restart (`guardians_restart`)
  - `make/common.mk` — `TEST_BLOCK_TIME`, the canonical test cadence
  - `make/devnet.mk` — the `GUARDIAN_COUNT` comment only; the default stays 24
  - `.github/workflows/ci.yml` — the `e2e` job: enable S5 and S8 via the
    existing dev overrides; still one job (owner ruling, 4 August 2026)
  - `docs/CHAIN_MECHANICS.md` — four ledger entries (the S8 draw lottery, the
    non-reproducibility of devnet guardian identities, the silently dropped
    share band on the rebate drill, and the selection coverage a constrained
    candidate pool gives up)
  - No change to `proto/`, the wire contract, `docs/spec.md`,
    `x/secrets/types/constants.go`, or any protocol timing constant.
    Candidate-pool parking drives `MsgGuardianUpdate` through the existing
    `guardianctl update --accepting-secrets` verb — a guardian-controlled
    production mechanism, used as-is.

## The problem

The suite is already one chain cycle. There is no per-scenario refresh to
remove: `devnet/e2e-scenarios.sh` is a single process driving S1–S11 against a
running chain, and both `make e2e` and `make e2e-scenarios` refuse to start one
(`make/devnet.mk`). The only reset is the single `dev-reset` in
`e2e-full-native`, and CI does one `dev-up` for both suites.

The cost is that **the scenarios wait serially on independent block-height
deadlines**. Each `create_secret "$M" 150 100 100` puts reveal 150 blocks out
with a 100-block window, so ~250 blocks pass before its assertions can fire —
and the next scenario does not begin until they have. Roughly, at 1s blocks:

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
because **they never run in CI**. Both gate on a dev override
(`TIMEFLARE_RETENTION_BLOCKS`, `TIMEFLARE_KEY_ROTATION_MIN_INTERVAL`) because
their real windows are ~6 months and ~30 days of blocks, and skip rather than
wait when it is unset. Neither override has ever been set in `ci.yml`, and no
commit records a decision either way, so the gap is an omission rather than a
trade — but it is one the suite's cost would defend: switching them on as-is
adds ~560 blocks and takes the job to ~28 minutes.

Separately, the suite was **not deterministic**. Guardian selection is hash
sortition, and three scenarios need a *particular* guardian to play a role:
S1's no-show victim, S3's leaker, S8's rotator. S8's was the acute case — it
redrew a secret until a pre-chosen guardian was selected, with a **~25%** chance
of exhausting its six attempts (5 of 24 per draw, `(19/24)^6 ≈ 0.25`). That went
unnoticed only because S8 skips: the scenario the suite could not afford to run
was also the one that would fail one run in four. It was observed failing exactly
that way during this plan's measurement work.

Both problems have one shape: **a window measured in blocks, and something
wall-clock or probabilistic that has to fit inside it.** The fixes are a shorter
block, a shorter distance, and a forced draw.

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

- **offset 150 → 100** on S1, S3, S10 and S8B: 50 blocks each.
- **duration**: no scope. Already at `MinRevealDuration`.
- **S8's A-side offset 400 → 200**: ~200 blocks. The only constraint is that
  secret A is still in flight when B settles. B is created ~20 blocks after A
  (the rotation, the restart and one park window); at the floor B ends at
  `A+220`, so A needs `offset + 100 > 220`. An offset of 200 ends A at `A+300` —
  an 80-block margin. The 30-block rotation-interval override is satisfied
  trivially, since epoch 0 is set at registration and the suite starts hundreds
  of blocks into a run.

`CommitTimeoutBlocks` and `MinRevealStartOffset` are deliberately left alone.
Both are load-bearing semantics rather than padding — the 50-block commit window
is precisely what S4 asserts, and the 50-block buffer is the guarantee that a
secret reaches `pending` before its reveal opens. Overriding them would have the
devnet testing a different protocol from the one we ship.

## Cadence is the largest lever, and it is one line

The protocol is denominated in blocks, so the suite's waits are block distances
and their wall-clock cost is a deployment fact. `TIMEFLARE_BLOCK_TIME` sets it,
`DONE_BLOCK_TIME_CONFIGURATION_PLAN.md` already put the canonical test value in
one place (`make/common.mk`, `TEST_BLOCK_TIME`), and its decision 7 explicitly
sanctions any value a test needs. `networks.json` keeps the devnet's real 6s
untouched, so the shipping cadence is unaffected by construction.

**Measured, 6 August 2026** — seven consecutive full runs, each a fresh
`dev-reset` plus the complete suite with S5 and S8 enabled, all eleven scenarios,
74 assertions, zero failures:

| set | measured | outcome |
|---|---|---|
| 50ms | 74ms | fails — S1's kill window |
| 100ms | 125–132ms | intermittent — two passes, then S5's retention prune |
| **200ms** | **225–233ms** | **7/7 clean, ~7.4 min per full iteration** |

Two properties worth carrying:

- **`timeout_commit` is not the cadence.** The overhead was a consistent ~27–30ms
  *additive* at every value tested (50→74, 100→125, 200→227), so a setting buys
  slightly less than it names. The suites already measure and report the real
  cadence (`cadence_note`), so this is observed rather than assumed.
- **The floor is the harness, not the fleet.** Both failures were bash
  orchestration overrunning a window sized for 1s blocks — `wait_state` polls
  every 3s, `guardian_dir_for` queries all 24 guardians, and each assertion costs
  query round-trips. The guardian daemons kept up throughout: S10b, which exists
  to catch a guardian paying for a duplicate acceptance, was clean at every
  cadence including 125ms. Its guard is denominated in blocks
  (`inFlightExpiryBlocks`), which is why cadence does not move it.

The two windows that set the floor, and how they differ:

- **S1's kill window** — the victim must be killed between activation
  (`creation + 50`) and reveal start (`creation + offset`). At offset 150 that is
  100 blocks: 7.4s at 74ms (fails), 23s at 227ms (holds). **Phase 1 halves it**,
  because offset 100 puts reveal start at `creation + 100` while the commit
  deadline stays at `+50`. This is a protocol-derived window and cannot be
  widened, so the cadence must be re-validated after the shrink.
- **S5's retention override** — `TIMEFLARE_RETENTION_BLOCKS=60` prunes a terminal
  secret 60 blocks after settlement, and the suite must read the record before it
  goes: 7.9s at 132ms (fails), 14s at 227ms (holds). Unlike the first this is a
  pure test knob, so raising it buys margin with no change to what S5 asserts.

## What this buys

Block counts are cadence-independent; the wall clock is not.

| | blocks | at 1s | at 227ms |
|---|---|---|---|
| today, S5 and S8 **skipped** | ~1,140 | 19 min | 4.3 min |
| S5 and S8 on, nothing else changed | ~1,700 | 28 min | 6.4 min |
| **after this plan** (shrink, determinism, cadence) | ~1,370 | 23 min | **5.2 min** |

Cadence dominates, and it is the cheapest change here. Overlapping the
independent scenarios would save a further ~320 blocks, which is why the suite's
sequential structure was once the headline of this work — but at 200ms that is
~73 seconds, and it is **not pursued** (owner ruling, 6 August 2026). See
"Overlapping the scenarios" below for the analysis, preserved for whenever the
critical path binds again.

**Fixed wall-clock costs do not scale with cadence** and become the larger share
once it drops. From the measured runs, ~110s of each ~445s iteration was
`dev-reset` plus the two daemon-restart health waits in S1 and S8; CI adds
checkout, `go-install`, `guardiand-sync` and `dev-up` on top. So the honest CI
projection is *around ten minutes* for all eleven scenarios, not five — the block
time is 5.2 minutes and the rest is setup that this plan does not touch.

## Determinism: two different properties

**Outcome determinism** — the suite never fails for a reason unrelated to
protocol behaviour. No sortition lottery anywhere in the pass/fail path. This is
the property CI needs, and phases 1–2 deliver it; it was verified across the
seven measured runs.

**Cast reproducibility** — the same guardians draw the same secrets on every run.
Strictly stronger, a debugging convenience, and **not pursued** (owner ruling,
6 August 2026). The only route to it is a devnet override on the selection seed,
and unlike the two overrides that already exist — which change *when* background
work fires — that one changes *who is selected*. It sits on the phase-1 hot path
of every secret, inside the function whose whole purpose is that its inputs are
publicly verifiable, and it would need a `docs/spec.md` amendment in the same
change. Everything CI needs is reachable without it.

(A *constant* seed would be wrong regardless: tickets depend only on the seed and
the address, so a fixed seed makes every secret select the identical five.)

What is delivered instead makes failures legible rather than repeatable: phase 2
gives guardians stable addresses, and `ComputeSelectionSeed` is already exported
and pure with its inputs on the reservation event, so a harness can always
*verify* which set was drawn — it simply cannot choose it in advance.

## Two mechanisms, and why one is not enough

Sortition cannot be steered, but it can be *read*, and the set it draws from can
be *constrained*. The suite needs both, because reading alone cannot serve S8.

**Reading the role from the selected set** suffices wherever the role can be
chosen after the draw: **S1's victim** and **S3's leaker** both come from their
own secret's `selected_guardians`, which is fixed at selection and never extended
or substituted (commit 443c770).

**S8's rotator cannot be read, and no reordering fixes it.** S8 proves that one
daemon holds two epoch keys and serves two live secrets — A with the retired key,
B with the new one. The rotation must sit strictly between A's reservation and
B's: the new epoch takes effect at `blockHeight + 1` and the epoch a guardian
serves is pinned by the secret's reservation height (`msg_server_rotate_key.go`,
"a same-block selection still hands the creator the pre-rotation key"), so a B
reserved before the rotation is an old-key secret and proves nothing. The rotator
must therefore be chosen *before B's draw exists*, leaving two independent draws
that must intersect — and the chance they do **falls as the pool grows**:
`1 − C(N−5,5)/C(N,5)` is 72.6% at 24 guardians and 43.8% at 48. Growing the fleet
makes this worse, not better.

## Constraining the candidate pool

**Validated 6 August 2026** against a live devnet, then across seven full suite
runs.

Selection draws only from the eligibility index, and that index holds **only
guardians with `accepting_secrets = true`** (`guardian_eligibility.go`;
`EligibleCandidatesFor` walks nothing else). `MsgGuardianUpdate` toggles the flag
presence-aware and writes only the guardian record — forward-only, touching no
existing secret or assignment. The verb already exists as
`guardianctl update --accepting-secrets=false --accept`, the suite already
asserts `guardianctl` is on PATH and drives it for `rotate-key`, and
`guardians.sh` holds every guardian's keyring. `guardiand` itself only *reads*
the flag, for status display, and never re-asserts it — so a guardian parked by
the suite stays parked until the suite restores it.

Measured behaviour: clearing the flag on 19 of 24 took ~7s of concurrent
transactions and left the eligible set **exactly** the kept five; a secret
reserved in that window selected precisely those five; restore returned all 24.

So S8 parks everything except A's five, reserves B, and restores. B's set is then
forced to equal A's and the rotator is drawn with certainty — one rotation, one
reservation, and no retry loop discarding live secrets until sortition
cooperates. This is not steering the protocol: sortition still runs honestly over
the candidates it is given, and pausing this way is what a real guardian does to
stop taking work. The price is that S8's draw then samples five candidates rather
than the fleet, which is the right trade for a key-rotation test and is recorded
in the ledger so it is not later mistaken for selection coverage.

`GUARDIAN_COUNT` stays at **24** (owner ruling, 6 August 2026), which is what CI
runs — `ci.yml` sets no override, so `dev-up` takes the devnet default. Parking
makes the cast exact at any pool size, so nothing is left for a bigger fleet to
buy. 24 is also evidenced on a two-core runner rather than assumed: a reduction
to 8 was tried and reverted, because the same deterministic secret failed at both
counts (a short health-probe bound was the cause) and because 8 left too little
margin for a scenario that deliberately removes a guardian. The `GUARDIAN_COUNT`
comment already explains that a larger pool adds no *acceptance* redundancy; it
adds no determinism either, and that is worth adding, since "raise
`GUARDIAN_COUNT`" is the intuitive and wrong answer to a flaky
selection-dependent scenario.

Three properties have to be respected, all three learned by doing:

1. **Wait on chain state, not on the command returning.** `guardianctl update`
   returns once its transaction passes CheckTx, not once it is in a block, so a
   park that reported success is not yet a park — read the pool immediately
   afterwards and it is still the full fleet. Confirming the eligible set is
   therefore load-bearing rather than belt-and-braces, and it doubles as the
   assertion a reservation needs.
2. **Restore on every exit path.** The flag is on-chain state, so a fleet left
   parked poisons the rest of the run *and* every later run against that chain —
   `dev-reset` becomes the only cure. An `EXIT` trap is a precondition of the
   technique; it was exercised for real when a check aborted mid-park and put all
   24 back.
3. **Park to exactly the group, and assert eligibility before reserving.** Bond
   affordability and the concurrency cap still filter a parked pool, and the floor
   is `max_shares` exactly — the protocol selects that many and fails outright if
   fewer are eligible, with no over-selection constant. A group of five has no
   slack, but that failure is loud rather than a silent bias, which is the right
   trade.

## Latent hazards fixed alongside

Three things are correct today only because the suite is sequential. They are
cheap, independently correct, and each is a trap for anyone who later revisits
overlapping the scenarios, so they land here rather than being rediscovered:

1. **Three block-event reads take `.[0]` of a height without filtering**: S1's
   `guardian_slashed` and `secret_rewards_distributed`, and S3's
   `secret_rewards_distributed`. Correct only while one secret can settle in a
   block. S4, S5, S8 and S10b already select by `secret_id`; these must too.
2. **S10b's acceptance audit leans on a height window.** `S10_FROM_HEIGHT` carries
   the comment "everything a guardian confirmed from here on belongs to this
   secret, *because the suite is sequential*". Its primary evidence — the daemon's
   own broadcast log — is already secret-scoped; the complementary `tx_search`
   audit is what needs narrowing.
3. **`guardians_restart` restarts the whole native fleet.** That is deliberate — a
   hardcoded count once left a victim dead for a whole run, and a dead guardian is
   still selectable — but the docker path already restarts only the victim and the
   native path should match it, with `assert_fleet_intact` redefined as "every
   daemon except those the suite knows to be down".

One further hazard is recorded rather than fixed, because it is a property of how
the suite starts: **S6b's withdraw drill asserts an exact balance delta on
`bootstrapping`**, and `make e2e-scenarios` runs `setup-test-users.sh fund` at
entry, which draws from that same pool along with `fund.sh`. Nothing inside the
suite touches it — S11 funds its courier from `george` — so the drill's
exclusivity holds only while no scenario funds anything mid-run.

## Execution

### Phase 1 — shrink the distances

1. S1, S3, S10 and S8B offsets 150 → 100; S8A 400 → 200. Duration stays at 100
   throughout (already the floor). S8B's shrink is what leaves A's window the
   80-block margin over B's settlement derived above.
2. Forward `create_secret`'s fifth argument. The function is documented
   `# manifest offset duration bump` and passes only four arguments to
   `scenario-create.js`, but the rebate drill calls it with a fifth — `7:9`, the
   share band the drill's whole dust calibration is derived from. The SDK example
   accepts a fifth `shares` argument and defaults to 5, so the drill runs the
   suite's ordinary zero-width 5 band and the calibration in its comment has never
   been exercised. One-line fix.
3. Leave the `REBATE_COLLECTION_DRILL` shape alone — its 900-block window and 10×
   bump are calibrated for the dust floor, and re-deriving that is a separate
   concern. Item 2 is what lets the calibration take effect.
4. Verify: `make e2e-scenarios` with both dev overrides set, whole suite green,
   plus one drill run since item 2 changes what it does.

Expected: ~1,700 → ~1,370 blocks with S5 and S8 enabled.

### Phase 2 — determinism

1. `devnet/guardians.sh`: derive each guardian's signing key from a fixed,
   index-derived mnemonic instead of `timeflared keys add` with a random
   passphrase, so `guardian-07` is the same address on every run. Devnet-only.
   Document in the script that these are throwaway test identities with published
   derivation and must never be reachable from a non-devnet chain ID.
2. `devnet/guardians.sh`: add `park <keep...>` and `restore`. One
   `guardianctl update --accepting-secrets` per guardian whose flag changes,
   broadcast concurrently, then **poll the chain until the eligible set is exactly
   what was asked for** — the command returns at CheckTx, so waiting on chain
   state is the correctness requirement, not a nicety. `restore` must be
   idempotent and safe when nothing is parked, because the suite calls it from a
   trap. Enumerate the fleet on the guardian name (`guardian-NN`): `guardians.sh
   status` wraps its table in header and summary lines that also carry two fields,
   so a looser filter collects `ADDRESS` and `height:` as addresses.
3. `devnet/e2e-scenarios.sh`: install an `EXIT` trap that restores the whole fleet,
   and prove it fires on the `fatal` path as well as on success. This lands before
   anything that parks.
4. S8: rotate one guardian from A's set, reserve B inside a park window holding
   only A's five, restore, and assert B's selection contains the rotator. The
   six-draw retry loop goes with it.
5. Close hazards 1 and 2 above: filter the three unscoped block-event reads by
   `secret_id`, and narrow S10b's `tx_search` audit to the secret.
6. `guardians_restart`: hazard 3 — restart only the named victim in native mode,
   with `assert_fleet_intact` redefined accordingly.
7. Add the four `docs/CHAIN_MECHANICS.md` ledger entries: the S8 draw lottery (an
   open defect, fixed here, and observed failing during this plan's measurement
   work), the non-reproducibility of devnet guardian identities (a trade-off closed
   by item 1), the dropped share-band argument on the rebate drill (fixed in phase
   1), and the coverage a parked pool gives up.
8. Update the `GUARDIAN_COUNT` comment in `make/devnet.mk` to record that a larger
   pool adds no determinism either, and why.
9. Verify: the suite green, and S8 green across at least five consecutive runs,
   confirming from the logs that B's selected set equalled A's on every one rather
   than inferring it from the scenario passing. Confirm the fleet is fully unparked
   afterwards by querying the eligible count, including after a deliberate
   mid-scenario failure.

### Phase 3 — cadence

1. `make/common.mk`: `TEST_BLOCK_TIME` 1s → **200ms**. One line, in the file
   `DONE_BLOCK_TIME_CONFIGURATION_PLAN.md` designated for exactly this.
   `networks.json` keeps the devnet's 6s, so the shipping cadence is untouched.
2. **Re-validate against the shrunk offsets.** 200ms was measured at offset 150,
   where S1's kill window is 100 blocks. Phase 1 halves it to 50, so the margin
   halves too — 23s becomes ~11s. If S1 fails there, the options in order are:
   reduce the harness latency inside the window (`wait_state`'s 3s poll and
   `guardian_dir_for`'s 24-guardian lookup are the two costs), or settle at a
   slower cadence. Do **not** widen the offset back — that trades the phase-1
   saving for the phase-3 one.
3. Raise `TIMEFLARE_RETENTION_BLOCKS` from 60 to 200 in the suite's documented
   invocation and in CI. At 227ms, 60 blocks is 14s between settlement and pruning
   for the suite to read a terminal record; it failed at 132ms and is the second
   floor-setter. S5 asserts pruning at `terminal_at + RetentionBlocks` whatever the
   value, so nothing about its coverage changes.
4. Verify: five consecutive green runs at the shrunk offsets, reporting the
   *measured* cadence each time rather than the setting — `timeout_commit` is a
   post-commit delay and ran ~27–30ms above its value at every point tested.

Expected: ~5.2 minutes of block time for all eleven scenarios, against 19 minutes
for nine today. Fixed setup costs put the realistic job at around ten minutes.

### Phase 4 — CI

1. Enable S5 and S8 in the `e2e` job by setting `TIMEFLARE_RETENTION_BLOCKS` (200,
   per phase 3) and `TIMEFLARE_KEY_ROTATION_MIN_INTERVAL=30` on both the `dev-up`
   and the suite steps. `dev-up` already forwards `--unsafe-dev-overrides` when
   either is set — once per override, so setting both passes the flag twice;
   confirm `timeflared start` accepts the repetition rather than discovering it at
   boot. Note the coupling: `RebateCollectionWindow()` clamps to the live retention
   value, so retention also bounds S10's rebate collection window.
2. One job, no sharding (owner ruling, 4 August 2026). Sharding would multiply
   runner minutes, re-pay `dev-up` per shard, and do nothing for local runs.
3. The cadence comes from `TEST_BLOCK_TIME`; CI passes `TIMEFLARE_BLOCK_TIME`
   explicitly today and should take the canonical value rather than carry its own.

## Overlapping the scenarios

Not pursued (owner ruling, 6 August 2026): the cadence reduction took the prize.
Overlapping the independent scenarios saves ~320 blocks, which was ~5 minutes at
1s blocks and is ~73 seconds at 200ms — against making the suite's whole
assertion harness concurrent. The analysis is kept because it is the record a
future plan would start from if the critical path ever binds again.

**The classification.** S2 (own secret), S4 (never reaches guardians beyond
selection), S10 (own secret) and S11 (bank-level) could overlap; S6 could too but
only behind S2, whose cancel transaction it reads. S1, S3 and S8 could not: each
perturbs the fleet *and* makes guardian-scoped assertions, and they **sum** rather
than overlap — 202 + 202 + 302 ≈ 706 blocks, which would be the floor. S6b needs
the `bootstrapping` pool to itself; S5 must follow S1's and S2's terminal records;
S7 and S9 are whole-chain scans that belong at the end and sit mid-file today.

**Why the harness cannot simply be parallelised.** `ok` and `fatal` mutate
`PASS`/`FAIL` in the shell's own scope and `fatal` calls `exit 1`, so a scenario
driven in a background subshell loses both its assertion counts and its ability to
fail the run. Several scenarios also take mid-flight *actions* rather than only
assertions — S2 cancels at `deadline + 15`, S4 attempts a cancel while `reserved`,
S8 rotates and restarts a daemon — so this is not a matter of deferring
assertions. The shape that would fit bash: one process, each scenario an ordered
list of `(target height, step)` pairs, the launcher running the next due step
across all in-flight scenarios in height order. Assertion state stays where it
belongs, `fatal` keeps its meaning, and reservation stays exclusive for free
because only one step runs at a time.

**Disjointness would be constructive, not probabilistic.** Extending parking into
a partition — 5 guardians per fleet-perturbing scenario, the remainder as a shared
pool for those that only assert their own secret — makes the casts disjoint by
construction. At 24 guardians that is 5 + 5 + 5 and a nine-strong parallel group,
so the fleet already registered is the right size.

**S1 and S3 would still stay serial** (owner ruling, 6 August 2026). They conflict
only through guardian-global assertions, and a partition would make their victims
disjoint — but a slash moves the guardian's `k`, which prices future bonds. The
assertions read frozen per-secret bonds so they hold, yet a reviewer could no
longer confirm that by inspection, and these two scenarios are precisely the ones
whose failures have historically been misdiagnosed as protocol defects.

## What this does not solve

- **Fixed setup costs.** `dev-reset`, the two daemon-restart health waits, and
  CI's checkout, `go-install`, `guardiand-sync` and `dev-up` do not scale with
  cadence, and they are the larger share once it drops. ~110s of each measured
  445s iteration was setup and restarts.
- **`make e2e` keeps its 250 blocks.** `secret-lifecycle.js` hardcodes
  `revealWindow: { startOffset: 150, duration: 100 }` in the SDK, so shrinking it
  needs an SDK release and a `SDK_VERSION` bump — a release-train item under
  `PROTOCOL_CHANGE.md`. Its wall-clock cost falls with cadence like everything
  else.
- **The suite stays sequential.** See "Overlapping the scenarios".
- **Cast reproducibility is not delivered.** The same run twice still draws
  different guardians within whatever pool it drew from.
- **S8 does not exercise selection over a realistic pool.** Its draw runs
  sortition over five candidates once the pool is constrained.
- **Parking leaves a residual trap.** `accepting_secrets` outlives the process, so
  a suite killed between park and restore leaves the fleet unable to reserve
  anything. The `EXIT` trap covers the paths the suite controls; it cannot cover
  `SIGKILL`.
- **The harness's own latency is the cadence floor, and this plan only bounds it.**
  `wait_state`'s 3s poll and `guardian_dir_for`'s 24-guardian lookup are what fail
  first. Reducing them would lower the floor further; that is its own piece of work,
  and phase 3 item 2 is where it would first be felt.
- **Nothing here compiles a consumer.** The suite exercises released guardian and
  SDK artefacts at pinned versions.

## Related plans

- `PENDING_CI_GATING_PLAN.md` — decides *when* the `e2e` job runs; this plan
  decides what it costs. Both edit `ci.yml`'s `e2e` job, so whichever lands second
  rebases. Its statement that `e2e` cannot pass yet is stale —
  `timeflareio/guardian` is public and `SDK_VERSION` is pinned at v0.0.5.
- `done/DONE_BLOCK_TIME_CONFIGURATION_PLAN.md` — the invariant that makes cadence
  safe to change, and the home phase 3 writes to.
- `done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md`,
  `done/DONE_GUARDIAN_SELECTION_SCALABILITY_PLAN.md` — the selection design, the
  seed derivation left untouched, and the eligibility predicate parking works
  through.
- `done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md` — the rotation mechanics S8 covers.
- `done/DONE_TERMINAL_SECRET_RETENTION_PLAN.md` — S5's subject and the
  `RetentionBlocks` override phase 3 raises.
- `done/DONE_DEVNET_PARALLEL_GUARDIAN_REGISTRATION_PLAN.md` — prior art for driving
  one transaction per guardian concurrently, which is what a park window does.
