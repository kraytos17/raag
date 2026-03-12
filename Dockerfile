FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o tracker ./tracker/cmd/tracker

FROM alpine
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/tracker .
CMD ["./tracker --http-port 8080 --libp2p-port 45678 --relay --dht"]
