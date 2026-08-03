#!/bin/bash
# Timeflare Test Chain Setup Script
# Automated setup with fixed supply VEIL economics and zero inflation model

set -e

# Import keyring utilities
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../lib/keyring-utils.sh"

# Configuration variables (can be overridden with environment variables)
CHAIN_ID="${TIMEFLARE_CHAIN_ID:-timeflare-test}"
DEFAULT_DENOM="${TIMEFLARE_DENOM:-uveil}"
MIN_GAS_PRICE="${TIMEFLARE_MIN_GAS_PRICE:-0.1uveil}"
BLOCK_TIME="${TIMEFLARE_BLOCK_TIME:-6s}"
HOME_DIR="${TIMEFLARE_HOME:-$HOME/.timeflare}"

echo "🚀 Setting up Timeflare test chain..."

# Check if chain is already initialized
if [[ -f "$HOME_DIR/config/genesis.json" ]]; then
    echo "❌ Test chain already exists at: $HOME_DIR"
    echo ""
    echo "🔧 To reinitialize:"
    echo "   rm -rf ~/.timeflare"
    echo "   then try again"
    echo ""
    exit 1
fi

# Initialize chain with VEIL as default denomination
echo "⚙️ Initializing chain..."
timeflared init testchain \
  --chain-id "$CHAIN_ID" \
  --default-denom "$DEFAULT_DENOM" \
  --home "$HOME_DIR" \
  --overwrite

# Load and validate genesis configuration
echo "🔍 Validating genesis configuration..."

# Load genesis addresses from configuration instead of generating keys
load_genesis_addresses() {
    local config_file="$SCRIPT_DIR/genesis-addresses.conf"

    if [[ ! -f "$config_file" ]]; then
        echo "❌ Genesis addresses configuration not found: $config_file"
        echo ""
        echo "🔧 To generate genesis addresses, run:"
        echo "   $SCRIPT_DIR/generate-genesis-keys.sh"
        echo ""
        echo "This will create secure keys in the existing genesis keyring and"
        echo "generate the addresses configuration file."
        exit 1
    fi

    echo "📋 Loading genesis addresses from: $config_file"
    source "$config_file"

    # Validate addresses exist and are valid
    for addr_var in BOOTSTRAPPING_ADDRESS REBATE_POOL_ADDRESS; do
        if [[ -z "${!addr_var}" ]] || [[ ! "${!addr_var}" =~ ^tmflr1[a-z0-9]{38}$ ]]; then
            echo "❌ Invalid or missing address: $addr_var = ${!addr_var}"
            exit 1
        fi
    done

    echo "✅ All genesis addresses loaded and validated"
}

# Check if genesis keys exist
echo "🔐 Checking for genesis keys..."
GENESIS_KEYRING_PASSPHRASE=$(get_genesis_keyring_passphrase "$HOME_DIR")
GENESIS_KEYRING_DIR=$(get_genesis_keyring_dir "$HOME_DIR")

if ! echo "$GENESIS_KEYRING_PASSPHRASE" | timeflared keys show "bootstrapping" -a \
    --keyring-backend file \
    --keyring-dir "$GENESIS_KEYRING_DIR" >/dev/null 2>&1; then
    echo "❌ No genesis keys found!"
    echo ""
    echo "🔧 To generate genesis keys and setup chain:"
    echo "   make init-test-chain"
    echo ""
    echo "💡 Or use 'make reset' to clean and initialize everything"
    exit 1
fi

echo "📋 Loading genesis addresses from configuration..."
load_genesis_addresses

# Function to get genesis key address using file backend with genesis keyring passphrase
get_genesis_key_address() {
    local key_name="$1"
    local keyring_passphrase=$(get_genesis_keyring_passphrase "$HOME_DIR")
    local genesis_keyring_dir=$(get_genesis_keyring_dir "$HOME_DIR")

    echo "$keyring_passphrase" | timeflared keys show "$key_name" -a --keyring-backend file --keyring-dir "$genesis_keyring_dir" 2>/dev/null
}

# Add genesis pool allocations (1B VEIL fixed total supply, two pools)
echo "🏦 Allocating genesis pools..."
echo "  • Rebate pool (KEYLESS): $REBATE_POOL_ADDRESS ($(echo "scale=0; $REBATE_POOL_AMOUNT / 1000000" | bc) VEIL)"
timeflared genesis add-genesis-account \
  "$REBATE_POOL_ADDRESS" \
  "${REBATE_POOL_AMOUNT}$DEFAULT_DENOM" \
  --home "$HOME_DIR"

echo "  • Bootstrapping: $BOOTSTRAPPING_ADDRESS ($(echo "scale=0; $BOOTSTRAPPING_AMOUNT / 1000000" | bc) VEIL)"
timeflared genesis add-genesis-account \
  "$BOOTSTRAPPING_ADDRESS" \
  "${BOOTSTRAPPING_AMOUNT}$DEFAULT_DENOM" \
  --home "$HOME_DIR"

# Generate initial validator genesis transaction
echo "🏛️ Creating genesis transaction..."
# Use genesis keyring passphrase
GENESIS_KEYRING_PASSPHRASE=$(get_genesis_keyring_passphrase "$HOME_DIR")
GENESIS_KEYRING_DIR=$(get_genesis_keyring_dir "$HOME_DIR")

echo "$GENESIS_KEYRING_PASSPHRASE" | timeflared genesis gentx bootstrapping "10000000000$DEFAULT_DENOM" \
  --chain-id "$CHAIN_ID" \
  --keyring-backend file \
  --keyring-dir "$GENESIS_KEYRING_DIR" \
  --commission-rate 0.05 \
  --commission-max-rate 0.20 \
  --commission-max-change-rate 0.01 \
  --min-self-delegation 1

# Collect genesis transactions
echo "📝 Collecting genesis transactions..."
timeflared genesis collect-gentxs --home "$HOME_DIR"

# Configure genesis economics (zero inflation + dev gov periods) — shared
# with the compose devnet so the knobs live in exactly one place
echo "⚙️ Configuring zero-inflation economics..."
GENESIS_FILE="$HOME_DIR/config/genesis.json"
GOV_VOTING_PERIOD="${TIMEFLARE_GOV_VOTING_PERIOD:-60s}"
"$SCRIPT_DIR/apply-genesis-economics.sh" "$GENESIS_FILE" "$GOV_VOTING_PERIOD"

echo "✅ Mint module configured for fixed supply economics"
echo "  • Total supply: 1,000,000,000 VEIL (fixed)"
echo "  • Inflation: ZERO (disabled permanently)"
echo "  • Economics: Fee-driven rewards with deflationary burning"
echo "  • Dev governance voting period: $GOV_VOTING_PERIOD"

# Ensure all module parameters are properly initialized
echo "🔧 Initializing module parameters..."
# This helps prevent genesis export panics for IBC modules
timeflared genesis validate-genesis --home "$HOME_DIR" > /dev/null || {
    echo "⚠️  Genesis validation failed, but continuing..."
}

# Configure application settings automatically
echo "⚙️ Configuring application settings..."
CONFIG_DIR="$HOME_DIR/config"

# Function to safely update config files
update_config() {
  local file="$1"
  local pattern="$2"
  local replacement="$3"

  if [[ -f "$file" ]]; then
    sed -i.bak "s|$pattern|$replacement|g" "$file"
  fi
}

# Configure app.toml - Essential settings to prevent errors
update_config "$CONFIG_DIR/app.toml" 'minimum-gas-prices = ""' "minimum-gas-prices = \"$MIN_GAS_PRICE\""
update_config "$CONFIG_DIR/app.toml" 'enable = false' 'enable = true'
update_config "$CONFIG_DIR/app.toml" 'address = "tcp://localhost:1317"' 'address = "tcp://0.0.0.0:1317"'
update_config "$CONFIG_DIR/app.toml" 'enabled-unsafe-cors = false' 'enabled-unsafe-cors = true'

# Configure config.toml - Optimized for testing
update_config "$CONFIG_DIR/config.toml" 'laddr = "tcp://127.0.0.1:26657"' 'laddr = "tcp://0.0.0.0:26657"'
update_config "$CONFIG_DIR/config.toml" 'cors_allowed_origins = \[\]' 'cors_allowed_origins = ["*"]'
update_config "$CONFIG_DIR/config.toml" 'timeout_commit = "5s"' "timeout_commit = \"$BLOCK_TIME\""
update_config "$CONFIG_DIR/config.toml" 'size = 5000' 'size = 1000'

# Validate genesis file
echo "✅ Validating genesis configuration..."
timeflared genesis validate-genesis --home "$HOME_DIR"

echo ""
echo "🎉 Chain initialization complete!"
echo ""
echo "📊 Configuration Summary:"
echo "  • Chain ID: $CHAIN_ID"
echo "  • Default denomination: $DEFAULT_DENOM"
echo "  • Minimum gas price: $MIN_GAS_PRICE (prevents gas price errors)"
echo "  • Block time: $BLOCK_TIME (fast testing)"
echo "  • API server: enabled on :1317"
echo "  • RPC server: enabled on :26657"
echo ""
echo "🚀 Start the chain with:"
if [[ "$HOME_DIR" != "$HOME/.timeflare" ]]; then
  echo "  timeflared start --home \"$HOME_DIR\""
else
  echo "  timeflared start"
fi
echo ""
echo "📋 Genesis Account Summary:"
echo "  • Rebate pool: $(echo "scale=0; $REBATE_POOL_AMOUNT / 1000000" | bc) VEIL — keyless ($REBATE_POOL_ADDRESS)"
echo "  • Bootstrapping: $(echo "scale=0; $BOOTSTRAPPING_AMOUNT / 1000000" | bc) VEIL ($BOOTSTRAPPING_ADDRESS)"
echo "  • Total Supply: 1,000,000,000 VEIL (fixed forever, ZERO inflation)"
echo ""
echo "🔐 Key Management:"
echo "  • Keyring Backend: file (secure storage)"
echo "  • Genesis Keyring: $HOME_DIR/genesis-keyring/ (existing structure)"
echo "  • Genesis Passphrase: $HOME_DIR/genesis_keyring_passphrase"
echo "  • Addresses Config: $SCRIPT_DIR/genesis-addresses.conf"
echo "  • Keys accessible via keyring-utils.sh functions"
echo ""
echo "💰 Economic Model:"
echo "  • Fee Distribution: 90% to validators, 10% burned (deflationary)"
echo "  • Guardian Rewards: Direct payment from secret creators only"
echo "  • No Treasury Rewards: Fully decentralised, market-driven"
echo "  • Sustainable Deflation: Permanent token burning via fees"
echo ""
echo "🔧 Customize with environment variables:"
echo "  export TIMEFLARE_MIN_GAS_PRICE=\"0.2uveil\"   # node mempool knob — the consensus floor (0.1uveil/gas) is the hard minimum"
echo "  export TIMEFLARE_BLOCK_TIME=\"2s\""
echo "  export TIMEFLARE_HOME=\"/custom/path\""
echo ""
echo "📖 For comprehensive testing, see TESTING_COMMANDS.md"
