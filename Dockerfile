FROM --platform=$BUILDPLATFORM golang:1.26.0-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    libasound2-dev \
    gcc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X github.com/p-society/raag/internal/version.Version=${VERSION} -X github.com/p-society/raag/internal/version.BuildTime=${BUILD_TIME}" \
    -o raagd ./cmd/raagd && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X github.com/p-society/raag/internal/version.Version=${VERSION} -X github.com/p-society/raag/internal/version.BuildTime=${BUILD_TIME}" \
    -o raag ./cmd/raag

FROM debian:bookworm-slim AS runtime

ARG RAAG_DATA_DIR=/home/raag/.local/share/raag

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libasound2 \
    && rm -rf /var/lib/apt/lists/* \
    && (getent group raag >/dev/null || groupadd --gid 1000 raag) \
    && (getent passwd raag >/dev/null || useradd -o --uid 1000 --gid raag --shell /bin/false --create-home raag)

# machineid (used for the BadgerDB encryption key) reads /etc/machine-id,
# which is absent in bookworm-slim. Bake a stable random ID so the DB key
# survives container restarts and is stable per-image.
RUN sh -c 'cat /proc/sys/kernel/random/uuid > /etc/machine-id'

WORKDIR /home/raag

COPY --from=builder /app/raagd /app/raag ./
COPY --from=builder /app/entrypoint.sh ./

RUN mkdir -p /home/raag/.config/raag ${RAAG_DATA_DIR} && \
    chmod +x entrypoint.sh && \
    chown -R raag:raag /home/raag

USER raag

ENV RAAG_DATA_DIR=${RAAG_DATA_DIR}

HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD ["./raag", "--socket", "/home/raag/.local/share/raag/raag.sock", "daemon", "status"] || exit 1

EXPOSE 7844/tcp 7844/udp 7845/udp 7846/udp

ENTRYPOINT ["./entrypoint.sh"]
