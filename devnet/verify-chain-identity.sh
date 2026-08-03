#!/bin/bash
# Chain Identity Check
# Verifies that the chain answering on RPC_ENDPOINT was started from the state
# in HOME_DIR. Catches the stale-chain mismatch: a chain from an earlier
# session still owns the RPC port while ~/.timeflare has been wiped and
# re-initialised with a fresh genesis, so every pool account the local keyring
# signs with is unknown to the running chain.
#
# The probe is the bootstrapping pool account: it is funded at genesis,
# so a chain started from this HOME_DIR always knows it.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "$SCRIPT_DIR/lib/common-utils.sh"

if [[ ! -f "$HOME_DIR/genesis_keyring_passphrase" ]]; then
    log_warn "No genesis keyring in $HOME_DIR — skipping chain identity check"
    exit 0
fi

pool_address=$(cat "$HOME_DIR/genesis_keyring_passphrase" |
    timeflared keys show bootstrapping -a \
        --keyring-backend file --keyring-dir "$HOME_DIR/genesis-keyring" 2>/dev/null) || true
if [[ -z "$pool_address" ]]; then
    log_warn "Could not read bootstrapping key from $HOME_DIR/genesis-keyring — skipping chain identity check"
    exit 0
fi

if timeflared query auth account "$pool_address" -o json >/dev/null 2>&1; then
    exit 0
fi

log_error "Running chain at $RPC_ENDPOINT does not know genesis pool account $pool_address"
log_error "It was started from different state than $HOME_DIR — likely a stale chain from an earlier session"
log_error "Fix: 'make dev-reset' (stops the stale chain and starts fresh from clean state)"
exit 1
