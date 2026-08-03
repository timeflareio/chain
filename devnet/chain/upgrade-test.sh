#!/bin/bash
# Devnet Upgrade Rehearsal
# Schedules a software upgrade on the local devnet via governance and walks the
# production upgrade flow:
#   1. run this with the OLD binary (upgrade NOT yet registered in app/upgrades.go)
#   2. the chain halts at the upgrade height with 'UPGRADE "<name>" NEEDED'
#   3. register the upgrade, 'make install', 'make dev-up' — the handler runs
#      once and the chain resumes (verify: timeflared query upgrade applied <name>)
#
# Note: x/upgrade deliberately fails consensus if a binary containing the
# handler runs BEFORE the upgrade height ("BINARY UPDATED BEFORE TRIGGER!") —
# this script detects that misordering and explains the recovery.
#
# Usage: ./devnet/chain/upgrade-test.sh <upgrade-name>
# Requires a running devnet (make dev-up) initialised with a short governance
# voting period (chains initialised before this tooling need 'make dev-reset').

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
source "$SCRIPT_DIR/../lib/common-utils.sh"

UPGRADE_NAME="${1:?usage: $0 <upgrade-name>}"
BLOCKS_UNTIL_UPGRADE="${BLOCKS_UNTIL_UPGRADE:-90}"
PROPOSER_KEY="bootstrapping"
GENESIS_KEYRING_DIR="$HOME_DIR/genesis-keyring"
CHAIN_LOG="$REPO_ROOT/.devnet/chain.log"

check_chain_status || exit 1

# The rehearsal needs a voting period shorter than the wait budget
voting_period=$(timeflared query gov params --output json | jq -r '.params.voting_period')
case "$voting_period" in
    *h*)
        log_error "Governance voting period is $voting_period — too long to rehearse locally."
        log_error "Re-initialise the devnet with the short dev voting period: make dev-reset"
        exit 1
        ;;
esac
log_info "Voting period: $voting_period"

current_height=$(get_current_height)
upgrade_height=$((current_height + BLOCKS_UNTIL_UPGRADE))
gov_address=$(timeflared query auth module-account gov --output json | jq -r '.account.value.address')
proposal_file="$REPO_ROOT/.devnet/upgrade-proposal-$UPGRADE_NAME.json"

log_header "Rehearsing upgrade '$UPGRADE_NAME' at height $upgrade_height (current: $current_height)"

cat > "$proposal_file" <<EOF
{
  "messages": [
    {
      "@type": "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
      "authority": "$gov_address",
      "plan": {
        "name": "$UPGRADE_NAME",
        "height": "$upgrade_height",
        "info": "devnet upgrade rehearsal"
      }
    }
  ],
  "metadata": "devnet-upgrade-rehearsal",
  "deposit": "10000000uveil",
  "title": "Upgrade to $UPGRADE_NAME",
  "summary": "Devnet rehearsal of the $UPGRADE_NAME software upgrade"
}
EOF

log_info "Submitting software-upgrade proposal..."
cat "$HOME_DIR/genesis_keyring_passphrase" | timeflared tx gov submit-proposal "$proposal_file" \
    --from "$PROPOSER_KEY" \
    --chain-id "$CHAIN_ID" \
    --keyring-backend file \
    --keyring-dir "$GENESIS_KEYRING_DIR" \
    --fees 20000uveil \
    -y >/dev/null
sleep 6

proposal_id=$(timeflared query gov proposals --output json | jq -r '.proposals[-1].id')
log_info "Proposal #$proposal_id submitted — voting yes..."

cat "$HOME_DIR/genesis_keyring_passphrase" | timeflared tx gov vote "$proposal_id" yes \
    --from "$PROPOSER_KEY" \
    --chain-id "$CHAIN_ID" \
    --keyring-backend file \
    --keyring-dir "$GENESIS_KEYRING_DIR" \
    --fees 20000uveil \
    -y >/dev/null

log_info "Waiting for the voting period to end..."
for _ in $(seq 1 60); do
    status=$(timeflared query gov proposal "$proposal_id" --output json 2>/dev/null | jq -r '.proposal.status')
    case "$status" in
        PROPOSAL_STATUS_PASSED) break ;;
        PROPOSAL_STATUS_REJECTED|PROPOSAL_STATUS_FAILED)
            log_error "Proposal $proposal_id ended with status $status"
            exit 1
            ;;
    esac
    sleep 5
done

if [[ "$status" != "PROPOSAL_STATUS_PASSED" ]]; then
    log_error "Proposal did not pass within the wait budget (status: ${status:-unknown})"
    exit 1
fi
log_info "Proposal passed — upgrade scheduled at height $upgrade_height"

log_info "Waiting for the upgrade height..."
while true; do
    if ! curl -s --max-time 2 "$RPC_ENDPOINT/status" >/dev/null 2>&1; then
        if grep -q "BINARY UPDATED BEFORE TRIGGER" "$CHAIN_LOG" 2>/dev/null; then
            log_error "Consensus failure: the running binary already registers the '$UPGRADE_NAME' handler."
            log_error "x/upgrade forbids running the new binary before the upgrade height."
            log_error "Recover: remove '$UPGRADE_NAME' from app/upgrades.go, 'make install',"
            log_error "restart with 'make dev-up', and let the chain halt at the height instead."
            exit 1
        fi
        # Chain stopped responding: expected halt for a binary without the handler
        if grep -q "UPGRADE \"$UPGRADE_NAME\" NEEDED" "$CHAIN_LOG" 2>/dev/null; then
            log_header "⛔ Chain halted at the upgrade height — production behaviour verified."
            log_info "To complete the upgrade:"
            log_info "  1. Register '$UPGRADE_NAME' in app/upgrades.go and run:  make install"
            log_info "  2. Restart the devnet:  make dev-up"
            log_info "  3. Verify:  timeflared query upgrade applied $UPGRADE_NAME"
            exit 0
        fi
        log_error "Chain became unreachable without an upgrade halt — check $CHAIN_LOG"
        exit 1
    fi

    height=$(get_current_height)
    if [[ "$height" -gt "$upgrade_height" ]]; then
        break
    fi
    sleep 3
done

log_error "Chain passed height $upgrade_height without halting — was the upgrade plan cleared?"
exit 1
