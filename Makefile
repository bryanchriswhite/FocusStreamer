.PHONY: build build-backend build-frontend dev dev-backend dev-frontend test clean install-deps help

# Default target
.DEFAULT_GOAL := help

# Binary name
BINARY_NAME=focusstreamer
BUILD_DIR=build

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Frontend parameters
NPM=npm
WEB_DIR=web

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install-deps: ## Install Go dependencies
	@echo "Installing Go dependencies..."
	$(GOGET) github.com/BurntSushi/xgb
	$(GOGET) github.com/gorilla/mux
	$(GOGET) github.com/gorilla/websocket
	$(GOGET) github.com/spf13/cobra@latest
	$(GOGET) github.com/spf13/viper@latest
	$(GOGET) gopkg.in/yaml.v3
	$(GOMOD) tidy

build: build-frontend build-backend ## Build the entire application (frontend + backend)
	@echo "Build complete!"

build-backend: ## Build the Go backend
	@echo "Building backend..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/focusstreamer
	@echo "Backend built: $(BUILD_DIR)/$(BINARY_NAME)"

build-frontend: ## Build the React frontend
	@echo "Installing frontend dependencies..."
	@cd $(WEB_DIR) && $(NPM) install
	@echo "Building frontend..."
	@cd $(WEB_DIR) && $(NPM) run build
	@echo "Frontend built: $(WEB_DIR)/dist"

dev: ## Run both backend and frontend in development mode
	@echo "Starting development servers..."
	@$(MAKE) dev-backend & $(MAKE) dev-frontend

dev-backend: ## Run backend in development mode
	@echo "Starting backend server..."
	$(GOCMD) run ./cmd/focusstreamer serve

dev-frontend: ## Run frontend in development mode
	@echo "Starting frontend dev server..."
	@cd $(WEB_DIR) && $(NPM) install && $(NPM) run dev

test: ## Run tests
	@echo "Running tests..."
	$(GOTEST) -v ./...

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@if [ -d "$(WEB_DIR)/dist" ]; then rm -rf $(WEB_DIR)/dist; fi
	@echo "Clean complete"

run: build ## Build and run the application
	@echo "Running FocusStreamer..."
	./$(BUILD_DIR)/$(BINARY_NAME) serve

.PHONY: docker-build docker-run docker-stop docker-logs
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t focusstreamer:latest .

docker-run: ## Run Docker container
	@echo "Starting Docker container..."
	docker-compose up -d

docker-stop: ## Stop Docker container
	@echo "Stopping Docker container..."
	docker-compose down

docker-logs: ## Show Docker container logs
	@echo "Showing logs..."
	docker-compose logs -f
