# Validator Reward Routing & `community_tax = 0` — Plan

*Fixes a live defect: every fee the protocol "routes to validators" — the
90 % gas share and the 1,000 VEIL entry fees — is parked in the distribution
module's account with no reward bookkeeping, so validators can withdraw none
of it. Establishes the one validator payment pipe: **everything
validator-bound enters the fee collector and passes through the 90/10
split** — the chain's single fee-side deflationary lever — before the SDK's
own allocation credits it properly; and zeroes the inherited 2 %
`community_tax` in the same change so the newly working pathway cannot skim
into the accidental treasury the design stance forbids.*

> **Status: done — July 2026, merged in PR #108** (executed as phase 1 of
> the combined fee-economics branch, with the fee-floor and creation-fee
> plans as phases 2–3; all suites green including the new S6b withdraw
> drill). Ruled by the owner, July 2026 (defect verified on the live
> devnet, 2026-07-26). The ruling: **every validator-bound flow passes
> through the 90/10 split** — gas fees, guardian entry fees, and the coming
> creation fee alike; the split is the chain's one fee-side deflationary
> lever (slash burns and dust remain the scenario-dependent sinks). This
> amends the dynamic-bond ruling that made the entry fee the only fee with
> no burn component. No open questions.
> **Priority**: P1 — validator income is currently fictional; blocks
> [DONE_CREATION_FEE_PLAN.md](DONE_CREATION_FEE_PLAN.md) (whose fee
> must land in withdrawable rewards) and must be fixed before any testnet
> that claims validator economics.
> **Origin**: July 2026 audit finding while answering the `community_tax`
> question from [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md)
> (its §3 item 2 / Phase 1 fold in here); the routing defect itself was
> introduced across [DONE_FEE_BURN_PLAN.md](DONE_FEE_BURN_PLAN.md)
> (gas share) and
> [DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md)
> (entry-fee routing) — both asserted the *send*, neither asserted a
> *withdrawable reward*.
> **Components**: `x/secrets/keeper` (`fee_distribution.go`,
> `msg_server_register_guardian.go`, invariants);
> `devnet/chain/apply-genesis-economics.sh` (+ the mainnet genesis
> checklist) for `community_tax`; `docs/spec.md` (fee distribution, money
> map) + PROTOCOL.md (funds-flow table, Open Defects entry, §D) + the two
> done-plans' claims corrected; conformance/ledger tests;
> `devnet/e2e-scenarios.sh` (S6 extension + withdraw drill);
> TESTING_COMMANDS.md.

## 1. The defect

Verified on the live devnet (2026-07-26, ~2,700 fee-bearing blocks, 8
registered guardians):

- The distribution module account holds **8,003 VEIL** (8 × 1,000 VEIL entry
  fees + every block's 90 % gas share).
- **Validator outstanding rewards: zero.** Community pool: zero.
- The distribution module's own account invariant (balance = outstanding
  rewards + community pool) is silently violated by the full 8,003 VEIL.

Mechanism: x/distribution only credits validator rewards from what its
BeginBlocker finds in the **fee collector** (`AllocateTokens`, which
allocates by bonded voting power and does the bookkeeping that makes rewards
withdrawable). Our `ProcessFeeSplit` BeginBlock work is ordered *before* it
and **empties the fee collector**, sending the 90 % share — like the entry
fee at registration — via a bare `SendCoinsFromModuleToModule` into the
distribution module **account**. Coins in a module account with no
accounting entry are invisible to reward withdrawal: validators earn nothing
from any fee today. (The 10 % burn is unaffected — `BurnCoins` is real.)

Why the test suites missed it: the fee-split e2e (S6) asserts the
`fee_distribution` **event amounts**, and the unit suites assert the mock
bank **sends** — nothing ever asserted a validator's withdrawable balance.

## 2. The fix — route through the fee collector

1. **Gas share**: `ProcessFeeSplit` stops moving the validator share
   anywhere. It removes and burns only the 10 % share; the 90 % **stays in
   the fee collector**, and x/distribution's BeginBlocker — already ordered
   immediately after ours — allocates it the same block with full
   bookkeeping. (One send less than today; the split arithmetic and event
   are unchanged.)
2. **Entry fee**: registration sends the fee to the **fee collector**
   (not the distribution account). It then rides the next block's split like
   any other fee — **90 % allocated to validators, 10 % burned** (ruled July
   2026, amending the dynamic-bond plan's no-burn choice: per registration,
   900 VEIL reaches validators and 100 VEIL burns). One pipe, no exemption
   machinery, and registration finally contributes to the deflation lever.
3. **`community_tax = 0`**, set explicitly in genesis
   (`apply-genesis-economics.sh` now; the mainnet genesis checklist
   inherits it). This must land **in the same change**: the moment funds
   flow through `AllocateTokens` again, the inherited 2 % SDK default starts
   skimming every routed fee into the community pool — the accidental
   treasury the no-treasury stance forbids. Today the skim is moot only
   because the broken routing starves the allocator.
4. **Devnet state**: no migration — devnets reset; the 8,003 VEIL stranded
   on the current devnet dies with it. (Had this reached a persistent
   network, the parked balance would have needed an upgrade-time
   reconciliation; pre-launch it does not.)

## 3. What this plan does not solve

- The **level** of validator income (that is the creation fee's job —
  [DONE_CREATION_FEE_PLAN.md](DONE_CREATION_FEE_PLAN.md) — and the
  token-economics plan's fiat-corridor question). This plan makes the
  existing flows *real*, not larger.
- The **advisory gas floor** (a validator can still accept zero-fee
  transactions) — carved into
  [DONE_CONSENSUS_FEE_FLOOR_PLAN.md](DONE_CONSENSUS_FEE_FLOOR_PLAN.md).

## 4. Verification (the assertions the original plans lacked)

- **Unit/conformance**: after a fee-bearing block + BeginBlockers, validator
  outstanding rewards equal exactly the 90 % share (and the entry fee, on
  registration blocks); the distribution module account invariant holds;
  the community pool never exceeds allocation dust across every scenario.
- **e2e (S6 extension)**: outstanding rewards grow block-on-block with
  fees; a delegator `withdraw-rewards` drill actually pays out; the
  community pool's **growth across the run is asserted below one uveil** —
  not "exactly zero", because each reward withdrawal truncates decimal
  rewards to integers and parks the sub-uveil remainder in the pool by SDK
  design (found in execution, July 2026: idle and fee-bearing blocks
  deposit nothing; only withdrawals do). A 2 % skim would deposit whole
  VEIL per run, so the per-run dust bound is the real no-treasury
  assertion.
- **Docs** (the entry-fee re-ruling makes this a real sweep): every claim
  that the entry fee is "routed in full / 100 % to validators" or "does not
  pass through the 90/10 split" is updated to the split-inclusive ruling —
  spec.md (Guardian Registration, Guardian Parameters table, Fee
  Distribution, money map), PROTOCOL.md (§6 registration, §D, funds-flow
  table, Open Defects §2 → resolved), CLAUDE.md project overview,
  TESTING_COMMANDS.md, and correction notes in the two done-plans' decision
  logs.

## 5. Decision log (ruled by the owner, July 2026)

1. **Every validator-bound flow passes through the 90/10 split** — gas fees,
   guardian entry fees, and the creation fee to come. The split is the
   chain's one fee-side deflationary lever; no flow is exempt, so no
   exemption machinery exists. This amends the dynamic-bond plan's "entry
   fee has no burn component" choice: registration now contributes
   900 VEIL to validators and 100 VEIL to the burn. The exemption-counter
   alternative was considered and rejected as machinery in service of a
   distinction not worth keeping.
