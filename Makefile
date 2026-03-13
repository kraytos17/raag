.PHONY: build run test clean install deps lint vet fmt build-tracker run-tracker update fix staticcheck

BINARY_NAME=raag
TRACKER_BINARY_NAME=tracker
GO_CMD=go
GO_BUILD_FLAGS=-ldflags="-s -w"

build:
	$(GO_CMD) build $(GO_BUILD_FLAGS) -o bin/$(BINARY_NAME) ./cmd/raag

build-tracker:
	$(GO_CMD) build $(GO_BUILD_FLAGS) -o bin/$(TRACKER_BINARY_NAME) tracker/cmd/tracker/main.go

run-tracker: build-tracker
	./bin/$(TRACKER_BINARY_NAME) --port 8080

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

update:
	$(GO_CMD) get -u ./...

fix:
	$(GO_CMD) fix ./...

staticcheck:
	$(GO_CMD) run honnef.co/go/tools/cmd/staticcheck@latest ./...

all: clean deps build build-tracker
