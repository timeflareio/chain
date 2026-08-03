# Dashboard Authentication — Plan

*Authentication for the guardian operator dashboard, so a guardian reachable
beyond its operator's own network can serve it safely. Discharges the
obligation recorded in the dashboard plan §2, using the credential and
override machinery the config system already provides — no new component.*

> **Status: done — proposed 28 July 2026, ready 2 August 2026, merged
> 3 August 2026** (PR #138, `worktree-dashboard-auth`). Every phase landed:
> config + CLI, middleware, listener + TLS, UI, devnet + docs, verification.
> One residual is recorded in §6 — the 24-guardian devnet fan-out was never
> exercised end to end.
>
> **Priority**: P1 — the dashboard shipped **enabled by default on
> `0.0.0.0`** and unauthenticated, so nothing beyond loopback could serve it
> until this landed, testnet included. This gated guardian exposure.
>
> **Origin**: owner request, 28 July 2026 ("include authentication in our
> dashboard view … I'm thinking of some password that's part of our config
> file, but there may be better approaches"), discharging the obligation in
> [`DONE_GUARDIAN_DASHBOARD_PLAN.md`](DONE_GUARDIAN_DASHBOARD_PLAN.md)
> §2, which recorded authentication as **required before exposure** and
> parked ownership with the unwritten runbook-mode plan. Ownership moves
> here: runbook mode is a larger, later concern, and the exposure exists
> now.
>
> **Components**:
> - `guardian/config/` — credential field, validation, defaults
> - `guardian/dashboard/` — authentication middleware wrapping the mux
> - `guardian/monitoring/` — pass the credential to `dashboard.Handler`;
>   startup warnings
> - `guardian/cmd/guardiand/cmd/` — `config set-dashboard-password`,
>   `config doctor` check
> - `guardian/dashboard/assets/app.js` — distinguish `401` from
>   "unavailable"
> - `devnet/guardians.sh` and `devnet/docker/init-guardians.sh` — credential
>   fan-out across guardians, in both the native and containerised devnets
> - Docs — `docs/guides/CONTAINERS.md`, `docs/guides/TESTING_COMMANDS.md`,
>   the `dashboard` package doc, and the dashboard plan's §2 obligation
> - Tests — `guardian/dashboard/`, `guardian/config/`,
>   `guardian/monitoring/`

## 1. What is actually being defended

The adversary is **anyone who can route to `dashboard_port`** (21200 by
default). Being precise about what they gain decides how much machinery is
proportionate.

**What the dashboard gives away today**: bond exposure and balance, the
`bond_k` trajectory, reveal obligations and missed windows, share-key
fingerprints, encrypted-at-rest status *including the legacy-plaintext
warning*, and the full effective configuration (endpoints, key paths).

**What it does not give away**: no key material, no signing, no state
change. Every route is `GET` and every handler reads.

So this is a **confidentiality and targeting** problem, not an integrity
one. The plaintext-key warning is the sharpest edge: it tells an attacker
which guardians are worth attacking, which is precisely the intelligence
that makes a read-only page worth protecting. Two consequences:

- **A single shared operator credential is proportionate.** There is no
  multi-user model to express, no per-action authorisation, and nothing to
  audit beyond access itself. Machinery beyond one credential buys nothing
  against this adversary.
- **It changes when runbook mode lands.** A surface that signs needs CSRF
  defence and plausibly a stronger model. §8 hands that over rather than
  pretending this plan settles it.

**Out of scope by design: `/health` and `/metrics` stay unauthenticated.**
Prometheus scrapers and container health checks expect them open, and they
are separate listeners on separate ports — which is exactly why the
dashboard got its own port. This is stated plainly rather than quietly
assumed, because `/metrics` already exposes a weaker version of the same
intelligence (`guardian_balance`,
`guardian_reveal_windows_missed_total`). **This plan does not close that**;
see §6.

## 2. The options

Twelve approaches considered, grouped by what they are actually solving.
Verdicts feed §3.

### Where the credential lives

| # | Option | Cost | What it defends | Verdict |
|---|---|---|---|---|
| 1 | **Plaintext password in the config file** | None — the field already gets a `GUARDIAN_*` override for free | Unauthorised readers, but stores a reusable secret in a file that `config doctor` prints and support requests routinely quote | **No** — see below |
| 2 | **Password *hash* in the config file** | One field, one dependency already present | Same as (1), and a leaked config yields only an offline attack against the hash | **Recommended** |
| 3 | **Credential in a separate `0600` file** (`dashboard_password_file`) | One field, a resolver, an extra file to manage | Same as (1); matches the existing `encryption_key_passphrase` / `keyring_passphrase` convention | **No** — see below |
| 4 | **Auto-generated at first start**, written `0600`, path logged | Generation, first-run branch, devnet handling | Never unauthenticated by accident, no operator chore | **No** — see below |

**Why (2) over (1)** — this is the one change to the owner's instinct, and
the instinct is otherwise right. The config file is already `0600`
(`config/manager.go:94`), so the file mode is not the argument. The
arguments are: `config doctor` and `config list` print effective values, so
a plaintext password becomes something an operator pastes into a support
thread or a screen share; every config key inherits a `GUARDIAN_<KEY>`
environment override, and env vars are readable via `docker inspect` and
`/proc/<pid>/environ`, so the Docker deployment path leaks it by
construction; and a password reused from elsewhere is worth more to an
attacker than dashboard access alone. A hash is **not a secret**, so it can
live in the config file honestly — which keeps the repo's actual rule
intact (the config file holds paths and hashes, never plaintext secrets)
*and* gives the owner the single-file management they asked for.

**Why (2) over (3)** — (3) is the established convention here, but that
convention exists for secrets the daemon must *present* (it decrypts a key
with the passphrase). A verifier only needs to *check*, so it needs no
secret at rest at all. Choosing the weaker artefact when the stronger one
costs nothing is the right trade. `bcrypt` is in
`golang.org/x/crypto v0.54.0`, already a direct dependency of the guardian
module — **no new dependency**. Prometheus is the precedent: `web.yml`
holds bcrypt hashes in `basic_auth_users`, and guardians run
Prometheus-adjacent tooling already.

**Why not (4)** — auto-generation and fail-closed are alternatives, not
additions: each one independently prevents an accidentally unauthenticated
dashboard. Fail-closed is the cheaper of the two. It needs no first-run
write path, and it leaves the operator with a credential they chose and know
where to find, rather than one the daemon invented that they must go and
retrieve from a file or a log line before they can open the page.

### How the credential is presented

| # | Option | Cost | Verdict |
|---|---|---|---|
| 5 | **HTTP Basic auth** | ~20 lines; `r.BasicAuth()` + `bcrypt.CompareHashAndPassword` | **Recommended** — browsers prompt natively, `curl -u` works, no session state, no login page, no CSRF surface |
| 6 | **Bearer token header only** | Similar | **No** — a browser cannot set a header from the address bar, so the page becomes unreachable without a login form anyway |
| 7 | **Login form + session cookie** | A login route, cookie signing or a session store, expiry, logout, CSRF on the POST | **No for v1** — real UX gains (logout, no per-request credential) but all of it is state and surface this adversary does not justify. Revisit with runbook mode |
| 8 | **mTLS client certificates** | Certificate authority, per-operator issuance, renewal; poor browser UX | **No** — strongest option available, and disproportionate for one operator reading one page |
| 9 | **Guardian-key signature challenge** | A signing client; kills plain browser access | **No** — elegant, unusable |
| 10 | **OIDC/SSO** | An identity provider dependency | **No** — enterprise machinery for a single-operator daemon |

### Complements, not substitutes

| # | Option | Verdict |
|---|---|---|
| 11 | **Reverse proxy delegation** (nginx/Caddy/Traefik: TLS + basic auth or OIDC) | **Supported, not relied upon.** Any operator may front the dashboard, and one running a fleet should. It cannot be the answer, because the dashboard is **on by default**: an operator who does nothing must not be exposed, and "configure a proxy" is not a default |
| 12 | **CIDR allow-list in config** | **Not now.** Cheap, but it is trivially wrong behind a proxy (`X-Forwarded-For` is attacker-controlled unless the proxy is trusted) and invites the belief that a network control is an access control. Firewalls already do this better, outside the daemon |
| — | **Loopback bind + SSH tunnel** | **Ruled out 28 July 2026.** Docker's `-p` only publishes a port bound on `0.0.0.0`, so loopback is unreachable from the host by construction for the majority deployment (dashboard plan §2) |

## 3. The design

**Credential**: a new config field `dashboard_password_hash`, holding a
bcrypt hash. Empty by default. Inherits `GUARDIAN_DASHBOARD_PASSWORD_HASH`
from the registry at no cost, which is the containerised deployment path.

**Username**: fixed as `guardian`, a compile-time constant in
`guardian/dashboard` — not a config field, not written by `config init`,
and so not a thing that can drift or be mistyped. A username buys legibility
here (`curl -u guardian:…`, one prompt shape in every browser), not defence:
the value sits in the docs and the source, so treating it as a second secret
would hide it from the operator and from nobody else. Where two secrets are
wanted the honest form is a longer password. The per-guardian name an
operator actually needs goes in the realm instead, so a browser's
saved-credential list stays legible when several guardians run side by side.

The realm names the guardian from `guardian_id`, falling back to `key_name`.
Both devnets set `key_name` per guardian (`guardian-01`…) and `guardian_id`
defaults to it, so the realms are distinct without anyone configuring
anything. Deliberately **not** `monitor_name`, despite what the field is
called: it defaults to `"Timeflare Guardian"`, is set nowhere in the repo,
and would make every realm identical. The realm is served on an
unauthenticated `401`, so whatever it names is public — which `guardian_id`
already is.

The comparison must not short-circuit on a username mismatch: returning
early skips the bcrypt call and answers measurably faster, which is a timing
oracle for the username. Run `bcrypt.CompareHashAndPassword` unconditionally
and combine the two results.

Whether the guardian's identity surface should look different at all —
derived names, a friendlier handle than a bech32 address — is a question for
the guardian setup work, not for a password change; it rides with that plan
when it is written.

**Presentation**: HTTP Basic auth, applied by wrapping the **entire mux**
inside `dashboard.Handler` rather than per-route. This matters: the shape
must make an unauthenticated route *impossible to add*, not merely
unlikely. `Handler` gains the verifier as a parameter, so a caller cannot
forget it — the same compile-time discipline the `Source` interface already
uses.

Failures return `401` with `WWW-Authenticate: Basic realm="…"`, no detail
about whether the username or the password was wrong, and
`bcrypt.CompareHashAndPassword` for the comparison (constant-time by
construction, so no hand-rolled comparison).

**Fail closed, but do not take the guardian offline.** When
`enable_dashboard` is true, the bind address is non-loopback, and no hash is
configured: **do not serve the dashboard**, and log an error naming the
exact command that fixes it. The daemon continues, serving health, metrics
and — critically — reveals.

Refusing to start the whole daemon was considered and rejected: a missed
reveal window is slashable, so failing a guardian's economic function over
a dashboard misconfiguration would cost the operator real money to protect
a page. The proportionate failure is a missing page, loudly explained.

The rule keys off the **bind address**, which under Docker is not a proxy
for exposure: a container must bind `0.0.0.0` for `-p` to publish anything,
so real reachability is decided by `ports:`, which the daemon cannot see.
The consequences are accepted rather than engineered around — a guardian
whose dashboard port is published nowhere still needs a credential, and so
does the careful operator publishing `127.0.0.1:21200:21200`. The heuristic
errs towards refusing to serve a page, never towards serving one
unprotected, and the fix is one command. An operator-asserted "this port is
not exposed" flag is the alternative, and it is the same escape hatch §4
rejects, carrying the same risk of being set once and forgotten. The docs
state this plainly so a container operator meets it as a documented
behaviour rather than a puzzle.

On loopback the dashboard still serves without a credential, because there
is no exposure to defend and forcing a password on a developer's `127.0.0.1`
is ceremony. **The exemption covers reads only.** It is argued entirely from
what a `GET` gives away; the argument does not reach a surface that signs,
which is reachable by any local process or browser tab. The actions plan
(§8) therefore requires the credential unconditionally for its routes,
extending this rule rather than contradicting it.

**Transport**: optional in-process TLS via `dashboard_tls_cert_file` and
`dashboard_tls_key_file` (both or neither; validated as a pair). When
authentication is enabled on a non-loopback address **without** TLS, warn
at startup, replacing the current unconditional "NOT authenticated"
warning.

The honest statement, which the plan and the docs both make: **Basic auth
without TLS defends against unauthorised readers, not against a network
eavesdropper.** Base64 is not encryption, and the credential crosses the
network on every poll. In-process TLS is included rather than deferred to
a proxy because the Docker-only operator this dashboard is designed
around may have no proxy, and telling them to acquire one is how a default
stays insecure.

**CLI**: `guardiand config set-dashboard-password` — prompts twice with no
echo, hashes, writes the hash to the config file. It never accepts the
password as an argument, because arguments land in shell history and
`ps`. `--generate` emits a 32-byte random password, prints it **once** to
stdout (not the log), and stores only the hash.

`--stdin` reads a password from a pipe, hashes it and stores the hash, so a
devnet script or an image build can provision a *chosen* credential without
a password reaching `argv`. Setting the hash directly —
`guardiand config set dashboard-password-hash <hash>`, which the generic
registry setter already supports — stays available and leaks nothing, since
a hash is not a secret.

`config doctor` gains a dashboard-exposure check: reachable, and
authenticated or not, with the fix named. `config list` / `doctor` print
the hash field as `set` / `not set` rather than the hash itself — not
because a hash is secret, but because a 60-character blob in an operator's
config report is noise.

**Brute force**: bcrypt's cost factor is the throttle (default cost, ~60 ms
per attempt, makes online guessing useless against a generated password).
Failed attempts log at warn, **rate-limited** — an unauthenticated
endpoint that writes one log line per request is a log-volume amplifier,
which would trade one exposure for another.

**UI**: `app.js` currently turns any non-`200` into `HTTP <status>` inside
the unavailable banner (`app.js:429`). With Basic auth the browser handles
the first `401` natively, but a wrong credential must not read as "the
daemon is broken" — `401` gets its own message naming authentication as the
cause.

## 4. Devnet

Both devnets run 24 guardians on `0.0.0.0`, so both meet the fail-closed
rule head-on: the native one (`devnet/guardians.sh`) and the containerised
one (`devnet/docker/init-guardians.sh`, whose guardian services publish no
dashboard port but still bind `0.0.0.0` inside the container).

**One shared, known password across all guardians**, set explicitly in both
devnet scripts rather than inherited by accident. Testing what ships beats
testing a bypass, and a `dashboard_auth_disabled` escape hatch is a thing
that can be left on in production. The honest cost: each port is a separate
browser origin, so a first visit to all 24 dashboards means 24 credential
prompts.

Both scripts provision it with `set-dashboard-password --stdin`, so the
devnet's credential is derived from the password the docs quote rather than
from an unexplained hash constant. `TESTING_COMMANDS.md` states that
password where an operator meets the port table.

## 5. Implementation phases

1. **Config + CLI** — `dashboard_password_hash`, TLS path pair, validation
   (pair completeness; hash parses as bcrypt), `config
   set-dashboard-password` (prompt, `--generate`, `--stdin`), `config
   doctor` check, redacted display.
2. **Middleware** — `dashboard.Handler` takes the verifier; wrap the mux;
   `401` shape and headers; failure logging rate-limited **per remote
   address over a fixed window**, so one noisy source cannot suppress the
   record of another. Unit tests
   **table-driven across every route the mux exposes**, so a future route
   that skips authentication fails the suite rather than shipping.
3. **Listener + TLS** — `monitoring.Service` passes the credential,
   `ServeTLS` when configured, startup warning rewritten for the four
   states (loopback; authenticated + TLS; authenticated, no TLS;
   unauthenticated and therefore not served).
4. **UI** — `401` handling in `app.js`.
5. **Devnet + docs** — `guardians.sh`, `devnet/docker/init-guardians.sh`,
   `CONTAINERS.md`, `TESTING_COMMANDS.md`; the `enable_dashboard` field
   description and the `dashboard` package doc, both of which still claimed
   the surface was unauthenticated; **and the dashboard plan's §2 obligation
   marked discharged**. There is no guardian README — `CONTAINERS.md` and
   `TESTING_COMMANDS.md` carry the operator instructions.
6. **Verification** — `cd guardian && make verify && make test`, `-race`
   across the three packages. **Over real sockets**, as the existing
   listener tests do: `401` without credentials, `200` with, `401` with a
   wrong password, every `/api/*` route refused unauthenticated, dashboard
   not bound at all when exposed without a credential, health and metrics
   still open. Plus a devnet browser check.

## 6. What this plan does not solve

- **The devnet fan-out was never exercised end to end.** Both devnet scripts
  provision the shared password through `set-dashboard-password --stdin`, and
  that invocation was verified directly against a config file — but no run
  brought up 24 guardians and opened their dashboards. CI did not cover it
  either: the `E2E (full devnet lifecycle)` job is label-gated (`e2e`), not
  path-gated, so a change to `devnet/guardians.sh` does not trigger it. The
  first `make dev-up` after this landed is the check.
- **`/metrics` exposure** — `guardian_balance` and
  `guardian_reveal_windows_missed_total` remain readable by anyone who can
  route to `metrics_port`. Scraper compatibility is why, and it is a real
  residual, not a technicality: it is a weaker version of the same
  intelligence. Prometheus supports basic auth on scrape targets, so this
  is closable later — it belongs with the metrics work-stream.
- **A leaked config file** — yields an offline attack against the hash. A
  generated password makes that worthless; a weak human-chosen one does
  not. `--generate` exists for this reason, and the docs recommend it.
- **Multi-operator access, roles, and an access audit trail** — one shared
  credential, by the §1 argument. Access logging is not included.
- **TLS certificate lifecycle** — issuance, renewal and trust are the
  operator's, as with any daemon serving TLS from config-supplied paths.
- **CSRF** — no defence is included because every route is `GET`. This is
  a property of the current surface, not a guarantee about the next one.
- **Denial of service** — an unauthenticated attacker can still cost the
  daemon a bcrypt verification per request. Bounded by the cost factor and
  the existing handler timeout; not otherwise addressed.

## 7. Open questions

**None outstanding.**

## 8. Handoff

[`PENDING_DASHBOARD_GUARDIAN_ACTIONS_PLAN.md`](../guardian/PENDING_DASHBOARD_GUARDIAN_ACTIONS_PLAN.md)
is the first slice of runbook mode and is hard-gated on this plan. It
inherits this credential and must extend it: state-changing
routes need CSRF defence (which `GET`-only status quo makes unnecessary
today), and a signing surface may justify the session model rejected as
option 7 or the client certificates rejected as option 8. That plan should
treat this one as its access-model baseline, not re-litigate it.
