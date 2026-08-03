#!/bin/bash

# Simple user setup for Timeflare testing
# Creates and funds the 'george' user account

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../lib/common-utils.sh"

USER_NAME="george"
CHAIN_ID="${CHAIN_ID:-timeflare-test}"
USER_FUNDING="25000000000"  # 25,000 VEIL
# TIMEFLARE_HOME redirects every path (user keyring + genesis keyring) so the
# same script drives the native devnet (~/.timeflare) and the compose devnet
# (.devnet/docker/home, where chain-init exported the genesis keyring)
TIMEFLARE_HOME="${TIMEFLARE_HOME:-$HOME/.timeflare}"
USER_HOME="$TIMEFLARE_HOME/user"
USER_KEYRING_DIR="$USER_HOME/keyring"
USER_KEYRING_PASSPHRASE_FILE="$USER_HOME/keyring_passphrase"
GENESIS_KEYRING_DIR="$TIMEFLARE_HOME/genesis-keyring"
GENESIS_PASSPHRASE_FILE="$TIMEFLARE_HOME/genesis_keyring_passphrase"

echo "🔧 Timeflare User Setup"
echo "======================"

case "${1:-help}" in
    "fund")
        echo "💰 Creating and funding user: $USER_NAME"
        
        # Create isolated directories
        mkdir -p "$USER_HOME"
        mkdir -p "$USER_KEYRING_DIR"
        
        # Generate or load keyring passphrase
        if [[ ! -f "$USER_KEYRING_PASSPHRASE_FILE" ]]; then
            echo "🔑 Generating keyring passphrase..."
            unique_passphrase=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-25)
            printf '%s' "$unique_passphrase" > "$USER_KEYRING_PASSPHRASE_FILE"
            chmod 600 "$USER_KEYRING_PASSPHRASE_FILE"
        fi
        
        # Get the user's keyring passphrase
        USER_KEYRING_PASSPHRASE=$(cat "$USER_KEYRING_PASSPHRASE_FILE")
        
        # Check if user exists and is already funded
        if echo "$USER_KEYRING_PASSPHRASE" | timeflared keys show "$USER_NAME" --keyring-backend file --keyring-dir "$USER_KEYRING_DIR" >/dev/null 2>&1; then
            USER_ADDRESS=$(echo "$USER_KEYRING_PASSPHRASE" | timeflared keys show "$USER_NAME" -a --keyring-backend file --keyring-dir "$USER_KEYRING_DIR")
            USER_BALANCE=$(timeflared query bank balances "$USER_ADDRESS" --output json 2>/dev/null | jq -r '.balances[0].amount // "0"')
            
            if [ "$USER_BALANCE" -ge 20000000000 ]; then
                echo "✅ User $USER_NAME already exists and is funded"
                echo "   Address: $USER_ADDRESS"
                echo "   Balance: $((USER_BALANCE / 1000000)) VEIL"
                exit 0
            fi
        fi
        
        # Create user if doesn't exist
        if ! echo "$USER_KEYRING_PASSPHRASE" | timeflared keys show "$USER_NAME" --keyring-backend file --keyring-dir "$USER_KEYRING_DIR" >/dev/null 2>&1; then
            echo "🔑 Creating user account: $USER_NAME"
            
            # Create temporary password file for non-interactive key creation
            temp_password_file=$(mktemp)
            echo "$USER_KEYRING_PASSPHRASE" > "$temp_password_file"
            echo "$USER_KEYRING_PASSPHRASE" >> "$temp_password_file"  # Confirmation
            
            if ! timeflared keys add "$USER_NAME" \
                --keyring-backend file \
                --keyring-dir "$USER_KEYRING_DIR" \
                --output json < "$temp_password_file" >/dev/null 2>&1; then
                
                rm -f "$temp_password_file"
                echo "❌ Failed to create user account"
                exit 1
            fi
            rm -f "$temp_password_file"
        fi
        
        USER_ADDRESS=$(echo "$USER_KEYRING_PASSPHRASE" | timeflared keys show "$USER_NAME" -a --keyring-backend file --keyring-dir "$USER_KEYRING_DIR" )
        
        # Fund from the bootstrapping pool
        echo "💸 Funding $USER_NAME from the bootstrapping pool..."
        
        # Check if user already has sufficient balance
        USER_BALANCE=$(timeflared query bank balances "$USER_ADDRESS" --output json 2>/dev/null | jq -r '.balances[0].amount // "0"')
        if [ "$USER_BALANCE" -ge "$USER_FUNDING" ]; then
            echo "✅ User already has sufficient balance ($((USER_BALANCE / 1000000)) VEIL), skipping funding"
        else
            # Use the same funding pattern as guardians with the raw passphrase file
            if cat "$GENESIS_PASSPHRASE_FILE" | timeflared tx bank send bootstrapping "$USER_ADDRESS" "${USER_FUNDING}uveil" \
                --chain-id "$CHAIN_ID" \
                --keyring-backend file \
                --keyring-dir "$GENESIS_KEYRING_DIR" \
                --fees 20000uveil \
                --yes >/dev/null 2>&1; then
                echo "✅ Funding transaction submitted successfully"
            else
                echo "❌ Failed to fund user account - check that blockchain is running and bootstrapping has funds"
                exit 1
            fi
        fi
        
        sleep 3
        
        # Verify funding
        USER_BALANCE=$(timeflared query bank balances "$USER_ADDRESS" --output json 2>/dev/null | jq -r '.balances[0].amount // "0"')
        echo "✅ User funded successfully!"
        echo "   Address: $USER_ADDRESS"
        echo "   Balance: $((USER_BALANCE / 1000000)) VEIL"
        ;;

    "status")
        echo "📊 Checking user status: $USER_NAME"
        
        # Check if keyring passphrase file exists
        if [[ -f "$USER_KEYRING_PASSPHRASE_FILE" ]]; then
            USER_KEYRING_PASSPHRASE=$(cat "$USER_KEYRING_PASSPHRASE_FILE")
            
            if echo "$USER_KEYRING_PASSPHRASE" | timeflared keys show "$USER_NAME" --keyring-backend file --keyring-dir "$USER_KEYRING_DIR"  >/dev/null 2>&1; then
                USER_ADDRESS=$(echo "$USER_KEYRING_PASSPHRASE" | timeflared keys show "$USER_NAME" -a --keyring-backend file --keyring-dir "$USER_KEYRING_DIR" )
                USER_BALANCE=$(timeflared query bank balances "$USER_ADDRESS" --output json 2>/dev/null | jq -r '.balances[0].amount // "0"')
                
                echo "   User: $USER_NAME"
                echo "   Address: $USER_ADDRESS"
                echo "   Balance: $((USER_BALANCE / 1000000)) VEIL"
                echo "   Keyring: $USER_KEYRING_DIR"
                
                if [ "$USER_BALANCE" -ge 20000000000 ]; then
                    echo "   Status: ✅ Ready for testing"
                else
                    echo "   Status: ⚠️  Low balance"
                fi
            else
                echo "   Status: ❌ User key not found in isolated keyring"
                echo "   Run: $0 fund"
            fi
        else
            echo "   Status: ❌ User not initialized"
            echo "   Run: $0 fund"
        fi
        ;;
        
    "cleanup")
        echo "🧹 Cleaning up user: $USER_NAME"
        
        # Remove the entire user directory (including keyring and passphrase)
        if [[ -d "$USER_HOME" ]]; then
            echo "🗑️  Removing user data directory..."
            rm -rf "$USER_HOME"
            echo "✅ User data removed: $USER_NAME"
        else
            echo "ℹ️  User data directory not found"
        fi
        ;;
        
    *)
        echo "Usage: $0 [fund|status|cleanup]"
        echo ""
        echo "Commands:"
        echo "  fund    - Create and fund the george user"
        echo "  status  - Show user status and balance"
        echo "  cleanup - Remove user key from keyring"
        echo ""
        echo "Examples:"
        echo "  $0 fund     # Create user"
        echo "  $0 status   # Check status"
        echo "  $0 cleanup  # Remove user"
        ;;
esac