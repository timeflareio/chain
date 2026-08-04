# timeflared — validator/full-node image
#
# The build context is this repository's root:
#   docker build -t timeflare/timeflared:dev \
#     --build-arg VERSION=$(git describe --tags --always) \
#     --build-arg COMMIT=$(git rev-parse HEAD) .
#
# Pure Go, no cgo: the binary is static, so the runtime image is distroless —
# tens of MB, no libc, no shell. The builder pins the exact go.mod toolchain.
#
# No workspace. The chain consumes github.com/timeflareio/crypto as a pinned
# public module, and x/secrets/types is a nested module resolved by the in-repo
# replace, so only this module's go.mod/go.sum need copying.
#
# Version arrives via build args rather than from git: .dockerignore excludes
# .git to keep the context small, so the toolchain cannot derive it here.

# --platform=$BUILDPLATFORM pins the BUILDER to the machine doing the building;
# the compiler then cross-compiles to $TARGETARCH below. Without it, a
# multi-platform build — the release workflow builds linux/amd64 and linux/arm64 —
# runs this whole stage inside an emulated container for every foreign target: the
# entire cosmos-sdk graph compiled under QEMU. That cost v0.0.2 a 43-minute image
# job while the rest of the release finished in three.
#
# Safe because there is no cgo. CGO_ENABLED=0 already makes this a pure-Go static
# build, which is precisely the case Go cross-compiles natively.
#
# Hermeticity is unchanged: the toolchain is still the pinned golang image and the
# source is still compiled inside the container. This is NOT the devnet's
# Dockerfile.prebuilt path, which copies host-built binaries in and is
# deliberately kept away from release images.
FROM --platform=$BUILDPLATFORM golang:1.26.5 AS build
WORKDIR /src

# Module graph first so source edits don't re-download modules. The nested
# types module is named here because the root go.mod's replace points at it.
COPY go.mod go.sum ./
COPY x/secrets/types/go.mod x/secrets/types/go.sum ./x/secrets/types/
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
# Supplied by BuildKit per target platform. GOOS is not needed: every target here
# is linux, which is what the builder image already is.
ARG TARGETARCH
# Mirrors make/common.mk ldflags so `timeflared version` inside the image
# reports the stamped build
#
# The build cache is keyed per architecture. Sharing one cache mount across
# targets would have amd64 and arm64 objects overwrite each other, turning the
# cache from a saving into a liability on every alternating build.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build,id=go-build-$TARGETARCH \
    CGO_ENABLED=0 GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-X github.com/cosmos/cosmos-sdk/version.Name=timeflare \
              -X github.com/cosmos/cosmos-sdk/version.AppName=timeflare \
              -X github.com/cosmos/cosmos-sdk/version.Version=${VERSION} \
              -X github.com/cosmos/cosmos-sdk/version.Commit=${COMMIT}" \
    -o /out/timeflared ./cmd/timeflared

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="timeflared" \
      org.opencontainers.image.source="https://github.com/timeflareio/chain" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"

COPY --from=build /out/timeflared /usr/local/bin/timeflared

# p2p, RPC, REST API, gRPC
EXPOSE 26656 26657 1317 9090
# Data home: mount a named volume at /home/nonroot/.timeflare. Deliberately
# no VOLUME directive — it would create a root-owned anonymous volume on
# every bare `docker run` (breaking even `version` for the nonroot user)
# and leak volumes; explicit mounts don't need it.

ENTRYPOINT ["timeflared"]
