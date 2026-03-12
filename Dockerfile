FROM golang:alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -ldflags="-s -w" -o tracker ./tracker/cmd/tracker

FROM alpine
RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/tracker .

ENV TRACKER_HTTP_PORT=8080
ENV TRACKER_LIBP2P_PORT=45678
ENV TRACKER_RELAY=true
ENV TRACKER_DHT=true

CMD ["sh", "-c", "./tracker --http-port ${TRACKER_HTTP_PORT:-8080} --libp2p-port ${TRACKER_LIBP2P_PORT:-45678} --relay ${TRACKER_RELAY:-true} --dht ${TRACKER_DHT:-true}"]