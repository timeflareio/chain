#!/usr/bin/env bash
# Compose devnet guardian initialisation (runs INSIDE the tools container).
# Mirrors devnet/guardians.sh's three phases — prepare keys/config, one batch
# funding tx, parallel registration — against the containerised chain.
#
# Mounts:
#   /homes/guardian-<NN>/.timeflare/guardian   each guardian's named volume
#   /shared                                    the chain-init bind mount
#                                              (genesis keyring + passphrase)
#
# Path convention: config.yaml stores the paths ABSOLUTE as seen from this
# init container (/homes/guardian-<NN>/...) — the config manager expands and
# persists paths at set time, so location-independent values cannot be stored.
# The guardian containers therefore override the three path fields via
# GUARDIAN_* env (precedence env > file), pointing at the same volume mounted
# at /home/nonroot/.timeflare/guardian. The layout below is asserted so those
# static env overrides can never drift from what init actually wrote.

set -euo pipefail

CHAIN_ID="${CHAIN_ID:-timeflare-test}"
RPC_ENDPOINT="${RPC_ENDPOINT:-http://validator-0:26657}"
GRPC_ENDPOINT="${GRPC_ENDPOINT:-validator-0:9090}"
GUARDIAN_COUNT="${GUARDIAN_COUNT:-24}"
# Same knobs as devnet/guardians.sh
GUARDIAN_STAKE="${GUARDIAN_STAKE:-50000000000uveil}"
GUARDIAN_FUNDING="${GUARDIAN_FUNDING:-55000000000uveil}"
# Offset in blocks from registration height; the protocol maximum
# (types.MaxAvailabilityWindow) so a guardian can serve any secret the chain
# will accept — see devnet/guardians.sh for the full rationale.
GUARDIAN_AVAILABLE_UNTIL="${GUARDIAN_AVAILABLE_UNTIL:-5256000}"
# Well-known share-key passphrase — devnet only (encrypted-by-default ruling);
# the passphrase file lands beside the key on the guardian's volume, so the
# guardian container resolves it via the sibling convention even though the
# config records init-container-absolute paths
GUARDIAN_KEY_PASSPHRASE="${GUARDIAN_KEY_PASSPHRASE:-timeflare-devnet-share-key}"
# Well-known dashboard password — devnet only, shared by every guardian, and
# identical to the native devnet's (devnet/guardians.sh)
DASHBOARD_PASSWORD="${DASHBOARD_PASSWORD:-timeflare-devnet}"

SHARED=/shared
KEYRING_DIR="$SHARED/genesis-keyring"
PASSPHRASE_FILE="$SHARED/genesis_keyring_passphrase"
REGISTRY_FILE="$SHARED/guardians-registry.conf"

log() { echo "[guardian-init] $*"; }

guardian_name() { printf "guardian-%02d" "$1"; }

# The chain must be reachable (compose health-gating covers this; the retry
# rides out the first-block window)
for _ in $(seq 1 30); do
    curl -s --max-time 2 "$RPC_ENDPOINT/status" >/dev/null 2>&1 && break
    sleep 2
done
curl -s --max-time 2 "$RPC_ENDPOINT/status" >/dev/null 2>&1 || {
    echo "[guardian-init] chain unreachable at $RPC_ENDPOINT" >&2
    exit 1
}

is_registered() { # address
    timeflared query secrets show-guardian "$1" --node "$RPC_ENDPOINT" --output json >/dev/null 2>&1
}

balance_of() { # address
    local amount
    amount=$(timeflared query bank balances "$1" --node "$RPC_ENDPOINT" -o json 2>/dev/null |
        jq -r '.balances[] | select(.denom=="uveil") | .amount' 2>/dev/null) || true
    echo "${amount:-0}"
}

# ── Phase 1: keys + config per guardian (idempotent) ────────────────────────
declare -a NAMES=() ADDRESSES=()
for i in $(seq 1 "$GUARDIAN_COUNT"); do
    name=$(guardian_name "$i")
    ghome="/homes/$name"
    gdir="$ghome/.timeflare/guardian"
    keyring_dir="$gdir/keyring"
    passfile="$gdir/keyring_passphrase"
    config_file="$gdir/config.yaml"

    mkdir -p "$keyring_dir"

    if [[ ! -f "$passfile" ]]; then
        openssl rand -base64 32 | tr -d "=+/" | cut -c1-25 | tr -d '\n' > "$passfile"
        chmod 600 "$passfile"
    fi
    passphrase=$(cat "$passfile")

    if ! echo "$passphrase" | timeflared keys show "$name" --keyring-backend file --keyring-dir "$keyring_dir" >/dev/null 2>&1; then
        printf '%s\n%s\n' "$passphrase" "$passphrase" | timeflared keys add "$name" \
            --keyring-backend file --keyring-dir "$keyring_dir" --output json >/dev/null
    fi
    address=$(echo "$passphrase" | timeflared keys show "$name" -a --keyring-backend file --keyring-dir "$keyring_dir")

    if [[ ! -f "$config_file" ]]; then
        HOME="$ghome" guardiand config init --config-path "$config_file" \
            --key-name "$name" \
            --keyring-backend file \
            --keyring-dir "$keyring_dir" \
            --keyring-passphrase "$passphrase" \
            --encryption-key-passphrase "$GUARDIAN_KEY_PASSPHRASE" \
            --auto-generate-key >/dev/null
        # Native parity (guardians.sh): passphrase path points at the root
        # copy; the guardian container re-points all paths via env overrides
        HOME="$ghome" guardiand config set --config-path "$config_file" keyring-passphrase "$passfile" >/dev/null
        HOME="$ghome" guardiand config set --config-path "$config_file" chain-id "$CHAIN_ID" >/dev/null
        HOME="$ghome" guardiand config set --config-path "$config_file" rpc-endpoint "$RPC_ENDPOINT" >/dev/null
        HOME="$ghome" guardiand config set --config-path "$config_file" grpc-endpoint "$GRPC_ENDPOINT" >/dev/null
        # Container-native monitoring: fixed ports, bind on all interfaces
        HOME="$ghome" guardiand config set --config-path "$config_file" bind-address '0.0.0.0' >/dev/null
        HOME="$ghome" guardiand config set --config-path "$config_file" health-port 21000 >/dev/null
        HOME="$ghome" guardiand config set --config-path "$config_file" metrics-port 21100 >/dev/null
        # The container binds 0.0.0.0 because -p publishes nothing otherwise, so
        # the daemon treats the dashboard as exposed and serves none without a
        # credential — whether or not compose publishes the port. Same shared
        # devnet password as the native path (devnet/guardians.sh), set through
        # the shipped command rather than as a hash constant.
        printf '%s' "$DASHBOARD_PASSWORD" | HOME="$ghome" guardiand config set-dashboard-password \
            --config-path "$config_file" --stdin >/dev/null
    fi

    # Assert the layout the guardian containers' static GUARDIAN_* path
    # overrides depend on (compose points them at the same files under
    # /home/nonroot/.timeflare/guardian). encryption_key_passphrase is the
    # share-key passphrase file the container finds via the sibling-of-the-
    # private-key convention.
    for expected in "$keyring_dir" "$passfile" "$gdir/private_key" "$gdir/encryption_key_passphrase"; do
        [[ -e "$expected" ]] || {
            echo "[guardian-init] $name: expected $expected to exist — volume layout drifted from the compose env overrides" >&2
            exit 1
        }
    done

    NAMES+=("$name")
    ADDRESSES+=("$address")
    log "$name prepared ($address)"
done

# ── Phase 2: one batch funding transaction (single pool sequence) ───────────
required=$(echo "$GUARDIAN_FUNDING" | sed 's/uveil$//')
declare -a UNFUNDED=()
for idx in "${!NAMES[@]}"; do
    if [[ "$(balance_of "${ADDRESSES[$idx]}")" -lt "$required" ]]; then
        UNFUNDED+=("${ADDRESSES[$idx]}")
    fi
done

if [[ ${#UNFUNDED[@]} -gt 0 ]]; then
    log "funding ${#UNFUNDED[@]} guardians in one transaction..."
    gas=$((150000 + 50000 * ${#UNFUNDED[@]}))
    fees="$((gas / 10))uveil"
    GENESIS_PASSPHRASE=$(cat "$PASSPHRASE_FILE")
    if [[ ${#UNFUNDED[@]} -eq 1 ]]; then
        tx_json=$(echo "$GENESIS_PASSPHRASE" | timeflared tx bank send bootstrapping "${UNFUNDED[0]}" "$GUARDIAN_FUNDING" \
            --chain-id "$CHAIN_ID" --keyring-backend file --keyring-dir "$KEYRING_DIR" \
            --node "$RPC_ENDPOINT" --gas "$gas" --fees "$fees" -y -o json)
    else
        tx_json=$(echo "$GENESIS_PASSPHRASE" | timeflared tx bank multi-send bootstrapping "${UNFUNDED[@]}" "$GUARDIAN_FUNDING" \
            --chain-id "$CHAIN_ID" --keyring-backend file --keyring-dir "$KEYRING_DIR" \
            --node "$RPC_ENDPOINT" --gas "$gas" --fees "$fees" -y -o json)
    fi
    tx_hash=$(echo "$tx_json" | jq -r '.txhash')
    [[ -n "$tx_hash" && "$tx_hash" != "null" ]] || { echo "funding broadcast failed: $tx_json" >&2; exit 1; }

    committed=1
    for _ in $(seq 1 20); do
        if timeflared query tx "$tx_hash" --node "$RPC_ENDPOINT" -o json 2>/dev/null | jq -e '.code == 0' >/dev/null 2>&1; then
            committed=0; break
        fi
        sleep 2
    done
    [[ "$committed" -eq 0 ]] || { echo "funding tx $tx_hash never committed" >&2; exit 1; }

    # Wait until every funded ACCOUNT is queryable before anything signs from it
    for address in "${UNFUNDED[@]}"; do
        for _ in $(seq 1 15); do
            timeflared query auth account "$address" --node "$RPC_ENDPOINT" -o json >/dev/null 2>&1 && break
            sleep 1
        done
    done
    log "funding committed (tx $tx_hash)"
fi

# ── Phase 3: parallel registration (each guardian signs from its own account)
declare -a TO_REGISTER=()
for idx in "${!NAMES[@]}"; do
    if is_registered "${ADDRESSES[$idx]}"; then
        log "${NAMES[$idx]} already registered on chain"
    else
        TO_REGISTER+=("$idx")
    fi
done

if [[ ${#TO_REGISTER[@]} -gt 0 ]]; then
    log "registering ${#TO_REGISTER[@]} guardians in parallel..."
    for idx in "${TO_REGISTER[@]}"; do
        name="${NAMES[$idx]}"
        HOME="/homes/$name" guardiand register \
            --config-path "/homes/$name/.timeflare/guardian/config.yaml" \
            --stake-amount "$GUARDIAN_STAKE" \
            --available-until "$GUARDIAN_AVAILABLE_UNTIL" \
            --accept > "/tmp/register-$name.log" 2>&1 &
    done
    wait || true

    failed=0
    for idx in "${TO_REGISTER[@]}"; do
        confirmed=1
        for _ in $(seq 1 20); do
            if is_registered "${ADDRESSES[$idx]}"; then confirmed=0; break; fi
            sleep 1
        done
        if [[ "$confirmed" -eq 0 ]]; then
            log "${NAMES[$idx]} registered (${ADDRESSES[$idx]})"
        else
            echo "[guardian-init] ${NAMES[$idx]} registration NOT confirmed:" >&2
            cat "/tmp/register-${NAMES[$idx]}.log" >&2 || true
            failed=$((failed + 1))
        fi
    done
    [[ "$failed" -eq 0 ]] || exit 1
fi

# ── Registry for the host (S1 drill maps address → container) + ownership ───
: > "$REGISTRY_FILE"
for idx in "${!NAMES[@]}"; do
    echo "${NAMES[$idx]}:${ADDRESSES[$idx]}:registered:$(date +%s)" >> "$REGISTRY_FILE"
done
chmod a+r "$REGISTRY_FILE"

for name in "${NAMES[@]}"; do
    chown -R 65532:65532 "/homes/$name/.timeflare"
done

log "$GUARDIAN_COUNT guardians prepared, funded, and registered"
