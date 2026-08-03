# Guardian Daemon Improvements — Config-Driven Operation & Code Quality

*A full audit of `guardian/` (July 2026, ~7,900 non-test lines): make the daemon
primarily config-driven, fix the config plumbing that silently discards
operator settings, modernise the config tooling, and address the code-quality
findings — prioritised by reveal reliability, because this daemon's failures
get guardians slashed.*

> **Status: DONE — implemented (July 2026).** All six phases landed in one
> pass on `guardian-improvements-dep-management`: single tag-driven config
> schema with env overrides and round-trip tests, fully native gRPC client
> with in-process keyring signing (CLI proxy and `timeflared` runtime
> dependency deleted), architecture fixes (§6.2–6.10), live metrics and real
> health/readiness, and event-driven operation (§7) with polling demoted to
> fallback. Verified by the full devnet e2e lifecycle and failure-path
> scenario suites. Originally compiled
> from a systematic sweep (hard-coded-value inventory + architecture/quality
> review + config-tooling read); all §9 questions resolved: fully native gRPC
> client with in-process signing (CLI proxy and `timeflared` dependency
> removed, §6.1), no backward-compatibility constraints on config schema or
> guardian state, config-default + flag-override for one-shot commands, and a
> tag-driven config registry (no viper). Cross-checked against the code
> (July 2026): bypass count corrected to five, `ClientInterface` impact of
> §6.1 corrected (`ValidateBinary` also leaves the interface), and items
> superseded by the native client are now marked in place. The
> polling→event-driven migration formerly sketched in
> PENDING_GUARDIAN_MONITORING_PLAN.md is **consolidated here as §7** — that
> plan predated these resolutions (viper, CLI proxy, two-struct config) and
> has been deleted; the code TODOs and doc links that referenced it have been
> repointed at this plan.

## Contents

1. [Headline finding: the config pipeline drops settings](#1-headline-finding-the-config-pipeline-drops-settings)
2. [Config-driven operation — new and repaired configuration](#2-config-driven-operation--new-and-repaired-configuration)
3. [Config tooling improvements](#3-config-tooling-improvements)
4. [Reveal reliability](#4-reveal-reliability)
5. [Observability that doesn't observe](#5-observability-that-doesnt-observe)
6. [Architecture & code quality](#6-architecture--code-quality)
7. [Event-driven operation](#7-event-driven-operation-consolidated-from-the-retired-monitoring-plan)
8. [Suggested phasing](#8-suggested-phasing)
9. [Open questions](#9-open-questions--all-resolved-july-2026)

---

## 1. Headline finding: the config pipeline drops settings

Five documented, settable, validated config keys **do nothing**:

| Key | What happens to it |
|---|---|
| `polling_interval` | Parsed, validated… then `ToGuardianConfig()` (config/manager.go:980) **hard-codes `6 * time.Second`**, discarding the value. Every consumer runs at 6s regardless |
| `retry_attempts` | Defaulted to 3 in `Config`, never carried into `GuardianConfig`; `blockchain/proxy.go:35` re-hard-codes `3` |
| `enable_metrics` | Never read outside the config package |
| `enable_health_check` | Never read outside the config package |
| `log_file_path` | Never read outside the config package |

And the `timeflared_binary` key — which the blockchain proxy honours — is
**bypassed at five call sites** that hard-code the literal `"timeflared"`:
`cmd/guardiand/cmd/register.go:176,366`, `update.go:131`, `utils.go:53`,
`config.go:676` (a sixth site, `utils.go:81`, executes the vector built at
register.go:176).

These are bugs, not style: an operator who tunes polling for a fast devnet, or
points at a non-PATH binary, gets silently ignored. Fixing the pipeline is the
first work item and the reason "config-driven" needs more than adding keys —
it needs a structure where a key that exists is *necessarily* wired (§3.1).

## 2. Config-driven operation — new and repaired configuration

Full inventory (file:line in the audit). Grouped by what should happen.

### 2.1 Repair (exists but broken — §1)
`polling_interval`, `retry_attempts`, `enable_metrics`, `enable_health_check`,
`log_file_path`. The five `timeflared_binary` bypasses get a **trivial interim
routing in phase 1** (one-line changes threading `cfg.TimeflaredBinary`
through each site), explicitly throwaway: the native client (§6.1) then
deletes the binary dependency, the config key, and these call sites wholesale
in phase 5. Cheap enough to keep the config honest through phases 1–4.

### 2.2 Promote to configuration (hard-coded today, genuinely operator-facing)

| Proposed key | Today | Where |
|---|---|---|
| `command_timeout` (becomes `request_timeout` when §6.1 lands) | `30s` per timeflared invocation | blockchain/proxy.go:34 |
| `retry_backoff` | `2s` linear base (the key survives §6.1; retryability *classification* moves to gRPC status codes) | proxy.go:35–36, strategy at :181 |
| `stake_amount` (register default) | `"10000000000uveil"` **hard-coded ×4** | guardian/registration.go:126,134,166,168 |
| `fee_buffer_percent` | `/100` (1%) duplicated ×2 | registration.go:140,286 |
| `cache_max_age` | `7*24h` | guardian/cache.go:76 |
| `cache_cleanup_interval` | `50` blocks | cache.go:75 |
| `bind_address` | implicit `0.0.0.0` (`":%d"`) — one key covering **both** the metrics and health listeners, while all display URLs say `localhost` | monitoring/service.go:170,180 |
| `shutdown_timeout` | `30s` at start.go:180 — a **single** key: the second `5s` literal (monitoring/service.go:213) exists only because `Stop` discards the caller's context, and vanishes when §6.7 fixes that | cmd/start.go:180 |
| `block_time` | `6s` assumption duplicated 4+ places (display maths, availability window, polling default) — informs display maths and derived defaults only; consensus timing stays the chain's | register.go:232,299; registration.go:316; manager.go:980 |

Two rows removed from earlier drafts, with reasons: `available_until`'s
register default (`5256000`, registration.go:316) is the chain's
`MaxAvailabilityWindow` constant — single-source it instead (§2.4); and
`known_secrets_ttl` (monitor.go:162) lives only inside dead code that §6.2
deletes outright.

### 2.3 Centralise as named defaults (config defaults, not user config)
Histogram buckets (monitoring/service.go:102); CLI flag defaults
(`startup-timeout` 30s, `status --timeout` 30s, `health --timeout` 10s —
three unrelated "wait" literals); monitor-name fallback `"Timeflare
Guardian"` (registration.go:161 duplicating manager.go:394); health-URL
scheme/host (`http://localhost`); output truncation length (client.go:226).

Superseded by §6.1 — do not centralise, they are deleted with the proxy:
gas mode `"auto"` (proxy.go:99 — gas simulation moves into the tx factory;
the `gas_adjustment`/`gas_price` config keys survive), the passphrase
3-prompt repetition (×3 sites), and proxy.go:261's error truncation.

### 2.4 Leave fixed, but single-source
Protocol/wire contracts stay constants but need one authoritative home each:
protocol state/enum strings (cache.go), bech32 prefix —
currently checked as `"tmflr1"` in config.go:133 but `"tmflr"` in
registration.go:232 (inconsistent), health port `8080` baked into register.go's
success message (ignores configured `HealthPort`), metric name
`guardian_balance_uveil` baking the denom into a metric name, and the register
default `available_until = 5256000` (registration.go:316) — this **is** the
chain's `MaxAvailabilityWindow` (x/secrets/types/constants.go:176), already
importable via the existing main-module dependency, so reference the constant
rather than promoting a config key (registration.go's own comment notes the
equivalence; the per-invocation `register` flag override remains). Units are
relative block offsets — the chain converts to absolute heights itself.
(CLI subcommand vectors were dropped from this list: §6.1 deletes them with
the proxy.)

## 3. Config tooling improvements

### 3.1 One schema, not two
There are **two parallel config structs** — `config.Config` (yaml, manager.go)
and `config.GuardianConfig` (mapstructure+yaml, config.go) — bridged by a
hand-written `ToGuardianConfig()`. That bridge is exactly where
`polling_interval` died and where `retry_attempts` was never carried; two
validation paths exist (`validateConfig` vs `Manager.Validate`), with
duplicated log-level validators. **Recommendation: collapse to a single struct
with typed fields** (`time.Duration`, not the string `PollingInterval`);
delete the conversion layer. A field that exists is then wired by
construction — the §1 bug class becomes impossible.

### 3.2 Replace the hand-rolled parameter registry
`configParams` (manager.go:25–~300) hand-writes Get/Set/AltKey/validator
closures per field — ~300 lines of boilerplate that must be kept in sync with
the struct, the defaults, and `generateDocumentedConfig`. Options:
struct-tag-driven reflection (`config:"key,desc=…"` + one generic get/set), or
adopt viper (the mapstructure tags already suggest it was intended). Either
removes the class of "field exists in struct but not in registry" drift.

### 3.3 Environment-variable overrides
No env support exists (`GUARDIAN_RPC_ENDPOINT=…` etc.) — table stakes for
container deployment and what the mapstructure tags imply was planned.
Precedence: flags > env > file > defaults.

### 3.4 Validation gaps
- Default `metrics_port: 9090` **collides with the default
  `grpc_endpoint: localhost:9090`** — broken out of the box on one host;
  validation checks metrics≠health but not against the endpoint ports.
- Binary validation is only *partially* missing: the daemon already calls
  `ValidateBinary` at startup (service.go:100,125); the actual gap is config
  load and the one-shot CLI commands, which shell out unvalidated. Superseded
  by §6.1 (no binary to validate) — skip unless the native client is more
  than a branch away.
- `validateGuardianAddress` hand-rolls bech32 (prefix/length/charset) —
  use the cosmos bech32 decoder and derive the prefix from one constant.
- Cross-field checks: `polling_interval` vs `block_time` sanity;
  `bind_address`+port availability at start.

### 3.5 Tooling UX
- `config set polling_interval 2s` succeeds today but has no effect (§1) —
  after 3.1 this is fixed structurally; add a `config doctor` that reports
  effective values *as the running service would consume them*.
- `generateDocumentedConfig` (manager.go:493) hand-maintains the documented
  YAML — generate it from the single schema so docs can't drift.
- `config get/list` should mark values that differ from defaults.
- `config/manager.go` has **no test file**; the §1 regression would have been
  caught by a round-trip test (`set → save → load → effective value`).

## 4. Reveal reliability

*(A missed reveal is a slashed bond — these outrank everything except §1.)*

1. **Serial, process-spawning reveals can overrun the window**
   (service.go:341→reveal.go:293→client.go:312). Each reveal = a `timeflared`
   subprocess with 30s timeout ×3 retries, processed one at a time. Many
   secrets sharing a window can exceed it → genuine no-shows. Direction:
   bounded parallel reveal workers; structurally, see §6.1.
2. **Retry logic almost never fires** (proxy.go:196–221): retryability is
   classified on `err.Error()` from `exec`, which is `exit status 1` — the
   real "connection refused"/"EOF" text is in the *output*. Transient RPC
   blips are treated as fatal. Cheap, high-value fix; a test would have
   caught it (proxy.go has none).
3. **One failed height query skips the whole tick** (service.go:310–314) —
   confirmations *and* reveals skipped for that interval. Use last-known
   height + degrade gracefully.
4. **No window-passed-while-down signal**: restart recovery is correctly
   idempotent (cache rebuilds from chain), but a reveal missed during
   downtime surfaces only as a too-late `Warn` (reveal.go:212). Emit the
   leading-indicator metric/alert (ties into §5).

## 5. Observability that doesn't observe

1. **Every Prometheus metric is dead** — registered
   (monitoring/service.go:64–159) but `GetMetrics()` has zero callers;
   `SuccessfulReveals`, `FailedReveals`, `RevealTiming`, `GuardianBalance` etc.
   are 0 forever. The monitoring service and guardian service are never
   wired together. Inject metrics into the reveal/confirm paths.
2. **`/health` and `/ready` always return 200** (monitoring/service.go:235–251,
   both TODO) — a wedged loop, dead RPC, or unloadable key still reports
   healthy, so supervisors never restart a broken guardian. Back them with
   real checks: last-successful-poll age, RPC reachability, key loadable.
3. In-process equivalents are also stubs: `checkMonitoringHealth` returns
   healthy unconditionally (service.go:387), `getRecentActivity` returns
   empty.
4. Structured-field gaps on the paths that matter (no window/height/attempt
   on reveal-failure logs; no counter for lost acceptance races).

*(This plan is the sole authority for the observability work. Note the tie-in
with §7: once event-driven operation lands, subscription liveness and
last-block-header age become the primary health/readiness signals.)*

## 6. Architecture & code quality

### 6.1 The CLI proxy: what it is, why it exists, and its replacement

**What the proxy is.** `blockchain/proxy.go` is a subprocess wrapper around
the `timeflared` binary: it builds argv vectors, spawns a process per chain
interaction, applies a 30s timeout and (intendedly) retries, pipes the keyring
passphrase to stdin three times, captures combined output, and hands the text
back for parsing. `blockchain/client.go` then implements every query and
transaction on top of it by formatting CLI subcommands and scraping stdout
("find first `{`", tx-hash regexes, string-matched errors).

**Why it exists — one real reason.** Queries never needed it (no key
involved). The proxy exists because **transactions need signing**, and the
guardian's key lives in a cosmos keyring: shelling out to `timeflared tx …`
delegated keyring access, account/sequence handling, gas estimation, signing
and broadcast to the chain binary rather than implementing them in Go. That
was expedient, and everything painful about the daemon follows from it: the
per-call process latency (§4.1), the retry misclassification (§4.2 — the
diagnostic text is in the subprocess *output* while `err` is `exit status 1`),
the passphrase-over-stdin hack, the parsing fragility, the
`timeflared_binary`/PATH runtime dependency, and the five CLI-layer sites that
bypass it entirely.

**Resolved (July 2026): replace it with a fully native client — including
transaction signing. The `timeflared` runtime dependency is removed
altogether.** The prerequisites already exist in `guardian/go.mod`: the module
imports the main timeflare module (all `Msg*`/`Query*` proto types and their
generated gRPC clients) and pins cosmos-sdk v0.53.2 (keyring, tx builder,
broadcast client). Design:

- **Queries**: `types.NewQueryClient(grpcConn)` against the already-configured
  (and today never-dialled) `grpc_endpoint`. Typed responses end the stdout
  parsing, the stringly-typed height fields (§6.4), and the error-substring
  matching (§6.8) at the root.
- **Transactions**: cosmos-sdk `keyring` (file backend, passphrase read once
  from the existing `keyring_passphrase` file — today a base64-encoded
  passphrase file re-read and piped to stdin ×3 on every command,
  proxy.go:133–141), `client/tx` factory for
  account/sequence resolution, gas simulation, signing, and broadcast over the
  same gRPC connection. Sequence handling in-process also enables safe
  concurrent submission (bounded, per-account-serialised) for §4.1.
- **Retry/timeout policy** moves to one place around the gRPC calls, driven by
  the §2.2 config keys (`command_timeout` becomes `request_timeout`), with
  gRPC status codes — not substrings — deciding retryability.
- **Deleted**: `blockchain/proxy.go`, the tx-hash/JSON scraping (×3 sites),
  the passphrase hack (×3 sites), the `timeflared_binary` config key and its
  five hard-coded call sites, and — from `ClientInterface` — both
  `GetCLIProxy()` and `ValidateBinary()` (interface.go:29,13): there is no
  proxy to expose and no binary to validate. The interface change is small
  but real: the mocks implement both (blockchain_mock.go:422 plus the
  `MockCLIProxy` type), and `service.go` calls `ValidateBinary` in its
  prerequisite and start paths (service.go:100,125) — all need matching
  edits (start-path validation becomes a gRPC connectivity check). The
  monitoring/reveal/cache logic itself is untouched.
- The `guardiand` CLI's own shell-outs (`keys show` for address resolution in
  register/update/config) move to the same in-process keyring — no subprocess
  anywhere.
### 6.2 Dead code: `guardian/monitor.go`

`SecretMonitor` (232 lines) is constructed (service.go:90) but its `Start` is
never called anywhere; `Service.runSecretMonitoring` duplicates it. Delete it —
its divergent 24h `knownSecrets` TTL (monitor.go:162) dies with it, which is
why that TTL is *not* promoted to config in §2.2.

### 6.3 Two divergent registration paths

`client.RegisterGuardian` (legacy) vs `registration.go:297` hand-building CLI
args via `GetCLIProxy()` — a layering violation. Fold into `ClientInterface`,
delete `GetCLIProxy()`.

### 6.4 Stringly-typed chain fields

client.go:36–64: heights/thresholds as strings, `strconv` on every access,
errors often discarded (a malformed height silently becomes 0). Parse once at
unmarshal into typed fields.

### 6.5 Hand-rolled `parseAmount`

registration.go:236 — digit-by-digit, no overflow guard, used for
balance-sufficiency checks. Use `strconv`/sdk coin parsing.

### 6.6 `Service.Start` holds the write lock for the daemon's lifetime

service.go:114 — `Start` takes `s.mu.Lock()` with a deferred unlock, then
blocks in `runSecretMonitoring` (service.go:159), so the write lock is held
for the daemon's lifetime; `GetStatus`/`Stop` on the running instance would
block forever. Latent deadlock, currently masked (shutdown happens via context
cancellation). Flip flags, release, then run.

### 6.7 Monitoring `Stop` ignores the caller's context

`monitoring.Service.Stop(ctx)` discards `ctx` and mints its own 5s context in
`shutdown()` (monitoring/service.go:206–213), ignoring the 30s the shutdown
path provides (cmd/start.go:180). Honouring the caller's context removes the
5s literal entirely — which is why §2.2 defines a single `shutdown_timeout`
key, not two. Also: HTTP-server goroutines die silently on bind failure (port
in use → guardian runs with no health/metrics and no error).

### 6.8 Error classification by substring matching

reveal.go:64–83, client.go:123 — replace with typed/sentinel errors from the
client layer.

### 6.9 Dead/unused exports

`GetSecret`, `GetThreshold`, cache wrappers (`GetAll`, `GetStateCount`,
`AddSecret`/`UpdateSecret`/`EvictSecret`), legacy `Service.Register` ignoring
its param, misleading "legacy fields" comment on the `Guardian` struct.

### 6.10 Test posture

No test file at all for `blockchain/proxy.go`, `blockchain/client.go`,
`guardian/registration.go`, `config/manager.go`, `monitoring/client.go` —
exactly where the audit found bugs (§4.2, §1).

## 7. Event-driven operation (consolidated from the retired monitoring plan)

*(The surviving intent of the deleted PENDING_GUARDIAN_MONITORING_PLAN.md,
consolidated July 2026: guardians should **react to operations that involve
them** rather than discover them by polling. That plan's mechanics were stale
— written against viper, the CLI proxy, and the two-struct config — so only
the goal and the event inventory carry over; everything below assumes this
plan's resolutions (single schema §3.1, native client §6.1).)*

### 7.1 Shape

Today the daemon discovers work by ticking every `polling_interval`
(service.go:289–307): query height → rebuild cache → act. Event-driven
inverts that — subscribe once, act when the chain says something happened:

- **Events**: a CometBFT WebSocket subscription (the `/websocket` endpoint of
  the already-configured `rpc_endpoint`) to secrets-module events. The chain
  already emits everything needed: the lifecycle transitions
  (`secret_reserved`, `secret_awaiting_acceptance`, `secret_pending`, … —
  x/secrets/types/constants.go:26–38), `assignment_accepted` /
  `assignment_rejected` (:42–44), and the settlement/slashing events, which
  already carry `guardian` attributes (endblock_logic.go:218,339,386).
- **Heights**: a new-block-header subscription replaces
  `GetCurrentBlockHeight` polling and gives exact reveal-window timing — the
  daemon acts *at* window-open height instead of up to one polling interval
  late, directly reducing the §4 slashing risk.
- **Polling demoted, not deleted**: the tick loop remains the degraded
  fallback when the socket drops — reconnect with backoff, then resync
  through the already-idempotent cache rebuild (cache.go:260,293). §4.3's
  tick resilience therefore still matters.

The CometBFT RPC client (WebSocket subscriptions included) ships with the
pinned cosmos-sdk — no new dependency.

### 7.2 Filtering: how a guardian knows an event involves it

Assignment-time events don't carry guardian addresses today —
`secret_reserved` (msg_server_request_guardians.go:366) and
`secret_awaiting_acceptance` (msg_server_distribute_shares.go:226) identify
secret and creator only. Two ways to get "events that involve *me*":

1. **Client-side filter (no chain change)** — subscribe to all secrets-module
   events; on each assignment-relevant one, a single native gRPC query
   (§6.1) answers "am I assigned?". One cheap query per new secret,
   network-wide.
2. **Chain-side enrichment (spec change)** — add assigned-guardian attributes
   to the assignment events, enabling server-side CometBFT query filters
   (`secret_reserved.guardian='tmflr1…'`).

**Resolved: start with (1)** — it works against the chain exactly as it is.
(2) is a compatible later optimisation, but it touches the event schema and
so needs explicit approval plus a spec.md update first; deliberately out of
scope here. (Logged as §9.5.)

### 7.3 Reveal scheduling within the window

Nothing protocol-side stops every guardian revealing at the first window
block (see PROTOCOL.md's implementation-gaps list, whose reference now points
here). The old plan's client-side anti-coordination carries over as a small
feature of this section: with exact heights from the block-header
subscription, schedule each reveal at window-open plus a bounded random
offset (`reveal_offset_blocks`, default small relative to the window, capped
so the §4.1 retry budget still fits before window close). Purely client-side;
no protocol enforcement implied or required.

### 7.4 Config & sequencing

New keys, defined in the single schema (§3.1) with tag-driven registration
(§3.2): `enable_event_monitoring` (default true once landed),
`event_reconnect_backoff`, `reveal_offset_blocks`, and `polling_interval`
re-documented as the *fallback* poll rate. Observability ties in per §5:
subscription liveness and last-header age become primary `/ready` signals.

Sequencing: this is **phase 6**. It wants the native client (phase 5) for
the cheap assignment queries and typed responses, and the §5 health plumbing
so subscription health is visible from day one.

## 8. Suggested phasing

1. **Config repair + collapse** (§1, §3.1–3.2, §2.1): single typed schema,
   delete the bridge, wire the five dead keys, interim-route the five
   hard-coded `timeflared` sites through `timeflared_binary` (one-line
   changes, explicitly throwaway — phase 5 deletes the key and the call
   sites, §2.1), tests for the round trip. *Everything else builds on this.*
2. **Reveal-reliability quick wins** (§4.2–4.4): retry classification, tick
   resilience, window-missed signal. Small diffs, direct slashing-risk
   reduction.
3. **Observability wiring** (§5): metrics into the reveal/confirm paths, real
   health/readiness.
4. **Config surface expansion** (§2.2–2.4): promote the inventory, env
   overrides (§3.3), validation gaps (§3.4), tooling UX (§3.5).
5. **Structural** (§6.1, §4.1, §6.2–6.5): the fully native client — gRPC
   queries + in-process keyring signing and broadcast, deleting the CLI
   proxy, the parsing hacks, and the `timeflared` runtime dependency —
   plus parallel reveals (unblocked by in-process sequence handling),
   delete monitor.go, unify registration, typed fields. Largest; possibly
   its own branch after 1–4. Note: landing this deletes several phase-1/2
   surfaces outright (retry classification, `command_timeout` semantics,
   binary validation), so if phases compress, jumping here early is
   legitimate — the quick fixes in §4.2 are only worth doing first if the
   native client is more than a branch away.
6. **Event-driven operation** (§7): WebSocket event + block-header
   subscriptions, client-side assignment filtering, polling demoted to
   fallback, reveal-offset scheduling. After phase 5 (needs the native
   client) — see §7.4.

## 9. Open questions — all resolved (July 2026)

1. ~~gRPC client vs CLI proxy~~ **Resolved: fully native client, including
   transaction signing; the CLI proxy and the `timeflared` runtime dependency
   are removed entirely.** Design and deletion list in §6.1. (Supersedes the
   earlier queries-first lean — the dependencies for in-process signing
   already exist in the module.)
2. ~~Config schema break~~ **Resolved: no backward-compatibility constraints.**
   Existing guardian configs/state need no preservation — the schema, key
   names, defaults and semantics are all freely improvable (devnet regenerates
   configs on reset).
3. ~~Config vs flags~~ **Resolved: config default + flag override.** Flags
   exist only on `guardiand`'s one-shot CLI commands (`register`, `update`,
   `status`, `health`, `config`) — they are per-invocation overrides, nothing
   more. The long-running daemon (`start`) is configured purely by
   config + env (§3.3); it grows no operational flags. The native client
   (§6.1) doesn't change this split: operators still register/update via
   the CLI commands, which read config defaults and accept flag overrides —
   it only changes what those commands do underneath (in-process signing
   instead of shelling out).
4. ~~Viper vs hand-rolled reflection~~ **Resolved: no viper.** A small
   tag-driven registry (struct tags carry key/description/validation; one
   generic get/set walks the fields) replaces the ~300-line hand-written
   `configParams` map, and env-var binding (§3.3) is derived from the same
   tags. The vestigial `mapstructure` tags are dropped with the
   `GuardianConfig` struct in §3.1.
5. ~~Event filtering: client-side vs chain-side~~ **Resolved: client-side
   first.** The guardian subscribes to all secrets-module events and answers
   "am I assigned?" with one native gRPC query per new secret (§7.2).
   Chain-side event enrichment (guardian attributes on assignment events,
   enabling server-side subscription filters) is a compatible later
   optimisation — but it changes the event schema, so it requires explicit
   approval and a spec.md update before anyone implements it.
