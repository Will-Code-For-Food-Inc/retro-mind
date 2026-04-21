package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/agent"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/mcp"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/serve"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/tui"
)

func main() {
	loadDotEnv()

	dataDir := os.Getenv("ROM_TAGGER_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "rom-tagger")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fatalf("create data dir: %v", err)
	}

	romBase := os.Getenv("ROM_TAGGER_ROM_PATH")
	if romBase == "" {
		romBase = "/data/nfs/roms"
	}

	if err := db.Open(dataDir); err != nil {
		fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.MigrateJSON(dataDir); err != nil {
		fatalf("migrate: %v", err)
	}

	cmd := ""
	args := os.Args[1:]
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := fs.String("addr", ":8765", "listen address")
		fs.Parse(args[1:])
		ag, _ := agent.New(dataDir)
		mcpSrv := mcp.NewServer(romBase, ag)
		srv := serve.NewServer(mcpSrv)
		fmt.Fprintf(os.Stderr, "retro-mind: serving on http://%s\n", *addr)
		if err := srv.Serve(*addr); err != nil {
			fatalf("serve: %v", err)
		}

	case "mcp":
		ag, _ := agent.New(dataDir)
		mcpSrv := mcp.NewServer(romBase, ag)
		mcpSrv.ServeStdio(os.Stdin, os.Stdout)

	case "tui":
		tui.Run()

	default:
		fmt.Fprintf(os.Stderr, "usage: retro-mind <serve|mcp|tui>\n")
		os.Exit(1)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "retro-mind: "+format+"\n", args...)
	os.Exit(1)
}

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
