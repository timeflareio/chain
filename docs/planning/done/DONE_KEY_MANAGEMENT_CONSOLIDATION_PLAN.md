# Key-management consolidation across components

**Priority**: P0 — the same 24-word wallet mnemonic currently resolves to two
different accounts depending on which tool restores it, and the implementation
contradicts a parameter `docs/spec.md` already documents. Nothing is deployed
and no key material needs preserving, so every fix here is a free, correct
realignment today. That is exactly why it is urgent: this is the cheapest this
work will ever be.
**Status**: done (1 August 2026 — PR #134, merged e33605d9; all seven open
questions ruled by the owner on 1 August 2026 and folded into the design
below). Every phase landed with its vector pins; the full devnet gate
(lifecycle + 53 scenario assertions) and CI were green at merge.
**Origin**: cross-component key-management comparison, 1 August 2026 —
guardian versus mobile client, prompted by
[guardian/PENDING_GUARDIAN_PRE_TESTNET_SWEEP.md](guardian/PENDING_GUARDIAN_PRE_TESTNET_SWEEP.md)
and read against
[DONE_MOBILE_PRE_TESTNET_SWEEP.md](DONE_MOBILE_PRE_TESTNET_SWEEP.md).
**Components**: `app/` (`app.go`, `config.go` — `ChainCoinType` moves out);
`x/secrets/types` (`constants.go` — the constant's new home);
`typescript-sdk/src/protocol/wallet.ts`, `crypto.ts`;
`mobile-client/packages/crypto/rust/src/lib.rs`,
`mobile-client/app/src/screens/onboarding/identityStore.ts`;
`guardian/custody/` (`mnemonic.go`, `keyfile.go`),
`guardian/blockchain/keys.go`, `guardian/cmd/guardiand/cmd/` (new `wallet`
group); `devnet/` (passphrase-file writers); `crypto/encryption.go`;
`testdata/vectors/` (new `wallet_derivation.json`, existing
`client_conventions.json`); `docs/guides/CLIENT_CONVENTIONS.md`,
`docs/guides/CONTAINERS.md`; `.github/workflows/ci.yml`.

**Protocol surface**: no wire change — no proto, no keeper, no message
change. But §1 settles a **chain parameter already documented in
`docs/spec.md:1254`**, phase 1 moves `ChainCoinType` into `x/secrets/types`
(types-module content changes are protocol changes per CLAUDE.md, so the move
lands with the spec references in the same PR), and §2 adds a section to
`CLIENT_CONVENTIONS.md`, which is an inter-client interface. All were approved
by the owner on 1 August 2026, and the spec/convention edits lead their phases.

---

## Why this is one plan

Six findings, four components, one concern: *how a private key is derived,
encoded, stored and recovered, and whether two implementations of that agree.*
Splitting them by component would put the wallet-derivation divergence in three
plans that each see half of it — which is how it survived this long. The
project rule these all sit under is CLAUDE.md's architectural minimalism:
"Two implementations of one concern is a defect, not a design. Where
duplication is genuinely unavoidable, it must be pinned by shared test data so
drift is caught mechanically."

The good news first, because it shapes what follows: the two key stacks are
**largely aligned and mostly pinned**. `testdata/vectors/` already pins
encryption, HMAC, detection hints, rebate commitments and low-order keys across
Go, Rust, the chain and the mobile bindings, and the mobile crypto provider
states the discipline outright — *"crypto source of truth stays in `rust/`;
this app consumes, never forks"* (`mobile-client/app/src/crypto/provider.ts`).
What follows are the places that escaped it.

**Standing constraint (owner ruling, 1 August 2026): there is no deployed state
and no key material to preserve.** Every choice below is therefore made on
correctness alone — no compatibility shim, no dual-path support, no migration
step, and no deprecation period. Where a fix changes a derived address, a
stored format or an exported symbol, the change simply lands and the devnet is
reset. Execution must not reintroduce a compatibility concern this ruling has
already removed; if one appears to be needed, that is a signal to stop and
raise it rather than to design around it.

---

## 1. Critical — the same mnemonic derives two different accounts

The chain sets BIP44 coin type **9733**:

- `app/app.go:59` — `ChainCoinType = 9733`
- `app/config.go:15` — `config.SetCoinType(ChainCoinType)`
- `docs/spec.md:1254` — documented as a chain parameter: "**Coin Type** | 9733
  | BIP44 HD wallet derivation"

So `timeflared keys add` derives at `m/44'/9733'/0'/0/0`.

The SDK does not. `generateWallet()`
(`typescript-sdk/src/protocol/wallet.ts:24`) calls
`DirectSecp256k1HdWallet.generate(24, { prefix: CHAIN_PREFIX })` and passes no
`hdPaths`, so cosmjs applies its default —
`hdPaths: [makeCosmoshubPath(0)]` = `m/44'/118'/0'/0/0`
(`@cosmjs/proto-signing/build/directsecp256k1hdwallet.js:49`,
`@cosmjs/amino/build/paths.js:9-17`). A repo-wide grep for `hdPaths`,
`HdPath` or `coinType` in `typescript-sdk/src/` returns nothing.

Derived from the same test mnemonic, on 1 August 2026:

```
coin type 118   m/44'/118'/0'/0/0    => tmflr1r5v5srda7xfth3hn2s26txvrcrntldjuu5vchq
coin type 9733  m/44'/9733'/0'/0/0   => tmflr16r63wvs6vsulx4hkmpeuf0hus4tuh0r2kwtg5s
```

**What it costs.** A user backs up their mobile wallet's 24 words, later
restores with `timeflared keys add --recover`, and lands on a different, empty
account. It presents as "my tokens are gone" — the worst possible failure text
for a token the user cannot recover by any other route. It also breaks the
implicit promise of BIP39: that the phrase is the wallet, portable across
tools.

**Why nobody has hit it.** The two derivations never meet. The mobile e2e
faucet credits by address (`mobile-client/e2e/src/faucet.ts`), the devnet creates
its keys with `timeflared`, and the app never round-trips a phrase through the
chain binary. Nothing tests the pairing, and `CLIENT_CONVENTIONS.md` pins the
*identity* mnemonic in §5 while saying nothing about wallet HD paths.

Note also that this is an implementation contradicting `docs/spec.md`, which
the spec-authority rule resolves in the spec's favour by default — the SDK is
wrong, and that is how it was ruled: **the SDK adopts 9733** (1 August 2026).

**Not affected**: courier keys for funded claim kits. `generateCourier` and
`walletFromPrivateKeyHex` (`wallet.ts:56,37`) use raw secp256k1 scalars with no
derivation path, so kits are path-independent and are untouched by the change.

---

## 2. High — the §5 mnemonic is implemented twice and asserted once

`CLIENT_CONVENTIONS.md` §5 pins the identity-key backup format: the raw 32-byte
X25519 scalar as BIP39 entropy, 24 English words, no derivation scheme. Two
implementations exist:

- `guardian/custody/mnemonic.go:17,28` — `github.com/cosmos/go-bip39`
- `typescript-sdk/src/protocol/conventions.ts:236,250` — `@cosmjs/crypto`'s
  `Bip39`/`EnglishMnemonic`

`testdata/vectors/client_conventions.json` carries `mnemonic` and
`mnemonic_invalid` arrays. Its only test consumer in the entire repository is
`typescript-sdk/src/protocol/__tests__/conventions.test.ts`. The guardian's Go
implementation has round-trip tests only (`custody/custody_test.go:192-214`) —
self-consistency, which cannot catch divergence from the corpus.

This directly violates the convention's own clause (`CLIENT_CONVENTIONS.md:23`):
"every implementation asserts them in CI."

**Verified, 1 August 2026**: a throwaway probe ran the guardian's
`KeyToMnemonic`/`KeyFromMnemonic` against the corpus. It matched all six valid
vectors and rejected all three invalid ones. So the two agree **today**; what
is missing is the mechanism that keeps them agreeing.

Related documentation gap: `CLIENT_CONVENTIONS.md`'s status line lists
consumers as "the mobile client, the TypeScript SDK, and any future web
app/CLI". The guardian implements §5 and cites it in `mnemonic.go:9-13`, but is
not listed — so a future change to §5 would not obviously implicate it.

---

## 3. High — X25519 derivation is forked to a third library

Three implementations of "public key from private scalar" exist:

- `crypto/encryption.go:147` — Go, `golang.org/x/crypto/curve25519` (guardian
  and chain)
- `rust/src/crypto.rs` — Rust, `x25519-dalek` (the WASM and UniFFI surfaces)
- `mobile-client/app/src/screens/onboarding/identityStore.ts:104` —
  **`@noble/curves`**

The first two are pinned together by `encryption.json` and asserted in CI. The
third is not; it is checked against an RFC 7748 base-point vector locally
(`app/test/identityStore.test.ts:84-89`), which is good practice but is neither
the shared corpus nor run by any workflow.

**The cause is a hole in one binding surface, not a decision.** The underlying
Rust crate already exposes the function — `rust/src/lib.rs:202`,
`public_key_from_private` — and the WASM surface exports it. The mobile UniFFI
wrapper (`mobile-client/packages/crypto/rust/src/lib.rs`) exports seven
functions and not that one, so the SDK's `CryptoProvider` surface has no
derive-public-key member, so `identityStore.ts` reached for `@noble`. The code
says so itself at `:26-30` with a `TODO(crypto-surface)`.

This matters more than a tidiness point because the derived key is compared for
equality on a critical path — `guardian/cmd/guardiand/cmd/key.go:330` and
`rotate_key.go:95` check a Go-derived public key against the on-chain record
before a restore or rotation is declared successful. That comparison is exactly
what `encryption.json` protects, and `@noble` sits outside it.

The mobile sweep tracks the *dependency-declaration* half of this (F21) and its
remediation plan pins the packages as direct dependencies — while stating
plainly that "`TODO(crypto-surface)` remains the real fix"
(`DONE_MOBILE_SWEEP_REMEDIATION_PLAN.md:184`). The real fix spans the Rust
wrapper, the SDK interface and the app, which is why it has no owner. This plan
takes it.

---

## 4. Medium — the guardian can manage one of its two key domains

The guardian holds two keys. Their tooling could hardly be more different:

| | Share key (X25519) | Wallet key (secp256k1) |
|---|---|---|
| Create | `guardiand config create-encryption-key` | **none** |
| Backup | `guardiand key backup` (bundle + 24 words) | keyring files inside the bundle |
| Restore | `guardiand key restore`, verified against chain | **none** |
| Rotate | `guardiand rotate-key` (full ceremony) | n/a |
| Migrate | `guardiand config migrate-key` | **none** |

`guardiand` cannot create, import or recover a signing key. Three call sites
instruct the operator to run `timeflared keys add`
(`cmd/guardiand/cmd/register.go:108`, `cmd/config.go:495`, `:498`) — and the
distroless guardian image ships no `timeflared`.

This is the same shape as the missing `withdraw` verb already recorded in
[guardian/PENDING_GUARDIAN_DEAD_CODE_SWEEP_PLAN.md](guardian/PENDING_GUARDIAN_DEAD_CODE_SWEEP_PLAN.md):
a core lifecycle step a containerised operator cannot perform with the tooling
they were given. It is recorded here rather than there because the decision is
about key-domain parity, not dead code.

---

## 5. Medium — a dead API that writes plaintext key files

`crypto/encryption.go:204-341` exposes an eleven-function key-file family —
each operation in a default-directory and an explicit-path form:
`GenerateEncryptionKeys`/`GenerateEncryptionKeysInDir`,
`LoadPrivateKey`/`LoadPrivateKeyFromDir`/`LoadPrivateKeyFromPath`,
`LoadPublicKeyHex`/`LoadPublicKeyHexFromDir`, `KeysExist`/`KeysExistInDir`,
`GetKeyPaths`/`GetKeyPathsFromDir`. The generate pair writes a **raw 32-byte
plaintext** private key to disk.

Verified (1 August 2026): each of the eleven has **zero** callers anywhere in
the repository outside `crypto/encryption.go` itself, and no test.

It sits beside `guardian/custody/keyfile.go`, which is encrypt-by-default and
explicitly refuses an empty passphrase (`cmd/config.go:1260-1262`). Two
implementations of "write a share key to disk", one of them unreferenced and
without encryption, in a module that is independently consumable.

---

## 6. Low — the passphrase-file reader exists twice, with the same bug in both

`guardian/blockchain/keys.go:97-107` (`readPassphraseFile`) and
`guardian/custody/keyfile.go:135-145` (`ReadPassphraseFile`) are the same
function: read the file, trim, try base64, fall back to raw.

Both therefore carry the base64-guessing defect recorded as finding 30 in the
[guardian sweep](guardian/PENDING_GUARDIAN_PRE_TESTNET_SWEEP.md) — a raw
passphrase that happens to be valid base64 (`correcthorse`, `Passw0rd`) is
silently decoded into binary garbage. That fix now has two sites, and fixing
one would leave the other quietly wrong.

---

## Design

### Phase 1 — settle the wallet derivation path (§1)

**The chain's 9733 stands; the SDK moves to it.** `docs/spec.md:1254` already
documents the parameter, so the SDK is the deviation.

**Spec first.** `docs/spec.md:1254` is confirmed as the authority and a new
`CLIENT_CONVENTIONS.md` section pins the wallet HD path for clients — citing
spec.md rather than restating it, so there is one source and one pointer. Both
land before any code moves, so the derivation path becomes a stated
inter-client interface rather than a default nobody chose.

A client author reading CLIENT_CONVENTIONS §5 today would reasonably conclude
the wallet domain has no pinned requirements, which is precisely the mistake
the SDK made — so the convention section is part of the fix, not documentation
that trails it.

**Fix (obvious).** In
`typescript-sdk/src/protocol/wallet.ts`, pass the chain's path explicitly in
both `generateWallet` and `walletFromMnemonic`:

```ts
import { stringToPath } from '@cosmjs/crypto';

/** The chain's registered BIP44 coin type (app/app.go ChainCoinType). */
export const CHAIN_HD_PATH = stringToPath("m/44'/9733'/0'/0/0");

// ... generate(24, { prefix: CHAIN_PREFIX, hdPaths: [CHAIN_HD_PATH] })
// ... fromMnemonic(mnemonic, { prefix: CHAIN_PREFIX, hdPaths: [CHAIN_HD_PATH] })
```

Exported rather than inlined, because the mobile client and any future web
client must be able to assert they are on the same path.

**One Go declaration.** `ChainCoinType` moves from `app/app.go` to
`x/secrets/types` (`constants.go`), and `app/config.go` reads it from there.
Its consumers are the chain and — from phase 5 — the guardian, and the
guardian may import only `x/secrets/types` and `crypto`: leaving the
declaration in `app/` would force the guardian to either import the chain
module (forbidden, `make verify-boundaries`) or duplicate the literal — the
drift shape this plan exists to remove. The types module is the wire
contract's stated home for constants; the move is content-only (no wire
change) but is a types-module change, so it lands with the spec references in
the same PR. The SDK cannot import a Go constant, so `CHAIN_HD_PATH` is its
pinned copy — held to the same value by the `wallet_derivation.json` corpus,
exactly as `dials.json` pins the dial bounds today.

**Prevention (obvious).** A new `testdata/vectors/wallet_derivation.json`:
mnemonic → expected `tmflr1…` address at the chain path, asserted by a Go test
against the chain's own keyring and by a TS test against the SDK. That is the
established pattern and it is what makes recurrence mechanical rather than
lucky. Include at least one vector whose 118-derived address is also recorded
as a **negative** assertion, so a regression to the cosmjs default fails loudly
with a recognisable message rather than an opaque address mismatch.

**Courier keys stay path-free, and say so.** `generateCourier` and
`walletFromPrivateKeyHex` (`wallet.ts:56,37`) use raw secp256k1 scalars with no
derivation, deliberately — the whole key has to fit in a claim URI. They are
unaffected by this change and must not be "aligned" to the HD path, which would
silently break every funded kit. A comment at both call sites recording that
reasoning is part of this phase: the next reader tidying derivation paths is
exactly who needs it.

**No migration.** Nothing is deployed and no key material needs preserving, so
the address change is simply the corrected behaviour — a devnet reset, not a
transition to design for. Execution should still confirm no devnet fixture or
test hard-codes a 118-derived address, because a stale expected value would
fail as a puzzling assertion rather than an obvious one.

### Phase 2 — pin the §5 mnemonic on the Go side (§2)

**Fix (obvious).** A vector test in `guardian/custody/` reading
`../../testdata/vectors/client_conventions.json` and asserting both directions
over `mnemonic` plus rejection over `mnemonic_invalid`. The probe written
during the investigation already passes and can serve as the starting shape.
Wire it into the guardian's existing test target so CI runs it — no new job is
needed.

**Fix (obvious).** Add the guardian to `CLIENT_CONVENTIONS.md`'s consumer list,
so a future §5 change implicates it by inspection.

Note the guardian's path to the corpus crosses a module boundary (the guardian
is its own Go module). It is test-only data read from disk, not an import, so
no dependency edge is created and `make verify-boundaries` is unaffected —
worth confirming during execution rather than assuming.

### Phase 3 — close the derive-public-key hole (§3)

Three ordered changes, all obvious once the sequence is right:

1. **Export it from the UniFFI wrapper.**
   `mobile-client/packages/crypto/rust/src/lib.rs` gains a
   `public_key_from_private` binding over the same crate function the WASM
   surface already exposes (`rust/src/lib.rs:202`).
2. **Add it to the `CryptoProvider` surface**
   (`typescript-sdk/src/protocol/crypto.ts`). Both implementors — the N-API
   bindings and the JSI module — are generated from the same ubrn config, so
   they gain it together.
3. **Route `identityStore.ts` through the provider** and delete the
   `@noble/curves` import and its `TODO(crypto-surface)`.

`derivePublicKeyX25519` is called from several places in `identityStore.ts` and
its tests, and the provider is injected rather than imported, so step 3 is a
signature change (the function becomes provider-taking, as `createIdentity`
already is) rather than a drop-in. That is the only non-mechanical part.

Keep the RFC 7748 assertion in `identityStore.test.ts` and add the shared
`encryption.json` derivation vectors alongside it, so the mobile path is pinned
to the same corpus as Go and Rust.

**Out of scope here**: `rebate.ts`'s `@noble` usage. It is pinned to
`rebate_commitment.json` and says so ("three implementations, one vector"), and
`app/test/rebate.test.ts` asserts it. That is the pattern working correctly;
leave it.

### Phase 4 — remove the duplicated and dead key-file code (§5, §6)

**Fix (obvious).** Delete `readPassphraseFile` from
`guardian/blockchain/keys.go` and call `custody.ReadPassphraseFile`. Both are
inside the guardian module so no boundary is crossed.

**The consolidation delivers the finding 30 fix itself** (ruled 1 August
2026): the guardian sweep is a findings inventory with no executing plan, so
gating on it would never resolve, and consolidating first would enshrine the
buggy version. The surviving `custody.ReadPassphraseFile` becomes
**raw-only** — the file contains the passphrase verbatim
(whitespace-trimmed), never decoded; the base64 guess is deleted rather than
repaired. That is the operator's natural reading of "a file containing the
keyring passphrase", and the encoding never bought anything — `keyfile.go`
itself records it as obscurity only, with the file's 0600 mode as the real
control. The writers change in the same commit (`custody.WritePassphraseFile`,
`config init`, the devnet scripts stop encoding) and the config-field
documentation states the raw semantics. Nothing is deployed, so no reader of
the old format survives (the standing constraint above).

**Fix (obvious).** Delete the eleven-function key-file family from
`crypto/encryption.go` (§5's full enumeration, lines 204–341) — the
explicit-path workers *and* their default-directory wrappers together, since
a surviving wrapper is the same plaintext trap with a hardcoded directory.
Removing exported symbols from a leaf module would
normally be a breaking change for external consumers, but there are none to
break, and the minimalism rule favours removal outright — a plaintext-writing
key API sitting next to an encrypt-by-default one is a trap, not an option.

### Phase 5 — guardian wallet-key parity (§4)

**`guardiand` grows the commands** rather than the image shipping `timeflared`.
Shipping a second binary to work around a missing subcommand would invert the
dependency the module boundaries deliberately establish — the guardian imports
`x/secrets/types` and `crypto` only, never the chain module, and bundling the
chain binary into its image reintroduces at the packaging layer what the code
carefully avoids.

The shape: a **`guardiand wallet`** subcommand group — create,
import-from-mnemonic and show-address for the signing key — delegating to the
cosmos keyring the daemon already opens (`blockchain/keys.go:112-131`) and
deriving at the chain path via `x/secrets/types`' `ChainCoinType` (phase 1) —
**never a second derivation implementation or a duplicated literal**, which is
the failure this whole plan exists to correct. A separate group rather than
new verbs under the existing `key` group: `key` is the share-key domain
(`backup`, `restore`), and §4's whole point is that the two key domains are
distinct — a bare `guardiand key create` would be ambiguous about which key it
creates.

The commands are thin because the keyring is already open in-process. The cost
worth acknowledging: key creation is a one-time bootstrap step that arguably
belongs to whatever provisions the host, and every command added is surface to
maintain. That is a fair objection to the scope, not to the direction — the
current state, where three call sites name a binary the image lacks, is the one
option that is clearly wrong. If execution finds the command set growing beyond
create, import and show-address, that is the objection reasserting itself and
worth pausing on. `CONTAINERS.md` gains the worked bootstrap example
regardless.

---

## What this plan does not solve

- **Mobile CI.** That the mobile unit and vector suites run nowhere
  (`.github/workflows/mobile-client.yml:157` runs only `npm run test:e2e`) is
  finding 6 of the mobile sweep, remediated in its phase 6. This plan's phase 3
  assertions land inside that job and are therefore **blocked on it** — without
  it they exist but never run.
- **The mobile-side `client_conventions.json` assertion** (mobile sweep
  finding 16) and **the `@noble` dependency declaration** (finding 21). Both
  owned by the mobile remediation plan. Phase 3 removes the reason for one of
  the `@noble` imports; it does not take over the declaration work.
- **At-rest custody differences.** The guardian's argon2id + ChaCha20-Poly1305
  envelope and the mobile client's delegation to Keychain/Keystore are answers
  to genuinely different threat models, both documented as such. Not
  duplication, not in scope.
- **Key rotation asymmetry.** Guardian epochs are a protocol feature with
  on-chain bindings; mobile's multiple named identities are a user feature.
  Different concerns that both involve more than one key.
- **The guardian's `withdraw` verb.** Same family as §4, owned by the guardian
  dead-code plan.
- **Whether 9733 is registered in SLIP-44.** Not verified here; the plan treats
  the chain's declaration plus spec.md as the authority regardless.

---

## Open questions

None outstanding. All seven were ruled on 1 August 2026 and are folded into
the design above:

1. **The SDK adopts 9733**, not the chain adopting 118 (phase 1). With nothing
   deployed both were equally cheap, so it was decided on merit: spec.md
   documents 9733, 118 is Cosmos Hub's and sharing it would make a multi-chain
   wallet derive the same account for both, and a distinct coin type is what
   external wallet integrations key on later.
2. **The wallet HD path is pinned in both `spec.md` and
   `CLIENT_CONVENTIONS.md`** (phase 1), spec.md as authority and the convention
   citing it.
3. **`guardiand` grows wallet-key commands** rather than the image shipping
   `timeflared` (phase 5).
4. **Courier keys stay path-free**, with the reasoning recorded at the call
   sites so nobody aligns them to the HD path and breaks funded kits (phase 1).
5. **`ChainCoinType` moves to `x/secrets/types`** (phases 1 and 5). The
   guardian cannot import `app/`, and a duplicated literal or a config value
   would reintroduce the drift this plan removes — one Go declaration for the
   chain and guardian, with the SDK's pinned copy held by the vector corpus.
6. **The wallet-key commands form a separate `guardiand wallet` group**
   (phase 5), not additions to the share-key `key` group — one verb per key
   domain.
7. **The passphrase file becomes raw-only, and phase 4 delivers the finding
   30 fix** (phase 4). The guardian sweep is an inventory with no executing
   plan, so the consolidation ships the corrected reader — verbatim file
   content, whitespace-trimmed, never decoded — and converts the writers in
   the same commit.

One decision was taken inside the design rather than raised as a question, and
is noted here so it is visible rather than buried: the negative assertion in
the phase 1 vector corpus deliberately records the *wrong* (118-derived)
address, so a regression to the cosmjs default fails with a recognisable
message instead of an opaque mismatch.

---

## Related plans

- [guardian/PENDING_GUARDIAN_PRE_TESTNET_SWEEP.md](guardian/PENDING_GUARDIAN_PRE_TESTNET_SWEEP.md)
  — finding 30 (the base64-guessing passphrase reader) is *delivered by*
  phase 4's consolidation (raw-only ruling, 1 August 2026); the sweep
  inventory should be annotated accordingly when it next updates.
- [guardian/PRIORITY_GUARDIAN_CUSTODY_HARDENING_PLAN.md](guardian/PRIORITY_GUARDIAN_CUSTODY_HARDENING_PLAN.md)
  — owns the guardian's share-key custody; this plan deliberately does not
  touch it beyond the dead `crypto/` API beside it.
- [guardian/PENDING_GUARDIAN_DEAD_CODE_SWEEP_PLAN.md](guardian/PENDING_GUARDIAN_DEAD_CODE_SWEEP_PLAN.md)
  — the `withdraw` verb, the same key-domain-parity family as §4.
- [DONE_MOBILE_PRE_TESTNET_SWEEP.md](DONE_MOBILE_PRE_TESTNET_SWEEP.md)
  findings 6, 16 and 21 — the CI job, the mobile corpus assertion and the
  dependency declaration. Phase 3 is blocked on finding 6's job existing.
- [DONE_MOBILE_SWEEP_REMEDIATION_PLAN.md](DONE_MOBILE_SWEEP_REMEDIATION_PLAN.md)
  — states that the `TODO(crypto-surface)` fix remains outstanding; phase 3 is
  that fix.
