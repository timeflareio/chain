# Guardian Encryption-Key Rotation — Forward-Only — Plan

*Lets a guardian rotate its share-encryption key for **future** assignments
while every old key remains bound to the assignments made under it. Caps the
blast radius of a key leak, makes long-lived guardianship hygienic, and ends
rotation-by-re-registration (which burns a fresh entry fee and abandons the
address's history). Explicitly does **not** rescue in-flight secrets after
key loss — nothing protocol-side can, and this plan says so honestly.*

> **Status: done — implemented July 2026, PR
> [#107](https://github.com/leedavis81/timeflare/pull/107) (merged)**;
> refined July 2026, all open questions ruled by the owner, July 2026;
> put onto the roadmap in the
> key-custody discussion (see
> [DONE_GUARDIAN_KEY_CUSTODY_PLAN.md](DONE_GUARDIAN_KEY_CUSTODY_PLAN.md)
> decision log Q2). The same discussion ruled the alternative key-lifecycle
> mechanism — share/bond **transfer** between guardians — **off the table
> permanently** (the transfer-and-report attack; PROTOCOL.md Security
> Observations §3, spec.md Common Attack Vectors #6), so rotation is the
> only live workstream. This plan amends spec.md's "encryption keys are
> permanently immutable" stance; it involves proto changes, which per
> CLAUDE.md require explicit confirmation — **approval of this plan is that
> confirmation**.
> **Priority**: P2 — additive protocol change (new message/state, not
> wire-breaking), so it *could* land post-testnet via upgrade, but
> shipping it in the genesis protocol is free (§8) while an upgrade path
> costs a migration — pre-testnet preferred, not a blocker.
> **Components**: `proto/timeflare/secrets/v1/` (guardian, tx, query);
> `x/secrets/types` (constants, economics derivation, message +
> `ValidateBasic`, codec); `x/secrets/keeper` (key history state,
> uniqueness index, msg server, epoch derivation, genesis, queries with
> AutoCLI parity); `guardian/` daemon (epoch keyring, automated key
> resolution, `rotate-key` command, self-check, custody bundle);
> regenerated TS SDK protos + mobile client vendored SDK repack
> (`pack-sdk.sh` + lockfile integrity sync — mobile CI runs the stale
> SDK otherwise); `docs/spec.md` + custody runbook + FAQ;
> `devnet/e2e-scenarios.sh`; TESTING_COMMANDS.md.

## Contents

1. [Problem](#1-problem)
2. [What rotation can and cannot buy](#2-what-rotation-can-and-cannot-buy)
3. [Design: forward-only, append-only](#3-design-forward-only-append-only)
4. [What deliberately does not change](#4-what-deliberately-does-not-change)
5. [State, message and query surface](#5-state-message-and-query-surface)
6. [Guardian daemon changes](#6-guardian-daemon-changes)
7. [Interactions with in-flight commit windows](#7-interactions-with-in-flight-commit-windows)
8. [Migration & genesis](#8-migration--genesis)
9. [Testing](#9-testing)
10. [Rulings carried in from the July 2026 discussion](#10-rulings-carried-in-from-the-july-2026-discussion)
11. [Open questions](#11-open-questions)

---

## 1. Problem

The encryption key is set once at `MsgGuardianRegister` and is immutable
forever. Consequences, at bonded-economics stakes:

- **Leak is retroactive across the guardian's whole history.** Every
  `encrypted_share` record is public on-chain, so one leaked key decrypts
  every share ever assigned to that guardian — enabling early-reveal
  evidence against it (full bond slash per secret) or silent threshold
  erosion across many still-locked secrets at once.
- **Loss is terminal for the address.** The guardian misses every reveal on
  every in-flight secret (no-reveal slash on each), and the address is
  permanently dead as a guardian: the only "rotation" is withdraw-the-float
  and register a new address — burning a fresh entry fee (1,000 VEIL at the
  settled token-economics values) and abandoning the address's history.
- **Hygiene is priced out.** A professional operator who wants to rotate
  keys periodically — normal custody practice — currently cannot, except at
  entry-fee cost per rotation.

## 2. What rotation can and cannot buy

**Cannot** (state plainly, everywhere this feature is documented): rescue
in-flight secrets after key **loss**. Shares are encrypted client-side by
creators to the then-current key; the chain holds only ciphertext and can
re-encrypt nothing. A guardian who loses its current key still eats the
no-reveal slashes on everything encrypted to it. Loss mitigation is the
custody plan's territory (encrypted-at-rest key, backup/restore, startup
self-check) — rotation is not a recovery mechanism.

**Can**:

- **Bound leak blast radius** to one epoch: a compromised key exposes only
  the assignments made under it, not the address's lifetime history.
- **Preserve the address** across key hygiene: availability, float,
  selection history and the burned entry fee all survive a rotation.
- **Make multi-year guardianship sustainable**: rotate freely while holding
  years-long secrets — the old epoch's key serves the old secrets to their
  settlement.

## 3. Design: forward-only, append-only

1. **Key epochs.** A guardian's keys form an append-only history
   `(guardian, epoch) → pubkey`, epoch starting at 0 (the registration key)
   and incrementing per rotation. The guardian record carries the current
   epoch; no key is ever overwritten or deleted.
2. **Forward-only semantics.** A rotation takes effect for **selections
   after it lands**. Every existing assignment remains bound to the epoch
   key it was created under; the reveal obligations, HMACs and slashing
   evidence for those assignments are untouched.
3. **The epoch in force is derivable, never stored per-secret.** Selection
   reads the guardian's current key and the Phase-1 response hands it to
   the creator — the epoch in force at selection is the one the creator
   encrypts to. Each history entry records the height it became effective
   (a rotation landing at height *h* takes effect from *h+1*, so a
   same-block rotation and selection are unambiguous), making "the epoch
   in force for guardian *g* at height *h*" a pure function of the
   history. No per-secret pin is stored (ruled July 2026; §7 explains why
   mid-commit rotation needs none).
4. **Global, permanent uniqueness.** Key uniqueness is enforced across
   **all keys ever registered by any guardian, all epochs included** — a
   retired key stays reserved forever, since share material encrypted to it
   may still exist. (A global uniqueness index covering all history is
   introduced — registration never actually enforced the spec's uniqueness
   claim before this plan; it never shrinks.)
5. **Rotation is priced and rate-limited.** Each rotation appends a
   permanent key record (~70B of state plus a forever-reserved
   uniqueness entry), so it carries a **flat burned fee of
   `rate × 14,400` (0.0144 VEIL at `rate = 1 uveil` — one guardian-day, the
   same shape as the creation-fee floor)** and a **minimum interval of one rotation per
   guardian per 432,000 blocks (30 days at 6s blocks)**; both ruled July
   2026. The interval costs nothing to enforce — the newest history
   entry already stores `effective_from_height`, so validation is one
   comparison (`current_height − last_effective_height ≥ 432,000`,
   applying uniformly from registration: epoch 0's effective height
   starts the clock). It bounds worst-case history growth to ~12 entries
   per guardian-year and makes same-block multi-rotation impossible;
   the price is anti-spam, not economics. Both values are **hard
   constants** in `x/secrets/types` (derived from `rate` and the
   guardian-day/block-time constants), per the immutable-economics
   stance (Position A) — changeable only via coordinated upgrade, never
   governance parameters. The interval costs nothing in incident
   response: a guardian whose *current* key is compromised inside the
   window sets `accepting = false` immediately (`MsgGuardianUpdate` —
   instant, free, and identical forward protection to rotating) and
   rotates when the window opens; the operator runbook states this
   explicitly so the interval is never experienced as a trap
   mid-incident.

## 4. What deliberately does not change

- **HMAC verification** — keys derive from
  `(secretID, guardianAddress)`, not the encryption key; reveals and
  early-reveal evidence are epoch-agnostic.
- **Selection** — tickets are `SHA256(seed ‖ address)`; rotation creates no
  selection surface and no grinding angle.
- **Bonds, settlement, economics** — untouched; rotation is a key-lifecycle
  mechanic only.
- **One share, one holder** — rotation moves no share material anywhere;
  the invariant that killed transfer holds trivially.
- **Registration** — epoch 0 is the registration key, exactly as today; the
  entry fee is unchanged.

## 5. State, message and query surface

Proto/state changes (explicit-confirmation items):

1. **State**: `GuardianKeyHistory collections.Map[(addr, uint64 epoch),
   KeyHistoryEntry{pubkey, effective_from_height}]`;
   `current_key_epoch` on the `Guardian` record (the current key itself
   stays on the record for the O(1) selection read). The uniqueness index
   covers all historical keys.
   **The selection read is effective-height-aware.** Rotation updates the
   record immediately but takes effect from *h+1* (§3.3), so a rotation tx
   ordered earlier in the same block leaves the record holding a key not
   yet in force. Selection — and therefore the key handed to the creator
   in the Phase-1 response — resolves the key in force: if the newest
   history entry's `effective_from_height` is still in the future, use the
   previous epoch's key. One extra read in the same-block case only; the
   plain record read everywhere else. Without this, a same-block
   rotation-then-selection would hand the creator a key the daemon's
   height-derivation disagrees with (§9 tests exactly this).
2. **No per-secret state.** The epoch a secret's shares were encrypted to
   is derived from the secret's selection height against the history's
   effective heights — nothing is stored on the `Secret` record (ruled
   July 2026: the chain never validates ciphertext against a key, so a
   stored pin would defend nothing; see §7).
3. **Message**: `MsgGuardianRotateKey { guardian, new_key (32B) }`
   (actor-first naming per
   [DONE_MSG_ACTOR_NAMING_PLAN.md](DONE_MSG_ACTOR_NAMING_PLAN.md)) —
   validation: registered guardian, key length/shape, globally-unique
   against all history, burned rotation fee and 30-day minimum interval
   (§3.5). Emits
   `guardian_key_rotated { guardian, new_epoch }`.
4. **Queries**: guardian views gain `current_key_epoch`;
   `GuardianKeyHistory(addr)` query (AutoCLI parity rule applies — the RPC
   ships with its `RpcCommandOptions` entry).
5. **Genesis**: key history exported/imported; `current_key_epoch`
   validated against the history's max epoch (ties into the genesis
   invariant sweep from
   [DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md](DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md)).

## 6. Guardian daemon changes

- The daemon keeps an **epoch keyring**: current private key plus every
  retired key that still has in-flight assignments; a retired key is
  eligible for local deletion **once its last assignment settles** (ruled
  July 2026: post-settlement, reveals and evidence no longer need the
  key, so there is no reason to hold it through the retention windows —
  the operator docs recommend settlement as the deletion point).
- **Per-assignment key resolution is fully automated** — this is guardian
  code work, not operator procedure. For every assignment the daemon
  resolves the epoch in force from the secret's selection height against
  the history's effective heights, and uses that epoch's key for the
  whole pipeline: decrypt → HMAC verification → accept (confirm) →
  reveal. The operator is never asked which key applies. A wrong
  resolution surfaces as a decrypt/HMAC mismatch, so trial-decrypt across
  the keyring is the belt-and-braces fallback (and logs loudly when the
  fallback disagrees with the derivation). The custody plan's
  encrypted-at-rest format and backup bundle (Phases 1–2) carry the whole
  keyring, not just the current key.
- The custody plan's startup self-check extends to: current local key
  matches the on-chain current epoch; every epoch with in-flight
  assignments has its key present locally — refuse to run loudly otherwise.
- `guardiand rotate-key` command: generates, backs up (ceremony), then
  submits `MsgGuardianRotateKey` — never submits before the backup
  ceremony completes.

## 7. Interactions with in-flight commit windows

Rotation landing between a secret's Phase 1 and Phase 2 is a **non-event
on-chain**, and the chain does not defend against it because there is
nothing to defend: `UserDistributeShares` performs no key validation — the
chain holds only ciphertext and cannot check which key it is encrypted
to. The creator encrypts to the key handed over in the Phase-1 response;
the daemon still holds that key (it has in-flight assignments under it)
and decrypts, verifies and accepts exactly as before. No freeze on
rotation during open commit windows, and no per-secret pin, is needed
(ruled July 2026).

Operationally the clean sequence is: stop accepting new selections
(`MsgGuardianUpdate`, accepting flag), drain, rotate. A guardian that
rotates with secrets mid-commit loses nothing — it simply carries the
old epoch's key until those secrets settle (§6); guardians routinely
hold multiple live epoch keys, and what matters is knowing when each
can be discarded, which settlement defines.

## 8. Migration & genesis

Pre-launch, this ships in the genesis protocol: every registered guardian's
key becomes epoch 0 of its history; `current_key_epoch = 0`. If it instead
lands via upgrade on a running devnet/testnet, the handler walks guardians
and writes epoch 0 from the existing record — no other state changes and no
per-secret backfill (derivation gives every existing secret epoch 0, the
only epoch that exists). The `Guardian` record stores no registration
height, so the upgrade handler stamps epoch 0's `effective_from_height`
with the **upgrade height** — conservative (an existing guardian's first
rotation waits the full 30-day interval from the upgrade) and simple; on
the genesis path the registration handler stamps the registration height
as normal.

## 9. Testing

- Conformance: full lifecycle spanning a rotation (Phase 1 → rotate →
  Phase 2/3/reveal/settle on the old epoch; next secret selects the new
  epoch); rotation mid-commit (§7) both accepted and rejected paths.
- Uniqueness: re-registering any historical key (own or another
  guardian's) fails.
- Slashing: early-reveal evidence against an old-epoch assignment still
  verifies and slashes after rotation.
- Genesis round-trip with multi-epoch histories; invariants:
  `current_key_epoch` = max epoch; history effective heights strictly
  increase; the derived epoch for every in-flight assignment exists in
  its guardian's history.
- Effective-next-block rule: a rotation and a selection landing in the
  same block select the pre-rotation key, and the derivation agrees.
- Minimum-interval boundary: a rotation at
  `last_effective_height + 431,999` is rejected, at `+ 432,000`
  accepted; the interval also applies to the first rotation after
  registration (epoch 0 starts the clock).
- e2e scenario (devnet `e2e-scenarios` suite): a guardian rotates
  mid-life with a secret in flight — the pre-rotation secret is
  decrypted, HMAC-verified, accepted and revealed **with the old epoch's
  key**, entirely automatically; a secret created after the rotation
  selects and is processed **with the new key**; both settle correctly
  with exact on-chain amount assertions; daemon restart mid-way restores
  the epoch keyring from the custody backup and both pipelines resume.

## 10. Rulings carried in from the July 2026 discussion

1. **Rotation is forward-only** — replacement/re-encryption semantics are
   impossible by construction (the chain cannot re-encrypt ciphertext) and
   are not attempted.
2. **Rotation is not loss recovery** — loss is answered by the custody
   plan's backups; every doc touching rotation states this.
3. **Transfer is off the table permanently** (transfer-and-report attack;
   PROTOCOL.md Security Observations §3) — rotation must not be extended
   into any handoff mechanism; "one share, one holder, forever" is
   load-bearing.
4. **Old keys stay reserved forever** (§3.4) and **old epochs serve their
   in-flight secrets to settlement** (§3.2).
5. **Rotation fee and rate limit**: flat burn of `rate × 14,400`
   (one guardian-day; 0.0144 VEIL at the settled rate) plus a minimum interval of one rotation per guardian per
   432,000 blocks — 30 days (§3.5).
6. **Retired-key deletion point is settlement** (§6) — the key is useless
   to the guardian after its last assignment settles.
7. **Spec amendment framing**: spec.md Guardian Key Management presents
   rotation as the hygiene path while keeping the immutability *of each
   epoch's binding* (the property that actually matters) explicit — each
   epoch's key is immutable forever; only the current-epoch pointer
   advances.
8. **No per-secret epoch pin** (§3.3, §5.2, §7): the chain never
   validates ciphertext against a key, so the epoch is derived from the
   secret's selection height and the history's effective heights
   (rotation effective from the next block). Mid-commit rotation is the
   guardian's own operational concern and is harmless — the clean path is
   stop-accepting → drain → rotate, and skipping it merely means holding
   the old key until its secrets settle.

## 11. Open questions

None — all questions ruled July 2026 (folded into §3, §5–§7 and the
decision log above).
