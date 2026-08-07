# Compatibility matrix

Which versions belong together. One row per chain release, appended by the
release train (`PROTOCOL_CHANGE.md` step 6).

This file exists because the components version independently. That is
deliberate — a keeper-only chain release should not oblige the guardian to move,
and a crypto release should not wait for the chain — but independence means the
pairing is recorded nowhere else. A missing or stale row makes it unanswerable.

**A row is a claim that was tested**, not an assumption: it is added only after
`make e2e` and `make e2e-scenarios` pass against those exact pinned artefacts.

The suites run at `TIMEFLARE_BLOCK_TIME=1s`, which is the cadence
`devnet/e2e-scenarios.sh` documents and CI uses — the protocol is denominated in
blocks, so cadence changes what a window costs in seconds and nothing about what
it means. Where a row's suites also ran with `TIMEFLARE_RETENTION_BLOCKS` and
`TIMEFLARE_KEY_ROTATION_MIN_INTERVAL` reduced, S5 and S8 ran too rather than
skipping, and the row covers the whole suite.

| Chain | Wire contract | Guardian | Crypto | SDK | Mobile | Chain vectors | Primitive vectors |
|---|---|---|---|---|---|---|---|
| `v0.0.5` | `x/secrets/types/v0.0.4` | `v0.0.5` | `v0.0.1` Go, `v0.0.2` npm | `v0.0.7` | untagged, `sdk v0.0.7` | `chain v0.0.5` (SDK), `v0.0.4` (guardian) | `crypto v0.0.2`, two files |
| `v0.0.4` | `x/secrets/types/v0.0.3` | `v0.0.4` | `v0.0.1` Go, `v0.0.2` npm | `v0.0.5` | untagged, `sdk v0.0.3` | `chain v0.0.3` (SDK), `v0.0.2` (guardian) | `crypto v0.0.2`, two files |
| `v0.0.3` | `x/secrets/types/v0.0.2` | `v0.0.3` | `v0.0.1` | `v0.0.2` | untagged, `sdk v0.0.2` | `chain v0.0.3` | `crypto v0.0.1` |
| `v0.0.1` | `x/secrets/types/v0.0.1` | `v0.0.2` | `v0.0.1` | — | — | in-repo | `crypto v0.0.1` |

**`v0.0.2` has no row.** It was released before the release train was walked, and
a row is a claim that was tested. Its component set was never verified together,
so naming one would be inventing the claim this file exists to record.

### Why the numbers in the `v0.0.4` row disagree

More than any row so far, because this release changed how artefacts travel
rather than what they contain. Each split is a real answer to a different
question:

- **Crypto is `v0.0.1` as a Go module and `v0.0.2` as an npm package.** Both are
  the same primitives — `v0.0.2` changed only what the release publishes — so the
  chain and the guardian had no reason to move their `go.mod` pin. The SDK moved
  because the WASM bundle it consumes is now a dependency it resolves by version.
  A single number in this column would have to be wrong for one of them.
- **The guardian cites chain vectors `v0.0.2` while the SDK cites `v0.0.3`.**
  `guardian v0.0.4` still carries vendored copies at its own pin; the release that
  reads them from `x/secrets/types` instead is not cut yet. The SDK's
  `CHAIN_VERSION` covers protobufs *and* vectors, so it tracks the chain release
  it generates from.
- **Mobile vendors `sdk v0.0.3`, two releases behind.** It moved to the released
  package while `v0.0.4` was being cut. Neither later release changes anything
  mobile uses: `v0.0.4` adds the crypto dependency and `v0.0.5` fixes the
  examples' raw-WASM escape hatch, and mobile uses neither — its runtime crypto is
  the native JSI binding, and it ships no examples.
- **Primitive vectors are two files, not five.** Only `low_order_keys` and
  `rebate_commitment` reach an implementation outside `timeflareio/crypto`, so
  only those are published — inside the WASM package. The other three pin crypto's
  own Go and Rust suites against each other and are not published at all.

### Why the numbers in the `v0.0.3` row disagree

They do not, and the row is the first place anyone will suspect a mistake — so
plainly:

- **Chain `v0.0.3` against wire contract `v0.0.2`.** Separate tag namespaces on
  separate cadences. The chain has had releases that moved no wire contract (a
  dependency bump, the devnet fixes), and an unchanged contract must not get a new
  tag or the tag stops meaning anything to whoever pins it.
- **Guardian `v0.0.3` citing chain vectors `v0.0.2` while the SDK cites chain
  `v0.0.3`.** The guardian pins its corpus separately
  (`CHAIN_VECTORS_VERSION`), and that corpus did not move, so it did not move its
  pin. The SDK's single `CHAIN_VERSION` covers protobufs *and* vectors, so its
  number tracks the chain release it generates from. Both files are byte-identical
  across chain `v0.0.2` and `v0.0.3`; the two repos are answering different
  questions — "which content am I asserting" versus "which release am I built
  against".
- **Mobile has no version of its own.** It ships to stores, so nothing downstream
  pins it; what matters is the SDK release it vendors.

## Reading the columns

- **Chain** — the node. `timeflared` binaries and the GHCR image.
- **Wire contract** — `x/secrets/types`, tagged separately in this repository.
  This is what Go integrators pin, and what the guardian requires.
- **Guardian** — `guardiand`. The chain's devnet pins this in
  `devnet/versions.env`.
- **Crypto** — the primitives. Two consumption routes with independent pins: a Go
  module for this repository and the guardian, and the `@timeflareio/crypto` npm
  package (the WASM bundle) for the SDK. Both numbers are recorded when they
  differ.
- **SDK** — `timeflareio/typescript-sdk`. The chain's devnet pins this in
  `devnet/versions.env` too: its examples drive the e2e suites.
- **Mobile** — `timeflareio/mobile-client`. Never tagged: it ships to stores, so
  nothing downstream pins it. The column records the SDK release it vendors,
  which is the only version anyone else can act on.
- **Chain vectors** — the six chain-semantics vector files this repository owns.
  All six ship in the release tarball; the two with a Go consumer outside this
  repository also travel inside `x/secrets/types`, so a consumer reading them from
  the module cites the wire contract rather than a chain tag. "in-repo" means the
  release predates the vectors tarball.
- **Primitive vectors** — the files `timeflareio/crypto` owns. It owns five and
  publishes two, inside the WASM package: those are the ones an implementation
  outside that repository asserts. Named separately from the chain vectors because
  a primitive change and a chain-semantics change are different events with
  different blast radii.

## Two corpora, deliberately

Vectors are split by which repository *implements* the behaviour they pin. The
chain asserts none of the primitive files, and crypto asserts none of the
chain-semantics files — there is no overlap. Recording both versions here is
what stops a consumer silently testing yesterday's protocol against today's
code, which checksums alone cannot catch: a checksum proves fidelity to a pin,
never that the pin is current.
