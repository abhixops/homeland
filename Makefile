# ══════════════════════════════════════════════════════════════════════════════
#  Homeland Dashboard — Makefile
#  Targets: dev setup, prod build, docker, testing, linting, and utilities.
#
#  Usage:
#    make help          → show all targets
#    make dev-setup     → install all dev dependencies (Go + Node)
#    make dev           → start app locally with hot-reload (requires air)
#    make dev-docker    → spin up dev environment via Docker Compose (with build)
#    make build         → full prod build: CSS + Go binary
#    make prod-up       → pull image and start prod via Docker Compose
#    make docker-build  → build the production Docker image locally
# ══════════════════════════════════════════════════════════════════════════════

# ─── Variables ────────────────────────────────────────────────────────────────

# Project
APP_NAME        := homeland
MODULE          := github.com/abhixops/homeland
CMD_PATH        := ./cmd/homeland/
BIN_DIR         := ./bin
BINARY          := $(BIN_DIR)/$(APP_NAME)

# Versioning — falls back to "dev" if no git tag is found
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE      ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS         := -s -w \
                   -X main.version=$(VERSION) \
                   -X main.commit=$(COMMIT) \
                   -X main.buildDate=$(BUILD_DATE)

# Go
GO              := go
GOOS            ?= $(shell go env GOOS)
GOARCH          ?= $(shell go env GOARCH)
CGO_ENABLED     := 0

# Node / Tailwind
NPM             := npm
TAILWIND_IN     := web/static/css/app.css
TAILWIND_OUT    := web/static/css/tailwind.css

# Docker
IMAGE_REGISTRY  := ghcr.io/abhixops
IMAGE_NAME      := $(IMAGE_REGISTRY)/$(APP_NAME)
IMAGE_TAG       ?= $(VERSION)
DOCKERFILE      := Dockerfile
COMPOSE_FILE    := docker-compose.yml

# Docker GID — required for the Docker socket volume in docker-compose.yml.
# Auto-detected from the host; override with: make prod-up DOCKER_GID=1001
DOCKER_GID      ?= $(shell getent group docker 2>/dev/null | cut -d: -f3 || \
                           stat -c '%g' /var/run/docker.sock 2>/dev/null || echo 999)

# Hot-reload: 'air' (https://github.com/air-verse/air) is used for dev runs.
AIR             := $(shell command -v air 2>/dev/null)

# ─── Phony targets ────────────────────────────────────────────────────────────

.PHONY: help \
        dev-setup deps-go deps-node \
        dev dev-run dev-docker dev-docker-down \
        css-build css-watch \
        build build-go build-linux build-darwin build-windows \
        test test-cover test-race \
        lint vet fmt \
        docker-build docker-push docker-run \
        prod-up prod-down prod-logs prod-restart prod-ps \
        clean clean-bin clean-css clean-docker \
        check-tools version

# ─── Default ──────────────────────────────────────────────────────────────────

.DEFAULT_GOAL := help

# ══════════════════════════════════════════════════════════════════════════════
#  HELP
# ══════════════════════════════════════════════════════════════════════════════

help: ## Show this help message
	@printf "\n\033[1mHomeland Dashboard — $(VERSION)\033[0m\n\n"
	@printf "\033[1m%-28s %s\033[0m\n" "Target" "Description"
	@printf "%-28s %s\n" "──────────────────────────" "──────────────────────────────────────────────────"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-26s\033[0m %s\n", $$1, $$2}'
	@echo ""

# ══════════════════════════════════════════════════════════════════════════════
#  DEV SETUP
# ══════════════════════════════════════════════════════════════════════════════

dev-setup: check-tools deps-go deps-node css-build ## ★ Full dev setup: install all deps and build CSS
	@echo ""
	@echo "✅  Dev environment ready! Run 'make dev' to start."

deps-go: ## Download Go module dependencies
	@echo "→ Downloading Go modules..."
	$(GO) mod download
	$(GO) mod verify

deps-node: ## Install Node.js / npm dependencies (Tailwind CSS)
	@echo "→ Installing npm packages..."
	$(NPM) install

# ══════════════════════════════════════════════════════════════════════════════
#  CSS
# ══════════════════════════════════════════════════════════════════════════════

css-build: ## Build Tailwind CSS (minified, one-shot)
	@echo "→ Building Tailwind CSS..."
	$(NPM) run build:css

css-watch: ## Watch and rebuild Tailwind CSS on file changes
	@echo "→ Watching Tailwind CSS (Ctrl+C to stop)..."
	$(NPM) run watch:css

# ══════════════════════════════════════════════════════════════════════════════
#  DEV RUN
# ══════════════════════════════════════════════════════════════════════════════

dev: css-build ## ★ Start the app locally with hot-reload (uses 'air' if installed, else go run)
ifdef AIR
	@echo "→ Starting with air (hot-reload)..."
	air -build.cmd "$(GO) build -o /tmp/$(APP_NAME) $(CMD_PATH)" \
	    -build.bin "/tmp/$(APP_NAME)"
else
	@echo "→ 'air' not found — starting with go run (no hot-reload)."
	@echo "   Install air for hot-reload: go install github.com/air-verse/air@latest"
	@echo ""
	$(GO) run $(CMD_PATH)
endif

dev-run: css-build ## Run the app directly with go run (no hot-reload)
	@echo "→ Running $(APP_NAME) via go run..."
	$(GO) run $(CMD_PATH)

dev-docker: ## ★ Build and start dev environment in Docker Compose (with fresh build)
	@echo "→ Starting dev environment via Docker Compose (build from source)..."
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) up --build

dev-docker-down: ## Stop the dev Docker Compose environment
	@echo "→ Stopping dev Docker Compose..."
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) down

# ══════════════════════════════════════════════════════════════════════════════
#  BUILD  (local binary)
# ══════════════════════════════════════════════════════════════════════════════

build: css-build build-go ## ★ Full production build: Tailwind CSS + Go binary

build-go: ## Compile the Go binary for the current OS/arch → bin/homeland
	@echo "→ Compiling Go binary ($(GOOS)/$(GOARCH))..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		$(GO) build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD_PATH)
	@echo "   Binary: $(BINARY)"

build-linux: css-build ## Cross-compile for Linux amd64 → bin/homeland-linux-amd64
	@echo "→ Cross-compiling for Linux amd64..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=amd64 \
		$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-amd64 $(CMD_PATH)

build-darwin: css-build ## Cross-compile for macOS arm64 → bin/homeland-darwin-arm64
	@echo "→ Cross-compiling for macOS arm64..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=darwin GOARCH=arm64 \
		$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-darwin-arm64 $(CMD_PATH)

build-windows: css-build ## Cross-compile for Windows amd64 → bin/homeland-windows-amd64.exe
	@echo "→ Cross-compiling for Windows amd64..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=windows GOARCH=amd64 \
		$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-windows-amd64.exe $(CMD_PATH)

# ══════════════════════════════════════════════════════════════════════════════
#  TESTING
# ══════════════════════════════════════════════════════════════════════════════

test: ## Run all tests
	@echo "→ Running tests..."
	$(GO) test ./... -v -timeout 60s

test-cover: ## Run tests with coverage report → coverage.html
	@echo "→ Running tests with coverage..."
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "   Coverage report: coverage.html"

test-race: ## Run tests with the race detector enabled
	@echo "→ Running tests with race detector..."
	$(GO) test -race ./... -timeout 60s

# ══════════════════════════════════════════════════════════════════════════════
#  LINT / FORMAT
# ══════════════════════════════════════════════════════════════════════════════

lint: vet ## Run go vet (add golangci-lint if installed)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "→ Running golangci-lint..."; \
		golangci-lint run ./...; \
	else \
		echo "   golangci-lint not found — skipping (install: https://golangci-lint.run/usage/install/)"; \
	fi

vet: ## Run go vet
	@echo "→ Running go vet..."
	$(GO) vet ./...

fmt: ## Format all Go source files with gofmt
	@echo "→ Formatting Go source files..."
	gofmt -w -s .
	@echo "   Done."

# ══════════════════════════════════════════════════════════════════════════════
#  DOCKER  (image lifecycle)
# ══════════════════════════════════════════════════════════════════════════════

docker-build: ## ★ Build the production Docker image (multi-stage)
	@echo "→ Building Docker image: $(IMAGE_NAME):$(IMAGE_TAG)"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) \
		-t $(IMAGE_NAME):latest \
		-f $(DOCKERFILE) .
	@echo "   Image: $(IMAGE_NAME):$(IMAGE_TAG)"

docker-push: docker-build ## Build and push image to GHCR
	@echo "→ Pushing Docker image: $(IMAGE_NAME):$(IMAGE_TAG)"
	docker push $(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(IMAGE_NAME):latest

docker-run: ## Run the production Docker image locally (quick smoke test)
	@echo "→ Running Docker container on http://localhost:3000 ..."
	docker run --rm \
		-p 3000:3000 \
		-v "$(PWD)/configs:/app/configs:ro" \
		-v /var/run/docker.sock:/var/run/docker.sock:ro \
		--group-add $(DOCKER_GID) \
		-e TZ=UTC \
		$(IMAGE_NAME):latest

# ══════════════════════════════════════════════════════════════════════════════
#  PRODUCTION  (docker compose)
# ══════════════════════════════════════════════════════════════════════════════

prod-up: ## ★ Pull latest image and start production via Docker Compose (detached)
	@echo "→ Starting production environment (DOCKER_GID=$(DOCKER_GID))..."
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) pull
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) up -d
	@echo ""
	@echo "✅  Homeland is running → http://localhost:3000"
	@echo "   Logs: make prod-logs"

prod-down: ## Stop and remove production containers
	@echo "→ Stopping production environment..."
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) down

prod-restart: ## Restart production containers (no rebuild)
	@echo "→ Restarting production containers..."
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) restart

prod-logs: ## Tail production container logs (Ctrl+C to stop)
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) logs -f

prod-ps: ## Show status of production containers
	DOCKER_GID=$(DOCKER_GID) docker compose -f $(COMPOSE_FILE) ps

# ══════════════════════════════════════════════════════════════════════════════
#  CLEAN
# ══════════════════════════════════════════════════════════════════════════════

clean: clean-bin clean-css ## Remove all build artifacts
	@echo "✅  Clean complete."

clean-bin: ## Remove compiled binaries (bin/)
	@echo "→ Removing binaries..."
	rm -rf $(BIN_DIR)

clean-css: ## Remove generated Tailwind CSS output
	@echo "→ Removing generated CSS..."
	rm -f $(TAILWIND_OUT)

clean-docker: ## Remove locally built Docker images for this project
	@echo "→ Removing Docker images for $(IMAGE_NAME)..."
	docker images --filter "reference=$(IMAGE_NAME)" -q | xargs -r docker rmi -f

# ══════════════════════════════════════════════════════════════════════════════
#  UTILITIES
# ══════════════════════════════════════════════════════════════════════════════

version: ## Print version info
	@echo "App:        $(APP_NAME)"
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build date: $(BUILD_DATE)"
	@echo "GOOS:       $(GOOS)"
	@echo "GOARCH:     $(GOARCH)"

check-tools: ## Verify required tools are installed
	@echo "→ Checking required tools..."
	@command -v go     >/dev/null 2>&1 || { echo "  ✗ go not found.  Install: https://go.dev/dl/"; exit 1; }
	@command -v node   >/dev/null 2>&1 || { echo "  ✗ node not found. Install: https://nodejs.org"; exit 1; }
	@command -v npm    >/dev/null 2>&1 || { echo "  ✗ npm not found.  Install: https://nodejs.org"; exit 1; }
	@command -v docker >/dev/null 2>&1 || { echo "  ✗ docker not found. Install: https://docs.docker.com/get-docker/"; exit 1; }
	@echo "  ✔ go     $(shell go version | awk '{print $$3}')"
	@echo "  ✔ node   $(shell node --version)"
	@echo "  ✔ npm    $(shell npm --version)"
	@echo "  ✔ docker $(shell docker --version | awk '{print $$3}' | tr -d ',')"
	@if command -v air >/dev/null 2>&1; then \
		echo "  ✔ air    $(shell air -v 2>/dev/null | head -1 || echo 'installed')"; \
	else \
		echo "  ⚠  air not found (optional, for hot-reload). Install: go install github.com/air-verse/air@latest"; \
	fi
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "  ✔ golangci-lint $(shell golangci-lint --version 2>/dev/null | awk '{print $$4}')"; \
	else \
		echo "  ⚠  golangci-lint not found (optional). Install: https://golangci-lint.run/usage/install/"; \
	fi