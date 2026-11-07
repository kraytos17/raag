.PHONY: help build build-raag build-raagd dev run-daemon run-cli test test-short test-coverage test-coverage-check test-bench lint lint-format lint-go lint-golangci lint-fix lint-fix-format lint-fix-golangci lint-ci sec generate daemon-start daemon-start-detach daemon-stop daemon-status docker-build docker-buildx docker-run docker-stop dev-setup deps install clean clean-all ci release docs-verify smoke-test

BIN := bin
RAAG := $(BIN)/raag
RAAGD := $(BIN)/raagd
GOTOOLBIN := $(shell go env GOBIN)
ifeq ($(strip $(GOTOOLBIN)),)
GOTOOLBIN := $(shell go env GOPATH)/bin
endif

GOFUMPT_VERSION := v0.12.0
GOIMPORTS_VERSION := v0.52.1-0.20261106145633-d407941a339a
GOLANGCI_LINT_VERSION := v2.15.0
GOSEC_VERSION := v2.30.0
GOVULNCHECK_VERSION := v1.9.0
AIR_VERSION := v1.67.3
BUF_VERSION := v1.75.0
PROTOC_GEN_GO_VERSION := v1.36.12
GORELEASER_VERSION := v2.20.0
TRIVY_VERSION := v0.76.2

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
VERSION_PKG := github.com/p-society/raag/internal/version
LDFLAGS := -s -w -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).BuildTime=$(BUILD_TIME)

help:
	@echo "Available targets:"
	@echo "  build              - Build all binaries"
	@echo "  build-raag         - Build CLI client"
	@echo "  build-raagd        - Build daemon"
	@echo "  dev                - Run daemon with hot-reload (air)"
	@echo "  run-daemon         - Run daemon directly"
	@echo "  run-cli            - Run CLI directly"
	@echo "  test               - Run tests with race detector"
	@echo "  test-short         - Run tests (no race detector)"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  test-coverage-check - Enforce per-package coverage floors"
	@echo "  test-bench         - Run benchmarks"
	@echo "  docs-verify        - Verify README CLI docs match the real binary"
	@echo "  smoke-test         - Run the two-daemon LAN P2P smoke test"
	@echo "  lint               - Check formatting and lint code"
	@echo "  lint-fix           - Auto-fix formatting and linting issues"
	@echo "  lint-ci            - Lint for CI (GitHub Actions annotations)"
	@echo "  sec                - Run security scanners"
	@echo "  generate           - Generate code (buf: proto)"
	@echo "  daemon-start       - Start daemon"
	@echo "  daemon-start-detach - Start daemon in background"
	@echo "  daemon-stop        - Stop daemon"
	@echo "  daemon-status      - Check daemon status"
	@echo "  docker-build       - Build Docker image"
	@echo "  docker-buildx      - Optimized buildx image (PUSH=1 for multi-arch push)"
	@echo "  docker-run         - Run Docker container"
	@echo "  docker-stop        - Stop Docker container"
	@echo "  dev-setup          - Setup dev environment (tools + deps + generate)"
	@echo "  deps               - Tidy and verify dependencies"
	@echo "  install            - Install dev tools (pin to known-good versions)"
	@echo "  clean              - Remove build artifacts"
	@echo "  clean-all          - Remove build artifacts and user data (requires CONFIRM_CLEAN_ALL=yes)"
	@echo "  ci                 - Full CI pipeline"
	@echo "  release            - Build release binaries"

build: $(BIN)
	go build -ldflags="$(LDFLAGS)" -o $(RAAG) ./cmd/raag
	go build -ldflags="$(LDFLAGS)" -o $(RAAGD) ./cmd/raagd

$(BIN):
	mkdir -p $(BIN)

build-raag: $(BIN)
	go build -ldflags="$(LDFLAGS)" -o $(RAAG) ./cmd/raag

build-raagd: $(BIN)
	go build -ldflags="$(LDFLAGS)" -o $(RAAGD) ./cmd/raagd

dev:
	$(GOTOOLBIN)/air

run-daemon:
	go run ./cmd/raagd --music-path "$${RAAG_MUSIC:-$$HOME/Music}"

run-cli:
	go run ./cmd/raag tui

test:
	go test -v -race -cover ./...

test-short:
	go test ./...

docs-verify:
	go test ./internal/docsverify/
	go test ./internal/civerify/

smoke-test: build
	./scripts/smoke-test.sh

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

test-coverage-check:
	bash scripts/coverage-check.sh

test-bench:
	go test -bench=. -benchmem ./...

lint: lint-format lint-go lint-golangci

lint-format:
	@if [ ! -x "$(GOTOOLBIN)/gofumpt" ]; then \
		echo "gofumpt not found. Run 'make install' first."; \
		exit 1; \
	fi
	@output=$$($(GOTOOLBIN)/gofumpt -l .); \
	if [ -n "$$output" ]; then \
		echo "Files not formatted with gofumpt:"; \
		echo "$$output"; \
		exit 1; \
	fi

lint-go:
	go vet ./...

lint-golangci:
	@if [ ! -x "$(GOTOOLBIN)/golangci-lint" ]; then \
		echo "golangci-lint not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOTOOLBIN)/golangci-lint run ./...

lint-fix: lint-fix-format lint-fix-golangci

lint-fix-format:
	@if [ ! -x "$(GOTOOLBIN)/gofumpt" ]; then \
		echo "gofumpt not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOTOOLBIN)/gofumpt -w .

lint-fix-golangci:
	@if [ ! -x "$(GOTOOLBIN)/golangci-lint" ]; then \
		echo "golangci-lint not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOTOOLBIN)/golangci-lint run --fix ./...

lint-ci: lint-golangci

sec:
	@if [ ! -x "$(GOTOOLBIN)/gosec" ]; then \
		echo "gosec not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOTOOLBIN)/gosec ./...
	@if [ ! -x "$(GOTOOLBIN)/govulncheck" ]; then \
		echo "govulncheck not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOTOOLBIN)/govulncheck ./...
	@if [ ! -x "$(GOTOOLBIN)/trivy" ]; then \
		echo "Trivy not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOTOOLBIN)/trivy fs --security-checks vuln,config ./

generate:
	$(GOTOOLBIN)/buf lint
	$(GOTOOLBIN)/buf generate

daemon-start:
	./bin/raagd

daemon-start-detach:
	./bin/raag daemon start

daemon-stop:
	./bin/raag daemon stop

daemon-status:
	./bin/raag daemon status

docker-build:
	docker build -t raag:latest .

# Optimized buildx build: fresh bases, version stamping, multi-arch ready.
# Default loads a native-arch image locally; PUSH=1 pushes multi-arch.
docker-buildx:
	VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); \
	BUILD_TIME=$$(date -u '+%Y-%m-%d_%H:%M:%S'); \
	if [ "$${PUSH:-0}" = "1" ]; then \
		OUTPUT="--push --platform linux/amd64,linux/arm64"; \
	else \
		OUTPUT="--load"; \
	fi; \
	docker buildx build --pull $$OUTPUT \
		--build-arg VERSION=$$VERSION --build-arg BUILD_TIME=$$BUILD_TIME \
		-t raag:latest .

docker-run:
	docker run -d --name raag \
		-p 7844:7844/tcp -p 7844:7844/udp \
		-p 7845:7845/udp -p 7846:7846/udp \
		-e RAAG_MUSIC=/music \
		-v "$${RAAG_MUSIC_DIR:-$$HOME/Music}:/music" \
		raag:latest

docker-stop:
	docker stop raag || true

dev-setup: install deps generate

deps:
	go mod tidy
	go mod verify

install:
	go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install github.com/air-verse/air@$(AIR_VERSION)
	go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)
	@echo "Installing Trivy..."
	@curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b $(GOTOOLBIN) $(TRIVY_VERSION)
	@echo "Done"

clean:
	rm -rf bin/ coverage.out coverage.html raagd.log raagd.pid

clean-all: clean
	@if [ "$(CONFIRM_CLEAN_ALL)" != "yes" ]; then \
		echo "Refusing to remove Raag user data. Re-run with CONFIRM_CLEAN_ALL=yes to confirm."; \
		exit 1; \
	fi
	@echo "Removing user data (config and database)..."
	rm -rf ~/.config/raag ~/.local/share/raag
	@echo "All data cleaned"

ci: lint-format lint-go lint-golangci
	go test -race ./...
	go test ./internal/docsverify/
	mkdir -p bin && go build -ldflags="$(LDFLAGS)" -o bin/raag ./cmd/raag && go build -ldflags="$(LDFLAGS)" -o bin/raagd ./cmd/raagd

release:
	$(GOTOOLBIN)/goreleaser release --clean --skip=publish
