#!/bin/bash
# Secure Genesis Key Generator for Timeflare
# Uses existing genesis keyring structure

set -e

# Import keyring utilities (reuse existing functions)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../lib/keyring-utils.sh"

CHAIN_PREFIX="tmflr"
DEFAULT_DENOM="uveil"
HOME_DIR="${TIMEFLARE_HOME:-$HOME/.timeflare}"

# Configuration files
GENESIS_CONFIG_FILE="$SCRIPT_DIR/genesis-addresses.conf"

echo "🔐 Timeflare Genesis Key Generator"
echo "=================================="
echo ""
echo "⚠️  This script generates private keys for genesis accounts"
echo "⚠️  Using existing genesis keyring structure"
echo ""

# Generate secure keys using existing keyring system
generate_genesis_keys() {
    echo "🏦 Generating genesis pool keys..."
    echo ""

    # Get existing keyring details
    local keyring_passphrase=$(get_genesis_keyring_passphrase "$HOME_DIR")
    local genesis_keyring_dir=$(get_genesis_keyring_dir "$HOME_DIR")

    # Array of accounts to create
    local accounts=("bootstrapping")

    # Initialize address variables
    local bootstrapping_addr=""

    for account in "${accounts[@]}"; do
        echo "🔐 Generating key for: $account"

        # Check if key already exists
        local existing_address
        existing_address=$(echo "$keyring_passphrase" | timeflared keys show "$account" -a \
            --keyring-backend file \
            --keyring-dir "$genesis_keyring_dir" \
            2>/dev/null || echo "")

        local address=""
        if [[ -n "$existing_address" ]]; then
            echo "  ✅ Using existing key: $existing_address"
            address="$existing_address"
        else
            # Create new key using existing keyring function
            create_genesis_key "$account"

            # Get the address
            address=$(echo "$keyring_passphrase" | timeflared keys show "$account" -a \
                --keyring-backend file \
                --keyring-dir "$genesis_keyring_dir" \
                2>/dev/null)

            echo "  ✅ Generated new key: $address"
        fi

        # Store address in appropriate variable
        case "$account" in
            "bootstrapping")
                bootstrapping_addr="$address"
                ;;
        esac
    done

    # Store addresses for config generation. The rebate pool deliberately has
    # no entry here: it is a module account with no key at all.
    BOOTSTRAPPING_ADDRESS="$bootstrapping_addr"
}

# Reuse existing create_genesis_key function from keyring-utils.sh
create_genesis_key() {
    local key_name="$1"

    # Get genesis keyring passphrase
    local keyring_passphrase=$(get_genesis_keyring_passphrase "$HOME_DIR")
    local genesis_keyring_dir=$(get_genesis_keyring_dir "$HOME_DIR")

    # Check if genesis keyring already has keys (not just directory structure)
    local keyring_exists=false
    if [[ -d "$genesis_keyring_dir/keyring-file" ]] && [[ -n "$(ls -A "$genesis_keyring_dir/keyring-file" 2>/dev/null)" ]]; then
        keyring_exists=true
    fi

    # Create temporary password file for timeflared
    local temp_password_file=$(mktemp)
    if [[ "$keyring_exists" == "false" ]]; then
        # For new keyring, need passphrase twice (create + confirm)
        printf '%s\n%s\n' "$keyring_passphrase" "$keyring_passphrase" > "$temp_password_file"
    else
        # For existing keyring, need passphrase once
        printf '%s\n' "$keyring_passphrase" > "$temp_password_file"
    fi

    timeflared keys add "$key_name" \
        --keyring-backend file \
        --keyring-dir "$genesis_keyring_dir" \
        --output json < "$temp_password_file" || {
            echo "ERROR: Failed to create genesis key $key_name" >&2
            rm -f "$temp_password_file"
            return 1
        }
    rm -f "$temp_password_file"

    echo "Created genesis key: $key_name"
}

# Create genesis addresses configuration
create_genesis_config() {
    echo "📝 Creating genesis addresses configuration..."

    cat > "$GENESIS_CONFIG_FILE" << EOF
# Timeflare Genesis Address Configuration
# Generated on: $(date)
# Keys stored in: $HOME_DIR/genesis-keyring/

# Pool Addresses
# The bootstrapping key comes from the genesis keyring. The rebate pool is a
# MODULE account: its address is derived from the module name and there is no
# key for it anywhere — asserted against the binary by
# TestRebatePoolGenesisAddress (app/rebate_pool_test.go).
BOOTSTRAPPING_ADDRESS="$BOOTSTRAPPING_ADDRESS"
REBATE_POOL_ADDRESS="tmflr1g6ct2qh5jtrew322yuumdehgwnk9pcexzzz3d2"

# Amounts (in uveil - 6 decimal places)
# Genesis is two pools (docs/spec.md "Genesis Pool Allocations"): a keyless
# rebate pool holding 70% of supply, spendable only by the recipient-rebate
# formula, and one key-controlled bootstrapping pool for every launch grant —
# validators, guardians and users alike.
REBATE_POOL_AMOUNT="700000000000000"           # 700M VEIL (70%) — keyless module account
BOOTSTRAPPING_AMOUNT="300000000000000"         # 300M VEIL (30%) — controlled key

# Validator Configuration
VALIDATOR_ADDRESS="\$BOOTSTRAPPING_ADDRESS"         # gentx is funded from bootstrapping
VALIDATOR_STAKE_AMOUNT="10000000000"                # 10K VEIL self-delegation

# Keyring Configuration (existing structure)
HOME_DIR="$HOME_DIR"
GENESIS_KEYRING_DIR="$HOME_DIR/genesis-keyring"
GENESIS_PASSPHRASE_FILE="$HOME_DIR/genesis_keyring_passphrase"
EOF

    chmod 644 "$GENESIS_CONFIG_FILE"
    echo "✅ Configuration saved to: $GENESIS_CONFIG_FILE"
}

# Show key access information
show_key_access_info() {
    echo ""
    echo "🔐 Key Access Information:"
    echo "  • Keyring Directory: $HOME_DIR/genesis-keyring/"
    echo "  • Passphrase File: $HOME_DIR/genesis_keyring_passphrase"
    echo ""
    echo "📋 To access keys:"
    echo "  PASSPHRASE=\$(cat $HOME_DIR/genesis_keyring_passphrase)"
    echo "  echo \"\$PASSPHRASE\" | timeflared keys list --keyring-backend file --keyring-dir $HOME_DIR/genesis-keyring/"
    echo ""
    echo "🔍 To view specific address:"
    echo "  echo \"\$PASSPHRASE\" | timeflared keys show [key-name] -a --keyring-backend file --keyring-dir $HOME_DIR/genesis-keyring/"
}

# Main execution
main() {
    echo "Starting secure key generation using existing keyring structure..."
    echo ""

    # Ensure keyring directory exists
    mkdir -p "$HOME_DIR"

    # Generate keys
    generate_genesis_keys
    echo ""

    # Create configuration
    create_genesis_config
    echo ""

    echo "🎉 Genesis key generation complete!"
    echo ""
    echo "📋 Summary:"
    echo "  • Bootstrapping (keyed): $BOOTSTRAPPING_ADDRESS"
    echo "  • Rebate pool (keyless): tmflr1g6ct2qh5jtrew322yuumdehgwnk9pcexzzz3d2"
    echo ""
    echo "🔐 Security:"
    echo "  • Keys stored in existing genesis keyring: $HOME_DIR/genesis-keyring/"
    echo "  • Passphrase: $HOME_DIR/genesis_keyring_passphrase"
    echo "  • Configuration: $GENESIS_CONFIG_FILE"

    show_key_access_info

    echo ""
    echo "🚀 Next steps:"
    echo "  • Run 'make setup' to use these addresses in genesis"
    echo "  • The setup script will automatically use the generated configuration"
    echo ""
    echo "⚠️  IMPORTANT:"
    echo "  • Backup $HOME_DIR/genesis-keyring/ and genesis_keyring_passphrase securely"
    echo "  • Never commit genesis-addresses.conf to public repos"
}

# Run if executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
