package tui

import (
	"strings"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
)

// Game is the TUI's view of a game, including its ID for detail lookups.
type Game struct {
	ID       int64
	Name     string
	Platform string
	CRCs     []string
	Tags     []string
}

// Tag is the TUI's view of a tag with its game count.
type Tag struct {
	Name  string
	Count int
}

// Playlist is the TUI's view of a playlist.
type Playlist struct {
	ID          int64
	Name        string
	Description string
	CreatedAt   string
	GameCount   int
}

// ── Game queries ──────────────────────────────────────────────────────────

func queryGames(filter string) ([]Game, error) {
	rows, err := db.DB.Query("SELECT id, name, platform FROM games ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	filter = strings.ToLower(filter)
	var games []Game
	for rows.Next() {
		var g Game
		rows.Scan(&g.ID, &g.Name, &g.Platform)
		g.CRCs = queryGameCRCs(g.ID)
		g.Tags = queryGameTags(g.ID)

		if filter != "" {
			if !matchesFilter(g, filter) {
				continue
			}
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

func matchesFilter(g Game, filter string) bool {
	if strings.Contains(strings.ToLower(g.Name), filter) {
		return true
	}
	if strings.Contains(strings.ToLower(g.Platform), filter) {
		return true
	}
	for _, t := range g.Tags {
		if strings.Contains(strings.ToLower(t), filter) {
			return true
		}
	}
	return false
}

func queryGameByID(id int64) (*Game, error) {
	var g Game
	err := db.DB.QueryRow("SELECT id, name, platform FROM games WHERE id=?", id).Scan(&g.ID, &g.Name, &g.Platform)
	if err != nil {
		return nil, err
	}
	g.CRCs = queryGameCRCs(g.ID)
	g.Tags = queryGameTags(g.ID)
	return &g, nil
}

func queryGameCRCs(gameID int64) []string {
	rows, err := db.DB.Query("SELECT crc FROM game_crcs WHERE game_id=? ORDER BY crc", gameID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var crcs []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		crcs = append(crcs, c)
	}
	return crcs
}

func queryGameTags(gameID int64) []string {
	rows, err := db.DB.Query(
		"SELECT t.name FROM tags t JOIN game_tags gt ON t.id=gt.tag_id WHERE gt.game_id=? ORDER BY t.name",
		gameID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tags = append(tags, t)
	}
	return tags
}

// ── Tag queries ───────────────────────────────────────────────────────────

func queryTags() ([]Tag, error) {
	rows, err := db.DB.Query(`
		SELECT t.name, COUNT(gt.game_id) as cnt
		FROM tags t
		LEFT JOIN game_tags gt ON t.id=gt.tag_id
		GROUP BY t.id
		ORDER BY cnt DESC, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []Tag
	for rows.Next() {
		var t Tag
		rows.Scan(&t.Name, &t.Count)
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func queryGamesByTag(tagName string) ([]Game, error) {
	rows, err := db.DB.Query(`
		SELECT g.id, g.name, g.platform FROM games g
		JOIN game_tags gt ON g.id=gt.game_id
		JOIN tags t ON t.id=gt.tag_id
		WHERE t.name=?
		ORDER BY g.name`, tagName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var games []Game
	for rows.Next() {
		var g Game
		rows.Scan(&g.ID, &g.Name, &g.Platform)
		g.CRCs = queryGameCRCs(g.ID)
		g.Tags = queryGameTags(g.ID)
		games = append(games, g)
	}
	return games, rows.Err()
}

// ── Playlist queries ──────────────────────────────────────────────────────

func queryPlaylists() ([]Playlist, error) {
	rows, err := db.DB.Query(`
		SELECT p.id, p.name, p.description, p.created_at, COUNT(pg.game_id)
		FROM playlists p
		LEFT JOIN playlist_games pg ON p.id=pg.playlist_id
		GROUP BY p.id ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.GameCount)
		out = append(out, p)
	}
	return out, rows.Err()
}

func queryPlaylistGames(playlistID int64) ([]Game, error) {
	rows, err := db.DB.Query(`
		SELECT g.id, g.name, g.platform FROM games g
		JOIN playlist_games pg ON g.id=pg.game_id
		WHERE pg.playlist_id=?
		ORDER BY pg.position`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var games []Game
	for rows.Next() {
		var g Game
		rows.Scan(&g.ID, &g.Name, &g.Platform)
		g.CRCs = queryGameCRCs(g.ID)
		g.Tags = queryGameTags(g.ID)
		games = append(games, g)
	}
	return games, rows.Err()
}

// ── Stats ─────────────────────────────────────────────────────────────────

func queryStats() (gameCount, tagCount, playlistCount int) {
	db.DB.QueryRow("SELECT COUNT(*) FROM games").Scan(&gameCount)
	db.DB.QueryRow("SELECT COUNT(*) FROM tags").Scan(&tagCount)
	db.DB.QueryRow("SELECT COUNT(*) FROM playlists").Scan(&playlistCount)
	return
}
