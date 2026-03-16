FROM golang:alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -ldflags="-s -w" -o tracker ./tracker/cmd/tracker

FROM ubuntu:24.04

# Install ca-certificates for SSL/TLS
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/tracker .
COPY entrypoint.sh .

RUN chmod +x entrypoint.sh

EXPOSE 8080 45678

ENV PORT=8080
ENV LIBP2P_PORT=45678
ENV AUTH_SECRET=${AUTH_SECRET:-}
ENV TLS_ENABLED=
ENV TLS_CERT_FILE=
ENV TLS_KEY_FILE=

ENTRYPOINT ["/app/entrypoint.sh"]
