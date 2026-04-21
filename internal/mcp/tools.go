package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/agent"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/core"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/rawg"
)

// SimilarTag pairs a tag with its edit distance.
type SimilarTag struct {
	Tag      string `json:"tag"`
	Distance int    `json:"distance"`
}

// ToolSchemas returns the JSON-RPC tool definitions for all MCP tools.
func (s *Server) ToolSchemas() []map[string]interface{} {
	return []map[string]interface{}{
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
		{
			"name":        "agent_query",
			"description": "Run a natural language query against the ROM library. The agent will use available tools to fulfill the request and return a terse data-only response.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Natural language instruction",
					},
				},
				"required": []string{"prompt"},
			},
		},
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

// generateFn returns a core.GenerateFn wrapping the agent, or nil if no agent.
func (s *Server) generateFn() core.GenerateFn {
	if s.Agent == nil {
		return nil
	}
	return func(messages []core.ChatMessage) (string, error) {
		// Convert core.ChatMessage to agent.ChatMessage
		agentMsgs := make([]agent.ChatMessage, len(messages))
		for i, m := range messages {
			agentMsgs[i] = agent.ChatMessage{Role: m.Role, Content: m.Content}
		}
		return s.Agent.Generate(agentMsgs)
	}
}

// ── Fuzzy matching helpers (inlined from fuzzy.go) ────────────────────────

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// ── Tool implementations ───────────────────────────────────────────────────

func (s *Server) toolMergeTags(keep string, merge []string) (string, bool) {
	if keep == "" {
		return `{"error":"keep tag required"}`, true
	}
	if len(merge) == 0 {
		return `{"error":"merge list required"}`, true
	}

	// Verify keep tag exists
	var keepID int64
	err := db.DB.QueryRow("SELECT id FROM tags WHERE name = ?", keep).Scan(&keepID)
	if err != nil {
		return fmt.Sprintf(`{"error":"keep tag %q not found"}`, keep), true
	}

	merged := 0
	reassigned := 0
	for _, tag := range merge {
		var mergeID int64
		err := db.DB.QueryRow("SELECT id FROM tags WHERE name = ?", tag).Scan(&mergeID)
		if err != nil {
			continue // skip tags that don't exist
		}

		// Reassign game associations (ignore dupes)
		res, _ := db.DB.Exec(`
			INSERT OR IGNORE INTO game_tags (game_id, tag_id)
			SELECT game_id, ? FROM game_tags WHERE tag_id = ?`, keepID, mergeID)
		n, _ := res.RowsAffected()
		reassigned += int(n)

		// Delete old associations, embedding, and tag
		db.DB.Exec("DELETE FROM game_tags WHERE tag_id = ?", mergeID)
		db.DB.Exec("DELETE FROM tag_embeddings WHERE tag_id = ?", mergeID)
		db.DB.Exec("DELETE FROM tags WHERE id = ?", mergeID)
		merged++
	}

	// Clear embedding cache so it reloads
	core.ClearEmbeddingCache()

	out, _ := json.Marshal(map[string]interface{}{
		"keep":       keep,
		"merged":     merged,
		"reassigned": reassigned,
	})
	return string(out), false
}

func (s *Server) toolDeleteTag(tag string) (string, bool) {
	if tag == "" {
		return `{"error":"tag required"}`, true
	}

	var tagID int64
	err := db.DB.QueryRow("SELECT id FROM tags WHERE name = ?", tag).Scan(&tagID)
	if err != nil {
		return fmt.Sprintf(`{"error":"tag %q not found"}`, tag), true
	}

	// Count affected games before deleting
	var affected int
	db.DB.QueryRow("SELECT COUNT(*) FROM game_tags WHERE tag_id = ?", tagID).Scan(&affected)

	db.DB.Exec("DELETE FROM game_tags WHERE tag_id = ?", tagID)
	db.DB.Exec("DELETE FROM tag_embeddings WHERE tag_id = ?", tagID)
	db.DB.Exec("DELETE FROM tags WHERE id = ?", tagID)

	core.ClearEmbeddingCache()

	out, _ := json.Marshal(map[string]interface{}{
		"deleted":        tag,
		"games_affected": affected,
	})
	return string(out), false
}

func (s *Server) toolListTags() (string, bool) {
	tags, err := db.ListTags()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	data, _ := json.Marshal(tags)
	return string(data), false
}

func (s *Server) toolCheckTag(tag string) (string, bool) {
	if tag == "" {
		return `{"error":"tag required"}`, true
	}
	norm := normalize(tag)

	allTags, err := db.ListTags()
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
				"semantic": []core.SemanticMatch{},
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
	var semantic []core.SemanticMatch
	queryVec := core.EmbedTagIfNeeded(tag)
	if queryVec != nil {
		raw := core.SemanticSearch(queryVec, 5)
		for _, sm := range raw {
			if normalize(sm.Tag) != norm && sm.Similarity > 0.5 {
				semantic = append(semantic, sm)
			}
		}
	}
	if semantic == nil {
		semantic = []core.SemanticMatch{}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"exact":    false,
		"similar":  similar,
		"semantic": semantic,
	})
	return string(out), false
}

func (s *Server) toolAddTag(tag string) (string, bool) {
	if tag == "" {
		return `{"error":"tag required"}`, true
	}
	norm := normalize(tag)

	allTags, err := db.ListTags()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	for _, t := range allTags {
		if normalize(t) == norm {
			out, _ := json.Marshal(map[string]interface{}{"added": false, "tag": t})
			return string(out), false
		}
	}

	if err := db.AddTag(tag); err != nil {
		return fmt.Sprintf(`{"error":"save failed: %s"}`, err), true
	}
	out, _ := json.Marshal(map[string]interface{}{"added": true, "tag": tag})
	return string(out), false
}

func (s *Server) toolSetGameTags(name, platform string, tags, crcs []string) (string, bool) {
	if name == "" || platform == "" {
		return `{"error":"name and platform required"}`, true
	}
	original := make([]string, len(tags))
	copy(original, tags)
	tags = core.NormalizeTagBatch(tags, s.generateFn())
	merged, err := db.SetGameTags(name, platform, tags, crcs)
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

func (s *Server) toolGetGame(name string) (string, bool) {
	entry, err := db.GetGame(name)
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

func (s *Server) toolGetGameByCRC(crc string) (string, bool) {
	if crc == "" {
		return `{"error":"crc required"}`, true
	}
	crc = strings.ToUpper(crc)

	entry, err := db.GetGameByCRC(crc)
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

func (s *Server) toolListGames(platform string) (string, bool) {
	games, err := db.ListGames(platform)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(games)
	return string(out), false
}

func (s *Server) toolCleanROMName(filename string) (string, bool) {
	if filename == "" {
		return `{"error":"filename required"}`, true
	}
	out, _ := json.Marshal(map[string]interface{}{"cleaned": core.CleanROMName(filename)})
	return string(out), false
}

func (s *Server) toolFetchGameMetadata(name string) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	meta, cached, err := rawg.FetchGameMetadata(name)
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

func (s *Server) toolFetchGameMetadataByID(name string, rawgID int) (string, bool) {
	if name == "" || rawgID == 0 {
		return `{"error":"name and rawg_id required"}`, true
	}
	meta, err := rawg.FetchGameMetadataByID(name, rawgID)
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

func (s *Server) toolGetWorkBatch(platform string, batchSize int) (string, bool) {
	if platform == "" {
		return `{"error":"platform required"}`, true
	}
	batch, err := core.GetBatch(s.RomBase, platform, batchSize)
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

func (s *Server) toolPrimeMetadataCache(platform string) (string, bool) {
	if platform == "" {
		return `{"error":"platform required"}`, true
	}
	files, err := core.ListROMFiles(s.RomBase, platform)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	seen := make(map[string]struct{})
	fetched, skipped, failed := 0, 0, 0

	for _, filename := range files {
		name := core.CleanROMName(filename)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		_, cached, err := rawg.FetchGameMetadata(name)
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

func (s *Server) toolResetQueue(platform string) (string, bool) {
	if platform == "" {
		return `{"error":"platform required"}`, true
	}
	core.ResetQueue(platform)
	out, _ := json.Marshal(map[string]interface{}{"ok": true, "platform": platform})
	return string(out), false
}

// ── Playlist tool implementations ──────────────────────────────────────────

func (s *Server) toolCreatePlaylist(name, description string, gameNames []string) (string, bool) {
	if name == "" || len(gameNames) == 0 {
		return `{"error":"name and games required"}`, true
	}
	entry, err := db.CreatePlaylist(name, description, gameNames)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(entry)
	return string(out), false
}

func (s *Server) toolGetPlaylist(name string) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	entry, games, err := db.GetPlaylist(name)
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

func (s *Server) toolListPlaylists() (string, bool) {
	playlists, err := db.ListPlaylists()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(playlists)
	return string(out), false
}

func (s *Server) toolDeletePlaylist(name string) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	if err := db.DeletePlaylist(name); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(map[string]interface{}{"ok": true})
	return string(out), false
}

func (s *Server) toolSyncEmbeddings() (string, bool) {
	count, err := core.SyncAllEmbeddings()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	core.EnsureEmbeddingCache()
	total := core.EmbeddingCacheLen()
	out, _ := json.Marshal(map[string]interface{}{
		"synced": count,
		"total":  total,
	})
	return string(out), false
}

func (s *Server) toolTagClusters(threshold float32) (string, bool) {
	core.EnsureEmbeddingCache()
	if core.EmbeddingCacheLen() == 0 {
		return `{"error":"no embeddings available — run sync_embeddings first"}`, true
	}

	clusters := core.BuildTagClusters(threshold)

	clusteredCount := 0
	for _, members := range clusters {
		clusteredCount += len(members)
	}

	out, _ := json.Marshal(map[string]interface{}{
		"clusters":       clusters,
		"threshold":      threshold,
		"total_tags":     core.EmbeddingCacheLen(),
		"clustered_tags": clusteredCount,
		"singleton_tags": core.EmbeddingCacheLen() - clusteredCount,
		"cluster_count":  len(clusters),
	})
	return string(out), false
}

func (s *Server) toolSyncGameEmbeddings() (string, bool) {
	count, err := core.SyncAllGameEmbeddings()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	core.EnsureGameEmbeddingCache()
	out, _ := json.Marshal(map[string]interface{}{
		"synced": count,
		"total":  core.GameEmbeddingCacheLen(),
	})
	return string(out), false
}

func (s *Server) toolSimilarGames(name string, tags []string, limit int) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	results, err := core.SimilarGames(name, tags, limit)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(results)
	return string(out), false
}

func (s *Server) toolGameVibes(name string, limit int) (string, bool) {
	if name == "" {
		return `{"error":"name required"}`, true
	}
	results, err := core.NearestTagsForGame(name, limit)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(results)
	return string(out), false
}

func (s *Server) toolResyncMetadataCache() (string, bool) {
	keys, err := db.ListCachedKeys()
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	updated, failed := 0, 0
	for _, key := range keys {
		// Delete existing entry to force re-fetch
		db.DB.Exec("DELETE FROM rawg_cache WHERE cache_key = ?", key)
		// Re-fetch using the key as the name (best effort)
		_, _, err := rawg.FetchGameMetadata(strings.ReplaceAll(key, "-", " "))
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

func (s *Server) toolEmitPlaylist(name, format, outDir string) (string, bool) {
	if name == "" || format == "" {
		return `{"error":"name and format required"}`, true
	}
	if outDir == "" {
		home, _ := os.UserHomeDir()
		outDir = filepath.Join(home, "Emulation", "playlists")
	}
	path, _, err := core.EmitPlaylist(s.RomBase, name, format, outDir)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	out, _ := json.Marshal(map[string]interface{}{"ok": true, "path": path, "format": format})
	return string(out), false
}

func (s *Server) toolFlagForReview(gameName, notes string) (string, bool) {
	if gameName == "" {
		return `{"error":"game_name required"}`, true
	}
	if err := db.FlagForReview(gameName, notes); err != nil {
		return fmt.Sprintf(`{"error":"%s"}`, err), true
	}
	return fmt.Sprintf(`{"flagged":true,"game":"%s"}`, gameName), false
}
