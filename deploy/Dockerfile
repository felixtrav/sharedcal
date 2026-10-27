# syntax=docker/dockerfile:1.7
#
# SharedCal container image.
#
#   docker buildx build -f deploy/Dockerfile --platform linux/amd64,linux/arm64 \
#     --build-arg VERSION=1.0.0 -t ghcr.io/<org>/sharedcal:1.0.0 .
#
# Builder stages run on the build host's native platform ($BUILDPLATFORM) and
# cross-compile, so multi-arch builds never run under QEMU emulation.
# Base images are pinned by tag *and* digest; Renovate keeps both current.

ARG NODE_IMAGE=docker.io/library/node:24-bookworm-slim@sha256:55fb5f5a527ea2c5c4a95f963e48ef90aa38c7b346054fdc8ee9957a2b55d205
ARG GO_IMAGE=docker.io/library/golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# ---- 1. Web app (platform-independent static files) -------------------------
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- 2. Go binary (cross-compiled for the target platform) ------------------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/sharedcal ./cmd/sharedcal
# distroless has no shell, so prepare the data directory here; it is copied
# with the right owner so a fresh named volume is writable by uid 65532.
RUN mkdir -p /out/data

# ---- 3. Runtime --------------------------------------------------------------
FROM ${RUNTIME_IMAGE}
ARG VERSION=dev
LABEL org.opencontainers.image.title="SharedCal" \
      org.opencontainers.image.description="Self-hosted shared calendar with OIDC sign-in, live updates, comments and suggestions" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/felixtrav/sharedcal"
COPY --from=build /out/sharedcal /sharedcal
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV DATA_DIR=/data \
    LISTEN_ADDR=:8080
EXPOSE 8080
VOLUME ["/data"]
# No shell in distroless: the binary probes its own /healthz.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/sharedcal", "healthcheck"]
ENTRYPOINT ["/sharedcal"]
CMD ["serve"]
