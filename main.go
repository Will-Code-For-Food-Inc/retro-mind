//go:build !slim

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

var romBase string // root of platform ROM directories, e.g. /data/nfs/roms

func main() {
	loadDotEnv() // load .env from working dir if present

	serveAddr := flag.String("serve", "", "start HTTP explorer on this address (e.g. :8765)")
	flag.Parse()

	dataDir := os.Getenv("ROM_TAGGER_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "rom-tagger")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: failed to create data dir: %v\n", err)
		os.Exit(1)
	}

	romBase = os.Getenv("ROM_TAGGER_ROM_PATH")
	if romBase == "" {
		romBase = "/data/nfs/roms"
	}

	if err := openDB(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := migrateJSON(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: migration failed: %v\n", err)
		os.Exit(1)
	}

	if err := initAgent(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: agent init failed: %v\n", err)
		os.Exit(1)
	}

	if *serveAddr != "" {
		fmt.Fprintf(os.Stderr, "rom-tagger: serving explorer on http://%s\n", *serveAddr)
		if err := Serve(*serveAddr); err != nil {
			fmt.Fprintf(os.Stderr, "rom-tagger: serve error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeError(nil, -32700, "parse error", nil)
			continue
		}
		handle(req)
	}
}

func handle(req Request) {
	switch req.Method {
	case "initialize":
		writeResult(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]string{"name": "rom-tagger", "version": "2.0.0"},
		})
	case "notifications/initialized":
		// no response for notifications
	case "tools/list":
		writeResult(req.ID, map[string]interface{}{"tools": toolSchemas()})
	case "tools/call":
		var p struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(req.ID, -32602, "invalid params", nil)
			return
		}
		result, isError := callTool(p.Name, p.Arguments)
		writeResult(req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": result},
			},
			"isError": isError,
		})
	default:
		writeError(req.ID, -32601, "method not found", nil)
	}
}

func writeResult(id interface{}, result interface{}) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

// loadDotEnv reads KEY=VALUE pairs from .env in the working directory.
// Only sets vars that aren't already in the environment.
func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func writeError(id interface{}, code int, msg string, data interface{}) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   map[string]interface{}{"code": code, "message": msg, "data": data},
	}
	out, _ := json.Marshal(resp)
	fmt.Println(string(out))
}
