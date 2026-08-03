# Compatibility matrix

Which versions belong together. One row per chain release, appended by the
release train (`PROTOCOL_CHANGE.md` step 6).

This file exists because the components version independently. That is
deliberate — a keeper-only chain release should not oblige the guardian to move,
and a crypto release should not wait for the chain — but independence means the
pairing is recorded nowhere else. A missing or stale row makes it unanswerable.

**A row is a claim that was tested**, not an assumption: it is added only after
`make e2e` and `make e2e-scenarios` pass against those exact pinned artefacts.

| Chain | Wire contract | Guardian | Crypto | SDK | Mobile | Chain vectors | Primitive vectors |
|---|---|---|---|---|---|---|---|
| `v0.0.1` | `x/secrets/types/v0.0.1` | `v0.0.2` | `v0.0.1` | — | — | in-repo | `crypto v0.0.1` |

## Reading the columns

- **Chain** — the node. `timeflared` binaries and the GHCR image.
- **Wire contract** — `x/secrets/types`, tagged separately in this repository.
  This is what Go integrators pin, and what the guardian requires.
- **Guardian** — `guardiand`. The chain's devnet pins this in
  `devnet/versions.env`.
- **Crypto** — the primitives, consumed as a Go module by this repository and
  the guardian, and as a WASM bundle by the SDK.
- **SDK / Mobile** — not yet lifted into their own repositories (migration
  phases 4 and 5), so they have no releases to name.
- **Chain vectors** — the six chain-semantics vector files this repository owns
  and publishes. "in-repo" means the release predates the vectors tarball.
- **Primitive vectors** — the five files `timeflareio/crypto` owns. Named
  separately because a primitive change and a chain-semantics change are
  different events with different blast radii.

## Two corpora, deliberately

Vectors are split by which repository *implements* the behaviour they pin. The
chain asserts none of the primitive files, and crypto asserts none of the
chain-semantics files — there is no overlap. Recording both versions here is
what stops a consumer silently testing yesterday's protocol against today's
code, which checksums alone cannot catch: a checksum proves fidelity to a pin,
never that the pin is current.
