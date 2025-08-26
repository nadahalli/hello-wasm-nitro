#!/bin/bash

# Build script for creating Nitro Enclave EIF file

set -e

echo "Building Nitro Enclave for WebAssembly GCD service..."

# Build the Docker image
echo "Building Docker image..."
docker build -t wasm-gcd-enclave .

# Convert Docker image to EIF
echo "Converting Docker image to EIF..."
nitro-cli build-enclave \
    --docker-uri wasm-gcd-enclave:latest \
    --output-file wasm-gcd-enclave.eif

echo "EIF file created: wasm-gcd-enclave.eif"

# Show EIF info including PCR measurements
echo "EIF Information:"
nitro-cli describe-eif --eif-path wasm-gcd-enclave.eif

echo ""
echo "PCR Measurements (save these for verification):"
nitro-cli describe-eif --eif-path wasm-gcd-enclave.eif | jq -r '.Measurements'

echo ""
echo "Build complete! You can now run the enclave with:"
echo "nitro-cli run-enclave --eif-path wasm-gcd-enclave.eif --memory 512 --cpu-count 2 --enclave-cid 16"
