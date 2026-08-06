# Block-Time Configuration — Plan

*Reduces the block cadence to two values in two homes — the deployment value and
the test value — leaves the components that do not need it carrying nothing, and
puts a guard under the rule that time appears only at a presentation surface.
Today the number is a literal in eleven places across two repositories, expressing
three different values, and CI runs a 1s chain against guardians configured for 6s.*

> **Status: done** — created and ruled 5 August 2026 (§6), executed 6 August 2026:
> phase 1 (chain #25), phase 2 (guardian #15), phase 3 (chain #27), phase 4
> (typescript-sdk #13). Phase 5 verified the collapse rather than changing anything:
> `make e2e-full` passed at the test cadence with three validators, agreeing on the
> app hash at height 194, so nothing depended on the `2s` defaults.
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

The invariant separates three things, and only the first is forbidden:

- **Converting protocol meaning** — turning a window, deadline or interval into
  seconds to decide something. This is what must not exist below a surface.
- **Converting at a surface** — a creator picks a date and the SDK turns it into a
  reveal height; a recipient is shown a date. Allowed, and the SDK insists on a
  *measured* cadence for the seal because, as `blockclock.ts` puts it, an
  unmeasured clock there "does not mislabel a date, it commits the secret to the
  wrong block".
- **Sizing a measurement window** — `ANCHOR_TARGET_BLOCKS` reaches back "about a
  day" of blocks to measure the real cadence, far enough to average out jitter and
  near enough that a pruned node still holds the block. It uses the seed estimate
  because there is no measurement yet; that is the chicken and egg, not a
  conversion of anything the protocol means.

### The rule

**Two values, two homes.**

- **6s is the real cadence**, defined once in `networks.json` per network. A devnet
  brought up for interactive work runs at it, because that is what the deployment
  is.
- **1s is the test cadence**, defined once, applied only by a path that exists to
  run tests.

**One default, one canonical test value, and overrides free.**
`TIMEFLARE_BLOCK_TIME` sets the cadence to whatever a test, a bespoke devnet or an
experiment needs, and nothing restricts the choice — two seconds, thirty seconds, it
is the run's business. What is disciplined is where the *default* comes from: the
registry states it, and no target, script or workflow carries one of its own.

The three `2s` defaults fail that, not because two seconds is wrong but because
nothing records why they chose it. They take the canonical test value unless a
reason to keep a distinct one turns up — and then the reason is written down beside
it.

**A test path owns bringing the chain up at the test cadence.** The cadence is a
property of the running chain, not of the command that drives it, so `make e2e` and
`make e2e-scenarios` inherit whatever the devnet was started with. That is a trap:
a 6s devnet is the default and correct for interactive work, and running the
scenario suite against it takes about ninety minutes rather than fifteen with
nothing to say why. Whatever sets the chain up for a test run therefore sets the
cadence too, and the suites name the cadence they found on the chain when it is not
the test one — long enough to notice before the wait, not after.

This plan therefore does two separable things: it reduces eleven literals to those
two definitions, and it puts a guard under the invariant so it stays true.

## 2. Why the definition needs centralising

The cadence is written eleven times as a literal, expressing **three** different
values — and only two of them were ever chosen on purpose:

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

The `2s` group is the clearer symptom. Three targets pick a cadence that is neither
the deployment value nor the test value, each with its own default, and nothing
records why two seconds. Under the rule they become the test value.

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
  `ANCHOR_TARGET_BLOCKS`, which sizes a measurement window rather than converting
  protocol meaning (§1).

So the cost of the live mismatch is **cosmetic**: one metric records a duration
six times too long, and `guardianctl register` prints a wall-clock estimate that is
six times out. No timing behaviour, no reveal decision, no scheduling. That is why
this is P3 and why changing the cadence is safe now — the guard in phase 3 is what
keeps that true rather than accidental.

## 4. Target shape

| Component | Where the cadence comes from | Runtime override |
|---|---|---|
| chain devnet (native and compose) | the `networks.json` entry, read in one place | `TIMEFLARE_BLOCK_TIME` |
| guardian | **nothing** — it records and prints block counts, and derives its poll interval from the network entry at `config init` without storing a cadence | the network entry the override already reached |
| SDK | measured from block samples, as now; fallback from the network entry the client fetches | n/a — it observes rather than configures |
| a devnet for interactive work (`dev-up`, `dev-reset`) | the `networks.json` entry — 6s, the deployment reality | `TIMEFLARE_BLOCK_TIME` for a one-off |
| test paths (CI, `e2e-full-native`, the dependency gate, compose full-E2E) | the single test cadence — `1s` — defined once, and the path sets it when it brings the chain up | as above |

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

**Phase 1 — two definitions, chain-side.** Add the cadence to `networks.json`; the
three devnet scripts read it; the three `:-6s` defaults go. The three `:-2s`
defaults and CI's two `1s` literals collapse to one test cadence defined once, which
every test path uses. `make verify` catches drift as it already does for the rest of
the file, and `docs/guides/NETWORKS.md` gains the field. No consumer changes, so
this lands alone.

One thing to verify rather than assume while collapsing `2s` to `1s`: the compose
full-E2E runs multiple validators (`make/docker.mk` passes `VALIDATOR_COUNT`), and
multi-validator consensus at one second is a different proposition from the single
validator the native devnet runs. If it holds, `2s` goes. If it does not, the compose
path keeps a value **and** the reason is written down beside it, which is what is
missing today.

The suites also gain the cadence check described in §1 — reading the chain's actual
cadence and saying so when it is not the test one. That is a few lines, and it is
the difference between a ninety-minute wait understood in advance and one
discovered at the end.

**Phase 2 — the guardian stops converting.** It needs no chain release: the
guardian fetches the registry from
`raw.githubusercontent.com/timeflareio/chain/main/networks.json`, so phase 1 being
on `main` is enough. `block_time` leaves its config entirely. `metrics.RecordReveal` takes blocks rather than a duration;
`register.go` prints block counts, naming the assumed cadence inline where a
human-readable figure helps ("5,256,000 blocks, about a year at 6s") so the
assumption is visible rather than embedded. `guardianctl config init` derives
`polling_interval` from the network entry's cadence at init time and does not
persist it, so the daemon has nothing to keep in step and the
`polling_interval < block_time/2` check moves to the one place that knows both.
Needs a chain release carrying phase 1, then a guardian release.

**Phase 3 — state the invariant where a reader meets it.** A rule in each
repository's `CLAUDE.md` rather than a `make` check. The chain's says never read the
block timestamp in `x/secrets`, with the reasoning that matters: the cadence is a
deployment fact, so a decision taken on wall-clock means something different on a
devnet than in CI, and the symptom reads as a protocol bug rather than the
configuration difference it is.

Not tooling, deliberately. A check written today would guard against something no
repository does: the chain never reads the timestamp, and after phase 2 the guardian
carries no cadence at all. The SDK is the one place a conversion legitimately lives,
confined to `blockclock.ts`, so a guard there would allowlist correct code rather
than prevent incorrect code. The instruction is where the rule will actually be met.

**Phase 4 — name the SDK's seed for what it is.** The constant stays hardcoded.
`BLOCK_TIME_ESTIMATE_MS` gains a comment saying it is a cold-start seed and a
sampling distance, not a timing guarantee, so the next reader does not mistake it
for a value that should track a network. `ANCHOR_TARGET_BLOCKS` is unchanged.

Nothing more, because nothing more is warranted. The seal path — the one
conversion that matters — refuses a seeded clock and measures, since an unmeasured
clock there commits the secret to the wrong block rather than mislabelling a date.
A seeded clock reports `measured: false` with a 5% band, so a consumer knows it
holds an estimate. And a seed that is too slow makes the anchor window shorter
rather than longer, which is safer against a pruned node. The consumers that do
know their network — the guardian and the mobile client — read `networks.json`
directly, so the knowledge lives where it already is rather than being plumbed
through a package whose value is staying small.

**Phase 5 — prove the collapse.** Done, and it changed nothing. `make e2e-full`
brought up a fresh three-validator compose stack at the test cadence, ran the
lifecycle, and confirmed app-hash agreement across all three at height 194. That
was the one risk worth checking before collapsing the `2s` defaults: three
validators independently executing every block at one second and having to agree.
They do, so no path keeps a cadence of its own and none needs a reason recorded.

## 6. Decisions — RULED (5 August 2026)

1. **Time appears only at a presentation surface; everything beneath is block
   periods** (§1). The two surfaces are the mobile client and `guardianctl`'s
   operator output. Changing the cadence is therefore safe: presented dates shift,
   which is what they always were — estimates of when a height would arrive.
2. **The guardian loses `block_time`.** Its three uses are metrics and display, so
   rather than derive the value it stops needing one: metrics record blocks, and
   `register` prints block counts. An operator-facing figure may name its assumed
   cadence inline, so the assumption is visible rather than configured.
3. **`ANCHOR_TARGET_BLOCKS` stays.** It sizes the window the block clock reaches
   back to *measure* cadence, not a protocol quantity, and it cannot use a
   measurement that does not exist yet. Phase 3's guard must therefore distinguish
   a measurement window from a conversion of protocol meaning.
4. **The SDK's seed stays hardcoded.** `BLOCK_TIME_ESTIMATE_MS` is a cold-start
   value the clock replaces as soon as it measures, and the seal path refuses to use
   it at all. The guardian and the mobile client lift the cadence from
   `networks.json`, which is where a component that needs to know its network should
   get it; the SDK does not gain a registry reader for a transient, self-labelled
   estimate.
5. **`networks.json` carries the cadence per network**, because a per-network value
   is the only honest shape — a testnet's cadence is an operational decision and
   naming one before the network exists invents a fact.
6. **The override exists for tests and devnet runs.** It is honoured at start, on
   the chain and on a guardian's `config init`. Hot-reloading a running chain is
   not what this solves.
7. **Two values, two homes — and overrides free** (§1). 6s is the shipping cadence
   in `networks.json`, which is why it is published: a downstream learns it from the
   registry. 1s is the canonical test value in `make/common.mk`. Neither is
   enforced on a run — `TIMEFLARE_BLOCK_TIME` sets any cadence a test or a bespoke
   devnet needs. What is checked is that no script carries its own default. The
   three `2s` defaults take the canonical value because nothing records why they
   chose two seconds, and a test path brings the chain up at the cadence it wants
   rather than inheriting one, with the suites reporting what they measured.

## 7. What this plan does not solve

- **Consensus timing.** `timeout_commit` remains the chain's. This stops three
  scripts from each having an opinion about its default; it does not change how
  consensus is tuned.
- **Protocol semantics.** Every window in `docs/spec.md` stays denominated in
  blocks. Cadence changes what a window costs in seconds, not what it means, which
  is why this is deployment fact and not a spec change.
- **The guardian's `polling_interval` upper bound.** Deriving the interval at
  `config init` (phase 2) means it starts out matched to the cadence, but nothing
  catches an operator later setting it too slow for the chain they are on. Worth
  adding, and it is a guardian-side change with its own argument.
- **Suite duration.** Phase 5 decides the default only. The suites are dominated by
  block-denominated waits, and shortening those would change what they test.
