# Terminal Secret Retention & Tombstones — Plan

*What happens to a secret's state after it reaches a terminal state (REVEALED,
FAILED, CANCELLED): staged pruning on a 6-month retention clock, with a compact
provable reference — a hash tombstone — left behind so a pruned secret's final
record remains verifiable forever.*

> **Status: DONE (July 2026) — shipped in PR #66 (`terminal-secret-retention`)**:
> staged pruning (Stage 1 at terminal transition, Stage 2 prune queue capped per
> block), the seven-field `SecretTombstone` + canonical `TerminalSecretRecord`
> digest, the archival event, `Query/SecretTombstone`, genesis export/rebuild,
> conformance/golden tests, and the same-session spec.md/PROTOCOL.md updates
> (plus a follow-up gating `TIMEFLARE_RETENTION_BLOCKS` behind
> `--unsafe-dev-overrides` for e2e). §9 is the decision log, §10 was the build
> checklist. Extracted from
> [DONE_SECRET_MODEL_RESTRUCTURING_PLAN.md](DONE_SECRET_MODEL_RESTRUCTURING_PLAN.md) (S7) so
> the retention design can iterate independently. Decisions so far: **6-month
> retention** for terminal secrets (ruled July 2026, revised from the original
> 3-month agreement); the **hash tombstone** is the decided provable
> reference, its **seven-field model settled** (July 2026 — §4, ~142B per
> tombstone); **no post-prune discovery mechanism** and **no creator listing
> index** (§9.7, §9.2); alternatives to the mechanism remain recorded in §6. Depends on the storage split (S1–S3) landing first — the
> staged pruning below is defined in terms of the split stores.
>
> **Sequencing (July 2026): the key-share architecture
> ([DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](DONE_KEY_SHARE_ARCHITECTURE_PLAN.md), S9) executes
> before this plan.** This plan therefore treats the S9 world as its baseline:
> secrets carry a stored payload ciphertext `C` (`SecretPayloads` cold store) and
> `pk_s` on the slim record; guardian shares and reveal records are ~34B key
> shares, not KB-scale payload shares. The S9-driven requirements are folded into
> §2, §4, §5 and §7 below.
>
> **Viability re-check (July 2026, against the current chain): the direction
> holds, and the plan is readier than the banner above suggests.** Every stated
> dependency has landed: the storage split and the key-share architecture are in
> production shape, and `terminal_at` already exists on the slim record (proto
> field 24, stamped by the FSM on terminal entry) — the proto addition §2
> anticipated is done. Two rulings since the last revision also bind here:
> **no governance** (Position A,
> [DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md),
> whose work item 3 names this plan) — `RetentionBlocks` is a hardcoded
> constant, resolving §9.1; and the recipient-privacy work landed, deleting the
> `recipient_metadata` canonical-encoding rule from §4. The re-check also found
> one store the plan's delete lists missed: the **`HintsByCreation` discovery
> feed** — resolved in §9.7: it joins the Stage 2 deletes.

## Contents

1. [Goal and non-goals](#1-goal-and-non-goals)
2. [Retention policy](#2-retention-policy)
3. [Mechanics: the prune queue](#3-mechanics-the-prune-queue)
4. [The hash tombstone](#4-the-hash-tombstone)
5. [Query and client behaviour after pruning](#5-query-and-client-behaviour-after-pruning)
6. [Alternatives considered](#6-alternatives-considered)
7. [Impact on operations](#7-impact-on-operations)
8. [Migration](#8-migration)
9. [Decision log](#9-decision-log-resolved-july-2026)
10. [Implementation checklist](#10-implementation-checklist)

---

## 1. Goal and non-goals

**Goal.** Live state should scale with *active* secrets, not with every secret ever
created — while preserving, for every pruned secret, an on-chain anchor that proves
what its final record was.

**Non-goals.** This plan does not archive the pruned data itself (that is an
indexer/archive-node concern — the chain provides the anchor and an event-based
archival hook); it does not change any economics, settlement, or lifecycle
behaviour before the terminal state.

## 2. Retention policy

Retention runs in two stages, because the data has two different lifetimes:

| Stage | When | What is deleted | Why it is safe |
|---|---|---|---|
| **Stage 1 — at terminal transition** | The block the secret reaches REVEALED / FAILED / CANCELLED | `SecretShares` (encrypted shares, incl. those of rejecting guardians), `SecretAssignments` (status records), `EarlyRevealSlash` marks | Settlement provably reads none of these after the terminal transition (endblock_logic.go operates on addresses, reveal records, and the slash marks — all consumed *during* settlement, not after) |
| **Stage 2 — terminal + retention** | `terminal_at + RetentionBlocks` | Slim `Secret` record, `SecretReveals` entries, `SecretPayloads` entry (the S9 payload ciphertext `C`), `SecretsByCreator` index entry, `HintsByCreation` entry (resolved §9.7) — replaced by one tombstone | The recipient has had the full retention window to fetch the reveal records **and `C`** and reconstruct; the tombstone preserves verifiability |

**Stage-1/Stage-2 boundary for `C`**: the payload ciphertext must **not** join the
Stage 1 deletes — post-S9 reconstruction needs `C` as well as ≥t revealed key
shares, so `C` lives exactly as long as the reveal records and dies with them at
Stage 2.

**`RetentionBlocks` = 2,592,000** (~6 months at 6s blocks — ruled July 2026,
revised from the original 3-month agreement; the cost is linear and bounded:
in-window terminal state carries ~6 months of creation volume instead of 3,
and the longer window halves the late-scanner exposure accepted in §9.7). A
**hardcoded compile-time constant** in `x/secrets/types/constants.go`, like
every other protocol value — ruled July 2026 under Position A (no governance
parameters;
[DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md)
work item 3 names this plan explicitly). Retuning it is a coordinated software
upgrade; §9.1 records the in-flight queue behaviour that follows.

The keying field already exists: **`terminal_at` (int64)** landed with the
storage split (slim `Secret`, proto field 24) — the FSM stamps it in the same
write that records the terminal state. No proto change to the slim record is
needed; the prune queue keys on it directly.

## 3. Mechanics: the prune queue

A third due-height queue, identical in shape and semantics to the existing commit
and settlement queues (keeper.go):

```go
PruneQueue collections.KeySet[collections.Pair[int64, string]] // (due_height, secret_id)
```

- **Enqueue** at every terminal transition: `(terminal_at + RetentionBlocks, id)`.
  All terminal transitions already funnel through `TransitionSecretState` /
  the settlement paths, so there are few call sites.
- **Drain** in EndBlock with the same `<=`-drain self-healing pattern the other
  queues use. Per due entry: build the tombstone (§4), emit the archival event,
  delete the slim record, the reveal records (prefix delete), the payload
  ciphertext (`SecretPayloads`), the creator-index entry, the hint-feed entry
  (`HintsByCreation`, keyed `(created_at, id)` — both values on the record
  being deleted; resolved §9.7), and the queue entry.
- **Bound the work**: cap prunes per block (e.g. 50) and leave the remainder in the
  queue for the next block, so a burst of same-height expiries cannot stretch
  EndBlock. The `<=`-drain makes the carry-over automatic.

Cancellation of a queue entry is never needed: terminal states are terminal.

## 4. The hash tombstone

```go
SecretTombstones collections.Map[string, types.SecretTombstone]
```

### The decided model (owner ruling, July 2026)

A tombstone provides two distinct services, and the decision below follows
from keeping them separate:

1. **Provability** — the `record_digest` makes any archived copy of the full
   final record self-authenticating. Every field of that record is already
   provable this way, forever, at zero extra state: hold the archived record,
   hash it, match the digest.
2. **Queryability without an archive** — a field carried in the tombstone
   itself answers its question on-chain forever, with no archived copy needed.

The digest therefore proves everything; a field earns tombstone residence
only if its question must be answerable **without** an archived copy.
Ranking every candidate field that came up across this plan, the S9 work
and the privacy work by that archive-free utility settled the model at
**seven fields** (the excluded candidates are recorded below):

```protobuf
message SecretTombstone {
  // ── Verification anchor ──
  bytes  record_digest     = 1;  // 32B  SHA256 of the canonical final record
  string final_state       = 2;  // ~9B  revealed | failed | cancelled
  int64  terminal_at       = 3;  //  8B  height of the terminal transition;
                                 //      locates the settlement events in history
  int64  pruned_at         = 4;  //  8B  height Stage 2 executed; locates the
                                 //      archival event (the full canonical record)

  // ── Identity & parties ──
  string creator           = 5;  // ~45B attribution without an archive (§9.2 option c)
  int64  created_at        = 6;  //  8B  lifetime = terminal_at − created_at; locates
                                 //      the distribution tx (where C lives) in block history

  // ── Cryptographic anchor ──
  bytes  secret_commitment = 7;  // 32B  SHA256(C_r) — verify an archived or reconstructed
                                 //      payload directly, no record needed
}
```

**Cost**, proto-encoded (tag + length + payload; heights as varints at
realistic values):

| Field | Encoded size |
|---|---|
| `record_digest` | 34B |
| `final_state` | ≤ 11B |
| `terminal_at`, `pruned_at`, `created_at` | ~5B each |
| `secret_commitment` | 34B |
| `creator` | ~47B |
| **Tombstone value** | **~142B** |
| **Full store entry** (value + key: prefix ‖ 36-char id) | **~182B** |

At scale, per **million pruned secrets: ~182MB of permanent state** — against
2GB+ if terminal records were never pruned (~2KB+ each retained today): a
~11× compaction. The ~80MB/M carried above a bare digest-only anchor
(~100MB/M with keys) buys the two archive-free capabilities the utility
ranking valued most: direct `C_r` verification (`secret_commitment`) and
attribution (`creator`, taking §9.2 option c).

### Considered and excluded

Eleven candidate fields were ranked and excluded — ten because each is
covered by the `record_digest` plus an archived record (and usually by
events too), and `detection_hint` by the §9.7 ruling that post-prune
discovery belongs to archives. The exclusion principles applied: a field
earns residence only if its question must be answerable *without* an
archived copy; enabling a client behaviour beats saving an archive lookup;
the burden of proof is on inclusion (every field is forever-state × every
secret ever); and nothing derivable from a kept field ships twice.

| Excluded field(s) | Why out |
|---|---|
| `detection_hint` (41B) | Excluded by ruling (§9.7): the retention window is the availability contract — once it has passed, tombstones can't help a late recipient; only archives can. The id-keyed tombstone map also makes hint scanning a non-resumable full walk, so the field's utility was illusory anyway |
| `payload_digest` (`SHA256(C)`, 32B) | Already inside the digested canonical record (see canonical encoding below); block history authenticates a recovered `C` for free |
| `secret_public_key` (`pk_s`, 32B) | Post-prune creator-fault attribution ("does this claimed `sk_s` match?") is a near-never dispute; provable via the archived record |
| `guardian_set_digest` (32B) | Guardian service history lives in permanent transaction history and settlement events; reputation is indexer territory |
| `accepted_count`, `revealed_count`, `no_show_count`, `early_slashed_count` (~2B each) | Settlement trivia without `threshold`/`requested_shares` beside them; events carry the full per-guardian picture, and `accepted_count` is near-constant by construction |
| `reward_pool`, `bond_amount`, `bump` (~18B) | Economics audit belongs to events and the metrics dashboard; `bond_amount` is additionally derivable from `bump` plus the constants in force at the time |

### Canonical encoding — the part that must be specified precisely

A digest is only as good as the encoding is deterministic. The **canonical final
record** is defined as the deterministic proto marshal of:

```go
message TerminalSecretRecord {
  Secret                 secret         = 1;  // the slim record, exactly as last stored
  repeated RevealedShare reveals        = 2;  // sorted by guardian address, ascending
  bytes                  payload_digest = 3;  // SHA256(C) — the S9 payload ciphertext
}
```

`payload_digest` (required by S9): post-key-share, third-party verification of a
reconstruction needs `C` (combine ≥t key shares → `sk_s` → decrypt `C` → check
the commitment), and Stage 2 deletes `C`. Embedding `SHA256(C)` — not `C` itself,
which is ~4KB — keeps the record compact while making any archived copy of `C`
self-authenticating, exactly as the record digest does for the record. The slim
`Secret` already carries `pk_s` and the commitment as ordinary fields, so those
need no special handling.

Rules that spec.md must state explicitly:

1. Reveals sorted by guardian address (bech32 string, byte-wise ascending) — never
   by store iteration order, even though those currently coincide.
2. Marshalling uses gogoproto's deterministic mode, pinned by a golden test
   (marshal a fixed record, assert the exact bytes and digest) so an encoder
   change can never silently re-digest history. *(The original worry here —
   `recipient_metadata` being an order-unstable proto map — is gone:
   [DONE_RECIPIENT_PRIVACY_PLAN.md](DONE_RECIPIENT_PRIVACY_PLAN.md) landed and
   removed the field entirely; the slim record now has no map fields.)*
3. The digest is computed over the marshalled bytes at prune time, from state —
   never from a re-assembled or query-derived view. `payload_digest` likewise:
   hashed from the `SecretPayloads` entry read at prune time.

### Verification flow (what the tombstone buys)

1. Holder obtains the claimed final record from any archive (indexer, event log,
   their own node's history).
2. Marshals it canonically, hashes, compares to `SecretTombstones[id].record_digest`
   via a light-client-verifiable state query.
3. Match ⇒ the record is the authentic final record of that secret, with the chain
   still attesting to its final state and timing directly in the tombstone fields.

### The archival hook

The prune event emitted in Stage 2 carries the full canonical record (base64) plus
the digest. Indexers that retain events get a complete, self-verifying archive at
zero state cost; nodes that discard events lose nothing consensus-critical.

## 5. Query and client behaviour after pruning

- `Secret(id)` on a pruned secret: `NotFound`, as for a never-existent id. A new
  **`SecretTombstone(id)`** query distinguishes "pruned" from "never existed" —
  important for the SDK and the mobile client so they can render "expired/archived"
  rather than "unknown".
- `SecretsByCreator` no longer returns pruned secrets (index entry deleted).
  Creator-scoped history beyond the retention window comes from indexers —
  resolved §9.2: tombstones are **not** creator-indexed on-chain, though each
  carries its `creator` for standalone attribution.
- `HintsSince` no longer serves a pruned secret's hint (the feed entry is
  deleted at Stage 2 — resolved §9.7). Recipient discovery is bounded by the
  retention window by design; the SDK documents the six-month scan cadence.
- The S9 payload query (`Query/SecretPayload` or its assembled-view equivalent)
  returns `NotFound` after Stage 2, same as the reveal records — clients
  distinguish "pruned" via the tombstone query as above.
- **SDK behaviour**: reconstruction flows should surface the prune deadline
  (`terminal_at + RetentionBlocks`, both queryable) so recipients are warned while
  the reconstruction inputs still exist — post-S9 that means **both** the reveal
  records and the payload ciphertext `C`, which expire together at Stage 2.
  Resolved (July 2026): the SDK **does** actively warn — it is the client
  half of the §9.7 contract (scan at least every six months; reconstruct
  before the prune deadline).

## 6. Alternatives considered

1. **Epoch Merkle roots.** Consolidate each epoch's tombstone digests into one
   root; delete individual tombstones. State O(epochs), but point lookups stop
   being on-chain queries and proofs need an off-chain-served Merkle path. At ~60B
   per tombstone (1M pruned secrets ≈ 60MB) this is a future consolidation path,
   not a day-one need. Kept as the documented v2 if tombstone growth ever
   registers; the tombstone map is trivially migratable into it.
2. **No new state — consensus history only.** Historical app hashes already commit
   to every version of every record; an archive node plus a light-client proof at a
   historical height proves anything. Zero cost, but proving UX is poor
   (archive-node dependence, height bookkeeping) and inherits the node's own
   pruning policy. Rejected as the primary mechanism; remains true regardless and
   costs nothing.
3. **Never prune the slim record** (prune only reveals/shares). Simple, keeps all
   queries working, but state still grows ~2KB per secret forever and the "6 months
   then gone" policy the protocol wants cannot be expressed. Rejected.

## 7. Impact on operations

No user-facing transaction changes and no gas changes on any message. All pruning
work is EndBlock (ungassed), bounded per block by the prune cap. Terminal
transitions gain one queue-entry write and (Stage 1) up to ~64 deletes — EndBlock
side today as well, except cancellation, where Stage 1 deletes add ~65k gas to
`MsgCancelSecret` (deletes are flat-cost) — noise against the transfers it already
performs. Stage 2 writes one tombstone per pruned secret — ~142B at the
decided §4 model — as it deletes the slim record, the reveal
records, the hint-feed entry and the payload ciphertext; post-S9 the deleted
weight is dominated by `C` (up to ~4KB), with reveal records at ~34B each, so
net state strictly shrinks at any tombstone shape.

## 8. Migration

Pre-launch, retention ships in the genesis protocol and no migration exists.
If it instead lands via upgrade on a running devnet/testnet:

1. Walk existing secrets; for each already-terminal secret, enqueue at
   `terminal_at + RetentionBlocks` — `terminal_at` is already stamped by the
   FSM (it landed with the storage split), so the true height is available.
   Only records predating that upgrade carry `terminal_at = 0`; those fall
   back to `upgrade_height + RetentionBlocks` — a full retention window from
   the upgrade, not retroactive pruning.
2. Non-terminal secrets: no action; they enqueue naturally on reaching terminal.
3. Rehearse with `make devnet-upgrade-test`; extend the no-stranded-bonds/
   conformance suites with: pruned secret ⇒ tombstone exists ∧ no residual entries
   in any secret-keyed store (shares, assignments, reveals, creator index, queues,
   slash marks).

## 9. Decision log (resolved July 2026)

1. ~~**Param vs constant.**~~ **Resolved (July 2026): hardcoded constant.**
   Under the Position A ruling
   ([DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md)
   work item 3, which names this plan) there is no params mechanism for
   `RetentionBlocks` to live in — it joins the other compile-time constants in
   `constants.go`. The old sub-question about param changes dissolves into a
   simpler rule: queue entries are enqueued at terminal time with the compiled
   value and never re-keyed; if a future software upgrade ever changes the
   constant, its migration decides whether to re-key existing entries
   (recommend: don't — the new retention applies to secrets that terminate
   after the upgrade).
2. ~~**Tombstone creator index.**~~ **Resolved (July 2026): no listing index
   — option (a).** History beyond the retention window is indexer territory;
   wallets that track their own secret ids keep point-query access, and the
   decided §4 model's `creator` field (option c) keeps every tombstone
   attributable on its own. The options as weighed, for the record:
   - **(a) No index** *(ruled)*: history beyond the retention window is
     indexer territory. Wallets that track their own secret ids lose nothing
     (tombstones stay point-queryable); wallets that don't, use an indexer.
     Zero state cost.
   - **(b) A parallel `TombstonesByCreator` index**: full on-chain listing
     forever, at ~90B per secret ever — a second unbounded, never-pruned
     index, which partially defeats the point of this plan. The tombstone map
     itself is accepted unbounded state; doubling the per-secret forever-cost
     needs a stronger justification than a wallet convenience an indexer
     covers.
   - **(c) A `creator` field in the tombstone value** (§4 Group B): no
     efficient listing, but any tombstone becomes attributable on its own —
     "whose secret was this?" answerable in a dispute without an archive — at
     ~45B per tombstone. This is a §4 whittling decision, not an index
     decision; (b) and (c) can be taken independently, and (b) without (c)
     makes little sense (the index would prove membership but the tombstone
     couldn't name its owner). *Update (July 2026): the decided §4 model
     carries `creator`, so (c) is taken; (b) is **rejected** and (a) stands —
     ruled with the resolution above.*
3. ~~**Early Stage 2 on acknowledged reconstruction.**~~ **Resolved (July
   2026): no new message — the fixed clock suffices.** The retention window
   exists so the recipient can fetch the reveal records and `C` and
   reconstruct; once a reconstruction has actually happened, those records
   serve nobody for the remainder of the window. The chain cannot see this:
   reconstruction is wholly client-side, and there is deliberately no
   on-chain recipient identity to hear it from. The question is whether to
   accept an explicit signal — an optional `MsgAcknowledgeReconstruction`,
   verifiable in principle by a signature checked against `pk_s` (only a
   holder of ≥ threshold revealed shares can derive `sk_s`) — that re-keys
   the secret's prune-queue entry to "now". **Benefit**: frees the payload
   ciphertext (up to ~4KB — post-S9 the dominant retained weight; the ~34B
   reveal records are noise) up to six months early. **Costs**: a new
   message, handler, and queue re-key path for a bounded one-off saving per
   secret; and an acknowledgement proves *someone* reconstructed, not that
   the *intended recipient* did — for a public-disclosure secret, any
   passer-by could truncate the window on which other readers still depend.
   The §9.7 ruling settles it on principle as well: **the retention window
   is the availability contract in both directions — nothing extends it,
   nothing truncates it.** Revisit only if in-window payload state ever
   registers as a real growth problem.
4. ~~**Tombstone content.**~~ **Resolved (July 2026): the seven-field model
   in §4** — verification anchor (`record_digest`, `final_state`,
   `terminal_at`, `pruned_at`) + identity (`creator`, `created_at`) +
   `secret_commitment`; ~142B per tombstone, ~182B with the store key,
   ~182MB per million pruned secrets (arithmetic in §4). Decided by
   archive-free utility ranking; the eleven excluded candidates
   (`detection_hint` joined them via the §9.7 ruling) and their reasons are
   recorded in §4 "Considered and excluded".
5. ~~**Do tombstones themselves ever consolidate** into epoch roots (§6.1)?~~
   **Resolved (July 2026): explicitly deferred.** At ~142B per tombstone the
   map is decades from mattering (1M pruned secrets ≈ 182MB); §6.1 records
   the consolidation path and the tombstone map migrates into it trivially
   if growth ever registers.
6. ~~**S9 interaction.** The key-share architecture plan adds a stored payload
   ciphertext per secret; it must join Stage 2 and be included in the canonical
   record definition.~~ **Resolved (July 2026) — absorbed into the body of this
   plan**: S9 executes before this plan and is now the stated baseline (status
   banner). `SecretPayloads` joins the Stage 2 deletes and must not join Stage 1
   (§2); the canonical record gains `payload_digest = SHA256(C)` (§4); the
   payload query goes `NotFound` after Stage 2 and the SDK prune-deadline warning
   covers `C` as well as the reveals (§5).
7. ~~**The hint-feed entry and late-scanning recipients**~~ **Resolved (July
   2026): no post-prune discovery mechanism — the retention window is the
   whole availability contract.** *(The question originated in the July 2026
   viability re-check: the original delete lists missed `HintsByCreation`
   entirely.)* The hint-feed entry **joins the Stage 2 deletes**, and
   `detection_hint` is **excluded from the §4 tombstone**. The ruling, in
   the owner's words: once the retention window has passed, tombstones can't
   help you — only archives can. Post-prune detection without reconstruction
   inputs is merely a pointer into archive territory, and archive territory
   is where the whole question belongs. A side benefit unique to this
   option: the retroactive-scan surface stays bounded — a compromised
   recipient key can sweep at most ~6 months of live hints, never the full
   history.

   The client contract that replaces the mechanism, to be documented in the
   SDK and operator docs: **scan at least every six months**. A recipient at
   that cadence always sees the hint while the reconstruction inputs still
   exist; a later one must turn to archives/indexers for both discovery and
   recovery, exactly as for any other post-retention question. The
   un-enrolled-recipient patterns (spec.md, Recipient Discovery) are
   unaffected — they never depended on scanning.

   The two mitigations priced and rejected:
   - **`detection_hint` in the tombstone** (43B/tombstone): illusory
     cheapness — the tombstone map is keyed by id, so a late scan is a
     non-resumable full walk of every tombstone ever (~180MB+ transferred
     and ~1M X25519 ops per catch-up at a million tombstones), and making it
     efficient would need a `(pruned_at, id)` index plus a new query — i.e.
     rebuilding the hint feed with extra steps.
   - **Exempting `HintsByCreation` from Stage 2** (~90B/entry forever, all
     in): mechanically the clean option (cursor semantics preserved, zero
     new surface — just don't delete one entry), rejected on principle: it
     silently extends the availability contract past its own window and
     grows a second permanent per-secret store of exactly the kind the
     retention policy exists to eliminate.

## 10. Implementation checklist

Everything below is settled by §§2–9; this is the build list, in landing
order. One session, tests and docs included (CLAUDE.md
documentation-synchronisation rule).

1. **Constants** (`x/secrets/types/constants.go`):
   `RetentionBlocks = 2,592,000`; `MaxPrunesPerBlock` (proposed 50 — §3).
2. **Proto** (`proto/timeflare/secrets/v1/`): `SecretTombstone` (the
   seven-field §4 model), `TerminalSecretRecord` (the §4 canonical record:
   slim `Secret` + reveals sorted by guardian address + `payload_digest`),
   and a `SecretTombstone(id)` query RPC. No changes to any existing
   message. (Proto changes require explicit confirmation per CLAUDE.md —
   this plan's approval is that confirmation.)
3. **State** (`keeper.go`): `SecretTombstones
   collections.Map[string, types.SecretTombstone]`; `PruneQueue
   collections.KeySet[collections.Pair[int64, string]]`. Genesis:
   tombstones export/import; the prune queue is derived — rebuilt on import
   from non-pruned terminal secrets' `terminal_at + RetentionBlocks`, like
   the existing queues.
4. **Stage 1** — at every terminal transition (all funnel through the FSM /
   settlement paths, §2): delete `SecretShares`, `SecretAssignments`,
   `EarlyRevealSlash` marks; enqueue `(terminal_at + RetentionBlocks, id)`.
5. **Stage 2** — EndBlock drain (`<=`-drain, capped at `MaxPrunesPerBlock`,
   §3): build the canonical record **from state**, hash it, write the
   tombstone, emit the archival event (full canonical record base64 +
   digest), then delete the slim record, reveal records, payload
   ciphertext, creator-index entry, hint-feed entry, and queue entry.
6. **Queries/CLI**: `SecretTombstone(id)` with its AutoCLI
   `RpcCommandOptions` entry (query/CLI parity is enforced by construction
   — the root-command test fails without it).
7. **SDK**: prune-deadline warning in reconstruction flows (§5);
   "archived" rendering from the tombstone query; the six-month
   scan-cadence contract documented (§9.7).
8. **Tests**: golden canonical-encoding test (fixed record ⇒ exact bytes
   and digest — an encoder change must never silently re-digest history);
   conformance: pruned secret ⇒ tombstone exists ∧ **zero residual entries
   in every secret-keyed store** (shares, assignments, reveals, payloads,
   hints, creator index, both queues, slash marks); prune-cap carry-over
   under a same-height expiry burst; **archival-event content pinned
   exactly** (under §9.7 it is the load-bearing recovery path); an e2e
   scenario against a fast retention constant.
9. **Docs, same session**: spec.md — retention policy, the seven-field
   tombstone, query behaviour after pruning, and the six-month recipient
   scan-cadence contract; PROTOCOL.md — state table rows (tombstones, prune
   queue), Inefficiencies §1 closed.
