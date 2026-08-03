# Guardian Key Custody Plan

**Status**: IMPLEMENTED (Phases 1–3, July 2026) — encrypted-at-rest share key (versioned envelope, argon2id + ChaCha20-Poly1305, `guardian/custody/`), `guardiand config migrate-key`, `guardiand key backup`/`key restore` with on-chain verification, startup self-check refusing to run on key mismatch, signer behind the `blockchain.TxSubmitter` interface, devnet guardians on the encrypted format with the well-known dev passphrase; operator runbook at [docs/guides/GUARDIAN_KEY_CUSTODY.md](../../guides/GUARDIAN_KEY_CUSTODY.md). Phase 4 (KMS/HSM) explicitly descoped for now (owner ruling); OS-keychain KEK sourcing deferred — the envelope format and passphrase-resolution seam admit it without a format change. Q4 (health attestation) remains open and gated nothing here. Protocol-level key rotation is its own plan: [DONE_GUARDIAN_KEY_ROTATION_PLAN.md](DONE_GUARDIAN_KEY_ROTATION_PLAN.md).
**Priority**: P1 — guardian operational security; complements (does not duplicate) `DONE_GUARDIAN_IMPROVEMENTS_PLAN.md`
**Components**: `guardian/config/`, `guardian/cmd/guardiand/cmd/config.go`, `guardian/cmd/guardiand/cmd/register.go`, `guardian/blockchain/proxy.go`, `docs/guides/`

## What this plan does

Hardens how a guardian's two keys are stored, backed up, and recovered. The guardian-improvements plan fixes the config system and replaces the CLI proxy with a native gRPC client (which changes *how* signing happens); this plan addresses the custody model itself, which that plan does not cover.

## Why

A guardian's economic life depends on two keys, and both are effectively plaintext on disk today:

1. **The X25519 share-encryption private key** is a raw, unencrypted 32-byte file (`private_key`, mode 0600, written by `cmd/config.go`). It is immutable on-chain (registration binds it forever) and is a **single point of total failure**:
   - **Loss** ⇒ the guardian can never again decrypt any assigned share ⇒ it misses every reveal on every in-flight secret ⇒ it is no-reveal slashed on all of them (40% of each bond burned), and the address is permanently dead as a guardian (key rotation requires withdrawing the float and registering a new address + entry fee).
   - **Leak** ⇒ every share ever assigned to this guardian, past and future, is decryptable by the holder — and combined with the public on-chain `encrypted_share` records, a leaked key retroactively exposes shares for still-locked secrets, enabling early-reveal evidence against the guardian (full bond slash) or silent threshold erosion across many secrets.
   - The only current protection is a CLI warning to back it up.
2. **The Cosmos signing key** sits in a `timeflared` file keyring, which is passphrase-encrypted — but the passphrase is stored beside it (`keyring_passphrase`, base64 file) and piped to stdin at three call sites, because the daemon must sign unattended. The at-rest encryption is therefore decorative for any automated deployment: whoever obtains the guardian home directory has both.
3. There is **no documented backup/restore or compromise procedure** for operators (spec.md's Key Management section points to an FAQ for compromise recovery; the operator-facing runbook does not exist — see TESTNET_LAUNCH plan).

Guardians are meant to be run by third-party operators staking five-figure VEIL floats; "one plaintext file, please back it up" is not a shippable custody story.

## How

### Phase 1 — Encrypted key file at rest

1. Encrypt the X25519 private key file with a key-encryption key (KEK): age/NaCl secretbox with a KEK sourced from (in preference order) an OS keychain where available, a passphrase-derived key (scrypt/argon2id) for interactive setups, or an environment/file-supplied KEK for automated fleets. Format versioned from day one.
2. In-memory hygiene: load-on-demand, zero after use (the current code caches `*[32]byte` for process lifetime and never wipes; acceptable to keep the cache, but wipe on shutdown and document the trade-off).
3. Migration: `guardiand config migrate-key` encrypts an existing plaintext key in place (with a backup prompt); `config init` generates encrypted-by-default.
4. Be honest in docs about the limit: an unattended daemon must be able to decrypt its own key, so a same-host attacker with the daemon's privileges always wins — the encryption defends the *backup copies* and *at-rest theft* (stolen disk, leaked snapshot, mis-scoped backup), which is where most real key theft happens.

### Phase 2 — Backup & restore as a first-class flow

1. `guardiand key backup` → exports an encrypted bundle (share key + keyring + config fingerprint) with a printed recovery phrase or passphrase confirmation; `guardiand key restore` reverses it and *verifies against the chain* (public key must match the registered guardian record) before declaring success.
2. Startup self-check: on `start`, verify the local share key derives the on-chain registered public key; refuse to run (loudly) on mismatch — today a wrong key would only surface as decryption failures at confirmation time.
3. Operator runbook page: backup cadence, storage guidance, restore drill, and the exact economic consequences of loss (feeds the guardian operator handbook in TESTNET_LAUNCH).

### Phase 3 — Signing-key posture (coordinates with the native-gRPC rewrite)

The improvements plan's in-process keyring signing removes the passphrase-piping-to-subprocess pattern; this plan adds the custody requirements to that work: support `file` keyring with an environment-supplied passphrase (documented as the automated-fleet baseline), OS keyring backend where available, and structure the signer behind an interface so a remote-signer/KMS backend can be added without refactoring.

### Phase 4 — KMS/HSM path — **descoped (owner ruling, July 2026)**

Not being solved for now. The interface-level hook (cloud KMS envelope decryption of the share-key file; scope is KEK custody, not key-op offload — the X25519 *operation* itself is not HSM-friendly for arbitrary HSMs) remains recorded here so the Phase 3 signer interface is still structured to admit it later, but no KMS/HSM work is planned. Revisit only if professional operators ask.

### Protocol-level follow-up — **promoted to its own plan (July 2026)**

The immutability of the encryption key is what makes loss catastrophic and leak retroactive. Forward-only key rotation (new key applies to *future* assignments only; old keys retained for in-flight secrets) is now designed in [DONE_GUARDIAN_KEY_ROTATION_PLAN.md](DONE_GUARDIAN_KEY_ROTATION_PLAN.md) — ruled onto the roadmap after the July 2026 discussion that also ruled share/bond **transfer** off the table permanently (PROTOCOL.md, Security Observations §3), leaving rotation as the only live key-lifecycle workstream.

## Decision log (ruled by the owner, July 2026) & open questions

1. ~~**KEK default for the devnet/dev experience**~~ **Ruled: encrypted-by-default with a well-known dev passphrase.** The plaintext `--dev` path never exists, so the unsafe configuration can never accidentally ship; devnet ergonomics cost is one known passphrase in the devnet scripts.
2. ~~**Is protocol-level key rotation wanted on the roadmap now?**~~ **Ruled: yes — forward-only rotation**, designed in [DONE_GUARDIAN_KEY_ROTATION_PLAN.md](DONE_GUARDIAN_KEY_ROTATION_PLAN.md). (The spec's "immutable forever" stance is amended by that plan, not this one.)
3. ~~**Recovery-phrase format**~~ **Resolved by convention**: BIP39 mnemonic directly over the raw 32-byte X25519 key, per [docs/guides/CLIENT_CONVENTIONS.md §5](../../guides/CLIENT_CONVENTIONS.md) (ruled July 2026 for recipient identity keys — the guardian share key is the same key shape, and one encoding convention serves the whole ecosystem). The encrypted file bundle (Phase 2) remains the primary backup artefact; the mnemonic is the human-typable fallback.
4. **Should the chain expose a guardian "health attestation"** (e.g. the daemon periodically proving it still holds the key, letting creators avoid selecting zombie guardians)? Interacts with selection and privacy; likely its own plan if wanted. **Still open.**
