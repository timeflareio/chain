#!/bin/bash

# Keyring Utilities
# Functions for managing keyring passphrases with random generation and raw
# file storage: the file content IS the passphrase, verbatim (0600 is the
# control — encoding was only ever obscurity, and the base64 guess it forced
# on readers silently mangled real passphrases; see the key-management
# consolidation plan, guardian sweep finding 30).

# Generate a random passphrase and return it
generate_keyring_passphrase() {
    # 12 random bytes, base64-alphabet text (this is passphrase MATERIAL,
    # not file encoding), newline-free
    openssl rand -base64 12 | tr -d '\n'
}

# Get or create genesis keyring passphrase
# For genesis pool accounts, use separate keyring with own passphrase
# Usage: get_genesis_keyring_passphrase [base_dir]
get_genesis_keyring_passphrase() {
    local base_dir="${1:-$HOME/.timeflare}"
    local passphrase_file="$base_dir/genesis_keyring_passphrase"

    # Create base directory if it doesn't exist
    mkdir -p "$base_dir"

    # If passphrase file exists, return its content verbatim
    if [[ -f "$passphrase_file" ]]; then
        cat "$passphrase_file"
        return 0
    fi

    # Generate new passphrase and store it raw
    local new_passphrase=$(generate_keyring_passphrase)
    printf '%s' "$new_passphrase" > "$passphrase_file"
    chmod 600 "$passphrase_file"

    # Return the new passphrase
    echo -n "$new_passphrase"
}

# Get or create regular keyring passphrase (for guardian keys, etc.)
# Usage: get_keyring_passphrase [keyring_dir]
get_keyring_passphrase() {
    local keyring_dir="${1:-$HOME/.timeflare}"
    local passphrase_file="$keyring_dir/keyring-passphrase.txt"

    # Create keyring directory if it doesn't exist
    mkdir -p "$keyring_dir"

    # If passphrase file exists, return its content verbatim
    if [[ -f "$passphrase_file" ]]; then
        cat "$passphrase_file"
        return 0
    fi

    # Generate new passphrase and store it raw
    local new_passphrase=$(generate_keyring_passphrase)
    printf '%s' "$new_passphrase" > "$passphrase_file"
    chmod 600 "$passphrase_file"

    # Return the new passphrase
    echo -n "$new_passphrase"
}

# Get genesis keyring directory
# Usage: get_genesis_keyring_dir [base_dir]
get_genesis_keyring_dir() {
    local base_dir="${1:-$HOME/.timeflare}"
    echo "$base_dir/genesis-keyring"
}

# Create keyring passphrase file for guardian (used by guardianctl config init)
# Usage: create_guardian_keyring_file <guardian_name> <guardian_home>
create_guardian_keyring_file() {
    local guardian_name="$1"
    local guardian_home="$2"
    local keyring_passphrase=$(get_keyring_passphrase)
    local keyring_file="$guardian_home/keyring_passphrase"

    # Create the raw keyring passphrase file for guardiand
    printf '%s' "$keyring_passphrase" > "$keyring_file"
    chmod 600 "$keyring_file"

    echo "$keyring_passphrase"
}

# Get keyring passphrase from guardian keyring file
# Usage: get_guardian_keyring_passphrase <guardian_home>
get_guardian_keyring_passphrase() {
    local guardian_home="$1"
    local keyring_file="$guardian_home/keyring_passphrase"

    if [[ -f "$keyring_file" ]]; then
        cat "$keyring_file"
    else
        echo ""
        return 1
    fi
}
