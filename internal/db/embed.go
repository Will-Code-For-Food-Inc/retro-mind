package db

import (
	"encoding/binary"
	"fmt"
	"math"
)

// EmbeddingModel is the current model used for tag and game embeddings.
const EmbeddingModel = "BAAI/bge-m3"

// MigrateEmbeddingModel wipes embeddings from the old model so they get re-computed.
func MigrateEmbeddingModel() error {
	for _, table := range []string{"tag_embeddings", "game_embeddings"} {
		_, err := DB.Exec(fmt.Sprintf(
			"DELETE FROM %s WHERE model != ?", table), EmbeddingModel)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetAllEmbeddings returns all tag embeddings keyed by tag name.
func GetAllEmbeddings() (map[string][]float32, error) {
	rows, err := DB.Query(`
		SELECT t.name, te.vector FROM tags t
		JOIN tag_embeddings te ON t.id = te.tag_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]float32)
	for rows.Next() {
		var name string
		var blob []byte
		rows.Scan(&name, &blob)
		result[name] = blobToVector(blob)
	}
	return result, rows.Err()
}

// StoreEmbeddings stores tag embeddings in batch.
func StoreEmbeddings(embeddings map[string][]float32, model string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for tag, vec := range embeddings {
		var tagID int64
		err := tx.QueryRow("SELECT id FROM tags WHERE name=?", tag).Scan(&tagID)
		if err != nil {
			continue
		}
		blob := vectorToBlob(vec)
		tx.Exec(`INSERT OR REPLACE INTO tag_embeddings(tag_id, vector, model) VALUES(?,?,?)`,
			tagID, blob, model)
	}
	return tx.Commit()
}

// UnembeddedGames returns games that have RAWG metadata but no embedding yet.
func UnembeddedGames() ([]UnembeddedGame, error) {
	rows, err := DB.Query(`
		SELECT g.id, g.name
		FROM games g
		LEFT JOIN game_embeddings ge ON g.id = ge.game_id
		WHERE ge.game_id IS NULL
		ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnembeddedGame
	for rows.Next() {
		var g UnembeddedGame
		rows.Scan(&g.ID, &g.Name)
		key := CacheKey(g.Name)
		meta, ok := GetCachedMeta(key)
		if !ok || meta.NotFound || meta.Description == "" {
			continue
		}
		g.Description = meta.Description
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetAllGameEmbeddings returns all game embeddings keyed by game name.
func GetAllGameEmbeddings() (map[string][]float32, error) {
	rows, err := DB.Query(`
		SELECT g.name, ge.vector FROM games g
		JOIN game_embeddings ge ON g.id = ge.game_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]float32)
	for rows.Next() {
		var name string
		var blob []byte
		rows.Scan(&name, &blob)
		result[name] = blobToVector(blob)
	}
	return result, rows.Err()
}

// StoreGameEmbedding stores a single game embedding.
func StoreGameEmbedding(gameName string, vec []float32, model string) error {
	var gameID int64
	err := DB.QueryRow("SELECT id FROM games WHERE name=?", gameName).Scan(&gameID)
	if err != nil {
		return fmt.Errorf("game %q not found: %w", gameName, err)
	}
	blob := vectorToBlob(vec)
	_, err = DB.Exec(`INSERT OR REPLACE INTO game_embeddings(game_id, vector, model) VALUES(?,?,?)`,
		gameID, blob, model)
	return err
}

// StoreGameEmbeddings stores game embeddings in batch.
func StoreGameEmbeddings(embeddings map[string][]float32, model string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for name, vec := range embeddings {
		var gameID int64
		err := tx.QueryRow("SELECT id FROM games WHERE name=?", name).Scan(&gameID)
		if err != nil {
			continue
		}
		blob := vectorToBlob(vec)
		tx.Exec(`INSERT OR REPLACE INTO game_embeddings(game_id, vector, model) VALUES(?,?,?)`,
			gameID, blob, model)
	}
	return tx.Commit()
}

// ── Vector encoding helpers ─────────────────────────────────────────────────

func blobToVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func vectorToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
