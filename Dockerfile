FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

# Install git for version info + ALSA for audio (CGO required)
RUN apk add --no-cache git libasound-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" -o tracker ./tracker/cmd/tracker && \
    CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" -o raag ./cmd/raag

FROM ubuntu:24.04 AS runtime

ARG PORT=8080
ARG LIBP2P_PORT=45678
ARG AUTH_SECRET=
ARG TLS_ENABLED=false
ARG TLS_CERT_FILE=
ARG TLS_KEY_FILE=

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 raag 2>/dev/null || groupadd -f --gid 1000 raag \
    && useradd -o -u 1000 --gid raag --shell /bin/false --create-home raag 2>/dev/null || usermod -o -u 1000 raag 2>/dev/null || true

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

EXPOSE 8080 45678

ENTRYPOINT ["./entrypoint.sh"]