#!/usr/bin/env bash

set -euo pipefail

BUF_YAML="buf.yaml"
DEST_DIR="third_party/proto"

if [ ! -f "$BUF_YAML" ]; then
  echo "❌ No buf.yaml found in current directory"
  exit 1
fi

mkdir -p "$DEST_DIR"

echo "🔍 Parsing dependencies from $BUF_YAML (no validation)..."

deps=()
in_deps=false
while IFS= read -r line; do
  if [[ "$line" =~ ^[[:space:]]*deps:[[:space:]]*$ ]]; then
    in_deps=true
    continue
  fi

  if $in_deps && [[ "$line" =~ ^[[:alnum:]] ]]; then
    break
  fi

  if $in_deps && [[ "$line" =~ ^[[:space:]]*-[[:space:]]*(.+)$ ]]; then
    dep="${BASH_REMATCH[1]}"
    deps+=("$dep")
  fi
done < "$BUF_YAML"

if [ ${#deps[@]} -eq 0 ]; then
  echo "⚠️  No dependencies found in 'deps:' section"
  exit 0
fi

for dep in "${deps[@]}"; do
  echo "📦 Exporting $dep into $DEST_DIR..."
  buf export "$dep" --output "$DEST_DIR"
done

echo "✅ Finished exporting all dependencies to $DEST_DIR"
