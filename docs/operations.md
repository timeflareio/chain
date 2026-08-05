# Timeflare Models and Operations Specification

This document is the operation-level API reference for the Timeflare protocol: every invocable operation and automatic (block-driven) operation, with its model context, field validation, preconditions, side effects, token flows and validated CLI usage.

**Related documents**: [spec.md](spec.md) is the protocol authority — the rationale, economics and security model behind every rule stated here. [CHAIN_MECHANICS.md](CHAIN_MECHANICS.md) is the implementation's inside view: state layout, genesis behaviour, measured performance, and the judgement ledger — accepted trade-offs, deliberate oddities, open concerns and defects. Where this document states *what an operation does and how to call it*, those two explain *why* and *at what cost*.

**Note**: All operations are implemented in a single unified `secrets` module that handles both guardian infrastructure and secret lifecycle management. Every handler first verifies the transaction signer equals the actor address named in the message.

**Symbols used throughout** (all derived from one master knob, `rate` = 1 uveil per guardian per block; see spec.md "Secret Economics & Slashing"). `bump` and `k` appear as their decimal values throughout; both are *carried on the wire* in hundredths (`bump` 100–1,000, `k` 400–2,400), and the implementation divides by that fixed-point scale — an encoding detail, not part of the pricing:

| Symbol | Meaning |
|--------|---------|
| `P` | Reward pool: `P = (max_shares × F_reveal) + (rate × distance × max_shares × bump)` |
| `A` | Accept fees: `A = max_shares × F_accept`, escrowed apart from `P` |
| `B_g` | A guardian's per-secret bond: `B_g = rate × distance × bump × k_g`, frozen at selection |
| `k` | Per-guardian bond multiplier, 4.00–24.00: ×1.26 per slash, ×0.963 per correct reveal |
| `bump` | Creator's security factor, 1.00–10.00: scales `P` and every `B_g` together |
| `distance` | `(reveal_end_block + 1) − commit_deadline` — the blocks a guardian holds the share |
| `F_accept` / `F_reveal` | Gas reimbursement legs: the guardian's accept (120,000 gas) and reveal (130,000 gas) transactions priced at the consensus fee floor — 12,000 and 13,000 uveil today |
| `S` | A revealed secret's irrecoverable creator spend: `P` plus the accept-fee slices paid to revealers |

## Table of Contents

1. [Guardian](#guardian)
   - [Model](#model)
   - [Operations](#operations)
     - [MsgGuardianRegister](#msgguardianregister)
     - [MsgGuardianUpdate](#msgguardianupdate)
     - [MsgGuardianRotateKey](#msgguardianrotatekey)
     - [MsgGuardianWithdrawStake](#msgguardianwithdrawstake)
     - [MsgGuardianConfirmShares](#msgguardianconfirmshares)
     - [MsgGuardianRevealShare](#msgguardianrevealshare)
2. [Secret](#secret)
   - [Model](#model-1)
   - [Operations](#operations-1)
     - [MsgUserRequestGuardians](#msguserrequestguardians)
     - [MsgUserDistributeShares](#msguserdistributeshares)
     - [MsgSlashGuardian](#msgslashguardian)
     - [MsgUserCancelSecret](#msgusercancelsecret)
3. [Recipient Rebate](#recipient-rebate)
   - [Operations](#operations-2)
     - [MsgRecipientCommitRebate](#msgrecipientcommitrebate)
     - [MsgRecipientCollectRebate](#msgrecipientcollectrebate)
4. [Time-Based Operations (BeginBlock/EndBlock)](#time-based-operations-beginblockendblock)
5. [Funds Flow Summary](#funds-flow-summary)
6. [Hard-Coded Protocol Values](#hard-coded-protocol-values)
7. [Queries](#queries)

---

## Guardian

### Model

The `Guardian` model represents a registered guardian node that provides secret-sharing services. Guardians are independent operators who pay a one-off 1,000 VEIL entry fee to register and maintain a deposited **float** of VEIL as working capital: accepting a secret locks a per-secret bond `B_g` from the float, and settlement returns or slashes it. Guardians earn from secret creators — a share of the reward pool `P` for revealing on time, plus gas reimbursements for the transactions the protocol obliges them to send. Each guardian maintains an append-only history of share-encryption key epochs and operates within a defined availability window. Registration is permanent: there is no deregistration, and a drained or lapsed guardian simply stops qualifying for selection until topped up or extended.

#### Properties

| Property | Type | Description |
|----------|------|-------------|
| `address` | `string` | Guardian's unique blockchain address (bech32, `tmflr` prefix) |
| `encryption_public_key` | `bytes` | Current-epoch X25519 public key used for encrypting key shares (32 bytes) |
| `current_key_epoch` | `uint64` | Current key epoch — 0 at registration, +1 per rotation; the full history is `(guardian, epoch) → {public_key, effective_from_height}` |
| `available_from` | `int64` | Absolute block height when the guardian starts accepting assignments |
| `available_until` | `int64` | Absolute block height when the guardian stops accepting new assignments |
| `stake` | `Coin` | Float **total** deposited in module escrow (working capital for bonds) |
| `locked_stake` | `Coin` | Portion of the float currently locked as per-secret bonds; `unlocked = stake − locked_stake` |
| `accepting_secrets` | `bool` | Whether the guardian accepts new assignments (existing commitments are unaffected) |
| `bond_k` | `int64` | Live bond multiplier `k` in hundredths (400–2,400 = 4.00–24.00); prices this guardian's bonds |
| `active_bond_count` | `int64` | Currently locked per-secret bonds — the concurrency-cap gate (100) |

#### Selection Eligibility

A guardian is a candidate for a given secret's selection draw when **all** of the following hold:

- `accepting_secrets` is true
- `available_from ≤ current_block` and `available_until ≥ reveal_end_block` (the whole window must be covered)
- `active_bond_count < 100` (`MaxActiveBondsPerGuardian`)
- unlocked float ≥ `B_g` for this secret, priced by the guardian's own live `k`

Selection among candidates is uniform hash sortition seeded from consensus data — float size and `k` buy no selection advantage (see spec.md "Guardian Selection (Normative)").

---

### Operations

### MsgGuardianRegister

Register a new guardian node to participate in the secret-sharing protocol. Registration charges a one-off **1,000 VEIL entry fee** (routed to the fee collector, where it rides the next block's 90/10 validator/burn split — never returned) plus an optional initial float deposit into module escrow. The registration key becomes **epoch 0** of the guardian's append-only key history.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `guardian` | `string` | ✓ | - | Valid bech32 address with `tmflr` prefix; not already registered; not a vesting account | Guardian address (also transaction signer) |
| `encryption_public_key` | `bytes` | ✓ | - | Exactly 32 bytes; valid X25519 key (not all zeros, not a small-order point); unique across **every key ever registered by any guardian at any epoch** | Key-share encryption key — becomes key epoch 0 |
| `available_from` | `int64` | ✗ | 0 (= current_block + 1) | Relative blocks from current; max 2,628,000 (~6 months) in the future | Availability start, converted to an absolute height |
| `available_until` | `int64` | ✓ | - | Relative blocks from current; resulting window (`until − from`) must be ≥ 100 blocks (~10 minutes) and ≤ 5,256,000 blocks (~1 year) | Availability end, converted to an absolute height |
| `deposit` | `Coin` | ✓ (may be zero) | - | `uveil`; no minimum or maximum | Initial float deposit — working capital for per-secret bonds |
| `accepting_secrets` | `bool` | ✗ (CLI) | `true` | - | Whether to accept new assignments |

#### Preconditions

- Guardian address does not already exist in the registry
- Guardian address is not a vesting account (the float is slashable collateral; slashing must never reach unvested tokens)
- The encryption key has never been registered by any guardian at any epoch (the key-uniqueness index never shrinks)
- The account balance covers the 1,000 VEIL entry fee plus the deposit plus gas

#### Side Effects & Token Flow

- Entry fee (1,000 VEIL) transferred guardian → **fee collector** (90% allocated to validators, 10% burned in the next block; never returned)
- Deposit (if non-zero) transferred guardian → module escrow as the initial float (`stake` = total, `locked_stake` = 0)
- Guardian record created with the bond multiplier at its floor (`bond_k = 400` = 4.00), zero active bonds and `current_key_epoch = 0`
- The key is recorded as epoch 0 of the key history with `effective_from_height = registration height + 1`, which also starts the rotation-interval clock
- Emits `guardian_registered`

#### Examples

The encryption key must be a real X25519 public key whose private half the operator holds — the guardian daemon (`guardiand`) generates and manages the keypair, and its registration flow wraps this transaction. A key nobody holds the private half of produces shares nobody can ever decrypt.

```bash
# Register a guardian with a 100,000 VEIL float, available for ~1 week (100,800 blocks)
# starting next block (available-from 0), accepting secrets
timeflared tx secrets guardian-register \
  tmflr1guardian... \
  <64-hex-char-x25519-public-key> \
  0 \
  100800 \
  100000000000uveil \
  true \
  --from guardian-account

# Register with a zero float (deposit later via guardian-update), not yet accepting
timeflared tx secrets guardian-register \
  tmflr1guardian... \
  <64-hex-char-x25519-public-key> \
  0 \
  5256000 \
  0uveil \
  false \
  --from guardian-account
```

A guardian with a zero or drained float is registered but never selected — every selection requires the unlocked float to cover that secret's bond `B_g`.

---

### MsgGuardianUpdate

Update an existing guardian's operational parameters: extend the availability window, top up the float, or toggle assignment acceptance. All parameters are optional flags, applied atomically; at least one field must be specified. The encryption key is **not** updatable here — each epoch's key binding is permanently immutable, and new keys arrive via [MsgGuardianRotateKey](#msgguardianrotatekey).

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `guardian` | `string` | ✓ | - | Must match an existing guardian; transaction signer | Guardian address |
| `available_from` | `int64` | ✗ | `0` (preserve existing) | Relative blocks from current; **silently preserved** while the guardian is within or precedes its availability period; max 2,628,000 in the future once the period has passed | Availability start |
| `available_until` | `int64` | ✗ | `0` (no change) | Relative blocks from current; while within or preceding the period, must **strictly extend** the existing end and lie ≤ 5,256,000 blocks from now; after the period, normal registration constraints apply | Availability end |
| `deposit` | `Coin` | ✗ | none | Positive `uveil` amount; uncapped | Additional float deposit (top-up; there is no decrease path other than withdrawal) |
| `accepting_secrets` | `BoolValue` | ✗ | omitted (no change) | Presence-aware: omit to preserve; an explicit true/false is applied and counts as a field change on its own | Whether to accept new assignments |

#### Availability-Window Policy

Restrictions depend on the guardian's position relative to its current window:

- **Within or preceding the period** (`current_block ≤ available_until`): only extensions — `available_until` may only increase (strictly), capped at 1 year from the current block; `available_from` changes are ignored (existing value preserved)
- **Passed the period** (`available_until < current_block`): full flexibility — both bounds may be set fresh under the registration constraints (window ≥ 100 blocks, ≤ 1 year; start ≤ 6 months out)

Toggling `accepting_secrets` off removes the guardian from the selection candidate set from the next selection onward (back on restores it); existing assignments are unaffected — the flag governs new candidacy only.

#### Side Effects & Token Flow

- Deposit (if given) transferred guardian → module escrow, added to the float total
- Availability and acceptance changes update the selection-eligibility index immediately (effective for the next selection, even in the same block)
- Emits `guardian_updated`

#### Examples

```bash
# Extend availability to 50,000 blocks from the current block
timeflared tx secrets guardian-update tmflr1guardian... \
  --available-until 50000 \
  --from guardian-account

# Deposit an additional 5,000 VEIL into the float
timeflared tx secrets guardian-update tmflr1guardian... \
  --deposit 5000000000uveil \
  --from guardian-account

# Stop accepting new secrets (graceful maintenance; existing commitments still honoured)
timeflared tx secrets guardian-update tmflr1guardian... \
  --accepting-secrets=false \
  --from guardian-account

# Combined: extend availability and top up the float
timeflared tx secrets guardian-update tmflr1guardian... \
  --available-until 100000 \
  --deposit 2000000000uveil \
  --from guardian-account
```

---

### MsgGuardianRotateKey

Rotate the guardian's share-encryption key forward for **future** assignments. The new key becomes the next epoch of the guardian's append-only key history and takes effect for selections from the next block; every existing assignment remains bound to the epoch key it was created under. Rotation is the key-hygiene path — it is **not** a loss-recovery mechanism (the chain holds only ciphertext and can re-encrypt nothing).

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `guardian` | `string` | ✓ | - | Must match an existing guardian; transaction signer | Guardian address |
| `new_key` | `bytes` | ✓ | - | Exactly 32 bytes; valid X25519 key (not all zeros, not a small-order point); globally unique across every key ever registered by any guardian at any epoch | The next epoch's encryption public key |

#### Preconditions

- **Minimum interval**: one rotation per guardian per 432,000 blocks (~30 days) — `current_height − last_effective_height ≥ 432,000`, where `last_effective_height` is the newest history entry's effective height (epoch 0's, set at registration, starts the clock)
- The guardian's account covers the rotation fee plus gas

#### Side Effects & Token Flow

- Flat rotation fee `rate × 14,400` (one guardian-day; 14,400 uveil today) **burned** from the guardian's account — anti-spam pricing of the permanent history entry
- Appends `(guardian, current_key_epoch + 1) → {new_key, height + 1}` to the key history; advances `current_key_epoch` and the record's current-key convenience copy
- **Effective next block**: a rotation landing at height *h* records `effective_from_height = h + 1`; a same-block selection still hands the creator the pre-rotation key
- **Permanent reservation**: the retired key stays reserved forever — it can never be registered again by any guardian
- Emits `guardian_key_rotated { guardian, new_epoch, effective_from_height, fee_burned }`

#### Examples

```bash
# Rotate to a freshly generated key (the guardian daemon wraps this in
# `guardianctl rotate-key`, which backs the new key up BEFORE submitting)
timeflared tx secrets guardian-rotate-key \
  <64-hex-char-new-x25519-public-key> \
  --from guardian-account
```

#### What Happens Next

Selections from the next block onward encrypt to the new key. The guardian daemon resolves the epoch key for each assignment automatically from the secret's creation height against the history's effective heights, and may delete a retired key locally once its last assignment settles. Key history is queryable via `Query/GuardianKeyHistory` (`timeflared query secrets guardian-key-history <address>`).

---

### MsgGuardianWithdrawStake

Withdraw the guardian's entire **unlocked** float (`stake − locked_stake`) at any time. Bonds for in-flight secrets remain locked in escrow until their settlements release them. The guardian record **persists** — registration is permanent (the entry fee is already spent); a drained guardian simply stops qualifying for selection until topped up again via [MsgGuardianUpdate](#msgguardianupdate).

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `guardian` | `string` | ✓ | - | Must match an existing guardian; transaction signer | Guardian requesting withdrawal |

#### Preconditions

- Guardian exists with a positive unlocked float (a guardian whose entire float is bonded has nothing withdrawable)

#### Side Effects & Token Flow

- The entire unlocked float is transferred module → guardian in one send
- The float total shrinks to exactly the locked portion; the guardian record and key history persist
- The guardian's selection-eligibility entry is updated (an empty unlocked float fails every bond-affordability test)

#### Examples

```bash
# Withdraw everything not currently bonded (the --from account is the guardian)
timeflared tx secrets guardian-withdraw-stake --from guardian-account
```

---

### MsgGuardianConfirmShares

Accept or reject a share assignment after verifying the encrypted share offline (Phase 3 of the three-phase commit). Guardians should decrypt their assigned share, verify the HMAC matches, and ensure the share data is valid before accepting — this protects them from malicious creators submitting invalid shares or incorrect HMACs. **Acceptance locks the guardian's frozen bond `B_g` from its unlocked float.** Acceptances accumulate up to `max_shares` until the commit deadline, where the roster finalises: with at least `min_shares` accepted, the secret activates with exactly the accepted set.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `guardian` | `string` | ✓ | - | Must hold a `PROPOSED` assignment record for this secret; transaction signer | Guardian responding |
| `secret_id` | `string` | ✓ | - | Must exist in `awaiting_acceptance` state | Target secret |
| `accept` | `bool` | ✓ | - | - | true = accept (locks the bond), false = reject |

#### Preconditions

- Secret is in `awaiting_acceptance` and `current_height ≤ commit_deadline`
- The caller has an assignment record still in `PROPOSED` (i.e. actually received share data — a selected guardian the creator skipped at distribution has no record and can never accept)
- If accepting: the guardian must still be active (within its availability window), below the 100-active-bond concurrency cap (re-checked here — the hard gate, since one guardian can be in flight on several selections at once), and its unlocked float must cover its frozen bond `B_g` — the lock is all-or-nothing, never partial

#### Side Effects & Token Flow

- **Accept**: the guardian's frozen `B_g` moves unlocked → locked within its float (internal accounting; the tokens already sit in module escrow), `active_bond_count` increments, the assignment flips to `ACCEPTED` and `accepted_count` increments. The guardian's eligibility projection is rewritten, so a later selection prices its next bond against the post-lock float
- **Reject**: the assignment flips to `REJECTED`; no bond is locked; the encrypted share record is retained for audit
- **No race**: every valid acceptance up to `max_shares` joins the roster — no confirmation is turned away while a slot remains and the deadline has not passed
- **Lock-in is inferred**: once `accepted_count ≥ min_shares` the secret is guaranteed to activate (acceptances are never revoked); there is no on-chain event and no state change — the secret stays in `awaiting_acceptance` until the deadline
- Candidates left in `PROPOSED` at the deadline never lock a bond and face no penalty
- Emits `assignment_accepted` / `assignment_rejected`. The response carries `locked_in` (whether `accepted_count ≥ min_shares` now holds)

#### Band Requirement

A secret activates only if the band's floor is met by the commit deadline:

- At least `min_shares` guardians must accept; everyone who accepted (up to `max_shares`) is an active participant
- Example: for `threshold=5, min=6, max=9`, any 6–9 acceptances by the deadline activate the secret with that exact roster
- Rejections and non-responses within the margin `max − min` do not affect progression
- The accepted count sets reveal-time redundancy: a `min`-sized activation tolerates only `min − threshold` no-shows

#### Examples

```bash
# Accept an assignment (the --from account is the guardian; locks the bond)
timeflared tx secrets guardian-confirm-shares <secret-id> true --from guardian-account

# Reject an assignment (e.g. HMAC verification failed offline)
timeflared tx secrets guardian-confirm-shares <secret-id> false --from guardian-account
```

In practice the guardian daemon verifies and responds automatically; these commands are the manual path.

---

### MsgGuardianRevealShare

Submit the decrypted key share during the designated reveal window. Each submission is HMAC-verified on-chain against the HMAC stored at distribution — a mismatched share is rejected without penalty, and an invalid share is never stored. The reveal that reaches `threshold` makes the secret reconstructable. **No tokens move at reveal** — payment and bond return are deliberately deferred to settlement when the window closes.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `guardian` | `string` | ✓ | - | Assignment must be `ACCEPTED`; transaction signer | Guardian revealing |
| `secret_id` | `string` | ✓ | - | Must exist in `pending` or `reconstructable` state | Target secret |
| `decrypted_share` | `bytes` | ✓ | - | ≤ 64 bytes (plaintext key-share envelope); must pass HMAC verification against the stored HMAC | The decrypted SSS key share (intrinsic share ID inside) |

#### Preconditions

- Secret is in `pending` or `reconstructable` (not cancelled, failed or revealed)
- Current height is inside `[reveal_start_block, reveal_end_block]` — **both bounds inclusive** (settlement falls due at `end + 1`, so a final-block reveal settles normally)
- The caller's assignment is `ACCEPTED` and it has not already revealed for this secret
- The recomputed HMAC over (secret ID, guardian address, submitted share) equals the stored HMAC

#### Side Effects & Token Flow

- One reveal record written (the ~34-byte plaintext key-share envelope, publicly readable); `revealed_count` increments
- The reveal that reaches `threshold` drives the secret → `reconstructable`
- The guardian's bond multiplier steps **down**: `k′ = max(4.00, k × 0.963)` — applied per reveal, so it is the reveal itself that cheapens future acceptances
- **No tokens move** — the bond stays locked and the reward share unpaid until settlement at `reveal_end_block + 1`
- Emits `share_revealed`, `guardian_bond_k_adjusted` (only when `k` actually moved), plus `secret_reconstructed` at threshold. The response carries `accepted` and `reconstruction_complete`

#### Examples

```bash
# Reveal the decrypted key share (hex, base64, or a file path containing raw bytes)
timeflared tx secrets guardian-reveal-share <secret-id> <hex-encoded-share> --from guardian-account
```

---

## Secret

### Model

The `Secret` model represents a time-locked secret progressing from creation to revelation. It tracks protocol-controlled guardian assignment, the three-phase commit (request, distribute, confirm), reveal-window timing, protocol-derived economics and the cryptographic commitments used for client-side verification. The payload is doubly encrypted client-side — once to the recipient's key, once to a per-secret key — and the per-secret private key is split with Shamir Secret Sharing (SSS) into key shares, one per selected guardian. The recipient's public key **never appears on chain**: recipients discover their secrets via the `detection_hint`, which only the holder of the recipient's private key can recognise.

#### Properties

| Property | Type | Description |
|----------|------|-------------|
| `id` | `string` | Protocol-assigned identifier (UUIDv5 over `chainID ‖ secret_counter`) — the creator supplies no ID |
| `creator` | `string` | Address of the secret creator |
| `detection_hint` | `DetectionHint` | Recipient discovery hint: `{version (1), ephemeral_pub (32B), tag (8B)}`. Only the recipient's private key can recompute the tag; random bytes are a valid no-discovery hint, indistinguishable from a real one |
| `reveal_start_block` | `int64` | Absolute block when guardians can start revealing shares |
| `reveal_end_block` | `int64` | Absolute block when the reveal window closes (inclusive) |
| `threshold` | `int64` | Minimum key shares needed for reconstruction (2–16) |
| `min_shares` | `int64` | Band floor: minimum acceptances for the secret to activate (`threshold ≤ min_shares`) |
| `max_shares` | `int64` | Band ceiling: candidates selected and SSS shares generated (`min_shares ≤ max_shares ≤ 32`, `max − min < threshold`) |
| `secret_commitment` | `bytes` | SHA256 of the recipient-encrypted payload (the reconstruction target), for client-side verification (32 bytes). Stored, never verified on-chain — by design |
| `commit_deadline` | `int64` | Absolute height by which the complete three-phase commit must finish: `created_at + 50` (fixed, not creator-settable) |
| `state` | `string` | Current lifecycle state (see [State Values](#state-values)) |
| `reward_pool` | `Coin` | `P` — locked from the creator at request time. Fixed at the band ceiling: unfilled slots are never refunded; they enlarge the revealers' split |
| `accept_fees` | `Coin` | `A` — escrowed apart from the pool, distributed at the terminal state to the guardians that did the job asked of them |
| `created_at` | `int64` | Block height when the secret was created |
| `bump` | `int64` | Creator's security factor in hundredths (100–1,000); scales `P` and every bond together |
| `selected_guardians` | `[]string` | Guardian addresses chosen by Phase-1 hash sortition (`max_shares` entries, ascending-ticket order), immutable. The reservation event carries the seed inputs (height, previous block hash, counter) and the derived seed for verification |
| `guardian_bond_amounts` | `[]int64` | Per-guardian bonds in uveil, aligned index-for-index with `selected_guardians`; frozen at selection from each guardian's own live `k` and never re-priced |
| `accepted_count` | `int64` | Denormalised count of `ACCEPTED` assignments, up to `max_shares`; at finalisation the accepted set is the active set |
| `revealed_count` | `int64` | Denormalised count of revealed shares |
| `terminal_at` | `int64` | Height the secret reached a terminal state (revealed/cancelled/failed); `0` while live. Drives retention and the rebate collection deadline |
| `secret_public_key` | `bytes` | `pk_s` — the per-secret public key (32B), set at distribution. A reconstructed per-secret private key is publicly checkable against it (fault attribution) |
| `rebate_amount` | `int64` | Rebate credited to this secret's recipient at settlement, in uveil, reserved in the rebate pool until collected. Zero = none was credited |
| `rebate_collected` | `bool` | True once the rebate has been paid out; a positive uncollected amount is the only collectable condition |

#### Side-Stores (keyed `(secret_id, guardian_address)` unless noted)

Per-guardian data lives outside the secret record so each operation reads and writes only what it touches:

| Store | Value | Description |
|-------|-------|-------------|
| Share records (cold, immutable) | `SecretShareData { encrypted_share, share_hmac }` | Guardian's SSS key share encrypted to its epoch key (intrinsic SSS ID inside) plus the HMAC for slashing detection — written once at MsgUserDistributeShares |
| Assignment records (hot, tiny) | `AssignmentRecord { status, responded_at_block }` | Guardian's response state (`PROPOSED`/`ACCEPTED`/`REJECTED`) — created `PROPOSED` at distribution, flipped exactly once by MsgGuardianConfirmShares. A selected guardian that received no share has no record and can never accept |
| Reveal records | `RevealedShare { guardian_address, decrypted_share, revealed_at_block }` | One per revealed key-share envelope (HMAC verified at submission; invalid shares are rejected, not stored) — written at MsgGuardianRevealShare |
| Payload record (cold, immutable; keyed by `secret_id` alone) | `bytes` | The doubly-encrypted payload ciphertext `C` (≤ 4,216 bytes), written once at distribution — the only copy of the secret material on chain. Served by `Query/SecretPayload`; reconstruction combines ≥ `threshold` revealed key shares into the per-secret private key and decrypts `C` client-side |
| Rebate commitments (keyed `(secret_id, collector_address)`) | `RebateCommitmentRecord { commitment, committed_at }` | Commit–reveal front-running protection for rebate collection — see [Recipient Rebate](#recipient-rebate) |

Queries assemble the full per-secret view (`SecretView`) from the slim record and side-stores on demand; granular endpoints (`SecretMeta`, `SecretAssignments`, `SecretReveals`, `SecretShare`, `SecretPayload`) serve light clients without the assembly.

#### State Values

| State | Description | Valid Transitions |
|-------|-------------|-------------------|
| `reserved` | MsgUserRequestGuardians complete: guardians selected, pool and accept fees locked, awaiting encrypted shares | → `awaiting_acceptance`, `failed` |
| `awaiting_acceptance` | MsgUserDistributeShares complete: shares stored, awaiting guardian acceptances until the commit deadline | → `pending`, `failed` |
| `pending` | Roster finalised at the commit deadline with ≥ `min_shares` acceptances; awaiting reveals | → `reconstructable`, `failed`, `cancelled` |
| `reconstructable` | Reveal threshold met while the window is still open | → `revealed` |
| `revealed` | Reveal window closed with threshold met; rebate credited | Terminal |
| `cancelled` | Creator cancelled the activated secret before the window opened, with pro-rata guardian pay | Terminal |
| `failed` | Commit deadline passed without activation, or window closed below threshold | Terminal |

```
                      MsgUserDistributeShares       EndBlock: commit deadline, ≥ min_shares accepted
 [reserved] ────────────────────────▶ [awaiting_acceptance] ────────────▶ [pending]
     │                                       │                                │
     │ EndBlock: commit deadline             │ EndBlock: commit deadline,     │ MsgGuardianRevealShare (threshold met)
     ▼                                       ▼ < min_shares accepted          ▼
 [failed]◀───────────────────────────────[failed]                    [reconstructable]
     ▲                                                                        │
     │ EndBlock: window closed, < threshold                                   │ EndBlock: window closed
     └────────────────────────────────── [pending]                           ▼
                                                                         [revealed]
 [pending] ── MsgUserCancelSecret ──▶ [cancelled]   (post-activation only)
```

Terminal secrets are retained for ~6 months (`RetentionBlocks`) and then pruned to a permanent tombstone — see [Time-Based Operations](#time-based-operations-beginblockendblock).

---

### Operations

### MsgUserRequestGuardians

Initiate secret publication by reserving a secret with protocol-controlled guardian assignment (Phase 1). This locks the protocol-derived reward pool `P` and the separately escrowed accept fees `A` from the creator, charges the non-refundable creation fee, assigns a protocol-derived secret ID, and selects exactly `max_shares` guardians by hash sortition seeded entirely from consensus data. The creator supplies **no secret ID, no reward amount and no selection input of any kind** — pricing is protocol-derived and the only economic dial is `bump`. The recipient's public key is used client-side only (payload encryption and hint derivation) and is never submitted.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `creator` | `string` | ✓ | - | Valid bech32 address; transaction signer | Secret creator |
| `detection_hint` | `DetectionHint` | ✓ | - | Version 1; 32-byte ephemeral key (valid X25519 point); 8-byte tag. Content is deliberately unverifiable — random bytes are a valid no-discovery hint | Recipient discovery hint, derived client-side from the recipient's public key |
| `reveal_window.start_offset` | `int64` | ✓ | - | 100–5,256,000 blocks. The floor is the fixed commit window (50) plus the reveal buffer (50); additionally `start_offset + duration ≤ 5,256,000` (~1 year horizon) | Blocks from now until reveals may start |
| `reveal_window.duration` | `int64` | ✓ | - | 100–14,400 blocks (~10 minutes – 1 day) | Reveal window length |
| `threshold` | `int64` | ✓ | - | 2–16 | Key shares needed for reconstruction |
| `min_shares` | `int64` | ✓ | - | `threshold ≤ min_shares` | Band floor: minimum acceptances for activation |
| `max_shares` | `int64` | ✓ | - | `min_shares ≤ max_shares ≤ 32`, `max_shares − min_shares < threshold` (strict gap bound) | Band ceiling: guardians selected and shares distributed |
| `bump` | `int64` | ✓ | - | 100–1,000 (hundredths: 1.00–10.00) | Security factor — scales `P` and every guardian's bond together |

#### Preconditions

- Enough eligible guardians exist to fill `max_shares` — the transaction **fails whole** if fewer qualify (no reduced-band fallback), and a failed request charges nothing (the transaction aborts atomically, including the ID counter)
- The creator's balance covers `P + A + creation fee + gas`

#### Side Effects & Token Flow

- Creates the secret in `reserved` with `commit_deadline = current_block + 50` (fixed for every secret) and enqueues its deadline and settlement queue entries
- Transfers `P + A` creator → module escrow in one send (locked, stored as separate fields)
- Charges the **non-refundable creation fee** `max(60,000 uveil, P_time × bps(distance) ÷ 10,000)` creator → fee collector, where `P_time = rate × distance × max_shares × bump` is the pool's time component only (never the gas pass-through legs) and `bps` falls linearly from 1,000 (10%) at minimal distance to 500 (5%) at 432,000 blocks, flat beyond. The fee never enters escrow, so no refund path touches it
- Selects exactly `max_shares` guardians by hash sortition: `ticket = SHA256(seed ‖ guardian_address)`, lowest tickets win, with `seed = SHA256(chainID ‖ height ‖ lastBlockHash ‖ counter)`; each selected guardian's bond `B_g` is frozen from its live `k` and recorded on the secret
- Emits `secret_reserved` (includes the seed inputs and derived seed for public verification, plus `creation_fee` and `creation_fee_regime` = `floor`/`percent`)

#### Response

```go
type MsgUserRequestGuardiansResponse {
    SecretId            string           // Protocol-assigned (UUIDv5 over chainID ‖ counter)
    GuardianAssignments []GuardianInfo   // Exactly max_shares entries, in selection order
}

type GuardianInfo {
    Address    string   // Guardian's blockchain address
    PublicKey  bytes    // Guardian's current-epoch encryption public key (32 bytes)
}
```

**Client usage** (the TypeScript SDK automates all of this):

1. Store the response for Phase 2
2. Split the per-secret private key into `max_shares` SSS key shares (the share ID is intrinsic to the share data)
3. Encrypt each share to its guardian's `PublicKey`
4. Compute each share's HMAC over (secret ID, guardian address, share data) — the HMAC key is derived from those same public inputs, so the chain can verify evidence keylessly
5. Submit everything via [MsgUserDistributeShares](#msguserdistributeshares) before the commit deadline (~50 blocks — automate this; it is not a manual-pace window)

#### Examples

```bash
# Request guardians: threshold 5, band [6, 9], bump 1.00,
# reveals open 150 blocks from now for 150 blocks
timeflared tx secrets user-request-guardians \
  "$DETECTION_HINT" 5 6 9 100 150 150 \
  --from alice --output json

# Devnet/testing: --random-hint replaces the positional hint (no discovery;
# random bytes are indistinguishable from a real hint by design)
timeflared tx secrets user-request-guardians \
  --random-hint 5 6 9 100 150 150 \
  --from alice
```

`$DETECTION_HINT` is 80 hex characters: the 32-byte ephemeral public key followed by the 8-byte tag, derived from the recipient's public key by the SDK tooling. Read the assigned `secret_id` and guardian assignments from the transaction response or the `secret_reserved` event.

---

### MsgUserDistributeShares

Complete secret publication (Phase 2): store the payload ciphertext, deliver each guardian's encrypted key share with its HMAC, and record the secret commitment and per-secret public key. The secret transitions to `awaiting_acceptance`, giving guardians until the commit deadline to verify their shares offline and respond via [MsgGuardianConfirmShares](#msgguardianconfirmshares).

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `creator` | `string` | ✓ | - | Must match the Phase-1 creator; transaction signer | Secret creator |
| `secret_id` | `string` | ✓ | - | Must exist in `reserved` state | Secret from Phase 1 |
| `shares[].guardian_address` | `string` | ✓ | - | Must be a Phase-1-selected guardian; no duplicates | Guardian receiving this share |
| `shares[].encrypted_share` | `bytes` | ✓ | - | Non-empty, ≤ 128 bytes; encrypted to the guardian's epoch key | Encrypted key share (intrinsic SSS ID inside) |
| `shares[].share_hmac` | `bytes` | ✓ | - | Non-empty (32 bytes in practice) | HMAC for slashing detection |
| `secret_commitment` | `bytes` | ✓ | - | Non-empty (SHA256, 32 bytes) | Hash of the recipient-encrypted payload — the reconstruction target |
| `payload_ciphertext` | `bytes` | ✓ | - | Non-empty, ≤ 4,216 bytes | `C` — the doubly-encrypted payload, stored on chain exactly once |
| `secret_public_key` | `bytes` | ✓ | - | Exactly 32 bytes; valid X25519 point | `pk_s` — the per-secret public key, for fault attribution |

#### Preconditions

- Secret in `reserved`, `current_height ≤ commit_deadline`, sender is the creator
- At least `threshold` shares provided. The creator **may** distribute to fewer guardians than were selected (minimum = `threshold`), silently stranding the rest of the band — but distributing to fewer than `min_shares` dooms the secret to the commit-timeout; a selected guardian who receives no share gets no assignment record and can never accept

#### Side Effects & Token Flow

- The payload ciphertext is stored **exactly once** (the only copy of `C`, independent of guardian count)
- Per targeted guardian: one cold share record (encrypted share + HMAC) and one assignment record (`PROPOSED`)
- `secret_commitment` (stored, never verified on-chain — verification is wholly client-side, by design) and `secret_public_key` land on the record
- Secret → `awaiting_acceptance`; emits `secret_awaiting_acceptance`
- **No tokens move** — everything was locked at Phase 1

#### Examples

```bash
# shares.json: [{"guardian_address": "tmflr1...", "encrypted_share": "<hex>", "share_hmac": "<hex>"}, ...]
timeflared tx secrets user-distribute-shares \
  <secret-id> \
  shares.json \
  <64-hex-char-secret-commitment> \
  payload.bin \
  <64-hex-char-secret-public-key> \
  --from alice
```

`payload.bin` may be a file path (raw bytes) or a hex string. In practice the TypeScript SDK performs the encryption, splitting, HMAC generation and both Phase-1/Phase-2 submissions as one flow.

---

### MsgSlashGuardian

Report a guardian for revealing its assigned key share before the reveal window opens. Anyone holding the leaked plaintext share can report: the evidence is verified against the HMAC stored at distribution, proving the share left the guardian's custody early. A successful report slashes the guardian's **entire per-secret bond** `B_g` immediately, splits it between reporter, creator and burn, and salvages the leaked share as the guardian's reveal.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `guardian_address` | `string` | ✓ | - | Valid bech32 address; must hold an `ACCEPTED` assignment for the secret | Guardian being reported |
| `evidence` | `bytes` | ✓ | - | 32–64 bytes; must pass HMAC verification against the stored assignment HMAC | The guardian's decrypted key share, proving early possession |
| `reporter_address` | `string` | ✓ | - | Valid bech32 address; ≠ `guardian_address` | Reporter (transaction signer); receives the bounty |
| `reason` | `string` | ✓ | - | Non-empty | Human-readable slash reason |
| `secret_id` | `string` | ✓ | - | Must exist with the guardian assigned | Secret the evidence relates to |

#### Preconditions

- `current_height < reveal_start_block` — reports close when the window opens (possession of the plaintext share is only proof of misconduct *before* reveals are legal)
- The guardian's assignment is `ACCEPTED` (only accepted assignments have a locked bond) and it has not already revealed on-chain, nor already been slashed for this secret
- Reporter is not the accused guardian
- Evidence passes HMAC verification — the reporter must present the guardian's actual decrypted share

#### Side Effects & Token Flow

- The guardian's full frozen bond `B_g` is deducted from its float, its active-bond slot is released, and its multiplier steps **up**: `k′ = min(24.00, k × 1.26)` — every future acceptance now costs it more
- Bond split: **50% → reporter (bounty) / 10% → creator (compensation) / 40% → burned**. The reporter takes the remainder, so the split is dust-free; an unresolvable creator's slice joins the burn
- The guardian is marked slashed for this secret: excluded from the reward split, the accept-fee slice **and** bond return at settlement
- The evidence is auto-submitted as the guardian's reveal (HMAC-verified) so the share is not lost and the guardian cannot also be slashed as a no-show; this internal path deliberately does **not** step `k` down. The auto-reveal counts toward `revealed_count`, so enough reports can carry a secret to `reconstructable` before its window ever opens
- The guardian record persists
- Emits `guardian_slashed` (`slash_type=early_reveal`), `guardian_bond_k_adjusted`, and the auto-reveal's `share_revealed`

#### Examples

```bash
# Report an early reveal with the leaked share as evidence (hex, base64, or file path)
timeflared tx secrets slash-guardian \
  tmflr1guardian... \
  <hex-encoded-leaked-share> \
  "share posted publicly before reveal window" \
  <secret-id> \
  --from reporter-account
```

---

### MsgUserCancelSecret

Cancel an **activated** secret (`pending` state) before its reveal window opens. Cancellation is a post-activation mechanic: it exists to release bonded guardians via a paid pro-rata exit, so it is valid **only from `pending`**. Pre-activation secrets (`reserved`, `awaiting_acceptance`) cannot be cancelled — they exit via the commit-timeout, which refunds the pool in full automatically while still reimbursing every guardian that accepted.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `secret_id` | `string` | ✓ | - | Must exist in `pending` state; `current_height < reveal_start_block` | Secret to cancel |
| `creator` | `string` | ✓ | - | Must match the secret's creator; transaction signer | Creator requesting cancellation |

#### Preconditions

- Sender is the creator; secret is in `pending` **only** (pre-activation: wait for the commit-timeout; reconstructable/terminal: too late)
- `current_height < reveal_start_block` — cancellation is legal up to the block before the window opens; pro-rata pay is what makes late cancellation non-abusive

#### Side Effects & Token Flow

- Secret → `cancelled`; all further reveals blocked; the settlement-queue entry is dropped
- Every accepted guardian's bond is unlocked in full and its active-bond slot released (early-slashed guardians excluded — their bond is already gone)
- Each active guardian is paid a pro-rata wage `P × max(elapsed, 0) ÷ (distance × max_shares)`, where `elapsed = cancel_height − commit_deadline` — derived from the **stored** pool, so the wage accrues both the time component and the reveal leg, and no upgrade can re-price an in-flight cancellation. Cancelling immediately after activation (before the deadline has passed) pays zero wages and refunds everything
- Each non-slashed acceptor is additionally reimbursed its full `A ÷ max_shares` accept-fee slice — accepting is work that happened, whatever the creator later decided
- The creator receives `P − Σ wages` plus every unearned `A` slice; the `max_shares` denominator keeps the per-guardian wage constant regardless of how many accepted, so unfilled band slots refund to the creator. An early-slashed guardian's wage and fee slices stay in the creator remainder
- Emits `guardian_cancellation_payout` × n, `guardian_accept_fee_paid` × n, `accept_fees_refunded`, `secret_cancelled`

#### Examples

```bash
# Cancel an activated secret before its reveal window opens
timeflared tx secrets user-cancel-secret <secret-id> --from alice
```

---

## Recipient Rebate

A secret that reaches `revealed` credits its **recipient** a rebate on what the creator irrecoverably spent to send it — the protocol's only distribution mechanism, paid from a keyless module account (`rebate_pool`, 70% of supply at genesis) that only the rebate formula can spend. The credit happens automatically at settlement (see [Time-Based Operations](#time-based-operations-beginblockendblock)); collecting it takes two transactions, because the proof of recipiency is a bearer secret that must be bound to the collecting address *before* it becomes public. See spec.md "Recipient Rebate" for the full model.

- **Amount**: `min(30% × S, allowance ÷ n)`, where `S` is the creator's irrecoverable spend and `n` the number of secrets settling at that height. Amounts below the dust floor (50,000 uveil = 0.05 VEIL) are not credited at all
- **Collection window**: 1,296,000 blocks (~3 months) from `terminal_at`, clamped to the retention window. At the deadline the rebate is voided and its reservation returns to the pool
- **⚠️ Collecting is public and permanent**: submitting the proof links the collecting address to that secret, and an address collecting on several secrets links those secrets to one another — a deliberate, opt-in exception to recipient privacy. A recipient who wants a secret to stay unlinkable should not collect its rebate, or should collect to a single-use address

### Operations

### MsgRecipientCommitRebate

Publish the commitment binding a recipiency proof to the collecting address (step 1 of 2). The commitment is `SHA256("timeflare/rebate-commit/v1" ‖ z ‖ collector address bytes)`, where `z = X25519(recipient private key, hint ephemeral key)` is the recipiency proof. The chain stores it opaquely — it cannot tell a real commitment from random bytes, and does not need to: its only job is to exist **before** the proof becomes public, so a front-runner who lifts the proof from a later reveal transaction has no commitment for it and cannot backdate one.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `recipient` | `string` | ✓ | - | Valid bech32 address; transaction signer | Address that will collect; the commitment binds to it |
| `secret_id` | `string` | ✓ | - | Secret must exist with an uncollected positive `rebate_amount`, inside the collection window | Secret whose rebate is being committed to |
| `commitment` | `bytes` | ✓ | - | Exactly 32 bytes | `SHA256(domain ‖ z ‖ recipient address bytes)` |

#### Preconditions

- The secret exists with `rebate_amount > 0`, not yet collected, and `current_height ≤ terminal_at + collection window`

#### Side Effects & Token Flow

- The commitment is recorded with the height it arrived at; the reveal must land in a **strictly later block**
- Re-committing overwrites — a collector who mistyped simply commits again and waits another block
- **No tokens move**
- Emits `rebate_committed { secret_id, recipient }`. The response carries `committed_at`

#### Examples

```bash
# Step 1: commit to collecting (commitment computed client-side from the proof z and your address)
timeflared tx secrets recipient-commit-rebate <secret-id> <64-hex-char-commitment> --from recipient
```

---

### MsgRecipientCollectRebate

Reveal the recipiency proof and collect the rebate (step 2 of 2). Pays the credited amount to the **signer**, which must be the address that committed to this proof in an earlier block. A credited rebate keeps its amount until collected — collecting a month later pays exactly what collecting immediately would.

#### Fields

| Field | Type | Required | Default | Validation | Description |
|-------|------|----------|---------|------------|-------------|
| `recipient` | `string` | ✓ | - | Valid bech32 address; transaction signer | Address collecting; the rebate is paid here |
| `secret_id` | `string` | ✓ | - | Secret must exist with an uncollected positive `rebate_amount`, inside the collection window | Secret whose rebate is being collected |
| `z` | `bytes` | ✓ | - | Exactly 32 bytes; must reproduce both the hint tag and the signer's commitment | `z = X25519(recipient private key, hint ephemeral key R)` — the recipiency proof |

#### Preconditions

- The secret exists with `rebate_amount > 0`, not yet collected, and `current_height ≤ terminal_at + collection window`
- **Recipiency**: `SHA256("timeflare/detect/v1" ‖ z)[:8]` equals the secret's stored hint tag
- **Priority**: the signer holds a commitment recorded in a **strictly earlier block**, and `z` with the signer's address reproduces it

#### Side Effects & Token Flow

- `rebate_amount` transferred `rebate_pool` → recipient; the secret is marked `rebate_collected` and the pool's reservation is released
- The spent commitment is deleted (losing commitments are swept when the secret is pruned)
- Emits `rebate_collected { secret_id, recipient, amount }`. The response carries the amount paid

#### Examples

```bash
# Step 2 (a later block): reveal the proof and collect
timeflared tx secrets recipient-collect-rebate <secret-id> <64-hex-char-proof-z> --from recipient
```

---

## Time-Based Operations (BeginBlock/EndBlock)

Block-driven operations run automatically — no transaction invokes them. All queue processing is due-height driven: an idle block does almost no work. Failures on these paths are quarantined per secret (partial state discarded, the entry retried every block, a `settlement_stalled` event and error log raised from the first failure) — never a chain halt. [CHAIN_MECHANICS.md](CHAIN_MECHANICS.md) carries the performance shape and open concerns of each path.

### Fee Split (BeginBlock)

The previous block's collected transaction fees are split per denom: **10% burned** (division dust joins the burn), **90% left for the standard distribution module** to allocate to validators by bonded voting power. This covers every fee-collector inflow — gas fees, creation fees and guardian entry fees. Emits `fee_distribution`.

### Commit-Deadline Finalisation (`commit_deadline + 1`)

The band's single decision point — the roster finalises here, never mid-window:

- **≥ `min_shares` accepted** → **activation**: secret → `pending` with exactly the accepted set; emits `secret_pending`
- **Still `reserved`, or below `min_shares`** → **failure**: full reward pool refunded to the creator; one `A ÷ max_shares` slice paid to every guardian that did accept (it did what was asked — no `k` moves on this path: nobody failed); every unearned slice refunded; all accepted bonds unlocked and slots released; secret → `failed`. Emits `secret_commit_timeout`, `guardian_bond_released` × n, `guardian_accept_fee_paid` × n, `accept_fees_refunded`. The full refund is by design — the per-draw price is the creation fee already charged at Phase 1

### Settlement (`reveal_end_block + 1`)

Settlement is **threshold-independent**: the threshold decides only the final state, never who gets paid.

1. **Partition** the accepted guardians into revealers and no-shows (early-slashed guardians excluded from both — their bond is already gone)
2. **Revealers**: bond unlocked in full, slot released; `k` already stepped down at each reveal
3. **No-shows**: bond slashed **40% burn / 10% creator / 50% returned**; slot released; `k` stepped **up**
4. **State**: threshold met → `revealed`; below threshold → `failed`
5. **Reward pool**: zero revealers → full refund to the creator; otherwise split **equally among revealers** (no-shows' forfeited slices enrich the revealers, not the creator; division dust is burned)
6. **Accept fees**: one `A ÷ max_shares` slice to each revealer; every other slice refunded to the creator (an acceptor that no-showed keeps nothing, including its own gas)
7. **Rebate credit**: every secret settling at this height shares the rebate pool's accrued allowance equally; each one that reached `revealed` is credited `min(30% × S, allowance ÷ n)` (zero below the dust floor) as `rebate_amount`, reserved in the pool until collected. Secrets that settled `failed` are counted in the divisor but credited nothing — deliberately, so the amounts do not leak how many secrets at a height had a real recipient. Emits `rebate_credited`

### Rebate Expiry (`terminal_at + collection window`)

An uncollected rebate is **voided** at its deadline: the credited amount returns to zero and the reservation returns to the pool, so unclaimed adoption funding funds the next newcomer instead of sitting reserved. Emits `rebate_expired`.

### Retention Pruning (`terminal_at + RetentionBlocks`, ~6 months)

Two-stage retention keeps live state proportional to *active* secrets:

- **Stage 1** (at the terminal transition): encrypted shares, assignment records and slash marks deleted; the prune scheduled
- **Stage 2** (at the retention deadline, capped at 50 prunes per block): everything else deleted — slim record, reveal records, payload ciphertext, creator-index and hint entries, rebate commitments — replaced by a permanent ~180-byte `SecretTombstone` and an archival `secret_pruned` event carrying the full canonical record (base64) plus its digest, so indexers that retain events hold a complete, self-verifying archive

---

## Funds Flow Summary

| Trigger | From → To | Amount |
|---|---|---|
| UserRequestGuardians | creator → module | reward pool `P = (max_shares × F_reveal) + (rate × distance × max_shares × bump)` |
| UserRequestGuardians | creator → module | accept fees `A = max_shares × F_accept`, escrowed **apart from** `P` |
| UserRequestGuardians | creator → **fee collector** → 90/10 split | creation fee `max(60,000 uveil, P_time × bps(distance) ÷ 10,000)` — charged on the time component only; non-refundable, never escrowed |
| GuardianRegister | guardian → **fee collector** → 90/10 split | entry fee 1,000 VEIL (900 allocated to validators, 100 burned next block) |
| Register/GuardianUpdate deposit | guardian → module | float deposit (escrowed) |
| GuardianRotateKey | guardian → module → **burn** | flat rotation fee `rate × 14,400` (one guardian-day) |
| GuardianConfirmShares (accept) | unlocked → locked float | the guardian's frozen bond `B_g = rate × distance × bump × k_g` (internal) |
| Settlement — revealer | locked → unlocked float; module → guardian | full bond back; `P ÷ revealers` (equal split); one slice `A ÷ max_shares` |
| Settlement — no-show | bond split | **40% burn** / 10% → creator / 50% unlocked back |
| Settlement — zero revealers | module → creator | full pool refund; full `A` refund |
| Settlement — division dust | module → **burn** | `P − Σ distributed` |
| Commit deadline expiry | module → guardians; module → creator; locked → unlocked | one slice `A ÷ max_shares` to each guardian that accepted; full pool refund plus every unearned `A` slice; all accepted bonds released |
| Cancel | locked → unlocked; module → guardians; module → creator | bonds released; `rate × elapsed × bump` each plus the accrued reveal leg and a full `A` slice; remainder |
| Early-reveal slash | bond split | 50% → reporter / 10% → creator (or burn) / **40% burn** |
| GuardianWithdrawStake | module → guardian | entire unlocked float |
| Settlement — rebate credit | reservation within `rebate_pool` (no transfer) | `min(30% × irrecoverable spend, allowance ÷ settling count)`, zero below the 50,000 uveil dust floor |
| RecipientCollectRebate | **`rebate_pool`** → recipient | the credited `rebate_amount`, exactly as booked at settlement |
| Rebate expiry / prune of uncollected rebate | reservation released (no transfer) | credited amount returns to the pool's spendable balance |


## Hard-Coded Protocol Values

Every arbitrary value decided in code. None are governance parameters — changing
any requires a chain software upgrade. Block-duration comments assume the ~6s
production block time (`x/secrets/types/constants.go` unless noted).

### Timing

| Value | Constant | Purpose |
|---|---|---|
| 50 blocks (~5 min) | `CommitTimeoutBlocks` | The single `commit_deadline` covering all three commit phases; fixed for every secret, not creator-settable |
| +50 blocks (~5 min) | `MinRevealStartOffset` | Buffer between commit deadline and earliest reveal start; with the commit window fixed, the `start_offset` floor is a constant 100 |
| 5,256,000 blocks (~1 year) | `MaxRevealHorizon` (= `MaxRevealStartOffset` = `MaxAvailabilityWindow`) | Furthest `reveal_end_block` can lie from creation; deliberately equals the availability cap so every valid window can be staffed |
| 100–14,400 blocks (10 min – 1 day) | `MinRevealDuration` / `MaxRevealDuration` | Reveal window size |
| 100 blocks (~10 min) | `MinAvailabilityWindow` | Minimum guardian registration window |
| +1 block / ≤ 2,628,000 blocks (~6 months) | `MinAvailableFromOffset` / `MaxAvailableFromOffset` | Guardian availability start bounds |
| 432,000 blocks (~30 days) | `KeyRotationMinIntervalBlocks` | Minimum spacing between a guardian's key rotations, from the newest epoch's effective height (epoch 0 starts the clock); bounds history growth to ~12 entries/guardian-year. Dev-override via `TIMEFLARE_KEY_ROTATION_MIN_INTERVAL` (see note below) |
| 2,592,000 blocks (~6 months) | `RetentionBlocks` (`MaxPrunesPerBlock` = 50) | Stage 2 retention delay from `terminal_at`. Dev-override via `TIMEFLARE_RETENTION_BLOCKS` (see note below) |

> **Dev/test overrides**: the two override variables above exist so the
> e2e-scenarios suite can exercise month-scale mechanics in minutes. Both are
> ⚠️ consensus-critical — every node in a network must agree — so
> `timeflared start` **refuses to boot** while either is set unless the
> operator passes `--unsafe-dev-overrides`; the devnet targets pass the flag
> automatically when (and only when) an override is active.

### Secret shape & guardian selection

| Value | Constant / location | Purpose |
|---|---|---|
| 2–16 | `MinThreshold` / `MaxThreshold` | SSS reconstruction threshold bounds |
| 2–32 | `MinShares` / `MaxTotalShares` | Band bounds: `threshold ≤ min_shares ≤ max_shares ≤ 32` (SSS limit); each of the `max_shares` selected receives a real unique SSS share |
| `max − min < threshold` (strict) | gap bound (`ValidateBasic`) | On any activated secret the never-confirmed candidates are a sub-threshold set — they can never reconstruct alone (conditional on activation) |
| `≥ min_shares` acceptances at the deadline | finalisation predicate (`endblock_logic.go`) | Activation trigger — the roster finalises at `commit_deadline` with exactly the accepted set; no first-`n` race |
| UUIDv5(chainID ‖ counter) | `keeper.go` (`DeriveSecretId`) | Protocol-assigned secret ID from the monotonic `SecretCounter` (genesis-exported) |
| SHA256 sortition | `guardian_selection.go` | Selection: seed from consensus data only; lowest-ticket-wins, address tie-break. **Uniform by construction** — because the seed differs per secret, every eligible guardian draws a fresh uniform ticket each time, so its probability is `max_shares/G` regardless of address, and load over many secrets is Binomial (proven by the chi-squared suite in `guardian_selection_sortition_test.go`). Float size and `k` buy **no** selection advantage: `k` prices bonds, never candidacy. Anything "smarter" than uniform (float-weighting, reputation) would *reduce* evenness and create selection-pressure targets — avoid unless a concrete problem demands it |
| 4,216 bytes | `MaxPayloadSize` | Cap on the stored payload ciphertext `C` (4,096 B secret + two 60 B layers) — stored ONCE per secret, independent of guardian count ([planning/done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](planning/done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md); 64 KB measured viable, §9.2) |
| 128 bytes | `MaxKeyShareSize` | Cap on a guardian's encrypted key share (34 B envelope + 60 B overhead, headroom for future versions) |
| 64 bytes | `MaxRevealedKeyShareSize` | Cap on the plaintext key-share envelope at reveal and as slash evidence |
| 36 chars (UUID) | `SecretIdLength` | Secret ID format |

### Economics

All amounts derive from one master knob; nothing else is hard-coded
(`economics.go`).

| Value | Constant | Purpose |
|---|---|---|
| 1 uveil / guardian / block | `RatePerGuardianBlock` | Master price knob at bump 1.00 |
| 120,000 / 130,000 gas | `GuardianAcceptGas` / `GuardianRevealGas` | The guardian's two obliged transactions, denominated in GAS and priced at the consensus floor: `F_accept = MinRequiredFee(120,000)` = 12,000 uveil, `F_reveal = MinRequiredFee(130,000)` = 13,000 uveil. Observations of the protocol's own code path (measured 110,314 / 118,984, rounded up), not policy knobs — the creator funds both so revenue can never fall below cost on a short secret |
| `A = max_shares × F_accept` | `AcceptFeesAmount` | Acceptance reimbursement, escrowed **apart from** `P` because the two are earned by different acts and settle on different rules (paid where `P` refunds at commit expiry; paid in full where `P` accrues pro-rata at cancellation). Divides exactly by `max_shares`, so every payout is whole uveil from stored state |
| 400–2400 (÷100) | `MinBondK` / `MaxBondK` (`InitialBondK = 400`) | Per-guardian bond multiplier k, 4.00–24.00; ×1.26 per slash, ×0.963 per correct reveal (truncating, clamped) |
| 100 | `MaxActiveBondsPerGuardian` | Concurrency cap: eligibility gate at selection, re-checked at bond lock |
| 1,000 VEIL | `EntryFeeAmount` | Registration entry fee — rides the 90/10 fee split (900 validators / 100 burn), never returned |
| `rate × 14,400` (one guardian-day) | `KeyRotationFeeBlocks` / `KeyRotationFee` | Flat burned fee per key rotation — anti-spam pricing of the permanent history entry, derived from the master rate |
| 100–1,000 (÷100) | `MinBump` / `MaxBump` (`BumpScale = 100`, `MaxTier = 10`) | Creator's single security dial 1.00–10.00; scales `P` and `B` together |
| `P = (max_shares × F_reveal) + (rate × distance × max_shares × bump)` | `RewardPoolAmount` (`TimeComponentAmount` = the second term, `P_time`) | Reward pool: the reveal transaction the guardian must send, whose gas is the same at every distance, plus the time it holds the share. `distance = (reveal_end + 1) − commit_deadline`, priced on the band ceiling and fixed — unfilled slots are never refunded |
| `B = rate × distance × bump × k` | `BondAmount` | Per-guardian, per-secret bond, frozen at selection from the guardian's live k |
| 1,000 / 500 bps, 432,000 blocks | `CreationFeeMaxBps` / `CreationFeeMinBps` / `CreationFeeCurveEndBlocks` | Creation-fee percentage curve: linear 10% → 5% over 30 days of distance, flat beyond |
| `⌈600,000 gas × 0.1 uveil⌉` = 60,000 uveil | `CreationFeeFloorGas` (floor via `MinRequiredFee`) | Creation-fee floor — three reference 200k-gas transactions; the anti-grinding draw price, tracking any gas-floor retune automatically |
| `max(floor, P_time × bps(d) ÷ 10,000)` | `CreationFee` (`economics.go`) | Non-refundable request-time fee, charged to the fee collector (90/10 split), never escrowed. Priced on the time component only — taxing the gas pass-through would route part of every guardian's reimbursement to validators and the burn |
| `P × elapsed ÷ (distance × max_shares)` | `ProRataCancellationPayout` | Per-guardian cancellation wage, `elapsed = height − commit_deadline`, floored at 0. Derived from the **stored** pool, never the live rate constant, so upgrades cannot re-price in-flight cancellations — and because `P` carries the reveal leg, that leg accrues with the hold exactly as the wage does. The `max_shares` denominator keeps the wage constant regardless of accepted count, with unfilled slots refunding to the creator |
| 90% / 10% | `FeeValidatorPercent` / `FeeBurnPercent` (`constants.go`) | Transaction-fee split, applied every block at BeginBlock (validators / burned) |
| 30% | `RebateRatioPercent` | Ceiling on a recipient rebate as a share of the creator's irrecoverable spend — manufacturing a rebate costs `S` to receive at most `0.30 × S`, so farming is a loss at any token price |
| balance ÷ 50,000,000 / block | `RebateAccrualDivisor` | Rebate-pool allowance accrual — proportional to the pool's own balance, so the pool decays geometrically (≤ 10% of remaining balance per year, fully claimed) and never empties |
| 14,400 blocks (~1 day) | `RebateBurstBlocks` | Cap on accumulated unclaimed allowance — lets a lone recipient collect a full rebate without an idle stretch becoming a drainable lump |
| 50,000 uveil (0.05 VEIL) | `RebateDustFloor` | Smallest rebate worth crediting — five times the gas of the transaction that collects it |
| 1,296,000 blocks (~3 months) | `RebateCollectionBlocks` (`RebateCollectionWindow()` clamps to live retention) | How long a credited rebate stays collectable from `terminal_at`; must always close before pruning deletes the hint the proof is checked against |
| 0.1 uveil/gas | `MinGasPriceUveilNum` / `MinGasPriceUveilDen` | Consensus-enforced fee floor: the ante chain (`app/ante.go`) rejects any tx paying under `⌈gas × 1 ÷ 10⌉ uveil` in CheckTx AND DeliverTx (genesis height 0 and simulate exempt) — the node-config `minimum-gas-prices` is only the mempool knob above it |

### Slashing

Every penalty is a **percentage of the posted per-secret bond** — slashing can
never fail for insufficient collateral, because the bond is locked at acceptance.
Each split sums to 100; the burn share is always > 0 and the creator share always
< 100 (self-dealing invariant).

| Violation | Split | Constant group |
|---|---|---|
| No-reveal (automatic, at settlement) | **40% burn / 10% creator / 50% returned** | `NoReveal*Percent` |
| Early reveal (reported, immediate, full bond) | **40% burn / 10% creator / 50% reporter** (0 returned) | `EarlyReveal*Percent` |
| — | evidence ≥ 32 bytes | `MinEvidenceLength` |

### Cryptographic bindings

| Value | Location | Purpose |
|---|---|---|
| `SHA256("secrets" ‖ secretID ‖ guardianAddr ‖ "hmac_salt")` | `crypto/hmac.go` (pure Go — chain + guardian) and `rust/src/utils.rs` (WASM/TS SDK) | HMAC key derivation — deliberately public inputs so the chain can verify evidence keylessly ([CHAIN_MECHANICS.md Trade-off §2](CHAIN_MECHANICS.md#accepted-trade-offs)) |
| 32 bytes | `PublicKeyLength` / HMAC output | Guardian/recipient key size and SHA256-HMAC size |
| 32 bytes, not a small-order point | `crypto.ValidateX25519PublicKey`, called from `keeper/validation.go` and asserted by invariant 8 | Every X25519 key the protocol accepts must yield a **contributory** exchange — a small-order point produces an all-zero shared secret, making any derived key publicly computable. Enforced at guardian registration, key rotation, `pk_s`, the detection hint's `ephemeral_pub`, and the genesis path (hard halt). Delegated to `curve25519.X25519` rather than a blacklist, so non-canonical encodings are covered too; pinned across implementations by `testdata/vectors/low_order_keys.json` |

Server-side crypto is **pure Go** (`crypto/`, no cgo); `rust/` is the WASM
implementation serving the TypeScript SDK, and is not in the chain's build path.
Byte-compatibility between the two is pinned by the shared append-only
`testdata/vectors/` corpus, asserted by both test suites.


---

## Queries

Every query RPC has a CLI verb under `timeflared query secrets` and a REST path under `/timeflare/secrets/v1`. Paginated queries take the standard Cosmos SDK pagination flags.

| CLI command | REST path | Returns |
|-------------|-----------|---------|
| `secret <secret-id>` (alias `show`) | `/secret/{secret_id}` | A secret's full assembled view (record joined with assignments and reveals) |
| `secrets` (alias `list-secrets`) | `/secrets` | All secrets (assembled views, paginated) |
| `secrets-by-creator <creator>` | `/secrets/creator/{creator}` | A creator's secrets (assembled views, paginated) |
| `pending-secrets` | `/secrets/pending` | Secrets in their active reveal phase (`pending`/`reconstructable`) |
| `secret-meta <secret-id>` | `/secret/{secret_id}/meta` | Only the slim metadata record — the cheap call for light clients |
| `secret-assignments <secret-id>` | `/secret/{secret_id}/assignments` | Per-guardian assignment status records (no share bytes) |
| `secret-reveals <secret-id>` | `/secret/{secret_id}/reveals` | The revealed key shares — what a recipient needs to reconstruct |
| `secret-share <secret-id> <guardian-address>` | `/secret/{secret_id}/share/{guardian_address}` | A single guardian's encrypted share — what a guardian daemon needs |
| `secret-payload <secret-id>` | `/secret/{secret_id}/payload` | The stored payload ciphertext `C` — needed alongside the reveals for reconstruction |
| `secret-tombstone <secret-id>` | `/secret/{secret_id}/tombstone` | The permanent tombstone of a pruned secret — distinguishes "pruned" from "never existed" |
| `hints-since <since-height>` | `/hints/{since_height}` | Compact `(secret_id, detection_hint)` tuples for secrets created at or after the height, in creation order — the incremental discovery-scan cursor. Hints are deleted at pruning, so recipients should scan at least every six months |
| `guardian <address>` (alias `show-guardian`) | `/guardian/{address}` | A guardian by address |
| `guardians` (alias `list-guardians`) | `/guardians` | All guardians (paginated) |
| `guardian-key-history <address>` | `/guardian/{address}/key-history` | The guardian's key-epoch history in epoch order (epoch 0 = the registration key) |

```bash
# Examples
timeflared query secrets secret 1b671a64-40d5-491e-99b0-da01ff1f3341
timeflared query secrets secret-reveals 1b671a64-40d5-491e-99b0-da01ff1f3341
timeflared query secrets guardians
timeflared query secrets hints-since 120000
```

---

This specification defines all operations, required fields, validation rules, preconditions and token flows for the Timeflare protocol. The gRPC services are the source of truth and the CLI covers them 1:1 by construction (AutoCLI); protocol rationale lives in [spec.md](spec.md) and the implementation internals and judgement ledger in [CHAIN_MECHANICS.md](CHAIN_MECHANICS.md).
