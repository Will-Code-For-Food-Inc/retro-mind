package db

import "time"

// GameEntry is the public-facing DTO for a game, used by MCP tools and the HTTP explorer.
type GameEntry struct {
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	CRCs     []string `json:"crcs"`
	Tags     []string `json:"tags"`
}

// PlaylistEntry represents a playlist with metadata.
type PlaylistEntry struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	GameCount   int    `json:"game_count"`
}

// SavedView is a persisted 2D scatter-plot configuration.
type SavedView struct {
	Name          string `json:"name"`
	XTag          string `json:"x_tag"`
	YTag          string `json:"y_tag"`
	PointsJSON    string `json:"points_json"`
	PlatformsJSON string `json:"platforms_json"`
	CreatedAt     string `json:"created_at"`
}

// ReviewEntry is a game flagged for manual RAWG review.
type ReviewEntry struct {
	Name     string
	Platform string
	Notes    string
	Skip     bool
}

// TagCount pairs a tag name with its usage count.
type TagCount struct {
	Tag   string
	Count int
}

// Tag is a tag with its game count, for TUI display.
type Tag struct {
	Name      string
	GameCount int
}

// RAWGTag is a community-curated label from RAWG (e.g. "atmospheric", "great soundtrack").
type RAWGTag struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// RAWGMeta is the cached result for a single game.
type RAWGMeta struct {
	RAWGID          int       `json:"rawg_id,omitempty"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Tags            []RAWGTag `json:"tags"`
	Released        string    `json:"released"`
	Metacritic      int       `json:"metacritic,omitempty"`
	BackgroundImage string    `json:"background_image,omitempty"`
	FetchedAt       time.Time `json:"fetched_at"`
	NotFound        bool      `json:"not_found,omitempty"`
}

// UnembeddedGame is a game that has RAWG metadata but no embedding yet.
type UnembeddedGame struct {
	ID          int64
	Name        string
	Description string
}
