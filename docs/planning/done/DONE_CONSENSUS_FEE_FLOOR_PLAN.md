# Consensus-Enforced Fee Floor — Plan

*Turns the minimum gas price from per-node etiquette into protocol law. In
stock Cosmos SDK, `minimum-gas-prices` is each operator's `app.toml` setting,
checked only when that node admits a transaction to its own mempool — block
execution skips the check entirely, so any validator can include zero-fee
transactions. Since the 10 % fee burn is live, that means both validator gas
revenue and the protocol's guaranteed deflation sink can be competed to
zero. This plan enforces the floor in the ante handler for every transaction,
in CheckTx AND DeliverTx, as a compile-time constant — consensus state with
zero governance parameters, per Position A.*

> **Status: done — July 2026, merged in PR #108** (phase 2 of the combined
> fee-economics branch).
> Value ruled by the owner, July 2026 (0.1 uveil,
> bundled with the creation-fee floor ruling: that floor is derived as
> `MinGasPriceUveil × 600,000`, so the two constants were ruled together —
> see [DONE_CREATION_FEE_PLAN.md](DONE_CREATION_FEE_PLAN.md)).
> Carved out of
> [REJECTED_TOKEN_ECONOMICS_PLAN.md](REJECTED_TOKEN_ECONOMICS_PLAN.md) (§5.7,
> §9 Q2, Phase 3). No open questions.
> **Priority**: P2 — structural integrity of the fee economics; wanted
> pre-testnet (retunes are software upgrades only), but nothing live is
> being exploited on a devnet we run ourselves.
> **Origin**: token-economics plan §5.7 analysis and its §3 audit item 3
> ("the fee floor is advisory"); calibration evidence in
> [ECONSIM_CALIBRATION_V1.md](../../reports/econsim-calibration-v1/ECONSIM_CALIBRATION_V1.md)
> (gas-fee sweep: health flat across the range — the value is not
> load-bearing; the structure is).
> **Components**: `app/` (the chain's **first explicitly-assembled ante
> chain** — see §2 item 5; today the app has no custom ante wiring at all);
> `x/secrets/types/constants.go` for the floor constants (placement ruled —
> decision log 2); `devnet/chain/`
> (keep node config aligned with the constant); `docs/spec.md`
> (Configuration Parameters — "Minimum Gas Price" becomes consensus-enforced)
> + PROTOCOL.md (§2.4 node-config caveat closes, hard-coded values table);
> ante unit tests + conformance; TESTING_COMMANDS.md.

## 1. Why

Three properties currently rest on nothing but every validator choosing the
same `app.toml` value:

1. **The 10 % burn** — the protocol's guaranteed usage-proportional
   deflation — is only as real as the fees actually charged. A validator
   accepting zero-fee transactions burns nothing on them.
2. **Validator gas revenue** (90 %) can be competed toward zero by
   fee-undercutting validators courting transaction flow.
3. **Spam cost** — filling blocks must stay expensive chain-wide
   (~43,000 VEIL/day at the 0.1 uveil floor), not merely on well-configured
   nodes.

How other chains close this: convention only (fragile), the Cosmos Hub's
`x/globalfee` (a *governance parameter* — off the table under Position A),
or EIP-1559-style dynamic fee markets (`x/feemarket` — machinery and a
moving price this protocol's fixed-price philosophy does not want). The fit
for Timeflare is the simplest of all: a **hard-coded floor checked in the
ante chain for every transaction in every mode** — the same
immutable-constant discipline as every other economic value, retuned only by
coordinated upgrade (a `rate` retune should revisit the floor in the same
upgrade — token-economics plan §5.7).

## 2. Design

1. **Constant**: `MinGasPriceUveil = 0.1 uveil/gas`, represented as an
   integer numerator/denominator pair (`MinGasPriceUveilNum = 1`,
   `MinGasPriceUveilDen = 10`) — no `Dec` types in the consensus path, no
   `Params` state, no genesis value, no governance.
2. **Enforcement**: an ante decorator in the app's ante chain that computes
   `required = ⌈gas_limit × MinGasPriceUveilNum ÷ MinGasPriceUveilDen⌉`
   (**ceiling division** — rounding in the protocol's favour, consistent
   with the house integer-arithmetic style) and rejects any transaction
   whose fee is below it — in **both** CheckTx and DeliverTx (the stock
   `DeductFeeDecorator` checks node config in CheckTx only; the new check is
   mode-independent), with exactly two mode exemptions (item 4). A block
   proposer including an under-priced transaction sees it fail like any
   other invalid transaction; there is no fee revenue in doing so, so the
   incentive to try is nil.
3. **Node config stays**: `minimum-gas-prices` in `app.toml` remains the
   mempool-admission knob (operators may set it *higher*); the constant is
   the chain-wide floor beneath it. Devnet setup keeps the two aligned so
   local behaviour matches protocol behaviour.
4. **Fee-less execution modes — exactly two, both exempted**: genesis and
   simulation. Gentxs **do** execute through the ante chain at chain
   initialisation and carry zero fee (`devnet/chain/setup-chain.sh` creates
   the validator gentx with no fee flag — the standard pattern), so without
   a genesis exemption the chain cannot initialise; simulate-mode gas
   estimation likewise runs fee-less. The decorator skips when the block
   height is 0 and when running in simulate mode — standard practice on
   chains enforcing fees at execution. Neither weakens the floor: no real
   transaction executes in either mode. No other fee-less message class or
   path exists.
5. **This is the app's first custom ante wiring** — and it **wraps** rather
   than reassembles. `app/` previously had no ante customisation; the chain
   runs the stock runtime-assembled decorator chain. The floor is added by
   wrapping that handler after build (`BaseApp.AnteHandler()` exposes what
   the tx module installed; `SetAnteHandler` replaces it with the wrapped
   version): the floor check runs first, then delegates to the untouched
   stock chain. Wrapping makes dropping a stock decorator — signature
   verification, sequence increment, fee deduction — impossible by
   construction, which is strictly stronger than reproducing the decorator
   list and verifying it by inspection. The full e2e suites remain the
   behavioural check that nothing else changed.

## 3. What this plan does not solve

- The **fiat adequacy** of 0.1 uveil (token-economics plan §9 Q5) — the
  calibration sweep showed the value is not load-bearing; this plan ships
  structure, not pricing.
- **Validator income level** — that is
  [DONE_CREATION_FEE_PLAN.md](DONE_CREATION_FEE_PLAN.md); this plan
  only stops what income exists being competed away.
- Fee-collector allocation correctness —
  [DONE_VALIDATOR_REWARD_ROUTING_PLAN.md](DONE_VALIDATOR_REWARD_ROUTING_PLAN.md).

## 4. Decision log (ruled July 2026)

1. **The value**: enforce at the current **0.1 uveil**. The arithmetic that
   binds: at `rate = 1`, one 200k-gas transaction (~0.02 VEIL) is already
   ~46 % of the smallest secret and ~83 % of its per-guardian wage — no
   headroom to raise the value; the calibration sweep showed it is not
   load-bearing. The creation-fee floor derives from this constant
   (`MinGasPriceUveil × 600,000`), so any future retune moves both together
   in one upgrade.
2. **Constant placement**: `x/secrets/types/constants.go`, keeping every
   economic constant in one audited file (settled on the standing
   recommendation; `make verify-boundaries` re-checked at execution — the
   app's ante decorator importing the types module is an allowed edge).
3. **Representation**: integer numerator/denominator pair with ceiling
   division — no `Dec` in the consensus path, rounding in the protocol's
   favour (ruled July 2026).
4. **Mode exemptions**: genesis (block height 0) and simulate only — forced
   by gentxs executing fee-less through the ante chain at initialisation;
   confirmed against the codebase that no other fee-less path exists
   (ruled July 2026).
