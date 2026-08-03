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
