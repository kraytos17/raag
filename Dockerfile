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

EXPOSE 8080 45678

CMD ["./tracker", "--http-port", "8080", "--libp2p-port", "45678", "--relay", "--dht"]