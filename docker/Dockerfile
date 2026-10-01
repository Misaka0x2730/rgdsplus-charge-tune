# syntax=docker/dockerfile:1
#
# Builds rgdsplus-charge-tune for linux/arm64. On an Apple Silicon Mac this
# runs natively (no emulation) and cgo needs no cross toolchain.
#
#   docker buildx build --platform linux/arm64 -f docker/Dockerfile \
#       --target export --output type=local,dest=build/arm64 .
#
# Base: quasimodo (Grout's gabagool image): Debian bullseye, glibc 2.31,
# SDL2 dev packages. The binary needs glibc 2.17; the console has 2.41.
# Pinned by digest; Dependabot (.github/dependabot.yml) proposes updates.
ARG BASE_IMAGE=ghcr.io/brandonkowalski/quasimodo:latest@sha256:c7201b5bc411c6cee4557940a0c12b1587ab046bb6e27bd0e497ac6ab6a6504b
FROM ${BASE_IMAGE} AS build

# quasimodo ships Go 1.25; golang.org/x/image and friends need >= 1.26.
ARG GO_VERSION=1.27.1
RUN ARCH="$(dpkg --print-architecture)" && \
    rm -rf /usr/local/go && \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}" \
    GOTOOLCHAIN=local \
    CGO_ENABLED=1

WORKDIR /src
COPY go.mod go.sum ./
# gabagool is a local fork (replace directive); its go.mod lists its deps.
COPY third_party/gabagool/go.mod third_party/gabagool/go.sum ./third_party/gabagool/
RUN --mount=type=cache,target=/root/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG DEBUG=0
ARG DLV_VERSION=v1.27.2
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -e; \
    BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
    LDFLAGS="-X rgdsplus-charge-tune/internal/version.Version=${VERSION} \
             -X rgdsplus-charge-tune/internal/version.GitCommit=${GIT_COMMIT} \
             -X rgdsplus-charge-tune/internal/version.BuildDate=${BUILD_DATE}"; \
    mkdir -p /out; \
    if [ "$DEBUG" = "1" ]; then \
        go build -gcflags='all=-N -l' -ldflags "$LDFLAGS" -o /out/rgdsplus-charge-tune ./cmd/rgdsplus-charge-tune; \
        GOBIN=/out go install github.com/go-delve/delve/cmd/dlv@${DLV_VERSION}; \
    else \
        go build -trimpath -ldflags "-s -w $LDFLAGS" -o /out/rgdsplus-charge-tune ./cmd/rgdsplus-charge-tune; \
    fi; \
    ldd --version | head -1 > /out/BUILD_GLIBC; \
    objdump -T /out/rgdsplus-charge-tune | grep -o 'GLIBC_[0-9.]*' | sort -Vu | tail -1 > /out/REQUIRED_GLIBC

FROM scratch AS export
COPY --from=build /out/ /
