package main

// Embedding read functions used by the viz handler.
// These are safe in slim builds — they only read, never compute.

func dbGetTagEmbedding(tagName string) ([]float32, bool) {
	var blob []byte
	err := db.QueryRow(`
		SELECT te.vector FROM tag_embeddings te
		JOIN tags t ON t.id = te.tag_id
		WHERE t.name = ?`, tagName).Scan(&blob)
	if err != nil {
		return nil, false
	}
	return blobToVector(blob), true
}

func dbGetAllGameEmbeddingsForViz() (map[string][]float32, error) {
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
