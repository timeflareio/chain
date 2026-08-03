# Low-Order Encryption Key Validation Plan

**Priority**: P1 — a cryptographic validation gap on a live protocol surface.
Guardian registration is open to anyone, and the gap lets a registrant make its
own share publicly readable with no bond at risk and no slashable event. Must
close before testnet exposes registration to third parties.

**Status**: done (29 July 2026) — executed on branch
`worktree-low-order-key-validation`, [PR #115](https://github.com/leedavis81/timeflare/pull/115)
(all checks green). All six phases landed; `make test`, `make test-clients`,
`make verify` and `make verify-boundaries` green locally before the PR. The
host-side `make e2e` against the native devnet was **not** run — the devnet was
in use — but CI's Lifecycle job exercised the headless e2e against a compose
devnet and passed.

**Origin**: [PROTOCOL.md](../../CHAIN_MECHANICS.md) "Open Defects & Cleanup Notes" §1,
found during the 28 July 2026 PROTOCOL.md / ECONOMICS.md alignment sweep against
`x/secrets/`. Not previously recorded anywhere.

**Components**

| Surface | What it touches |
|---|---|
| `docs/spec.md` | Guardian Registration (encryption-key rules), Guardian Key Rotation / Key Epochs (Normative), the Configuration Parameters tables, Common Attack Vectors (new entry) |
| `docs/PROTOCOL.md` | Open Defects §1 → resolved; the Cryptographic bindings row |
| `docs/ACCEPTED_TRADEOFFS.md` | no change expected — every entry point is in scope, so no new accepted cost is created |
| `crypto/` | new exported validator (the module already depends on `golang.org/x/crypto`) |
| `x/secrets/keeper/` | `validation.go` (`isValidPublicKey`), `invariants.go` (genesis-path coverage) |
| `x/secrets/types/` | **untouched** — the boundary keeps the predicate keeper-side (§4.4); the existing all-zeros check in `message_rotate_key.go` stays as it is |
| `rust/src/crypto.rs` | `encrypt_for_public_key` — the silent-failure half |
| `rust/src/detect.rs` | `scan_hint` / `derive_detection_hint` — the same missing check on the hint exchange |
| `typescript-sdk/` | pre-seal guardian-key validation and a typed error (Phase 5) |
| `mobile-client/` | vendored SDK tarball repack + lockfile integrity sync, if the SDK changes |
| `testdata/vectors/` | hostile-input corpus pinning Go/Rust agreement on **rejection** |

---

## 1. What this plan does

Rejects X25519 small-order public keys at **every** chain entry point that
accepts one (§4.2), and makes the WASM/TypeScript client fail loudly rather than
silently on the same inputs — on the hint exchange as well as the share
exchange. Adds the shared vector coverage that would have caught the divergence.

## 2. Why

The chain's only check on a guardian encryption key is "exactly 32 bytes, not all
zeros" (`isValidPublicKey`, `x/secrets/keeper/validation.go`), applied at
registration and at rotation. Curve25519's torsion subgroup has order 8, which
yields **five canonical u-encodings** of small order plus **two non-canonical**
ones that reduce to them — the seven entries of libsodium's `has_small_order`
table. Only the all-zeros entry is rejected, so the other six register cleanly.

The two implementations then diverge on exactly those inputs:

- **Go** (`crypto/encryption.go`, via `curve25519.X25519`) returns
  `bad X25519 remote ECDH input: low order point`. It fails loudly.
- **Rust/WASM** (`rust/src/crypto.rs`, `encrypt_for_public_key`) calls
  `ephemeral_secret.diffie_hellman(...)` and hashes `shared_secret.as_bytes()`
  directly. x25519-dalek v3 does not reject small-order points — it returns an
  all-zero shared secret and offers `was_contributory()` for the caller to check.
  That method is called nowhere in `rust/` or `typescript-sdk/`.

So a key share encrypted by the WASM/TS client to a small-order guardian key is
encrypted under `SHA256(0x00…00 ‖ "timeflare_encryption")` — a **publicly
computable key**. Any observer can decrypt that guardian's share straight out of
public chain state as soon as Phase 2 lands.

**Why this is worse than ordinary Sybil collusion.** The attacker never accepts
the assignment, so **no bond is ever at risk**, and nothing here is slashable:
early-reveal reporting requires an accepted assignment *and* an actual reveal,
and this is neither. No reveal-time coordination is needed either, because the
shares are readable by anyone rather than by a coalition. It remains **bounded by
the draw** — breaking a time-lock still needs `≥ threshold` of the `max_shares`
drawn to be the attacker's own small-order-key guardians, so the gap-bound
arithmetic in [ACCEPTED_TRADEOFFS.md §4](../../CHAIN_MECHANICS.md) holds by
count. What collapses is the *cost* of exploiting a registry fraction once held.

**A second, cheaper effect.** Because the Go path errors, a small-order key can
instead make a selected guardian simply undistributable: the creator cannot
produce a share for it, having already paid the creation fee at Phase 1.

### 2.1 The same gap poisons recipient discovery — a separate, third-party harm

`rust/src/detect.rs` has the identical omission on a different exchange.
`scan_hint` computes `shared = X25519(a, R)` from the recipient's private key and
the hint's `ephemeral_pub`, then `hint_tag(shared.as_bytes())` — again with no
`was_contributory()` check.

If `R` is a small-order point, `X25519(a, R)` is all-zeros **for every recipient
key `a`**, so `hint_tag` returns one constant for everybody. An attacker
publishing a secret whose hint is `(R = small-order point, tag = SHA256(domain ‖
0…0)[:8])` makes **every** recipient's scan return true. That voids the property
asserted three lines above the constant in the same file — "Tag length in bytes:
2^-64 scan false positives" — turning a 2⁻⁶⁴ false-positive rate into 100% for
every scanning client on the network.

This one is **not** creator-harming and needs no guardian registration at all:
the cost is one secret creation (~0.15 VEIL), the harm is a wasted
fetch-and-decrypt cycle for every recipient that scans, and it persists in
`HintsByCreation` until Stage 2 retention prunes it (~6 months). One creation, N
wasted client operations — the only entry point in this plan with an
amplification factor.

Rejecting a small-order `R` does **not** conflict with the deliberate
unverifiability of hint content (spec.md "Recipient Discovery": random bytes are
a valid no-discovery hint). Random bytes are a small-order point with probability
~2⁻²⁵⁰; rejecting a value that cannot be a real ephemeral key is not inspecting
what the hint means.

**Why the vector corpus did not catch it.** `testdata/vectors/` pins the two
implementations to agreement on *valid* inputs. It says nothing about their
behaviour on hostile ones — which is exactly where they diverge. Worth reading
against CLAUDE.md's "two implementations of one concern is a defect" rule: the
shared-corpus mitigation is narrower than it reads, and Phase 4 below is what
actually closes it.

### 2.2 Evidence status

The Go rejection was **reproduced directly** against `curve25519.X25519`: all
seven table entries error, and they do so for *any* scalar, because X25519
clamps scalars to a multiple of 8, making `s·P` the identity for any `P` whose
order divides 8.

Both Rust halves — the silent share encryption (§2) and the universal hint match
(§2.1) — are derived from the call sites plus x25519-dalek v3's documented
semantics, and **neither is yet reproduced by a running test**. Phase 1 below is
that test and covers both; it is a prerequisite for the rest, because the plan
does not proceed on an inferred failure mode. The hint case is the cheaper one to
demonstrate: two arbitrary recipient keypairs and one small-order `R` should
produce identical tags, which is a self-contained assertion needing no chain.

## 3. What this plan does not solve

- **It does not make possession-based evidence sound against leaks.** A guardian
  that holds a legitimate key can still leak its share; that is
  [ACCEPTED_TRADEOFFS.md §2 and §3](../../CHAIN_MECHANICS.md).
- **It does not retroactively purge anything.** `GuardianKeyIndex` reserves every
  key ever registered, forever, and tightening validation does not remove an
  already-stored key. Pre-launch there is no live state, so no migration exists;
  if that changes before this ships, a migration becomes a required phase and
  this plan must be reopened.
- **It does not change the Sybil economics.** The 1,000 VEIL entry fee is the
  Sybil price and stays exactly as it is
  ([ACCEPTED_TRADEOFFS.md §9](../../CHAIN_MECHANICS.md)). This plan removes a way
  to make a purchased registry fraction cheaper to exploit, not the fraction
  itself.
- **It does not address the unrelated concerns** sharing the same registry:
  unpriced assignment responsiveness and registry-scaled phase-1 gas
  ([PROTOCOL.md](../../CHAIN_MECHANICS.md) Security Observations §2 and §3, the latter owned
  by [DONE_GUARDIAN_SELECTION_SCALABILITY_PLAN.md](DONE_GUARDIAN_SELECTION_SCALABILITY_PLAN.md)).
- **No honest actor is affected.** The guardian daemon generates keys properly, so
  the only holder of such a key is a deliberate attacker. There is no UX
  regression to manage and no legitimate registration this breaks.

## 4. Design

### 4.1 The check

Delegate to `golang.org/x/crypto`, do not hand-roll a blacklist:

```go
// crypto/encryption.go (or a sibling) — module already depends on x/crypto
// ValidateX25519PublicKey rejects keys unusable for share encryption: wrong
// length, or a small-order point (including non-canonical encodings that
// reduce to one). Deterministic and consensus-safe — a pure function of the
// input bytes.
func ValidateX25519PublicKey(key []byte) error
```

Implementation: attempt `curve25519.X25519(fixedScalar, key)` and reject on
error. Any fixed clamped scalar works, per the clamping argument above; the
function must be a pure byte-in/error-out predicate with no randomness, so every
node reaches the same verdict.

**Not a blacklist.** libsodium's seven-entry table was considered and rejected:
it has to be extended by hand to cover the non-canonical encodings, and a table
that ships one entry short fails silently in the exact direction this defect
already went. Delegating puts correctness in a maintained library rather than in
a table of ours. The blacklist's only advantage was dependency-freedom inside
`x/secrets/types`, which §4.4's wiring makes irrelevant.

**No new component.** This extends `crypto/`, which already owns the X25519
primitives and already carries the `golang.org/x/crypto` dependency — the
architectural-minimalism default of extending what exists.

### 4.2 Scope — every entry point, no exceptions

All five places a 32-byte X25519 key enters are in scope:

| Entry point | Why in scope |
|---|---|
| `MsgGuardianRegister.EncryptionPublicKey` | Third-party shares are encrypted to it — the core defect (§2) |
| `MsgGuardianRotateKey.NewKey` | Same key, later epoch; already carries an all-zeros check in `ValidateBasic` |
| `DetectionHint.EphemeralPub` | Poisons every recipient's scan, with amplification and no registration needed (§2.1) |
| `MsgUserDistributeShares.SecretPublicKey` (pk_s) | Uniformity — see below |
| Genesis (guardian records + key-history entries) | State assembled outside the message path must not smuggle in what the handlers reject |

`pk_s` is the one entry point where an exception was arguable, and it was
rejected. A low-order `pk_s` makes the payload ciphertext `C` publicly
decryptable, but `C` yields only `C_r`, which the recipient-encryption layer
still protects, and the creator already holds the plaintext — so the harm is
self-inflicted and lands squarely under the existing GIGO ruling (spec.md
"Invalid Share Submission", [ACCEPTED_TRADEOFFS.md §1](../../CHAIN_MECHANICS.md)).
Fault attribution also survives: no scalar times the basepoint yields a
small-order point, so the `sk_s` → `pk_s` check fails and correctly reads as
creator fault.

Excluding it is nonetheless the more expensive option. The check is one shared
predicate and `pk_s` already has a length assertion at `invariants.go:423` to sit
beside, so inclusion costs about two lines. Exclusion costs a spec paragraph
justifying the asymmetry, a permanent inconsistency for every future reader to
re-derive, and a reviewer conversation — for a rule that has to be argued rather
than stated. Uniform rejection is cheaper to build, cheaper to document and
cheaper to audit.

### 4.3 The Rust half

Check `SharedSecret::was_contributory()` before deriving anything from a shared
secret, and return a `CryptoError` when it is false — in `encrypt_for_public_key`
(`crypto.rs`) **and** in both hint functions (`detect.rs`). Every
`diffie_hellman` call site, not just the one that carries the headline defect.
Defence in depth: once the chain rejects these keys such a value should never
reach the client, but the client must not be the component that fails silently,
and `derive_detection_hint` operates on a recipient key the chain never sees at
all.

### 4.4 Where the chain check runs

`make/go-quality.mk` (`verify-boundaries`) forbids `x/secrets/types` from
importing `crypto`, and two of the key-accepting call sites live in that module:
`MsgGuardianRotateKey.ValidateBasic` (which already carries an all-zeros check)
and `GenesisState.Validate`. The predicate therefore runs **keeper-side**, with
the genesis path covered by `CheckStateInvariants` — which already executes at
`InitGenesis` behind a hard halt, so an inconsistent genesis still cannot produce
blocks. That gives complete coverage with **no boundary change and no new
dependency anywhere**.

The alternatives were rejected. Putting a pure-bytes blacklist in
`x/secrets/types` reaches every call site but reintroduces the hand-maintained
table §4.1 rejects. Promoting `golang.org/x/crypto` to a direct dependency of
`x/secrets/types` works against the reason the two leaves are kept independent —
recorded in CLAUDE.md and
[done/DONE_MODULE_BOUNDARIES_PLAN.md](DONE_MODULE_BOUNDARIES_PLAN.md) §3:
they are consumed and tagged separately, so anything handing the wire-contract
module a crypto dependency reaches every external client pinning it. A poor trade
for one predicate, and the general rule it would breach is worth more than the
convenience: do not relax a boundary to solve a problem that has a clean solution
inside it.

The existing all-zeros checks stay. They are a strict subset of the new predicate
and cost nothing, and the one in `message_rotate_key.go` keeps `ValidateBasic`
useful without needing the boundary relaxed.

## 5. Phases

**Phase 1 — Reproduce, before anything else.** *Outcome: all three reproduced,
so the plan proceeded at its stated severity.* The share-encryption half was
demonstrated beyond the shared-secret check — the test decrypts a real
ciphertext using `SHA256(0x00…00 ‖ "timeflare_encryption")`, proving the key
needed no secret material. Three assertions against current
behaviour: (i) the chain accepts six of the seven table entries at
registration and rotation; (ii) `crypto/`'s Go path errors on all seven while the
Rust `encrypt_for_public_key` path yields an all-zero shared secret; (iii) two
unrelated recipient keypairs produce **identical** hint tags for one small-order
`R`, so `scan_hint` returns true for both. These are the failing tests the rest of
the plan turns green, and they are what upgrade §2 and §2.1 from inferred to
demonstrated. If either Rust half does *not* reproduce, stop and re-scope — the
chain-side hardening is still worth doing, but the severity changes materially and
this plan's priority should be revisited rather than carried forward on a stale
premise.

**Phase 2 — Spec first.** Update `docs/spec.md` before any production code:
Guardian Registration's encryption-key rules, Guardian Key Rotation / Key Epochs
(Normative), the Configuration Parameters tables, and a new Common Attack Vectors
entry stating the rejection as normative protocol behaviour for all five entry
points, plus a line in "Recipient Discovery" making clear that rejecting a
small-order `R` does not weaken the deliberate unverifiability of hint content.

**Phase 3 — Chain validation.** Add `crypto.ValidateX25519PublicKey`; call it
keeper-side from all five entry points in §4.2; extend `CheckStateInvariants` to
cover the genesis path, per §4.4.

**Phase 4 — Cross-implementation pinning.** Add the `was_contributory()` checks
per §4.3 (`crypto.rs` and `detect.rs`), then add hostile-input vectors to
`testdata/vectors/` asserting that **both** implementations reject the same
inputs. This is the deliverable that closes the drift gap, not an afterthought to
Phase 3: without it the next divergence is invisible again.

**Phase 5 — Clients.** The TypeScript SDK validates guardian keys **before**
sealing and surfaces a typed error. Relying on the WASM boundary alone would
surface an opaque crypto failure mid-seal, after the creator has already paid for
Phase 1; the actionable message — "guardian X's registered key is unusable" — can
only be phrased at the SDK layer, which is also where the offending guardian can
be named and skipped. Then `pack-sdk.sh` repack plus lockfile integrity sync, or
mobile CI silently runs the stale SDK.

**Phase 6 — Doc sweep (a deliverable in its own right).** PROTOCOL.md Open
Defects §1 → resolved; the Cryptographic bindings row updated to state what is
now enforced. Correct the muddled phrase in the current §1 text — it says
"attempt an X25519 against the basepoint", but testing a candidate *public* key
means `X25519(anyClampedScalar, candidate)`; the basepoint derives a public key
from a private one.

## 6. Open questions

None — all decisions are settled (owner ruling, 29 July 2026). The three that
were open are folded into the design above: where the predicate runs and why the
module boundary is not relaxed (§4.4), delegation to `curve25519.X25519` over a
maintained blacklist (§4.1), and SDK-side validation ahead of the WASM boundary
(Phase 5). Scope was settled earlier and is §4.2.

## 7. Cross-component sweep

No symbol is renamed or removed, so the grep-driven audit is narrow, but three
checks are required before this reaches `done`:

1. Every call site accepting a 32-byte key is enumerated in §4.2 and all five are
   in scope — confirm each is actually covered, rather than assuming the shared
   predicate reached it.
2. `was_contributory()` appears nowhere in `rust/` or `typescript-sdk/` today.
   After Phase 4, grep `diffie_hellman` and confirm **every** call site is
   covered — `encrypt_for_public_key` and `decrypt_*` in `crypto.rs`, and both
   `derive_detection_hint` and `scan_hint` in `detect.rs`. The hint pair is the
   easy one to miss precisely because the headline defect is about shares.
3. `crypto/` and `rust/` must agree on rejection for every new vector, asserted
   by both suites (`make test-clients` and the Go corpus tests).

## 8. Acceptance

- Phase 1's reproducing test passes before the fix and fails after, for both
  implementations.
- Registration and rotation reject all seven table entries, the two
  non-canonical encodings included; existing valid-key tests are unchanged.
- A genesis carrying such a key fails the import sweep with a hard halt.
- `make test`, `make test-clients`, `make verify` and `make verify-boundaries`
  green locally **before** a PR is raised; the PR watched until CI is green.
- `make e2e` unaffected — no honest path changes.

## 9. Notes from execution

Four things surfaced during execution that the plan had not anticipated. All are
folded into the design above; recorded here because each was a judgement call.

1. **The small-order count was wrong in the plan.** It said "eight small-order
   points", conflating the order-8 torsion subgroup with the number of
   u-coordinate encodings. There are **five canonical** small-order u-encodings
   plus **two non-canonical** ones — libsodium's seven-entry table. The chain was
   accepting six of the seven, not seven of eight. Corrected throughout.

2. **Invariant 8 is scoped to hints that are present, not to hint presence.**
   The first draft asserted every secret carries a usable `DetectionHint`, which
   broke 59 existing tests — all of them fixtures that hand-build `types.Secret{}`
   and write it straight to the store with no hint. Investigating showed
   `GenesisState.Validate` never checked hint shape either, so hint *presence* is
   a property nothing guarantees, and an absent hint is harmless (it means no
   recipient can discover the secret — the no-discovery choice taken to its
   limit). Asserting it here would have smuggled a second, unrelated invariant in
   under this one, so the check skips absent hints and rejects only present-but-
   unusable ones. The fixtures were left alone.

3. **The SDK predicate is exported from Rust, not reimplemented in TypeScript.**
   The plan said the SDK validates before sealing but not how. A TypeScript
   small-order table would have been a second copy of a security-relevant list —
   exactly what §4.1 rejects for the chain. Instead `is_usable_x25519_public_key`
   is a new `#[wasm_bindgen]` export over the same `was_contributory()` predicate,
   so there is one source of truth and a Rust test pins the export against the
   corpus.

4. **`npm` will not resync the vendored tarball's lockfile integrity.** For a
   `file:` dependency it reuses its content-addressable cache and leaves the old
   hash in place — `npm install`, `npm install --package-lock-only`, and dropping
   the field all failed to rewrite it. The integrity is by definition the
   tarball's sha512, so it was written directly and then **verified by a clean
   `npm ci`**, which is what CI runs. Worth knowing for the next SDK change: a
   silently stale integrity is the failure mode this step exists to prevent.

5. **The cross-component sweep was scoped too narrowly, and CI caught it.**
   Changing `detect::derive_detection_hint` to return `Result` broke
   `mobile-client/packages/crypto/rust/src/lib.rs` — the UniFFI wrapper crate,
   a **second consumer of the same Rust crate** that §7's sweep never looked at
   because the grep was scoped to `rust/src/` and `typescript-sdk/src/`. The fix
   was one `?` (a `From<timeflare_crypto::CryptoError>` impl already existed),
   but the lesson is the sweep's, not the fix's: §7 item 2 asked for every
   `diffie_hellman` call site and got them, while nothing asked for every caller
   of a **changed signature**. That is the ShareIndex lesson (CLAUDE.md) arriving
   by a different door. Any future plan that changes a shared Rust signature
   should grep repo-wide for the symbol, `mobile-client/packages/crypto/rust`
   included, before claiming the sweep is clean.

   Related: a fresh worktree has no `mobile-client/packages/crypto/vendor/`
   (gitignored), so that crate's vector tests fail on a missing corpus until
   `vendor.sh` — or its first step alone — has run. Not a defect, but it makes
   "cargo test in the FFI crate" look broken on a clean checkout.

## 10. Related plans

- [automated/CRYPTO_ASSURANCE_PLAN.md](../automated/CRYPTO_ASSURANCE_PLAN.md) — the
  broader crypto-assurance surface; this plan is one concrete defect within it and
  should be cross-referenced there rather than merged into it.
- [automated/SECURITY_AUDIT_READINESS_PLAN.md](../automated/SECURITY_AUDIT_READINESS_PLAN.md)
  — a resolved finding of this shape is audit-package material.
- [done/DONE_GUARDIAN_KEY_ROTATION_PLAN.md](DONE_GUARDIAN_KEY_ROTATION_PLAN.md)
  — established the key-epoch model and the permanent uniqueness index this plan
  validates entries into.
- [done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](DONE_KEY_SHARE_ARCHITECTURE_PLAN.md)
  §8 — the share-entropy analysis whose 256-bit claim this defect voids for an
  affected share.
