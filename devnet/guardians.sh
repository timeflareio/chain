#!/bin/bash
# Devnet Guardian Orchestration
# Thin wrapper around guardiand/timeflared for running multiple local guardians.
# All guardian behaviour lives in the guardiand binary — this script only
# creates keys, funds accounts from the genesis pool, and manages processes.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

source "$SCRIPT_DIR/lib/common-utils.sh"

# Configuration (override via environment)
# GUARDIAN_STAKE is the initial float deposit (--stake-amount); GUARDIAN_FUNDING
# is the bank transfer that must cover it. Funding must stay above
# stake + the 1,000 VEIL entry fee + gas, or registration fails with
# insufficient funds — raise the two together.
GUARDIAN_STAKE="${GUARDIAN_STAKE:-50000000000uveil}"
GUARDIAN_FUNDING="${GUARDIAN_FUNDING:-55000000000uveil}"
# Availability is an OFFSET in blocks from the registration height, and devnet
# guardians take the longest term the protocol allows: types.MaxAvailabilityWindow
# (5,256,000 blocks = one year at the 6s default cadence). It is a block count,
# so the wall-clock term scales with TIMEFLARE_BLOCK_TIME — a 1s devnet gets the
# same 5,256,000 blocks, which is only ~61 days.
#
# Guardian selection requires AvailableUntil >= the secret's reveal END block
# (x/secrets/keeper/guardian_selection.go), so a short window silently caps how
# far ahead a secret can be scheduled and surfaces as "insufficient guardians"
# — saying nothing about availability, which is what made the old 10,000-block
# default (~16.7 hours) confusing rather than merely limiting. The protocol pins
# MaxRevealHorizon to this same constant, so a guardian at the maximum can serve
# any secret the chain will accept.
GUARDIAN_AVAILABLE_UNTIL="${GUARDIAN_AVAILABLE_UNTIL:-5256000}"
# Well-known share-key passphrase — devnet only (key custody plan ruling:
# generated keys are encrypted-by-default with no plaintext path, so the
# devnet's ergonomics cost is this one known passphrase)
GUARDIAN_KEY_PASSPHRASE="${GUARDIAN_KEY_PASSPHRASE:-timeflare-devnet-share-key}"
GRPC_ENDPOINT="${GRPC_ENDPOINT:-localhost:9090}"
# Guardian i gets health BASE_HEALTH_PORT+i, metrics BASE_METRICS_PORT+i and
# dashboard BASE_DASHBOARD_PORT+i,
# so the devnet claims one contiguous 21000–21199 region: 100 guardians of
# headroom per block before the two could meet. The region is deliberate —
# above 1024 (no root), below macOS's 49152 ephemeral floor (else collisions are
# random), below the chain's 26657 so TIMEFLARE_DOCKER_PORT_OFFSET (only ever
# added) can never slide onto it, outside Kubernetes' 30000–32767 NodePort range,
# and clear of the local dev crowd — Metro 8081/8090, adb 5037 + emulator
# 5554/5555, Prometheus 9090, node_exporter 9100, Grafana 3000, Syncthing 22000.
BASE_HEALTH_PORT="${BASE_HEALTH_PORT:-21000}"
BASE_METRICS_PORT="${BASE_METRICS_PORT:-21100}"
# Guardian i also gets a dashboard on BASE_DASHBOARD_PORT+i. Fanned out like the
# other two listeners because a 24-guardian devnet would otherwise have 24
# daemons fighting over one port. Bound to the daemon's bind_address (0.0.0.0 by
# default) like health and metrics.
BASE_DASHBOARD_PORT="${BASE_DASHBOARD_PORT:-21200}"
# One shared password across every devnet guardian, hashed into each config by
# start_one. On 0.0.0.0 the daemon serves no dashboard without a credential, so
# the dev path exercises the authentication code that ships rather than a bypass
# — an escape hatch is a thing that can be left on in production. The cost is
# per-origin: a first visit to all 24 dashboards means 24 browser prompts.
DASHBOARD_PASSWORD="${DASHBOARD_PASSWORD:-timeflare-devnet}"

# Runtime state (PIDs, logs, registry) — gitignored
RUNTIME_DIR="$REPO_ROOT/.devnet/guardians"
REGISTRY_FILE="$RUNTIME_DIR/registry.conf"

usage() {
    cat <<EOF
Usage: $0 <command> [options]

Commands:
    register <count>   Create, fund, and register <count> guardians on chain
    start [count]      Start registered guardians (default: all registered)
    stop               Stop all running guardians
    status             Show registration, process, and health status
    logs <name>        Tail the log of a guardian (e.g. guardian-01)
    clean              Stop guardians and remove runtime state

Environment overrides:
    GUARDIAN_STAKE=$GUARDIAN_STAKE
    GUARDIAN_FUNDING=$GUARDIAN_FUNDING
    BASE_HEALTH_PORT=$BASE_HEALTH_PORT  BASE_METRICS_PORT=$BASE_METRICS_PORT
    BASE_DASHBOARD_PORT=$BASE_DASHBOARD_PORT
EOF
    exit 1
}

guardian_name() {
    printf "guardian-%02d" "$1"
}

guardian_home() {
    echo "$HOME_DIR/guardian/$1"
}

guardian_pid() {
    local pid_file="$RUNTIME_DIR/$1/guardian.pid"
    [[ -f "$pid_file" ]] && cat "$pid_file"
}

is_running() {
    local pid
    pid=$(guardian_pid "$1")
    [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

in_registry() {
    [[ -f "$REGISTRY_FILE" ]] && grep -q "^$1:" "$REGISTRY_FILE"
}

# Current uveil balance of an address (0 if the account does not exist yet)
pool_balance_of() {
    local amount
    amount=$(timeflared query bank balances "$1" --home "$HOME_DIR" -o json 2>/dev/null |
        jq -r '.balances[] | select(.denom=="uveil") | .amount' 2>/dev/null) || true
    echo "${amount:-0}"
}

# Guardian's on-chain float in uveil, as "stake locked" — stake is the total
# deposited working capital, locked is the portion tied up in per-secret
# bonds right now. unlocked (stake - locked) is what actually gates bond
# eligibility for a new secret — a much smaller number than the wallet
# balance once a guardian has deposited its float.
# The guardian's spendable WALLET balance — distinct from its float. Gas leaves
# here and rewards arrive here, so this is the number that moves as the chain is
# used; the float only changes on top-up, withdrawal or slashing.
guardian_wallet_of() {
    local amount
    amount=$(timeflared query bank balances "$1" --home "$HOME_DIR" -o json 2>/dev/null \
        | jq -r '(.balances[]? | select(.denom == "uveil") | .amount) // empty' 2>/dev/null)
    echo "${amount:-0}"
}

guardian_float_of() {
    local json stake locked
    json=$(timeflared query secrets guardian "$1" --home "$HOME_DIR" -o json 2>/dev/null) || true
    stake=$(echo "$json" | jq -r '.guardian.stake.amount // empty' 2>/dev/null)
    locked=$(echo "$json" | jq -r '.guardian.locked_stake.amount // empty' 2>/dev/null)
    echo "${stake:-0} ${locked:-0}"
}

# Phase 1 (local, no chain interaction): keyring, key, and guardiand config.
# Prints the guardian's address. Idempotent.
prepare_one() {
    local name="$1"
    local home keyring_dir passphrase_file passphrase address config_file

    home=$(guardian_home "$name")
    keyring_dir="$home/keyring"
    passphrase_file="$home/keyring_passphrase"
    config_file="$home/config.yaml"

    mkdir -p "$keyring_dir"

    # Per-guardian keyring passphrase
    if [[ ! -f "$passphrase_file" ]]; then
        openssl rand -base64 32 | tr -d "=+/" | cut -c1-25 | tr -d '\n' > "$passphrase_file"
        chmod 600 "$passphrase_file"
    fi
    passphrase=$(cat "$passphrase_file")

    # Signing key (idempotent)
    if ! echo "$passphrase" | timeflared keys show "$name" --keyring-backend file --keyring-dir "$keyring_dir" >/dev/null 2>&1; then
        printf '%s\n%s\n' "$passphrase" "$passphrase" | timeflared keys add "$name" \
            --keyring-backend file --keyring-dir "$keyring_dir" --output json >/dev/null
    fi
    address=$(echo "$passphrase" | timeflared keys show "$name" -a --keyring-backend file --keyring-dir "$keyring_dir")

    # Guardian configuration (idempotent — config init fails if it already exists)
    if [[ ! -f "$config_file" ]]; then
        guardiand config init --config-path "$config_file" \
            --key-name "$name" \
            --keyring-backend file \
            --keyring-dir "$keyring_dir" \
            --keyring-passphrase "$passphrase" \
            --encryption-key-passphrase "$GUARDIAN_KEY_PASSPHRASE" \
            --auto-generate-key >/dev/null
        guardiand config set --config-path "$config_file" keyring-passphrase "$passphrase_file" >/dev/null
        guardiand config set --config-path "$config_file" chain-id "$CHAIN_ID" >/dev/null
        guardiand config set --config-path "$config_file" rpc-endpoint "$RPC_ENDPOINT" >/dev/null
        guardiand config set --config-path "$config_file" grpc-endpoint "$GRPC_ENDPOINT" >/dev/null
    fi

    echo "$address"
}

# Phase 2 (one transaction): fund every listed address from the
# bootstrapping pool. One multi-send means ONE sequence number for the
# shared pool account — this is what makes registration parallel-safe (each
# CLI invocation signs with the sequence from committed state, so concurrent
# or rapid sends from the pool would collide; a batch cannot).
fund_batch() {
    local addresses=("$@")
    local required unfunded=() tx_json tx_hash
    required=$(echo "$GUARDIAN_FUNDING" | sed 's/uveil$//')

    for address in "${addresses[@]}"; do
        if [[ "$(pool_balance_of "$address")" -ge "$required" ]]; then
            log_info "$address already funded"
        else
            unfunded+=("$address")
        fi
    done

    if [[ ${#unfunded[@]} -eq 0 ]]; then
        log_info "All guardians already funded"
        return 0
    fi

    log_info "Funding ${#unfunded[@]} guardians in one transaction..."
    # Gas scales with recipient count (one bank write per output); fees cover
    # the 0.1 uveil/gas minimum price
    local gas fees
    gas=$((150000 + 50000 * ${#unfunded[@]}))
    fees="$((gas / 10))uveil"
    if [[ ${#unfunded[@]} -eq 1 ]]; then
        # multi-send requires two or more recipients
        tx_json=$(cat "$HOME_DIR/genesis_keyring_passphrase" | timeflared tx bank send bootstrapping "${unfunded[0]}" "$GUARDIAN_FUNDING" \
            --chain-id "$CHAIN_ID" --keyring-backend file --keyring-dir "$HOME_DIR/genesis-keyring" \
            --gas "$gas" --fees "$fees" -y -o json)
    else
        tx_json=$(cat "$HOME_DIR/genesis_keyring_passphrase" | timeflared tx bank multi-send bootstrapping "${unfunded[@]}" "$GUARDIAN_FUNDING" \
            --chain-id "$CHAIN_ID" --keyring-backend file --keyring-dir "$HOME_DIR/genesis-keyring" \
            --gas "$gas" --fees "$fees" -y -o json)
    fi

    tx_hash=$(echo "$tx_json" | jq -r '.txhash')
    if [[ -z "$tx_hash" || "$tx_hash" == "null" ]]; then
        log_error "Funding broadcast failed: $tx_json"
        return 1
    fi
    if ! wait_for_tx "$tx_hash" 15 2; then
        log_error "Funding tx $tx_hash was not committed — see chain logs"
        return 1
    fi

    # The tx index confirms commitment, but the account query a signer makes
    # can lag a beat behind it — wait until every freshly funded ACCOUNT is
    # queryable before anything signs from it
    local address visible
    for address in "${unfunded[@]}"; do
        visible=1
        for _ in $(seq 1 15); do
            if timeflared query auth account "$address" -o json >/dev/null 2>&1; then
                visible=0
                break
            fi
            sleep 1
        done
        if [[ "$visible" -ne 0 ]]; then
            log_error "Funded account $address not queryable after funding commit"
            return 1
        fi
    done
    log_info "Funding committed (tx $tx_hash), all accounts visible"
}

# Phase 3 (parallel): registrations sign from each guardian's OWN account, so
# there is no shared sequence and no contention — fire them all, then confirm
# each on chain.
register_batch() {
    local -a names=("$@")
    local name address config_file

    for name in "${names[@]}"; do
        config_file="$(guardian_home "$name")/config.yaml"
        mkdir -p "$RUNTIME_DIR/$name"
        guardiand register --config-path "$config_file" \
            --stake-amount "$GUARDIAN_STAKE" \
            --available-until "$GUARDIAN_AVAILABLE_UNTIL" \
            --accept > "$RUNTIME_DIR/$name/register.log" 2>&1 &
    done
    wait || true

    # Confirm each registration on chain (bounded poll), then record the
    # registry serially in one pass
    local failed=0
    for name in "${names[@]}"; do
        address=$(cat "$RUNTIME_DIR/$name/address")
        local confirmed=1
        for _ in $(seq 1 20); do
            if is_guardian_registered "$address"; then confirmed=0; break; fi
            sleep 1
        done
        if [[ "$confirmed" -eq 0 ]]; then
            echo "$name:$address:registered:$(date +%s)" >> "$REGISTRY_FILE"
            log_info "$name registered ($address)"
        else
            log_error "$name registration not confirmed on chain"
            failed=$((failed + 1))
        fi
    done
    return "$failed"
}

cmd_register() {
    local count="${1:?usage: $0 register <count>}"
    check_chain_status || exit 1
    mkdir -p "$RUNTIME_DIR"
    touch "$REGISTRY_FILE"

    # Phase 1: prepare keys/configs locally and collect addresses
    local -a pending_names=() pending_addresses=()
    local name address
    for i in $(seq 1 "$count"); do
        name=$(guardian_name "$i")
        if in_registry "$name"; then
            log_info "$name already in local registry"
            continue
        fi
        address=$(prepare_one "$name")
        log_info "$name address: $address"
        mkdir -p "$RUNTIME_DIR/$name"
        echo "$address" > "$RUNTIME_DIR/$name/address"
        pending_names+=("$name")
        pending_addresses+=("$address")
    done

    if [[ ${#pending_names[@]} -eq 0 ]]; then
        log_info "$count guardians registered"
        return 0
    fi

    # Phase 2: one funding transaction for everyone
    fund_batch "${pending_addresses[@]}" || exit 1

    # Phase 3: register in parallel, splitting out anyone already on chain
    local -a to_register=()
    for i in "${!pending_names[@]}"; do
        if is_guardian_registered "${pending_addresses[$i]}"; then
            log_info "${pending_names[$i]} already registered on chain"
            echo "${pending_names[$i]}:${pending_addresses[$i]}:registered:$(date +%s)" >> "$REGISTRY_FILE"
        else
            to_register+=("${pending_names[$i]}")
        fi
    done

    if [[ ${#to_register[@]} -gt 0 ]]; then
        log_header "Registering ${#to_register[@]} guardians in parallel..."
        register_batch "${to_register[@]}" || exit 1
    fi
    log_info "$count guardians registered"
}

start_one() {
    local name="$1" index="$2"
    local config_file health_port metrics_port dashboard_port log_file pid_file

    config_file="$(guardian_home "$name")/config.yaml"
    health_port=$((BASE_HEALTH_PORT + index))
    metrics_port=$((BASE_METRICS_PORT + index))
    dashboard_port=$((BASE_DASHBOARD_PORT + index))
    log_file="$RUNTIME_DIR/$name/guardian.log"
    pid_file="$RUNTIME_DIR/$name/guardian.pid"

    if [[ ! -f "$config_file" ]]; then
        log_error "$name has no configuration — run '$0 register' first"
        return 1
    fi

    mkdir -p "$RUNTIME_DIR/$name"
    guardiand config set --config-path "$config_file" health-port "$health_port" >/dev/null
    guardiand config set --config-path "$config_file" metrics-port "$metrics_port" >/dev/null
    guardiand config set --config-path "$config_file" dashboard-port "$dashboard_port" >/dev/null
    # --stdin rather than a hash constant, so the credential the docs quote is
    # demonstrably the credential the devnet uses.
    printf '%s' "$DASHBOARD_PASSWORD" | guardiand config set-dashboard-password \
        --config-path "$config_file" --stdin >/dev/null

    guardiand start --config-path "$config_file" --accept > "$log_file" 2>&1 &
    echo $! > "$pid_file"

    # Confirm the process survives startup
    for _ in 1 2 3 4 5; do
        sleep 1
        if is_running "$name"; then
            log_info "$name started (health :$health_port, metrics :$metrics_port, dashboard :$dashboard_port)"
            return 0
        fi
    done
    log_error "$name failed to start — last log lines:"
    tail -5 "$log_file" >&2
    return 1
}

cmd_start() {
    local count="$1"
    check_chain_status || exit 1

    if [[ ! -f "$REGISTRY_FILE" ]]; then
        log_error "No guardians registered — run '$0 register <count>' first"
        exit 1
    fi

    local names=()
    while IFS=':' read -r name _rest; do
        [[ -n "$name" ]] && names+=("$name")
    done < "$REGISTRY_FILE"

    [[ -z "$count" ]] && count=${#names[@]}
    if [[ "$count" -gt ${#names[@]} ]]; then
        log_error "Cannot start $count guardians — only ${#names[@]} registered"
        exit 1
    fi

    local started=0 failed=0 index=0
    for name in "${names[@]:0:$count}"; do
        if is_running "$name"; then
            log_info "$name already running"
            started=$((started + 1))
        elif start_one "$name" "$index"; then
            started=$((started + 1))
        else
            failed=$((failed + 1))
        fi
        index=$((index + 1))
    done

    log_info "$started guardians running"
    [[ "$failed" -gt 0 ]] && { log_warn "$failed guardians failed to start"; exit 1; }
    exit 0
}

cmd_stop() {
    local stopped=0
    if [[ -d "$RUNTIME_DIR" ]]; then
        for pid_file in "$RUNTIME_DIR"/*/guardian.pid; do
            [[ -f "$pid_file" ]] || continue
            local pid
            pid=$(cat "$pid_file")
            if kill -0 "$pid" 2>/dev/null; then
                kill "$pid"
                stopped=$((stopped + 1))
            fi
            rm -f "$pid_file"
        done
    fi
    log_info "Stopped $stopped guardians"
}

cmd_status() {
    log_header "Guardian Status"
    local chain_up=0
    if check_chain_status 2>/dev/null; then
        chain_up=1
        echo "  Chain height:         $(get_current_height)"
        echo "  On-chain guardians:   $(get_total_guardian_count)"
    else
        echo "  Chain:                not running"
    fi

    local total=0 running=0
    if [[ -f "$REGISTRY_FILE" ]]; then
        # Three different numbers, previously two with the wrong name. WALLET is
        # the spendable bank balance — gas out, rewards in, so it moves as the
        # chain is used. FLOAT is the collateral deposited into the module, which
        # is identical across guardians registered together and only changes on
        # top-up, withdrawal or slashing; LOCKED is the part of it currently
        # bonded to secrets, so FLOAT minus LOCKED is what can back a new bond.
        printf "  %-12s %-45s %-30s %13s %13s %13s\n" \
            "NAME" "ADDRESS" "STATUS" "WALLET uveil" "FLOAT uveil" "LOCKED uveil"
        while IFS=':' read -r name address _rest; do
            [[ -z "$name" ]] && continue
            total=$((total + 1))
            local state="stopped"
            if is_running "$name"; then
                running=$((running + 1))
                state="running (pid $(guardian_pid "$name"))"
                local config_file
                config_file="$(guardian_home "$name")/config.yaml"
                if guardiand health --config-path "$config_file" --timeout 3 >/dev/null 2>&1; then
                    state="$state, healthy"
                else
                    state="$state, UNHEALTHY"
                fi
            fi
            # All in uveil, never VEIL: the previous display floored to whole
            # VEIL, so a per-secret bond of 60,140 uveil read as "0" — exactly
            # the figure this table is opened for.
            local wallet_uveil="—" float_uveil="—" locked_uveil_out="—"
            if [[ "$chain_up" -eq 1 ]]; then
                local stake_uveil locked_uveil
                read -r stake_uveil locked_uveil <<<"$(guardian_float_of "$address")"
                wallet_uveil="$(guardian_wallet_of "$address")"
                float_uveil="$stake_uveil"
                locked_uveil_out="$locked_uveil"
            fi
            printf "  %-12s %-45s %-30s %13s %13s %13s\n" \
                "$name" "$address" "$state" "$wallet_uveil" "$float_uveil" "$locked_uveil_out"
        done < "$REGISTRY_FILE"
    fi
    echo "  Local registry:       $running/$total running"
}

cmd_logs() {
    local name="${1:?usage: $0 logs <guardian-name>}"
    local log_file="$RUNTIME_DIR/$name/guardian.log"
    [[ -f "$log_file" ]] || { log_error "No log for $name at $log_file"; exit 1; }
    tail -50 "$log_file"
}

cmd_clean() {
    cmd_stop
    rm -rf "$RUNTIME_DIR"
    log_info "Removed guardian runtime state ($RUNTIME_DIR)"
    log_warn "Guardian keys and configs in $HOME_DIR/guardian/ are kept — remove manually if required"
}

case "${1:-}" in
    register) shift; cmd_register "$@" ;;
    start)    shift; cmd_start "$@" ;;
    stop)     cmd_stop ;;
    status)   cmd_status ;;
    logs)     shift; cmd_logs "$@" ;;
    clean)    cmd_clean ;;
    *)        usage ;;
esac
