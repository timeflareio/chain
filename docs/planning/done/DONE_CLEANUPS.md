# Pending Cleanups — Plan

*Small, independent code hygiene items with no behavioural impact. Each is safe to
land pre-launch in any order; batch them into whichever PR next touches the
relevant file.*

> **Status: DONE (July 2026).** Collected from the July 2026 PROTOCOL.md
> refresh audit; all four items have landed. None changed protocol behaviour.
>
> 1. ✅ `NextSecretId` deleted (devnet-and-cleanups branch)
> 2. ✅ `MinRevealBuffer` deleted, PROTOCOL.md flag removed (same branch)
> 3. ✅ `secret_commitment` proto comment corrected (landed with the key-share
>    architecture, PR #37)
> 4. ✅ Superseded sleeps and comment removed by
>    [DONE_DEVNET_PARALLEL_GUARDIAN_REGISTRATION_PLAN.md](DONE_DEVNET_PARALLEL_GUARDIAN_REGISTRATION_PLAN.md)
>    (same branch)

## 1. Delete the `NextSecretId` sequence (vestigial state)

- **What**: `collections.Sequence` in `x/secrets/keeper/keeper.go:37,81`.
- **Evidence**: written only at genesis import (`genesis.go:66,71`), **never
  read** anywhere — secret IDs are client-supplied or chain-generated UUIDs
  (`SecretIdLength = 36`, `isValidUUID`), not sequence-derived. Its origin/purpose
  is unknown even to the project; nothing depends on it.
- **Also**: its genesis restore sets `len(secrets) + 1` (element count, not max
  ID) — a latent count-vs-max bug that becomes moot on deletion; noted so a
  "keep and document" decision doesn't silently retain it.
- **Action**: remove the field, its `NewSequence` wiring, the `NextSecretIdKey`
  prefix, and both genesis writes. Pre-launch — no migration.

## 2. Delete the `MinRevealBuffer` dead constant

- **What**: `MinRevealBuffer = 10` (`x/secrets/types/constants.go:178`,
  "minimum blocks for guardian preparation"). Declared, referenced nowhere in
  `x/` or `app/`.
- **Why it is safe** (ruling, July 2026): its intended job — guaranteeing
  guardians a schedulable reveal window — is already covered by two live,
  validated bounds: `MinRevealDuration = 100` blocks (~10 min floor on window
  size, `validateRevealWindow`) and `MinRevealStartOffset = 50` (~5 min gap
  between commit deadline and earliest reveal). The constant is an earlier draft
  of the same protection, superseded — not an unimplemented feature.
- **Action**: delete the constant and its PROTOCOL.md dead-constant flag.

## 3. Fix the stale `secret_commitment` proto comment

- **What**: `proto/timeflare/secrets/v1/tx.proto:90` says
  `// SHA256(original_secret)`; the commitment is actually SHA256 of the
  **recipient-encrypted payload** (spec.md creator flow; deliberately not
  plaintext-binding — see DONE_KEY_SHARE_ARCHITECTURE_PLAN.md §8).
- **Action**: correct the comment next time `tx.proto` is edited (already noted
  in [DONE_KEY_SHARE_ARCHITECTURE_PLAN.md](DONE_KEY_SHARE_ARCHITECTURE_PLAN.md) §7 — do it
  in whichever lands first, then strike it from the other).

## 4. Fix the misleading sleep comment in `devnet/guardians.sh`

- **What**: `guardians.sh:141` — "separate consecutive registrations into
  different blocks". The sleep actually protects the *next guardian's funding
  transaction* from an account-sequence race on the shared pool account;
  registrations themselves are parallel-safe (distinct signers).
- **Action**: superseded entirely by
  [DONE_DEVNET_PARALLEL_GUARDIAN_REGISTRATION_PLAN.md](DONE_DEVNET_PARALLEL_GUARDIAN_REGISTRATION_PLAN.md),
  which removes both sleeps; if that plan is deferred, fix the comment alone.
