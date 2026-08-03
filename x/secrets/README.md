# Secrets Module

The Secrets module handles the publication, management, and reconstruction of time-locked secrets.

## Overview

This module enables users to publish secrets that can only be revealed after a specific block height, using a network of guardian nodes to ensure decentralised and secure secret management. The system provides strong guarantees against early revelation through HMAC-based slashing protection.

## Key Features

- **Time-Locked Secrets**: Secrets can only be revealed after a specified future block height
- **Threshold Cryptography**: Configurable threshold (e.g., 3-of-5) ensures no single point of failure
- **Guardian Integration**: Works seamlessly with the guardians module for decentralised operation
- **Early Reveal Protection**: HMAC verification prevents guardians from revealing shares early
- **Recipient Encryption**: Secrets are encrypted for specific recipients using their public keys
- **Flexible Reconstruction**: Anyone can submit valid shares to reconstruct a secret once the reveal block is reached

## Architecture

### Core Components

1. **Secret Management**: Core data structures and lifecycle management
2. **Guardian Integration**: Interfaces with the guardians module for share distribution
3. **Cryptographic Operations**: HMAC verification and share validation
4. **Query/Transaction Services**: gRPC endpoints for secret operations

### State Storage

- `Secrets`: Map of secret ID to secret metadata and assignments
- `SecretsByUser`: Index for querying secrets by publisher
- `NextSecretId`: Sequence counter for generating unique secret IDs
- `Params`: Module parameters and configuration

## Message Types

### MsgPublishSecret

Publishes a new time-locked secret with guardian assignments.

```go
type MsgPublishSecret struct {
    Publisher           string                   // Secret publisher address
    RevealBlock         uint64                   // Block height when secret can be revealed
    Threshold           uint32                   // Minimum shares needed for reconstruction
    GuardianAssignments []GuardianAssignment     // Guardian addresses and encrypted shares
    RecipientMetadata   []RecipientMetadata      // Encrypted secret data for recipients
}
```

**Validation Rules:**
- Reveal block must be in the future
- Threshold must be ≤ number of guardian assignments
- All assigned guardians must be active in the guardians module
- No duplicate guardian assignments allowed

### MsgUserCancelSecret

Allows the publisher to cancel a pending secret before the reveal block.

```go
type MsgUserCancelSecret struct {
    Publisher string // Must match the original publisher
    SecretId  string // Secret to cancel
}
```

**Validation Rules:**
- Only the original publisher can cancel
- Secret must be in PENDING status
- Current block height must be < reveal block

### MsgReconstructSecret

Reconstructs a secret using submitted guardian shares after the reveal block.

```go
type MsgReconstructSecret struct {
    Revealer string                    // Address submitting the reconstruction
    SecretId string                    // Secret to reconstruct
    Shares   []GuardianShareWithHMAC   // Guardian shares with HMAC proofs
}
```

**Validation Rules:**
- Current block height must be ≥ reveal block
- Secret must be in PENDING status
- Must provide at least `threshold` number of shares
- All HMAC proofs must be valid

## Secret States

```go
const (
    SECRET_STATUS_PENDING  = "PENDING"   // Awaiting reveal block
    SECRET_STATUS_REVEALED = "REVEALED"  // Successfully reconstructed
    SECRET_STATUS_ABORTED  = "ABORTED"   // Cancelled by publisher
)
```

## Query Services

### QuerySecret
Retrieves a specific secret by ID, including all metadata and guardian assignments.

### QuerySecretsByPublisher
Lists all secrets published by a specific address, useful for user dashboards.

### QueryAllSecrets
Returns all secrets in the system (with pagination in production deployments).

### QueryParams
Returns the current module parameters.

## Integration with Guardians Module

The secrets module has a clean dependency on the guardians module through well-defined interfaces:

```go
type GuardiansKeeper interface {
    IsGuardianActive(ctx context.Context, address string) bool
    VerifyShareHMAC(secretID, guardianAddress string, share, hmac []byte) bool
}
```

This design ensures:
- **Separation of Concerns**: Secrets focuses on user operations, guardians on infrastructure
- **Modularity**: Either module can be upgraded independently
- **Testability**: Easy to mock guardian operations in tests

## Security Considerations

### HMAC-Based Slashing Protection

Each guardian receives not just their SSS share, but also an HMAC of that share. This HMAC is calculated using a key derived from:
- Secret ID
- Guardian address  
- Block-specific entropy

When shares are submitted for reconstruction, the HMAC is verified to ensure:
1. The share hasn't been tampered with
2. The guardian isn't revealing shares early (slashing protection)
3. The share corresponds to the correct secret and guardian

### Cryptographic Flow

1. **Publication**: Publisher encrypts secret for recipients, generates SSS shares, creates HMACs
2. **Distribution**: Shares and HMACs are assigned to guardians via blockchain transactions
3. **Storage**: Guardians store their shares off-chain, HMACs stored on-chain
4. **Revelation**: After reveal block, anyone can submit valid shares to reconstruct the secret
5. **Verification**: HMAC proofs ensure share integrity and prevent early revelation

## Example Usage

### Publishing a Secret

```bash
# Publish a secret that can be revealed after block 1000000
timeflared tx secrets publish-secret \
  --reveal-block 1000000 \
  --threshold 3 \
  --guardian-assignments guardian1,guardian2,guardian3,guardian4,guardian5 \
  --recipient-metadata encrypted_data_for_recipient \
  --from publisher_address
```

### Querying Secrets

```bash
# Get a specific secret
timeflared query secrets secret secret-123

# Get all secrets by a publisher
timeflared query secrets secrets-by-publisher cosmos1abc...

# Get all secrets
timeflared query secrets all-secrets
```

### Reconstructing a Secret

```bash
# Reconstruct secret after reveal block (submitted by anyone)
timeflared tx secrets reconstruct-secret secret-123 \
  --shares guardian1:share1:hmac1,guardian2:share2:hmac2,guardian3:share3:hmac3 \
  --from reconstructor_address
```

## Events

The module emits the following events:

- `secret_published`: When a new secret is published
- `secret_cancelled`: When a publisher cancels their secret  
- `secret_revealed`: When a secret is successfully reconstructed

## Parameters

Module parameters control operational limits and security settings:

```go
type Params struct {
    MaxRevealBlockDelay uint64 // Maximum blocks in future for reveal
    MinThreshold        uint32 // Minimum threshold value
    MaxGuardians        uint32 // Maximum guardians per secret
}
```

## Development and Testing

The module includes comprehensive test coverage for:
- Message validation and state transitions
- Cryptographic operations and HMAC verification
- Integration with the guardians module
- Genesis import/export functionality

For local development:
```bash
# Generate protobuf files
make proto-gen

# Run tests
go test ./x/secrets/...

# Run integration tests
make test-integration
```

## Future Enhancements

- **Pagination**: Add pagination support to queries for production scalability
- **CLI Commands**: Implement user-friendly CLI commands for all operations
- **REST API**: Add REST endpoints alongside gRPC for web integration
- **Metrics**: Add prometheus metrics for monitoring secret publication/revelation rates
- **Gas Optimization**: Optimize gas costs for large guardian sets
