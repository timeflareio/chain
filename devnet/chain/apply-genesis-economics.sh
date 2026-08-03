#!/usr/bin/env bash
# Apply Timeflare's genesis economics to a genesis.json — shared by the
# native devnet (setup-chain.sh) and the compose devnet (docker/init-chain.sh)
# so the knobs live in exactly one place.
#
# Usage: apply-genesis-economics.sh <genesis.json> [gov_voting_period]

set -euo pipefail

GENESIS_FILE="${1:?usage: $0 <genesis.json> [gov_voting_period]}"
GOV_VOTING_PERIOD="${2:-60s}"

apply() { # [jq options...] <filter>
    jq "$@" "$GENESIS_FILE" > "$GENESIS_FILE.tmp" && mv "$GENESIS_FILE.tmp" "$GENESIS_FILE"
}

# Block gas limit: the per-block execution bound (spec.md "Network
# Configuration"). Without it CometBFT defaults to -1 (unlimited). 75M is
# ~36x the largest legitimate transaction (32-guardian cancel, ~2.07M gas);
# a consensus parameter, not an immutable economic constant.
apply '.consensus.params.block.max_gas = "75000000"'

# Fixed 1B VEIL supply: zero inflation, forever — rewards come purely from fees
apply '.app_state.mint.params.inflation_rate_change = "0.000000000000000000"'
apply '.app_state.mint.params.inflation_max = "0.000000000000000000"'
apply '.app_state.mint.params.inflation_min = "0.000000000000000000"'
apply '.app_state.mint.minter.inflation = "0.000000000000000000"'
apply '.app_state.mint.minter.annual_provisions = "0.000000000000000000"'

# No treasury: zero the SDK's inherited 2% community tax so fee allocation
# never skims into the community pool (ruled July 2026 —
# docs/planning/done/DONE_VALIDATOR_REWARD_ROUTING_PLAN.md)
apply '.app_state.distribution.params.community_tax = "0.000000000000000000"'

# Short dev governance periods so upgrades can be rehearsed locally
# (production uses the default 48h)
apply --arg period "$GOV_VOTING_PERIOD" '.app_state.gov.params.voting_period = $period'
apply '.app_state.gov.params.expedited_voting_period = "30s"'
