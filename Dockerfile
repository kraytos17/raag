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

CMD sh -c './tracker --http-port $PORT --libp2p-port $LIBP2P_PORT --relay'
