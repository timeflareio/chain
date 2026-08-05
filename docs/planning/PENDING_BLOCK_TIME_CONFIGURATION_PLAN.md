# Block-Time Configuration — Plan

*Gives the block cadence one definition that every component derives from, one
override that reaches all of them at runtime, and a guard that keeps time out of
everything below the presentation surface. Today the number is a literal in
eleven places across two repositories, and CI runs a 1s chain against guardians
configured for 6s.*

> **Status: refining** — created 5 August 2026, §6 Q1 ruled the same day. The
> remaining questions are in §6; not executable until they are ruled and folded
> into the body.
> **Priority**: P3 — nothing is behaviourally broken (§3), so this is
> maintainability plus two wrong numbers in operator-facing output. It becomes P2
> the moment anything below the surface starts converting.
> **Origin**: a question about why the local devnet runs at 6s while the suites
> wait on block-denominated deadlines, August 2026.
> **Components**: `networks.json`, `devnet/chain/setup-chain.sh`,
> `devnet/docker/init-chain.sh`, `devnet/docker/generate-compose.sh`,
> `make/devnet.mk`, `make/deps.mk`, `make/docker.mk`,
> `.github/workflows/ci.yml`, `docs/guides/NETWORKS.md`, `make verify`; in
> `timeflareio/guardian`, `internal/config/{config.go,networks.go}`,
> `internal/cli/{config_init.go,register.go}` and `internal/guardian/reveal.go`;
> in `timeflareio/typescript-sdk`, `src/protocol/{constants.ts,blockclock.ts}`.

## 1. The invariant this plan protects

**Time appears only at a presentation surface. Everything beneath it is block
periods.**

The protocol is denominated in blocks throughout, so a component below the surface
has no business converting between blocks and wall-clock. Two surfaces exist: the
mobile client, which shows a recipient a date, and `guardianctl`'s operator
output, which shows a human an availability window. Both may convert. Nothing else
may.

That is what makes the cadence safe to change: a faster or slower chain moves what
a window costs in seconds and nothing about what it means. Change it mid-flight and
the only visible effect is that presented reveal dates shift, which is correct —
they were always estimates of when a block height would arrive.

This plan therefore does two separable things: it stops the cadence being eleven
literals, and it puts a guard under the invariant so it stays true.

## 2. Why the definition needs centralising

The number is written eleven times, all literals, nothing derived from anything:

| Where | Value | Governs |
|---|---|---|
| `devnet/chain/setup-chain.sh:15` | `:-6s` | `timeout_commit`, the native devnet's real cadence |
| `devnet/docker/init-chain.sh:23` | `:-6s` | `timeout_commit`, the compose devnet's |
| `devnet/docker/generate-compose.sh:22` | `:-6s` | what the compose file passes through |
| `make/devnet.mk:369` | `:-2s` | `e2e-full-native` |
| `make/deps.mk:103` | `:-2s` | the dependency-verification devnet gate |
| `make/docker.mk:195` | `:-2s` | the compose full-E2E |
| `.github/workflows/ci.yml:166` | `1s` | the CI devnet |
| `.github/workflows/ci.yml:171` | `1s` | the CI suites |
| `guardian/internal/config/config.go:186` | `6 * time.Second` | the guardian's metrics and CLI display |
| `guardian`'s generated `config.yaml` | `block_time: 6s` | what a running daemon believes |
| `typescript-sdk/src/protocol/constants.ts:167` | `6_000` | the SDK's fallback when it has no measurement |

CI sets `1s` for both the devnet and the suites — the workflow records why, that
block-denominated waits at 6s would take the suite past forty minutes — while the
guardians in that run carry `block_time: 6s`, from a Go default that never sees the
variable. So the mismatch is live and has been silent.

## 3. What the mismatch actually costs, measured

The invariant holds today. Every consumer was audited:

- **Chain (`x/secrets`): no conversions.** The protocol is blocks end to end.
- **Guardian: three uses, all after the fact.** `reveal.go:207` computes
  `sinceWindowOpen` *after* the reveal has succeeded — the transaction hash is
  recorded two lines earlier — solely to pass a duration to
  `metrics.RecordReveal`. The log line beneath it reports
  `blocks_after_window_open`, a block count. `register.go:223` and `:278` convert
  for operator-facing display of availability windows. None of the three feeds a
  decision.
- **SDK: every conversion is inside `blockclock.ts`**, which measures the real
  cadence from block samples and falls back to the constant only when it has none.
  Nothing else in `src/` reads it. The single time→block conversion is
  `ANCHOR_TARGET_BLOCKS` (§6 Q1).

So the cost of the live mismatch is **cosmetic**: one metric records a duration
six times too long, and `guardianctl register` prints a wall-clock estimate that is
six times out. No timing behaviour, no reveal decision, no scheduling. That is why
this is P3 and why changing the cadence is safe now — the guard in phase 3 is what
keeps that true rather than accidental.

## 4. Target shape

| Component | Where the cadence comes from | Runtime override |
|---|---|---|
| chain devnet (native and compose) | the `networks.json` entry, read in one place | `TIMEFLARE_BLOCK_TIME` |
| guardian | derived from the network entry it already reads, at `config init` | `block_time` in its own config, and an environment override for a devnet run |
| SDK | measured from block samples, as now; fallback from the network entry the client fetches | n/a — it observes rather than configures |
| CI | sets the override once; every component follows |  |

`networks.json` is the right home and the surrounding machinery already exists:
`CLAUDE.md` describes it as the single definition of the networks this chain runs
as, which consumers read to derive their defaults rather than each carrying its own
copy; it is deployment fact rather than protocol, so deliberately outside
`docs/spec.md`; `make verify` already fails when it drifts from `app/app.go` and
the devnet scripts; and it is **already read by the guardian**
(`internal/config/networks.go`) and by the mobile client at launch. Its
per-network entry carries `id`, `label`, `chainId`, `local` and `endpoints` — the
cadence belongs beside them.

**The override must reach the guardian, not stop at the chain.** That is the gap
that produced the live mismatch, and a local `TIMEFLARE_BLOCK_TIME=2s` run is only
trustworthy once both move together — the difference between a fifteen-minute
scenario suite and a seventy-minute one.

## 5. Phases

**Phase 1 — one definition, chain-side.** Add the cadence to `networks.json`; the
three devnet scripts read it; the three `:-6s` and three `:-2s` literals collapse
to one override applied once; `make verify` catches drift as it already does for
the rest of the file; `docs/guides/NETWORKS.md` gains the field. No consumer
changes, so this lands alone.

**Phase 2 — the guardian derives it.** `guardianctl config init` sets `block_time`
from the network entry, with an environment override for devnet runs, and
`polling_interval` derived from it so the existing
`polling_interval < block_time/2` validation describes one chain rather than two.
This corrects the metric and the register display. Needs a chain release carrying
phase 1, then a guardian release.

**Phase 3 — guard the invariant.** A `make verify` check in the house style of
`verify-boundaries` and `verify-choke-points`: below a presentation surface, no
symbol converts between blocks and wall-clock. The audit in §3 is the baseline —
the chain has none, the guardian's three are display and metrics, the SDK's are
confined to `blockclock.ts`. The check is what stops a fourth appearing in a keeper
or a reveal path, where it would turn a cosmetic mismatch into a behavioural one.

**Phase 4 — the SDK's fallback.** Source the unmeasured fallback from the network
entry the client already fetches, leaving the constant as a last resort. Settles
`ANCHOR_TARGET_BLOCKS` per §6 Q1.

**Phase 5 — decide the default.** With the cadence coherent end to end, validate
the scenario suite at `2s` locally and decide whether the devnet default moves off
`6s`. Last deliberately: a faster devnet is only worth having once a fast run
means the same thing as a slow one.

## 6. Open questions

**Q1 — `ANCHOR_TARGET_BLOCKS` converts a day into blocks at module load.**
`blockclock.ts:347` divides twenty-four hours by the constant to size the block
clock's anchor sampling distances (a day, a quarter, a sixteenth). It is the one
time→block conversion in the SDK, it cannot see a runtime-measured cadence, and
under §1 it sits below the presentation surface.
*Recommendation*: keep it derived from the constant and state in the code that it
is a sampling distance rather than a timing guarantee — a sample spaced "about a
day" is doing its job at any cadence. If the guard in phase 3 would flag it,
that is the right prompt to make the sampling distances a function of the measured
clock instead.

**Q2 — does the guardian need `block_time` at all?** Its three uses are metrics
and operator display. Deriving it (phase 2) keeps the operator output honest;
removing it would mean the metric records blocks and `register` prints block
counts, which is what §1 argues for everywhere below a surface — and
`guardianctl` output *is* a surface.
*Recommendation*: derive rather than remove. An operator reading "available for
about a year" is better served than one reading "5,256,000 blocks", and that is
exactly the conversion a surface is allowed to make.

**Q3 — how is the override expressed for a running daemon?** The chain's cadence
is `timeout_commit`, applied at node start, so "runtime" there means a restart with
a different value — which is what `dev-reset` already does. A guardian reads its
config at start too.
*Recommendation*: environment override honoured at start for both, no hot reload.
Hot-reloading a value that only affects display would add a reload path for no
behavioural gain.

**Q4 — one cadence per network, or devnet-only?** Testnet and mainnet cadence is
an operational decision that will not be a devnet default, and naming a number for
networks that do not exist invents a fact.
*Recommendation*: per-network and optional; absent means "measure it", which is
what the SDK already does.

## 7. What this plan does not solve

- **Consensus timing.** `timeout_commit` remains the chain's. This stops three
  scripts from each having an opinion about its default; it does not change how
  consensus is tuned.
- **Protocol semantics.** Every window in `docs/spec.md` stays denominated in
  blocks. Cadence changes what a window costs in seconds, not what it means, which
  is why this is deployment fact and not a spec change.
- **The guardian's `polling_interval` upper bound.** Its validation catches polling
  faster than half the block time; nothing catches polling too slow for the
  cadence. Worth adding, but it is a guardian-side change with its own argument.
- **Suite duration.** Phase 5 decides the default only. The suites are dominated by
  block-denominated waits, and shortening those would change what they test.
