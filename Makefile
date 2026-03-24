.PHONY: build build-raag build-raagd dev run-daemon run-cli test test-short test-coverage test-bench lint lint-fix lint-ci sec generate daemon-start daemon-start-detach daemon-stop daemon-status docker-build docker-run docker-stop dev-setup deps install clean clean-all ci release help

BIN := bin
RAAG := $(BIN)/raag
RAAGD := $(BIN)/raagd
GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint

help:
	@echo "Available targets:"
	@echo "  build              - Build all binaries"
	@echo "  build-raag        - Build CLI client"
	@echo "  build-raagd       - Build daemon"
	@echo "  dev               - Run daemon with hot-reload"
	@echo "  run-daemon        - Run daemon directly"
	@echo "  run-cli           - Run CLI directly"
	@echo "  test              - Run tests with race detector"
	@echo "  test-short        - Run tests (no race detector)"
	@echo "  test-coverage     - Run tests with coverage report"
	@echo "  test-bench        - Run benchmarks"
	@echo "  lint              - Format and lint code"
	@echo "  lint-fix          - Auto-fix linting issues"
	@echo "  lint-ci           - Lint for CI"
	@echo "  sec               - Run security scanners"
	@echo "  generate          - Generate code (proto + mocks)"
	@echo "  daemon-start      - Start daemon"
	@echo "  daemon-start-detach - Start daemon in background"
	@echo "  daemon-stop       - Stop daemon"
	@echo "  daemon-status     - Check daemon status"
	@echo "  docker-build      - Build Docker image"
	@echo "  docker-run        - Run Docker container"
	@echo "  docker-stop       - Stop Docker container"
	@echo "  dev-setup         - Setup dev environment"
	@echo "  deps              - Tidy and verify dependencies"
	@echo "  install           - Install dev tools"
	@echo "  clean             - Remove build artifacts"
	@echo "  clean-all         - Remove build artifacts and user data (config/db)"
	@echo "  ci                - Full CI pipeline"
	@echo "  release           - Build release binaries"

build: $(BIN)
	go build -ldflags="-s -w" -o $(RAAG) ./cmd/raag
	go build -ldflags="-s -w" -o $(RAAGD) ./cmd/raagd

$(BIN):
	mkdir -p $(BIN)

build-raag: $(BIN)
	go build -ldflags="-s -w" -o $(RAAG) ./cmd/raag

build-raagd: $(BIN)
	go build -ldflags="-s -w" -o $(RAAGD) ./cmd/raagd

dev:
	air -c .air.toml

run-daemon:
	go run ./cmd/raagd

run-cli:
	go run ./cmd/raag

test:
	go test -v -race -cover ./...

test-short:
	go test ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

test-bench:
	go test -bench=. -benchmem ./...

lint:
	gofumpt -w -e .
	go vet ./...
	@if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -f $(GOLANGCI_LINT) ]; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	$(GOLANGCI_LINT) run ./...

lint-fix:
	gofumpt -w -e .
	@if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -f $(GOLANGCI_LINT) ]; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	$(GOLANGCI_LINT) run --fix ./...

lint-ci:
	@if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -f $(GOLANGCI_LINT) ]; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	$(GOLANGCI_LINT) run --out-format=github-actions ./...

sec:
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "Installing gosec..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	gosec ./...
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "Installing govulncheck..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	fi
	govulncheck ./...
	@if ! command -v trivy >/dev/null 2>&1; then \
		echo "Installing Trivy..."; \
		curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b $$(go env GOPATH)/bin v0.69.3; \
	fi
	trivy fs --security-checks vuln,config ./

generate: $(BIN)
	mkdir -p proto/gen
	protoc --go_out=proto/gen --go_opt=paths=source_relative --go-grpc_out=proto/gen --go-grpc_opt=paths=source_relative -Iproto proto/raag.proto
	mockgen -source=internal/app/interfaces.go -destination=internal/app/mocks/mocks.go -package=mocks 2>/dev/null || true

daemon-start:
	./bin/raagd

daemon-start-detach:
	@nohup ./bin/raagd > raagd.log 2>&1 & echo $$! > raagd.pid && echo "Daemon started with PID $$(cat raagd.pid)"

daemon-stop:
	@if [ -f raagd.pid ]; then \
		kill $$(cat raagd.pid) 2>/dev/null && rm raagd.pid && echo "Daemon stopped"; \
	else \
		pkill raagd && echo "Daemon stopped (no PID file)" || echo "No daemon running"; \
	fi

daemon-status:
	./bin/raag status || echo "Daemon not running"

docker-build:
	docker build -t raag:1.0.0 .

docker-run:
	docker run -d --name raag -p 7844:7844 -v ~/Music:/music raag:1.0.0

docker-stop:
	docker stop raag || true

dev-setup: install deps generate

deps:
	go mod tidy
	go mod verify

install:
	go install github.com/air-verse/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/bufbuild/buf/cmd/buf@latest
	@echo "Installing Trivy..."
	@curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b $$(go env GOPATH)/bin v0.69.3
	@echo "Done"

clean:
	rm -rf bin/ coverage.out coverage.html raagd.log raagd.pid

clean-all: clean
	@echo "Removing user data (config and database)..."
	rm -rf ~/.config/raag ~/.local/share/raag
	@echo "All data cleaned"

ci:
	go vet ./...
	go test -race ./...
	@if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -f $(GOLANGCI_LINT) ]; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	$(GOLANGCI_LINT) run --out-format=github-actions ./...
	mkdir -p bin && go build -o bin/raag ./cmd/raag && go build -o bin/raagd ./cmd/raagd

release:
	goreleaser release --clean --skip-publish 2>/dev/null || echo "goreleaser not configured"
