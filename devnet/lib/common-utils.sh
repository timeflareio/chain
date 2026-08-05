#!/bin/bash
# Common Utilities
# Shared functions for all Timeflare devnet scripts. Source this file, don't run it.

# Configuration defaults
export CHAIN_ID="${CHAIN_ID:-timeflare-test}"
export DEFAULT_DENOM="${DEFAULT_DENOM:-uveil}"
export MIN_GAS_PRICE="${MIN_GAS_PRICE:-0.1uveil}"
export HOME_DIR="${HOME_DIR:-$HOME/.timeflare}"
export RPC_ENDPOINT="${RPC_ENDPOINT:-http://localhost:26657}"
export API_ENDPOINT="${API_ENDPOINT:-http://localhost:1317}"

# Colours for output
export GREEN='\033[0;32m'
export YELLOW='\033[1;33m'
export RED='\033[0;31m'
export BLUE='\033[0;34m'
export CYAN='\033[0;36m'
export NC='\033[0m' # No Colour

# Logging functions (stderr so command substitution stays clean)
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1" >&2
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_header() {
    echo -e "${CYAN}$1${NC}" >&2
}

# Check if chain is running (retries to ride out transient RPC hiccups)
check_chain_status() {
    for _ in 1 2 3; do
        if curl -s --max-time 2 "$RPC_ENDPOINT/status" > /dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    log_error "Chain is not running at $RPC_ENDPOINT — start it with 'make dev-up'"
    return 1
}

# Get current block height
get_current_height() {
    curl -s "$RPC_ENDPOINT/status" | jq -r '.result.sync_info.latest_block_height' 2>/dev/null || echo "0"
}

# Wait for a transaction to be confirmed
wait_for_tx() {
    local tx_hash="$1"
    local max_attempts="${2:-10}"
    local delay="${3:-2}"

    for _ in $(seq 1 "$max_attempts"); do
        if timeflared query tx "$tx_hash" --output json 2>/dev/null | jq -e '.code == 0' > /dev/null 2>&1; then
            return 0
        fi
        sleep "$delay"
    done

    log_error "Transaction $tx_hash not confirmed after $max_attempts attempts"
    return 1
}

# Get total registered guardian count from the chain
get_total_guardian_count() {
    timeflared query secrets list-guardians --output json 2>/dev/null | \
        jq -r '.guardians | length' 2>/dev/null || echo "0"
}

# Check if a guardian address is registered on chain
is_guardian_registered() {
    local address="$1"
    timeflared query secrets show-guardian "$address" --output json >/dev/null 2>&1
}

# Format uveil amount to VEIL
format_veil_amount() {
    local uveil="$1"
    echo "$((uveil / 1000000)) VEIL"
}

# Export all functions
export -f log_info log_warn log_error log_header
export -f check_chain_status get_current_height wait_for_tx
export -f get_total_guardian_count is_guardian_registered
export -f format_veil_amount

# Seconds per block, measured from two consecutive headers. Empty when the chain
# cannot be read: a cadence note is never worth failing a run over.
measure_block_seconds() {
    local rpc="${1:-$RPC_ENDPOINT}" h t1 t2
    h=$(curl -s --max-time 3 "$rpc/status" | jq -r '.result.sync_info.latest_block_height' 2>/dev/null) || return 1
    [ -n "$h" ] && [ "$h" != "null" ] && [ "$h" -gt 1 ] 2>/dev/null || return 1
    t1=$(curl -s --max-time 3 "$rpc/block?height=$((h - 1))" | jq -r '.result.block.header.time' 2>/dev/null)
    t2=$(curl -s --max-time 3 "$rpc/block?height=$h" | jq -r '.result.block.header.time' 2>/dev/null)
    [ -n "$t1" ] && [ -n "$t2" ] && [ "$t1" != "null" ] && [ "$t2" != "null" ] || return 1
    python3 -c 'import sys,datetime
def p(x):
    x=x.rstrip("Z")
    if "." in x: x=x[:x.index(".")+7]
    return datetime.datetime.fromisoformat(x)
print("%.1f" % (p(sys.argv[2])-p(sys.argv[1])).total_seconds())' "$t1" "$t2" 2>/dev/null || return 1
}

# Report the cadence a suite is about to run against, and how to change it.
#
# Every wait in the e2e and scenario suites is a block distance, so the cadence
# decides how long a run takes and nothing about what it asserts. At the shipping
# cadence the same assertions take about six times as long, which is worth knowing
# before the wait rather than after it.
#
# It compares what it MEASURED against the test cadence, not whether an environment
# variable happens to be set in this shell — the cadence belongs to whoever started
# the chain, which may have been another session. Nothing here fails a run: the
# cadence is the run's choice.
cadence_note() {
    local rpc="${1:-$RPC_ENDPOINT}" test_cadence="${2:-${TEST_BLOCK_TIME:-1s}}" measured target
    measured=$(measure_block_seconds "$rpc" || true)
    [ -n "$measured" ] || return 0
    target=$(printf '%s' "$test_cadence" | sed -E 's/ms$//; s/s$//')
    case "$test_cadence" in *ms) target=$(python3 -c "print(float('$target')/1000)" 2>/dev/null || echo 1);; esac
    echo -e "${CYAN}[cadence]${NC} ~${measured}s per block" >&2
    if python3 -c "import sys; sys.exit(0 if float('$measured') > float('$target') * 1.5 else 1)" 2>/dev/null; then
        echo -e "${CYAN}[cadence]${NC} slower than the test cadence (${test_cadence}), so this run will take" >&2
        echo -e "${CYAN}[cadence]${NC} proportionally longer. To restart the devnet at the test cadence:" >&2
        echo -e "${CYAN}[cadence]${NC}   TIMEFLARE_BLOCK_TIME=${test_cadence} make dev-reset" >&2
    fi
}
