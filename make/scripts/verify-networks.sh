#!/usr/bin/env bash
#
# Verify networks.json — the network registry consumers read to derive their
# defaults (docs/guides/NETWORKS.md).
#
# The structural checks keep the file readable by every consumer. The last two
# are the point of it: addressPrefix and the devnet chain id are not new facts,
# they are the ones already stated in app/app.go and the devnet scripts and
# previously hand-copied into other repositories. A registry that can disagree
# with its own origin is worse than no registry, because a consumer reading it
# has no way to tell.

set -euo pipefail

REGISTRY="networks.json"

if ! jq -e . "$REGISTRY" >/dev/null 2>&1; then
  echo "❌ $REGISTRY is not valid JSON"
  exit 1
fi

# Every structural rule in one pass: each produces a line of prose, and the file
# is sound when nothing is produced. Grouped rather than run as separate jq
# invocations so a file with several faults reports all of them at once.
problems=$(jq -r '
  .default as $dflt
  | [.networks[].id] as $ids
  | [
      (if ($ids | index($dflt)) == null then "default \"\($dflt)\" names no network" else empty end),

      (if ($ids | length) != ($ids | unique | length) then "duplicate network ids" else empty end),

      (.networks[]
       | select((.id and .label and .chainId
                 and (.local | type == "boolean")
                 and (.endpoints.rpc | type == "array")
                 and (.endpoints.rest | type == "array")
                 and (.endpoints.grpc | type == "array")) | not)
       | "network \(.id // "<no id>") is missing a field"),

      (.networks[]
       | select(.chainId | test("^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$"; "i") | not)
       | "\(.id): \"\(.chainId)\" is not a well-formed chain id"),

      # blockTime is optional — absent means "measure it", which is what a client
      # with a block clock already does. Present, it must be a duration that
      # timeout_commit accepts.
      (.networks[]
       | select(has("blockTime"))
       | select(.blockTime | type == "string" and test("^[0-9]+(ms|s|m)$") | not)
       | "\(.id): blockTime \"\(.blockTime)\" is not a duration like 6s or 500ms"),

      # Loopback-scoped: cleartext is permitted, but only to a loopback host.
      (.networks[] | select(.local) | .id as $id | .endpoints | (.rpc[], .rest[])
       | select(test("^https?://(localhost|127\\.0\\.0\\.1|\\[::1\\])(:[0-9]+)?(/.*)?$") | not)
       | "\($id): local endpoint \"\(.)\" is not loopback"),

      (.networks[] | select(.local) | .id as $id | .endpoints.grpc[]
       | select(test("^(localhost|127\\.0\\.0\\.1|\\[::1\\]):[0-9]+$") | not)
       | "\($id): local gRPC endpoint \"\(.)\" is not loopback host:port"),

      # Anything off the machine: guardian encryption keys are fetched over REST
      # and shares are sealed to them, so a rewritable response chooses who can
      # read the secret.
      (.networks[] | select(.local | not) | .id as $id | .endpoints | (.rpc[], .rest[])
       | select(startswith("https://") | not)
       | "\($id): remote endpoint \"\(.)\" is not https"),

      (.networks[] | select(.local | not) | .id as $id | .endpoints.grpc[]
       | select(test("^[^:/]+:[0-9]+$") | not)
       | "\($id): gRPC endpoint \"\(.)\" is not host:port")
    ]
  | .[]
' "$REGISTRY")

if [ -n "$problems" ]; then
  echo "❌ $REGISTRY:"
  echo "$problems" | sed 's/^/   /'
  exit 1
fi

# addressPrefix mirrors app/app.go, which remains the definition.
want=$(grep -oE 'AccountAddressPrefix = "[^"]+"' app/app.go | head -1 | cut -d'"' -f2)
got=$(jq -r '.addressPrefix' "$REGISTRY")
if [ -z "$want" ]; then
  echo "❌ could not read AccountAddressPrefix from app/app.go"
  exit 1
fi
if [ "$want" != "$got" ]; then
  echo "❌ $REGISTRY addressPrefix \"$got\" != AccountAddressPrefix \"$want\" in app/app.go"
  exit 1
fi

# The devnet entry describes the devnet these scripts actually start.
want=$(sed -nE 's/.*CHAIN_ID:-([^}]*)\}.*/\1/p' devnet/lib/common-utils.sh | head -1)
got=$(jq -r '.networks[] | select(.id == "devnet") | .chainId' "$REGISTRY")
if [ -z "$want" ]; then
  echo "❌ could not read the default CHAIN_ID from devnet/lib/common-utils.sh"
  exit 1
fi
if [ "$want" != "$got" ]; then
  echo "❌ $REGISTRY devnet chainId \"$got\" != CHAIN_ID \"$want\" in devnet/lib/common-utils.sh"
  exit 1
fi

# No script may carry its own opinion about the cadence. The registry states it and
# the scripts read it; a default in a script is a second answer that cannot be seen
# from the registry — which is the fault this file exists to prevent.
offenders=$(grep -rnE 'TIMEFLARE_BLOCK_TIME:-[0-9]' devnet/ 2>/dev/null || true)
if [ -n "$offenders" ]; then
  echo "❌ a devnet script carries its own block-time default:"
  echo "$offenders" | sed 's/^/   /'
  echo "   The registry states the cadence (networks.json blockTime); read it instead."
  exit 1
fi

# The test cadence is make's TEST_BLOCK_TIME. CI names it too, because a workflow
# cannot expand a make variable — so the two are checked rather than trusted.
want=$(grep -oE '^TEST_BLOCK_TIME \?= *[0-9]+(ms|s|m)' make/common.mk | head -1 | sed -E 's/.*= *//')
if [ -z "$want" ]; then
  echo "❌ could not read TEST_BLOCK_TIME from make/common.mk"
  exit 1
fi
mismatched=$(grep -nE 'TIMEFLARE_BLOCK_TIME: *[0-9]+(ms|s|m)' .github/workflows/*.yml 2>/dev/null \
  | grep -vE "TIMEFLARE_BLOCK_TIME: *${want}\$" || true)
if [ -n "$mismatched" ]; then
  echo "❌ a workflow sets a block time that is not TEST_BLOCK_TIME ($want):"
  echo "$mismatched" | sed 's/^/   /'
  exit 1
fi

echo "✅ Network registry consistent"
