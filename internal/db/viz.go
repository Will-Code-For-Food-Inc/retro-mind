package db

// GetTagEmbedding returns the embedding vector for a single tag.
func GetTagEmbedding(tagName string) ([]float32, bool) {
	var blob []byte
	err := DB.QueryRow(`
		SELECT te.vector FROM tag_embeddings te
		JOIN tags t ON t.id = te.tag_id
		WHERE t.name = ?`, tagName).Scan(&blob)
	if err != nil {
		return nil, false
	}
	return blobToVector(blob), true
}

// GetAllGameEmbeddingsForViz returns all game embeddings for visualization.
func GetAllGameEmbeddingsForViz() (map[string][]float32, error) {
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
