# Recipient Privacy — No Identity Channel, Unlinkable Discovery

*Two coupled changes that together make the chain carry **no recipient identity
at all**: (I) the free-form `recipient_metadata` map is **removed entirely** —
no reference field replaces it; and (II) the plaintext `recipient_public_key`
is replaced by a per-secret unlinkable **detection hint**, so a recipient's
secrets cannot be enumerated, linked to each other, or tied to a real-world
identity — by anyone except the recipient themselves.*

> **Status: DONE — implemented (July 2026).** All §6 items were ruled,
> resolved to requirements, or explicitly deferred (dual-key hints, behind the
> `version` field). Landed on the `recipient-privacy` branch: proto re-cut,
> keeper hint feed + `Query/HintsSince`, Rust `detect.rs` + WASM exports, a
> pure-Go CLI helper pinned to the Rust implementation by a shared test
> vector, SDK `findMySecrets`, and spec.md's Recipient Discovery section.
> Verified by `make test`, `make e2e` (which now asserts hint-based
> discovery end-to-end) and `make e2e-scenarios` on a fresh devnet. This
> document remains as the design rationale and decision log.
> Renamed and expanded from `RECIPIENT_METADATA_PLAN.md`: the metadata
> discussion surfaced that the stored recipient public key is a persistent
> pseudonymous identifier, so the two problems are solved together.
> **Rulings**: references are **removed entirely** (the earlier sealed-refs
> design is superseded — see §2 for the rationale and what replaces the
> notification story); the stored `recipient_public_key` is **removed** and
> replaced by the detection hint (§3); hints are **single-key** now with a
> version field reserved for a dual-key upgrade (§6.1).
> Sequencing: after [DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](DONE_KEY_SHARE_ARCHITECTURE_PLAN.md)
> (landed) and **before**
> [DONE_TERMINAL_SECRET_RETENTION_PLAN.md](DONE_TERMINAL_SECRET_RETENTION_PLAN.md)
> (the canonical record should hash the final field set, and this plan deletes
> its map-determinism special case).

## Contents

1. [The two problems](#1-the-two-problems)
2. [Part I — references: removed](#2-part-i--references-removed)
3. [Part II — unlinkable detection hints](#3-part-ii--unlinkable-detection-hints)
4. [The resulting privacy model](#4-the-resulting-privacy-model)
5. [Surface area and sequencing](#5-surface-area-and-sequencing)
6. [Open questions (refinement backlog)](#6-open-questions-refinement-backlog)

---

## 1. The two problems

**Problem 1 — the metadata field is an identity channel.**
`map<string, string> recipient_metadata` (10 entries, ≤64B keys, ≤256B values —
a ~3.2KB worst-case envelope) existed to carry a pointer to a real-world
identity ("this secret is addressed to `twitter:alice`") so that tooling could
contact a recipient unaware a reveal is due. That is creator-supplied PII about
a third party, on an immutable ledger, without that party's consent —
published up to a year before anything happens, and permanent in transaction
history regardless of pruning. The map shape also invites structured abuse
(JSON stuffed into values) and forces a map-determinism special case on the
retention plan's canonical record.

**Problem 2 — the recipient public key is a persistent pseudonymous
identifier.** `recipient_public_key` is stored in plaintext on every secret
(verified: the keeper only length/validity-checks it — it is pure metadata;
encryption happens client-side). Every secret addressed to the same key is
linkable to every other: count, cadence, sizes, and `bump` spend are readable
per recipient. Any single real-world link — one identity reference anywhere,
one out-of-band correlation — **retroactively de-anonymises the recipient's
entire history and future pipeline, forever**. References don't create this
linkage; key reuse does.

## 2. Part I — references: removed

**Ruling (July 2026): the field is removed entirely, with no on-chain
replacement.** The earlier iteration of this plan (compact `RecipientRef`s,
sealed under the time-lock by default) is superseded. Rationale:

1. **The only irreplaceable job references performed was notifying un-enrolled
   recipients** — people with no keypair, reachable only by a human-readable
   identifier. That job is inherently a PII publication.
2. **Sealing only defers the PII, it does not remove it.** A sealed reference
   opens the moment the secret reveals, and the identity link then sits in
   immutable history forever.
3. **The identifiers are fragile for the very scenario they serve.** Against a
   ≤1-year horizon and a recipient who doesn't know to expect anything, an
   email or handle changes, bounces, or is abandoned. The out-of-band route —
   the secret ID (and, where used, the recipient's private key) delivered via
   will, estate, or directly — is the reliable mechanism, and it needs no
   on-chain identity.

**The notification story after removal:**

- **Enrolled recipient** (has a keypair): discovers their secrets themselves
  via detection hints (§3), or is simply told the secret ID by the creator.
- **Un-enrolled recipient** (the "secret for my son, I only have his email"
  case): the creator generates a keypair *for* the recipient and delivers the
  private key + secret ID out-of-band (estate, sealed letter) — zero on-chain
  identity, and the delivery channel is the same one the notification would
  have needed anyway.
- **Public disclosure** ("message board"): encrypt to a well-known keypair
  whose private half is published. Anyone can scan with it and decrypt after
  reveal. This is a *convention*, not a protocol feature — the recipient-key
  layer stays mandatory and uniform.
- **Notify-tooling** can return later as a purely off-chain convention
  (e.g. an indexer accepting out-of-band contact registrations) without any
  protocol surface.

## 3. Part II — unlinkable detection hints

### What changes

`recipient_public_key` (32B, plaintext, reused across secrets) is **removed
from all stored and message shapes** and replaced by a per-secret detection
hint:

```proto
message DetectionHint {
  uint32 version       = 1;  // 1 = single-key X25519 hint (room for dual-key later)
  bytes  ephemeral_pub = 2;  // R — fresh X25519 public key, 32B, one per secret
  bytes  tag           = 3;  // SHA256("timeflare/detect/v1" || shared)[:8]
}
```

The recipient's long-term public key `A` is held by the creator exactly as
today — it is simply **never published**. Payload encryption to `A` is
unchanged (`encrypt_for_public_key` is already ephemeral-static X25519, and
its ciphertext embeds its own ephemeral and no identifier of `A` — the
ciphertext was never the linkage; the stored field was).

### Data components

| Component | Size | Where it lives | Lifetime |
|---|---|---|---|
| `A` — recipient long-term public key | 32B | **Off-chain only**: creator's device (and the recipient's own key management) | Long-lived; never appears in any transaction or state |
| `a` — recipient long-term private key | 32B | Recipient's device only | Long-lived |
| `e` — per-secret ephemeral private key | 32B | Creator's device, moments | Destroyed immediately after the hint is computed |
| `R` — per-secret ephemeral public key | 32B | **On-chain**: `DetectionHint.ephemeral_pub` on the slim `Secret` | Written once at creation; pruned with the record (retention Stage 2) |
| `shared = X25519(e, A) = X25519(a, R)` | 32B | Computed transiently on either side | Never stored anywhere |
| `tag = SHA256("timeflare/detect/v1" ‖ shared)[:8]` | 8B | **On-chain**: `DetectionHint.tag` | As `R` |

Total on-chain footprint: ~44B per secret (versus 32B today), written once in
`MsgRequestGuardians`, immutable thereafter. The chain validates only shape
(version known, `R` = 32B, tag = 8B) — it cannot and need not verify the hint
targets anyone in particular, the same trust position as share content.

**The hint is mandatory on every secret (ruled, July 2026).** If it were
optional, presence/absence would publicly partition secrets into
"recipient is discoverable" vs "arranged out-of-band" — a one-bit identity
leak of exactly the class this plan removes. A creator who wants no discovery
supplies **random bytes** instead: X25519 accepts any 32 bytes and the tag is
hash output, so a null hint is cryptographically indistinguishable from a real
one and the anonymity set stays whole. spec.md must document the random-hint
opt-out explicitly, so "hint is mandatory" is not misread as "discovery is
mandatory".

### Workflow — creation (creator client)

1. Obtain `A` out-of-band (or generate a keypair for the recipient, or use a
   published bulletin-board key — §2).
2. `derive_detection_hint(A)` (new Rust/WASM helper): generate fresh `(e, R)`;
   `shared = X25519(e, A)`; `tag = SHA256(domain ‖ shared)[:8]`; **discard `e`
   and `shared`**.
3. Submit `MsgRequestGuardians` carrying `DetectionHint{1, R, tag}` in place of
   today's `recipient_public_key`. Everything else in the create flow is
   unchanged — the inner payload encryption still targets `A`.
4. The chain stores the hint on the slim record and includes it in the
   reservation event.

### Workflow — discovery (recipient or their wallet)

1. Enumerate candidate hints: paginated `SecretMeta` query (state), or the
   reservation events via any indexer.
2. Per candidate: `shared' = X25519(a, R)`; `tag' = SHA256(domain ‖ shared')[:8]`;
   `tag' == tag` ⇒ this secret is addressed to `a`'s owner. One scalar
   multiplication per candidate — embarrassingly parallel; false positives are
   2⁻⁶⁴ (§6.2). SDK helper: `findMySecrets(privKey)` over `scan_hint`.
3. On match: read the reveal schedule from the same record; after guardians
   reveal, reconstruct as normal (combine key shares → `sk_s` → decrypt `C` →
   `C_r`) and decrypt the inner layer with `a` — recipient key *usage* is
   exactly today's, only its publication is gone.

### Why nobody else can run the match test

Today, linking is **byte equality on public data** — the same 32 bytes of `A`
sit verbatim on every secret. With hints, the only way to test a candidate
secret against a key is to recompute `shared`, and both routes to it contain a
private key: `X25519(e, A)` needs `e` (destroyed after creation) and
`X25519(a, R)` needs `a` (the recipient's). Deriving `shared` from the two
*public* halves (`R`, `A`) is the computational Diffie–Hellman problem — the
same hardness that keeps a TLS session key from an eavesdropper who saw the
whole handshake. Knowing a recipient's public key therefore no longer helps an
observer at all; each `(R, tag)` is indistinguishable from random, and every
secret's pair is different, so there is no cross-secret pattern either.

**Caveat (honest limit):** if the recipient's *private* key leaks, the holder
can retroactively scan their entire history — but they can also decrypt the
payloads, so the hint degrades exactly in step with total key compromise,
never before it.

**No new cryptography.** The hint is the same ephemeral-static X25519 exchange
`crypto.rs` already performs inside `encrypt_for_public_key` — composition of
the audited primitive, consistent with the key-share plan's discipline.

### How it resolves the issues

| Issue today | Property after Part II |
|---|---|
| All secrets to one recipient are mutually linkable (same stored pubkey) | Each secret carries a fresh random-looking `R` — **no two secrets are linkable** on-chain |
| Per-recipient intelligence: count, cadence, sizes, aggregate `bump` spend | Not aggregatable — no per-recipient join key anywhere in state or tx history |
| Discovery is world-readable (anyone can scan for a known pubkey) | Discovery requires the recipient's **private** key — recipient-only |
| The long-term key sits in immutable tx history forever | The long-term key never appears in any transaction or state |

## 4. The resulting privacy model

With references removed and hints in place, a public observer (state + full
transaction history) learns about a secret's recipient: **nothing** — before,
during, and after the reveal. Post-reveal the payload becomes reconstructable
by anyone but readable only by the recipient (or by everyone, if the creator
deliberately used a published bulletin-board key). The only parties who can
associate a secret with its recipient are the creator (who knows everything
anyway) and the recipient. There is no accidental identity channel left, and
no deliberate one either.

## 5. Surface area and sequencing

| Surface | Change |
|---|---|
| `secret.proto` / `tx.proto` / `query.proto` | **Delete** `recipient_public_key` and `recipient_metadata` outright and re-cut the messages cleanly — no `reserved` markers, renumber freely (pre-launch, nothing to stay compatible with); add `DetectionHint detection_hint`; re-cut `SecretView` the same way; add the `HintsSince(height)` cursor query over a `(created_at, secret_id)` index (§6.3) |
| Keeper validation | Drop the Ed25519 point check and the three `MaxMetadata*` constants + map validation; validate hint shape (version, 32B `R`, 8B tag) |
| `rust/src/` + WASM/FFI | `derive_detection_hint(recipient_pub) -> DetectionHint` and `scan_hint(recipient_priv, hint) -> bool` — thin wrappers over the existing X25519 core |
| TypeScript SDK | Create flow builds the hint; new scan API (`findMySecrets(privKey)`) over the `HintsSince` cursor query with a persisted resume height (§6.3); metadata APIs deleted |
| Guardian daemon | Unaffected (never reads any of these fields) |
| spec.md | Recipient discovery section (hint construction, normative domain-separation string, scan flow); removal of the metadata field and its caps from all tables; the bulletin-board and out-of-band notification conventions (§2) documented as the recipient-reachability story |
| Retention plan | Canonical record hashes the new field set; its map-determinism rule (§4.2 there) is **deleted outright** — no proto map remains on the slim record |

**Ordering**: key-share (landed) → **this plan** → retention. Breaking proto
change with **no migration and no compatibility shims** — pre-launch, every
proto struct is freely replaceable (ruling, July 2026; consistent with
[DONE_SECRET_MODEL_RESTRUCTURING_PLAN.md](DONE_SECRET_MODEL_RESTRUCTURING_PLAN.md));
absorbed by `make dev-reset`.

## 6. Open questions (refinement backlog)

1. **Dual-key hints (deferred by design).** Monero-style view/spend split:
   detection via `X25519(view, R)`, decryption requiring the spend key — lets
   a recipient delegate *detection* to an indexer/notifier without granting
   decryption, serving "notify the inattentive-but-enrolled recipient" with
   zero published identity. The `version` field exists so this adds without a
   proto re-cut. (Ruled: single-key now.)
2. ~~**Tag length.** 8B vs 4B.~~ **Ruled (July 2026): 8 bytes.** False
   positives at 2⁻⁶⁴; the 4B scanning-deniability argument is weak tea when
   scanning is local.
3. **Scan ergonomics — resolved to a requirement (July 2026).** Discovery will
   be a common polling operation, and the correct pattern is **incremental**:
   hints are immutable and written once, so a poller scans each hint exactly
   once and resumes from a cursor — steady-state cost tracks the creation
   rate (~50µs of X25519 per new secret), never chain size. Full-history
   scanning is paid once at first wallet sync. But `SecretMeta` pagination is
   keyed by secret ID (a UUID — random order), which gives state-based pollers
   no cursor. **Requirement**: a creation-ordered hint feed — a
   `(created_at, secret_id)` index serving a `HintsSince(height)` query
   returning compact `(secret_id, R, tag)` tuples (~50B each) — so any client
   resumes from a height cursor with one range read. The reservation event
   (which carries the hint) remains the push-based path for
   indexers/subscribers; the SDK's `findMySecrets(privKey)` should use the
   cursor query with a persisted resume height. Bulletin-board keys scan
   identically — a board is just a recipient whose private key is published.
4. **Un-enrolled recipient UX.** The generate-a-keypair-for-them flow (§2)
   should be a first-class SDK/client feature (key + secret ID exported as a
   printable claim document), so the estate route is easy enough that nobody
   misses the removed reference field.
