package mcp

import (
	"bufio"
	"encoding/json"
	"io"
)

// ServeStdio runs the JSON-RPC stdio loop, reading requests from r and
// writing responses to w.
func (s *Server) ServeStdio(r io.Reader, w io.Writer) {
	write := func(b []byte) {
		w.Write(b)
		w.Write([]byte("\n"))
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			WriteError(nil, -32700, "parse error", nil, write)
			continue
		}
		s.Handle(req, write)
	}
}
