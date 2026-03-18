FROM --platform=$BUILDPLATFORM golang:1.26.1-bookworm AS builder

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
    -o raag ./cmd/raag

FROM debian:bookworm-slim AS runtime

ARG LIBP2P_PORT=45678
ARG DATA_DIR=/home/raag/data
ARG MUSIC_DIR=/home/raag/music
ARG AUTH_SECRET=

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libasound2 \
    && rm -rf /var/lib/apt/lists/* \
    && (getent group raag >/dev/null || groupadd --gid 1000 raag) \
    && (getent passwd raag >/dev/null || useradd -o --uid 1000 --gid raag --shell /bin/false --create-home raag)

WORKDIR /home/raag

RUN mkdir -p "$DATA_DIR" "$MUSIC_DIR"

COPY --from=builder /app/raag ./

RUN chown -R raag:raag /home/raag

USER raag

ENV LIBP2P_PORT=${LIBP2P_PORT}
ENV DATA_DIR=${DATA_DIR}
ENV MUSIC_DIR=${MUSIC_DIR}
ENV AUTH_SECRET=${AUTH_SECRET}

EXPOSE ${LIBP2P_PORT}

ENTRYPOINT ["./raag"]
CMD ["daemon"]
