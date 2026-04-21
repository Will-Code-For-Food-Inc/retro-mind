//go:build !slim

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

var (
	mcpSessions   sync.Map // sessionID -> chan []byte
)

func registerMCPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mcp/sse", handleMCPSSE)
	mux.HandleFunc("/mcp/message", handleMCPMessage)
}

// handleMCPSSE opens an SSE stream for an MCP client.
// It immediately sends an "endpoint" event with the POST URL for this session.
func handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()
	ch := make(chan []byte, 32)
	mcpSessions.Store(sessionID, ch)
	defer mcpSessions.Delete(sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Fprintf(w, "event: endpoint\ndata: /mcp/message?session=%s\n\n", sessionID)
	flusher.Flush()

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleMCPMessage receives a JSON-RPC request and dispatches it,
// writing the response back to the session's SSE stream.
func handleMCPMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session")
	val, ok := mcpSessions.Load(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	ch := val.(chan []byte)

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sseWrite := sseWriter(ch)
		writeError(nil, -32700, "parse error", nil, sseWrite)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	handle(req, sseWriter(ch))
	w.WriteHeader(http.StatusAccepted)
}

func sseWriter(ch chan []byte) writeFn {
	return func(b []byte) {
		select {
		case ch <- b:
		default:
			// drop if buffer full
		}
	}
}
