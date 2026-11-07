# syntax=docker/dockerfile:1

# Pure-Go build: audio output (oto/purego) dlopens ALSA at runtime, so no C
# toolchain or ALSA headers are needed to compile. CGO stays off, which
# yields a fully static binary that runs on the musl-based runtime below.
FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine3.24 AS builder

ENV CGO_ENABLED=0

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X github.com/p-society/raag/internal/version.Version=${VERSION} -X github.com/p-society/raag/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/raagd ./cmd/raagd && \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X github.com/p-society/raag/internal/version.Version=${VERSION} -X github.com/p-society/raag/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/raag ./cmd/raag

FROM alpine:3.24 AS runtime

ARG RAAG_DATA_DIR=/home/raag/.local/share/raag

# alsa-lib is dlopened at runtime by oto for audio output; ca-certificates
# for any TLS dials. machine-id (used for the BadgerDB encryption key) is
# absent in alpine-minimal, so bake a stable random ID to keep the DB key
# stable across container restarts of the same image.
RUN apk add --no-cache ca-certificates alsa-lib \
    && addgroup -g 1000 raag \
    && adduser -D -u 1000 -G raag -h /home/raag -s /bin/false raag \
    && cat /proc/sys/kernel/random/uuid > /etc/machine-id

WORKDIR /home/raag

COPY --from=builder --chown=raag:raag --chmod=755 /out/raagd /out/raag ./
COPY --from=builder --chown=raag:raag --chmod=755 /app/entrypoint.sh ./

USER raag

RUN mkdir -p .config/raag ${RAAG_DATA_DIR}

ENV RAAG_DATA_DIR=${RAAG_DATA_DIR}

HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD ["./raag", "--socket", "/home/raag/.local/share/raag/raag.sock", "daemon", "status"] || exit 1

EXPOSE 7844/tcp 7844/udp 7845/udp 7846/udp

ENTRYPOINT ["./entrypoint.sh"]
