FROM public.ecr.aws/amazonlinux/amazonlinux:2023

# Install dependencies including build tools and WebAssembly tools
RUN dnf update -y && \
    dnf install -y wget tar gzip gcc gcc-c++ make python3 cmake && \
    dnf clean all

# Install Go
RUN wget https://go.dev/dl/go1.21.3.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.21.3.linux-amd64.tar.gz && \
    rm go1.21.3.linux-amd64.tar.gz

# Install WebAssembly Binary Toolkit (wabt) for wat2wasm
RUN wget https://github.com/WebAssembly/wabt/releases/download/1.0.34/wabt-1.0.34-ubuntu.tar.gz && \
    tar -xzf wabt-1.0.34-ubuntu.tar.gz && \
    mv wabt-1.0.34/bin/* /usr/local/bin/ && \
    rm -rf wabt-1.0.34* && \
    chmod +x /usr/local/bin/wat2wasm

ENV PATH="/usr/local/go/bin:${PATH}"

# Set working directory
WORKDIR /app

# Create go.mod for vsock and wasmtime
RUN echo "module vsock-wasm-enclave" > go.mod && \
    echo "go 1.21" >> go.mod && \
    echo "" >> go.mod && \
    echo "require (" >> go.mod && \
    echo "    github.com/mdlayher/vsock v1.2.1" >> go.mod && \
    echo "    github.com/bytecodealliance/wasmtime-go v0.40.0" >> go.mod && \
    echo ")" >> go.mod

# Copy WASM executor source code
COPY enclave/main.go ./main.go

# Download dependencies and create go.sum
RUN go mod tidy && go mod download

# Build the enclave binary (CGO needed for vsock)
RUN CGO_ENABLED=1 GOOS=linux go build -o enclave-server .

# Use same base and copy wabt tools
FROM public.ecr.aws/amazonlinux/amazonlinux:2023

# Install runtime dependencies for vsock and wabt
RUN dnf update -y && \
    dnf install -y ca-certificates && \
    dnf clean all

# Copy wabt tools
COPY --from=0 /usr/local/bin/wat2wasm /usr/local/bin/wat2wasm

# Copy the binary
COPY --from=0 /app/enclave-server /enclave-server

# Set the entrypoint
ENTRYPOINT ["/enclave-server"]