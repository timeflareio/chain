# Secret Model Restructuring — Storage Layout & Performance Plan

*Restructure the monolithic `Secret` record into a slim metadata record plus keyed
side-stores (share data, assignment status, revealed shares), with denormalised
counters, so that the common on-chain operations stop paying to rewrite ~134KB of
state they did not touch.*

> **Status: IMPLEMENTED (July 2026).** S1–S6 and S8 landed on the
> `secret-model-restructure` branch
> ([PR #36](https://github.com/leedavis81/timeflare/pull/36)): the storage split,
> counters, creator index, single-write transitions, paginated/granular queries and
> the assembled `SecretView`, with spec.md/operations.md synced and the full e2e
> suites passing (lifecycle + 26/26 scenario assertions). S7 (retention) was spun
> out to [DONE_TERMINAL_SECRET_RETENTION_PLAN.md](DONE_TERMINAL_SECRET_RETENTION_PLAN.md).
> This document remains as the design rationale and decision log.

## Contents

1. [Why: the cost model](#1-why-the-cost-model)
2. [Current structure and its size](#2-current-structure-and-its-size)
3. [What each operation costs today](#3-what-each-operation-costs-today)
4. [Proposed structure](#4-proposed-structure)
5. [Suggestions](#5-suggestions)
6. [Per-operation impact matrix](#6-per-operation-impact-matrix)
7. [Regression guards — where costs could increase](#7-regression-guards--where-costs-could-increase)
8. [Migration and sequencing](#8-migration-and-sequencing)
9. [Cross-component impact](#9-cross-component-impact)
10. [Open questions](#10-open-questions)

---

## 1. Why: the cost model

Two costs matter, and they fall on different parties:

1. **Gas paid by the transaction signer.** With the Cosmos SDK default KV gas config,
   every store read charges `1,000 + 3 × bytes` and every write charges
   `2,000 + 30 × bytes` (key + value bytes). Because Collections serialises the whole
   value on every `Set`, a message that flips one status byte inside a 134KB `Secret`
   charges its signer for reading **and rewriting all 134KB** — roughly **4.4M gas**.
   At the 0.1 uveil minimum gas price that is ~0.44 VEIL per transaction, paid by
   guardians and reporters, the parties whose participation the protocol most needs
   to keep cheap.

2. **State written by every node.** Each `Set` creates a new versioned IAVL node
   holding the full value. A max-size secret's lifecycle rewrites the full blob
   roughly 70 times (32 confirmations, up to 32 reveals, plus double-writes on each
   state transition — see S4), i.e. **~9MB of IAVL writes for a few hundred KB of
   actual data change**. This is disk growth, pruning load, and state-sync weight on
   every node, for every secret.

The fix is the one the data shapes suggest: the large fields are **written once and
read individually** (per guardian), while the fields that change often are **tiny**.
They belong in different stores. Making the model more verbose — extra maps and
denormalised counters — is what makes the hot operations O(what-they-touch) instead
of O(secret-size).

CPU is explicitly *not* the motivation: everything is capped at
`MaxTotalShares = 32`, so the in-memory scans are trivial. Every win and every
regression in this plan is serialisation and storage gas.

---

## 2. Current structure and its size

`Secret` (proto/timeflare/secrets/v1/secret.proto) is one record holding:

| Field group | Size (worst case) | Mutability | Read pattern |
|---|---|---|---|
| Identity, timing, economics, commitment, recipient key + metadata | ~0.7KB | Written at request/distribute, then static | Every operation |
| `state` | ~20B | Changes 3–5 times over the lifecycle | Every operation |
| `guardian_assignments` (≤32 × {address, encrypted share ≤4KB, HMAC, status, height}) | **~134KB** | Share data written once at distribute; status flipped once per guardian | One guardian's entry at a time (confirm, reveal, slash) |
| `active_assignments` (≤32 addresses) | ~1.4KB | Written once at activation | Settlement, cancellation |
| `revealed_shares` (≤32 × {address, decrypted share, height}) | tens of KB | One append per reveal | Threshold check (count), settlement (addresses), recipient reconstruction (data) |
| `guardian_selection` (algorithm + client entropy ≤256B) | ~0.3KB | Written at request | **Never read again** |

The dominant weight — `guardian_assignments` — is dead weight for every operation
except the single-guardian lookup each message actually performs. Terminal secrets
(REVEALED / FAILED / CANCELLED) retain all of it forever, including the encrypted
shares of guardians who **rejected** their assignment, kept "for audit"
(msg_server_confirm_shares.go).

Storage today is a single map: `Secrets: collections.Map[string, types.Secret]`,
plus the (already well-designed) due-height queues and the `EarlyRevealSlash` map.

---

## 3. What each operation costs today

Gas figures are worst-case estimates (32 assignments × 4KB shares ≈ 134KB blob,
default KV gas config), stated to justify ordering — not as precise predictions.

| Operation | Signer (payer) | Storage work today | Approx. gas (storage only) |
|---|---|---|---|
| `MsgRequestGuardians` | Creator | Write blob (no shares yet, ~3KB) + queue entries | ~100k |
| `MsgDistributeShares` | Creator | Write full blob, then `TransitionSecretState` reads and writes it **again** (S4) | ~8.5M (+ ~1.3M tx-bytes gas, unavoidable) |
| `MsgConfirmShares` (×≤32) | **Guardian** | Read full blob, flip one status, write full blob; the activating confirm adds a second read+write via `TransitionSecretState` | **~4.4M each; ~8.9M for the activating one** |
| `MsgRevealShare` (×≤32) | **Guardian** | Read full blob, append one share, write full (growing) blob | **~4.5M+ each** |
| `MsgSlashGuardian` (early-reveal report) | **Reporter** | Reads blob twice (handler + `GetGuardianAssignments`); `AutoRevealShare` reads and writes the full blob again | **~5M+** |
| `MsgCancelSecret` | Creator | Read blob, release bonds (in-memory scan), `TransitionSecretState` re-reads and rewrites blob | ~4.9M |
| EndBlock commit expiry / settlement | Nobody (ungassed) but every node | Read full blob per due secret; `TransitionSecretState` re-reads and rewrites it (twice on the PENDING→RECONSTRUCTABLE→REVEALED path) | node I/O only |
| `Secret` / `Secrets` / `SecretsByCreator` / `PendingSecrets` queries | Query node | Single get / **unpaginated full-store walk** | node only; unbounded memory as state grows |

Two structural observations the suggestions build on:

- Every mutating path already ends in `TransitionSecretState`, which **re-fetches
  the secret and rewrites the whole value** even when the caller has just written it
  (secret_state_machine.go:251-257). Fixing that alone halves several rows above.
- Settlement and cancellation never need the encrypted shares at all — they operate
  on addresses (`active_assignments`), the revealed set, and `EarlyRevealSlash`.

---

## 4. Proposed structure

One slim record plus three keyed side-stores, all under the existing module store.
`Pair[secretID, guardianAddress]` keys give free per-secret prefix iteration.

```go
// Slim metadata — the only record most operations write.
Secrets            collections.Map[string, types.Secret]

// COLD, immutable: written once at DistributeShares, read one-at-a-time,
// deleted at terminal state (S7). The 4KB payloads live here and only here.
SecretShares       collections.Map[collections.Pair[string, string], types.SecretShareData]
                   // { encrypted_share bytes, share_hmac bytes }

// HOT, tiny (~150B incl. key): one per selected guardian, created at
// DistributeShares, mutated once when the guardian responds.
SecretAssignments  collections.Map[collections.Pair[string, string], types.AssignmentRecord]
                   // { status AssignmentStatus, responded_at_block int64 }

// One per reveal, written once. Retained for the recipient (see Open Questions).
SecretReveals      collections.Map[collections.Pair[string, string], types.RevealedShare]

// Secondary index for creator queries (S5).
SecretsByCreator   collections.KeySet[collections.Pair[string, string]]
```

The slim `Secret` proto keeps every scalar field it has today and changes as follows:

- **Removed:** `guardian_assignments`, `revealed_shares`, `active_assignments`
  (derivable from `SecretAssignments` where status = ACCEPTED — the accepted set and
  the active set are identical because confirmations are hard-capped at
  `requested_shares`), and `guardian_selection` (S8).
- **Added (denormalised, S3):**
  - `repeated string selected_guardians` — the Phase-1 selection (~1.4KB, written
    once), so `DistributeShares` can validate share addressing without touching the
    side-stores.
  - `int64 accepted_count` — so `ConfirmShares` slot-cap and activation checks are
    O(1).
  - `int64 revealed_count` — so the reveal-threshold check is O(1).

Worst-case slim record: ~2.2KB. A write costs ~68k gas instead of ~4M.

---

## 5. Suggestions

Each suggestion states what changes, why it is justified, its impact per affected
operation, and any cost it introduces. S1–S3 are one coherent change (the split);
they are separated so each piece carries its own justification, but they should ship
as a single migration — S3 in particular exists to prevent S1/S2 from regressing
anything (see §7).

### S1 — Move share data and assignment status out of `Secret` (proto + state change)

**What.** Replace `Secret.guardian_assignments` with the `SecretShares` (cold,
immutable) and `SecretAssignments` (hot, tiny) maps above.

**Justification.** Share data is written exactly once and only ever read for one
guardian at a time (confirm-time existence check, reveal-time HMAC, slash-time
HMAC). Status is the only mutable part and is ~10 bytes. Today both live inside the
value that every message must round-trip. Splitting them is the single biggest win
in the plan: it converts every `ConfirmShares` from ~4.4M gas to ~35k, every
`RevealShare` HMAC lookup from a 134KB read to a 4.2KB read, and every
`MsgSlashGuardian` from ~5M gas to well under 100k. I verified every consumer of
`GuardianAssignments` (confirm, distribute, reveal, slash, `ReleaseAllAcceptedBonds`
in guardian_float.go, reveal_window.go) — each one either looks up a single guardian
or iterates one secret's set; both patterns map directly onto point-gets and prefix
walks.

**Why two records, not one.** A single combined assignment record (share + status)
would still force `ConfirmShares` to rewrite ~4.2KB to flip a status byte (~130k
gas). Splitting hot from cold makes the confirm write ~6k gas and — just as
important — makes the accepted-set prefix walk used by cancellation and settlement
cheap (32 × ~150B ≈ 42k gas) instead of dragging 134KB of share bytes through the
gas meter (~435k, which would be a **regression** versus today's single 403k blob
read; see §7).

**Impact per operation.**
- `ConfirmShares` (guardian): read slim + `Has` on `SecretShares` (existence proves a
  share was distributed) + read/write own `AssignmentRecord` + write slim (counter).
  ~4.4M → **~35k gas** (~125×). The rejection path no longer retains the rejecting
  guardian's encrypted share by accident of structure — see S7.
- `RevealShare` (guardian): read slim + own `AssignmentRecord` (accepted?) + own
  `SecretShares` entry (HMAC) + write one `SecretReveals` entry + slim counter.
  ~4.5M → **~150k gas** (dominated by the reveal payload itself, which is
  irreducible).
- `MsgSlashGuardian` (reporter): two point-reads replace two full-blob reads, and
  `AutoRevealShare` stops rewriting the blob. ~5M → **<100k gas**. Cheap reporting
  is a protocol-security property: the early-reveal deterrent assumes reporting is
  economically trivial.
- `DistributeShares` (creator): writes 32 cold records + 32 tiny status records
  instead of one 134KB blob twice (with S4). ~8.5M → **~4.5M** storage gas (~2×).
  The per-record flat costs add ~64k versus a hypothetical single write of the same
  bytes — negligible against the saved double-write, and the 30 gas/byte on 134KB of
  share data is inherent to putting the shares on chain at all, not to this layout.
- `RequestGuardians` (creator): writes the slim record with `selected_guardians`
  instead of a blob of empty assignments. Status records are **deliberately not
  created here** — they are meaningless before distribution — which keeps Phase 1
  cost flat (~100k) rather than adding 32 record-creation flat fees.
- EndBlock commit expiry: `ReleaseAllAcceptedBonds` walks 32 tiny status records
  instead of decoding the blob. Node I/O strictly down.

**Costs / risks.** Proto and state-layout change → migration required (§8); multiple
stores must be kept consistent (all mutations already sit inside single-message state
machines, and the no-stranded-bonds and conformance suites must be extended to assert
cross-store consistency); guardian automation and TypeScript SDK touch these shapes
(§9).

### S2 — Move revealed shares out of `Secret` (proto + state change)

**What.** Replace `Secret.revealed_shares` with the `SecretReveals` map.

**Justification.** Reveals are the protocol's hottest path (every guardian, every
secret, inside a bounded window — reveal traffic clusters). Today each reveal
rewrites the entire blob, and because the blob grows with each accepted reveal, the
*n*-th revealer pays more than the first — a perverse fee for being late but honest.
After the split a reveal writes only its own record. Consumers map cleanly:
`HasGuardianRevealed` becomes a `Has()` (1k gas, replacing a linear scan of an
already-deserialised blob); the threshold check uses `revealed_count` (S3);
settlement's partition and the recipient's reconstruction query become per-secret
prefix walks.

**Impact per operation.** Included in the `RevealShare` figure under S1. Settlement
reads ≤32 reveal records (only needed for the partition's address set — a keys-only
iteration suffices) instead of one giant blob. Recipient-facing queries unchanged in
shape (§9).

**Costs / risks.** Same migration/consistency envelope as S1. Reveal records are the
one side-store that must **outlive** settlement (the recipient reconstructs from
them) — retention is an open question (§10).

### S3 — Denormalised counters and selection list on the slim `Secret` (proto change)

**What.** Add `accepted_count`, `revealed_count`, and `selected_guardians` to
`Secret`; drop `active_assignments`.

**Justification.** This is what makes S1/S2 safe rather than merely different. The
slot-cap check in `ConfirmShares` (count accepted so far), the activation check
(accepted ≥ requested), and the threshold check in `hasEnoughShares` (revealed ≥
threshold) all currently scan repeated fields that arrive "free" inside the loaded
blob. Once the blob is split, recomputing those counts would need prefix walks on
every confirm/reveal — cheap walks (S1 sized the records to keep them cheap), but a
counter read is free and cannot drift into an O(n) surprise. The counters are
maintained at exactly two write sites each (accept; reveal), inside the same message
handlers that already write the slim record, so there is no new consistency surface
beyond what the invariant tests must cover anyway. `selected_guardians` preserves
`DistributeShares`' Phase-1 validation without side-store reads; dropping
`active_assignments` removes a duplicate of "status = ACCEPTED" that would otherwise
be a second source of truth to keep consistent.

**Impact per operation.** Confirm slot-cap, activation and threshold checks: O(1)
field reads. Cancellation/settlement derive the active set from the tiny status
records (~42k, EndBlock side ungassed).

**Costs / risks.** Denormalised values can lie if a future write site forgets them —
mitigate with an invariant (`accepted_count == |status=ACCEPTED|`,
`revealed_count == |SecretReveals prefix|`) wired into the existing conformance/
invariant test suites.

### S4 — Stop the double read/write in `TransitionSecretState` (keeper only, no proto)

**What.** `TransitionSecretState` (secret_state_machine.go) re-fetches the secret
and rewrites the whole value after the caller has, in three of its call sites, just
written the same record. Change it to operate on the caller's in-memory secret (or
return the new state for the caller to persist in its single final `SetSecret`).

**Justification.** Pure waste on today's layout: it is the difference between ~4.4M
and ~8.9M gas on the activating confirm, and between one and two full-blob writes at
distribute, cancel, and each settlement transition. It remains a (much smaller) win
after the split. It also removes a real state-consistency trap the code already had
to grow a warning comment about (endblock_logic.go:100-105: writing a stale local
copy after the transition silently reverts the state).

**Impact per operation.** Halves distribute/activating-confirm/cancel storage gas
today; removes one slim-record write from every transition after the split.

**Costs / risks.** None material — signature change on an internal helper; the FSM
validation logic is untouched.

### S5 — Creator secondary index (keeper only, no proto)

**What.** Maintain `SecretsByCreator: KeySet[Pair[creator, secretID]]` on
create/delete; `GetSecretsByUser` (keeper.go:138, already carrying a TODO for this)
becomes a prefix walk over the creator's own secrets.

**Justification.** Today it walks the entire secret store — with the current model
it also *deserialises every 134KB blob on the node* to compare one string field.
Cost grows with global state, not with the caller's data.

**Impact.** Query-node only; no consensus-path or gas change. After S1/S2 the walk
would at least deserialise slim records, but it would still be O(all secrets) —
the index is justified either way.

**Costs / risks.** One extra ~90B KeySet write at `RequestGuardians` (~5k gas, on the
creator) and index maintenance at deletion/pruning.

### S6 — Paginate and filter the list queries (query proto additions only)

**What.** `Secrets` and `PendingSecrets` (query.go:47, :89) return the full store
unpaginated; `PendingSecrets` does not even filter by state yet. Add standard
`cosmos.base.query.v1beta1.PageRequest/PageResponse` (additive proto fields, wire-
compatible) and use `query.CollectionPaginate` as the `Guardians` query already
does; make `PendingSecrets` filter on state.

**Justification.** An unpaginated walk that materialises every secret is an
unbounded-memory endpoint on public query nodes — with the current model that is
the entire chain's share data in one gRPC response. This is the only suggestion
motivated by node robustness rather than gas.

**Impact.** Query nodes only. SDK callers gain optional pagination parameters.

**Costs / risks.** Additive proto change to `query.proto` only (no stored-state
impact); SDK convenience wrappers should be updated to iterate pages.

### S7 — Prune cold data at terminal states (behavioural change, spec update)

> **Now planned in detail separately:**
> [DONE_TERMINAL_SECRET_RETENTION_PLAN.md](DONE_TERMINAL_SECRET_RETENTION_PLAN.md) — staged
> pruning, the 3-month retention clock, the prune queue, and the hash-tombstone
> design. The section below is retained as the summary and original rationale.

**What.** When a secret reaches REVEALED, FAILED, or CANCELLED (all three already
funnel through a small number of transition sites), delete its `SecretShares`
entries and `SecretAssignments` records, and its `EarlyRevealSlash` marks (which are
currently never cleaned up and accumulate forever). Reveal records and the slim
record are retained (see §10). Additionally, delete a rejecting guardian's share
record at rejection time rather than "keeping it for audit" — an encrypted share
held by someone who declined custody has no audit value that the emitted rejection
event does not already provide.

**Justification.** Once the window closes, the encrypted shares are cryptographically
inert — settlement provably never reads them (endblock_logic.go operates on
addresses, reveal records and `EarlyRevealSlash` only). Retaining them makes every
completed max-size secret a permanent ~134KB of live state on every node, forever.
Deletion caps live state at (active secrets × size) instead of (all secrets ever ×
size).

**Impact per operation.** EndBlock settlement/expiry gains ≤64 ungassed deletes plus
the same at cancellation (gassed, ~1k each — noise against cancellation's transfers).
No user-facing operation reads what is deleted. Historical state remains available
to archive nodes via IAVL versions; pruned nodes reclaim on their pruning schedule.

**Costs / risks.** Spec must state the retention rule explicitly (spec.md currently
implies assignments persist). Queries for terminal secrets return the slim record and
reveals but no assignment detail — the SDK and guardian automation must not assume
assignment presence on terminal secrets (§9).

**Decision (July 2026): terminal secrets are retained for 3 months** (~1,296,000
blocks at 6s), then pruned entirely — slim record and reveal records included. The
existing due-height queue pattern extends naturally: enqueue a prune entry at
`terminal_height + retention` when a secret reaches a terminal state, drained by
EndBlock exactly as the commit and settlement queues are.

**Provable reference after pruning.** Options for keeping a verifiable trace of a
pruned secret, in increasing order of machinery:

1. **Hash tombstone (recommended).** At prune time, write
   `SecretTombstones: Map[secretID, TombstoneRecord]` — SHA256 of the canonical
   final record (slim record + reveal set, deterministically encoded), plus final
   state and pruned-at height (~50B each). Anyone holding an archived copy (from an
   indexer, an event log, or their own node) can prove it is the authentic final
   record; the chain itself stays small (~50B per secret ever, versus ~2KB+ —
   a 40× reduction on top of S7's 134KB one). Trivial to implement and to query.
2. **Epoch Merkle roots.** Batch the tombstone digests of everything pruned in an
   epoch (e.g. daily) into one Merkle root; delete individual tombstones. State
   becomes O(epochs) instead of O(secrets), but proofs now need a Merkle path that
   some off-chain archive must serve, and point lookups ("was secret X real, what
   was its final state?") stop being an on-chain query. Only worth it if tombstone
   count ever matters — at ~50B each, one million pruned secrets is ~50MB, so this
   is a v2 consolidation path, not a day-one need.
3. **No new state — rely on consensus history.** Every historical app hash already
   commits to every version of every record; an archive node plus a light-client
   proof at a historical height proves anything. Zero cost, but the proving UX
   (archive-node dependence, height bookkeeping) is poor and prunes with the node's
   own pruning policy.

Recommendation: (1) now, with the canonical-encoding rule written into spec.md
(proofs are only as good as the encoding is deterministic); revisit (2) only if
tombstone growth ever registers. Emitting the full final record in the prune event
gives indexers a clean archival hook at zero state cost.

### S8 — Stop storing `guardian_selection` on the secret (proto change, opportunistic)

**What.** Drop `GuardianSelectionConfig` (algorithm string + up to 256B client
entropy) from the stored record. It is consumed during `RequestGuardians` in the
same transaction that stores it and — verified by grep across the keeper — never
read again.

**How the entropy is actually used (verified).** It is a selection *input*, not a
proof artefact: `generateProtocolSeed` (guardian_selection.go) computes
`SHA256(chainID ‖ blockHeight ‖ lastBlockHash ‖ clientEntropy)` and that seed drives
a deterministic Fisher–Yates shuffle of the eligible guardian set. Its purpose is to
stop a colluding proposer from fully controlling selection (the creator contributes
unpredictability the proposer cannot choose). Verifiability does not depend on the
*stored* copy: the entropy lives permanently in the `MsgRequestGuardians` tx, and the
other seed inputs are consensus data, so anyone can recompute the seed and replay the
shuffle. The genuinely hard part of an after-the-fact selection proof is
reconstructing the *candidate set* (every active, accepting, window-covering,
bond-affording guardian at that height), which requires historical state at that
height regardless of whether entropy is stored — storing it buys nothing.

**Justification.** Write-only state is pure cost. Small (~0.3KB), but it rides along
in every slim-record write for the secret's whole life. To make off-chain
verification convenient without any state, additionally emit the computed seed (or
the entropy) as an attribute on the existing reservation event.

**Impact / costs.** None at runtime; it is a proto field removal, so it shares the
migration and cross-component sweep with S1–S3 (per the ShareIndex lesson in
CLAUDE.md, grep every component). Only worth doing because that migration is
happening anyway — do not ship it alone.

### S9 — Split the key, not the payload (future protocol-level opportunity)

> **Now planned in detail separately:**
> [DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](DONE_KEY_SHARE_ARCHITECTURE_PLAN.md) — the two-layer
> design (per-secret X25519 keypair as the time-lock, recipient layer unchanged),
> chain-side changes, per-operation impact, surface area, and security analysis.
> The section below is retained as the summary.

**What.** Today the creator encrypts the payload to the recipient, SSS-splits the
*ciphertext*, and encrypts each share per guardian. Because a GF(256) SSS share is
**byte-for-byte the same size as its input** (verified in `rust/src/sss.rs`:
`share.data.len() == secret.len()`), every guardian's share carries the full payload
size, and on-chain cost is `O(secret_size × num_guardians)`. The standard
alternative: encrypt the payload once under a random 32-byte symmetric key, store
that single ciphertext on chain, and SSS-split **the key**. Each guardian share
becomes 33 bytes (key share + SSS id) + 60B encryption overhead ≈ **~100 bytes,
independent of secret size**; cost becomes `O(secret_size + num_guardians)`.

**Impact.** A max-size secret's share data drops from ~134KB to one ~4KB ciphertext
plus ~3KB of key shares (~20×), `MsgDistributeShares` tx bytes drop the same way,
and — decoupled from guardian count — the `MaxEncryptedShareSize` cap could later be
raised for the payload alone without multiplying it by 32 across every store and
block. Reveal transactions shrink to ~100B payloads. Every mechanism this plan
relies on survives intact: HMACs bind to the key share (early-reveal detection
unchanged), the commitment remains SHA256 of the recipient-encrypted payload
(reconstruction verification unchanged), threshold semantics unchanged.

**Why it is not in this plan's scope.** It changes the client-side cryptographic
protocol (Rust crypto, WASM, TypeScript SDK, guardian daemon all touch it), adds a
stored-payload availability rule to the spec, and deserves its own security review
(e.g. the payload ciphertext becomes public on day one — its confidentiality rests
entirely on the key shares, which is also true today of the share set, but the spec
should say so explicitly). The storage layout in §4 is deliberately compatible with
it: S9 would shrink what `SecretShares` holds without changing any map or any
message flow, so S1–S8 are not throwaway work. Recommend a separate plan document
once this one lands.

### Noted but not recommended now

- **`state` as enum instead of string** and **`EarlyRevealSlash` keyed by
  `Pair` instead of `fmt.Sprintf`**: real but tiny wins; fold the latter in only if
  S7 touches that map anyway. Not worth independent churn.
- **Encrypted shares off-chain (events or external distribution)**: would eliminate
  the 134KB entirely, but changes the protocol's availability guarantee (a guardian
  must always be able to fetch its share from state) and the trust model. Out of
  scope; recorded so the option is not re-litigated from scratch later.
- **Guardian selection's full-guardian-set walk** (`GetActiveGuardians` at
  `RequestGuardians` time): a Guardian-model concern, not a Secret-model one —
  flagged here as the natural follow-up plan if guardian counts grow.

---

## 6. Per-operation impact matrix

Worst-case storage gas, before → after (S1–S4 combined; S7 affects state size, not
gas). "Before" figures include today's `TransitionSecretState` double-write where it
applies.

| Operation | Payer | Before | After | Change |
|---|---|---|---|---|
| `RequestGuardians` | Creator | ~100k | ~105k (slim + index) | ~flat |
| `DistributeShares` | Creator | ~8.5M | ~4.5M | ~2× cheaper |
| `ConfirmShares` (non-activating) | Guardian | ~4.4M | ~35k | ~125× cheaper |
| `ConfirmShares` (activating) | Guardian | ~8.9M | ~40k | ~220× cheaper |
| `RevealShare` | Guardian | ~4.5M+ (grows per reveal) | ~150k (flat) | ~30× cheaper |
| `MsgSlashGuardian` | Reporter | ~5M+ | <100k | ~50× cheaper |
| `CancelSecret` | Creator | ~4.9M | ~120k (slim + status walk + payouts) | ~40× cheaper |
| EndBlock (per due secret) | all nodes | 2–3 full-blob round-trips | slim round-trip + tiny walks + deletes | strictly less I/O |
| Lifecycle IAVL writes (max-size secret) | all nodes | ~9MB | ~350KB | ~25× less |
| Live state per completed secret | all nodes | ~134KB forever | slim + reveals only (S7) | bounded |

No operation for any party gets more than marginally more expensive, and the two
marginal cases (`RequestGuardians` +~5k for the creator index; `CancelSecret`'s
prefix-walk flat costs, absorbed many times over by shedding the blob) are on the
creator — the party already paying the reward pool — not on guardians or reporters.

## 7. Regression guards — where costs could increase

These are the traps this plan explicitly designs around; any implementation must
re-check them:

1. **Prefix walks over fat records.** If assignment status lived with the share data
   (single combined record), the accepted-set walks in cancellation, commit expiry
   and settlement would read ~134KB in 32 pieces (~435k gas / equivalent node I/O) —
   *worse* than today's single 403k blob read. The hot/cold split in S1 is what keeps
   those walks at ~42k. Do not "simplify" the two maps into one.
2. **Recomputing counts.** Without S3, every confirm and reveal would prefix-walk to
   count statuses/reveals. Cheap, but per-message flat read costs (32 × 1k) would eat
   a chunk of the win and scale with `MaxTotalShares` if that cap ever rises. Ship
   S3 with S1/S2, not after.
3. **Per-record flat costs at distribute.** 64 record creations cost ~192k in flat
   fees that a single blob write does not pay. This is why status records are created
   at distribute (when they become meaningful) and not at request, and why the plan
   accepts the distribute-time flat cost against the ~4M saved there.
4. **Multi-store consistency.** The blob was atomic by construction. After the split,
   consistency is per-message-handler discipline plus invariants. Extend
   `no_stranded_bonds_test.go` and the conformance suite with cross-store invariants
   before the migration merges, not after.
5. **Query assembly cost.** The `Secret` query must assemble the full view from
   1 + ≤64 + ≤32 records. Query-node only, and bounded — acceptable — but resist any
   temptation to add an assembled-view cache into consensus state.

## 8. Migration and sequencing

**Phase 0 — no proto changes (can land now, independently):**
1. S4 (`TransitionSecretState` signature) — immediate ~2× on several paths.
2. S5 (creator index) + S6 (query pagination/filtering).

**Phase 1 — the split (one upgrade, one migration):** S1 + S2 + S3 + S8 together,
via `make upgrade-scaffold NAME=<vN>`:
1. Spec first: update `docs/spec.md` (storage model, retention, and the counters'
   definitions) and get it approved before code.
2. New proto messages (`SecretShareData`, `AssignmentRecord`) and slim `Secret`;
   `buf breaking` will flag the removals — expected and deliberate.
3. ~~Upgrade handler~~ **Decided (July 2026): no state migration.** The chain is
   pre-launch — no persistent network carries state — so this is a genesis-breaking
   change absorbed by `make dev-reset`. If a live network exists before this lands,
   the migration is: walk `Secrets`, fan each blob out into the side-stores, rewrite
   the slim record with counters derived from the data being migrated (O(secrets)
   one-off, rehearsed with `make devnet-upgrade-test`).
4. Extend invariant/conformance tests with the cross-store checks (§7.4); full
   `make test` + `make e2e-scenarios` (behaviour, including exact on-chain amounts,
   must be identical — this plan changes layout, not economics).

**Phase 2 — retention (needs §10 answered):** S7, plus `EarlyRevealSlash` cleanup.

## 9. Cross-component impact

Per the ShareIndex removal lesson (CLAUDE.md): protocol-shape changes reach further
than `x/secrets/`.

- **Queries / TypeScript SDK.** The `QuerySecretResponse` wire shape can be kept
  fully compatible: the query handler assembles assignments and reveals from the
  side-stores into the existing response message. If we instead expose the new shape
  directly, the SDK, its tests, and `typescript-sdk/examples` all need the sweep.
  Recommendation: keep the assembled view; the SDK then only changes for S6
  pagination.
- **Guardian automation (`guardian/`).** Historically the component most easily
  missed. It consumes secrets via queries and events; with the assembled view its
  changes should be limited to any assumption that terminal secrets retain
  assignment data (S7) — grep `guardian/` for `GuardianAssignments`,
  `RevealedShares`, `ActiveAssignments` before and after.
- **Events.** Unchanged — all existing events carry scalars already present on the
  slim record.
- **spec.md.** "Secret lifecycle", storage/architecture sections, and (for S7) an
  explicit retention statement. Same-session rule applies.

## 10. Open questions

1. ~~Reveal-record and terminal-record retention~~ **Decided: 3 months** for
   everything (slim record, reveal records), then pruned behind a hash tombstone —
   see S7. Remaining detail for spec: whether the recipient-facing SDK should warn
   as the prune height approaches.
2. **Tombstone form** — S7 recommends per-secret hash tombstones now, epoch Merkle
   consolidation only if growth ever warrants it. Confirm before Phase 2.
3. ~~Assembled query view vs. new query shape~~ **Decided: both.** The existing
   `Secret` query keeps today's wire shape — the handler assembles slim record +
   side-stores into a query-only view message that preserves the old field numbers,
   so the TypeScript SDK and guardian daemon wiring is retained as-is. Alongside
   it, new granular `SecretMeta` / `SecretAssignments` / `SecretReveals` endpoints
   let light clients (the S5 mobile use case) fetch ~2KB of metadata instead of the
   assembled view.
4. ~~`MaxEncryptedShareSize` headroom~~ **Investigated — the cap is doing real
   work; do not raise it under the current scheme.** Findings from `rust/src/`:
   a GF(256) SSS share is exactly the size of its input (`sss.rs`), and share
   encryption (`crypto.rs`: X25519 ephemeral key 32B + nonce 12B +
   ChaCha20-Poly1305 tag 16B) adds exactly 60B per layer. So an encrypted guardian
   share = recipient-encrypted payload + 60B, and the 4KB cap bounds the original
   secret to ~3,976B (two 60B layers). The worst cases in §3 are therefore real,
   not slack: creators of ~4KB secrets genuinely put ~134KB per secret on chain,
   because *every guardian share carries the full payload*. The cap's remembered
   rationale holds up: `MsgDistributeShares` carries all shares in one tx
   (32 × 4KB ≈ 131KB), sized against mempool/block tx limits (CometBFT default
   `max_tx_bytes` 1MB) — raising the cap multiplies by up to 32 in the tx, the
   block, and the store. The Rust library's own 1MB/50MB limits are client-side
   memory guards, not chain limits, and are irrelevant on-chain. The real headroom
   is architectural, not parametric: see S9.
