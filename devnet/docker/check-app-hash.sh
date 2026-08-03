#!/usr/bin/env bash
# Assert app-hash agreement across every validator in the compose devnet —
# the multi-validator determinism check. Each validator executes every block
# independently; if x/secrets (or anything else in the state machine) were
# non-deterministic, the app hashes would diverge and this exits non-zero.
#
# Usage: check-app-hash.sh <validator_count> [port_offset]
# Validator i's RPC is published at 26657 + 10*i (+ offset).

set -euo pipefail

COUNT="${1:?usage: $0 <validator_count> [port_offset]}"
OFFSET="${2:-0}"

if [[ "$COUNT" -lt 2 ]]; then
    echo "app-hash check skipped (single validator — nothing to compare)"
    exit 0
fi

rpc_port() { echo $((26657 + 10 * $1 + OFFSET)); }

# Compare at a common committed height: the minimum of the validators'
# latest heights (every node has committed it).
min_height=""
for ((i = 0; i < COUNT; i++)); do
    h=$(curl -s --max-time 5 "localhost:$(rpc_port $i)/status" | jq -r '.result.sync_info.latest_block_height')
    [[ "$h" =~ ^[0-9]+$ ]] || { echo "validator-$i RPC unreachable"; exit 1; }
    if [[ -z "$min_height" || "$h" -lt "$min_height" ]]; then min_height="$h"; fi
done

declare -a hashes=()
for ((i = 0; i < COUNT; i++)); do
    hash=$(curl -s --max-time 5 "localhost:$(rpc_port $i)/block?height=$min_height" | jq -r '.result.block.header.app_hash')
    [[ -n "$hash" && "$hash" != "null" ]] || { echo "validator-$i: no block at height $min_height"; exit 1; }
    hashes+=("$hash")
    echo "validator-$i @ height $min_height: app_hash $hash"
done

unique=$(printf '%s\n' "${hashes[@]}" | sort -u | wc -l | tr -d ' ')
if [[ "$unique" -ne 1 ]]; then
    echo "❌ APP HASH DIVERGENCE at height $min_height — state machine non-determinism (treat as a P0 protocol bug)"
    exit 1
fi
echo "✅ app-hash agreement across $COUNT validators at height $min_height"
