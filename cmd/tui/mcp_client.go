package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type toolCatalogMsg struct {
	tools []toolSchema
	err   error
}

type toolCallMsg struct {
	command string
	output  string
	isError bool
	err     error
}

type toolSchema struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	InputSchema toolSpec `json:"inputSchema"`
}

type toolSpec struct {
	Type       string                  `json:"type"`
	Properties map[string]toolProperty `json:"properties"`
	Required   []string                `json:"required"`
}

type toolProperty struct {
	Type        string        `json:"type"`
	Description string        `json:"description"`
	Items       *toolProperty `json:"items"`
}

type mcpClient struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  *strings.Builder
	nextID  int
	started bool
}

func newMCPClient() *mcpClient {
	return &mcpClient{stderr: &strings.Builder{}}
}

func (c *mcpClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	_ = c.stdin.Close()
	err := c.cmd.Process.Kill()
	_, _ = c.cmd.Process.Wait()
	c.cmd = nil
	c.started = false
	return err
}

func (c *mcpClient) ensureStarted() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	bin, err := resolveROMTaggerBinary()
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}
	slog.Info("mcp: starting", "binary", bin)
	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = c.stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.nextID = 1
	c.started = true
	if _, err := c.requestLocked("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "rom-tagger-tui", "version": "0.1.0"},
	}); err != nil {
		return fmt.Errorf("initialize: %w (stderr: %s)", err, c.stderr.String())
	}
	slog.Info("mcp: initialized")
	return nil
}

func (c *mcpClient) ListTools() ([]toolSchema, error) {
	if err := c.ensureStarted(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := c.requestLocked("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []toolSchema `json:"tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload.Tools, nil
}

func (c *mcpClient) CallTool(name string, args map[string]interface{}) (string, bool, error) {
	if err := c.ensureStarted(); err != nil {
		return "", false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := c.requestLocked("tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", false, err
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false, err
	}
	var parts []string
	for _, item := range payload.Content {
		if item.Type == "text" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n"), payload.IsError, nil
}

func (c *mcpClient) requestLocked(method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID
	c.nextID++
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		if c.stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(c.stderr.String()))
		}
		return nil, err
	}
	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      interface{}     `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error.Message)
	}
	return resp.Result, nil
}

func resolveROMTaggerBinary() (string, error) {
	if path := os.Getenv("ROM_TAGGER_MCP_BIN"); path != "" {
		return path, nil
	}
	// Try rom-tagger-mcp first (Nix-installed name), then rom-tagger (build dir name).
	for _, name := range []string{"rom-tagger-mcp", "rom-tagger"} {
		if path, err := exec.LookPath(name); err == nil {
			// Don't launch ourselves — skip if it resolves to the TUI binary.
			if self, err2 := os.Executable(); err2 == nil {
				selfReal, _ := filepath.EvalSymlinks(self)
				pathReal, _ := filepath.EvalSymlinks(path)
				if selfReal == pathReal {
					continue
				}
			}
			return path, nil
		}
	}

	searchRoots := []string{}
	if wd, err := os.Getwd(); err == nil {
		searchRoots = append(searchRoots, wd)
	}
	if exe, err := os.Executable(); err == nil {
		searchRoots = append(searchRoots, filepath.Dir(exe))
	}
	for _, root := range searchRoots {
		candidate := filepath.Join(root, "rom-tagger")
		slog.Debug("mcp: checking candidate", "path", candidate)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find rom-tagger MCP binary; set ROM_TAGGER_MCP_BIN or put rom-tagger-mcp on PATH")
}

func findBinaryUpward(start, rel string) (string, bool) {
	dir := start
	for {
		candidate := filepath.Join(dir, rel)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
