# Retry Client Convention — Fresh Seal Per Attempt — Plan

*Documents the July 2026 ruling that closes the failed-secret share-exposure
question by client convention rather than protocol change: a failed secret
is never resumed — a retry restarts the workflow with an entirely fresh
seal, so material distributed for a failed attempt can neither reconstruct
nor link to any later attempt. The convention is **advisory** (documented
client-side guidance), not mechanically enforced. No protocol, proto, or
SDK code changes; the SDK's seal path already behaves this way — the gap is
that nothing documents the convention as binding.*

> **Status: done — 27 July 2026** (PR #112, branch
> `worktree-retry-convention`). Proposed and ruled July 2026 in the
> pre-acceptance-exposure discussion; executed as specified, documentation
> only. PROTOCOL.md's Open Defects list is now empty — the pre-acceptance
> leak window lives on as Security Observations §3.
> **Priority**: P2 — security-posture documentation; cheap, and it settles
> the disposition of PROTOCOL.md's sole open defect.
> **Origin**: PROTOCOL.md "Open Defects" #1 (the collateral-free
> pre-acceptance leak window). The owner ruled: solve via client
> conventions; structural fixes rejected (see §4).
> **Components**: `docs/spec.md` (Common Attack Vectors #7 + Client-Side
> Integration Guide), `docs/PROTOCOL.md` (Open Defects → Security
> Observations), `typescript-sdk` (JSDoc advisory on the seal/session API —
> comments only). No proto, chain, guardian, or test changes.

## 1. Problem

Phase 2 distributes a decryptable key share to every one of the
`max_shares` selected candidates before any bond locks (acceptance *means*
"I decrypted my share and verified its HMAC", so material must precede the
bond). The variable-quorum gap bound (`max_shares − min_shares <
threshold`) keeps the never-confirmed set sub-threshold **on any secret
that activates** — but a secret that fails below `min_shares` leaves up to
`max_shares` never-bonded holders with live shares, collateral-free,
forever. The open question was what, if anything, stops that discarded
material from mattering.

## 2. The ruling (July 2026)

**A failed attempt is never resumed. A retry is a complete re-run of the
workflow with an entirely fresh seal:**

- a new `MsgUserRequestGuardians` (new protocol-assigned secret ID, new
  selection draw);
- a new inner encryption of the payload — fresh ephemeral, so the
  recipient-layer ciphertext `C_r` and therefore `secret_commitment =
  SHA256(C_r)` differ from the failed attempt's;
- a new per-secret keypair `(pk_s, sk_s)` (the key whose Shamir shares the
  guardians hold);
- a new t-of-n split, new per-guardian envelopes and HMACs, and a new
  detection hint.

**Clients must not reuse any sealed material across attempts.** The
convention is **advisory**: documented client-side, not enforced by the
chain or mechanically by the SDK.

### What the convention guarantees

Shares from a failed attempt are points on a discarded polynomial: they
cannot count toward any later attempt's threshold, cannot decrypt any later
attempt's ciphertext, and — because the inner seal is also fresh — cannot
even be linked to a later attempt on-chain (a reused inner seal would
repeat `secret_commitment` byte-for-byte and publicly connect the
attempts).

### The residual, accepted with open eyes

Any attempt that reached Phase 2 left its outer ciphertext `C` in public
chain history permanently (retention pruning removes live state, not
transaction history). A `≥ threshold` coalition of *that* attempt's
share-holders can therefore always unlock *that* ciphertext — for the
recipient only: `C` decrypts to the recipient-encrypted `C_r`, so the
coalition alone reads nothing. The content's time-lock is thus the minimum
over all distributed attempts. Bounds: protocol-random selection
(controlling `threshold` slots requires a large fraction of the guardian
population), the sunk 1,000 VEIL entry fee per colluding registration, and
the early-reveal slashing regime once bonds lock.

### The refund wait is deliberate

The reward pool refunds automatically in the EndBlock after
`commit_deadline`; the creator chose that deadline (20–200 blocks, so a
retry-conscious client picks the short end, ~2 minutes). There is **no
early exit** — the sit-out is what prices abandoned draws (the July 2026
commit-abandonment ruling,
[DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md](DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md))
and must not be softened by any fast-retry mechanism.

## 3. Implementation status — verified, nothing to change in code

`rust/src/seal.rs` (`seal_secret`) already generates a fresh single-use
per-secret keypair and randomised inner encryption on **every** call, and
the SDK session layer (`typescript-sdk/src/protocol/session.ts`) seals once
per secret ID — a retry necessarily has a new secret ID, hence a new
session and a fresh seal. The WASM and mobile paths share this seal
implementation (vector-pinned). The deliverable is therefore documentation
only: make the convention explicit and binding in the protocol docs, and
advisory-visible at the client API surface.

## 4. Structural alternatives — considered and ruled out

- **Accept-then-distribute** (bond before material): a fourth interaction
  in substance, and it forces a "was a share actually delivered?" gate onto
  the no-reveal slash. Rejected — the owner wants no growth in phase count.
- **Bond-at-selection** (lock every selected candidate's bond at Phase 1):
  ruled **too aggressive** — it seizes float for assignments the guardian
  never confirmed.
- **On-chain `pk_s` uniqueness check** (reject a previously seen
  `secret_public_key` at Phase 2): considered as a belt-and-braces guard
  against accidental key reuse; not adopted — client discipline suffices,
  and it would add proto/state surface for a failure mode the SDK cannot
  produce.

## 5. Changes

All documentation; British English per STYLE_GUIDE.md. One branch, one PR.

### 5.1 `docs/spec.md` — Common Attack Vectors #7 (the authority)

Retitle "Pre-Acceptance Share Exposure" from *(bounded, not closed — known
residual surface)* to *(accepted risk, ruled July 2026 — mitigated by
client convention)*, keeping the existing Exposure and Bound bullets and
adding:

- **Ruling — the retry convention** (§2 above, condensed): full restart,
  fresh seal, clients MUST NOT reuse sealed material across attempts.
- **Permanent residual** (§2 above): per-attempt `C` exposure, recipient-
  only, content time-lock = minimum over distributed attempts.
- **Structural fixes considered and ruled out** (§4 above, one line each).

The existing "Full fix: accept-then-distribute — deliberately out of scope"
bullet is absorbed into the ruled-out list.

### 5.2 `docs/spec.md` — Client-Side Integration Guide

Add a **Retry semantics** bullet to "Critical Points": on a commit-deadline
failure the pool refunds automatically in the next block; a retry restarts
the workflow from the top — new request, new inner seal, new
`(pk_s, sk_s)`, new shares, new hint; never reuse sealed material (link to
Common Attack Vectors #7); creators who care about retry latency should
choose short commit timeouts, and the sit-out until the deadline is
deliberate (no early exit — it prices abandoned draws).

### 5.3 `docs/PROTOCOL.md` — defect disposition

- Replace **Open Defects #1** with a "none currently open" note pointing at
  the new Security Observations entry and spec.md #7.
- Add **Security Observations §3** — "Pre-acceptance share exposure —
  accepted risk, client-convention mitigation (July 2026)": condensed
  problem statement, the ruling, the residual, the ruled-out structural
  fixes, authority pointers (spec.md #7 + the integration guide). §3, not
  §4: the grinding observation was resolved and removed from that list, so
  the surviving entries are §1 (guardian workload) and §2 (share/bond
  transfer).
- Update the Load-Bearing Oddities intro note's cross-reference ("remains
  Open Defect #1" → the new §3).
- **Redirect the inbound link.** `docs/ACCEPTED_TRADEOFFS.md` §4 cites
  `PROTOCOL.md#open-defects--cleanup-notes` for this residual; retiring
  Open Defect #1 breaks that, so it is repointed at the new §3 in the same
  change. Left to a later sweep it would read as a live defect that no
  longer exists.

### 5.4 `typescript-sdk` — the client-side advisory

JSDoc additions only (no behavioural change): on the seal API
(`sealSecret` in `src/protocol/crypto.ts` / `src/backends/wasm.ts`) and the
session create/distribute flow (`src/protocol/session.ts`), a short
advisory: *"Retry semantics: a failed secret is never resumed. Retry by
starting a new session — every attempt must be sealed fresh; never persist
and re-submit a previous attempt's sealed output (spec.md, Common Attack
Vectors #7)."* The mobile client consumes the same SDK and inherits the
advisory; no separate change.

### 5.5 Cross-plan hook — already delivered

The consolidated tradeoff entry exists: `docs/ACCEPTED_TRADEOFFS.md` §4, "A
failed attempt's ciphertext stays exposed to its coalition", carries the
ruling, the residual and the ruled-out structural fixes, and cites this plan
as where it was decided. It landed as a standalone catalogue rather than a
section inside the docs-accuracy refresh, which no longer carries a Known
Tradeoffs deliverable.

So this plan owns only the underlying content (5.1–5.4), and the dependency
runs **both** ways: that entry links back here, so moving this plan to
`done/` must update it — as must retiring Open Defect #1 (§5.3).

## 6. Verification

Doc-only: `make verify` (lint/format), link check on the edited documents
**including the inbound link from `ACCEPTED_TRADEOFFS.md` §4**, and a
read-through against the ruling in this plan. No e2e (no behaviour changes).
The seal-freshness property this plan relies on is already pinned by the
shared `testdata/vectors/` corpus and both crypto suites — no new tests (and
none would be meaningful for an advisory).

§3's claim that no code changes are needed is re-verified by inspection
before the advisory is written, rather than trusted from July: if the seal
path had stopped being fresh per attempt, the advisory would document a
convention the SDK no longer honours.
