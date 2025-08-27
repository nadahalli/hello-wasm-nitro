package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/mdlayher/vsock"
)

// WASMRequest represents a request to execute WASM code
type WASMRequest struct {
	WASMCode     string            `json:"wasm_code"`     // WAT text with template variables
	FunctionName string            `json:"function_name"` // Function to call in the WASM module
	Args         []int32           `json:"args"`          // Arguments to pass to the function
	Secrets      map[string]string `json:"secrets"`       // Secret values to inject into template
}

// WASMResponse represents the response from WASM execution
type WASMResponse struct {
	Result int32  `json:"result"`
	Error  string `json:"error,omitempty"`
}

const (
	// Port for our WASM service
	WASMPort = 8080
	// Enclave CID (the enclave you're running)
	EnclaveCID = 16 // This should match the CID you used when running the enclave
)

type HostService struct {
	mu               sync.RWMutex
	enclaveConn      net.Conn
	enclaveConnected bool
}

func NewHostService() *HostService {
	return &HostService{}
}

func (h *HostService) connectToEnclave() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.enclaveConnected {
		return nil
	}

	log.Printf("Connecting to enclave at CID %d, port %d", EnclaveCID, WASMPort)

	conn, err := vsock.Dial(EnclaveCID, WASMPort, &vsock.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect to enclave: %v", err)
	}

	h.enclaveConn = conn
	h.enclaveConnected = true
	log.Println("Successfully connected to enclave")
	return nil
}

func (h *HostService) forwardToEnclave(req WASMRequest) (WASMResponse, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if !h.enclaveConnected || h.enclaveConn == nil {
		return WASMResponse{}, fmt.Errorf("not connected to enclave")
	}

	encoder := json.NewEncoder(h.enclaveConn)
	decoder := json.NewDecoder(h.enclaveConn)

	log.Printf("Forwarding to enclave: function=%s, args=%v, code_length=%d",
		req.FunctionName, req.Args, len(req.WASMCode))

	// Send request to enclave
	if err := encoder.Encode(req); err != nil {
		return WASMResponse{}, fmt.Errorf("failed to send request to enclave: %v", err)
	}

	log.Println("Request sent to enclave, waiting for response...")

	// Receive response from enclave
	var response WASMResponse
	if err := decoder.Decode(&response); err != nil {
		return WASMResponse{}, fmt.Errorf("failed to decode WASM response from enclave: %v", err)
	}

	log.Printf("Received response from enclave: result=%d, error=%s", response.Result, response.Error)

	return response, nil
}

func main() {
	log.Println("Starting enclave host...")

	hostService := NewHostService()

	// Try to connect to enclave
	log.Println("Attempting to connect to enclave...")
	if err := hostService.connectToEnclave(); err != nil {
		log.Printf("Warning: Could not connect to enclave initially: %v", err)
		log.Println("Will retry when handling client requests")
	}

	// Listen on TCP for clients (since host process runs on EC2, not in enclave)
	listener, err := net.Listen("tcp", ":8081")
	if err != nil {
		log.Fatalf("Failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	log.Printf("Listening for clients on TCP port 8081")
	log.Printf("Ready to forward requests to enclave on CID %d", EnclaveCID)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		log.Println("New client connection")
		go handleClientConnection(conn, hostService)
	}
}

func handleClientConnection(conn net.Conn, hostService *HostService) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	log.Println("Client connected, handling requests...")

	for {
		var req WASMRequest
		if err := decoder.Decode(&req); err != nil {
			log.Printf("Failed to decode request or client disconnected: %v", err)
			return
		}

		log.Printf("Received WASM request from client: function=%s, args=%v", req.FunctionName, req.Args)

		// Try to connect to enclave if not connected
		if !hostService.enclaveConnected {
			if err := hostService.connectToEnclave(); err != nil {
				response := WASMResponse{
					Result: 0,
					Error:  fmt.Sprintf("Could not connect to enclave: %v", err),
				}
				encoder.Encode(response)
				continue
			}
		}

		// Forward to enclave
		wasmResp, err := hostService.forwardToEnclave(req)
		if err != nil {
			log.Printf("Failed to forward request to enclave: %v", err)
			response := WASMResponse{
				Result: 0,
				Error:  fmt.Sprintf("Enclave communication error: %v", err),
			}
			encoder.Encode(response)
			continue
		}

		log.Printf("Sending response to client: %s(%v) = %d", req.FunctionName, req.Args, wasmResp.Result)

		if err := encoder.Encode(wasmResp); err != nil {
			log.Printf("Failed to encode response to client: %v", err)
			return
		}
	}
}
