//go:build !slim

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SimilarTag struct {
	Tag      string `json:"tag"`
	Distance int    `json:"distance"`
}

func toolSchemas() []map[string]interface{} {
	base := []map[string]interface{}{
		{
			"name":        "list_tags",
			"description": "Return all known vibe tags.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "check_tag",
			"description": "Check if a tag exists exactly and return similar tags by edit distance. Always call this before add_tag.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tag": map[string]interface{}{"type": "string", "description": "Tag to check"},
				},
				"required": []string{"tag"},
			},
		},
		{
			"name":        "add_tag",
			"description": "Add a new tag. Only call this after check_tag confirms no similar tag exists.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tag": map[string]interface{}{"type": "string"},
				},
				"required": []string{"tag"},
			},
		},
		{
			"name":        "set_game_tags",
			"description": "Upsert a game entry with vibe tags and CRC32 identifiers. CRCs are additive — existing CRCs are kept.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":     map[string]interface{}{"type": "string", "description": "Canonical game title (e.g. 'Golden Axe')"},
					"platform": map[string]interface{}{"type": "string", "description": "Platform ID (gba, genesis, snes, etc.)"},
					"tags":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"crcs":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "CRC32 hashes for this game's known dumps/regions (uppercase hex, e.g. '74C65A49')"},
				},
				"required": []string{"name", "platform", "tags"},
			},
		},
		{
			"name":        "get_game",
			"description": "Get a game entry by canonical name.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "get_game_by_crc",
			"description": "Look up a game by one of its CRC32 values. Returns the full entry if found.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"crc": map[string]interface{}{"type": "string", "description": "CRC32 in uppercase hex"},
				},
				"required": []string{"crc"},
			},
		},
		{
			"name":        "list_games",
			"description": "List all games, optionally filtered by platform.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"platform": map[string]interface{}{"type": "string", "description": "Optional platform filter"},
				},
			},
		},
		{
			"name":        "clean_rom_name",
			"description": "Convert a raw ROM filename to a canonical game title by stripping region codes, version markers, and patch flags. Always use this rather than doing the stripping yourself.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filename": map[string]interface{}{"type": "string", "description": "Raw filename, e.g. 'Golden Axe (USA, Europe).zip'"},
				},
				"required": []string{"filename"},
			},
		},
		{
			"name":        "fetch_game_metadata",
			"description": "Fetch RAWG.io metadata for a game (description and community tags). Results are cached permanently — each game hits the API at most once across all sessions.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string", "description": "Canonical game title"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "get_work_batch",
			"description": "Return the next batch of untagged ROMs for a platform and mark them in-progress so parallel agents don't get duplicate assignments. Call set_game_tags when done to clear in-progress status.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"platform":   map[string]interface{}{"type": "string", "description": "Platform ID (gba, genesis, etc.)"},
					"batch_size": map[string]interface{}{"type": "integer", "description": "Number of games to return (default 8)"},
				},
				"required": []string{"platform"},
			},
		},
		{
			"name":        "prime_metadata_cache",
			"description": "Pre-fetch RAWG metadata for all untagged ROMs in a platform directory. Run this before a swarm to warm the cache so agents don't hit the API directly. Fetches sequentially to respect rate limits.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"platform": map[string]interface{}{"type": "string", "description": "Platform ID"},
				},
				"required": []string{"platform"},
			},
		},
		{
			"name":        "fetch_game_metadata_by_id",
			"description": "Fetch RAWG metadata for a game using a known RAWG game ID, bypassing name-based search entirely. Use this when the correct RAWG page has been identified via web research (the ID is the number in the RAWG URL, e.g. rawg.io/games/12345). Stores result in cache under the provided game name.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":    map[string]interface{}{"type": "string", "description": "Game name as it appears in the library (used as cache key)"},
					"rawg_id": map[string]interface{}{"type": "integer", "description": "RAWG game ID from the game's URL on rawg.io"},
				},
				"required": []string{"name", "rawg_id"},
			},
		},
		{
			"name":        "resync_metadata_cache",
			"description": "Re-fetch RAWG metadata for all previously cached games, updating background images and any other fields that have changed. Skips not_found entries.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "reset_queue",
			"description": "Clear all in-progress assignments for a platform. Use to recover after a crashed or abandoned swarm run.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"platform": map[string]interface{}{"type": "string", "description": "Platform ID"},
				},
				"required": []string{"platform"},
			},
		},
		{
			"name":        "create_playlist",
			"description": "Create or update a playlist from a list of game names. Games must already exist in the database.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":        map[string]interface{}{"type": "string", "description": "Playlist name (e.g. 'Chill SMS Games')"},
					"description": map[string]interface{}{"type": "string", "description": "Short description of the playlist's vibe or purpose"},
					"games":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "List of canonical game names"},
				},
				"required": []string{"name", "games"},
			},
		},
		{
			"name":        "get_playlist",
			"description": "Get a playlist by name, including all its games and their tags.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "list_playlists",
			"description": "List all saved playlists with game counts.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "delete_playlist",
			"description": "Delete a playlist by name.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "sync_embeddings",
			"description": "Compute semantic embeddings for all tags missing from the vector store. Requires Python 3 with sentence-transformers. Run after adding new tags to enable semantic search in check_tag and tag_clusters.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "merge_tags",
			"description": "Merge duplicate tags. Reassigns all game associations from the 'merge' tags into the 'keep' tag, then deletes the merged tags and their embeddings. Use after tag_clusters identifies duplicates.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"keep": map[string]interface{}{
						"type":        "string",
						"description": "The canonical tag to keep",
					},
					"merge": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Tags to merge into the keep tag and then delete",
					},
				},
				"required": []string{"keep", "merge"},
			},
		},
		{
			"name":        "delete_tag",
			"description": "Delete a tag entirely — removes it from all games and deletes its embedding. Use for tags that are too generic to be useful (e.g. bare 'action').",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Tag to delete",
					},
				},
				"required": []string{"tag"},
			},
		},
		{
			"name":        "tag_clusters",
			"description": "Group tags into semantic clusters by embedding similarity. Returns clusters where all members have cosine similarity above threshold. Useful for finding redundant tags and understanding vocabulary structure. Requires embeddings (run sync_embeddings first).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"threshold": map[string]interface{}{
						"type":        "number",
						"description": "Minimum cosine similarity to cluster together (default 0.75, range 0.5–0.95)",
					},
				},
			},
		},
		{
			"name":        "sync_game_embeddings",
			"description": "Compute description embeddings for all games that have cached RAWG metadata but no embedding yet. Uses only the game description (no community tags) to create a pure game-similarity vector. Run after fetch_game_metadata to build the game vector space.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "similar_games",
			"description": "Find games most similar to a given game by description embedding. Optionally filter to games that have specific vibe tags. Requires game embeddings (run sync_game_embeddings first).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Canonical game name to find similar games for",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Optional: only return games that have ALL of these vibe tags",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Max results to return (default 10)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "game_vibes",
			"description": "Find the vibe tags closest to a game's description in vector space. Shows what vibes the game's description naturally evokes, even if those tags aren't assigned to the game. Useful for discovering missing tags.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Canonical game name",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Max tags to return (default 10)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "emit_playlist",
			"description": "Write a playlist to disk in the specified format. Supported formats: pegasus, m3u, retroarch, emulationstation. Returns the output file path.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":    map[string]interface{}{"type": "string", "description": "Playlist name"},
					"format":  map[string]interface{}{"type": "string", "description": "Output format: pegasus, m3u, retroarch, emulationstation"},
					"out_dir": map[string]interface{}{"type": "string", "description": "Output directory (default: ~/Emulation/playlists)"},
				},
				"required": []string{"name", "format"},
			},
		},
		{
			"name":        "flag_for_review",
			"description": "Flag a game for human review — use when you reject a bad RAWG match or the game is genuinely not found. The game will appear in the /review queue so a human can supply the correct RAWG ID.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"game_name": map[string]interface{}{"type": "string", "description": "Canonical game name as it appears in the library"},
					"notes":     map[string]interface{}{"type": "string", "description": "Brief reason for flagging (e.g. 'RAWG returned Starship Troopers: Extermination instead of VR Troopers')"},
				},
				"required": []string{"game_name"},
			},
		},
	}
	return append(base, extraToolSchemas()...)
}

func callTool(name string, args map[string]interface{}) (string, bool) {
	switch name {
	case "list_tags":
		return toolListTags()
	case "check_tag":
		tag, _ := args["tag"].(string)
		return toolCheckTag(tag)
	case "add_tag":
		tag, _ := args["tag"].(string)
		return toolAddTag(tag)
	case "set_game_tags":
		gameName, _ := args["name"].(string)
		platform, _ := args["platform"].(string)
		tags := toStrSlice(args["tags"])
		crcs := toStrSlice(args["crcs"])
		return toolSetGameTags(gameName, platform, tags, crcs)
	case "get_game":
		n, _ := args["name"].(string)
		return toolGetGame(n)
	case "get_game_by_crc":
		crc, _ := args["crc"].(string)
		return toolGetGameByCRC(crc)
	case "list_games":
		platform, _ := args["platform"].(string)
		return toolListGames(platform)
	case "clean_rom_name":
		filename, _ := args["filename"].(string)
		return toolCleanROMName(filename)
	case "fetch_game_metadata":
		name, _ := args["name"].(string)
		return toolFetchGameMetadata(name)
	case "fetch_game_metadata_by_id":
		name, _ := args["name"].(string)
		rawgID := 0
		if v, ok := args["rawg_id"].(float64); ok {
			rawgID = int(v)
		}
		return toolFetchGameMetadataByID(name, rawgID)
	case "get_work_batch":
		platform, _ := args["platform"].(string)
		batchSize := 8
		if v, ok := args["batch_size"].(float64); ok && v > 0 {
			batchSize = int(v)
		}
		return toolGetWorkBatch(platform, batchSize)
	case "prime_metadata_cache":
		platform, _ := args["platform"].(string)
		return toolPrimeMetadataCache(platform)
	case "resync_metadata_cache":
		return toolResyncMetadataCache()
	case "reset_queue":
		platform, _ := args["platform"].(string)
		return toolResetQueue(platform)
	case "create_playlist":
		n, _ := args["name"].(string)
		desc, _ := args["description"].(string)
		gameNames := toStrSlice(args["games"])
		return toolCreatePlaylist(n, desc, gameNames)
	case "get_playlist":
		n, _ := args["name"].(string)
		return toolGetPlaylist(n)
	case "list_playlists":
		return toolListPlaylists()
	case "delete_playlist":
		n, _ := args["name"].(string)
		return toolDeletePlaylist(n)
	case "merge_tags":
		keep, _ := args["keep"].(string)
		merge := toStrSlice(args["merge"])
		return toolMergeTags(keep, merge)
	case "delete_tag":
		tag, _ := args["tag"].(string)
		return toolDeleteTag(tag)
	case "sync_embeddings":
		return toolSyncEmbeddings()
	case "sync_game_embeddings":
		return toolSyncGameEmbeddings()
	case "similar_games":
		n, _ := args["name"].(string)
		tags := toStrSlice(args["tags"])
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		return toolSimilarGames(n, tags, limit)
	case "game_vibes":
		n, _ := args["name"].(string)
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		return toolGameVibes(n, limit)
	case "tag_clusters":
		threshold := float32(0.75)
		if v, ok := args["threshold"].(float64); ok && v > 0 {
			threshold = float32(v)
		}
		return toolTagClusters(threshold)
	case "emit_playlist":
		n, _ := args["name"].(string)
		format, _ := args["format"].(string)
		outDir, _ := args["out_dir"].(string)
		return toolEmitPlaylist(n, format, outDir)
	case "flag_for_review":
		gameName, _ := args["game_name"].(string)
		notes, _ := args["notes"].(string)
		if gameName == "" {
			return `{"error":"game_name required"}`, true
		}
		if err := dbFlagForReview(gameName, notes); err != nil {
			return fmt.Sprintf(`{"error":"%s"}`, err), true
		}
		return fmt.Sprintf(`{"flagged":true,"game":"%s"}`, gameName), false
	default:
		return callExtraTool(name, args)
	}
}

func toStrSlice(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ── Tool implementations ───────────────────────────────────────────────────

func toolMergeTags(keep string, merge []string) (string, bool) {
	if keep == "" {
		return `{"error":"keep tag required"}`, true
	}
	if len(merge) == 0 {
		return `{"error":"merge list required"}`, true
	}

	// Verify keep tag exists
	var keepID int64
	err := db.QueryRow("SELECT id FROM tags WHERE name = ?", keep).Scan(&keepID)
	if err != nil {
		return fmt.Sprintf(`{"error":"keep tag %q not found"}`, keep), true
	}

	merged := 0
	reassigned := 0
	for _, tag := range merge {
		var mergeID int64
		err := db.QueryRow("SELECT id FROM tags WHERE name = ?", tag).Scan(&mergeID)
		if err != nil {
			continue // skip tags that don't exist
		}

		// Reassign game associations (ignore dupes)
		res, _ := db.Exec(`
			INSERT OR IGNORE INTO game_tags (game_id, tag_id)
			SELECT game_id, ? FROM game_tags WHERE tag_id = ?`, keepID, mergeID)
		n, _ := res.RowsAffected()
		reassigned += int(n)

		// Delete old associations, embedding, and tag
		db.Exec("DELETE FROM game_tags WHERE tag_id = ?", mergeID)
		db.Exec("DELETE FROM tag_embeddings WHERE tag_id = ?", mergeID)
		db.Exec("DELETE FROM tags WHERE id = ?", mergeID)
		merged++
	}

	// Clear embedding cache so it reloads
	embeddingCache = nil

	out, _ := json.Marshal(map[string]interface{}{
		"keep":       keep,
		"merged":     merged,
		"reassigned": reassigned,
	})
	return string(out), false
}

func toolDeleteTag(tag string) (string, bool) {
	if tag == "" {
		return `{"error":"tag required"}`, true
	}

	var tagID int64
	err := db.QueryRow("SELECT id FROM tags WHERE name = ?", tag).Scan(&tagID)
	if err != nil {
		return fmt.Sprintf(`{"error":"tag %q not found"}`, tag), true
	}

	// Count affected games before deleting
	var affected int
	db.QueryRow("SELECT COUNT(*) FROM game_tags WHERE tag_id = ?", tagID).Scan(&affected)

	db.Exec("DELETE FROM game_tags WHERE tag_id = ?", tagID)
	db.Exec("DELETE FROM tag_embeddings WHERE tag_id = ?", tagID)
	db.Exec("DELETE FROM tags WHERE id = ?", tagID)

	embeddingCache = nil

	out, _ := json.Marshal(map[string]interface{}{
		"deleted":        tag,
		"games_affected": affected,
	})
	return string(out), false
}

func toolListTags() (string, bool) {
	tags, err := dbListTags()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	data, _ := json.Marshal(tags)
	return string(data), false
}

func toolCheckTag(tag string) (string, bool) {
	if tag == "" {
		return `{"error":"tag required"}`, true
	}
	norm := normalize(tag)

	allTags, err := dbListTags()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	// Exact match
	for _, t := range allTags {
		if normalize(t) == norm {
			out, _ := json.Marshal(map[string]interface{}{
				"exact":    true,
				"match":    t,
				"similar":  []SimilarTag{},
				"semantic": []SemanticMatch{},
			})
			return string(out), false
		}
	}

	// Levenshtein matches
	type scored struct {
		tag  string
		dist int
	}
	var candidates []scored
	for _, t := range allTags {
		d := levenshtein(norm, normalize(t))
		nt := normalize(t)
		if d <= 3 || strings.Contains(nt, norm) || strings.Contains(norm, nt) {
			candidates = append(candidates, scored{t, d})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dist < candidates[j].dist })
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	similar := make([]SimilarTag, len(candidates))
	for i, c := range candidates {
		similar[i] = SimilarTag{Tag: c.tag, Distance: c.dist}
	}

	// Semantic matches (graceful: empty if embeddings unavailable)
	var semantic []SemanticMatch
	queryVec := embedTagIfNeeded(tag)
	if queryVec != nil {
		raw := semanticSearch(queryVec, 5)
		for _, s := range raw {
			if normalize(s.Tag) != norm && s.Similarity > 0.5 {
				semantic = append(semantic, s)
			}
		}
	}
	if semantic == nil {
		semantic = []SemanticMatch{}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"exact":    false,
		"similar":  similar,
		"semantic": semantic,
	})
	return string(out), false
}

func toolAddTag(tag string) (string, bool) {
	if tag == "" {
		return `{"error":"tag required"}`, true
	}
	norm := normalize(tag)

	allTags, err := dbListTags()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	for _, t := range allTags {
		if normalize(t) == norm {
			out, _ := json.Marshal(map[string]interface{}{"added": false, "tag": t})
			return string(out), false
		}
	}

	if err := dbAddTag(tag); err != nil {
		return fmt.Sprintf(`{"error":"save failed: %s"}`, err), true
	}
	out, _ := json.Marshal(map[string]interface{}{"added": true, "tag": tag})
	return string(out), false
}

func toolSetGameTags(name, platform string, tags, crcs []string) (string, bool) {
	if name == "" || platform == "" {
		return `{"error":"name and platform required"}`, true
	}
	original := make([]string, len(tags))
	copy(original, tags)
	tags = normalizeTagBatch(tags)
	merged, err := dbSetGameTags(name, platform, tags, crcs)
	if err != nil {
		return fmt.Sprintf(`{"error":"save failed: %s"}`, err), true
	}

	// Report what was normalized so the caller/LLM can summarize.
	var normalized []map[string]string
	for i, orig := range original {
		if i < len(tags) && orig != tags[i] {
			normalized = append(normalized, map[string]string{"from": orig, "to": tags[i]})
		}
	}
	out, _ := json.Marshal(map[string]interface{}{
		"ok":         true,
		"name":       name,
		"platform":   platform,
		"tags":       tags,
		"crcs":       merged,
		"normalized": normalized,
	})
	return string(out), false
}

func toolGetGame(name string) (string, bool) {
	entry, err := dbGetGame(name)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	if entry == nil {
		out, _ := json.Marshal(map[string]interface{}{"found": false})
		return string(out), false
	}
	out, _ := json.Marshal(map[string]interface{}{
		"found": true, "name": entry.Name, "platform": entry.Platform,
		"crcs": entry.CRCs, "tags": entry.Tags,
	})
	return string(out), false
}

func toolGetGameByCRC(crc string) (string, bool) {
	if crc == "" {
		return `{"error":"crc required"}`, true
	}
	crc = strings.ToUpper(crc)

	entry, err := dbGetGameByCRC(crc)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	if entry == nil {
		out, _ := json.Marshal(map[string]interface{}{"found": false})
		return string(out), false
	}
	out, _ := json.Marshal(map[string]interface{}{
		"found": true, "name": entry.Name, "platform": entry.Platform,
		"crcs": entry.CRCs, "tags": entry.Tags,
	})
	return string(out), false
}

func toolListGames(platform string) (string, bool) {
	games, err := dbListGames(platform)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(games)
	return string(out), false
}

func toolCleanROMName(filename string) (string, bool) {
	if filename == "" {
		return `{"error":"filename required"}`, true
	}
	out, _ := json.Marshal(map[string]interface{}{"cleaned": CleanROMName(filename)})
	return string(out), false
}

func toolFetchGameMetadata(name string) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	meta, cached, err := FetchGameMetadata(name)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	tagNames := make([]string, len(meta.Tags))
	for i, t := range meta.Tags {
		tagNames[i] = t.Name
	}
	out, _ := json.Marshal(map[string]interface{}{
		"cached":      cached,
		"found":       !meta.NotFound,
		"title":       meta.Title,
		"description": meta.Description,
		"rawg_tags":   tagNames,
		"released":    meta.Released,
		"metacritic":  meta.Metacritic,
	})
	return string(out), false
}

func toolFetchGameMetadataByID(name string, rawgID int) (string, bool) {
	if name == "" || rawgID == 0 {
		return `{"error":"name and rawg_id required"}`, true
	}
	meta, err := FetchGameMetadataByID(name, rawgID)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	tagNames := make([]string, len(meta.Tags))
	for i, t := range meta.Tags {
		tagNames[i] = t.Name
	}
	out, _ := json.Marshal(map[string]interface{}{
		"found":       true,
		"title":       meta.Title,
		"description": meta.Description,
		"rawg_tags":   tagNames,
		"released":    meta.Released,
		"metacritic":  meta.Metacritic,
	})
	return string(out), false
}

func toolGetWorkBatch(platform string, batchSize int) (string, bool) {
	if platform == "" {
		return `{"error":"platform required"}`, true
	}
	batch, err := GetBatch(platform, batchSize)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(map[string]interface{}{
		"platform": platform,
		"count":    len(batch),
		"items":    batch,
	})
	return string(out), false
}

func toolPrimeMetadataCache(platform string) (string, bool) {
	if platform == "" {
		return `{"error":"platform required"}`, true
	}
	files, err := ListROMFiles(romBase, platform)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	seen := make(map[string]struct{})
	fetched, skipped, failed := 0, 0, 0

	for _, filename := range files {
		name := CleanROMName(filename)
		if name == "" {
			continue
		}
		if _, s := seen[name]; s {
			continue
		}
		seen[name] = struct{}{}

		_, cached, err := FetchGameMetadata(name)
		if err != nil {
			failed++
			continue
		}
		if cached {
			skipped++
		} else {
			fetched++
		}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"platform":       platform,
		"fetched":        fetched,
		"already_cached": skipped,
		"failed":         failed,
	})
	return string(out), false
}

func toolResetQueue(platform string) (string, bool) {
	if platform == "" {
		return `{"error":"platform required"}`, true
	}
	ResetQueue(platform)
	out, _ := json.Marshal(map[string]interface{}{"ok": true, "platform": platform})
	return string(out), false
}

// ── Playlist tool implementations ──────────────────────────────────────────

func toolCreatePlaylist(name, description string, gameNames []string) (string, bool) {
	if name == "" || len(gameNames) == 0 {
		return `{"error":"name and games required"}`, true
	}
	entry, err := dbCreatePlaylist(name, description, gameNames)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(entry)
	return string(out), false
}

func toolGetPlaylist(name string) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	entry, games, err := dbGetPlaylist(name)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	if entry == nil {
		return `{"found":false}`, false
	}
	out, _ := json.Marshal(map[string]interface{}{
		"found":       true,
		"name":        entry.Name,
		"description": entry.Description,
		"created_at":  entry.CreatedAt,
		"game_count":  entry.GameCount,
		"games":       games,
	})
	return string(out), false
}

func toolListPlaylists() (string, bool) {
	playlists, err := dbListPlaylists()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(playlists)
	return string(out), false
}

func toolDeletePlaylist(name string) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	if err := dbDeletePlaylist(name); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(map[string]interface{}{"ok": true})
	return string(out), false
}

func toolSyncEmbeddings() (string, bool) {
	count, err := syncAllEmbeddings()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	total := 0
	ensureEmbeddingCache()
	total = len(embeddingCache)
	out, _ := json.Marshal(map[string]interface{}{
		"synced": count,
		"total":  total,
	})
	return string(out), false
}

func toolTagClusters(threshold float32) (string, bool) {
	ensureEmbeddingCache()
	if len(embeddingCache) == 0 {
		return `{"error":"no embeddings available — run sync_embeddings first"}`, true
	}

	clusters := buildTagClusters(threshold)

	clusteredCount := 0
	for _, members := range clusters {
		clusteredCount += len(members)
	}

	out, _ := json.Marshal(map[string]interface{}{
		"clusters":       clusters,
		"threshold":      threshold,
		"total_tags":     len(embeddingCache),
		"clustered_tags": clusteredCount,
		"singleton_tags": len(embeddingCache) - clusteredCount,
		"cluster_count":  len(clusters),
	})
	return string(out), false
}

func toolSyncGameEmbeddings() (string, bool) {
	count, err := syncAllGameEmbeddings()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	ensureGameEmbeddingCache()
	out, _ := json.Marshal(map[string]interface{}{
		"synced": count,
		"total":  len(gameEmbeddingCache),
	})
	return string(out), false
}

func toolSimilarGames(name string, tags []string, limit int) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	results, err := similarGames(name, tags, limit)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(results)
	return string(out), false
}

func toolGameVibes(name string, limit int) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	results, err := nearestTagsForGame(name, limit)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(results)
	return string(out), false
}

func toolResyncMetadataCache() (string, bool) {
	keys, err := dbListCachedKeys()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	updated, failed := 0, 0
	for _, key := range keys {
		// Delete existing entry to force re-fetch
		db.Exec("DELETE FROM rawg_cache WHERE cache_key = ?", key)
		// Re-fetch using the key as the name (best effort)
		_, _, err := FetchGameMetadata(strings.ReplaceAll(key, "-", " "))
		if err != nil {
			failed++
		} else {
			updated++
		}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"updated": updated,
		"failed":  failed,
	})
	return string(out), false
}

func toolEmitPlaylist(name, format, outDir string) (string, bool) {
	if name == "" || format == "" {
		return `{"error":"name and format required"}`, true
	}
	if outDir == "" {
		home, _ := os.UserHomeDir()
		outDir = filepath.Join(home, "Emulation", "playlists")
	}
	path, _, err := dbEmitPlaylist(name, format, outDir)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(map[string]interface{}{"ok": true, "path": path, "format": format})
	return string(out), false
}
