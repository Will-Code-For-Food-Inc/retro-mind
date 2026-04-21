//go:build !slim

package main

import "fmt"

// migrateEmbeddingModel wipes embeddings from the old model so they get re-computed.
func migrateEmbeddingModel() error {
	for _, table := range []string{"tag_embeddings", "game_embeddings"} {
		_, err := db.Exec(fmt.Sprintf(
			"DELETE FROM %s WHERE model != ?", table), embeddingModel)
		if err != nil {
			return err
		}
	}
	return nil
}

func dbGetAllEmbeddings() (map[string][]float32, error) {
	rows, err := db.Query(`
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

func dbStoreEmbeddings(embeddings map[string][]float32, model string) error {
	tx, err := db.Begin()
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

func dbUnembeddedGames() ([]struct {
	id          int64
	name        string
	description string
}, error) {
	rows, err := db.Query(`
		SELECT g.id, g.name
		FROM games g
		LEFT JOIN game_embeddings ge ON g.id = ge.game_id
		WHERE ge.game_id IS NULL
		ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		id          int64
		name        string
		description string
	}
	for rows.Next() {
		var g struct {
			id          int64
			name        string
			description string
		}
		rows.Scan(&g.id, &g.name)
		key := cacheKey(g.name)
		meta, ok := dbGetCachedMeta(key)
		if !ok || meta.NotFound || meta.Description == "" {
			continue
		}
		g.description = meta.Description
		out = append(out, g)
	}
	return out, rows.Err()
}

func dbGetAllGameEmbeddings() (map[string][]float32, error) {
	rows, err := db.Query(`
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

func dbStoreGameEmbedding(gameName string, vec []float32, model string) error {
	var gameID int64
	err := db.QueryRow("SELECT id FROM games WHERE name=?", gameName).Scan(&gameID)
	if err != nil {
		return fmt.Errorf("game %q not found: %w", gameName, err)
	}
	blob := vectorToBlob(vec)
	_, err = db.Exec(`INSERT OR REPLACE INTO game_embeddings(game_id, vector, model) VALUES(?,?,?)`,
		gameID, blob, model)
	return err
}

func dbStoreGameEmbeddings(embeddings map[string][]float32, model string) error {
	tx, err := db.Begin()
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
