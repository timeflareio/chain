#!/usr/bin/env bash
# E2E scenario suite (test strategy plan §3 tier 3): drives the failure-path
# scenarios the happy-path e2e never touches, against a running devnet, and
# asserts exact on-chain amounts and events via block_results / tx queries.
#
#   S1  no-show slash        — one healthy daemon killed after acceptance
#   S10 recipient rebate     — credited at settlement; REBATE_COLLECTION_DRILL=1
#                               additionally collects it (commit–reveal, ~18 min)
#   S10b one accept per guardian — the fleet pays for each slot exactly once,
#                               counted from the tx index (failed deliveries
#                               included), not from the assignment record
#   S11 funded claim kit     — a never-funded address swept onto the chain
#   S2  mid-hold cancellation — pro-rata wages + creator remainder, via CLI
#   S3  early-reveal report   — creator-as-reporter with real share evidence
#   S4  commit-phase cancel   — rejected pre-activation (pending-only rule);
#                               the abandoned commit exits via timeout
#   S6  fee burn              — every fee-bearing block's 90/10 split at
#                               BeginBlock: exact ratio from block events;
#                               S6b asserts the 90% is WITHDRAWABLE — positive
#                               outstanding validator rewards, growth on new
#                               fees, and a real withdraw-rewards drill
#   S5  retention pruning     — terminal secrets pruned at terminal_at +
#                               RetentionBlocks, replaced by a digest-anchored
#                               tombstone + archival event. 200 blocks rather than
#                               the smallest window that proves pruning: the suite
#                               has to READ a terminal record before it is pruned,
#                               and at the test cadence 60 blocks was under eight
#                               seconds — less than the queries take. Runs ONLY when
#                               the chain was started with a reduced window:
#                               TIMEFLARE_RETENTION_BLOCKS=200 make dev-reset &&
#                               TIMEFLARE_RETENTION_BLOCKS=200 make e2e-scenarios
#                               (production retention is ~6 months of blocks —
#                               unset, S5 skips rather than waits)
# ⚠️ Block time and the commit window. The commit window is the protocol
# constant CommitTimeoutBlocks (50 blocks): five minutes at the production 6s
# cadence, but about fifty SECONDS under the TIMEFLARE_BLOCK_TIME=1s these
# scenarios normally run with. S1 kills a guardian daemon and restarts it, and a
# fleet still coming back up used to miss acceptances inside that window — the
# secret then failed at the commit timeout with "insufficient guardian
# acceptances" and the next scenario reported one that "never reached state
# pending". Three things closed that race: guardians_restart waits for the
# victim's health probe before returning, it restarts every registered guardian
# rather than a hardcoded eight (a victim above that index stayed dead for the
# rest of the run, and a dead guardian is still selectable — fatal under the
# zero-width band these scenarios use), and both the restart and the probe now
# fail loudly instead of continuing. A failure of that shape is therefore a real
# signal, not something to re-run past.
#
#   S8  key rotation          — a guardian rotates mid-life with a secret in
#                               flight: the pre-rotation secret is served with
#                               the retired epoch's key, a post-rotation secret
#                               with the new key, both settle exactly, and a
#                               daemon restart mid-way resumes both pipelines.
#                               Runs ONLY when the chain was started with a
#                               reduced rotation interval (native control):
#                               TIMEFLARE_KEY_ROTATION_MIN_INTERVAL=30 make dev-reset &&
#                               TIMEFLARE_KEY_ROTATION_MIN_INTERVAL=30 make e2e-scenarios
#                               (production interval is ~30 days of blocks —
#                               unset, S8 skips rather than waits)
#   S7  settlement liveness    — no settlement_stalled event anywhere in the
#                               run (the quarantine alarm never fired)
#   S9  no treasury            — the community pool is exactly zero at the end
#                               of the run (community_tax = 0 holds)
#
# Requires: make dev-up (chain + guardians), funded user, recipient keypair,
# built SDK. Run via `make e2e-scenarios`.

set -euo pipefail

# The SDK examples this suite drives live in their own repository now. SDK_DIR
# says where they are: a working tree, or a directory unpacked from a pinned SDK
# release. The Makefile passes it; this default keeps the script runnable alone.
SDK_DIR="${SDK_DIR:-.devnet/sdk}"

# How long to wait for a restarted guardian to report healthy.
#
# S1 kills a daemon and restarts it; the next scenario creates a secret expecting
# the full pool. If the restart has not finished, selection can still pick that
# guardian — it is registered and eligible on chain, health being an off-chain
# notion — and the secret fails when it never accepts.
#
# Exceeding this bound is fatal. It used to log and continue, on the reasoning
# that the scenario could judge the outcome for itself; what that actually
# produced was a failure five minutes later in a different scenario, reported as
# a secret that never left 'pending'. The restart not taking is the defect, and
# it should be named where it happens.
#
# Generous by default because the bound is fatal: the loop exits the moment the
# daemon answers, so a long bound costs a healthy run nothing, while a short one
# risks failing a run that was merely slow.
GUARDIAN_HEALTH_TIMEOUT="${GUARDIAN_HEALTH_TIMEOUT:-180}"

RPC="${CHAIN_RPC:-http://localhost:26657}"
# shellcheck source=lib/common-utils.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/common-utils.sh"

DEVNET_DIR=".devnet"
SCENARIO_DIR="$DEVNET_DIR/scenarios"
# TIMEFLARE_HOME redirects the user keyring so the suite drives the native
# devnet (~/.timeflare) or the compose devnet (.devnet/docker/home) unchanged
TIMEFLARE_HOME="${TIMEFLARE_HOME:-$HOME/.timeflare}"
USER_KEYRING="$TIMEFLARE_HOME/user/keyring"
USER_PASSPHRASE_FILE="$TIMEFLARE_HOME/user/keyring_passphrase"
CHAIN_ID="${CHAIN_ID:-timeflare-test}"
# GUARDIAN_CONTROL selects how S1 maps/kills/restarts a guardian daemon:
#   native (default) — guardians.sh pid files
#   docker           — the compose stack's containers + exported registry
GUARDIAN_CONTROL="${GUARDIAN_CONTROL:-native}"
DOCKER_REGISTRY_FILE="$TIMEFLARE_HOME/guardians-registry.conf"

RED='\033[0;31m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'; NC='\033[0m'
PASS=0; FAIL=0

# All logging goes to stderr: several helpers run inside $(...) command
# substitution, where stdout is captured — a failure message on stdout would
# vanish with the substitution instead of reaching the terminal.
info()  { echo -e "${CYAN}[S]${NC} $*" >&2; }
ok()    { echo -e "${GREEN}  ✓${NC} $*" >&2; PASS=$((PASS+1)); }
fatal() { echo -e "${RED}  ✗ $*${NC}" >&2; FAIL=$((FAIL+1)); summary; exit 1; }

assert_eq() { # actual expected message
    if [ "$1" = "$2" ]; then ok "$3 ($1)"; else fatal "$3: got '$1', want '$2'"; fi
}

summary() {
    {
        echo ""
        echo "──────────────────────────────────────"
        echo -e "  scenarios: ${GREEN}${PASS} assertions passed${NC}, ${FAIL} failed"
        echo "──────────────────────────────────────"
    } >&2
}

height() { curl -s "$RPC/status" | jq -r '.result.sync_info.latest_block_height' || true; }

wait_height() { # target
    local target=$1 h
    while :; do
        h=$(height)
        case "$h" in (*[!0-9]*|'') h=0 ;; esac # RPC hiccup: retry, don't die
        [ "$h" -ge "$target" ] && return 0
        sleep 2
    done
}

wait_state() { # secret_id state timeout_s
    # Default sized for deadline finalisation: under the [min, max] band a
    # secret activates only at its commit deadline (the fixed 50-block commit
    # window), never on the nth acceptance.
    local id=$1 want=$2 timeout=${3:-300} waited=0 got
    while [ $waited -lt $timeout ]; do
        got=$(timeflared query secrets show "$id" -o json 2>/dev/null | jq -r '.secret.state' || true)
        [ "$got" = "$want" ] && return 0
        sleep 3; waited=$((waited+3))
    done
    secret_diagnosis "$id"
    fatal "secret $id never reached state '$want' (last seen: '$got')"
}

# Why a secret did not get where it was going.
#
# "never reached state 'pending' (last seen: 'failed')" names the symptom of
# roughly every acceptance problem in this suite and distinguishes none of them:
# too few guardians selected, enough selected but too few accepting, a daemon
# that died hours ago, a commit window that closed while the fleet was busy. The
# state alone sent one such failure to the wrong diagnosis for two rounds.
#
# The chain already records what is needed — selected_guardians, accepted_count
# and the bounds it was judged against — so print that, and cross it with which
# of those daemons is actually alive. A guardian is selectable while dead: health
# is off-chain, so the chain will happily assign work to a process that no longer
# exists, and that asymmetry is the thing to make visible.
secret_diagnosis() { # secret_id
    local j
    j=$(timeflared query secrets show "$1" -o json 2>/dev/null) || return 0
    [ -n "$j" ] || return 0
    {
        echo ""
        echo "  ── why: secret $1 ──"
        echo "$j" | jq -r '.secret |
            "     state           : \(.state)
     accepted        : \(.accepted_count) (needs \(.min_shares)–\(.max_shares))
     revealed        : \(.revealed_count)
     commit deadline : \(.commit_deadline)
     selected        : \(.selected_guardians | length) guardians"'
        echo "     current height  : $(height)"
        echo "     selected guardians, and whether their daemon is alive:"
        # The address→daemon map is fetched once. guardian_dir_for shells out to
        # `guardians.sh status`, which queries every registered guardian; calling
        # it per selected address turns a diagnostic into a minute of waiting.
        local map addr dir alive
        map=$(guardian_registry_map)
        for addr in $(echo "$j" | jq -r '.secret.selected_guardians[]?'); do
            dir=$(awk -v a="$addr" '$2 == a {print $1}' <<<"$map")
            if [ -z "$dir" ]; then
                alive="not mapped to any local daemon"
            elif guardian_healthy "$dir"; then
                alive="healthy"
            elif guardian_process_alive "$dir"; then
                alive="⚠️  process up but NOT healthy"
            else
                alive="❌ DEAD — selectable on chain, no process"
            fi
            echo "       ${dir:-?} ${addr} — ${alive}"
        done
        fleet_report
    } >&2
}

# Is the daemon's process there at all, health aside?
guardian_process_alive() { # guardian-NN
    if [ "$GUARDIAN_CONTROL" = "docker" ]; then
        [ "$(docker inspect -f '{{.State.Running}}' "timeflare-$1" 2>/dev/null)" = "true" ]
    else
        local pf="$DEVNET_DIR/guardians/$1/guardian.pid"
        [ -f "$pf" ] && kill -0 "$(cat "$pf")" 2>/dev/null
    fi
}

# The fleet as a whole: registered versus actually running.
#
# A run of this suite once ended with the teardown reporting "Stopped 23
# guardians" after starting 24, and nothing between those two lines noticed. One
# daemon had gone, it stayed selectable, and the failure it eventually caused
# surfaced as a protocol-shaped assertion twenty minutes later.
fleet_report() {
    [ "$GUARDIAN_CONTROL" = "native" ] || return 0
    local total=0 up=0 dead="" d name
    for d in "$DEVNET_DIR"/guardians/*/; do
        [ -d "$d" ] || continue
        name=$(basename "$d")
        total=$((total + 1))
        if guardian_process_alive "$name"; then
            up=$((up + 1))
        else
            dead="$dead $name"
        fi
    done
    echo "     fleet: $up/$total daemons running"
    [ -n "$dead" ] && echo "     not running:$dead"
    return 0
}

secret_json() { timeflared query secrets show "$1" -o json; }
guardian_json() { timeflared query secrets show-guardian "$1" -o json; }

# finalize_block_events of a height, filtered by type
block_events() { # height type
    curl -s "$RPC/block_results?height=$1" \
      | jq -c --arg t "$2" '[.result.finalize_block_events[]? | select(.type == $t)
          | [.attributes[] | {(.key): .value}] | add]'
}

# events of a delivered tx, filtered by type
tx_events() { # hash type
    curl -s "$RPC/tx?hash=0x$1" \
      | jq -c --arg t "$2" '[.result.tx_result.events[]? | select(.type == $t)
          | [.attributes[] | {(.key): .value}] | add]'
}

tx_height() { curl -s "$RPC/tx?hash=0x$1" | jq -r '.result.height'; }
tx_code()   { curl -s "$RPC/tx?hash=0x$1" | jq -r '.result.tx_result.code'; }

user_tx() { # subcommand args... — signs as george, returns txhash after inclusion
    local passphrase out submit hash code
    passphrase=$(cat "$USER_PASSPHRASE_FILE")
    # 'gas estimate: N' precedes the JSON on the same stream — parse the last line
    out=$(echo "$passphrase" | timeflared tx secrets "$@" \
        --from george --keyring-backend file --keyring-dir "$USER_KEYRING" \
        --chain-id "$CHAIN_ID" --gas auto --gas-adjustment 1.5 \
        --gas-prices 0.1uveil \
        --node "$RPC" -o json --yes 2>&1) || fatal "tx submission failed: $out"
    submit=$(echo "$out" | tail -1)
    hash=$(echo "$submit" | jq -r '.txhash // empty' 2>/dev/null || true)
    [ -n "$hash" ] || fatal "tx submission produced no hash: $out"
    code=$(echo "$submit" | jq -r '.code')
    [ "$code" = "0" ] || fatal "tx rejected at CheckTx (code $code): $(echo "$submit" | jq -r '.raw_log')"
    # wait for inclusion
    local waited=0
    while [ $waited -lt 30 ]; do
        if [ "$(tx_code "$hash" 2>/dev/null || true)" = "0" ]; then echo "$hash"; return 0; fi
        sleep 2; waited=$((waited+2))
    done
    fatal "tx $hash was not delivered with code 0"
}

user_tx_expect_fail() { # expected_log_substring subcommand args... — asserts DeliverTx failure
    local want=$1; shift
    local passphrase out submit hash code log waited=0
    passphrase=$(cat "$USER_PASSPHRASE_FILE")
    # With --gas auto the message is simulated pre-broadcast, so an invalid
    # cancel typically fails right here — assert the reason either way.
    if ! out=$(echo "$passphrase" | timeflared tx secrets "$@" \
        --from george --keyring-backend file --keyring-dir "$USER_KEYRING" \
        --chain-id "$CHAIN_ID" --gas auto --gas-adjustment 1.5 \
        --gas-prices 0.1uveil \
        --node "$RPC" -o json --yes 2>&1); then
        case "$out" in (*"$want"*) echo "$out"; return 0 ;; esac
        fatal "tx rejected pre-broadcast but with the wrong reason: $out (wanted '$want')"
    fi
    submit=$(echo "$out" | tail -1)
    hash=$(echo "$submit" | jq -r '.txhash // empty' 2>/dev/null || true)
    if [ -z "$hash" ]; then
        case "$out" in (*"$want"*) echo "$out"; return 0 ;; esac
        fatal "tx produced no hash and the wrong reason: $out (wanted '$want')"
    fi
    while [ $waited -lt 30 ]; do
        code=$(tx_code "$hash" 2>/dev/null || true)
        if [ -n "$code" ] && [ "$code" != "null" ]; then
            [ "$code" != "0" ] || fatal "tx $hash unexpectedly succeeded (wanted failure containing '$want')"
            log=$(curl -s "$RPC/tx?hash=0x$hash" | jq -r '.result.tx_result.log')
            case "$log" in (*"$want"*) echo "$log"; return 0 ;; esac
            fatal "tx failed but with the wrong reason: $log (wanted '$want')"
        fi
        sleep 2; waited=$((waited+2))
    done
    fatal "tx $hash was never delivered"
}

# The whole address→daemon mapping in one shot, as "guardian-NN <address>" lines.
# guardian_dir_for resolves one address and pays the full registry lookup to do
# it; anything resolving several should take this once instead.
guardian_registry_map() {
    if [ "$GUARDIAN_CONTROL" = "docker" ]; then
        awk -F: '{print $1, $2}' "$DOCKER_REGISTRY_FILE" 2>/dev/null
    else
        ./devnet/guardians.sh status 2>/dev/null | awk 'NF >= 2 {print $1, $2}'
    fi
}

guardian_dir_for() { # address → guardian-NN
    if [ "$GUARDIAN_CONTROL" = "docker" ]; then
        awk -F: -v a="$1" '$2 == a {print $1}' "$DOCKER_REGISTRY_FILE" 2>/dev/null
    else
        ./devnet/guardians.sh status 2>/dev/null | awk -v a="$1" '$2 == a {print $1}'
    fi
}

guardian_kill() { # guardian-NN
    if [ "$GUARDIAN_CONTROL" = "docker" ]; then
        docker stop "timeflare-$1" >/dev/null 2>&1 || true
    else
        kill "$(cat "$DEVNET_DIR/guardians/$1/guardian.pid")" 2>/dev/null || true
    fi
}

guardians_restart() { # guardian-NN — brings the victim back, both control modes
    local out
    # Output is captured rather than discarded, and a failure is fatal here.
    # Swallowing it left the victim dead and moved the diagnosis two scenarios
    # downstream, where it read as a protocol defect: a dead guardian is still
    # selectable, so the next secret picked it and never reached quorum.
    if [ "$GUARDIAN_CONTROL" = "docker" ]; then
        if ! out="$(docker start "timeflare-$1" 2>&1)"; then
            echo "$out" >&2
            fatal "restarting container timeflare-$1 failed"
        fi
    else
        # No count, so every registered guardian is considered — and
        # `guardians.sh start` skips the ones already running, so in practice this
        # starts the victim and leaves the rest alone. Passing a count instead
        # silently leaves the victim dead whenever its index exceeds it, and a dead
        # guardian stays selectable — with the scenarios' zero-width band, one such
        # selection fails the whole secret.
        if ! out="$(./devnet/guardians.sh start 2>&1)"; then
            echo "$out" >&2
            fatal "restarting the guardian fleet failed"
        fi
    fi
    guardian_wait_healthy "$1"
    assert_fleet_intact
}

# After a restart, every registered daemon should be running — that is what
# `guardians.sh start` with no count means. Checked here because this is the one
# point in the run where "all of them" is unambiguously the expectation: S1
# deliberately runs one guardian short between its kill and this restart, so a
# blanket check elsewhere would fire on correct behaviour.
#
# It is fatal. A fleet quietly one daemon short stays selectable and fails a
# later, unrelated-looking scenario instead.
assert_fleet_intact() {
    [ "$GUARDIAN_CONTROL" = "native" ] || return 0
    local total=0 dead="" d name
    for d in "$DEVNET_DIR"/guardians/*/; do
        [ -d "$d" ] || continue
        name=$(basename "$d")
        total=$((total + 1))
        guardian_process_alive "$name" || dead="$dead $name"
    done
    if [ -n "$dead" ]; then
        info "fleet is short after restart ($total registered):$dead"
        for name in $dead; do
            info "  last lines of $name:"
            tail -5 "$DEVNET_DIR/guardians/$name/guardian.log" 2>/dev/null | sed 's/^/    /' >&2
        done
        fatal "restart left the fleet short:$dead"
    fi
}

# Block until the restarted guardian reports healthy. Starting a daemon is not
# the same as it being ready to accept, and the next scenario creates a secret
# immediately: without this the fleet can still be coming up through the whole
# commit window, and the secret fails for want of acceptances. The commit window
# is a fixed 50 blocks and cannot be stretched to hide restart latency, so the
# wait belongs here.
guardian_wait_healthy() { # guardian-NN
    local waited=0
    while [ "$waited" -lt "$GUARDIAN_HEALTH_TIMEOUT" ]; do
        if guardian_healthy "$1"; then
            return 0
        fi
        sleep 1
        waited=$((waited + 1))
    done
    fatal "$1 never reported healthy within ${GUARDIAN_HEALTH_TIMEOUT}s of restart"
}

# One probe per control mode, because the daemon is reached differently:
#
#   native — the CLI's own health command against the config on disk
#   docker — the container healthcheck compose already defines, read back with
#            docker inspect. Guardian configs live in named volumes there, so
#            there is no host-side path to point the CLI at; the previous
#            config-file probe silently found nothing and waited on a daemon it
#            was never checking.
guardian_healthy() { # guardian-NN
    if [ "$GUARDIAN_CONTROL" = "docker" ]; then
        [ "$(docker inspect -f '{{.State.Health.Status}}' "timeflare-$1" 2>/dev/null)" = "healthy" ]
    else
        guardiand health --config-path "$TIMEFLARE_HOME/guardian/$1/config.yaml" \
            --timeout 3 >/dev/null 2>&1
    fi
}

create_secret() { # manifest offset duration bump [min:max shares]
    node ${SDK_DIR}/examples/scenario-create.js "$1" "$2" "$3" "$4" ${5:+"$5"} >/dev/null
    jq -r '.secretId' "$1"
}

# ── Candidate-pool parking ───────────────────────────────────────────────────
#
# S8 needs a named guardian to be drawn rather than hoping sortition obliges, so
# it constrains what the next reservation can draw from. guardians.sh owns the
# mechanism and the wait; this is the suite's side of it — remembering whether a
# park is outstanding, so the trap can restore unconditionally without the caller
# tracking it.
#
# The flag is on-chain state and outlives this process. A run that dies parked
# leaves every later run against the same chain unable to reserve anything, with
# dev-reset the only cure, which is why the trap is installed here rather than
# alongside the one scenario that parks.
PARKED=0

guardian_park() { # address...
    PARKED=1   # set before the call: a failure part-way still needs restoring
    ./devnet/guardians.sh park "$@" >/dev/null
}

guardian_restore() {
    [ "$PARKED" = "1" ] || return 0
    PARKED=0
    ./devnet/guardians.sh restore >/dev/null
}

trap guardian_restore EXIT
# ─────────────────────────────────────────────────────────────────────────────

mkdir -p "$SCENARIO_DIR"

if ! curl -s --max-time 2 "$RPC/status" >/dev/null 2>&1; then
    echo "❌ Chain is not running — start the devnet first with 'make dev-up'"
    exit 1
fi

# guardiand is a precondition of this suite, not an incidental dependency: S1
# and S8 restart daemons and probe their health through it. Since the split it
# is a synced release artefact in .devnet/bin rather than a globally installed
# binary, so a caller that does not put that directory on PATH gets "command not
# found" from every one of those calls — and an exit code alone cannot tell that
# apart from "the daemon is unhealthy". Assert it up front, and print which
# binary answered: a stale globally installed guardiand from another checkout
# would otherwise be indistinguishable from the pinned one in the log.
if [ "$GUARDIAN_CONTROL" = "native" ]; then
    # BOTH binaries, and their versions printed. S1 and S8 restart daemons and probe
    # health through guardiand; S8 also rotates a key through guardianctl. A missing
    # guardianctl would surface only when S8 runs, minutes in, as a command-not-found
    # inside a scenario rather than as a precondition.
    for bin in guardiand guardianctl; do
        if ! command -v "$bin" >/dev/null 2>&1; then
            echo "❌ $bin is not on PATH."
            echo "   This suite restarts guardian daemons, probes their health, and"
            echo "   rotates a key — the first two through guardiand, the last through"
            echo "   guardianctl."
            echo "   Run it via 'make e2e-scenarios', which puts the pinned binaries"
            echo "   (.devnet/bin, synced by 'make guardiand-sync') on PATH."
            exit 1
        fi
    done
    dv=$(guardiand version 2>/dev/null | awk '/^Version:/{print $2}')
    cv=$(guardianctl version 2>/dev/null | awk '/^Version:/{print $2}')
    info "guardiand: $(command -v guardiand) ($dv)"
    info "guardianctl: $(command -v guardianctl) ($cv)"

    # Says which cadence this run is about to wait on, and how to change it.
    cadence_note "$RPC"
    # They share a config schema, so a mismatched pair is a real hazard rather than
    # untidiness — one writes a file the other cannot read.
    if [ "$dv" != "$cv" ]; then
        echo "❌ guardiand ($dv) and guardianctl ($cv) are from different releases."
        echo "   They share the configuration schema; run 'make guardiand-sync'."
        exit 1
    fi
fi

# community_pool_total sums the community pool in uveil (decimal — DecCoins
# render as "1.2uveil" strings on some CLI versions and objects on others).
community_pool_total() {
    timeflared query distribution community-pool -o json 2>/dev/null \
        | jq -r '(.pool // [])
                 | map((if type == "string" then sub("[a-z]+$"; "") else .amount end) | tonumber)
                 | add // 0'
}
# Captured before any scenario runs; S9 asserts the run's delta stays below
# withdrawal-truncation dust.
POOL_START=$(community_pool_total)

# ─────────────────────────────────────────────────────────────────────────────
info "S1: no-show slash — a healthy daemon is killed after acceptance"
# ─────────────────────────────────────────────────────────────────────────────
M1="$SCENARIO_DIR/s1-manifest.json"
S1=$(create_secret "$M1" 100 100 100)
info "S1 secret: $S1 — waiting for guardian acceptance"
wait_state "$S1" pending

VICTIM=$(secret_json "$S1" | jq -r '.secret.active_assignments[0]')
# Bonds are frozen per-guardian at selection (B_g = rate × distance × bump × k_g,
# aligned with selected_guardians) — read the victim's own amount
BOND=$(secret_json "$S1" | jq -r --arg g "$VICTIM" \
    '.secret | .guardian_bond_amounts[(.selected_guardians | index($g))]')
POOL=$(jq -r '.rewardPool.amount' "$M1")
END=$(jq -r '.revealEndBlock' "$M1")
VICTIM_TOTAL_BEFORE=$(guardian_json "$VICTIM" | jq -r '.guardian.stake.amount')

VDIR=$(guardian_dir_for "$VICTIM")
[ -n "$VDIR" ] || fatal "cannot map victim $VICTIM to a guardian daemon"
guardian_kill "$VDIR"
info "S1 killed $VDIR ($VICTIM) — it will no-show"

wait_height $((END + 2))
SETTLE=$((END + 1))

SLASH=$(block_events "$SETTLE" guardian_slashed | jq -c --arg id "$S1" '[.[] | select(.secret_id == $id)][0]')
assert_eq "$(echo "$SLASH" | jq -r '.guardian_address')" "$VICTIM"                  "S1 slashed guardian is the killed daemon"
assert_eq "$(echo "$SLASH" | jq -r '.slash_type')"       "no_reveal"                "S1 slash type"
# Splits are floored; the remainder (including division dust) is what returns
BURN1=$((BOND * 40 / 100)); CREATOR1=$((BOND * 10 / 100)); RETURNED1=$((BOND - BURN1 - CREATOR1))
assert_eq "$(echo "$SLASH" | jq -r '.burned')"           "$BURN1"      "S1 burn = 40% of bond (floored)"
assert_eq "$(echo "$SLASH" | jq -r '.to_creator')"       "$CREATOR1"   "S1 creator slice = 10% of bond (floored)"
assert_eq "$(echo "$SLASH" | jq -r '.returned')"         "$RETURNED1"  "S1 returned = remainder of bond"

DIST=$(block_events "$SETTLE" secret_rewards_distributed | jq -c --arg id "$S1" '[.[] | select(.secret_id == $id)][0]')
assert_eq "$(echo "$DIST" | jq -r '.total_eligible')"        "4"                    "S1 four survivors split the pool"
assert_eq "$(echo "$DIST" | jq -r '.reward_per_guardian')"   "$((POOL / 4))uveil"   "S1 per-survivor reward = pool/4"

assert_eq "$(secret_json "$S1" | jq -r '.secret.state')" "revealed"                 "S1 terminal state"
assert_eq "$(guardian_json "$VICTIM" | jq -r '.guardian.locked_stake.amount')" "0"  "S1 victim bond released (nothing stranded)"
assert_eq "$(guardian_json "$VICTIM" | jq -r '.guardian.stake.amount')" \
          "$((VICTIM_TOTAL_BEFORE - BURN1 - CREATOR1))"                             "S1 victim float shrank by exactly the slashed portion"

info "S1 restarting the killed daemon"
guardians_restart "$VDIR"

# ─────────────────────────────────────────────────────────────────────────────
info "S2: mid-hold cancellation — pro-rata wages + creator remainder"
# ─────────────────────────────────────────────────────────────────────────────
M2="$SCENARIO_DIR/s2-manifest.json"
S2=$(create_secret "$M2" 150 100 100)
info "S2 secret: $S2 — waiting for guardian acceptance"
wait_state "$S2" pending

DEADLINE=$(jq -r '.commitDeadline' "$M2")
POOL2=$(jq -r '.rewardPool.amount' "$M2")
wait_height $((DEADLINE + 15)) # comfortably mid-hold, well before the window

HASH=$(user_tx user-cancel-secret "$S2")
# the tx carries TWO secret_cancelled events — the FSM's bare state-transition
# event fires first; the economics one is the one carrying guardians_paid
CANCEL=$(tx_events "$HASH" secret_cancelled | jq -c '[.[] | select(.guardians_paid != null)][0]')
ELAPSED=$(( $(tx_height "$HASH") - DEADLINE ))
DISTANCE2=$(( $(jq -r '.revealEndBlock' "$M2") + 1 - DEADLINE ))
# The wage is rate(1) × elapsed × bump(1.00) PLUS the pool's reveal leg
# accruing on the same clock — the pool prices the guardians' reveal
# transaction as well as their time (spec.md "Economic Parameters")
WAGE=$(( (POOL2 * ELAPSED) / (DISTANCE2 * 5) ))

assert_eq "$(echo "$CANCEL" | jq -r '.guardians_paid')"       "5"                          "S2 all five active guardians paid"
assert_eq "$(echo "$CANCEL" | jq -r '.elapsed_blocks')"       "$ELAPSED"                   "S2 elapsed blocks"
assert_eq "$(echo "$CANCEL" | jq -r '.per_guardian_payout')"  "$WAGE"                      "S2 wage = P × elapsed ÷ (distance × max_shares)"
assert_eq "$(echo "$CANCEL" | jq -r '.creator_refund')"       "$((POOL2 - 5 * WAGE))"      "S2 creator refund = P − 5 × wage"
assert_eq "$(secret_json "$S2" | jq -r '.secret.state')"      "cancelled"                  "S2 terminal state"

# Acceptance is reimbursed in full at the terminal state, whenever the creator
# cancelled — the accept fees are escrowed apart from the pool and are earned
# outright by accepting (spec.md "Terminal-state disposition")
ACCEPT_FEES2=$(jq -r '.acceptFees.amount' "$M2")
FEE_EVENTS=$(tx_events "$HASH" guardian_accept_fee_paid)
assert_eq "$(echo "$FEE_EVENTS" | jq -r 'length')"        "5"                          "S2 all five acceptors reimbursed"
assert_eq "$(echo "$FEE_EVENTS" | jq -r '.[0].amount')"   "$((ACCEPT_FEES2 / 5))uveil" "S2 accept fee = A ÷ max_shares"


for g in $(secret_json "$S2" | jq -r '.secret.active_assignments[]'); do
    locked=$(guardian_json "$g" | jq -r '.guardian.locked_stake.amount')
    [ "$locked" = "0" ] || fatal "S2 guardian $g still has $locked locked after cancellation"
done
ok "S2 every bond released"

# ─────────────────────────────────────────────────────────────────────────────
info "S3: early-reveal report — creator-as-reporter with real share evidence"
# ─────────────────────────────────────────────────────────────────────────────
M3="$SCENARIO_DIR/s3-manifest.json"
S3=$(create_secret "$M3" 100 100 100)
info "S3 secret: $S3 — waiting for guardian acceptance"
wait_state "$S3" pending

LEAKER=$(secret_json "$S3" | jq -r '.secret.active_assignments[0]')
EVIDENCE=$(jq -r --arg g "$LEAKER" '.plaintextShares[$g]' "$M3")
BOND3=$(secret_json "$S3" | jq -r --arg g "$LEAKER" \
    '.secret | .guardian_bond_amounts[(.selected_guardians | index($g))]')
POOL3=$(jq -r '.rewardPool.amount' "$M3")
END3=$(jq -r '.revealEndBlock' "$M3")
LEAKER_TOTAL_BEFORE=$(guardian_json "$LEAKER" | jq -r '.guardian.stake.amount')
[ -n "$EVIDENCE" ] && [ "$EVIDENCE" != "null" ] || fatal "S3 no plaintext share for $LEAKER in manifest"

HASH3=$(user_tx slash-guardian "$LEAKER" "$EVIDENCE" "e2e scenario leak" "$S3")
SLASH3=$(tx_events "$HASH3" guardian_slashed | jq -c '.[0]')

assert_eq "$(echo "$SLASH3" | jq -r '.slash_type')"            "early_reveal"                  "S3 slash type"
# Splits are floored; the remainder (including division dust) is the bounty
BURN3=$((BOND3 * 40 / 100)); CREATOR3=$((BOND3 * 10 / 100)); REPORTER3=$((BOND3 - BURN3 - CREATOR3))
assert_eq "$(echo "$SLASH3" | jq -r '.bond_slashed')"          "${BOND3}uveil"  "S3 the FULL bond is slashed"
assert_eq "$(echo "$SLASH3" | jq -r '.burned')"                "$BURN3"         "S3 burn = 40% (floored)"
assert_eq "$(echo "$SLASH3" | jq -r '.reporter_bounty')"       "$REPORTER3"     "S3 reporter bounty = remainder"
assert_eq "$(echo "$SLASH3" | jq -r '.creator_compensation')"  "$CREATOR3"      "S3 creator compensation = 10% (floored)"
assert_eq "$(guardian_json "$LEAKER" | jq -r '.guardian.stake.amount')" \
          "$((LEAKER_TOTAL_BEFORE - BOND3))"                                                   "S3 leaker float lost the whole bond immediately"

info "S3 waiting for settlement (leaker must be excluded from the pool)"
wait_height $((END3 + 2))

DIST3=$(block_events $((END3 + 1)) secret_rewards_distributed | jq -c --arg id "$S3" '[.[] | select(.secret_id == $id)][0]')
assert_eq "$(echo "$DIST3" | jq -r '.total_eligible')"      "4"                     "S3 only the four honest guardians are eligible"
assert_eq "$(echo "$DIST3" | jq -r '.reward_per_guardian')" "$((POOL3 / 4))uveil"   "S3 per-honest-guardian reward = pool/4"
assert_eq "$(secret_json "$S3" | jq -r '.secret.state')"    "revealed"              "S3 terminal state (leaked evidence reconstructs)"
assert_eq "$(guardian_json "$LEAKER" | jq -r '.guardian.locked_stake.amount')" "0"  "S3 nothing stranded on the leaker"

# ─────────────────────────────────────────────────────────────────────────────
info "S4: commit-phase cancel rejected — pre-activation secrets exit via timeout"
# ─────────────────────────────────────────────────────────────────────────────
# Phase 1 only (no distribution), so the secret deterministically stays
# reserved: cancellation must be rejected (pending-only rule, July 2026) and
# the only exit is the commit-timeout. The commit window is the protocol
# constant CommitTimeoutBlocks (50), so the reveal start offset must be ≥ 100.
HASH4=$(user_tx user-request-guardians --random-hint 2 5 5 100 200 100)
S4=$(tx_events "$HASH4" secret_reserved | jq -r '.[0].secret_id')
[ -n "$S4" ] && [ "$S4" != "null" ] || fatal "S4 no secret_id in secret_reserved event"
DEADLINE4=$(( $(tx_height "$HASH4") + 50 ))
info "S4 secret: $S4 (reserved; commit deadline $DEADLINE4)"

user_tx_expect_fail "can only cancel secrets in pending state" user-cancel-secret "$S4" >/dev/null
ok "S4 commit-phase cancel rejected with the pending-only rule"
assert_eq "$(secret_json "$S4" | jq -r '.secret.state')" "reserved"  "S4 rejected cancel left the state untouched"

info "S4 waiting out the commit deadline (the only pre-activation exit)"
wait_height $((DEADLINE4 + 2))
TIMEOUT4=$(block_events $((DEADLINE4 + 1)) secret_commit_timeout | jq -c --arg id "$S4" '[.[] | select(.secret_id == $id)][0]')
[ "$TIMEOUT4" != "null" ] && [ -n "$TIMEOUT4" ] || fatal "S4 no secret_commit_timeout event at deadline + 1"
assert_eq "$(echo "$TIMEOUT4" | jq -r '.phase')"  "1_guardian_selection"  "S4 timeout fired from the reserved (phase-1) state"
assert_eq "$(secret_json "$S4" | jq -r '.secret.state')"  "failed"    "S4 terminal state via commit-timeout (pool refunded in full)"

# ─────────────────────────────────────────────────────────────────────────────
if [ -z "${TIMEFLARE_RETENTION_BLOCKS:-}" ]; then
    info "S5: retention pruning — SKIPPED (chain running with production retention;"
    info "    rerun with TIMEFLARE_RETENTION_BLOCKS=200 on both dev-reset and this suite)"
else
info "S5: retention pruning — terminal secrets tombstoned after $TIMEFLARE_RETENTION_BLOCKS blocks"
# ─────────────────────────────────────────────────────────────────────────────
# S1 (revealed) and S2 (cancelled) went terminal long ago on this run's clock;
# with the reduced window they must already be pruned — or become so shortly.
tombstone_json() { timeflared query secrets secret-tombstone "$1" -o json 2>/dev/null; }

wait_pruned() { # secret_id timeout_s
    local id=$1 timeout=${2:-180} waited=0
    while [ $waited -lt $timeout ]; do
        if tombstone_json "$id" | jq -e '.tombstone' >/dev/null 2>&1; then return 0; fi
        sleep 3; waited=$((waited+3))
    done
    fatal "secret $id was never pruned (no tombstone after ${timeout}s)"
}

wait_pruned "$S2"
T5=$(tombstone_json "$S2" | jq -c '.tombstone')
FINAL_STATE=$(echo "$T5" | jq -r '.finalState // .final_state')
assert_eq "$FINAL_STATE" "cancelled"                                  "S5 tombstone final_state preserves the outcome"
DIGEST_B64=$(echo "$T5" | jq -r '.recordDigest // .record_digest')
DIGEST_HEX=$(echo "$DIGEST_B64" | base64 -d | xxd -p -c 256)
assert_eq "${#DIGEST_HEX}" "64"                                       "S5 tombstone carries a 32-byte record digest"
TERMINAL_AT=$(echo "$T5" | jq -r '(.terminalAt // .terminal_at)|tonumber')
PRUNED_AT=$(echo "$T5" | jq -r '(.prunedAt // .pruned_at)|tonumber')
assert_eq "$PRUNED_AT" "$((TERMINAL_AT + TIMEFLARE_RETENTION_BLOCKS))" "S5 pruned exactly at terminal_at + RetentionBlocks"

# The pruned secret is gone from every live query
if timeflared query secrets show "$S2" -o json >/dev/null 2>&1; then
    fatal "S5 pruned secret $S2 still answers Query/Secret"
fi
ok "S5 pruned secret is NotFound on Query/Secret"
if timeflared query secrets secret-payload "$S2" -o json >/dev/null 2>&1; then
    fatal "S5 pruned secret $S2 still has a payload ciphertext"
fi
ok "S5 payload ciphertext gone (reconstruction inputs expired with the window)"

# The archival event at pruned_at carries the canonical record whose hash IS
# the tombstone digest — the self-authenticating archive property, end to end
PRUNE_EV=$(block_events "$PRUNED_AT" secret_pruned | jq -c --arg id "$S2" '[.[] | select(.secret_id == $id)][0]')
[ "$PRUNE_EV" != "null" ] && [ -n "$PRUNE_EV" ] || fatal "S5 no secret_pruned event at pruned_at"
assert_eq "$(echo "$PRUNE_EV" | jq -r '.record_digest')" "$DIGEST_HEX"  "S5 archival event digest matches the tombstone"
ARCHIVED_HEX=$(echo "$PRUNE_EV" | jq -r '.canonical_record' | base64 -d | shasum -a 256 | cut -d' ' -f1)
assert_eq "$ARCHIVED_HEX" "$DIGEST_HEX"                                 "S5 archived canonical record hashes to the tombstone digest"

# A revealed secret tombstones identically, preserving its own final state
wait_pruned "$S1"
assert_eq "$(tombstone_json "$S1" | jq -r '.tombstone | (.finalState // .final_state)')" "revealed" "S5 revealed secret's tombstone preserves final_state"
fi

# ─────────────────────────────────────────────────────────────────────────────
info "S6: fee burn — the 90/10 split runs at BeginBlock of the next block"
# ─────────────────────────────────────────────────────────────────────────────
# The S2 cancel tx paid a known fee at height H; the split of H's collected
# fees runs at H+1 BeginBlock. The ratio is exact regardless of what else
# shared the block: validator = floor(total × 90 / 100), burn = remainder.
FEE_PAID=$(timeflared query tx "$HASH" -o json 2>/dev/null | jq -r '.tx.auth_info.fee.amount[0].amount')
H6=$(tx_height "$HASH")
SPLIT=$(block_events $((H6 + 1)) fee_distribution | jq -c '.[0]')
[ "$SPLIT" != "null" ] && [ -n "$SPLIT" ] || fatal "S6 no fee_distribution event at BeginBlock of $((H6 + 1))"
VAL6=$(echo "$SPLIT" | jq -r '.validator_fees' | sed 's/uveil//')
BURN6=$(echo "$SPLIT" | jq -r '.burned_fees' | sed 's/uveil//')
TOTAL6=$((VAL6 + BURN6))
assert_eq "$VAL6"  "$((TOTAL6 * 90 / 100))"           "S6 validators receive exactly 90% (floored)"
assert_eq "$BURN6" "$((TOTAL6 - TOTAL6 * 90 / 100))"  "S6 the remainder is burned (dust joins the burn)"
if [ "$TOTAL6" -ge "$FEE_PAID" ]; then
    ok "S6 the split covers at least the S2 cancel fee ($TOTAL6 >= $FEE_PAID)"
else
    fatal "S6 split total $TOTAL6 is smaller than the block's known fee $FEE_PAID"
fi

# ── S6b: routed fees are withdrawable (validator reward routing, July 2026) ──
# The 90% share left in the fee collector must surface as x/distribution
# OUTSTANDING REWARDS (AllocateTokens bookkeeping) — the assertion the
# original suites lacked: a bare send to the distribution account passes
# every event check yet leaves validators unable to withdraw a single uveil.
outstanding_rewards() { # valoper -> integer uveil (floored)
    # DecCoins render as "123.4uveil" strings on some CLI versions and as
    # {denom, amount} objects on others — accept both shapes.
    timeflared query distribution validator-outstanding-rewards "$1" -o json 2>/dev/null \
        | jq -r '(.rewards.rewards // .rewards // [])
                 | map((if type == "string" then sub("[a-z]+$"; "") else .amount end) | tonumber)
                 | add // 0 | floor'
}
VALOPER=$(timeflared query staking validators -o json | jq -r '.validators[0].operator_address')
OUT1=$(outstanding_rewards "$VALOPER")
[ "${OUT1:-0}" -gt 0 ] || fatal "S6b outstanding validator rewards are zero — routed fees are not being allocated"
ok "S6b outstanding validator rewards are positive ($OUT1 uveil)"

# Rewards must GROW when another fee is paid: send a fee-bearing tx and
# re-check after its split lands.
GEORGE_PASS=$(cat "$USER_PASSPHRASE_FILE")
GEORGE_ADDR=$(echo "$GEORGE_PASS" | timeflared keys show george -a --keyring-backend file --keyring-dir "$USER_KEYRING")
SEND_OUT=$(echo "$GEORGE_PASS" | timeflared tx bank send george "$GEORGE_ADDR" 1uveil \
    --from george --keyring-backend file --keyring-dir "$USER_KEYRING" \
    --chain-id "$CHAIN_ID" --gas 200000 --fees 20000uveil \
    --node "$RPC" -o json --yes 2>&1 | tail -1)
SEND_HASH=$(echo "$SEND_OUT" | jq -r '.txhash // empty')
[ -n "$SEND_HASH" ] || fatal "S6b fee-bearing bank send produced no hash: $SEND_OUT"
waited=0
while [ "$(tx_code "$SEND_HASH" 2>/dev/null)" != "0" ] && [ $waited -lt 30 ]; do sleep 1; waited=$((waited+1)); done
SEND_H=$(tx_height "$SEND_HASH")
wait_height $((SEND_H + 2))
OUT2=$(outstanding_rewards "$VALOPER")
if [ "$OUT2" -gt "$OUT1" ]; then
    ok "S6b outstanding rewards grew with the new fee ($OUT1 -> $OUT2)"
else
    fatal "S6b outstanding rewards did not grow after a fee-bearing block ($OUT1 -> $OUT2)"
fi

# Withdraw drill: the validator operator actually collects — rewards leave
# the books and arrive as spendable balance, exactly (drill fee accounted).
GENESIS_PASSPHRASE_FILE="$TIMEFLARE_HOME/genesis_keyring_passphrase"
GENESIS_KEYRING_DIR="$TIMEFLARE_HOME/genesis-keyring"
if [ -f "$GENESIS_PASSPHRASE_FILE" ] && [ -d "$GENESIS_KEYRING_DIR" ]; then
    GEN_PASS=$(cat "$GENESIS_PASSPHRASE_FILE")
    VALADDR=$(echo "$GEN_PASS" | timeflared keys show bootstrapping -a --keyring-backend file --keyring-dir "$GENESIS_KEYRING_DIR")
    BAL_BEFORE=$(timeflared query bank balance "$VALADDR" uveil -o json | jq -r '.balance.amount')
    WD_OUT=$(echo "$GEN_PASS" | timeflared tx distribution withdraw-rewards "$VALOPER" \
        --from bootstrapping --keyring-backend file --keyring-dir "$GENESIS_KEYRING_DIR" \
        --chain-id "$CHAIN_ID" --gas 300000 --fees 30000uveil \
        --node "$RPC" -o json --yes 2>&1 | tail -1)
    WD_HASH=$(echo "$WD_OUT" | jq -r '.txhash // empty')
    [ -n "$WD_HASH" ] || fatal "S6b withdraw-rewards produced no hash: $WD_OUT"
    waited=0
    while [ "$(tx_code "$WD_HASH" 2>/dev/null)" != "0" ] && [ $waited -lt 30 ]; do sleep 1; waited=$((waited+1)); done
    assert_eq "$(tx_code "$WD_HASH")" "0" "S6b withdraw-rewards tx delivered"
    WITHDRAWN=$(tx_events "$WD_HASH" withdraw_rewards | jq -r '[.[].amount | sub("uveil$"; "") | tonumber] | add // 0' | cut -d. -f1)
    [ "${WITHDRAWN:-0}" -gt 0 ] || fatal "S6b withdraw_rewards event carries no positive amount"
    BAL_AFTER=$(timeflared query bank balance "$VALADDR" uveil -o json | jq -r '.balance.amount')
    assert_eq "$((BAL_AFTER - BAL_BEFORE))" "$((WITHDRAWN - 30000))" "S6b withdrawn rewards arrived as spendable balance (net of the drill fee)"
else
    info "S6b withdraw drill SKIPPED (no genesis keyring under $TIMEFLARE_HOME — compose stack without exported chain keys)"
fi

# ─────────────────────────────────────────────────────────────────────────────
if [ -z "${TIMEFLARE_KEY_ROTATION_MIN_INTERVAL:-}" ]; then
    info "S8: key rotation — SKIPPED (chain running with the production ~30-day interval;"
    info "    rerun with TIMEFLARE_KEY_ROTATION_MIN_INTERVAL=30 on both dev-reset and this suite)"
elif [ "$GUARDIAN_CONTROL" != "native" ]; then
    info "S8: key rotation — SKIPPED (guardianctl rotate-key drives native guardian homes;"
    info "    GUARDIAN_CONTROL=$GUARDIAN_CONTROL)"
else
info "S8: key rotation — forward-only mid-life rotation, both epochs served"
# ─────────────────────────────────────────────────────────────────────────────
# Secret A goes in flight FIRST (encrypted to the current epoch); the guardian
# then rotates, restarts, and must still serve A with the retired key while a
# post-rotation secret B is served with the new key.
M8A="$SCENARIO_DIR/s8a-manifest.json"
S8A=$(create_secret "$M8A" 200 100 100)
info "S8 secret A (pre-rotation): $S8A — waiting for guardian acceptance"
wait_state "$S8A" pending

ROTATOR=$(secret_json "$S8A" | jq -r '.secret.active_assignments[0]')
RDIR=$(guardian_dir_for "$ROTATOR")
[ -n "$RDIR" ] || fatal "S8 cannot map rotator $ROTATOR to a guardian daemon"
RHOME="$TIMEFLARE_HOME/guardian/$RDIR"
RCONF="$RHOME/config.yaml"
[ -f "$RCONF" ] || fatal "S8 no guardian config at $RCONF"

OLD_KEY=$(guardian_json "$ROTATOR" | jq -r '.guardian.encryption_public_key // .guardian.encryptionPublicKey')
OLD_EPOCH=$(guardian_json "$ROTATOR" | jq -r '(.guardian.current_key_epoch // .guardian.currentKeyEpoch // 0) | tonumber')

# Rotate: generate → backup ceremony (whole keyring) → submit → retire the
# old key beside the new one. The daemon keeps running through it.
BK_PASS="$SCENARIO_DIR/s8-backup-pass"
echo "s8-rotation-backup-passphrase" > "$BK_PASS"
info "S8 rotating $RDIR ($ROTATOR) — epoch $OLD_EPOCH → $((OLD_EPOCH + 1))"
guardianctl rotate-key --config-path "$RCONF" \
    --backup-output "$SCENARIO_DIR/s8-rotation.tfb" \
    --backup-passphrase-file "$BK_PASS" --yes >/dev/null 2>&1 \
    || fatal "S8 guardianctl rotate-key failed"

[ -s "$SCENARIO_DIR/s8-rotation.tfb" ] || fatal "S8 rotation backup bundle missing/empty"
ok "S8 backup ceremony produced a bundle before submission"
[ -f "$RHOME/private_key.epoch${OLD_EPOCH}" ] || fatal "S8 retired key file private_key.epoch${OLD_EPOCH} missing"
ok "S8 old key retired beside the new one (private_key.epoch${OLD_EPOCH})"

NEW_EPOCH=$(guardian_json "$ROTATOR" | jq -r '(.guardian.current_key_epoch // .guardian.currentKeyEpoch // 0) | tonumber')
NEW_KEY=$(guardian_json "$ROTATOR" | jq -r '.guardian.encryption_public_key // .guardian.encryptionPublicKey')
assert_eq "$NEW_EPOCH" "$((OLD_EPOCH + 1))"     "S8 current_key_epoch advanced"
[ "$NEW_KEY" != "$OLD_KEY" ] || fatal "S8 record still holds the old key after rotation"
ok "S8 record carries the new epoch's key"

HISTORY_LEN=$(timeflared query secrets guardian-key-history "$ROTATOR" -o json | jq '.epochs | length')
assert_eq "$HISTORY_LEN" "$((NEW_EPOCH + 1))"   "S8 key history is contiguous from epoch 0"

# Restart the daemon mid-way (runbook guidance after a rotation) — the epoch
# keyring must reload from disk and BOTH pipelines must resume.
info "S8 restarting $RDIR mid-way"
guardian_kill "$RDIR"
sleep 2
guardians_restart "$RDIR"

# Secret B is created AFTER the rotation: the Phase-1 response hands creators
# the NEW key, so the rotator accepting B proves the new-key pipeline
# (decrypt + HMAC + confirm) end to end. Selection is sortition — retry until
# the rotator is drawn (abandoned attempts exit via commit-timeout refunds).
# B's pool is constrained to A's selected set, so the rotator is drawn with
# certainty. The alternative — redrawing until sortition cooperates — is a
# lottery: five of twenty-four per draw, so six attempts fail about a quarter of
# the time, and the scenario that exercises key rotation cannot be trusted to
# report on key rotation while a quarter of its failures mean nothing.
A_SET=$(secret_json "$S8A" | jq -r '.secret.selected_guardians[]')
info "S8 constraining the candidate pool to A's five for B's reservation"
guardian_park $A_SET || fatal "S8 could not constrain the candidate pool to A's set"
M8B="$SCENARIO_DIR/s8b-manifest.json"
S8B=$(create_secret "$M8B" 100 100 100)
guardian_restore || fatal "S8 could not restore the fleet after B's reservation"
secret_json "$S8B" | jq -e --arg g "$ROTATOR" \
    '.secret.selected_guardians | index($g)' >/dev/null \
    || fatal "S8 B did not draw the rotator despite a pool of exactly A's five"
ok "S8 B drew the rotator from the constrained pool (no redraw)"
info "S8 secret B (post-rotation): $S8B — waiting for guardian acceptance"
wait_state "$S8B" pending

ROTATOR_B_STATUS=$(timeflared query secrets secret-assignments "$S8B" -o json \
    | jq -r --arg g "$ROTATOR" '.assignments[] | select(.guardian_address == $g or .guardianAddress == $g) | .status')
assert_eq "$ROTATOR_B_STATUS" "ASSIGNMENT_STATUS_ACCEPTED" "S8 rotator accepted a NEW-key assignment (decrypt + HMAC passed)"

# Secret B settles first (shorter window): the new-key reveal pipeline.
B_END=$(jq -r '.revealEndBlock' "$M8B")
B_POOL=$(jq -r '.rewardPool.amount' "$M8B")
wait_height $((B_END + 2))
assert_eq "$(secret_json "$S8B" | jq -r '.secret.state')" "revealed" "S8 post-rotation secret settled"
timeflared query secrets secret-reveals "$S8B" -o json \
    | jq -e --arg g "$ROTATOR" '.reveals[] | select((.guardian_address // .guardianAddress) == $g)' >/dev/null \
    || fatal "S8 rotator never revealed on the post-rotation secret"
ok "S8 rotator revealed B with the NEW epoch's key"

# Secret A settles on the RETIRED epoch: the daemon derived the old epoch from
# A's creation height and used the retired key file — fully automatically.
A_END=$(jq -r '.revealEndBlock' "$M8A")
A_POOL=$(jq -r '.rewardPool.amount' "$M8A")
wait_height $((A_END + 2))
assert_eq "$(secret_json "$S8A" | jq -r '.secret.state')" "revealed" "S8 pre-rotation secret settled"
timeflared query secrets secret-reveals "$S8A" -o json \
    | jq -e --arg g "$ROTATOR" '.reveals[] | select((.guardian_address // .guardianAddress) == $g)' >/dev/null \
    || fatal "S8 rotator never revealed on the pre-rotation secret (retired-epoch pipeline broken)"
ok "S8 rotator revealed A with the RETIRED epoch's key"

# Exact settlement amounts on A: all five active guardians revealed, so the
# pool splits five ways with no slashes.
DIST8=$(block_events $((A_END + 1)) secret_rewards_distributed | jq -c --arg id "$S8A" '[.[] | select(.secret_id == $id)][0]')
[ "$DIST8" != "null" ] && [ -n "$DIST8" ] || fatal "S8 no reward distribution event at A's settlement"
ELIGIBLE8=$(echo "$DIST8" | jq -r '.total_eligible')
assert_eq "$(echo "$DIST8" | jq -r '.reward_per_guardian')" "$((A_POOL / ELIGIBLE8))uveil" "S8 per-revealer reward on A = pool/${ELIGIBLE8} exactly"
assert_eq "$(guardian_json "$ROTATOR" | jq -r '.guardian.locked_stake.amount')" "0" "S8 rotator's bonds all released"
fi

# ─────────────────────────────────────────────────────────────────────────────
info "S7: settlement liveness — no settlement stalled anywhere in the run"
# ─────────────────────────────────────────────────────────────────────────────
# settlement_stalled is the fail-safe alarm (spec.md "Settlement failure
# handling"): every failed settlement/commit-expiry attempt emits it from
# EndBlock, so a single hit anywhere in history means a deterministic bug
# corrupted the books. The block indexer lets us sweep the whole chain in one
# query instead of walking block_results height by height.
STALLED_COUNT=$(curl -s "$RPC/block_search?query=%22settlement_stalled.secret_id%20EXISTS%22&per_page=1" \
    | jq -r '.result.total_count // "0"')
assert_eq "$STALLED_COUNT" "0" "S7 no settlement_stalled event in any block"

# ─────────────────────────────────────────────────────────────────────────────
info "S10: recipient rebate — credited at settlement, collected commit–reveal"
# ─────────────────────────────────────────────────────────────────────────────
# A revealed secret credits its recipient a rebate on what the creator
# irrecoverably spent (spec.md "Recipient Rebate"). Collection is commit–reveal:
# the proof is a bearer secret, so it is committed one block before it is
# revealed, which is what stops an observer front-running it out of the mempool.
M10="$SCENARIO_DIR/s10-manifest.json"
# Two shapes, because the dust floor is calibrated for PRODUCTION economics.
# A rebate is 30% of the creator's irrecoverable spend, and at devnet scales
# (1s blocks, minute-long windows) that lands under the 0.05 VEIL floor — so the
# default shape asserts the suppression, which is correct behaviour worth
# pinning, and the drill asserts the collection.
#
#   default:  the suite's usual 5-wide zero-width band, ~4 minutes
#   drill:    band 7:9 at bump 10× over ~1,020 blocks — the cheapest shape whose
#             30% clears the floor for EVERY allowed activation outcome
#             (7 revealers → 52,740 uveil, 9 → 59,940), ~18 minutes
#
# Set REBATE_COLLECTION_DRILL=1 to run the drill. A wide ZERO-WIDTH band is not
# the shortcut it looks like: all its guardians must accept before the deadline,
# and this suite has already killed one daemon (S1) and slashed another (S3).
if [ "${REBATE_COLLECTION_DRILL:-0}" = "1" ]; then
    info "S10 running the collection drill — a ~1,020-block secret, this takes a while"
    S10=$(create_secret "$M10" 150 900 1000 7:9)
else
    S10=$(create_secret "$M10" 100 100 100)
fi
[ -n "$S10" ] || fatal "S10 secret creation failed — see the scenario-create output above"
info "S10 secret: $S10 — waiting for guardian acceptance"
# Height floor for the acceptance audit below: everything a guardian confirmed
# from here on belongs to this secret, because the suite is sequential.
S10_FROM_HEIGHT=$(height)
wait_state "$S10" pending 240

REVEAL_END10=$(jq -r '.revealEndBlock' "$M10")
wait_height $((REVEAL_END10 + 2)) # settlement fires at reveal_end_block + 1
wait_state "$S10" revealed 120

REBATE=$(secret_json "$S10" | jq -r '.secret.rebate_amount // "0"')
POOL10=$(jq -r '.rewardPool.amount' "$M10")
ACCEPT10=$(jq -r '.acceptFees.amount' "$M10")
REVEALERS=$(secret_json "$S10" | jq -r '.secret.revealed_count')
MAXS10=$(jq -r '.maxShares' "$M10")
# S = P + the accept slices actually paid to revealers; the rebate is 30% of it,
# and never more than that whatever the allowance allowed.
SPEND=$(( POOL10 + (ACCEPT10 / MAXS10) * REVEALERS ))
RATIO_CEILING=$(( (SPEND * 30) / 100 ))

if [ "$REBATE" = "0" ]; then
    # A devnet secret is cheap: 30% of its spend can fall under the 0.05 VEIL
    # dust floor, in which case crediting nothing is correct behaviour, not a
    # failure. Assert that reading rather than skipping silently.
    if [ "$RATIO_CEILING" -lt 50000 ]; then
        ok "S10 rebate correctly suppressed as dust (30% of $SPEND uveil = $RATIO_CEILING < 50000)"
    else
        fatal "S10 no rebate credited though 30% of the spend ($RATIO_CEILING uveil) clears the dust floor"
    fi
else
    [ "$REBATE" -le "$RATIO_CEILING" ] \
        || fatal "S10 rebate $REBATE exceeds the 30% ratio ceiling $RATIO_CEILING"
    ok "S10 rebate credited: $REBATE uveil (≤ 30% of $SPEND)"

    REBATE_POOL_ADDR=$(grep -o 'tmflr1[a-z0-9]*' devnet/chain/generate-genesis-keys.sh | head -1)
    POOL_BEFORE=$(timeflared query bank balances "$REBATE_POOL_ADDR" -o json \
        | jq -r '(.balances[]? | select(.denom == "uveil") | .amount) // "0"')

    COLLECTOR=$(echo "$(cat "$USER_PASSPHRASE_FILE")" | timeflared keys show george -a \
        --keyring-backend file --keyring-dir "$USER_KEYRING")
    PROOF_JSON=$(node ${SDK_DIR}/examples/rebate-proof.js "$S10" "$COLLECTOR") \
        || fatal "S10 could not derive the recipiency proof"
    PROOF=$(echo "$PROOF_JSON" | jq -r '.proof')
    COMMITMENT=$(echo "$PROOF_JSON" | jq -r '.commitment')

    # Revealing without a prior commitment must be refused, and refused FOR THE
    # RIGHT REASON: a transaction that fails for an unrelated cause (a wrong
    # keyring path, an unreachable node) would otherwise masquerade as the
    # front-running defence working. So assert the chain's own error text, not
    # merely that nothing was collected. Note the refusal usually surfaces during
    # --gas auto simulation rather than at delivery; either is the handler
    # rejecting it.
    UNCOMMITTED_OUT=$(echo "$(cat "$USER_PASSPHRASE_FILE")" | timeflared tx secrets recipient-collect-rebate \
        "$S10" "$PROOF" --from george --keyring-backend file --keyring-dir "$USER_KEYRING" \
        --chain-id "$CHAIN_ID" --gas auto --gas-adjustment 1.5 --gas-prices 0.1uveil \
        --node "$RPC" -o json --yes 2>&1 || true)
    echo "$UNCOMMITTED_OUT" | grep -q "commit first" \
        || fatal "S10 uncommitted reveal was not refused for the commitment reason: $UNCOMMITTED_OUT"
    sleep 3
    assert_eq "$(secret_json "$S10" | jq -r '.secret.rebate_collected // false')" "false" \
        "S10 uncommitted reveal collected nothing"
    ok "S10 reveal without a prior commitment is refused: commit first"

    COMMIT_HASH=$(user_tx recipient-commit-rebate "$S10" "$COMMITMENT")
    COMMIT_HEIGHT=$(tx_height "$COMMIT_HASH")
    wait_height $((COMMIT_HEIGHT + 1)) # the reveal must land strictly later

    BAL_BEFORE=$(timeflared query bank balances "$COLLECTOR" -o json \
        | jq -r '(.balances[]? | select(.denom == "uveil") | .amount) // "0"')
    COLLECT_HASH=$(user_tx recipient-collect-rebate "$S10" "$PROOF")
    COLLECT_EVENT=$(tx_events "$COLLECT_HASH" rebate_collected | jq -c '.[0]')

    assert_eq "$(echo "$COLLECT_EVENT" | jq -r '.amount')" "${REBATE}uveil" "S10 collected amount matches the credit"
    assert_eq "$(secret_json "$S10" | jq -r '.secret.rebate_collected')" "true" "S10 rebate marked collected"

    POOL_AFTER=$(timeflared query bank balances "$REBATE_POOL_ADDR" -o json \
        | jq -r '(.balances[]? | select(.denom == "uveil") | .amount) // "0"')
    assert_eq "$((POOL_BEFORE - POOL_AFTER))" "$REBATE" "S10 the keyless pool paid exactly the rebate"

    # Collecting twice must fail, and the balance must reflect one payment
    # (net of the collection fee, so assert the pool side for exactness).
    BAL_AFTER=$(timeflared query bank balances "$COLLECTOR" -o json \
        | jq -r '(.balances[]? | select(.denom == "uveil") | .amount) // "0"')
    [ "$BAL_AFTER" -gt "$BAL_BEFORE" ] \
        || fatal "S10 collector balance did not rise ($BAL_BEFORE -> $BAL_AFTER)"
    ok "S10 rebate collected from the keyless pool"
fi

# ─────────────────────────────────────────────────────────────────────────────
info "S10b: one accept per guardian — the daemon does not pay twice for a slot"
# ─────────────────────────────────────────────────────────────────────────────
# Every other assertion in this suite checks the protocol OUTCOME and never what
# the daemon spent reaching it, which is how a duplicate accept on every single
# secret went unnoticed until a guardian's ledger was read by hand: broadcast
# returns at CheckTx, so a poll cycle finishing inside one block time saw the
# assignment as still unhandled and submitted again. The chain rejected the
# second one with code 24 — correctly, and at the guardian's expense.
#
# Reuses S10's settled secret rather than creating another: it has just run a
# full accept/reveal lifecycle across the whole fleet.
S10_TO_HEIGHT=$(height)
CONFIRM_MSG="/timeflare.secrets.v1.MsgGuardianConfirmShares"
# The guardian set comes from selected_guardians, NOT from assignment
# statuses: retention Stage 1 deletes the assignment-status records at the
# terminal transition (keeper/retention.go onSecretTerminal), so a settled
# secret's assignments all render as PROPOSED and an ACCEPTED filter matches
# nothing. On this secret the whole selected set accepted and revealed —
# S10's own lifecycle assertions established that — so the selected set IS
# the accepted set.
S10_GUARDIANS=$(secret_json "$S10" \
    | jq -r '.secret.selected_guardians[]?')
[ -n "$S10_GUARDIANS" ] || fatal "S10b no selected guardians on the settled secret $S10"

# The daemon's own broadcast log is the primary evidence, NOT the tx index.
# A duplicate signed before the first is included reuses the account sequence
# and dies in CheckTx: no block, no index entry, no fee — invisible to
# tx_search while the daemon is still doing the wrong thing, and one timing
# shift away from becoming a charged code-24. Counting what was SENT catches
# both; counting what landed catches only the expensive half.
DUPLICATES=0
CHARGED=0
for GADDR in $S10_GUARDIANS; do
    GDIR=$(guardian_dir_for "$GADDR")
    GLOG="$DEVNET_DIR/guardians/$GDIR/guardian.log"
    if [ "$GUARDIAN_CONTROL" = "docker" ] || [ ! -f "$GLOG" ]; then
        continue # containerised fleets log to the docker driver, not to a file
    fi

    SENT=$(grep -c "Acceptance transaction broadcast.*$S10" "$GLOG" 2>/dev/null || echo 0)
    assert_eq "$SENT" "1" "S10b $GDIR broadcast exactly one accept for $S10"
    [ "$SENT" -le 1 ] || DUPLICATES=$((DUPLICATES + SENT - 1))

    # Second, complementary check: of those that did reach a block, none was
    # rejected. A charged duplicate is the form the defect took on the ledger
    # that first exposed it.
    #
    # Scoped by HEIGHT rather than by secret, and it cannot be otherwise: a
    # rejected message's own events are discarded with its state changes, so the
    # transactions this is hunting carry no secret_id to filter on — only the
    # successful accepts do, which are not the ones in question. The window is
    # S10's lifetime, so a hit inside it belongs to S10 while the suite runs one
    # secret at a time. Anything that overlaps secrets has to reach for the
    # guardian log above instead, which is scoped to the secret by construction.
    QUERY=$(printf "message.action='%s' AND message.sender='%s' AND tx.height>=%s AND tx.height<=%s" \
        "$CONFIRM_MSG" "$GADDR" "$S10_FROM_HEIGHT" "$S10_TO_HEIGHT")
    RESULT=$(curl -s -G "$RPC/tx_search" --data-urlencode "query=\"$QUERY\"" --data-urlencode "per_page=50")
    FAILED=$(echo "$RESULT" | jq -r '[.result.txs[]? | select(.tx_result.code != 0)] | length')
    if [ "$FAILED" != "0" ]; then
        CHARGED=$((CHARGED + FAILED))
        echo "$RESULT" | jq -r '.result.txs[]? | select(.tx_result.code != 0)
            | "    code \(.tx_result.code) at height \(.height): \(.tx_result.log)"' >&2
    fi
done
assert_eq "$DUPLICATES" "0" "S10b no guardian sent a second acceptance for $S10"
assert_eq "$CHARGED" "0" "S10b no acceptance was delivered and rejected — nobody paid for a duplicate"

# ─────────────────────────────────────────────────────────────────────────────
info "S9: no treasury — the community pool gained at most withdrawal dust this run"
# ─────────────────────────────────────────────────────────────────────────────
# community_tax is pinned to 0 in genesis, so allocation skims nothing. The
# pool is not frozen, though: every reward WITHDRAWAL truncates decimal
# rewards to integers and parks the sub-uveil remainder in the pool (SDK
# design, x/distribution withdrawDelegationRewards). This suite performs
# one withdraw drill (S6b), so the pool may grow by strictly less than one
# uveil across the run — while a 2% community-tax skim of this run's fees
# would deposit thousands. Assert the delta, not an absolute (dust accrues
# over the chain's lifetime).
POOL_END=$(community_pool_total)
if awk -v a="$POOL_START" -v b="$POOL_END" 'BEGIN { exit !(b - a < 1) }'; then
    ok "S9 community pool gained only sub-uveil withdrawal dust ($POOL_START -> $POOL_END)"
else
    fatal "S9 community pool grew $POOL_START -> $POOL_END uveil this run — more than withdrawal dust; the community_tax skim is back"
fi

# ─────────────────────────────────────────────────────────────────────────────
info "S11: funded claim kit — a never-funded address is brought onto the chain"
# ─────────────────────────────────────────────────────────────────────────────
# The one claim no other scenario can make. Every other path in this suite
# transacts as `george`, a pre-funded devnet account — which is exactly how the
# bootstrapping defect survived into a release: a wallet that has never RECEIVED
# anything has no auth account, so it cannot sign, so it cannot collect the
# rebate the protocol credited it. A funded claim kit carries a single-use
# courier the recipient sweeps; the sweep is what creates their account.
# See docs/planning/done/DONE_WALLET_BOOTSTRAPPING_PLAN.md §3.
S11_STATE="$SCENARIO_DIR/s11-courier.json"
S11_SETUP=$(COURIER_DRILL_STATE="$S11_STATE" \
    node ${SDK_DIR}/examples/courier-bootstrap.mjs setup 2>&1) \
    || fatal "S11 courier setup failed: $S11_SETUP"
S11_COURIER=$(echo "$S11_SETUP" | jq -r '.courier')
S11_SEED=$(echo "$S11_SETUP" | jq -r '.seed')
[ -n "$S11_COURIER" ] && [ "$S11_COURIER" != "null" ] || fatal "S11 no courier address: $S11_SETUP"

# The recipient must genuinely have nothing, or the scenario proves nothing.
assert_eq "$(echo "$S11_SETUP" | jq -r '.recipientAccountExists')" "false" \
    "S11 fresh recipient has no on-chain account"
assert_eq "$(echo "$S11_SETUP" | jq -r '.kitVersion')" "2" "S11 funded kit is version 2"

info "S11 seeding courier $S11_COURIER with $S11_SEED uveil"
S11_PASS=$(cat "$USER_PASSPHRASE_FILE")
S11_FUND=$(echo "$S11_PASS" | timeflared tx bank send george "$S11_COURIER" "${S11_SEED}uveil" \
    --from george --keyring-backend file --keyring-dir "$USER_KEYRING" \
    --chain-id "$CHAIN_ID" --gas auto --gas-adjustment 1.5 --gas-prices 0.1uveil \
    --node "$RPC" -o json --yes 2>&1 | tail -1)
assert_eq "$(echo "$S11_FUND" | jq -r '.code // "no-code"')" "0" "S11 courier funding accepted"
wait_height $(( $(height) + 2 ))

if COURIER_DRILL_STATE="$S11_STATE" \
   node ${SDK_DIR}/examples/courier-bootstrap.mjs verify; then
    ok "S11 the swept recipient can sign — bootstrapping works end to end"
else
    fatal "S11 a never-funded recipient could NOT be brought onto the chain"
fi

summary
[ $FAIL -eq 0 ]
