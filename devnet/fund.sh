#!/usr/bin/env bash
# fund.sh — the ONE funding primitive, for the devnet and for every named
# network (mobile build plan §7.1).
#
# Sends uveil from a network's funding key to any bech32 address, waits for
# on-chain inclusion, and exits with a deterministic code. Every funding use
# case (app onboarding testing, e2e suites, guardian bring-up, grants on a
# public network) composes over this script; policy (who may be funded, how
# often) deliberately lives elsewhere — the primitive stays policy-free.
#
# Usage:
#   devnet/fund.sh --network <id> <tmflr-address> [amount-uveil]
#                  [--key <name>] [--node <host:port|url>]
#
#   --network        REQUIRED, and there is no default. The id of a network in
#                    the registry (networks.json), which supplies the chain id
#                    and the node. A command that signs must be told where: a
#                    default that is safe on one machine is a transfer on the
#                    wrong chain on another.
#   <tmflr-address>  recipient, validated (prefix + bech32 checksum) BEFORE
#                    anything is sent
#   [amount-uveil]   integer uveil amount; default 1000000000 (1,000 VEIL —
#                    a preset secret's full Phase-1 invoice with headroom)
#   --key            the key to send from. Default: `bootstrapping` on a local
#                    network, `grant` on any other.
#   --node           Tendermint RPC endpoint, for a LOCAL network only (default:
#                    the registry's) — the native devnet, the compose stack and
#                    a devnet on the LAN are all the same network at different
#                    addresses. Any other network is reached where the registry
#                    says and nowhere else.
#
# Where the key is read from:
#   a local network   $TIMEFLARE_HOME/genesis-keyring, passphrase in
#                     $TIMEFLARE_HOME/genesis_keyring_passphrase (what
#                     `make dev-up` creates)
#   any other         $TIMEFLARE_HOME/networks/<id>/keyring, passphrase in
#                     $TIMEFLARE_HOME/networks/<id>/keyring_passphrase. One
#                     keyring per network, so a key for one network is never
#                     loaded while acting on another.
#
# The chain id the registry states is asserted twice: against what the node
# reports before anything is signed, and by the signature itself, which every
# other chain rejects.
#
# Environment: TIMEFLARE_HOME (default ~/.timeflare); TIMEFLARE_NETWORKS_FILE
# (default: this repository's networks.json) — a registry file in the same
# format, for a network whose entry is not published yet.
#
# Mechanics: broadcast-mode sync, then poll the RPC to inclusion. Retries on
# account-sequence mismatch so parallel callers drawing on the same key are
# safe.
#
# Exit codes (deterministic — CI consumers rely on these):
#   0  funded and delivered on-chain (code 0)
#   2  usage error (missing/extra arguments, malformed amount)
#   3  invalid recipient address (wrong prefix or bech32 checksum)
#   4  RETIRED (was: unknown pool key) — never reused, so a consumer matching on
#      exit codes cannot mistake a new failure for the old one
#   5  node unreachable or answering for another chain, or the keyring or its
#      passphrase missing
#   6  broadcast failed (after sequence-mismatch retries)
#   7  tx included but failed on-chain (non-zero DeliverTx code)
#   8  timed out waiting for inclusion

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common-utils.sh
source "$SCRIPT_DIR/lib/common-utils.sh"

TIMEFLARE_HOME="${TIMEFLARE_HOME:-$HOME/.timeflare}"
NETWORKS_FILE="${TIMEFLARE_NETWORKS_FILE:-$SCRIPT_DIR/../networks.json}"

DEFAULT_AMOUNT=1000000000 # 1,000 VEIL
NETWORK=""
NODE=""
KEY=""

usage() {
    sed -n '2,/^set -uo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'
}

fail() { # exit_code message
    local code=$1; shift
    echo "❌ $*" >&2
    exit "$code"
}

# ── argument parsing ─────────────────────────────────────────────────────────
POSITIONAL=()
while [ $# -gt 0 ]; do
    case "$1" in
        --network)
            [ $# -ge 2 ] || { usage >&2; fail 2 "--network requires a value"; }
            NETWORK="$2"; shift 2 ;;
        --network=*)
            NETWORK="${1#--network=}"; shift ;;
        --key)
            [ $# -ge 2 ] || { usage >&2; fail 2 "--key requires a value"; }
            KEY="$2"; shift 2 ;;
        --key=*)
            KEY="${1#--key=}"; shift ;;
        --node)
            [ $# -ge 2 ] || { usage >&2; fail 2 "--node requires a value"; }
            NODE="$2"; shift 2 ;;
        --node=*)
            NODE="${1#--node=}"; shift ;;
        -h|--help)
            usage; exit 0 ;;
        -*)
            usage >&2; fail 2 "unknown flag: $1" ;;
        *)
            POSITIONAL+=("$1"); shift ;;
    esac
done

[ ${#POSITIONAL[@]} -ge 1 ] || { usage >&2; fail 2 "missing recipient address"; }
[ ${#POSITIONAL[@]} -le 2 ] || { usage >&2; fail 2 "too many arguments"; }

RECIPIENT="${POSITIONAL[0]}"
AMOUNT="${POSITIONAL[1]:-$DEFAULT_AMOUNT}"

# ── the network: everything a signature depends on comes from its entry ──────
[ -n "$NETWORK" ] || { usage >&2; fail 2 "missing --network — name the network to act on (devnet, for the local one)"; }
[ -f "$NETWORKS_FILE" ] || fail 2 "network registry not found at $NETWORKS_FILE"
ENTRY=$(jq -c --arg id "$NETWORK" '.networks[] | select(.id == $id)' "$NETWORKS_FILE" 2>/dev/null)
[ -n "$ENTRY" ] || fail 2 "no network '$NETWORK' in $NETWORKS_FILE (known: $(jq -r '[.networks[].id] | join(", ")' "$NETWORKS_FILE"))"

CHAIN_ID=$(jq -r '.chainId' <<<"$ENTRY")
IS_LOCAL=$(jq -r '.local' <<<"$ENTRY")
REGISTRY_NODE=$(jq -r '.endpoints.rpc[0]' <<<"$ENTRY")

if [ "$IS_LOCAL" = "true" ]; then
    KEY="${KEY:-bootstrapping}"
    KEYRING_DIR="$TIMEFLARE_HOME/genesis-keyring"
    PASSPHRASE_FILE="$TIMEFLARE_HOME/genesis_keyring_passphrase"
    KEYRING_HINT="is this a devnet host? (make dev-up)"
    NODE="${NODE:-$REGISTRY_NODE}"
else
    [ -z "$NODE" ] || fail 2 "--node is for a local network; '$NETWORK' is reached where the registry says ($REGISTRY_NODE)"
    KEY="${KEY:-grant}"
    KEYRING_DIR="$TIMEFLARE_HOME/networks/$NETWORK/keyring"
    PASSPHRASE_FILE="$TIMEFLARE_HOME/networks/$NETWORK/keyring_passphrase"
    KEYRING_HINT="fetch the '$KEY' key for $NETWORK first"
    NODE="$REGISTRY_NODE"
fi

# Two forms of one address: a URL to poll, and what `timeflared --node` takes.
# A local node is plain HTTP on its own port. Any other is HTTPS, and the CLI
# needs the port spelled out.
case "$NODE" in
    https://*)
        NODE_HTTP="${NODE%/}"
        NODE_TM="$NODE_HTTP"
        case "${NODE_HTTP#https://}" in *:*) ;; *) NODE_TM="$NODE_HTTP:443" ;; esac
        ;;
    *)
        NODE_BARE="${NODE#tcp://}"; NODE_BARE="${NODE_BARE#http://}"
        NODE_TM="tcp://$NODE_BARE"
        NODE_HTTP="http://$NODE_BARE"
        ;;
esac
[ "$IS_LOCAL" = "true" ] || [ "${NODE_HTTP#https://}" != "$NODE_HTTP" ] || fail 2 "network '$NETWORK' is not local and its node is not https: $NODE"

# ── validation (before anything is sent) ─────────────────────────────────────
case "$AMOUNT" in
    ''|*[!0-9]*) fail 2 "amount must be a positive integer uveil value, got '$AMOUNT'" ;;
esac
[ "$AMOUNT" -gt 0 ] || fail 2 "amount must be greater than zero"

case "$RECIPIENT" in
    tmflr1*) ;;
    *) fail 3 "recipient must be a bech32 tmflr… address, got '$RECIPIENT'" ;;
esac
# Checksum validation via the address codec — rejects any mangled address
if ! timeflared keys parse "$RECIPIENT" >/dev/null 2>&1; then
    fail 3 "recipient address failed bech32 validation: $RECIPIENT"
fi

[ -d "$KEYRING_DIR" ] || fail 5 "keyring not found at $KEYRING_DIR — $KEYRING_HINT"
[ -f "$PASSPHRASE_FILE" ] || fail 5 "keyring passphrase not found at $PASSPHRASE_FILE"

NODE_STATUS=$(curl -s --max-time 5 "$NODE_HTTP/status" 2>/dev/null) ||
    fail 5 "node unreachable at $NODE_HTTP — is the chain running?"
# Before anything is signed: the node must be the chain the registry names.
REPORTED_CHAIN_ID=$(jq -r '.result.node_info.network // empty' <<<"$NODE_STATUS" 2>/dev/null)
[ "$REPORTED_CHAIN_ID" = "$CHAIN_ID" ] ||
    fail 5 "$NODE_HTTP reports chain '${REPORTED_CHAIN_ID:-nothing}', but network '$NETWORK' is $CHAIN_ID — nothing was sent"

# ── broadcast (sync) with account-sequence-mismatch retry ────────────────────
MAX_BROADCAST_ATTEMPTS=5
TX_HASH=""
attempt=1
while [ $attempt -le $MAX_BROADCAST_ATTEMPTS ]; do
    OUT=$(cat "$PASSPHRASE_FILE" | timeflared tx bank send "$KEY" "$RECIPIENT" "${AMOUNT}uveil" \
        --chain-id "$CHAIN_ID" \
        --keyring-backend file \
        --keyring-dir "$KEYRING_DIR" \
        --fees 20000uveil \
        --broadcast-mode sync \
        --node "$NODE_TM" \
        --output json \
        --yes 2>&1)
    STATUS=$?

    # The CLI prints diagnostics before the JSON — the result is the last line
    SUBMIT=$(printf '%s\n' "$OUT" | tail -1)
    CODE=$(printf '%s\n' "$SUBMIT" | jq -r '.code // empty' 2>/dev/null)
    HASH=$(printf '%s\n' "$SUBMIT" | jq -r '.txhash // empty' 2>/dev/null)

    if [ $STATUS -eq 0 ] && [ "$CODE" = "0" ] && [ -n "$HASH" ]; then
        TX_HASH="$HASH"
        break
    fi

    # Code 19 (tx already in mempool cache): a parallel caller broadcast the
    # byte-identical transfer on the same sequence — that tx IS this caller's
    # transfer, so adopt its hash and poll it to inclusion below.
    if [ "$CODE" = "19" ] && [ -n "$HASH" ]; then
        echo "…identical transfer already in the mempool (parallel caller) — tracking it" >&2
        TX_HASH="$HASH"
        break
    fi

    # Parallel callers race the pool key's sequence — refresh and retry
    if [ "$CODE" = "32" ] || printf '%s' "$OUT" | grep -qi "account sequence mismatch"; then
        echo "…account sequence mismatch (parallel caller) — retrying ($attempt/$MAX_BROADCAST_ATTEMPTS)" >&2
        attempt=$((attempt + 1))
        sleep $attempt
        continue
    fi

    fail 6 "broadcast failed (CheckTx code ${CODE:-n/a}): $OUT"
done
[ -n "$TX_HASH" ] || fail 6 "broadcast failed after $MAX_BROADCAST_ATTEMPTS sequence-mismatch retries"

# ── poll to inclusion ────────────────────────────────────────────────────────
WAITED=0
POLL_TIMEOUT="${FUND_TIMEOUT_SECONDS:-60}"
while [ "$WAITED" -lt "$POLL_TIMEOUT" ]; do
    RESULT=$(curl -s --max-time 3 "$NODE_HTTP/tx?hash=0x$TX_HASH" 2>/dev/null)
    DELIVER_CODE=$(printf '%s' "$RESULT" | jq -r '.result.tx_result.code // empty' 2>/dev/null)
    if [ -n "$DELIVER_CODE" ]; then
        if [ "$DELIVER_CODE" = "0" ]; then
            HEIGHT=$(printf '%s' "$RESULT" | jq -r '.result.height')
            echo "✅ funded $RECIPIENT with ${AMOUNT}uveil from $KEY on $NETWORK ($CHAIN_ID) (tx $TX_HASH, height $HEIGHT)"
            exit 0
        fi
        LOG=$(printf '%s' "$RESULT" | jq -r '.result.tx_result.log // ""')
        fail 7 "tx $TX_HASH failed on-chain (code $DELIVER_CODE): $LOG"
    fi
    sleep 2
    WAITED=$((WAITED + 2))
done

fail 8 "timed out after ${POLL_TIMEOUT}s waiting for tx $TX_HASH to be included"
