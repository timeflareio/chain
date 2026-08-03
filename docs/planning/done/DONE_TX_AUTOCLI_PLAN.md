# Tx AutoCLI Migration — Plan

*Extends the AutoCLI parity-by-construction guarantee from the query surface
to the transaction surface: five of the nine hand-written tx commands become
generated commands, four stay hand-written behind explicit `Skip` markers
(two for genuine client-side logic, two blocked by client/v2's coin flag),
and a descriptor-walk test makes tx-command drift a build failure rather
than a latent defect.*

> **Status: done — 25 July 2026, branch `tx-autocli`.** The
> `MsgUpdateGuardian` wrapper-field spike resolved in favour of generation,
> but end-to-end verification then surfaced a client/v2 coin-flag
> incompatibility that keeps both Coin-bearing messages hand-written
> (findings folded into §2.1–2.2). Verified locally: `make verify`,
> `make test`, fresh-devnet `make e2e` (full lifecycle) and
> `make e2e-scenarios` (34 assertions — exercises `request-guardians`,
> `cancel-secret`, `slash-guardian` through the CLI on-chain), plus an
> on-chain register → update → withdraw-stake cycle covering the retained
> hand-written commands and the generated `withdraw-stake`.
>
> **Priority**: P2 — tooling correctness. Not protocol work, but the
> motivating defect is live: the `distribute-shares` tx command is broken
> today (see Origin), and the same drift mechanism threatens every
> hand-written command on every protocol change.
>
> **Origin**: July 2026 CLI review. `CmdDistributeShares`
> (`x/secrets/client/cli/tx.go`) never sets `payload_ciphertext` or
> `secret_public_key`, both required by `ValidateBasic` since the key-share
> architecture change
> ([done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](done/DONE_KEY_SHARE_ARCHITECTURE_PLAN.md))
> — the command fails on every invocation and nothing noticed, because the
> e2e creator flow runs through the TypeScript SDK and guardians act via
> `guardiand` over gRPC. This is precisely the failure mode the query
> surface already engineered away (CLAUDE.md "CLI/Query Parity"); this plan
> closes the tx half. Enabled by the AutoCLI-capable sdk line (see
> [PENDING_AUTOCLI_PIN_REMOVAL_PLAN.md](PENDING_AUTOCLI_PIN_REMOVAL_PLAN.md),
> which retires the carried pin — independent of this plan, no ordering
> constraint).
>
> **Components**:
> - `x/secrets/module/autocli.go` — new `Tx` service-command descriptor
> - `x/secrets/client/cli/tx.go` — five commands deleted; `distribute-shares`
>   repaired; `request-guardians`, `register-guardian`, `update-guardian`
>   retained
> - `x/secrets/module/module.go` — `GetTxCmd` doc comment
> - `cmd/timeflared/cmd/root_test.go` — new tx-parity descriptor-walk test
> - `devnet/e2e-scenarios.sh` — command-shape audit (expected clear)
> - `docs/guides/TESTING_COMMANDS.md` — tx examples updated
> - `docs/guides/FAQ.md` — stale `tx guardians` command examples corrected
> - `docs/architecture.md` — stale tx CLI examples corrected in passing
> - `CLAUDE.md` — "CLI/Query Parity" section extended to cover tx
> - No proto changes; no `docs/spec.md` change (the CLI is not protocol
>   surface — command semantics, message validation and state transitions
>   are untouched)

## 1. Problem

The query CLI is generated from the gRPC service descriptor
(`x/secrets/module/autocli.go`), so a query RPC without a CLI verb is
impossible by construction. The tx CLI is 662 hand-written lines in
`x/secrets/client/cli/tx.go` with no equivalent guarantee: when a message
gains, loses or changes a field, nothing forces the command to follow. The
guarantee gap is not hypothetical — `distribute-shares` silently broke when
`MsgDistributeShares` gained two required fields, and stale in-file examples
(`accept-assignment`, a command that has never existed under that name)
show the same rot at documentation level.

Hand-written code earns its keep only where it does something a generated
command cannot. Audit of the nine commands:

| Message | Client-side logic beyond field mapping | Verdict |
|---|---|---|
| `MsgConfirmShares` | none | generate |
| `MsgRevealShare` | hex-decode one bytes arg | generate |
| `MsgCancelSecret` | none | generate |
| `MsgWithdrawStake` | none (positional address is redundant — must equal signer) | generate |
| `MsgSlashGuardian` | hex-decode one bytes arg | generate |
| `MsgRegisterGuardian` | hex-decode key; optional trailing bool | keep hand-written — Coin field (see §2.1 findings) |
| `MsgUpdateGuardian` | presence-aware `google.protobuf.BoolValue` flag | keep hand-written — Coin field (the BoolValue itself generates cleanly; see §2.1 findings) |
| `MsgRequestGuardians` | `--random-hint` generation, 40-byte hint split into `DetectionHint{version, R, tag}`, timing defaults | keep hand-written |
| `MsgDistributeShares` | shares-file JSON parsing + hex decode | keep hand-written, **repair** |

The bytes-argument concern dissolves on inspection: client/v2's binary flag
value accepts a file path, hex, or base64 (`autocli/flag/binary.go`), so
existing hex-encoded invocations keep working unchanged.

## 2. Design

### 2.1 Generated tx commands

`AutoCLIOptions()` gains a `Tx` descriptor alongside the existing `Query`
one:

```go
Tx: &autocliv1.ServiceCommandDescriptor{
    Service:              "timeflare.secrets.v1.Msg",
    EnhanceCustomCommand: true, // merge generated commands into the module's custom GetTxCmd tree
    RpcCommandOptions: []*autocliv1.RpcCommandOptions{
        // seven generated commands, each with Use/Short/PositionalArgs …
        // plus Skip entries for the hand-written survivors:
        {RpcMethod: "RequestGuardians", Skip: true},  // hand-written: hint tooling + defaults (client/cli/tx.go)
        {RpcMethod: "DistributeShares", Skip: true},  // hand-written: shares-file parsing (client/cli/tx.go)
    },
},
```

`EnhanceCustomCommand: true` is what lets the generated commands coexist
with the retained hand-written ones: autocli adds its commands into the
existing `tx secrets` tree rather than skipping the module because a custom
`GetTxCmd` exists. Every `Skip` entry carries a comment naming the
hand-written implementation, so the descriptor remains the single inventory
of the tx surface.

Signer fields (`cosmos.msg.v1.signer`) are auto-populated from `--from` and
never appear as arguments — the generated commands cannot express the
"positional address that must equal the signer" redundancy two current
commands carry.

Flag-rendering findings (verified July 2026 against
`cosmossdk.io/client/v2@v2.0.0-beta.8`, the version in the module graph):

- **`google.protobuf.BoolValue`** falls through to the JSON-message flag
  type, and protojson maps wrapper types to their primitive JSON form — so
  `--accepting-secrets true|false` parses directly, and an omitted flag
  leaves the field `nil` (the binder skips invalid values, preserving the
  tri-state "no change" semantics exactly; verified in a generated tx).
- **`Coin` / `*Coin` fields are blocked.** The dedicated coin flag type
  returns a `cosmossdk.io/api` (pulsar) `Coin`, but for a module with no
  pulsar-generated types autocli builds the message with `dynamicpb` over
  the gogo-resolved descriptor, whose `Coin` field descriptor is a
  different instance — `dynamicpb` rejects the value with a
  `"field descriptor does not belong to this message"` panic on every
  invocation that sets the flag or positional. Verified against the
  in-graph `v2.0.0-beta.8` and unchanged through `v2.0.0-beta.11` (the
  v2.10/v2.11 lines target sdk v2 and are not applicable). Consequence:
  `MsgRegisterGuardian` and `MsgUpdateGuardian` stay hand-written; the exit
  condition is a client/v2 release whose coin flag binds against the
  field's own descriptor.
- **Scalar defaults** are settable per-field via
  `FlagOptions.DefaultValue` (applied with `flagSet.Set` at build time).
- **Bytes** fields accept a file path, hex, or base64
  (`autocli/flag/binary.go`), so existing hex invocations are unchanged.

### 2.2 Command surface (before → after)

| Command | Before | After |
|---|---|---|
| `confirm-shares` | `[secret-id] [accept]` | unchanged |
| `reveal-share` | `[secret-id] [decrypted-share]` (hex) | unchanged (hex/base64/file all accepted) |
| `cancel-secret` | `[secret-id]` | unchanged |
| `slash-guardian` | `[guardian-address] [evidence] [reason] [secret-id]` | unchanged (positional order preserved in the descriptor) |
| `withdraw-stake` | `[guardian-address]` (redundant) | **no arguments** — signer from `--from` |
| `register-guardian` | `[guardian-address] [key] [from] [until] [deposit] [accepting?]` | unchanged — hand-written (Coin field, §2.1) |
| `update-guardian` | `[guardian-address]` + flags | unchanged — hand-written (Coin field, §2.1) |
| `request-guardians` | hand-written | unchanged |
| `distribute-shares` | hand-written, **broken** | repaired (§2.3) |

The `withdraw-stake` argument-shape change is breaking for scripts. Ruled
acceptable: pre-launch breaking changes are permitted, no shipped tooling
depends on the old shape (the consumer audit in §3 found none), and the
redundant-signer form is strictly worse — it invites the address/`--from`
mismatch error the generated form makes unrepresentable.

Behavioural note: generated commands do not run `ValidateBasic` client-side;
malformed messages are rejected at CheckTx instead of before signing. Same
outcome, marginally later error — accepted.

### 2.3 Repairing `distribute-shares`

The retained hand-written command gains the two missing message fields as
positional arguments:

```
distribute-shares [secret-id] [shares-file] [secret-commitment] [payload-ciphertext] [secret-public-key]
```

`payload-ciphertext` accepts a file path or hex (mirroring the client/v2
binary-value convention, since `C` at up to 4,216 bytes is unwieldy as a hex
argument); `secret-public-key` is 32 bytes hex. The shares-file JSON schema
is unchanged. A CLI-level test pins that the constructed message passes
`ValidateBasic` — the exact gap that let the breakage land invisibly.

### 2.4 Drift prevention — the point of the exercise

Two mechanisms, one existing and one new:

1. **`TestRootCmdSmoke`** (existing) already builds the full root command
   through `EnhanceRootCommand`; a descriptor the client/v2 flag builder
   cannot resolve (e.g. a positional naming a removed proto field) panics
   on every invocation and therefore fails this test. Adding the `Tx`
   descriptor brings the five generated commands under that protection
   automatically.
2. **Tx parity test** (new, alongside `TestRootCmdSmoke`): walks the
   `timeflare.secrets.v1.Msg` service descriptor and asserts every RPC is
   either resolvable as a subcommand under `tx secrets` or explicitly
   listed as `Skip` in `AutoCLIOptions()` with a corresponding hand-written
   command present in the tree. A new Msg RPC then fails the build until it
   is deliberately placed on one side or the other — the tx analogue of the
   query-parity guarantee. The `Skip`-must-have-a-command direction also
   catches a hand-written command being deleted without its descriptor
   entry.

CLAUDE.md's "CLI/Query Parity" section is retitled/extended to state the tx
rule: *tx commands are generated from the Msg service descriptor; a message
may opt out only via an explicit `Skip` entry pointing at a hand-written
command, and the parity test enforces both directions.*

## 3. Consumer audit (blast radius)

Grep-driven, July 2026 — per the cross-component sweep rule:

- **`guardiand`** — constructs and broadcasts messages over gRPC
  (`guardian/blockchain/signer.go`); no CLI dependency. **Clear.**
- **TypeScript SDK / e2e lifecycle** — creator flow via SDK, not CLI.
  **Clear.**
- **`devnet/e2e-scenarios.sh`** — uses `request-guardians --random-hint`
  (hand-written, unchanged), `cancel-secret` and `slash-guardian` (shapes
  preserved). **Clear; re-verified by running the suite in Phase 5.**
- **`docs/guides/TESTING_COMMANDS.md`** — seven tx-command examples;
  `register-guardian`/`withdraw-stake` examples change shape. **Updated by
  this plan.**
- **`docs/architecture.md`** — tx examples predate even the current
  commands; corrected to the post-migration shapes in passing. **Updated by
  this plan.**
- **`docs/guides/FAQ.md`** — seven examples still routed through a
  nonexistent `tx guardians` module with pre-bonded-economics argument
  shapes; corrected to the current commands (surfaced by the execution-time
  grep audit). **Updated by this plan.**
- **`docs/operations.md`** — describes messages, not CLI invocations; its
  broader staleness (pre-bonded-economics content, July 2026 review) is an
  unrelated concern and explicitly **not** absorbed here.

## 4. What this plan does not solve

- **`operations.md` staleness.** The file contradicts spec.md on guardian
  economics, slashing amounts and message fields. Separate doc-sync
  concern; needs its own plan.
- **Query CLI** — already generated; untouched.
- **A machine-readable seal artefact** for the creator flow (one JSON
  emitted by the SDK's seal step consumed whole by `distribute-shares`).
  Attractive, but it is TypeScript SDK surface design and belongs with
  [PRIORITY_PENDING_TS_CLIENT_CONSOLIDATION_PLAN.md](PRIORITY_PENDING_TS_CLIENT_CONSOLIDATION_PLAN.md);
  the §2.3 repair keeps today's file format.
- **Protocol behaviour** — no message, validation or state-machine change
  of any kind.

## 5. Implementation phases

Executed in a worktree forked from `main` (branch `tx-autocli`).

- **Phase 1 — descriptor + pruning**: add the `Tx` service-command
  descriptor (five generated commands, `Skip` entries per §2.1); delete the
  migrated command constructors from `client/cli/tx.go`; update the
  `GetTxCmd`/`autocli.go` doc comments to describe the split.
- **Phase 2 — `distribute-shares` repair**: add the payload-ciphertext and
  secret-public-key arguments (§2.3) with a `ValidateBasic`-level test;
  fix the stale `accept-assignment` example text while in the file.
- **Phase 3 — parity test**: descriptor-walk test per §2.4, both
  directions.
- **Phase 4 — docs sweep**: `TESTING_COMMANDS.md`, `architecture.md` tx
  examples, CLAUDE.md parity section. Grep-confirm every §3 component is
  clear or updated.
- **Phase 5 — verification**: `make verify && make test`; on a fresh devnet
  `make e2e` and `make e2e-scenarios` (exercises `request-guardians`,
  `cancel-secret`, `slash-guardian` through the real CLI); manual smoke of
  each migrated command against the devnet, including a hex-encoded bytes
  argument (`reveal-share`) and the repaired `distribute-shares`.
