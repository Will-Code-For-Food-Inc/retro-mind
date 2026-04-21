//go:build !slim

package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
)

//go:embed embed_tags.py
var embedScript string

const embeddingModel = "BAAI/bge-m3"

// SemanticMatch is a tag with its cosine similarity score.
type SemanticMatch struct {
	Tag        string  `json:"tag"`
	Similarity float32 `json:"similarity"`
}

// In-memory caches, loaded lazily from DB.
var embeddingCache map[string][]float32     // tag name → vector
var gameEmbeddingCache map[string][]float32 // game name → vector

func ensureEmbeddingCache() {
	if embeddingCache != nil {
		return
	}
	var err error
	embeddingCache, err = dbGetAllEmbeddings()
	if err != nil || embeddingCache == nil {
		embeddingCache = make(map[string][]float32)
	}
}

func ensureGameEmbeddingCache() {
	if gameEmbeddingCache != nil {
		return
	}
	var err error
	gameEmbeddingCache, err = dbGetAllGameEmbeddings()
	if err != nil || gameEmbeddingCache == nil {
		gameEmbeddingCache = make(map[string][]float32)
	}
}

// ── Python sidecar ──────────────────────────────────────────────────────────

// computeEmbeddings shells out to Python to embed a batch of tag strings.
// Returns nil and error if Python or sentence-transformers is unavailable.
func computeEmbeddings(tags []string) (map[string][]float32, error) {
	if len(tags) == 0 {
		return map[string][]float32{}, nil
	}

	tmpFile, err := os.CreateTemp("", "rom-tagger-embed-*.py")
	if err != nil {
		return nil, fmt.Errorf("create temp script: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(embedScript); err != nil {
		tmpFile.Close()
		return nil, err
	}
	tmpFile.Close()

	input, _ := json.Marshal(tags)
	cmd := exec.Command("uv", "run", "--no-project",
		"--extra-index-url", "https://download.pytorch.org/whl/cu124",
		"--with", "sentence-transformers",
		tmpFile.Name())
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python3 embed: %v\n%s", err, stderr.String())
	}

	// Check for error response
	var errResp map[string]string
	if json.Unmarshal(stdout.Bytes(), &errResp) == nil {
		if e, ok := errResp["error"]; ok {
			return nil, fmt.Errorf("%s", e)
		}
	}

	// Python outputs float64 JSON; parse and convert to float32
	var raw map[string][]float64
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("parse embeddings: %w", err)
	}

	out := make(map[string][]float32, len(raw))
	for tag, vec := range raw {
		f32 := make([]float32, len(vec))
		for i, v := range vec {
			f32[i] = float32(v)
		}
		out[tag] = f32
	}
	return out, nil
}

// ── Vector encoding ─────────────────────────────────────────────────────────

func vectorToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// blobToVector and cosineSim are in vec.go (shared with slim build).

// semanticSearch returns the topK tags most similar to queryVec from the cache.
func semanticSearch(queryVec []float32, topK int) []SemanticMatch {
	ensureEmbeddingCache()

	type scored struct {
		tag string
		sim float32
	}
	results := make([]scored, 0, len(embeddingCache))
	for tag, vec := range embeddingCache {
		sim := cosineSim(queryVec, vec)
		results = append(results, scored{tag, sim})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].sim > results[j].sim
	})
	if len(results) > topK {
		results = results[:topK]
	}
	out := make([]SemanticMatch, len(results))
	for i, r := range results {
		out[i] = SemanticMatch{
			Tag:        r.tag,
			Similarity: float32(math.Round(float64(r.sim)*1000) / 1000),
		}
	}
	return out
}

// ── Cache-aware embedding ───────────────────────────────────────────────────

// embedTagIfNeeded ensures the given tag has an embedding in the cache.
// If missing, it batch-embeds all unembedded tags (including this one).
// Returns the vector, or nil if embedding is unavailable.
func embedTagIfNeeded(tag string) []float32 {
	ensureEmbeddingCache()
	if vec, ok := embeddingCache[tag]; ok {
		return vec
	}

	// Gather all unembedded tags + the query tag
	missing, _ := dbUnembeddedTags()
	hasTarg := false
	for _, t := range missing {
		if t == tag {
			hasTarg = true
			break
		}
	}
	if !hasTarg {
		missing = append(missing, tag)
	}

	embeddings, err := computeEmbeddings(missing)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: embedding failed (lexical fallback): %v\n", err)
		return nil
	}

	// Persist embeddings for tags that exist in the DB
	_ = dbStoreEmbeddings(embeddings, embeddingModel)

	// Update in-memory cache
	for t, v := range embeddings {
		embeddingCache[t] = v
	}

	return embeddingCache[tag]
}

// ── Batch operations ────────────────────────────────────────────────────────

// syncAllEmbeddings computes embeddings for all tags missing from the store.
func syncAllEmbeddings() (int, error) {
	missing, err := dbUnembeddedTags()
	if err != nil {
		return 0, err
	}
	if len(missing) == 0 {
		return 0, nil
	}

	embeddings, err := computeEmbeddings(missing)
	if err != nil {
		return 0, err
	}

	if err := dbStoreEmbeddings(embeddings, embeddingModel); err != nil {
		return 0, err
	}

	// Update cache
	ensureEmbeddingCache()
	for t, v := range embeddings {
		embeddingCache[t] = v
	}

	return len(embeddings), nil
}

// ── Game embeddings ────────────────────────────────────────────────────────

// GameSimilarity pairs a game name with its cosine similarity score.
type GameSimilarity struct {
	Name       string  `json:"name"`
	Similarity float32 `json:"similarity"`
}

// embedGame embeds a game's RAWG description (no community tags) and stores it.
func embedGame(gameName, description string) ([]float32, error) {
	if description == "" {
		return nil, fmt.Errorf("no description for %q", gameName)
	}

	vecs, err := computeEmbeddings([]string{description})
	if err != nil {
		return nil, err
	}
	vec, ok := vecs[description]
	if !ok {
		return nil, fmt.Errorf("embedding not returned for %q", gameName)
	}

	if err := dbStoreGameEmbedding(gameName, vec, embeddingModel); err != nil {
		return nil, err
	}

	// Update cache
	ensureGameEmbeddingCache()
	gameEmbeddingCache[gameName] = vec
	return vec, nil
}

// syncAllGameEmbeddings embeds all games that have RAWG descriptions but no embedding.
func syncAllGameEmbeddings() (int, error) {
	games, err := dbUnembeddedGames()
	if err != nil {
		return 0, err
	}
	if len(games) == 0 {
		return 0, nil
	}

	// Build text list — just descriptions, no tags
	texts := make([]string, len(games))
	for i, g := range games {
		texts[i] = g.description
	}

	vecs, err := computeEmbeddings(texts)
	if err != nil {
		return 0, err
	}

	// Map back to game names
	gameVecs := make(map[string][]float32, len(games))
	for i, g := range games {
		if vec, ok := vecs[texts[i]]; ok {
			gameVecs[g.name] = vec
		}
	}

	if err := dbStoreGameEmbeddings(gameVecs, embeddingModel); err != nil {
		return 0, err
	}

	ensureGameEmbeddingCache()
	for name, vec := range gameVecs {
		gameEmbeddingCache[name] = vec
	}

	return len(gameVecs), nil
}

// similarGames returns games most similar to the query game by description embedding.
// If filterTags is non-empty, only games with ALL of those tags are considered.
func similarGames(gameName string, filterTags []string, limit int) ([]GameSimilarity, error) {
	ensureGameEmbeddingCache()

	queryVec, ok := gameEmbeddingCache[gameName]
	if !ok {
		return nil, fmt.Errorf("no embedding for %q — run embed_game or sync_game_embeddings first", gameName)
	}

	// Get tag filter set if specified
	var requiredGameIDs map[int64]bool
	if len(filterTags) > 0 {
		requiredGameIDs = make(map[int64]bool)
		// Find games that have ALL required tags
		query := `
			SELECT gt.game_id FROM game_tags gt
			JOIN tags t ON gt.tag_id = t.id
			WHERE t.name IN (?` + strings.Repeat(",?", len(filterTags)-1) + `)
			GROUP BY gt.game_id
			HAVING COUNT(DISTINCT t.name) = ?`
		args := make([]interface{}, len(filterTags)+1)
		for i, t := range filterTags {
			args[i] = t
		}
		args[len(filterTags)] = len(filterTags)
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			requiredGameIDs[id] = true
		}
	}

	// Build a name→gameID lookup if we need tag filtering
	var nameToID map[string]int64
	if requiredGameIDs != nil {
		nameToID = make(map[string]int64)
		rows, err := db.Query("SELECT id, name FROM games")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			rows.Scan(&id, &name)
			nameToID[name] = id
		}
	}

	type scored struct {
		name string
		sim  float32
	}
	var results []scored
	for name, vec := range gameEmbeddingCache {
		if name == gameName {
			continue
		}
		if requiredGameIDs != nil {
			gid, ok := nameToID[name]
			if !ok || !requiredGameIDs[gid] {
				continue
			}
		}
		sim := cosineSim(queryVec, vec)
		results = append(results, scored{name, sim})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].sim > results[j].sim
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	out := make([]GameSimilarity, len(results))
	for i, r := range results {
		out[i] = GameSimilarity{
			Name:       r.name,
			Similarity: float32(math.Round(float64(r.sim)*1000) / 1000),
		}
	}
	return out, nil
}

// nearestTagsForGame returns the vibe tags closest to a game's description vector.
func nearestTagsForGame(gameName string, topK int) ([]SemanticMatch, error) {
	ensureGameEmbeddingCache()
	vec, ok := gameEmbeddingCache[gameName]
	if !ok {
		return nil, fmt.Errorf("no embedding for %q", gameName)
	}
	return semanticSearch(vec, topK), nil
}

// buildTagClusters groups tags by semantic similarity above a threshold.
func buildTagClusters(threshold float32) map[string][]string {
	ensureEmbeddingCache()

	tags := make([]string, 0, len(embeddingCache))
	for t := range embeddingCache {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	assigned := make(map[string]bool)
	clusters := make(map[string][]string)

	for _, tag := range tags {
		if assigned[tag] {
			continue
		}
		members := []string{tag}
		assigned[tag] = true

		for _, other := range tags {
			if assigned[other] {
				continue
			}
			if cosineSim(embeddingCache[tag], embeddingCache[other]) >= threshold {
				members = append(members, other)
				assigned[other] = true
			}
		}
		if len(members) > 1 {
			clusters[tag] = members
		}
	}
	return clusters
}
