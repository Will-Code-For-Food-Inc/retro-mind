package mcp

import "encoding/json"

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

// WriteFn writes a serialised JSON-RPC message to the transport.
type WriteFn func([]byte)

// WriteResult sends a successful JSON-RPC response.
func WriteResult(id interface{}, result interface{}, write WriteFn) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	write(data)
}

// WriteError sends a JSON-RPC error response.
func WriteError(id interface{}, code int, msg string, data interface{}, write WriteFn) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   map[string]interface{}{"code": code, "message": msg, "data": data},
	}
	out, _ := json.Marshal(resp)
	write(out)
}
