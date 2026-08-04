# TODO

Known gaps that are deliberately not fixed yet. Each was found by doing something
real — a failing run, a release, a walked runbook — and each is recorded here
rather than fixed because the fix is a decision, not a mechanical change.

This file is for items with no owner and no plan. Anything with a plan belongs in
`docs/planning/`; anything already decided belongs in the code.

## Nothing verifies that generated code matches the protos

`make proto-gen` output is committed, and no check confirms the commit matches the
`proto/` it came from. CI's protobuf job runs `buf lint` and `buf breaking`, which
compare proto files to proto files.

Two failure directions, and the second is the serious one:

- **Stale generated code.** Regenerating with no proto change rewrites all six
  `.pb.go` files — around 400 lines, entirely inside the gzipped
  `FileDescriptorProto` blobs, zero semantic difference. Those blobs' gzip
  encoding is not stable across Go toolchains, and the committed output predated
  the current one. Re-baselined in `chore: re-baseline the generated protobuf
  code` (August 2026), so the tree is currently consistent — and will drift again
  the next time the toolchain moves.
- **A proto edited without regenerating.** Nothing catches this at all. It leaves
  `x/secrets/types` — the module every integrator pins — describing something
  other than its own `proto/`. Silent, and discovered by a consumer.

A gate would be a `make proto-gen && git diff --exit-code` step, which fails on
either. The complication is the first direction: with encoding unstable across
toolchains, that gate fails for anyone whose Go differs from CI's, so it needs a
pinned generation environment to be honest rather than annoying.

May belong with `docs/planning/PENDING_CI_GATING_PLAN.md`.

## A release can publish from a commit whose E2E is unknown

`release.yml`'s `verify` job gates publication on `make verify && make test`. It
does not run the E2E suite, and `PROTOCOL_CHANGE.md` step 2 says "tag and publish"
without requiring `main`'s CI to be green first.

So a tag pushed while `main`'s E2E is red, or still running, publishes anyway:
binaries, a GHCR image and the protocol surface, at a version number that cannot
be unpublished. Nothing is wrong with the artefacts — `verify` and `test` did
pass — but the claim a version number makes is broader than what was checked.

Worked around by judgement today (checking the merged PR's E2E was green before
tagging `v0.0.3`), which is exactly the kind of step a checklist exists to not
depend on.

Options: require green `main` in the runbook; add E2E to the release gate and
accept ~20 minutes on every release; or state plainly that a release asserts
`verify && test` and nothing more.

## What the vendored SDK tarball's integrity is actually worth

In `timeflareio/typescript-sdk`, `release.yml`'s header comment explains that the
dist-only tarball must be byte-deterministic because *"the mobile client vendors
it and commits a lockfile whose integrity hash covers it"*.

That is overstated, and the correction I first recorded here — that npm maintains
no `integrity` for `file:` dependencies — was simply wrong. Measured on npm 11.4.1:

- npm **can** record it. One appeared in the mobile client's `package-lock.json`
  during the `v0.0.1` → `v0.0.2` re-vendor, and its sha512 matched the tarball
  byte-for-byte.
- npm **does not** do so dependably. Neither `npm install --package-lock-only`
  (which the sync script runs), nor a full `npm install`, nor deleting
  `node_modules/timeflare-sdk` and reinstalling reproduces it. The committed
  lockfile carries no integrity for that entry at all, and its `version` field is
  the tarball's internal `0.1.0`, which does not track the release tag.

So determinism cannot be justified *or* dismissed by the lockfile. The open question
is whether the requirement is worth keeping on other grounds — a vendored artefact
that changes byte-for-byte between identical builds is unpleasant regardless, and
`wasm-opt` output being irreproducible is why WASM is excluded from that tarball —
and if it is kept, the comment should say so without citing a guard that does not
reliably exist.

What does guarantee the pairing is `make sdk-verify` in the mobile client: it
re-downloads the pinned release and byte-compares. Recorded here rather than in
that repository so the deferred items stay in one place; the comment fix belongs
there.
