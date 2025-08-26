package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bytecodealliance/wasmtime-go"
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
)

type WASMExecutor struct {
	engine *wasmtime.Engine
}

func NewWASMExecutor() *WASMExecutor {
	return &WASMExecutor{
		engine: wasmtime.NewEngine(),
	}
}

func (w *WASMExecutor) ExecuteWASM(wasmCode, functionName string, args []int32, secrets map[string]string) (int32, error) {
	store := wasmtime.NewStore(w.engine)

	var module *wasmtime.Module
	var err error

	log.Printf("Parsing WASM code (length: %d)", len(wasmCode))
	log.Printf("Secrets received: %d", len(secrets))
	for key, value := range secrets {
		log.Printf("  Secret: %s = %s", key, maskSecret(value))
	}

	// Check if input is WAT text or binary WASM
	if isWATText(wasmCode) {
		log.Println("Detected WAT text format")

		// Process template variables if this is WAT with secrets
		processedWAT := wasmCode
		if len(secrets) > 0 {
			log.Println("Injecting secrets into WAT template...")
			processedWAT, err = injectSecretsIntoWAT(wasmCode, secrets)
			if err != nil {
				return 0, fmt.Errorf("failed to inject secrets: %v", err)
			}
			log.Println("Secrets injected successfully")
			log.Printf("Original WAT length: %d", len(wasmCode))
			log.Printf("Processed WAT length: %d", len(processedWAT))
		}

		// Compile WAT to WASM binary using wat2wasm
		wasmBytes, compileErr := compileWATToWASM(processedWAT)
		if compileErr != nil {
			return 0, fmt.Errorf("failed to compile WAT to WASM: %v", compileErr)
		}
		log.Printf("Successfully compiled WAT to %d bytes of WASM binary", len(wasmBytes))

		module, err = wasmtime.NewModule(w.engine, wasmBytes)
	} else {
		log.Println("Attempting to decode as base64 WASM binary")
		// Assume it's base64 encoded binary WASM
		wasmBytes, decodeErr := base64DecodeWASM(wasmCode)
		if decodeErr != nil {
			return 0, fmt.Errorf("failed to decode WASM bytecode: %v", decodeErr)
		}
		log.Printf("Decoded %d bytes of WASM binary", len(wasmBytes))
		module, err = wasmtime.NewModule(w.engine, wasmBytes)
	}

	if err != nil {
		return 0, fmt.Errorf("failed to create WASM module: %v", err)
	}

	log.Println("WASM module created successfully")

	// If there were imports, we need to provide them when creating the instance
	// But since we replaced imports with globals, we don't need to provide any imports
	instance, err := wasmtime.NewInstance(store, module, []wasmtime.AsExtern{})
	if err != nil {
		return 0, fmt.Errorf("failed to create WASM instance: %v", err)
	}

	log.Println("WASM instance created successfully")

	// List all exports for debugging
	exports := instance.Exports(store)
	log.Printf("Available exports: %d", len(exports))
	for name, _ := range exports {
		log.Printf("  Export: %s", name)
	}

	// Get the requested function
	exportedFunc := instance.GetExport(store, functionName)
	if exportedFunc == nil {
		return 0, fmt.Errorf("function '%s' not found in WASM module", functionName)
	}

	wasmFunc := exportedFunc.Func()
	if wasmFunc == nil {
		return 0, fmt.Errorf("'%s' is not a function", functionName)
	}

	log.Printf("Found function '%s', calling with args: %v", functionName, args)

	// Convert args to interface{} slice for the Call method
	callArgs := make([]interface{}, len(args))
	for i, arg := range args {
		callArgs[i] = arg
	}

	// Call the function
	result, err := wasmFunc.Call(store, callArgs...)
	if err != nil {
		return 0, fmt.Errorf("WASM function call failed: %v", err)
	}

	log.Printf("WASM function returned: %v (type: %T)", result, result)

	// Convert result back to int32
	if resultVal, ok := result.(int32); ok {
		log.Printf("Successfully converted result to int32: %d", resultVal)
		return resultVal, nil
	}

	return 0, fmt.Errorf("unexpected return type from WASM function: %T", result)
}

// injectSecretsIntoWAT replaces import statements with global definitions
func injectSecretsIntoWAT(watCode string, secrets map[string]string) (string, error) {
	result := watCode

	// Pattern to match import statements for secrets
	// Matches: (import "env" "SECRET_NAME" (global $SECRET_NAME i32))
	importPattern := regexp.MustCompile(`\(import\s+"[^"]*"\s+"([^"]+)"\s+\(global\s+\$([^\s\)]+)\s+(i32|i64|f32|f64)\)\)`)

	matches := importPattern.FindAllStringSubmatch(watCode, -1)
	log.Printf("Found %d import statements to process", len(matches))

	for _, match := range matches {
		fullImport := match[0] // Full import statement
		secretName := match[1] // SECRET_NAME
		globalName := match[2] // SECRET_NAME (without $)
		wasmType := match[3]   // i32, i64, etc.

		log.Printf("Processing import: %s (type: %s)", secretName, wasmType)
		log.Printf("Full import statement: %s", fullImport)

		// Check if we have a secret for this import
		if secretValue, exists := secrets[secretName]; exists {
			log.Printf("Injecting secret for %s", secretName)

			// Convert string secret to appropriate WASM value
			wasmValue, err := convertSecretToWASMValue(secretValue, wasmType)
			if err != nil {
				return "", fmt.Errorf("failed to convert secret %s: %v", secretName, err)
			}

			// Replace import with global definition
			globalDef := fmt.Sprintf("(global $%s %s (%s.const %s))", globalName, wasmType, wasmType, wasmValue)
			result = strings.Replace(result, fullImport, globalDef, 1)

			log.Printf("Replaced import with: %s", globalDef)
		} else {
			log.Printf("Warning: secret %s not provided, keeping import", secretName)
		}
	}

	log.Printf("Template processing complete")
	return result, nil
}

// convertSecretToWASMValue converts a string secret to appropriate WASM constant
func convertSecretToWASMValue(secret, wasmType string) (string, error) {
	switch wasmType {
	case "i32":
		// Try to parse as integer
		if intVal, err := strconv.ParseInt(secret, 10, 32); err == nil {
			return fmt.Sprintf("%d", intVal), nil
		}
		// For string secrets, use a hash or checksum as i32
		hash := simpleStringHash(secret)
		return fmt.Sprintf("%d", hash), nil

	case "i64":
		if intVal, err := strconv.ParseInt(secret, 10, 64); err == nil {
			return fmt.Sprintf("%d", intVal), nil
		}
		hash := simpleStringHash(secret)
		return fmt.Sprintf("%d", int64(hash)), nil

	case "f32", "f64":
		if floatVal, err := strconv.ParseFloat(secret, 64); err == nil {
			return fmt.Sprintf("%g", floatVal), nil
		}
		return "", fmt.Errorf("cannot convert string secret to float type %s", wasmType)

	default:
		return "", fmt.Errorf("unsupported WASM type: %s", wasmType)
	}
}

// Simple hash function for string secrets (convert to i32)
func simpleStringHash(s string) int32 {
	var hash int32 = 0
	for _, c := range s {
		hash = hash*31 + int32(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

// Helper function to mask secrets in logs
func maskSecret(secret string) string {
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "***" + secret[len(secret)-4:]
}

// Helper function to compile WAT text to WASM binary using wat2wasm
func compileWATToWASM(watCode string) ([]byte, error) {
	// Create temporary files
	tmpDir := "/tmp"
	watFile := tmpDir + "/temp.wat"
	wasmFile := tmpDir + "/temp.wasm"

	// Write WAT to temporary file
	if err := ioutil.WriteFile(watFile, []byte(watCode), 0644); err != nil {
		return nil, fmt.Errorf("failed to write WAT file: %v", err)
	}
	defer os.Remove(watFile)

	// Compile with wat2wasm
	cmd := exec.Command("wat2wasm", watFile, "-o", wasmFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("wat2wasm compilation failed: %v, output: %s", err, string(output))
	}
	defer os.Remove(wasmFile)

	// Read compiled WASM binary
	wasmBytes, err := ioutil.ReadFile(wasmFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read compiled WASM file: %v", err)
	}

	return wasmBytes, nil
}

// Helper function to detect if input is WAT text format
func isWATText(input string) bool {
	return len(input) > 0 && input[0] == '(' &&
		(strings.Contains(input, "module") || strings.Contains(input, "func"))
}

// Helper function to decode base64 WASM bytecode
func base64DecodeWASM(encoded string) ([]byte, error) {
	// Remove whitespace
	cleaned := strings.ReplaceAll(encoded, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")

	// Try base64 decoding
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		// Try hex decoding as fallback
		if len(cleaned)%2 == 0 {
			hexDecoded := make([]byte, len(cleaned)/2)
			for i := 0; i < len(cleaned); i += 2 {
				b, hexErr := strconv.ParseUint(cleaned[i:i+2], 16, 8)
				if hexErr != nil {
					return nil, err // Return original base64 error
				}
				hexDecoded[i/2] = byte(b)
			}
			return hexDecoded, nil
		}
		return nil, err
	}

	return decoded, nil
}

func main() {
	log.Println("Starting WASM executor enclave...")

	// Add startup delay
	time.Sleep(2 * time.Second)

	// Initialize WASM executor
	wasmExecutor := NewWASMExecutor()

	log.Println("WASM executor initialized successfully")

	log.Println("Setting up vsock listener...")

	// Listen on vsock
	listener, err := vsock.Listen(WASMPort, &vsock.Config{})
	if err != nil {
		log.Fatalf("FATAL: Failed to listen on vsock port %d: %v", WASMPort, err)
	}
	defer listener.Close()

	log.Printf("SUCCESS: Enclave listening on vsock port %d", WASMPort)
	log.Println("Ready to execute arbitrary WASM code!")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("ERROR: Failed to accept connection: %v", err)
			continue
		}

		log.Println("SUCCESS: Connection received from parent!")
		go handleConnection(conn, wasmExecutor)
	}
}

func handleConnection(conn net.Conn, wasmExecutor *WASMExecutor) {
	defer conn.Close()

	log.Println("Handling connection...")

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var wasmReq WASMRequest
		if err := decoder.Decode(&wasmReq); err != nil {
			log.Printf("Failed to decode request or connection closed: %v", err)
			return
		}

		log.Printf("Received WASM execution request: function=%s, args=%v", wasmReq.FunctionName, wasmReq.Args)
		log.Printf("WASM code length: %d bytes", len(wasmReq.WASMCode))
		if len(wasmReq.Secrets) > 0 {
			log.Printf("Secrets provided: %d", len(wasmReq.Secrets))
		}

		// Execute WASM code with secret injection
		result, err := wasmExecutor.ExecuteWASM(wasmReq.WASMCode, wasmReq.FunctionName, wasmReq.Args, wasmReq.Secrets)

		response := WASMResponse{
			Result: result,
			Error:  "",
		}
		if err != nil {
			response.Error = fmt.Sprintf("WASM execution failed: %v", err)
			log.Printf("WASM execution error: %v", err)
		} else {
			log.Printf("WASM execution success: %s(%v) = %d", wasmReq.FunctionName, wasmReq.Args, result)
		}

		if err := encoder.Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
			return
		}

		log.Println("Response sent successfully")
	}
}
