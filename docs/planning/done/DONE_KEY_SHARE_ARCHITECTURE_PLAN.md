# Key-Share Architecture — Split the Key, Not the Payload

*Replace payload-splitting with key-splitting: each secret gets its own ephemeral
X25519 keypair; the payload ciphertext is stored on chain once; guardians custody
Shamir shares of the per-secret private key instead of shares of the payload.
On-chain cost falls from `O(secret_size × guardians)` to
`O(secret_size + guardians)` — a ~20× reduction at current caps.*

> **Status: IMPLEMENTED (July 2026).** Merged via
> [PR #37](https://github.com/leedavis81/timeflare/pull/37): Rust
> `seal_secret`/`unseal_secret` with WASM exports, the `SecretPayloads` store,
> `MsgDistributeShares` payload + `pk_s`, the re-cut size caps, the granular
> `Query/SecretPayload` endpoint, TypeScript SDK seal/unseal flows, spec.md sync
> including the large-secret envelope pattern, and the §9.2 measurement
> evidence (lifecycle verified at 1KB/16KB/64KB; guardian daemon unchanged;
> 26/26 scenario assertions). Recipient-metadata sizing was extracted to
> [DONE_RECIPIENT_PRIVACY_PLAN.md](DONE_RECIPIENT_PRIVACY_PLAN.md), which
> is the next branch. This document remains as the design rationale and
> decision log.

## Contents

1. [Motivation — the measured facts](#1-motivation--the-measured-facts)
2. [Current cryptographic flow](#2-current-cryptographic-flow)
3. [Proposed flow: two independent layers](#3-proposed-flow-two-independent-layers)
4. [How the per-secret keypair plays with recipient encryption](#4-how-the-per-secret-keypair-plays-with-recipient-encryption)
5. [What the chain sees — protocol changes](#5-what-the-chain-sees--protocol-changes)
6. [Impact per operation](#6-impact-per-operation)
7. [Surface area and sequencing](#7-surface-area-and-sequencing)
8. [Security analysis](#8-security-analysis)
9. [Open questions](#9-open-questions)

---

## 1. Motivation — the measured facts

Two properties verified in `rust/src/`:

1. **A GF(256) SSS share is byte-for-byte the same size as its input**
   (`sss.rs`: per-byte polynomials; `share.data.len() == secret.len()`). Splitting
   a 4KB ciphertext into 32 shares puts 128KB of share data on chain — the payload
   32 times over.
2. **Share encryption overhead is exactly 60 bytes per layer**
   (`crypto.rs`: ephemeral X25519 public key 32B + nonce 12B + Poly1305 tag 16B).

So today's `MaxEncryptedShareSize = 4KB` cap bounds the original secret to ~3,976B,
and the cap exists for good reason: `MsgDistributeShares` carries every share in one
transaction (32 × 4KB ≈ 131KB against CometBFT's default 1MB `max_tx_bytes`), and
each share is stored in full. Raising the cap multiplies by up to 32 through the
transaction, the block, and the store. The way out is architectural: make share
size **independent of secret size**.

## 2. Current cryptographic flow

```
payload
  → C_r = Enc_recipient(payload)          // X25519+ChaCha20-Poly1305, +60B
  → SSS-split C_r into n shares           // each |C_r| bytes
  → per guardian: Enc_guardian(share_i)   // +60B each
commitment = SHA256(C_r)
```

Reveal window: guardians reveal decrypted shares (each |C_r| bytes, HMAC-verified);
the recipient combines ≥t shares → C_r, checks the commitment, decrypts with their
long-lived key. Note what the time-lock actually is: *the recipient cannot obtain
C_r before the window because the guardians hold it in pieces.*

## 3. Proposed flow: two independent layers

The creator generates a fresh, single-use X25519 keypair `(pk_s, sk_s)` **per
secret** at distribution time, client-side:

```
payload
  → C_r = Enc_recipient(payload)          // INNER layer — unchanged from today
  → C   = Enc_pk_s(C_r)                   // OUTER layer — the time-lock, +60B
  → SSS-split sk_s (32 bytes) into n key shares   // each 34B enveloped (§9.3)
  → per guardian: Enc_guardian(key_share_i)       // ≈ 94B each
store on chain: C (once) + n encrypted key shares
commitment = SHA256(C_r)                  // unchanged
discard sk_s and pk_s                     // creator keeps nothing (§8)
```

Reveal window: guardians reveal decrypted **key shares** (~34B each,
HMAC-verified exactly as today — see the entropy note in §8). Anyone can combine
≥t key shares → `sk_s`, strip the outer layer of the on-chain `C` → `C_r`, and
verify `SHA256(C_r)` against the commitment. Only the recipient can go the final
step to the payload.

No new cryptographic primitives: both layers are the existing
`encrypt_for_public_key` (ephemeral-static X25519 + ChaCha20-Poly1305), and the key
split is the existing `split_secret` applied to 32 bytes. The change is pure
composition of audited pieces.

**Scalar representation (normative).** The bytes split are the raw 32-byte X25519
scalar exactly as emitted by keypair generation (x25519-dalek `StaticSecret`
bytes; clamping is applied at Diffie–Hellman time, so the bytes round-trip
exactly through split → combine). This makes reconstruction byte-exact across
clients and makes the `pk_s` consistency check (§9.4) well defined. spec.md must
state this.

### End-to-end data flow — creator to reveal

The flow rides the existing three-phase commit unchanged; only the *contents* of
the blobs change. First the notation, then the steps.

#### Notation

| Symbol | What it is | Size | Where it lives | Who can read it |
|---|---|---|---|---|
| `payload` | The plaintext secret being time-locked | ≤ ~3.9KB | Creator's device only (recipient's device at the very end) | Creator; recipient after step 12 |
| `recipient_public_key` | Recipient's long-lived X25519 public key (existing field, unchanged) | 32B | Slim `Secret` record | Public |
| `C_r` | `payload` encrypted to the recipient — the **inner** layer | payload + 60B | Never stored on chain; derivable by anyone post-window | Anyone can *derive* it post-window; only the recipient can decrypt it |
| `commitment` | `SHA256(C_r)` — lets anyone verify a reconstruction | 32B | Slim `Secret` record (submitted in `MsgDistributeShares`) | Public |
| `pk_s`, `sk_s` | Fresh single-use per-secret X25519 keypair — the **time-lock** | 32B each | `pk_s`: slim `Secret` record (§9.4). `sk_s`: exists intact only momentarily on the creator's device, then only as shares | `pk_s` public; `sk_s` nobody, until ≥t reveals |
| `C` | `C_r` encrypted to `pk_s` — the **outer** layer; the only copy of the secret material on chain | payload + 120B | `SecretPayloads` cold store, written once | Public bytes; decryptable only with `sk_s` |
| `ks_i` | Key share i: one SSS share of `sk_s` in the versioned envelope `version ‖ sss_id ‖ share` (§9.3) | 34B | Guardian i's device (plaintext); on chain as a reveal record after step 10 | Guardian i before the window; public after reveal |
| `Enc_gᵢ(ksᵢ)` | `ks_i` encrypted to guardian i's public key | ≈94B | `SecretShares` store | Only guardian i can decrypt |
| `HMACᵢ` | Commitment to the plaintext `ks_i` (publicly-derivable key — §8 entropy floor) | 32B | `SecretShares` store, beside `Enc_gᵢ(ksᵢ)` | Public; used to verify reveals and early-reveal evidence |
| `t`, `n` | Reconstruction threshold / total shares (existing params) | — | Slim `Secret` record | Public |
| `P`, `B` | Creator-paid reward pool / per-secret guardian bond (existing economics, untouched) | — | — | — |

#### Steps

**Phase 1 — request (creator ↔ chain).**

1. Creator sends `MsgRequestGuardians`, paying the reward pool `P`. The protocol
   selects `n` bond-affordable guardians and returns their addresses and
   encryption public keys in the response.

**Seal (creator's device, between phases 1 and 2 — one `seal_secret` call, §7).**

2. Encrypt `payload` to `recipient_public_key` → `C_r` (inner layer — identical
   to today).
3. Compute `commitment = SHA256(C_r)`.
4. Generate the fresh per-secret keypair `(pk_s, sk_s)`.
5. Encrypt `C_r` to `pk_s` → `C` (outer layer — this is the time-lock).
6. SSS-split `sk_s` t-of-n → `ks_1 … ks_n`; for each guardian, encrypt
   `ks_i` to guardian i's key → `Enc_gᵢ(ksᵢ)` and compute `HMACᵢ` over the
   plaintext `ks_i`.
7. Discard `sk_s` (and the local copy of `pk_s`) — from here the private key
   exists only as guardian shares.

**Phase 2 — distribute (creator → chain).**

8. Creator sends `MsgDistributeShares` carrying `C`, `pk_s`, `commitment`, and
   the `n` pairs `(Enc_gᵢ(ksᵢ), HMACᵢ)`. The chain stores each piece exactly
   once: `C` → `SecretPayloads`; `pk_s` and `commitment` → the slim `Secret`;
   each `(Enc_gᵢ(ksᵢ), HMACᵢ)` → `SecretShares`.

**Phase 3 — confirm (guardians ↔ chain).**

9. Each guardian fetches its `Enc_gᵢ(ksᵢ)`, decrypts it with its guardian key,
   holds the 34B plaintext `ks_i` off-chain, and sends `MsgConfirmShares`. The
   first `n` confirmations lock bond `B` from each guardian's float.

**Reveal (guardians → chain, once the window opens at the target height).**

10. Each guardian submits `MsgRevealShare` containing its plaintext `ks_i`. The
    chain recomputes `HMACᵢ` over the submitted bytes, checks it against the
    stored value, and stores the ~34B reveal record. A reveal *before* the
    window is reportable by anyone via `MsgSlashGuardian` with the plaintext
    `ks_i` as evidence — the same HMAC check (§6).

**Reconstruct (client-side `unseal_secret` — no transaction).**

11. *Anyone*: combine ≥t revealed key shares → `sk_s`; optionally check it
    against the stored `pk_s` (§9.4); fetch `C`; outer-decrypt → `C_r`; verify
    `SHA256(C_r) == commitment`. This is the public proof that reconstruction
    succeeded.
12. *Recipient only*: inner-decrypt `C_r` with their private key → `payload`.

## 4. How the per-secret keypair plays with recipient encryption

They are **two independent layers with two independent jobs**, and they compose
cleanly because each is ordinary public-key encryption to a different key:

| Layer | Keypair | Lifetime | Who holds the private key | Job |
|---|---|---|---|---|
| Inner | Recipient's keypair (the existing `recipient_public_key` field) | Long-lived, recipient-managed | The recipient, always | Confidentiality: only the recipient ever reads the payload |
| Outer | Per-secret keypair `(pk_s, sk_s)` | Single use; `sk_s` exists intact only for moments on the creator's device | **Nobody** — it exists only as guardian shares until the window | The time-lock: *when* anyone (including the recipient) can peel back to `C_r` |

Nothing about the recipient layer changes: same field on the secret, same client
key management, same decrypt call at the end. The per-secret keypair simply takes
over the role the SSS payload-split used to play — it *is* the time-lock — and
because it is 32 bytes, it is cheap to custody in pieces.

**Layer order matters and is deliberate.** With recipient-inner / secret-outer, the
post-window world is exactly today's: the public learns `C_r` (reconstructable by
anyone from revealed shares today; derivable by anyone from `sk_s` + on-chain `C`
tomorrow), the commitment `SHA256(C_r)` is verifiable by anyone — guardians,
watchtowers, the recipient — and only the recipient reaches the payload. The
reverse order (secret-inner) would also time-lock, but post-window the public could
no longer verify the commitment (verification would need the recipient's private
key), which would break third-party verifiability of reconstruction and change the
spec's verification story. Keep recipient-inner.

One trust-model note: before the window, the *recipient* is time-locked exactly as
today (they lack `sk_s` just as they lacked the shares of `C_r`). The *creator* can
always decrypt early in either scheme — they know the payload. So discarding `sk_s`
is hygiene, not a trust requirement (§8).

## 5. What the chain sees — protocol changes

A key insight keeps the consensus diff small: **the chain never interprets share
bytes today** — it stores opaque blobs, verifies HMACs over them, and counts them.
Key shares are just smaller opaque blobs. The chain-side changes are:

1. **`MsgDistributeShares` gains a `payload_ciphertext` field** (`C`). Stored once
   in a new cold store, written at distribution, never rewritten:
   `SecretPayloads: Map[string, bytes]` (joins the S1–S3 layout as a fourth
   side-store; pruned at Stage 2 of the retention plan — the recipient needs it to
   decrypt, so it lives exactly as long as the reveal records.
   [DONE_TERMINAL_SECRET_RETENTION_PLAN.md](DONE_TERMINAL_SECRET_RETENTION_PLAN.md) §9.6
   coordinates the canonical-record definition.) **Concrete tombstone
   requirement:** post-S9, third-party verification needs `C` as well as the
   reveal records (combine → `sk_s` → decrypt `C` → check `SHA256(C_r)`), so the
   canonical `TerminalSecretRecord` must include `SHA256(C)` — reveal records are
   now ~34B and cheap to retain, but `C` itself is pruned, and without its digest
   post-prune provability of reconstruction is lost.
2. **Size caps re-cut.** `MaxEncryptedShareSize` splits into:
   - `MaxPayloadSize` for `C` — initially 4KB + 120B (preserving today's effective
     secret size exactly); raising it later is a one-parameter decision that no
     longer multiplies by 32 (§9.2).
   - `MaxKeyShareSize` ≈ 128B for encrypted key shares (94B + margin), enforced at
     `DistributeShares`; and the same bound on `MsgRevealShare.decrypted_share`
     (~64B), enforced at reveal.
3. **Commitment semantics: unchanged** (`SHA256(C_r)`) — spec.md's "Invalid Share
   Submission" stance (share content validity is the creator's responsibility) also
   carries over unchanged: a creator can still distribute garbage; HMACs bind
   whatever was committed; settlement remains threshold-independent.
4. **Enforcement (resolved, July 2026):** the chain is pre-launch, so the new
   scheme is **required from genesis** — payload field mandatory, tight share
   caps, one validation path. There is no grandfathering, no dual-scheme window,
   no upgrade migration, and no need for a scheme-discriminator field on the
   secret record (the versioned key-share envelope, §9.3, covers future format
   evolution). This mirrors the parent plan's "no state migration — pre-launch"
   ruling for S1–S3.

Everything else — selection, bonds, pricing (`P = rate × distance × shares ×
bump`), acceptance, HMAC early-reveal slashing, settlement, cancellation — is
untouched: those mechanisms operate on guardians, heights and amounts, never on
share contents.

## 6. Impact per operation

Worst case (32 guardians, 4KB payload), assuming the S1–S3 storage split is in
place; "before" is the split-but-payload-splitting world:

| Operation | Payer | Before (post-S1–S3) | After S9 | Change |
|---|---|---|---|---|
| `DistributeShares` tx size | Creator | ~131KB (32 × 4KB shares) | ~7.2KB (4.1KB payload + 32 × ~100B) | ~18× smaller |
| `DistributeShares` storage gas | Creator | ~4.5M | ~300k | ~15× cheaper |
| `RevealShare` tx + storage | Guardian | ~150k gas (4KB HMAC read, KB-scale reveal write) | ~20k gas (~100B everything) | ~7× cheaper |
| `MsgSlashGuardian` evidence | Reporter | 4KB share in tx | ~64B key share | smaller still |
| Live state per active max-size secret | all nodes | ~140KB | ~11KB | ~13× less |
| Block capacity at saturation | network | ~7 max-size distributions per 1MB of block txs | ~140 | ~20× throughput headroom |

`ConfirmShares` is unchanged (never touches share bytes after S1). Reconstruction
moves one step client-side (combine → decrypt outer → verify commitment → decrypt
inner) but operates on ~100B shares plus one 4KB ciphertext fetched once, instead
of ≥t × 4KB reveal records — a strict win for the mobile client.

## 7. Surface area and sequencing

Ordered by risk, not size — the crypto is composition, the coordination is the work:

- **Rust (`rust/src/`)**: new high-level helpers (`seal_secret` /
  `unseal_secret` composing keypair-gen + two-layer encrypt + key split, and the
  reverse) so every client shares one audited code path. No new primitives.
- **WASM + FFI (`rust/src/ffi.rs`, `wasm/`)**: expose the helpers; rebuilds are
  file-based make rules already.
- **TypeScript SDK**: create/distribute and reconstruct flows switch to the
  helpers; reconstruction gains the payload fetch. Wire types gain the payload
  field (additive).
- **Guardian daemon (`guardian/`)**: flow is identical (receive, decrypt, hold,
  reveal) — only sizes change. Per the ShareIndex lesson, still grep and run its
  full suite; cache/reveal tests hard-code share fixtures.
- **Proto**: `MsgDistributeShares` gains `payload_ciphertext` and
  `secret_public_key` (`pk_s`, §9.4 — landing on the slim `Secret` record); new
  `SecretPayloads` store with genesis import/export alongside the other
  side-stores. **Payload query surface (resolved, July 2026): a granular
  `Query/SecretPayload` endpoint only** — it matches the granular-endpoint
  pattern (reconstruction clients fetch reveals + payload, both targeted), and
  the assembled `SecretView` keeps meaning "the pre-split wire shape plus new
  slim-record fields": `pk_s` joins the view additively, the payload blob does
  not. Per the CLI/query parity rule (CLAUDE.md), the endpoint ships with its
  AutoCLI `RpcCommandOptions` entry. Constants (`MaxPayloadSize`,
  `MaxKeyShareSize`). Also correct the stale `secret_commitment` comment
  ("SHA256(original_secret)" → SHA256 of the recipient-encrypted payload)
  while the file is open.
- **spec.md**: Core Operations (distribute/reveal flows), Security Model (the
  stored-ciphertext point, §8), commitment/verification section, size caps table,
  retention coordination. **Plus the large-secret envelope pattern (in scope,
  July 2026)**: for payloads above the cap, the secret *is* a key — encrypt the
  blob off-chain (IPFS/Arweave/anywhere durable), time-lock
  `key ‖ URI ‖ SHA256(blob)` (a few hundred bytes), and the recipient verifies
  the fetched blob against the hash. Unlimited effective payload size with
  identical reveal guarantees; this is the protocol's answer to large secrets,
  so the §9.2 cap review happens with the alternative on record. (A
  `sealLargeSecret` SDK helper is a sensible follow-up, not part of this
  branch.)
- **Scope boundary (July 2026): this change does not touch recipient
  discovery.** `recipient_public_key` stays exactly as it is (the
  [DONE_RECIPIENT_PRIVACY_PLAN.md](DONE_RECIPIENT_PRIVACY_PLAN.md)
  detection-hint swap lands in its own branch immediately after), and
  `sealed_refs` also waits for that branch even though `pk_s` makes it
  implementable here — one proto re-cut per plan.
- **Tests**: e2e lifecycle asserts on-chain amounts — unaffected; add an e2e that
  reconstructs via the new path end-to-end. During implementation, also run e2e
  passes at payload sizes **above** the 4KB cap-equivalent (e.g. 8KB, 16KB, 32KB
  behind a devnet-only parameter) and feed the results back as a candidate
  `MaxPayloadSize` (§9.2) — the goal is an evidence-based cap, not a guessed one.

**Sequencing (updated for pre-launch, July 2026).** With no live chain there are
no upgrade heights and no state migrations — the earlier (a) separate-upgrades /
(b) combined-upgrade analysis is superseded. Sequencing is now ordinary PR
ordering: the S1–S3 storage split landed first (merged via
[PR #36](https://github.com/leedavis81/timeflare/pull/36); its layout was
designed so S9 slots in without reshaping any store), and S9 lands as a
coordinated change across chain, Rust/WASM, SDK and guardian daemon in one
review. Genesis validation enforces the new scheme from day one (§5.4).

## 8. Security analysis

- **Confidentiality before the window.** `C` is public from day one, protected by
  `sk_s`, which exists only as an SSS share set — information-theoretically, <t
  shares reveal nothing about `sk_s`. This is the *same* residual assumption as
  today, where <t revealed payload shares reveal nothing about `C_r` — except
  today's assumption already tolerates the full share *set* existing on-chain
  encrypted per-guardian. Net: no new assumption; the spec should still state
  explicitly that the payload ciphertext's pre-window confidentiality rests
  entirely on the key shares.
- **HMAC entropy floor.** The share HMAC's key is *publicly derivable* —
  `SHA256("secrets" ‖ secret_id ‖ guardian_address ‖ "hmac_salt")`
  (`crypto/hmac.go`, `rust/src/utils.rs`) — so it is a salted commitment, not a
  secret-keyed MAC: its pre-window security rests entirely on the entropy of the
  share bytes. Today that entropy is KB-scale; after S9 it is exactly the 256
  bits of the key share's y-values — still far beyond brute force, and the n
  on-chain HMAC commitments already made the scheme computational rather than
  information-theoretic (as today's `SHA256(C_r)` commitment does). No mechanism
  change needed, but spec.md should state that key-share HMAC security rests on
  this 256-bit entropy floor.
- **Guardian collusion.** Unchanged: any t colluding guardians could reconstruct
  early today (C_r) and tomorrow (sk_s → C_r). Same boundary as spec.md's existing
  security model; early-reveal HMAC slashing deters identically, now over key
  shares.
- **Creator.** Knows the payload regardless; discarding `sk_s`/`pk_s` after
  distribution is hygiene. A creator who *keeps* `sk_s` gains nothing they do not
  already have.
- **Recipient.** Time-locked exactly as before (needs guardian reveals either way).
  Recipient-layer key management unchanged.
- **Integrity.** `C` is consensus-attested at distribution; AEAD tags authenticate
  both layers; the commitment binds `C_r` for third-party verification. A wrong
  `sk_s` reconstruction fails the outer AEAD tag loudly.
- **What the commitment does — and does not — prove.** `commitment = SHA256(C_r)`
  proves *reconstruction integrity*: the reveal-time result is byte-for-byte the
  payload the creator distributed, checkable by anyone without the recipient's
  key. It deliberately does **not** bind the plaintext. Because the inner
  encryption is randomised (fresh ephemeral key + nonce), a third party who
  knows the plaintext cannot recompute `C_r` and verify it against the
  commitment — so a creator cannot prove on-protocol that a secret "contains S"
  (e.g. to show a counterparty they aren't bluffing), and even post-reveal the
  public learns only `C_r`, never the content. This is a feature: a
  plaintext-binding commitment would hand anyone with a *guess* an offline
  confirmation oracle for the secret's entire lifetime — fatal for low-entropy
  secrets. Any future "provable content" feature must be a deliberate opt-in
  design, not a side effect. Post-S9 the commitment is *partially* redundant
  for integrity (consensus-attested `C` + the outer AEAD tag already
  authenticate `C_r` on decryption), but it is kept (32B): it verifies `C_r`
  independently of the outer layer — it still works if the key shares turn out
  to be garbage, or if `C_r` surfaces by another route — and it anchors the
  post-prune tombstone record (§5.1).
- **Future hardening (cheap now that the secret is 32B):** Feldman-style VSS on the
  key polynomial would let guardians (and the chain, if ever desired) verify share
  consistency at distribution — infeasible economically for payload-sized shares,
  trivial for a 32-byte key. Out of scope; noted so the share format can leave room
  (§9.3).

## 9. Open questions

All resolved or extracted, July 2026.

1. ~~**Enforcement.** Required-after-upgrade with grandfathered in-flight secrets,
   or a permissive dual-scheme window?~~ **Resolved: required from genesis.** The
   chain is pre-launch, so there are no in-flight secrets to grandfather — no dual
   validation path, no scheme-discriminator field, no migration (§5.4).
2. ~~**`MaxPayloadSize`.** Hold at today's effective ~4KB initially, or take the
   opportunity to raise it?~~ **Resolved: hold at ~4KB (4KB + 120B) at S9
   launch.** During implementation, run e2e passes at larger payload sizes (§7,
   Tests) and feed the measurements back as a candidate for a raised cap —
   revisit with evidence, not up front.

   **Measurement evidence (July 2026, implementation devnet, 2s blocks,
   7 guardians / threshold 3, local uncommitted cap bump).** Full lifecycle —
   distribute → accept → reveal → unseal → byte-identical decrypt — passed at
   every size; reveals are payload-independent throughout (34B envelopes,
   ~86k gas, ~340B tx):

   | Payload | Distribute tx | Distribute gas | Fee @ 0.1 uveil min |
   |---|---|---|---|
   | 1KB | ~2.8KB | 231,830 | ~0.023 VEIL |
   | 16KB | ~18KB | 846,350 | ~0.085 VEIL |
   | 64KB | ~67KB | 2,812,470 | ~0.28 VEIL |

   Cost is linear at ≈40 gas per payload byte (10 tx-bytes + 30 store), with
   no non-linear effects observed to 64KB; a 64KB distribute tx sits
   comfortably under the 1MB mempool cap. Conclusion: **64KB is a viable
   raised cap whenever product demand appears** — the binding considerations
   are state residency (~64KB × live secrets for up to ~15 months) and the
   SDK's fixed distribute gas budget (1M gas covers ~20KB; a raise must switch
   the SDK to simulated gas). The launch value stays 4,216B per the
   resolution; the envelope pattern (§7 spec bullet) remains the answer above
   the cap.
3. ~~**Key-share format.** Exactly `sss_id ‖ sk_s_share` (33B), or a small
   versioned envelope?~~ **Resolved: versioned envelope** —
   `version(1B) ‖ sss_id(1B) ‖ sk_s_share(32B)` = 34B plaintext, ≈94B encrypted.
   Near-free insurance leaving room for Feldman VSS commitments (§8) without a
   format break.
4. ~~**Should `pk_s` be stored** (~32B on the slim record)?~~ **Resolved: yes.**
   Beyond the watchtower sanity check, it makes creator misbehaviour *publicly
   attributable*: a reconstructed `sk_s` that matches `pk_s` while `C` fails to
   decrypt (or `SHA256(C_r)` fails the commitment) is provable creator fault, not
   guardian fault — upgrading spec.md's "share content validity is the creator's
   responsibility" stance from an unfalsifiable convention to a checkable verdict.
5. ~~**Recipient-metadata sizing.** With shares tiny, `recipient_metadata` (up to
   ~3.2KB) becomes one of the larger residents of the slim record.~~
   **Extracted (July 2026)** to
   [DONE_RECIPIENT_PRIVACY_PLAN.md](DONE_RECIPIENT_PRIVACY_PLAN.md): the map is replaced
   by a compact serialised recipient reference (not JSON); format and caps are
   refined there.
