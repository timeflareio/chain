# Creation Fee — Request-Time Draw Pricing & Validator Budget — Plan

*Adds the one structural fee the economics still lack: a non-refundable
`creation_fee = max(floor, pct(distance) × P)` charged at
`MsgUserRequestGuardians` and routed through the standard validator pipe
(the fee collector and its 90/10 split). The percentage is a linear curve
over the secret's distance — 10 % at the shortest secrets falling to 5 % at
30 days and beyond. One fee, two jobs: it prices every selection draw
(closing the abandon-and-refund grinding hole, PROTOCOL.md Security
Observations §1) and it is the recurring consensus security budget that
scales with the value validators actually secure.*

> **Status: done — July 2026, merged in PR #108** (phase 3 of the combined
> fee-economics branch; closes PROTOCOL.md Security Observations §1).
> All values ruled by the owner, July 2026. Carved out of
> [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) (§5.4(b),
> §9 Q1, Phase 2) so the grinding observation can close ahead of the wider
> token-economics work, exactly as that plan anticipated. The fee's mechanism
> was ruled July 2026: structure, non-refundability, request-time collection,
> a **distance-based linear percentage curve — 10 % at minimal distance
> falling to 5 % at 30 days, flat 5 % beyond** — and routing through the fee
> collector's 90/10 split like every validator-bound flow (the owner's
> one-pipe ruling — see
> [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md)),
> and the anti-grinding floor is **gas-denominated**: three times a
> reference transaction's gas cost at the consensus-enforced floor price
> (0.06 VEIL today). **No open questions.**
> **Priority**: P1 — closes the protocol's last open "needs fixing" security
> observation; pre-testnet (retunes are software upgrades only, Position A).
> **Origin**: PROTOCOL.md Security Observations §1 (selection grinding —
> the pending half);
> [DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md](DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md)
> (restructured the exit-time forfeit into this request-time fee);
> [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) §5.4
> (validator-budget analysis) with the calibration evidence in
> [ECONSIM_CALIBRATION_V1.md](../../reports/econsim-calibration-v1/ECONSIM_CALIBRATION_V1.md).
> **Components**: `x/secrets/types` (constants, `economics.go` derivation,
> fee-split tests); `x/secrets/keeper` (`msg_server_request_guardians.go`
> funding flow, reservation event); `docs/spec.md` (Secret Pricing, money
> map, validation constants) + `docs/operations.md` + PROTOCOL.md (§1
> closure, funds-flow table, hard-coded values); conformance +
> no-stranded-bonds suites; `devnet/e2e-scenarios.sh` (exact-amount
> assertions change); TESTING_COMMANDS.md; TS SDK cost documentation (no
> proto change — the fee is charged server-side from existing message
> fields; sweep the SDK/mobile client for any client-side cost estimator
> that must add the fee).

## 1. Why

Two problems, one fee:

1. **Selection grinding is half-fixed** (PROTOCOL.md Security Observations
   §1). Selection is uniform hash sortition the creator cannot bias — but a
   creator can *discard* draws almost free: submit `MsgUserRequestGuardians`,
   read the assignment, and simply never send Phase 2. The commit timeout
   refunds the pool in full (by design), so the cost per rejected draw is one
   gas fee plus a 20-block wait — repeatable in parallel until the attacker's
   Sybil guardians hold ≥ threshold of some draw. The cancel-during-commit
   bypass is already closed (`MsgUserCancelSecret` is `pending`-only, ruled
   July 2026); what remains is to **price the draw itself**. A non-refundable
   fee at request time does exactly that, while leaving every refund path
   untouched — the fee never enters escrow, so the full-refund-on-timeout
   design stays intact.
2. **Nothing pays validators proportionally to what they secure.** Validator
   income today is 90 % of gas (scales with transaction *count*) plus entry
   fees (front-loaded — they fade once guardian growth slows). Meanwhile
   consensus secures escrowed pools and guardian floats that scale with usage
   and duration. The creation fee scales with `P` — value and duration of the
   commitment being secured — and becomes the dominant recurring validator
   revenue as entry-fee income fades (the calibration report's no-fee row was
   measurably worse: validator count 1 vs 4).

## 2. Design

1. **Formula**: `creation_fee = max(floor, pct(distance) × P)`, where
   `P = rate × distance × max_shares × bump ÷ 100` is the reward pool already
   derived at request time and `pct(distance)` is a **linear curve ruled
   July 2026**: 10 % at minimal distance, falling to 5 % at 30 days, flat
   5 % beyond. In deterministic integer arithmetic (basis points, truncating
   division — the house fixed-point style):

   ```
   bps(d)       = CreationFeeMaxBps − ((CreationFeeMaxBps − CreationFeeMinBps)
                                        × min(d, CreationFeeCurveEndBlocks))
                                        ÷ CreationFeeCurveEndBlocks
   creation_fee = max(CreationFeeFloor, P × bps(d) ÷ 10,000)
   ```

   All knobs are hard constants in `x/secrets/types` (Position A — no
   governance parameters):
   - `CreationFeeMaxBps = 1,000` (10 %) and `CreationFeeMinBps = 500`
     (5 %) — **ruled July 2026**; every point on the curve sits inside the
     calibration sweep's healthy 5–10 band (the no-fee row was measurably
     worse). Because `P` grows linearly with distance while `bps` falls, the
     absolute fee is non-decreasing in distance up to integer-truncation
     dust (each 1-bps step of the truncating curve can dip the fee by at
     most `P ÷ 10,000` — found in implementation, July 2026) — no window
     shape meaningfully lowers the bill by inflating or deflating distance.
   - `CreationFeeCurveEndBlocks = 432,000` (30 days at 6-second blocks — the
     same guardian-day block base as every other duration constant).
   - `CreationFeeFloor = MinGasPriceUveil × CreationFeeFloorGas` with
     `CreationFeeFloorGas = 600,000` (three reference 200k-gas
     transactions) — **ruled July 2026**: 0.06 VEIL at the enforced
     0.1 uveil/gas floor. The floor is deliberately **gas-denominated**,
     not wage-denominated: its job is to make a discarded selection draw
     cost more than the gas that accompanies it, so "a draw costs ~3× its
     gas" holds by construction and tracks any future gas-floor retune
     automatically. (`MinGasPriceUveil` is established by
     [DONE_CONSENSUS_FEE_FLOOR_PLAN.md](DONE_CONSENSUS_FEE_FLOOR_PLAN.md)
     — a deliberate dependency: the two constants move together.) The
     curve does not replace the floor: at minimal distances even 10 % of
     `P` is far below one gas fee, so the flat floor is what prices a
     grinding draw.
2. **Charged at `MsgUserRequestGuardians`**, in the same funding flow that
   escrows `P`: the creator's Phase-1 debit becomes `P + creation_fee + gas`.
   Atomic with the rest of the handler — a failed request charges nothing
   (selection precedes the pool lock and the fee charge).
3. **Non-refundable on every exit path, by construction**: the fee never
   enters module escrow, so commit-timeout, zero-reveal refund, cancellation
   and settlement code paths are untouched — there is nothing to refund from.
   This is what makes it a draw price rather than a deposit.
4. **Routed through the standard validator pipe** (ruled July 2026): the
   fee enters the fee collector and rides the 90/10 split like every
   validator-bound flow — 90 % allocated to validators, 10 % burned — and
   never accumulates anywhere discretionary: it is a *price*, not a *fund*
   (the no-treasury stance). Routing mechanics: **blocked on the
   reward-routing plan** (§3) — the fee must land in validator *withdrawable
   rewards*, not in a module account's unaccounted balance.
5. **Observability**: the reservation event gains `creation_fee` (amount) and
   the regime that priced it (`floor` vs `percent`); the Phase-1 response is
   unchanged (the fee is derivable client-side from the message fields).

### What the creator pays (at `rate = 1 uveil` and the ruled values)

| Secret | Distance (blocks) | Curve rate | `P` | Creation fee | Regime |
|---|---|---|---|---|---|
| 1-day sealed bid (3g, bump 1) | 14,400 | 9.84 % | 0.0432 | **0.06** | floor |
| 7-day announcement (5g, bump 1) | 100,800 | 8.84 % | 0.504 | **0.06** | floor |
| 30-day dead-man's handle (5g, bump 2) | 432,000 | 5.00 % | 4.32 | 0.216 | curve |
| 70-day escrow (9g, bump 5) | 1,000,000 | 5.00 % (flat) | 45 | 2.25 | curve |
| 1-year max (32g, bump 10) | 5,256,000 | 5.00 % (flat) | ~1,682 | ~84 | curve |

(Of every fee, 90 % reaches validators and 10 % burns. The floor governs
small secrets up to roughly a two-week, low-bump shape — exactly the sizes
where a percentage alone would be no grinding deterrent.)

## 3. Dependency — validator reward routing

This plan lands **after**
[DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md),
which owns the routing defect (validator-bound fees parked unaccounted in
the distribution module account) and establishes the working pipe: fees
enter the **fee collector**, x/distribution's `AllocateTokens` credits them
the next block, `community_tax = 0`. The creation fee simply uses that pipe
— it must land in validator *withdrawable rewards*, and until the routing
fix lands there is no such destination.

## 4. What this plan does not solve

- **The fiat level of validator income** — whether ~16,200 VEIL/year at the
  §1 scenario pays for real infrastructure is entirely a token-price
  question (token-economics plan §9 Q5, the pricing corridor). This plan
  fixes the *shape* of validator revenue, not its fiat magnitude.
- **The consensus-enforced gas floor** (token-economics plan §9 Q2 /
  Phase 3) — complementary, separate concern.
- **Bulk distribution / genesis configuration** — remains with the
  token-economics plan.

## 5. Implementation

1. **Spec first**: spec.md Secret Pricing gains the fee (formula, worked
   invoice table, refundability-at-a-glance), the money map and Phase-1 flow
   updated; operations.md `MsgUserRequestGuardians` fields/validation;
   PROTOCOL.md §1 closes (observation → resolved), funds-flow and
   hard-coded-values tables gain the fee.
2. **Types**: `CreationFeeMaxBps`, `CreationFeeMinBps`,
   `CreationFeeCurveEndBlocks`, `CreationFeeFloorGas` constants (the floor
   derives as `MinGasPriceUveil × CreationFeeFloorGas`); a
   `CreationFee(p math.Int, distance uint64) math.Int` derivation in
   `economics.go` (parameterised core + constant-bound wrapper, matching
   the house pattern); derivation unit tests covering both regimes and the
   floor/curve crossover.
3. **Keeper**: charge in `UserRequestGuardians` after selection succeeds and
   alongside the pool lock (atomic abort on failure); route per §3; extend
   the reservation event.
4. **Tests**: conformance assertions at both `max()` regimes and the exact
   crossover; failed-Phase-1 charges nothing (selection failure, insufficient
   funds for `P + fee`); no-stranded-bonds and solvency suites extended with
   the fee flow; e2e scenario exact-amount assertions updated (every
   scenario's creator debit changes).
5. **Docs sweep**: TESTING_COMMANDS.md invoice examples; TS SDK / mobile
   client checked for client-side cost estimators that must add the fee.

## 6. Decision log (ruled by the owner, July 2026)

1. **Percentage curve**: linear, 10 % at minimal distance → 5 % at 30 days
   (432,000 blocks), flat 5 % beyond. Guardian count and bump play no part
   in the rate — distance is the only input.
2. **Floor**: gas-denominated — `MinGasPriceUveil × 600,000` (three
   reference 200k-gas transactions ≈ 0.06 VEIL), NOT wage-denominated: the
   deterrence property "a discarded draw costs ~3× its gas" holds by
   construction. A percentage alone was shown unable to do this job — at
   the minimal shape it would need ~133 % of `P` to reach the same price.
3. **Routing**: through the one validator pipe (fee collector, 90/10
   split) like every validator-bound flow.
