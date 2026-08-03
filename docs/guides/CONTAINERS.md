# Containers — Images, Compose Devnet & Operator Recipes

Implements [docs/planning/done/DONE_CONTAINERISATION_PLAN.md](../planning/done/DONE_CONTAINERISATION_PLAN.md):
two distroless images for the network's actors (validators and guardians), an
isolated multi-node compose devnet, and run recipes for operators. Image
**publishing** (registry, tags, signing) belongs to the release pipeline.

## The images

| Image | Contents | Ports | Data volume |
|---|---|---|---|
| `timeflare/timeflared` | The chain binary, `FROM distroless/static:nonroot` | 26656 p2p, 26657 RPC, 1317 API, 9090 gRPC | `/home/nonroot/.timeflare` |
| `timeflare/guardiand` | The guardian daemon, same base | 21000 health, 21100 metrics, 21200 dashboard | `/home/nonroot/.timeflare/guardian` |
| `timeflare/tools` | Both binaries + bash/jq/curl/openssl — devnet init only, never a deployment artefact | — | — |

Build locally: `make docker-build`. Both runtime images are pure-Go static
binaries on `gcr.io/distroless/static-debian12:nonroot` — no shell, no libc,
tens of MB. Version stamping mirrors the native build (`timeflared version`
inside the image reports the build's git describe). Debug variants of the
base (`:debug`) exist when a shell is needed.

`DOCKER_PREBUILT=1 make docker-build` is the CI fast path: it compiles both
binaries on the **host** (`GOOS=linux`, CGO disabled) and COPYs them into the
same runtime bases via `devnet/docker/Dockerfile.prebuilt`. The hermetic
Dockerfiles' BuildKit cache mounts do not survive into GitHub's `type=gha`
cache, so in-container builds recompile the whole module graph on every CI
run (~6–7 minutes); the host build cache persists via `actions/setup-go`,
dropping the three image builds to seconds. Runtime parity with the hermetic
images is 1:1 (base, binary path, ports, entrypoint) — only the build
provenance differs, so it is a devnet/CI convenience, never a release path.

Healthchecks use exec form against the binaries themselves (distroless has no
shell): `["timeflared", "status"]` for the validator, `["guardiand",
"health"]` for the guardian.

## The compose devnet

Opt-in parallel to the native devnet — `make dev-up` remains the default
developer loop (no image rebuild per code change); compose buys isolation,
multi-validator topology, and zero-toolchain bring-up.

```sh
make docker-up                                    # 1 validator + 24 guardians
VALIDATOR_COUNT=3 make docker-up                  # multi-validator topology
TIMEFLARE_BLOCK_TIME=2s VALIDATOR_COUNT=3 make docker-reset   # fast blocks, fresh state
make docker-status / docker-logs / docker-down / docker-reset
```

What comes up (all containers on one bridge network, services generated and
enumerated — never `--scale`d, because replicas would share a volume and
therefore an on-chain identity):

1. **chain-init** (tools image, one-shot): builds one genesis for
   `VALIDATOR_COUNT` validators — same pools and zero-inflation economics as
   the native devnet via the shared `apply-genesis-economics.sh` — wires
   `persistent_peers` between the validators, and exports the genesis keyring
   to the host bind mount `.devnet/docker/home/` so host-side clients can
   fund and sign. It also owns creating/chowning the `nonroot` (uid 65532)
   data directories.
2. **validator-0..N-1** (timeflared image): one named volume and one
   consensus key each. RPC/gRPC/API published to the host from validator-0
   (26657/9090/1317 + `TIMEFLARE_DOCKER_PORT_OFFSET`); each further validator
   publishes RPC at 26667, 26677, …
3. **guardian-init** (tools image, one-shot, gated on validator-0 healthy):
   mirrors `devnet/guardians.sh` — per-guardian keys and config on each
   guardian's volume, **one** batch funding transaction from the pool (one
   sequence number, parallel-safe), parallel registration, then writes the
   name→address registry to `.devnet/docker/home/guardians-registry.conf`.
4. **guardian-01..M** (guardiand image): config.yaml on the volume carries
   the generated identity; `GUARDIAN_*` env re-points the path fields at the
   container mount (env > file precedence). Health-gated with automatic
   restarts (`unless-stopped`).

**Scope principle: the actors are containerised, the clients are not.** The
e2e harness, user funding, and chain admin stay host-side and drive the stack
through its published ports:

```sh
make docker-e2e             # full secret lifecycle against the stack
make docker-e2e-scenarios   # failure-path suite S1..Sn against the stack
make e2e-full               # the verification gate: fresh 3-validator stack →
                            # lifecycle → app-hash determinism check → teardown
```

**Containers are the default verification environment** (ruled July 2026,
amending the plan's §8.2/8.3): `make e2e-full` — locally and in the
label-gated CI job — runs against a fresh 3-validator compose stack and
asserts cross-validator **app-hash agreement**
(`devnet/docker/check-app-hash.sh`), so every full-E2E pass doubles as a
state-machine determinism check. Native (`make dev-up`) remains the default
*iteration* environment, and `make e2e-full-native` keeps a Docker-free
fallback of the old gate.

Both export `TIMEFLARE_HOME=.devnet/docker/home` (the chain-init bind mount)
so the harness signs with the stack's genesis keyring and keeps George's user
keyring isolated from any native devnet, and `GUARDIAN_CONTROL=docker` so the
no-show drill stops/starts the victim's **container** instead of a host
process. A host without `timeflared` can still run admin commands through the
image: `docker run --rm --network timeflare_timeflare timeflare/timeflared:dev
query bank total --node http://validator-0:26657`.

## Multi-validator consensus notes

The compose devnet is the first place Timeflare runs with more than one
validator. Facts that matter when drilling failures:

- **Equal stakes and the 2/3 rule.** All devnet validators self-delegate the
  same 10,000 VEIL. CometBFT commits a block only with **strictly more than
  2/3** of voting power. With 3 equal validators, losing one leaves exactly
  2/3 — **the chain halts** (liveness, not safety: no fork, and it resumes
  the moment the validator returns). This is correct BFT behaviour: 3
  validators tolerate zero faults. Run `VALIDATOR_COUNT=4` when a drill
  should *survive* one validator down (3/4 = 75% > 2/3).
- **App-hash determinism.** Every validator executes every block
  independently; any non-determinism in state machine code forks the chain
  ("wrong app hash" / consensus failure in the logs). Multi-validator runs
  are therefore also a determinism test for `x/secrets` — treat any app-hash
  divergence as a P0 protocol bug, never an infrastructure flake.
- **Peer topology.** Fixed `persistent_peers`, `pex = false`,
  `addr_book_strict = false` — a closed set of known peers on a private
  bridge network. Genesis and node IDs are wired by chain-init.
- **One consensus key, one container.** Never run two containers against one
  validator volume: `data/priv_validator_state.json` is what stops a
  restarted validator double-signing, and two writers guarantee a
  double-sign and a tombstoned validator. The same rule is why validator
  services are enumerated, never scaled.

### Failure drills

```sh
# Consensus liveness: partition a validator (3-validator stack halts at
# exactly 2/3; 4-validator stack keeps producing blocks)
docker network disconnect timeflare_timeflare timeflare-validator-2
watch 'curl -s localhost:26657/status | jq .result.sync_info.latest_block_height'
docker network connect timeflare_timeflare timeflare-validator-2   # recovery

# No-show slash: kill a guardian through its reveal window, watch settlement
# slash it 40/10/50, then restart-recover (this is scenario S1, automated in
# make docker-e2e-scenarios)
docker stop timeflare-guardian-03
# ... wait out the reveal window ...
docker start timeflare-guardian-03

# Validator restart recovery: state and consensus key co-located on the
# volume, so a restart is atomic and double-sign-safe
docker restart timeflare-validator-1
```

## Operator recipe: run a guardian in a container

A guardian that misses its reveal window is slashed — supervised restarts and
health probes are the headline feature, not a nicety.

```sh
# 1. One-time init on a persistent volume (keys + config).
#    The X25519 encryption key this generates is the crown jewel: it is
#    registered immutably on-chain — losing it means every in-flight secret
#    no-reveal-slashes you and the address is permanently dead. It is
#    generated ENCRYPTED AT REST (there is no plaintext path); init prompts
#    for a share-key passphrase and stores it in a 0600 file beside the key
#    so the daemon runs unattended.
docker volume create guardian-data
docker run --rm -it -v guardian-data:/home/nonroot/.timeflare/guardian \
  timeflare/guardiand:latest config init \
  --key-name myguardian --keyring-backend file --auto-generate-key

# 2. Create the signing (wallet) key in the same keyring. The image ships
#    no timeflared and needs none: 'guardiand wallet' derives at the
#    chain's HD path (m/44'/9733'/0'/0/0), so the 24 words shown once here
#    restore the same account in any Timeflare wallet. Write them down and
#    store them off-host — they are the only recovery for the key and its
#    balance. ('wallet import-from-mnemonic' restores; 'wallet
#    show-address' prints the address to fund.)
docker run --rm -it -v guardian-data:/home/nonroot/.timeflare/guardian \
  timeflare/guardiand:latest wallet create --name myguardian

# 3. Passphrases as mounted secret files — passphrase content never enters
#    an env var (docker inspect leaks env). Both config fields are file
#    paths by design; the GUARDIAN_* env vars below carry PATHS to secrets,
#    not the secrets themselves. The files hold the passphrase VERBATIM
#    (never encoded). If you skip the second mount, the daemon falls back
#    to the passphrase file init wrote beside the key on the volume.

# 4. Run supervised, health-checked:
docker run -d --name guardian --restart unless-stopped \
  -v guardian-data:/home/nonroot/.timeflare/guardian \
  -v /secure/passphrase:/run/secrets/keyring_passphrase:ro \
  -v /secure/share-key-passphrase:/run/secrets/share_key_passphrase:ro \
  -e GUARDIAN_KEYRING_PASSPHRASE=/run/secrets/keyring_passphrase \
  -e GUARDIAN_ENCRYPTION_KEY_PASSPHRASE=/run/secrets/share_key_passphrase \
  -e GUARDIAN_CHAIN_ID=timeflare-test \
  -e GUARDIAN_RPC_ENDPOINT=http://my-node:26657 \
  -e GUARDIAN_GRPC_ENDPOINT=my-node:9090 \
  -p 21100:21100 \
  --health-cmd 'guardiand health --timeout 3' --health-interval 10s \
  timeflare/guardiand:latest start --accept

# 5. Back up — the volume alone is not a backup strategy. Export the
#    encrypted bundle (share key + keyring + config fingerprint) and copy it
#    off-host; drill the restore before it matters.
docker run --rm -it -v guardian-data:/home/nonroot/.timeflare/guardian \
  timeflare/guardiand:latest key backup --output /home/nonroot/.timeflare/guardian/backup.tfb
```

Every config key has a `GUARDIAN_<KEY>` env override (precedence: flags >
env > file > defaults) — a container needs no config file at all if it
prefers pure env. Register with `guardiand register` (run once, same volume).
**The operator dashboard needs a credential in a container.** `guardiand` serves
its read-only dashboard on 21200, on the same `bind_address` as health and
metrics (`0.0.0.0` by default). The page names bond exposure, key fingerprints,
encrypted-at-rest status — including a plaintext-key warning that tells an
attacker which guardians are worth attacking — and the full config, so beyond
loopback it authenticates: HTTP Basic as user `guardian`, against the bcrypt
hash in `dashboard_password_hash`. Set it with `guardiand config
set-dashboard-password` (`--generate` for a strong one, printed once; `--stdin`
to provision a chosen one from a build script). `GUARDIAN_DASHBOARD_PASSWORD_HASH`
works too — the hash is not a secret, which is why the plaintext never goes
near a config file or an environment variable.

**Without one, the dashboard is not served at all.** The daemon logs an error
naming the fix and carries on: health, metrics and reveals are unaffected,
because a missed reveal window is slashable and a missing page is not. Note
that this keys off `bind_address`, and a container binds `0.0.0.0` for `-p` to
publish anything — so a guardian whose dashboard port is published nowhere
still needs a credential. That is deliberate: the daemon cannot see what
`ports:` published, and the alternative is an operator-asserted "not exposed"
flag that gets set once and forgotten.

Basic auth without TLS defends against unauthorised readers, not against a
network eavesdropper — base64 is not encryption, and the credential crosses the
network on every poll. Set `dashboard_tls_cert_file` and `dashboard_tls_key_file`
(both or neither) to serve the dashboard over TLS in-process, or front the port
with a TLS proxy. The daemon warns at startup when it authenticates without
encryption.

Guardian containers publish no ports (health checks run in-container via exec
form), so a compose dashboard is reachable only from the container network until
a `ports:` mapping is added — the credential is still required. The native
devnet fans it out over `BASE_DASHBOARD_PORT + i` (21200-21299); both devnets
set the shared password `timeflare-devnet` on every guardian.

Monitor via the metrics port; `/ready` and `/health` map directly onto
container and Kubernetes probes. On start the daemon verifies the local
share key derives the on-chain registered public key and refuses to run on a
mismatch. Full custody model, backup cadence, and the restore drill:
[GUARDIAN_KEY_CUSTODY.md](GUARDIAN_KEY_CUSTODY.md).

## Operator recipe: run a validator in a container

The volume at `/home/nonroot/.timeflare` carries three tiers that need
different treatment:

| Tier | Contents | Treatment |
|---|---|---|
| Identity keys | `config/priv_validator_key.json` (consensus key — the dangerous one), `config/node_key.json`, operator keyring | Persist **and back up** |
| Anti-double-sign state | `data/priv_validator_state.json` | Must survive every restart; **never** wiped or restored-from-backup independently of the data dir |
| Regenerable | `data/` blockchain DB | Wiping = resync, not loss |

```sh
docker volume create validator-data
# init/genesis per the network's join instructions, then:
docker run -d --name validator --restart unless-stopped \
  -v validator-data:/home/nonroot/.timeflare \
  -p 26656:26656 -p 26657:26657 \
  --health-cmd 'timeflared status' --health-interval 10s \
  timeflare/timeflared:latest start
```

Cosmovisor-in-container packaging is release-pipeline territory
(`automated/RELEASE_ENGINEERING_PLAN.md`); the image does not preclude it —
the binary path and `DAEMON_HOME` conventions sit on the volume.
