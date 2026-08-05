# Block-Time Configuration — Plan

*Gives the devnet's block cadence one definition that every component derives
from, and one override for scenario testing. Today the number is written
independently in eleven places across two repositories, and CI runs a 1s chain
against guardians that believe blocks take six seconds.*

> **Status: refining** — created 5 August 2026. §6 carries the open questions;
> not executable until they are ruled and folded into the body.
> **Priority**: P2 — a live mismatch in CI, and the thing that makes a local
> scenario run cost seventy minutes instead of fifteen.
> **Origin**: a question about why the local devnet runs at 6s while the suites
> wait on block-denominated deadlines, August 2026. The survey below is what that
> question turned up.
> **Components**: `networks.json`, `devnet/chain/setup-chain.sh`,
> `devnet/docker/init-chain.sh`, `devnet/docker/generate-compose.sh`,
> `devnet/guardians.sh`, `make/devnet.mk`, `make/deps.mk`, `make/docker.mk`,
> `.github/workflows/ci.yml`, `docs/guides/NETWORKS.md`; in
> `timeflareio/guardian`, `internal/config/{config.go,networks.go}` and
> `internal/cli/config_init.go`; in `timeflareio/typescript-sdk`,
> `src/protocol/{constants.ts,blockclock.ts}`.

## 1. What this plan does

Three things:

1. **Names the cadence once**, in `networks.json`, which already exists to hold
   exactly this class of fact and is already read by the guardian and the mobile
   client.
2. **Derives the guardian's `block_time` from it**, so the value it uses for
   timing maths is the value the chain is actually running at.
3. **Keeps one override** — `TIMEFLARE_BLOCK_TIME` — that reaches every component
   rather than stopping at the chain, which is what makes a fast local run
   trustworthy.

## 2. Why

### The number is written eleven times

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
| `guardian/internal/config/config.go:186` | `6 * time.Second` | the guardian's timing maths |
| `typescript-sdk/src/protocol/constants.ts:167` | `6_000` | the SDK's unmeasured fallback |
| `guardian`'s generated `config.yaml` | `block_time: 6s` | what a running daemon believes |

Nothing derives from anything. Every one of those is a literal.

### The consequence is already live

CI sets `TIMEFLARE_BLOCK_TIME: 1s` for both the devnet and the suites, with the
reason recorded in the workflow: lifecycle waits are chain-distance in blocks, so
the 6s default would take the suite past forty minutes. The guardians in that
same run are configured `block_time: 6s`, because their value comes from a Go
default that never sees the environment variable.

So CI runs a **1s chain against 6s guardians**, and has been doing so silently.

### The exposure is uneven, which is what makes it worth ranking

- **The guardian is most exposed.** `internal/guardian/reveal.go:207` computes
  `sinceWindowOpen` as `(currentHeight - RevealStartBlock) × config.BlockTime`.
  At a 1s chain with a 6s config that overstates elapsed wall-clock sixfold, and
  it feeds reveal timing and the monitoring metrics. `internal/cli/register.go`
  converts availability windows through the same value.
- **The SDK is least exposed**, and deliberately so: `blockclock.ts` *measures*
  the cadence from block samples and falls back to the constant only when it has
  none. Its one hard dependency is `ANCHOR_TARGET_BLOCKS`, computed from the
  constant at module load (`blockclock.ts:347`).
- **The chain is not exposed at all.** Consensus timing is `timeout_commit`, and
  the protocol is denominated in blocks throughout, so the cadence changes what a
  window costs in seconds and nothing about what it means.

The guardian's own config comment claims the value is for "display maths and
derived defaults only; consensus timing stays the chain's". Whether `reveal.go` is
consistent with that claim is §6 Q1, and it decides whether the live mismatch is
cosmetic or behavioural.

## 3. Why `networks.json`

It already exists for this, and the surrounding machinery is already built:

- `CLAUDE.md` describes it as "the single definition of the networks this chain
  runs as… which consumers read to derive their defaults rather than each
  carrying its own copy of a chain id, a port and the address prefix".
- It is explicitly **deployment fact, not protocol**, and therefore deliberately
  outside `docs/spec.md`. Block cadence is the same kind of fact as the endpoints
  and address prefix already in it.
- `make verify` already fails when it drifts from `app/app.go` or the devnet
  scripts, so there is an enforcement point to extend rather than invent.
- **The guardian already reads it** (`internal/config/networks.go`, with tests),
  and the mobile client reads it at launch (`app/src/state/settings.ts`). Adding a
  field reaches two consumers through plumbing that exists.

Its per-network entry currently carries `id`, `label`, `chainId`, `local` and
`endpoints`. The cadence belongs beside them.

## 4. Target shape

| Component | Where the cadence comes from |
|---|---|
| chain devnet (native and compose) | the network entry, with `TIMEFLARE_BLOCK_TIME` as the single override, applied in one place |
| guardian | `guardianctl config init` derives `block_time` — and `polling_interval` from it — out of the network entry it already reads |
| SDK | measured from block samples, as now; the fallback comes from the network entry the client fetches, with the constant as a last resort |
| CI | sets the override once; every component follows |

Two properties this buys:

- The guardian's existing validation —
  `polling_interval < block_time/2 → "pointless load"` — starts meaning
  something, because both sides describe the same chain.
- A local `TIMEFLARE_BLOCK_TIME=2s` run is trustworthy, because the guardians
  move with it. That is the difference between a fifteen-minute scenario suite and
  a seventy-minute one.

## 5. Phases

**Phase 1 — one definition, chain-side.** Add the cadence to `networks.json`,
have the three devnet scripts read it, collapse the three `:-6s` and three `:-2s`
literals to one override applied once, and extend `make verify` to catch drift.
`docs/guides/NETWORKS.md` gains the field. No consumer changes, so this is
landable alone.

**Phase 2 — the guardian derives it.** `guardianctl config init` sets `block_time`
and `polling_interval` from the network entry. This closes the CI mismatch and is
the phase that changes behaviour, so it wants its own review of §6 Q1's findings.
Needs a chain release carrying phase 1's `networks.json`, then a guardian release.

**Phase 3 — the SDK's fallback.** The measured path is already correct; this only
sources the unmeasured fallback from the network entry, and settles what to do
about `ANCHOR_TARGET_BLOCKS` (§6 Q2).

**Phase 4 — adopt a faster default for the suites.** With the cadence coherent
end to end, validate the scenario suite at `2s` locally and decide whether the
devnet default should move off `6s`. Deliberately last: a faster devnet is only
worth having once a fast run means the same thing as a slow one.

## 6. Open questions

**Q1 — is the guardian's `block_time` really display-only?** Its config
description says so, but `reveal.go:207` uses it to compute how long a reveal
window has been open, and `internal/monitoring/metrics.go` records timing derived
from `blocks × blockTime`. If either feeds a decision rather than a log line, the
live CI mismatch is behavioural and phase 2 is a fix rather than a tidy.
*Recommendation*: read those two paths before anything else in this plan; the
answer sets its priority.

**Q2 — `ANCHOR_TARGET_BLOCKS` is computed at module load.** It divides a day by
the block-time estimate, so a value discovered at runtime cannot reach it without
restructuring how it is exposed.
*Recommendation*: leave it derived from the constant and document that it is a
sizing heuristic rather than a timing guarantee — unless Q1 shows client-side
anchoring is sensitive to the real cadence, in which case it becomes a function of
the measured clock.

**Q3 — does the compose devnet need its own answer?** It passes
`TIMEFLARE_BLOCK_TIME` through a generated compose file, so the value crosses a
container boundary rather than a shell.
*Recommendation*: same source, same override; the generator reads the network
entry like the native scripts, and the compose file remains a rendering of it.

**Q4 — one cadence per network, or a devnet-only field?** Testnet and mainnet
cadence is an operational decision that will not be a devnet default, and putting
a number for them in `networks.json` before either exists invents a fact.
*Recommendation*: the field is per-network and optional; absent means "ask the
chain", which is what the SDK already does by measuring.

## 7. What this plan does not solve

- **Consensus timing.** `timeout_commit` remains the chain's, and this plan only
  stops three scripts from each having their own opinion about its default.
- **Protocol semantics.** Every window in `docs/spec.md` is denominated in blocks
  and stays that way. Cadence changes what a window costs in seconds, not what it
  means, which is why this is deployment fact and not a spec change.
- **The guardian's `polling_interval` upper bound.** Its validation catches
  polling faster than half the block time; nothing catches polling too slow for
  the cadence, which is the failure a fast devnet would produce. Worth adding, but
  it is a guardian-side change with its own argument to make.
- **Making the suites fast.** Phase 4 only decides whether the default should
  move. The suites' duration is dominated by block-denominated waits, and shortening
  those would be a change to what they test.
