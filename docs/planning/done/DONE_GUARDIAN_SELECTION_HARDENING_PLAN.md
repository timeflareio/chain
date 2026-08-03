# Guardian Selection Hardening Plan

**Status**: DONE — implemented (July 2026, `guardian-selection-hardening-plan` branch). All decisions D1–D9 and the resolved open questions below are live: protocol-assigned secret IDs (`SecretCounter` sequence + UUIDv5 derivation, genesis-exported), consensus-only selection seed, hash sortition with the strict `shares + buffer` gate, `GuardianSelectionConfig` and creator `secret_id` removed from the message surface, seed-input event attributes, the chi-squared fairness suite, and spec.md/operations.md/PROTOCOL.md synchronisation. This document remains as the design rationale and decision log.
**Priority**: P0 — protocol security, pre-testnet
**Components**: `x/secrets/keeper/guardian_selection.go`, `x/secrets/keeper/keeper.go` (new secret counter), `proto/timeflare/secrets/v1/tx.proto`, `x/secrets/client/cli/tx.go`, `typescript-sdk`, `docs/spec.md`, `docs/operations.md`, devnet/e2e scripts

## What this plan does

Removes the creator's ability to bias which guardians are selected for a secret, and replaces the selection mechanics with a primitive that is easier to make deterministic and verifiable. Today the selection seed is `SHA256(chainID ‖ height ‖ lastBlockHash ‖ clientEntropy)`, truncated to 64 bits and fed into Go's non-cryptographic `math/rand` for a Fisher–Yates shuffle — every seed input is either public or creator-supplied, so a creator can grind the outcome.

## Why

Every input to the current seed is known or chosen by the creator at submission time:

- `chainID` and `height` are fixed and public.
- `lastBlockHash` is public the moment the previous block commits.
- `clientEntropy` is supplied by the creator in `MsgRequestGuardians`.

`clientEntropy` is the load-bearing defect. It is the **only lever an attacker has to influence someone else's selection** — the "attacker who compromised specific guardians steers a victim's secret onto them" scenario works only if the attacker can influence the victim's client, i.e. through `clientEntropy`. Removing it closes that third-party threat outright. `clientEntropy` also adds nothing defensive: the creator is the adversary the randomness must resist, so an honest creator gains nothing by contributing entropy.

Secondary defects: truncating a 256-bit digest to a 64-bit `int64` seed discards entropy for no reason, and seeding `math/rand` from attacker-influenced input is the core weakness — a keyed, hash-based primitive removes the PRNG question entirely.

## Decisions (July 2026)

These were settled in review and supersede the "options" framing the automated draft carried.

### D1 — Option A: remove client entropy; keep verifiable random selection

Selection stays protocol-controlled and re-verifiable from public data. `clientEntropy` is removed from the message and the seed. Commit–reveal (the old Option B) is **held in reserve** — it does not actually close retry-grinding without also making commit abandonment costly, and it adds a full round-trip of latency to every secret to defend a residual threat we are ruling out of scope (D2). An on-chain randomness beacon (old Option C, e.g. drand/VRF) remains the strongest long-term fix and is recorded as future infrastructure, not a launch requirement.

### D2 — Self-selection is out of scope

A creator biasing *their own* secret's selection endangers only their own material — the same category as choosing `threshold=2`. We do not defend against it, which means the offline grinding-advantage analysis memo the draft proposed is **not needed**. The only third-party threat (a creator making a secret *look* neutrally selected while secretly landing ≥threshold on guardians they own) is judged immaterial: the corrected seed (D3) gives whole-set re-rolls only, never surgical placement, and that is no cheaper than the sybil route the bond economics already price.

### D3 — Corrected seed formula

```
seed = SHA256(chainID ‖ height ‖ lastBlockHash ‖ secretCounter)
```

`creatorAddress` is **removed** from the seed — it was an *offline-parallelisable* grind lever (a griefer generates many candidate addresses, computes the resulting selection for each offline, funds and submits from the winner — no block-waiting). `secretCounter` (D4) supplies the per-secret differentiator that keeps two secrets in the same block from sharing a selection, and the creator cannot choose it. The creator's only residual influence is submission timing (one whole-set re-roll per block), which D2 rules acceptable.

`lastBlockHash` MUST be read from consensus-agreed header state (the previous block's `LastBlockID.Hash`), never anything derived from the current block (whose hash is unknown during execution).

### D4 — Protocol-derived secret ID via a monotonic counter

`secretID` becomes protocol-assigned, not creator-supplied, so nothing the creator picks touches the seed. Mechanism: a `collections.Sequence` in keeper state (`SecretCounter`), read-and-incremented at execution time. The value is:

- **Deterministic** — every validator executes the block's transactions in the same consensus order, so each request reads the same counter value.
- **Unique** — a strictly increasing `uint64` never repeats within the chain.
- **Grind-proof** — the creator has zero control over the value they receive.

The user-facing ID keeps its UUID string shape via a deterministic derivation (`UUIDv5(namespace, chainID ‖ counter)` or `hex(SHA256(chainID ‖ counter))`), which also folds in `chainID` for global uniqueness and avoids leaking a clean running total. The `secretCounter` value (not the creator's input) is what feeds the seed.

Two correctness requirements:
- **Persist the high-water mark independently of pruning.** The `TERMINAL_SECRET_RETENTION` plan deletes terminal secrets; the counter must be its own persisted sequence that only ever climbs — never re-derive "next ID" from "max existing secret ID", or IDs reissue after pruning.
- **Export/import the counter in genesis** — it is consensus state; a chain restarted from exported genesis must continue the sequence, not reset to zero.

### D5 — Sortition, not a shuffle

Selection uses hash-sortition rather than a Fisher–Yates shuffle:

```
ticket(g) = SHA256(seed ‖ guardian_address)      // per guardian
select    = the k guardians with the lowest tickets, k = shares + buffer
```

Tickets are compared as big-endian 256-bit integers; the astronomically unlikely tie is broken by guardian address ascending. This is chosen over a shuffle because:

- **Order-independence** — a guardian's ticket depends only on the seed and their own address, not on their position in any candidate list. The outcome is the same regardless of enumeration order, so the fragile "every validator must agree on the exact candidate ordering" requirement shrinks to just the tie-break. This is what lets the `GUARDIAN_SELECTION_SCALABILITY` plan later change candidate *enumeration* (an eligibility index) without changing selection *outcomes*.
- **Stable under pool changes** — adding or removing one guardian changes only whether *that* guardian wins a slot; everyone else's ticket is unchanged. A shuffle's swap sequence cascades, moving unrelated guardians in and out.
- **Trivial verification** — recompute each guardian's ticket and check the lowest k; independent, per-guardian, no replay of a stepwise shuffle.

Fairness is unchanged: because the seed differs per secret, each guardian draws a fresh uniform ticket every time, so every eligible guardian has probability `k/n` of selection on any secret regardless of address. "Lowest" is an arbitrary convention (highest would be identical). This also removes the 64-bit truncation and the `math/rand` dependency in one move — the mechanical fixes the draft listed are subsumed here.

### D6 — Verifiability means "the seed was honest", not full recomputation

The property we claim and support is **Claim A**: anyone can confirm from public data that the seed was `SHA256(chainID ‖ height ‖ lastBlockHash ‖ secretCounter)` with the real public `lastBlockHash` and the on-chain counter — which proves the *creator* could not have biased it, because none of those inputs are theirs. We do **not** claim, and do not build for, **Claim B** (independently recomputing the exact selection from the eligible set as it stood at height `h`) — that would require reconstructing historical guardian state and is really a statement about validator honesty, which BFT consensus already provides. Consequence: the reservation event carries the seed inputs but **not** the full candidate set (which could be hundreds of guardians of bloat). spec.md's current "re-verify from public data" language is tightened to Claim A.

### D7 — Delete `GuardianSelectionConfig`

With `clientEntropy` gone and `random` the only algorithm, the whole `GuardianSelectionConfig` message is dead weight and is removed (proto change; pre-launch breaking is acceptable per project convention). If the future randomness beacon (D1) ever lands, it reintroduces a version discriminator then — adding a field later is cheap; carrying a dead enum-of-one now is the thing the codebase style rejects.

### D8 — Test bar: fairness first

The acceptance bar is **provably uniform selection across eligible guardians** (chi-squared uniformity over many seeds) — this matters for operator fairness regardless of the grind decision, since a biased tie-break or hash quirk would starve some guardians of work. The grinding-resistance simulation the draft proposed is dropped, following D2.

### D9 — No migration

Pre-launch; devnet resets and there are no live secrets, so there is no state migration. The counter (D4) starts at genesis.

## How

Implementation lands in one branch (protocol change ⇒ pre-launch breaking change is acceptable):

1. **Keeper counter** — add `SecretCounter collections.Sequence`; assign at the start of `MsgRequestGuardians` handling; derive the user-facing `secretID`; wire genesis export/import.
2. **Seed + sortition** — rewrite `guardian_selection.go` to build the seed per D3, score eligible guardians per D5, and take the lowest-k. Pin the canonical eligibility predicate (unlocked float ≥ `B`, within availability window, `accepting`) evaluated at the seed height, and the tie-break rule, in spec.md.
3. **Proto/message** — remove `GuardianSelectionConfig` and the creator-supplied `secret_id` from `MsgRequestGuardians`; regenerate. (Proto edit requires explicit confirmation per CLAUDE.md — treat as covered by these recorded decisions.)
4. **Full-stack ripple of the secretID change** — CLI `request-guardians` loses its `secret-id` positional arg (the same command touched by the pure-Go crypto PR); `operations.md` validation rules updated; `isValidUUID` likely becomes dead; TypeScript SDK `requestGuardianAssignments`; devnet/e2e scripts.
5. **Reservation event** — emit the seed inputs (height, `lastBlockHash`, counter) for Claim-A verification; do not emit the candidate set (D6).
6. **Tests** — uniformity/fairness suite (D8); determinism across validators; genesis round-trip of the counter.
7. **Docs** — spec.md (seed description, sortition, secretID assignment, event contents, tightened verifiability claim, removal of the `clientEntropy`/`selection_seed` language) and operations.md.

## Open questions — all resolved (July 2026)

1. **Insufficient-eligible-guardians floor — resolved: fail the transaction.** This is
   the verified current behaviour (`guardian_selection.go` requires the full
   `shares + buffer` and rejects otherwise, with no reduced-buffer fallback); the
   rewrite preserves it and spec.md pins it. A reduced-buffer mode was rejected —
   it would silently change the secret's rejection tolerance without the creator
   knowing.
2. **User-facing ID encoding — resolved: UUID-shaped.**
   `UUIDv5(namespace, chainID ‖ counter)` — keeps the 36-char shape existing
   clients and `SecretIdLength` validation expect, and hides the running total.
3. **Byte-level sortition spec — resolved: as recommended.**
   `ticket = SHA256(seed ‖ guardian_address)`, compared as full 256-bit
   **big-endian** integers, lowest k win, ties broken by guardian address
   ascending (byte-wise on the bech32 string). Pinned normatively in spec.md as
   part of step 7.

### Related decision — abandon-and-refund forfeit is a separate plan

The commit-timeout forfeit fix (PROTOCOL.md Security Observations §1 — refund `P`
minus a forfeit so discarded draws are not free) is **deliberately out of this
plan's scope** (ruled July 2026). This branch stays scoped to selection
mechanics; the forfeit is economics-only (no proto change) and has its own
plan: [DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md](DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md). Note the consequence honestly: until it lands,
D3's "one whole-set re-roll per block" understates the residual — each request
draws its own `secretCounter`, so N parallel requests are N independent draws,
each fully refundable at commit-timeout for the cost of a transaction fee. D2
rules self-selection out of scope, so this residual is accepted for now, not
solved.

### Implementation notes (recorded during review, July 2026)

- `calculateBuffer` currently uses `float64` arithmetic
  (`shares × 0.30 + 0.99`) on a consensus path — replace with integer math
  (`(shares×30 + 99) / 100`) while the D5 rewrite has the file open.
- When wiring the D4 counter's genesis export/import, add its validation
  (present, and ≥ what existing secrets imply) to `GenesisState.Validate()`
  rather than deferring it to the SETTLEMENT_AND_STATE_INTEGRITY plan.

## Sequencing

- Land before the first public testnet — this changes consensus behaviour and the message surface; both are free to change pre-launch and painful after.
- Do this **before** `GUARDIAN_SELECTION_SCALABILITY` — both rewrite `guardian_selection.go`; sortition (D5) makes the scalability index safe by decoupling enumeration order from selection outcome.
- Feeds the `SECURITY_AUDIT_READINESS` threat model (the "protocol-controlled selection" claim becomes accurate once this lands).
