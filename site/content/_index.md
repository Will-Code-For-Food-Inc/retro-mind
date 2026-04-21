---
title: rom-tagger
type: docs
---

# rom-tagger

A ROM library tagging tool with MCP server, local LLM agent, and TUI.

## What it does

- Scans ROM files and stores metadata in a local SQLite database
- Fetches game metadata from RAWG and other sources
- Tags games with "vibe" tags using a local LLM agent (Ollama)
- Exposes a [Model Context Protocol](https://modelcontextprotocol.io) server for Claude Code integration
- Provides a terminal UI for browsing, tagging, and managing playlists

## Quick start

```bash
# Build the MCP backend
make backend

# Build the TUI (with agent support)
make agent-tui

# Run
ROM_TAGGER_ROM_PATH=/path/to/roms ./build/rom-tagger-tui-agent
```

## Sections

- [Guide]({{< relref "/guide" >}}) — usage, configuration, commands
- [API Reference]({{< relref "/api" >}}) — generated from Go source
