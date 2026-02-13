package memory

import "fmt"

// SearchResult represents a single FTS5 search hit.
type SearchResult struct {
	ChunkID    string
	SourceType string
	SourceID   string
	SourceName string
	Text       string
	Score      float64
}

// InsertChunk inserts or replaces a chunk in the chunks table.
func (d *DB) InsertChunk(chunkID, sourceType, sourceID, sourceName string, chunkIndex int, text string, embedding []byte) error {
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO chunks (chunk_id, source_type, source_id, source_name, chunk_index, text, token_count, embedding)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		chunkID, sourceType, sourceID, sourceName, chunkIndex, text, EstimateTokens(text), embedding,
	)
	if err != nil {
		return fmt.Errorf("memory: insert chunk: %w", err)
	}
	return nil
}

// DeleteChunksBySource removes all chunks for a given source.
func (d *DB) DeleteChunksBySource(sourceType, sourceID string) error {
	_, err := d.db.Exec(`DELETE FROM chunks WHERE source_type = ? AND source_id = ?`, sourceType, sourceID)
	if err != nil {
		return fmt.Errorf("memory: delete chunks: %w", err)
	}
	return nil
}

// SearchFTS performs a full-text search using FTS5 MATCH with BM25 ranking.
// If sourceType is empty, all source types are searched.
func (d *DB) SearchFTS(query, sourceType string, limit int) ([]SearchResult, error) {
	q := `SELECT c.chunk_id, c.source_type, c.source_id, c.source_name, c.text, f.rank
		FROM chunks_fts f
		JOIN chunks c ON c.rowid = f.rowid
		WHERE chunks_fts MATCH ?`

	var args []any
	args = append(args, query)
	if sourceType != "" {
		q += ` AND c.source_type = ?`
		args = append(args, sourceType)
	}
	q += ` ORDER BY f.rank LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: search fts: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		if err := rows.Scan(&sr.ChunkID, &sr.SourceType, &sr.SourceID, &sr.SourceName, &sr.Text, &sr.Score); err != nil {
			return nil, fmt.Errorf("memory: scan search result: %w", err)
		}
		results = append(results, sr)
	}
	return results, rows.Err()
}
