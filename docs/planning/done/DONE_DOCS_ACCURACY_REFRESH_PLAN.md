# Documentation Accuracy Refresh Plan

**Status**: done (2 August 2026 — [PR #137](https://github.com/leedavis81/timeflare/pull/137) merged, CI green).
Every open question was ruled by the owner on 1 August 2026, and the two ruled
deletions executed the same day on main (TODO.md deleted in favour of
`docs/planning/` as the roadmap; `docs/guides/FAQ.md` deleted with its one
live procedure lifted into GUARDIAN_KEY_CUSTODY.md "Compromise"). The August
2026 chain-docs reconciliation (PR #135) had already resolved the plan's
largest items; every remaining item was re-verified against the code on
1 August 2026 before execution.
**Priority**: P3 — cheap, high-trust-value housekeeping
**Components**: `docs/guides/TESTING_COMMANDS.md`,
`rust/README.md` (the source of the wasm build copy), `typescript-sdk/package.json`,
`x/secrets/client/cli/tx.go` (help text only), `x/secrets/module/autocli.go`,
`proto/timeflare/secrets/v1/` (comments only, regenerated), `CLAUDE.md`,
`docs/CHAIN_MECHANICS.md` (Design decisions FAQ),
`docs/planning/automated/TESTNET_LAUNCH_PLAN.md` (one content note),
`cmd/timeflared` (the example drift-guard test)

## What this plan does

A single sweep to bring every document that contradicts the current codebase
back into line, plus one guard so the class of rot stops recurring. The
project's convention (spec.md is authoritative; every code change updates
docs) has been honoured for spec.md and, since August 2026, for operations.md
and CHAIN_MECHANICS.md — but the remaining satellites (guide examples,
in-code help text, proto comments) have drifted, and several actively
mislead.

## Why

Stale documents are not neutral: they burn contributor time, embarrass the
project in front of operators and auditors, and — the sharpest case below —
give users commands that fail against the running devnet. The August 2026
reconciliation also demonstrated the failure mode this plan exists to
prevent: operations.md had described a retired protocol for months because
nothing pinned it to the code.

## Scope boundaries

- **Guardian documentation** (`guardian/`) is owned by
  [PENDING_GUARDIAN_DOCS_RESYNC_PLAN.md](../guardian/PENDING_GUARDIAN_DOCS_RESYNC_PLAN.md)
  and the guardian pre-testnet sweep — not touched here.
- **Mobile documentation** (`mobile-client/`) is owned by the mobile sweep
  plans — not touched here.
- **Behaviour-claim misalignments in spec.md and CLAUDE.md** (protocol-enforced
  "progressive time-locks" phrasing, chain-side validation claims, the retired
  share-index paragraph) are
  [CHAIN_MECHANICS.md Deviations #1–#3](../../CHAIN_MECHANICS.md#deviations-from-documented-intent)
  — each needs an owner ruling and stays in the ledger. This plan takes only
  mechanical accuracy fixes that no ruling is needed for.

## The verified drift inventory (1 August 2026)

### 1. docs/guides/TESTING_COMMANDS.md — every command targets the wrong chain

All 15 `--chain-id` flags say `timeflare-local`; the devnet (`make dev-up`,
`devnet/fund.sh`) uses **`timeflare-test`**. Every copy-pasted command in the
project's own testing guide fails against the devnet it documents. The
command shapes themselves were re-swept July 2026 and are otherwise current.

**Action**: mechanical replace; while in the file, spot-check flags against
the current devnet defaults (keyring, node address).

### 2. typescript-sdk/wasm/README.md — export list three functions behind

The listed WASM surface is accurate except it omits the three newest exports:
`is_usable_x`, `rebate_commitment`, `recipiency_proof` (the low-order-key
check and the rebate collection pair). `rust/README.md` was re-verified and
is clean.

**Action**: add the three exports with one-line descriptions.

### 3. typescript-sdk/package.json — dead homepage

`homepage` points at `…/tree/main/client#readme`; `client/` does not exist.
(Also in SDK_PRODUCTIONISATION Phase 1 — whichever lands first fixes it.)

### 4. x/secrets/client/cli/tx.go — in-code help examples fail validation

`CmdUserRequestGuardians`'s `Long` text carries pre-band examples:
`user-request-guardians <80-hex-chars> 3 15 100` parses today as
threshold 3, min 15, max **100** and fails `ValidateBasic` (max > 32); the
`--random-hint 3 15 250 150 200` example is similarly mis-shaped. Found
during the August 2026 reconciliation, which validated every operations.md
example against the built binary — the in-code help was out of its scope.

**Action**: rewrite the two `Long` examples to match operations.md's
validated shapes (e.g. `5 6 9 100 150 150`). Help-text-only change; no
behaviour.

### 5. x/secrets/module/autocli.go — `hints-since` is the only uncurated query

Every query RPC has a curated `RpcCommandOptions` entry except `HintsSince`,
which falls back to the generated `"Execute the HintsSince RPC method"` help
with a bare `--since-height` flag.

**Action**: add the entry (positional `since_height`, a `Short` describing
the discovery-scan cursor, pagination note), matching the file's convention
that the descriptor is the single inventory of the CLI surface.

### 6. Proto comment drift (comments only; requires `make proto-gen`)

- `secret.proto` `Secret.id`: "Unique identifier for this secret, **chosen by
  the creator**" — IDs are protocol-assigned (UUIDv5 over
  `chainID ‖ counter`); the creator supplies nothing.
- `secret.proto` `reward_pool` and the `tx.proto` `MsgUserRequestGuardians`
  header both state `P = rate × distance × max_shares × bump` — the
  pre-cost-recovery formula. The pool has carried the reveal leg since the
  guardian cost-recovery ruling:
  `P = (max_shares × F_reveal) + (rate × distance × max_shares × bump)`.

**Action**: correct both comments, regenerate, and grep the generated `.pb.go`
diff to confirm comments-only. The protocol-surface rule (proto + types +
spec in one PR) is satisfied trivially: no field or behaviour changes.

### 7. CLAUDE.md — same pricing-formula omission

Line "Protocol-derived pricing: reward pool `P = rate × distance × shares ×
bump`" omits the reveal leg (and uses the retired `shares` name for the band
ceiling). Mechanical correction to the current formula; CLAUDE.md's
"progressive time-locks" phrasing is Deviation #1 and is **not** touched here
(needs a ruling).

### 8. Guardian listener-node guidance — homeless since architecture.md

The one salvageable idea from the deleted architecture.md: guardians should
run a non-validating full node ("listener node") rather than depend on
third-party RPC. Operator-facing, so its home is the guardian operator
handbook that TESTNET_LAUNCH Phase 2 scopes.

**Action**: one content note added to TESTNET_LAUNCH Phase 2 item 2 so the
handbook inherits the recommendation; nothing else.

### 9. Design decisions FAQ in CHAIN_MECHANICS.md (owner request, July 2026; placement ruled 1 August 2026)

The deliberate design decisions the owner has ruled on live in exactly two
engineering-facing places — spec.md's Common Attack Vectors *(accepted risk)*
entries and CHAIN_MECHANICS.md's judgement ledger — but there is no single
reader-facing page a creator, guardian operator or auditor can skim. These
are **not known issues**: each looks wrong at first glance, solves a real
problem, and was chosen with the alternative understood — the documentation's
job is to explain them, not apologise for them.

**Placement (ruled)**: a compact **"Design decisions — the questions readers
ask"** section in `docs/CHAIN_MECHANICS.md`, fronting the Judgement Ledger.
Living in the same document as the entries it indexes means zero cross-doc
drift — each answer is one short paragraph plus an anchor link to the ledger
entry or spec section that owns the argument; the FAQ summarises, it never
becomes a second authority. Seed list, with citations against the current
ledger:

- *Why does creating a secret take two transactions?* — the financially
  binding first step **is** the anti-grinding mechanism; never collapse it
  (Oddity #1).
- *Why can anyone compute a share's HMAC key?* — deliberately public inputs
  so the chain verifies evidence keylessly; HMAC proves binding, not honesty
  (Trade-off §2).
- *Why does a slashed guardian's "reveal" come from their accuser?* — the
  auto-submitted evidence salvages the leaked share and prevents double
  punishment (Oddity #2).
- *Why can't I report an early leak once the reveal window opens?* —
  possession stops proving a pre-window leak the moment reveals are legal
  (Trade-off §3).
- *Why do guardians get paid only at window close, even when the threshold
  was met on day one?* — one settlement point with complete information;
  paying early races the slashing regime (Trade-off §11).
- *Why must guardians be available for the whole reveal window, and why is
  the reveal horizon capped at a year?* — the cap is permanent, share/bond
  transfer is ruled out forever (Trade-off §13; spec.md Common Attack
  Vectors #6).
- *Why doesn't the creator get a partial refund for unfilled slots or
  no-shows?* — the pool prices the band ceiling; forfeited slices reward the
  guardians who did the work (Trade-off §6).
- *Why doesn't the chain check that my shares or commitment are valid?* —
  the chain guarantees reconstruction integrity, never content validity
  (Trade-off §1; spec.md Common Attack Vectors #5).
- *My secret failed to activate — can I resume it?* — never; retry is a full
  restart with a fresh seal (Trade-off §4;
  [DONE_RETRY_CONVENTION_PLAN.md](../done/DONE_RETRY_CONVENTION_PLAN.md)).
- *Why must an abandoned draw wait out the full commit timeout, and why does
  every draw cost a fee?* — the non-refundable creation fee prices the draw
  and the sit-out is deliberate anti-grinding structure (Trade-off §5;
  [DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md](../done/DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md)).
- *Who can collect a rebate, and why does collecting link my address to the
  secret?* — recipiency is proved by a bearer secret, and publishing it is a
  deliberate, opt-in exception to recipient privacy (spec.md "Recipient
  Rebate"; operations.md "Recipient Rebate").

The list is expected to grow; the section's rule is that every entry cites an
owner ruling — it records decisions, it does not make them.

### 10. The example drift guard (ruled in, 1 August 2026)

Nothing pins the guide examples or in-code help to the CLI the way
`TestTxCommandParity` pins the command inventory — which is why operations.md
rotted for months and the tx.go help is stale right now.

**Action**: a test in `cmd/timeflared` that extracts the `timeflared …` bash
examples from `docs/operations.md` and the tx-command `Long` texts, and
asserts each against the built command tree: the command path resolves and
the positional argument count satisfies the command's `Args` validator
(message-level `ValidateBasic` where cheaply constructible). Lands in the
same commit as item 4 and must catch item 4's stale examples before they are
fixed (red → green). This is the only deliverable here that prevents
recurrence rather than fixing an instance.

## How

One branch, ordered by blast radius: (1) the TESTING_COMMANDS chain-id fix;
(2) wasm README exports + package.json homepage; (3) the drift-guard test
(item 10) proving item 4's examples stale, then the tx.go help-text fix and
the autocli `hints-since` entry — Go changes, so `make test` and
`TestRootCmdSmoke`/`TestTxCommandParity` must pass; (4) proto comment
corrections + `make proto-gen` (verify comments-only diff) + the CLAUDE.md
formula line in the same commit (protocol-surface convention); (5) the
TESTNET_LAUNCH content note; (6) the Design decisions FAQ section in
CHAIN_MECHANICS.md. Everything is verifiable against the codebase as listed
above; no behaviour changes. British English per STYLE_GUIDE.md throughout.

## Open questions

None — all ruled 1 August 2026 (TODO.md deleted in favour of the planning
directory; the operational FAQ deleted with its live procedure lifted to
GUARDIAN_KEY_CUSTODY.md; the Design decisions FAQ homes in
CHAIN_MECHANICS.md; the drift guard is in scope).
