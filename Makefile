.PHONY: build run test clean install-deps validate help

# Build configuration
BINARY_NAME=prop-voter
BUILD_DIR=bin
CONFIG_FILE=config.yaml

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/prop-voter

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/prop-voter
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/prop-voter
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/prop-voter

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	@if [ ! -f $(CONFIG_FILE) ]; then \
		echo "Error: $(CONFIG_FILE) not found. Please copy config.example.yaml to config.yaml and configure it."; \
		exit 1; \
	fi
	./$(BUILD_DIR)/$(BINARY_NAME) -config $(CONFIG_FILE)

# Run in debug mode
run-debug: build
	@echo "Running $(BINARY_NAME) in debug mode..."
	@if [ ! -f $(CONFIG_FILE) ]; then \
		echo "Error: $(CONFIG_FILE) not found. Please copy config.example.yaml to config.yaml and configure it."; \
		exit 1; \
	fi
	./$(BUILD_DIR)/$(BINARY_NAME) -config $(CONFIG_FILE) -debug

# Validate configuration and setup
validate: build
	@echo "Validating configuration..."
	@if [ ! -f $(CONFIG_FILE) ]; then \
		echo "Error: $(CONFIG_FILE) not found. Please copy config.example.yaml to config.yaml and configure it."; \
		exit 1; \
	fi
	./$(BUILD_DIR)/$(BINARY_NAME) -config $(CONFIG_FILE) -validate

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Install dependencies
install-deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	rm -f prop-voter.db

# Setup development environment
setup: install-deps
	@echo "Setting up development environment..."
	@if [ ! -f $(CONFIG_FILE) ]; then \
		echo "Creating config file from example..."; \
		cp config.example.yaml $(CONFIG_FILE); \
		echo "Please edit $(CONFIG_FILE) with your actual configuration."; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	golangci-lint run

# Install golangci-lint if not present
install-lint:
	@which golangci-lint || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)

# Test Discord setup
test-discord:
	@echo "Testing Discord bot configuration..."
	@echo "Usage: make test-discord TOKEN=your_token CHANNEL=channel_id USER=user_id"
	@if [ -z "$(TOKEN)" ] || [ -z "$(CHANNEL)" ] || [ -z "$(USER)" ]; then \
		echo "Error: Missing required parameters."; \
		echo "Example: make test-discord TOKEN=OTg4NzY1... CHANNEL=987654321... USER=123456789..."; \
		exit 1; \
	fi
	cd scripts && go run test-discord.go -token $(TOKEN) -channel $(CHANNEL) -user $(USER)

# Help
help:
	@echo "Available commands:"
	@echo "  build        - Build the application"
	@echo "  build-all    - Build for multiple platforms"
	@echo "  run          - Build and run the application"
	@echo "  run-debug    - Run in debug mode"
	@echo "  validate     - Validate configuration and chains"
	@echo "  test         - Run tests"
	@echo "  test-coverage- Run tests with coverage"
	@echo "  test-discord - Test Discord bot setup (requires TOKEN, CHANNEL, USER)"
	@echo "  install-deps - Install Go dependencies"
	@echo "  clean        - Clean build artifacts"
	@echo "  setup        - Setup development environment"
	@echo "  fmt          - Format code"
	@echo "  lint         - Lint code"
	@echo "  install-lint - Install golangci-lint"
	@echo "  help         - Show this help"