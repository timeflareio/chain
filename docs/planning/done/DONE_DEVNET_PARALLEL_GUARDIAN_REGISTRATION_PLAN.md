# Devnet Parallel Guardian Registration — Plan

*Replace the serial fund-sleep-register-sleep loop in `devnet/guardians.sh` with
one batch funding transaction followed by fully parallel registrations, taking
guardian bring-up from ~2 blocks per guardian to ~2–3 blocks total.*

> **Status: IMPLEMENTED (July 2026, devnet-and-cleanups branch).** Devnet
> tooling only — no chain, proto, or spec.md changes. Registration now runs as
> prepare (local) → one batch multi-send funding tx (poll-confirmed, no sleeps)
> → parallel registrations confirmed via on-chain polling. This document
> remains as the rationale and decision log.

## 1. Root cause (verified, July 2026)

The current flow (`devnet/guardians.sh`, `register_one`) is serial with two
6-second sleeps per guardian. The reasons, established from the code:

1. **Funding — the real constraint.** Every guardian is funded from the shared
   `guardian-incentives` account via a plain `timeflared tx bank send`
   (`guardians.sh:82`). Each CLI invocation reads the account's sequence number
   from **last committed state**, signs, and broadcasts in sync mode (returns
   after CheckTx, before inclusion). Parallel invocations all read the same
   sequence `S` and all sign `S` — the first into the mempool wins, the rest are
   rejected with `account sequence mismatch, expected S+1`. This also fails
   *serially* without a block-wait, hence the first sleep. The chain itself is
   not the limit: a block happily carries `S, S+1, S+2` from one sender — the
   naive one-shot CLI simply cannot produce consecutive sequences.
2. **Account existence.** An address that has never received funds does not
   exist on chain, so `guardiand register` would fail its account lookup if it
   fired before the funding committed. Registration must be ordered after
   funding confirmation — but only after that one event.
3. **Registrations are already parallel-safe.** Each `guardiand register` signs
   from the guardian's *own* account (`guardian/cmd/guardiand/cmd/register.go`,
   `--from config.KeyName`); there is no shared sequence and no chain-side
   contention in the registration handler. The second sleep's comment
   ("separate consecutive registrations into different blocks") misdiagnoses its
   own purpose — what it actually protects is the *next* guardian's funding tx
   from racing the previous one's commit.

## 2. Design

Restructure `cmd_register` into three phases:

1. **Prepare (parallel, local).** Create all keyrings, keys, and `guardiand`
   configs — no chain interaction. Collect the N addresses.
2. **Fund (one transaction).**
   `timeflared tx bank multi-send guardian-incentives <addr1> … <addrN> $GUARDIAN_FUNDING`
   — one sequence number, one block, funds every guardian at once. Poll
   `timeflared query tx <hash>` for commitment instead of sleeping (bounded
   retries, then fail loudly). Skip already-funded addresses as today; if *all*
   are funded, skip the tx entirely.
3. **Register (parallel).** Fire all `guardiand register` calls concurrently
   (shell background jobs). Confirm each via the existing
   `is_guardian_registered` poll rather than a sleep. Append to
   `registry.conf` only after confirmation, serialised (collect results and
   write once at the end — simpler than `flock` and sufficient here).

Idempotency is preserved: every phase skips work already done (existing key,
sufficient balance, already registered), so re-running `register <count>` after
a partial failure completes the remainder, exactly as today.

## 3. Alternatives rejected

- **Explicit `--sequence $((S+i))` per send.** Works, but brittle: one rejected
  tx mid-batch strands every later sequence, and error recovery becomes manual.
- **SDK v0.53 unordered transactions (`--unordered`).** Removes sequence
  coupling entirely, but nothing in `app/` enables unordered-tx handling today —
  a chain-side change is overkill for a devnet script.
- **One funding account per guardian in genesis.** Trades a script fix for
  permanent genesis complexity; the pool model is fine once funding is one tx.

## 4. Surface area

- `devnet/guardians.sh` — `fund_from_pool` becomes batch-aware; `register_one`
  splits into prepare/register halves; `cmd_register` orchestrates the phases;
  both sleeps and the misleading comment go.
- `devnet/lib/common-utils.sh` — possibly a small `wait_for_tx <hash>` helper.
- Nothing else: `make dev-up`, `make e2e`, `make e2e-full` consume the same
  `register`/`start` interface unchanged.

## 5. Acceptance

- `make dev-reset` with `GUARDIAN_COUNT=8` brings up all guardians in a handful
  of blocks, with total registration wall-clock roughly independent of N.
- `make e2e` and `make e2e-scenarios` pass against the parallel-registered
  devnet.
- Re-running `register` is a no-op on a fully registered devnet and completes
  stragglers after a partial failure.
