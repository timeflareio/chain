#!/usr/bin/env bash
# fund.sh — the ONE devnet funding primitive (mobile build plan §7.1).
#
# Sends uveil from the bootstrapping genesis key to any bech32 address, waits for
# on-chain inclusion, and exits with a deterministic code. Every funding use
# case (Spike B wallets, app onboarding testing, e2e suites, guardian
# bring-up) composes over this script; policy (who may be funded, how often)
# deliberately lives elsewhere — the primitive stays policy-free.
#
# Usage:
#   devnet/fund.sh <tmflr-address> [amount-uveil] [--node <host:port|url>]
#
#   <tmflr-address>  recipient, validated (prefix + bech32 checksum) BEFORE
#                    anything is sent
#   [amount-uveil]   integer uveil amount; default 1000000000 (1,000 VEIL —
#                    a preset secret's full Phase-1 invoice with headroom)
#   --node           Tendermint RPC endpoint (default localhost:26657) — one
#                    script serves the native devnet and the compose stack
#
# Environment: TIMEFLARE_HOME (default ~/.timeflare) locates the genesis
# keyring + raw passphrase file (devnet/lib/keyring-utils.sh pattern);
# CHAIN_ID (default timeflare-test).
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
#   5  node unreachable, or genesis keyring/passphrase missing
#   6  broadcast failed (after sequence-mismatch retries)
#   7  tx included but failed on-chain (non-zero DeliverTx code)
#   8  timed out waiting for inclusion

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common-utils.sh
source "$SCRIPT_DIR/lib/common-utils.sh"

CHAIN_ID="${CHAIN_ID:-timeflare-test}"
TIMEFLARE_HOME="${TIMEFLARE_HOME:-$HOME/.timeflare}"
GENESIS_KEYRING_DIR="$TIMEFLARE_HOME/genesis-keyring"
GENESIS_PASSPHRASE_FILE="$TIMEFLARE_HOME/genesis_keyring_passphrase"

DEFAULT_AMOUNT=1000000000 # 1,000 VEIL
NODE="localhost:26657"

# Genesis holds two pools (docs/spec.md "Genesis Pool Allocations"): this one
# bootstrapping key, which funds every actor a launch needs, and the rebate
# pool, which has no key at all. There is nothing to choose between, so this
# script takes no pool argument.
BOOTSTRAP_KEY="bootstrapping"

usage() {
    sed -n '2,38p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
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

# Normalise the node argument: accept host:port, tcp://…, http(s)://…
NODE_BARE="${NODE#tcp://}"; NODE_BARE="${NODE_BARE#http://}"; NODE_BARE="${NODE_BARE#https://}"
NODE_TM="tcp://$NODE_BARE"
NODE_HTTP="http://$NODE_BARE"

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

[ -d "$GENESIS_KEYRING_DIR" ] || fail 5 "genesis keyring not found at $GENESIS_KEYRING_DIR — is this a devnet host? (make dev-up)"
[ -f "$GENESIS_PASSPHRASE_FILE" ] || fail 5 "genesis keyring passphrase not found at $GENESIS_PASSPHRASE_FILE"

if ! curl -s --max-time 3 "$NODE_HTTP/status" >/dev/null 2>&1; then
    fail 5 "node unreachable at $NODE_HTTP — is the chain running?"
fi

# ── broadcast (sync) with account-sequence-mismatch retry ────────────────────
MAX_BROADCAST_ATTEMPTS=5
TX_HASH=""
attempt=1
while [ $attempt -le $MAX_BROADCAST_ATTEMPTS ]; do
    OUT=$(cat "$GENESIS_PASSPHRASE_FILE" | timeflared tx bank send "$BOOTSTRAP_KEY" "$RECIPIENT" "${AMOUNT}uveil" \
        --chain-id "$CHAIN_ID" \
        --keyring-backend file \
        --keyring-dir "$GENESIS_KEYRING_DIR" \
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
            echo "✅ funded $RECIPIENT with ${AMOUNT}uveil from $BOOTSTRAP_KEY (tx $TX_HASH, height $HEIGHT)"
            exit 0
        fi
        LOG=$(printf '%s' "$RESULT" | jq -r '.result.tx_result.log // ""')
        fail 7 "tx $TX_HASH failed on-chain (code $DELIVER_CODE): $LOG"
    fi
    sleep 2
    WAITED=$((WAITED + 2))
done

fail 8 "timed out after ${POLL_TIMEOUT}s waiting for tx $TX_HASH to be included"
