package main

// GameEntry is the public-facing DTO for a game, used by MCP tools and the HTTP explorer.
type GameEntry struct {
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	CRCs     []string `json:"crcs"`
	Tags     []string `json:"tags"`
}
