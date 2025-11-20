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
	$(GOMOD) tidy

build: build-backend ## Build the entire application (backend only for now)
	@echo "Build complete!"

build-backend: ## Build the Go backend
	@echo "Building backend..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server
	@echo "Backend built: $(BUILD_DIR)/$(BINARY_NAME)"

build-frontend: ## Build the React frontend (when implemented)
	@if [ -d "$(WEB_DIR)" ]; then \
		echo "Building frontend..."; \
		cd $(WEB_DIR) && $(NPM) run build; \
	else \
		echo "Frontend not yet implemented"; \
	fi

dev: ## Run both backend and frontend in development mode
	@echo "Starting development servers..."
	@$(MAKE) dev-backend & $(MAKE) dev-frontend

dev-backend: ## Run backend in development mode
	@echo "Starting backend server..."
	$(GOCMD) run ./cmd/server/main.go

dev-frontend: ## Run frontend in development mode (when implemented)
	@if [ -d "$(WEB_DIR)" ]; then \
		echo "Starting frontend dev server..."; \
		cd $(WEB_DIR) && $(NPM) run dev; \
	else \
		echo "Frontend not yet implemented"; \
		echo "You can still access the API at http://localhost:8080/api"; \
	fi

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
	./$(BUILD_DIR)/$(BINARY_NAME)

.PHONY: docker-build docker-run
docker-build: ## Build Docker image (future feature)
	@echo "Docker support coming soon..."

docker-run: ## Run in Docker (future feature)
	@echo "Docker support coming soon..."
