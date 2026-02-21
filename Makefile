.PHONY: all build build-all start stop restart prune bootstrap run test test-docker test-api coverage test-coverage fmt vet lint lint-install lint-go lint-js lint-yaml lint-docs lint-api deps deps-go deps-macos deps-linux deps-wsl deps-linux-apt deps-linux-dnf deps-linux-pacman clean distclean install config dev-setup help release release-major release-minor release-patch k8s-apply k8s-delete linkerd-setup linkerd-check linkerd-dashboard linkerd-tap proto-gen agents scale-coders scale-reviewers scale-qa scale-agents logs-agents stop-agents docs docs-serve docs-deploy helm-install helm-upgrade helm-uninstall helm-deps helm-lint helm-template helm-package helm-secrets

# Build variables
BINARY_NAME=loom
BIN_DIR=bin
OBJ_DIR=obj
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"
GO_REQUIRED := $(shell awk '/^go /{print $$2}' go.mod)
GO_TOOLCHAIN_VERSION ?= $(GO_REQUIRED).0

all: build

# Build all Go binaries and documentation
build:
	@mkdir -p $(BIN_DIR)
	@echo "Building loom server..."
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/loom
	@echo "Building loomctl CLI..."
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(BIN_DIR)/loomctl ./cmd/loomctl
	@echo "Building loom-project-agent..."
	go build $(LDFLAGS) -o $(BIN_DIR)/loom-project-agent ./cmd/loom-project-agent
	@echo "Building connectors-service..."
	go build $(LDFLAGS) -o $(BIN_DIR)/connectors-service ./cmd/connectors-service
	@echo "Build complete: bin/loom, bin/loomctl, bin/loom-project-agent, bin/connectors-service"
	@echo ""
	@echo "=== Building Documentation ==="
	@if command -v mkdocs >/dev/null 2>&1; then \
		mkdocs build --quiet && echo "Docs built → site/"; \
	elif pip install --quiet mkdocs-material 2>/dev/null; then \
		mkdocs build --quiet && echo "Docs built → site/"; \
	else \
		echo "  (docs skipped — pip install mkdocs-material to enable)"; \
	fi

# Build for multiple platforms
build-all: lint-yaml
	@mkdir -p $(BIN_DIR)
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/loom
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/loom
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/loom
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/loom
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/loom-project-agent-linux-amd64 ./cmd/loom-project-agent
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BIN_DIR)/loom-project-agent-linux-arm64 ./cmd/loom-project-agent
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/connectors-service-linux-amd64 ./cmd/connectors-service
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BIN_DIR)/connectors-service-linux-arm64 ./cmd/connectors-service

# Build + launch full stack (Docker Compose) + wait for health + bootstrap
run: build
	@echo "=== Starting full Loom stack ==="
	docker compose up -d --build
	@$(MAKE) -s bootstrap
	@echo ""
	@echo "Loom is running:"
	@echo "  UI:    http://localhost:8080"
	@echo "  API:   http://localhost:8080/api/v1/system/state"
	@echo "  Logs:  make logs"
	@echo "  Stop:  make stop"

# Start loom (build binaries + container + start full stack in background)
start: build
	docker compose up -d --build
	@$(MAKE) -s bootstrap

# Stop loom (completely shut down all containers)
stop:
	docker compose down --remove-orphans

# Rebuild and restart loom (build binaries first)
restart: build
	docker compose down
	docker compose up -d --build
	@$(MAKE) -s bootstrap

# Run bootstrap.local if present (configures TokenHub + registers provider)
bootstrap:
	@if [ -f bootstrap.local ]; then \
		echo "Waiting for loom to be healthy..."; \
		for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do \
			status=$$(curl -s --connect-timeout 2 --max-time 5 http://localhost:8080/health 2>/dev/null | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4); \
			if [ "$$status" = "healthy" ]; then \
				echo "Loom is healthy, running bootstrap.local..."; \
				chmod +x bootstrap.local && ./bootstrap.local; \
				exit 0; \
			fi; \
			echo "  Attempt $$i/20: waiting (status=$$status)..."; \
			sleep 5; \
		done; \
		echo "WARNING: Loom did not become healthy in 100s, skipping bootstrap.local"; \
		echo "         Run 'make bootstrap' manually once loom is ready."; \
	else \
		echo "No bootstrap.local found (copy bootstrap.local.example to create one)"; \
	fi

# Prune stale Docker images (preserves volumes/databases)
prune:
	@echo "Removing stopped containers..."
	docker container prune -f
	@echo "Removing dangling images..."
	docker image prune -f
	@echo "Removing unused build cache..."
	docker builder prune -f
	@echo "Prune complete. Volumes (databases) preserved."

# View loom container logs (follow)
logs:
	docker compose logs -f loom

# Run tests: build binaries first, then run full test suite with PostgreSQL env
# Set DB vars explicitly so tests can find the local postgres (docker compose must be up)
test: build
	@echo "=== Running tests ==="
	@echo "  Ensure 'docker compose up -d loom-postgresql' is running before this target"
	POSTGRES_HOST=localhost \
	POSTGRES_PORT=5432 \
	POSTGRES_USER=loom \
	POSTGRES_PASSWORD=loom \
	POSTGRES_DB=loom \
	go test ./... -count=1 -timeout 600s

# Run tests in Docker (with Temporal)
test-docker:
	docker compose up -d --build temporal-postgresql temporal temporal-ui
	docker compose run --rm loom-test
	docker compose down

# Run post-flight API tests
test-api:
	@echo "Running post-flight API tests..."
	@./test/postflight/api_test.sh $(BASE_URL)

# Run tests with coverage (simple)
coverage:
	@mkdir -p $(OBJ_DIR)
	go test -v -coverprofile=$(OBJ_DIR)/coverage.out ./...
	go tool cover -html=$(OBJ_DIR)/coverage.out -o $(OBJ_DIR)/coverage.html

# Run tests with coverage analysis and threshold checking
test-coverage:
	@./scripts/test-coverage.sh

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Install linting tools
lint-install:
	@echo "Installing linting tools..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.55.2; \
	else \
		echo "✓ golangci-lint already installed"; \
	fi
	@if ! command -v eslint >/dev/null 2>&1; then \
		echo "Installing eslint..."; \
		npm install -g eslint || echo "⚠ npm not found, eslint install skipped"; \
	else \
		echo "✓ eslint already installed"; \
	fi
	@echo "Linting tools ready"

# Run all linters (comprehensive quality checks)
lint: lint-go lint-js lint-yaml lint-docs lint-api

# Go linting with golangci-lint (comprehensive)
lint-go:
	@echo "=== Go Linting ==="
	@PATH="$$PATH:$$(go env GOPATH)/bin"; \
	if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "⚠ golangci-lint not found, running basic checks only"; \
		echo "  Run 'make lint-install' to install comprehensive linters"; \
		go fmt ./...; \
		go vet ./...; \
	else \
		golangci-lint run --timeout 5m; \
	fi

# JavaScript linting with eslint
lint-js:
	@echo ""
	@echo "=== JavaScript Linting ==="
	@if ! command -v eslint >/dev/null 2>&1; then \
		echo "⚠ eslint not found, skipping JS linting"; \
		echo "  Run 'make lint-install' to install eslint"; \
	else \
		eslint web/static/js/*.js || true; \
	fi

# YAML linting
lint-yaml:
	@echo ""
	@echo "=== YAML Linting ==="
	@go run ./cmd/yaml-lint

# Documentation structure check
lint-docs:
	@echo ""
	@echo "=== Documentation Linting ==="
	@bash scripts/check-docs-structure.sh

# API/Frontend validation (catch API mismatches)
lint-api:
	@echo ""
	@echo "=== API/Frontend Validation ==="
	@bash scripts/check-api-frontend.sh

# Install dependencies
deps:
	@set -e; \
	os=$$(uname -s); \
	if [ "$$os" = "Darwin" ]; then \
		$(MAKE) deps-macos; \
	elif [ "$$os" = "Linux" ]; then \
		if grep -qi microsoft /proc/version 2>/dev/null; then \
			$(MAKE) deps-wsl; \
		else \
			$(MAKE) deps-linux; \
		fi; \
	else \
		echo "Unsupported OS: $$os"; \
		exit 1; \
	fi; \
	$(MAKE) deps-go

deps-go:
	@set -e; \
	required="$(GO_REQUIRED)"; \
	toolchain="$(GO_TOOLCHAIN_VERSION)"; \
	os=$$(uname -s); \
	arch=$$(uname -m); \
	case "$$arch" in \
		x86_64) arch=amd64 ;; \
		aarch64|arm64) arch=arm64 ;; \
		*) echo "Unsupported architecture: $$arch"; exit 1 ;; \
	esac; \
	current=""; \
	if command -v go >/dev/null 2>&1; then \
		current=$$(go env GOVERSION | sed 's/^go//' | awk -F. '{print $$1"."$$2}'); \
	fi; \
	if [ "$$current" != "$$required" ]; then \
		if [ "$$os" = "Linux" ]; then \
			url="https://go.dev/dl/go$${toolchain}.linux-$${arch}.tar.gz"; \
			echo "Installing Go $$toolchain from $$url"; \
			sudo rm -rf /usr/local/go; \
			curl -fsSL "$$url" | sudo tar -C /usr/local -xz; \
			sudo ln -sf /usr/local/go/bin/go /usr/local/bin/go; \
			sudo ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt; \
			export PATH=/usr/local/go/bin:$$PATH; \
		elif [ "$$os" = "Darwin" ]; then \
			echo "Ensuring Go $$required is installed via Homebrew"; \
			brew install go || brew upgrade go; \
		fi; \
	fi; \
	go mod download; \
	go mod tidy

deps-macos:
	@command -v brew >/dev/null || { echo "Homebrew is required: https://brew.sh/"; exit 1; }
	brew update
	brew install go git pkg-config icu4c
	@if ! pkg-config --exists sqlite3 2>/dev/null; then \
		brew install sqlite; \
		echo "sqlite is keg-only; set PKG_CONFIG_PATH/LDFLAGS/CPPFLAGS if builds fail."; \
	fi
	@if ! command -v docker >/dev/null; then \
		brew install --cask docker; \
		echo "Docker Desktop installed. Start it to enable docker compose."; \
	fi

deps-linux:
	@if command -v apt-get >/dev/null; then \
		$(MAKE) deps-linux-apt; \
	elif command -v dnf >/dev/null; then \
		$(MAKE) deps-linux-dnf; \
	elif command -v pacman >/dev/null; then \
		$(MAKE) deps-linux-pacman; \
	else \
		echo "Unsupported Linux distro (no apt-get, dnf, or pacman)"; \
		exit 1; \
	fi

deps-wsl:
	@$(MAKE) deps-linux
	@if ! command -v docker >/dev/null; then \
		echo "Docker Desktop with WSL2 integration is required for docker compose."; \
		exit 1; \
	fi

deps-linux-apt:
	sudo apt-get update
	sudo apt-get install -y build-essential git curl ca-certificates pkg-config libicu-dev libsqlite3-dev golang-go
	@if ! command -v docker >/dev/null; then \
		echo "Docker not found; attempting to install docker.io and docker-compose-plugin..."; \
		sudo apt-get install -y docker.io docker-compose-plugin || { \
			echo "Docker install failed (possible containerd conflict)."; \
			echo "If Docker is already installed, ensure it is on PATH and re-run make deps."; \
			exit 1; \
		}; \
	fi
	@if ! docker compose version >/dev/null 2>&1; then \
		echo "docker compose plugin missing; attempting to install docker-compose-plugin..."; \
		sudo apt-get install -y docker-compose-plugin || { \
			echo "Failed to install docker-compose-plugin. Install via Docker's repo and re-run make deps."; \
			exit 1; \
		}; \
	fi

deps-linux-dnf:
	sudo dnf install -y gcc gcc-c++ make git curl ca-certificates pkgconf-pkg-config libicu-devel sqlite-devel golang docker docker-compose-plugin
	@if ! command -v docker >/dev/null; then \
		echo "Docker install failed or not in PATH."; \
		exit 1; \
	fi

deps-linux-pacman:
	sudo pacman -Sy --noconfirm base-devel git curl ca-certificates pkgconf icu sqlite go docker docker-compose
	@if ! command -v docker >/dev/null; then \
		echo "Docker install failed or not in PATH."; \
		exit 1; \
	fi

# Clean build artifacts (preserves databases)
clean:
	rm -rf $(BIN_DIR)
	rm -rf $(OBJ_DIR)

# Deep clean: stop containers, remove volumes (DELETES DATABASES), remove images, clean all caches
distclean: clean
	@docker compose down -v --remove-orphans 2>/dev/null || true
	@docker rmi loom:latest loom-loom-test:latest 2>/dev/null || true
	@docker image prune -f
	@go clean -cache -testcache

# Install binary to $GOPATH/bin
install: build
	@echo "Installing loom and loomctl to ~/.local/bin..."
	@mkdir -p ~/.local/bin
	@cp $(BIN_DIR)/$(BINARY_NAME) ~/.local/bin/
	@cp $(BIN_DIR)/loomctl ~/.local/bin/
	@echo "✓ Installation complete!"
	@echo ""
	@if echo $$PATH | grep -q "$$HOME/.local/bin"; then \
		echo "✓ ~/.local/bin is already in your PATH"; \
		echo "  Run 'loom' or 'loomctl' from anywhere."; \
	else \
		echo "⚠ Add ~/.local/bin to your PATH:"; \
		echo "  export PATH=\"\$$HOME/.local/bin:\$$PATH\""; \
		echo "  (Add this to your ~/.bashrc or ~/.zshrc to make it permanent)"; \
	fi

# Create config.yaml from example if missing
config:
	@if [ ! -f config.yaml ]; then \
		cp config.yaml.example config.yaml; \
		echo "Created config.yaml from example"; \
	else \
		echo "config.yaml already exists"; \
	fi

# Development setup
dev-setup: deps config
	@echo "Development environment setup complete"
	@echo "Run 'make start' to start loom"

# ── Release targets ──────────────────────────────────────────────────────
# Full release pipeline: lint → test → build all platforms → Docker images →
# documentation → git tag + GitHub release.
#
# Usage:
#   make release              # patch bump (x.y.Z+1)
#   make release-minor        # minor bump (x.Y+1.0)
#   make release-major        # major bump (X+1.0.0)
#   VERSION=1.2.3 make release-patch  # explicit version
#
# Pre-requisites: ./scripts/release.sh must exist and handle git tagging.

release: _release-preflight
	@BATCH=$(BATCH) ./scripts/release.sh patch

release-patch: _release-preflight
	@BATCH=$(BATCH) ./scripts/release.sh patch

release-minor: _release-preflight
	@BATCH=$(BATCH) ./scripts/release.sh minor

release-major: _release-preflight
	@BATCH=$(BATCH) ./scripts/release.sh major

# Internal: shared preflight checks before any release
_release-preflight: lint-go lint-yaml build-all docs
	@echo ""
	@echo "=== Release preflight complete ==="
	@echo "  All binaries compiled for all platforms"
	@echo "  Linters passed"
	@echo "  Documentation built"
	@echo ""

# ── Agent Swarm targets ───────────────────────────────────────────────────

# Start agent swarm (all three agent roles)
agents: start
	@echo "Scaling agent swarm..."
	docker compose up -d loom-agent-coder loom-agent-reviewer loom-agent-qa
	@echo "Agent swarm running."

# Scale coder agents (usage: make scale-coders N=3)
scale-coders:
	docker compose up -d --scale loom-agent-coder=$(N) --no-recreate

# Scale reviewer agents (usage: make scale-reviewers N=2)
scale-reviewers:
	docker compose up -d --scale loom-agent-reviewer=$(N) --no-recreate

# Scale QA agents (usage: make scale-qa N=2)
scale-qa:
	docker compose up -d --scale loom-agent-qa=$(N) --no-recreate

# Scale all agent types independently (usage: make scale-agents CODERS=3 REVIEWERS=2 QA=2)
scale-agents:
	docker compose up -d \
		--scale loom-agent-coder=$(CODERS) \
		--scale loom-agent-reviewer=$(REVIEWERS) \
		--scale loom-agent-qa=$(QA) \
		--no-recreate

# View agent container logs
logs-agents:
	docker compose logs -f loom-agent-coder loom-agent-reviewer loom-agent-qa

# Stop only agent containers (keep control plane running)
stop-agents:
	docker compose stop loom-agent-coder loom-agent-reviewer loom-agent-qa

# ── Kubernetes / Linkerd targets ──────────────────────────────────────────

# Apply base + local overlay (requires kubectl context set to loom-dev cluster)
k8s-apply:
	kubectl apply -k deploy/k8s/overlays/local

# Tear down all loom resources from the cluster
k8s-delete:
	kubectl delete -k deploy/k8s/overlays/local --ignore-not-found

# Full Linkerd setup: create k3d cluster, install Linkerd, deploy loom
linkerd-setup:
	@./scripts/setup-linkerd.sh

# Check Linkerd health in the loom namespace
linkerd-check:
	linkerd -n loom check --proxy

# Open Linkerd viz dashboard in browser
linkerd-dashboard:
	linkerd viz dashboard

# Live traffic tap for loom deployment
linkerd-tap:
	linkerd -n loom tap deploy/loom

# Regenerate protobuf Go bindings (requires protoc + plugins in PATH)
proto-gen:
	@PROTOC=$$(which protoc || echo /tmp/protoc-arm/bin/protoc); \
	export PATH=$$(go env GOPATH)/bin:$$PATH; \
	$$PROTOC \
	  --proto_path=api/proto/connectors \
	  --go_out=api/proto/connectors \
	  --go_opt=paths=source_relative \
	  --go-grpc_out=api/proto/connectors \
	  --go-grpc_opt=paths=source_relative \
	  api/proto/connectors/connectors.proto
	@echo "Proto generation complete."

# ── Helm targets ─────────────────────────────────────────────────────────
HELM_RELEASE  ?= loom
HELM_NAMESPACE ?= loom
HELM_CHART     = deploy/helm/loom
HELM_VALUES   ?= $(HELM_CHART)/values.yaml

# Update Helm chart dependencies (postgresql, nats, temporal)
helm-deps:
	helm dependency update $(HELM_CHART)

# Install Loom into k8s cluster for the first time
helm-install: helm-deps
	helm install $(HELM_RELEASE) $(HELM_CHART) \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--values $(HELM_VALUES)

# Upgrade (or install) an existing Helm release
helm-upgrade: helm-deps
	helm upgrade $(HELM_RELEASE) $(HELM_CHART) \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--install \
		--values $(HELM_VALUES)

# Uninstall Loom from Kubernetes (preserves PVCs by default)
helm-uninstall:
	helm uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

# Lint the Helm chart templates
helm-lint:
	helm lint $(HELM_CHART) --values $(HELM_VALUES)

# Render templates without deploying (dry-run review)
helm-template:
	helm template $(HELM_RELEASE) $(HELM_CHART) \
		--namespace $(HELM_NAMESPACE) \
		--values $(HELM_VALUES)

# Package the chart into a .tgz for distribution
helm-package: helm-deps
	@mkdir -p $(BIN_DIR)
	helm package $(HELM_CHART) --destination $(BIN_DIR)

# Generate/rotate secrets for the Helm release
# Usage: make helm-secrets HELM_NAMESPACE=loom
helm-secrets:
	@echo "Creating Loom secrets in namespace $(HELM_NAMESPACE)..."
	@kubectl create namespace $(HELM_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	@kubectl create secret generic loom-postgresql-secret \
		--namespace $(HELM_NAMESPACE) \
		--from-literal=username=loom \
		--from-literal=password=$$(openssl rand -base64 24) \
		--dry-run=client -o yaml | kubectl apply -f -
	@kubectl create secret generic loom-secret \
		--namespace $(HELM_NAMESPACE) \
		--from-literal=password=$$(openssl rand -base64 24) \
		--dry-run=client -o yaml | kubectl apply -f -
	@echo "Secrets created. Run 'make helm-install' to deploy."

# Documentation (mkdocs)
docs:
	@echo ""
	@echo "=== Building Documentation ==="
	@if command -v mkdocs >/dev/null 2>&1; then \
		mkdocs build; \
	else \
		echo "⚠ mkdocs not installed, skipping docs build"; \
		echo "  Install with: pip install mkdocs-material"; \
	fi

docs-serve:
	@echo ""
	@echo "=== Serving Documentation ==="
	@pip install --quiet mkdocs-material 2>/dev/null || pip install --quiet --user mkdocs-material
	@mkdocs serve

docs-deploy:
	@echo ""
	@echo "=== Deploying to GitHub Pages ==="
	@pip install --quiet mkdocs-material 2>/dev/null || pip install --quiet --user mkdocs-material
	@mkdocs gh-deploy --force

help:
	@echo "Loom - Makefile Commands"
	@echo ""
	@echo "Quickstart:"
	@echo "  make run          - Build everything + start full stack (Docker) + bootstrap"
	@echo "  make test         - Build binaries + run full test suite (needs docker compose up)"
	@echo "  make build        - Build all binaries + documentation"
	@echo ""
	@echo "Service (Docker Compose):"
	@echo "  make run          - Build and launch full stack: containers, bootstrap"
	@echo "  make start        - Alias for run"
	@echo "  make stop         - Stop all containers"
	@echo "  make restart      - Rebuild and restart + bootstrap"
	@echo "  make prune        - Remove stale Docker images (preserves volumes/databases)"
	@echo "  make bootstrap    - Run bootstrap.local if present (registers providers)"
	@echo "  make logs         - Follow loom container logs"
	@echo ""
	@echo "Development:"
	@echo "  make build        - Build all 4 binaries + documentation (mkdocs)"
	@echo "  make build-all    - Cross-compile for linux/darwin/windows"
	@echo "  make test         - Build binaries + run test suite with PostgreSQL env"
	@echo "  make test-docker  - Run tests in Docker (with Temporal)"
	@echo "  make test-api     - Run post-flight API tests against running stack"
	@echo "  make coverage     - Run tests with HTML coverage report"
	@echo "  make lint         - Run all linters (Go, JS, YAML, docs, API)"
	@echo "  make lint-install - Install linting tools (golangci-lint, eslint)"
	@echo "  make lint-go      - Go linters only"
	@echo "  make lint-js      - JavaScript linters only"
	@echo "  make lint-api     - API/frontend validation"
	@echo "  make fmt          - Format Go code"
	@echo "  make vet          - Run go vet"
	@echo "  make deps         - Install system + Go module dependencies"
	@echo "  make clean        - Remove build artifacts (preserves databases)"
	@echo "  make distclean    - Deep clean: stops containers, DELETES DATABASES"
	@echo "  make install      - Install loom + loomctl to ~/.local/bin"
	@echo "  make config       - Create config.yaml from example"
	@echo "  make dev-setup    - Full dev environment setup"
	@echo ""
	@echo "Documentation:"
	@echo "  make docs         - Build documentation site (mkdocs → site/)"
	@echo "  make docs-serve   - Serve docs locally at http://localhost:8000"
	@echo "  make docs-deploy  - Deploy to GitHub Pages"
	@echo ""
	@echo "Helm / Kubernetes:"
	@echo "  make helm-deps                  - Update chart dependencies"
	@echo "  make helm-install               - Install Loom into k8s cluster"
	@echo "  make helm-upgrade               - Upgrade (or install) existing release"
	@echo "  make helm-uninstall             - Remove Loom from k8s"
	@echo "  make helm-lint                  - Lint the Helm chart"
	@echo "  make helm-template              - Render templates (dry run)"
	@echo "  make helm-package               - Package chart to bin/*.tgz"
	@echo "  make helm-secrets               - Create k8s secrets for Loom"
	@echo "  Options: HELM_RELEASE=loom HELM_NAMESPACE=loom HELM_VALUES=path/to/vals.yaml"
	@echo ""
	@echo "Agent Swarm:"
	@echo "  make agents               - Start all agent service containers"
	@echo "  make scale-coders N=3     - Scale coder agents to N replicas"
	@echo "  make scale-reviewers N=2  - Scale reviewer agents"
	@echo "  make scale-qa N=2         - Scale QA agents"
	@echo "  make scale-agents CODERS=3 REVIEWERS=2 QA=2"
	@echo "  make logs-agents          - Follow agent container logs"
	@echo "  make stop-agents          - Stop agent containers only"
	@echo ""
	@echo "Kustomize / Linkerd:"
	@echo "  make linkerd-setup        - Create k3d cluster, install Linkerd, deploy"
	@echo "  make k8s-apply            - Apply K8s manifests (overlays/local)"
	@echo "  make k8s-delete           - Delete K8s resources"
	@echo "  make linkerd-check        - Check Linkerd proxy health"
	@echo "  make linkerd-dashboard    - Open Linkerd viz dashboard"
	@echo "  make linkerd-tap          - Live traffic tap"
	@echo ""
	@echo "Code Generation:"
	@echo "  make proto-gen            - Regenerate protobuf Go bindings"
	@echo ""
	@echo "Release:"
	@echo "  make release              - Patch release: lint+build-all+docs → x.y.Z+1"
	@echo "  make release-minor        - Minor release → x.Y+1.0"
	@echo "  make release-major        - Major release → X+1.0.0"
