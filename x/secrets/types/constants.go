package types

import (
	"os"
	"strconv"
)

// ChainCoinType is the chain's registered BIP44 coin type: wallet keys derive
// at m/44'/9733'/0'/0/0 (docs/spec.md, "Network Configuration"). Declared in
// this module — the shared wire contract — so the chain (app/config.go) and
// the guardian derive from a single Go declaration; clients that cannot
// import Go pin their copy against this module's
// testdata/vectors/wallet_derivation.json, which travels with it.
const ChainCoinType = 9733

// Secret state constants to avoid magic strings throughout the codebase
const (
	// SECRET_STATUS_RESERVED indicates Phase 1 complete, guardians assigned, awaiting encrypted shares
	SECRET_STATUS_RESERVED = "reserved"
	// SECRET_STATUS_AWAITING_ACCEPTANCE indicates Phase 2 complete, shares stored, awaiting guardian acceptance
	SECRET_STATUS_AWAITING_ACCEPTANCE = "awaiting_acceptance"
	// SECRET_STATUS_PENDING indicates Phase 3 complete, sufficient guardians accepted, awaiting reveal window
	SECRET_STATUS_PENDING = "pending"
	// SECRET_STATUS_RECONSTRUCTABLE indicates threshold has been met, secret can be reconstructed, but reveal window is still open
	SECRET_STATUS_RECONSTRUCTABLE = "reconstructable"
	// SECRET_STATUS_REVEALED indicates reveal window has closed and secret lifecycle is complete
	SECRET_STATUS_REVEALED = "revealed"
	// SECRET_STATUS_CANCELLED indicates the creator cancelled the secret before reveal
	SECRET_STATUS_CANCELLED = "cancelled"
	// SECRET_STATUS_FAILED indicates insufficient guardian acceptances or reveal deadline passed without sufficient shares
	SECRET_STATUS_FAILED = "failed"
)

// Event type constants derived from secret states for consistency
const (
	// EventSecretPrefix is a prefix for various event types
	EventSecretPrefix = "secret_"
	// EventTypeSecretReserved is emitted when transitioning to RESERVED state (Phase 1 completes, guardian assignment)
	EventTypeSecretReserved = EventSecretPrefix + SECRET_STATUS_RESERVED
	// EventTypeSecretAwaitingAcceptance is emitted when transitioning to AWAITING_ACCEPTANCE state (Phase 2 completes, encrypted shares stored)
	EventTypeSecretAwaitingAcceptance = EventSecretPrefix + SECRET_STATUS_AWAITING_ACCEPTANCE
	// EventTypeSecretPending is emitted when transitioning to PENDING state (sufficient guardians accept, awaiting reveal window)
	EventTypeSecretPending = EventSecretPrefix + SECRET_STATUS_PENDING
	// EventTypeSecretReconstructable is emitted when transitioning to RECONSTRUCTABLE state (threshold reached, secret can be reconstructed)
	EventTypeSecretReconstructable = EventSecretPrefix + SECRET_STATUS_RECONSTRUCTABLE
	// EventTypeSecretRevealed is emitted when transitioning to REVEALED state (reveal window closed, lifecycle complete)
	EventTypeSecretRevealed = EventSecretPrefix + SECRET_STATUS_REVEALED
	// EventRebateCommitted is emitted when a collector publishes the commitment
	// binding a recipiency proof to its address (step 1 of collection)
	EventRebateCommitted = "rebate_committed"
	// EventRebateCredited is emitted at settlement when a revealed secret's
	// recipient rebate is credited and reserved in the rebate pool
	EventRebateCredited = "rebate_credited"
	// EventRebateCollected is emitted when a recipient proves recipiency and
	// the credited rebate is paid out
	EventRebateCollected = "rebate_collected"
	// EventRebateExpired is emitted when an uncollected rebate's reservation
	// returns to the pool as its secret is pruned
	EventRebateExpired = "rebate_expired"
	// EventTypeSecretCancelled is emitted when transitioning to CANCELLED state (creator cancels secret)
	EventTypeSecretCancelled = EventSecretPrefix + SECRET_STATUS_CANCELLED
	// EventTypeSecretPruned is emitted at Stage 2 retention pruning — the
	// archival hook: it carries the full canonical TerminalSecretRecord
	// (base64) plus its digest, so indexers that retain events hold a
	// complete, self-verifying archive of every pruned secret.
	EventTypeSecretPruned = EventSecretPrefix + "pruned"
	// EventTypeSecretFailed is emitted when transitioning to FAILED state (insufficient acceptances or reveal deadline passed)
	EventTypeSecretFailed = EventSecretPrefix + SECRET_STATUS_FAILED

	// Guardian assignment events (not state transitions)
	// EventTypeAssignmentAccepted is emitted when a guardian accepts an assignment
	EventTypeAssignmentAccepted = "assignment_accepted"
	// EventTypeAssignmentRejected is emitted when a guardian rejects an assignment
	EventTypeAssignmentRejected = "assignment_rejected"
)

// Message validation constants (SSS-aligned)
const (
	// MinThreshold is the minimum threshold value allowed for secrets (SSS constraint)
	MinThreshold = int64(2)
	// MaxThreshold is the maximum threshold value allowed for secrets (SSS constraint)
	MaxThreshold = int64(16)
	// MinShares is the floor of the guardian band: min_shares can never be
	// below it (SSS constraint; band validation additionally requires
	// threshold ≤ min_shares)
	MinShares = int64(2)
	// MaxTotalShares is the absolute maximum for max_shares — the count
	// selected and SSS-split (SSS implementation constraint)
	MaxTotalShares = int64(32)
	// MinRevealStartOffset is the minimum buffer blocks from acceptance deadline to reveal start (5 minutes)
	MinRevealStartOffset = int64(50)
	// MaxRevealHorizon (H) is the maximum blocks from creation to reveal_end_block
	// (~1 year at 6s blocks). Kept equal to MaxAvailabilityWindow so that every
	// validated reveal window can be covered by a freshly registered guardian —
	// validation must never promise a window selection cannot staff. Raising H
	// beyond the availability cap requires guardian handoff/bond-transfer
	// mechanics. See docs/spec.md "Timing Constraints".
	MaxRevealHorizon = int64(MaxAvailabilityWindow)
	// MaxRevealStartOffset is the maximum blocks from current to reveal start.
	// Bounded by the reveal horizon: an offset beyond H cannot yield a valid window.
	MaxRevealStartOffset = MaxRevealHorizon
	// MinRevealDuration is the minimum reveal window duration (10 minutes)
	MinRevealDuration = int64(100)
	// MaxRevealDuration is the maximum reveal window duration (1 day)
	MaxRevealDuration = int64(14_400)
	// NOTE: MinCancelBlocks removed — cancellation is permitted at any point
	// before reveal_start_block (pro-rata guardian pay makes late cancellation
	// non-abusive). NOTE: Min/MaxRewardAmount removed — the reward pool is
	// protocol-derived (P = rate × distance × shares × bump), not creator-chosen.
	// See docs/spec.md "Secret Economics & Slashing".
)

// Commit timeout constant (assuming ~6 second block time)
const (
	// CommitTimeoutBlocks is the blocks every secret gets for the complete
	// 3-phase commit: commit_deadline = creation height + CommitTimeoutBlocks
	// (~5 minutes). Fixed by the protocol rather than chosen by the creator —
	// guardian acceptance is automated and lands within seconds, so a longer
	// window buys nothing, and the window is time in which a secret cannot be
	// cancelled. See docs/spec.md "Secret Parameters".
	CommitTimeoutBlocks = int64(50)
	// MinRevealStartOffsetTotal is the floor on a reveal window's start offset:
	// the commit window plus MinRevealStartOffset. A constant rather than a
	// computation, because the commit window no longer varies.
	MinRevealStartOffsetTotal = CommitTimeoutBlocks + MinRevealStartOffset
)

// Event attribute keys for consistent event structure
const (
	// AttributeKeySecretId is the key for secret ID in events
	AttributeKeySecretId = "secret_id"
	// AttributeKeyCreator is the key for creator address in events
	AttributeKeyCreator = "creator"
	// AttributeKeyGuardianAddress is the key for guardian address in events
	AttributeKeyGuardianAddress = "guardian_address"
	// AttributeKeyShareIndex is the key for share index in events
	AttributeKeyShareIndex = "share_index"
)

// Guardian event type constants
const (
	// EventGuardianRegistered is emitted when a guardian registers
	EventGuardianRegistered = "guardian_registered"
	// EventGuardianUpdated is emitted when a guardian updates their parameters
	EventGuardianUpdated = "guardian_updated"
	// EventGuardianSlashed is emitted when a guardian is slashed for misbehavior
	EventGuardianSlashed = "guardian_slashed"
	// EventGuardianKeyRotated is emitted when a guardian rotates its
	// share-encryption key forward (a new key epoch takes effect)
	EventGuardianKeyRotated = "guardian_key_rotated"
)

// Guardian key rotation — forward-only key epochs (docs/spec.md "Guardian
// Key Rotation"). Both values are hard constants per the immutable-economics
// stance (Position A) — retuning is a software upgrade, never governance.
const (
	// KeyRotationFeeBlocks prices the flat burned rotation fee at one
	// guardian-day of the master rate (fee = rate × KeyRotationFeeBlocks,
	// derived in economics.go): anti-spam pricing of the permanent history
	// entry and its forever-reserved uniqueness slot, not economics.
	KeyRotationFeeBlocks = int64(14_400)
	// KeyRotationMinIntervalBlocks is the minimum spacing between a
	// guardian's rotations (~30 days at 6s blocks): the newest history
	// entry's effective height must be at least this many blocks old
	// (epoch 0's, set at registration, starts the clock). Bounds worst-case
	// history growth to ~12 entries per guardian-year. A guardian whose
	// current key is compromised inside the window sets accepting_secrets =
	// false immediately (identical forward protection) and rotates when the
	// window opens.
	KeyRotationMinIntervalBlocks = int64(432_000)
)

// State-integrity alarm constants. Settlement and commit-expiry failures are
// deterministic assertions (a bug elsewhere has already corrupted the books —
// there are no transient errors on those paths), so the alarm fires from the
// FIRST failure and every retry: the alarm IS the detection mechanism.
// See docs/spec.md "Settlement trigger — due-height queues".
const (
	// EventSettlementStalled is emitted every block a due settlement or
	// commit-expiry entry fails to process. The failed secret's partial state
	// is discarded (per-secret cache-commit), the queue entry is retained,
	// and the funds stay locked — not lost — in module escrow until an
	// upgraded binary ships the fix and the pending retry completes.
	EventSettlementStalled = "settlement_stalled"
	// StalledOpSettlement marks a stalled reveal-window settlement.
	StalledOpSettlement = "settlement"
	// StalledOpCommitExpiry marks a stalled commit-timeout expiry.
	StalledOpCommitExpiry = "commit_expiry"
)

// Slashing type constants
const (
	// SlashTypeNoReveal indicates guardian failed to reveal share in time
	SlashTypeNoReveal = "no_reveal"
	// SlashTypeEarlyReveal indicates guardian revealed share before authorized window (reporter evidence)
	SlashTypeEarlyReveal = "early_reveal"
)

// Bonded guardian economics — base constants (v1 provisional values).
//
// These are the hand-set knobs of the economic model; everything else (the
// per-secret bond B, reward pool P) is DERIVED from them via the helpers in
// economics.go and must never be hard-coded. See docs/spec.md
// "Secret Economics & Slashing" for the full model.
const (
	// RatePerGuardianBlock is the master reward price level: what one guardian earns
	// per block at bump = 1, in base units (1 uveil = 0.000001 VEIL).
	RatePerGuardianBlock = int64(1)
	// GuardianAcceptGas / GuardianRevealGas denominate a guardian's cost of
	// doing the job in GAS: the two transactions it must send, priced at the
	// consensus gas floor via MinRequiredFee. The creator funds both — the
	// accept leg in the secret's accept_fees, the reveal leg inside the reward
	// pool — because revenue scales with distance while this cost does not, so
	// without them every short secret is completed at a loss.
	//
	// These are observations of the protocol's own code path, not policy
	// knobs: the measured limits are 110,314 and 118,984, rounded up so a
	// guardian paying the floor price is never left short. Gas-denominated by
	// the same device as CreationFeeFloorGas, so both track any future
	// gas-floor retune automatically.
	GuardianAcceptGas = uint64(120_000)
	GuardianRevealGas = uint64(130_000)
	// EntryFeeAmount is the one-off guardian registration fee (1,000 VEIL in
	// base units), charged into the fee collector, where it rides the next
	// block's 90/10 fee split like every validator-bound flow — 90% allocated
	// to validator rewards, 10% burned (one-pipe ruling, July 2026).
	EntryFeeAmount = int64(1_000_000_000)
	// BumpScale is the fixed-point scale for the creator's security factor:
	// bump is expressed in hundredths (2 decimal places), so 100 = 1.00.
	BumpScale = int64(100)
	// MaxTier is the ceiling on the security factor (bump ∈ [1.00, MaxTier]).
	MaxTier = int64(10)
	// MinBump / MaxBump bound the wire representation (hundredths).
	MinBump = BumpScale           // 1.00
	MaxBump = MaxTier * BumpScale // 10.00
)

// Per-guardian bond multiplier k — the live reputation value that prices each
// guardian's bonds (B = rate × distance × bump × k). k shares BumpScale's
// hundredths fixed-point representation and moves on each individual event:
// × 1.26 per slash (either violation), × 0.963 per correct on-chain reveal,
// truncating integer arithmetic, clamped into [MinBondK, MaxBondK]. See
// docs/spec.md "The Per-Guardian Bond Multiplier k" (ruled July 2026).
const (
	// MinBondK is the floor of the bond multiplier range (4.00 in hundredths).
	// New registrants start here; a clean history can never price a bond below it.
	MinBondK = int64(400)
	// MaxBondK is the ceiling of the bond multiplier range (24.00 in hundredths).
	// Eight consecutive slashes climb the full range from the floor.
	MaxBondK = int64(2400)
	// InitialBondK is every new registrant's starting multiplier — the floor.
	InitialBondK = MinBondK
	// KSlashMulNum/KSlashMulDen: on every slash, k′ = min(MaxBondK, k × 126 ÷ 100).
	KSlashMulNum = int64(126)
	KSlashMulDen = int64(100)
	// KRevealMulNum/KRevealMulDen: on every correct reveal,
	// k′ = max(MinBondK, k × 963 ÷ 1000) — recovery ≈ 6× slower than the climb.
	KRevealMulNum = int64(963)
	KRevealMulDen = int64(1000)
	// MaxActiveBondsPerGuardian is the hard per-guardian concurrency cap: a
	// guardian at the cap is not a selection candidate and cannot lock further
	// bonds until an existing secret settles. An eligibility gate, checked at
	// selection and re-checked at acceptance (ruled July 2026).
	MaxActiveBondsPerGuardian = int64(100)
)

// Terminal-secret retention (docs/planning/PENDING_TERMINAL_SECRET_RETENTION_PLAN.md).
// Live state scales with active secrets, not every secret ever: a terminal
// secret's remaining records are pruned RetentionBlocks after terminal_at,
// replaced by a permanent ~180B SecretTombstone.
const (
	// RetentionBlocks is how long a terminal secret's reconstruction inputs
	// (reveal records + payload ciphertext) and slim record remain in state
	// after the terminal transition: ~6 months at 6s blocks (ruled July 2026,
	// revised from 3 months). Hardcoded like every other protocol constant
	// (Position A — no governance parameters); retuning is a software upgrade.
	RetentionBlocks = int64(2_592_000)
	// MaxPrunesPerBlock caps Stage 2 pruning work per EndBlock; a burst of
	// same-height expiries carries over to the next block via the <=-drain.
	MaxPrunesPerBlock = 50
)

// RetentionBlocksEnvVar is the devnet/test-only override consumed by
// RetentionBlocksValue. The start command refuses to boot while it is set
// unless --unsafe-dev-overrides is passed (cmd/timeflared/cmd/commands.go).
const RetentionBlocksEnvVar = "TIMEFLARE_RETENTION_BLOCKS"

// KeyRotationMinIntervalEnvVar is the devnet/test-only override consumed by
// KeyRotationMinIntervalValue, so the e2e-scenarios suite can exercise key
// rotation in minutes instead of thirty days. Guarded by the same
// --unsafe-dev-overrides acknowledgement as the retention override.
const KeyRotationMinIntervalEnvVar = "TIMEFLARE_KEY_ROTATION_MIN_INTERVAL"

// KeyRotationMinIntervalValue returns the minimum rotation interval,
// honouring a devnet/test-only environment override.
//
// ⚠️ CONSENSUS-CRITICAL: every node in a network must agree on the value —
// production nodes must never set the variable (unset ⇒ the compiled
// constant, which is the protocol value). `timeflared start` refuses to run
// with the variable set unless the operator explicitly acknowledges it with
// --unsafe-dev-overrides.
func KeyRotationMinIntervalValue() int64 {
	if v := os.Getenv(KeyRotationMinIntervalEnvVar); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return KeyRotationMinIntervalBlocks
}

// RetentionBlocksValue returns the retention window, honouring a
// devnet/test-only environment override (TIMEFLARE_RETENTION_BLOCKS).
//
// ⚠️ CONSENSUS-CRITICAL: the override exists so devnet e2e scenarios can
// exercise pruning in minutes instead of months. Every node in a network
// must agree on the value — production nodes must never set the variable
// (unset ⇒ the compiled constant, which is the protocol value). As a
// backstop, `timeflared start` refuses to run with the variable set unless
// the operator explicitly acknowledges it with --unsafe-dev-overrides.
func RetentionBlocksValue() int64 {
	if v := os.Getenv(RetentionBlocksEnvVar); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return RetentionBlocks
}

// RebateCollectionWindow is how long a credited rebate stays collectable, in
// blocks, clamped to the live retention window. The clamp is what makes
// "collection always ends before pruning" true by construction rather than by
// convention: a devnet running TIMEFLARE_RETENTION_BLOCKS=60 gets a 60-block
// collection window, not a promise it cannot keep.
func RebateCollectionWindow() int64 {
	if retention := RetentionBlocksValue(); retention < RebateCollectionBlocks {
		return retention
	}
	return RebateCollectionBlocks
}

// RebateCollectionDeadline is the last height at which a rebate credited to a
// secret that went terminal at terminalAt can be collected.
func RebateCollectionDeadline(terminalAt int64) int64 {
	return terminalAt + RebateCollectionWindow()
}

// Transaction-fee split, applied once per block at BeginBlock (ordered
// before x/distribution allocates the fee collector): the protocol's
// GUARANTEED, usage-proportional deflation — the mechanical burns (entry
// fees, slashing, dust) are scenario-dependent, this one is not. Shares
// must total 100 (guarded by TestFeeSplitSharesTotal100). Ruled July 2026
// (docs/planning/PENDING_FEE_BURN_PLAN.md); the split ratio is a sweepable
// input to the economic simulation via SplitFeeAmountWith.
const (
	// FeeValidatorPercent of every transaction fee flows to validators via
	// the standard distribution module.
	FeeValidatorPercent = int64(90)
	// FeeBurnPercent of every transaction fee is permanently burned. The
	// integer-division dust joins the burn (deflation-biased, matching the
	// house rule that split dust is always burned).
	FeeBurnPercent = int64(10)

	// MinGasPriceUveilNum / MinGasPriceUveilDen express the consensus-enforced
	// minimum gas price of 0.1 uveil per gas unit as an integer fraction — no
	// Dec types in the consensus path. The app's ante chain rejects, in BOTH
	// CheckTx and DeliverTx, any transaction paying less than
	// ⌈gas_limit × Num ÷ Den⌉ uveil (ceiling division — rounding in the
	// protocol's favour). This is protocol law, not node etiquette: the
	// app.toml `minimum-gas-prices` setting remains the per-node
	// mempool-admission knob and may only sit at or above this floor
	// (ruled July 2026 — DONE_CONSENSUS_FEE_FLOOR_PLAN.md).
	MinGasPriceUveilNum = int64(1)
	MinGasPriceUveilDen = int64(10)
)

// Creation fee — the non-refundable price of a selection draw, charged at
// MsgUserRequestGuardians on top of the escrowed pool P and routed to the
// fee collector (90/10 split). One fee, two jobs: it closes the
// abandon-and-refund grinding hole by pricing every draw, and it is the
// recurring validator budget that scales with the value consensus secures.
// See docs/spec.md "Creation Fee" (ruled July 2026).
const (
	// CreationFeeMaxBps / CreationFeeMinBps bound the percentage curve in
	// basis points of P: linear from 10% at minimal distance down to 5% at
	// CreationFeeCurveEndBlocks, flat 5% beyond. Because P grows linearly
	// with distance while the rate falls, the absolute fee never decreases
	// with distance — no window shape lowers the bill.
	CreationFeeMaxBps = int64(1_000)
	CreationFeeMinBps = int64(500)
	// CreationFeeCurveEndBlocks is where the curve reaches its 5% floor —
	// 30 days at the ~6s production block time, the same guardian-day base
	// as every other duration constant.
	CreationFeeCurveEndBlocks = int64(432_000)
	// CreationFeeFloorGas denominates the anti-grinding floor in GAS: the
	// floor is MinRequiredFee(CreationFeeFloorGas) — three reference
	// 200k-gas transactions at the consensus-enforced gas floor (60,000
	// uveil today), so "a discarded draw costs ~3× its gas" holds by
	// construction and tracks any future gas-floor retune automatically.
	// Deliberately NOT wage-denominated: at minimal distances even 10% of P
	// is far below one gas fee, so only a flat gas-anchored floor prices a
	// grinding draw.
	CreationFeeFloorGas = uint64(600_000)
)

// Bond distribution on slashing, as percentages of the posted bond.
// Each violation's shares must total 100 (the no-reveal remainder is returned
// to the guardian). The creator share must stay below 100 (self-dealing
// invariant) and the burn share above 0.
const (
	// No reveal (operational failure): 40% burned, 10% creator, 50% returned.
	NoRevealBurnPercent     = int64(40)
	NoRevealCreatorPercent  = int64(10)
	NoRevealReturnedPercent = int64(50)
	// Early reveal (malicious, proven): 40% burned, 10% creator, 50% reporter, 0% returned.
	EarlyRevealBurnPercent     = int64(40)
	EarlyRevealCreatorPercent  = int64(10)
	EarlyRevealReporterPercent = int64(50)
)

// Recipient rebate — the protocol's only distribution mechanism. A secret that
// reaches `revealed` credits its recipient a rebate on what the creator
// irrecoverably spent, paid from the keyless rebate pool. See docs/spec.md
// "Recipient Rebate" (ruled July 2026).
const (
	// RebateRatioPercent is the share of the creator's irrecoverable spend a
	// recipient can be credited. Below 100 by construction: manufacturing a
	// rebate costs S to receive 0.30 × S, so farming is a loss at ANY token
	// price — the property that lets one mechanism serve testnets and mainnet.
	// The 70-point margin covers the one recapture path (being a selected
	// guardian, which sortition allocates and an entry fee, float and slashing
	// exposure price).
	RebateRatioPercent = int64(30)
	// RebateAccrualDivisor sets the per-block accrual as a fraction of the
	// rebate pool's own balance: accrual = balance ÷ 50,000,000. Fully claimed
	// that is 10% of the remaining balance per year at 5,256,000 blocks —
	// the fastest drain the protocol can produce, and it requires every block
	// of the year to settle a secret. Because the rate follows the balance, the
	// pool decays geometrically and never empties.
	RebateAccrualDivisor = int64(50_000_000)
	// RebateBurstBlocks caps accumulated allowance at one day of accrual.
	// Accumulation is what lets a lone recipient receive a full rebate rather
	// than a single block's accrual, and lets a cluster of simultaneous
	// settlements be paid at once; the cap is what stops an idle stretch
	// becoming a drainable lump.
	RebateBurstBlocks = int64(14_400)
	// RebateDustFloor is the smallest rebate worth crediting: 0.05 VEIL, five
	// times the gas of the transaction that collects it (~100,000 gas at the
	// 0.1 uveil consensus floor). A share below this is not credited at all.
	//
	// Sized against what the protocol actually charges rather than against a
	// round number: at RatePerGuardianBlock the pool for a month-long
	// five-share secret is ~2.16 VEIL, so its rebate is ~0.65 VEIL. A 1 VEIL
	// floor would have excluded every secret shorter than about four months —
	// most of them — while still being only 100× the gas. This floor admits any
	// secret whose irrecoverable spend exceeds ~0.17 VEIL (about eleven days at
	// five shares) and leaves collection comfortably worth doing.
	RebateDustFloor = int64(50_000)
	// RebateCollectionBlocks is how long a credited rebate stays collectable:
	// three months at ~6s blocks, measured from the settlement that credited it.
	// After it, the rebate is void and its reservation returns to the pool for
	// redistribution — adoption funding that nobody claimed should fund the next
	// newcomer rather than sit reserved forever.
	//
	// ⚠️ This MUST stay below RetentionBlocks. The proof of recipiency is
	// recomputed against the secret's detection hint, and pruning takes the hint
	// with it — a collection window outliving retention would promise a rebate
	// the chain has already lost the means to verify. Three months against six
	// leaves the same margin again. The invariant is enforced two ways: a test
	// (TestRebateCollectionWindowStaysInsideRetention) and
	// RebateCollectionWindow, which clamps to the live retention value so a
	// devnet running a short retention override cannot promise more than it can
	// honour.
	RebateCollectionBlocks = int64(1_296_000)
	// RebatePoolName is the keyless module account holding the rebate pool
	// (70% of supply at genesis). It has no permissions: no minting, no
	// burning, and no key — only the rebate formula can move it.
	RebatePoolName = "rebate_pool"
)

// Denomination constants
const (
	// DefaultDenom is the default denomination for the chain
	DefaultDenom = "uveil"
)

// Validation constants
const (
	// PublicKeyLength is the required length for guardian public keys
	PublicKeyLength = 32
	// SecretIdLength is the exact length for UUID format secret IDs (36 characters: "xxxxxxxx-xxxx-Vxxx-Yxxx-xxxxxxxxxxxx")
	SecretIdLength = 36
	// MaxAvailabilityWindow is the maximum blocks a guardian can be available (1 year at 6s blocks)
	MaxAvailabilityWindow = 5_256_000
	// MinEvidenceLength is the minimum bytes required for slash evidence
	MinEvidenceLength = 32
	// MaxPayloadSize caps the stored payload ciphertext C (key-share
	// architecture). 4096B of original secret + two 60B encryption layers —
	// preserving the pre-key-share effective secret size exactly. Raising it
	// is a one-parameter decision that no longer multiplies by guardian count;
	// revisit with the measurement evidence gathered during implementation
	// (see DONE_KEY_SHARE_ARCHITECTURE_PLAN.md §9.2).
	MaxPayloadSize = int64(4216)
	// MaxKeyShareSize caps a guardian's ENCRYPTED key share: the 34B versioned
	// envelope + 60B encryption overhead = 94B, with margin for future
	// envelope versions (e.g. VSS commitments).
	MaxKeyShareSize = int64(128)
	// MaxRevealedKeyShareSize caps the PLAINTEXT key-share envelope submitted
	// at reveal (and as early-reveal evidence): 34B today, margin for future
	// envelope versions.
	MaxRevealedKeyShareSize = int64(64)
	// SecretPublicKeySize is the exact length of the per-secret public key pk_s.
	SecretPublicKeySize = 32
)

// Guardian availability validation constants
const (
	// MinAvailabilityWindow is the minimum duration a guardian must be available (10 minutes at 6s blocks)
	MinAvailabilityWindow = 100
	// MaxAvailableFromOffset is the maximum blocks in future for availability start (6 months at 6s blocks)
	MaxAvailableFromOffset = 2_628_000
)
