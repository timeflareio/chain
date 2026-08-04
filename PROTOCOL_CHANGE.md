# Making a protocol change

A protocol change is not done when this repository is green. It is done when
every component that speaks the protocol has moved.

Inside the monorepo that was one pull request, proven by one `make test`. Across
repositories it is a release train, and the failure mode is well documented: the
December 2024 `shareIndex` removal compiled cleanly in the chain and the
TypeScript SDK and still left the guardian broken, because the guardian sat at
arm's length. Every component now sits at arm's length. That episode is the
default topology, not an accident, and this checklist exists because nothing in
CI will notice for you.

## What counts as a protocol change

Any of these:

- a change under `proto/`
- a change to `x/secrets/types` that alters the wire contract — message shapes,
  constants, validation bounds, economics pricing
- a change to what the cryptographic primitives produce (owned by
  `timeflareio/crypto`, not this repository)
- a change to the chain-semantics vectors in `testdata/vectors/`

A keeper-only change that leaves all of the above untouched is not a protocol
change and does not need this.

## The train

Work down the list. Each step waits for the one above, because each consumes a
published artefact of it.

**1. This repository — one pull request**

- [ ] `proto/` updated
- [ ] `x/secrets/types` regenerated and its helpers/constants updated
- [ ] `docs/spec.md` updated in the *same* PR — it is the protocol authority for
      every repository, and a spec that trails the code is worse than no spec
- [ ] `testdata/vectors/` updated if chain semantics moved
- [ ] `make verify && make test` green

**2. Tag and publish**

- [ ] tag `x/secrets/types/vX.Y.Z` — the wire contract consumers pin
- [ ] tag `vX.Y.Z` — publishes binaries, image, proto tarball, vectors tarball

Both namespaces are required when the wire contract moved. Tagging only the
chain leaves consumers unable to pin the change.

**The two numbers are independent and will not match.** They are separate tag
namespaces on separate release cadences: a keeper-only or dependency-only change
moves the chain without moving the wire contract, and after a few of those the
chain is ahead. Do not "catch types up" to the chain's number — an unchanged
wire contract must not get a new tag, or the tag stops meaning anything to the
integrators who pin it. `COMPATIBILITY.md` is what relates the two.

**Every consumer's move is the same two actions**: edit its pin, then run its
sync target. The pin is the thing that carries the change; the sync target only
does what the pin tells it. Running a sync target without moving the pin first
re-fetches the *old* artefact and reports success.

**3. `timeflareio/guardian`**

- [ ] bump `github.com/timeflareio/chain/x/secrets/types` to the new tag
- [ ] `make verify` — which runs `verify-pins`; if the cosmos-sdk pin block moved
      here, the guardian's must move with it or the two daemons build against
      different SDK versions
- [ ] if the chain-semantics vectors changed:
      `make vectors-sync CHAIN_VECTORS_VERSION=vX.Y.Z`. The version is a required
      argument here, not a default — bare `make vectors-sync` re-syncs at the
      currently pinned tag and changes nothing
- [ ] release, so the devnet and operators can pin it

**4. `timeflareio/typescript-sdk`**

- [ ] set `CHAIN_VERSION=vX.Y.Z` in `versions.env` — this is the pin, and both
      targets below read it. There is no `CHAIN_TAG` variable; passing one is
      accepted silently by make and regenerates from the old pin
- [ ] `make proto-sync` and commit the regenerated code
- [ ] `make vectors-sync` if the vectors changed
- [ ] release the dist tarball

**5. `timeflareio/mobile-client`**

- [ ] set `SDK_VERSION` in `versions.env`, then `scripts/sdk-sync.sh` to
      re-vendor the tarball
- [ ] set `CHAIN_VERSION` if the chain-semantics vectors moved — mobile asserts
      `client_conventions` too
- [ ] re-vendor the primitive corpus (`CRYPTO_VERSION` + `scripts/vendor.sh`) if
      those moved — mobile reimplements the primitives natively, so it is the
      component most exposed to a silent primitive change
- [ ] no release: this client ships to stores, so nothing downstream pins it

**6. Point the chain's devnet at what you just released**

- [ ] set `GUARDIAN_VERSION` and `SDK_VERSION` in `devnet/versions.env` to the
      releases cut in steps 3 and 4

This step is not bookkeeping, and skipping it is the most plausible way to run
this whole checklist and prove nothing. Those two pins are what step 7 actually
tests. Leave them and the suites verify the *previous* guardian and SDK against
the new chain — the exact cross-component mismatch this document exists to catch
— and pass, because the old components are internally consistent with each
other.

**7. Close the loop**

- [ ] `make dev-up && make e2e && make e2e-scenarios` against the *pinned*
      artefacts, not local builds. No `GUARDIAND_BIN=` or `SDK_DIR=` overrides:
      those exist for cross-repo development and they substitute a working tree
      for the artefact under test
- [ ] add the row to `COMPATIBILITY.md` naming every version that belongs
      together — after the suites pass, not before. A row is a claim that was
      tested

## What this checklist does not do

It does not make the change atomic. Between steps 2 and 5 the components
disagree, and nothing is red. The checklist, `buf breaking`, `verify-pins` and
the vector corpora catch a miss *after* the fact rather than making it
impossible; that trade was made deliberately when the monorepo was split, and it
is the standing cost of the arrangement.

The mitigation available is speed: a train left half-run is the dangerous state,
so finish it or revert the tag.

**Nothing here is enforced.** Every step is a human reading a list, and the
failure mode is not forgetting a step — it is performing one that quietly does
nothing: a sync target run before its pin moved, a devnet still pinned to
yesterday's artefacts, a command with an argument name that make accepts and
ignores. Each of those *reports success*. When a step's output does not name the
version you expect to see, treat that as a failure of the step, not noise.
