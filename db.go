package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func openDB(dataDir string) error {
	dbPath := filepath.Join(dataDir, "rom-tagger.db")
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return ensureSchema()
}

func ensureSchema() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tags (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS games (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			name     TEXT NOT NULL UNIQUE,
			platform TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS game_crcs (
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			crc     TEXT NOT NULL,
			PRIMARY KEY (game_id, crc)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_game_crcs_crc ON game_crcs(crc);
		CREATE TABLE IF NOT EXISTS game_tags (
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (game_id, tag_id)
		);
		CREATE TABLE IF NOT EXISTS rawg_cache (
			cache_key   TEXT PRIMARY KEY,
			rawg_id     INTEGER,
			title       TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			released    TEXT NOT NULL DEFAULT '',
			metacritic  INTEGER NOT NULL DEFAULT 0,
			tags_json   TEXT NOT NULL DEFAULT '[]',
			fetched_at  TEXT NOT NULL,
			not_found   INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS work_queue (
			platform TEXT NOT NULL,
			name     TEXT NOT NULL,
			PRIMARY KEY (platform, name)
		);
		CREATE TABLE IF NOT EXISTS playlists (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS playlist_games (
			playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
			game_id     INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			position    INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (playlist_id, game_id)
		);
		CREATE TABLE IF NOT EXISTS tag_embeddings (
			tag_id INTEGER PRIMARY KEY REFERENCES tags(id) ON DELETE CASCADE,
			vector BLOB NOT NULL,
			model  TEXT NOT NULL DEFAULT 'BAAI/bge-m3'
		);
		CREATE TABLE IF NOT EXISTS game_embeddings (
			game_id INTEGER PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
			vector  BLOB NOT NULL,
			model   TEXT NOT NULL DEFAULT 'BAAI/bge-m3'
		);
		CREATE TABLE IF NOT EXISTS saved_views (
			name        TEXT PRIMARY KEY,
			x_tag       TEXT NOT NULL,
			y_tag       TEXT NOT NULL,
			points_json TEXT NOT NULL,
			created_at  TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS manual_corrections (
			game_name   TEXT PRIMARY KEY,
			rawg_id     INTEGER,
			notes       TEXT NOT NULL DEFAULT '',
			skip        INTEGER NOT NULL DEFAULT 0,
			reviewed_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	if err != nil {
		return err
	}
	// Add background_image column if it doesn't exist (migration)
	db.Exec("ALTER TABLE rawg_cache ADD COLUMN background_image TEXT NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE saved_views ADD COLUMN platforms_json TEXT NOT NULL DEFAULT ''")
	return migrateEmbeddingModel()
}

// ── Migration from JSON files ─────────────────────────────────────────────

func migrateJSON(dataDir string) error {
	tagsPath := filepath.Join(dataDir, "tags.json")
	gamesPath := filepath.Join(dataDir, "games.json")
	metaPath := filepath.Join(dataDir, "metadata.json")
	queuePath := filepath.Join(dataDir, "queue.json")

	// Check if any JSON files exist
	anyExist := false
	for _, p := range []string{tagsPath, gamesPath, metaPath, queuePath} {
		if _, err := os.Stat(p); err == nil {
			anyExist = true
			break
		}
	}
	if !anyExist {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Tags
	if data, err := os.ReadFile(tagsPath); err == nil {
		var tags []string
		if json.Unmarshal(data, &tags) == nil {
			for _, t := range tags {
				tx.Exec("INSERT OR IGNORE INTO tags(name) VALUES(?)", t)
			}
		}
	}

	// Games
	if data, err := os.ReadFile(gamesPath); err == nil {
		var games map[string]GameEntry
		if json.Unmarshal(data, &games) == nil {
			for _, g := range games {
				res, err := tx.Exec(
					"INSERT INTO games(name, platform) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET platform=excluded.platform",
					g.Name, g.Platform,
				)
				if err != nil {
					continue
				}
				gameID, _ := res.LastInsertId()
				// If ON CONFLICT fired, LastInsertId is 0 — look it up
				if gameID == 0 {
					tx.QueryRow("SELECT id FROM games WHERE name=?", g.Name).Scan(&gameID)
				}
				for _, crc := range g.CRCs {
					tx.Exec("INSERT OR IGNORE INTO game_crcs(game_id, crc) VALUES(?,?)", gameID, crc)
				}
				for _, tag := range g.Tags {
					tx.Exec("INSERT OR IGNORE INTO tags(name) VALUES(?)", tag)
					var tagID int64
					tx.QueryRow("SELECT id FROM tags WHERE name=?", tag).Scan(&tagID)
					tx.Exec("INSERT OR IGNORE INTO game_tags(game_id, tag_id) VALUES(?,?)", gameID, tagID)
				}
			}
		}
	}

	// RAWG cache
	if data, err := os.ReadFile(metaPath); err == nil {
		var entries map[string]*RAWGMeta
		if json.Unmarshal(data, &entries) == nil {
			for key, meta := range entries {
				tagsJSON, _ := json.Marshal(meta.Tags)
				notFound := 0
				if meta.NotFound {
					notFound = 1
				}
				tx.Exec(`INSERT OR IGNORE INTO rawg_cache(cache_key, rawg_id, title, description, released, metacritic, tags_json, fetched_at, not_found)
					VALUES(?,?,?,?,?,?,?,?,?)`,
					key, meta.RAWGID, meta.Title, meta.Description, meta.Released,
					meta.Metacritic, string(tagsJSON), meta.FetchedAt.Format(time.RFC3339), notFound,
				)
			}
		}
	}

	// Work queue
	if data, err := os.ReadFile(queuePath); err == nil {
		var q struct {
			InProgress map[string][]string `json:"in_progress"`
		}
		if json.Unmarshal(data, &q) == nil {
			for platform, names := range q.InProgress {
				for _, name := range names {
					tx.Exec("INSERT OR IGNORE INTO work_queue(platform, name) VALUES(?,?)", platform, name)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration commit: %w", err)
	}

	// Rename JSON files so migration doesn't re-run
	for _, p := range []string{tagsPath, gamesPath, metaPath, queuePath} {
		if _, err := os.Stat(p); err == nil {
			os.Rename(p, p+".migrated")
		}
	}
	return nil
}

// ── Tag queries ───────────────────────────────────────────────────────────

func dbListTags() ([]string, error) {
	rows, err := db.Query("SELECT name FROM tags ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func dbAddTag(tag string) error {
	_, err := db.Exec("INSERT OR IGNORE INTO tags(name) VALUES(?)", tag)
	return err
}

// ── Game queries ──────────────────────────────────────────────────────────

func dbGetGame(name string) (*GameEntry, error) {
	var g GameEntry
	var gameID int64
	err := db.QueryRow("SELECT id, name, platform FROM games WHERE name=?", name).Scan(&gameID, &g.Name, &g.Platform)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.CRCs, _ = dbGameCRCs(gameID)
	g.Tags, _ = dbGameTags(gameID)
	return &g, nil
}

func dbGetGameByCRC(crc string) (*GameEntry, error) {
	var gameID int64
	err := db.QueryRow("SELECT game_id FROM game_crcs WHERE crc=?", crc).Scan(&gameID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var g GameEntry
	db.QueryRow("SELECT name, platform FROM games WHERE id=?", gameID).Scan(&g.Name, &g.Platform)
	g.CRCs, _ = dbGameCRCs(gameID)
	g.Tags, _ = dbGameTags(gameID)
	return &g, nil
}

func dbListGames(platform string) ([]GameEntry, error) {
	var rows *sql.Rows
	var err error
	if platform == "" {
		rows, err = db.Query("SELECT id, name, platform FROM games ORDER BY name")
	} else {
		rows, err = db.Query("SELECT id, name, platform FROM games WHERE platform=? ORDER BY name", platform)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []GameEntry
	for rows.Next() {
		var g GameEntry
		var gameID int64
		rows.Scan(&gameID, &g.Name, &g.Platform)
		g.CRCs, _ = dbGameCRCs(gameID)
		g.Tags, _ = dbGameTags(gameID)
		games = append(games, g)
	}
	return games, rows.Err()
}

func dbSetGameTags(name, platform string, tags, crcs []string) ([]string, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Upsert game
	res, err := tx.Exec(
		"INSERT INTO games(name, platform) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET platform=excluded.platform",
		name, platform,
	)
	if err != nil {
		return nil, err
	}
	gameID, _ := res.LastInsertId()
	if gameID == 0 {
		tx.QueryRow("SELECT id FROM games WHERE name=?", name).Scan(&gameID)
	}

	// Add CRCs (additive)
	for _, crc := range crcs {
		if crc != "" {
			tx.Exec("INSERT OR IGNORE INTO game_crcs(game_id, crc) VALUES(?,?)", gameID, strings.ToUpper(crc))
		}
	}

	// Replace tags
	tx.Exec("DELETE FROM game_tags WHERE game_id=?", gameID)
	for _, tag := range tags {
		tx.Exec("INSERT OR IGNORE INTO tags(name) VALUES(?)", tag)
		var tagID int64
		tx.QueryRow("SELECT id FROM tags WHERE name=?", tag).Scan(&tagID)
		tx.Exec("INSERT OR IGNORE INTO game_tags(game_id, tag_id) VALUES(?,?)", gameID, tagID)
	}

	// Clear in-progress
	tx.Exec("DELETE FROM work_queue WHERE platform=? AND name=?", platform, name)

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Return merged CRC list
	allCRCs, _ := dbGameCRCs(gameID)
	return allCRCs, nil
}

func dbGameCRCs(gameID int64) ([]string, error) {
	rows, err := db.Query("SELECT crc FROM game_crcs WHERE game_id=? ORDER BY crc", gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var crcs []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		crcs = append(crcs, c)
	}
	return crcs, rows.Err()
}

func dbGameTags(gameID int64) ([]string, error) {
	rows, err := db.Query("SELECT t.name FROM tags t JOIN game_tags gt ON t.id=gt.tag_id WHERE gt.game_id=? ORDER BY t.name", gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ── Tagged game names (for queue dedup) ───────────────────────────────────

func dbTaggedNames(platform string) (map[string]struct{}, error) {
	rows, err := db.Query(`
		SELECT DISTINCT g.name FROM games g
		JOIN game_tags gt ON g.id=gt.game_id
		WHERE g.platform=?`, platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := make(map[string]struct{})
	for rows.Next() {
		var n string
		rows.Scan(&n)
		done[n] = struct{}{}
	}
	return done, rows.Err()
}

// ── RAWG cache queries ───────────────────────────────────────────────────

func dbGetCachedMeta(key string) (*RAWGMeta, bool) {
	var m RAWGMeta
	var tagsJSON string
	var fetchedAt string
	var notFound int
	err := db.QueryRow(
		"SELECT rawg_id, title, description, released, metacritic, tags_json, fetched_at, not_found, background_image FROM rawg_cache WHERE cache_key=?",
		key,
	).Scan(&m.RAWGID, &m.Title, &m.Description, &m.Released, &m.Metacritic, &tagsJSON, &fetchedAt, &notFound, &m.BackgroundImage)
	if err != nil {
		return nil, false
	}
	json.Unmarshal([]byte(tagsJSON), &m.Tags)
	m.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)
	m.NotFound = notFound != 0
	return &m, true
}

func dbPutCachedMeta(key string, meta *RAWGMeta) {
	tagsJSON, _ := json.Marshal(meta.Tags)
	notFound := 0
	if meta.NotFound {
		notFound = 1
	}
	db.Exec(`INSERT OR REPLACE INTO rawg_cache(cache_key, rawg_id, title, description, released, metacritic, tags_json, fetched_at, not_found, background_image)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		key, meta.RAWGID, meta.Title, meta.Description, meta.Released,
		meta.Metacritic, string(tagsJSON), meta.FetchedAt.Format(time.RFC3339), notFound, meta.BackgroundImage,
	)
}

func dbListCachedKeys() ([]string, error) {
	rows, err := db.Query("SELECT cache_key FROM rawg_cache WHERE not_found = 0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ── Work queue queries ───────────────────────────────────────────────────

func dbInProgress(platform string) (map[string]struct{}, error) {
	rows, err := db.Query("SELECT name FROM work_queue WHERE platform=?", platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]struct{})
	for rows.Next() {
		var n string
		rows.Scan(&n)
		m[n] = struct{}{}
	}
	return m, rows.Err()
}

func dbAddInProgress(platform string, names []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, n := range names {
		tx.Exec("INSERT OR IGNORE INTO work_queue(platform, name) VALUES(?,?)", platform, n)
	}
	return tx.Commit()
}

func dbResetQueue(platform string) error {
	_, err := db.Exec("DELETE FROM work_queue WHERE platform=?", platform)
	return err
}

// ── Tag counts for serve.go ──────────────────────────────────────────────

// ── Playlist queries ────────────────────────────────────────────────────

type PlaylistEntry struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	GameCount   int    `json:"game_count"`
}

func dbCreatePlaylist(name, description string, gameNames []string) (*PlaylistEntry, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		"INSERT INTO playlists(name, description) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET description=excluded.description",
		name, description,
	)
	if err != nil {
		return nil, err
	}
	plID, _ := res.LastInsertId()
	if plID == 0 {
		tx.QueryRow("SELECT id FROM playlists WHERE name=?", name).Scan(&plID)
	}

	// Replace game list
	tx.Exec("DELETE FROM playlist_games WHERE playlist_id=?", plID)
	for i, gn := range gameNames {
		var gameID int64
		err := tx.QueryRow("SELECT id FROM games WHERE name=?", gn).Scan(&gameID)
		if err != nil {
			continue // skip games not in DB
		}
		tx.Exec("INSERT OR IGNORE INTO playlist_games(playlist_id, game_id, position) VALUES(?,?,?)", plID, gameID, i)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	var entry PlaylistEntry
	db.QueryRow("SELECT id, name, description, created_at FROM playlists WHERE id=?", plID).Scan(
		&entry.ID, &entry.Name, &entry.Description, &entry.CreatedAt,
	)
	db.QueryRow("SELECT COUNT(*) FROM playlist_games WHERE playlist_id=?", plID).Scan(&entry.GameCount)
	return &entry, nil
}

func dbGetPlaylist(name string) (*PlaylistEntry, []GameEntry, error) {
	var entry PlaylistEntry
	var plID int64
	err := db.QueryRow("SELECT id, name, description, created_at FROM playlists WHERE name=?", name).Scan(
		&plID, &entry.Name, &entry.Description, &entry.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	entry.ID = plID

	rows, err := db.Query(`
		SELECT g.id, g.name, g.platform FROM games g
		JOIN playlist_games pg ON g.id=pg.game_id
		WHERE pg.playlist_id=?
		ORDER BY pg.position`, plID)
	if err != nil {
		return &entry, nil, err
	}
	defer rows.Close()

	var games []GameEntry
	for rows.Next() {
		var g GameEntry
		var gameID int64
		rows.Scan(&gameID, &g.Name, &g.Platform)
		g.CRCs, _ = dbGameCRCs(gameID)
		g.Tags, _ = dbGameTags(gameID)
		games = append(games, g)
	}
	entry.GameCount = len(games)
	return &entry, games, rows.Err()
}

func dbListPlaylists() ([]PlaylistEntry, error) {
	rows, err := db.Query(`
		SELECT p.id, p.name, p.description, p.created_at, COUNT(pg.game_id)
		FROM playlists p
		LEFT JOIN playlist_games pg ON p.id=pg.playlist_id
		GROUP BY p.id ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaylistEntry
	for rows.Next() {
		var e PlaylistEntry
		rows.Scan(&e.ID, &e.Name, &e.Description, &e.CreatedAt, &e.GameCount)
		out = append(out, e)
	}
	return out, rows.Err()
}

func dbDeletePlaylist(name string) error {
	_, err := db.Exec("DELETE FROM playlists WHERE name=?", name)
	return err
}

// ── Embedding queries ───────────────────────────────────────────────────

func dbUnembeddedTags() ([]string, error) {
	rows, err := db.Query(`
		SELECT t.name FROM tags t
		LEFT JOIN tag_embeddings te ON t.id = te.tag_id
		WHERE te.tag_id IS NULL
		ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func dbTagCounts() ([]tagCount, error) {
	rows, err := db.Query(`
		SELECT t.name, COUNT(*) as cnt
		FROM tags t JOIN game_tags gt ON t.id=gt.tag_id
		GROUP BY t.id ORDER BY cnt DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tagCount
	for rows.Next() {
		var tc tagCount
		rows.Scan(&tc.Tag, &tc.Count)
		out = append(out, tc)
	}
	return out, rows.Err()
}

// ── Saved views ───────────────────────────────────────────────────────────────

type savedView struct {
	Name          string `json:"name"`
	XTag          string `json:"x_tag"`
	YTag          string `json:"y_tag"`
	PointsJSON    string `json:"points_json"`
	PlatformsJSON string `json:"platforms_json"`
	CreatedAt     string `json:"created_at"`
}

func dbSaveView(name, xTag, yTag, pointsJSON, platformsJSON string) error {
	_, err := db.Exec(`
		INSERT INTO saved_views(name, x_tag, y_tag, points_json, platforms_json)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			x_tag=excluded.x_tag, y_tag=excluded.y_tag,
			points_json=excluded.points_json, platforms_json=excluded.platforms_json,
			created_at=datetime('now')`,
		name, xTag, yTag, pointsJSON, platformsJSON)
	return err
}

func dbListViews() ([]savedView, error) {
	rows, err := db.Query(`SELECT name, x_tag, y_tag, platforms_json, created_at FROM saved_views ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []savedView
	for rows.Next() {
		var v savedView
		rows.Scan(&v.Name, &v.XTag, &v.YTag, &v.PlatformsJSON, &v.CreatedAt)
		out = append(out, v)
	}
	return out, rows.Err()
}

func dbGetView(name string) (*savedView, error) {
	var v savedView
	err := db.QueryRow(`SELECT name, x_tag, y_tag, points_json, platforms_json, created_at FROM saved_views WHERE name=?`, name).
		Scan(&v.Name, &v.XTag, &v.YTag, &v.PointsJSON, &v.PlatformsJSON, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ── Manual review queue ───────────────────────────────────────────────────────

type reviewEntry struct {
	Name     string
	Platform string
	Notes    string
	Skip     bool
}

// dbListReviewQueue returns games that are hidden (no valid rawg_cache entry)
// and not already marked skip in manual_corrections.
func dbListReviewQueue() ([]reviewEntry, error) {
	rows, err := db.Query(`
		SELECT g.name, g.platform, COALESCE(mc.notes, ''), COALESCE(mc.skip, 0)
		FROM games g
		LEFT JOIN rawg_cache rc ON rc.cache_key = lower(replace(trim(g.name), ' ', '-'))
		LEFT JOIN manual_corrections mc ON mc.game_name = g.name
		WHERE (rc.cache_key IS NULL OR rc.not_found = 1)
		  AND COALESCE(mc.skip, 0) = 0
		ORDER BY g.platform, g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reviewEntry
	for rows.Next() {
		var e reviewEntry
		var skip int
		rows.Scan(&e.Name, &e.Platform, &e.Notes, &skip)
		e.Skip = skip != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func dbFlagForReview(gameName, notes string) error {
	_, err := db.Exec(`
		INSERT INTO manual_corrections(game_name, notes)
		VALUES(?, ?)
		ON CONFLICT(game_name) DO UPDATE SET notes=excluded.notes, reviewed_at=datetime('now')`,
		gameName, notes)
	return err
}

func dbMarkSkip(gameName, notes string) error {
	_, err := db.Exec(`
		INSERT INTO manual_corrections(game_name, notes, skip)
		VALUES(?, ?, 1)
		ON CONFLICT(game_name) DO UPDATE SET skip=1, notes=excluded.notes, reviewed_at=datetime('now')`,
		gameName, notes)
	return err
}

func dbRecordCorrection(gameName string, rawgID int, notes string) error {
	_, err := db.Exec(`
		INSERT INTO manual_corrections(game_name, rawg_id, notes, skip)
		VALUES(?, ?, ?, 0)
		ON CONFLICT(game_name) DO UPDATE SET
			rawg_id=excluded.rawg_id, notes=excluded.notes, skip=0, reviewed_at=datetime('now')`,
		gameName, rawgID, notes)
	return err
}
