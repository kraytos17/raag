.PHONY: help build build-raag build-raagd dev run-daemon run-cli test test-short test-coverage test-bench lint lint-fix lint-ci sec generate daemon-start daemon-start-detach daemon-stop daemon-status docker-build docker-run docker-stop dev-setup deps install clean clean-all ci release docs-verify

BIN := bin
RAAG := $(BIN)/raag
RAAGD := $(BIN)/raagd
GOLANGCI_LINT := $(shell command -v golangci-lint 2>/dev/null || echo $(shell go env GOPATH)/bin/golangci-lint)

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
	@echo "  test-bench         - Run benchmarks"
	@echo "  docs-verify        - Verify README CLI docs match the real binary"
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
	@echo "  docker-run         - Run Docker container"
	@echo "  docker-stop        - Stop Docker container"
	@echo "  dev-setup          - Setup dev environment (tools + deps + generate)"
	@echo "  deps               - Tidy and verify dependencies"
	@echo "  install            - Install dev tools (pin to known-good versions)"
	@echo "  clean              - Remove build artifacts"
	@echo "  clean-all          - Remove build artifacts and user data (config/db)"
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
	air

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

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

test-bench:
	go test -bench=. -benchmem ./...

lint: lint-format lint-go lint-golangci

lint-format:
	@if ! command -v gofumpt >/dev/null 2>&1; then \
		echo "gofumpt not found. Run 'make install' first."; \
		exit 1; \
	fi
	@output=$$(gofumpt -l .); \
	if [ -n "$$output" ]; then \
		echo "Files not formatted with gofumpt:"; \
		echo "$$output"; \
		exit 1; \
	fi

lint-go:
	go vet ./...

lint-golangci:
	@if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -f $(GOLANGCI_LINT) ]; then \
		echo "golangci-lint not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOLANGCI_LINT) run ./...

lint-fix: lint-fix-format lint-fix-golangci

lint-fix-format:
	@if ! command -v gofumpt >/dev/null 2>&1; then \
		echo "gofumpt not found. Run 'make install' first."; \
		exit 1; \
	fi
	gofumpt -w .

lint-fix-golangci:
	@if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -f $(GOLANGCI_LINT) ]; then \
		echo "golangci-lint not found. Run 'make install' first."; \
		exit 1; \
	fi
	$(GOLANGCI_LINT) run --fix ./...

lint-ci: lint-golangci

sec:
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "gosec not found. Run 'make install' first."; \
		exit 1; \
	fi
	gosec ./...
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "govulncheck not found. Run 'make install' first."; \
		exit 1; \
	fi
	govulncheck ./...
	@if ! command -v trivy >/dev/null 2>&1; then \
		echo "Trivy not found. Run 'make install' first."; \
		exit 1; \
	fi
	trivy fs --security-checks vuln,config ./

generate:
	buf lint
	buf generate

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
	go install mvdan.cc/gofumpt@v0.11.0
	go install golang.org/x/tools/cmd/goimports@v0.49.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/air-verse/air@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@if ! command -v goreleaser >/dev/null 2>&1; then \
		echo "Installing goreleaser..."; \
		go install github.com/goreleaser/goreleaser@latest; \
	fi
	@echo "Installing Trivy..."
	@curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b $$(go env GOPATH)/bin v0.74.0
	@echo "Done"

clean:
	rm -rf bin/ coverage.out coverage.html raagd.log raagd.pid

clean-all: clean
	@echo "Removing user data (config and database)..."
	rm -rf ~/.config/raag ~/.local/share/raag
	@echo "All data cleaned"

ci: lint-format lint-go lint-golangci
	go test -race ./...
	go test ./internal/docsverify/
	mkdir -p bin && go build -ldflags="$(LDFLAGS)" -o bin/raag ./cmd/raag && go build -ldflags="$(LDFLAGS)" -o bin/raagd ./cmd/raagd

release:
	goreleaser release --clean --skip=publish
