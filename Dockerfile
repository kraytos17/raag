FROM golang:alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -ldflags="-s -w" -o tracker ./tracker/cmd/tracker

FROM alpine

RUN apk --no-cache add ca-certificates wget

WORKDIR /app
COPY --from=builder /app/tracker .

EXPOSE 8080 45678

ENV PORT=8080
ENV LIBP2P_PORT=45678
ENV AUTH_SECRET=${AUTH_SECRET:-}
ENV TLS_ENABLED=${TLS_ENABLED:-false}
ENV TLS_CERT_FILE=${TLS_CERT_FILE:-}
ENV TLS_KEY_FILE=${TLS_KEY_FILE:-}

CMD sh -c './tracker --http-port $PORT --libp2p-port $LIBP2P_PORT --relay ${TLS_ENABLED:+--tls} ${TLS_CERT_FILE:+--tls-cert $TLS_CERT_FILE} ${TLS_KEY_FILE:+--tls-key $TLS_KEY_FILE}'
