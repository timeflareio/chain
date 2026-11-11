# Security Audit Readiness Plan

| | |
|---|---|
| **Priority** | P1 |
| **Status** | refining |
| **Created** | July 2026 |
| **Updated** | 11 November 2026 |
| **Protocol surface** | none |
| **Repositories** | chain, crypto, guardian, typescript-sdk, mobile-client |
| **Depends on** | `timeflareio/guardian` — `PRIORITY_GUARDIAN_ACCEPT_REVEAL_CORRECTNESS_PLAN.md`; `timeflareio/guardian` — `PRIORITY_GUARDIAN_CUSTODY_HARDENING_PLAN.md`; `timeflareio/guardian` — `PRIORITY_GUARDIAN_TX_PATH_RESILIENCE_PLAN.md` |
| **Related** | [DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md](done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md); [DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md](done/DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md); [DONE_GUARDIAN_KEY_CUSTODY_PLAN.md](done/DONE_GUARDIAN_KEY_CUSTODY_PLAN.md); `timeflareio/crypto` — `docs/planning/done/DONE_CONSENSUS_CRYPTO_PURE_GO_PLAN.md`; `timeflareio/crypto` — `docs/planning/done/DONE_CRYPTO_ASSURANCE_PLAN.md`; `timeflareio/network` — `PENDING_TESTNET_LAUNCH_PLAN.md`; `timeflareio/typescript-sdk` — `PENDING_SDK_PRODUCTIONISATION_PLAN.md` |
| **Open questions** | 5 |

**Why this priority**: programme-level; gates mainnet
**Status notes**: no phase has started; five open questions below.
**Origin**: automated review, July 2026.
**Components**: documentation programme + open-issue triage (absorbed the deleted `docs/guides/UNSOLVED.md`); `docs/security/THREAT_MODEL.md` (new); coordinates the pre-audit hardening plans in every repository

**Already landed**: the four technical hardening plans this programme wraps — [DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md](done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md) and [DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md](done/DONE_SETTLEMENT_AND_STATE_INTEGRITY_PLAN.md) here, and `DONE_CONSENSUS_CRYPTO_PURE_GO_PLAN.md` and `DONE_CRYPTO_ASSURANCE_PLAN.md` in `timeflareio/crypto` (its fuzz targets are in `go/fuzz_test.go`).

## What this plan does

Prepares the project to commission — and pass — an external security audit: a written threat model, triage and disposition of every known-unsolved issue, a self-assessment against the findings of the July 2026 automated review, and the audit engagement plan itself. This is the programme wrapper around the technical hardening plans. The chain-side and crypto-side ones are done; the pre-audit hardening still open is the guardian's: `timeflareio/guardian` — `PRIORITY_GUARDIAN_ACCEPT_REVEAL_CORRECTNESS_PLAN.md`, `PRIORITY_GUARDIAN_CUSTODY_HARDENING_PLAN.md` and `PRIORITY_GUARDIAN_TX_PATH_RESILIENCE_PLAN.md`.

## Why

- The since-deleted `docs/guides/ROLLOUT_PLAN.md` (removed August 2026 — pre-bonded-economics, superseded by per-network rollout plans, which live in `timeflareio/network`) named an 8–12 week audit programme with multiple firms; none of it started.
- The since-deleted `docs/guides/UNSOLVED.md` (removed August 2026) collected unmitigated issues with no owner, severity, or disposition; its triage lives here (Phase 2) — most entries were resolved or superseded by later rulings, leaving the reporter-spam disposition below and client upgrade-compatibility (owned by `timeflareio/typescript-sdk` — `PENDING_SDK_PRODUCTIONISATION_PLAN.md`).
- Auditors price and scope by what you hand them. A codebase with a threat model, an invariant catalogue, a fuzz corpus, and a documented accepted-risk register gets a deeper audit for the same cost than one where the auditors spend week one reconstructing intent.
- Several accepted risks are currently documented only as asides in `docs/spec.md` (self-report bounty recapture, invalid-share submission, pre-acceptance share exposure, detection-dependence of early-reveal deterrence, threshold-as-primary-guarantee). These are *good* explicit decisions — they should be consolidated where an auditor (and a user) can find them.

## How

### Phase 1 — Threat model document (`docs/security/THREAT_MODEL.md`, new)

Structured by adversary, covering at minimum:

- **Malicious creator**: bogus HMACs at distribution (mitigated by guardian pre-acceptance verification — which `guardiand` lets an operator switch off with `enable_hmac_validation: false`; removing that opt-out is Phase 7 of `timeflareio/guardian` — `PRIORITY_GUARDIAN_ACCEPT_REVEAL_CORRECTNESS_PLAN.md`), junk payloads (accepted risk), entropy grinding (closed — selection consumes no creator input, `docs/spec.md` "Guardian selection"; [DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md](done/DONE_GUARDIAN_SELECTION_HARDENING_PLAN.md)), griefing via mass secret creation.
- **Malicious guardian / cartel**: early reveal economics (self-report recapture bound), collusion at/above threshold, selective no-reveal, sybil registration economics.
- **Malicious reporter**: spam false reports (gas-priced by the consensus fee floor since July 2026; needs a disposition on whether that suffices — see open questions), evidence replay.
- **Malicious validator / MEV**: transaction ordering around reveal windows, and censorship of reveals near window close (a censored guardian gets slashed — quantify the validator's power to cause slashing). Acceptance carries no ordering race: every valid confirmation joins the roster up to `max_shares` until the commit deadline (`docs/spec.md`, guardian lifecycle step 4).
- **Network observer**: hint-scanning economics, payload-ciphertext harvest-now-decrypt-later (quantum posture statement), linkage analysis across transactions.
- **Infrastructure**: consensus determinism of the cryptographic path (closed — the consensus path is the pure-Go `timeflareio/crypto` module, `go/`), guardian key custody (`timeflareio/guardian` — `docs/guides/GUARDIAN_KEY_CUSTODY.md` and `PRIORITY_GUARDIAN_CUSTODY_HARDENING_PLAN.md`).

Each threat: impact, current mitigation, residual risk, disposition (fix / accept / monitor), and a pointer to the fixing plan where one exists.

### Phase 2 — Open-issue triage (absorbs the deleted UNSOLVED.md)

Convert every open issue into the threat-model register with an owner decision: fix-before-testnet, fix-before-mainnet, or accept-with-rationale. The reporter-spam issue specifically needs a disposition before a public network exists. Since July 2026 a report is no longer free — the consensus fee floor prices every submission (~0.01 VEIL at typical gas) and the per-report HMAC verification is metered gas the reporter pays — so the question has narrowed from "spam is free" to whether gas-priced spam still warrants a dedicated answer (a small non-refundable evidence-submission fee, or per-secret one-report-per-address) or is accepted.

### Phase 3 — Audit package assembly

- Invariant catalogue: the economic invariants from `docs/spec.md` + the state-side invariant library (`CheckStateInvariants` in `x/secrets/keeper/invariants.go`, which also runs after genesis import and inside upgrade migrations) and the solvency identity asserted in `x/secrets/keeper/invariants_test.go`, in prose an auditor can check the code against.
- Crypto design note: the four-layer encryption architecture (already excellent in `docs/spec.md`) plus the *implementation* map — which primitive lives where (`timeflareio/crypto` `go/` for the chain and guardian, `rust/` for the WASM bundle, and the mobile client's native bindings in `timeflareio/mobile-client` `packages/crypto/`), which implementations are duplicated, and where the cross-implementation vectors are (`timeflareio/crypto` `vectors/`).
- Known-issues register from Phases 1–2 (auditors verify accepted risks are truly bounded).
- Reproducible build + `make e2e-full` instructions so auditors can run the failure-path scenarios.

### Phase 4 — Engagement

- Scope: the chain module and the primitives (`timeflareio/crypto`) as the core — its pure-Go implementation (`go/`) is the consensus path and what the guardian runs, its Rust implementation (`rust/`) is what clients run as WASM; the guardian daemon and the SDK as a second ring. Whether the mobile client's native reimplementation of the primitives is in scope is an open question. Cosmos-experienced firms need a fresh shortlist (the deleted ROLLOUT_PLAN.md carried one from an earlier era of the project — Trail of Bits, Consensys Diligence, OtterSec).
- Sequence with the technical plans: hardening fixes should land *before* the audit (paying auditors to find what an internal review already found is waste). The chain and crypto ones have; the guardian's three `PRIORITY_` plans have not.
- Budget for a remediation window + fix-verification pass; publish the report.

## Open questions

1. **Timing anchor**: audit before public testnet, or after a testnet burn-in period (audit sees battle-tested code, but testnet users see unaudited code)? Common practice: internal hardening → testnet → audit → mainnet. `timeflareio/network` — `PENDING_TESTNET_LAUNCH_PLAN.md` already proceeds on the basis that this plan gates mainnet and not the testnet, without a ruling recorded here. *Recommendation: testnet first, audit before mainnet — and record it here so the two plans agree by decision rather than by assumption.*
2. **Reporter-spam disposition**: gas-priced today (consensus fee floor); dedicated evidence fee, rate-limiting, or accept? This may need its own small plan once decided. *Recommendation: accept for the testnet with the gas price as the only deterrent, measure, and decide on a dedicated fee before mainnet — a fee is a protocol change and a release train.*
3. **Disclosure policy**: `timeflareio/typescript-sdk` — `package.json` references a security policy URL, and no `SECURITY.md` exists in any repository. Is there one policy for the project or one per repository? *Recommendation: one disclosure process and contact, stated in a `SECURITY.md` in each public repository, written in Phase 3.*
4. **Quantum posture**: X25519 shares harvested today are decryptable by a future quantum adversary; for multi-year time-locks this is inside the threat window arguably *more* than for ordinary TLS. A statement (accepted risk + envelope-version upgrade path) is needed even if the answer is "accepted for v1". *Recommendation: accepted for v1, stated in `docs/spec.md` alongside the other accepted risks, with the envelope version named as the upgrade path.*
5. **Is the mobile client's native reimplementation of the primitives in audit scope?** It is a third implementation of the same cryptography (`timeflareio/mobile-client` `packages/crypto/`), pinned to the shared vectors, running on the device that holds the user's keys; the scope above does not include it today. *Recommendation: second ring, alongside the SDK — it guards user keys rather than consensus, and the vector corpus bounds how far it can drift.*
