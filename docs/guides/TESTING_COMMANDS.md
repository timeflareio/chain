# Timeflare Testing Guide

Comprehensive testing commands for the Timeflare blockchain with automated 👮 guardian network and two-phase secret protocol.

## 🚀 Quick Start

### **1. Complete Environment Setup**
```bash
make doctor    # verify toolchain
make build     # build timeflared, guardiand, and the TypeScript SDK
make dev-up    # start chain + 5 guardians + funded test user
```

### **2. Manage the 👮 Guardian Network**
```bash
# Overview of chain and guardian health
make dev-status

# Direct guardian orchestration
./devnet/guardians.sh status
./devnet/guardians.sh logs guardian-01
```

### **3. Test Complete Secret Workflow**
```bash
# Full three-phase lifecycle against the running devnet
make e2e

# Monitor secrets
node typescript-sdk/examples/monitor-secrets.js list
```

## 🏗️ Architecture Overview

**Timeflare Protocol:**
- **Two-Phase Commit**: Secure secret publication without on-chain exposure
- **Automated 👮 Guardians**: Self-managing nodes that register and reveal shares
- **Deflationary Economics**: 90% 🏛️ validators, 10% burn (👮 guardians earn directly from secret creators)
- **Fixed Supply**: 1 billion VEIL tokens (no inflation)

## 🛡️ 👮 Guardian Network Testing

### **Start Automated 👮 Guardian Network**
```bash
# Start 5 👮 guardians (default, as part of the devnet)
make dev-up

# Start 10 👮 guardians
make dev-up GUARDIAN_COUNT=10

# Check 👮 guardian status (process, on-chain state, health)
./devnet/guardians.sh status

# Check a single running guardian's health endpoint
guardiand health --config-path ~/.timeflare/guardian/guardian-01/config.yaml

# Open a guardian's read-only operator dashboard. Guardian i serves on
# 21200 + (i-1), so guardian-01 is 21200 and guardian-24 is 21223.
# Unauthenticated as of guardian v0.0.4: every field it serves is something the
# chain already publishes about that guardian, or plain liveness, so there is no
# credential to supply and nothing for the devnet to provision.
open http://127.0.0.1:21200

# Or read one section without a browser — the page polls exactly these:
curl -s http://127.0.0.1:21200/api/vitals      | jq
curl -s http://127.0.0.1:21200/api/assignments | jq '.at_risk'
curl -s http://127.0.0.1:21200/api/economics   | jq '{bond_k_display, float_unlocked_uveil, active_bond_count}'
curl -s http://127.0.0.1:21200/api/keys        | jq '{fingerprints_match, plaintext_key_warning, current_epoch}'

guardianctl config doctor   # reports what the dashboard exposes, among other checks
```

### **👮 Guardian Operations**
```bash
# View 👮 guardian logs
./devnet/guardians.sh logs guardian-01  # Guardian 1
./devnet/guardians.sh logs guardian-02  # Guardian 2

# Stop all 👮 guardians
./devnet/guardians.sh stop

# Clean up all 👮 guardian data
./devnet/guardians.sh clean
```

### **👮 Guardian Key Custody (see docs/guides/GUARDIAN_KEY_CUSTODY.md)**
```bash
# Devnet guardians run on the encrypted-at-rest key format with a well-known
# dev passphrase (override with GUARDIAN_KEY_PASSPHRASE=... before dev-up)

# Export an encrypted backup bundle for a devnet guardian
guardianctl key backup --config-path ~/.timeflare/guardian/guardian-01/config.yaml \
  --output /tmp/guardian-01.tfb

# Restore it (chain verification against the registered record)
guardianctl key restore --config-path ~/.timeflare/guardian/guardian-01/config.yaml \
  --input /tmp/guardian-01.tfb --force

# Encrypt a legacy plaintext key in place
guardianctl config migrate-key --config-path ~/.timeflare/guardian/guardian-01/config.yaml

# Startup self-check: swap in a wrong private_key and 'guardiand start'
# refuses to run (share key must derive the registered public key)
```

### **👮 Guardian Key Rotation (see docs/spec.md "Guardian Key Rotation")**
```bash
# Full daemon ceremony: generate → backup bundle (whole keyring) → submit →
# retire the old key beside the new one (private_key.epoch<N>)
guardianctl rotate-key --config-path ~/.timeflare/guardian/guardian-01/config.yaml \
  --backup-output /tmp/guardian-01-rotation.tfb \
  --backup-passphrase-file /tmp/backup-pass --yes

# Bare CLI equivalent (the daemon ceremony above is the recommended path —
# it backs the new key up BEFORE broadcasting)
timeflared tx secrets guardian-rotate-key $(openssl rand -hex 32) \
  --from guardian-manual --keyring-backend test --chain-id timeflare-test --yes

# Query a guardian's key-epoch history (epoch 0 = the registration key)
timeflared query secrets guardian-key-history \
  $(timeflared keys show guardian-manual -a --keyring-backend test)

# Constraint checks: a second rotation inside 432,000 blocks is rejected
# ("key rotation minimum interval not met"), and rotating to ANY key ever
# registered — including your own retired one — is rejected ("encryption key
# already registered"). The rotation burns a flat rate × 14,400 fee.
```

### **Manual 👮 Guardian Registration (if needed)**
```bash
# Create test 👮 guardian account
timeflared keys add guardian-manual --keyring-backend test

# Fund the account
timeflared tx bank send \
  $(timeflared keys show alice -a --keyring-backend test) \
  $(timeflared keys show guardian-manual -a --keyring-backend test) \
  15000000000uveil \
  --keyring-backend test \
  --chain-id timeflare-test \
  --yes

# Register 👮 guardian
# Positional args: guardian-address encryption-key available-from available-until deposit [accepting-secrets]
# Availability values are RELATIVE block offsets from the current height
# (available-from 0 = current block + 1). The deposit is the initial FLOAT
# (working capital for per-secret bonds), not a minimum stake. Registration
# additionally charges the 1,000 VEIL entry fee (routed through the 90/10 fee split: 900 VEIL to validators, 100 VEIL burned).
# 20,000 VEIL covers four bump-1.00 bonds.
timeflared tx secrets guardian-register \
  $(timeflared keys show guardian-manual -a --keyring-backend test) \
  $(openssl rand -hex 32) \
  0 \
  100000 \
  20000000000uveil \
  false \
  --from guardian-manual \
  --keyring-backend test \
  --chain-id timeflare-test \
  --gas auto \
  --gas-adjustment 1.5 \
  --yes

# Update the 👮 guardian (all flags optional; --accepting-secrets is
# presence-aware — omit it for no change)
timeflared tx secrets guardian-update \
  $(timeflared keys show guardian-manual -a --keyring-backend test) \
  --accepting-secrets=true \
  --from guardian-manual --keyring-backend test --chain-id timeflare-test --yes

# Withdraw the guardian's entire unlocked float (signer is the guardian;
# bonds for in-flight secrets stay locked, the registration persists)
timeflared tx secrets guardian-withdraw-stake \
  --from guardian-manual --keyring-backend test --chain-id timeflare-test --yes
```

### **Query 👮 Guardian Information**
```bash
# List all 👮 guardians
timeflared query secrets guardians

# Query specific 👮 guardian
timeflared query secrets guardian $(timeflared keys show alice -a --keyring-backend test)
```

## 🔐 Secrets Module Testing

### **Three-Phase Secret Publication**

The full creator flow (client-side encryption, share preparation) is easiest
through the TypeScript SDK (`make e2e` exercises it end-to-end); the CLI
commands below submit the corresponding messages directly.

#### **Phase 1: Reserve Secret**
```bash
# Positional args:
#   [detection-hint] threshold min-shares max-shares bump [reveal-start-offset] [reveal-duration]
# bump is the security factor in hundredths (100-1000 = 1.00-10.00); the reward
# pool is protocol-derived (P = max_shares × F_reveal + rate × distance ×
# max_shares × bump) and the secret ID is protocol-assigned (read it from the
# tx response/event), not chosen. Band validation: threshold ≤ min ≤ max ≤ 32,
# max − min < threshold.
# The creator is debited P + the accept fees A = max_shares × F_accept
# (escrowed apart from the pool, reported as accept_fees on the
# secret_reserved event) + the NON-REFUNDABLE creation fee
# max(60,000 uveil, P_time × bps(distance) ÷ 10,000) + gas — the fee is
# charged on the pool's TIME component only, and it and its pricing regime are
# reported on the same event (creation_fee / creation_fee_regime); see
# spec.md "Creation Fee" and "Terminal-state disposition".
# --random-hint replaces the positional detection hint (no-discovery pattern):
timeflared tx secrets user-request-guardians \
  --random-hint 3 15 17 100 250 150 \
  --from alice --keyring-backend test --chain-id timeflare-test --yes
```

#### **Phase 2: Distribute Shares**
```bash
# Positional args: secret-id shares-file secret-commitment payload-ciphertext secret-public-key
# shares-file: JSON array of {guardian_address, encrypted_share, share_hmac} (hex fields)
# payload-ciphertext: file path (raw bytes) or hex; the rest are hex
timeflared tx secrets user-distribute-shares \
  "$SECRET_ID" shares.json "$COMMITMENT_HEX" payload.bin "$SECRET_PUBKEY_HEX" \
  --from alice --keyring-backend test --chain-id timeflare-test --yes
```

#### **Phase 3: Guardian Confirmation**
```bash
# Signer is the guardian; accept=true locks the per-secret bond
timeflared tx secrets guardian-confirm-shares "$SECRET_ID" true \
  --from guardian-manual --keyring-backend test --chain-id timeflare-test --yes
```

### **Secret Lifecycle Operations**
```bash
# Query secret status
timeflared query secrets show "secret-123"

# List all secrets
timeflared query secrets secrets

# Cancel an activated (pending) secret before its reveal window opens
# (pro-rata guardian pay; unearned remainder refunded)
timeflared tx secrets user-cancel-secret "secret-123" \
  --from alice --keyring-backend test --chain-id timeflare-test --yes
```

### **📨 Recipient Rebate (docs/spec.md "Recipient Rebate")**
```bash
# A revealed secret carries the rebate credited to its recipient at settlement.
# Collectable for three months from settlement (RebateCollectionBlocks); after
# that it is voided and the reservation returns to the pool. A zero amount is
# omitted from JSON output, so no match means no rebate was credited — the
# secret did not settle revealed, or its share fell below the dust floor.
timeflared query secrets show "$SECRET_ID" | grep -E "rebate_amount|rebate_collected"

# The rebate pool is keyless: its balance is the accrual rate's only input, and
# nothing can top it up.
timeflared query bank balances tmflr1g6ct2qh5jtrew322yuumdehgwnk9pcexzzz3d2

# Collection is commit–reveal. Derive both values with the SDK helper (it reads
# the secret's hint and the devnet recipient key):
node typescript-sdk/examples/rebate-proof.js "$SECRET_ID" "$(timeflared keys show recipient -a)"
# -> {"proof":"<hex>","commitment":"<hex>","rebate":"<uveil>","collected":false}

# Step 1: commit. Binds the proof to this address without revealing it.
timeflared tx secrets recipient-commit-rebate "$SECRET_ID" "$COMMITMENT_HEX" \
  --from recipient --keyring-backend test --chain-id timeflare-test --yes

# Step 2 (a LATER block): reveal and collect. Revealing in the same block is
# refused — that gap is what stops an observer copying the proof out of the
# mempool and collecting ahead of you.
#
# WARNING: the proof is public and permanent once submitted. It links the
# collecting address to that secret, and collecting on several links those
# secrets together. Collect to a single-use address if that matters.
timeflared tx secrets recipient-collect-rebate "$SECRET_ID" "$PROOF_HEX" \
  --from recipient --keyring-backend test --chain-id timeflare-test --yes

# Refusals name their reason: no rebate credited, already collected, or the
# proof does not match the secret's detection hint.
```

### **👮 Guardian Share Revelation**
```bash
# During the reveal window; the share is the plaintext key-share envelope
# (hex, base64, or a file path). Reconstruction itself is client-side.
timeflared tx secrets guardian-reveal-share "$SECRET_ID" "$DECRYPTED_SHARE_HEX" \
  --from guardian-manual --keyring-backend test --chain-id timeflare-test --yes
```

## 💰 Economic Testing

### **Fee Distribution Verification**
```bash
# Check 🏛️ validator rewards
timeflared query distribution rewards $(timeflared keys show alice -a --keyring-backend test)

# Check 👮 guardian module balance
timeflared query bank balances $(timeflared query auth module-account guardians -o json | jq -r '.account.base_account.address')

# Check distribution module balance  
timeflared query bank balances $(timeflared query auth module-account distribution -o json | jq -r '.account.base_account.address')

# Verify fee burn (total supply should decrease)
timeflared query bank total --denom uveil
```

### **👮 Guardian Slashing Test**
```bash
# Report 👮 guardian early reveal (evidence is the guardian's decrypted
# share — hex, base64, or a file path — verified against the stored HMAC)
timeflared tx secrets slash-guardian \
  $(timeflared keys show guardian-manual -a --keyring-backend test) \
  "$DECRYPTED_SHARE_HEX" \
  "Early reveal detected" \
  "$SECRET_ID" \
  --from alice \
  --keyring-backend test \
  --chain-id timeflare-test \
  --yes

# Verify slashing results
timeflared query secrets guardian $(timeflared keys show guardian-manual -a --keyring-backend test)
timeflared query bank total --denom uveil  # Should decrease (tokens burned)
```

## 🧪 Integration Testing

### **End-to-End Client Library Test**
```bash
# Test complete workflow using client library
cd typescript-sdk/examples

# Install dependencies
npm install

# Run automated test
npx ts-node run-with-local-chain.ts

# Expected output:
# ✅ Connected to local chain
# ✅ Guardian automation detected
# ✅ Secret published successfully
# ✅ Guardians reveal shares automatically
# ✅ Secret reconstructed and retrieved
```

### **👮 Guardian Network Integration**
```bash
# Start 👮 guardian network
make dev-up

# Publish secret using CLI
CURRENT_HEIGHT=$(timeflared query block | jq -r '.block.header.height | tonumber')
timeflared tx secrets reserve-secret \
  --secret-commitment $(echo -n "integration test secret" | sha256sum | cut -d' ' -f1) \
  --recipient-public-key $(openssl rand -hex 32) \
  --recipient-metadata "test=integration" \
  --reveal-window-start-offset 20 \
  --reveal-window-duration 80 \
  --threshold 2 \
  --min-shares 6 \
  --max-shares 7 \
  --bump 100 \
  --from alice \
  --keyring-backend test \
  --chain-id timeflare-test \
  --gas auto \
  --yes

# Monitor 👮 guardian logs to see automatic processing
./devnet/guardians.sh status
```

## 📊 Performance Testing

### **Bulk Secret Operations**
```bash
# Create multiple secrets for load testing
for i in {1..10}; do
  CURRENT_HEIGHT=$(timeflared query block | jq -r '.block.header.height | tonumber')
  SECRET_ID="load-test-$i"
  
  timeflared tx secrets reserve-secret \
    --secret-commitment $(echo -n "load test secret $i" | sha256sum | cut -d' ' -f1) \
    --recipient-public-key $(openssl rand -hex 32) \
    --recipient-metadata "test=load,id=$i" \
    --reveal-window-start-offset $((50 + i*10)) \
    --reveal-window-duration 100 \
      --threshold 2 \
    --min-shares 6 \
    --max-shares 7 \
    --bump 100 \
    --from alice \
    --keyring-backend test \
    --chain-id timeflare-test \
    --gas auto \
    --yes
  
  sleep 1
done
```

### **👮 Guardian Network Load Test**
```bash
# Start many 👮 guardians
make dev-up GUARDIAN_COUNT=20

# Monitor performance
while true; do
  echo "=== 👮 Guardian Network Status ==="
  ./devnet/guardians.sh status
  echo "=== Blockchain Status ==="
  timeflared query block | jq -r '.block.header.height'
  sleep 10
done
```

## 🔍 Debugging Commands

### **Chain Status**
```bash
# Basic chain info
timeflared status
timeflared query block

# Module info
timeflared query guardians params
timeflared query secrets params

# Account balances
timeflared query bank balances $(timeflared keys show alice -a --keyring-backend test)
```

### **Transaction Debugging**
```bash
# Get transaction details
TX_HASH="your_tx_hash_here"
timeflared query tx $TX_HASH

# Check transaction events
timeflared query tx $TX_HASH | jq '.events'

# Search for transactions
timeflared query txs --events 'message.action="/timeflare.guardians.v1.MsgGuardianRegister"'
```

### **Module State Inspection**
```bash
# Check module accounts
timeflared query auth module-account guardians
timeflared query auth module-account secrets
timeflared query auth module-account distribution

# Total supply monitoring
timeflared query bank total

# All 👮 guardians
timeflared query guardians list-guardian

# All secrets
timeflared query secrets list-secret
```

## 🎯 Expected Results

### **Successful Operations Should:**
1. ✅ Guardian network starts and registers automatically
2. ✅ Secrets publish in two phases without exposing data
3. ✅ Guardians automatically reveal shares at optimal times
4. ✅ Secret reconstruction works when threshold is met
5. ✅ Fee distribution splits 90/10 correctly (validators/burn)
6. ✅ Client library provides seamless UX
7. ✅ All cryptographic operations happen client-side
8. ✅ Protocol enforces security through economics

### **Failed Operations Should:**
1. ❌ Reject secrets with insufficient guardians
2. ❌ Reject invalid guardian registrations  
3. ❌ Reject share reveals outside reveal window
4. ❌ Slash guardians for invalid HMACs
5. ❌ Prevent unauthorized operations

## 🔧 Troubleshooting

### **👮 Guardian Issues**
```bash
# Check 👮 guardian logs
./devnet/guardians.sh logs guardian-01

# Restart specific 👮 guardian
# (Stop all and restart for now)
./devnet/guardians.sh stop
make dev-up
```

### **Chain Issues**
```bash
# Reset chain state
make reset

# Check chain connectivity
curl -s http://localhost:26657/status

# View chain logs
timeflared start --log_level debug
```

### **Client Library Issues**
```bash
# Check Node.js dependencies
cd typescript-sdk && npm install

# Run with debug logging
DEBUG=* npx ts-node run-with-local-chain.ts
```

## 📝 Test Checklist

- [ ] Chain starts successfully
- [ ] 👮 Guardian network registers automatically
- [ ] Two-phase secret publication works
- [ ] 👮 Guardians reveal shares automatically  
- [ ] Secret reconstruction succeeds
- [ ] Fee distribution works correctly
- [ ] Client library integration works
- [ ] Slashing mechanics function
- [ ] Economic incentives align properly
- [ ] All security properties maintained

## 🔗 Quick Reference

```bash
# Start everything
make start
make dev-up

# Test end-to-end
cd typescript-sdk/examples && npx ts-node run-with-local-chain.ts

# Monitor
./devnet/guardians.sh status

# Stop everything  
./devnet/guardians.sh stop
```

This testing framework provides comprehensive coverage of the automated guardian network, two-phase secret protocol, and deflationary economics with real guardian nodes performing their duties automatically.