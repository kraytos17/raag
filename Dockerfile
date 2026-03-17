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
    go build -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o tracker ./tracker/cmd/tracker && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o raag ./cmd/raag

FROM debian:bookworm-slim AS runtime

ARG PORT=8080
ARG LIBP2P_PORT=45678
ARG AUTH_SECRET=
ARG TLS_ENABLED=false
ARG TLS_CERT_FILE=
ARG TLS_KEY_FILE=

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libasound2 \
    wget \
    && rm -rf /var/lib/apt/lists/* \
    && (getent group raag >/dev/null || groupadd --gid 1000 raag) \
    && (getent passwd raag >/dev/null || useradd -o --uid 1000 --gid raag --shell /bin/false --create-home raag)

WORKDIR /home/raag

COPY --from=builder /app/tracker /app/raag ./
COPY --from=builder /app/entrypoint.sh ./

RUN chmod +x entrypoint.sh && \
    chown -R raag:raag /home/raag

USER raag

ENV PORT=${PORT}
ENV LIBP2P_PORT=${LIBP2P_PORT}
ENV AUTH_SECRET=${AUTH_SECRET}
ENV TLS_ENABLED=${TLS_ENABLED}
ENV TLS_CERT_FILE=${TLS_CERT_FILE}
ENV TLS_KEY_FILE=${TLS_KEY_FILE}

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${PORT}/health || exit 1

EXPOSE ${PORT} ${LIBP2P_PORT}

ENTRYPOINT ["./entrypoint.sh"]
