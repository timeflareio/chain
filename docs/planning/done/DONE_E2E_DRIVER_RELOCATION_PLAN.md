# E2E Driver Relocation Plan

- **Priority**: P2 — release-engineering friction: every harness-side driver
  change required a typescript-sdk release and a pin bump, however chain-local
  the change actually was.
- **Status**: done (26 August 2026 — phase 1 merged as chain PR #30, phase 2 as
  typescript-sdk PR #16 with SDK v0.0.8 released bundle-less, phase 3's pin
  bump validated by the full pinned suites, phase 4's workspace root update
  applied. The owner's rulings of 9 August 2026 — key name `user`,
  `monitor-secrets.js` moves, the release cut and validated within this plan —
  are folded into the body.)
- **Origin**: design session with the owner, 9 August 2026. Trigger: removing
  the devnet test account's bootstrap-time funding was a chain-local edit, but
  removing the account's *name* was not — the name is hardcoded inside the
  SDK's released examples bundle, turning a cosmetic devnet rename into a
  cross-repository release. The owner ruled that the e2e driver belongs with
  the harness it is half of, and that wallet identity must never cross a
  repository boundary (owner, August 2026).
- **Components**:
  - **chain**: `devnet/e2e/` (new — argued under "Architectural minimalism"),
    `make/devnet.mk`, `make/docker.mk`, `devnet/e2e-scenarios.sh`,
    `devnet/users/setup-test-users.sh`, `devnet/versions.env`,
    `.github/workflows/ci.yml`, `PROTOCOL_CHANGE.md`, `COMPATIBILITY.md`
    (prose only), `devnet/README.md`, `docs/guides/CONTAINERS.md`,
    `docs/guides/TESTING_COMMANDS.md`, `.gitignore`
  - **typescript-sdk**: `examples/`, `.github/workflows/release.yml`,
    `CLAUDE.md`
  - **workspace root `CLAUDE.md`**: the dependency diagram and the
    "Where the pins live" table
  - **unaffected, confirmed by sweep**: guardian, mobile-client (both consume
    the SDK's dist tarball or nothing; neither touches the examples bundle)
  - **noted, not executed here**: the "George, the pre-funded test user"
    fixture row in `network/docs/planning/PENDING_TEST_BRAVO_PLAN.md`
    is stale once this lands; that plan's owner amends it when it is next
    touched.

## Problem

The chain's e2e suites are driven by JavaScript that lives in the
typescript-sdk repository under `examples/` and reaches this repository as a
release asset — the `timeflare-sdk-examples-<tag>.tar.gz` bundle, fetched by
`sdk-sync` at the `SDK_VERSION` pinned in `devnet/versions.env`.

The directory name is a misnomer. Its contents are:

| Script | Caller |
|---|---|
| `secret-lifecycle.js` | `make e2e` / `make docker-e2e` — the happy-path suite itself |
| `generate-keypair.js` | the `e2e`/`e2e-scenarios` targets, to mint the recipient keypair |
| `scenario-create.js` | `devnet/e2e-scenarios.sh`, per scenario |
| `rebate-proof.js` | `devnet/e2e-scenarios.sh` (S10 rebate collection) |
| `courier-bootstrap.mjs` | `devnet/e2e-scenarios.sh` (S11 funded claim kit) |
| `monitor-secrets.js` | a devnet inspection utility (`docs/guides/TESTING_COMMANDS.md`) |
| `lib.js` | shared wiring for all of the above |

None of this is documentation: the SDK's README never links it, and `lib.js`
carries an escape hatch (`loadWasmPrimitives`) whose own comment says it exists
for "the scenario tooling ONLY" and is not part of the SDK API. These scripts
are the **client half of the chain's e2e harness**, written in JavaScript
because the client side of the protocol — sealing, share distribution,
reconstruction — exists only in the SDK and its WASM crypto. They sit in the
SDK repository because that is where they were when the monorepo split, and the
SDK's `DONE_PUBLICATION_FOOTPRINT_PLAN.md` standardised the two-tarball release
around them rather than questioning their placement.

The placement has three costs:

1. **The cross-repository contract is wider than anyone intended.** Harness
   facts leak across the boundary unversioned: the devnet wallet's key name is
   hardcoded on both sides (`setup-test-users.sh` creates it; the bundled
   `lib.js` runs `timeflared keys export <name>`), so renaming a devnet test
   key requires an SDK release and a pin bump.
2. **Every driver change is an SDK release**, even when the SDK's public
   surface is untouched — new scenario tooling, changed assertions, harness
   refactors all ride another repository's release cadence.
3. **Dedicated machinery exists only to bridge the gap**: the `sdk-sync` /
   `sdk-install` fetch-and-install targets here, and a second release tarball
   in the SDK whose only consumer is this repository.

## Design

The driver moves into this repository as **`devnet/e2e/`** — a private npm
package (not published anywhere) that consumes the SDK's **public dist
tarball** the same way the mobile client already does: pinned by release-asset
URL in a manifest, with a committed lockfile whose integrity hash makes the pin
verifiable.

- **`devnet/e2e/package.json`** declares, by release-asset URL:
  - `@timeflareio/typescript-sdk` — the dist tarball (`timeflare-sdk-<tag>.tgz`)
  - `@timeflareio/crypto` — the WASM bundle, which `loadWasmPrimitives`
    resolves for the scenario suite's raw-primitive needs
  - the scripts' direct third-party imports (today `@cosmjs/encoding` only;
    everything else arrives transitively through the SDK package). Enumerate by
    grep at execution — a direct import satisfied only transitively is a latent
    breakage.

  The committed `package-lock.json` is the pin. "Every edge is a released
  artefact at a pinned version" is preserved in full — the pin's home moves
  from `versions.env` + download script to manifest + lockfile, the mechanism
  this workspace already trusts for the mobile client.

- **The scripts move with two adaptations**: `require('../dist')` becomes
  `require('@timeflareio/typescript-sdk')`, and wallet loading takes the key
  name from the harness's own configuration (a single definition shared with
  `setup-test-users.sh`) rather than a literal. `RECIPIENT_KEYPAIR`,
  `TIMEFLARE_HOME`, `RPC_ENDPOINT` and the other environment contracts are
  unchanged.

- **Make targets**: `sdk-sync` and `sdk-install` are replaced by one install
  step (`npm ci` in `devnet/e2e/`, target name `e2e-deps`) that `e2e`,
  `e2e-scenarios`, `docker-e2e` and `docker-e2e-scenarios` depend on.
  `E2E_TEST`/`E2E_KEYGEN` and the `${SDK_DIR}/examples/…` invocations in
  `devnet/e2e-scenarios.sh` point at `devnet/e2e/`.

- **The `SDK_DIR` escape hatch keeps its job with new mechanics**:
  `make e2e SDK_DIR=~/dev/timeflareio/typescript-sdk` runs
  `npm install --no-save "file:$SDK_DIR"` into the harness (the working tree
  must be built), substituting the SDK library while the driver stays this
  repository's own; a later `npm ci` restores the pinned artefact. As today,
  the override is never valid for a release-train verification run.

- **`devnet/versions.env`** keeps only `GUARDIAN_VERSION`. `.devnet/sdk`
  ceases to exist; `.gitignore` gains `devnet/e2e/node_modules`.

- **`ci.yml` e2e job**: the prerequisite check that reads `SDK_VERSION` from
  `versions.env` is replaced by the install step failing loudly (`npm ci`
  against a manifest with no valid pin cannot succeed silently). The job's
  network dependency — fetching a GitHub release asset — is the same one
  `sdk-sync` had.

### The devnet test account

With the driver chain-local, the test key's name is a private fact of this
repository, and the rename that motivated this plan folds in as a step: the
key currently named `george` becomes **`user`** (ruled by the owner,
9 August 2026) — the name matches the account's home (`$TIMEFLARE_HOME/user`),
and the account plays several roles across the scenarios (creator, rebate
collector, courier funding source), so a role name would mislead. The rename
lands across `devnet/users/setup-test-users.sh`, `devnet/e2e-scenarios.sh`,
the moved `lib.js`, `devnet/README.md`, `docs/guides/CONTAINERS.md` and the
`make/docker.mk` comment. Funding stays an
explicit per-suite action — each of the four suite targets runs
`setup-test-users.sh fund` against the bootstrapping pool; `dev-up` starts the
chain and guardians only.

### None of the driver is SDK helper material

Audited 9 August 2026, script by script, against the SDK's public surface
(`src/protocol/`) — the gate being that relocating a *capability* into a test
harness would bury API a client needs, whereas relocating *harness opinion*
costs nothing. Every protocol operation the scripts perform is already public
SDK API; what remains in them is harness opinion no client consumes:

- **`secret-lifecycle.js`** composes `CommitSession`, the acceptance/reveal
  watchers, `reconstructSecret`, `decryptReconstructed` and `discoverSecrets` —
  all exported. Its only opinions are test parameters (band shape, payload
  size).
- **`generate-keypair.js`** makes one API call (`keygenX25519`) and writes the
  devnet's keypair file format — a harness fixture contract
  (`RECIPIENT_KEYPAIR`), not a client format.
- **`courier-bootstrap.mjs`** exercises `generateCourier`, `sweepCourier`,
  `courierSeedUveil`, `encodeClaimUri`/`parseClaimUri` and
  `walletFromMnemonic` — all exported; the remainder is drill assertions.
- **`monitor-secrets.js`** composes exported queries and recipient machinery.
  Its assignment-status labels and terminal-states list are the sole lines in
  any script restating protocol vocabulary; should a client ever need that
  vocabulary, it belongs in the SDK's constants as its own change — nothing
  about this relocation forecloses it.
- **`scenario-create.js`** is the inverse of helper material: it performs the
  seal manually through the raw-WASM escape hatch precisely to retain the
  plaintext key-share envelopes that the public `sealSecret` **discards by
  design** (they are early-reveal evidence). Shipping a worked example of
  bypassing that safety property inside the SDK's own release asset blurs the
  API boundary; housing it with the scenario suite sharpens it.
- **`rebate-proof.js`** is wallet-shaped — hint fetch, `scanHint` gate,
  `recipiencyProof`, `rebateCommitment` over raw account bytes — and the same
  composition exists independently in the mobile client
  (`app/src/state/rebate.ts` + `useRebateCollection.ts`). That duplication is
  already governed by the sanctioned mechanism: the primitives stay on the
  `CryptoProvider` API, and the arithmetic is pinned across all three
  implementations (chain Go, WASM Rust, mobile TS) by the shared
  `rebate_commitment.json` vector corpus. The mobile client deliberately does
  not consume this through the SDK seam, so promoting the composition to an
  SDK helper would be a new API feature needing adoption on its own merits —
  this plan neither forces nor forecloses it.

### Architectural minimalism

This repository gains a package manifest, but the workspace as a whole loses
more: the SDK's second release tarball, the `sdk-sync`/`sdk-install` machinery,
and an unversioned cross-repository contract. After the change, one consumption
pattern — manifest + lockfile pinning a release asset — covers every JavaScript
consumer of the SDK, and the SDK releases exactly one artefact. This is the
consolidation the working agreement favours, not a new parallel implementation.

## Execution

### Phase 1 — chain (one PR, this repository)

1. Create `devnet/e2e/` from the v0.0.7 bundle's scripts, adapted as designed;
   manifest pins SDK `v0.0.7` and crypto `v0.0.2` (the pairing
   `COMPATIBILITY.md` already records for chain `v0.0.5`); commit the lockfile.
2. Rewire `make/devnet.mk`, `make/docker.mk` and `devnet/e2e-scenarios.sh`.
3. Rename the test key to `user`.
4. Documentation sweep: `devnet/README.md` (tree listing, `SDK_DIR` text),
   `docs/guides/CONTAINERS.md`, `docs/guides/TESTING_COMMANDS.md` (whose
   `typescript-sdk/examples/monitor-secrets.js` path is monorepo-era and gets
   corrected by the move), `devnet/versions.env` comments, `PROTOCOL_CHANGE.md`
   steps 4–6 (the SDK leg becomes "bump the pin in `devnet/e2e/package.json`"),
   `COMPATIBILITY.md` prose on where the SDK column is read from.
5. Removal audit (grep-driven, per the planning rules): `george`, `sdk-sync`,
   `sdk-install`, `SDK_VERSION`, `examples/` across this repository, and
   confirm guardian and mobile-client are clear — not merely unchanged.
6. Prove the `SDK_DIR` escape hatch against a built SDK working tree — the
   `npm install --no-save file:` mechanics (npm symlinks directory installs,
   which changes how `require` resolves) are confirmed here, as a dev-loop
   feature check distinct from the pinned verification runs.
7. Verify: claim the devnet lock, then `make e2e` and `make e2e-scenarios`
   native and `make e2e-full` against the compose stack — all against pinned
   artefacts, no `SDK_DIR`/`GUARDIAND_BIN` overrides.

### Phase 2 — typescript-sdk (after phase 1 lands)

1. Remove `examples/`; drop the examples-bundle packing step and its
   release-notes rows from `.github/workflows/release.yml`; update `CLAUDE.md`
   (the Releases section describes one tarball; the "no path may be derived"
   section keeps its rule with its remaining subjects).
2. Bump the manifest version and tag: the release publishes the dist tarball
   only (`release.yml` refuses a release where manifest and tag disagree).
   Earlier tags keep their published bundles, so chain releases pinned to them
   remain reproducible.

### Phase 3 — chain: adopt and validate the bundle-less release

1. Bump `@timeflareio/typescript-sdk` in `devnet/e2e/package.json` to the new
   release's dist tarball URL; refresh the lockfile.
2. Claim the devnet lock, then run the full suites on chain — `make e2e`,
   `make e2e-scenarios` native and `make e2e-full` against the compose stack —
   all against pinned artefacts. This validates the first bundle-less SDK
   release end to end and demonstrates the harness consumes nothing but the
   dist tarball.

### Phase 4 — workspace root `CLAUDE.md`

1. The dependency diagram's SDK edges become
   `typescript-sdk ── dist tarball ──► mobile-client, chain/devnet/e2e (test-time)`.
2. The pin table's `chain/devnet/versions.env` row shrinks to
   `GUARDIAN_VERSION`, and a row is added for `chain/devnet/e2e/package.json`
   + lockfile; the cross-repo escape-hatch example is updated to the new
   `SDK_DIR` mechanics.

The next chain release's `COMPATIBILITY.md` row records the SDK version from
the harness manifest; no historical row changes.

## What this plan does not solve

- **The SDK ends with no examples at all.** Documentation examples for
  integrators — written as documentation, against the public surface only —
  are a separate concern for that repository, taken up when wanted; nothing
  here blocks or supplies them.
- **The devnet remains machine-global.** Verification runs still claim
  `chain.lock`; this plan changes where the driver lives, not how the devnet
  is shared.
- **The stale fixture row in the network repository's testnet launch plan** is
  recorded under Components as an edge for its owner, not changed here.

## Related plans

- `docs/planning/done/DONE_E2E_SCENARIO_DETERMINISM_PLAN.md` — the scenario
  suite this plan relocates the client half of.
- `docs/planning/PENDING_CI_GATING_PLAN.md` — discusses the e2e job's
  prerequisite gating, which phase 1 step 4 touches.
- typescript-sdk `docs/planning/done/DONE_PUBLICATION_FOOTPRINT_PLAN.md` — the
  rationale for the two-tarball release this plan reduces to one.
