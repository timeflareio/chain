# Security Audit Readiness Plan

**Status**: Proposed (automated review, July 2026)
**Priority**: P1 — programme-level; gates mainnet
**Components**: documentation programme + open-issue triage (absorbed the deleted `docs/guides/UNSOLVED.md`); coordinates all P0 security plans

## What this plan does

Prepares the project to commission — and pass — an external security audit: a written threat model, triage and disposition of every known-unsolved issue, a self-assessment against the findings of this review, and the audit engagement plan itself. This is the programme wrapper around the technical hardening plans (GUARDIAN_SELECTION_HARDENING, SETTLEMENT_AND_STATE_INTEGRITY, CONSENSUS_CRYPTO_PURE_GO, CRYPTO_ASSURANCE).

## Why

- The since-deleted `docs/guides/ROLLOUT_PLAN.md` (removed August 2026 — pre-bonded-economics, superseded by per-network rollout plans) named an 8–12 week audit programme with multiple firms; none of it started.
- The since-deleted `docs/guides/UNSOLVED.md` (removed August 2026) collected unmitigated issues with no owner, severity, or disposition; its triage lives here (Phase 2) — most entries were resolved or superseded by later rulings, leaving the reporter-spam disposition below and client upgrade-compatibility (owned by SDK_PRODUCTIONISATION).
- Auditors price and scope by what you hand them. A codebase with a threat model, an invariant catalogue, a fuzz corpus, and a documented accepted-risk register gets a deeper audit for the same money than one where the auditors spend week one reconstructing intent.
- Several accepted risks are currently documented only as asides in spec.md (self-report bounty recapture, invalid-share submission, detection-dependence of early-reveal deterrence, threshold-as-primary-guarantee). These are *good* explicit decisions — they should be consolidated where an auditor (and a user) can find them.

## How

### Phase 1 — Threat model document (`docs/security/THREAT_MODEL.md`)

Structured by adversary, covering at minimum:

- **Malicious creator**: bogus HMACs at distribution (mitigated by guardian pre-acceptance verification — verify this is *mandatory* in guardiand, not skippable), junk payloads (accepted risk), entropy grinding (see selection plan), griefing via mass secret creation.
- **Malicious guardian / cartel**: early reveal economics (self-report recapture bound), collusion at/above threshold, selective no-reveal, sybil registration economics.
- **Malicious reporter**: spam false reports (gas-priced by the consensus fee floor since July 2026; needs a disposition on whether that suffices — see open questions), evidence replay.
- **Malicious validator / MEV**: transaction ordering around reveal windows and first-N-to-confirm races (in-block ordering decides bond slots — a validator can sell slot priority; assess materiality), censorship of reveals near window close (a censored guardian gets slashed — quantify the validator's power to cause slashing).
- **Network observer**: hint-scanning economics, payload-ciphertext harvest-now-decrypt-later (quantum posture statement), linkage analysis across transactions.
- **Infrastructure**: ~~FFI/consensus determinism~~ (resolved — DONE_CONSENSUS_CRYPTO_PURE_GO landed; the consensus path is pure Go), guardian key custody (see GUARDIAN_KEY_CUSTODY).

Each threat: impact, current mitigation, residual risk, disposition (fix / accept / monitor), and a pointer to the fixing plan where one exists.

### Phase 2 — Open-issue triage (absorbs the deleted UNSOLVED.md)

Convert every open issue into the threat-model register with an owner decision: fix-before-testnet, fix-before-mainnet, or accept-with-rationale. The reporter-spam issue specifically needs a disposition before a public network exists. Since July 2026 a report is no longer free — the consensus fee floor prices every submission (~0.01 VEIL at typical gas) and the per-report HMAC verification is metered gas the reporter pays — so the question has narrowed from "spam is free" to whether gas-priced spam still warrants a dedicated answer (a small non-refundable evidence-submission fee, or per-secret one-report-per-address) or is accepted.

### Phase 3 — Audit package assembly

- Invariant catalogue: the economic invariants from spec.md + the six-assertion library from `keeper/invariants_test.go`, in prose an auditor can check the code against.
- Crypto design note: the four-layer encryption architecture (already excellent in spec.md) plus the *implementation* map — which primitive lives where, which implementations are duplicated, where the cross-impl vectors are.
- Known-issues register from Phases 1–2 (auditors verify accepted risks are truly bounded).
- Reproducible build + `make e2e-full` instructions so auditors can run the failure-path scenarios.

### Phase 4 — Engagement

- Scope: chain module + Rust crypto as the core; guardian daemon and SDK as a second ring. Cosmos-experienced firms need a fresh shortlist (the deleted ROLLOUT_PLAN.md carried one from an earlier era of the project — Trail of Bits, Consensys Diligence, OtterSec).
- Sequence with the technical plans: the P0 fixes should land *before* the audit (paying auditors to find what an internal review already found is waste); CRYPTO_ASSURANCE's fuzz corpus should exist before the crypto review.
- Budget for a remediation window + fix-verification pass; publish the report.

## Open questions

1. **Timing anchor**: audit before public testnet, or after a testnet burn-in period (audit sees battle-tested code, but testnet users see unaudited code)? Common practice: internal hardening → testnet → audit → mainnet.
2. **Reporter-spam disposition**: gas-priced today (consensus fee floor); dedicated evidence fee, rate-limiting, or accept? This may need its own small plan once decided.
3. **MEV/ordering around first-N-to-confirm**: is bond-slot priority-selling by proposers material enough to redesign (e.g. randomised slot allocation among same-block confirmations), or an accepted market mechanic?
4. **Disclosure policy**: `package.json` references a security policy URL — does a `SECURITY.md` with a disclosure process and contact exist anywhere? If not, it belongs in Phase 3.
5. **Quantum posture**: X25519 shares harvested today are decryptable by a future quantum adversary; for multi-year time-locks this is inside the threat window arguably *more* than for ordinary TLS. A statement (accepted risk + envelope-version upgrade path) is needed even if the answer is "accepted for v1".
