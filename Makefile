.PHONY: build run test clean install deps lint vet fmt

BINARY_NAME=raag
GO_CMD=go
GO_BUILD_FLAGS=-ldflags="-s -w"

build:
	$(GO_CMD) build $(GO_BUILD_FLAGS) -o bin/$(BINARY_NAME) cmd/raag/main.go

run: build
	./bin/$(BINARY_NAME)

test:
	$(GO_CMD) test -v ./...

clean:
	rm -rf bin/

install:
	$(GO_CMD) install $(GO_BUILD_FLAGS) ./cmd/raag

deps:
	$(GO_CMD) mod download
	$(GO_CMD) mod tidy

lint:
	$(GO_CMD) run mvdan.cc/gofumpt@latest -l -w .

vet:
	$(GO_CMD) vet ./...

fmt:
	$(GO_CMD) fmt ./...

tidy:
	$(GO_CMD) mod tidy

all: clean deps build
