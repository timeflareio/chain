# Guardian Dashboard — Plan

*A read-only, browser-based dashboard embedded in `guardiand`, giving a
guardian operator visibility into operations, assignments, economics, keys
and configuration — served from the daemon's existing monitoring surface,
styled to the Timeflare brand, with no new binary, service, or build
toolchain.*

> **Status: done — merged 28 July 2026 as PR #113.** All five phases
> landed: buffers/snapshots, the JSON API, the embedded UI, config +
> devnet + docs wiring, and verification. Scope confirmed by the owner
> (read-only v1; alerting excluded — Prometheus/Grafana own that later;
> state-changing actions deferred to a future "runbook mode"; no
> dashboard persistence — history panels are since-process-start,
> durable history belongs to the metrics work-stream; dedicated
> `dashboard_port` 21200, enabled by default on the shared `bind_address`,
> with authentication deferred to the runbook-mode plan — §2). **Ready —
> 28 July 2026**; no open questions.
>
> **Priority**: P2 — operator visibility. Guardians currently have no view
> of their own bond exposure, `k` trajectory, reveal obligations or config
> drift beyond raw CLI queries and log lines.
>
> **Origin**: owner design session, July 2026. Delivery shape ruled by the
> owner: **embed in `guardiand`** — the daemon already runs the monitoring
> HTTP service (`guardian/monitoring/service.go`, `/health` + `/ready` +
> `/metrics` listeners) and holds all daemon-local state, so this extends
> an existing component (architectural-minimalism rule satisfied; no new
> module, binary or service).
>
> **Components**:
> - `guardian/monitoring/` — dashboard listener, JSON API handlers
> - `guardian/dashboard/` — new *package* (same module): embedded static
>   UI assets (`go:embed`), snapshot assembly
> - `guardian/guardian/` — expose read snapshots of the active-secret
>   cache; in-memory since-start event buffers (decisions, submissions,
>   observed settlements)
> - `guardian/blockchain/` — widen the daemon's **projections**, not the
>   protocol: `Secret` gains the economics the panels quote (reward pool,
>   `[min_shares, max_shares]`, commit deadline, per-guardian bond) and
>   `Guardian` gains `bond_k` and `active_bond_count`. All of it is already
>   returned by the existing queries and simply dropped on parse today, so
>   panels 5 and 8–11 cannot be built without this. Still no proto or chain
>   change — the wire contract is untouched
> - `guardian/config/` — dashboard block (enable, port)
> - `mobile-client/design/` — brand source of truth (read-only reference;
>   tokens + logo copied into the embedded assets, provenance-commented)
> - `devnet/guardians.sh`, `docs/guides/TESTING_COMMANDS.md`,
>   `guardian/README` surface — docs sweep
> - **No proto, chain, or spec.md changes** (see §4 — daemon-local data
>   sources suffice; `docs/spec.md` untouched because nothing
>   protocol-visible changes)

## 1. Scope (owner-confirmed feature set)

Read-only throughout. Anything that would sign a transaction is out of
scope, deferred to a future "runbook mode" plan (first candidates: key
rotation, guardian updates).

### Operations & liveness
1. **Daemon vitals** — uptime, version, config path, RPC endpoint in use,
   node sync height vs. chain height (lag), event-stream state
   (websocket/polling, last processed height, catch-up backlog).
2. **Work queue & reveal calendar** — assignments awaiting offline
   verification, confirmations pending submission, and every accepted
   secret's reveal window as a block countdown (including the planned
   per-secret reveal height when `reveal_offset_blocks` is configured).
3. **At-risk reveals** — urgency-ordered view of unrevealed shares inside
   or approaching their window. Display only; notification is
   Grafana/Alertmanager territory, outside this plan.
4. **Transaction outcomes** — recent confirm/reveal submissions with tx
   hash, gas, success/failure; `settlement_stalled` events touching own
   secrets.

### Assignments & lifecycle
5. **Active assignments table** — secret id, state, commit-deadline
   countdown, window bounds, bond locked, reward-share floor
   (`P ÷ max_shares`).
6. **Decision log** — each accept/reject with the recorded reason
   (float insufficient, concurrency cap, policy declined, HMAC failure).
7. **History** — settled secrets with outcome and per-secret earnings;
   rejected and expired candidacies.

### Economics
8. **Float panel** — total / locked / unlocked; affordability ("~N more
   bonds at recent typical secret size"); concurrency headroom (active
   bonds vs. the cap of 100).
9. **Bond multiplier `k`** — current value, step history (×1.26 per
   slash, ×0.963 per reveal), projected correct-reveals back to the
   floor.
10. **Earnings ledger** — rewards, cancellation wages, bond returns vs.
    gas spend; earnings per block bonded.
11. **Risk exposure** — total VEIL bonded, largest single bond, slash
    history with splits.
12. **Signing account balance** — gas funds display (thresholding is
    Grafana's job).

### Keys & rotation (visibility only)
13. **Key identity card** — registered encryption pubkey fingerprint vs.
    local key file (the startup self-check, shown continuously), account
    address, encrypted-at-rest status with a prominent legacy-plaintext
    warning.
14. **Rotation panel** — read-only view over the shipped key-rotation state
    ([done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md](DONE_GUARDIAN_KEY_ROTATION_PLAN.md)):
    the current epoch (`Guardian.current_key_epoch`, 0 at registration and
    +1 per `MsgGuardianRotateKey`), the epoch history with each entry's
    effective height from the key-epochs query, rotation eligibility against
    the one-per-guardian-per-interval rate limit, and secrets still bound to
    an outgoing epoch — each assignment is permanently bound to the epoch key
    it was created under, so wind-down is "how many of my active assignments
    predate the current epoch", answerable from the daemon's own cache
    without a chain call. The act of rotating stays in the CLI
    (`guardiand rotate-key`).

### Configuration (one unified section)
15. **Full config presentation** with three integrated overlays: on-chain
    registration vs. local config **drift** highlighted field-by-field;
    the **availability countdown** with the selection-eligibility warning
    (a shrinking `available_until` stops long-dated assignments *before*
    it expires, since selection requires
    `available_until ≥ reveal_end_block`); and policy/RPC/reveal-offset
    values with validation status. Display only — renewal and edits are
    CLI actions.

**Explicitly cut by the owner**: backup hygiene, compromise-runbook mode,
network-context statistics, alerting (→ Prometheus/Grafana, separate
work), audit trail, authentication (v1 shipped unauthenticated on the shared
`bind_address` — §2; discharged since by
[`DONE_DASHBOARD_AUTHENTICATION_PLAN.md`](DONE_DASHBOARD_AUTHENTICATION_PLAN.md)).

## 2. Delivery shape

- **Embedded in `guardiand`** as a third listener owned by the existing
  monitoring `Service` (which already binds metrics and health listeners
  synchronously and fails startup loudly on taken ports — the dashboard
  listener follows the same discipline). **Dedicated `dashboard_port`,
  default `21200`** (ruled July 2026), enabled by default with a config
  off-switch.
- **Shares the monitoring `bind_address`, so `0.0.0.0` by default** (ruled
  July 2026, and settled on 28 July 2026 against the deployment reality). The
  dashboard is reachable wherever `/health` and `/metrics` are: one address to
  set, and the devnet's per-guardian dashboards are routable from the host
  without special-casing. No separate `dashboard_bind_address` — a knob for a
  distinction the ruling declines to make is surface without purpose.

  **The deciding argument is Docker.** Most guardians will run containerised,
  and Docker's `-p` only publishes a port the process bound on `0.0.0.0` —
  a loopback-bound dashboard is unreachable from the host *by construction*, no
  matter what is published. Loopback would therefore not be a conservative
  default but a broken one for the majority deployment, and an override every
  operator must discover is not a safety feature. This is why the question is
  closed rather than a matter of taste.

  **This ships v1 unauthenticated on all interfaces, and that is a deliberate,
  time-limited position.** The page surfaces bond exposure, key fingerprints,
  encrypted-at-rest status *including the legacy-plaintext warning*, and the
  full configuration. The warning is a targeting signal — it names which
  guardians are worth attacking — so read-only does not make the surface
  harmless, and `/metrics` (which already binds `0.0.0.0`) is a weaker version
  of the same exposure via `guardian_balance` and
  `guardian_reveal_windows_missed_total`.

  **Obligation, not a footnote**: authentication had to land before any
  guardian exposed beyond an operator's own network ran this — testnet
  included. [`DONE_DASHBOARD_AUTHENTICATION_PLAN.md`](DONE_DASHBOARD_AUTHENTICATION_PLAN.md)
  took the obligation off runbook mode precisely because the exposure existed
  now and runbook mode is a larger, later concern, and **discharged it in
  August 2026**: Basic auth against a bcrypt hash in `dashboard_password_hash`,
  and a listener that is not bound at all when a non-loopback dashboard has no
  credential. `CONTAINERS.md` and `TESTING_COMMANDS.md` carry the operator
  instructions. `21200` continues the devnet's
  contiguous port region — health `21000 + i`, metrics `21100 + i`, dashboard
  `21200 + i` for guardian *i* — and is deliberately clear of Metro's 8090
  (`mobile-client/scripts/run-ios.sh`), which the mobile client needs while a
  devnet shares the machine. The devnet therefore fans the dashboard out over
  `BASE_DASHBOARD_PORT + i` (21200–21299) exactly as it already does for the
  other two listeners, so a 24-guardian devnet does not have 24 daemons
  fighting over one port. Config validation extends the existing
  metrics-vs-health distinctness check to all three ports, and to the
  `grpc_endpoint` port.
- **No new build toolchain.** The UI is a single static page (hand-written
  HTML/CSS + vanilla JS) embedded via `go:embed`, polling the JSON API.
  No npm, no bundler, no framework — a new build target would need its
  own argued case and none is warranted for ~15 read-only panels.
- **JSON API** under the same listener: one snapshot endpoint per section
  (`/api/vitals`, `/api/assignments`, `/api/economics`, `/api/keys`,
  `/api/config`), each a versionless plain JSON struct assembled in
  `guardian/dashboard`. The UI polls (5–10s); no websockets in v1.

## 3. Branding

Source of truth: `mobile-client/design/timeflare-brand-1a.md` ("Ledger"
direction — dark, cryptographic, precise) and `logo-split-seal.svg`.

- The CSS token block (`--tf-ink #0E0F13`, `--tf-slate #1C1E24`,
  `--tf-flare #4D7FFF`, `--tf-signal #F5521E`, `--tf-bone #F4F2EC`,
  radii, hairline) is copied verbatim into the embedded stylesheet, and
  the Split Seal SVG inlined as the header mark with the lowercase
  `timeflare` wordmark. `go:embed` cannot reach outside the guardian
  module, so the copies carry a provenance comment naming the brand file;
  the brand doc is the single place either is edited.
- Type per the guide (Space Grotesk / Space Mono / Hanken Grotesk) loaded
  from Google Fonts **with the guide's full fallback stacks** — the
  dashboard must render acceptably offline on system fonts (no embedded
  font files in v1; that is weight without operator value).
- Simple and clean per the owner: brand patterns used sparingly — mono
  uppercase eyebrow labels, status chips (`--tf-flare` border for live
  states, `--tf-signal` for urgent), `--tf-slate` cards on `--tf-ink`.
  At-risk reveals are the only `--tf-signal`-saturated surface.

## 4. Data sources — why no chain-side changes are needed

| Section | Source |
|---|---|
| Vitals | daemon runtime + node RPC status (already polled) |
| Work queue / assignments | `ActiveSecretCache` (daemon-local, already event-fed) |
| Decision log, history, earnings, tx outcomes | **in-memory since-start buffers** (capped ring buffers) fed by the events the daemon already watches — no persistence (ruled July 2026): long-horizon history is Prometheus/Grafana territory, and settled secrets remain chain-queryable by ID for the ~6-month retention window |
| Float / `k` / bonds / registration record | existing `Query/Guardian` gRPC |
| Per-secret detail | existing `Query/SecretMeta` / `Query/SecretAssignments` |
| Signing balance | existing bank query |
| Key identity | local key custody state + registered record (both already read at startup) |
| Rotation state | `Guardian.current_key_epoch` + the key-epochs query, read-only; outgoing-epoch assignments from the daemon's own cache |
| Config | loaded config + on-chain record diff |

The tempting alternative — a chain-side "assignments by guardian" index —
is deliberately **not** taken: the daemon already learns everything about
its own assignments from events, and a chain index would be a
consensus-state change serving one client. If a *fleet* view ever needs
cross-guardian queries, that is a different plan.

**The dashboard persists nothing** (ruled July 2026). The history-flavoured
panels (decision log, settled-secret history, earnings, tx outcomes, `k`
steps, slash events) are since-process-start views over capped in-memory
buffers, fed by events the daemon already observes — a restart clears
them, and each panel says so. This mirrors the daemon's logging stance
(structured zap to stdout; retention is operator infrastructure) and keeps
the long-horizon record where it belongs: Prometheus metrics and Grafana,
which own trends and alerting outside this plan. Per-secret forensics
beyond a restart remain available from the chain itself for the ~6-month
terminal-retention window via the existing queries.

## 5. What this plan does not solve

- **Alerting/metrics** — Prometheus metrics exist (`/metrics`); Grafana
  dashboards and alert rules are separate, later work.
- **Runbook mode** — any state-changing action (rotation, guardian
  updates, accepting toggle). Needs its own plan.
- **Authentication** — deferred by this plan (§2) and required before any
  externally-reachable guardian serves the dashboard. Owned by
  [`DONE_DASHBOARD_AUTHENTICATION_PLAN.md`](DONE_DASHBOARD_AUTHENTICATION_PLAN.md),
  not runbook mode: the exposure is live as soon as v1 ships, so it cannot
  wait on the larger signing surface.
- **Fleet visibility** — this is one daemon's view of itself; the devnet's
  multi-guardian overview stays `guardians.sh status`.
- **History across restarts** — the history panels are since-process-start
  by design; durable trends and retention are the Prometheus/Grafana
  work-stream's concern, not the dashboard's.

## 6. Implementation phases

1. **Buffers + snapshots** — capped in-memory event buffers (decisions,
   submissions, observed settlements) and read-snapshot accessors on the
   cache; unit-tested.
2. **API** — `guardian/dashboard` snapshot assembly + handlers mounted on
   the new listener in `monitoring.Service`; handler tests with golden
   JSON.
3. **UI** — embedded static page, branded per §3, polling the API;
   rendered against golden snapshots.
4. **Config + wiring** — `dashboard` config block (enable, port; the bind
   address is the shared `bind_address`), devnet enablement
   (`BASE_DASHBOARD_PORT` in `devnet/guardians.sh`, set per guardian at
   start alongside `health-port`/`metrics-port`), docs sweep
   (`TESTING_COMMANDS.md`, guardian README, CONTAINERS.md port note). The
   docs must state that v1 is unauthenticated and reachable on every
   interface, so firewalling is the operator's job until auth lands —
   inferring that from a port table is not good enough for a surface that
   names plaintext key storage.
5. **Verification** — `cd guardian && make verify && make test`; devnet
   walkthrough: register/accept/reveal cycle visible end-to-end in the
   dashboard; e2e suites unaffected (no chain changes).
