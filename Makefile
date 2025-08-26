.PHONY: all build-host build-client build-enclave build-eif run-host run-enclave test clean

# Build all components
all: build-host build-client build-enclave

# Build local testing components
all-local: build-local-host build-local-client

# Build the host (parent instance)
build-host:
	@echo "Building host..."
	@go build -o bin/host main.go

# Build the client
build-client:
	@echo "Building client..."
	@go build -o bin/client client/main.go

# Build the WASM client
build-wasm-client:
	@echo "Building WASM client..."
	@go build -o bin/wasm-client wasm-client/main.go

# Build local host for testing
build-local-host:
	@echo "Building local host..."
	@go build -o bin/local-host local-host.go

# Build local client for testing
build-local-client:
	@echo "Building local client..."
	@go build -o bin/local-client local-client.go

# Build the enclave server
build-enclave:
	@echo "Building enclave server..."
	@cd enclave && go build -o ../bin/enclave-server main.go

# Build and deploy enclave with secrets support
deploy-enclave:
	@echo "Deploying WASM executor enclave..."
	@nitro-cli terminate-enclave --all || true
	@docker build -f Dockerfile -t wasm-executor-enclave .
	@nitro-cli build-enclave --docker-uri wasm-executor-enclave:latest --output-file wasm-executor-enclave.eif
	@nitro-cli run-enclave --eif-path wasm-executor-enclave.eif --memory 1024 --cpu-count 2 --enclave-cid 16 --debug-mode
	@echo "Enclave deployed successfully!"

# Quick rebuild and redeploy (useful during development)
redeploy: deploy-enclave

# Deploy without debug mode (production)
deploy-enclave-prod:
	@echo "Deploying WASM executor enclave (production mode)..."
	@nitro-cli terminate-enclave --all || true
	@docker build -f Dockerfile -t wasm-executor-enclave .
	@nitro-cli build-enclave --docker-uri wasm-executor-enclave:latest --output-file wasm-executor-enclave.eif
	@nitro-cli run-enclave --eif-path wasm-executor-enclave.eif --memory 1024 --cpu-count 2 --enclave-cid 16
	@echo "Enclave deployed successfully (production mode)!"

# Run the host
run-host:
	@echo "Starting host..."
	@./bin/host

# Run the local host for testing
run-local-host:
	@echo "Starting local host..."
	@./bin/local-host

# Test secret injection
test-secrets: build-wasm-client
	@echo "Testing secret injection with template..."
	@./bin/wasm-client secret-template.wat secure_compute 100

# Test the client (requires host to be running)
test: build-client
	@echo "Testing with client..."
	@./bin/client 48 18

# Run enclave locally (for testing without Nitro)
run-enclave:
	@echo "Running enclave server locally..."
	@./bin/enclave-server

# Clean build artifacts
clean:
	@echo "Cleaning up..."
	@rm -rf bin/
	@docker rmi wasm-gcd-enclave:latest || true
	@rm -f wasm-gcd-enclave.eif

# Initialize Go modules
init:
	@echo "Initializing Go modules..."
	@go mod init hello-wasm-enclave
	@go mod tidy

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

# Verify PCR measurements
verify-pcr:
	@echo "Verifying PCR measurements..."
	@nitro-cli describe-eif --eif-path wasm-gcd-enclave.eif | jq -r '.Measurements'

# Show help
help:
	@echo "Available targets:"
	@echo "  all              - Build all components"
	@echo "  all-local        - Build local testing components"
	@echo "  build-host       - Build the host component"
	@echo "  build-client     - Build the legacy client component" 
	@echo "  build-wasm-client - Build the WASM client"
	@echo "  build-enclave    - Build the enclave server"
	@echo "  build-local-host - Build local host for testing"
	@echo "  build-local-client - Build local client for testing"
	@echo "  deploy-enclave   - Deploy enclave with debug mode"
	@echo "  deploy-enclave-prod - Deploy enclave without debug mode"
	@echo "  redeploy         - Quick rebuild and redeploy enclave"
	@echo "  run-host         - Run the host"
	@echo "  run-local-host   - Run local host for testing"
	@echo "  run-enclave      - Run enclave server locally"
	@echo "  test             - Test with legacy client"
	@echo "  test-local       - Test with local client (TCP)"
	@echo "  test-secrets     - Test secret injection"
	@echo "  clean            - Clean build artifacts"
	@echo "  init             - Initialize Go modules"
	@echo "  deps             - Download dependencies"
	@echo "  verify-pcr       - Show PCR measurements"
	@echo "  help             - Show this help"