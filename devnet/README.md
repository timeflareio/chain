# Timeflare Devnet Tooling

Development-only scripts for running a full local Timeflare environment. Nothing
in this directory ships to production — product behaviour lives in `timeflared`,
`guardiand`, and the TypeScript SDK.

**Drive everything through make** — these scripts are implementation details of
the make targets:

```bash
make doctor        # verify your toolchain (go, rust, wasm-pack, node, buf, jq)
make dev-up        # chain + 24 guardians + funded test user (GUARDIAN_COUNT=n to change)
make dev-status    # chain height + guardian process/health overview
make e2e           # run the full secret lifecycle against the devnet
make e2e-scenarios # failure-path suite: no-show slash, mid-hold cancel, early-reveal report
make dev-down      # stop guardians and the chain
make dev-reset     # wipe all state (~/.timeflare, .devnet) and start fresh
```

Runtime state (chain PID/log, guardian PIDs/logs, the local guardian registry,
the recipient test keypair) lives in the gitignored `.devnet/` directory at the
repo root. Chain data lives in `~/.timeflare` as always.

## Compose devnet (containers)

An opt-in parallel to the native devnet — same lifecycle verbs, prefixed
`docker-` — running every **actor** (validators, guardians) in containers on
one isolated bridge network, including multi-validator topology the native
devnet cannot do. The e2e harness stays host-side and drives the stack
through its published ports. See
[docs/guides/CONTAINERS.md](../docs/guides/CONTAINERS.md).

```bash
make docker-up                    # 1 validator + 24 guardians in containers
VALIDATOR_COUNT=3 make docker-up  # multi-validator topology
make docker-e2e                   # lifecycle suite against the stack
make docker-e2e-scenarios         # failure-path suite against the stack
make e2e-full                     # verification gate: fresh 3-validator stack
                                  # → lifecycle → app-hash check → teardown
make docker-status / docker-logs / docker-down / docker-reset
```

`make e2e-full` is the containerised verification gate (used by CI's
label-gated e2e job); `make e2e-full-native` keeps the old native-devnet
variant for hosts without Docker.

Compose runtime state (generated compose.yaml, the exported genesis keyring
the host signs with, the guardian registry) lives in `.devnet/docker/`; chain
and guardian state live in named Docker volumes. The `docker/` subdirectory
here holds the tools-image Dockerfile and the in-stack init scripts
(`init-chain.sh`, `init-guardians.sh`, `generate-compose.sh`).

## Layout

```
devnet/
├── lib/
│   ├── common-utils.sh    # logging, chain status, tx helpers — source this
│   └── keyring-utils.sh   # keyring passphrase management
├── chain/
│   ├── generate-genesis-keys.sh   # genesis pool accounts (run once per env)
│   ├── setup-chain.sh             # init chain: genesis, accounts, dev params
│   └── upgrade-test.sh            # rehearse a software upgrade via governance
├── fund.sh                # THE funding primitive: bootstrapping pool → any address (see below)
├── guardians.sh           # register/start/stop/status/logs/clean for local guardians
└── users/
    └── setup-test-users.sh        # create and fund the 'george' test user
```

## Funding any address (`fund.sh` / `make faucet`)

`devnet/fund.sh` is the single funding primitive every use case composes
over — Spike B wallets, app onboarding testing, the e2e suites, guardian
bring-up. It validates the recipient (prefix + bech32 checksum) before
sending, broadcasts sync from the bootstrapping genesis key, polls to inclusion,
retries on account-sequence mismatch (parallel callers are safe), and exits
with deterministic codes (documented in the script header — CI consumers
rely on them):

```bash
./devnet/fund.sh tmflr1…                          # 1,000 VEIL from the bootstrapping pool
./devnet/fund.sh tmflr1… 5000000000               # explicit uveil amount
./devnet/fund.sh tmflr1… --node 192.168.1.20:26657  # non-default node (compose stack, LAN)

make faucet ADDR=tmflr1…                          # thin preset wrapper
make faucet ADDR=tmflr1… AMOUNT=5000000000
```

There is no pool to choose. Genesis holds two (docs/spec.md "Genesis Pool
Allocations"): the **bootstrapping** key, which funds every actor a launch needs
and is what this script draws on, and the **keyless rebate pool**, which has no
key at all. The script stays policy-free: on a public network, funding a wallet
is either a bootstrapping grant or a protocol rebate, never a service.

## Guardian orchestration

`guardians.sh` is a thin loop over `guardiand` and `timeflared` — all real
guardian behaviour (registration, share handling, reveals, health) is in the
`guardiand` binary:

```bash
./devnet/guardians.sh register 5    # keys + funding + config + on-chain registration
./devnet/guardians.sh start 5      # start with per-instance health/metrics ports
./devnet/guardians.sh status       # processes + on-chain state + health checks
./devnet/guardians.sh logs guardian-01
./devnet/guardians.sh stop
./devnet/guardians.sh clean        # stop + remove runtime state (keeps keys/configs)
```

Every step is idempotent: re-running `register` skips guardians that already
have keys, funding, or an on-chain registration. Health is checked through
`guardiand health`, which queries each guardian's health endpoint.

Defaults (override via environment): `GUARDIAN_STAKE=50000000000uveil` (the
initial float deposit — ten bump-1.00 bonds; registration additionally charges
the 1,000 VEIL entry fee), `GUARDIAN_FUNDING=55000000000uveil` (the bank
transfer, which must stay above stake + entry fee + gas — raise the two
together), health ports
from `21000`, metrics ports from `21100` (`BASE_HEALTH_PORT` /
`BASE_METRICS_PORT`). Guardian *i* takes `21000 + i` and `21100 + i`, so the
devnet owns one 21000–21199 region — chosen to stay below the chain's 26657
(the docker port offset only ever adds), below macOS's 49152 ephemeral floor,
and clear of Metro's 8090 and node_exporter's 9100.

## Upgrade rehearsal

The dev chain is initialised with a 60-second governance voting period
(`TIMEFLARE_GOV_VOTING_PERIOD` to change), so software upgrades can be
rehearsed end to end:

```bash
make upgrade-scaffold NAME=v2        # generate app/upgrades/v2/
make devnet-upgrade-test NAME=v2     # proposal → vote → upgrade height
```

See [docs/upgrades.md](../docs/upgrades.md) for the full runbook. Chains
initialised before this tooling existed have the 48h default — `make dev-reset`
to pick up the dev parameters.

## End-to-end test

`make e2e` runs `typescript-sdk/examples/secret-lifecycle.js` against the
running devnet and verifies the **complete** lifecycle strictly (non-zero exit
on any stage failure):

1. Secret creation and share distribution (three-phase commit)
2. Guardian acceptance up to the threshold
3. Reveal window opening at the scheduled height
4. Guardians revealing shares within the window
5. Client-side Shamir reconstruction, byte-for-byte verified against the
   original secret

`make e2e-full` is the self-contained version with cleanup: it resets to a
fresh chain with fast blocks (`TIMEFLARE_BLOCK_TIME=2s` by default), runs the
full lifecycle, then tears the devnet down — pass or fail. On failure the
chain state is kept in `~/.timeflare` for inspection.

`make e2e-scenarios` runs `devnet/e2e-scenarios.sh` — the failure-path suite
from the economics test strategy (tier 3): a no-show slash with a killed
daemon, a mid-hold cancellation with pro-rata wages, and an early-reveal
report using real share evidence. Every payout, burn, and slash slice is
asserted to the exact uveil via `block_results`/`tx` events. Run it against a
fresh fast-block devnet (`TIMEFLARE_BLOCK_TIME=2s make dev-reset`) — the two
reveal-window waits take ~20 minutes at the default 6-second blocks.

Note the guardian maths: the protocol assigns `shares + 30% buffer` distinct
guardians, so the default e2e config (5 shares) needs at least 7 registered.
`GUARDIAN_COUNT` defaults to 24 — comfortably above that floor, so selection
runs against a real candidate pool rather than the bare minimum. Related SDK
examples live in `typescript-sdk/examples/` (`monitor-secrets.js`,
`generate-keypair.js`).

## Mobile clients on this devnet

The devnet already serves mobile development: RPC (26657) and REST (1317)
are bound to `0.0.0.0` by `setup-chain.sh`, so simulators, emulators and
physical devices can all reach a devnet running on your workstation. The
endpoint matrix (the mobile client keeps the endpoint as a Settings field):

| Client | RPC / REST endpoint |
|---|---|
| iOS simulator | `localhost:26657` / `localhost:1317` (shares the host network) |
| Android emulator | `10.0.2.2:26657` / `10.0.2.2:1317` (the emulator's host alias) |
| Physical device (same Wi-Fi) | `<workstation LAN IP>:26657` / `:1317` |
| App CI (compose stack) | compose service names |

Caveats and profiles:

- **macOS firewall**: the first `make dev-up` after enabling LAN access
  triggers the "allow incoming connections?" prompt for `timeflared` —
  accept it or physical devices cannot connect.
- **Block-time profiles**: develop against fast blocks
  (`TIMEFLARE_BLOCK_TIME=2s make dev-reset` — a full secret lifecycle in
  minutes); periodically run at the production ~6 s default to keep
  countdown/estimate UX honest ("blocks, not clocks").
- **Funding device wallets**: `make faucet ADDR=tmflr1…` (or `fund.sh` with
  `--node` for non-local stacks) funds any address the app generates.

## Troubleshooting

- **"Chain is not running"** — `make dev-up`; logs at `.devnet/chain.log`.
- **Guardians unhealthy in `dev-status`** — `./devnet/guardians.sh logs <name>`.
- **Registration fails with insufficient funds** — the bootstrapping genesis
  pool funds guardians; a fresh chain (`make dev-reset`) restores it.
- **`command not found: timeflared/guardiand`** — `make build` installs both;
  ensure `$GOPATH/bin` is in your `PATH`.
