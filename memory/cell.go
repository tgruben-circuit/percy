package memory

import (
	"database/sql"
	"fmt"
	"strings"
)

// Cell is an atomic knowledge unit extracted from a conversation or file.
type Cell struct {
	CellID     string
	TopicID    string
	SourceType string
	SourceID   string
	SourceName string
	CellType   string
	Salience   float64
	Content    string
	Embedding  []byte
}

// CellResult is a search result for a cell query.
type CellResult struct {
	CellID     string
	TopicID    string
	SourceType string
	SourceID   string
	SourceName string
	CellType   string
	Salience   float64
	Content    string
	Score      float64
}

// Topic groups related cells under a theme.
type Topic struct {
	TopicID   string
	Name      string
	Summary   string
	Embedding []byte
	CellCount int
}

// TopicResult is a search result for a topic query.
type TopicResult struct {
	TopicID   string
	Name      string
	Summary   string
	Score     float64
	UpdatedAt string
}

// InsertCell inserts or replaces a cell and updates the parent topic's cell_count.
func (d *DB) InsertCell(c Cell) error {
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO cells (cell_id, topic_id, source_type, source_id, source_name, cell_type, salience, content, embedding)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CellID, c.TopicID, c.SourceType, c.SourceID, c.SourceName, c.CellType, c.Salience, c.Content, c.Embedding,
	)
	if err != nil {
		return fmt.Errorf("memory: insert cell: %w", err)
	}

	if c.TopicID != "" {
		_, err = d.db.Exec(
			`UPDATE topics SET cell_count = (SELECT COUNT(*) FROM cells WHERE topic_id = ?) WHERE topic_id = ?`,
			c.TopicID, c.TopicID,
		)
		if err != nil {
			return fmt.Errorf("memory: update topic cell_count: %w", err)
		}
	}
	return nil
}

// SupersedeCells marks the given cell IDs as superseded.
func (d *DB) SupersedeCells(cellIDs []string) error {
	if len(cellIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(cellIDs))
	args := make([]any, len(cellIDs))
	for i, id := range cellIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(`UPDATE cells SET superseded = TRUE WHERE cell_id IN (%s)`, strings.Join(placeholders, ","))
	_, err := d.db.Exec(q, args...)
	if err != nil {
		return fmt.Errorf("memory: supersede cells: %w", err)
	}
	return nil
}

// DeleteCellsBySource removes all cells for a given source.
func (d *DB) DeleteCellsBySource(sourceType, sourceID string) error {
	_, err := d.db.Exec(`DELETE FROM cells WHERE source_type = ? AND source_id = ?`, sourceType, sourceID)
	if err != nil {
		return fmt.Errorf("memory: delete cells by source: %w", err)
	}
	return nil
}

// GetCellsByTopic returns cells for a topic, ordered by salience DESC.
// If includeSuperseded is false, superseded cells are excluded.
func (d *DB) GetCellsByTopic(topicID string, includeSuperseded bool) ([]Cell, error) {
	q := `SELECT cell_id, topic_id, source_type, source_id, source_name, cell_type, salience, content, embedding
		  FROM cells WHERE topic_id = ?`
	if !includeSuperseded {
		q += ` AND superseded = FALSE`
	}
	q += ` ORDER BY salience DESC`

	rows, err := d.db.Query(q, topicID)
	if err != nil {
		return nil, fmt.Errorf("memory: get cells by topic: %w", err)
	}
	defer rows.Close()

	var cells []Cell
	for rows.Next() {
		var c Cell
		if err := rows.Scan(&c.CellID, &c.TopicID, &c.SourceType, &c.SourceID, &c.SourceName, &c.CellType, &c.Salience, &c.Content, &c.Embedding); err != nil {
			return nil, fmt.Errorf("memory: scan cell: %w", err)
		}
		cells = append(cells, c)
	}
	return cells, rows.Err()
}

// UnsummarizedCellCount returns the number of non-superseded cells created
// after the topic's last updated_at.
func (d *DB) UnsummarizedCellCount(topicID string) (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM cells
		 WHERE topic_id = ? AND superseded = FALSE
		   AND created_at >= (SELECT updated_at FROM topics WHERE topic_id = ?)`,
		topicID, topicID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("memory: unsummarized cell count: %w", err)
	}
	return count, nil
}

// UpsertTopic inserts or updates a topic.
func (d *DB) UpsertTopic(t Topic) error {
	_, err := d.db.Exec(
		`INSERT INTO topics (topic_id, name, summary, embedding, cell_count)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(topic_id) DO UPDATE SET
		   name = excluded.name,
		   summary = excluded.summary,
		   embedding = excluded.embedding,
		   cell_count = excluded.cell_count,
		   updated_at = CURRENT_TIMESTAMP`,
		t.TopicID, t.Name, t.Summary, t.Embedding, t.CellCount,
	)
	if err != nil {
		return fmt.Errorf("memory: upsert topic: %w", err)
	}
	return nil
}

// GetTopic returns a topic by ID. Returns nil, nil if not found.
func (d *DB) GetTopic(topicID string) (*Topic, error) {
	var t Topic
	err := d.db.QueryRow(
		`SELECT topic_id, name, COALESCE(summary, ''), embedding, cell_count FROM topics WHERE topic_id = ?`,
		topicID,
	).Scan(&t.TopicID, &t.Name, &t.Summary, &t.Embedding, &t.CellCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: get topic: %w", err)
	}
	return &t, nil
}

// AllTopics returns all topics with their embeddings.
func (d *DB) AllTopics() ([]Topic, error) {
	rows, err := d.db.Query(`SELECT topic_id, name, COALESCE(summary, ''), embedding, cell_count FROM topics`)
	if err != nil {
		return nil, fmt.Errorf("memory: all topics: %w", err)
	}
	defer rows.Close()

	var topics []Topic
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.TopicID, &t.Name, &t.Summary, &t.Embedding, &t.CellCount); err != nil {
			return nil, fmt.Errorf("memory: scan topic: %w", err)
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// SearchCellsFTS performs FTS5 search on non-superseded cells.
// If sourceType is non-empty, results are further filtered by source_type.
func (d *DB) SearchCellsFTS(query, sourceType string, limit int) ([]CellResult, error) {
	q := `SELECT c.cell_id, c.topic_id, c.source_type, c.source_id, c.source_name, c.cell_type, c.salience, c.content, f.rank
		  FROM cells_fts f
		  JOIN cells c ON c.rowid = f.rowid
		  WHERE cells_fts MATCH ? AND c.superseded = FALSE`

	args := []any{query}
	if sourceType != "" {
		q += ` AND c.source_type = ?`
		args = append(args, sourceType)
	}
	q += ` ORDER BY f.rank LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: search cells fts: %w", err)
	}
	defer rows.Close()

	var results []CellResult
	for rows.Next() {
		var cr CellResult
		if err := rows.Scan(&cr.CellID, &cr.TopicID, &cr.SourceType, &cr.SourceID, &cr.SourceName, &cr.CellType, &cr.Salience, &cr.Content, &cr.Score); err != nil {
			return nil, fmt.Errorf("memory: scan cell result: %w", err)
		}
		results = append(results, cr)
	}
	return results, rows.Err()
}

// SearchTopicsFTS performs FTS5 search on topic summaries.
func (d *DB) SearchTopicsFTS(query string, limit int) ([]TopicResult, error) {
	q := `SELECT t.topic_id, t.name, COALESCE(t.summary, ''), f.rank, t.updated_at
		  FROM topics_fts f
		  JOIN topics t ON t.rowid = f.rowid
		  WHERE topics_fts MATCH ?
		  ORDER BY f.rank LIMIT ?`

	rows, err := d.db.Query(q, query, limit)
	if err != nil {
		return nil, fmt.Errorf("memory: search topics fts: %w", err)
	}
	defer rows.Close()

	var results []TopicResult
	for rows.Next() {
		var tr TopicResult
		if err := rows.Scan(&tr.TopicID, &tr.Name, &tr.Summary, &tr.Score, &tr.UpdatedAt); err != nil {
			return nil, fmt.Errorf("memory: scan topic result: %w", err)
		}
		results = append(results, tr)
	}
	return results, rows.Err()
}
