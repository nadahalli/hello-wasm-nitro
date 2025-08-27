package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
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

func main() {
	if len(os.Args) < 4 {
		fmt.Printf("Usage: %s <wasm-file|wat-content> <function-name> <arg1> [arg2] ...\n", os.Args[0])
		fmt.Println("Examples:")
		fmt.Println("  ./wasm-client simple.wat square 7")
		fmt.Println("  ./wasm-client secret-template.wat secure_compute 100")
		os.Exit(1)
	}

	wasmInput := os.Args[1]
	functionName := os.Args[2]

	// Parse arguments
	var args []int32
	for i := 3; i < len(os.Args); i++ {
		arg, err := strconv.Atoi(os.Args[i])
		if err != nil {
			log.Fatalf("Invalid argument %s: %v", os.Args[i], err)
		}
		args = append(args, int32(arg))
	}

	// Determine if input is a file or inline WAT/WASM content
	var wasmCode string
	if isInlineWAT(wasmInput) {
		// Inline WAT content
		wasmCode = wasmInput
		log.Printf("Using inline WAT content")
	} else {
		// Try to read as file
		content, err := ioutil.ReadFile(wasmInput)
		if err != nil {
			log.Fatalf("Failed to read WASM file %s: %v", wasmInput, err)
		}
		wasmCode = string(content)
		log.Printf("Loaded WASM from file: %s (%d bytes)", wasmInput, len(content))
	}

	log.Printf("Requesting execution: %s(%v)", functionName, args)

	// Mock secret fetching (in real use, this would call AWS Secrets Manager, etc.)
	secrets := map[string]string{
		"API_KEY_HASH":      "sk-abc123def456",
		"SECRET_MULTIPLIER": "7",
		"DB_PASSWORD":       "mySecretPassword",
		"SECRET_KEY":        "42",
		"PRIVATE_KEY":       "-----BEGIN PRIVATE KEY-----",
	}

	if strings.Contains(wasmCode, "import") {
		log.Printf("Template detected - will inject %d secrets", len(secrets))
		for key := range secrets {
			log.Printf("  Available secret: %s", key)
		}
	}

	// Connect to host
	conn, err := net.Dial("tcp", "localhost:8081")
	if err != nil {
		log.Fatalf("Failed to connect to host: %v", err)
	}
	defer conn.Close()

	log.Println("Connected to host")

	// Send WASM execution request with secrets
	request := WASMRequest{
		WASMCode:     wasmCode,
		FunctionName: functionName,
		Args:         args,
		Secrets:      secrets,
	}

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(request); err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}

	log.Println("Request sent, waiting for response...")

	// Receive response
	var response WASMResponse
	if err := decoder.Decode(&response); err != nil {
		log.Fatalf("Failed to decode response: %v", err)
	}

	// Display result
	if response.Error != "" {
		log.Printf("Error from enclave: %s", response.Error)
		os.Exit(1)
	} else {
		fmt.Printf("%s(%v) = %d\n", functionName, args, response.Result)
		log.Println("Secure computation with secrets completed")
	}
}

// Helper to detect inline WAT content
func isInlineWAT(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "(module")
}
