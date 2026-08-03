# Actor-First Message Naming — Plan

*Renames every transaction message so the signing role leads the name:
`MsgGuardian…` for guardian-signed messages, `MsgUser…` for user-signed
(secret-creating) messages. The prefix answers "who must sign this?" at a
glance — today the actor is discoverable only from the
`cosmos.msg.v1.signer` option in the proto.*

> **Status: done — executed July 2026** on branch
> `worktree-msg-actor-naming` (PR #106). All gates passed locally: `make verify` +
> `make test`, fresh-devnet `make e2e` (full lifecycle) +
> `make e2e-scenarios` (34/34 assertions); grep audit returned zero
> stale references outside `done/`.
> **Priority**: P1 — protocol-surface naming; wire-breaking, so it must
> land pre-testnet, and it should land **before**
> [DONE_GUARDIAN_KEY_ROTATION_PLAN.md](DONE_GUARDIAN_KEY_ROTATION_PLAN.md)
> executes (that plan's new message is already specified in the new
> convention, so ordering this first avoids renaming twice).
> **Origin**: owner ruling, July 2026 — review session following the
> guardian key custody plan; the trigger was
> `MsgRotateEncryptionKey` → `MsgGuardianRotateKey`, widened to
> a repo-wide convention for both roles.
> **Components**: `proto/timeflare/secrets/v1/tx.proto`;
> `x/secrets/types` (regenerated pb, message constructors,
> `ValidateBasic`, codec registration); `x/secrets/keeper` (msg server);
> `x/secrets/client/cli/tx.go` (hand-written tx verbs);
> `guardian/` daemon; `typescript-sdk/`; `mobile-client/`;
> `docs/spec.md` and guides (`TESTING_COMMANDS.md`, FAQ, custody
> runbook); devnet/e2e scripts; pending plans that cite old names.

## The convention

**The message name leads with the role that signs it.** The role is the
protocol actor with an on-chain identity: `Guardian` or `User` (the
account that creates, funds, and may cancel a secret). Messages where a
role is the *object* rather than the signer do not carry that role as a
prefix.

**The role appears in message names only** (ruled July 2026): the signer
fields keep `creator` — with `MsgUserCancelSecret`'s `sender` field
renamed to `creator` in the same break, so the three user-signed
messages agree — and spec/guide prose keeps "creator" as the descriptive
domain term (a recipient is also a user; this plan's purpose is signer
clarity in the protocol surface, not a vocabulary migration).

## Renames

| Old | New | Signer |
|---|---|---|
| `MsgRegisterGuardian` | `MsgGuardianRegister` | `guardian` |
| `MsgUpdateGuardian` | `MsgGuardianUpdate` | `guardian` |
| `MsgConfirmShares` | `MsgGuardianConfirmShares` | `guardian` |
| `MsgRevealShare` | `MsgGuardianRevealShare` | `guardian` |
| `MsgWithdrawStake` | `MsgGuardianWithdrawStake` | `guardian` |
| `MsgRequestGuardians` | `MsgUserRequestGuardians` | `creator` |
| `MsgDistributeShares` | `MsgUserDistributeShares` | `creator` |
| `MsgCancelSecret` | `MsgUserCancelSecret` | `sender` → `creator` |

`MsgRegisterGuardian`/`MsgUpdateGuardian` invert rather than prefix —
`MsgGuardianRegisterGuardian` would be redundant.

**Unchanged**: `MsgSlashGuardian` — signed by `reporter_address` (anyone
holding early-reveal evidence); the guardian is the object of the
message, not its actor, and "reporter" is not a bonded protocol role.
Ruled to stay as-is (owner, July 2026).

**Planned messages adopt the convention at birth**: the key rotation
plan's message is specified as `MsgGuardianRotateKey`
(already updated there).

The service RPC names follow the message names (`rpc GuardianRegister`,
`rpc UserRequestGuardians`, …) — gRPC method paths change with them.
The hand-written CLI tx verbs follow too (`guardian-register`,
`user-request-guardians`, …), with **no legacy aliases**: pre-launch, a
clean break beats a compatibility surface (the AutoCLI *query* aliases
are unaffected — query RPCs are not renamed by this plan).

Event type names are out of scope: they are state-transition-named
(`secret_reserved`, `secret_pending`, …), not actor-named, and carry no
signer ambiguity.

## Execution order (spec-first)

1. `docs/spec.md` — rename messages in "Core Operations" and everywhere
   cited; this leads the code change.
2. `proto/timeflare/secrets/v1/tx.proto` — messages, RPCs, comments;
   regenerate (`make proto-gen`). Protocol-surface rule: proto +
   `x/secrets/types` + spec.md land as one unit.
3. `x/secrets/types` — constructors, `ValidateBasic`, codec/amino
   registration names.
4. Keeper msg server + module wiring; CLI tx verbs.
5. Guardian daemon (message construction and any string references).
6. TypeScript SDK + mobile client (single protocol layer since the TS
   consolidation — one rename surface, but sweep both packages).
7. Docs and scripts: `TESTING_COMMANDS.md`, custody runbook, FAQ,
   devnet/e2e scripts that invoke tx verbs.
8. Pending-plan sweep — update message citations in:
   `REJECTED_TOKEN_ECONOMICS_PLAN.md`, `PENDING_RETRY_CONVENTION_PLAN.md`,
   `automated/GUARDIAN_SELECTION_SCALABILITY_PLAN.md`,
   `automated/DOCS_ACCURACY_REFRESH_PLAN.md`, and the three
   `client-app/` plans that cite old names. (Done plans are historical
   records — not swept.)

Grep-driven audit before and after (the ShareIndex lesson): every old
name must return zero hits outside `docs/planning/done/` and git
history.

## Gates

- `make test` + `make verify` (includes the AutoCLI root-command guard).
- Fresh-devnet `make e2e` + `make e2e-scenarios`; PR labelled `e2e`.
- Mobile-client lifecycle e2e (runs in CI on mobile-client paths).
- `buf breaking` will flag every rename — expected and accepted
  pre-launch; this is a deliberate wire break (type URLs, signatures,
  and gRPC method paths all change). Post-launch this class of change
  would be impossible without a migration; that is exactly why it lands
  now.

## Not solved here

- No renaming of query RPCs, events, or state keys — no actor ambiguity
  exists there today.
- `MsgSlashGuardian` keeps its name (rationale above); if a bonded
  "reporter"/watcher role ever emerges, its messages adopt the
  convention then.

## Open questions

None — all questions ruled July 2026 (folded into "The convention"
above).
