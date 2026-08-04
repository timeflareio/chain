package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// Derived economic quantities for the bonded guardian model.
//
// ⚠️ These values are DERIVED from the base constants in constants.go and must
// never be hard-coded anywhere: change RatePerGuardianBlock (or the k-curve
// constants) and every bond, reward pool, and candidacy check cascades
// automatically. See docs/spec.md "Secret Economics & Slashing".

// ValidateBump checks a wire-format security factor (hundredths, 2 d.p.).
func ValidateBump(bump int64) error {
	if bump < MinBump || bump > MaxBump {
		return fmt.Errorf("bump must be between %d and %d hundredths (%.2f–%.2f), got %d",
			MinBump, MaxBump, float64(MinBump)/float64(BumpScale), float64(MaxBump)/float64(BumpScale), bump)
	}
	return nil
}

// BondAmountWith is the parameterised core of BondAmount; op order matches
// the wrapper exactly (all multiplications, then a single truncating division
// by both fixed-point scales) so swept runs reproduce on-chain truncation bit
// for bit. bump and k are wire-format hundredths.
func BondAmountWith(rate, distance, bump, k int64) math.Int {
	return math.NewInt(rate).
		Mul(math.NewInt(distance)).
		Mul(math.NewInt(bump)).
		Mul(math.NewInt(k)).
		Quo(math.NewInt(BumpScale * BumpScale))
}

// BondAmount returns the per-secret bond a guardian must lock to accept a
// secret, anchored to the secret's own duration and priced by the guardian's
// live bond multiplier k (both bump and k in wire-format hundredths):
//
//	B = rate × distance × bump × k
//
// A guardian's bond is exactly k× its own reward share (P ÷ num_guardians =
// rate × distance × bump), making the collusion-cost-to-reward ratio the
// duration-independent constant threshold × k. See docs/spec.md "The
// Per-Guardian Bond Multiplier k".
func BondAmount(distance, bump, k int64) math.Int {
	return BondAmountWith(RatePerGuardianBlock, distance, bump, k)
}

// GuardianBondAmount returns the bond frozen at selection for the given
// selected guardian (GuardianBondAmounts is aligned index-for-index with
// SelectedGuardians). The bool is false for an address that was never
// selected, or if the record carries no frozen bond for it — consensus-path
// callers treat that as a state-integrity assertion, never a zero bond.
func (s *Secret) GuardianBondAmount(guardianAddress string) (math.Int, bool) {
	for i, addr := range s.SelectedGuardians {
		if addr == guardianAddress {
			if i < len(s.GuardianBondAmounts) {
				return math.NewInt(s.GuardianBondAmounts[i]), true
			}
			return math.ZeroInt(), false
		}
	}
	return math.ZeroInt(), false
}

// ClampBondK forces a bond multiplier into the valid [MinBondK, MaxBondK]
// range. Guardian records always store a valid k (set at registration), so
// this is defensive normalisation for zero values from older or imported
// state — an unset k reads as the floor, never as a free pass below it.
func ClampBondK(k int64) int64 {
	if k < MinBondK {
		return MinBondK
	}
	if k > MaxBondK {
		return MaxBondK
	}
	return k
}

// NextBondKAfterSlash returns a guardian's bond multiplier after a slash
// event (no-reveal or early-reveal, either one):
//
//	k′ = min(MaxBondK, k × 126 ÷ 100)    (truncating integer division)
//
// The clamp on the rising side must be the CEILING (min): eight consecutive
// slashes climb the full range 4.00 → 24.00. Clamping the wrong way would
// snap k to the ceiling on the first slash — see the plan's §2.2 warning.
func NextBondKAfterSlash(k int64) int64 {
	return ClampBondK(ClampBondK(k) * KSlashMulNum / KSlashMulDen)
}

// NextBondKAfterReveal returns a guardian's bond multiplier after a correct
// on-chain reveal:
//
//	k′ = max(MinBondK, k × 963 ÷ 1000)    (truncating integer division)
//
// The clamp on the falling side must be the FLOOR (max). Recovery is
// deliberately ~6× slower than the climb: one slash step takes about six
// reveal steps to unwind; full recovery from the ceiling takes ~48 reveals.
func NextBondKAfterReveal(k int64) int64 {
	return ClampBondK(ClampBondK(k) * KRevealMulNum / KRevealMulDen)
}

// AcceptLeg returns one guardian's acceptance reimbursement: the accept
// transaction's gas at the consensus floor price. Not scaled by bump — gas
// does not get more expensive because the creator chose more security.
func AcceptLeg() math.Int {
	return MinRequiredFee(GuardianAcceptGas)
}

// RevealLeg returns one guardian's reveal reimbursement, on the same basis as
// AcceptLeg. It rides inside the reward pool rather than accept_fees: it is
// earned by seeing the secret through, alongside the time component.
func RevealLeg() math.Int {
	return MinRequiredFee(GuardianRevealGas)
}

// AcceptFeesAmount returns the acceptance reimbursement the creator escrows
// alongside — and separately from — the reward pool:
//
//	A = max_shares × F_accept
//
// Held apart from P because the two are earned by different acts and settle
// on different rules: A is owed for accepting, P for seeing the job through.
// Divides exactly by max_shares by construction, so every terminal-state
// payout is a whole number of uveil derived from stored state alone.
func AcceptFeesAmount(maxShares int64) math.Int {
	return AcceptLeg().Mul(math.NewInt(maxShares))
}

// PerGuardianAcceptFee returns one guardian's slice of a secret's STORED
// accept_fees — never the live constant, so a gas retune cannot re-price a
// secret already in flight (the immutable-economics ruling, as for
// ProRataCancellationPayout).
func PerGuardianAcceptFee(acceptFees math.Int, maxShares int64) math.Int {
	if maxShares <= 0 {
		return math.ZeroInt()
	}
	return acceptFees.Quo(math.NewInt(maxShares))
}

// TimeComponentAmount returns the duration half of the reward pool — the wage
// for holding a share, and the base the creation-fee curve prices:
//
//	P_time = rate × distance × max_shares × bump
func TimeComponentAmount(distance, maxShares, bump int64) math.Int {
	return TimeComponentAmountWith(RatePerGuardianBlock, distance, maxShares, bump)
}

// TimeComponentAmountWith is the parameterised core of TimeComponentAmount;
// op order matches the wrapper exactly (single truncating division last).
func TimeComponentAmountWith(rate, distance, maxShares, bump int64) math.Int {
	return math.NewInt(rate).
		Mul(math.NewInt(distance)).
		Mul(math.NewInt(maxShares)).
		Mul(math.NewInt(bump)).
		Quo(math.NewInt(BumpScale))
}

// RewardPoolAmount returns the reward pool P the creator funds up front:
//
//	P = max_shares × F_reveal + rate × distance × max_shares × bump
//	    └──── work ────────┘   └────────── time ──────────────┘
//
// distance is commit_deadline → reveal_end_block in blocks; maxShares is the
// band ceiling — the count selected, distributed to, and priced for. The pool
// prices both halves of what a guardian gives up: the reveal transaction it
// must send, whose gas is the same at every distance, and the time it holds
// the share. Without the first, revenue falls below cost on any short secret
// and the guardian completes the job at a loss.
//
// The pool is FIXED at this amount: band slots left unfilled at activation are
// never refunded (they enlarge the revealers' split at settlement). bump is in
// wire format (hundredths) and scales the time component only. Truncation dust
// from the fixed-point division is negligible and absorbed here (the creator
// pays the truncated value).
func RewardPoolAmount(distance, maxShares, bump int64) math.Int {
	return RewardPoolAmountWith(RatePerGuardianBlock, distance, maxShares, bump)
}

// RewardPoolAmountWith is the parameterised core of RewardPoolAmount; op
// order matches the wrapper exactly (single truncating division last).
func RewardPoolAmountWith(rate, distance, maxShares, bump int64) math.Int {
	return RevealLeg().Mul(math.NewInt(maxShares)).
		Add(TimeComponentAmountWith(rate, distance, maxShares, bump))
}

// ProRataCancellationPayout returns each guardian's payout when the creator
// cancels at elapsed blocks past the commit deadline (floor 0), derived
// ENTIRELY from the secret's stored economics — never from the live
// RatePerGuardianBlock constant:
//
//	per-guardian payout = P × elapsed ÷ (distance × max_shares)
//	                    = (F_reveal_at_creation + rate_at_creation × bump × distance)
//	                      × elapsed ÷ distance
//
// The pool's reveal leg accrues with the hold exactly as the wage does, so a
// creator who cancels at activation pays neither and one who cancels at the
// last cancellable block pays both. A guardian is never out of pocket at any
// cancellation point: its acceptance is reimbursed in full from accept_fees,
// which this function does not touch. The max_shares denominator
// keeps the per-guardian-per-block wage constant regardless of how many
// accepted — the unearned remainder refunded to the creator therefore
// includes any unfilled band slots' portion. Deriving from the stored
// pool is the in-flight repricing guarantee of the immutable-economics
// ruling (docs/planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md, work
// item 2): a software upgrade that retunes the rate re-prices FUTURE secrets
// only — no in-flight secret's cancellation wage moves by one uveil. The
// creator is refunded P − accepted × payout.
func ProRataCancellationPayout(rewardPool math.Int, distance, maxShares, elapsed int64) math.Int {
	if elapsed < 0 {
		elapsed = 0
	}
	denominator := math.NewInt(distance).Mul(math.NewInt(maxShares))
	if !denominator.IsPositive() {
		return math.ZeroInt()
	}
	return rewardPool.Mul(math.NewInt(elapsed)).Quo(denominator)
}

// EntryFee returns the one-off registration fee as an Int. It is charged into
// the fee collector and rides the next block's 90/10 split — 90% allocated to
// validator rewards, 10% burned — and is never returned. See docs/spec.md
// "Guardian Registration".
func EntryFee() math.Int {
	return math.NewInt(EntryFeeAmount)
}

// KeyRotationFee returns the flat burned fee charged per key rotation:
//
//	fee = rate × KeyRotationFeeBlocks    (one guardian-day)
//
// Derived from the master rate so a rate retune cascades automatically —
// never hard-code the product. Anti-spam pricing of the permanent history
// entry, not economics (see docs/spec.md "Guardian Key Rotation").
func KeyRotationFee() math.Int {
	return math.NewInt(RatePerGuardianBlock).Mul(math.NewInt(KeyRotationFeeBlocks))
}

// SplitFeeAmountWith divides a transaction-fee amount into validator and
// burn shares at the given burn percentage. This is the PARAMETERISED CORE
// of the fee split: the chain calls the constant-bound SplitFeeAmount;
// the core stays parameterised so tests can sweep burnPercent (originally
// for the since-decommissioned economic simulator).
//
// The validator share is floored, so the integer-division dust joins the
// burn — deflation-biased, matching the house rule that split dust is
// always burned. Conservation is exact by construction:
// validator + burn == amount, always.
func SplitFeeAmountWith(amount math.Int, burnPercent int64) (validator, burn math.Int) {
	validator = amount.
		Mul(math.NewInt(100 - burnPercent)).
		Quo(math.NewInt(100))
	burn = amount.Sub(validator)
	return validator, burn
}

// SplitFeeAmount applies the protocol's fee split (FeeValidatorPercent /
// FeeBurnPercent) to a single fee amount.
func SplitFeeAmount(amount math.Int) (validator, burn math.Int) {
	return SplitFeeAmountWith(amount, FeeBurnPercent)
}

// MinRequiredFeeWith is the parameterised core of MinRequiredFee: the
// smallest fee (uveil) a transaction declaring gasLimit may pay at the
// given price fraction, with CEILING division — rounding in the protocol's
// favour, so no gas limit prices to zero.
func MinRequiredFeeWith(gasLimit uint64, priceNum, priceDen int64) math.Int {
	gas := math.NewIntFromUint64(gasLimit)
	num := gas.Mul(math.NewInt(priceNum))
	den := math.NewInt(priceDen)
	return num.Add(den.SubRaw(1)).Quo(den)
}

// MinRequiredFee returns the consensus-enforced minimum fee for a
// transaction declaring gasLimit:
//
//	required = ⌈gas_limit × MinGasPriceUveilNum ÷ MinGasPriceUveilDen⌉
//
// The app's ante chain rejects any transaction paying less, in both CheckTx
// and DeliverTx (DONE_CONSENSUS_FEE_FLOOR_PLAN.md).
func MinRequiredFee(gasLimit uint64) math.Int {
	return MinRequiredFeeWith(gasLimit, MinGasPriceUveilNum, MinGasPriceUveilDen)
}

// CreationFeeBpsWith is the parameterised core of the creation-fee
// percentage curve, in basis points of the pool's time component: linear from maxBps at zero
// distance down to minBps at curveEndBlocks, flat beyond (truncating
// division — the house fixed-point style).
func CreationFeeBpsWith(distance, maxBps, minBps, curveEndBlocks int64) int64 {
	d := distance
	if d < 0 {
		d = 0
	}
	if d > curveEndBlocks {
		d = curveEndBlocks
	}
	return maxBps - (maxBps-minBps)*d/curveEndBlocks
}

// CreationFeeBps applies the protocol curve (CreationFeeMaxBps →
// CreationFeeMinBps over CreationFeeCurveEndBlocks) to a secret's distance.
func CreationFeeBps(distance int64) int64 {
	return CreationFeeBpsWith(distance, CreationFeeMaxBps, CreationFeeMinBps, CreationFeeCurveEndBlocks)
}

// CreationFeeFloor is the flat anti-grinding floor of the creation fee:
// CreationFeeFloorGas priced at the consensus-enforced minimum gas price
// (60,000 uveil = 0.06 VEIL today). Gas-denominated by design — a
// discarded selection draw always costs ~3× the gas that accompanies it,
// and the floor moves automatically with any gas-floor retune.
func CreationFeeFloor() math.Int {
	return MinRequiredFee(CreationFeeFloorGas)
}

// CreationFeeWith is the parameterised core of CreationFee:
//
//	fee = max(floor, timeComponent × bps(distance) ÷ 10,000)
func CreationFeeWith(timeComponent math.Int, distance, maxBps, minBps, curveEndBlocks int64, floor math.Int) math.Int {
	curve := timeComponent.Mul(math.NewInt(CreationFeeBpsWith(distance, maxBps, minBps, curveEndBlocks))).Quo(math.NewInt(10_000))
	return math.MaxInt(floor, curve)
}

// CreationFee returns the non-refundable fee charged at
// MsgUserRequestGuardians. It never enters module escrow — it is a draw
// price, not a deposit — and rides the fee collector's 90/10 split
// (docs/spec.md "Creation Fee").
//
// The percentage is charged on the pool's TIME component only, never on the
// gas reimbursements the creator also funds (accept_fees and the pool's
// reveal legs). A draw does not become more expensive because the guardians'
// gas is being passed through, and taxing a pass-through would route part of
// every guardian's reimbursement to validators and the burn.
func CreationFee(timeComponent math.Int, distance int64) math.Int {
	return CreationFeeWith(timeComponent, distance, CreationFeeMaxBps, CreationFeeMinBps, CreationFeeCurveEndBlocks, CreationFeeFloor())
}

// CreationFeeIsFloorPriced reports whether the floor (rather than the
// percentage curve) sets the fee at this shape — surfaced in the
// reservation event as the pricing regime (`floor` vs `percent`). Takes the
// same time-component base as CreationFee.
func CreationFeeIsFloorPriced(timeComponent math.Int, distance int64) bool {
	curve := timeComponent.Mul(math.NewInt(CreationFeeBps(distance))).Quo(math.NewInt(10_000))
	return CreationFeeFloor().GT(curve)
}

// SlashSplitWith is the parameterised core of the bond-slash split: both
// percentage slices are floored and the remainder — including all
// integer-division dust — goes to the third party (the reporter for early
// reveals, the slashed guardian for no-shows). Conservation is exact by
// construction: burn + creator + remainder == bond, always.
func SlashSplitWith(bond math.Int, burnPercent, creatorPercent int64) (burn, creator, remainder math.Int) {
	burn = bond.MulRaw(burnPercent).QuoRaw(100)
	creator = bond.MulRaw(creatorPercent).QuoRaw(100)
	remainder = bond.Sub(burn).Sub(creator)
	return burn, creator, remainder
}

// NoRevealSlashSplit applies the protocol's no-reveal split
// (NoRevealBurnPercent / NoRevealCreatorPercent / remainder returned to the
// guardian) to a posted bond.
func NoRevealSlashSplit(bond math.Int) (burn, creator, returned math.Int) {
	return SlashSplitWith(bond, NoRevealBurnPercent, NoRevealCreatorPercent)
}

// EarlyRevealSlashSplit applies the protocol's early-reveal split
// (EarlyRevealBurnPercent / EarlyRevealCreatorPercent / remainder to the
// reporter as bounty) to a posted bond.
func EarlyRevealSlashSplit(bond math.Int) (burn, creator, reporter math.Int) {
	return SlashSplitWith(bond, EarlyRevealBurnPercent, EarlyRevealCreatorPercent)
}

// ── Recipient rebate ────────────────────────────────────────────────────────
//
// The rebate is bounded twice over: by the creator's own irrecoverable spend
// (so farming is a loss) and by an allowance that accrues from the rebate
// pool's balance (so the drain has a ceiling). See docs/spec.md
// "Recipient Rebate".

// RebateAccrualPerBlock is the allowance the rebate pool accrues each block:
// its own balance divided by RebateAccrualDivisor. Proportional by design — as
// the pool falls the rate falls with it, so the pool decays geometrically and
// never empties.
func RebateAccrualPerBlock(poolBalance math.Int) math.Int {
	if !poolBalance.IsPositive() {
		return math.ZeroInt()
	}
	return poolBalance.QuoRaw(RebateAccrualDivisor)
}

// RebateAllowanceCap is the most unclaimed allowance may accumulate:
// RebateBurstBlocks of accrual at the current balance.
func RebateAllowanceCap(poolBalance math.Int) math.Int {
	return RebateAccrualPerBlock(poolBalance).MulRaw(RebateBurstBlocks)
}

// AccrueRebateAllowance advances an allowance by the blocks elapsed since it
// was last touched, capped at RebateAllowanceCap. Elapsed heights at or below
// zero accrue nothing, so a replayed or out-of-order call cannot inflate it.
func AccrueRebateAllowance(allowance, poolBalance math.Int, blocksElapsed int64) math.Int {
	ceiling := RebateAllowanceCap(poolBalance)
	if blocksElapsed > 0 {
		allowance = allowance.Add(RebateAccrualPerBlock(poolBalance).MulRaw(blocksElapsed))
	}
	if allowance.GT(ceiling) {
		return ceiling
	}
	return allowance
}

// RebateRatioOf is the ratio ceiling: RebateRatioPercent of the creator's
// irrecoverable spend on the secret.
func RebateRatioOf(irrecoverableSpend math.Int) math.Int {
	if !irrecoverableSpend.IsPositive() {
		return math.ZeroInt()
	}
	return irrecoverableSpend.MulRaw(RebateRatioPercent).QuoRaw(100)
}

// RebateAmount is the credited rebate for one secret settling alongside
// `settlingCount` others: the smaller of the ratio ceiling and this secret's
// equal share of the allowance. Returns zero — meaning "credit nothing" —
// below RebateDustFloor, so the protocol never books a rebate worth less than
// the transaction that would collect it.
//
// settlingCount is the number of secrets settling at this height, whatever
// their outcome: a secret that settles `failed` is counted and credited
// nothing, and a no-discovery secret's share is likewise never collected.
// Counting them is deliberate — the alternative would leak how many secrets at
// a height carry a real recipient.
func RebateAmount(irrecoverableSpend, allowance math.Int, settlingCount int64) math.Int {
	if settlingCount <= 0 || !allowance.IsPositive() {
		return math.ZeroInt()
	}
	amount := RebateRatioOf(irrecoverableSpend)
	if share := allowance.QuoRaw(settlingCount); amount.GT(share) {
		amount = share
	}
	if amount.LT(math.NewInt(RebateDustFloor)) {
		return math.ZeroInt()
	}
	return amount
}
