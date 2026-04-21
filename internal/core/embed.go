//go:build !slim

package core

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
)

//go:embed embed_tags.py
var embedScript string

const EmbeddingModel = "BAAI/bge-m3"

// SemanticMatch is a tag with its cosine similarity score.
type SemanticMatch struct {
	Tag        string  `json:"tag"`
	Similarity float32 `json:"similarity"`
}

// In-memory caches, loaded lazily from DB.
var embeddingCache map[string][]float32     // tag name → vector
var gameEmbeddingCache map[string][]float32 // game name → vector

func EnsureEmbeddingCache() {
	if embeddingCache != nil {
		return
	}
	var err error
	embeddingCache, err = db.GetAllEmbeddings()
	if err != nil || embeddingCache == nil {
		embeddingCache = make(map[string][]float32)
	}
}

func EnsureGameEmbeddingCache() {
	if gameEmbeddingCache != nil {
		return
	}
	var err error
	gameEmbeddingCache, err = db.GetAllGameEmbeddings()
	if err != nil || gameEmbeddingCache == nil {
		gameEmbeddingCache = make(map[string][]float32)
	}
}

// ── Python sidecar ──────────────────────────────────────────────────────────

// ComputeEmbeddings shells out to Python to embed a batch of tag strings.
// Returns nil and error if Python or sentence-transformers is unavailable.
func ComputeEmbeddings(tags []string) (map[string][]float32, error) {
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

// SemanticSearch returns the topK tags most similar to queryVec from the cache.
func SemanticSearch(queryVec []float32, topK int) []SemanticMatch {
	EnsureEmbeddingCache()

	type scored struct {
		tag string
		sim float32
	}
	results := make([]scored, 0, len(embeddingCache))
	for tag, vec := range embeddingCache {
		sim := CosineSim(queryVec, vec)
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

// EmbedTagIfNeeded ensures the given tag has an embedding in the cache.
// If missing, it batch-embeds all unembedded tags (including this one).
// Returns the vector, or nil if embedding is unavailable.
func EmbedTagIfNeeded(tag string) []float32 {
	EnsureEmbeddingCache()
	if vec, ok := embeddingCache[tag]; ok {
		return vec
	}

	// Gather all unembedded tags + the query tag
	missing, _ := db.UnembeddedTags()
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

	embeddings, err := ComputeEmbeddings(missing)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rom-tagger: embedding failed (lexical fallback): %v\n", err)
		return nil
	}

	// Persist embeddings for tags that exist in the DB
	_ = db.StoreEmbeddings(embeddings, EmbeddingModel)

	// Update in-memory cache
	for t, v := range embeddings {
		embeddingCache[t] = v
	}

	return embeddingCache[tag]
}

// ── Batch operations ────────────────────────────────────────────────────────

// SyncAllEmbeddings computes embeddings for all tags missing from the store.
func SyncAllEmbeddings() (int, error) {
	missing, err := db.UnembeddedTags()
	if err != nil {
		return 0, err
	}
	if len(missing) == 0 {
		return 0, nil
	}

	embeddings, err := ComputeEmbeddings(missing)
	if err != nil {
		return 0, err
	}

	if err := db.StoreEmbeddings(embeddings, EmbeddingModel); err != nil {
		return 0, err
	}

	// Update cache
	EnsureEmbeddingCache()
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

// EmbedGame embeds a game's RAWG description (no community tags) and stores it.
func EmbedGame(gameName, description string) ([]float32, error) {
	if description == "" {
		return nil, fmt.Errorf("no description for %q", gameName)
	}

	vecs, err := ComputeEmbeddings([]string{description})
	if err != nil {
		return nil, err
	}
	vec, ok := vecs[description]
	if !ok {
		return nil, fmt.Errorf("embedding not returned for %q", gameName)
	}

	if err := db.StoreGameEmbedding(gameName, vec, EmbeddingModel); err != nil {
		return nil, err
	}

	// Update cache
	EnsureGameEmbeddingCache()
	gameEmbeddingCache[gameName] = vec
	return vec, nil
}

// SyncAllGameEmbeddings embeds all games that have RAWG descriptions but no embedding.
func SyncAllGameEmbeddings() (int, error) {
	games, err := db.UnembeddedGames()
	if err != nil {
		return 0, err
	}
	if len(games) == 0 {
		return 0, nil
	}

	// Build text list — just descriptions, no tags
	texts := make([]string, len(games))
	for i, g := range games {
		texts[i] = g.Description
	}

	vecs, err := ComputeEmbeddings(texts)
	if err != nil {
		return 0, err
	}

	// Map back to game names
	gameVecs := make(map[string][]float32, len(games))
	for i, g := range games {
		if vec, ok := vecs[texts[i]]; ok {
			gameVecs[g.Name] = vec
		}
	}

	if err := db.StoreGameEmbeddings(gameVecs, EmbeddingModel); err != nil {
		return 0, err
	}

	EnsureGameEmbeddingCache()
	for name, vec := range gameVecs {
		gameEmbeddingCache[name] = vec
	}

	return len(gameVecs), nil
}

// SimilarGames returns games most similar to the query game by description embedding.
// If filterTags is non-empty, only games with ALL of those tags are considered.
func SimilarGames(gameName string, filterTags []string, limit int) ([]GameSimilarity, error) {
	EnsureGameEmbeddingCache()

	queryVec, ok := gameEmbeddingCache[gameName]
	if !ok {
		return nil, fmt.Errorf("no embedding for %q — run embed_game or sync_game_embeddings first", gameName)
	}

	// Get tag filter set if specified
	var filteredNames map[string]bool
	if len(filterTags) > 0 {
		names, err := db.GamesWithAllTags(filterTags)
		if err != nil {
			return nil, err
		}
		filteredNames = make(map[string]bool, len(names))
		for _, n := range names {
			filteredNames[n] = true
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
		if filteredNames != nil {
			if !filteredNames[name] {
				continue
			}
		}
		sim := CosineSim(queryVec, vec)
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

// NearestTagsForGame returns the vibe tags closest to a game's description vector.
func NearestTagsForGame(gameName string, topK int) ([]SemanticMatch, error) {
	EnsureGameEmbeddingCache()
	vec, ok := gameEmbeddingCache[gameName]
	if !ok {
		return nil, fmt.Errorf("no embedding for %q", gameName)
	}
	return SemanticSearch(vec, topK), nil
}

// BuildTagClusters groups tags by semantic similarity above a threshold.
func BuildTagClusters(threshold float32) map[string][]string {
	EnsureEmbeddingCache()

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
			if CosineSim(embeddingCache[tag], embeddingCache[other]) >= threshold {
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

// ClearEmbeddingCache resets the in-memory tag embedding cache so it reloads on next access.
func ClearEmbeddingCache() {
	embeddingCache = nil
}

// EmbeddingCacheLen returns the number of entries in the tag embedding cache.
func EmbeddingCacheLen() int {
	return len(embeddingCache)
}

// GameEmbeddingCacheLen returns the number of entries in the game embedding cache.
func GameEmbeddingCacheLen() int {
	return len(gameEmbeddingCache)
}
