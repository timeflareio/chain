#!/usr/bin/env bash
# Compose devnet chain initialisation (runs INSIDE the tools container).
# Builds one genesis for VALIDATOR_COUNT validators, configures every
# validator home, and exports the genesis keyring to /shared so host-side
# clients (funding, e2e harness) can sign.
#
# Mounts:
#   /data/validator-<i>  each validator's named volume (its ~/.timeflare)
#   /shared              host bind mount (.devnet/docker/home) — genesis
#                        keyring + passphrase + genesis.json for the host
#
# Native parity: the single-validator genesis this produces matches
# devnet/chain/setup-chain.sh (same pools, same zero-inflation economics via
# the shared apply-genesis-economics.sh, same dev gov periods). Multi-validator
# extends it with per-validator operator accounts carved out of the
# bootstrapping budget so total supply stays exactly 1B VEIL.

set -euo pipefail

CHAIN_ID="${CHAIN_ID:-timeflare-test}"
DENOM="${TIMEFLARE_DENOM:-uveil}"
MIN_GAS_PRICE="${TIMEFLARE_MIN_GAS_PRICE:-0.1uveil}"
# Passed in by generate-compose.sh, which read it from networks.json. A default
# here would be a second opinion about the cadence, and a silent one — so this
# refuses instead.
BLOCK_TIME="${TIMEFLARE_BLOCK_TIME:?TIMEFLARE_BLOCK_TIME must be set by the compose environment}"
VALIDATOR_COUNT="${VALIDATOR_COUNT:-1}"
GOV_VOTING_PERIOD="${TIMEFLARE_GOV_VOTING_PERIOD:-60s}"

# Native pool allocations (devnet/chain/generate-genesis-keys.sh) — 1B total
# Two pools (docs/spec.md "Genesis Pool Allocations"): the keyless rebate pool
# and one key-controlled bootstrapping pool.
REBATE_POOL_AMOUNT=700000000000000           # 700M VEIL (70%) — keyless module account
BOOTSTRAPPING_AMOUNT=300000000000000         # 300M VEIL (30%) — controlled key
REBATE_POOL_ADDRESS=tmflr1g6ct2qh5jtrew322yuumdehgwnk9pcexzzz3d2

# Per-validator staking: validator-0 gentx's from bootstrapping (native
# parity); each EXTRA validator gets its own operator account funded out of
# the bootstrapping budget (stake + fee buffer), keeping supply fixed.
STAKE_AMOUNT=10000000000                     # 10k VEIL self-delegation
OP_ACCOUNT_AMOUNT=10100000000                # 10k stake + 100 VEIL fee buffer

# The genesis keyring's AUTHORITATIVE home is the chain-shared named volume —
# it survives host-side wipes of .devnet/. /shared (the host bind mount) only
# ever holds a re-exported copy, refreshed on every run, so a deleted
# .devnet/docker/home self-heals on the next docker-up.
CHAIN_SHARED=/chain-shared
SHARED=/shared
KEYRING_DIR="$CHAIN_SHARED/genesis-keyring"
PASSPHRASE_FILE="$CHAIN_SHARED/genesis_keyring_passphrase"
VAL0_HOME=/data/validator-0

log() { echo "[chain-init] $*"; }

export_shared() {
    mkdir -p "$SHARED"
    cp -R "$KEYRING_DIR" "$SHARED/"
    cp "$PASSPHRASE_FILE" "$SHARED/genesis_keyring_passphrase"
    cp "$VAL0_HOME/config/genesis.json" "$SHARED/genesis.json"
    # The host user must read the genesis keyring (funding) and write beside
    # it (user keyring, scenario scratch) — devnet-only permissiveness
    chmod -R a+rwX "$SHARED"
}

if [[ -f "$VAL0_HOME/config/genesis.json" ]]; then
    log "chain already initialised — refreshing the /shared export"
    export_shared
    exit 0
fi

log "initialising $VALIDATOR_COUNT validator(s), chain-id $CHAIN_ID, block time $BLOCK_TIME"

# ── Genesis keyring (exported to the host via /shared) ──────────────────────
mkdir -p "$KEYRING_DIR"
if [[ ! -f "$PASSPHRASE_FILE" ]]; then
    openssl rand -base64 12 | tr -d '\n' > "$PASSPHRASE_FILE"
fi
PASSPHRASE=$(cat "$PASSPHRASE_FILE")

new_key() { # name → address (idempotent)
    local name="$1" addr
    addr=$(echo "$PASSPHRASE" | timeflared keys show "$name" -a \
        --keyring-backend file --keyring-dir "$KEYRING_DIR" 2>/dev/null || true)
    if [[ -z "$addr" ]]; then
        printf '%s\n%s\n' "$PASSPHRASE" "$PASSPHRASE" | timeflared keys add "$name" \
            --keyring-backend file --keyring-dir "$KEYRING_DIR" --output json >/dev/null
        addr=$(echo "$PASSPHRASE" | timeflared keys show "$name" -a \
            --keyring-backend file --keyring-dir "$KEYRING_DIR")
    fi
    echo "$addr"
}

BOOTSTRAPPING_ADDR=$(new_key bootstrapping)

declare -a OP_ADDRS=()
for ((i = 1; i < VALIDATOR_COUNT; i++)); do
    OP_ADDRS[$i]=$(new_key "validator-op-$i")
done

# ── Init every validator home ───────────────────────────────────────────────
for ((i = 0; i < VALIDATOR_COUNT; i++)); do
    timeflared init "validator-$i" \
        --chain-id "$CHAIN_ID" --default-denom "$DENOM" \
        --home "/data/validator-$i" --overwrite >/dev/null 2>&1
done

# ── Genesis accounts (on validator-0, the genesis-authoring home) ───────────
# Extra validators' operator accounts come out of the bootstrapping budget —
# never out of the rebate pool, whose balance IS the accrual rate. Total supply
# stays exactly 1B VEIL.
POOL_DEDUCTION=$(( (VALIDATOR_COUNT - 1) * OP_ACCOUNT_AMOUNT ))
timeflared genesis add-genesis-account "$REBATE_POOL_ADDRESS" \
    "${REBATE_POOL_AMOUNT}$DENOM" --home "$VAL0_HOME"
timeflared genesis add-genesis-account "$BOOTSTRAPPING_ADDR" \
    "$((BOOTSTRAPPING_AMOUNT - POOL_DEDUCTION))$DENOM" --home "$VAL0_HOME"
for ((i = 1; i < VALIDATOR_COUNT; i++)); do
    timeflared genesis add-genesis-account "${OP_ADDRS[$i]}" \
        "${OP_ACCOUNT_AMOUNT}$DENOM" --home "$VAL0_HOME"
done

# ── Gentxs: one per validator home (each home signs with its own consensus
#    key), collected on validator-0 ──────────────────────────────────────────
for ((i = 1; i < VALIDATOR_COUNT; i++)); do
    cp "$VAL0_HOME/config/genesis.json" "/data/validator-$i/config/genesis.json"
done

echo "$PASSPHRASE" | timeflared genesis gentx bootstrapping "${STAKE_AMOUNT}$DENOM" \
    --chain-id "$CHAIN_ID" --home "$VAL0_HOME" \
    --keyring-backend file --keyring-dir "$KEYRING_DIR" \
    --moniker "validator-0" \
    --commission-rate 0.05 --commission-max-rate 0.20 --commission-max-change-rate 0.01 \
    --min-self-delegation 1 >/dev/null 2>&1

for ((i = 1; i < VALIDATOR_COUNT; i++)); do
    echo "$PASSPHRASE" | timeflared genesis gentx "validator-op-$i" "${STAKE_AMOUNT}$DENOM" \
        --chain-id "$CHAIN_ID" --home "/data/validator-$i" \
        --keyring-backend file --keyring-dir "$KEYRING_DIR" \
        --moniker "validator-$i" \
        --commission-rate 0.05 --commission-max-rate 0.20 --commission-max-change-rate 0.01 \
        --min-self-delegation 1 \
        --output-document "$VAL0_HOME/config/gentx/gentx-validator-$i.json" >/dev/null 2>&1
done

timeflared genesis collect-gentxs --home "$VAL0_HOME" >/dev/null 2>&1

# ── Economics: identical knobs to the native devnet, one shared script ──────
/scripts/chain/apply-genesis-economics.sh "$VAL0_HOME/config/genesis.json" "$GOV_VOTING_PERIOD"

timeflared genesis validate-genesis --home "$VAL0_HOME" >/dev/null

# ── Distribute the final genesis and configure every validator ──────────────
for ((i = 1; i < VALIDATOR_COUNT; i++)); do
    cp "$VAL0_HOME/config/genesis.json" "/data/validator-$i/config/genesis.json"
done

declare -a NODE_IDS=()
for ((i = 0; i < VALIDATOR_COUNT; i++)); do
    NODE_IDS[$i]=$(timeflared comet show-node-id --home "/data/validator-$i")
done

configure() { # file pattern replacement
    sed -i "s|$2|$3|g" "$1"
}

for ((i = 0; i < VALIDATOR_COUNT; i++)); do
    HOME_I="/data/validator-$i"
    CONFIG="$HOME_I/config/config.toml"
    APP="$HOME_I/config/app.toml"

    peers=""
    for ((j = 0; j < VALIDATOR_COUNT; j++)); do
        [[ $j -eq $i ]] && continue
        peers+="${peers:+,}${NODE_IDS[$j]}@validator-$j:26656"
    done

    configure "$CONFIG" 'laddr = "tcp://127.0.0.1:26657"' 'laddr = "tcp://0.0.0.0:26657"'
    configure "$CONFIG" 'cors_allowed_origins = \[\]' 'cors_allowed_origins = ["*"]'
    configure "$CONFIG" 'timeout_commit = "5s"' "timeout_commit = \"$BLOCK_TIME\""
    configure "$CONFIG" 'size = 5000' 'size = 1000'
    configure "$CONFIG" 'persistent_peers = ""' "persistent_peers = \"$peers\""
    # Fixed peer set on a private bridge network: no peer exchange, no
    # routability filtering, and duplicate IPs allowed (drill restarts can
    # briefly look duplicated to the address book)
    configure "$CONFIG" 'pex = true' 'pex = false'
    configure "$CONFIG" 'addr_book_strict = true' 'addr_book_strict = false'
    configure "$CONFIG" 'allow_duplicate_ip = false' 'allow_duplicate_ip = true'

    configure "$APP" 'minimum-gas-prices = ""' "minimum-gas-prices = \"$MIN_GAS_PRICE\""
    configure "$APP" 'enable = false' 'enable = true'
    configure "$APP" 'address = "tcp://localhost:1317"' 'address = "tcp://0.0.0.0:1317"'
    configure "$APP" 'enabled-unsafe-cors = false' 'enabled-unsafe-cors = true'
    # gRPC binds loopback by default — unreachable from other containers
    # (the native devnet never notices: everything shares the host loopback)
    configure "$APP" 'address = "localhost:9090"' 'address = "0.0.0.0:9090"'
done

# ── Host-side artefacts and volume ownership ────────────────────────────────
export_shared

# distroless :nonroot runs uid 65532; named volumes initialise root-owned
for ((i = 0; i < VALIDATOR_COUNT; i++)); do
    chown -R 65532:65532 "/data/validator-$i"
done

log "genesis ready: $VALIDATOR_COUNT validator(s), peers wired, keyring exported to /shared"
for ((i = 0; i < VALIDATOR_COUNT; i++)); do
    log "  validator-$i node id: ${NODE_IDS[$i]}"
done
