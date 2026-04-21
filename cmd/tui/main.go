package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Log to file since TUI owns the terminal.
	dataDir := os.Getenv("ROM_TAGGER_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "rom-tagger")
	}
	logPath := filepath.Join(dataDir, "tui.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		var handler slog.Handler
		if os.Getenv("LOG_FORMAT") == "text" {
			handler = slog.NewTextHandler(logFile, nil)
		} else {
			handler = slog.NewJSONHandler(logFile, nil)
		}
		slog.SetDefault(slog.New(handler))
		defer logFile.Close()
	}

	if err := openDB(); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger-tui: %v\n", err)
		os.Exit(1)
	}
	defer closeDB()

	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger-tui: %v\n", err)
		os.Exit(1)
	}
}
