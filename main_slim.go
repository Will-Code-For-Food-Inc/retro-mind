//go:build slim

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	serveAddr := flag.String("serve", "127.0.0.1:8765", "address to listen on")
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

	if err := openDB(dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	fmt.Fprintf(os.Stderr, "rom-tagger: serving on http://%s\n", *serveAddr)
	if err := Serve(*serveAddr); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: serve error: %v\n", err)
		os.Exit(1)
	}
}
