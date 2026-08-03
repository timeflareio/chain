# Timeflare Protocol Specification

*A decentralised protocol for time-locked secret reveals using threshold cryptography and economic incentives*

> **📋 Complete Operation Specifications**: For detailed field definitions, validation rules, examples, and implementation details of all messages and operations, see **[operations.md](operations.md)**. This document focuses on protocol architecture, economics, and security design.

## Table of Contents

**Protocol Overview**
1. [Introduction](#introduction)
   - [What is Timeflare?](#what-is-timeflare)
   - [Revolutionary Capabilities](#revolutionary-capabilities)
   - [Solving Real-world Problems](#solving-real-world-problems)
   - [Architecture Overview](#architecture-overview)
2. [Actors, Operations and Workflow](#actors-operations-and-workflow)
   - [Actors](#actors)
   - [Three-Phase Secret Lifecycle Journey](#three-phase-secret-lifecycle-journey)
   - [👮 Guardian Registration](#-guardian-registration)
   - [👮 Guardian Updates](#-guardian-updates)
   - [👮 Guardian Key Rotation](#-guardian-key-rotation)
   - [👤 Secret Request & Guardian Assignment (Phase 1)](#-secret-request--guardian-assignment-phase-1)
   - [👤 Share Distribution (Phase 2)](#-share-distribution-phase-2)
   - [👮 Guardian Acceptance (Phase 3)](#-guardian-acceptance-phase-3)
   - [👤 Secret Cancellation](#-secret-cancellation)
   - [👮 Guardian Reveal Stage](#-guardian-reveal-stage)
   - [Window Closure & Reward Distribution](#window-closure--reward-distribution)
3. [Secret Economics & Slashing](#secret-economics--slashing)
   - [Actors and Quantities](#actors-and-quantities)
   - [Economic Parameters](#economic-parameters)
   - [Guardian Lifecycle and the Float](#guardian-lifecycle-and-the-float)
   - [Secret Lifecycle and Money Movement](#secret-lifecycle-and-money-movement)
   - [Settlement](#settlement-threshold-independent)
   - [Early-Reveal Reporting](#early-reveal-reporting)
   - [Cancellation and No-Fault Refunds](#cancellation-and-no-fault-refunds)
   - [Recipient Rebate](#recipient-rebate)
   - [Economic Invariants](#economic-invariants)
4. [Security Architecture](#4-security-architecture)
   - [Slashing Resilience Through Redundancy](#slashing-resilience-through-redundancy)
   - [State Integrity & Import-Time Validation](#state-integrity--import-time-validation)
   - [Multi-Layer Encryption Protection](#multi-layer-encryption-protection)
   - [Common Attack Vectors & Mitigations](#common-attack-vectors--mitigations)
5. [Complete Secret Workflow](#5-complete-secret-workflow)
   - [Client-Side Integration Guide](#client-side-integration-guide)

**Technical Details**
6. [Configuration & Parameters](#configuration--parameters)
   - [Network Configuration](#network-configuration)
   - [Guardian Parameters](#guardian-parameters)
   - [Guardian Key Management](#guardian-key-management)
   - [Secret Parameters](#secret-parameters)
   - [Economic Constants](#economic-constants)
   - [Secret Pricing](#secret-pricing)
   - [Genesis Pool Allocations](#genesis-pool-allocations)
   - [Key Economic & Strategic Decisions](#key-economic--strategic-decisions)

---

## Introduction

### What is Timeflare?

Timeflare is a decentralised blockchain protocol built on the Cosmos SDK that revolutionises time-locked secret management through **cryptographically enforced delays** and **economic security**. The protocol enables users to publish secrets that remain provably inaccessible until a predetermined future time, solving critical trust and coordination problems across digital systems.

At its core, Timeflare operates as a **dual-network ecosystem**: validators secure consensus whilst specialised guardians manage secret-sharing operations. This separation ensures optimal security for both blockchain operations and sensitive secret data through purpose-built economic incentives.

**"Set it and forget it"** - Once configured, Timeflare requires zero maintenance while providing cryptographic guarantees stronger than any legal contract. Whether you're protecting your family's future, ensuring fair play, or creating trustless business arrangements, Timeflare delivers certainty in an uncertain world. The protocol serves anyone who needs to say: *"If X happens (or doesn't happen) by time Y, then reveal Z"* - a fundamental primitive for countless applications we haven't even imagined yet.

### Revolutionary Capabilities

- 🔒 **Cryptographic Time-locks**: Mathematically guaranteed secret protection using Shamir Secret Sharing with HMAC verification
- 🏗️ **Trustless Architecture**: No central authorities or trusted third parties - security through distributed economic incentives  
- 💰 **Sustainable Economics**: Fixed 1B VEIL supply with deflationary fee burning creates lasting value without inflation
- ⚡ **Universal Access**: Very low 0.1 uveil fees make time-locking accessible for any use case
- 🛡️ **Multi-layer Security**: HMAC verification, progressive reveal windows, verifiable guardian selection, and immediate economic slashing
- 🌐 **Protocol-controlled Selection**: Automated guardian assignment prevents user bias and ensures optimal security distribution

### Solving Real-world Problems

| 🎯 Use Case & Scenario | 🔧 Applications |
|------------------------|-----------------|
| **🔐 Recovery & Backup Scenarios**<br/>Future-dated access for emergency situations | Password Recovery: Backup access for forgotten credentials<br/>Emergency Access: Trusted contacts get access if unreachable<br/>Travel Safety: Location/contact info if you don't check in |
| **🔔 Personal Safety & Deadman Switches**<br/>Journalist investigating dangerous story needs insurance | Set reveal 30 days out, refresh weekly while safe<br/>Multiple revelation times for escalating disclosures<br/>Distributed to multiple news outlets simultaneously<br/>Cancellable if situation resolves safely |
| **🎯 Time-Delayed Announcements**<br/>Coordinate reveals across time and geography | Family Announcements: Share pregnancy/engagement news with extended family simultaneously<br/>Public Information: Ensure fair access to important disclosures for all stakeholders<br/>Regulatory Changes: Give all affected parties equal advance notice of new rules<br/>Contest Winners: Prevent early leaks while coordinating announcements across platforms<br/>Documentary Premieres: Coordinate global releases across timezones for maximum impact<br/>News Embargo Releases: Synchronize publication across multiple media outlets |
| **🏛️ Estate Planning & Digital Inheritance**<br/>Sarah wants family to access her digital assets if she passes | Create encrypted instructions<br/>Set reveal 1 year out<br/>Renew annually while alive<br/>Auto-access if she passes<br/>One-time fee, no lawyers |
| **🎮 Gaming & Digital Entertainment**<br/>Game studio needs provably fair competition with sealed moves | Tournament Play: Encrypted strategies revealed after submission<br/>NFT Drops: Rarity locked until mint completion<br/>Story Events: Plot twists time-locked for anticipation<br/>Puzzle Hunts: Progressive clues over weeks/months<br/>Beta Rewards: Time-locked exclusive content |
| **📊 Corporate Transparency & Governance**<br/>Public company proves earnings projections made in good faith | Sealed Predictions: Lock estimates, reveal after quarter ends<br/>Audit Trails: Time-lock discussions for investigations<br/>Vesting Schedules: Automate releases without intermediaries<br/>M&A Processes: Progressive reveal of due diligence<br/>Regulatory Compliance: Prove document timestamps |

#### Why These Use Cases Need Timeflare

All these scenarios share common requirements that existing solutions cannot adequately address. Users need absolute certainty that secrets will be revealed at the specified time, without relying on lawyers, companies, or other trusted third parties that could fail or disappear. The solution must work globally regardless of geography or jurisdiction, whilst offering one-time payment rather than ongoing subscription fees. Crucially, users must retain the ability to cancel their time-locked secrets if circumstances change, all whilst ensuring complete privacy preservation until the designated reveal time.

Timeflare uniquely delivers on all these requirements through its innovative design. The protocol combines trustless operation with economic security—requiring no central authority whilst incentivising proper behaviour through per-secret guardian bonds and immediate slashing penalties. It achieves decentralised reliability through a distributed guardian network with built-in redundancy and direct payment models that ensure availability. The system maintains privacy through encryption until reveal time, yet provides transparency through verifiable guardian selection and public proofs of protocol compliance. Most importantly, it offers unmatched flexibility by supporting arbitrary future reveal times and guaranteed cancellation options, all backed by cryptographic certainty rather than legal promises.


#### Why Existing Solutions Fall Short

Time-locked secrets are essential for countless applications, yet existing approaches force unacceptable tradeoffs:

**Trusted third parties** offer the simplest path to time-locked secrets with straightforward setup processes that require minimal technical knowledge. However, they introduce fundamental risks that make them unsuitable for critical applications. Users face business continuity risks when service providers shut down or change their business models, leaving secrets permanently inaccessible. Security vulnerabilities in centralised systems create single points of failure that can expose all stored secrets to attackers. The ongoing costs and subscription dependencies make these solutions expensive over time, whilst geographic limitations prevent global access and may subject users to changing regulatory environments.

**Randomness beacons** provide a decentralised approach that eliminates single points of control, making them appealing for trustless applications. Yet they suffer from critical economic and practical limitations. Without economic incentives, there's no guarantee that beacon operators will maintain their services long-term, leading to potential service degradation or abandonment. The public nature of randomness means all timing is predictable and observable, removing privacy and enabling coordination attacks. Fixed timing constraints limit flexibility for complex use cases, whilst the lack of participant compensation creates sustainability concerns for the underlying infrastructure.

**Computational puzzles** deliver truly trustless design where no third parties are required, making them attractive for pure decentralisation advocates. However, they impose massive environmental costs through energy waste, making them ethically questionable and economically inefficient. The unpredictable timing inherent in proof-of-work systems means secrets may be revealed hours or days off schedule, making them unsuitable for time-sensitive applications. Each puzzle represents a single point of failure where hardware problems or network issues can prevent revelation entirely, whilst the lack of cancellation options means users cannot cancel secrets even when circumstances change dramatically.

**Smart contract locks** enable sophisticated programmable logic and complex conditional reveals, making them flexible for advanced use cases. Unfortunately, they force permanent public visibility of all secret metadata and timing information, destroying privacy before secrets are even revealed. High storage costs on blockchain networks make them prohibitively expensive for larger secrets or long-term storage. The blockchain dependency means secrets are forever tied to the survival and accessibility of specific networks, whilst privacy limitations make them unsuitable for sensitive personal or commercial information that requires confidentiality until reveal time.


### Architecture Overview

Timeflare implements a **dual-network design** that separates consensus (validators) from secret-sharing services (guardians), each with distinct economic incentives:

- **Validators**: Standard Cosmos SDK validators earning `90%` of transaction fees for consensus security
- **Guardians**: Specialised operators who pay an entry fee (routed through the 90/10 fee split) and lock per-secret bonds, earning direct rewards from secret creators
- **Unified Module**: Single `x/secrets/` module manages both guardian infrastructure and secret lifecycle

The protocol uses Shamir Secret Sharing with threshold cryptography and HMAC verification, ensuring secrets remain mathematically unrecoverable until the designated reveal time while providing slashing protection against early reveals. Guardian selection is protocol-controlled using verifiable randomness, preventing bias and ensuring optimal security distribution.

#### State Storage Model

Each secret is stored as a **slim metadata record** plus per-guardian **side-stores** keyed `(secret_id, guardian_address)`, so every operation reads and writes only what it touches (a guardian's confirmation never rewrites other guardians' encrypted shares):

- **Secret** (slim record): identity, timing, economics, commitment, state, the recipient **detection hint** (the recipient's key itself is never stored — see [Recipient Discovery](#recipient-discovery--detection-hints)), the Phase-1 `selected_guardians` list, and denormalised counters — `accepted_count` (assignments accepted, up to `max_shares`; at finalisation the accepted set *is* the active set), `revealed_count` (shares revealed), and `terminal_at` (height the secret reached a terminal state; `0` while live).
- **Share records** (cold, immutable): each guardian's encrypted share and HMAC, written once at share distribution and read one guardian at a time for confirmation, reveal verification and early-reveal evidence.
- **Assignment records** (hot, tiny): each guardian's response status (`proposed`/`accepted`/`rejected`) and response height, created at distribution and flipped exactly once. A Phase-1-selected guardian that received no share has no records and can never accept.
- **Reveal records**: one per revealed key share, written at reveal time.
- **Payload record** (cold, immutable): the secret's doubly-encrypted payload ciphertext `C`, keyed by secret id, written once at share distribution — the only copy of the secret material on chain. Reconstruction needs `C` plus ≥threshold revealed key shares.
- **Hint feed** (derived index): `(created_at, secret_id) → detection hint`, written once at creation and served in creation order by `Query/HintsSince` so recipients scan incrementally from a height cursor. Deleted when its secret is pruned (see Terminal Secret Retention) — discovery is bounded by the retention window by design.
- **Tombstones** (permanent, ~180B each): `secret_id → SecretTombstone`, written when a terminal secret is pruned — the last on-chain anchor, whose digest makes any archived copy of the full final record self-authenticating. Served by `Query/SecretTombstone`.

The counters are protocol state, not caches of convenience: acceptance slot-capping, activation and the reveal threshold are evaluated against them in O(1), and invariants assert they always match the side-stores. Queries assemble the full per-secret view from these stores on demand; consensus paths never do.

Guardian selection consumes **no creator input at all**: the selection seed is built entirely from consensus data (`SHA256(chainID ‖ uint64_be(height) ‖ lastBlockHash ‖ uint64_be(secret_counter))`), and the secret ID itself is protocol-assigned from a monotonic counter. The reservation event carries the seed inputs (height, previous block hash, counter) plus the derived seed, so anyone can confirm from public data that **the seed was honest** — none of its inputs are the creator's, so the creator could not have biased the selection. The event deliberately does not carry the candidate set; independently recomputing the full selection would require historical guardian state, which is a statement about validator honesty that BFT consensus already provides. See [Guardian Selection (Normative)](#guardian-selection-normative).

> **📋 For the operation-level API reference** (fields, validation, preconditions, side effects, CLI usage), see **[operations.md](operations.md)**; for state layout, genesis behaviour, measured performance and the judgement ledger, see **[CHAIN_MECHANICS.md](CHAIN_MECHANICS.md)**.

---


## Actors, Operations and Workflow

### Actors

The Timeflare protocol involves four distinct types of participants, each with specific roles and incentives:

👤 **Secret Creators** initiate the time-locked secret process by defining reveal parameters, funding formula-priced reward pools, and managing the three-phase publication workflow. They specify when secrets should be revealed, how many guardians are required, and tune their security level through a single `bump` dial that scales both the reward they pay and the collateral each guardian must lock. Creators can cancel their secrets once activated and before the reveal window opens — a paid exit that compensates guardians pro-rata for the blocks already guarded and refunds the unearned remainder (pre-activation, an abandoned secret exits via the commit-timeout with a full pool refund). They bear the cost of guardian services through direct reward payments but gain cryptographic certainty that their secrets will be revealed at exactly the specified time.

📨 **Recipients** are the intended beneficiaries of revealed secrets. Their long-term public key is used **client-side only**: the payload is encrypted to it before being sealed under the per-secret time-lock key, and a per-secret [detection hint](#recipient-discovery--detection-hints) is derived from it — **the key itself never appears in any transaction or state**, so no observer can enumerate a recipient's secrets or link two secrets to the same recipient. Recipients require no active participation in the protocol; they (or their wallet) discover secrets addressed to them by scanning detection hints with their private key, and the system handles reconstruction automatically once threshold requirements are met.

This always-encrypt design supports two disclosure patterns without any protocol difference: **targeted delivery**, where the private key is held by a specific individual and the revealed content remains confidential to them; and **public disclosure** (the bulletin-board pattern), where the creator uses a keypair whose private key is published or well known, making the content world-readable at reveal time by convention rather than by protocol.

👮 **Guardians** are specialised operators who pay a one-off entry fee (`1,000 VEIL`, routed through the 90/10 fee split) and maintain a deposited float of VEIL from which a returnable **bond** is locked for each secret they accept. They store encrypted secret shares and reveal them during designated time windows, earning direct rewards from secret creators for successful participation. Guardians set their own availability schedules and can pause new assignments at any time, allowing them to control their operational commitments. Misbehaviour costs them a percentage of the posted bond *and* their share of the secret's reward pool — early reveals forfeit the entire bond — and raises their personal bond multiplier `k`, making every future acceptance more expensive until a run of honest reveals earns it back down. See [Secret Economics & Slashing](#secret-economics--slashing) for the full penalty structure. Guardian selection is protocol-controlled using verifiable randomness, ensuring fair distribution of opportunities across all qualified participants: a guardian's `k` affects the bond they must be able to afford, never their selection probability.

🏛️ **Validators** secure the Timeflare blockchain through standard Cosmos SDK consensus mechanisms, processing all transactions and maintaining network integrity. They earn `90%` of all transaction fees as their sole compensation, creating strong incentives for reliable block production and network security. Validators do not participate in secret-sharing operations but provide the underlying blockchain infrastructure that enables the entire protocol. Their economic model is completely separate from guardian operations, with no cross-dependencies or shared reward pools.

🚨 **Reporters** are community members who monitor guardian behaviour and report protocol violations to earn bounty rewards. They can submit evidence of early secret reveals, receiving 50 % of the slashed bond as compensation for protecting network integrity — a bounty that scales with the security level of the secret breached. See [Early-Reveal Reporting](#early-reveal-reporting) for detailed procedures. Reporters face no stake requirements or registration processes - anyone can participate by providing valid cryptographic evidence of guardian misbehaviour. This creates a distributed monitoring system that incentivises community policing without requiring centralised oversight.

### Three-Phase Secret Lifecycle Journey

```
                           THREE-PHASE SECRET LIFECYCLE JOURNEY
                           ════════════════════════════════════

 TIME ─────────────────────────────────────────────────────────────────────────────────►

 👤 CREATOR             🔐 PROTOCOL                👮 GUARDIANS              📨 RECIPIENT
 ──────────             ──────────                ────────────              ────────────
      │                      │                         │                         │
      │                      │                         │                         │
  ┌───┴───┐                  │                         │                         │
  │Prepare│                  │                         │                         │
  │Secret │                  │                         │                         │
  │Offline│                  │                         │                         │
  └───┬───┘                  │                         │                         │
      │                      │                         │                         │
      ├─MsgUserRequestGuardians───►                        │                         │
      │   (Phase 1)          │                         │                         │
      │                  ┌───┴───┐                     │                         │
      │                  │Random │                     │                         │
      │                  │Select │                     │                         │
      │                  │   &   │                     │                         │
      │                  │Assign │                     │                         │
      │                  └───┬───┘                     │                         │
      │◄─Guardian List───────┤                         │                         │
      │   Response           │                         │                         │
      │  (reserved state)    │                         │                         │
      │                      │                         │                         │
  ┌───┴───┐                  │                         │                         │
  │Client │                  │   reserved              │                         │
  │Side   │                  │   STATE                 │                         │
  │Share  │                  │                         │                         │
  │Encrypt│                  │                         │                         │
  └───┬───┘                  │                         │                         │
      │                      │                         │                         │
      ├─MsgUserDistributeShares───►                        │                         │
      │   (Phase 2)          │                         │                         │
      │                      ├─Store Encrypted─────────►                         │
      │                      │  Shares & HMACs         │                         │
      │◄─Shares Stored───────┤                     ┌───┴───┐                     │
      │ (awaiting_           │                     │Decrypt│                     │
      │  acceptance)         │ awaiting_acceptance │   &   │                     │
      │                      │   STATE             │Verify │                     │
      │                      │                     │Offline│                     │
      │                      │                     └───┬───┘                     │
      │                      │                         │                         │
      │                      ◄─MsgGuardianConfirmShares────────┤                         │
      │                      │  (Phase 3)              │                         │
      │◄─Threshold Status────┤  Accept/Reject          │                         │
      │  (pending/failed)    │                         │                         │
      │                      │                         │                         │
  ┌───┴───┐                  │                         │                         │
  │ Wait  │                  │   pending               │                         │
  │Period │                  │   STATE                 │                         │
  │(or    │                  │  (MsgUserCancelSecret        │                         │
  │Cancel) │                  │   available)            │                         │
  └───┬───┘                  │                         │                         │
      │                      │                         │                         │
      │                      ├═══Reveal Window Opens═══►                         │
      │                      │                         │                         │
      │                      ◄─MsgGuardianRevealShare──────────┤                         │
      │                      │  (Staggered Timing)     │  ┌───────────────────┐  │
      │                      │  HMAC Verified          │  │ Client-side       │  │
      │                      │                         │  │ Reconstruction    │  │
      │                      │                         │  │ (when threshold   │  │
      │                      │                         │  │  shares available)│  │
      │                      │                         │  └───────────────────┘  │
      │                      ├═══Reveal Window Closes══►                         │
      │                      │                         │                         │
      │                  ┌───┴───┐                     │                         │
      │                  │EndBlk │                     │                         │
      │                  │Auto   │                     │                         │
      │                  │Process│                     │                         │
      │                  └───┬───┘                     │                         │
      │                      ├─Slash Non-Revealers────►                         │
      │                      ├─Threshold Check         │                         │
      │                      ├─Distribute Rewards──────►                         │
      │                      │  (if threshold met) OR  │                         │
      │                      ├─Refund Creator──────────┐                         │
      │                      │  (if threshold failed)  │                         │
      │◄─Final State─────────┼─Secret REVEALED/FAILED───┼─────────────────────────►
      │  (protocol complete) │  Ready for Decryption   │                         │
      │                      │                         │                         │
      ▼                      ▼                         ▼                         ▼
```

The Timeflare protocol orchestrates a sophisticated three-phase commit process that ensures secrets remain secure until their designated reveal time. **Phase 1** begins when creators request guardian assignment, triggering the protocol's verifiable random selection of qualified guardians who will store encrypted shares. **Phase 2** sees creators encrypt their secret shares client-side and distribute them to assigned guardians along with HMAC verification data for future slashing protection (see [Secret Economics & Slashing](#secret-economics--slashing)). **Phase 3** requires guardians to decrypt and verify their shares offline before explicitly accepting or rejecting their assignments via `MsgGuardianConfirmShares` — each acceptance locking the secret's bond from the guardian's float. Acceptances accumulate until the commit deadline, where the roster finalises: with at least `min_shares` accepted, the secret enters a waiting period until the reveal window opens.

During the reveal phase, guardians progressively submit their decrypted shares using `MsgGuardianRevealShare`. Recipients can begin client-side secret reconstruction immediately once sufficient shares are available, but **protocol settlement and final state transitions only occur after the reveal window closes**. The reveal window is inclusive of both bounds (`reveal_start_block ≤ height ≤ reveal_end_block`); in the EndBlock of block `reveal_end_block + 1` the protocol's settlement returns each revealing guardian's bond and splits the reward pool among them, slashes non-revealing guardians' bonds, and refunds the pool to the creator only if nobody revealed (see [Settlement](#settlement-threshold-independent)).

**📋 Complete Specifications**: For detailed field definitions, validation rules, examples, and implementation details of all operations, see **[operations.md](operations.md)**.

### 👮 Guardian Registration

Guardians are the backbone of Timeflare's time-lock security, serving as the distributed network that makes cryptographically enforced delays possible. Without guardians, secrets cannot be time-locked since they are the only actors capable of storing encrypted shares and revealing them at predetermined times. To become a guardian, operators pay a one-off **entry fee** of `1,000 VEIL` (routed through the 90/10 fee split) and provide an encryption key, establishing themselves as participants in the secret-sharing network. They then maintain a deposited **float** of VEIL — the working capital from which a per-secret bond is locked each time they accept an assignment. Once registered and within their availability window, guardians become eligible for automatic selection by the protocol when creators publish new secrets, provided their unlocked float covers the specific secret's bond.

```go
type MsgGuardianRegister struct {
    Guardian              string    // Guardian's blockchain address (also transaction signer)
    EncryptionPublicKey   []byte    // Public key for encrypting secret shares (32 bytes)
    AvailableFrom         int64     // Relative blocks from current when guardian starts accepting (0 = current_block + 1)
    AvailableUntil        int64     // Relative blocks from available_from when guardian stops accepting (minimum 100 blocks)
    Deposit               Coin      // Initial float deposit (may be zero; the entry fee is charged separately into the fee split)
    AcceptingSecrets      bool      // Whether to accept new secret assignments
}
```

#### Registration Requirements
**Guardian addresses** must be unique and cannot already exist in the protocol, and must **not be vesting accounts** — guardian floats are slashable collateral, and slashing must never reach into unvested tokens, so any account carrying a vesting schedule is rejected at registration. (The Cosmos SDK vesting-account machinery is otherwise available on the chain — nothing restricts vesting accounts from validating or delegating — but no protocol allocation currently carries a vesting schedule.) **The entry fee** (`1,000 VEIL`) is charged from the guardian's account at registration and sent to the **fee collector**, where it rides the next block's 90/10 fee split like every validator-bound flow — `900 VEIL` allocated to validator rewards, `100 VEIL` permanently burned (ruled July 2026: the split is the chain's one fee-side deflationary lever, and no flow is exempt). The fee is not part of the float and is never returned. New registrants start with the bond multiplier at its floor, `k = 4.00` (see [Secret Economics & Slashing](#secret-economics--slashing)). **Encryption keys** must be exactly `32 bytes`, must be a **usable X25519 public key**, and must be globally unique across **every key ever registered by any guardian, all epochs included** — a retired key stays reserved forever, since share material encrypted to it may still exist. "Usable" excludes the curve's **small-order points**: an X25519 exchange against one yields an all-zero shared secret, so every share encrypted to such a key would be encrypted under a publicly computable key and readable by any observer. Rejection is normative and applies at every point the protocol accepts an X25519 key (see [Common Attack Vectors](#common-attack-vectors--mitigations), "Small-Order Key Registration"). The registration key becomes **epoch 0** of the guardian's append-only key history. **Each epoch's key binding is permanently immutable**: the key a share was encrypted to can never change, preserving the cryptographic binding between guardian assignments and specific keys throughout secret lifecycles. Guardians requiring new encryption keys rotate forward via `MsgGuardianRotateKey` (see [Guardian Key Rotation](#-guardian-key-rotation)) — the current-epoch pointer advances for future selections while every old epoch continues to serve the assignments made under it. **Availability windows** cannot exceed `5,256,000` blocks (approximately one year) and must start at the current block or later. **Failed registrations** consume transaction fees but leave all existing state unchanged.

#### Registration Process
**New guardians only** - this operation is for initial registration. Existing guardians must use `MsgGuardianUpdate` to modify their parameters or top up their float. **Duplicate registrations** are rejected if the guardian address already exists. **Float custody** is per-guardian module escrow: the protocol tracks each guardian's `total` and `locked` amounts (`unlocked = total − locked`); withdrawal is capped at `unlocked`. **Availability activation** makes guardians eligible for selection once their specified window begins.

#### What Happens Next
After successful registration, guardians enter a monitoring phase where they watch for new secret assignments from the protocol's selection algorithm. When selected, they receive encrypted shares that they must decrypt, verify, and store securely until the designated reveal time. Guardians earn rewards directly from secret creators based on successful participation, with typical assignments lasting from hours to months depending on the creator's requirements.

> **📋 For detailed field specifications, validation rules, examples, and technical implementation details, see [MsgGuardianRegister in operations.md](operations.md#msgregisterguardian).**

### 👮 Guardian Updates

Once registered, guardians can modify their operational parameters to adapt to changing circumstances whilst maintaining their position in the network. Update operations allow guardians to extend availability windows, top up their float to increase bond capacity, and control assignment acceptance without disrupting existing commitments. This flexibility ensures guardians can maintain optimal service levels throughout their operational lifecycle whilst protecting ongoing secret assignments from disruption.

```go
type MsgGuardianUpdate struct {
    Guardian              string    // Guardian's blockchain address (also transaction signer)
    AvailableFrom         int64     // Optional: Relative blocks from current when guardian starts accepting (0 = preserve existing)
    AvailableUntil        int64     // Optional: Relative blocks from available_from when guardian stops accepting
    Deposit               Coin      // Optional: Additional VEIL deposited into the float
    AcceptingSecrets      *bool     // Optional (presence-aware): omit = no change; explicit true/false = set
}
```

#### Update Constraints and Process

**Availability Window Restrictions** are enforced based on the guardian's current state relative to their availability period:

- **Within Availability Period** (`available_from ≤ current_block ≤ available_until`): Only extensions allowed - can increase `available_until` but `available_from` changes are ignored and the existing value is preserved
- **Precedes Availability Period** (`current_block < available_from`): Only extensions allowed - can increase `available_until` but `available_from` changes are ignored and the existing value is preserved  
- **Passed Availability Period** (`available_until < current_block`): Full updates allowed - can change both `available_from` and `available_until` with normal timing constraints

**Other Constraints**: Float deposits are incremental top-ups; reducing the float is done through `MsgGuardianWithdrawStake`, which returns the entire *unlocked* portion at any time whilst bonds for in-flight secrets remain locked (the guardian record persists — registration is permanent). Assignment acceptance can be freely adjusted for operational control, with accepting secrets set to false enabling graceful maintenance whilst honouring existing commitments.

Updates must be signed by the guardian's address and specify at least one field change to prevent unnecessary transactions (an explicit `accepting_secrets` value — true or false — counts as a field change on its own). All fields are applied atomically with complete failure if any constraint is violated, ensuring updates cannot compromise existing secret commitments or active assignment obligations.

#### What Happens Next

After successful updates, guardians continue operating with their new parameters. Availability window changes affect future selection eligibility. Assignment acceptance changes take effect immediately for new secret publications. Existing commitments remain unaffected by any parameter changes, ensuring continuity of service for active secrets.

> **📋 For detailed field specifications, validation rules, examples, and technical implementation details, see [MsgGuardianUpdate in operations.md](operations.md#msgupdateguardian).**

### 👮 Guardian Key Rotation

Rotation lets a guardian replace its share-encryption key for **future** assignments while every old key remains bound to the assignments made under it. It is the hygiene path professional operators expect — bounding the blast radius of a key leak to one epoch and making multi-year guardianship sustainable — without abandoning the address, its float, its selection history, or the entry fee already paid.

```go
type MsgGuardianRotateKey struct {
    Guardian    string    // Guardian's blockchain address (also transaction signer)
    NewKey      []byte    // The next epoch's encryption public key (32 bytes)
}
```

#### Key Epochs (Normative)

A guardian's keys form an **append-only history** `(guardian, epoch) → {public_key, effective_from_height}`, with epoch `0` being the registration key. The guardian record carries the current epoch; no key is ever overwritten or deleted. The following is consensus-critical:

1. **Forward-only semantics.** A rotation takes effect for **selections after it lands**: a rotation landing at height *h* records `effective_from_height = h + 1` (registration records its own height + 1 for epoch 0). Every existing assignment remains bound to the epoch key it was created under; reveal obligations, HMACs and slashing evidence for those assignments are untouched. Replacement or re-encryption semantics are impossible by construction — the chain holds only ciphertext and can re-encrypt nothing.
2. **The epoch in force is derivable, never stored per-secret.** The epoch in force for guardian *g* at height *h* is the newest history entry with `effective_from_height ≤ h` — a pure function of the history. Selection hands the creator the key in force at the selection height (a same-block rotation and selection therefore select the **pre-rotation** key, whatever the transaction order), and that is the key the creator encrypts to. Nothing is stored on the secret record.
3. **Global, permanent uniqueness.** The new key must be unique across **every key ever registered by any guardian, all epochs included**; a retired key stays reserved forever, since share material encrypted to it may still exist.
4. **Usable-key validation.** The new key must pass the same X25519 usability check as a registration key: exactly `32 bytes` and not a small-order point. A rotation is the one place a guardian could otherwise *downgrade* itself to an unusable key after passing registration, so the check is applied identically at both entry points.
5. **Priced and rate-limited.** Each rotation appends a permanent key record, so it carries a **flat burned fee of `rate × 14,400`** (one guardian-day at 6-second blocks — anti-spam, not economics) and a **minimum interval of one rotation per guardian per `432,000` blocks** (30 days): `current_height − last_effective_height ≥ 432,000`, applying uniformly from registration (epoch 0's effective height starts the clock). Both are hard constants (Position A — no governance parameters). The interval bounds worst-case history growth to ~12 entries per guardian-year and makes same-block multi-rotation impossible.
6. **Event.** A successful rotation emits `guardian_key_rotated { guardian, new_epoch, effective_from_height, fee_burned }`.

#### What Rotation Deliberately Does Not Change

- **HMAC verification** — HMAC keys derive from `(secretID, guardianAddress)`, not the encryption key; reveals and early-reveal evidence are epoch-agnostic.
- **Selection** — tickets are `SHA256(seed ‖ address)`; rotation creates no selection surface and no grinding angle.
- **Bonds, settlement, economics, the bond multiplier `k`** — untouched; rotation is a key-lifecycle mechanic only (it is *not* a reputation reset — `k` survives rotation).
- **One share, one holder** — rotation moves no share material anywhere (share/bond transfer remains ruled out permanently; see [Common Attack Vectors](#common-attack-vectors--mitigations), "Share Ownership Transfer").
- **Registration** — epoch 0 is the registration key, exactly as today; the entry fee is unchanged.

#### What Rotation Cannot Do

Rotation is **not a loss-recovery mechanism**. Shares are encrypted client-side by creators to the then-current key; the chain holds only ciphertext. A guardian who loses a key still misses every reveal encrypted to it and eats the no-reveal slashes — loss mitigation is the custody story (encrypted-at-rest key file, backup/restore, startup self-check; see [GUARDIAN_KEY_CUSTODY.md](guides/GUARDIAN_KEY_CUSTODY.md)).

#### Rotation and In-Flight Commit Windows

A rotation landing between a secret's Phase 1 and Phase 2 is a **non-event on-chain**: `MsgUserDistributeShares` performs no key validation (the chain cannot check which key ciphertext is encrypted to), the creator encrypts to the key handed over in the Phase-1 response, and the guardian daemon still holds that epoch's key. There is no freeze on rotation during open commit windows and no per-secret pin. Operationally the clean sequence is stop accepting (`MsgGuardianUpdate`, `accepting_secrets = false`) → drain → rotate; skipping it merely means holding the old epoch's key until its secrets settle. A guardian whose *current* key is compromised inside the minimum interval sets `accepting_secrets = false` immediately — instant, free, and identical forward protection to rotating — and rotates when the window opens.

#### What Happens Next

After a successful rotation, selections from the next block onward hand creators the new epoch's key. The guardian daemon resolves the correct epoch key for every assignment automatically (from the secret's creation height against the history's effective heights) and carries each retired key until the last assignment encrypted to it settles, at which point the key can be deleted locally. The key history is queryable via `Query/GuardianKeyHistory`.

### 👤 Secret Request & Guardian Assignment (Phase 1)

Secret creation begins when users need to time-lock information for future revelation, whether for personal safety, business arrangements, or technical coordination. This operation transforms a user's intent into an active protocol commitment by automatically selecting qualified guardians through cryptographically verifiable randomness. The protocol handles all guardian selection complexity, ensuring creators get the exact security level they need without having to research or choose individual guardians. Once complete, creators receive everything needed to proceed with encrypted share distribution.

```go
type MsgUserRequestGuardians struct {
    Creator              string                    // Secret creator's address (transaction signer)
    DetectionHint        DetectionHint             // Per-secret recipient discovery hint (see Recipient Discovery); the recipient's key is never submitted
    RevealWindow         RevealWindow              // When and how shares should be revealed
    Threshold            int64                     // Minimum shares needed for reconstruction (2-16)
    MinShares            int64                     // Minimum guardian acceptances for the secret to proceed (threshold ≤ min)
    MaxShares            int64                     // Guardian candidates selected and shares distributed (min ≤ max ≤ 32, max − min < threshold)
    Bump                 int64                     // Security factor in hundredths (100-1000 = 1.00-10.00); scales reward pool and guardian bonds together
}

type MsgUserRequestGuardiansResponse struct {
    SecretId            string                      // Protocol-assigned secret ID (UUIDv5 over chainID ‖ counter)
    GuardianAssignments []GuardianInfo              // Guardian info for client encryption
}

type GuardianInfo struct {
    Address    string                               // Guardian's account address
    PublicKey  []byte                               // Guardian's encryption public key (32 bytes)
}
```

#### Guardian Selection (Normative)

Selection is fully protocol-controlled — the creator supplies **no selection input of any kind**. The secret ID is protocol-assigned and the selection is a **hash sortition** over the eligible guardian set. The following is consensus-critical and byte-exact:

1. **Secret ID assignment.** Each request consumes the next value of a monotonic `secret_counter` (a chain-lifetime sequence, read in consensus transaction order, exported/imported in genesis, and never re-derived from the stored secret set — pruned secrets consumed values too). The user-facing ID is `UUIDv5(namespace, chainID ‖ uint64_be(counter))` with `namespace = UUIDv5(DNS, "secrets.timeflare")` — UUID-shaped for client continuity, chain-scoped, and opaque about the running total.
2. **Eligibility predicate**, evaluated at the request's execution height: registered, within the availability window now, `available_until ≥ reveal_end_block`, `accepting_secrets = true`, active-bond count below the concurrency cap (`100`), and unlocked float ≥ *this candidate's own* bond for this secret, `B_g = rate × distance × bump × k_g` (where `k_g` is the candidate's live bond multiplier — see [Secret Economics & Slashing](#secret-economics--slashing)). There is no float-weighting and no reputation-weighting — capital buys candidacy, never probability; `k` sizes the bond, never the ticket.
3. **Strict gate.** If fewer than `max_shares` guardians are eligible, the whole transaction **fails** — there is no reduced-band fallback. There is no protocol-side over-selection constant: the acceptance margin *is* the creator's chosen band `max_shares − min_shares`.
4. **Seed.** `seed = SHA256(chainID ‖ uint64_be(height) ‖ lastBlockHash ‖ uint64_be(secret_counter))`, where `lastBlockHash` is the **previous** block's hash from consensus-agreed header state (the current block's hash is unknown during execution) and integers are 8-byte big-endian. Every input is fixed, consensus-agreed, or protocol-assigned — none is the creator's.
5. **Sortition.** `ticket(g) = SHA256(seed ‖ guardian_address)` (the bech32 address string's bytes); the `max_shares` guardians with the **lowest** tickets win, tickets compared as 256-bit big-endian integers, the astronomically unlikely tie broken by guardian address ascending (byte-wise). Selection output is in ascending-ticket order. Because each guardian's ticket depends only on the seed and its own address, the outcome is independent of candidate enumeration order and stable under pool changes, and — the seed differing per secret — every eligible guardian is selected with equal probability (slots ÷ candidates) on any secret.
6. **Bond freeze.** Each selected guardian's bond amount `B_g = rate × distance × bump × k_g`, computed from their `k` at this height, is recorded on the secret alongside the selection list (in selection order). These frozen amounts are what acceptance locks and settlement releases or slashes — a guardian's `k` moving after selection never re-prices an existing selection (the in-flight immutability guarantee, Position A). Two guardians selected for an otherwise identical secret can therefore owe **different** bond amounts, reflecting their individual slash/reveal histories — an intentional design property.

**Verifiability claim.** The reservation event carries the seed inputs (height, `lastBlockHash`, `secret_counter`) and the derived seed: anyone can confirm from public data that the seed was honest, i.e. that the *creator* could not have biased the selection. The protocol does **not** claim third-party recomputability of the full selection (that would require reconstructing historical guardian state — a validator-honesty property that BFT consensus already provides), and the candidate set is deliberately not emitted. The creator's only residual influence is submission timing.

**The reward pool** `P = rate × distance × max_shares × bump` is computed by the protocol — the creator does not choose an amount — and is immediately locked in escrow, distributed only at settlement or cancellation. The pool is priced on `max_shares` and **fixed**: there is no activation-time refund of unfilled slots — fewer acceptances simply mean a larger per-guardian payout for the same creator cost. See [Secret Economics & Slashing](#secret-economics--slashing).

#### Timing Constraints
**Reveal windows** must start in the future and end within the reveal horizon: `reveal_end_block` cannot lie more than `H = 5,256,000` blocks (≈ 1 year) after the creation block. `H` deliberately equals the maximum guardian availability window, so every secret that passes validation can be covered by a freshly registered guardian — validation never promises a window that selection cannot staff. Longer-lived reveals are achieved by cancel-and-recreate cycles (the paid pro-rata cancellation makes this a first-class pattern, e.g. a dead-man's handle) — and this is permanent, not an interim gap: guardian handoff/bond-transfer was **ruled off the table** (July 2026) because any transfer mechanism is unsound under possession-based slashing evidence (see [Common Attack Vectors](#common-attack-vectors--mitigations), "Share Ownership Transfer"). **Commit timeout** is a protocol constant, not a creator choice: every secret gets `CommitTimeoutBlocks = 50` blocks (≈ 5 minutes at the production 6s cadence) to complete the entire 3-phase commitment process, after which it activates or fails automatically. The term is denominated in blocks, so its wall-clock length follows the chain's actual cadence. **Selection finality** means guardian assignments cannot be changed once committed to blockchain state.


#### What Happens Next
Creators receive guardian assignments with encryption public keys needed for Phase 2 share preparation. The secret enters "reserved" state with guardian commitments locked until share distribution or timeout. Creators must encrypt their secret shares offline using the provided keys before proceeding to Phase 2 within the specified commit timeout period. 

**The `[min_shares, max_shares]` band**: The protocol selects exactly `max_shares` candidates; each receives its own unique SSS share (the share's ID is intrinsic to the share data). The acceptance margin `max_shares − min_shares` is the creator's explicit rejection tolerance — there is no protocol-side buffer constant. Validation is relational and enforced in `ValidateBasic`:

```
MinThreshold ≤ threshold ≤ min_shares ≤ max_shares ≤ MaxTotalShares (32)
max_shares − min_shares < threshold        (strict)
```

- `threshold ≤ min_shares`: a `min`-sized activation must still be able to reach `threshold` reveals.
- `max_shares − min_shares < threshold` (the **gap bound**): on any secret that activates, at least `min_shares` guardians have confirmed, so the never-confirmed set is at most `max − min`; keeping it strictly below `threshold` ensures candidates who received a share but never bonded can never be a reconstruction-capable set on their own. The bound is **conditional on activation** — a secret that fails below `min_shares` leaves up to `max_shares` never-bonded share holders, exactly as a failed commit does today.
- `min_shares == max_shares` (a zero-width band, no tolerance) is a legitimate "exactly this many" request.

**A low threshold forces a narrow band — by design.** Combined with `threshold ≤ min`, the gap bound means a low threshold permits almost no over-selection (`threshold = 2` allows only `max ≤ min + 1`). This is self-regulating, not a defect: if any 2 shares reconstruct, any 2 idle recipients are dangerous, so a low-threshold secret cannot carry much safe redundancy. Permissible over-selection scales with `threshold`.

**Variable redundancy is an intentional semantic change.** With `threshold = 5`, a `max = 9` activation tolerates 4 reveal-time no-shows; a `min = 6` activation tolerates only 1. The creator's robustness guarantee is a *range*, and `min` close to `threshold` is doubly fragile — to activation failure and to reveal-time no-shows. Clients should visualise the low-end slack, not just the launch probability.

> **📋 For detailed field specifications, validation rules, examples, and technical implementation details, see [MsgUserRequestGuardians in operations.md](operations.md#msgrequestguardians-phase-1).**

### 👤 Share Distribution (Phase 2)

Share distribution completes the creator's cryptographic preparation by uploading encrypted secret fragments to their assigned guardians along with verification data for future security enforcement. This phase requires creators to perform client-side encryption using each guardian's public key, ensuring that secrets never exist in plaintext on the blockchain. The operation also establishes cryptographic evidence through HMACs that enable slashing detection if guardians reveal shares prematurely, creating the foundation for time-lock enforcement.

```go
type MsgUserDistributeShares struct {
    Creator           string                 // Secret creator's address (transaction signer)
    SecretId          string                 // Secret identifier from Phase 1
    Shares            []EncryptedShareData   // Encrypted KEY shares, one per guardian (~94B each)
    SecretCommitment  []byte                 // SHA256(C_r) — the recipient-encrypted payload
    PayloadCiphertext []byte                 // C — the doubly-encrypted payload, stored once (≤ MaxPayloadSize)
    SecretPublicKey   []byte                 // pk_s — the per-secret public key (32 bytes)
}

type EncryptedShareData struct {
    GuardianAddress   string    // Guardian's address (must match Phase 1 selection)
    EncryptedShare    []byte    // 34B key-share envelope encrypted with guardian's public key
    ShareHmac         []byte    // HMAC over the plaintext envelope, for reveal verification and slashing evidence
}
```

#### Distribution Requirements
**Guardian selection** requires creators to distribute shares to all assigned guardians from Phase 1. **Direct assignment** means each guardian must receive exactly one share at their assigned share index (1 to shares). **No flexibility** in share assignments - guardians receive the shares they were assigned during Phase 1. **Encryption validation** ensures shares are properly encrypted for their assigned guardians. **HMAC generation** must follow protocol standards to enable future slashing detection. **Secret commitments** provide cryptographic proof for post-reconstruction verification without revealing content.

#### Security Measures
**Client-side encryption** ensures secrets never exist in plaintext on-chain. **HMAC protection** enables detection of early reveals and provides slashing evidence. **Share validation** prevents malformed data that could compromise reconstruction. **Pre-configured commit timeout** (set in Phase 1) prevents indefinite guardian commitments and ensures timely progression.

#### What Happens Next
Guardians receive encrypted shares and begin offline verification of HMAC integrity and share validity. The secret transitions to "awaiting_acceptance" state where guardians must explicitly accept or reject their assignments. Creators wait for guardian responses while monitoring acceptance progress toward the `[min_shares, max_shares]` band.

> **📋 For detailed field specifications, validation rules, examples, and technical implementation details, see [MsgUserDistributeShares in operations.md](operations.md#msgdistributeshares-phase-2).**

### 👮 Guardian Acceptance (Phase 3)

Guardian acceptance represents the final checkpoint before secrets enter their waiting period, where assigned guardians verify share integrity and commit to participating in future revelation. This critical decision point protects guardians from malicious creators who might distribute invalid data while protecting creators from guardians who cannot fulfill their obligations. Only guardians who explicitly accept their assignments can participate in the reveal phase, ensuring all participants have verified their ability to complete the protocol.

```go
type MsgGuardianConfirmShares struct {
    Guardian    string    // Guardian's address (transaction signer)
    SecretId    string    // Secret identifier from previous phases
    Accept      bool      // true=accept assignment, false=reject assignment
}
```

#### Verification Requirements
**Offline validation** requires guardians to decrypt shares and verify HMAC integrity before responding. **Share authenticity** must be confirmed through cryptographic verification against stored commitments. **Bond collateral**: acceptance locks the guardian's *own* bond `B` — frozen for this guardian at selection (`B = rate × distance × bump × k`, using the guardian's `k` at selection height) — from the unlocked float; acceptance **fails** if `unlocked < B`, and also if the guardian's active-bond count has meanwhile reached the concurrency cap (`100`) — the cap is re-checked at the moment the bond actually locks, because a guardian can be in flight on several selections at once. **Capacity assessment** is otherwise the guardian's own responsibility — its client should decline assignments beyond what its infrastructure can serve. **Decision finality** means acceptance responses cannot be changed once submitted to the blockchain.

#### Response Handling within the Band
**No race**: every valid acceptance up to `max_shares` locks that guardian's bond and joins the roster — there is no first-`n` gate to lose, no acceptance is ever turned away for being late (within the deadline), and no guardian pays a fee just to learn it lost. **Lock-in is inferred, not signalled**: the moment `accepted_count` first reaches `min_shares` the secret is guaranteed to proceed (acceptances are never revoked, so the count can only hold or grow) — clients infer this from `accepted_count ≥ min_shares`; there is no on-chain event and no state transition, and the secret stays in `awaiting_acceptance` with confirmation open to the rest. **Acceptance creates a bonded commitment** to participate in the reveal phase when the designated time arrives. **Rejection allows graceful exit** without penalty but excludes the guardian from potential rewards. **Finalisation happens at `commit_deadline`**: `accepted_count ≥ min_shares` → `pending` with *exactly the accepted set*; fewer → `failed`, all bonds returned, pool refunded in full.

#### Critical Acceptance Logic with the Band
For a secret to progress to `pending` state:
- At least `min_shares` guardians must accept by `commit_deadline`; everyone who accepts (up to `max_shares`) is a real, activated participant
- Each acceptance locks that guardian's bond `B` in escrow
- Candidates may reject, fail the float check, or simply not respond — progression only requires the band's floor
- Example: with `threshold=5`, `min=6`, `max=9`, any 6–9 acceptances by the deadline activate the secret with that exact roster
- The accepted count sets the secret's reveal-time redundancy; the fixed pool splits among whoever accepted and reveals

#### What Happens Next
Secrets with sufficient guardian acceptance transition to "pending" state and enter their waiting period until reveal windows open. Guardians monitor for reveal timing while securely storing their verified shares offline. Creators can cancel secrets during the waiting period if circumstances change — a paid exit that compensates guardians pro-rata for blocks already guarded and refunds the unearned remainder.

> **📋 For detailed field specifications, validation rules, examples, and technical implementation details, see [MsgGuardianConfirmShares in operations.md](operations.md#msgconfirmshares-phase-3).**

### 👤 Secret Cancellation (Optional)

Secret cancellation allows creators to cancel their time-locked secrets once activated (`pending` state) and before the reveal window opens, providing essential flexibility for changed circumstances while maintaining economic fairness. **Cancellation is a post-activation mechanic** (ruled July 2026): it exists to release bonded guardians via a paid pro-rata exit, and before activation there is no committed guardian set to release — a pre-activation secret's only exit is the commit-timeout, which refunds the pool in full automatically. This also closes the cancel-instead-of-timeout bypass of selection-draw pricing (see [planning/done/DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md](planning/done/DONE_COMMIT_ABANDONMENT_FORFEIT_PLAN.md)).

```go
type MsgUserCancelSecret struct {
    Creator    string    // Secret creator's address (transaction signer)
    SecretId   string    // Secret identifier to cancel
    Reason     string    // Optional cancellation reason for audit trails
}
```

#### Cancellation Requirements
**Creator authorization** ensures only the original secret creator can cancel their secrets. **State validation** permits cancellation from the `pending` state **only** — pre-activation secrets (`reserved`, `awaiting_acceptance`) cannot be cancelled and exit via the commit-timeout instead. **Timing constraint**: cancellation is permitted from activation until `reveal_start_block`; once the reveal window opens the secret proceeds to normal settlement. **Transaction signing** must match the original creator's address for security.

**Cancellation is a paid exit.** All locked bonds are returned in full, and the reward pool settles **pro-rata by distance travelled**: each active guardian is paid `rate × elapsed × bump` where `elapsed = cancel_block − commit_deadline` (floor 0), plus the pool's reveal leg accruing on the same clock, and the creator is refunded the unearned remainder. Each active guardian is *also* paid its full accept slice from `A`, whenever the cancellation lands: accepting was work, it was done, and the creator's later change of mind cannot make it unpaid. Cancelling immediately after activation but still inside the commit phase (`elapsed = 0`) therefore refunds the whole pool while still reimbursing every acceptance. At the other extreme — one block before the window opens, the last cancellable moment — guardians are paid the full pre-window hold, and the creator is always refunded at least the window span's slice of the pool (the window itself is never earned on a cancelled secret).

#### Economic Impact
**Guardians are always compensated** for every block a bond stays locked after commit — a creator can never lock guardian capital for free (the "paid hold" invariant). **Immediate fund release** returns the unearned pool portion to the creator and unlocks every guardian bond. **Protocol efficiency** prevents resource waste by allowing early cancellation of unfeasible secrets.

#### What Happens Next
Cancelled secrets transition to permanent `cancelled` state and cannot be reactivated. All guardian assignments are marked as cancelled, and appropriate refunds are processed immediately. The protocol emits cancellation events for transparency and audit purposes.

> **📋 For detailed field specifications, validation rules, examples, and technical implementation details, see [MsgUserCancelSecret in operations.md](operations.md#msgcancelsecret).**

### 👮 Guardian Reveal Stage

The guardian reveal stage is where assigned guardians submit their decrypted secret shares during the designated reveal window to enable secret reconstruction. **Before the reveal window opens**, secrets remain in `pending` state during a waiting period where guardians prepare for their reveal obligations. During the reveal window, guardians can submit their shares at any time without coordination restrictions - the protocol relies on economic incentives and slashing penalties rather than timing constraints to ensure proper behaviour. Each submitted share undergoes HMAC verification to confirm authenticity and detect any protocol violations for slashing purposes (see [Secret Economics & Slashing](#secret-economics--slashing)).

```go
type MsgGuardianRevealShare struct {
    Guardian        string    // Guardian's address (transaction signer)
    SecretId        string    // Secret identifier from previous phases
    DecryptedShare  []byte    // The actual share data after decryption (contains intrinsic SSS ID)
}
```

#### Reveal Requirements
**Window timing** restricts submissions to the designated reveal period `reveal_start_block ≤ height ≤ reveal_end_block` (both bounds inclusive — a reveal in the window's final block is valid and settles normally). **HMAC verification** confirms share authenticity against Phase 2 commitments and enables slashing detection for protocol violations. **Assignment verification** ensures only guardians who explicitly accepted assignments in Phase 3 can participate. **One reveal per guardian** prevents duplicate submissions from the same guardian address.

#### Security Measures
**Early reveal slashing** penalises guardians who reveal shares before the window opens (see [Early-Reveal Reporting](#early-reveal-reporting) for details). **HMAC-based authenticity** ensures submitted shares match the encrypted data distributed in Phase 2. **Economic incentives** align guardian behaviour through reward distribution for proper reveals and slashing for violations. **No coordination required** - guardians can reveal independently at any time during the window without timing constraints.

#### What Happens Next
Valid share submissions contribute toward threshold requirements for successful secret reconstruction, and each revealing guardian earns its bond back plus a share of the reward pool at settlement — regardless of whether the threshold is ultimately met. Recipients can reconstruct secrets client-side once threshold shares are available, completing the time-lock cycle.

> **📋 For detailed field specifications, validation rules, examples, and technical implementation details, see [MsgGuardianRevealShare in operations.md](operations.md#msgrevealshare-reveal-phase).**

### Window Closure & Settlement

Automatic end-block processing - no direct user operation. Settlement is **threshold-independent**: whether the threshold was met determines only whether the recipient can reconstruct the secret, and does not branch the payments.

**Logic Applied** (one settlement event, in the EndBlock of block `reveal_end_block + 1` — the first block after the inclusive window):
- Guardians who **revealed correctly** (HMAC-verified) have their bonds returned and split the reward pool `P` equally
- **No-reveal** guardians are slashed: 40 % of their bond burned, 10 % to the creator, 50 % returned; excluded from `P`
- **Early-slashed** guardians (bond already gone at report time) are excluded from `P`
- If **no** guardian revealed, `P` is refunded to the creator; otherwise the creator receives no pool refund
- The secret transitions to "revealed" (threshold met) or "failed" (threshold not met) — a cryptographic outcome only

**Key Edge Cases**:
- A failed guardian's `P` share goes to the revealers, not back to the creator — fewer revealers means a larger share each
- Integer-division dust from any split is burned
- Client-side reconstruction possible once threshold shares available

See [Secret Economics & Slashing](#secret-economics--slashing) for the full model.

### Terminal Secret Retention & Tombstones

Live state scales with **active** secrets, not every secret ever created.
Once a secret reaches a terminal state (`revealed`, `cancelled`, `failed`),
retention runs in two stages:

- **Stage 1 — at the terminal transition**: the encrypted share records
  (including rejected assignments'), the assignment statuses, and the
  early-reveal slash marks are deleted — settlement provably reads none of
  them after the transition. The **reconstruction inputs survive**: the
  reveal records and the payload ciphertext `C`.
- **Stage 2 — at `terminal_at + RetentionBlocks`** (~6 months): the slim
  record, reveal records, payload ciphertext, creator-index entry and hint
  entry are deleted, replaced by a permanent **seven-field tombstone**:

  ```
  record_digest      SHA256 of the canonical TerminalSecretRecord
                     (slim record ‖ reveals sorted by guardian address ‖ SHA256(C))
  final_state        revealed | failed | cancelled
  terminal_at        height of the terminal transition
  pruned_at          height Stage 2 executed — locates the archival event
  creator            attribution without an archive
  created_at         lifetime; locates the distribution tx in block history
  secret_commitment  SHA256(C_r) — verify a reconstructed payload directly
  ```

  Stage 2 also emits the **archival event** (`secret_pruned`) carrying the
  full canonical record: indexers that retain events hold a complete,
  self-verifying archive of every pruned secret at zero state cost.

**The retention window is the availability contract — in both directions.**
Nothing extends it (there is no exemption or acknowledgement mechanism) and
nothing truncates it. Recipients must fetch the reveal records and `C` and
reconstruct within the window; after it, only archives can help — anyone
holding an archived canonical record proves its authenticity by hashing it
against the tombstone. The client contract: **scan for hints at least every
six months** — a recipient at that cadence always sees a secret's hint while
its reconstruction inputs still exist.

**Query behaviour after pruning**: `Query/Secret`, `Query/SecretPayload` and
`Query/SecretReveals` return `NotFound` exactly as for a never-existent id;
`Query/SecretTombstone` distinguishes "pruned" from "never existed".
`Query/SecretsByCreator` and `Query/HintsSince` no longer include the
secret — creator-scoped history beyond the window is indexer territory (each
tombstone still names its `creator` for standalone attribution).

Pruning work is bounded (a per-block cap with carry-over), scheduled by a
due-height queue exactly like commit and settlement processing, and prunes
are deterministic consensus operations.

---

## Secret Economics & Slashing

> For the derivation behind these values — every flow of VEIL, the curves
> that shape them, worked invoices and the reasoning for each choice — see
> [ECONOMICS.md](ECONOMICS.md). This section remains normative; that page
> explains it.

Timeflare uses a **bonded guardian economics** model: guardians post returnable collateral (**bonds**) per secret they accept, and creators pay a formula-derived reward priced by a single security dial. Accountability is per-obligation — every accepted secret carries its own bond, sized so that slashing is always a percentage of collateral that is guaranteed to be present. There is no shared stake pool to drain and no global stake floor to fall below.

> **Design authority**: this section is the settled specification lifted from
> [planning/done/DONE_BONDED_GUARDIAN_ECONOMICS_PLAN.md](planning/done/DONE_BONDED_GUARDIAN_ECONOMICS_PLAN.md),
> which records the full design rationale and decision log.

### Actors and Quantities

- **Guardian** — an address that has paid the entry fee `F` and may store shares and reveal them. Maintains a deposited **float** of VEIL, partitioned into `locked` and `unlocked`, and carries a live **bond multiplier `k`** — a per-guardian reputation value that prices its bonds (see below).
- **Creator** — an address that publishes a secret and funds its reward pool.
- **Reporter** — any address that submits valid evidence of an early reveal.

Three distinct VEIL quantities, with different owners and fates:

| Quantity | Owner | Lifetime | Fate |
|----------|-------|----------|------|
| Entry fee `F` | Guardian | Paid once at registration | **Routed through the fee collector's 90/10 split** like every validator-bound flow — 900 VEIL allocated to validator rewards, 100 VEIL burned (the one-pipe ruling: no flow is exempt) |
| Bond `B` | Guardian | Frozen per-guardian at selection, locked at acceptance, released at settlement | **Returned** if honest; **slashed** if not |
| Reward pool `P` | Creator | Funded at publication, distributed at settlement or cancellation | Split to revealers at settlement; on cancellation paid pro-rata for blocks guarded (remainder refunded); refunded in full on commit-timeout or if no one reveals |
| Accept fees `A` | Creator | Escrowed at publication **apart from `P`**, distributed at the terminal state | One slice to each guardian that did the job asked of it — revealers at settlement, acceptors on a cancelled or never-activated secret; every unearned slice refunded to the creator |

### Economic Parameters

Base constants (**immutable** — compile-time values in `x/secrets/types/constants.go`; ruled July 2026, Position A): `rate` (master reward price), `F`, `max_tier` (the `bump` ceiling), the `k` mechanism (range, start value, and adjustment multipliers — see below), the per-guardian concurrency cap, the per-violation bond distribution, and `H` (max reveal horizon, kept equal to the maximum guardian availability window). `rate` is the master knob — reward and bond both scale with it; `k` sets each guardian's bond-to-reward ratio. `bump` is fixed-point with 2 decimal places, `bump ∈ [1, max_tier]`; `k` reuses the same hundredths fixed-point scale. (`bond_multiplier` — the former flat bond horizon — is retired; the bond is now anchored to the secret's own duration. Decision record: [planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md).)

**There is no parameter governance.** No `Params` state, no `MsgUpdateParams`, and no vote can move an economic constant — immutability is a product feature: a guardian underwriting a year-long secret is underwriting it against economics that cannot float beneath it. The only retuning path is a rare, coordinated **software upgrade** ([docs/upgrades.md](upgrades.md)), rehearsed on testnets before it is ever needed on mainnet. Upgrades never re-price in-flight secrets: every obligation is snapshotted on the secret at creation (`reward_pool`, `bond_amount`), settlement reads only stored values, and the cancellation wage derives from the stored pool — a rate retune affects future secrets only. `x/gov` remains wired **solely to coordinate software upgrades**; with no parameters and no treasury (`community_tax = 0`), there is nothing else it can govern. (Decision record: [planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md](planning/done/DONE_PARAMS_GOVERNANCE_DECISION_PLAN.md).)

Derived quantities (computed, never stored as literals):

```
B (per guardian g) = rate × distance × bump × k_g
P_time             = rate × distance × max_shares × bump
P                  = max_shares × F_reveal + P_time
A                  = max_shares × F_accept
F_accept           = MinRequiredFee(guardian_accept_gas)     (120,000 gas → 0.012 VEIL)
F_reveal           = MinRequiredFee(guardian_reveal_gas)     (130,000 gas → 0.013 VEIL)
```

**The creator funds the work as well as the time.** A guardian's revenue used
to scale with distance while its cost — two transactions, `accept` and
`reveal` — did not, so every short secret settled the guardian at a loss: at
the devnet's shapes a guardian earned ~439 uveil against ~22,900 uveil of gas.
`F_accept` and `F_reveal` close that gap. They are **gas-denominated**, priced
by the same `MinRequiredFee` device as the creation-fee floor, so they are
observations of the protocol's own code path rather than a second dial for
creators — `bump` remains the only thing a creator chooses, and it multiplies
the time component only, because gas does not get more expensive when more
security is bought. The guardian is never out of pocket for completing the job
at any distance the protocol permits, down to a single block.

**`A` is escrowed apart from `P`.** The two are earned by different acts and
settle on different rules: `A` is owed for *accepting*, `P` for seeing the job
through. Keeping them separate is what lets every terminal-state payout derive
from stored values alone (see [Terminal-state disposition](#terminal-state-disposition)).

The bond is anchored to what the creator is actually paying: a guardian's bond is exactly `k_g` times the pool's per-slot slice (`P ÷ max_shares = rate × distance × bump`), so the collusion-cost-to-reward ratio is a deliberate, duration-independent constant (at least `threshold × k` — a guardian's realised reward share `P ÷ revealers` can only exceed the per-slot slice, never fall below it) rather than an accident of hold length, and total bonded capital scales with real usage instead of hitting a flat per-secret ceiling. There is **no floor and no ceiling on `B` itself** — the formula's own bounds cap it (at `rate = 1 uveil`, the maximum possible bond — max distance, max `bump`, max `k` — is ≈ 1,262 VEIL), and a short secret's proportionally small bond is intentional: the constant ratio is the security property, and a short secret whose *external* value is high buys more deterrence via `bump`.

`distance` is measured from `commit_deadline` to settlement, which is `reveal_end_block + 1` (the reveal window is inclusive of its end block, so bonds stay locked through block `reveal_end_block` and release the block after — the paid-hold invariant prices every locked block). It is bounded by the reveal horizon: `reveal_end_block ≤ created_at + H`, so `distance ≤ H + 1 − CommitTimeoutBlocks` — a fixed bound of `H − 49`, strictly below `H`, because the commit window is the same 50 blocks on every secret. `max_shares` is the band ceiling — the count selected, distributed to, and priced for. The pool is **fixed at `P(max_shares)`**: slots left unfilled at activation are never refunded; they enlarge the split for whoever does reveal (see Settlement). The creator's trade for a wide band is redundancy, not price.

**v1 values** (provisional, ~6-second blocks assumed):

| Constant | Value |
|----------|-------|
| `rate` | 0.000001 VEIL / guardian / block (1 uveil) |
| `guardian_accept_gas` / `guardian_reveal_gas` | 120,000 / 130,000 gas → `F_accept` 0.012 VEIL, `F_reveal` 0.013 VEIL at the 0.1 uveil/gas floor |
| `F` | 1,000 VEIL (routed through the 90/10 fee split) |
| `max_tier` | 10 (so `bump ∈ [1.00, 10.00]`) |
| `k` range | 4.00 – 24.00 (hundredths fixed-point, 400–2400) |
| `k` at registration | 4.00 (the floor) |
| `k` slash multiplier | × 1.26 (`k′ = min(2400, k × 126 ÷ 100)`, truncating) |
| `k` reveal multiplier | × 0.963 (`k′ = max(400, k × 963 ÷ 1000)`, truncating) |
| concurrency cap | 100 active bonds per guardian |
| no-reveal bond split | 40 % burn / 10 % creator / 50 % returned |
| early-reveal bond split | 40 % burn / 10 % creator / 50 % reporter / 0 % returned |
| `H` | 5,256,000 blocks (≈ 1 year — equals the max guardian availability window) |

### The Per-Guardian Bond Multiplier `k`

Each guardian carries a live bond multiplier `k ∈ [4.00, 24.00]`, stored in hundredths (the same fixed-point scale as `bump`), starting at the **floor of 4.00** on registration. `k` is a recent-history reputation signal, adjusted on each individual event:

- **On every slash** (no-reveal at settlement, or early-reveal at report time): `k′ = min(2400, k × 126 ÷ 100)` — truncating integer division on the hundredths representation, clamped at the ceiling. Eight consecutive slashes climb the full range: `4.00 → 5.04 → 6.35 → 8.00 → 10.08 → 12.70 → 16.00 → 20.16 → 24.00`.
- **On every correct on-chain reveal** (accepted by `MsgGuardianRevealShare`): `k′ = max(400, k × 963 ÷ 1000)` — truncating, clamped at the floor. Recovery is deliberately ~6× slower than the climb: one slash step takes about six reveal steps to unwind, and full recovery from the ceiling takes ~48 consecutive correct reveals.

The adjustment functions are normative in exactly this integer form (multiply, truncating divide, clamp) and live in `x/secrets/types` — chain, tests, and tooling all call the same code. `k` is **per-guardian, never network-wide**: a guardian's `k` responds only to its own history, so no adversary can raise other guardians' bond prices by deliberately getting slashed. And `k` affects only what a guardian must bond — never selection probability, never reward share. At realistic failure rates most guardians sit at or near the floor, spiking after an incident and decaying back over the next handful of reveals; the ceiling is reachable only through sustained failure streaks, which is precisely the behaviour that should price a guardian out of long-duration work.

**Timing (normative):** the `k` used for a secret is the guardian's `k` at **selection height**, frozen on the secret record (see [Guardian Selection](#guardian-selection-normative)); `k` adjustments apply at the **event** that triggers them (reveal acceptance, no-reveal settlement, early-reveal report) and affect only future selections. A guardian slashed between selection and acceptance keeps the cheaper frozen bond for that one secret — a window bounded by the commit deadline (≤ 200 blocks) and accepted by design.

**Known accepted risks** (decision record in [planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md](planning/done/DONE_DYNAMIC_BOND_ECONOMICS_PLAN.md) §7): *reputation farming* — behaving impeccably at the floor, then defecting once at the cheapest bond; and *re-registration whitewashing* — a high-`k` guardian withdrawing its float and re-registering fresh at `k = 4.00`, making `F` the effective price of a reputation reset. Both are accepted for v1; retuning is a software upgrade (Position A).

### Guardian Lifecycle and the Float

1. **Register.** Pay `F` (routed through the 90/10 fee split). The address becomes a guardian with `k = 4.00`; the real economic barrier is the working float.
2. **Fund.** Deposit VEIL into the float. Withdrawals are permitted only from `unlocked`.
3. **Be selected.** For a given secret a guardian is a candidate only while `unlocked ≥ B_g` (its *own* bond for that secret, priced by its live `k`) and its active-bond count is below the concurrency cap (`100`). Selection freezes `B_g` on the secret. Selection is otherwise protocol-controlled and verifiably random, and is advisory — acceptance (step 4) is the hard gate.
4. **Accept** (`MsgGuardianConfirmShares`). Locks the frozen `B_g` from `unlocked` and increments the active-bond count. Acceptance **fails** if `unlocked < B_g` or the count has meanwhile reached the cap. Acceptances accumulate up to `max_shares` until `commit_deadline` — every valid confirmation joins the roster; there is no first-`n` race and no confirmation is turned away while a slot remains.
5. **Settle.** At settlement, `B_g` is returned, slashed, or refunded per the rules below, the corresponding amount moves from `locked` back to `unlocked` (or out of the float), and the active-bond count decrements. Every reveal and every slash also moves the guardian's `k` (see above).

There is deliberately **no eviction or deregistration** on any violation — a repeat offender simply keeps burning its own bonds and forfeiting rewards, at an ever-higher `k`. Bonds are per-secret and slashing is percentage-based, so a guardian's capital limits it to roughly `float ÷ B` concurrent secrets; on top of that capital limit sits the hard **concurrency cap of 100 active bonds**, an eligibility gate that binds only the largest floats (a whale-concentration bound, not a typical guardian's first constraint).

### Secret Lifecycle and Money Movement

Timeline (all offsets fixed at publication; `created_at < commit_deadline < reveal_start_block < reveal_end_block`):

1. **Publish** (Phase 1). The creator funds `P` up front, computed by formula from its dials (`bump`, `distance`, `max_shares`). The reward is exact — no tipping, no arbitrary top-ups.
2. **Distribute & accept** (Phases 2–3). Guardians accept and lock bonds up to `commit_deadline`, where the roster finalises: `≥ min_shares` accepted → `pending` with exactly the accepted set, else `failed` with bonds returned and the pool refunded.
3. **Hold**. Between `commit_deadline` and `reveal_start_block`, bonds remain locked. The creator may cancel at any point before `reveal_start_block`.
4. **Reveal window** (`reveal_start_block` … `reveal_end_block`, both inclusive). Guardians reveal shares. Cancellation is no longer possible.
5. **Settlement** (one EndBlock event, in block `reveal_end_block + 1`).

### Settlement (Threshold-Independent)

A guardian has **revealed correctly** if and only if its revealed share verifies against the HMAC commitment recorded at distribution; anything else — an invalid share or no submission — is a no-reveal.

Settlement is **threshold-independent**: whether the reveal threshold was met determines only whether the *recipient can reconstruct the secret* — a cryptographic outcome — and does not branch the payments. At settlement, for each guardian:

- **Revealed correctly** → bond `B_g` (the guardian's own frozen amount) returned to `unlocked`; guardian included in the split of `P`. (The guardian's `k` was already stepped down at each accepted `MsgGuardianRevealShare`.)
- **No-reveal** → of the bond `B_g`: 40 % burned, 10 % to the creator, 50 % returned; guardian excluded from `P`; the guardian's `k` steps up (× 1.26, ceiling-clamped).
- **Early-reveal, proven** → handled immediately on report (see below), and excluded from `P` here.

The reward pool `P` is split equally among the included (revealing) guardians. If **no** guardian revealed, `P` is refunded to the creator. `P` is never partially refunded on a per-guardian failure — a failed guardian's share goes to the revealers, making revealing the dominant strategy regardless of what other guardians do. Integer-division dust from any split (pool or bond) is **burned**.

The accept fees `A` settle on the same test: one slice of `A ÷ max_shares` to each **revealing** guardian, and every slice nobody earned — an unfilled band slot, or a guardian that accepted and then no-showed — refunded to the creator. A no-show forfeits its accept slice along with everything else: it is being slashed for non-performance, and the accept fee is a payment for work, not an attendance allowance. `A` divides exactly by `max_shares` by construction, so this split never produces dust.

#### Terminal-state disposition

**Nothing is disbursed before the secret reaches a terminal state.** Both escrowed amounts stay whole from `MsgUserRequestGuardians` until the end, which is what keeps `reward_pool` and `accept_fees` equal to the module escrow actually held for that secret at every height — every payout therefore reads a real balance, and a later retune of the gas constants can never re-price a secret already in flight.

| Terminal state | Each guardian receives | Creator refunded |
|---|---|---|
| **revealed** (≥ 1 revealer) | revealers split `P` equally, plus one slice of `A` each | slices of `A` nobody earned |
| **failed** — commit expired below `min_shares` | one slice of `A` to each guardian that accepted | `P` in full, plus the unearned `A` slices |
| **failed** — settled with no revealers | nothing | `P` and `A` in full |
| **cancelled** by the creator | one slice of `A`, plus the pro-rata wage | the unearned remainder of `P`, and any unearned `A` |

The asymmetry between the two `failed` rows is deliberate and is about blame, not bookkeeping. A guardian that accepted a secret which **never activated** did exactly what was asked of it and the roster simply did not fill — it keeps its accept slice, and the creator's refund is reduced accordingly. A guardian that accepted and then **no-showed** is at fault, and keeps nothing.

**There is no refund-on-failure for the creator**: if a secret fails because too few revealed, the pool is still paid to whoever did reveal. The creator's downside protection is the slashed no-show bonds (10 % of each), and the creator bears the risk of choosing too high a threshold or too few reliable guardians.

**Late submission attempts** at `reveal_end_block + 1` or later are rejected as invalid transactions within the block, before that block's EndBlock settles — so the revealer set is final when settlement reads it, with no gap and no race.

**Settlement trigger — due-height queues.** Settlement is *scheduled*, not scanned for. At publication the secret is registered in two due-height queues: a commit entry due at `commit_deadline + 1` and a settlement entry due at `reveal_end_block + 1`. Each EndBlock drains only the entries whose due height is `≤` the current height — because consensus executes every height sequentially this fires at exactly the due height (`==` in practice), while the `≤` form is a self-healing safety net for state whose due height predates it (genesis import, migrations), which settles on the next block instead of being stranded. Entries are removed when processed and when obsoleted (the commit entry always fires at the deadline — it is the finalisation trigger, activating or failing the secret; cancellation and commit-timeout retire the settlement entry). Consequences: **a settled, cancelled, or failed secret is never re-examined by consensus**, an idle block does near-zero settlement work, and validators never re-evaluate thresholds on completed secrets.

**Settlement failure handling — quarantine, never halt (ruled July 2026).** There are no *transient* errors on the settlement and commit-expiry paths: both are pure deterministic computation over committed state with exact-amount bond accounting (each guardian's `B_g` is frozen on the secret record at selection; locks, unlocks and slash splits use it verbatim), so an error there is an **assertion** that a bug elsewhere has already corrupted the books — and every node fails identically. The failure model is therefore fail-safe rather than fail-open:

- **All-or-nothing per secret**: each secret's settlement (and each commit expiry) runs inside a per-secret cache context; its state writes — including the queue dequeue — commit only if every step succeeded. There is never a half-settled secret, and retries are trivially safe.
- **Retain and retry, never panic**: on failure the queue entry is retained and re-attempted every block. An EndBlock panic would halt every node deterministically, converting any attacker-reachable trigger into a chain-wide DoS; a stalled settlement instead leaves the funds **locked, not lost**, in module escrow, with a blast radius of one secret and no effect on queue neighbours.
- **Alarm from the first failure**: because these failures are deterministic, retry alone would be a silent liveness failure — the alarm *is* the detection mechanism. Every failed attempt emits a `settlement_stalled` event (secret id, failing operation — `settlement` or `commit_expiry` — and the error), logs at error level, and bumps a node-local telemetry counter. There is no N-retry escalation threshold: no retry count distinguishes anything on a deterministic path, and no stronger on-chain action exists to escalate to.
- **Self-recovery**: when an upgraded binary ships the underlying fix, the pending retry completes on the next block and the books balance with zero migration work.

The same discipline applies on the message paths: a bond-release or payout failure inside `MsgUserCancelSecret` fails the whole transaction (atomic abort via the transaction cache) rather than half-cancelling.

### Early-Reveal Reporting

Early reveal — leaking share data before the reveal window opens — is the protocol's most serious violation. It requires community reporting with cryptographic evidence via `MsgSlashGuardian`:

```go
MsgSlashGuardian {
    guardian_address: string  // Guardian being reported
    reporter_address: string  // Reporter receiving bounty
    evidence: bytes           // Share data revealed early
    secret_id: string         // Secret the evidence relates to
    reason: string            // Human-readable explanation
}
```

The protocol verifies the evidence against the guardian's stored HMAC (`HMAC(ShareData || GuardianAddress || SecretID) == StoredHMAC`). On a valid report, **immediately**: the full bond `B_g` (the guardian's own frozen amount for this secret) is slashed — 40 % burned, 10 % to the creator, 50 % to the reporter, 0 % returned — the guardian's `k` steps up (× 1.26, ceiling-clamped), and the guardian is marked for exclusion from `P` at settlement. Slashing is immediate rather than deferred because reveal windows can be years away; deferring the bounty would gut the monitoring incentive.

Reports are accepted **only before `reveal_start_block`**. Once the reveal window opens the shares are due to be public, so evidence of an "early" reveal is moot — late reports are rejected. (By construction, a bond can therefore never be slashed after it has been released.) Each guardian can be slashed at most once per secret, and a guardian who has already revealed on-chain cannot be reported.

**Honesty note**: because anyone can be the reporter, a leaking guardian can self-report through a second address and recapture the 50 % bounty on its own bond. The *guaranteed* early-reveal loss is therefore the 40 % burn, the 10 % creator slice, and the forfeited reward share. This is inherent to reporter-bounty designs and is accepted. Early-reveal deterrence also depends on **detection** — an undetected off-chain leak forfeits nothing; a large bond is not an absolute guarantee. The reconstruction **threshold remains the primary time-lock guarantee**; `bump` raises the ante.

### Cancellation and No-Fault Refunds

Two paths end a secret before its reveal window; both return every *honest* bond in full:

- **Commit-timeout** (no fault): fewer than `min_shares` confirmations by `commit_deadline` — the band's floor must be met to commit; `threshold` is only the reconstruction minimum. Accepted bonds returned in full; `P` refunded in full.
- **Creator cancellation** (paid exit, permitted only from `pending` — post-activation — and before `reveal_start_block`): all bonds returned in full; the pool settles **pro-rata by distance travelled**, with the unearned remainder refunded to the creator:

  ```
  elapsed             = cancel_block − commit_deadline   (floor 0)
  per-guardian payout = rate × elapsed × bump            (per honest active guardian;
                        derived on-chain from the STORED pool as
                        P × elapsed ÷ (distance × max_shares) — numerically
                        identical, and immune to any future rate retune)
  creator refund      = P − Σ payouts
  ```

  The `max_shares` denominator keeps the per-guardian-per-block wage constant regardless of how many accepted; the unearned remainder refunded to the creator therefore includes any unfilled slots' portion. (Deliberate asymmetry with settlement, where unfilled slots enrich revealers — cancellation is a refund path, settlement a reward path.)

  Cancelling immediately after activation (`elapsed = 0`) refunds everything; at the last cancellable moment (one block before the window opens) guardians are paid the full pre-window hold, with the window span's slice always refunded to the creator. Cancellation is never a free way to lock guardian capital — and it does not exist pre-activation (ruled July 2026): a creator who abandons the commit phase waits out the commit-timeout, which refunds the pool in full.

**Early-slashed guardians are excluded from both paths' guardian-side flows.** A guardian already slashed for an early reveal has no bond left to release (it was fully deducted at report time) and **earns no cancellation wage** — its guarding was breached, so its would-be slice falls into the unearned remainder and flows to the **creator** via the refund arithmetic above. This mirrors settlement's exclusion rule, with each path's forfeited value following that path's default remainder flow: settlement → revealers (reveal-dominance incentive), cancellation → creator (unearned-remainder principle). The slice does not go to the reporter (already compensated by the 50 % bond bounty at report time) and is not burned (the pool is the creator's money; burning it would punish the leak's victim). The self-dealing bound holds throughout: a creator acting as reporter nets at most 10 % + 50 % of the slashed bond plus its own unearned wage back — the 40 % burn always stands.

### Recipient Rebate

A secret that reaches `revealed` credits its **recipient** a rebate on what the
creator irrecoverably spent to send it. This is the protocol's only distribution
mechanism: no faucet, no allowlist, no off-chain service, and no key over the
funds. The creator's spend is what authorises the rebate; the recipient is the
beneficiary.

**Trigger and amount.** At `reveal_end_block + 1`, in the same settlement that
pays revealing guardians, each secret reaching `revealed` is credited:

```
S       = P paid to revealers + accept slices paid to revealers
rebate  = min(RebateRatioPercent × S ÷ 100, allowance ÷ n)
```

- `S` is the **irrecoverable** spend. `revealed` is the earliest state at which
  it is irrecoverable: a creator who cancels immediately after activation is
  refunded essentially the whole pool, so crediting at creation or activation
  would be farmable for the price of a creation fee. The creation fee itself is
  excluded from `S` — it is not carried on the secret record, and excluding it
  understates the spend, which only widens the margin below.
- `n` is the number of secrets settling at this height. They share the
  allowance equally; a secret settling `failed` is counted but credited
  nothing, and its share is not redistributed.
- A credited amount below `RebateDustFloor` is not credited at all — a rebate
  must be worth more than the transaction that collects it.

**Farming is a loss by construction.** Manufacturing a rebate means paying `S`
to receive at most `S × 30%`. The only way to recapture the rest is to *be* a
selected guardian, and selection is protocol-controlled hash sortition over the
eligible set, bought with an entry fee, a float and slashing exposure. The
inequality holds at any token price, which is what lets one mechanism serve
every network.

**The allowance and the pool.** Rebates are paid from the **rebate pool**, a
keyless module account holding 70 % of supply from genesis (see
[Genesis Pool Allocations](#-genesis-pool-allocations)). It has no controlling
key: it can only ever be spent by this formula, and it can never be topped up.
The spendable allowance accrues per block from the pool's own balance:

```
per-block accrual = pool balance ÷ RebateAccrualDivisor
allowance cap     = RebateBurstBlocks × per-block accrual
```

Accrual is lazy — computed from the height of the last settlement that consumed
it, so an idle block does no work. Unclaimed allowance accumulates up to the
cap, which is what lets a lone recipient receive a full rebate rather than one
block's accrual, and lets a cluster of simultaneous settlements be paid at once
without an idle stretch becoming a drainable lump.

Because the accrual is proportional to the balance, **fully claimed the pool
decays by 10 % of its remaining balance per year** — the fastest drain the
protocol can produce, requiring every block of the year to settle a secret. The
pool therefore never empties: distribution slows asymptotically rather than
stopping at a cliff. At realistic volumes the `30 %` cap binds instead and the
pool declines far more slowly.

**Collection is commit–reveal, in two transactions.** The proof of recipiency is
`z = X25519(a, R)` — the shared value only the recipient's private key can
compute against the hint's stored ephemeral key `R`. It is also a *bearer*
secret: the moment it appears in a transaction it is public, so a rebate paid to
whoever presents `z` could be taken by any observer, a validator most easily of
all. The two steps close that:

1. **Commit** — `MsgRecipientCommitRebate{secret_id, commitment}`, where

   ```
   commitment = SHA256("timeflare/rebate-commit/v1" ‖ z ‖ collector address bytes)
   ```

   The chain stores it opaquely with the height it arrived at. It cannot tell a
   real commitment from a random 32 bytes, and does not need to.

2. **Reveal** — `MsgRecipientCollectRebate{secret_id, z}`, in a **strictly later
   block**, pays the credited amount to the **signer**. The chain requires

   ```
   SHA256("timeflare/detect/v1" ‖ z)[:8] == secret.detection_hint.tag   (recipiency)
   commitment(z, signer) == the signer's stored commitment                (priority)
   ```

An observer who lifts `z` out of a reveal transaction has no commitment for it
and cannot backdate one, so the rebate can only be paid to the address that
committed before the proof was public. Committing costs nothing but a
transaction, binds to exactly one address, and a re-commit simply replaces the
previous one. A spent commitment is deleted with the payment; the rest are swept
when the secret is pruned.

A credited rebate keeps its amount until collected — collecting a month later
pays exactly what collecting immediately would, and no ordering, polling or
automation can enlarge it.

**⚠️ Collecting is public and permanent.** Submitting `z` proves recipiency to
*every* observer, not just to the chain: it links the collecting address to that
secret, and an address collecting on several secrets links those secrets to one
another. This is a deliberate, opt-in exception to the recipient-privacy
property of [Recipient Discovery](#recipient-discovery--detection-hints) — `z`
reveals neither the recipient's long-term key nor anything decryptable, but it
does reveal *that this address received that secret*. A recipient who wants a
secret to stay unlinkable should not collect its rebate, or should collect to a
single-use address.

**The collection window is three months** (`RebateCollectionBlocks`,
1,296,000 blocks) from the settlement that credited the rebate. At the deadline
the rebate is **voided**: its credited amount returns to zero and its
reservation returns to the pool, so adoption funding nobody claimed funds the
next newcomer instead of sitting reserved indefinitely. Voiding is queue-driven
like every other deadline in the module, so an idle block does no work.

⚠️ **This window must always close before pruning.** The proof is checked
against the secret's detection hint, and pruning
(see [Terminal Secret Retention](#terminal-secret-retention--tombstones), 
`RetentionBlocks` = 2,592,000 blocks ≈ 6 months) takes the hint with it — a
collection window outliving retention would promise a rebate the chain has lost
the means to verify. Three months against six leaves the same margin again, and
the implementation clamps the window to the live retention value so a network
running a shorter retention cannot promise more than it can honour.

### Economic Invariants

- **Self-dealing**: the creator's share of any slash is < 100 %.
- **No stranded bonds**: every path a secret can take terminates in a settlement that returns, slashes, or refunds every locked bond. No bond is left locked indefinitely.
- **Percentage slashing**: every penalty is a fraction of the posted bond, so slashing can never fail for insufficient collateral.
- **Paid hold**: once a secret commits, every block a bond stays locked is a block its guardian is earning for — via the pool at settlement, or pro-rata on cancellation. A creator can never lock guardian capital for free.

> **Note on escrow accounting**: per-`(guardian, secret)` bond escrow is deliberate state-management overhead, reversing the earlier "simple removal without state management" principle. The accountability gains justify it.

---

## 4. Security Architecture

### Slashing Resilience Through Redundancy

**Core Principle**: Secrets survive guardian removal through having more shares than threshold, not complex slashing prevention.

```
Shares Model Requirements:
├── Band: threshold ≤ min_shares ≤ max_shares ≤ 32 (max_shares is the SSS total)
├── Gap bound: max_shares − min_shares < threshold (strict)
├── Minimum threshold = 2 (basic SSS requirement)
└── Maximum threshold = 16 (SSS implementation limit)

Example with threshold=5, min_shares=6, max_shares=9:
├── Selection: 9 candidates drawn, each receives a unique SSS share
├── Required for pending: at least 6 acceptances by commit_deadline
├── Active guardians: the exact accepted set (6–9 guardians)
├── Required for reconstruction: 5 unique shares from active guardians
├── Rejection tolerance: up to 3 candidates can decline or lapse safely
├── Gap bound check: 9 − 6 = 3 < 5, so never-confirmed candidates can never reconstruct alone
└── Security: recoverable while 5+ activated guardians remain honest; a 6-guardian activation tolerates only 1 reveal-time no-show, a 9-guardian one tolerates 4
```

**Slashing Policy**:
- **Immediate Justice**: Guardians slashed immediately when early-reveal evidence is provided (before the reveal window opens)
- **Per-Obligation Accountability**: Every accepted secret carries its own bond, tracked in per-`(guardian, secret)` escrow — slashing can never fail for insufficient collateral
- **User Choice**: Secret creators choose risk tolerance via shares count and the `bump` security factor

### State Integrity & Import-Time Validation

The protocol's state-side invariants (no stranded bonds, commit/settlement/prune
queue hygiene, denormalised-counter consistency, payload presence) live in a
single library (`x/secrets/keeper/invariants.go`), and the same sweep runs at
every moment state is at wholesale risk (ruled July 2026):

- **After genesis import** — `InitGenesis` runs the full sweep and **hard-halts
  on any violation**: a genesis known to be inconsistent must never produce
  blocks. Pre-launch there is no legacy state to be gentle with; if migration
  development ever needs a soft mode, it is an explicit dev-only flag, never
  the default.
- **Inside upgrade migrations** at the upgrade height — upgrade handlers call
  the sweep after their state changes.
- **Continuously in the test suites** — every conformance scenario and every
  fuzzer block asserts the library.

The sweep is deliberately **not** part of per-block execution: settlement
failures are already contained by the per-secret cache-commit, and a per-block
sweep would convert any attacker-reachable inconsistency into a chain-wide
halt.

`GenesisState.Validate()` complements the sweep with structural checks on the
genesis document itself: every secret carries a legal FSM state,
`threshold ≤ shares`, well-formed reward-pool and bond coins; every side-store
entry (shares, assignments, reveals, payloads, early-reveal slash marks)
references a stored secret and a registered guardian with no duplicate keys;
the denormalised `accepted_count`/`revealed_count` match the side-stores
(terminal secrets must instead have **no** share/assignment records — Stage 1
retention deleted them); tombstones never collide with live records; and the
secret counter is never below the stored secret count.

### Multi-Layer Encryption Protection

The protocol splits the **key, not the payload** (key-share architecture): the
payload ciphertext is stored on chain exactly once, and guardians custody
34-byte shares of a per-secret private key — share size is independent of
secret size.

```
Layer 1 (inner): Recipient Encryption
├── payload → C_r, encrypted to the recipient's long-lived key
├── Ensures only the final recipient can ever read the plaintext
└── secret_commitment = SHA256(C_r) — any observer can verify a reconstruction

Layer 2 (outer): Per-Secret Key — THE TIME-LOCK
├── C_r → C, encrypted to a fresh single-use X25519 keypair (pk_s, sk_s)
├── C is stored on chain once; pk_s is stored for public fault attribution
└── sk_s is discarded by the creator — it exists ONLY as guardian shares

Layer 3: Shamir Secret Sharing over sk_s (32 bytes)
├── t-of-n threshold over the per-secret PRIVATE key, not the payload
├── Key-share envelope: version(1B) ‖ sss_id(1B) ‖ share(32B) = 34B
└── <t shares reveal nothing (information-theoretic)

Layer 4: Guardian-Specific Encryption
├── Each 34B envelope encrypted to its guardian's public key (~94B)
└── HMAC over the plaintext envelope binds reveals and early-reveal evidence
```

**Normative details.** The bytes split are the raw 32-byte X25519 scalar
exactly as generated (clamping applies at Diffie–Hellman time), so
reconstruction is byte-exact across clients. The share HMAC's key is publicly
derivable (`SHA256("secrets" ‖ secret_id ‖ guardian_address ‖ "hmac_salt")`),
and the tag is `HMAC-SHA256(key, share ‖ guardian_address ‖ secret_id)` — a
salted commitment whose pre-window security rests on the 256 bits of
key-share entropy. All asymmetric encryption layers share one wire format:
`ephemeral_public(32) ‖ nonce(12) ‖ ChaCha20-Poly1305 ciphertext+tag`, with
the AEAD key derived as `SHA256(X25519_shared ‖ "timeflare_encryption")`.
The payload ciphertext `C` is public from the moment of
distribution: its pre-window confidentiality rests entirely on the key shares,
the same threshold assumption as the previous payload-splitting scheme.

**Implementation note — no native code on the consensus path.** HMAC
verification inside message handlers uses a pure-Go implementation (stdlib
`crypto/hmac`); the guardian daemon's share decryption is likewise pure Go.
The Rust crate remains the sole client-side implementation (compiled to WASM
for the SDK). Byte-compatibility between the Go and Rust implementations is
pinned by the shared, append-only vector corpus in `testdata/vectors/`
(`hmac.json`, `encryption.json`, `detection_hint.json`), asserted by both
test suites in CI — a change to either implementation or the corpus re-runs
both, so drift fails on the side that changed.

**What the commitment proves — and does not.** `secret_commitment = SHA256(C_r)`
proves *reconstruction integrity*: anyone can verify the unsealed result is
byte-for-byte what the creator distributed, without the recipient's key. It
deliberately does **not** bind the plaintext — the inner encryption is
randomised, so a third party with a *guess* gains no confirmation oracle.
`pk_s` upgrades creator accountability: a reconstructed `sk_s` that matches
`pk_s` while `C` fails to decrypt (or the commitment fails) is provable
creator fault, not guardian fault. `pk_s` is validated as a usable X25519 key
(32 bytes, not a small-order point) on the same terms as a guardian key — a
small-order `pk_s` would make `C` decryptable by any observer, and while that is
self-inflicted, the check is applied uniformly rather than carved out.

**Large secrets — the envelope pattern.** For payloads above the on-chain cap,
the secret *is* a key: encrypt the blob off-chain (IPFS/Arweave/anywhere
durable), time-lock `key ‖ URI ‖ SHA256(blob)` (a few hundred bytes), and the
recipient verifies the fetched blob against the hash. Unlimited effective
payload size with identical reveal guarantees; this — not a larger cap — is
the protocol's answer to large secrets.

### Recipient Discovery — Detection Hints

The recipient's long-term public key `A` never appears on chain. Each secret
instead carries a **detection hint**, derived client-side at creation:

```
Creation (creator device):            Discovery (recipient device):
  fresh ephemeral keypair (e, R)        for each candidate secret's (R, tag):
  shared = X25519(e, A)                   shared' = X25519(a, R)     // a = recipient private key
  tag = SHA256(domain ‖ shared)[:8]       match ⇔ SHA256(domain ‖ shared')[:8] == tag
  store {version=1, R, tag}; discard e
```

**Normative details.** `domain = "timeflare/detect/v1"` (byte-exact across
implementations; a shared test vector pins the Go and Rust code paths). The
tag is exactly 8 bytes (scan false positives ≈ 2⁻⁶⁴). The hint is validated
by shape only (version 1, 32-byte `R`, 8-byte tag) — the chain cannot and
need not verify it targets anyone.

`R` additionally must be a **usable X25519 public key**, i.e. not a small-order
point. This is a shape check, not content verification: against a small-order
`R` every recipient computes the *same* all-zero `shared'`, so one hint would
match **every** recipient and the 2⁻⁶⁴ false-positive bound above would become
1. Rejecting it does not weaken the deliberate unverifiability of hint content —
a creator wanting no discovery still supplies random bytes, which are
small-order with probability ≈ 2⁻²⁵⁰.

**Unlinkability.** Testing a hint requires recomputing `shared`, and both
routes to it contain a private key (`e` is destroyed at creation; `a` is the
recipient's). Deriving `shared` from the public halves is the computational
Diffie–Hellman problem, so to any observer every `(R, tag)` is random noise,
fresh per secret: no two secrets are linkable, and there is no per-recipient
join key anywhere in state or transaction history. A leaked recipient private
key allows retroactive scanning — but it also decrypts the payloads, so the
hint degrades exactly in step with total key compromise, never before it.

**The hint is mandatory; discovery is not.** A creator who wants no
discovery supplies **random bytes** — indistinguishable from a real hint, so
the field's presence reveals nothing. (An optional field would leak one bit —
"this recipient is enrolled" — and partition the anonymity set.)

**Scanning is incremental.** Hints are written once at creation and served in
creation order by `Query/HintsSince(since_height)` (compact
`(secret_id, created_at, hint)` records). A poller scans each hint exactly
once and resumes from its last height cursor — steady-state cost tracks the
chain's creation rate (~one X25519 operation per new secret), not its size.
Duplicate testing across resumes is harmless. Only the private-key holder can
scan; there is no third-party or indexed equivalent, by design.

**Scan at least every six months.** A pruned secret's hint is deleted with it
(see Terminal Secret Retention), so discovery is bounded by the retention
window by design: a recipient scanning at that cadence always sees a secret's
hint while its reconstruction inputs still exist; a later one must turn to
archives for both discovery and recovery.

**Reaching recipients without on-chain identity.** There is deliberately no
recipient reference or metadata field — creator-supplied identity claims
about third parties on an immutable ledger were removed as a PII channel.
The supported patterns:
- **Enrolled recipient**: scans hints (above), or is told the secret ID
  directly by the creator.
- **Un-enrolled recipient** (e.g. an inheritance): the creator generates a
  keypair *for* them and delivers the private key + secret ID out-of-band
  (estate, sealed letter) — the same channel any notification would need.
- **Public disclosure**: encrypt to a well-known keypair whose private half
  is published (the bulletin-board pattern) — world-readable at reveal by
  convention, with no protocol difference.

### Common Attack Vectors & Mitigations

1. **Guardian Collusion**
   - **Attack**: Guardians coordinate to reveal early
   - **Mitigation**: Full bond forfeiture + forfeited reward share, both scaled by the creator's `bump`; reporter bounty (50 % of bond) incentivises defection
   - **Result**: Each colluder risks its entire bond plus its payday; the threshold dictates how many must be subverted. The protocol does not claim to price out collusion on arbitrarily valuable secrets — the threshold remains the primary time-lock guarantee

2. **Sybil Attacks**  
   - **Attack**: Single entity registers multiple guardians
   - **Mitigation**: 1,000 VEIL entry fee per address (routed through the 90/10 fee split), plus the working float each guardian must post to accept any secret. Fresh addresses gain no `k` advantage — every registrant starts at the floor (4.00), the minimum any guardian can hold
   - **Result**: Economically prohibitive at scale

3. **Griefing Attacks**
   - **Attack**: Guardian accepts assignment but never reveals
   - **Mitigation**: Redundancy factor + no-reveal bond slash (40 % burned / 10 % to creator) + forfeited reward share redistributed to revealers
   - **Result**: Other guardians meet threshold and earn more each; bad actor penalised

4. **Time Manipulation**
   - **Attack**: Validators manipulate block times
   - **Mitigation**: Tendermint BFT consensus requires ⅔ validator agreement
   - **Result**: Consensus-level protection prevents manipulation

5. **Invalid Share Submission** *(accepted risk — deliberately out of scope)*
   - **Attack**: Creator submits shares that do not reconstruct, or reconstruct to junk
   - **Decision**: The protocol does not guarantee content validity — only reconstruction integrity. Guardians are paid by the creator regardless and cannot be slashed over share content (HMACs prove they revealed exactly what they were given); the recipient receiving junk is indistinguishable from the creator encrypting junk, which no share-validity proof could prevent. `secret_commitment` guarantees the reconstructed payload is exactly what the creator committed to; whether that payload is meaningful is the creator's own responsibility.

6. **Share Ownership Transfer** *(ruled out, July 2026 — no such mechanism will exist)*
   - **Proposal considered**: allow a guardian to transfer an in-flight share (and its bond obligation) to another guardian, with compensation forming part of the transaction — as an operator exit path and as the unlock for reveal horizons beyond the availability cap
   - **Why it is unsound — the transfer-and-report attack**: nothing can make the transferor forget the share. Post-transfer, guardian A retains the plaintext with no collateral at stake, and can submit it as early-reveal "evidence" against transferee B through a second address. The evidence verifies — it is the genuine share, and possession-based HMAC evidence cannot distinguish "B leaked it" from "A retained it" — so A collects the transfer payment *plus* 50 % of B's bond as reporter bounty, while B, having done nothing wrong, is fully slashed. Every defence fails: barring the transferor from reporting is one Sybil address away from meaningless; closing reporting for transferred shares removes leak deterrence exactly where it is needed; slashing both parties punishes a provably innocent one. Accepting a transfer is handing the transferor a call option on half your bond — no rational guardian would accept, so the mechanism is self-defeating as well as unsafe
   - **The load-bearing invariant**: possession-based slashing evidence is sound only because exactly one party ever legitimately possesses each share — **one share, one holder, forever**
   - **Result**: long-lived reveals remain cancel-and-recreate cycles (the endorsed first-class pattern); operator exit remains `accepting_secrets = false` plus serving out existing commitments. Transfer does not address key loss either way — the transferor must be able to decrypt in order to hand off

7. **Pre-Acceptance Share Exposure** *(accepted risk, ruled July 2026 — mitigated by client convention)*
   - **Exposure**: Phase 2 hands a decryptable share to every one of the `max_shares` selected candidates *before* any bond locks — acceptance means "I decrypted my share and verified its HMAC", so material must precede the bond. An attacker who gets `≥ threshold` of its own guardians selected can reconstruct at distribution time with no collateral at stake.
   - **Bound**: the gap constraint `max_shares − min_shares < threshold` guarantees that, on any secret that activates, the candidates who received a share but never bonded are always a sub-threshold set — they can never reconstruct on their own. The bound is conditional on activation: a secret that fails below `min_shares` leaves up to `max_shares` never-bonded holders, exactly as a failed commit always has.
   - **Ruling — the retry convention**: a failed attempt is **never resumed**. A retry restarts the workflow with an entirely fresh seal: a new `MsgUserRequestGuardians` (new secret ID, new selection draw), a new inner encryption (so `C_r` and therefore `secret_commitment = SHA256(C_r)` differ), a new per-secret keypair `(pk_s, sk_s)`, a new `t`-of-`n` split with new envelopes and HMACs, and a new detection hint. **Clients MUST NOT reuse sealed material across attempts.** Shares from a failed attempt are points on a discarded polynomial: they cannot count toward a later attempt's threshold, cannot decrypt its ciphertext, and cannot even be *linked* to it on chain — a reused inner seal would repeat `secret_commitment` byte-for-byte and publicly connect the attempts. The convention is **advisory**: documented client-side, not enforced by the chain.
   - **Permanent residual**: any attempt that reached Phase 2 left its outer ciphertext `C` in transaction history forever (retention pruning removes live state, not history), so a `≥ threshold` coalition of *that* attempt's share-holders can always unlock *that* ciphertext. It decrypts only to the recipient-encrypted `C_r`, so the coalition alone reads nothing. The content's effective time-lock is therefore the **minimum over all distributed attempts** — which is the reason the convention forbids reuse rather than merely discouraging it.
   - **Residual mitigations**: protocol-random selection (controlling `threshold` slots requires a large fraction of the guardian population), the sunk 1,000 VEIL entry fee per colluding registration, and the early-reveal slashing regime once bonds do lock.
   - **Structural fixes considered and ruled out**: *accept-then-distribute* (bond before material) — a fourth interaction in substance, and it forces a "was a share actually delivered?" gate onto the no-reveal slash; *bond-at-selection* (lock every selected candidate's bond at Phase 1) — too aggressive, it seizes float for assignments the guardian never confirmed; *an on-chain `pk_s` uniqueness check* at Phase 2 — proto and state surface for a failure mode the SDK cannot produce.

8. **Small-Order Key Registration** *(closed, 29 July 2026)*
   - **Attack**: register a guardian (or supply a hint / `pk_s`) whose 32-byte X25519 public key is one of the curve's **small-order points**. An X25519 exchange against such a point yields an all-zero shared secret, so every key derived from it is publicly computable. A share encrypted to such a guardian key is encrypted under `SHA256(0x00…00 ‖ "timeflare_encryption")` and readable by **any observer** the moment Phase 2 lands — no collusion, no coalition, and no bond at risk, because the attacker never accepts the assignment and so is never slashable (early-reveal reporting requires an accepted assignment *and* a reveal). It stays bounded by the draw — a time-lock still needs `≥ threshold` of the `max_shares` drawn to be the attacker's own — but it removes the collateral that otherwise prices exploiting a registry fraction.
   - **Second vector — discovery poisoning**: against a small-order hint `R`, every recipient computes the same all-zero shared value, so one hint matches **every** recipient and the 2⁻⁶⁴ scan false-positive bound becomes 1. This needs no guardian registration at all: one secret creation imposes a wasted fetch-and-decrypt on every scanning client, persisting until retention prunes the hint.
   - **Mitigation**: every X25519 public key the protocol accepts is validated as usable — exactly 32 bytes and not a small-order point — at guardian registration, key rotation, `pk_s` at share distribution, the detection hint's `R`, and their genesis equivalents (the genesis path via the `InitGenesis` state-integrity sweep, which hard-halts). The chain delegates the predicate to `curve25519.X25519`, which also rejects non-canonical encodings that reduce to a small-order point; the WASM/TypeScript client independently refuses a non-contributory exchange so it can never fail silently.
   - **Result**: unusable keys cannot enter state. Validation is uniform across all five entry points deliberately — `pk_s` alone would have been defensible to exclude as creator-harming under #5, but an argued asymmetry costs more to document and audit than the two lines it saves.

This security architecture ensures **mathematical guarantees** combined with **economic deterrents** to provide robust protection for time-locked secrets without relying on trusted third parties.

---

## 5. Complete Secret Workflow

### Client-Side Integration Guide

This section provides a comprehensive walkthrough of the entire secret lifecycle, from initial preparation through final reveal, showing the complete client-side integration with the three-phase protocol.

#### Three-Phase Secret Publication Workflow

The complete secret publication process integrates client-side preparation with the three-phase on-chain protocol:

```
Original Secret
    ↓
1. Encrypt with Recipient's Public Key → C_r (inner ciphertext)
   Derive detection hint: fresh (e, R); tag = SHA256("timeflare/detect/v1" ‖ X25519(e, A))[:8]
    ↓
═══════════════════ PHASE 1: RESERVE SECRET ═══════════════════
2. Submit MsgUserRequestGuardians with {R, tag} (protocol selects guardians;
   the recipient's key A stays on the creator's device)
    ↓
   Receive Guardian Assignments:
   - `guardian_address₁` → `encryption_public_key₁`
   - `guardian_address₂` → `encryption_public_key₂`
   - ...
    ↓
═════════════ CLIENT-SIDE SEAL (OFFLINE, one seal_secret call) ═════════════
3. Generate fresh per-secret keypair (pk_s, sk_s); encrypt C_r to pk_s → C
    ↓
4. Split sk_s via SSS t-of-n → 34B key-share envelopes [ks₁, ks₂, ..., ksₙ]
   For each guardian:
   - Encrypt ksᵢ with Guardian's Public Key → `encrypted_share` (~94B)
   - Generate HMAC(ksᵢ || guardianAddr || secretID) → `share_hmac`
5. Generate SHA256(C_r) → `secret_commitment`; DISCARD sk_s
    ↓
═══════════════════ PHASE 2: FINALIZE SECRET ═══════════════════
6. Submit MsgUserDistributeShares with C + pk_s + encrypted key shares + `secret_commitment`
   C is stored on chain exactly once; Secret State: reserved → awaiting_acceptance
    ↓
═══════════════════ PHASE 3: GUARDIAN ACCEPTANCE ═══════════════════
7. Each Guardian (OFFLINE):
   - Decrypt their `encrypted_share`
   - Verify HMAC(`decrypted_share` || `guardian_addr` || `secret_id`)
   - Submit MsgGuardianConfirmShares(accept: true/false)
    ↓
8. Protocol checks `threshold`:
   - If sufficient acceptances → Secret State: awaiting_acceptance → pending
   - If insufficient → Secret State: awaiting_acceptance → failed
    ↓
═══════════════════ REVEAL WINDOW ═══════════════════
9. Guardians submit their 34B key-share envelopes during the reveal window
   (only accepted guardians; HMAC-verified at submission)
10. Protocol records revealed key shares on chain; threshold reached → reconstructable
11. Anyone unseals: combine ≥t key shares → sk_s (check against pk_s),
    fetch C (Query/SecretPayload), decrypt outer layer → C_r,
    verify SHA256(C_r) == `secret_commitment`
12. Recipient decrypts C_r with their private key → original secret
    (authenticated encryption also provides end-to-end integrity for the recipient)
```

**Critical Points:**
- **Phase 1**: Protocol controls guardian selection (no client choice)
- **Client Preparation**: Happens between Phase 1 and 2 with assigned guardians
- **Phase 2**: Client submits encrypted shares + commitment for post-reconstruction verification
- **Phase 3**: Guardians verify HMACs before accepting (prevents malicious HMAC attacks)
- **Security**: Only accepted guardians can participate in reveals
- **Verification**: Secret commitment provides convenience validation that reconstruction succeeded
- **Retry semantics**: a secret that fails at `commit_deadline` refunds its reward pool automatically in the next block. A retry **restarts this workflow from the top** — new `MsgUserRequestGuardians`, new inner seal, new `(pk_s, sk_s)`, new shares, new detection hint. **Never reuse a previous attempt's sealed material**: it cannot serve the new attempt, and reusing the inner seal would publicly link the two through an identical `secret_commitment` (see [Common Attack Vectors](#common-attack-vectors--mitigations) #7). The wait until the deadline is deliberate and has no early exit, because it is what prices abandoned selection draws — a fixed 50 blocks, the same for every creator


---

## Configuration & Parameters

### Network Configuration

| Parameter | Value | Purpose |
|-----------|-------|---------|
| **Chain Prefix** | `tmflr` | Bech32 address format |
| **Coin Type** | 9733 | BIP44 HD wallet derivation at `m/44'/9733'/0'/0/0` |
| **Block Time** | ~6 seconds | Expected interval — an expectation, never protocol truth. The real interval is a property of the running network (`timeout_commit` plus round and processing time, both of which move with validator count and load), so clients derive dates from the **measured** interval read off block header consensus timestamps, never from this figure |
| **Block Gas Limit** | 75,000,000 gas | `consensus.params.block.max_gas`, set at genesis — the per-block execution bound (declared gas), distinct from the gas price floor |
| **Base Denom** | uveil | Smallest unit (1 VEIL = 1M uveil) |

The coin type is the shared constant `ChainCoinType` (`x/secrets/types`),
applied to the SDK config in `app/config.go`. Every client that generates or
restores a wallet from a BIP39 mnemonic derives at `m/44'/9733'/0'/0/0` —
never a library default — per
[CLIENT_CONVENTIONS.md §9](guides/CLIENT_CONVENTIONS.md); the
mnemonic → address pairing is pinned across implementations by
`testdata/vectors/wallet_derivation.json`.

The block gas limit exists as an execution bound, not a price: the gas price
floor (see [Economic Constants](#economic-constants)) prices work but does
not cap how much of it one block may carry, and `max_bytes` bounds serialised
size, which the protocol's expensive handlers barely register (they are
expensive in execution, not bytes). `max_gas` is what keeps a block's
contents executable within the block interval, bounds the reach of a forged
gas simulation at the limit rather than the signer's balance, and denies the
single-height concentration that mass-creation attacks require. 75,000,000
is ~36× the largest legitimate transaction (a 32-guardian cancel at
~2.07M gas) and holds a full 32-guardian activation burst many times over;
measured on a devnet (August 2026), a full 75M block of real creation
traffic executes in under 10 ms, so the value is bounded by economics, not
execution time. It is a CometBFT consensus parameter — adjustable through
the ordinary consensus-params upgrade path, unlike the protocol's immutable
economic constants.

### Guardian Parameters

| Parameter | Value | Location | Description |
|-----------|-------|----------|-------------|
| **Entry Fee `F`** | 1,000 VEIL | `constants.go` | One-off at registration; rides the 90/10 fee split (900 validators / 100 burn) |
| **Base Rate `rate`** | 0.000001 VEIL/guardian/block (1 uveil) | `constants.go` | Master reward price level |
| **Bond Multiplier `k` Range** | 4.00 – 24.00 | `constants.go` | Per-guardian; starts at 4.00, × 1.26 per slash, × 0.963 per reveal (truncating, clamped) |
| **Concurrency Cap** | 100 active bonds | `constants.go` | Per-guardian eligibility gate, checked at selection and acceptance |
| **Max Tier** | 10 | `constants.go` | `bump` ceiling (`bump ∈ [1.00, 10.00]`, 2 d.p.) |
| **No-Reveal Bond Split** | 40 % burn / 10 % creator / 50 % returned | `constants.go` | Percentage of the posted bond |
| **Early-Reveal Bond Split** | 40 % burn / 10 % creator / 50 % reporter | `constants.go` | Full bond slashed, none returned |
| **Max Reveal Horizon `H`** | 5,256,000 blocks (~1 year) | `constants.go` | Bounds `reveal_end_block` from creation; equals Max Availability so validated windows are always staffable |
| **Max Availability** | 5,256,000 blocks (~1 year) | `constants.go` | Guardian availability window limit |
| **Availability Enforcement** | Automatic | Built-in | Commitment-based selection eligibility |
| **Encryption Key Length** | 32 bytes | `constants.go` | Required format for secret share encryption |
| **Encryption Key Validity** | 32 bytes, not a small-order point | `crypto/encryption.go` | Every X25519 key the protocol accepts must yield a contributory exchange; enforced at registration, rotation, `pk_s`, the hint's `R`, and their genesis equivalents |
| **Key Rotation Fee** | `rate × 14,400` (one guardian-day) | `constants.go` | Flat burn per rotation — anti-spam pricing of the permanent history entry |
| **Key Rotation Min Interval** | 432,000 blocks (~30 days) | `constants.go` | One rotation per guardian per interval, measured from the newest epoch's effective height (epoch 0 starts the clock) |

### Guardian Key Management

**Guardian Key Architecture**: Guardians use their standard blockchain account for identity and transactions, plus a separate encryption key for receiving secret shares:

| Key Type | Purpose | Usage | Rotation |
|----------|---------|--------|----------|
| **Guardian Address** | Identity and authentication | Transaction signing, reward receipt | Standard Cosmos account |
| **Encryption Public Key** | Secret share encryption | Client-side share encryption before distribution | Forward-only via `MsgGuardianRotateKey`; each epoch's binding immutable forever |

**Key Management Principles**:
- **Account Security**: Guardian address follows standard Cosmos SDK account security
- **Epoch Immutability**: Each epoch's key binding is permanently immutable — the key an assignment was encrypted to can never change; only the current-epoch pointer advances (see [Guardian Key Rotation](#-guardian-key-rotation))
- **Key Rotation**: The hygiene path — rotate forward for future assignments while old epochs serve their in-flight secrets to settlement; rotation is **not** loss recovery
- **Custody**: Encrypted-at-rest key file, backup/restore with on-chain verification, and startup self-check — see [GUARDIAN_KEY_CUSTODY.md](guides/GUARDIAN_KEY_CUSTODY.md)
- **Compromise Recovery**: Set `accepting_secrets = false` immediately, then rotate; see [GUARDIAN_KEY_CUSTODY.md](guides/GUARDIAN_KEY_CUSTODY.md) "Compromise" for the full procedures (including signing-key compromise)

**Security Benefits**:
- **Standard Identity**: Uses proven Cosmos SDK account system for identity
- **Dedicated Encryption**: Separate key exclusively for encryption operations
- **Clear Separation**: Transaction signing and share encryption never use same key

### Secret Parameters

| Parameter | Value | Description |
|-----------|-------|-------------|
| **Commit Window** | 50 blocks (~5 minutes) | Fixed for every secret: `commit_deadline = creation_height + 50`. Not creator-settable |
| **Min Reveal Buffer** | 50 blocks | Buffer from commit deadline to reveal start |
| **Min Reveal Start Offset** | 100 blocks | Commit window + buffer; the floor is a constant, not a computation |
| **Min Window Duration** | 100 blocks (~10 minutes) | Shortest reveal period |
| **Max Window Duration** | 14,400 blocks (~1 day) | Longest reveal period |
| **Max Reveal Horizon `H`** | 5,256,000 blocks (~1 year) | `reveal_end_block` within `H` of creation (= max guardian availability) |
| **Min/Max Threshold** | 2 / 16 | SSS reconstruction bounds |
| **Min/Max Shares** | 2 / 32 | Band bounds: `threshold ≤ min_shares ≤ max_shares ≤ 32`, `max − min < threshold` |
| **Retention Window** | 2,592,000 blocks (~6 months) | Terminal secrets pruned at `terminal_at + RetentionBlocks`, replaced by a tombstone |
| **Max Prunes / Block** | 50 | Stage 2 pruning cap; same-height bursts carry over |

### Economic Constants

| Parameter | Value | Purpose |
|-----------|-------|---------|
| **Total Supply** | 1,000,000,000 VEIL | Fixed forever |
| **Validator Fee Share** | `90%` | Consensus rewards |
| **Guardian Fee Share** | `0%` | Direct payment only |
| **Burn Rate** | `10%` | Supply deflation |
| **Minimum Gas** | 0.1 uveil | Accessibility |

The block gas limit (75,000,000 — see
[Network Configuration](#network-configuration)) is deliberately absent from
this table: it is a CometBFT consensus parameter, not one of the protocol's
immutable economic constants, and changing it is an ordinary consensus-params
update rather than a change to the economic model.

### Secret Pricing

Secret pricing is fully determined by the protocol formula — the creator chooses its dials (`bump`, reveal timing, guardian band) and the reward pool follows:

```
P_time = rate × distance × max_shares × bump
P      = max_shares × F_reveal + P_time
A      = max_shares × F_accept
```

**Worked examples** (v1 values: `rate = 0.000001 VEIL/guardian/block`, `F_accept = 0.012 VEIL`, `F_reveal = 0.013 VEIL`; bond shown at the `k` floor of 4.00 and, in brackets, the 24.00 ceiling):

| Secret | `bump` | `distance` | `max_shares` | Reward pool `P` | Accept fees `A` | Bond `B` (each, `k=4.00` [`k=24.00`]) |
|--------|--------|-----------|-----------|-----------------|-----------------|----------------------------------------|
| Short, low security | 1.00 | 100,000 (≈ 7 d) | 5 | 0.565 VEIL | 0.06 VEIL | 0.4 VEIL [2.4 VEIL] |
| Medium | 5.00 | 1,000,000 (≈ 70 d) | 9 | 45.117 VEIL | 0.108 VEIL | 20 VEIL [120 VEIL] |
| Long, high security | 10.00 | 5,256,000 (≈ 1 y) | 15 | 788.595 VEIL | 0.18 VEIL | 210.24 VEIL [1,261.44 VEIL] |

The gas reimbursements are a rounding error on a long secret — 0.025% of the one-year row — and the difference between working at a profit and working at a loss on a short one.

The reward `P` responds to all three creator inputs; the bond `B` responds to duration and `bump` (the same quantities the creator is paying for) times the guardian's own `k` — each guardian's obligation is exactly `k` times its reward share, independent of headcount, so deterrence is a constant multiple of what is at stake rather than an accident of hold length. An extreme-`bump` secret that cannot attract enough capitalised guardians is allowed to fail to clear: that is a market signal, and the creator lowers `bump` or waits for deeper capital. See [Secret Economics & Slashing](#secret-economics--slashing) for the full model.

#### Creation Fee

Alongside the escrowed pool `P`, `MsgUserRequestGuardians` charges a
**non-refundable creation fee** (ruled July 2026 — one fee, two jobs: it
prices every selection draw, closing the abandon-and-refund grinding hole,
and it is the recurring consensus security budget that scales with the value
validators secure):

```
bps(d)       = CreationFeeMaxBps − (CreationFeeMaxBps − CreationFeeMinBps)
                                    × min(d, CreationFeeCurveEndBlocks)
                                    ÷ CreationFeeCurveEndBlocks
creation_fee = max(CreationFeeFloor, P_time × bps(d) ÷ 10,000)
```

The percentage is charged on the pool's **time component**, never on the gas
reimbursements the creator also funds (`A` and the pool's reveal legs). The
fee prices a selection draw, and a draw does not become more expensive because
a pass-through was added; taxing one would route part of every guardian's
reimbursement to validators and the burn.

with truncating integer division throughout, `d` the secret's `distance`,
and hard constants (no governance): `CreationFeeMaxBps = 1,000` (10%),
`CreationFeeMinBps = 500` (5%), `CreationFeeCurveEndBlocks = 432,000`
(30 days), and `CreationFeeFloor = ⌈CreationFeeFloorGas × MinGasPrice⌉`
with `CreationFeeFloorGas = 600,000` — three reference 200k-gas
transactions at the consensus-enforced floor price, `0.06 VEIL` today. The
percentage falls linearly from 10% at minimal distance to 5% at 30 days and
stays flat beyond; because `P` grows linearly with distance while the rate
falls, the absolute fee is non-decreasing in distance up to
integer-truncation dust (each 1-bps step of the truncating curve can dip
the fee by at most `P ÷ 10,000`) — no window shape meaningfully lowers the
bill. The floor is deliberately **gas-denominated**: its job is
to make a discarded selection draw cost ~3× the gas that accompanies it,
which a percentage of small pools mathematically cannot do.

**Worked invoice** (at `rate = 1 uveil` and the ruled values; every fee then
rides the 90/10 split — 90% to validators, 10% burned):

| Secret | Distance (blocks) | Curve rate | `P` | Creation fee | Regime |
|---|---|---|---|---|---|
| 1-day sealed bid (3g, bump 1) | 14,400 | 9.84% | 0.0432 | **0.06** | floor |
| 7-day announcement (5g, bump 1) | 100,800 | 8.84% | 0.504 | **0.06** | floor |
| 30-day dead-man's handle (5g, bump 2) | 432,000 | 5.00% | 4.32 | 0.216 | curve |
| 70-day escrow (9g, bump 5) | 1,000,000 | 5.00% (flat) | 45 | 2.25 | curve |
| 1-year max (32g, bump 10) | 5,256,000 | 5.00% (flat) | ~1,682 | ~84 | curve |

**Refundability at a glance**: the fee is charged at request time, goes
straight to the fee collector, and **never enters module escrow** — so
commit-timeout, zero-reveal refund, cancellation and settlement refund
exactly what they always did (the full pool `P` where applicable), and
there is nothing to refund the fee from on any exit path. A failed request
(selection failure, insufficient funds for `P + fee`) charges nothing — the
transaction aborts atomically. The reservation event reports the fee and
the regime that priced it (`floor` vs `percent`).

#### Fee Distribution

All transaction fees follow the standard Timeflare distribution:
- **90%** → Validators (consensus security)
- **10%** → Permanent burn (deflationary mechanism)

The split runs once per block at BeginBlock, ordered before the
distribution module allocates rewards: the previous block's collected fees
are divided per denomination (validator share floored, integer-division
dust joins the burn), the burn share is permanently destroyed, and a
`fee_distribution` event reports both amounts. The validator share is
**left in the fee collector**, where the distribution module's own
BeginBlocker — ordered immediately after — allocates it to validators by
bonded voting power with full reward bookkeeping (`AllocateTokens`), making
it **withdrawable** through the standard distribution flow. This is the
protocol's **guaranteed** usage-proportional deflation — unlike the
scenario-dependent burns (slashing, dust), it applies to every fee-bearing
block. The guardian entry fee enters the same pipe: it is sent to the fee
collector at registration and rides the next block's split. `community_tax`
is pinned to **zero** in genesis — no fraction of any allocated fee is
skimmed into a community pool (the no-treasury stance). The pool is not
literally frozen: every reward **withdrawal** truncates decimal rewards to
integers and parks the sub-uveil remainder there (SDK design,
`withdrawDelegationRewards`) — dust bounded below one uveil per withdrawal
event, versus whole VEIL for any real skim. The e2e suite asserts the
pool's growth across a full run stays below one uveil.

Note: Guardians earn directly from secret creators through reward pools, not from transaction fees.

#### Implementation Notes

- **Current Status**: Basic fixed fees implemented, advanced pricing under development
- **Protocol Parameters**: Fee structure parameters are hardcoded for predictability
- **Market Response**: Fees will be calibrated based on network usage and guardian participation
- **User Experience**: Fee estimation tools will be provided in client SDKs

*This fee structure is designed to evolve with network maturity and user requirements while maintaining the core principle of value-based pricing.*

### 🔒 Protocol Constants

#### Economic Model
| Parameter | Value | Description |
|-----------|-------|-------------|
| **Total Supply** | 1,000,000,000 VEIL | Fixed forever, zero inflation |
| **Fee Distribution** | 90% / 10% | Validators / Burn |
| **Guardian Entry Fee** | 1,000 VEIL | One-off at registration; rides the 90/10 split (900 VEIL validators / 100 VEIL burn) |
| **Creation Fee** | `max(0.06 VEIL, P × bps(d) ÷ 10,000)` | Non-refundable at `MsgUserRequestGuardians`; `bps` linear 10% → 5% over 30 days of distance; rides the 90/10 split |
| **Guardian Bond** | `rate × distance × bump × k` | Per-secret returnable collateral, frozen per-guardian at selection |
| **Creator Slash Share** | 10 % of slashed bond | Compensation in all slashing cases |
| **Minimum Gas Price** | 0.1 uveil/gas | **Consensus-enforced** fee floor (`MinGasPriceUveilNum ÷ MinGasPriceUveilDen`): the ante chain rejects any transaction paying under `⌈gas_limit × 1 ÷ 10⌉ uveil` in both CheckTx and DeliverTx — genesis (height 0) and simulation exempt. `minimum-gas-prices` in `app.toml` remains the per-node mempool knob and may only sit at or above the floor |

#### Security Constants
| Parameter | Value | Description |
|-----------|-------|-------------|
| **Public Key Length** | 32 bytes | All cryptographic keys |
| **X25519 Key Validity** | Not a small-order point | Every accepted X25519 key must yield a contributory exchange; a small-order point makes the derived key publicly computable |
| **Min Evidence Length** | 32 bytes | Slashing evidence requirement |
| **Max Payload Size** | 4,216B | Cap on the stored payload ciphertext `C` (4,096B of original secret + two 60B encryption layers). Stored once per secret — independent of guardian count. Larger secrets use the envelope pattern (time-lock a key + URI + hash) |
| **Max Key Share Size** | 128B | Cap on a guardian's encrypted key share (34B envelope + 60B encryption overhead, with headroom for future envelope versions) |
| **Max Revealed Key Share Size** | 64B | Cap on the plaintext key-share envelope submitted at reveal or as early-reveal evidence |
| **Availability Commitment** | Enforced | Guardian availability window restrictions |
| **Max Availability Window** | 5,256,000 blocks | ~1 year maximum duration |

### ⚙️ Message Validation Constants

#### Secret Publication Limits (in MsgUserRequestGuardians)
| Constraint | Value | Purpose |
|------------|-------|---------|  
| **Threshold Range** | 2–16 | SSS reconstruction bounds |
| **Shares Band** | `threshold ≤ min ≤ max ≤ 32`, `max − min < threshold` | Creator-chosen guardian range; `max_shares` candidates selected, `min_shares` acceptances required |
| **Bump Range** | 100–1000 (hundredths) | Security factor 1.00–10.00 |
| **Max Reveal Horizon** | 5,256,000 blocks (~1 year) | `reveal_end_block` within `H` of creation (= max guardian availability) |

#### Guardian Registration Limits (in MsgGuardianRegister)
| Constraint | Value | Purpose |
|------------|-------|---------|  
| **Min Reveal Buffer** | 10 blocks | Guardian preparation time |
| **Max Availability Window** | 5,256,000 blocks | Maximum guardian availability (~1 year) |
| **Identity Key Length** | 32 bytes | Required format for identity verification |
| **Encryption Key Length** | 32 bytes | Required format for secret share encryption |
| **Encryption Key Validity** | Not a small-order point | Rejects keys whose X25519 exchange is non-contributory |

#### Guardian Key Rotation Limits (in MsgGuardianRotateKey)
| Constraint | Value | Purpose |
|------------|-------|---------|  
| **New Key Length** | 32 bytes | Required format for secret share encryption |
| **New Key Validity** | Not a small-order point | Same usability check as registration — rotation must not be a downgrade path |
| **Global Uniqueness** | All keys, all epochs, forever | New key must never have been registered by any guardian at any epoch |
| **Minimum Interval** | 432,000 blocks (~30 days) | `current_height − last_effective_height ≥ 432,000`; epoch 0 starts the clock |
| **Rotation Fee** | `rate × 14,400`, burned | Flat anti-spam price of the permanent history entry |

#### Secret Cancellation Limits (in MsgUserCancelSecret)
| Constraint | Value | Purpose |
|------------|-------|---------|  
| **Cancellation States** | `pending` only | Cancellation is a post-activation mechanic (ruled July 2026) — pre-activation secrets exit via commit-timeout |
| **Cancellation Deadline** | `reveal_start_block` | Cancellation permitted from activation until the reveal window opens (pro-rata guardian pay makes late cancellation non-abusive) |

**Implementation Location**: All validation constants are defined in `x/secrets/types/constants.go` and used directly in message validation functions.

### 🏛️ Genesis Pool Allocations

Fixed distribution of the 1 billion VEIL supply at network launch:

| Pool | Exact Amount | Controlling Key | Purpose |
|------|--------------|-----------------|---------|
| **Rebate Pool** | 700,000,000 VEIL (70%) | ❌ **None** — module account | Spendable only by the [Recipient Rebate](#recipient-rebate) formula |
| **Bootstrapping Pool** | 300,000,000 VEIL (30%) | ✅ Yes | Launch funding for any actor — validators, guardians, users |

**Total Supply**: Exactly 1,000,000,000 VEIL (fixed forever)

#### Pool Access Control

- **Rebate pool**: a **keyless module account**. No key can move it, no
  governance vote can redirect it, and nothing can top it up — it is committed
  to the rebate formula permanently, and its balance is the input to the accrual
  rate, so it also *bounds* the rate as it declines.
- **Bootstrapping pool**: one controlled key covering every launch grant. It
  replaces the earlier split between validator and guardian allocations, which
  were the same activity under two names, and it is the only key-controlled
  supply at genesis.
- **No central treasury**: neither pool funds development; there is nothing else
  to spend.

#### Distribution Strategy

Distribution has two phases and no committee:
1. **Bootstrapping**: hand-made grants from the bootstrapping pool bring the
   first validators, guardians and creators online. Nothing can be rebated until
   a secret has settled, so this phase is unavoidable and deliberately manual.
2. **Self-sustaining**: those creators' settled secrets credit rebates to their
   recipients, who become creators in turn. The protocol's share of distribution
   declines automatically as the pool does, with no governance event.

*Implementation: See `devnet/chain/setup-chain.sh` for exact genesis configuration*

### Key Economic & Strategic Decisions

#### Genesis Pool Allocations (1B VEIL)

| Pool | Amount | Key Control | Purpose |
|------|--------|-------------|---------|
| **Rebate Pool** | 700M VEIL (70%) | ❌ None | Adoption, distributed only by the rebate formula |
| **Bootstrapping Pool** | 300M VEIL (30%) | ✅ Yes | Launch funding for validators, guardians and users |

**Rationale**: No central treasury ensures true decentralisation from launch. The majority of supply is committed at genesis to a mechanism rather than to a discretion — the rebate pool has no key, so adoption funding cannot be redirected, front-run or withheld — while one bootstrapping pool covers the hand-made grants a launch unavoidably needs.

#### Token Economics & Fee Structure

- **Fixed Supply**: 1 billion VEIL forever (zero inflation)
- **Fee Distribution**: 90% validators, 10% burn
- **Guardian Rewards**: Direct payment model ensures compensation is tied to actual service provision
- **Very Low Fees**: 0.1 uveil/gas minimum gas price for accessibility — consensus-enforced in the ante chain (protocol law, not node configuration), so validator gas revenue and the 10% burn cannot be competed to zero by fee-undercutting validators
- **Usage-Driven**: All compensation tied to actual network utility, not token existence

**Rationale**: No block rewards ensures all participants are compensated for providing real value, while direct guardian payments create clear service-based incentives.

#### Deflationary Mechanisms

- **Transaction Fee Burn**: 10% of all fees permanently destroyed
- **Slashing Burns**: 40% of every slashed bond eliminated from supply
- **Dust Burn**: integer-division remainders from pool and bond splits are burned
- **Natural Scarcity**: Lost keys accelerate deflation organically
- **Anti-Spam**: Permanent costs make attacks economically unfeasible

**Rationale**: Creates lasting value through scarcity without threatening network longevity, while maintaining predictable economic rules.

#### Security & Staking Model

- **Dual Collateral System**: Validators (consensus staking) + Guardians (per-secret bonds) with separate roles
- **Bonded Guardians**: per-obligation bonds (`rate × distance × bump × k`) replace a fixed shared stake — creators price their own security, and each guardian's history prices its collateral
- **User Protection**: 10 % of every slashed bond compensates the secret creator, scaling with the security purchased
- **No Security Budget Risk**: Fee-only model ensures security scales with usage
- **Aligned Incentives**: Earn only when providing actual network value

**Rationale**: Dual model separates concerns while per-obligation bonds make penalties market-priced and impossible to escape by draining a shared stake. Fee-only security ensures long-term sustainability.

#### Decentralization Strategy

- **Hardcoded Economics**: Core economics embedded in protocol to prevent manipulation
- **No Treasury Control**: Zero community tax prevents centralised fund accumulation
- **Immutable Economics**: Fee splits, burns, and stakes cannot be changed
- **Predictable Rules**: Protocol behavior is deterministic and transparent
- **No Emergency Powers**: Protocol designed to self-heal or fail gracefully without central intervention
- **Fork-Based Evolution**: Critical issues resolved through community consensus and network forks

**Rationale**: Ensures true decentralisation while maintaining economic predictability and network resilience.

#### Long-term Sustainability Framework

- **Value Accrual Model**: Token appreciation through scarcity vs. dilution through inflation
- **Market-Driven Development**: No central treasury eliminates rent-seeking behavior
- **Usage Dependency**: Network value tied directly to secret-sharing utility
- **Economic Predictability**: Fixed rules prevent policy manipulation
- **Natural Network Effects**: More usage → more burns → higher value → more adoption

**Rationale**: Creates sustainable growth through real utility rather than artificial mechanisms, with predictable economics fostering long-term adoption.

#### Strategic Positioning

- **True Utility Token**: Value derived from actual secret-sharing network usage
- **Guardian-Centric Economics**: Premium rewards for unique protocol-specific services
- **Deflationary by Design**: Network becomes more valuable as it becomes more useful
- **Launch-to-Decentralization Path**: Structured transition from controlled launch to full decentralisation
- **Minimal Attack Surface**: Hardcoded parameters prevent economic manipulation

**Rationale**: Positions Timeflare as a sustainable, utility-driven network with clear value proposition and protection against manipulation.

---


