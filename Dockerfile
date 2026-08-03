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

FROM golang:1.26.5 AS build
WORKDIR /src

# Module graph first so source edits don't re-download modules. The nested
# types module is named here because the root go.mod's replace points at it.
COPY go.mod go.sum ./
COPY x/secrets/types/go.mod x/secrets/types/go.sum ./x/secrets/types/
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
# Mirrors make/common.mk ldflags so `timeflared version` inside the image
# reports the stamped build
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
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
