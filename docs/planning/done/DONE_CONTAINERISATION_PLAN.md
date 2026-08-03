# Containerisation — Images, Compose Devnet & Deployable Guardians/Validators

*One containerisation story for both audiences: developers get an isolated,
one-command, multi-node devnet with realistic networking; operators get small,
reproducible, health-checked images for running validators and guardians in
production. Supersedes the original Docker plan, which predated the pure-Go
migration and described a Node.js guardian that no longer exists.*

> **Status: DONE (July 2026) — shipped in PR #67 (`containerisation`)**:
> distroless `timeflared`/`guardiand` images, the compose devnet with
> multi-validator topology (`make docker-up`, `VALIDATOR_COUNT`/`GUARDIAN_COUNT`),
> host-side e2e against the stack (`make docker-e2e`/`docker-e2e-scenarios`),
> CI integration with fail-fast tiers, and the operator guide
> ([docs/guides/CONTAINERS.md](../../guides/CONTAINERS.md)). §7 was the build
> list, §8 the decision log. (Rewritten July 2026: the previous draft targeted
> `automated-guardian.js`, cgo-linked binaries, and scripts deleted long ago.)
> Rewritten against the current codebase: both binaries are **pure Go, no cgo**
> (DONE_CONSENSUS_CRYPTO_PURE_GO_PLAN.md), and the guardian daemon is already
> container-shaped — `GUARDIAN_*` env overrides, a file-based keyring
> passphrase for promptless automation, real `/health` and `/ready` endpoints,
> and a configurable `bind_address`
> (DONE_GUARDIAN_IMPROVEMENTS_PLAN.md §3.3/§5). Scope boundary: this plan owns
> the Dockerfiles, the compose devnet, and operator run recipes; **publishing**
> images (registry, tags, signing, multi-arch release matrix) belongs to the
> release pipeline (automated/RELEASE_ENGINEERING_PLAN.md, which coordinates
> with this plan).
>
> **Implementation order (ruled, July 2026): first in the economics-calibration
> chain.** The agreed sequence is **1. this plan →
> 2. [PENDING_ECONOMIC_SIMULATION_PLAN.md](../PENDING_ECONOMIC_SIMULATION_PLAN.md) →
> 3. [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md)**. The
> reproducible network environments delivered here (isolated compose devnet
> with zero-toolchain bring-up, multi-validator topology, `make docker-e2e`
> driving the stack from the host) are what make the simulation plan's runs
> — its devnet scenario-suite replay validation and repeated calibration
> sweeps — cheap to execute, repeat and compare; the
> simulation's calibration report in turn advises the token-economics plan's
> launch values. There is deliberately no governance path to correct economic
> values post-launch (Position A,
> [DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md)),
> which is exactly why good simulation — and therefore this plan — comes first.

## Contents

1. [What this solves](#1-what-this-solves)
2. [Why now — the advantages are newly cheap](#2-why-now--the-advantages-are-newly-cheap)
3. [The images](#3-the-images)
4. [The compose devnet](#4-the-compose-devnet)
5. [Deployment containerisation](#5-deployment-containerisation)
6. [Repo decomposition considered](#6-repo-decomposition-considered)
7. [Implementation phases](#7-implementation-phases)
8. [Decision log](#8-decision-log-resolved-july-2026)

---

## 1. What this solves

**Today there is no container anything** — no Dockerfile, no compose file, no
`.dockerignore`. Everything runs natively:

- `make dev-up` starts one validator (`timeflared`) plus N `guardiand`
  processes on the host, orchestrated by `devnet/` shell scripts, with runtime
  state under the gitignored `.devnet/`.
- New contributors need the full toolchain (`make doctor`: go, rust,
  wasm-pack, node, buf, jq) even if they only want a running chain to point a
  client at.
- The devnet is **single-validator**: consensus behaviour under validator
  failure, network partition, or peer churn is untested territory locally,
  even though `timeflared testnet` (in-process multi-node scaffolding)
  already exists.
- Port collisions are managed by convention (guardian health 8080+i, metrics
  9100+i); running two devnets, or a devnet next to another Cosmos project, is
  a manual port dance.
- Operators — especially **guardian operators, the network's second citizen** —
  have no run recipe beyond "build from source and babysit a process". A
  guardian that misses its reveal window is slashed; the deployment story is a
  protocol-economics concern, not a convenience.

Concretely, this plan delivers:

1. **A supported container run path for operators** — `docker run` a guardian
   with a mounted config/keyring and a secrets-mounted passphrase file, with
   container-native health/readiness wiring, so supervisors (compose,
   systemd + podman, k8s) restart broken guardians automatically.
2. **An isolated, reproducible local devnet** — `make docker-up` as an opt-in
   parallel to `make dev-up`: same lifecycle, no port bleed, and zero host
   toolchain beyond Docker **to bring the network up** (the test harness and
   chain admin remain host-side clients — see the scope principle below).
3. **Multi-validator topology testing** — N validators + M guardians on one
   bridge network, enabling the failure drills the native devnet cannot do
   (partition a validator, kill a guardian mid-window and watch the no-show
   slash, restart-recovery under real network identity).

**Scope principle (ruled, July 2026): containerise the actors, not the
clients.** Validators and guardians — the long-running, slashable,
operator-run processes — are what get images. Chain admin, user funding,
secret submission and the e2e harness remain host-side scripts and SDK
programs that drive the containerised network through its published ports:
they are clients, they run where developers run, and containerising them
would buy isolation nobody needs at real complexity cost (the e2e harness
alone would drag node + wasm-pack + the SDK build into an image). The one
exception is genesis/key init, which stays inside the stack so
`make docker-up` is self-contained (§4). A host without the `timeflared`
binary can still run any admin command through the image itself
(`docker run --rm <image> tx … --node http://…`).

## 2. Why now — the advantages are newly cheap

Two completed plans removed everything that used to make this hard:

- **Pure Go, no cgo** (DONE_CONSENSUS_CRYPTO_PURE_GO_PLAN.md): both binaries
  compile to static executables. Images can be built `FROM scratch`/distroless
  — tens of MB, no libc, no Rust static library per platform, trivially
  multi-arch via `CGO_ENABLED=0 GOOS/GOARCH`, and byte-reproducible with
  `-trimpath` + pinned toolchain. The old plan's hardest problem (shipping
  `libtimeflare_crypto.a` into containers) no longer exists.
- **The guardian is already container-shaped**
  (DONE_GUARDIAN_IMPROVEMENTS_PLAN.md): every config key has a `GUARDIAN_*`
  env override (precedence: flags > env > file > defaults), so a container
  needs no config file at all if it prefers pure env; the keyring passphrase
  file gives promptless automation (and is honoured even on a TTY);
  `/health` and `/ready` are real checks (chain reachability, key
  loadability, loop liveness) that map 1:1 onto container healthchecks and
  readiness probes; `bind_address` handles the container networking case
  explicitly.

The remaining work is therefore assembly, not invention.

## 3. The images

**Two Dockerfiles, one per component** — `Dockerfile` (repo root, timeflared)
and `guardian/Dockerfile` (guardiand). Each component's build is
self-describing and independently evolvable, which matches the direction of
travel on repo shape (§6): the few duplicated build-stage lines are cheap,
BuildKit still shares the module-download cache, and nothing about one
image's build couples it to the other.

```dockerfile
# guardian/Dockerfile — guardiand (timeflared's is the same shape)
FROM golang:1.25.8 AS build   # pin the exact go.mod/go.work toolchain (by digest for releases)
WORKDIR /src
COPY . .
# NB: build context is the REPO ROOT (docker build -f guardian/Dockerfile .)
# because guardian/go.mod carries `replace github.com/leedavis81/timeflare
# => ../` — the module coupling, not the Dockerfile, is the tether; it
# narrows to guardian/-only context once the shared modules are
# version-tagged (§6).
RUN CGO_ENABLED=0 go build -trimpath -C guardian -o /out/guardiand ./cmd/guardiand

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/guardiand /usr/local/bin/guardiand
VOLUME /home/nonroot/.timeflare/guardian
EXPOSE 8080 9100
ENTRYPOINT ["guardiand"]
```

The timeflared image mirrors this: `EXPOSE 26656 26657 1317 9090`,
`VOLUME /home/nonroot/.timeflare`, `ENTRYPOINT ["timeflared"]`.

Decisions embedded here (all ratified — §8 is the decision log):

- **distroless/static:nonroot** over `scratch` (ships CA certs + tzdata +
  a non-root user — worth the ~2 MB) and over alpine (no shell, no package
  CVEs to scan around). Debug variants (`:debug`) exist when a shell is
  needed.
- **Health wiring**: distroless has no shell and no curl, so container
  healthchecks use **exec form against the binaries themselves** — guardian:
  `["guardiand", "health"]` (the self-check subcommand already exists in
  `guardian/cmd/guardiand/cmd/health.go`); validator:
  `["timeflared", "status"]`. Kubernetes additionally gets `httpGet` probes
  against `/ready` and CometBFT's `:26657/health`. No health-check shell
  scripts.
- **Version stamping & toolchain**: build args feed the existing
  `make/common.mk` ldflags so `timeflared version` inside the image matches
  the tag; OCI labels (`org.opencontainers.image.revision/source/version`)
  for provenance. The builder pins the exact toolchain from go.mod
  (`golang:1.25.8`) — the byte-reproducibility claim depends on it — and
  builds run in **workspace mode deliberately** (`go.work` ships in the
  root context; revisit `GOWORK` only if the context ever narrows per §6).
- **Layer caching**: a bare `COPY . .` re-downloads modules on every source
  change — copy `go.mod`/`go.sum`/`go.work` first and `go mod download`, or
  use BuildKit `--mount=type=cache` for `GOMODCACHE`/`GOCACHE`.
- **Volume ownership**: `:nonroot` runs uid 65532 while Docker initialises
  named volumes root-owned — the init step must create and chown the data
  directories before the actors start (a classic first-write failure
  otherwise).
- **`.dockerignore`**: exclude `.devnet/`, `node_modules/`, `rust/target/`,
  `typescript-sdk/dist|wasm/`, and `.git/` (version info arrives via build
  args) — the Go build needs none of them and the context shrinks from GBs
  to MBs.

## 4. The compose devnet

`docker-compose.yml` (or `compose.yaml`) mirroring the native devnet's
lifecycle, **opt-in alongside `make dev-up`, not replacing it** — the native
devnet stays the default for day-to-day iteration (no image rebuild per code
change); compose is for isolation, multi-node topology, and
zero-toolchain onboarding:

- **Services — actors only** (per the §1 scope principle): `chain-init`
  (one-shot: runs the genesis/key setup the `devnet/chain/` scripts do
  today against a shared volume — in a small **tools image** (build-stage
  base, or binary + busybox/jq), since the distroless runtime images have
  no shell; init is the one non-actor kept inside the stack so
  `make docker-up` is self-contained, and it also owns creating/chowning
  the nonroot data directories, §3), `validator-0` (later
  `validator-{0..N-1}` via `timeflared testnet`-generated configs), and
  `guardian-{1..N}` — **generated, enumerated services with one named
  volume each, never `--scale`**: compose replicas share a service's
  volume, so scaled guardians would all be the same on-chain identity
  (same keyring, same encryption key). There is deliberately **no
  user-fund and no test-runner service** — funding, secret submission and
  the e2e harness are host-side client activity (see `docker-e2e` below).
- **Guardian configuration is pure env + secrets**: `GUARDIAN_CHAIN_ID`,
  `GUARDIAN_RPC_ENDPOINT=http://validator-0:26657`,
  `GUARDIAN_GRPC_ENDPOINT=validator-0:9090`,
  `GUARDIAN_KEYRING_PASSPHRASE=/run/secrets/keyring_passphrase` (compose
  secret), keyring + encryption key on a named volume. Registration runs as
  the container's init step (`guardiand register --accept`) before `start` —
  or as a separate one-shot service, mirroring `devnet/guardians.sh` phases.
  Guardian funding amounts are sourced from the same knobs as the native
  scripts — the token-economics plan's Phase 4 raises the entry fee to
  10,000 VEIL (~20k bring-up per guardian) and that change must land in
  exactly one place.
- **Networking**: one bridge network; internal service discovery
  (`validator-0:26657`) replaces localhost port arithmetic. Only the primary
  RPC/gRPC/API ports are published to the host, and under an env-tunable
  prefix so two compose devnets coexist.
- **Health-gated startup**: guardians `depends_on: validator-0:
  condition: service_healthy` — replacing the polling/retry loops the shell
  scripts need today.
- **Make targets**: `docker-up`, `docker-down`, `docker-reset`,
  `docker-logs`, `docker-e2e`. Same verbs as the native devnet, prefixed.
  `docker-e2e` runs the existing **host-side** e2e suites against the
  compose stack's published ports — the suites are Node/SDK programs
  (`make e2e` builds the SDK and runs under `node`), they already take
  endpoints via env, and the host needs the client toolchain exactly as for
  `make e2e` today; funding and admin steps stay inside those host flows
  (`$(USER_SETUP) fund` already runs within the e2e target). The fast-block
  knob (`TIMEFLARE_BLOCK_TIME`) must pass through `chain-init` so
  `docker-e2e` timing matches the native `e2e-full`.
- **Failure drills** (the capability the native devnet lacks): documented
  recipes — `docker network disconnect` a validator (consensus liveness),
  `docker stop guardian-3` through a reveal window (no-show slash, then
  restart recovery), add/remove guardian services mid-lifecycle.

### Configuration & persistence, per actor

The two actors have opposite config natures, and the design fights neither:
`guardiand` is **env-native** (`guardian/config/registry.go` auto-generates a
`GUARDIAN_<KEY>` override for every config key; precedence flags > env >
file > defaults), while `timeflared` is **file-native** (standard Cosmos
`config.toml`/`app.toml` under `~/.timeflare/config/` — no env layer exists
and none is invented). The rule: **env for what varies, files-on-volume
where the software is file-native, secret mounts for credentials, one named
volume per actor for what must persist.**

**Guardian** — per-guardian named volume at `~/.timeflare/guardian`:

| Concern | Mechanism |
|---|---|
| Config | Pure env in the compose devnet (no config file in the container); operators may mount `config.yaml` on the volume instead — both first-class |
| Credentials | Keyring passphrase as a **mounted secret file**, referenced by path via `GUARDIAN_KEYRING_PASSPHRASE` (the config field is a file path by design) — the passphrase content never enters an env var (`docker inspect` leaks env) |
| Persistent state | The file-backend keyring (account signing key) and the **X25519 encryption private key — the crown jewel**: registered immutably on-chain; loss ⇒ every in-flight secret no-reveal-slashed and the address permanently dead. The volume is necessary, not sufficient — off-host backup is DONE_GUARDIAN_KEY_CUSTODY_PLAN.md territory |
| Disposable | Everything else; logs to stdout |

**Validator** — per-validator named volume at `~/.timeflare`, config files
written by `chain-init` (devnet) or the operator (production). Three
persistence tiers on the one volume, each needing different treatment:

| Tier | Contents | Treatment |
|---|---|---|
| Identity keys | `config/priv_validator_key.json` (consensus key — the dangerous one), `config/node_key.json` (p2p identity, low stakes), operator keyring | Persist **and back up** |
| Anti-double-sign state | `data/priv_validator_state.json` | Must survive every restart; **never** wiped or restored-from-backup independently of the data dir — it is what stops a restarted validator re-signing old heights |
| Regenerable | `data/` blockchain DB | Wiping = resync, not loss |

`genesis.json` is network-shared and identical everywhere; `chain-init`
distributes it.

Two rules that fall out: **one consensus key, one container** — two
containers on the same validator volume is a guaranteed double-sign and a
tombstoned validator, so validators (like guardians, above) are enumerated
services, never scaled; and **key + state stay co-located on one volume**
so a restart is atomic.

## 5. Deployment containerisation

The same images are the deployment artefact. This plan ships the **recipes**;
the release pipeline ships the **registry artefacts**:

- **Guardian operator quickstart** (`docs/guides/` page, written as part of
  Phase 3): create keys + config on a volume (`guardiand config init` in the
  image), mount the passphrase file as a secret, `docker run` /
  `podman run` / compose snippet with `restart: unless-stopped` and the
  `/ready` healthcheck; monitoring via the metrics port. The guardian's
  slashing exposure makes supervised restarts the headline feature, not a
  nicety.
- **Validator recipe**: volume layout for `~/.timeflare` (config/data/keys),
  the standard Cosmos port set, and a cosmovisor-in-container note —
  full cosmovisor packaging is RELEASE_ENGINEERING's phase, but the image
  must not preclude it (binary path conventions, `DAEMON_HOME` on the
  volume).
- **Handoff to RELEASE_ENGINEERING**: that plan's build matrix becomes
  `docker buildx` over the same Dockerfile (linux/amd64 + arm64), published
  to GHCR with tags ↔ git tags, SHA256 digests in release notes, and
  signing per its open question 1. This plan deliberately stops at "images
  build locally and are correct" so the release pipeline has one thing to
  wire, not two to design.

## 6. Repo decomposition considered

The per-audience front door ("wanna be a guardian — off you go") was weighed
against splitting the repository. Position (July 2026):

- **Distribution ≠ decomposition.** Operators onboard via artefacts (image +
  quickstart), not clones; this plan plus the release pipeline delivers the
  per-audience identity without touching repo shape.
- **The monorepo is load-bearing pre-launch**: protocol changes land
  atomically across chain, guardian, and SDK (the ShareIndex incident in
  CLAUDE.md shows the cost of missing even one component *inside* the
  monorepo); the crypto same-PR rule (spec + vectors + every implementation
  in one PR) is mechanically enforceable only in one repo; and with
  `ENFORCE_PROTO_BREAKING=false`, breaking churn is deliberately cheap right
  now — a split would tax it hardest exactly when it is most frequent.
- **Decision heuristic**: split along *published-artefact* boundaries (a
  consumer that can live on releases), never along boundaries that need
  same-PR atomicity. The **mobile client** is the one clean split today — a
  separate repo consuming the published npm SDK. Guardian/chain sit on the
  atomic side until the protocol stabilises and the shared modules are
  version-tagged.
- **Preparation, not separation**: the module reorganisation (shared
  `api`/`crypto` library modules with the guardian importing only those —
  [DONE_MODULE_BOUNDARIES_PLAN.md](DONE_MODULE_BOUNDARIES_PLAN.md))
  makes any future split a directory-move plus retag, and its Phase 4
  narrows the guardian Dockerfile's build context.
- If the clone-a-repo *identity* matters before then: per-audience
  **distribution repos** (quickstart + compose + config templates + pinned
  image refs, no code) deliver the front door at near-zero drift cost.

## 7. Implementation phases

1. **Images** — the multi-stage Dockerfile (two targets), `.dockerignore`,
   version-stamping build args, `make docker-build`. Exit: both images build
   from a clean checkout; `docker run guardiand version` and
   `timeflared version` report the stamped version; image sizes in the tens
   of MB.
2. **Compose devnet (single validator)** — the chain-init tools image +
   validator + N guardians (actors only, §1 scope principle), health-gated
   ordering, `make docker-up/down/reset/logs`. Exit: `make docker-up`
   brings up the full actor stack with **zero host toolchain beyond
   Docker**, and `make docker-e2e` passes the full lifecycle suite run from
   the host against its published ports (client toolchain required, exactly
   as `make e2e` today).
3. **Operator recipes** — the guardian quickstart guide + validator recipe;
   `make docker-e2e` wired as an optional CI job variant if Phase 2 proves
   stable (see §8.3). Exit: a guardian runs from the published quickstart
   on a machine with only Docker installed.
4. **Multi-validator topology** — `timeflared testnet`-generated configs into
   `validator-{0..N-1}` services with persistent peers; the failure-drill
   recipes (partition, kill-mid-window, restart recovery) documented and
   smoke-tested. Exit: 3-validator + 8-guardian stack passes `docker-e2e`;
   one documented drill of each type executed.

Phases 1–2 are the bulk of the value and are independent of everything else
in flight; they are also the gate the economics-calibration chain waits on —
the economic simulation plan's replay validation reproduces amounts asserted
by the devnet scenario suite, and the containerised stack is what makes those
runs reproducible and disposable (see the implementation-order note in the
status banner). Phase 4 is the first genuinely new *capability*
(multi-validator failure testing) and can trail.

## 8. Decision log (resolved July 2026)

1. ~~**Base image**~~ **Resolved (July 2026): `distroless/static:nonroot`**
   (rationale in §3; `:debug` variants exist when a shell is needed).
2. ~~**Native devnet's future**~~ **Resolved (July 2026): keep `make dev-up`
   as the default developer loop** indefinitely (no rebuild-per-change tax);
   compose is held to parity via `docker-e2e`. The old plan's "deprecate
   local development" phase stays dropped.
   *Amended (July 2026, owner ruling during implementation): containers are
   the default **verification** environment while native remains the default
   **iteration** environment — `make e2e-full` now targets a fresh
   3-validator compose stack and asserts cross-validator app-hash agreement,
   so every full-E2E pass doubles as a state-machine determinism check.
   `make e2e-full-native` keeps the Docker-free fallback.*
3. ~~**CI adoption**~~ ~~Resolved (July 2026): out of scope~~ **Amended
   (July 2026, owner ruling during implementation): the label-gated
   `e2e-full` CI job now runs the 3-validator compose stack** (buildx gha
   layer caching keeps the image builds cheap). Rationale: the first live
   multi-validator run proved the app-hash determinism check works and is
   the highest-value consensus regression test available — every labelled
   PR now gets it. The harness remains host-side in CI exactly as locally.
4. ~~**Kubernetes**~~ **Settled position: out of scope beyond
   not-precluding it** (the health/readiness endpoints and env-only config
   are the k8s-enabling features). Helm charts/manifests become a follow-up
   plan if testnet operators ask for them.
5. ~~**Windows/ARM dev hosts**~~ **Settled position**: buildx multi-arch
   covers linux/amd64+arm64; macOS ARM developers run linux/arm64 images
   natively under Docker Desktop. No native Windows containers.
